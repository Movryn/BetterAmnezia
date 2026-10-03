/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package splittunnel

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestParsePrefix(t *testing.T) {
	cases := map[string]string{
		"1.2.3.4":            "1.2.3.4/32",
		" 10.1.2.3/8 ":       "10.0.0.0/8",
		"2001:db8::1":        "2001:db8::1/128",
		"[2001:db8::1]":      "2001:db8::1/128",
		"2001:db8::/32":      "2001:db8::/32",
		"::ffff:1.2.3.4":     "1.2.3.4/32",
		"::ffff:1.2.3.0/120": "1.2.3.0/24",
	}
	for in, want := range cases {
		p, err := ParsePrefix(in)
		if err != nil {
			t.Fatalf("ParsePrefix(%q): %v", in, err)
		}
		if p.String() != want {
			t.Errorf("ParsePrefix(%q) = %s, want %s", in, p, want)
		}
	}
	for _, bad := range []string{"", "1.2.3", "10.0.0.0/33", "example.com", "::ffff:1.2.3.0/90"} {
		if _, err := ParsePrefix(bad); err == nil {
			t.Errorf("ParsePrefix(%q) succeeded", bad)
		}
	}
}

func TestMergePrefixes(t *testing.T) {
	in := []netip.Prefix{
		netip.MustParsePrefix("10.1.0.0/16"),
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("192.168.1.10/32"),
		netip.MustParsePrefix("192.168.1.10/32"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("2001:db8:1::/48"),
		netip.MustParsePrefix("1.1.1.1/32"),
	}
	got := MergePrefixes(in)
	want := []netip.Prefix{
		netip.MustParsePrefix("1.1.1.1/32"),
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("192.168.1.10/32"),
		netip.MustParsePrefix("2001:db8::/32"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MergePrefixes = %v, want %v", got, want)
	}
}

func TestDomainPatterns(t *testing.T) {
	type tc struct {
		pattern string
		name    string
		want    bool
	}
	cases := []tc{
		{"example.com", "example.com", true},
		{"example.com", "www.example.com.", true},
		{"example.com", "a.b.example.com", true},
		{"example.com", "notexample.com", false},
		{"example.com", "example.com.evil.org", false},
		{"=example.com", "example.com", true},
		{"=example.com", "www.example.com", false},
		{"*.example.com", "example.com", true},
		{"*.example.com", "cdn.example.com", true},
		{"*.example.com", "badexample.com", false},
		{"*domain.com", "domain.com", true},
		{"*domain.com", "mydomain.com", true},
		{"*domain.com", "a.b.domain.com", true},
		{"*domain.com", "domain.com.org", false},
		{"api.*.example.com", "api.eu.example.com", true},
		{"api.*.example.com", "api.example.com", false},
		{"*.googlevideo.com", "rr3---sn-abc.googlevideo.com", true},
		{"Example.COM", "WWW.example.com", true},
		{"*cdn*", "static.cdn.example.net", true},
	}
	for _, c := range cases {
		p, err := ParseDomainPattern(c.pattern)
		if err != nil {
			t.Fatalf("ParseDomainPattern(%q): %v", c.pattern, err)
		}
		if got := p.Match(c.name); got != c.want {
			t.Errorf("%q.Match(%q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
	for _, bad := range []string{"", "*", "exa mple.com", "a..b", "=", "foo/bar"} {
		if _, err := ParseDomainPattern(bad); err == nil {
			t.Errorf("ParseDomainPattern(%q) succeeded", bad)
		}
	}
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		p, s string
		want bool
	}{
		{"*", "", true},
		{"a*", "a", true},
		{"a*b", "axxxb", true},
		{"a*b", "axxxbc", false},
		{"*a*b*", "xxaxxbxx", true},
		{"**", "abc", true},
		{"a*b*c", "abbbc", true},
		{"a*b*c", "acb", false},
	}
	for _, c := range cases {
		if got := globMatch(c.p, c.s); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.p, c.s, got, c.want)
		}
	}
}

func TestConfigParseValidate(t *testing.T) {
	c, err := Parse([]byte(`{
		"mode": "exclude",
		"ips": ["1.1.1.1", "10.0.0.0/8"],
		"domains": ["*.youtube.com", "*domain.com"],
		"apps": [{"path": "C:\\Games\\game.exe", "action": "bypass"}, {"path": "D:\\Apps", "folder": true, "action": "vpnonly", "enabled": false}],
		"allowLan": true,
		"dns": {"upstreams": ["1.1.1.1", "tls://dns.google#8.8.8.8", "https://cloudflare-dns.com/dns-query#1.1.1.1"]},
		"proxy": {"socks5": true}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Active() || !c.RoutingActive() || !c.DNSForwarderNeeded() {
		t.Fatal("expected config to be active")
	}
	if n := len(c.EnabledApps()); n != 1 {
		t.Fatalf("EnabledApps = %d, want 1", n)
	}
	ps := c.Prefixes()
	if !ContainsAddr(ps, netip.MustParseAddr("192.168.1.1")) || !ContainsAddr(ps, netip.MustParseAddr("1.1.1.1")) {
		t.Fatalf("Prefixes missing entries: %v", ps)
	}
	if got := c.Proxy.SOCKSAddr().String(); got != "127.0.0.1:1080" {
		t.Fatalf("SOCKSAddr = %s", got)
	}
	if _, ok := c.Matcher().Match("www.youtube.com"); !ok {
		t.Fatal("matcher did not match")
	}

	_, err = Parse([]byte(`{"mode":"sideways","ips":["nope"],"apps":[{"path":"relative.exe","action":"bypass"}],"dns":{"upstreams":["dns.google"]}}`))
	if err == nil {
		t.Fatal("expected validation error")
	}
	for _, want := range []string{"unknown mode", "invalid IP", "not absolute", "must be an IP"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}

	empty, err := Parse(nil)
	if err != nil || empty.Mode != ModeOff || empty.Active() {
		t.Fatalf("Parse(nil) = %+v, %v", empty, err)
	}
}

func TestParseUpstream(t *testing.T) {
	cases := []struct {
		in   string
		kind UpstreamKind
		host string
		port uint16
		boot []string
	}{
		{"1.1.1.1", UpstreamPlain, "1.1.1.1", 53, []string{"1.1.1.1"}},
		{"1.1.1.1:5353", UpstreamPlain, "1.1.1.1", 5353, []string{"1.1.1.1"}},
		{"[2606:4700::1111]:53", UpstreamPlain, "2606:4700::1111", 53, []string{"2606:4700::1111"}},
		{"2606:4700::1111", UpstreamPlain, "2606:4700::1111", 53, []string{"2606:4700::1111"}},
		{"tls://1.1.1.1", UpstreamTLS, "1.1.1.1", 853, []string{"1.1.1.1"}},
		{"tls://dns.google#8.8.8.8,8.8.4.4", UpstreamTLS, "dns.google", 853, []string{"8.8.8.8", "8.8.4.4"}},
		{"tls://dns.google:8853", UpstreamTLS, "dns.google", 8853, nil},
		{"https://cloudflare-dns.com/dns-query#1.1.1.1", UpstreamHTTPS, "cloudflare-dns.com", 443, []string{"1.1.1.1"}},
		{"https://1.1.1.1/dns-query", UpstreamHTTPS, "1.1.1.1", 443, []string{"1.1.1.1"}},
	}
	for _, c := range cases {
		u, err := ParseUpstream(c.in)
		if err != nil {
			t.Fatalf("ParseUpstream(%q): %v", c.in, err)
		}
		var boot []string
		for _, b := range u.Bootstrap {
			boot = append(boot, b.String())
		}
		if u.Kind != c.kind || u.Host != c.host || u.Port != c.port || !reflect.DeepEqual(boot, c.boot) {
			t.Errorf("ParseUpstream(%q) = %+v", c.in, u)
		}
	}
	for _, bad := range []string{"", "dns.google", "tls://", "https://", "1.1.1.1#nope"} {
		if _, err := ParseUpstream(bad); err == nil {
			t.Errorf("ParseUpstream(%q) succeeded", bad)
		}
	}
}

func buildQuery(t *testing.T, name string, qtype dnsmessage.Type) []byte {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: 0x1234, RecursionDesired: true})
	b.StartQuestions()
	b.Question(dnsmessage.Question{Name: dnsmessage.MustNewName(name), Type: qtype, Class: dnsmessage.ClassINET})
	q, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// fakeUpstream answers every query with a CNAME chain and A/AAAA records.
type fakeUpstream struct {
	mu      sync.Mutex
	queries []string
}

func (f *fakeUpstream) String() string { return "fake" }

func (f *fakeUpstream) Exchange(ctx context.Context, query []byte) ([]byte, error) {
	var p dnsmessage.Parser
	h, err := p.Start(query)
	if err != nil {
		return nil, err
	}
	q, err := p.Question()
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.queries = append(f.queries, q.Name.String())
	f.mu.Unlock()
	h.Response = true
	b := dnsmessage.NewBuilder(nil, h)
	b.StartQuestions()
	b.Question(q)
	b.StartAnswers()
	target := dnsmessage.MustNewName("edge.cdn-provider.net.")
	b.CNAMEResource(dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 300}, dnsmessage.CNAMEResource{CNAME: target})
	switch q.Type {
	case dnsmessage.TypeA:
		b.AResource(dnsmessage.ResourceHeader{Name: target, Class: dnsmessage.ClassINET, TTL: 60}, dnsmessage.AResource{A: [4]byte{93, 184, 216, 34}})
		b.AResource(dnsmessage.ResourceHeader{Name: target, Class: dnsmessage.ClassINET, TTL: 120}, dnsmessage.AResource{A: [4]byte{93, 184, 216, 35}})
	case dnsmessage.TypeAAAA:
		b.AAAAResource(dnsmessage.ResourceHeader{Name: target, Class: dnsmessage.ClassINET, TTL: 60}, dnsmessage.AAAAResource{AAAA: netip.MustParseAddr("2001:db8::1").As16()})
	}
	return b.Finish()
}

type fakeSink struct {
	mu      sync.Mutex
	added   []netip.Addr
	removed []netip.Addr
}

func (s *fakeSink) AddHostRoutes(a []netip.Addr) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.added = append(s.added, a...)
	return nil
}

func (s *fakeSink) RemoveHostRoutes(a []netip.Addr) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removed = append(s.removed, a...)
	return nil
}

func TestForwarderMatching(t *testing.T) {
	up := &fakeUpstream{}
	bypass := &fakeUpstream{}
	sink := &fakeSink{}
	learned := &LearnedRoutes{Sink: sink}
	f := &Forwarder{
		Upstreams:        []Exchanger{up},
		MatchedUpstreams: []Exchanger{bypass},
		Matcher:          NewDomainMatcher([]string{"*example.com", "cdn-provider.net"}),
		OnMatch: func(a *Answer) {
			learned.Learn(a.Addrs)
		},
	}
	resp := f.Handle(context.Background(), buildQuery(t, "www.example.com.", dnsmessage.TypeA))
	a, err := ParseAnswer(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Addrs) != 2 || a.TTL != 60 || len(a.Names) != 2 || a.Names[1] != "edge.cdn-provider.net" {
		t.Fatalf("unexpected answer %+v", a)
	}
	if len(bypass.queries) != 1 || len(up.queries) != 0 {
		t.Fatalf("matched query used wrong upstream: up=%v bypass=%v", up.queries, bypass.queries)
	}
	if len(sink.added) != 2 {
		t.Fatalf("expected 2 learned routes, got %v", sink.added)
	}

	// A name that does not match directly but whose CNAME target does.
	f.Handle(context.Background(), buildQuery(t, "other.org.", dnsmessage.TypeA))
	if len(up.queries) != 1 {
		t.Fatalf("unmatched query should use main upstream")
	}
	if len(sink.added) != 2 {
		t.Fatalf("known addresses must not be re-added, got %v", sink.added)
	}

	// AAAA is dropped for matched names when requested.
	f.DropAAAAForMatched = true
	resp = f.Handle(context.Background(), buildQuery(t, "example.com.", dnsmessage.TypeAAAA))
	a, err = ParseAnswer(resp)
	if err != nil || len(a.Addrs) != 0 {
		t.Fatalf("expected empty AAAA answer, got %+v %v", a, err)
	}
	if resp[2]&0x80 == 0 || resp[0] != 0x12 || resp[1] != 0x34 {
		t.Fatal("empty response must be a response with the original ID")
	}

	// Upstream failure yields SERVFAIL.
	f.Upstreams = []Exchanger{failingExchanger{}}
	resp = f.Handle(context.Background(), buildQuery(t, "fail.test.", dnsmessage.TypeA))
	if len(resp) < 12 || resp[3]&0x0f != 2 {
		t.Fatalf("expected SERVFAIL, got %x", resp)
	}
}

type failingExchanger struct{}

func (failingExchanger) String() string { return "fail" }
func (failingExchanger) Exchange(context.Context, []byte) ([]byte, error) {
	return nil, fmt.Errorf("boom")
}

func TestLearnedRoutesLimit(t *testing.T) {
	sink := &fakeSink{}
	l := &LearnedRoutes{Sink: sink, Limit: 3, Filter: func(a netip.Addr) bool { return a.Is4() }}
	l.Learn([]netip.Addr{netip.MustParseAddr("1.0.0.1"), netip.MustParseAddr("1.0.0.2"), netip.MustParseAddr("::1"), netip.MustParseAddr("2001:db8::5")})
	l.Learn([]netip.Addr{netip.MustParseAddr("1.0.0.3")})
	l.Learn([]netip.Addr{netip.MustParseAddr("1.0.0.1")}) // refresh
	l.Learn([]netip.Addr{netip.MustParseAddr("1.0.0.4")})
	if len(sink.removed) != 1 || sink.removed[0] != netip.MustParseAddr("1.0.0.2") {
		t.Fatalf("expected 1.0.0.2 evicted, got %v", sink.removed)
	}
	if n := len(l.Snapshot()); n != 3 {
		t.Fatalf("snapshot has %d entries", n)
	}
}

func TestForwarderUDPAndTCP(t *testing.T) {
	f := &Forwarder{Upstreams: []Exchanger{&fakeUpstream{}}}
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go f.ServeUDP(pc)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go f.ServeTCP(l)

	up, _ := ParseUpstream(pc.LocalAddr().String())
	ex := NewExchanger(up, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := ex.Exchange(ctx, buildQuery(t, "udp.test.", dnsmessage.TypeA))
	if err != nil {
		t.Fatal(err)
	}
	if a, err := ParseAnswer(resp); err != nil || len(a.Addrs) != 2 {
		t.Fatalf("bad UDP answer %+v %v", a, err)
	}

	resp, err = exchangeStream(ctx, (&net.Dialer{}).DialContext, "tcp", l.Addr().String(), nil, buildQuery(t, "tcp.test.", dnsmessage.TypeA))
	if err != nil {
		t.Fatal(err)
	}
	if a, err := ParseAnswer(resp); err != nil || len(a.Addrs) != 2 || a.Question != "tcp.test" {
		t.Fatalf("bad TCP answer %+v %v", a, err)
	}
}

func TestTruncate(t *testing.T) {
	q := buildQuery(t, "big.test.", dnsmessage.TypeA)
	resp, _ := (&fakeUpstream{}).Exchange(context.Background(), q)
	tr := truncate(resp)
	if !isTruncated(tr) || binary.BigEndian.Uint16(tr[6:]) != 0 {
		t.Fatalf("truncate produced %x", tr)
	}
}

func startEchoServer(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				br := bufio.NewReader(c)
				// Behave as a tiny HTTP server if spoken to in HTTP, else echo.
				peek, _ := br.Peek(4)
				if string(peek) == "GET " {
					req, err := http.ReadRequest(br)
					if err != nil {
						return
					}
					body := "hello " + req.URL.Path
					fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
					return
				}
				io.Copy(c, br)
			}()
		}
	}()
	return l
}

func newTestProxy(t *testing.T, user, pass string) (*ProxyServer, net.Listener, net.Listener, *[]string) {
	t.Helper()
	var dialed []string
	var mu sync.Mutex
	s := &ProxyServer{
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			mu.Lock()
			dialed = append(dialed, address)
			mu.Unlock()
			return (&net.Dialer{}).DialContext(ctx, network, address)
		},
		Lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			if host == "echo.test" {
				return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
			}
			return nil, fmt.Errorf("no such host %s", host)
		},
		Username: user,
		Password: pass,
	}
	sl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go s.ServeSOCKS5(sl)
	go s.ServeHTTP(hl)
	t.Cleanup(s.Close)
	return s, sl, hl, &dialed
}

func TestSOCKS5Proxy(t *testing.T) {
	echo := startEchoServer(t)
	defer echo.Close()
	_, port, _ := net.SplitHostPort(echo.Addr().String())
	var portNum uint16
	fmt.Sscan(port, &portNum)

	for _, auth := range []bool{false, true} {
		user, pass := "", ""
		if auth {
			user, pass = "alice", "s3cret"
		}
		_, sl, _, dialed := newTestProxy(t, user, pass)
		c, err := net.Dial("tcp", sl.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		c.SetDeadline(time.Now().Add(5 * time.Second))
		if auth {
			c.Write([]byte{5, 1, 2})
		} else {
			c.Write([]byte{5, 1, 0})
		}
		var sel [2]byte
		io.ReadFull(c, sel[:])
		if auth {
			if sel != [2]byte{5, 2} {
				t.Fatalf("method selection %v", sel)
			}
			msg := []byte{1, byte(len(user))}
			msg = append(msg, user...)
			msg = append(msg, byte(len(pass)))
			msg = append(msg, pass...)
			c.Write(msg)
			var st [2]byte
			io.ReadFull(c, st[:])
			if st != [2]byte{1, 0} {
				t.Fatalf("auth status %v", st)
			}
		} else if sel != [2]byte{5, 0} {
			t.Fatalf("method selection %v", sel)
		}
		req := []byte{5, 1, 0, 3, byte(len("echo.test"))}
		req = append(req, "echo.test"...)
		req = binary.BigEndian.AppendUint16(req, portNum)
		c.Write(req)
		var rep [10]byte
		if _, err := io.ReadFull(c, rep[:]); err != nil {
			t.Fatal(err)
		}
		if rep[1] != 0 {
			t.Fatalf("SOCKS reply code %d", rep[1])
		}
		c.Write([]byte("ping"))
		var buf [4]byte
		if _, err := io.ReadFull(c, buf[:]); err != nil || string(buf[:]) != "ping" {
			t.Fatalf("echo failed: %q %v", buf, err)
		}
		c.Close()
		if len(*dialed) != 1 || (*dialed)[0] != "127.0.0.1:"+port {
			t.Fatalf("dialed %v", *dialed)
		}
	}
}

func TestSOCKS5WrongPassword(t *testing.T) {
	_, sl, _, _ := newTestProxy(t, "alice", "right")
	c, err := net.Dial("tcp", sl.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write([]byte{5, 1, 0})
	var sel [2]byte
	io.ReadFull(c, sel[:])
	if sel != [2]byte{5, 0xff} {
		t.Fatalf("server accepted unauthenticated client: %v", sel)
	}
}

func TestHTTPProxy(t *testing.T) {
	echo := startEchoServer(t)
	defer echo.Close()
	_, _, hl, dialed := newTestProxy(t, "bob", "pw")
	_, port, _ := net.SplitHostPort(echo.Addr().String())

	// Absolute-form GET without credentials is rejected.
	c, err := net.Dial("tcp", hl.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(c, "GET http://echo.test:%s/x HTTP/1.1\r\nHost: echo.test\r\n\r\n", port)
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil || resp.StatusCode != 407 {
		t.Fatalf("expected 407, got %v %v", resp, err)
	}
	c.Close()

	// With credentials.
	c, _ = net.Dial("tcp", hl.Addr().String())
	c.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(c, "GET http://echo.test:%s/path HTTP/1.1\r\nHost: echo.test\r\nProxy-Authorization: Basic Ym9iOnB3\r\n\r\n", port)
	br := bufio.NewReader(c)
	resp, err = http.ReadResponse(br, nil)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("GET via proxy: %v %v", resp, err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello /path" {
		t.Fatalf("body %q", body)
	}
	c.Close()

	// CONNECT tunnel.
	c, _ = net.Dial("tcp", hl.Addr().String())
	c.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(c, "CONNECT echo.test:%s HTTP/1.1\r\nHost: echo.test:%s\r\nProxy-Authorization: Basic Ym9iOnB3\r\n\r\n", port, port)
	br = bufio.NewReader(c)
	resp, err = http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("CONNECT: %v %v", resp, err)
	}
	c.Write([]byte("tunnel"))
	buf := make([]byte, 6)
	if _, err := io.ReadFull(br, buf); err != nil || string(buf) != "tunnel" {
		t.Fatalf("CONNECT echo %q %v", buf, err)
	}
	c.Close()
	if len(*dialed) != 2 {
		t.Fatalf("dialed %v", *dialed)
	}
}

func TestLookupHost(t *testing.T) {
	ctx := context.Background()
	addrs, err := LookupHost(ctx, []Exchanger{&fakeUpstream{}}, "www.example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("93.184.216.35"), netip.MustParseAddr("2001:db8::1")}
	if !reflect.DeepEqual(addrs, want) {
		t.Fatalf("LookupHost = %v", addrs)
	}
	addrs, err = LookupHost(ctx, nil, "10.1.2.3", false)
	if err != nil || len(addrs) != 1 || addrs[0].String() != "10.1.2.3" {
		t.Fatalf("literal lookup = %v %v", addrs, err)
	}
	if _, err := LookupHost(ctx, []Exchanger{failingExchanger{}}, "x.test", false); err == nil {
		t.Fatal("expected failure")
	}
}
