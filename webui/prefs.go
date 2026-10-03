/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package webui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/windows/registry"
)

// Prefs are per-user interface preferences. They live in the user's local
// app data, separate from the service settings.
type Prefs struct {
	Theme  string `json:"theme"`  // system, light, dark, amoled
	Accent string `json:"accent"` // CSS colour
	// Language is "auto", "en" or "ru".
	Language       string `json:"language"`
	CloseToTray    bool   `json:"closeToTray"`
	StartMinimized bool   `json:"startMinimized"`
	Notifications  bool   `json:"notifications"`
	RemoteControl  bool   `json:"remoteControl"`
	LegacyUI       bool   `json:"legacyUi"`
	Compact        bool   `json:"compact"`
	// LastTunnel is the tunnel selected when the UI was last closed.
	LastTunnel string `json:"lastTunnel,omitempty"`
}

func defaultPrefs() Prefs {
	return Prefs{
		Theme:          "system",
		Accent:         "#7c5cff",
		Language:       "auto",
		CloseToTray:    true,
		StartMinimized: true,
		Notifications:  true,
	}
}

var (
	prefsMu     sync.Mutex
	prefsCache  *Prefs
	prefsLoaded bool
)

func prefsPath() string { return filepath.Join(dataDir(), "ui.json") }

func loadPrefs() Prefs {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	if prefsLoaded {
		return *prefsCache
	}
	p := defaultPrefs()
	if data, err := os.ReadFile(prefsPath()); err == nil {
		json.Unmarshal(data, &p)
	}
	switch p.Theme {
	case "system", "light", "dark", "amoled":
	default:
		p.Theme = "system"
	}
	prefsCache = &p
	prefsLoaded = true
	return p
}

func savePrefs(p Prefs) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := prefsPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, prefsPath()); err != nil {
		return err
	}
	prefsMu.Lock()
	prefsCache = &p
	prefsLoaded = true
	prefsMu.Unlock()
	return nil
}

// systemUsesLightTheme reads the Windows app theme.
func systemUsesLightTheme() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v != 0
}

func (p Prefs) effectiveTheme() string {
	if p.Theme == "system" {
		if systemUsesLightTheme() {
			return "light"
		}
		return "dark"
	}
	return p.Theme
}

// UseLegacyUI reports whether the user asked for the classic interface.
func UseLegacyUI() bool {
	return loadPrefs().LegacyUI
}

// SetLegacyUI switches between the classic and the new interface.
func SetLegacyUI(legacy bool) error {
	p := loadPrefs()
	p.LegacyUI = legacy
	return savePrefs(p)
}
