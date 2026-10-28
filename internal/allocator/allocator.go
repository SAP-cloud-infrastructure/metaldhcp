// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

// Package allocator provides a concurrency-safe, in-memory IPv4 allocator backed by
// the DHCPLease ledger. The oob plugin seeds it from existing DHCPLease objects on
// startup (Restore) and reserves addresses on each request (Allocate); the onboarding
// plugin durably persists each reservation as a DHCPLease. Expired dynamic leases are
// reclaimed lazily during allocation once their expiry plus a configured grace has passed.
package allocator

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// entry is a MAC's current assignment. expiresAt is the lease expiry for a dynamic entry;
// the zero time means the entry never expires — static reservations, and dynamic entries
// whose pool defines no lease time, are never reclaimed.
type entry struct {
	ip        net.IP
	expiresAt time.Time
}

// Allocator tracks MAC-to-IP assignments across all subnets. byIP is the reverse index and
// enforces one MAC per IP. reserved holds static bindings, which are excluded from expiry
// and reclamation. All exported methods are safe for concurrent use.
type Allocator struct {
	mu       sync.Mutex
	byMAC    map[string]entry
	byIP     map[string]string // IP string -> MAC key
	reserved map[string]struct{}
	grace    time.Duration
	now      func() time.Time
}

// New returns an allocator that reclaims a dynamic lease only after its expiry plus grace
// has elapsed.
func New(grace time.Duration) *Allocator {
	return &Allocator{
		byMAC:    make(map[string]entry),
		byIP:     make(map[string]string),
		reserved: make(map[string]struct{}),
		grace:    grace,
		now:      time.Now,
	}
}

// set records macKey -> ip with the given expiry, maintaining the byIP reverse index and
// dropping any previous index entry for this MAC. Caller holds a.mu.
func (a *Allocator) set(macKey string, ip net.IP, expiresAt time.Time) {
	if old, ok := a.byMAC[macKey]; ok {
		delete(a.byIP, old.ip.String())
	}
	a.byMAC[macKey] = entry{ip: ip, expiresAt: expiresAt}
	a.byIP[ip.String()] = macKey
}

// Reserve permanently pins macKey to ip. The binding has no expiry and is never reclaimed or
// handed to another MAC. Subsequent Restore calls for the same macKey are ignored so the
// DHCPLease ledger cannot overwrite a static config binding on restart.
func (a *Allocator) Reserve(macKey string, ip net.IP) {
	ip = normalize(ip)
	if ip == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.set(macKey, ip, time.Time{})
	a.reserved[macKey] = struct{}{}
}

// Restore seeds a MAC->IP assignment from a persisted DHCPLease together with its lease
// expiry (zero time when the lease records none). Reserved MACs are skipped so static config
// bindings take precedence. If the IP is already held by a different MAC, Restore refuses and
// returns false rather than double-mapping the address; the caller resolves the duplicate in
// the ledger. Returns true when the assignment was recorded.
func (a *Allocator) Restore(macKey string, ip net.IP, expiresAt time.Time) bool {
	ip = normalize(ip)
	if ip == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.reserved[macKey]; ok {
		return false
	}
	if holder, ok := a.byIP[ip.String()]; ok && holder != macKey {
		return false
	}
	a.set(macKey, ip, expiresAt)
	return true
}

// Pool describes the usable address space of an OOBSubnet.
type Pool struct {
	CIDR          string
	Gateway       string        // optional, excluded from allocation
	RangeStart    string        // optional lower bound (inclusive)
	RangeEnd      string        // optional upper bound (inclusive)
	BootURL       string        // optional UEFI HTTP boot URL sent as DHCP BootFileName
	LeaseTime     time.Duration // optional per-pool override; 0 means use the global lease_time plugin
	DNSServers    []net.IP      // optional per-subnet DNS servers; nil means use global
	Domain        string        // optional per-subnet search domain; "" means use global
	Mode          string        // MAC filter posture: allowAll | allow | deny ("" == allowAll)
	MacList       []string      // MAC set the mode operates on; normalized before matching
	ResponseDelay time.Duration // when non-zero, sleep this long before sending the offer
}

