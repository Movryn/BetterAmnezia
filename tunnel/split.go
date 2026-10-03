/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package tunnel

import (
	"context"
	"encoding/binary"
	"errors"
	"io/fs"
	"log"
	"net"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/amnezia-vpn/amneziawg-windows/v3/conf"
	"github.com/amnezia-vpn/amneziawg-windows/v3/tunnel/winipcfg"

	"github.com/amnezia-vpn/amneziawg-windows-client/extras"
	"github.com/amnezia-vpn/amneziawg-windows-client/splittunnel"
	"github.com/amnezia-vpn/amneziawg-windows-client/tunnel/firewall"
)

// fallbackRouteMetric is used for a default route on the tunnel in include
// mode. It is far worse than any real default route, so regular traffic keeps
// using the physical network, but sockets bound to the tunnel address (the
// DNS forwarder and the local proxies) still find a route.
const fallbackRouteMetric = 4096

// Socket options from ws2ipdef.h that pin a socket to an interface.
const (
	ipUnicastIf   = 31
	ipv6UnicastIf = 31
)

// dnsListenCandidates are loopback addresses tried for the DNS forwarder.
var dnsListenCandidates = []string{"127.0.0.53", "127.0.53.53", "127.53.0.1", "127.83.65.1"}

// splitRuntime holds the split tunneling state of the running tunnel.
type splitRuntime struct {
	cfg  *splittunnel.Config
	conf *conf.Config

	luid  winipcfg.LUID
	tunV4 netip.Addr
	tunV6 netip.Addr

	includePrefixes []netip.Prefix
	excludePrefixes []netip.Prefix
	matcher         *splittunnel.DomainMatcher

	tunnelUpstreams []splittunnel.Upstream
	systemUpstreams []splittunnel.Upstream
	systemDNS       []netip.Addr

	dnsListen netip.Addr
	udp       net.PacketConn
	tcp       net.Listener
	forwarder *splittunnel.Forwarder

	proxy   *splittunnel.ProxyServer
	learned *splittunnel.LearnedRoutes
	phys    *physicalRoutes
	driver  *splitDriver

	routeCb   *winipcfg.RouteChangeCallback
	bumpTimer *time.Timer

	mu sync.Mutex
}

// activeSplit is set while a tunnel with split tunneling runs in this
// process. Each tunnel service hosts exactly one tunnel.
var activeSplit *splitRuntime

// loadSplit reads the split configuration for the tunnel. Errors are logged
// and result in plain routing, so a broken rule file never prevents
// connecting.
func loadSplit(c *conf.Config) *splitRuntime {
	cfg, err := extras.LoadSplit(c.Name)
	if err != nil {
		log.Printf("Split tunneling: unable to load rules, continuing without them: %v", err)
		return nil
	}
	if !cfg.Active() {
		return nil
	}
	s := &splitRuntime{cfg: cfg, conf: c, matcher: cfg.Matcher()}
	for _, a := range c.Interface.Addresses {
		ip, ok := netip.AddrFromSlice(a.IP)
		if !ok {
			continue
		}
		ip = ip.Unmap()
		if ip.Is4() && !s.tunV4.IsValid() {
			s.tunV4 = ip
		} else if ip.Is6() && !s.tunV6.IsValid() {
			s.tunV6 = ip
		}
	}
	switch cfg.Mode {
	case splittunnel.ModeInclude:
		s.includePrefixes = cfg.Prefixes()
	case splittunnel.ModeExclude:
		s.excludePrefixes = cfg.Prefixes()
	}
	log.Printf("Split tunneling: mode=%s, %d address rules, %d domain rules, %d app rules", cfg.Mode, len(cfg.IPs), len(cfg.Domains), len(cfg.EnabledApps()))
	s.systemDNS = currentSystemDNS()
	s.prepareUpstreams()
	return s
}

