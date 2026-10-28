// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package allocator

import (
	"net"
	"testing"
	"time"
)

func ip(s string) net.IP { return net.ParseIP(s) }

// atTime pins the allocator's clock for deterministic expiry/reclamation tests.
func atTime(a *Allocator, t time.Time) { a.now = func() time.Time { return t } }

func TestAllocateFirstFree(t *testing.T) {
	tests := []struct {
		name string
		pool Pool
		want string
	}{
		{
			name: "first usable host excludes network and gateway at .1",
			pool: Pool{CIDR: "10.0.1.0/24", Gateway: "10.0.1.1"},
			want: "10.0.1.2",
		},
		{
			name: "no gateway yields first usable host",
			pool: Pool{CIDR: "10.0.1.0/24"},
			want: "10.0.1.1",
		},
		{
			name: "rangeStart windows the lower bound",
			pool: Pool{CIDR: "10.0.1.0/24", RangeStart: "10.0.1.100"},
			want: "10.0.1.100",
		},
		{
			name: "gateway inside range is skipped",
			pool: Pool{CIDR: "10.0.1.0/24", Gateway: "10.0.1.100", RangeStart: "10.0.1.100"},
			want: "10.0.1.101",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := New(0)
			got, _, err := a.Allocate(tt.pool, "aa", nil, false)
			if err != nil {
				t.Fatalf("Allocate: %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestAllocateSkipsInUse(t *testing.T) {
	a := New(0)
	// Occupy .1 and .2 via other MACs; .3 is the first free host.
	if got, _, _ := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "m1", nil, false); got.String() != "10.0.1.1" {
		t.Fatalf("m1 got %s", got)
	}
	if got, _, _ := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "m2", nil, false); got.String() != "10.0.1.2" {
		t.Fatalf("m2 got %s", got)
	}
	got, _, err := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "m3", nil, false)
	if err != nil {
		t.Fatalf("m3: %v", err)
	}
	if got.String() != "10.0.1.3" {
		t.Fatalf("m3 got %s, want 10.0.1.3", got)
	}
}

func TestAllocateRangeEndWindowing(t *testing.T) {
	a := New(0)
	pool := Pool{CIDR: "10.0.1.0/24", RangeStart: "10.0.1.10", RangeEnd: "10.0.1.11"}
	first, _, err := a.Allocate(pool, "m1", nil, false)
	if err != nil || first.String() != "10.0.1.10" {
		t.Fatalf("first: %s err %v", first, err)
	}
	second, _, err := a.Allocate(pool, "m2", nil, false)
	if err != nil || second.String() != "10.0.1.11" {
		t.Fatalf("second: %s err %v", second, err)
	}
	if _, _, err := a.Allocate(pool, "m3", nil, false); err == nil {
		t.Fatalf("expected exhaustion error once window is full")
	}
}

func TestReLeaseReturnsSameIP(t *testing.T) {
	a := New(0)
	pool := Pool{CIDR: "10.0.1.0/24"}
	first, _, err := a.Allocate(pool, "mac", nil, false)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, _, err := a.Allocate(pool, "mac", nil, false)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !first.Equal(second) {
		t.Fatalf("re-lease changed IP: %s != %s", first, second)
	}
}

func TestReLeaseAfterRestore(t *testing.T) {
	a := New(0)
	a.Restore("mac", ip("10.0.1.50"), time.Time{})
	got, _, err := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "mac", nil, false)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if got.String() != "10.0.1.50" {
		t.Fatalf("got %s, want restored 10.0.1.50", got)
	}
}

func TestReLeaseIgnoredWhenExistingOutOfCIDR(t *testing.T) {
	a := New(0)
	a.Restore("mac", ip("192.168.0.9"), time.Time{})
	// Existing assignment is not within the requested pool CIDR, so a fresh
	// address from the pool is handed out instead.
	got, _, err := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "mac", nil, false)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if got.String() != "10.0.1.1" {
		t.Fatalf("got %s, want 10.0.1.1", got)
	}
}

func TestExactIPHonoredWhenFree(t *testing.T) {
	a := New(0)
	got, _, err := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "mac", ip("10.0.1.77"), true)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if got.String() != "10.0.1.77" {
		t.Fatalf("got %s, want exact 10.0.1.77", got)
	}
}

