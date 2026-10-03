/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

// Package webui is the WebView2 based user interface. The page itself is
// plain HTML/CSS/JS embedded in the binary; this package hosts it in a Win32
// window, provides the tray icon and bridges calls to the manager service.
package webui

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"github.com/wailsapp/go-webview2/pkg/edge"
	"github.com/wailsapp/go-webview2/webviewloader"
	"golang.org/x/sys/windows"

	"github.com/amnezia-vpn/amneziawg-windows-client/manager"
)

const (
	// WindowClass is shared with the legacy UI, so that raising an already
	// running UI works the same with either.
	WindowClass = "BetterAmnezia UI - Manage Tunnels"
	// RaiseMsg is sent by a second instance to bring the window forward.
	RaiseMsg = win.WM_USER + 0x3510

	wmDispatch = win.WM_APP + 1
	wmTray     = win.WM_APP + 2

	// CopyDataCommand marks WM_COPYDATA messages carrying CLI commands.
	CopyDataCommand = 0xBA77
)

var (
	IsAdmin = false

	mainWindow *window

	// remoteControlEnabled mirrors the service setting; see extras.Settings.
	remoteControlEnabled atomic.Bool
)

// copyDataStruct is COPYDATASTRUCT.
type copyDataStruct struct {
	DwData uintptr
	CbData uint32
	LpData uintptr
}

type window struct {
	hwnd     win.HWND
	chromium *edge.Chromium
	bridge   *bridge
	tray     *tray
	ready    bool

	dispatchMu    sync.Mutex
	dispatchQueue []func()

	taskbarCreated uint32
	quitting       bool
	startMaximized bool
	inSizeMove     bool
	webviewOff     bool
}

// Minimum window size in DIPs; the page adapts down to this.
const (
	minWidth  = 640
	minHeight = 480
)

// WM_SIZE wParam values.
const (
	sizeRestored  = 0
	sizeMinimized = 1
	sizeMaximized = 2
)

// minRuntimeMajor is the oldest WebView2 runtime the page is written for
// (CSS color-mix and container queries).
const minRuntimeMajor = 111

// Available reports whether a recent enough WebView2 runtime is installed.
// Windows 7 and 8.1 are stuck on runtime 109 and keep the classic UI.
func Available() bool {
	v, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
	if err != nil || v == "" {
		return false
	}
	major, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(major)
	return err == nil && n >= minRuntimeMajor
}

func dataDir() string {
	base, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_CREATE)
	if err != nil {
		base = os.TempDir()
	}
	d := filepath.Join(base, "BetterAmnezia")
	os.MkdirAll(d, 0700)
	return d
}

// startupMarker detects a UI that crashed during WebView2 start-up, so the
// next launch can fall back to the classic UI instead of crash-looping.
func startupMarker() string { return filepath.Join(dataDir(), "webui-starting") }

// PreviousStartFailed reports whether the last start never completed. A
// failure is forgotten after a day, so a transient problem does not pin the
// classic UI forever.
func PreviousStartFailed() bool {
	fi, err := os.Stat(startupMarker())
	if err != nil {
		return false
	}
	if time.Since(fi.ModTime()) > 24*time.Hour {
		os.Remove(startupMarker())
		return false
	}
	return true
}

// ClearStartFailure forgets a failed start, e.g. after an update.
func ClearStartFailure() {
	os.Remove(startupMarker())
}

func (w *window) dispatch(f func()) {
	w.dispatchMu.Lock()
	w.dispatchQueue = append(w.dispatchQueue, f)
	w.dispatchMu.Unlock()
	win.PostMessage(w.hwnd, wmDispatch, 0, 0)
}

// dispatchSync runs f on the UI thread and waits for it.
func (w *window) dispatchSync(f func()) {
	done := make(chan struct{})
	w.dispatch(func() {
		defer close(done)
		f()
	})
	<-done
}

func (w *window) runDispatched() {
	w.dispatchMu.Lock()
	q := w.dispatchQueue
	w.dispatchQueue = nil
	w.dispatchMu.Unlock()
	for _, f := range q {
		f()
	}
}

