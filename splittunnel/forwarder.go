/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package splittunnel

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"
	"net/netip"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Answer is the routing relevant content of a DNS response.
type Answer struct {
	Question string
	QType    dnsmessage.Type
	// Names holds the question name and every CNAME target in the answer.
	Names []string
	Addrs []netip.Addr
	TTL   uint32
}

// ParseAnswer extracts names and addresses from a DNS response.
func ParseAnswer(msg []byte) (*Answer, error) {
	var p dnsmessage.Parser
	if _, err := p.Start(msg); err != nil {
		return nil, err
	}
	q, err := p.Question()
	if err != nil {
		return nil, err
	}
	a := &Answer{Question: NormalizeName(q.Name.String()), QType: q.Type, TTL: ^uint32(0)}
	a.Names = append(a.Names, a.Question)
	if err := p.SkipAllQuestions(); err != nil {
		return nil, err
	}
	for {
		h, err := p.AnswerHeader()
		if err == dnsmessage.ErrSectionDone {
			break
		}
		if err != nil {
			return a, nil
		}
		switch h.Type {
		case dnsmessage.TypeA:
			r, err := p.AResource()
			if err != nil {
				return a, nil
			}
			a.Addrs = append(a.Addrs, netip.AddrFrom4(r.A))
			a.TTL = min(a.TTL, h.TTL)
		case dnsmessage.TypeAAAA:
			r, err := p.AAAAResource()
			if err != nil {
				return a, nil
			}
			a.Addrs = append(a.Addrs, netip.AddrFrom16(r.AAAA).Unmap())
			a.TTL = min(a.TTL, h.TTL)
		case dnsmessage.TypeCNAME:
			r, err := p.CNAMEResource()
			if err != nil {
				return a, nil
			}
			a.Names = append(a.Names, NormalizeName(r.CNAME.String()))
		default:
			if err := p.SkipAnswer(); err != nil {
				return a, nil
			}
		}
	}
	if len(a.Addrs) == 0 {
		a.TTL = 0
	}
	return a, nil
}

// questionName returns the first question of a query, or "" if malformed.
func questionName(msg []byte) (string, dnsmessage.Type) {
	var p dnsmessage.Parser
	if _, err := p.Start(msg); err != nil {
		return "", 0
	}
	q, err := p.Question()
	if err != nil {
		return "", 0
	}
	return NormalizeName(q.Name.String()), q.Type
}

// emptyResponse builds a NOERROR response without answers for query.
func emptyResponse(query []byte) ([]byte, error) {
	var p dnsmessage.Parser
	h, err := p.Start(query)
	if err != nil {
		return nil, err
	}
	q, err := p.Question()
	if err != nil {
		return nil, err
	}
	h.Response = true
	h.RecursionAvailable = true
	h.RCode = dnsmessage.RCodeSuccess
	b := dnsmessage.NewBuilder(nil, h)
	if err := b.StartQuestions(); err != nil {
		return nil, err
	}
	if err := b.Question(q); err != nil {
		return nil, err
	}
	return b.Finish()
}

// failureResponse builds a SERVFAIL response for query.
func failureResponse(query []byte) []byte {
	if len(query) < 12 {
		return nil
	}
	var p dnsmessage.Parser
	if h, err := p.Start(query); err == nil {
		if qs, err := p.AllQuestions(); err == nil {
			h.Response = true
			h.RecursionAvailable = true
			h.RCode = dnsmessage.RCodeServerFailure
			b := dnsmessage.NewBuilder(nil, h)
			b.StartQuestions()
			for _, q := range qs {
				b.Question(q)
			}
			if out, err := b.Finish(); err == nil {
				return out
			}
		}
	}
	resp := append([]byte(nil), query...)
	resp[2] |= 0x80               // QR
	resp[3] = resp[3]&0xf0 | 0x82 // RA + SERVFAIL
	// Keep only the question section.
	binary.BigEndian.PutUint16(resp[6:], 0)
	binary.BigEndian.PutUint16(resp[8:], 0)
	binary.BigEndian.PutUint16(resp[10:], 0)
	return resp
}

