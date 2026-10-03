/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package splittunnel

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"

	"golang.org/x/net/dns/dnsmessage"
)

// LookupHost resolves host to addresses using the given exchangers, asking
// for A and, when wantV6 is set, AAAA records in parallel. IPv4 results are
// returned first.
func LookupHost(ctx context.Context, exchangers []Exchanger, host string, wantV6 bool) ([]netip.Addr, error) {
	if a, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return []netip.Addr{a.Unmap()}, nil
	}
	if len(exchangers) == 0 {
		return nil, errors.New("no DNS servers available")
	}
	name, err := dnsmessage.NewName(strings.TrimSuffix(host, ".") + ".")
	if err != nil {
		return nil, fmt.Errorf("invalid host name %q", host)
	}
	types := []dnsmessage.Type{dnsmessage.TypeA}
	if wantV6 {
		types = append(types, dnsmessage.TypeAAAA)
	}
	results := make([][]netip.Addr, len(types))
	errs := make([]error, len(types))
	var wg sync.WaitGroup
	for i, t := range types {
		wg.Add(1)
		go func(i int, t dnsmessage.Type) {
			defer wg.Done()
			results[i], errs[i] = lookupType(ctx, exchangers, name, t)
		}(i, t)
	}
	wg.Wait()
	var out []netip.Addr
	for _, r := range results {
		out = append(out, r...)
	}
	if len(out) == 0 {
		if err := errors.Join(errs...); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("no such host %s", host)
	}
	return out, nil
}

func lookupType(ctx context.Context, exchangers []Exchanger, name dnsmessage.Name, t dnsmessage.Type) ([]netip.Addr, error) {
	var id [2]byte
	rand.Read(id[:])
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: uint16(id[0])<<8 | uint16(id[1]), RecursionDesired: true})
	b.StartQuestions()
	b.Question(dnsmessage.Question{Name: name, Type: t, Class: dnsmessage.ClassINET})
	query, err := b.Finish()
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, ex := range exchangers {
		resp, err := ex.Exchange(ctx, query)
		if err != nil {
			lastErr = err
			continue
		}
		a, err := ParseAnswer(resp)
		if err != nil {
			lastErr = err
			continue
		}
		var out []netip.Addr
		for _, addr := range a.Addrs {
			if (t == dnsmessage.TypeA) == addr.Is4() {
				out = append(out, addr)
			}
		}
		return out, nil
	}
	return nil, lastErr
}
