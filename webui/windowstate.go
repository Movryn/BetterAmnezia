/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package webui

import (
	"math"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// WindowRect is the restored (not maximised) window position in screen
// coordinates.
type WindowRect struct {
	X, Y, W, H int32
}

// Window size presets: "remember" restores the last size; the percentages
// are the share of the monitor's work area the window covers; "max" opens
// maximised.
const (
	sizeRemember = "remember"
	sizeMax      = "max"
)

var sizePresets = map[string]float64{"25": 0.25, "50": 0.50, "75": 0.75}

func workArea(hwnd win.HWND) (win.RECT, bool) {
	var mi win.MONITORINFO
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if !win.GetMonitorInfo(win.MonitorFromWindow(hwnd, win.MONITOR_DEFAULTTONEAREST), &mi) {
		return win.RECT{}, false
	}
	return mi.RcWork, true
}

func clampSize(hwnd win.HWND, w, h int32, wa win.RECT) (int32, int32) {
	w = max(w, scale(hwnd, minWidth))
	h = max(h, scale(hwnd, minHeight))
	return min(w, wa.Right-wa.Left), min(h, wa.Bottom-wa.Top)
}

// centeredRect returns a rectangle of the given size centred in the work
// area.
func centeredRect(wa win.RECT, w, h int32) win.RECT {
	x := wa.Left + (wa.Right-wa.Left-w)/2
	y := wa.Top + (wa.Bottom-wa.Top-h)/2
	return win.RECT{Left: x, Top: y, Right: x + w, Bottom: y + h}
}

// presetRect computes the window rectangle for a size preset. A share of the
// work area's surface keeps the screen's aspect ratio.
func presetRect(hwnd win.HWND, preset string) (win.RECT, bool) {
	share, ok := sizePresets[preset]
	if !ok {
		return win.RECT{}, false
	}
	wa, ok := workArea(hwnd)
	if !ok {
		return win.RECT{}, false
	}
	f := math.Sqrt(share)
	w, h := clampSize(hwnd, int32(float64(wa.Right-wa.Left)*f), int32(float64(wa.Bottom-wa.Top)*f), wa)
	return centeredRect(wa, w, h), true
}

// rectOnScreen reports whether enough of r is visible on some monitor to be
// grabbed, so a window saved on a disconnected screen is not restored there.
func rectOnScreen(r win.RECT) bool {
	// The title bar strip must be on a monitor.
	strip := win.RECT{Left: r.Left + 40, Top: r.Top, Right: r.Right - 40, Bottom: r.Top + 30}
	m, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&strip)), win.MONITOR_DEFAULTTONULL)
	return m != 0
}

var procMonitorFromRect = windows.NewLazySystemDLL("user32.dll").NewProc("MonitorFromRect")

// initialPlacement positions the freshly created (hidden) window and
// reports whether it should open maximised.
func (w *window) initialPlacement() bool {
	p := loadPrefs()
	wa, ok := workArea(w.hwnd)
	if !ok {
		return false
	}
	switch p.WindowSize {
	case sizeMax:
		return true
	case "25", "50", "75":
		if r, ok := presetRect(w.hwnd, p.WindowSize); ok {
			win.SetWindowPos(w.hwnd, 0, r.Left, r.Top, r.Right-r.Left, r.Bottom-r.Top, win.SWP_NOZORDER|win.SWP_NOACTIVATE)
			return false
		}
	default:
		if s := p.Window; s != nil && s.W > 0 && s.H > 0 {
			r := win.RECT{Left: s.X, Top: s.Y, Right: s.X + s.W, Bottom: s.Y + s.H}
			if rectOnScreen(r) {
				// The saved rectangle came from GetWindowPlacement, so it is
				// in the same (workspace) coordinates SetWindowPlacement uses.
				var wp win.WINDOWPLACEMENT
				wp.Length = uint32(unsafe.Sizeof(wp))
				wp.ShowCmd = win.SW_HIDE
				wp.RcNormalPosition = r
				if win.SetWindowPlacement(w.hwnd, &wp) {
					return p.WindowMaximized
				}
			}
		}
	}
	width, height := clampSize(w.hwnd, scale(w.hwnd, 1080), scale(w.hwnd, 720), wa)
	r := centeredRect(wa, width, height)
	win.SetWindowPos(w.hwnd, 0, r.Left, r.Top, width, height, win.SWP_NOZORDER|win.SWP_NOACTIVATE)
	return false
}

// saveWindowState remembers the restored rectangle and maximised state.
func (w *window) saveWindowState() {
	if w.hwnd == 0 || !win.IsWindowVisible(w.hwnd) {
		return
	}
	var wp win.WINDOWPLACEMENT
	wp.Length = uint32(unsafe.Sizeof(wp))
	if !win.GetWindowPlacement(w.hwnd, &wp) {
		return
	}
	r := wp.RcNormalPosition
	if r.Right-r.Left <= 0 || r.Bottom-r.Top <= 0 {
		return
	}
	p := loadPrefs()
	rect := &WindowRect{X: r.Left, Y: r.Top, W: r.Right - r.Left, H: r.Bottom - r.Top}
	maximized := win.IsZoomed(w.hwnd)
	if p.Window != nil && *p.Window == *rect && p.WindowMaximized == maximized {
		return
	}
	p.Window = rect
	p.WindowMaximized = maximized
	savePrefs(p)
}

// applySizePreset resizes the window right away when the user picks a
// preset in the settings.
func (w *window) applySizePreset(preset string) {
	switch preset {
	case sizeMax:
		win.ShowWindow(w.hwnd, win.SW_MAXIMIZE)
	case "25", "50", "75":
		if win.IsZoomed(w.hwnd) {
			win.ShowWindow(w.hwnd, win.SW_RESTORE)
		}
		if r, ok := presetRect(w.hwnd, preset); ok {
			win.SetWindowPos(w.hwnd, 0, r.Left, r.Top, r.Right-r.Left, r.Bottom-r.Top, win.SWP_NOZORDER)
		}
	}
}
