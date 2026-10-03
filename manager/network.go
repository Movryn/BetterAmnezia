/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package manager

import (
	"errors"
	"log"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/amnezia-vpn/amneziawg-windows/v3/conf"
	"github.com/amnezia-vpn/amneziawg-windows/v3/tunnel/winipcfg"

	"github.com/amnezia-vpn/amneziawg-windows-client/extras"
)

var (
	networkMu       sync.Mutex
	lastNetwork     extras.NetworkState
	lastNetworkSeen bool
	networkKick     = make(chan bool, 1)
)

func currentNetwork() extras.NetworkState {
	networkMu.Lock()
	defer networkMu.Unlock()
	return lastNetwork
}

// forceNetworkEvaluation makes the watcher apply auto-tunnel rules even if
// the network did not change.
func forceNetworkEvaluation() {
	select {
	case networkKick <- true:
	default:
	}
}

func isOurTunnel(alias string, names map[string]bool) bool {
	return names[alias]
}

// detectNetwork finds the physical interface with the best default route
// and classifies it.
func detectNetwork() extras.NetworkState {
	tunnelNames := make(map[string]bool)
	if names, err := conf.ListConfigNames(); err == nil {
		for _, n := range names {
			tunnelNames[n] = true
		}
	}
	type candidate struct {
		luid   winipcfg.LUID
		metric uint32
	}
	var best *candidate
	for _, family := range []winipcfg.AddressFamily{windows.AF_INET, windows.AF_INET6} {
		routes, err := winipcfg.GetIPForwardTable2(family)
		if err != nil {
			continue
		}
		for i := range routes {
			r := &routes[i]
			if r.DestinationPrefix.PrefixLength != 0 {
				continue
			}
			row, err := r.InterfaceLUID.Interface()
			if err != nil || row.OperStatus != winipcfg.IfOperStatusUp {
				continue
			}
			if row.Type == winipcfg.IfTypePropVirtual || row.Type == winipcfg.IfTypeTunnel || row.Type == winipcfg.IfTypeSoftwareLoopback {
				continue
			}
			if isOurTunnel(row.Alias(), tunnelNames) {
				continue
			}
			ipif, err := r.InterfaceLUID.IPInterface(family)
			if err != nil {
				continue
			}
			m := r.Metric + ipif.Metric
			if best == nil || m < best.metric {
				best = &candidate{luid: r.InterfaceLUID, metric: m}
			}
		}
		if best != nil {
			// Prefer the IPv4 view; IPv6-only networks fall through.
			break
		}
	}
	if best == nil {
		return extras.NetworkState{Kind: extras.NetworkNone}
	}
	row, err := best.luid.Interface()
	if err != nil {
		return extras.NetworkState{Kind: extras.NetworkNone}
	}
	state := extras.NetworkState{Name: row.Alias()}
	switch {
	case row.Type == winipcfg.IfTypeIEEE80211:
		state.Kind = extras.NetworkWiFi
		if guid, err := best.luid.GUID(); err == nil {
			ssid, err := currentSSID(guid)
			if err != nil {
				logSSIDErrorOnce(err)
			} else {
				state.SSID = ssid
			}
		}
	case row.Type == winipcfg.IfTypeEthernetCSMACD:
		state.Kind = extras.NetworkEthernet
	default:
		state.Kind = extras.NetworkOther
	}
	return state
}

var ssidErrorOnce sync.Once

func logSSIDErrorOnce(err error) {
	ssidErrorOnce.Do(func() {
		log.Printf("Unable to read the Wi-Fi network name (%v); on Windows 11 enable location access for desktop apps so auto-tunneling can tell networks apart", err)
	})
}

var (
	modwlanapi             = windows.NewLazySystemDLL("wlanapi.dll")
	procWlanOpenHandle     = modwlanapi.NewProc("WlanOpenHandle")
	procWlanCloseHandle    = modwlanapi.NewProc("WlanCloseHandle")
	procWlanQueryInterface = modwlanapi.NewProc("WlanQueryInterface")
	procWlanFreeMemory     = modwlanapi.NewProc("WlanFreeMemory")
)

const wlanIntfOpcodeCurrentConnection = 7