// Forwarder answers DNS queries by forwarding them to upstream resolvers and
// reports addresses of names that match the domain rules.
type Forwarder struct {
	// Upstreams are tried in order for regular queries.
	Upstreams []Exchanger
	// MatchedUpstreams, when set, answer queries for matched names, e.g. to
	// resolve excluded domains with the regular network's DNS, or included
	// domains with the tunnel's DNS while other names use the regular one.
	MatchedUpstreams []Exchanger
	Matcher          *DomainMatcher
	// OnMatch is called synchronously with the addresses of matched names
	// before the response is returned, so routes exist before the
	// application connects.
	OnMatch func(a *Answer)
	// DropAAAAForMatched answers AAAA queries for matched names with an
	// empty response, used when matched traffic cannot be routed over IPv6.
	DropAAAAForMatched bool
	Timeout            time.Duration
	Logf               func(format string, args ...any)
}

func (f *Forwarder) logf(format string, args ...any) {
	if f.Logf != nil {
		f.Logf(format, args...)
	}
}

// Handle answers one query.
func (f *Forwarder) Handle(ctx context.Context, query []byte) []byte {
	name, qtype := questionName(query)
	if name == "" {
		return failureResponse(query)
	}
	matched := false
	if !f.Matcher.Empty() {
		_, matched = f.Matcher.Match(name)
	}
	if matched && f.DropAAAAForMatched && qtype == dnsmessage.TypeAAAA {
		if resp, err := emptyResponse(query); err == nil {
			return resp
		}
	}
	upstreams := f.Upstreams
	if matched && len(f.MatchedUpstreams) > 0 {
		upstreams = f.MatchedUpstreams
	}
	timeout := f.Timeout
	if timeout == 0 {
		timeout = 4 * time.Second
	}
	var resp []byte
	var lastErr error = errors.New("no upstreams")
	for _, up := range upstreams {
		qctx, cancel := context.WithTimeout(ctx, timeout)
		r, err := up.Exchange(qctx, query)
		cancel()
		if err == nil && len(r) >= 12 {
			resp = r
			break
		}
		lastErr = err
	}
	if resp == nil {
		f.logf("DNS query for %s failed: %v", name, lastErr)
		return failureResponse(query)
	}
	if f.OnMatch != nil && !f.Matcher.Empty() {
		if a, err := ParseAnswer(resp); err == nil && len(a.Addrs) > 0 {
			for _, n := range a.Names {
				if _, ok := f.Matcher.Match(n); ok {
					f.OnMatch(a)
					break
				}
			}
		}
	}
	return resp
}

// ServeUDP answers queries on a packet connection until it is closed.
func (f *Forwarder) ServeUDP(pc net.PacketConn) error {
	buf := make([]byte, maxDNSMessage)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			// Windows reports ICMP port unreachable for earlier replies as
			// read errors on UDP sockets; skip them.
			continue
		}
		query := append([]byte(nil), buf[:n]...)
		go func() {
			resp := f.Handle(context.Background(), query)
			if resp == nil {
				return
			}
			if len(resp) > 512 && !hasEDNS(query) {
				resp = truncate(resp)
			}
			pc.WriteTo(resp, addr)
		}()
	}
}

// ServeTCP answers length-prefixed queries on a stream listener.
func (f *Forwarder) ServeTCP(l net.Listener) error {
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
		go f.serveStream(c)
	}
}

func (f *Forwarder) serveStream(c net.Conn) {
	defer c.Close()
	var wmu sync.Mutex
	for {
		c.SetReadDeadline(time.Now().Add(30 * time.Second))
		var lenBuf [2]byte
		if _, err := io.ReadFull(c, lenBuf[:]); err != nil {
			return
		}
		query := make([]byte, binary.BigEndian.Uint16(lenBuf[:]))
		if _, err := io.ReadFull(c, query); err != nil {
			return
		}
		go func() {
			resp := f.Handle(context.Background(), query)
			if resp == nil {
				return
			}
			out := make([]byte, 2+len(resp))
			binary.BigEndian.PutUint16(out, uint16(len(resp)))
			copy(out[2:], resp)
			wmu.Lock()
			c.SetWriteDeadline(time.Now().Add(10 * time.Second))
			c.Write(out)
			wmu.Unlock()
		}()
	}
}

