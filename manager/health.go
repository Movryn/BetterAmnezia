/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package manager

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/amnezia-vpn/amneziawg-windows/v3/conf"
)

// HealthStatus is reported to the UI for each running tunnel.
type HealthStatus struct {
	Healthy       bool      `json:"healthy"`
	LastHandshake time.Time `json:"lastHandshake"`
	LastPingOK    time.Time `json:"lastPingOk,omitempty"`
	PingFailures  int       `json:"pingFailures"`
	Restarts      int       `json:"restarts"`
	LastRestart   time.Time `json:"lastRestart,omitempty"`
	Message       string    `json:"message,omitempty"`
}

type tunnelHealth struct {
	status      HealthStatus
	startedAt   time.Time
	lastRx      uint64
	lastTx      uint64
	stallSince  time.Time
	nextPing    time.Time
	restarting  bool
	initialized bool
}

var (
	healthMu sync.Mutex
	health   = make(map[string]*tunnelHealth)
)

func healthSnapshot() map[string]HealthStatus {
	healthMu.Lock()
	defer healthMu.Unlock()
	out := make(map[string]HealthStatus, len(health))
	for name, h := range health {
		out[name] = h.status
	}
	return out
}

func healthStateChanged(name string, state TunnelState) {
	healthMu.Lock()
	defer healthMu.Unlock()
	switch state {
	case TunnelStarted:
		h := health[name]
		if h == nil {
			h = &tunnelHealth{}
			health[name] = h
		}
		h.startedAt = time.Now()
		h.stallSince = time.Time{}
		h.initialized = false
		h.restarting = false
		h.status.Healthy = true
		h.status.PingFailures = 0
		h.status.Message = ""
	case TunnelStopped:
		if h := health[name]; h != nil && !h.restarting {
			delete(health, name)
		}
	}
}

// runtimeConfig reads the live configuration of a running tunnel without
// redacting keys; it is used internally only.
func runtimeConfig(tunnelName string) (*conf.Config, error) {
	storedConfig, err := conf.LoadFromName(tunnelName)
	if err != nil {
		return nil, err
	}
	pipe, err := connectTunnelServicePipe(tunnelName)
	if err != nil {
		return nil, err
	}
	pipe.SetDeadline(time.Now().Add(time.Second * 2))
	_, err = pipe.Write([]byte("get=1\n\n"))
	if err != nil {
		pipe.Unlock()
		disconnectTunnelServicePipe(tunnelName)
		return nil, err
	}
	c, err := conf.FromUAPI(pipe, storedConfig)
	pipe.Unlock()
	if err != nil {
		disconnectTunnelServicePipe(tunnelName)
	}
	return c, err
}

// uapiSet sends a set operation to a running tunnel.
func uapiSet(tunnelName, body string) error {
	pipe, err := connectTunnelServicePipe(tunnelName)
	if err != nil {
		return err
	}
	defer pipe.Unlock()
	pipe.SetDeadline(time.Now().Add(time.Second * 2))
	if _, err := pipe.Write([]byte("set=1\n" + body + "\n")); err != nil {
		pipe.Unlock()
		disconnectTunnelServicePipe(tunnelName)
		pipe.Lock()
		return err
	}
	r := bufio.NewReader(pipe)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return nil
		}
		if strings.HasPrefix(line, "errno=") && line != "errno=0" {
			return fmt.Errorf("tunnel rejected update (%s)", line)
		}
	}
}

func healthMonitor() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s := currentSettings().Health
		if !s.Enabled {
			continue
		}
		for _, name := range startedTunnelsOnly() {
			checkTunnelHealth(name, s.HandshakeTimeoutSeconds, s.PingTarget, s.PingIntervalSeconds, s.PingFailuresForRestart, s.RestartCooldownSeconds)
		}
	}
}

func startedTunnelsOnly() []string {
	trackedTunnelsLock.Lock()
	defer trackedTunnelsLock.Unlock()
	var out []string
	for name, state := range trackedTunnels {
		if state == TunnelStarted {
			out = append(out, name)
		}
	}
	return out
}

