/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package webui

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"sync"
	"time"

	"github.com/amnezia-vpn/amneziawg-windows/v3/conf"

	"github.com/amnezia-vpn/amneziawg-windows-client/awgprobe"
	"github.com/amnezia-vpn/amneziawg-windows-client/manager"
)

var awgScanMu sync.Mutex

// currentParams reads the obfuscation parameters from a configuration.
func currentParams(c *conf.Config) awgprobe.Params {
	i := c.Interface
	return awgprobe.Params{
		Jc: int(i.JunkPacketCount), Jmin: int(i.JunkPacketMinSize), Jmax: int(i.JunkPacketMaxSize),
		S1: int(i.InitPacketJunkSize), S2: int(i.ResponsePacketJunkSize),
		S3: int(i.CookieReplyPacketJunkSize), S4: int(i.TransportPacketJunkSize),
		H1: i.InitPacketMagicHeader, H2: i.ResponsePacketMagicHeader, H3: i.UnderloadPacketMagicHeader, H4: i.TransportPacketMagicHeader,
	}
}

// customHeaders reports whether the parameters need an AmneziaWG server.
func customHeaders(p awgprobe.Params) bool {
	isDefault := func(v, d string) bool { return v == "" || v == d }
	return p.S1 > 0 || p.S2 > 0 || p.S3 > 0 || p.S4 > 0 || !isDefault(p.H1, "1") || !isDefault(p.H2, "2") || !isDefault(p.H3, "3") || !isDefault(p.H4, "4")
}

// scanCandidates lists what to try, in order of preference. Junk packets
// only matter to the network in between, so they are varied freely; S1, S2
// S3, S4 and H1–H4 must match the server, so the only choices are the values in the
// configuration and plain WireGuard's.
func scanCandidates(cur awgprobe.Params) []awgprobe.Candidate {
	var list []awgprobe.Candidate
	if customHeaders(cur) || cur.Jc > 0 {
		list = append(list, awgprobe.Candidate{Name: "current", Params: cur})
	}
	if customHeaders(cur) {
		withJunk := func(jc, jmin, jmax int) awgprobe.Params {
			p := cur
			p.Jc, p.Jmin, p.Jmax = jc, jmin, jmax
			return p
		}
		list = append(list,
			awgprobe.Candidate{Name: "current-light", Params: withJunk(3, 10, 50)},
			awgprobe.Candidate{Name: "current-heavy", Params: withJunk(8, 64, 512)})
	}
	return append(list,
		awgprobe.Candidate{Name: "default", Params: awgprobe.WireGuard(4, 40, 70)},
		awgprobe.Candidate{Name: "default-heavy", Params: awgprobe.WireGuard(8, 64, 512)},
		awgprobe.Candidate{Name: "wireguard", Params: awgprobe.WireGuard(0, 0, 0)})
}

// probeTarget turns a configuration into a probe target: obfuscation and
// the listen port are stripped (the probe adds its own), endpoints are
// resolved, and a test packet is addressed into the first peer's routes.
func probeTarget(c *conf.Config) (awgprobe.Target, error) {
	if len(c.Peers) == 0 || c.Peers[0].Endpoint.IsEmpty() {
		return awgprobe.Target{}, errors.New(tr("The configuration needs a peer with an endpoint."))
	}
	cc := *c
	cc.Interface.ListenPort = 0
	cc.Interface.JunkPacketCount, cc.Interface.JunkPacketMinSize, cc.Interface.JunkPacketMaxSize = 0, 0, 0
	cc.Interface.InitPacketJunkSize, cc.Interface.ResponsePacketJunkSize = 0, 0
	cc.Interface.CookieReplyPacketJunkSize, cc.Interface.TransportPacketJunkSize = 0, 0
	cc.Interface.InitPacketMagicHeader, cc.Interface.ResponsePacketMagicHeader = "", ""
	cc.Interface.UnderloadPacketMagicHeader, cc.Interface.TransportPacketMagicHeader = "", ""
	cc.Peers = c.Peers[:1]
	uapi, err := cc.ToUAPI()
	if err != nil {
		return awgprobe.Target{}, err
	}
	var src netip.Addr
	for _, a := range c.Interface.Addresses {
		if ip, ok := netip.AddrFromSlice(a.IP); ok {
			src = ip.Unmap()
			if src.Is4() {
				break
			}
		}
	}
	if !src.IsValid() {
		return awgprobe.Target{}, errors.New(tr("The configuration needs an interface address."))
	}
	var dst netip.Addr
	for _, a := range c.Peers[0].AllowedIPs {
		ip, ok := netip.AddrFromSlice(a.IP)
		if !ok || ip.Unmap().Is4() != src.Is4() {
			continue
		}
		dst = ip.Unmap()
		if a.Cidr == 0 {
			if src.Is4() {
				dst = netip.MustParseAddr("1.1.1.1")
			} else {
				dst = netip.MustParseAddr("2606:4700:4700::1111")
			}
		}
		break
	}
	if !dst.IsValid() {
		return awgprobe.Target{}, errors.New(tr("The peer has no allowed IPs of the interface's address family."))
	}
	return awgprobe.Target{UAPI: uapi, Src: src, Dst: dst}, nil
}