// currentSystemDNS returns the DNS servers of all connected adapters. It is
// called before the tunnel adapter exists.
func currentSystemDNS() []netip.Addr {
	adapters, err := winipcfg.GetAdaptersAddresses(windows.AF_UNSPEC, winipcfg.GAAFlagSkipAnycast|winipcfg.GAAFlagSkipMulticast)
	if err != nil {
		return nil
	}
	seen := make(map[netip.Addr]bool)
	var out []netip.Addr
	for _, a := range adapters {
		if a.OperStatus != winipcfg.IfOperStatusUp || a.IfType == winipcfg.IfTypeSoftwareLoopback {
			continue
		}
		for d := a.FirstDNSServerAddress; d != nil; d = d.Next {
			ip, ok := netip.AddrFromSlice(d.Address.IP())
			if !ok {
				continue
			}
			ip = ip.Unmap()
			// Site-local IPv6 resolvers (fec0::/10) are Windows placeholders.
			if ip.IsUnspecified() || ip.IsLoopback() || (ip.Is6() && ip.As16()[0] == 0xfe && ip.As16()[1]&0xc0 == 0xc0) || seen[ip] {
				continue
			}
			seen[ip] = true
			out = append(out, ip)
		}
	}
	return out
}

func plainUpstreams(addrs []netip.Addr) []splittunnel.Upstream {
	var out []splittunnel.Upstream
	for _, a := range addrs {
		out = append(out, splittunnel.Upstream{Kind: splittunnel.UpstreamPlain, Host: a.String(), Port: 53, Bootstrap: []netip.Addr{a}})
	}
	return out
}

func (s *splitRuntime) prepareUpstreams() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	lookup := func(ctx context.Context, host string) ([]netip.Addr, error) {
		return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	}
	for _, u := range s.cfg.DNS.Upstreams {
		up, err := splittunnel.ParseUpstream(u)
		if err != nil {
			log.Printf("Split tunneling: %v", err)
			continue
		}
		if err := up.Resolve(ctx, lookup); err != nil {
			log.Printf("Split tunneling: unable to resolve DNS upstream %s: %v", u, err)
			continue
		}
		s.tunnelUpstreams = append(s.tunnelUpstreams, up)
	}
	custom := len(s.tunnelUpstreams) > 0
	if !custom {
		var tunnelDNS []netip.Addr
		for _, ip := range s.conf.Interface.DNS {
			if a, ok := netip.AddrFromSlice(ip); ok {
				tunnelDNS = append(tunnelDNS, a.Unmap())
			}
		}
		s.tunnelUpstreams = plainUpstreams(tunnelDNS)
	}
	s.systemUpstreams = plainUpstreams(s.systemDNS)
	if len(s.tunnelUpstreams) == 0 {
		// Without any tunnel DNS, fall back to the regular resolvers.
		s.tunnelUpstreams = s.systemUpstreams
	}
}

// forwarderEnabled reports whether the DNS forwarder runs.
func (s *splitRuntime) forwarderEnabled() bool {
	return s != nil && s.cfg.DNSForwarderNeeded() && s.dnsListen.IsValid()
}

func (s *splitRuntime) lockDNS() bool {
	return s.forwarderEnabled() && (s.cfg.DNS.BlockOtherDNS || len(s.cfg.Domains) > 0)
}

// startDNSListener binds the forwarder sockets. It runs before the firewall
// is enabled so that the listen address can be allowed.
func (s *splitRuntime) startDNSListener() {
	if !s.cfg.DNSForwarderNeeded() {
		return
	}
	for _, cand := range dnsListenCandidates {
		addr := netip.MustParseAddr(cand)
		udp, err := net.ListenPacket("udp4", netip.AddrPortFrom(addr, 53).String())
		if err != nil {
			log.Printf("Split tunneling: unable to listen for DNS on %s: %v", cand, err)
			continue
		}
		tcp, err := net.Listen("tcp4", netip.AddrPortFrom(addr, 53).String())
		if err != nil {
			udp.Close()
			log.Printf("Split tunneling: unable to listen for DNS on %s (TCP): %v", cand, err)
			continue
		}
		s.udp, s.tcp, s.dnsListen = udp, tcp, addr
		log.Printf("Split tunneling: DNS forwarder listening on %s", cand)
		return
	}
	log.Println("Split tunneling: DNS forwarder disabled, domain rules will not apply")
}

