/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package tunnel

import (
	"context"
	"fmt"
	"log"
	"net/netip"
	"strings"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-windows/v3/conf"
	"github.com/amnezia-vpn/amneziawg-windows/v3/tunnel/winipcfg"

	"github.com/amnezia-vpn/amneziawg-windows-client/splittunnel"
)

// pendingEndpoint is a peer whose endpoint host name could not be resolved
// before the tunnel came up.
type pendingEndpoint struct {
	publicKey conf.Key
	host      string
	port      uint16
}

// deferredEndpoints is set when the tunnel started without some endpoints.
type deferredEndpoints struct {
	pending   []pendingEndpoint
	systemDNS []netip.Addr
	stop      chan struct{}
}

// uapiWithDeferredEndpoints builds the UAPI configuration. When endpoint host
// names cannot be resolved, the affected peers start without an endpoint and
// are resolved in the background once the tunnel is up, instead of failing
// the start.
func uapiWithDeferredEndpoints(config *conf.Config) (string, *deferredEndpoints, error) {
	uapi, err := config.ToUAPI()
	if err == nil {
		return uapi, nil, nil
	}
	resolveErr := err
	stripped := *config
	stripped.Peers = append([]conf.Peer(nil), config.Peers...)
	d := &deferredEndpoints{stop: make(chan struct{})}
	for i := range stripped.Peers {
		p := &stripped.Peers[i]
		if p.Endpoint.IsEmpty() {
			continue
		}
		if _, err := netip.ParseAddr(strings.Trim(p.Endpoint.Host, "[]")); err == nil {
			continue
		}
		d.pending = append(d.pending, pendingEndpoint{publicKey: p.PublicKey, host: p.Endpoint.Host, port: p.Endpoint.Port})
		p.Endpoint = conf.Endpoint{}
	}
	if len(d.pending) == 0 {
		return "", nil, resolveErr
	}
	uapi, err = stripped.ToUAPI()
	if err != nil {
		return "", nil, err
	}
	d.systemDNS = currentSystemDNS()
	log.Printf("Unable to resolve %d endpoint(s) (%v); connecting now and resolving in the background", len(d.pending), resolveErr)
	return uapi, d, nil
}

// run resolves the pending endpoints through the regular network's DNS
// servers, which the tunnel service may reach even with the kill switch on,
// and hands them to the device.
func (d *deferredEndpoints) run(dev *device.Device, ourLUID winipcfg.LUID) {
	if d == nil {
		return
	}
	dial := physicalDial(func() winipcfg.LUID { return ourLUID })
	var exchangers []splittunnel.Exchanger
	for _, a := range d.systemDNS {
		exchangers = append(exchangers, splittunnel.NewExchanger(splittunnel.Upstream{Kind: splittunnel.UpstreamPlain, Host: a.String(), Port: 53, Bootstrap: []netip.Addr{a}}, dial))
	}
	go func() {
		backoff := 2 * time.Second
		for len(d.pending) > 0 {
			var remaining []pendingEndpoint
			for _, p := range d.pending {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				addrs, err := splittunnel.LookupHost(ctx, exchangers, p.host, true)
				cancel()
				if err != nil || len(addrs) == 0 {
					remaining = append(remaining, p)
					continue
				}
				endpoint := netip.AddrPortFrom(addrs[0], p.port).String()
				err = dev.IpcSet(fmt.Sprintf("public_key=%s\nendpoint=%s\n", p.publicKey.HexString(), endpoint))
				if err != nil {
					log.Printf("Unable to set deferred endpoint %s: %v", p.host, err)
					remaining = append(remaining, p)
					continue
				}
				log.Printf("Resolved deferred endpoint %s to %s", p.host, endpoint)
			}
			d.pending = remaining
			if len(remaining) == 0 {
				return
			}
			select {
			case <-d.stop:
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 30*time.Second)
			if len(exchangers) == 0 {
				// The network may have come up since; pick up its resolvers.
				for _, a := range currentSystemDNS() {
					exchangers = append(exchangers, splittunnel.NewExchanger(splittunnel.Upstream{Kind: splittunnel.UpstreamPlain, Host: a.String(), Port: 53, Bootstrap: []netip.Addr{a}}, dial))
				}
			}
		}
	}()
}

func (d *deferredEndpoints) close() {
	if d != nil {
		close(d.stop)
	}
}
