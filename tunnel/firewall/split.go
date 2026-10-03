/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package firewall

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Options extends the classic WireGuard firewall with split tunneling rules.
type Options struct {
	// LockDNS blocks plain DNS to every server except DNSAllow, even when
	// the tunnel does not route everything. Used to force applications
	// through the DNS forwarder.
	LockDNS bool
	// DNSAllow lists DNS servers (the forwarder) that stay reachable
	// whenever DNS is restricted.
	DNSAllow []net.IP
	// PermitPrefixes are remote ranges that may be reached outside the
	// tunnel while the kill switch is active (excluded IPs, LAN).
	PermitPrefixes []netip.Prefix
	// VPNOnlyApps may only communicate through the tunnel interface.
	VPNOnlyApps []string
	// BlockedApps may not communicate at all while the tunnel is up.
	BlockedApps []string
	// BypassApps are allowed outside the tunnel by the kill switch; the
	// split tunnel driver keeps them out of the tunnel.
	BypassApps []string
}

const (
	weightServicePermit = 15
	weightAppBlock      = 14
	weightBypassApp     = 13
	weightPermitPrefix  = 12
)

var (
	dynamicMu      sync.Mutex
	dynamicBase    *baseObjects
	dynamicPermits = make(map[netip.Prefix]bool)
)

func addFilterAllLayers(session uintptr, filter *wtFwpmFilter0, name string, v4, v6 bool) error {
	layers := []struct {
		key  windows.GUID
		desc string
		v6   bool
	}{
		{cFWPM_LAYER_ALE_AUTH_CONNECT_V4, "outbound IPv4", false},
		{cFWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4, "inbound IPv4", false},
		{cFWPM_LAYER_ALE_AUTH_CONNECT_V6, "outbound IPv6", true},
		{cFWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V6, "inbound IPv6", true},
	}
	for _, l := range layers {
		if (l.v6 && !v6) || (!l.v6 && !v4) {
			continue
		}
		displayData, err := createWtFwpmDisplayData0(fmt.Sprintf("%s (%s)", name, l.desc), "")
		if err != nil {
			return wrapErr(err)
		}
		filter.displayData = *displayData
		filter.layerKey = l.key
		filterID := uint64(0)
		if err := fwpmFilterAdd0(session, filter, 0, &filterID); err != nil {
			return wrapErr(err)
		}
	}
	return nil
}

// appID converts an executable path to a WFP application identifier. The
// returned blob must be freed with fwpmFreeMemory0.
func appID(path string) (*wtFwpByteBlob, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var id *wtFwpByteBlob
	if err := fwpmGetAppIdFromFileName0(p, unsafe.Pointer(&id)); err != nil {
		return nil, err
	}
	return id, nil
}

func appCondition(id *wtFwpByteBlob) wtFwpmFilterCondition0 {
	return wtFwpmFilterCondition0{
		fieldKey:  cFWPM_CONDITION_ALE_APP_ID,
		matchType: cFWP_MATCH_EQUAL,
		conditionValue: wtFwpConditionValue0{
			_type: cFWP_BYTE_BLOB_TYPE,
			value: uintptr(unsafe.Pointer(id)),
		},
	}
}

func notLoopbackCondition() wtFwpmFilterCondition0 {
	return wtFwpmFilterCondition0{
		fieldKey:  cFWPM_CONDITION_FLAGS,
		matchType: cFWP_MATCH_FLAGS_NONE_SET,
		conditionValue: wtFwpConditionValue0{
			_type: cFWP_UINT32,
			value: uintptr(cFWP_CONDITION_FLAG_IS_LOOPBACK),
		},
	}
}