// firewallOptions builds the extra firewall rules.
func (s *splitRuntime) firewallOptions() *firewall.Options {
	if s == nil {
		return nil
	}
	opts := &firewall.Options{PermitPrefixes: s.excludePrefixes}
	if s.forwarderEnabled() {
		opts.DNSAllow = []net.IP{s.dnsListen.AsSlice()}
		opts.LockDNS = s.lockDNS()
	}
	for _, rule := range s.cfg.EnabledApps() {
		paths := []string{rule.Path}
		if rule.Folder {
			paths = executablesInFolder(rule.Path)
			log.Printf("Split tunneling: folder %s contains %d executables", rule.Path, len(paths))
		}
		switch rule.Action {
		case splittunnel.AppVPNOnly:
			opts.VPNOnlyApps = append(opts.VPNOnlyApps, paths...)
		case splittunnel.AppBlock:
			opts.BlockedApps = append(opts.BlockedApps, paths...)
		case splittunnel.AppBypass:
			if s.driver != nil {
				opts.BypassApps = append(opts.BypassApps, paths...)
			} else {
				log.Printf("Split tunneling: %s cannot bypass the tunnel without the split tunnel driver", rule.Path)
			}
		}
	}
	return opts
}

const maxFolderExecutables = 4096

func executablesInFolder(root string) []string {
	var out []string
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if len(out) >= maxFolderExecutables {
			return fs.SkipAll
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".exe") {
			out = append(out, path)
		}
		return nil
	})
	return out
}

// bypassApps lists executables for the split tunnel driver.
func (s *splitRuntime) bypassApps() []string {
	var out []string
	for _, rule := range s.cfg.EnabledApps() {
		if rule.Action != splittunnel.AppBypass {
			continue
		}
		if rule.Folder {
			out = append(out, executablesInFolder(rule.Path)...)
		} else {
			out = append(out, rule.Path)
		}
	}
	return out
}

func hasDefaultFor(family winipcfg.AddressFamily, c *conf.Config) bool {
	return hasDefaultRoute(family, c.Peers)
}

func (s *splitRuntime) hasTunnelAddr(family winipcfg.AddressFamily) bool {
	if family == windows.AF_INET {
		return s.tunV4.IsValid()
	}
	return s.tunV6.IsValid()
}

func familyOf(a netip.Addr) winipcfg.AddressFamily {
	if a.Is4() {
		return windows.AF_INET
	}
	return windows.AF_INET6
}

func prefixToIPNet(p netip.Prefix) net.IPNet {
	return net.IPNet{IP: p.Addr().AsSlice(), Mask: net.CIDRMask(p.Bits(), p.Addr().BitLen())}
}

func hostPrefix(a netip.Addr) netip.Prefix {
	return netip.PrefixFrom(a, a.BitLen())
}

// includeRoutes returns the tunnel routes in include mode. ok is false when
// the configuration does not change routing.
func (s *splitRuntime) includeRoutes(family winipcfg.AddressFamily) (routes []winipcfg.RouteData, ok bool) {
	if s == nil || s.cfg.Mode != splittunnel.ModeInclude {
		return nil, false
	}
	add := func(p netip.Prefix, metric uint32) {
		if familyOf(p.Addr()) != family {
			return
		}
		routes = append(routes, winipcfg.RouteData{Destination: prefixToIPNet(p), NextHop: zeroIP(family), Metric: metric})
	}
	for _, p := range s.includePrefixes {
		add(p, 0)
	}
	for _, u := range s.tunnelUpstreams {
		for _, a := range u.Bootstrap {
			add(hostPrefix(a), 0)
		}
	}
	if s.learned != nil {
		for _, a := range s.learned.Snapshot() {
			add(hostPrefix(a), 0)
		}
	}
	if hasDefaultFor(family, s.conf) && (s.forwarderEnabled() || s.cfg.Proxy.SOCKS5 || s.cfg.Proxy.HTTP) {
		if family == windows.AF_INET {
			add(netip.MustParsePrefix("0.0.0.0/0"), fallbackRouteMetric)
		} else {
			add(netip.MustParsePrefix("::/0"), fallbackRouteMetric)
		}
	}
	return routes, true
}

