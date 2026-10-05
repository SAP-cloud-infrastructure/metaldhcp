// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package oob

import (
	"net"
	"os"
	"path/filepath"

	metaldhcpv1alpha1 "github.com/SAP-cloud-infrastructure/metaldhcp/api/v1alpha1"
	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/api"
	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/helper"

	"github.com/insomniacslk/dhcp/dhcpv4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"
	"gopkg.in/yaml.v3"
)

const testMAC = "aa:bb:cc:dd:ee:01"

// buildPlugin writes cfg to a temp config file and runs setup4, which assigns the
// package-level k8sClient and returns the handler.
func buildPlugin(cfg api.OOBConfig) {
	data, err := yaml.Marshal(cfg)
	Expect(err).NotTo(HaveOccurred())

	path := filepath.Join(GinkgoT().TempDir(), "config.yaml")
	Expect(os.WriteFile(path, data, 0644)).To(Succeed())

	h, err := setup4(path)
	Expect(err).NotTo(HaveOccurred())
	Expect(h).NotTo(BeNil())
}

// discover builds a DISCOVER req and its reply stub, optionally seeding the reply's
// ServerIPAddr as the (non-exact) pool-selection hint.
func discover(mac net.HardwareAddr, serverHint net.IP) (*dhcpv4.DHCPv4, *dhcpv4.DHCPv4) {
	req, err := dhcpv4.NewDiscovery(mac)
	Expect(err).NotTo(HaveOccurred())
	resp, err := dhcpv4.NewReplyFromRequest(req)
	Expect(err).NotTo(HaveOccurred())
	if serverHint != nil {
		resp.ServerIPAddr = serverHint
	}
	return req, resp
}

func contains(cidr string, ip net.IP) bool {
	_, n, err := net.ParseCIDR(cidr)
	Expect(err).NotTo(HaveOccurred())
	return n.Contains(ip)
}

// discoverRelay builds a DISCOVER as a relay agent would: giaddr set to an IP on the
// client's subnet, client fields zeroed.
func discoverRelay(mac net.HardwareAddr, giaddr net.IP) (*dhcpv4.DHCPv4, *dhcpv4.DHCPv4) {
	req, err := dhcpv4.NewDiscovery(mac)
	Expect(err).NotTo(HaveOccurred())
	req.GatewayIPAddr = giaddr
	resp, err := dhcpv4.NewReplyFromRequest(req)
	Expect(err).NotTo(HaveOccurred())
	return req, resp
}

