/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package splittunnel

import (
	"fmt"
	"strings"
)

// DomainPattern is a single domain rule.
//
// Supported forms:
//
//	example.com      example.com and every subdomain of it
//	=example.com     only example.com itself
//	*.example.com    example.com and every subdomain of it
//	*example.com     every name ending in "example.com", e.g. myexample.com
//	api.*.example.com  glob, where * matches any run of characters
type DomainPattern struct {
	raw   string
	kind  patternKind
	value string
}

type patternKind int

const (
	patternSuffix patternKind = iota // name == value || name ends with "." + value
	patternExact
	patternGlob
)

// ParseDomainPattern validates and normalizes a domain rule.
func ParseDomainPattern(s string) (DomainPattern, error) {
	raw := s
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return DomainPattern{}, fmt.Errorf("empty domain")
	}
	if strings.HasPrefix(s, "=") {
		v := s[1:]
		if err := checkDomainChars(v, false); err != nil {
			return DomainPattern{}, fmt.Errorf("invalid domain %q: %v", raw, err)
		}
		return DomainPattern{raw: raw, kind: patternExact, value: v}, nil
	}
	if strings.HasPrefix(s, "*.") && !strings.Contains(s[2:], "*") {
		v := s[2:]
		if err := checkDomainChars(v, false); err != nil {
			return DomainPattern{}, fmt.Errorf("invalid domain %q: %v", raw, err)
		}
		return DomainPattern{raw: raw, kind: patternSuffix, value: v}, nil
	}
	if strings.Contains(s, "*") {
		if s == "*" {
			return DomainPattern{}, fmt.Errorf("invalid domain %q: a lone * would match everything; use the mode instead", raw)
		}
		if err := checkDomainChars(s, true); err != nil {
			return DomainPattern{}, fmt.Errorf("invalid domain %q: %v", raw, err)
		}
		return DomainPattern{raw: raw, kind: patternGlob, value: s}, nil
	}
	if err := checkDomainChars(s, false); err != nil {
		return DomainPattern{}, fmt.Errorf("invalid domain %q: %v", raw, err)
	}
	return DomainPattern{raw: raw, kind: patternSuffix, value: s}, nil
}

func checkDomainChars(s string, allowStar bool) error {
	if s == "" {
		return fmt.Errorf("empty name")
	}
	if len(s) > 253 {
		return fmt.Errorf("name too long")
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" {
			return fmt.Errorf("empty label")
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			case r == '*' && allowStar:
			case r > 127:
				// Allow IDNs typed in Unicode; they are compared as typed.
			default:
				return fmt.Errorf("unexpected character %q", r)
			}
		}
	}
	return nil
}

// String returns the rule as typed.
func (p DomainPattern) String() string { return p.raw }

// Match reports whether a fully qualified or relative name matches.
func (p DomainPattern) Match(name string) bool {
	name = NormalizeName(name)
	switch p.kind {
	case patternExact:
		return name == p.value
	case patternSuffix:
		return name == p.value || strings.HasSuffix(name, "."+p.value)
	case patternGlob:
		return globMatch(p.value, name)
	}
	return false
}

// NormalizeName lowercases a DNS name and strips the trailing root dot.
func NormalizeName(name string) string {
	return strings.TrimSuffix(strings.ToLower(name), ".")
}

// GlobMatch matches s against pattern where '*' matches any run of
// characters. It is case sensitive; callers fold case as needed.
func GlobMatch(pattern, s string) bool { return globMatch(pattern, s) }

// globMatch matches pattern against s where '*' matches any (possibly empty)
// sequence of characters, dots included.
func globMatch(pattern, s string) bool {
	px, sx := 0, 0
	starPx, starSx := -1, 0
	for sx < len(s) {
		if px < len(pattern) && pattern[px] == '*' {
			starPx, starSx = px, sx
			px++
			continue
		}
		if px < len(pattern) && pattern[px] == s[sx] {
			px++
			sx++
			continue
		}
		if starPx >= 0 {
			px = starPx + 1
			starSx++
			sx = starSx
			continue
		}
		return false
	}
	for px < len(pattern) && pattern[px] == '*' {
		px++
	}
	return px == len(pattern)
}

// DomainMatcher matches names against a list of patterns.
type DomainMatcher struct {
	patterns []DomainPattern
}

// NewDomainMatcher parses the given rules, skipping invalid ones.
func NewDomainMatcher(rules []string) *DomainMatcher {
	m := &DomainMatcher{}
	for _, r := range rules {
		if p, err := ParseDomainPattern(r); err == nil {
			m.patterns = append(m.patterns, p)
		}
	}
	return m
}

// Empty reports whether there are no patterns.
func (m *DomainMatcher) Empty() bool { return m == nil || len(m.patterns) == 0 }

// Match returns the first matching pattern.
func (m *DomainMatcher) Match(name string) (DomainPattern, bool) {
	if m == nil {
		return DomainPattern{}, false
	}
	for _, p := range m.patterns {
		if p.Match(name) {
			return p, true
		}
	}
	return DomainPattern{}, false
}
