/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package extras

import "testing"

func TestParseSettingsDefaults(t *testing.T) {
	s, err := ParseSettings([]byte(`{"health":{"enabled":false,"handshakeTimeoutSeconds":5},"autoTunnel":{"onEthernet":"bogus"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Health.Enabled {
		t.Error("explicit false must be kept")
	}
	if s.Health.HandshakeTimeoutSeconds != 180 {
		t.Errorf("too small timeout should be reset, got %d", s.Health.HandshakeTimeoutSeconds)
	}
	if !s.DynamicDNS.Enabled || s.DynamicDNS.IntervalSeconds != 300 {
		t.Errorf("defaults lost: %+v", s.DynamicDNS)
	}
	if s.AutoTunnel.OnEthernet != ActionNone || s.AutoTunnel.OnUntrustedWiFi != ActionConnect {
		t.Errorf("auto tunnel actions: %+v", s.AutoTunnel)
	}
}

func TestAutoTunnelDecide(t *testing.T) {
	a := &AutoTunnel{
		Enabled:         true,
		Tunnel:          "main",
		TrustedSSIDs:    []string{"HomeWiFi", "Office-*"},
		OnUntrustedWiFi: ActionConnect,
		OnEthernet:      ActionDisconnect,
		OnOther:         ActionNone,
		Rules: []SSIDRule{
			{SSID: "Cafe *", Action: ActionConnect, Tunnel: "fast"},
			{SSID: "Office-Guest", Action: ActionConnect},
		},
	}
	cases := []struct {
		n      NetworkState
		action NetworkAction
		tunnel string
	}{
		{NetworkState{Kind: NetworkWiFi, SSID: "homewifi"}, ActionDisconnect, ""},
		{NetworkState{Kind: NetworkWiFi, SSID: "Office-5G"}, ActionDisconnect, ""},
		{NetworkState{Kind: NetworkWiFi, SSID: "Office-Guest"}, ActionConnect, "main"},
		{NetworkState{Kind: NetworkWiFi, SSID: "Cafe Central"}, ActionConnect, "fast"},
		{NetworkState{Kind: NetworkWiFi, SSID: "Airport"}, ActionConnect, "main"},
		{NetworkState{Kind: NetworkEthernet}, ActionDisconnect, "main"},
		{NetworkState{Kind: NetworkOther}, ActionNone, "main"},
		{NetworkState{Kind: NetworkNone}, ActionNone, ""},
	}
	for _, c := range cases {
		d := a.Decide(c.n)
		if d.Action != c.action || d.Tunnel != c.tunnel {
			t.Errorf("Decide(%+v) = %+v, want %s/%s", c.n, d, c.action, c.tunnel)
		}
	}
	a.Enabled = false
	if d := a.Decide(NetworkState{Kind: NetworkWiFi, SSID: "Airport"}); d.Action != ActionNone {
		t.Errorf("disabled auto tunnel acted: %+v", d)
	}
}