func TestExactIPRejectedWhenTakenByAnotherMAC(t *testing.T) {
	a := New(0)
	if _, _, err := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "owner", ip("10.0.1.5"), true); err != nil {
		t.Fatalf("owner: %v", err)
	}
	// Another MAC requesting the same address falls back to first-free.
	got, _, err := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "other", ip("10.0.1.5"), true)
	if err != nil {
		t.Fatalf("other: %v", err)
	}
	if got.String() == "10.0.1.5" {
		t.Fatalf("exact IP should not be reassigned to another MAC")
	}
	if got.String() != "10.0.1.1" {
		t.Fatalf("got %s, want first-free 10.0.1.1", got)
	}
}

func TestExactIPAlreadyThisMAC(t *testing.T) {
	a := New(0)
	first, _, err := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "mac", ip("10.0.1.5"), true)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, _, err := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "mac", ip("10.0.1.5"), true)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !first.Equal(second) || second.String() != "10.0.1.5" {
		t.Fatalf("got %s then %s, want stable 10.0.1.5", first, second)
	}
}

func TestExactIPOutOfCIDRFallsToFirstFree(t *testing.T) {
	a := New(0)
	got, _, err := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "mac", ip("172.16.0.5"), true)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if got.String() != "10.0.1.1" {
		t.Fatalf("got %s, want first-free 10.0.1.1", got)
	}
}

func TestSlash30(t *testing.T) {
	// 10.0.0.0/30: network .0, broadcast .3, usable hosts .1 and .2.
	a := New(0)
	pool := Pool{CIDR: "10.0.0.0/30"}
	first, _, err := a.Allocate(pool, "m1", nil, false)
	if err != nil || first.String() != "10.0.0.1" {
		t.Fatalf("first: %s err %v", first, err)
	}
	second, _, err := a.Allocate(pool, "m2", nil, false)
	if err != nil || second.String() != "10.0.0.2" {
		t.Fatalf("second: %s err %v", second, err)
	}
	if _, _, err := a.Allocate(pool, "m3", nil, false); err == nil {
		t.Fatalf("expected exhaustion on /30 after two hosts")
	}
}

func TestSlash30GatewayExcluded(t *testing.T) {
	a := New(0)
	pool := Pool{CIDR: "10.0.0.0/30", Gateway: "10.0.0.1"}
	first, _, err := a.Allocate(pool, "m1", nil, false)
	if err != nil || first.String() != "10.0.0.2" {
		t.Fatalf("first: %s err %v", first, err)
	}
	if _, _, err := a.Allocate(pool, "m2", nil, false); err == nil {
		t.Fatalf("expected exhaustion: only .2 is usable with gateway at .1")
	}
}

func TestSlash31(t *testing.T) {
	// 10.0.0.0/31: network .0, broadcast .1. firstFree scans from nextIP(network)
	// up to prevIP(broadcast), i.e. .1 down to .0, an empty range -> exhaustion.
	a := New(0)
	if _, _, err := a.Allocate(Pool{CIDR: "10.0.0.0/31"}, "m1", nil, false); err == nil {
		t.Fatalf("expected no usable host in /31")
	}
}

func TestExhaustion(t *testing.T) {
	a := New(0)
	pool := Pool{CIDR: "10.0.0.0/29", Gateway: "10.0.0.1"}
	// Usable hosts .1...6; gateway .1 excluded -> .2..6 = 5 addresses.
	for i := range 5 {
		if _, _, err := a.Allocate(pool, netKey(i), nil, false); err != nil {
			t.Fatalf("alloc %d: %v", i, err)
		}
	}
	if _, _, err := a.Allocate(pool, "overflow", nil, false); err == nil {
		t.Fatalf("expected exhaustion after draining /29")
	}
}

func TestInvalidCIDR(t *testing.T) {
	a := New(0)
	if _, _, err := a.Allocate(Pool{CIDR: "not-a-cidr"}, "mac", nil, false); err == nil {
		t.Fatalf("expected error for invalid CIDR")
	}
}

