// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package oob

import (
	"context"
	"net"
	"time"

	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/allocator"
	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/api"
	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/helper"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("seedStaticLeases / lookupStaticLease", func() {
	var k *K8sClient

	BeforeEach(func() {
		k = &K8sClient{
			statics: make(map[string]staticEntry),
			alloc:   allocator.New(time.Hour),
		}
	})

	Context("MAC-keyed static lease", func() {
		It("returns the correct entry on MAC lookup", func() {
			leases := []api.StaticLease{{MAC: "aa:bb:cc:dd:ee:01", IP: "10.0.0.1", Hostname: "host-a"}}
			k.seedStaticLeases(context.Background(), leases, nil, "10.0.0.254", "", nil, "")

			mac, err := net.ParseMAC("aa:bb:cc:dd:ee:01")
			Expect(err).NotTo(HaveOccurred())
			macKey := normKey(mac)

			entry, ok := k.lookupStaticLease(macKey, "")
			Expect(ok).To(BeTrue())
			Expect(entry.ip.String()).To(Equal("10.0.0.1"))
			Expect(entry.hostname).To(Equal("host-a"))
		})

		It("is not found by an unrelated clientId", func() {
			leases := []api.StaticLease{{MAC: "aa:bb:cc:dd:ee:01", IP: "10.0.0.1"}}
			k.seedStaticLeases(context.Background(), leases, nil, "10.0.0.254", "", nil, "")

			_, ok := k.lookupStaticLease("", "ff010203")
			Expect(ok).To(BeFalse())
		})
	})

	Context("clientId-keyed static lease", func() {
		It("returns the correct entry on clientId lookup", func() {
			leases := []api.StaticLease{{ClientId: "ff0102aabb", IP: "10.0.0.2", Hostname: "host-b"}}
			k.seedStaticLeases(context.Background(), leases, nil, "10.0.0.254", "", nil, "")

			entry, ok := k.lookupStaticLease("", "ff0102aabb")
			Expect(ok).To(BeTrue())
			Expect(entry.ip.String()).To(Equal("10.0.0.2"))
			Expect(entry.hostname).To(Equal("host-b"))
		})

		It("normalizes clientId to lowercase on seed and lookup", func() {
			leases := []api.StaticLease{{ClientId: "FF0102AABB", IP: "10.0.0.3"}}
			k.seedStaticLeases(context.Background(), leases, nil, "10.0.0.254", "", nil, "")

			entry, ok := k.lookupStaticLease("", "ff0102aabb")
			Expect(ok).To(BeTrue())
			Expect(entry.ip.String()).To(Equal("10.0.0.3"))
		})

		It("strips 0x prefix on seed", func() {
			leases := []api.StaticLease{{ClientId: "0xff0102aabb", IP: "10.0.0.4"}}
			k.seedStaticLeases(context.Background(), leases, nil, "10.0.0.254", "", nil, "")

			entry, ok := k.lookupStaticLease("", "ff0102aabb")
			Expect(ok).To(BeTrue())
			Expect(entry.ip.String()).To(Equal("10.0.0.4"))
		})

		It("is not found by an unrelated MAC", func() {
			leases := []api.StaticLease{{ClientId: "ff0102aabb", IP: "10.0.0.2"}}
			k.seedStaticLeases(context.Background(), leases, nil, "10.0.0.254", "", nil, "")

			mac, err := net.ParseMAC("aa:bb:cc:dd:ee:01")
			Expect(err).NotTo(HaveOccurred())
			_, ok := k.lookupStaticLease(normKey(mac), "")
			Expect(ok).To(BeFalse())
		})
	})

	It("MAC and clientId keys coexist in the same map without collision", func() {
		leases := []api.StaticLease{
			{MAC: "aa:bb:cc:dd:ee:01", IP: "10.0.0.10"},
			{ClientId: "ff0102aabb", IP: "10.0.0.20"},
		}
		k.seedStaticLeases(context.Background(), leases, nil, "10.0.0.254", "", nil, "")

		mac, err := net.ParseMAC("aa:bb:cc:dd:ee:01")
		Expect(err).NotTo(HaveOccurred())

		e1, ok1 := k.lookupStaticLease(normKey(mac), "")
		Expect(ok1).To(BeTrue())
		Expect(e1.ip.String()).To(Equal("10.0.0.10"))

		e2, ok2 := k.lookupStaticLease("", "ff0102aabb")
		Expect(ok2).To(BeTrue())
		Expect(e2.ip.String()).To(Equal("10.0.0.20"))
	})
})

// normKey normalizes a MAC the same way seedStaticLeases does.
func normKey(mac net.HardwareAddr) string {
	return helper.NormalizeMAC(mac)
}
