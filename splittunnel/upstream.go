/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package splittunnel

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// UpstreamKind is the transport used to reach an upstream resolver.
type UpstreamKind string

const (
	UpstreamPlain UpstreamKind = "plain"
	UpstreamTLS   UpstreamKind = "tls"
	UpstreamHTTPS UpstreamKind = "https"
)

// Upstream describes a resolver the DNS forwarder sends queries to.
type Upstream struct {
	Kind UpstreamKind
	// Host is the name or address used for TLS verification and HTTP.
	Host string
	Port uint16
	// Bootstrap holds addresses to connect to. For hosts given as names it is
	// filled from "#addr" suffixes or by Resolve.
	Bootstrap []netip.Addr
	// URL is the full DoH URL.
	URL string
}

// ParseUpstream parses "1.1.1.1", "1.1.1.1:53", "[::1]:53", "tls://host[:port][#ip,...]"
// and "https://host[:port]/path[#ip,...]".
func ParseUpstream(s string) (Upstream, error) {
	orig := s
	s = strings.TrimSpace(s)
	var bootstrap []netip.Addr
	if i := strings.LastIndexByte(s, '#'); i >= 0 {
		for _, b := range strings.Split(s[i+1:], ",") {
			a, err := netip.ParseAddr(strings.Trim(strings.TrimSpace(b), "[]"))
			if err != nil {
				return Upstream{}, fmt.Errorf("invalid bootstrap address in DNS upstream %q", orig)
			}
			bootstrap = append(bootstrap, a.Unmap())
		}
		s = s[:i]
	}
	switch {
	case strings.HasPrefix(s, "https://"):
		u, err := url.Parse(s)
		if err != nil || u.Host == "" {
			return Upstream{}, fmt.Errorf("invalid DNS-over-HTTPS upstream %q", orig)
		}
		port := uint16(443)
		if p := u.Port(); p != "" {
			n, err := strconv.ParseUint(p, 10, 16)
			if err != nil {
				return Upstream{}, fmt.Errorf("invalid port in DNS upstream %q", orig)
			}
			port = uint16(n)
		}
		up := Upstream{Kind: UpstreamHTTPS, Host: u.Hostname(), Port: port, URL: s, Bootstrap: bootstrap}
		if a, err := netip.ParseAddr(up.Host); err == nil {
			up.Bootstrap = append([]netip.Addr{a.Unmap()}, up.Bootstrap...)
		}
		return up, nil
	case strings.HasPrefix(s, "tls://"):
		host, port, err := splitHostPortDefault(strings.TrimPrefix(s, "tls://"), 853)
		if err != nil {
			return Upstream{}, fmt.Errorf("invalid DNS-over-TLS upstream %q", orig)
		}
		up := Upstream{Kind: UpstreamTLS, Host: host, Port: port, Bootstrap: bootstrap}
		if a, err := netip.ParseAddr(host); err == nil {
			up.Bootstrap = append([]netip.Addr{a.Unmap()}, up.Bootstrap...)
		}
		return up, nil
	default:
		s = strings.TrimPrefix(strings.TrimPrefix(s, "udp://"), "dns://")
		host, port, err := splitHostPortDefault(s, 53)
		if err != nil {
			return Upstream{}, fmt.Errorf("invalid DNS upstream %q", orig)
		}
		a, err := netip.ParseAddr(host)
		if err != nil {
			return Upstream{}, fmt.Errorf("plain DNS upstream %q must be an IP address", orig)
		}
		return Upstream{Kind: UpstreamPlain, Host: host, Port: port, Bootstrap: []netip.Addr{a.Unmap()}}, nil
	}
}

func splitHostPortDefault(s string, def uint16) (string, uint16, error) {
	if s == "" {
		return "", 0, errors.New("empty")
	}
	if a, err := netip.ParseAddr(strings.Trim(s, "[]")); err == nil {
		return a.String(), def, nil
	}
	host, portStr, err := net.SplitHostPort(s)
	if err != nil {
		if strings.Contains(s, ":") || strings.Contains(s, "/") {
			return "", 0, err
		}
		return s, def, nil
	}
	n, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil || n == 0 {
		return "", 0, errors.New("invalid port")
	}
	return host, uint16(n), nil
}

// Addrs returns the address/port pairs to try.
func (u *Upstream) Addrs() []netip.AddrPort {
	out := make([]netip.AddrPort, 0, len(u.Bootstrap))
	for _, a := range u.Bootstrap {
		out = append(out, netip.AddrPortFrom(a, u.Port))
	}
	return out
}

// Resolve fills Bootstrap for upstreams that were given by name, using the
// supplied lookup function. It must be called before the forwarder starts
// answering, so that resolution never loops through ourselves.
func (u *Upstream) Resolve(ctx context.Context, lookup func(ctx context.Context, host string) ([]netip.Addr, error)) error {
	if len(u.Bootstrap) > 0 {
		return nil
	}
	addrs, err := lookup(ctx, u.Host)
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		return fmt.Errorf("no addresses for DNS upstream %s", u.Host)
	}
	for _, a := range addrs {
		u.Bootstrap = append(u.Bootstrap, a.Unmap())
	}
	return nil
}

func (u Upstream) String() string {
	switch u.Kind {
	case UpstreamHTTPS:
		return u.URL
	case UpstreamTLS:
		return "tls://" + net.JoinHostPort(u.Host, strconv.Itoa(int(u.Port)))
	}
	return net.JoinHostPort(u.Host, strconv.Itoa(int(u.Port)))
}