func checkTunnelHealth(name string, timeoutSec int, pingTarget string, pingInterval, pingFailures, cooldownSec int) {
	c, err := runtimeConfig(name)
	if err != nil {
		return
	}
	var rx, tx uint64
	var lastHandshake time.Time
	for _, p := range c.Peers {
		rx += uint64(p.RxBytes)
		tx += uint64(p.TxBytes)
		if p.LastHandshakeTime.IsEmpty() {
			continue
		}
		if t := time.Unix(0, 0).Add(time.Duration(p.LastHandshakeTime)); t.After(lastHandshake) {
			lastHandshake = t
		}
	}
	now := time.Now()
	timeout := time.Duration(timeoutSec) * time.Second

	healthMu.Lock()
	h := health[name]
	if h == nil {
		h = &tunnelHealth{startedAt: now}
		health[name] = h
	}
	if h.restarting {
		healthMu.Unlock()
		return
	}
	h.status.LastHandshake = lastHandshake
	reason := ""
	if !h.initialized {
		h.initialized = true
	} else if rx > h.lastRx {
		h.stallSince = time.Time{}
	} else if tx > h.lastTx && h.stallSince.IsZero() {
		h.stallSince = now
	}
	h.lastRx, h.lastTx = rx, tx
	if !h.stallSince.IsZero() && now.Sub(h.stallSince) > timeout && now.Sub(lastHandshake) > timeout && now.Sub(h.startedAt) > timeout {
		reason = fmt.Sprintf("no data received and no handshake for %s", now.Sub(lastHandshake).Round(time.Second))
	}
	doPing := pingTarget != "" && now.After(h.nextPing) && now.Sub(h.startedAt) > 15*time.Second
	if doPing {
		h.nextPing = now.Add(time.Duration(pingInterval) * time.Second)
	}
	healthMu.Unlock()

	if doPing && reason == "" {
		ok := pingThroughTunnel(c, pingTarget)
		healthMu.Lock()
		if ok {
			h.status.PingFailures = 0
			h.status.LastPingOK = now
		} else {
			h.status.PingFailures++
			if h.status.PingFailures >= pingFailures {
				reason = fmt.Sprintf("%d pings to %s failed", h.status.PingFailures, pingTarget)
			}
		}
		healthMu.Unlock()
	}

	healthMu.Lock()
	if reason == "" {
		h.status.Healthy = true
		h.status.Message = ""
		healthMu.Unlock()
		return
	}
	h.status.Healthy = false
	h.status.Message = reason
	if !h.status.LastRestart.IsZero() && now.Sub(h.status.LastRestart) < time.Duration(cooldownSec)*time.Second {
		healthMu.Unlock()
		return
	}
	h.restarting = true
	h.status.Restarts++
	h.status.LastRestart = now
	status := h.status
	healthMu.Unlock()

	log.Printf("[%s] Health check failed: %s", name, reason)
	IPCServerNotifyExt("health", map[string]any{"tunnel": name, "message": reason, "status": status})
	go func() {
		err := restartTunnel(nil, name, "health check failed: "+reason)
		healthMu.Lock()
		if h := health[name]; h != nil {
			h.restarting = false
		}
		healthMu.Unlock()
		if err != nil {
			log.Printf("[%s] Unable to restart tunnel: %v", name, err)
		}
	}()
}

var (
	modiphlpapi           = windows.NewLazySystemDLL("iphlpapi.dll")
	procIcmpCreateFile    = modiphlpapi.NewProc("IcmpCreateFile")
	procIcmpCloseHandle   = modiphlpapi.NewProc("IcmpCloseHandle")
	procIcmpSendEcho2Ex   = modiphlpapi.NewProc("IcmpSendEcho2Ex")
	procIcmp6CreateFile   = modiphlpapi.NewProc("Icmp6CreateFile")
	procIcmp6SendEcho2    = modiphlpapi.NewProc("Icmp6SendEcho2")
	icmpPayload           = []byte("BetterAmnezia health check")
	errNoTunnelAddressFor = errors.New("tunnel has no address of this family")
)

// pingThroughTunnel sends an ICMP echo request from the tunnel address.
func pingThroughTunnel(c *conf.Config, target string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", target)
	if err != nil || len(addrs) == 0 {
		return false
	}
	dst := addrs[0].Unmap()
	var src netip.Addr
	for _, a := range c.Interface.Addresses {
		if ip, ok := netip.AddrFromSlice(a.IP); ok && ip.Unmap().Is4() == dst.Is4() {
			src = ip.Unmap()
			break
		}
	}
	if !src.IsValid() {
		return false
	}
	ok, err := icmpEcho(src, dst, 3*time.Second)
	if err != nil && !errors.Is(err, errNoTunnelAddressFor) {
		return false
	}
	return ok
}