func hasEDNS(query []byte) bool {
	return len(query) >= 12 && binary.BigEndian.Uint16(query[10:]) > 0
}

// truncate returns the header and question of resp with the TC bit set.
func truncate(resp []byte) []byte {
	var p dnsmessage.Parser
	h, err := p.Start(resp)
	if err != nil {
		return failureResponse(resp)
	}
	qs, err := p.AllQuestions()
	if err != nil {
		return failureResponse(resp)
	}
	h.Truncated = true
	b := dnsmessage.NewBuilder(nil, h)
	b.StartQuestions()
	for _, q := range qs {
		b.Question(q)
	}
	out, err := b.Finish()
	if err != nil {
		return failureResponse(resp)
	}
	return out
}

// RouteSink installs and removes host routes for learned addresses.
type RouteSink interface {
	AddHostRoutes(addrs []netip.Addr) error
	RemoveHostRoutes(addrs []netip.Addr) error
}

// LearnedRoutes remembers addresses learned from DNS and keeps them routed for
// the lifetime of the tunnel. When the limit is reached the least recently
// seen addresses are dropped.
type LearnedRoutes struct {
	Sink  RouteSink
	Limit int
	// Filter, when set, decides whether an address may be routed, e.g. to
	// skip IPv6 when the tunnel has no IPv6 address.
	Filter func(netip.Addr) bool

	mu    sync.Mutex
	seen  map[netip.Addr]uint64
	clock uint64
}

// Learn routes any new addresses and refreshes known ones. It returns the
// addresses that were newly added.
func (l *LearnedRoutes) Learn(addrs []netip.Addr) ([]netip.Addr, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.seen == nil {
		l.seen = make(map[netip.Addr]uint64)
	}
	var fresh []netip.Addr
	for _, a := range addrs {
		a = a.Unmap()
		if !a.IsValid() || a.IsUnspecified() || a.IsLoopback() {
			continue
		}
		if l.Filter != nil && !l.Filter(a) {
			continue
		}
		l.clock++
		if _, ok := l.seen[a]; !ok {
			fresh = append(fresh, a)
		}
		l.seen[a] = l.clock
	}
	if len(fresh) == 0 {
		return nil, nil
	}
	if err := l.Sink.AddHostRoutes(fresh); err != nil {
		for _, a := range fresh {
			delete(l.seen, a)
		}
		return nil, err
	}
	limit := l.Limit
	if limit <= 0 {
		limit = 16384
	}
	if over := len(l.seen) - limit; over > 0 {
		evict := oldest(l.seen, over)
		for _, a := range evict {
			delete(l.seen, a)
		}
		if err := l.Sink.RemoveHostRoutes(evict); err != nil {
			log.Printf("Unable to remove expired split tunnel routes: %v", err)
		}
	}
	return fresh, nil
}

// Snapshot returns the currently routed addresses.
func (l *LearnedRoutes) Snapshot() []netip.Addr {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]netip.Addr, 0, len(l.seen))
	for a := range l.seen {
		out = append(out, a)
	}
	return out
}

// Reapply installs all known routes again, e.g. after the default gateway
// changed.
func (l *LearnedRoutes) Reapply() error {
	addrs := l.Snapshot()
	if len(addrs) == 0 {
		return nil
	}
	return l.Sink.AddHostRoutes(addrs)
}

func oldest(m map[netip.Addr]uint64, n int) []netip.Addr {
	type kv struct {
		a netip.Addr
		t uint64
	}
	all := make([]kv, 0, len(m))
	for a, t := range m {
		all = append(all, kv{a, t})
	}
	// Partial selection is plenty here; eviction is rare.
	for i := 0; i < n && i < len(all); i++ {
		minIdx := i
		for j := i + 1; j < len(all); j++ {
			if all[j].t < all[minIdx].t {
				minIdx = j
			}
		}
		all[i], all[minIdx] = all[minIdx], all[i]
	}
	out := make([]netip.Addr, 0, n)
	for i := 0; i < n && i < len(all); i++ {
		out = append(out, all[i].a)
	}
	return out
}
