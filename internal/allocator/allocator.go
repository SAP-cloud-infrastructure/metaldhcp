// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

// Package allocator provides a concurrency-safe, in-memory IPv4 allocator backed by
// the DHCPLease ledger. The oob plugin seeds it from existing DHCPLease objects on
// startup (Restore) and reserves addresses on each request (Allocate); the onboarding
// plugin durably persists each reservation as a DHCPLease.
package allocator

import (
	"fmt"
	"net"
	"sync"
)

// Allocator tracks MAC-to-IP assignments and the set of in-use addresses across all
// subnets. All exported methods are safe for concurrent use.
type Allocator struct {
	mu    sync.Mutex
	byMAC map[string]net.IP   // normalized MAC key -> assigned IP
	inUse map[string]struct{} // IP string -> reserved
}

func New() *Allocator {
	return &Allocator{
		byMAC: make(map[string]net.IP),
		inUse: make(map[string]struct{}),
	}
}

// Restore seeds the allocator with an existing MAC->IP assignment, e.g. from a
// persisted DHCPLease. Safe to call repeatedly during startup.
func (a *Allocator) Restore(macKey string, ip net.IP) {
	ip = normalize(ip)
	if ip == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.byMAC[macKey] = ip
	a.inUse[ip.String()] = struct{}{}
}

// Pool describes the usable address space of an OOBSubnet.
type Pool struct {
	CIDR       string
	Gateway    string // optional, excluded from allocation
	RangeStart string // optional lower bound (inclusive)
	RangeEnd   string // optional upper bound (inclusive)
	BootURL    string // optional UEFI HTTP boot URL sent as DHCP BootFileName
}

// Allocate returns the IPv4 address for macKey within pool. It returns the MAC's
// existing in-CIDR assignment if present (idempotent re-lease), honors requestedIP
// when exact is set and the address is free (or already this MAC's), and otherwise
// assigns the first free host address in the pool. It errors when the pool is
// exhausted or misconfigured.
func (a *Allocator) Allocate(pool Pool, macKey string, requestedIP net.IP, exact bool) (net.IP, error) {
	_, cidrNet, err := net.ParseCIDR(pool.CIDR)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", pool.CIDR, err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if existing, ok := a.byMAC[macKey]; ok && cidrNet.Contains(existing) {
		return existing, nil
	}

	if exact {
		if req := normalize(requestedIP); req != nil && cidrNet.Contains(req) {
			if _, used := a.inUse[req.String()]; !used {
				a.assign(macKey, req)
				return req, nil
			}
		}
	}

	ip, err := a.firstFree(cidrNet, pool)
	if err != nil {
		return nil, err
	}
	a.assign(macKey, ip)
	return ip, nil
}

func (a *Allocator) assign(macKey string, ip net.IP) {
	a.byMAC[macKey] = ip
	a.inUse[ip.String()] = struct{}{}
}

// firstFree scans the usable host range of cidrNet (narrowed by the optional
// RangeStart/RangeEnd) and returns the first address not already in use, excluding
// the network, broadcast and gateway addresses. The caller must hold a.mu.
func (a *Allocator) firstFree(cidrNet *net.IPNet, pool Pool) (net.IP, error) {
	network := cidrNet.IP.Mask(cidrNet.Mask).To4()
	if network == nil {
		return nil, fmt.Errorf("CIDR %q is not IPv4", pool.CIDR)
	}
	broadcast := broadcastAddr(cidrNet)
	gateway := normalize(net.ParseIP(pool.Gateway))

	lo := nextIP(network) // first usable host
	if start := normalize(net.ParseIP(pool.RangeStart)); start != nil && cidrNet.Contains(start) {
		lo = start
	}
	hi := prevIP(broadcast) // last usable host
	if end := normalize(net.ParseIP(pool.RangeEnd)); end != nil && cidrNet.Contains(end) {
		hi = end
	}

	for ip := cloneIP(lo); compareIP(ip, hi) <= 0; ip = nextIP(ip) {
		s := ip.String()
		if gateway != nil && ip.Equal(gateway) {
			continue
		}
		if _, used := a.inUse[s]; used {
			continue
		}
		return cloneIP(ip), nil
	}
	return nil, fmt.Errorf("no free address in pool %s", pool.CIDR)
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
