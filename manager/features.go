/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package manager

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/windows"

	"github.com/amnezia-vpn/amneziawg-windows-client/extras"
	"github.com/amnezia-vpn/amneziawg-windows-client/tunnel/firewall"
)

var (
	settingsMu sync.RWMutex
	settings   = extras.DefaultSettings()
)

func currentSettings() *extras.Settings {
	settingsMu.RLock()
	defer settingsMu.RUnlock()
	c := *settings
	return &c
}

// startFeatures loads settings and starts the background features. It is
// called once the manager service runs.
func startFeatures() {
	s, err := extras.LoadSettings()
	if err != nil {
		log.Printf("Unable to load settings, using defaults: %v", err)
		s = extras.DefaultSettings()
	}
	settingsMu.Lock()
	settings = s
	settingsMu.Unlock()
	updateLockdown()
	go networkWatcher()
	go healthMonitor()
	go dynamicDNSMonitor()
}

// applySettings stores new settings and reacts to changes immediately.
func applySettings(s *extras.Settings) error {
	if err := extras.SaveSettings(s); err != nil {
		return err
	}
	settingsMu.Lock()
	old := settings
	settings = s
	settingsMu.Unlock()
	if old.Lockdown != s.Lockdown {
		DisableLockdownIfActive()
	}
	updateLockdown()
	if s.AutoTunnel.Enabled && !old.AutoTunnel.Enabled {
		// Act on the current network right away.
		forceNetworkEvaluation()
	}
	IPCServerNotifyExt("settings", s)
	return nil
}

// anyTunnelStarted reports whether a tunnel is up. Tunnels whose state is
// still being discovered count as up, so lockdown does not cut a running
// tunnel off while the manager starts.
func anyTunnelStarted() bool {
	trackedTunnelsLock.Lock()
	defer trackedTunnelsLock.Unlock()
	for _, state := range trackedTunnels {
		if state == TunnelStarted || state == TunnelUnknown {
			return true
		}
	}
	return false
}

var lockdownMu sync.Mutex

// updateLockdown engages the lockdown kill switch while lockdown is on and no
// tunnel is connected.
func updateLockdown() {
	lockdownMu.Lock()
	defer lockdownMu.Unlock()
	s := currentSettings()
	want := s.Lockdown.Enabled && !anyTunnelStarted()
	have := firewall.LockdownActive()
	if want == have {
		return
	}
	if want {
		if err := firewall.EnableLockdown(s.Lockdown.AllowLAN); err != nil {
			log.Printf("Unable to enable lockdown: %v", err)
			return
		}
		log.Println("Lockdown engaged: blocking traffic until a tunnel connects")
	} else {
		firewall.DisableLockdown()
		log.Println("Lockdown lifted")
	}
	IPCServerNotifyExt("lockdown", map[string]bool{"active": !have})
}

// DisableLockdownIfActive drops the lockdown filters so they can be rebuilt
// with new options.
func DisableLockdownIfActive() {
	lockdownMu.Lock()
	defer lockdownMu.Unlock()
	firewall.DisableLockdown()
}

func lockdownActive() bool {
	return firewall.LockdownActive()
}

// onTunnelStateChange is called by the tunnel tracker for every state
// change.
func onTunnelStateChange(name string, state TunnelState) {
	if state == TunnelStarted || state == TunnelStopped {
		go updateLockdown()
	}
	healthStateChanged(name, state)
}

var restartMu sync.Mutex

// restartTunnel stops and starts a tunnel, waiting for the old service to
// disappear in between.
func restartTunnel(s *ManagerService, name, reason string) error {
	restartMu.Lock()
	defer restartMu.Unlock()
	log.Printf("[%s] Restarting tunnel: %s", name, reason)
	if s == nil {
		s = &ManagerService{}
	}
	if err := s.Stop(name); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- s.WaitForStop(name) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		log.Printf("[%s] Tunnel did not stop within 30 seconds", name)
	}
	return s.Start(name)
}

// splitDriverAvailable reports whether the optional split tunnel driver can
// be used.
func splitDriverAvailable() bool {
	name, _ := windows.UTF16PtrFromString(`\\.\MULLVADSPLITTUNNEL`)
	h, err := windows.CreateFile(name, 0, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err == nil {
		windows.CloseHandle(h)
		return true
	}
	if err == windows.ERROR_ACCESS_DENIED || err == windows.ERROR_SHARING_VIOLATION {
		return true
	}
	if p := currentSettings().SplitDriverPath; p != "" {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(filepath.Dir(exe), "mullvad-split-tunnel.sys"))
	return err == nil
}