func zeroIP(family winipcfg.AddressFamily) net.IP {
	if family == windows.AF_INET {
		return net.IPv4zero
	}
	return net.IPv6zero
}

// dnsServers returns the DNS servers to configure on the tunnel interface.
func (s *splitRuntime) dnsServers(family winipcfg.AddressFamily, configured []net.IP) []net.IP {
	if !s.forwarderEnabled() {
		return configured
	}
	if family == windows.AF_INET {
		return []net.IP{s.dnsListen.AsSlice()}
	}
	return nil
}

// preferTunnelDNS reports whether the tunnel interface should get a low
// metric although it carries no default route, so Windows asks the
// forwarder first.
func (s *splitRuntime) preferTunnelDNS() bool {
	return s != nil && s.cfg.Mode == splittunnel.ModeInclude && s.forwarderEnabled()
}

// tunnelDialer returns a dial function that sends through the tunnel by
// binding to the tunnel address.
func (s *splitRuntime) tunnelDialer() splittunnel.DialFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		d := &net.Dialer{Timeout: 10 * time.Second}
		if ap, err := netip.ParseAddrPort(address); err == nil {
			local := s.tunV4
			if ap.Addr().Unmap().Is6() {
				local = s.tunV6
			}
			if !local.IsValid() {
				// Without a tunnel address of this family the connection
				// would leave through the regular network.
				return nil, errors.New("the tunnel has no address for " + ap.Addr().String())
			}
			if strings.HasPrefix(network, "udp") {
				d.LocalAddr = &net.UDPAddr{IP: local.AsSlice()}
			} else {
				d.LocalAddr = &net.TCPAddr{IP: local.AsSlice()}
			}
		}
		return d.DialContext(ctx, network, address)
	}
}

// physicalDialer sends through the regular network by pinning the socket to
// the interface that carries the best non-tunnel default route.
func (s *splitRuntime) physicalDialer() splittunnel.DialFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		d := &net.Dialer{Timeout: 10 * time.Second}
		ap, err := netip.ParseAddrPort(address)
		if err == nil {
			family := familyOf(ap.Addr().Unmap())
			if gw, ok := bestPhysicalDefault(family, s.luid); ok {
				index := gw.index
				d.Control = func(network, address string, c syscall.RawConn) error {
					var serr error
					err := c.Control(func(fd uintptr) {
						if family == windows.AF_INET {
							var be [4]byte
							binary.BigEndian.PutUint32(be[:], index)
							serr = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, ipUnicastIf, int(binary.LittleEndian.Uint32(be[:])))
						} else {
							serr = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IPV6, ipv6UnicastIf, int(index))
						}
					})
					if err != nil {
						return err
					}
					return serr
				}
			}
		}
		return d.DialContext(ctx, network, address)
	}
}

func exchangers(ups []splittunnel.Upstream, dial splittunnel.DialFunc) []splittunnel.Exchanger {
	out := make([]splittunnel.Exchanger, 0, len(ups))
	for _, u := range ups {
		out = append(out, splittunnel.NewExchanger(u, dial))
	}
	return out
}