func icmpEcho(src, dst netip.Addr, timeout time.Duration) (bool, error) {
	reply := make([]byte, 256+len(icmpPayload))
	if dst.Is4() {
		h, _, err := procIcmpCreateFile.Call()
		if windows.Handle(h) == windows.InvalidHandle {
			return false, err
		}
		defer procIcmpCloseHandle.Call(h)
		s4, d4 := src.As4(), dst.As4()
		n, _, _ := procIcmpSendEcho2Ex.Call(h, 0, 0, 0,
			uintptr(*(*uint32)(unsafe.Pointer(&s4[0]))),
			uintptr(*(*uint32)(unsafe.Pointer(&d4[0]))),
			uintptr(unsafe.Pointer(&icmpPayload[0])), uintptr(len(icmpPayload)), 0,
			uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), uintptr(timeout.Milliseconds()))
		// ICMP_ECHO_REPLY.Status at offset 4 must be IP_SUCCESS.
		return n > 0 && *(*uint32)(unsafe.Pointer(&reply[4])) == 0, nil
	}
	h, _, err := procIcmp6CreateFile.Call()
	if windows.Handle(h) == windows.InvalidHandle {
		return false, err
	}
	defer procIcmpCloseHandle.Call(h)
	var srcSa, dstSa windows.RawSockaddrInet6
	srcSa.Family, dstSa.Family = windows.AF_INET6, windows.AF_INET6
	srcSa.Addr, dstSa.Addr = src.As16(), dst.As16()
	n, _, _ := procIcmp6SendEcho2.Call(h, 0, 0, 0,
		uintptr(unsafe.Pointer(&srcSa)), uintptr(unsafe.Pointer(&dstSa)),
		uintptr(unsafe.Pointer(&icmpPayload[0])), uintptr(len(icmpPayload)), 0,
		uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), uintptr(timeout.Milliseconds()))
	// ICMPV6_ECHO_REPLY: IPV6_ADDRESS_EX (28 bytes) then Status.
	return n > 0 && *(*uint32)(unsafe.Pointer(&reply[28])) == 0, nil
}

// dynamicDNSMonitor re-resolves endpoints given as host names and updates
// running tunnels when the address changed.
func dynamicDNSMonitor() {
	for {
		s := currentSettings().DynamicDNS
		time.Sleep(time.Duration(s.IntervalSeconds) * time.Second)
		s = currentSettings().DynamicDNS
		if !s.Enabled {
			continue
		}
		for _, name := range startedTunnelsOnly() {
			refreshEndpoints(name, s.PreferIPv6)
		}
	}
}

func refreshEndpoints(name string, preferV6 bool) {
	stored, err := conf.LoadFromName(name)
	if err != nil {
		return
	}
	var running *conf.Config
	for _, peer := range stored.Peers {
		host := peer.Endpoint.Host
		if host == "" {
			continue
		}
		if _, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
			continue
		}
		if running == nil {
			running, err = runtimeConfig(name)
			if err != nil {
				return
			}
		}
		var current string
		for _, rp := range running.Peers {
			if rp.PublicKey == peer.PublicKey {
				current = rp.Endpoint.Host
				break
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		cancel()
		if err != nil || len(addrs) == 0 {
			continue
		}
		known := false
		for _, a := range addrs {
			if a.Unmap().String() == strings.Trim(current, "[]") {
				known = true
				break
			}
		}
		if known {
			continue
		}
		pick := addrs[0].Unmap()
		for _, a := range addrs {
			if a.Unmap().Is6() == preferV6 {
				pick = a.Unmap()
				break
			}
		}
		endpoint := netip.AddrPortFrom(pick, peer.Endpoint.Port).String()
		log.Printf("[%s] Endpoint %s moved from %s to %s", name, host, current, endpoint)
		if err := uapiSet(name, fmt.Sprintf("public_key=%s\nendpoint=%s\n", peer.PublicKey.HexString(), endpoint)); err != nil {
			log.Printf("[%s] Unable to update endpoint: %v", name, err)
			continue
		}
		IPCServerNotifyExt("endpoint", map[string]string{"tunnel": name, "host": host, "endpoint": endpoint})
	}
}
