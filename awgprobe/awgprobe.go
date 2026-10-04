/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

// Package awgprobe tests which AmneziaWG obfuscation parameters reach a
// server, by performing real handshakes with a throwaway in-memory device.
package awgprobe

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-go/v3/tun/tuntest"
)

// Params are the obfuscation settings of an [Interface] section. Jc, Jmin
// and Jmax only affect the client; S1–S4 and H1–H4 must match the server.
type Params struct {
	Jc, Jmin, Jmax int
	S1, S2, S3, S4 int
	H1, H2, H3, H4 string
}

// WireGuard returns the parameters a plain WireGuard server understands,
// with the given junk packets in front of the handshake.
func WireGuard(jc, jmin, jmax int) Params {
	return Params{Jc: jc, Jmin: jmin, Jmax: jmax, H1: "1", H2: "2", H3: "3", H4: "4"}
}

// UAPI renders the parameters as UAPI interface keys. Zero values are left
// out, which leaves the device's defaults (plain WireGuard) in place.
func (p Params) UAPI() string {
	var b strings.Builder
	num := func(k string, v int) {
		if v > 0 {
			fmt.Fprintf(&b, "%s=%d\n", k, v)
		}
	}
	str := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "%s=%s\n", k, v)
		}
	}
	num("jc", p.Jc)
	num("jmin", p.Jmin)
	num("jmax", p.Jmax)
	num("s1", p.S1)
	num("s2", p.S2)
	num("s3", p.S3)
	num("s4", p.S4)
	str("h1", p.H1)
	str("h2", p.H2)
	str("h3", p.H3)
	str("h4", p.H4)
	return b.String()
}

// Target describes the tunnel to probe.
type Target struct {
	// UAPI is the tunnel's configuration in UAPI form without obfuscation
	// parameters and without listen_port; endpoints must be resolved.
	UAPI string
	// Src and Dst are the addresses of a test packet that is routed to the
	// peer, which makes the device start a handshake.
	Src, Dst netip.Addr
}

// insertParams puts the interface keys before the first peer, where UAPI
// expects them.
func insertParams(base, params string) string {
	for _, marker := range []string{"replace_peers=", "public_key="} {
		if i := strings.Index(base, marker); i >= 0 && (i == 0 || base[i-1] == '\n') {
			return base[:i] + params + base[i:]
		}
	}
	return base + params
}

// Handshake starts a fresh device with the parameters and waits until the
// handshake with the peer completes. It returns how long that took.
func Handshake(ctx context.Context, t Target, p Params) (time.Duration, error) {
	tunDev := tuntest.NewChannelTUN()
	quiet := &device.Logger{Verbosef: device.DiscardLogf, Errorf: device.DiscardLogf}
	dev := device.NewDevice(tunDev.TUN(), conn.NewDefaultBind(), quiet)
	defer dev.Close()
	if err := dev.IpcSet(insertParams(t.UAPI, p.UAPI())); err != nil {
		return 0, fmt.Errorf("invalid parameters: %w", err)
	}
	if err := dev.Up(); err != nil {
		return 0, err
	}
	// Drain anything the device delivers so it never blocks.
	go func() {
		for range tunDev.Inbound {
		}
	}()
	start := time.Now()
	ping := tuntest.Ping(t.Dst, t.Src)
	select {
	case tunDev.Outbound <- ping:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0, errors.New("no handshake")
		case <-tick.C:
			if handshakeDone(dev) {
				return time.Since(start), nil
			}
		}
	}
}

func handshakeDone(dev *device.Device) bool {
	s, err := dev.IpcGet()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(s, "\n") {
		if v, ok := strings.CutPrefix(line, "last_handshake_time_sec="); ok && v != "0" {
			return true
		}
	}
	return false
}

// Candidate is one parameter set to try.
type Candidate struct {
	Name   string
	Params Params
}

// Result is the outcome of probing a candidate.
type Result struct {
	Candidate
	OK    bool
	RTT   time.Duration
	Error string
}

// Run probes the candidates one after another (so the server only ever
// sees one handshake at a time) and calls progress after each. Each
// candidate gets two attempts of the given timeout; the faster success
// counts.
func Run(ctx context.Context, t Target, candidates []Candidate, timeout time.Duration, progress func(done int, r Result)) []Result {
	results := make([]Result, 0, len(candidates))
	for i, c := range candidates {
		r := Result{Candidate: c}
		for attempt := 0; attempt < 2 && ctx.Err() == nil; attempt++ {
			actx, cancel := context.WithTimeout(ctx, timeout)
			rtt, err := Handshake(actx, t, c.Params)
			cancel()
			if err != nil {
				if !r.OK {
					r.Error = err.Error()
				}
				continue
			}
			if !r.OK || rtt < r.RTT {
				r.OK, r.RTT, r.Error = true, rtt, ""
			}
		}
		results = append(results, r)
		if progress != nil {
			progress(i+1, r)
		}
	}
	return results
}

// Best returns the working candidate with the fastest handshake. Ties
// within 15% go to the earlier candidate, so the order of the list
// expresses preference.
func Best(results []Result) (Result, bool) {
	var best Result
	found := false
	for _, r := range results {
		if !r.OK {
			continue
		}
		if !found || r.RTT*100 < best.RTT*85 {
			best, found = r, true
		}
	}
	return best, found
}
