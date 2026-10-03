/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

// Package extras stores the data that BetterAmnezia adds on top of plain
// tunnel configurations: per-tunnel split tunneling rules and the global
// settings consumed by the manager service.
package extras

import (
	"encoding/json"
	"strings"

	"github.com/amnezia-vpn/amneziawg-windows-client/splittunnel"
)

// NetworkAction is what auto-tunneling does when a network becomes active.
type NetworkAction string

const (
	ActionNone       NetworkAction = "none"
	ActionConnect    NetworkAction = "connect"
	ActionDisconnect NetworkAction = "disconnect"
)

// SSIDRule maps a Wi-Fi network (wildcards allowed) to a tunnel.
type SSIDRule struct {
	SSID   string        `json:"ssid"`
	Action NetworkAction `json:"action"`
	Tunnel string        `json:"tunnel,omitempty"`
}

// AutoTunnel connects and disconnects tunnels based on the active network.
type AutoTunnel struct {
	Enabled bool `json:"enabled"`
	// Tunnel is used by rules that do not name a tunnel.
	Tunnel string `json:"tunnel"`
	// TrustedSSIDs disconnect tunnels; wildcards are allowed.
	TrustedSSIDs    []string      `json:"trustedSsids,omitempty"`
	OnUntrustedWiFi NetworkAction `json:"onUntrustedWifi"`
	OnEthernet      NetworkAction `json:"onEthernet"`
	OnOther         NetworkAction `json:"onOther"`
	// Rules are checked before the generic actions above.
	Rules []SSIDRule `json:"rules,omitempty"`
	// DebounceSeconds waits for the network to settle before acting.
	DebounceSeconds int `json:"debounceSeconds,omitempty"`
}

// Health watches running tunnels and restarts broken ones.
type Health struct {
	Enabled bool `json:"enabled"`
	// HandshakeTimeoutSeconds: a tunnel that keeps sending but has not
	// completed a handshake for this long is considered dead.
	HandshakeTimeoutSeconds int `json:"handshakeTimeoutSeconds"`
	// PingTarget is optionally pinged through the tunnel.
	PingTarget             string `json:"pingTarget,omitempty"`
	PingIntervalSeconds    int    `json:"pingIntervalSeconds"`
	PingFailuresForRestart int    `json:"pingFailuresForRestart"`
	// RestartCooldownSeconds prevents restart loops.
	RestartCooldownSeconds int `json:"restartCooldownSeconds"`
}

// DynamicDNS re-resolves peer endpoints given as host names.
type DynamicDNS struct {
	Enabled         bool `json:"enabled"`
	IntervalSeconds int  `json:"intervalSeconds"`
	// PreferIPv6 picks an IPv6 address for endpoints when one exists and
	// IPv6 connectivity is available.
	PreferIPv6 bool `json:"preferIpv6,omitempty"`
}

// Lockdown blocks all traffic while no tunnel is connected.
type Lockdown struct {
	Enabled  bool `json:"enabled"`
	AllowLAN bool `json:"allowLan"`
}

// Settings are the global, service-side settings.
type Settings struct {
	AutoTunnel AutoTunnel `json:"autoTunnel"`
	Health     Health     `json:"health"`
	DynamicDNS DynamicDNS `json:"dynamicDns"`
	Lockdown   Lockdown   `json:"lockdown"`
	// SplitDriverPath points at mullvad-split-tunnel.sys for app bypass.
	SplitDriverPath string `json:"splitDriverPath,omitempty"`
	// RemoteControl lets non-elevated processes start and stop tunnels with
	// amneziawg.exe /connect and friends. It lives here rather than in the
	// per-user interface preferences so that only administrators can turn
	// it on.
	RemoteControl bool `json:"remoteControl,omitempty"`
}

// DefaultSettings returns the settings used before anything is saved.
func DefaultSettings() *Settings {
	return &Settings{
		AutoTunnel: AutoTunnel{
			OnUntrustedWiFi: ActionConnect,
			OnEthernet:      ActionNone,
			OnOther:         ActionNone,
			DebounceSeconds: 3,
		},
		Health: Health{
			Enabled:                 true,
			HandshakeTimeoutSeconds: 180,
			PingIntervalSeconds:     30,
			PingFailuresForRestart:  3,
			RestartCooldownSeconds:  60,
		},
		DynamicDNS: DynamicDNS{
			Enabled:         true,
			IntervalSeconds: 300,
		},
	}
}