// DialFunc dials a connection; implementations can pin the source address or
// interface.
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// Exchanger sends a raw DNS query and returns the raw response.
type Exchanger interface {
	Exchange(ctx context.Context, query []byte) ([]byte, error)
	String() string
}

// NewExchanger builds an exchanger for an upstream.
func NewExchanger(u Upstream, dial DialFunc) Exchanger {
	if dial == nil {
		d := &net.Dialer{}
		dial = d.DialContext
	}
	switch u.Kind {
	case UpstreamTLS:
		return &tlsExchanger{up: u, dial: dial}
	case UpstreamHTTPS:
		return newHTTPSExchanger(u, dial)
	}
	return &plainExchanger{up: u, dial: dial}
}

const maxDNSMessage = 65535

type plainExchanger struct {
	up   Upstream
	dial DialFunc
}

func (e *plainExchanger) String() string { return e.up.String() }

func (e *plainExchanger) Exchange(ctx context.Context, query []byte) ([]byte, error) {
	var lastErr error = errors.New("no upstream address")
	for _, ap := range e.up.Addrs() {
		resp, err := e.exchangeUDP(ctx, ap, query)
		if err == nil && isTruncated(resp) {
			resp, err = exchangeStream(ctx, e.dial, "tcp", ap.String(), nil, query)
		}
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, lastErr
}

func (e *plainExchanger) exchangeUDP(ctx context.Context, ap netip.AddrPort, query []byte) ([]byte, error) {
	conn, err := e.dial(ctx, "udp", ap.String())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(5 * time.Second)
	}
	conn.SetDeadline(deadline)
	if _, err := conn.Write(query); err != nil {
		return nil, err
	}
	buf := make([]byte, maxDNSMessage)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return nil, err
		}
		// Ignore stray datagrams that do not answer our query.
		if n >= 2 && len(query) >= 2 && buf[0] == query[0] && buf[1] == query[1] {
			return append([]byte(nil), buf[:n]...), nil
		}
	}
}

func isTruncated(msg []byte) bool {
	return len(msg) >= 3 && msg[2]&0x02 != 0
}

// exchangeStream performs a length-prefixed exchange over TCP or TLS.
func exchangeStream(ctx context.Context, dial DialFunc, network, addr string, tlsConf *tls.Config, query []byte) ([]byte, error) {
	conn, err := dial(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(5 * time.Second)
	}
	conn.SetDeadline(deadline)
	if tlsConf != nil {
		tc := tls.Client(conn, tlsConf)
		if err := tc.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, err
		}
		conn = tc
	}
	defer conn.Close()
	return streamRoundTrip(conn, query)
}

func streamRoundTrip(conn io.ReadWriter, query []byte) ([]byte, error) {
	if len(query) > maxDNSMessage {
		return nil, errors.New("query too large")
	}
	out := make([]byte, 2+len(query))
	binary.BigEndian.PutUint16(out, uint16(len(query)))
	copy(out[2:], query)
	if _, err := conn.Write(out); err != nil {
		return nil, err
	}
	var lenBuf [2]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return nil, err
	}
	resp := make([]byte, binary.BigEndian.Uint16(lenBuf[:]))
	if _, err := io.ReadFull(conn, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

type tlsExchanger struct {
	up   Upstream
	dial DialFunc
}

func (e *tlsExchanger) String() string { return e.up.String() }

func (e *tlsExchanger) Exchange(ctx context.Context, query []byte) ([]byte, error) {
	var lastErr error = errors.New("no upstream address")
	conf := &tls.Config{ServerName: e.up.Host, MinVersion: tls.VersionTLS12}
	for _, ap := range e.up.Addrs() {
		resp, err := exchangeStream(ctx, e.dial, "tcp", ap.String(), conf, query)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, lastErr
}

type httpsExchanger struct {
	up     Upstream
	client *http.Client
}

func newHTTPSExchanger(u Upstream, dial DialFunc) *httpsExchanger {
	e := &httpsExchanger{up: u}
	var idx int
	var mu sync.Mutex
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			addrs := e.up.Addrs()
			if len(addrs) == 0 {
				return nil, errors.New("no upstream address")
			}
			mu.Lock()
			start := idx
			mu.Unlock()
			var lastErr error
			for i := range addrs {
				ap := addrs[(start+i)%len(addrs)]
				c, err := dial(ctx, "tcp", ap.String())
				if err == nil {
					return c, nil
				}
				lastErr = err
				mu.Lock()
				idx = (start + i + 1) % len(addrs)
				mu.Unlock()
			}
			return nil, lastErr
		},
		TLSClientConfig:     &tls.Config{ServerName: u.Host, MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        4,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	}
	e.client = &http.Client{Transport: transport, Timeout: 8 * time.Second}
	return e
}

func (e *httpsExchanger) String() string { return e.up.String() }

func (e *httpsExchanger) Exchange(ctx context.Context, query []byte) ([]byte, error) {
	// RFC 8484 recommends a zero ID for cacheability; restore it afterwards.
	q := append([]byte(nil), query...)
	var id [2]byte
	if len(q) >= 2 {
		copy(id[:], q[:2])
		q[0], q[1] = 0, 0
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.up.URL, bytes.NewReader(q))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DoH server returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDNSMessage))
	if err != nil {
		return nil, err
	}
	if len(body) >= 2 {
		body[0], body[1] = id[0], id[1]
	}
	return body, nil
}
