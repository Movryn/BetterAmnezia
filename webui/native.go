/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package webui

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"rsc.io/qr"

	"github.com/amnezia-vpn/amneziawg-windows-client/importer"
)

// utf16z encodes s (which may contain NULs, as dialog filters do) followed
// by two terminating NULs.
func utf16z(s string) []uint16 {
	return append(utf16.Encode([]rune(s)), 0, 0)
}

func openFileDialog(owner win.HWND, title, filter string, multi bool) []string {
	buf := make([]uint16, 64*1024)
	filter16 := utf16z(filter)
	title16 := utf16z(title)
	ofn := win.OPENFILENAME{
		LStructSize: uint32(unsafe.Sizeof(win.OPENFILENAME{})),
		HwndOwner:   owner,
		LpstrFilter: &filter16[0],
		LpstrFile:   &buf[0],
		NMaxFile:    uint32(len(buf)),
		LpstrTitle:  &title16[0],
		Flags:       win.OFN_EXPLORER | win.OFN_FILEMUSTEXIST | win.OFN_PATHMUSTEXIST | win.OFN_HIDEREADONLY | win.OFN_NOCHANGEDIR,
	}
	if multi {
		ofn.Flags |= win.OFN_ALLOWMULTISELECT
	}
	if !win.GetOpenFileName(&ofn) {
		return nil
	}
	// Multi-select returns "dir\0file1\0file2\0\0"; single select a path.
	var parts []string
	start := 0
	for i, c := range buf {
		if c == 0 {
			if i == start {
				break
			}
			parts = append(parts, string(utf16.Decode(buf[start:i])))
			start = i + 1
		}
	}
	if len(parts) <= 1 {
		return parts
	}
	dir := parts[0]
	out := make([]string, 0, len(parts)-1)
	for _, f := range parts[1:] {
		out = append(out, filepath.Join(dir, f))
	}
	return out
}

func saveFileDialog(owner win.HWND, title, filter, defaultName, defExt string) string {
	buf := make([]uint16, 4096)
	copy(buf, utf16.Encode([]rune(defaultName)))
	filter16 := utf16z(filter)
	title16 := utf16z(title)
	ext16 := utf16z(defExt)
	ofn := win.OPENFILENAME{
		LStructSize: uint32(unsafe.Sizeof(win.OPENFILENAME{})),
		HwndOwner:   owner,
		LpstrFilter: &filter16[0],
		LpstrFile:   &buf[0],
		NMaxFile:    uint32(len(buf)),
		LpstrTitle:  &title16[0],
		LpstrDefExt: &ext16[0],
		Flags:       win.OFN_EXPLORER | win.OFN_OVERWRITEPROMPT | win.OFN_PATHMUSTEXIST | win.OFN_HIDEREADONLY | win.OFN_NOCHANGEDIR,
	}
	if !win.GetSaveFileName(&ofn) {
		return ""
	}
	return windows.UTF16ToString(buf)
}

const (
	bifReturnOnlyFSDirs  = 0x0001
	bifNewDialogStyle    = 0x0040
	bifNoNewFolderButton = 0x0200
)

func pickFolderDialog(owner win.HWND, title string) string {
	title16 := utf16z(title)
	name := make([]uint16, win.MAX_PATH)
	bi := win.BROWSEINFO{
		HwndOwner:      owner,
		PszDisplayName: &name[0],
		LpszTitle:      &title16[0],
		UlFlags:        bifReturnOnlyFSDirs | bifNewDialogStyle | bifNoNewFolderButton,
	}
	pidl := win.SHBrowseForFolder(&bi)
	if pidl == 0 {
		return ""
	}
	defer win.CoTaskMemFree(pidl)
	path := make([]uint16, 32*1024)
	if !win.SHGetPathFromIDList(pidl, &path[0]) {
		return ""
	}
	return windows.UTF16ToString(path)
}

type appInfo struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Icon string `json:"icon,omitempty"`
}