// attach is called once the tunnel adapter exists.
func (s *splitRuntime) attach(luid winipcfg.LUID) {
	s.luid = luid
	switch s.cfg.Mode {
	case splittunnel.ModeInclude:
		s.learned = &splittunnel.LearnedRoutes{
			Sink:   &tunnelRouteSink{s: s},
			Filter: func(a netip.Addr) bool { return s.hasTunnelAddr(familyOf(a)) },
		}
	case splittunnel.ModeExclude:
		s.phys = &physicalRoutes{ourLUID: luid, installed: make(map[netip.Prefix]physicalRoute)}
		s.learned = &splittunnel.LearnedRoutes{Sink: &physicalRouteSink{s: s}}
	}

	if s.forwarderEnabled() {
		f := &splittunnel.Forwarder{
			Matcher: s.matcher,
			Logf:    log.Printf,
		}
		tunnelEx := exchangers(s.tunnelUpstreams, s.tunnelDialer())
		systemEx := exchangers(s.systemUpstreams, s.physicalDialer())
		switch s.cfg.Mode {
		case splittunnel.ModeInclude:
			if len(s.cfg.DNS.Upstreams) == 0 && len(systemEx) > 0 {
				// Only matched names need the tunnel's resolver; resolve the
				// rest like the regular network would.
				f.Upstreams = systemEx
				f.MatchedUpstreams = tunnelEx
			} else {
				f.Upstreams = tunnelEx
			}
			f.DropAAAAForMatched = !s.tunV6.IsValid()
		case splittunnel.ModeExclude:
			f.Upstreams = tunnelEx
			if s.cfg.DNS.ExcludedViaSystemDNS && len(systemEx) > 0 {
				f.MatchedUpstreams = systemEx
			}
		default:
			f.Upstreams = tunnelEx
		}
		if s.learned != nil && !s.matcher.Empty() {
			f.OnMatch = func(a *splittunnel.Answer) {
				fresh, err := s.learned.Learn(a.Addrs)
				if err != nil {
					log.Printf("Split tunneling: unable to route %s: %v", a.Question, err)
				} else if len(fresh) > 0 {
					log.Printf("Split tunneling: %s -> %v (%s)", a.Question, fresh, s.cfg.Mode)
				}
			}
		}
		s.forwarder = f
		go f.ServeUDP(s.udp)
		go f.ServeTCP(s.tcp)
	}
}

// afterConfigure runs after the interface addresses and routes for a family
// have been set.
func (s *splitRuntime) afterConfigure(family winipcfg.AddressFamily) {
	if s.cfg.Mode != splittunnel.ModeExclude || s.phys == nil {
		return
	}
	var prefixes []netip.Prefix
	for _, p := range s.excludePrefixes {
		if familyOf(p.Addr()) == family && routablePrefix(p) {
			prefixes = append(prefixes, p)
		}
	}
	if err := s.phys.add(prefixes); err != nil {
		log.Printf("Split tunneling: unable to add excluded routes: %v", err)
	}
	s.watchDefaultRoutes()
}

// watchDefaultRoutes follows changes of the physical default route, moving
// excluded routes and updating the driver's internet address.
func (s *splitRuntime) watchDefaultRoutes() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.routeCb != nil {
		return
	}
	s.bumpTimer = time.AfterFunc(time.Hour, func() {
		if s.phys != nil {
			if err := s.phys.refresh(); err != nil {
				log.Printf("Split tunneling: unable to move excluded routes to new gateway: %v", err)
			}
		}
		s.mu.Lock()
		d := s.driver
		s.mu.Unlock()
		if d != nil {
			if err := d.registerIPs(); err != nil {
				log.Printf("Split tunneling: %v", err)
			}
		}
	})
	s.bumpTimer.Stop()
	cb, err := winipcfg.RegisterRouteChangeCallback(func(notificationType winipcfg.MibNotificationType, route *winipcfg.MibIPforwardRow2) {
		if route != nil && route.DestinationPrefix.PrefixLength == 0 && route.InterfaceLUID != s.luid {
			s.bumpTimer.Reset(500 * time.Millisecond)
		}
	})
	if err != nil {
		log.Printf("Split tunneling: unable to watch default routes: %v", err)
		return
	}
	s.routeCb = cb
}

// routablePrefix filters ranges that already have on-link routes on every
// interface and must not be sent to a gateway.
func routablePrefix(p netip.Prefix) bool {
	a := p.Addr()
	return !a.IsMulticast() && !a.IsLinkLocalUnicast() && !(a.Is4() && p.Bits() == 32 && a == netip.AddrFrom4([4]byte{255, 255, 255, 255}))
}

