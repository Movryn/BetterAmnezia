/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package webui

import (
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"

	"github.com/amnezia-vpn/amneziawg-windows-client/version"
)

const iconApp = 7

// appIcon loads the application icon at the given pixel size.
func appIcon(size int32) win.HICON {
	return win.HICON(win.LoadImage(win.GetModuleHandle(nil), win.MAKEINTRESOURCE(iconApp), win.IMAGE_ICON, size, size, win.LR_SHARED))
}

// setWindowIcons gives the window crisp big and small icons; Task Manager
// and Alt+Tab use these.
func (w *window) setWindowIcons() {
	if big := appIcon(win.GetSystemMetrics(win.SM_CXICON)); big != 0 {
		win.SendMessage(w.hwnd, win.WM_SETICON, 1 /* ICON_BIG */, uintptr(big))
	}
	if small := appIcon(win.GetSystemMetrics(win.SM_CXSMICON)); small != 0 {
		win.SendMessage(w.hwnd, win.WM_SETICON, 0 /* ICON_SMALL */, uintptr(small))
	}
}

var procSHChangeNotify = windows.NewLazySystemDLL("shell32.dll").NewProc("SHChangeNotify")

// refreshShellIconsOnce makes Explorer drop its cached copy of our icon
// after the app was updated. Windows caches executable icons by path, so
// without this the taskbar, Task Manager and notifications can keep showing
// the previous version's icon.
func refreshShellIconsOnce() {
	p := loadPrefs()
	if p.IconVersion == version.Number {
		return
	}
	go func() {
		const shcneAssocChanged = 0x08000000
		procSHChangeNotify.Call(shcneAssocChanged, 0, 0, 0)
		if system32, err := windows.GetSystemDirectory(); err == nil {
			cmd := exec.Command(filepath.Join(system32, "ie4uinit.exe"), "-show")
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			cmd.Run()
		}
		p := loadPrefs()
		p.IconVersion = version.Number
		savePrefs(p)
	}()
}