// installAppRules adds the per-application filters. Applications that cannot
// be resolved are logged and skipped so one stale path does not take the
// tunnel down.
func installAppRules(session uintptr, bo *baseObjects, luid uint64, opts *Options) error {
	ifLUID := luid
	for _, path := range opts.VPNOnlyApps {
		id, err := appID(path)
		if err != nil {
			log.Printf("Split tunneling: skipping VPN-only rule for %s: %v", path, err)
			continue
		}
		conditions := []wtFwpmFilterCondition0{
			appCondition(id),
			notLoopbackCondition(),
			{
				fieldKey:  cFWPM_CONDITION_IP_LOCAL_INTERFACE,
				matchType: cFWP_MATCH_NOT_EQUAL,
				conditionValue: wtFwpConditionValue0{
					_type: cFWP_UINT64,
					value: uintptr(unsafe.Pointer(&ifLUID)),
				},
			},
		}
		filter := wtFwpmFilter0{
			providerKey:         &bo.provider,
			subLayerKey:         bo.filters,
			weight:              filterWeight(weightAppBlock),
			numFilterConditions: uint32(len(conditions)),
			filterCondition:     &conditions[0],
			action:              wtFwpmAction0{_type: cFWP_ACTION_BLOCK},
		}
		err = addFilterAllLayers(session, &filter, "Block traffic outside the tunnel for "+path, true, true)
		runtime.KeepAlive(&ifLUID)
		fwpmFreeMemory0(unsafe.Pointer(&id))
		if err != nil {
			return err
		}
	}
	for _, path := range opts.BlockedApps {
		id, err := appID(path)
		if err != nil {
			log.Printf("Split tunneling: skipping block rule for %s: %v", path, err)
			continue
		}
		conditions := []wtFwpmFilterCondition0{appCondition(id), notLoopbackCondition()}
		filter := wtFwpmFilter0{
			providerKey:         &bo.provider,
			subLayerKey:         bo.filters,
			weight:              filterWeight(weightAppBlock),
			numFilterConditions: uint32(len(conditions)),
			filterCondition:     &conditions[0],
			action:              wtFwpmAction0{_type: cFWP_ACTION_BLOCK},
		}
		err = addFilterAllLayers(session, &filter, "Block all traffic for "+path, true, true)
		fwpmFreeMemory0(unsafe.Pointer(&id))
		if err != nil {
			return err
		}
	}
	for _, path := range opts.BypassApps {
		id, err := appID(path)
		if err != nil {
			log.Printf("Split tunneling: skipping bypass permit for %s: %v", path, err)
			continue
		}
		conditions := []wtFwpmFilterCondition0{appCondition(id)}
		filter := wtFwpmFilter0{
			providerKey:         &bo.provider,
			subLayerKey:         bo.filters,
			weight:              filterWeight(weightBypassApp),
			numFilterConditions: uint32(len(conditions)),
			filterCondition:     &conditions[0],
			action:              wtFwpmAction0{_type: cFWP_ACTION_PERMIT},
		}
		err = addFilterAllLayers(session, &filter, "Permit traffic outside the tunnel for "+path, true, true)
		fwpmFreeMemory0(unsafe.Pointer(&id))
		if err != nil {
			return err
		}
	}
	return nil
}

// permitPrefix adds a permit filter for a remote address range.
func permitPrefix(session uintptr, bo *baseObjects, p netip.Prefix) error {
	p = p.Masked()
	condition := wtFwpmFilterCondition0{
		fieldKey:  cFWPM_CONDITION_IP_REMOTE_ADDRESS,
		matchType: cFWP_MATCH_EQUAL,
	}
	var v4 wtFwpV4AddrAndMask
	var v6 wtFwpV6AddrAndMask
	if p.Addr().Is4() {
		a := p.Addr().As4()
		v4.addr = binary.BigEndian.Uint32(a[:])
		if p.Bits() > 0 {
			v4.mask = ^uint32(0) << (32 - p.Bits())
		}
		condition.conditionValue = wtFwpConditionValue0{_type: cFWP_V4_ADDR_MASK, value: uintptr(unsafe.Pointer(&v4))}
	} else {
		v6.addr = p.Addr().As16()
		v6.prefixLength = uint8(p.Bits())
		condition.conditionValue = wtFwpConditionValue0{_type: cFWP_V6_ADDR_MASK, value: uintptr(unsafe.Pointer(&v6))}
	}
	filter := wtFwpmFilter0{
		providerKey:         &bo.provider,
		subLayerKey:         bo.filters,
		weight:              filterWeight(weightPermitPrefix),
		numFilterConditions: 1,
		filterCondition:     &condition,
		action:              wtFwpmAction0{_type: cFWP_ACTION_PERMIT},
	}
	err := addFilterAllLayers(session, &filter, "Permit split tunnel destination "+p.String(), p.Addr().Is4(), p.Addr().Is6())
	runtime.KeepAlive(&v4)
	runtime.KeepAlive(&v6)
	return err
}

// PermitRemoteAddrs allows addresses learned at runtime (e.g. from DNS answers
// for excluded domains) to pass the kill switch. It is a no-op when the kill
// switch is not active.
func PermitRemoteAddrs(addrs []netip.Addr) error {
	dynamicMu.Lock()
	defer dynamicMu.Unlock()
	if wfpSession == 0 || dynamicBase == nil {
		return nil
	}
	var errs []error
	for _, a := range addrs {
		p := netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen())
		if dynamicPermits[p] {
			continue
		}
		if err := permitPrefix(wfpSession, dynamicBase, p); err != nil {
			errs = append(errs, err)
			continue
		}
		dynamicPermits[p] = true
	}
	return errors.Join(errs...)
}

func resetDynamic() {
	dynamicMu.Lock()
	sublayerKey = nil
	dynamicBase = nil
	dynamicPermits = make(map[netip.Prefix]bool)
	dynamicMu.Unlock()
}

// SublayerKey returns the GUID of the active filter sublayer, so that the
// split tunnel driver can place its filters next to ours.
func SublayerKey() (windows.GUID, bool) {
	dynamicMu.Lock()
	defer dynamicMu.Unlock()
	if sublayerKey == nil {
		return windows.GUID{}, false
	}
	return *sublayerKey, true
}

var sublayerKey *windows.GUID