// ParseSettings decodes settings on top of the defaults.
func ParseSettings(data []byte) (*Settings, error) {
	s := DefaultSettings()
	if len(strings.TrimSpace(string(data))) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}
	s.normalize()
	return s, nil
}

func (s *Settings) normalize() {
	d := DefaultSettings()
	if s.Health.HandshakeTimeoutSeconds < 30 {
		s.Health.HandshakeTimeoutSeconds = d.Health.HandshakeTimeoutSeconds
	}
	if s.Health.PingIntervalSeconds < 5 {
		s.Health.PingIntervalSeconds = d.Health.PingIntervalSeconds
	}
	if s.Health.PingFailuresForRestart < 1 {
		s.Health.PingFailuresForRestart = d.Health.PingFailuresForRestart
	}
	if s.Health.RestartCooldownSeconds < 10 {
		s.Health.RestartCooldownSeconds = d.Health.RestartCooldownSeconds
	}
	if s.DynamicDNS.IntervalSeconds < 30 {
		s.DynamicDNS.IntervalSeconds = d.DynamicDNS.IntervalSeconds
	}
	if s.AutoTunnel.DebounceSeconds < 0 || s.AutoTunnel.DebounceSeconds > 60 {
		s.AutoTunnel.DebounceSeconds = d.AutoTunnel.DebounceSeconds
	}
	for _, a := range []*NetworkAction{&s.AutoTunnel.OnUntrustedWiFi, &s.AutoTunnel.OnEthernet, &s.AutoTunnel.OnOther} {
		switch *a {
		case ActionConnect, ActionDisconnect, ActionNone:
		default:
			*a = ActionNone
		}
	}
}

// Marshal encodes the settings.
func (s *Settings) Marshal() ([]byte, error) {
	s.normalize()
	return json.MarshalIndent(s, "", "  ")
}

// NetworkKind classifies the active network for auto-tunneling.
type NetworkKind string

const (
	NetworkNone     NetworkKind = "none"
	NetworkWiFi     NetworkKind = "wifi"
	NetworkEthernet NetworkKind = "ethernet"
	NetworkOther    NetworkKind = "other"
)

// NetworkState describes the active network.
type NetworkState struct {
	Kind NetworkKind `json:"kind"`
	SSID string      `json:"ssid,omitempty"`
	// Name is the adapter or network profile name.
	Name string `json:"name,omitempty"`
}

// Key identifies a network for change detection.
func (n NetworkState) Key() string {
	return string(n.Kind) + "\x00" + n.SSID
}

// Decision is the outcome of evaluating auto-tunnel rules.
type Decision struct {
	Action NetworkAction
	Tunnel string
	Reason string
}

func ssidMatches(pattern, ssid string) bool {
	if pattern == "" {
		return false
	}
	return splittunnel.GlobMatch(strings.ToLower(pattern), strings.ToLower(ssid))
}

// Decide evaluates the auto-tunnel configuration for a network.
func (a *AutoTunnel) Decide(n NetworkState) Decision {
	if !a.Enabled {
		return Decision{Action: ActionNone, Reason: "auto-tunneling is off"}
	}
	tunnelOr := func(t string) string {
		if t != "" {
			return t
		}
		return a.Tunnel
	}
	switch n.Kind {
	case NetworkNone:
		return Decision{Action: ActionNone, Reason: "no network"}
	case NetworkWiFi:
		for _, r := range a.Rules {
			if ssidMatches(r.SSID, n.SSID) {
				return Decision{Action: r.Action, Tunnel: tunnelOr(r.Tunnel), Reason: "rule for Wi-Fi " + r.SSID}
			}
		}
		for _, t := range a.TrustedSSIDs {
			if ssidMatches(t, n.SSID) {
				return Decision{Action: ActionDisconnect, Reason: "trusted Wi-Fi " + n.SSID}
			}
		}
		return Decision{Action: a.OnUntrustedWiFi, Tunnel: a.Tunnel, Reason: "untrusted Wi-Fi " + n.SSID}
	case NetworkEthernet:
		return Decision{Action: a.OnEthernet, Tunnel: a.Tunnel, Reason: "Ethernet"}
	}
	return Decision{Action: a.OnOther, Tunnel: a.Tunnel, Reason: "other network"}
}
