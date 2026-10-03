/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package splittunnel

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProxyServer exposes a dialer (normally one bound to the tunnel) as local
// SOCKS5 and HTTP proxies, so that individual applications can be pointed at
// the tunnel without routing the whole system through it.
type ProxyServer struct {
	// Dial opens outbound connections; it must route through the tunnel.
	Dial DialFunc
	// Lookup resolves host names; it must resolve through the tunnel to
	// avoid DNS leaks.
	Lookup   func(ctx context.Context, host string) ([]netip.Addr, error)
	Username string
	Password string
	Logf     func(format string, args ...any)

	mu        sync.Mutex
	listeners []net.Listener
	conns     map[net.Conn]struct{}
	closed    bool
}

func (s *ProxyServer) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

func (s *ProxyServer) track(c net.Conn, add bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if add {
		if s.closed {
			return false
		}
		if s.conns == nil {
			s.conns = make(map[net.Conn]struct{})
		}
		s.conns[c] = struct{}{}
	} else {
		delete(s.conns, c)
	}
	return true
}

// ServeSOCKS5 accepts SOCKS5 clients on l until it is closed.
func (s *ProxyServer) ServeSOCKS5(l net.Listener) error {
	return s.serve(l, s.handleSOCKS5)
}

// ServeHTTP accepts HTTP proxy clients (CONNECT and absolute-form requests).
func (s *ProxyServer) ServeHTTP(l net.Listener) error {
	return s.serve(l, s.handleHTTP)
}

func (s *ProxyServer) serve(l net.Listener, handle func(net.Conn)) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		l.Close()
		return net.ErrClosed
	}
	s.listeners = append(s.listeners, l)
	s.mu.Unlock()
	for {
		c, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if !s.track(c, true) {
			c.Close()
			return nil
		}
		go func() {
			defer func() {
				s.track(c, false)
				c.Close()
			}()
			handle(c)
		}()
	}
}

// Close stops all listeners and open connections.
func (s *ProxyServer) Close() {
	s.mu.Lock()
	s.closed = true
	ls := s.listeners
	s.listeners = nil
	conns := s.conns
	s.conns = nil
	s.mu.Unlock()
	for _, l := range ls {
		l.Close()
	}
	for c := range conns {
		c.Close()
	}
}

func (s *ProxyServer) authRequired() bool { return s.Username != "" || s.Password != "" }

func (s *ProxyServer) checkAuth(user, pass string) bool {
	u := subtle.ConstantTimeCompare([]byte(user), []byte(s.Username))
	p := subtle.ConstantTimeCompare([]byte(pass), []byte(s.Password))
	return u&p == 1
}

// dialTarget resolves host through Lookup and connects to the first address
// that works.
func (s *ProxyServer) dialTarget(ctx context.Context, host string, port uint16) (net.Conn, error) {
	if a, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return s.Dial(ctx, "tcp", netip.AddrPortFrom(a.Unmap(), port).String())
	}
	if s.Lookup == nil {
		return s.Dial(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(int(port))))
	}
	addrs, err := s.Lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no addresses for %s", host)
	}
	var lastErr error
	for _, a := range addrs {
		c, err := s.Dial(ctx, "tcp", netip.AddrPortFrom(a.Unmap(), port).String())
		if err == nil {
			return c, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			cw.CloseWrite()
		} else {
			dst.Close()
		}
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	<-done
}

const (
	socksVersion5     = 5
	socksAuthNone     = 0
	socksAuthPassword = 2
	socksAuthNoAccept = 0xff
	socksCmdConnect   = 1
	socksAtypIPv4     = 1
	socksAtypDomain   = 3
	socksAtypIPv6     = 4

	socksRepSuccess          = 0
	socksRepGeneralFailure   = 1
	socksRepNetUnreachable   = 3
	socksRepHostUnreachable  = 4
	socksRepConnRefused      = 5
	socksRepCmdNotSupported  = 7
	socksRepAtypNotSupported = 8
)

