// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package allocator

import (
	"net"
	"testing"
)

func ip(s string) net.IP { return net.ParseIP(s) }

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
			a := New()
			got, err := a.Allocate(tt.pool, "aa", nil, false)
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
	a := New()
	// Occupy .1 and .2 via other MACs; .3 is the first free host.
	if got, _ := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "m1", nil, false); got.String() != "10.0.1.1" {
		t.Fatalf("m1 got %s", got)
	}
	if got, _ := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "m2", nil, false); got.String() != "10.0.1.2" {
		t.Fatalf("m2 got %s", got)
	}
	got, err := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "m3", nil, false)
	if err != nil {
		t.Fatalf("m3: %v", err)
	}
	if got.String() != "10.0.1.3" {
		t.Fatalf("m3 got %s, want 10.0.1.3", got)
	}
}

func TestAllocateRangeEndWindowing(t *testing.T) {
	a := New()
	pool := Pool{CIDR: "10.0.1.0/24", RangeStart: "10.0.1.10", RangeEnd: "10.0.1.11"}
	first, err := a.Allocate(pool, "m1", nil, false)
	if err != nil || first.String() != "10.0.1.10" {
		t.Fatalf("first: %s err %v", first, err)
	}
	second, err := a.Allocate(pool, "m2", nil, false)
	if err != nil || second.String() != "10.0.1.11" {
		t.Fatalf("second: %s err %v", second, err)
	}
	if _, err := a.Allocate(pool, "m3", nil, false); err == nil {
		t.Fatalf("expected exhaustion error once window is full")
	}
}

func TestReLeaseReturnsSameIP(t *testing.T) {
	a := New()
	pool := Pool{CIDR: "10.0.1.0/24"}
	first, err := a.Allocate(pool, "mac", nil, false)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := a.Allocate(pool, "mac", nil, false)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !first.Equal(second) {
		t.Fatalf("re-lease changed IP: %s != %s", first, second)
	}
}

func TestReLeaseAfterRestore(t *testing.T) {
	a := New()
	a.Restore("mac", ip("10.0.1.50"))
	got, err := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "mac", nil, false)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if got.String() != "10.0.1.50" {
		t.Fatalf("got %s, want restored 10.0.1.50", got)
	}
}

func TestReLeaseIgnoredWhenExistingOutOfCIDR(t *testing.T) {
	a := New()
	a.Restore("mac", ip("192.168.0.9"))
	// Existing assignment is not within the requested pool CIDR, so a fresh
	// address from the pool is handed out instead.
	got, err := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "mac", nil, false)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if got.String() != "10.0.1.1" {
		t.Fatalf("got %s, want 10.0.1.1", got)
	}
}

func TestExactIPHonoredWhenFree(t *testing.T) {
	a := New()
	got, err := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "mac", ip("10.0.1.77"), true)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if got.String() != "10.0.1.77" {
		t.Fatalf("got %s, want exact 10.0.1.77", got)
	}
}

func TestExactIPRejectedWhenTakenByAnotherMAC(t *testing.T) {
	a := New()
	if _, err := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "owner", ip("10.0.1.5"), true); err != nil {
		t.Fatalf("owner: %v", err)
	}
	// Another MAC requesting the same address falls back to first-free.
	got, err := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "other", ip("10.0.1.5"), true)
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
	a := New()
	first, err := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "mac", ip("10.0.1.5"), true)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "mac", ip("10.0.1.5"), true)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !first.Equal(second) || second.String() != "10.0.1.5" {
		t.Fatalf("got %s then %s, want stable 10.0.1.5", first, second)
	}
}

func TestExactIPOutOfCIDRFallsToFirstFree(t *testing.T) {
	a := New()
	got, err := a.Allocate(Pool{CIDR: "10.0.1.0/24"}, "mac", ip("172.16.0.5"), true)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if got.String() != "10.0.1.1" {
		t.Fatalf("got %s, want first-free 10.0.1.1", got)
	}
}

func TestSlash30(t *testing.T) {
	// 10.0.0.0/30: network .0, broadcast .3, usable hosts .1 and .2.
	a := New()
	pool := Pool{CIDR: "10.0.0.0/30"}
	first, err := a.Allocate(pool, "m1", nil, false)
	if err != nil || first.String() != "10.0.0.1" {
		t.Fatalf("first: %s err %v", first, err)
	}
	second, err := a.Allocate(pool, "m2", nil, false)
	if err != nil || second.String() != "10.0.0.2" {
		t.Fatalf("second: %s err %v", second, err)
	}
	if _, err := a.Allocate(pool, "m3", nil, false); err == nil {
		t.Fatalf("expected exhaustion on /30 after two hosts")
	}
}

func TestSlash30GatewayExcluded(t *testing.T) {
	a := New()
	pool := Pool{CIDR: "10.0.0.0/30", Gateway: "10.0.0.1"}
	first, err := a.Allocate(pool, "m1", nil, false)
	if err != nil || first.String() != "10.0.0.2" {
		t.Fatalf("first: %s err %v", first, err)
	}
	if _, err := a.Allocate(pool, "m2", nil, false); err == nil {
		t.Fatalf("expected exhaustion: only .2 is usable with gateway at .1")
	}
}

func TestSlash31(t *testing.T) {
	// 10.0.0.0/31: network .0, broadcast .1. firstFree scans from nextIP(network)
	// up to prevIP(broadcast), i.e. .1 down to .0, an empty range -> exhaustion.
	a := New()
	if _, err := a.Allocate(Pool{CIDR: "10.0.0.0/31"}, "m1", nil, false); err == nil {
		t.Fatalf("expected no usable host in /31")
	}
}

func TestExhaustion(t *testing.T) {
	a := New()
	pool := Pool{CIDR: "10.0.0.0/29", Gateway: "10.0.0.1"}
	// Usable hosts .1...6; gateway .1 excluded -> .2..6 = 5 addresses.
	for i := 0; i < 5; i++ {
		if _, err := a.Allocate(pool, netKey(i), nil, false); err != nil {
			t.Fatalf("alloc %d: %v", i, err)
		}
	}
	if _, err := a.Allocate(pool, "overflow", nil, false); err == nil {
		t.Fatalf("expected exhaustion after draining /29")
	}
}

func TestInvalidCIDR(t *testing.T) {
	a := New()
	if _, err := a.Allocate(Pool{CIDR: "not-a-cidr"}, "mac", nil, false); err == nil {
		t.Fatalf("expected error for invalid CIDR")
	}
}

func TestNonIPv4CIDR(t *testing.T) {
	a := New()
	if _, err := a.Allocate(Pool{CIDR: "2001:db8::/64"}, "mac", nil, false); err == nil {
		t.Fatalf("expected error for non-IPv4 CIDR")
	}
}

func netKey(i int) string {
	return string(rune('a' + i))
}
