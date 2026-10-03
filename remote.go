/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package main

import (
	"log"
	"strings"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"

	"github.com/amnezia-vpn/amneziawg-windows-client/webui"
)

var procSendMessageTimeoutW = windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")

// Exit codes of the remote control commands.
const (
	remoteOK       = 0
	remoteFailed   = 1
	remoteDisabled = 2
	remoteNoUI     = 3
)

// remoteCommand forwards a command to the running UI, which performs it with
// its manager connection. This lets automation tools connect and disconnect
// tunnels without elevation, provided remote control is enabled in Settings.
func remoteCommand(args ...string) int {
	class, _ := windows.UTF16PtrFromString(webui.WindowClass)
	hwnd := win.FindWindow(class, nil)
	if hwnd == 0 {
		log.Println("BetterAmnezia is not running")
		return remoteNoUI
	}
	payload := windows.StringToUTF16(strings.Join(args, "\x00"))
	cds := struct {
		DwData uintptr
		CbData uint32
		LpData uintptr
	}{
		DwData: webui.CopyDataCommand,
		CbData: uint32(len(payload) * 2),
		LpData: uintptr(unsafe.Pointer(&payload[0])),
	}
	var result uintptr
	const smtoAbortIfHung = 0x0002
	r, _, _ := procSendMessageTimeoutW.Call(uintptr(hwnd), win.WM_COPYDATA, 0, uintptr(unsafe.Pointer(&cds)), smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)))
	if r == 0 {
		log.Println("BetterAmnezia did not respond")
		return remoteFailed
	}
	switch result {
	case 1:
		return remoteOK
	case 2:
		log.Println("Remote control is disabled; enable it in Settings → Remote control")
		return remoteDisabled
	}
	log.Println("BetterAmnezia rejected the command")
	return remoteFailed
}