func (s *ProxyServer) handleSOCKS5(c net.Conn) {
	c.SetDeadline(time.Now().Add(30 * time.Second))
	br := bufio.NewReader(c)
	var hdr [2]byte
	if _, err := io.ReadFull(br, hdr[:]); err != nil || hdr[0] != socksVersion5 {
		return
	}
	methods := make([]byte, hdr[1])
	if _, err := io.ReadFull(br, methods); err != nil {
		return
	}
	want := byte(socksAuthNone)
	if s.authRequired() {
		want = socksAuthPassword
	}
	found := false
	for _, m := range methods {
		if m == want {
			found = true
			break
		}
	}
	if !found {
		c.Write([]byte{socksVersion5, socksAuthNoAccept})
		return
	}
	if _, err := c.Write([]byte{socksVersion5, want}); err != nil {
		return
	}
	if want == socksAuthPassword {
		// RFC 1929
		var ver [1]byte
		if _, err := io.ReadFull(br, ver[:]); err != nil || ver[0] != 1 {
			return
		}
		user, err := readLenPrefixed(br)
		if err != nil {
			return
		}
		pass, err := readLenPrefixed(br)
		if err != nil {
			return
		}
		if !s.checkAuth(user, pass) {
			c.Write([]byte{1, 1})
			return
		}
		if _, err := c.Write([]byte{1, 0}); err != nil {
			return
		}
	}
	var req [4]byte
	if _, err := io.ReadFull(br, req[:]); err != nil || req[0] != socksVersion5 {
		return
	}
	var host string
	switch req[3] {
	case socksAtypIPv4:
		var a [4]byte
		if _, err := io.ReadFull(br, a[:]); err != nil {
			return
		}
		host = netip.AddrFrom4(a).String()
	case socksAtypIPv6:
		var a [16]byte
		if _, err := io.ReadFull(br, a[:]); err != nil {
			return
		}
		host = netip.AddrFrom16(a).String()
	case socksAtypDomain:
		h, err := readLenPrefixed(br)
		if err != nil {
			return
		}
		host = h
	default:
		socksReply(c, socksRepAtypNotSupported, nil)
		return
	}
	var portBuf [2]byte
	if _, err := io.ReadFull(br, portBuf[:]); err != nil {
		return
	}
	port := binary.BigEndian.Uint16(portBuf[:])
	if req[1] != socksCmdConnect {
		socksReply(c, socksRepCmdNotSupported, nil)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	remote, err := s.dialTarget(ctx, host, port)
	cancel()
	if err != nil {
		s.logf("Proxy: unable to connect to %s:%d: %v", host, port, err)
		socksReply(c, socksErrorCode(err), nil)
		return
	}
	defer remote.Close()
	if err := socksReply(c, socksRepSuccess, remote.LocalAddr()); err != nil {
		return
	}
	c.SetDeadline(time.Time{})
	if n := br.Buffered(); n > 0 {
		buffered, _ := br.Peek(n)
		if _, err := remote.Write(buffered); err != nil {
			return
		}
	}
	pipe(c, remote)
}

func socksErrorCode(err error) byte {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "refused"):
		return socksRepConnRefused
	case strings.Contains(msg, "unreachable") && strings.Contains(msg, "network"):
		return socksRepNetUnreachable
	case strings.Contains(msg, "no addresses"), strings.Contains(msg, "no such host"), strings.Contains(msg, "unreachable"), strings.Contains(msg, "timeout"):
		return socksRepHostUnreachable
	}
	return socksRepGeneralFailure
}

func readLenPrefixed(r io.Reader) (string, error) {
	var l [1]byte
	if _, err := io.ReadFull(r, l[:]); err != nil {
		return "", err
	}
	b := make([]byte, l[0])
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return string(b), nil
}