func TestNonIPv4CIDR(t *testing.T) {
	a := New(0)
	if _, _, err := a.Allocate(Pool{CIDR: "2001:db8::/64"}, "mac", nil, false); err == nil {
		t.Fatalf("expected error for non-IPv4 CIDR")
	}
}

func netKey(i int) string {
	return string(rune('a' + i))
}

func TestReserve(t *testing.T) {
	const (
		staticMAC = "m-static"
		otherMAC  = "m-other"
		pool      = "10.0.1.0/24"
	)
	staticIP := ip("10.0.1.50")

	t.Run("reserved MAC always gets its static IP", func(t *testing.T) {
		a := New(0)
		a.Reserve(staticMAC, staticIP)
		got, _, err := a.Allocate(Pool{CIDR: pool}, staticMAC, nil, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.Equal(staticIP) {
			t.Fatalf("got %s, want %s", got, staticIP)
		}
	})

	t.Run("static IP is not reassigned to other MACs", func(t *testing.T) {
		a := New(0)
		a.Reserve(staticMAC, staticIP)
		got, _, err := a.Allocate(Pool{CIDR: pool}, otherMAC, nil, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Equal(staticIP) {
			t.Fatalf("static IP %s was handed to another MAC", staticIP)
		}
	})

	t.Run("static IP outside pool CIDR is reserved and in-use", func(t *testing.T) {
		a := New(0)
		outsideIP := ip("192.168.1.1")
		a.Reserve(staticMAC, outsideIP)
		// dynamic allocation in the pool should not hand out the outside IP
		got, _, err := a.Allocate(Pool{CIDR: pool}, otherMAC, outsideIP, true)
		if err != nil {
			// outside CIDR, exact-IP rejected — that's fine
			return
		}
		if got.Equal(outsideIP) {
			t.Fatalf("IP outside pool CIDR should not be handed out via dynamic allocation")
		}
	})

	t.Run("Restore does not overwrite a static binding", func(t *testing.T) {
		a := New(0)
		a.Reserve(staticMAC, staticIP)
		// simulate a stale DHCPLease seeding a different IP for the same MAC
		a.Restore(staticMAC, ip("10.0.1.99"), time.Time{})
		got, _, err := a.Allocate(Pool{CIDR: pool}, staticMAC, nil, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.Equal(staticIP) {
			t.Fatalf("static binding overwritten: got %s, want %s", got, staticIP)
		}
	})
}

// oneIPPool is a pool with exactly one assignable address and a per-pool lease time, used to
// force reclamation decisions.
func oneIPPool(addr string, lease time.Duration) Pool {
	return Pool{CIDR: "10.0.1.0/24", RangeStart: addr, RangeEnd: addr, LeaseTime: lease}
}

func TestReclaimExpiredBeyondGrace(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	a := New(10 * time.Second)
	atTime(a, base)
	pool := oneIPPool("10.0.1.5", time.Minute)

	first, evicted, err := a.Allocate(pool, "m1", nil, false)
	if err != nil || first.String() != "10.0.1.5" || evicted != "" {
		t.Fatalf("m1: ip=%s evicted=%q err=%v", first, evicted, err)
	}

	// Expired (past lease) but still within grace: must not reclaim.
	atTime(a, base.Add(time.Minute+5*time.Second))
	if _, _, err := a.Allocate(pool, "m2", nil, false); err == nil {
		t.Fatalf("m2 within grace: expected exhaustion, lease must not be reclaimed yet")
	}

	// Past lease + grace: m2 reclaims m1's address.
	atTime(a, base.Add(time.Minute+11*time.Second))
	got, evicted, err := a.Allocate(pool, "m2", nil, false)
	if err != nil {
		t.Fatalf("m2 past grace: %v", err)
	}
	if got.String() != "10.0.1.5" {
		t.Fatalf("m2 got %s, want reclaimed 10.0.1.5", got)
	}
	if evicted != "m1" {
		t.Fatalf("evicted %q, want m1", evicted)
	}
}

func TestNoReclaimBeforeExpiry(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	a := New(0)
	atTime(a, base)
	pool := oneIPPool("10.0.1.5", time.Hour)

	if _, _, err := a.Allocate(pool, "m1", nil, false); err != nil {
		t.Fatalf("m1: %v", err)
	}
	// Well before expiry even with zero grace.
	atTime(a, base.Add(time.Minute))
	if _, _, err := a.Allocate(pool, "m2", nil, false); err == nil {
		t.Fatalf("m2: live lease must not be reclaimed")
	}
}

func TestPreferFreeBeforeReclaim(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	a := New(0)
	atTime(a, base)
	// Two assignable addresses; m1 takes .5.
	pool := Pool{CIDR: "10.0.1.0/24", RangeStart: "10.0.1.5", RangeEnd: "10.0.1.6", LeaseTime: time.Minute}
	if _, _, err := a.Allocate(pool, "m1", nil, false); err != nil {
		t.Fatalf("m1: %v", err)
	}
	// m1 is now expired, but .6 is still free: a new MAC must take the free .6, not reclaim .5.
	atTime(a, base.Add(2*time.Minute))
	got, evicted, err := a.Allocate(pool, "m2", nil, false)
	if err != nil {
		t.Fatalf("m2: %v", err)
	}
	if got.String() != "10.0.1.6" {
		t.Fatalf("m2 got %s, want free 10.0.1.6", got)
	}
	if evicted != "" {
		t.Fatalf("evicted %q, want none (free address preferred)", evicted)
	}
}

func TestStaticNeverReclaimed(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	a := New(0)
	atTime(a, base)
	a.Reserve("static", ip("10.0.1.5"))
	pool := oneIPPool("10.0.1.5", time.Minute)
	// Far in the future: a static binding never expires, so the only address stays off-limits.
	atTime(a, base.Add(1000*time.Hour))
	if _, _, err := a.Allocate(pool, "dynamic", nil, false); err == nil {
		t.Fatalf("static address must never be reclaimed for a dynamic client")
	}
}

func TestReLeaseRefreshesExpiry(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	a := New(0)
	atTime(a, base)
	pool := oneIPPool("10.0.1.5", time.Minute)

	if _, _, err := a.Allocate(pool, "m1", nil, false); err != nil {
		t.Fatalf("m1: %v", err)
	}
	// m1 renews at +30s, pushing expiry to +90s.
	atTime(a, base.Add(30*time.Second))
	if _, _, err := a.Allocate(pool, "m1", nil, false); err != nil {
		t.Fatalf("m1 renew: %v", err)
	}
	// At +70s the original expiry (+60s) has passed, but the refreshed one (+90s) has not:
	// a different MAC must not reclaim.
	atTime(a, base.Add(70*time.Second))
	if _, _, err := a.Allocate(pool, "m2", nil, false); err == nil {
		t.Fatalf("m2: refreshed lease must not be reclaimed")
	}
}

func TestRestoreConflictRefused(t *testing.T) {
	a := New(0)
	if !a.Restore("m1", ip("10.0.1.5"), time.Time{}) {
		t.Fatalf("first Restore should succeed")
	}
	if a.Restore("m2", ip("10.0.1.5"), time.Time{}) {
		t.Fatalf("Restore of a duplicate IP for a different MAC must be refused")
	}
	// m1 keeps .5; m2 gets a different address.
	if got, _, _ := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "m1", nil, false); got.String() != "10.0.1.5" {
		t.Fatalf("m1 got %s, want kept 10.0.1.5", got)
	}
	if got, _, _ := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "m2", nil, false); got.String() == "10.0.1.5" {
		t.Fatalf("m2 must not share the IP already held by m1")
	}
}

func TestRestoreExpiredThenReclaimed(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	a := New(0)
	atTime(a, base)
	// A restored lease that already expired a minute ago.
	a.Restore("m1", ip("10.0.1.5"), base.Add(-time.Minute))
	pool := oneIPPool("10.0.1.5", time.Minute)

	got, evicted, err := a.Allocate(pool, "m2", nil, false)
	if err != nil {
		t.Fatalf("m2: %v", err)
	}
	if got.String() != "10.0.1.5" || evicted != "m1" {
		t.Fatalf("m2 got %s evicted %q, want 10.0.1.5 evicting m1", got, evicted)
	}
}