// startProxies starts the local proxies once the tunnel is up.
func (s *splitRuntime) startProxies() {
	if !s.cfg.Proxy.SOCKS5 && !s.cfg.Proxy.HTTP {
		return
	}
	tunnelEx := exchangers(s.tunnelUpstreams, s.tunnelDialer())
	s.proxy = &splittunnel.ProxyServer{
		Dial: s.tunnelDialer(),
		Lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return splittunnel.LookupHost(ctx, tunnelEx, host, s.tunV6.IsValid())
		},
		Username: s.cfg.Proxy.Username,
		Password: s.cfg.Proxy.Password,
		Logf:     log.Printf,
	}
	if s.cfg.Proxy.SOCKS5 {
		addr := s.cfg.Proxy.SOCKSAddr()
		l, err := net.Listen("tcp", addr.String())
		if err != nil {
			log.Printf("Proxy: unable to listen for SOCKS5 on %s: %v", addr, err)
		} else {
			log.Printf("Proxy: SOCKS5 listening on %s", addr)
			go s.proxy.ServeSOCKS5(l)
		}
	}
	if s.cfg.Proxy.HTTP {
		addr := s.cfg.Proxy.HTTPAddr()
		l, err := net.Listen("tcp", addr.String())
		if err != nil {
			log.Printf("Proxy: unable to listen for HTTP on %s: %v", addr, err)
		} else {
			log.Printf("Proxy: HTTP listening on %s", addr)
			go s.proxy.ServeHTTP(l)
		}
	}
}

// close stops servers and removes routes installed outside the tunnel
// interface. Routes on the tunnel interface vanish with the adapter.
func (s *splitRuntime) close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.routeCb != nil {
		s.routeCb.Unregister()
		s.routeCb = nil
	}
	if s.bumpTimer != nil {
		s.bumpTimer.Stop()
	}
	s.mu.Unlock()
	if s.proxy != nil {
		s.proxy.Close()
	}
	if s.udp != nil {
		s.udp.Close()
	}
	if s.tcp != nil {
		s.tcp.Close()
	}
	if s.phys != nil {
		s.phys.removeAll()
	}
	s.mu.Lock()
	d := s.driver
	s.driver = nil
	s.mu.Unlock()
	if d != nil {
		d.Close()
	}
}

type tunnelRouteSink struct{ s *splitRuntime }