// eval runs JavaScript in the page from any goroutine.
func (w *window) eval(js string) {
	w.dispatch(func() {
		if w.chromium != nil && w.ready {
			w.chromium.Eval(js)
		}
	})
}

var (
	moddwmapi                 = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmSetWindowAttribute = moddwmapi.NewProc("DwmSetWindowAttribute")
)

// setDarkTitleBar switches the caption to the dark variant on Windows 10
// 20H1 and later; older versions ignore the attribute.
func (w *window) setDarkTitleBar(dark bool) {
	if procDwmSetWindowAttribute.Find() != nil {
		return
	}
	v := uint32(0)
	if dark {
		v = 1
	}
	const dwmwaUseImmersiveDarkMode = 20
	procDwmSetWindowAttribute.Call(uintptr(w.hwnd), dwmwaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&v)), 4)
}

// setCaptionColor colours the title bar on Windows 11 (AMOLED black).
func (w *window) setCaptionColor(rgb uint32, set bool) {
	if procDwmSetWindowAttribute.Find() != nil {
		return
	}
	const dwmwaCaptionColor = 35
	v := rgb
	if !set {
		v = 0xFFFFFFFF // DWMWA_COLOR_DEFAULT
	}
	procDwmSetWindowAttribute.Call(uintptr(w.hwnd), dwmwaCaptionColor, uintptr(unsafe.Pointer(&v)), 4)
}

func (w *window) show() {
	if w.startMaximized {
		w.startMaximized = false
		win.ShowWindow(w.hwnd, win.SW_SHOWMAXIMIZED)
	} else if win.IsIconic(w.hwnd) {
		win.ShowWindow(w.hwnd, win.SW_RESTORE)
	} else {
		win.ShowWindow(w.hwnd, win.SW_SHOW)
	}
	win.SetWindowPos(w.hwnd, win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_SHOWWINDOW)
	win.SetWindowPos(w.hwnd, win.HWND_NOTOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_SHOWWINDOW)
	win.SetForegroundWindow(w.hwnd)
	w.webviewVisible(true)
	w.focusWebView()
}

func (w *window) hide() {
	w.saveWindowState()
	win.ShowWindow(w.hwnd, win.SW_HIDE)
	w.webviewVisible(false)
}

// The go-webview2 wrappers call os.Exit on any failed COM call, and some
// calls legitimately fail while the window is minimised or hidden (focus,
// bounds). These helpers talk to the controller directly and only log.

// webviewVisible shows or hides the WebView and, when showing, refits it to
// the client area, so a window restored from the tray or the taskbar never
// comes back black.
func (w *window) webviewVisible(visible bool) {
	if w.chromium == nil || !w.ready {
		return
	}
	c := w.chromium.GetController()
	if c == nil {
		return
	}
	if err := c.PutIsVisible(visible); err != nil {
		log.Printf("WebView2 visibility: %v", err)
	}
	w.webviewOff = !visible
	if visible {
		w.resizeWebView()
		c.NotifyParentWindowPositionChanged()
	}
}

// resizeWebView fits the WebView to the client area unless the window is
// minimised (empty client rectangle).
func (w *window) resizeWebView() {
	if w.chromium == nil || !w.ready || win.IsIconic(w.hwnd) {
		return
	}
	var r win.RECT
	if !win.GetClientRect(w.hwnd, &r) || r.Right <= r.Left || r.Bottom <= r.Top {
		return
	}
	w.chromium.Resize()
}

func (w *window) focusWebView() {
	if w.chromium == nil || !w.ready || win.IsIconic(w.hwnd) || !win.IsWindowVisible(w.hwnd) {
		return
	}
	if c := w.chromium.GetController(); c != nil {
		if err := c.MoveFocus(edge.COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC); err != nil {
			log.Printf("WebView2 focus: %v", err)
		}
	}
}

func (w *window) quit() {
	if w.quitting {
		return
	}
	w.quitting = true
	w.saveWindowState()
	if w.tray != nil {
		w.tray.dispose()
	}
	if w.chromium != nil {
		w.chromium.ShuttingDown()
	}
	win.DestroyWindow(w.hwnd)
}