// currentSSID returns the SSID the given Wi-Fi interface is associated with.
func currentSSID(guid *windows.GUID) (string, error) {
	if err := procWlanOpenHandle.Find(); err != nil {
		return "", err
	}
	var negotiated uint32
	var handle windows.Handle
	r, _, _ := procWlanOpenHandle.Call(2, 0, uintptr(unsafe.Pointer(&negotiated)), uintptr(unsafe.Pointer(&handle)))
	if r != 0 {
		return "", windows.Errno(r)
	}
	defer procWlanCloseHandle.Call(uintptr(handle), 0)
	var size uint32
	var data *byte
	r, _, _ = procWlanQueryInterface.Call(uintptr(handle), uintptr(unsafe.Pointer(guid)), wlanIntfOpcodeCurrentConnection, 0, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&data)), 0)
	if r != 0 {
		return "", windows.Errno(r)
	}
	defer procWlanFreeMemory.Call(uintptr(unsafe.Pointer(data)))
	// WLAN_CONNECTION_ATTRIBUTES: isState(4) wlanConnectionMode(4)
	// strProfileName(WCHAR[256]) then WLAN_ASSOCIATION_ATTRIBUTES starting
	// with DOT11_SSID { ULONG uSSIDLength; UCHAR ucSSID[32]; }.
	const ssidOffset = 8 + 512
	if size < ssidOffset+4+32 {
		return "", errors.New("short WLAN connection attributes")
	}
	buf := unsafe.Slice(data, size)
	n := uint32(buf[ssidOffset]) | uint32(buf[ssidOffset+1])<<8 | uint32(buf[ssidOffset+2])<<16 | uint32(buf[ssidOffset+3])<<24
	if n > 32 {
		n = 32
	}
	ssid := string(buf[ssidOffset+4 : ssidOffset+4+n])
	if ssid == "" {
		// Fall back to the profile name, which normally equals the SSID.
		profile := make([]uint16, 256)
		for i := range profile {
			profile[i] = uint16(buf[8+2*i]) | uint16(buf[9+2*i])<<8
		}
		ssid = windows.UTF16ToString(profile)
	}
	return strings.TrimRight(ssid, "\x00"), nil
}

// networkWatcher follows network changes and applies auto-tunnel rules.
func networkWatcher() {
	changed := make(chan bool, 1)
	bump := func() {
		select {
		case changed <- true:
		default:
		}
	}
	if cb, err := winipcfg.RegisterRouteChangeCallback(func(t winipcfg.MibNotificationType, r *winipcfg.MibIPforwardRow2) {
		if r != nil && r.DestinationPrefix.PrefixLength == 0 {
			bump()
		}
	}); err != nil {
		log.Printf("Auto-tunnel: unable to watch routes: %v", err)
	} else {
		defer cb.Unregister()
	}
	if cb, err := winipcfg.RegisterInterfaceChangeCallback(func(t winipcfg.MibNotificationType, i *winipcfg.MibIPInterfaceRow) {
		bump()
	}); err != nil {
		log.Printf("Auto-tunnel: unable to watch interfaces: %v", err)
	} else {
		defer cb.Unregister()
	}
	poll := time.NewTicker(20 * time.Second)
	defer poll.Stop()
	evaluate := func(force bool) {
		debounce := time.Duration(currentSettings().AutoTunnel.DebounceSeconds) * time.Second
		if !force && debounce > 0 {
			time.Sleep(debounce)
			// Drain bursts that arrived while waiting.
			select {
			case <-changed:
			default:
			}
		}
		n := detectNetwork()
		networkMu.Lock()
		same := lastNetworkSeen && n.Key() == lastNetwork.Key()
		lastNetwork = n
		lastNetworkSeen = true
		networkMu.Unlock()
		if same && !force {
			return
		}
		if !same {
			log.Printf("Network changed: %s %q (%s)", n.Kind, n.SSID, n.Name)
			IPCServerNotifyExt("network", n)
		}
		applyAutoTunnel(n)
	}
	evaluate(true)
	for {
		select {
		case <-changed:
			evaluate(false)
		case <-networkKick:
			evaluate(true)
		case <-poll.C:
			evaluate(false)
		}
	}
}

func startedTunnels() []string {
	trackedTunnelsLock.Lock()
	defer trackedTunnelsLock.Unlock()
	var out []string
	for name, state := range trackedTunnels {
		if state == TunnelStarted || state == TunnelStarting {
			out = append(out, name)
		}
	}
	return out
}

func applyAutoTunnel(n extras.NetworkState) {
	at := currentSettings().AutoTunnel
	d := at.Decide(n)
	s := &ManagerService{}
	switch d.Action {
	case extras.ActionConnect:
		if d.Tunnel == "" {
			log.Printf("Auto-tunnel: %s, but no tunnel is selected", d.Reason)
			return
		}
		if state, err := s.State(d.Tunnel); err == nil && (state == TunnelStarted || state == TunnelStarting) {
			return
		}
		log.Printf("Auto-tunnel: connecting %s (%s)", d.Tunnel, d.Reason)
		if err := s.Start(d.Tunnel); err != nil {
			log.Printf("Auto-tunnel: unable to start %s: %v", d.Tunnel, err)
			return
		}
		IPCServerNotifyExt("autotunnel", map[string]string{"action": "connect", "tunnel": d.Tunnel, "reason": d.Reason})
	case extras.ActionDisconnect:
		running := startedTunnels()
		if len(running) == 0 {
			return
		}
		log.Printf("Auto-tunnel: disconnecting %v (%s)", running, d.Reason)
		for _, t := range running {
			s.Stop(t)
		}
		IPCServerNotifyExt("autotunnel", map[string]string{"action": "disconnect", "tunnel": strings.Join(running, ", "), "reason": d.Reason})
	}
}