func (t *tunnelRouteSink) AddHostRoutes(addrs []netip.Addr) error {
	var errs []error
	for _, a := range addrs {
		err := t.s.luid.AddRoute(prefixToIPNet(hostPrefix(a)), zeroIP(familyOf(a)), 0)
		if err != nil && !errors.Is(err, windows.ERROR_OBJECT_ALREADY_EXISTS) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (t *tunnelRouteSink) RemoveHostRoutes(addrs []netip.Addr) error {
	for _, a := range addrs {
		t.s.luid.DeleteRoute(prefixToIPNet(hostPrefix(a)), zeroIP(familyOf(a)))
	}
	return nil
}

type physicalRouteSink struct{ s *splitRuntime }

func (p *physicalRouteSink) AddHostRoutes(addrs []netip.Addr) error {
	prefixes := make([]netip.Prefix, 0, len(addrs))
	for _, a := range addrs {
		prefixes = append(prefixes, hostPrefix(a))
	}
	err := p.s.phys.add(prefixes)
	if ferr := firewall.PermitRemoteAddrs(addrs); ferr != nil {
		err = errors.Join(err, ferr)
	}
	return err
}

func (p *physicalRouteSink) RemoveHostRoutes(addrs []netip.Addr) error {
	prefixes := make([]netip.Prefix, 0, len(addrs))
	for _, a := range addrs {
		prefixes = append(prefixes, hostPrefix(a))
	}
	p.s.phys.remove(prefixes)
	return nil
}

type gateway struct {
	luid    winipcfg.LUID
	index   uint32
	nextHop net.IP
}

// bestPhysicalDefault finds the default route with the lowest metric that is
// not on the tunnel interface.
func bestPhysicalDefault(family winipcfg.AddressFamily, ourLUID winipcfg.LUID) (gateway, bool) {
	r, err := winipcfg.GetIPForwardTable2(family)
	if err != nil {
		return gateway{}, false
	}
	lowest := ^uint32(0)
	var best gateway
	found := false
	for i := range r {
		if r[i].DestinationPrefix.PrefixLength != 0 || r[i].InterfaceLUID == ourLUID {
			continue
		}
		ifrow, err := r[i].InterfaceLUID.Interface()
		if err != nil || ifrow.OperStatus != winipcfg.IfOperStatusUp {
			continue
		}
		iface, err := r[i].InterfaceLUID.IPInterface(family)
		if err != nil {
			continue
		}
		if m := r[i].Metric + iface.Metric; m < lowest {
			lowest = m
			best = gateway{luid: r[i].InterfaceLUID, index: r[i].InterfaceIndex, nextHop: r[i].NextHop.IP()}
			found = true
		}
	}
	return best, found
}

type physicalRoute struct {
	luid    winipcfg.LUID
	nextHop net.IP
}

// physicalRoutes keeps excluded destinations on the regular network by adding
// more specific routes via the physical default gateway.
type physicalRoutes struct {
	mu        sync.Mutex
	ourLUID   winipcfg.LUID
	installed map[netip.Prefix]physicalRoute
}

func (p *physicalRoutes) add(prefixes []netip.Prefix) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var errs []error
	gws := make(map[winipcfg.AddressFamily]*gateway)
	for _, pfx := range prefixes {
		if _, ok := p.installed[pfx]; ok {
			continue
		}
		family := familyOf(pfx.Addr())
		gw, ok := gws[family]
		if !ok {
			if g, found := bestPhysicalDefault(family, p.ourLUID); found {
				gw = &g
			}
			gws[family] = gw
		}
		if gw == nil {
			continue
		}
		err := gw.luid.AddRoute(prefixToIPNet(pfx), gw.nextHop, 0)
		if err != nil && !errors.Is(err, windows.ERROR_OBJECT_ALREADY_EXISTS) {
			errs = append(errs, err)
			continue
		}
		p.installed[pfx] = physicalRoute{luid: gw.luid, nextHop: gw.nextHop}
	}
	return errors.Join(errs...)
}

func (p *physicalRoutes) remove(prefixes []netip.Prefix) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, pfx := range prefixes {
		if r, ok := p.installed[pfx]; ok {
			r.luid.DeleteRoute(prefixToIPNet(pfx), r.nextHop)
			delete(p.installed, pfx)
		}
	}
}

// refresh moves routes when the default gateway changed.
func (p *physicalRoutes) refresh() error {
	p.mu.Lock()
	var moved []netip.Prefix
	gws := make(map[winipcfg.AddressFamily]*gateway)
	for pfx, r := range p.installed {
		family := familyOf(pfx.Addr())
		gw, ok := gws[family]
		if !ok {
			if g, found := bestPhysicalDefault(family, p.ourLUID); found {
				gw = &g
			}
			gws[family] = gw
		}
		if gw != nil && gw.luid == r.luid && gw.nextHop.Equal(r.nextHop) {
			continue
		}
		r.luid.DeleteRoute(prefixToIPNet(pfx), r.nextHop)
		delete(p.installed, pfx)
		moved = append(moved, pfx)
	}
	p.mu.Unlock()
	if len(moved) == 0 {
		return nil
	}
	log.Printf("Split tunneling: default gateway changed, moving %d excluded routes", len(moved))
	return p.add(moved)
}

func (p *physicalRoutes) removeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for pfx, r := range p.installed {
		r.luid.DeleteRoute(prefixToIPNet(pfx), r.nextHop)
	}
	p.installed = make(map[netip.Prefix]physicalRoute)
}