// runningWithKey reports whether a running tunnel uses the private key. A
// probe handshake would take over its session on the server.
func runningWithKey(k conf.Key) bool {
	tunnels, err := manager.IPCClientTunnels()
	if err != nil {
		return false
	}
	for _, t := range tunnels {
		if s, err := t.State(); err != nil || (s != manager.TunnelStarted && s != manager.TunnelStarting) {
			continue
		}
		if c, err := t.StoredConfig(); err == nil && c.Interface.PrivateKey == k {
			return true
		}
	}
	return false
}

type scanResult struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	RTTms int64  `json:"rttMs"`
	Error string `json:"error,omitempty"`
	Jc    int    `json:"jc"`
	Jmin  int    `json:"jmin"`
	Jmax  int    `json:"jmax"`
	S1    int    `json:"s1"`
	S2    int    `json:"s2"`
	S3    int    `json:"s3"`
	S4    int    `json:"s4"`
	H1    string `json:"h1"`
	H2    string `json:"h2"`
	H3    string `json:"h3"`
	H4    string `json:"h4"`
}

func toScanResult(r awgprobe.Result) scanResult {
	p := r.Params
	return scanResult{Name: r.Name, OK: r.OK, RTTms: r.RTT.Milliseconds(), Error: r.Error,
		Jc: p.Jc, Jmin: p.Jmin, Jmax: p.Jmax, S1: p.S1, S2: p.S2, S3: p.S3, S4: p.S4, H1: p.H1, H2: p.H2, H3: p.H3, H4: p.H4}
}

// awgScan tries the candidate parameters against the server of the
// configuration being edited and reports the best one.
func (b *bridge) awgScan(params json.RawMessage) (any, error) {
	p, err := decode[struct {
		Text string `json:"text"`
	}](params)
	if err != nil {
		return nil, err
	}
	c, err := conf.FromWgQuickWithUnknownEncoding(p.Text, "probe")
	if err != nil {
		return nil, err
	}
	if runningWithKey(c.Interface.PrivateKey) {
		return nil, errors.New(tr("Disconnect this tunnel first: the test would interrupt its connection."))
	}
	target, err := probeTarget(c)
	if err != nil {
		return nil, err
	}
	if !awgScanMu.TryLock() {
		return nil, errors.New(tr("A scan is already running."))
	}
	defer awgScanMu.Unlock()

	candidates := scanCandidates(currentParams(c))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	results := awgprobe.Run(ctx, target, candidates, 4*time.Second, func(done int, r awgprobe.Result) {
		b.emit("awgScan", map[string]any{"done": done, "total": len(candidates), "result": toScanResult(r)})
	})
	out := map[string]any{}
	list := make([]scanResult, 0, len(results))
	for _, r := range results {
		list = append(list, toScanResult(r))
	}
	out["results"] = list
	if best, ok := awgprobe.Best(results); ok {
		out["best"] = toScanResult(best)
	}
	return out, nil
}