func scale(hwnd win.HWND, v int32) int32 {
	dpi := win.GetDpiForWindow(hwnd)
	if dpi == 0 {
		dpi = 96
	}
	return v * int32(dpi) / 96
}

func (w *window) wndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmDispatch:
		w.runDispatched()
		return 0
	case wmTray:
		if w.tray != nil {
			w.tray.handle(lParam)
		}
		return 0
	case RaiseMsg:
		w.show()
		w.bridge.emit("raised", nil)
		return 0
	case win.WM_SIZE:
		switch wParam {
		case sizeMinimized:
			w.webviewVisible(false)
		case sizeRestored, sizeMaximized:
			if w.webviewOff && win.IsWindowVisible(hwnd) {
				w.webviewVisible(true)
			} else {
				w.resizeWebView()
			}
		}
		// Maximising and restoring do not go through a move/size loop.
		if !w.inSizeMove && (wParam == sizeMaximized || wParam == sizeRestored) {
			w.saveWindowState()
		}
		return 0
	case win.WM_ENTERSIZEMOVE:
		w.inSizeMove = true
	case win.WM_EXITSIZEMOVE:
		w.inSizeMove = false
		w.saveWindowState()
	case win.WM_MOVE, win.WM_MOVING:
		if w.chromium != nil && w.ready {
			w.chromium.NotifyParentWindowPositionChanged()
		}
	case win.WM_ACTIVATE:
		// HIWORD is non-zero while the window is still minimised.
		if win.LOWORD(uint32(wParam)) != win.WA_INACTIVE && win.HIWORD(uint32(wParam)) == 0 {
			w.focusWebView()
		}
	case win.WM_GETMINMAXINFO:
		mmi := (*win.MINMAXINFO)(unsafe.Pointer(lParam))
		mmi.PtMinTrackSize.X = scale(hwnd, minWidth)
		mmi.PtMinTrackSize.Y = scale(hwnd, minHeight)
		return 0
	case win.WM_DPICHANGED:
		r := (*win.RECT)(unsafe.Pointer(lParam))
		win.SetWindowPos(hwnd, 0, r.Left, r.Top, r.Right-r.Left, r.Bottom-r.Top, win.SWP_NOZORDER|win.SWP_NOACTIVATE)
		return 0
	case win.WM_SETTINGCHANGE:
		if lParam != 0 && windows.UTF16PtrToString((*uint16)(unsafe.Pointer(lParam))) == "ImmersiveColorSet" {
			w.applyTheme(loadPrefs().effectiveTheme())
			w.bridge.emit("systemTheme", nil)
		}
	case win.WM_COPYDATA:
		cds := (*copyDataStruct)(unsafe.Pointer(lParam))
		if cds.DwData != CopyDataCommand || cds.CbData == 0 || cds.CbData > 4096 {
			return 0
		}
		raw := unsafe.Slice((*uint16)(unsafe.Pointer(cds.LpData)), cds.CbData/2)
		args := strings.Split(strings.TrimRight(windows.UTF16ToString(raw), "\x00"), "\x00")
		return w.handleCommand(args)
	case win.WM_CLOSE:
		// The UI process stays alive either way; the manager would just
		// relaunch it. Without the tray, minimise to the taskbar instead.
		if w.tray != nil && loadPrefs().CloseToTray {
			w.hide()
		} else {
			win.ShowWindow(w.hwnd, win.SW_MINIMIZE)
		}
		return 0
	case win.WM_QUERYENDSESSION:
		return 1
	case win.WM_ENDSESSION:
		if wParam != 0 {
			w.quit()
		}
		return 0
	case win.WM_DESTROY:
		win.PostQuitMessage(0)
		return 0
	default:
		if w.taskbarCreated != 0 && msg == w.taskbarCreated && w.tray != nil {
			// Explorer restarted; the tray icon has to be added again.
			w.tray.add()
			return 0
		}
	}
	return win.DefWindowProc(hwnd, msg, wParam, lParam)
}

