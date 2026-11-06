// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package oob

import (
	"net"

	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/api"
	"github.com/insomniacslk/dhcp/dhcpv4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	archX64     = "PXEClient:Arch:00007"
	archARM64   = "PXEClient:Arch:00014"
	classX64Ext = "PXEClient:Arch:00007:UNDI:003000"
)

// discoverRelayWithClass is discoverRelay with option 60 (vendor class identifier) set.
func discoverRelayWithClass(mac net.HardwareAddr, giaddr net.IP, classID string) (*dhcpv4.DHCPv4, *dhcpv4.DHCPv4) {
	req, resp := discoverRelay(mac, giaddr)
	req.Options.Update(dhcpv4.OptClassIdentifier(classID))
	return req, resp
}

var _ = Describe("selectTFTPFile", func() {
	It("returns the filename for an exact vendorClass match", func() {
		files := []api.TFTPFile{{VendorClass: archX64, Filename: "ipxe-x64.efi"}}
		Expect(selectTFTPFile(files, archX64)).To(Equal("ipxe-x64.efi"))
	})

	It("returns the filename when vendorClass is a prefix of the class ID", func() {
		files := []api.TFTPFile{{VendorClass: archX64, Filename: "ipxe-x64.efi"}}
		Expect(selectTFTPFile(files, classX64Ext)).To(Equal("ipxe-x64.efi"))
	})

	It("falls back to the entry with an empty vendorClass when nothing matches", func() {
		files := []api.TFTPFile{
			{VendorClass: archX64, Filename: "ipxe-x64.efi"},
			{Filename: "snponly.efi"},
		}
		Expect(selectTFTPFile(files, archARM64)).To(Equal("snponly.efi"))
	})

	It("returns empty when nothing matches and there is no fallback", func() {
		files := []api.TFTPFile{{VendorClass: archX64, Filename: "ipxe-x64.efi"}}
		Expect(selectTFTPFile(files, archARM64)).To(BeEmpty())
	})

	It("returns empty for an empty list", func() {
		Expect(selectTFTPFile(nil, classX64Ext)).To(BeEmpty())
	})

	It("uses the first matching entry when multiple prefixes could match", func() {
		files := []api.TFTPFile{
			{VendorClass: archX64, Filename: "ipxe-x64.efi"},
			{VendorClass: "PXEClient:Arch:0000", Filename: "wrong.efi"},
		}
		Expect(selectTFTPFile(files, classX64Ext)).To(Equal("ipxe-x64.efi"))
	})
})

var _ = Describe("OOB plugin handler4 tftpFiles selection", func() {
	const (
		cidr     = "10.67.0.0/24"
		gateway  = "10.67.0.1"
		serverIP = "10.67.0.254"
	)
	var gi = net.ParseIP(gateway)

	It("sets options 66/67 and stops the chain on a vendorClass match", func(ctx SpecContext) {
		h := buildPlugin(api.OOBConfig{
			ServerIP: serverIP,
			Subnets:  []api.Subnet{{CIDR: cidr, Gateway: gateway}},
			TFTPFiles: []api.TFTPFile{
				{VendorClass: archX64, Filename: "ipxe-x64.efi"},
				{Filename: "snponly.efi"},
			},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)
		req, resp := discoverRelayWithClass(mac, gi, classX64Ext)

		out, stop := h(req, resp)
		Expect(stop).To(BeTrue(), "chain must stop so nbp cannot overwrite option 67")
		Expect(out).NotTo(BeNil())
		Expect(dhcpv4.GetString(dhcpv4.OptionBootfileName, out.Options)).To(Equal("ipxe-x64.efi"))
		Expect(dhcpv4.GetString(dhcpv4.OptionTFTPServerName, out.Options)).To(Equal(serverIP))
	})

	It("selects a distinct file per architecture from a multi-arch config", func(ctx SpecContext) {
		h := buildPlugin(api.OOBConfig{
			ServerIP: serverIP,
			Subnets:  []api.Subnet{{CIDR: cidr, Gateway: gateway}},
			TFTPFiles: []api.TFTPFile{
				{VendorClass: archX64, Filename: "ipxe-x64.efi"},
				{VendorClass: archARM64, Filename: "ipxe-arm64.efi"},
				{Filename: "snponly.efi"},
			},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)
		req, resp := discoverRelayWithClass(mac, gi, archARM64+":UNDI:003000")

		out, stop := h(req, resp)
		Expect(stop).To(BeTrue())
		Expect(out).NotTo(BeNil())
		Expect(dhcpv4.GetString(dhcpv4.OptionBootfileName, out.Options)).To(Equal("ipxe-arm64.efi"),
			"ARM64 class must select its own file, not the x64 entry or the fallback")
	})

	It("stops the chain without setting 66/67 when no entry matches and no fallback", func(ctx SpecContext) {
		h := buildPlugin(api.OOBConfig{
			ServerIP:  serverIP,
			Subnets:   []api.Subnet{{CIDR: cidr, Gateway: gateway}},
			TFTPFiles: []api.TFTPFile{{VendorClass: archX64, Filename: "ipxe-x64.efi"}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)
		req, resp := discoverRelayWithClass(mac, gi, archARM64)

		out, stop := h(req, resp)
		Expect(stop).To(BeTrue(), "chain stops whenever tftpFiles is non-empty")
		Expect(out).NotTo(BeNil())
		Expect(out.Options.Get(dhcpv4.OptionBootfileName)).To(BeEmpty())
		Expect(out.Options.Get(dhcpv4.OptionTFTPServerName)).To(BeEmpty())
	})

	It("does not set 66/67 and continues the chain when tftpFiles is empty", func(ctx SpecContext) {
		h := buildPlugin(api.OOBConfig{
			ServerIP: serverIP,
			Subnets:  []api.Subnet{{CIDR: cidr, Gateway: gateway}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)
		req, resp := discoverRelayWithClass(mac, gi, classX64Ext)

		out, stop := h(req, resp)
		Expect(stop).To(BeFalse(), "nbp must run as before when tftpFiles is empty")
		Expect(out).NotTo(BeNil())
		Expect(out.Options.Get(dhcpv4.OptionBootfileName)).To(BeEmpty())
		Expect(out.Options.Get(dhcpv4.OptionTFTPServerName)).To(BeEmpty())
	})
})
