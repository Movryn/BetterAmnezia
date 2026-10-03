/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package webui

import (
	"fmt"
	"sort"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"

	"github.com/amnezia-vpn/amneziawg-windows/v3/conf"

	"github.com/amnezia-vpn/amneziawg-windows-client/manager"
)

const (
	trayID           = 1
	ninBalloonClick  = win.WM_USER + 5
	menuOpen         = 1
	menuImport       = 2
	menuQuit         = 3
	menuDisconnect   = 4
	menuTunnelOffset = 100

	iconDisconnected = 7
	iconConnected    = 9
)

type tray struct {
	w       *window
	nid     win.NOTIFYICONDATA
	icons   map[int]win.HICON
	added   bool
	tunnels []string

	balloonIcon win.HICON
}

func loadIcon(id int) win.HICON {
	size := win.GetSystemMetrics(win.SM_CXSMICON)
	return win.HICON(win.LoadImage(win.GetModuleHandle(nil), win.MAKEINTRESOURCE(uintptr(id)), win.IMAGE_ICON, size, size, win.LR_SHARED))
}

func newTray(w *window) (*tray, error) {
	t := &tray{w: w, icons: make(map[int]win.HICON)}
	t.icons[iconDisconnected] = loadIcon(iconDisconnected)
	t.icons[iconConnected] = loadIcon(iconConnected)
	if t.icons[iconConnected] == 0 {
		t.icons[iconConnected] = t.icons[iconDisconnected]
	}
	big := win.GetSystemMetrics(win.SM_CXICON)
	t.balloonIcon = win.HICON(win.LoadImage(win.GetModuleHandle(nil), win.MAKEINTRESOURCE(iconDisconnected), win.IMAGE_ICON, big, big, win.LR_SHARED))
	t.nid.CbSize = uint32(unsafe.Sizeof(t.nid))
	t.nid.HWnd = w.hwnd
	t.nid.UID = trayID
	t.nid.UFlags = win.NIF_MESSAGE | win.NIF_ICON | win.NIF_TIP
	t.nid.UCallbackMessage = wmTray
	t.nid.HIcon = t.icons[iconDisconnected]
	t.setTip("BetterAmnezia: Disconnected")
	if !t.add() {
		return nil, fmt.Errorf("Shell_NotifyIcon failed")
	}
	return t, nil
}

func (t *tray) setTip(s string) {
	u := windows.StringToUTF16(s)
	if len(u) > len(t.nid.SzTip) {
		u = append(u[:len(t.nid.SzTip)-1], 0)
	}
	copy(t.nid.SzTip[:], u)
}

func (t *tray) add() bool {
	nid := t.nid
	nid.UFlags = win.NIF_MESSAGE | win.NIF_ICON | win.NIF_TIP
	t.added = win.Shell_NotifyIcon(win.NIM_ADD, &nid)
	return t.added
}

func (t *tray) dispose() {
	if t.added {
		win.Shell_NotifyIcon(win.NIM_DELETE, &t.nid)
		t.added = false
	}
}

// setState updates icon and tooltip; it runs on the UI thread.
func (t *tray) setState(globalState manager.TunnelState, active []string) {
	icon := iconDisconnected
	tip := "BetterAmnezia: Disconnected"
	switch globalState {
	case manager.TunnelStarted:
		icon = iconConnected
		tip = "BetterAmnezia: Connected"
		if len(active) > 0 {
			tip = "BetterAmnezia: " + active[0]
			if len(active) > 1 {
				tip += fmt.Sprintf(" +%d", len(active)-1)
			}
		}
	case manager.TunnelStarting:
		tip = "BetterAmnezia: Connecting…"
	case manager.TunnelStopping:
		tip = "BetterAmnezia: Disconnecting…"
	}
	t.nid.HIcon = t.icons[icon]
	t.setTip(tip)
	nid := t.nid
	nid.UFlags = win.NIF_ICON | win.NIF_TIP
	win.Shell_NotifyIcon(win.NIM_MODIFY, &nid)
}

// balloon shows a notification.
func (t *tray) balloon(title, text string) {
	nid := t.nid
	nid.UFlags = win.NIF_INFO
	// Show the app icon rather than the generic info icon.
	if t.balloonIcon != 0 {
		nid.DwInfoFlags = win.NIIF_USER | win.NIIF_LARGE_ICON
		nid.HBalloonIcon = t.balloonIcon
	} else {
		nid.DwInfoFlags = win.NIIF_INFO
	}
	ti := windows.StringToUTF16(title)
	if len(ti) > len(nid.SzInfoTitle) {
		ti = append(ti[:len(nid.SzInfoTitle)-1], 0)
	}
	copy(nid.SzInfoTitle[:], ti)
	tx := windows.StringToUTF16(text)
	if len(tx) > len(nid.SzInfo) {
		tx = append(tx[:len(nid.SzInfo)-1], 0)
	}
	copy(nid.SzInfo[:], tx)
	win.Shell_NotifyIcon(win.NIM_MODIFY, &nid)
}