// handleCommand executes a remote control command forwarded by a second
// instance (betteramnezia.exe /connect NAME and friends). It returns 1 on
// success, 2 when remote control is disabled and 0 on failure.
func (w *window) handleCommand(args []string) uintptr {
	if len(args) == 0 {
		return 0
	}
	if args[0] == "show" {
		w.show()
		return 1
	}
	if !remoteControlEnabled.Load() {
		return 2
	}
	go func() {
		if err := runRemoteCommand(args); err != nil {
			log.Printf("Remote control %v failed: %v", args, err)
			w.notify("BetterAmnezia", err.Error())
		}
	}()
	return 1
}

func runRemoteCommand(args []string) error {
	switch args[0] {
	case "connect", "disconnect", "toggle":
		if len(args) != 2 {
			return errors.New("missing tunnel name")
		}
		t := manager.Tunnel{Name: args[1]}
		switch args[0] {
		case "connect":
			return t.Start()
		case "disconnect":
			return t.Stop()
		default:
			_, err := t.Toggle()
			return err
		}
	case "disconnectall":
		tunnels, err := manager.IPCClientTunnels()
		if err != nil {
			return err
		}
		for _, t := range tunnels {
			if s, err := t.State(); err == nil && (s == manager.TunnelStarted || s == manager.TunnelStarting) {
				t.Stop()
			}
		}
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}

var wndProcCallback = windows.NewCallback(func(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	if mainWindow == nil || mainWindow.hwnd == 0 {
		return win.DefWindowProc(hwnd, msg, wParam, lParam)
	}
	return mainWindow.wndProc(hwnd, msg, wParam, lParam)
})

func (w *window) create() error {
	hInstance := win.GetModuleHandle(nil)
	className, _ := windows.UTF16PtrFromString(WindowClass)
	icon := win.HICON(win.LoadImage(hInstance, win.MAKEINTRESOURCE(7), win.IMAGE_ICON, 0, 0, win.LR_DEFAULTSIZE|win.LR_SHARED))
	bg := win.HBRUSH(win.GetStockObject(win.BLACK_BRUSH))
	if theme := loadPrefs().effectiveTheme(); theme == "light" {
		bg = win.HBRUSH(win.GetStockObject(win.WHITE_BRUSH))
	}
	wc := win.WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
		LpfnWndProc:   wndProcCallback,
		HInstance:     hInstance,
		HIcon:         icon,
		HIconSm:       icon,
		HCursor:       win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_ARROW)),
		HbrBackground: bg,
		LpszClassName: className,
	}
	if win.RegisterClassEx(&wc) == 0 {
		return fmt.Errorf("unable to register window class: %v", windows.GetLastError())
	}
	title, _ := windows.UTF16PtrFromString("BetterAmnezia")
	w.hwnd = win.CreateWindowEx(0, className, title, win.WS_OVERLAPPEDWINDOW,
		win.CW_USEDEFAULT, win.CW_USEDEFAULT, 1080, 720, 0, 0, hInstance, nil)
	if w.hwnd == 0 {
		return fmt.Errorf("unable to create window: %v", windows.GetLastError())
	}
	w.startMaximized = w.initialPlacement()
	// The UI is elevated; let the non-elevated launcher talk to it.
	win.ChangeWindowMessageFilterEx(w.hwnd, RaiseMsg, win.MSGFLT_ALLOW, nil)
	win.ChangeWindowMessageFilterEx(w.hwnd, win.WM_COPYDATA, win.MSGFLT_ALLOW, nil)
	w.taskbarCreated = win.RegisterWindowMessage(windows.StringToUTF16Ptr("TaskbarCreated"))
	win.ChangeWindowMessageFilterEx(w.hwnd, w.taskbarCreated, win.MSGFLT_ALLOW, nil)
	w.applyTheme(loadPrefs().effectiveTheme())
	return nil
}

func (w *window) applyTheme(theme string) {
	w.setDarkTitleBar(theme != "light")
	if theme == "amoled" {
		w.setCaptionColor(0x000000, true)
	} else {
		w.setCaptionColor(0, false)
	}
	if w.chromium != nil && w.ready {
		switch theme {
		case "light":
			w.chromium.SetBackgroundColour(0xf5, 0xf6, 0xfa, 0xff)
		case "amoled":
			w.chromium.SetBackgroundColour(0, 0, 0, 0xff)
		default:
			w.chromium.SetBackgroundColour(0x0f, 0x11, 0x16, 0xff)
		}
	}
}