func socksReply(c net.Conn, code byte, bound net.Addr) error {
	reply := []byte{socksVersion5, code, 0, socksAtypIPv4, 0, 0, 0, 0, 0, 0}
	if ta, ok := bound.(*net.TCPAddr); ok && ta != nil {
		if ip4 := ta.IP.To4(); ip4 != nil {
			copy(reply[4:8], ip4)
			binary.BigEndian.PutUint16(reply[8:], uint16(ta.Port))
		} else if ip16 := ta.IP.To16(); ip16 != nil {
			reply = append([]byte{socksVersion5, code, 0, socksAtypIPv6}, ip16...)
			reply = binary.BigEndian.AppendUint16(reply, uint16(ta.Port))
		}
	}
	_, err := c.Write(reply)
	return err
}

func (s *ProxyServer) handleHTTP(c net.Conn) {
	br := bufio.NewReader(c)
	for {
		c.SetReadDeadline(time.Now().Add(60 * time.Second))
		req, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		if s.authRequired() && !s.httpAuthOK(req) {
			io.WriteString(c, "HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic realm=\"BetterAmnezia\"\r\nContent-Length: 0\r\n\r\n")
			if req.Close {
				return
			}
			continue
		}
		if req.Method == http.MethodConnect {
			s.httpConnect(c, br, req)
			return
		}
		if !s.httpForward(c, req) {
			return
		}
	}
}

func (s *ProxyServer) httpAuthOK(req *http.Request) bool {
	h := req.Header.Get("Proxy-Authorization")
	const prefix = "Basic "
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(h[len(prefix):])
	if err != nil {
		return false
	}
	user, pass, ok := strings.Cut(string(raw), ":")
	return ok && s.checkAuth(user, pass)
}

func splitTarget(hostport string, defPort uint16) (string, uint16, error) {
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return strings.Trim(hostport, "[]"), defPort, nil
	}
	n, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return "", 0, err
	}
	return host, uint16(n), nil
}

func (s *ProxyServer) httpConnect(c net.Conn, br *bufio.Reader, req *http.Request) {
	host, port, err := splitTarget(req.Host, 443)
	if err != nil {
		io.WriteString(c, "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	remote, err := s.dialTarget(ctx, host, port)
	cancel()
	if err != nil {
		s.logf("Proxy: unable to connect to %s: %v", req.Host, err)
		io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer remote.Close()
	if _, err := io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	c.SetDeadline(time.Time{})
	if n := br.Buffered(); n > 0 {
		buffered, _ := br.Peek(n)
		if _, err := remote.Write(buffered); err != nil {
			return
		}
	}
	pipe(c, remote)
}

var hopHeaders = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
	"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

// httpForward relays a plain HTTP request. It returns whether the client
// connection can be reused.
func (s *ProxyServer) httpForward(c net.Conn, req *http.Request) bool {
	if req.URL == nil || req.URL.Scheme != "http" || req.URL.Host == "" {
		io.WriteString(c, "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		return false
	}
	host, port, err := splitTarget(req.URL.Host, 80)
	if err != nil {
		io.WriteString(c, "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	remote, err := s.dialTarget(ctx, host, port)
	cancel()
	if err != nil {
		s.logf("Proxy: unable to connect to %s: %v", req.URL.Host, err)
		io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		return false
	}
	defer remote.Close()
	for _, h := range hopHeaders {
		req.Header.Del(h)
	}
	req.RequestURI = ""
	req.Close = true
	req.Header.Set("Connection", "close")
	remote.SetDeadline(time.Now().Add(10 * time.Minute))
	if err := req.Write(remote); err != nil {
		return false
	}
	resp, err := http.ReadResponse(bufio.NewReader(remote), req)
	if err != nil {
		io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		return false
	}
	defer resp.Body.Close()
	for _, h := range hopHeaders {
		resp.Header.Del(h)
	}
	resp.Close = true
	c.SetWriteDeadline(time.Time{})
	resp.Write(c)
	return false
}
