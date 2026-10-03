/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

// Package splittunnel holds the platform independent parts of split tunneling:
// the per-tunnel rule set, prefix handling, domain matching and the bookkeeping
// for routes that are learned from DNS answers.
package splittunnel

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"sort"
	"strings"
)

// Mode selects how IP and domain rules are interpreted.
type Mode string

const (
	// ModeOff routes exactly what the tunnel configuration says.
	ModeOff Mode = "off"
	// ModeExclude sends everything through the tunnel except the listed
	// addresses and domains.
	ModeExclude Mode = "exclude"
	// ModeInclude sends only the listed addresses and domains through the
	// tunnel; everything else uses the regular network.
	ModeInclude Mode = "include"
)

// AppAction is what happens to traffic of a matching application.
type AppAction string

const (
	// AppBypass excludes the application from the tunnel. It requires the
	// split tunnel driver; without it the rule is reported as unsupported.
	AppBypass AppAction = "bypass"
	// AppVPNOnly allows the application to talk only through the tunnel,
	// which is a per-application kill switch.
	AppVPNOnly AppAction = "vpnonly"
	// AppBlock blocks all network access of the application while the
	// tunnel is up.
	AppBlock AppAction = "block"
)

// AppRule matches a single executable or every executable within a folder.
type AppRule struct {
	Path    string    `json:"path"`
	Folder  bool      `json:"folder,omitempty"`
	Action  AppAction `json:"action"`
	Enabled *bool     `json:"enabled,omitempty"`
}

// IsEnabled reports whether the rule is active. Rules are enabled unless
// explicitly switched off.
func (r AppRule) IsEnabled() bool {
	return r.Enabled == nil || *r.Enabled
}

// DNSConfig controls the in-tunnel DNS forwarder.
type DNSConfig struct {
	// Upstreams are used instead of the tunnel's DNS servers when set. Plain
	// addresses ("1.1.1.1", "[2606:4700::1111]:53"), DNS-over-TLS
	// ("tls://1.1.1.1", "tls://dns.example#1.1.1.1") and DNS-over-HTTPS
	// ("https://1.1.1.1/dns-query") are accepted.
	Upstreams []string `json:"upstreams,omitempty"`
	// Force enables the forwarder even when no domain rules exist, for
	// example to use encrypted DNS.
	Force bool `json:"force,omitempty"`
	// BlockOtherDNS blocks plain DNS to every server except the forwarder,
	// so that applications cannot side-step domain rules.
	BlockOtherDNS bool `json:"blockOtherDns,omitempty"`
	// ExcludedViaSystemDNS resolves domains that are excluded from the
	// tunnel with the regular network's DNS servers instead of the tunnel's.
	ExcludedViaSystemDNS bool `json:"excludedViaSystemDns,omitempty"`
}

