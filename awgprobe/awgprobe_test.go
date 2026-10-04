/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package awgprobe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"testing"
	"time"

	"golang.org/x/crypto/curve25519"

	"github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-go/v3/tun/tuntest"
)

func keypair(t *testing.T) (priv, pub string) {
	var k [32]byte
	if _, err := rand.Read(k[:]); err != nil {
		t.Fatal(err)
	}
	k[0] &= 248
	k[31] = (k[31] & 127) | 64
	p, err := curve25519.X25519(k[:], curve25519.Basepoint)
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(k[:]), hex.EncodeToString(p)
}

func freePort(t *testing.T) int {
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

// server starts an AmneziaWG server on loopback with the given parameters
// and returns the client's target.
func server(t *testing.T, p Params) Target {
	sPriv, sPub := keypair(t)
	cPriv, cPub := keypair(t)
	port := freePort(t)
	tunDev := tuntest.NewChannelTUN()
	quiet := &device.Logger{Verbosef: device.DiscardLogf, Errorf: device.DiscardLogf}
	dev := device.NewDevice(tunDev.TUN(), conn.NewDefaultBind(), quiet)
	t.Cleanup(dev.Close)
	cfg := fmt.Sprintf("private_key=%s\nlisten_port=%d\n%spublic_key=%s\nallowed_ip=10.9.0.2/32\n", sPriv, port, p.UAPI(), cPub)
	if err := dev.IpcSet(cfg); err != nil {
		t.Fatal(err)
	}
	if err := dev.Up(); err != nil {
		t.Fatal(err)
	}
	go func() {
		for range tunDev.Inbound {
		}
	}()
	return Target{
		UAPI: fmt.Sprintf("private_key=%s\nreplace_peers=true\npublic_key=%s\nendpoint=127.0.0.1:%d\nallowed_ip=10.9.0.1/32\n", cPriv, sPub, port),
		Src:  netip.MustParseAddr("10.9.0.2"),
		Dst:  netip.MustParseAddr("10.9.0.1"),
	}
}

func TestProbeFindsMatchingParameters(t *testing.T) {
	custom := Params{Jc: 4, Jmin: 40, Jmax: 70, S1: 30, S2: 40, H1: "1111", H2: "2222", H3: "3333", H4: "4444"}
	target := server(t, custom)

	candidates := []Candidate{
		{"wireguard", WireGuard(4, 40, 70)},
		{"custom", custom},
		{"custom-more-junk", Params{Jc: 8, Jmin: 60, Jmax: 300, S1: 30, S2: 40, H1: "1111", H2: "2222", H3: "3333", H4: "4444"}},
	}
	results := Run(context.Background(), target, candidates, 2*time.Second, nil)
	if results[0].OK {
		t.Error("WireGuard parameters must not reach a server with custom headers")
	}
	if !results[1].OK || !results[2].OK {
		t.Fatalf("matching parameters failed: %+v", results)
	}
	best, ok := Best(results)
	if !ok || best.Name == "wireguard" {
		t.Fatalf("best = %+v", best)
	}
}

func TestProbeWireGuardServer(t *testing.T) {
	// A server without obfuscation behaves like plain WireGuard: junk
	// packets are ignored, the headers must be 1-4.
	target := server(t, Params{})
	r := Run(context.Background(), target, []Candidate{
		{"default", WireGuard(4, 40, 70)},
		{"random", Params{Jc: 4, Jmin: 40, Jmax: 70, S1: 20, S2: 30, H1: "5555", H2: "6666", H3: "7777", H4: "8888"}},
	}, 2*time.Second, nil)
	if !r[0].OK {
		t.Fatalf("default parameters failed against WireGuard: %s", r[0].Error)
	}
	if r[1].OK {
		t.Fatal("random headers must not reach plain WireGuard")
	}
}

func TestInsertParams(t *testing.T) {
	got := insertParams("private_key=a\nreplace_peers=true\npublic_key=b\n", "jc=4\n")
	if got != "private_key=a\njc=4\nreplace_peers=true\npublic_key=b\n" {
		t.Fatalf("%q", got)
	}
}