func (t *tray) handle(lParam uintptr) {
	switch uint32(lParam) {
	case win.WM_LBUTTONUP, ninBalloonClick:
		t.w.show()
	case win.WM_RBUTTONUP, win.WM_CONTEXTMENU:
		t.showMenu()
	}
}

func appendMenu(menu win.HMENU, id uint32, text string, checked, disabled, bold bool) {
	var mii win.MENUITEMINFO
	mii.CbSize = uint32(unsafe.Sizeof(mii))
	mii.FMask = win.MIIM_ID | win.MIIM_STRING | win.MIIM_STATE | win.MIIM_FTYPE
	mii.FType = win.MFT_STRING
	mii.WID = id
	if checked {
		mii.FState |= win.MFS_CHECKED
	}
	if disabled {
		mii.FState |= win.MFS_DISABLED
	}
	if bold {
		mii.FState |= win.MFS_DEFAULT
	}
	text16, _ := windows.UTF16PtrFromString(text)
	mii.DwTypeData = text16
	win.InsertMenuItem(menu, ^uint32(0), true, &mii)
}

func appendSeparator(menu win.HMENU) {
	var mii win.MENUITEMINFO
	mii.CbSize = uint32(unsafe.Sizeof(mii))
	mii.FMask = win.MIIM_FTYPE
	mii.FType = win.MFT_SEPARATOR
	win.InsertMenuItem(menu, ^uint32(0), true, &mii)
}

func (t *tray) showMenu() {
	menu := win.CreatePopupMenu()
	defer win.DestroyMenu(menu)

	tunnels, _ := manager.IPCClientTunnels()
	sort.Slice(tunnels, func(i, j int) bool { return conf.TunnelNameIsLess(tunnels[i].Name, tunnels[j].Name) })
	t.tunnels = t.tunnels[:0]
	anyActive := false
	appendMenu(menu, menuOpen, tr("Open BetterAmnezia"), false, false, true)
	appendSeparator(menu)
	if len(tunnels) == 0 {
		appendMenu(menu, 0, tr("No tunnels"), false, true, false)
	}
	for i, tun := range tunnels {
		if i >= 40 {
			break
		}
		state, _ := tun.State()
		label := tun.Name
		switch state {
		case manager.TunnelStarting:
			label += "  (" + tr("connecting") + ")"
		case manager.TunnelStopping:
			label += "  (" + tr("disconnecting") + ")"
		}
		active := state == manager.TunnelStarted || state == manager.TunnelStarting
		anyActive = anyActive || active
		appendMenu(menu, uint32(menuTunnelOffset+len(t.tunnels)), label, active, false, false)
		t.tunnels = append(t.tunnels, tun.Name)
	}
	appendSeparator(menu)
	if anyActive {
		appendMenu(menu, menuDisconnect, tr("Disconnect all"), false, false, false)
	}
	appendMenu(menu, menuImport, tr("Import tunnel…"), false, !IsAdmin, false)
	appendSeparator(menu)
	appendMenu(menu, menuQuit, tr("Exit"), false, !IsAdmin, false)

	var pt win.POINT
	win.GetCursorPos(&pt)
	win.SetForegroundWindow(t.w.hwnd)
	cmd := win.TrackPopupMenu(menu, win.TPM_RETURNCMD|win.TPM_RIGHTBUTTON|win.TPM_NONOTIFY, pt.X, pt.Y, 0, t.w.hwnd, nil)
	win.PostMessage(t.w.hwnd, win.WM_NULL, 0, 0)
	switch {
	case cmd == menuOpen:
		t.w.show()
	case cmd == menuImport:
		t.w.show()
		t.w.bridge.emit("navigate", map[string]string{"action": "import"})
	case cmd == menuQuit:
		t.w.bridge.quitManager = true
		t.w.bridge.stopTunnelsOnQuit = true
		t.w.quit()
	case cmd == menuDisconnect:
		go runRemoteCommand([]string{"disconnectall"})
	case cmd >= menuTunnelOffset && int(cmd-menuTunnelOffset) < len(t.tunnels):
		name := t.tunnels[cmd-menuTunnelOffset]
		go func() {
			tun := manager.Tunnel{Name: name}
			if _, err := tun.Toggle(); err != nil {
				t.w.notify(name, err.Error())
			}
		}()
	}
}