var (
	iconCacheMu sync.Mutex
	iconCache   = make(map[string]string)
)

// describeApp returns a display name and icon for an executable or folder.
func (w *window) describeApp(path string) appInfo {
	info := appInfo{Path: path, Name: fileDescription(path)}
	if info.Name == "" {
		info.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	iconCacheMu.Lock()
	icon, ok := iconCache[strings.ToLower(path)]
	iconCacheMu.Unlock()
	if !ok {
		w.dispatchSync(func() { icon = iconDataURL(path) })
		iconCacheMu.Lock()
		iconCache[strings.ToLower(path)] = icon
		iconCacheMu.Unlock()
	}
	info.Icon = icon
	return info
}

func (b *bridge) pickApps(json.RawMessage) (any, error) {
	var paths []string
	b.w.dispatchSync(func() {
		paths = openFileDialog(b.w.hwnd, tr("Choose applications"), tr("Programs")+" (*.exe)\x00*.exe\x00", true)
	})
	out := make([]appInfo, 0, len(paths))
	for _, p := range paths {
		out = append(out, b.w.describeApp(p))
	}
	return out, nil
}

func (b *bridge) pickFolder(json.RawMessage) (any, error) {
	var path string
	b.w.dispatchSync(func() {
		path = pickFolderDialog(b.w.hwnd, tr("Choose a folder; every program inside it will follow the rule."))
	})
	if path == "" {
		return nil, nil
	}
	return b.w.describeApp(path), nil
}

func (b *bridge) pickDriver(json.RawMessage) (any, error) {
	var paths []string
	b.w.dispatchSync(func() {
		paths = openFileDialog(b.w.hwnd, tr("Choose the split tunnel driver"), "mullvad-split-tunnel.sys\x00mullvad-split-tunnel.sys;*.sys\x00", false)
	})
	if len(paths) == 0 {
		return nil, nil
	}
	return map[string]string{"path": paths[0]}, nil
}

func (b *bridge) appsDescribe(params json.RawMessage) (any, error) {
	p, err := decode[struct {
		Paths []string `json:"paths"`
	}](params)
	if err != nil {
		return nil, err
	}
	out := make([]appInfo, 0, len(p.Paths))
	for _, path := range p.Paths {
		out = append(out, b.w.describeApp(path))
	}
	return out, nil
}

// appsRunning lists programs that currently run, without system processes.
func (b *bridge) appsRunning(json.RawMessage) (any, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snap, &entry); err != nil {
		return nil, err
	}
	windir := strings.ToLower(os.Getenv("SystemRoot"))
	self, _ := os.Executable()
	seen := make(map[string]bool)
	var paths []string
	for {
		if h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID); err == nil {
			buf := make([]uint16, windows.MAX_LONG_PATH)
			size := uint32(len(buf))
			if windows.QueryFullProcessImageName(h, 0, &buf[0], &size) == nil {
				p := windows.UTF16ToString(buf[:size])
				lp := strings.ToLower(p)
				if !seen[lp] && !(windir != "" && strings.HasPrefix(lp, windir+`\`)) && !strings.EqualFold(p, self) {
					seen[lp] = true
					paths = append(paths, p)
				}
			}
			windows.CloseHandle(h)
		}
		if windows.Process32Next(snap, &entry) != nil {
			break
		}
	}
	out := make([]appInfo, 0, len(paths))
	for _, p := range paths {
		out = append(out, b.w.describeApp(p))
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

var (
	modversion                  = windows.NewLazySystemDLL("version.dll")
	procGetFileVersionInfoSizeW = modversion.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfoW     = modversion.NewProc("GetFileVersionInfoW")
	procVerQueryValueW          = modversion.NewProc("VerQueryValueW")
)

// fileDescription reads FileDescription from an executable's version
// resource.
func fileDescription(path string) string {
	p16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	size, _, _ := procGetFileVersionInfoSizeW.Call(uintptr(unsafe.Pointer(p16)), 0)
	if size == 0 || size > 4*1024*1024 {
		return ""
	}
	data := make([]byte, size)
	if r, _, _ := procGetFileVersionInfoW.Call(uintptr(unsafe.Pointer(p16)), 0, size, uintptr(unsafe.Pointer(&data[0]))); r == 0 {
		return ""
	}
	query := func(sub string) (*uint16, uint32) {
		s16, _ := windows.UTF16PtrFromString(sub)
		var ptr *uint16
		var n uint32
		if r, _, _ := procVerQueryValueW.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(s16)), uintptr(unsafe.Pointer(&ptr)), uintptr(unsafe.Pointer(&n))); r == 0 {
			return nil, 0
		}
		return ptr, n
	}
	ptr, n := query(`\VarFileInfo\Translation`)
	langs := []string{"040904b0", "040904e4", "000004b0"}
	if ptr != nil && n >= 4 {
		tr := unsafe.Slice(ptr, 2)
		langs = append([]string{fmt.Sprintf("%04x%04x", tr[0], tr[1])}, langs...)
	}
	for _, l := range langs {
		ptr, n := query(`\StringFileInfo\` + l + `\FileDescription`)
		if ptr != nil && n > 1 {
			s := windows.UTF16ToString(unsafe.Slice(ptr, n))
			if s = strings.TrimSpace(s); s != "" {
				return s
			}
		}
	}
	return ""
}

// iconDataURL extracts the large shell icon of a file as a PNG data URL. It
// must run on the UI thread, which has COM initialised.
func iconDataURL(path string) string {
	p16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	var sfi win.SHFILEINFO
	if win.SHGetFileInfo(p16, 0, &sfi, uint32(unsafe.Sizeof(sfi)), win.SHGFI_ICON|win.SHGFI_LARGEICON) == 0 || sfi.HIcon == 0 {
		return ""
	}
	defer win.DestroyIcon(sfi.HIcon)
	img := iconToImage(sfi.HIcon)
	if img == nil {
		return ""
	}
	var buf bytes.Buffer
	if png.Encode(&buf, img) != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func iconToImage(icon win.HICON) image.Image {
	var ii win.ICONINFO
	if !win.GetIconInfo(icon, &ii) {
		return nil
	}
	defer win.DeleteObject(win.HGDIOBJ(ii.HbmColor))
	defer win.DeleteObject(win.HGDIOBJ(ii.HbmMask))
	if ii.HbmColor == 0 {
		return nil
	}
	var bm win.BITMAP
	if win.GetObject(win.HGDIOBJ(ii.HbmColor), unsafe.Sizeof(bm), unsafe.Pointer(&bm)) == 0 {
		return nil
	}
	w, h := int(bm.BmWidth), int(bm.BmHeight)
	if w <= 0 || h <= 0 || w > 256 || h > 256 {
		return nil
	}
	var bi win.BITMAPINFO
	bi.BmiHeader.BiSize = uint32(unsafe.Sizeof(bi.BmiHeader))
	bi.BmiHeader.BiWidth = int32(w)
	bi.BmiHeader.BiHeight = -int32(h)
	bi.BmiHeader.BiPlanes = 1
	bi.BmiHeader.BiBitCount = 32
	bi.BmiHeader.BiCompression = win.BI_RGB
	pixels := make([]byte, w*h*4)
	hdc := win.GetDC(0)
	defer win.ReleaseDC(0, hdc)
	if win.GetDIBits(hdc, ii.HbmColor, 0, uint32(h), &pixels[0], &bi, win.DIB_RGB_COLORS) == 0 {
		return nil
	}
	hasAlpha := false
	for i := 3; i < len(pixels); i += 4 {
		if pixels[i] != 0 {
			hasAlpha = true
			break
		}
	}
	var mask []byte
	if !hasAlpha {
		mask = make([]byte, w*h*4)
		win.GetDIBits(hdc, ii.HbmMask, 0, uint32(h), &mask[0], &bi, win.DIB_RGB_COLORS)
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			a := pixels[i+3]
			if !hasAlpha {
				a = 255
				if mask != nil && mask[i] != 0 {
					a = 0
				}
			}
			img.SetNRGBA(x, y, color.NRGBA{R: pixels[i+2], G: pixels[i+1], B: pixels[i], A: a})
		}
	}
	return img
}

func (b *bridge) clipboardRead(json.RawMessage) (any, error) {
	var text string
	var err error
	b.w.dispatchSync(func() { text, err = readClipboard(b.w.hwnd) })
	return map[string]string{"text": text}, err
}

func (b *bridge) clipboardWrite(params json.RawMessage) (any, error) {
	p, err := decode[struct {
		Text string `json:"text"`
	}](params)
	if err != nil {
		return nil, err
	}
	b.w.dispatchSync(func() { err = writeClipboard(b.w.hwnd, p.Text) })
	return nil, err
}

func readClipboard(owner win.HWND) (string, error) {
	if !win.OpenClipboard(owner) {
		return "", errors.New("clipboard is busy")
	}
	defer win.CloseClipboard()
	h := win.GetClipboardData(win.CF_UNICODETEXT)
	if h == 0 {
		return "", nil
	}
	p := win.GlobalLock(win.HGLOBAL(h))
	if p == nil {
		return "", nil
	}
	defer win.GlobalUnlock(win.HGLOBAL(h))
	return windows.UTF16PtrToString((*uint16)(p)), nil
}

func writeClipboard(owner win.HWND, text string) error {
	if !win.OpenClipboard(owner) {
		return errors.New("clipboard is busy")
	}
	defer win.CloseClipboard()
	win.EmptyClipboard()
	u := windows.StringToUTF16(text)
	h := win.GlobalAlloc(win.GMEM_MOVEABLE, uintptr(len(u)*2))
	if h == 0 {
		return errors.New("out of memory")
	}
	p := win.GlobalLock(h)
	copy(unsafe.Slice((*uint16)(p), len(u)), u)
	win.GlobalUnlock(h)
	if win.SetClipboardData(win.CF_UNICODETEXT, win.HANDLE(h)) == 0 {
		win.GlobalFree(h)
		return errors.New("unable to set clipboard")
	}
	return nil
}

func (b *bridge) openURL(params json.RawMessage) (any, error) {
	p, err := decode[struct {
		URL string `json:"url"`
	}](params)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(p.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("only web links can be opened")
	}
	b.w.dispatch(func() {
		verb, _ := windows.UTF16PtrFromString("open")
		target, _ := windows.UTF16PtrFromString(u.String())
		win.ShellExecute(b.w.hwnd, verb, target, nil, nil, win.SW_SHOWNORMAL)
	})
	return nil, nil
}

// qrSVG renders text as a QR code in SVG.
func qrSVG(text string) (string, error) {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return "", err
	}
	const quiet = 4
	n := code.Size + 2*quiet
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="100%%" height="100%%" fill="#fff"/><path fill="#000" d="`, n, n)
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String(), nil
}

func (b *bridge) qrCode(params json.RawMessage) (any, error) {
	if err := requireAdmin(); err != nil {
		return nil, err
	}
	p, err := decode[struct {
		Name   string `json:"name"`
		Format string `json:"format"`
	}](params)
	if err != nil {
		return nil, err
	}
	configs, err := storedTexts([]string{p.Name})
	if err != nil {
		return nil, err
	}
	text := configs[0].Text
	if p.Format == "vpn" {
		if text, err = importer.EncodeVPNKey(p.Name, text, ""); err != nil {
			return nil, err
		}
	}
	svg, err := qrSVG(text)
	if err != nil {
		return nil, fmt.Errorf(tr("The configuration is too large for a QR code: %v"), err)
	}
	return map[string]string{"svg": svg}, nil
}
