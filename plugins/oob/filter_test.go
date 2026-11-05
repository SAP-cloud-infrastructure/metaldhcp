// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package oob

import (
	"encoding/hex"
	"net"

	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/api"
	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/helper"
	"github.com/insomniacslk/dhcp/dhcpv4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	filterClientID = "ff0102aabb"        // hex string fed via option 61
	filterMAC      = "cc:dd:ee:ff:00:01" // MAC used in clientIdList filter tests
)

// discoverRelayWithClientID is discoverRelay with option 61 set.
func discoverRelayWithClientID(mac net.HardwareAddr, giaddr net.IP, hexID string) (*dhcpv4.DHCPv4, *dhcpv4.DHCPv4) {
	req, resp := discoverRelay(mac, giaddr)
	b, err := hex.DecodeString(hexID)
	Expect(err).NotTo(HaveOccurred())
	req.Options.Update(dhcpv4.Option{Code: dhcpv4.OptionClientIdentifier, Value: dhcpv4.OptionGeneric{Data: b}})
	return req, resp
}

var _ = Describe("clientIdList filter", func() {
	const (
		cidr    = "10.61.0.0/24"
		gateway = "10.61.0.1"
	)

	var (
		mac net.HardwareAddr
		gi  = net.ParseIP(gateway)
	)

	BeforeEach(func() {
		var err error
		mac, err = net.ParseMAC(filterMAC)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("allow mode", func() {
		It("admits when MAC is in macList", func(ctx SpecContext) {
			h := buildPlugin(api.OOBConfig{Subnets: []api.Subnet{{
				CIDR: cidr, Gateway: gateway, Mode: "allow",
				MacList: []string{helper.NormalizeMAC(mac)},
			}}})
			deferDeleteLease(mac)
			req, resp := discoverRelay(mac, gi)
			out, drop := h(req, resp)
			Expect(drop).To(BeFalse())
			Expect(out).NotTo(BeNil())
		})

		It("drops when MAC not in macList and clientId not in clientIdList", func(_ SpecContext) {
			otherMAC, _ := net.ParseMAC(dellMAC)
			h := buildPlugin(api.OOBConfig{Subnets: []api.Subnet{{
				CIDR: cidr, Gateway: gateway, Mode: "allow",
				MacList:      []string{helper.NormalizeMAC(otherMAC)},
				ClientIdList: []string{"aabbccdd"},
			}}})
			req, resp := discoverRelayWithClientID(mac, gi, filterClientID)
			out, drop := h(req, resp)
			Expect(drop).To(BeTrue())
			Expect(out).To(BeNil())
		})

		It("admits when clientId is in clientIdList", func(ctx SpecContext) {
			h := buildPlugin(api.OOBConfig{Subnets: []api.Subnet{{
				CIDR: cidr, Gateway: gateway, Mode: "allow",
				ClientIdList: []string{filterClientID},
			}}})
			deferDeleteLease(mac)
			req, resp := discoverRelayWithClientID(mac, gi, filterClientID)
			out, drop := h(req, resp)
			Expect(drop).To(BeFalse())
			Expect(out).NotTo(BeNil())
		})

		It("admits when MAC matches in a mixed list (clientId does not match)", func(ctx SpecContext) {
			h := buildPlugin(api.OOBConfig{Subnets: []api.Subnet{{
				CIDR: cidr, Gateway: gateway, Mode: "allow",
				MacList:      []string{helper.NormalizeMAC(mac)},
				ClientIdList: []string{"aabbccdd"},
			}}})
			deferDeleteLease(mac)
			req, resp := discoverRelayWithClientID(mac, gi, filterClientID)
			out, drop := h(req, resp)
			Expect(drop).To(BeFalse())
			Expect(out).NotTo(BeNil())
		})

		It("admits when clientId matches in a mixed list (MAC does not match)", func(ctx SpecContext) {
			otherMAC, _ := net.ParseMAC(dellMAC)
			h := buildPlugin(api.OOBConfig{Subnets: []api.Subnet{{
				CIDR: cidr, Gateway: gateway, Mode: "allow",
				MacList:      []string{helper.NormalizeMAC(otherMAC)},
				ClientIdList: []string{filterClientID},
			}}})
			deferDeleteLease(mac)
			req, resp := discoverRelayWithClientID(mac, gi, filterClientID)
			out, drop := h(req, resp)
			Expect(drop).To(BeFalse())
			Expect(out).NotTo(BeNil())
		})

		It("drops when neither MAC nor clientId matches", func(_ SpecContext) {
			otherMAC, _ := net.ParseMAC(dellMAC)
			h := buildPlugin(api.OOBConfig{Subnets: []api.Subnet{{
				CIDR: cidr, Gateway: gateway, Mode: "allow",
				MacList:      []string{helper.NormalizeMAC(otherMAC)},
				ClientIdList: []string{"aabbccdd"},
			}}})
			req, resp := discoverRelayWithClientID(mac, gi, filterClientID)
			out, drop := h(req, resp)
			Expect(drop).To(BeTrue())
			Expect(out).To(BeNil())
		})
	})

	Context("deny mode", func() {
		It("drops when MAC is in macList", func(_ SpecContext) {
			h := buildPlugin(api.OOBConfig{Subnets: []api.Subnet{{
				CIDR: cidr, Gateway: gateway, Mode: "deny",
				MacList: []string{helper.NormalizeMAC(mac)},
			}}})
			req, resp := discoverRelay(mac, gi)
			out, drop := h(req, resp)
			Expect(drop).To(BeTrue())
			Expect(out).To(BeNil())
		})

		It("admits when MAC not in macList and clientId not in clientIdList", func(ctx SpecContext) {
			otherMAC, _ := net.ParseMAC(dellMAC)
			h := buildPlugin(api.OOBConfig{Subnets: []api.Subnet{{
				CIDR: cidr, Gateway: gateway, Mode: "deny",
				MacList:      []string{helper.NormalizeMAC(otherMAC)},
				ClientIdList: []string{"aabbccdd"},
			}}})
			deferDeleteLease(mac)
			req, resp := discoverRelayWithClientID(mac, gi, filterClientID)
			out, drop := h(req, resp)
			Expect(drop).To(BeFalse())
			Expect(out).NotTo(BeNil())
		})

		It("drops when clientId is in clientIdList", func(_ SpecContext) {
			h := buildPlugin(api.OOBConfig{Subnets: []api.Subnet{{
				CIDR: cidr, Gateway: gateway, Mode: "deny",
				ClientIdList: []string{filterClientID},
			}}})
			req, resp := discoverRelayWithClientID(mac, gi, filterClientID)
			out, drop := h(req, resp)
			Expect(drop).To(BeTrue())
			Expect(out).To(BeNil())
		})

		It("drops when MAC matches in a mixed list", func(_ SpecContext) {
			h := buildPlugin(api.OOBConfig{Subnets: []api.Subnet{{
				CIDR: cidr, Gateway: gateway, Mode: "deny",
				MacList:      []string{helper.NormalizeMAC(mac)},
				ClientIdList: []string{"aabbccdd"},
			}}})
			req, resp := discoverRelayWithClientID(mac, gi, filterClientID)
			out, drop := h(req, resp)
			Expect(drop).To(BeTrue())
			Expect(out).To(BeNil())
		})

		It("drops when clientId matches in a mixed list", func(_ SpecContext) {
			otherMAC, _ := net.ParseMAC(dellMAC)
			h := buildPlugin(api.OOBConfig{Subnets: []api.Subnet{{
				CIDR: cidr, Gateway: gateway, Mode: "deny",
				MacList:      []string{helper.NormalizeMAC(otherMAC)},
				ClientIdList: []string{filterClientID},
			}}})
			req, resp := discoverRelayWithClientID(mac, gi, filterClientID)
			out, drop := h(req, resp)
			Expect(drop).To(BeTrue())
			Expect(out).To(BeNil())
		})

		It("admits when neither MAC nor clientId matches", func(ctx SpecContext) {
			otherMAC, _ := net.ParseMAC(dellMAC)
			h := buildPlugin(api.OOBConfig{Subnets: []api.Subnet{{
				CIDR: cidr, Gateway: gateway, Mode: "deny",
				MacList:      []string{helper.NormalizeMAC(otherMAC)},
				ClientIdList: []string{"aabbccdd"},
			}}})
			deferDeleteLease(mac)
			req, resp := discoverRelayWithClientID(mac, gi, filterClientID)
			out, drop := h(req, resp)
			Expect(drop).To(BeFalse())
			Expect(out).NotTo(BeNil())
		})
	})
})