var _ = Describe("OOB plugin handler4", func() {
	It("allocates from a config-defined pool and records a DHCPLease", func(ctx SpecContext) {
		ns := newNamespace(ctx)
		const cidr = "192.168.42.0/24"

		buildPlugin(api.OOBConfig{
			Namespace: ns.Name,
			Subnets:   []api.Subnet{{CIDR: cidr, Gateway: "192.168.42.1"}},
		})

		mac, err := net.ParseMAC(testMAC)
		Expect(err).NotTo(HaveOccurred())
		req, resp := discover(mac, net.ParseIP("192.168.42.10"))

		out, drop := handler4(req, resp)
		Expect(drop).To(BeFalse())
		Expect(out).NotTo(BeNil())
		Expect(contains(cidr, out.YourIPAddr)).To(BeTrue(), "YourIPAddr %s must be within %s", out.YourIPAddr, cidr)

		lease := &metaldhcpv1alpha1.DHCPLease{
			ObjectMeta: metav1.ObjectMeta{Name: helper.NormalizeMAC(mac), Namespace: ns.Name},
		}
		Eventually(Object(lease)).Should(SatisfyAll(
			HaveField("Spec.MACAddress", mac.String()),
			HaveField("Spec.IP", out.YourIPAddr.String()),
		))
	})

	It("allocates from a labeled OOBSubnet CR and records a DHCPLease", func(ctx SpecContext) {
		ns := newNamespace(ctx)
		const cidr = "192.168.43.0/24"

		subnet := &metaldhcpv1alpha1.OOBSubnet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "subnet-a",
				Namespace: ns.Name,
				Labels:    map[string]string{"oob": "true"},
			},
			Spec: metaldhcpv1alpha1.OOBSubnetSpec{CIDR: cidr, Gateway: "192.168.43.1"},
		}
		Expect(crClient.Create(ctx, subnet)).To(Succeed())
		DeferCleanup(crClient.Delete, subnet)

		buildPlugin(api.OOBConfig{
			Namespace:    ns.Name,
			SubnetLabels: []api.SubnetLabel{{Key: "oob", Value: "true"}},
		})

		mac, err := net.ParseMAC(testMAC)
		Expect(err).NotTo(HaveOccurred())
		req, resp := discover(mac, net.ParseIP("192.168.43.10"))

		out, drop := handler4(req, resp)
		Expect(drop).To(BeFalse())
		Expect(out).NotTo(BeNil())
		Expect(contains(cidr, out.YourIPAddr)).To(BeTrue(), "YourIPAddr %s must be within %s", out.YourIPAddr, cidr)

		lease := &metaldhcpv1alpha1.DHCPLease{
			ObjectMeta: metav1.ObjectMeta{Name: helper.NormalizeMAC(mac), Namespace: ns.Name},
		}
		Eventually(Object(lease)).Should(SatisfyAll(
			HaveField("Spec.MACAddress", mac.String()),
			HaveField("Spec.IP", out.YourIPAddr.String()),
		))
	})

	It("returns the same IP on re-lease and patches the single DHCPLease", func(ctx SpecContext) {
		ns := newNamespace(ctx)
		const cidr = "192.168.44.0/24"

		buildPlugin(api.OOBConfig{
			Namespace: ns.Name,
			Subnets:   []api.Subnet{{CIDR: cidr, Gateway: "192.168.44.1"}},
		})

		mac, err := net.ParseMAC(testMAC)
		Expect(err).NotTo(HaveOccurred())

		req1, resp1 := discover(mac, net.ParseIP("192.168.44.10"))
		out1, drop1 := handler4(req1, resp1)
		Expect(drop1).To(BeFalse())
		Expect(out1).NotTo(BeNil())
		firstIP := out1.YourIPAddr.String()

		lease := &metaldhcpv1alpha1.DHCPLease{
			ObjectMeta: metav1.ObjectMeta{Name: helper.NormalizeMAC(mac), Namespace: ns.Name},
		}
		Eventually(Object(lease)).Should(HaveField("Spec.IP", firstIP))

		req2, resp2 := discover(mac, net.ParseIP("192.168.44.10"))
		out2, drop2 := handler4(req2, resp2)
		Expect(drop2).To(BeFalse())
		Expect(out2).NotTo(BeNil())
		Expect(out2.YourIPAddr.String()).To(Equal(firstIP), "re-lease must return the same IP")

		list := &metaldhcpv1alpha1.DHCPLeaseList{}
		Eventually(ObjectList(list, client.InNamespace(ns.Name))).Should(HaveField("Items", HaveLen(1)))
	})

	It("allocates from the correct pool when giaddr is set (relay)", func(ctx SpecContext) {
		ns := newNamespace(ctx)
		const cidr1 = "10.0.1.0/24"
		const cidr2 = "10.0.2.0/24"

		buildPlugin(api.OOBConfig{
			Namespace: ns.Name,
			Subnets: []api.Subnet{
				{CIDR: cidr1, Gateway: "10.0.1.1"},
				{CIDR: cidr2, Gateway: "10.0.2.1"},
			},
		})

		mac, err := net.ParseMAC(testMAC)
		Expect(err).NotTo(HaveOccurred())

		// Relay for the second subnet: giaddr is in cidr2.
		req, resp := discoverRelay(mac, net.ParseIP("10.0.2.1"))

		out, drop := handler4(req, resp)
		Expect(drop).To(BeFalse())
		Expect(out).NotTo(BeNil())
		Expect(contains(cidr2, out.YourIPAddr)).To(BeTrue(),
			"YourIPAddr %s must be within relay subnet %s", out.YourIPAddr, cidr2)
		Expect(contains(cidr1, out.YourIPAddr)).To(BeFalse(),
			"YourIPAddr must not be in the non-relay subnet %s", cidr1)

		lease := &metaldhcpv1alpha1.DHCPLease{
			ObjectMeta: metav1.ObjectMeta{Name: helper.NormalizeMAC(mac), Namespace: ns.Name},
		}
		Eventually(Object(lease)).Should(SatisfyAll(
			HaveField("Spec.MACAddress", mac.String()),
			HaveField("Spec.IP", out.YourIPAddr.String()),
		))
	})

	It("drops the request when no pool CIDR matches the hint", func(ctx SpecContext) {
		ns := newNamespace(ctx)

		buildPlugin(api.OOBConfig{
			Namespace: ns.Name,
			Subnets:   []api.Subnet{{CIDR: "192.168.45.0/24", Gateway: "192.168.45.1"}},
		})

		mac, err := net.ParseMAC(testMAC)
		Expect(err).NotTo(HaveOccurred())
		req, resp := discover(mac, net.ParseIP("10.99.99.5"))

		out, drop := handler4(req, resp)
		Expect(out).To(BeNil())
		Expect(drop).To(BeTrue())
	})

	It("drops the request when the matching pool is exhausted", func(ctx SpecContext) {
		ns := newNamespace(ctx)

		// A /31 has no usable host addresses, so allocation always fails.
		buildPlugin(api.OOBConfig{
			Namespace: ns.Name,
			Subnets:   []api.Subnet{{CIDR: "10.0.0.0/31"}},
		})

		mac, err := net.ParseMAC(testMAC)
		Expect(err).NotTo(HaveOccurred())
		req, resp := discover(mac, net.ParseIP("10.0.0.0"))

		out, drop := handler4(req, resp)
		Expect(out).To(BeNil())
		Expect(drop).To(BeTrue())
	})
})