// ProxyConfig exposes the tunnel as a local proxy.
type ProxyConfig struct {
	SOCKS5    bool   `json:"socks5,omitempty"`
	SOCKSPort uint16 `json:"socksPort,omitempty"`
	HTTP      bool   `json:"http,omitempty"`
	HTTPPort  uint16 `json:"httpPort,omitempty"`
	// Listen is the address to listen on; defaults to 127.0.0.1.
	Listen   string `json:"listen,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// Config is the per-tunnel split tunneling configuration.
type Config struct {
	Mode    Mode      `json:"mode"`
	IPs     []string  `json:"ips,omitempty"`
	Domains []string  `json:"domains,omitempty"`
	Apps    []AppRule `json:"apps,omitempty"`
	// AllowLAN keeps private ranges off the tunnel in exclude mode and
	// permits them through the kill switch.
	AllowLAN bool        `json:"allowLan,omitempty"`
	DNS      DNSConfig   `json:"dns"`
	Proxy    ProxyConfig `json:"proxy"`
}

const (
	DefaultSOCKSPort = 1080
	DefaultHTTPPort  = 8118
)

// LANPrefixes are the private and link-local ranges that AllowLAN keeps local.
var LANPrefixes = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("255.255.255.255/32"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

// Parse decodes a configuration and validates it.
func Parse(data []byte) (*Config, error) {
	c := &Config{}
	if len(strings.TrimSpace(string(data))) == 0 {
		c.Mode = ModeOff
		return c, nil
	}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, err
	}
	if c.Mode == "" {
		c.Mode = ModeOff
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// Marshal encodes the configuration in a stable, readable form.
func (c *Config) Marshal() ([]byte, error) {
	return json.MarshalIndent(c, "", "  ")
}

// Validate checks every rule and returns a combined error describing all
// invalid entries.
func (c *Config) Validate() error {
	var problems []string
	switch c.Mode {
	case ModeOff, ModeExclude, ModeInclude:
	default:
		problems = append(problems, fmt.Sprintf("unknown mode %q", c.Mode))
	}
	for _, ip := range c.IPs {
		if _, err := ParsePrefix(ip); err != nil {
			problems = append(problems, err.Error())
		}
	}
	for _, d := range c.Domains {
		if _, err := ParseDomainPattern(d); err != nil {
			problems = append(problems, err.Error())
		}
	}
	for _, a := range c.Apps {
		if !filepath.IsAbs(a.Path) && !strings.HasPrefix(a.Path, `\\`) && !(len(a.Path) >= 3 && a.Path[1] == ':' && (a.Path[2] == '\\' || a.Path[2] == '/')) {
			problems = append(problems, fmt.Sprintf("application path %q is not absolute", a.Path))
		}
		switch a.Action {
		case AppBypass, AppVPNOnly, AppBlock:
		default:
			problems = append(problems, fmt.Sprintf("unknown action %q for %q", a.Action, a.Path))
		}
	}
	for _, u := range c.DNS.Upstreams {
		if _, err := ParseUpstream(u); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if c.Proxy.Listen != "" {
		if _, err := netip.ParseAddr(c.Proxy.Listen); err != nil {
			problems = append(problems, fmt.Sprintf("invalid proxy listen address %q", c.Proxy.Listen))
		}
	}
	if c.Proxy.SOCKS5 && c.Proxy.HTTP && c.Proxy.socksPort() == c.Proxy.httpPort() {
		problems = append(problems, "SOCKS5 and HTTP proxies cannot share a port")
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func (p *ProxyConfig) socksPort() uint16 {
	if p.SOCKSPort == 0 {
		return DefaultSOCKSPort
	}
	return p.SOCKSPort
}

func (p *ProxyConfig) httpPort() uint16 {
	if p.HTTPPort == 0 {
		return DefaultHTTPPort
	}
	return p.HTTPPort
}

// SOCKSAddr returns the listen address of the SOCKS5 proxy.
func (p *ProxyConfig) SOCKSAddr() netip.AddrPort {
	return netip.AddrPortFrom(p.listenAddr(), p.socksPort())
}

// HTTPAddr returns the listen address of the HTTP proxy.
func (p *ProxyConfig) HTTPAddr() netip.AddrPort {
	return netip.AddrPortFrom(p.listenAddr(), p.httpPort())
}

func (p *ProxyConfig) listenAddr() netip.Addr {
	if a, err := netip.ParseAddr(p.Listen); err == nil {
		return a
	}
	return netip.AddrFrom4([4]byte{127, 0, 0, 1})
}

// Active reports whether any split tunneling feature is in use.
func (c *Config) Active() bool {
	if c == nil {
		return false
	}
	return c.RoutingActive() || len(c.EnabledApps()) > 0 || c.Proxy.SOCKS5 || c.Proxy.HTTP || c.DNSForwarderNeeded()
}

// RoutingActive reports whether routes deviate from the tunnel configuration.
func (c *Config) RoutingActive() bool {
	if c == nil {
		return false
	}
	switch c.Mode {
	case ModeInclude:
		return true
	case ModeExclude:
		return len(c.IPs) > 0 || len(c.Domains) > 0 || c.AllowLAN
	}
	return false
}

// DNSForwarderNeeded reports whether the tunnel has to run its DNS forwarder.
func (c *Config) DNSForwarderNeeded() bool {
	if c == nil {
		return false
	}
	return c.DNS.Force || len(c.DNS.Upstreams) > 0 || (c.Mode != ModeOff && len(c.Domains) > 0)
}

// EnabledApps returns the application rules that are switched on.
func (c *Config) EnabledApps() []AppRule {
	if c == nil {
		return nil
	}
	var apps []AppRule
	for _, a := range c.Apps {
		if a.IsEnabled() && a.Path != "" {
			apps = append(apps, a)
		}
	}
	return apps
}

// Prefixes returns the parsed, de-duplicated IP rules, with LAN ranges added
// in exclude mode when AllowLAN is set.
func (c *Config) Prefixes() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range c.IPs {
		if p, err := ParsePrefix(s); err == nil {
			out = append(out, p)
		}
	}
	if c.Mode == ModeExclude && c.AllowLAN {
		out = append(out, LANPrefixes...)
	}
	return MergePrefixes(out)
}

// Matcher builds the domain matcher for this configuration.
func (c *Config) Matcher() *DomainMatcher {
	m := &DomainMatcher{}
	for _, d := range c.Domains {
		if p, err := ParseDomainPattern(d); err == nil {
			m.patterns = append(m.patterns, p)
		}
	}
	return m
}

// ParsePrefix accepts a bare address or a CIDR and returns the masked prefix.
func ParsePrefix(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return netip.Prefix{}, errors.New("empty address")
	}
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("invalid CIDR %q", s)
		}
		bits := p.Bits() - unmapBits(p.Addr())
		if bits < 0 {
			return netip.Prefix{}, fmt.Errorf("invalid CIDR %q", s)
		}
		return netip.PrefixFrom(p.Addr().Unmap(), bits).Masked(), nil
	}
	a, err := netip.ParseAddr(strings.Trim(s, "[]"))
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid IP address %q", s)
	}
	a = a.Unmap().WithZone("")
	return netip.PrefixFrom(a, a.BitLen()), nil
}

func unmapBits(a netip.Addr) int {
	if a.Is4In6() {
		return 96
	}
	return 0
}

// MergePrefixes sorts prefixes, drops duplicates and removes any prefix that
// is already covered by a broader one.
func MergePrefixes(in []netip.Prefix) []netip.Prefix {
	ps := make([]netip.Prefix, 0, len(in))
	for _, p := range in {
		if p.IsValid() {
			ps = append(ps, p.Masked())
		}
	}
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].Addr().Is4() != ps[j].Addr().Is4() {
			return ps[i].Addr().Is4()
		}
		if ps[i].Bits() != ps[j].Bits() {
			return ps[i].Bits() < ps[j].Bits()
		}
		return ps[i].Addr().Less(ps[j].Addr())
	})
	out := make([]netip.Prefix, 0, len(ps))
next:
	for _, p := range ps {
		for _, q := range out {
			if q.Bits() <= p.Bits() && q.Contains(p.Addr()) {
				continue next
			}
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Addr().Is4() != out[j].Addr().Is4() {
			return out[i].Addr().Is4()
		}
		if c := out[i].Addr().Compare(out[j].Addr()); c != 0 {
			return c < 0
		}
		return out[i].Bits() < out[j].Bits()
	})
	return out
}

// ContainsAddr reports whether any prefix contains addr.
func ContainsAddr(prefixes []netip.Prefix, addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