// Allocate returns the IPv4 address for macKey within pool, plus the MAC key of any expired
// dynamic lease evicted to make room ("" when none). It returns the MAC's existing in-CIDR
// assignment and refreshes its expiry (idempotent re-lease), honors requestedIP when exact is
// set and the address is free, and otherwise assigns the first free host — reclaiming an
// expired dynamic lease only when no never-used address remains. It errors when the pool is
// exhausted (no free and no reclaimable address) or misconfigured.
func (a *Allocator) Allocate(pool Pool, macKey string, requestedIP net.IP, exact bool) (net.IP, string, error) {
	_, cidrNet, err := net.ParseCIDR(pool.CIDR)
	if err != nil {
		return nil, "", fmt.Errorf("invalid CIDR %q: %w", pool.CIDR, err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	now := a.now()
	exp := a.expiryFor(pool, now)

	if existing, ok := a.byMAC[macKey]; ok && cidrNet.Contains(existing.ip) {
		if _, static := a.reserved[macKey]; !static {
			a.byMAC[macKey] = entry{ip: existing.ip, expiresAt: exp} // refresh expiry on re-lease
		}
		return existing.ip, "", nil
	}

	if exact {
		if req := normalize(requestedIP); req != nil && cidrNet.Contains(req) {
			if _, used := a.byIP[req.String()]; !used {
				a.set(macKey, req, exp)
				return req, "", nil
			}
		}
	}

	ip, evicted, err := a.firstFree(cidrNet, pool, now)
	if err != nil {
		return nil, "", err
	}
	a.set(macKey, ip, exp)
	return ip, evicted, nil
}

// expiryFor returns the expiry to stamp on a dynamic assignment from pool, or the zero time
// when the pool defines no lease time (such entries never expire and are never reclaimed).
func (a *Allocator) expiryFor(pool Pool, now time.Time) time.Time {
	if pool.LeaseTime <= 0 {
		return time.Time{}
	}
	return now.Add(pool.LeaseTime)
}

// firstFree scans the usable host range of cidrNet (narrowed by the optional
// RangeStart/RangeEnd), excluding the network, broadcast and gateway addresses. It returns
// the first never-used address if any; otherwise it reclaims the stalest dynamic lease whose
// expiry plus grace has passed, evicting it and returning its MAC key so the caller can delete
// the stale record. The caller holds a.mu.
func (a *Allocator) firstFree(cidrNet *net.IPNet, pool Pool, now time.Time) (net.IP, string, error) {
	network := cidrNet.IP.Mask(cidrNet.Mask).To4()
	if network == nil {
		return nil, "", fmt.Errorf("CIDR %q is not IPv4", pool.CIDR)
	}
	broadcast := broadcastAddr(cidrNet)
	gateway := normalize(net.ParseIP(pool.Gateway))

	lo := nextIP(network) // first usable host
	if start := normalize(net.ParseIP(pool.RangeStart)); start != nil && cidrNet.Contains(start) && !start.Equal(network) {
		lo = start
	}
	hi := prevIP(broadcast) // last usable host
	if end := normalize(net.ParseIP(pool.RangeEnd)); end != nil && cidrNet.Contains(end) && !end.Equal(broadcast) {
		hi = end
	}

	var reclaimIP net.IP
	var reclaimMAC string
	var reclaimExp time.Time

	for ip := cloneIP(lo); compareIP(ip, hi) <= 0; ip = nextIP(ip) {
		if gateway != nil && ip.Equal(gateway) {
			continue
		}
		s := ip.String()
		holder, used := a.byIP[s]
		if !used {
			return cloneIP(ip), "", nil // prefer a never-used address
		}
		if _, static := a.reserved[holder]; static {
			continue
		}
		e := a.byMAC[holder]
		if e.expiresAt.IsZero() || !now.After(e.expiresAt.Add(a.grace)) {
			continue // live or non-expiring: not reclaimable
		}
		if reclaimMAC == "" || e.expiresAt.Before(reclaimExp) {
			reclaimIP, reclaimMAC, reclaimExp = cloneIP(ip), holder, e.expiresAt // stalest wins
		}
	}

	if reclaimMAC != "" {
		delete(a.byMAC, reclaimMAC)
		delete(a.byIP, reclaimIP.String())
		return reclaimIP, reclaimMAC, nil
	}
	return nil, "", fmt.Errorf("no free address in pool %s", pool.CIDR)
}

func broadcastAddr(cidrNet *net.IPNet) net.IP {
	ip := cidrNet.IP.Mask(cidrNet.Mask).To4()
	mask := cidrNet.Mask
	bc := make(net.IP, len(ip))
	for i := range ip {
		bc[i] = ip[i] | ^mask[i]
	}
	return bc
}

func normalize(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4
	}
	return nil
}

func cloneIP(ip net.IP) net.IP {
	out := make(net.IP, len(ip))
	copy(out, ip)
	return out
}

func nextIP(ip net.IP) net.IP {
	out := cloneIP(ip.To4())
	for i := len(out) - 1; i >= 0; i-- {
		out[i]++
		if out[i] != 0 {
			break
		}
	}
	return out
}

func prevIP(ip net.IP) net.IP {
	out := cloneIP(ip.To4())
	for i := len(out) - 1; i >= 0; i-- {
		if out[i] != 0 {
			out[i]--
			break
		}
		out[i] = 0xff
	}
	return out
}

func compareIP(a, b net.IP) int {
	a4, b4 := a.To4(), b.To4()
	for i := range 4 {
		switch {
		case a4[i] < b4[i]:
			return -1
		case a4[i] > b4[i]:
			return 1
		}
	}
	return 0
}