func (w *window) embed() error {
	c := edge.NewChromium()
	c.DataPath = filepath.Join(dataDir(), "WebView2")
	c.Debug = os.Getenv("BETTERAMNEZIA_DEVTOOLS") == "1"
	c.SetErrorCallback(func(err error) {
		log.Printf("WebView2 error: %v", err)
	})
	c.ProcessFailedCallback = func(_ *edge.ICoreWebView2, _ *edge.ICoreWebView2ProcessFailedEventArgs) {
		// Exit; the manager starts a fresh UI process.
		log.Println("WebView2 process failed, restarting the interface")
		w.dispatch(w.quit)
	}
	c.MessageCallback = func(message string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		w.bridge.handleMessage(message)
	}
	c.AcceleratorKeyCallback = func(key uint) bool {
		ctrl := win.GetKeyState(win.VK_CONTROL) < 0
		switch {
		case key == win.VK_F5, ctrl && (key == 'R' || key == 'P' || key == 'J' || key == 'U'):
			return true // swallow reload, print, downloads and view source
		case key == win.VK_F12 || (ctrl && win.GetKeyState(win.VK_SHIFT) < 0 && key == 'I'):
			return !c.Debug
		}
		return false
	}
	w.chromium = c
	if !c.Embed(uintptr(w.hwnd)) {
		return errors.New("unable to embed WebView2")
	}
	w.ready = true
	if settings, err := c.GetSettings(); err == nil {
		settings.PutAreDefaultContextMenusEnabled(c.Debug)
		settings.PutAreDevToolsEnabled(c.Debug)
		settings.PutIsStatusBarEnabled(false)
		settings.PutIsZoomControlEnabled(false)
		settings.PutAreDefaultScriptDialogsEnabled(true)
		settings.PutIsBuiltInErrorPageEnabled(false)
		settings.PutAreBrowserAcceleratorKeysEnabled(c.Debug)
		settings.PutIsPinchZoomEnabled(false)
		settings.PutIsSwipeNavigationEnabled(false)
	}
	c.PutIsGeneralAutofillEnabled(false)
	c.PutIsPasswordAutosaveEnabled(false)
	// Files dropped on the window are imported by the page, which cancels
	// the browser's default navigation.
	c.AllowExternalDrag(true)
	w.applyTheme(loadPrefs().effectiveTheme())
	c.Resize()
	c.Init(bootstrapScript())
	c.NavigateToString(pageHTML())
	return nil
}

// Run shows the UI and returns when it exits. On error everything it
// created is torn down again, so the classic UI can take over in the same
// process.
func Run() (err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil && !errors.Is(err, windows.Errno(1)) {
		log.Printf("CoInitializeEx: %v", err)
	}

	w := &window{}
	w.bridge = newBridge(w)
	mainWindow = w
	defer func() {
		if err == nil {
			return
		}
		if w.tray != nil {
			w.tray.dispose()
		}
		if w.hwnd != 0 {
			hwnd := w.hwnd
			w.hwnd = 0
			win.DestroyWindow(hwnd)
		}
		className, _ := windows.UTF16PtrFromString(WindowClass)
		win.UnregisterClass(className)
		mainWindow = nil
	}()
	if err := w.create(); err != nil {
		return err
	}

	t, err := newTray(w)
	if err != nil {
		log.Printf("Tray icon unavailable: %v", err)
	} else {
		w.tray = t
	}
	if w.tray == nil || !loadPrefs().StartMinimized {
		w.show()
	}

	marker := startupMarker()
	os.WriteFile(marker, []byte("1"), 0600)
	if err := w.embed(); err != nil {
		return err
	}
	os.Remove(marker)

	w.bridge.subscribe()

	var msg win.MSG
	for win.GetMessage(&msg, 0, 0, 0) > 0 {
		win.TranslateMessage(&msg)
		win.DispatchMessage(&msg)
	}
	w.bridge.unsubscribe()
	if w.bridge.quitManager {
		if _, err := manager.IPCClientQuit(w.bridge.stopTunnelsOnQuit); err != nil {
			return fmt.Errorf("unable to exit service: %w", err)
		}
	}
	return nil
}
