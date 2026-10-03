/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package firewall

import (
	"net/netip"
	"sync"
	"unsafe"
)

// Lockdown is a global kill switch used by the manager service while no
// tunnel is connected. It lives in its own dynamic WFP session, so it is
// lifted automatically if the manager exits.
var (
	lockdownMu      sync.Mutex
	lockdownSession uintptr
)

var lanPrefixes = []netip.Prefix{
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

// permitOwnExecutable lets every process of our executable through, which
// covers the tunnel services that need to reach their endpoints.
func permitOwnExecutable(session uintptr, bo *baseObjects, weight uint8) error {
	appID, err := getCurrentProcessAppID()
	if err != nil {
		return wrapErr(err)
	}
	defer fwpmFreeMemory0(unsafe.Pointer(&appID))
	condition := appCondition(appID)
	filter := wtFwpmFilter0{
		providerKey:         &bo.provider,
		subLayerKey:         bo.filters,
		weight:              filterWeight(weight),
		numFilterConditions: 1,
		filterCondition:     &condition,
		action:              wtFwpmAction0{_type: cFWP_ACTION_PERMIT},
	}
	return addFilterAllLayers(session, &filter, "Permit AmneziaWG during lockdown", true, true)
}

// EnableLockdown blocks all traffic except loopback, DHCP, neighbor
// discovery, our own executable and, optionally, the local network.
func EnableLockdown(allowLAN bool) error {
	lockdownMu.Lock()
	defer lockdownMu.Unlock()
	if lockdownSession != 0 {
		return nil
	}
	session, err := createWfpSession()
	if err != nil {
		return err
	}
	installer := func(session uintptr) error {
		bo, err := registerBaseObjects(session)
		if err != nil {
			return err
		}
		if err := permitOwnExecutable(session, bo, 15); err != nil {
			return err
		}
		if err := permitLoopback(session, bo, 13); err != nil {
			return err
		}
		if err := permitDHCPIPv4(session, bo, 12); err != nil {
			return err
		}
		if err := permitDHCPIPv6(session, bo, 12); err != nil {
			return err
		}
		if err := permitNdp(session, bo, 12); err != nil {
			return err
		}
		if allowLAN {
			for _, p := range lanPrefixes {
				if err := permitPrefix(session, bo, p); err != nil {
					return err
				}
			}
		}
		return blockAll(session, bo, 0)
	}
	if err := runTransaction(session, installer); err != nil {
		fwpmEngineClose0(session)
		return wrapErr(err)
	}
	lockdownSession = session
	return nil
}

// DisableLockdown removes the lockdown filters.
func DisableLockdown() {
	lockdownMu.Lock()
	defer lockdownMu.Unlock()
	if lockdownSession != 0 {
		fwpmEngineClose0(lockdownSession)
		lockdownSession = 0
	}
}

// LockdownActive reports whether lockdown filters are installed.
func LockdownActive() bool {
	lockdownMu.Lock()
	defer lockdownMu.Unlock()
	return lockdownSession != 0
}
