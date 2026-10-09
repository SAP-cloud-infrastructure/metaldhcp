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
	"github.com/coredhcp/coredhcp/handler"

	"github.com/insomniacslk/dhcp/dhcpv4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sclientset "k8s.io/client-go/kubernetes"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"
)

const (
	dellMAC   = "18:66:da:00:11:01" // Dell
	hpeMAC    = "04:09:73:00:11:01" // Hewlett Packard Enterprise
	lenovoMAC = "10:c5:95:00:11:01" // Lenovo
)

// buildPlugin writes cfg to a temp config file, runs setup4, and returns the handler.
func buildPlugin(cfg api.OOBConfig) handler.Handler4 {
	data, err := yaml.Marshal(cfg)
	Expect(err).NotTo(HaveOccurred())

	path := filepath.Join(GinkgoT().TempDir(), "config.yaml")
	Expect(os.WriteFile(path, data, 0644)).To(Succeed())

	h, err := setup4(path)
	Expect(err).NotTo(HaveOccurred())
	Expect(h).NotTo(BeNil())
	return h
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

// deferDeleteLease schedules deletion of the DHCPLease for mac after the current spec.
func deferDeleteLease(mac net.HardwareAddr) {
	DeferCleanup(func(ctx SpecContext) {
		lease := &metaldhcpv1alpha1.DHCPLease{
			ObjectMeta: metav1.ObjectMeta{Name: helper.NormalizeMAC(mac)},
		}
		_ = crClient.Delete(ctx, lease)
	})
}

var _ = Describe("OOB plugin handler4", func() {
	It("allocates from a config-defined pool and records a DHCPLease", func(ctx SpecContext) {
		const cidr = "192.168.42.0/24"

		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: cidr, Gateway: "192.168.42.1"}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)
		req, resp := discover(mac, net.ParseIP("192.168.42.10"))

		out, drop := h(req, resp)
		Expect(drop).To(BeFalse())
		Expect(out).NotTo(BeNil())
		Expect(contains(cidr, out.YourIPAddr)).To(BeTrue(), "YourIPAddr %s must be within %s", out.YourIPAddr, cidr)

		lease := &metaldhcpv1alpha1.DHCPLease{
			ObjectMeta: metav1.ObjectMeta{Name: helper.NormalizeMAC(mac)},
		}
		Eventually(Object(lease)).Should(SatisfyAll(
			HaveField("Spec.MACAddress", mac.String()),
			HaveField("Spec.IP", out.YourIPAddr.String()),
		))
	})

	It("allocates from a labeled OOBSubnet CR and records a DHCPLease", func(ctx SpecContext) {
		const cidr = "192.168.43.0/24"

		subnet := &metaldhcpv1alpha1.OOBSubnet{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "subnet-a",
				Labels: map[string]string{"oob": "true"},
			},
			Spec: metaldhcpv1alpha1.OOBSubnetSpec{CIDR: cidr, Gateway: "192.168.43.1"},
		}
		Expect(crClient.Create(ctx, subnet)).To(Succeed())
		DeferCleanup(crClient.Delete, subnet)

		h := buildPlugin(api.OOBConfig{
			SubnetLabels: []api.SubnetLabel{{Key: "oob", Value: "true"}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)
		req, resp := discover(mac, net.ParseIP("192.168.43.10"))

		out, drop := h(req, resp)
		Expect(drop).To(BeFalse())
		Expect(out).NotTo(BeNil())
		Expect(contains(cidr, out.YourIPAddr)).To(BeTrue(), "YourIPAddr %s must be within %s", out.YourIPAddr, cidr)

		lease := &metaldhcpv1alpha1.DHCPLease{
			ObjectMeta: metav1.ObjectMeta{Name: helper.NormalizeMAC(mac)},
		}
		Eventually(Object(lease)).Should(SatisfyAll(
			HaveField("Spec.MACAddress", mac.String()),
			HaveField("Spec.IP", out.YourIPAddr.String()),
		))
	})

	It("returns the same IP on re-lease and patches the single DHCPLease", func(ctx SpecContext) {
		const cidr = "192.168.44.0/24"

		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: cidr, Gateway: "192.168.44.1"}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)

		req1, resp1 := discover(mac, net.ParseIP("192.168.44.10"))
		out1, drop1 := h(req1, resp1)
		Expect(drop1).To(BeFalse())
		Expect(out1).NotTo(BeNil())
		firstIP := out1.YourIPAddr.String()

		lease := &metaldhcpv1alpha1.DHCPLease{
			ObjectMeta: metav1.ObjectMeta{Name: helper.NormalizeMAC(mac)},
		}
		Eventually(Object(lease)).Should(HaveField("Spec.IP", firstIP))

		req2, resp2 := discover(mac, net.ParseIP("192.168.44.10"))
		out2, drop2 := h(req2, resp2)
		Expect(drop2).To(BeFalse())
		Expect(out2).NotTo(BeNil())
		Expect(out2.YourIPAddr.String()).To(Equal(firstIP), "re-lease must return the same IP")

		list := &metaldhcpv1alpha1.DHCPLeaseList{}
		Eventually(ObjectList(list)).Should(HaveField("Items", HaveLen(1)))
	})

	It("allocates from the correct pool when giaddr is set (relay)", func(ctx SpecContext) {
		const cidr1 = "10.0.1.0/24"
		const cidr2 = "10.0.2.0/24"

		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{
				{CIDR: cidr1, Gateway: "10.0.1.1"},
				{CIDR: cidr2, Gateway: "10.0.2.1"},
			},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)

		// Relay for the second subnet: giaddr is in cidr2.
		req, resp := discoverRelay(mac, net.ParseIP("10.0.2.1"))

		out, drop := h(req, resp)
		Expect(drop).To(BeFalse())
		Expect(out).NotTo(BeNil())
		Expect(contains(cidr2, out.YourIPAddr)).To(BeTrue(),
			"YourIPAddr %s must be within relay subnet %s", out.YourIPAddr, cidr2)
		Expect(contains(cidr1, out.YourIPAddr)).To(BeFalse(),
			"YourIPAddr must not be in the non-relay subnet %s", cidr1)

		lease := &metaldhcpv1alpha1.DHCPLease{
			ObjectMeta: metav1.ObjectMeta{Name: helper.NormalizeMAC(mac)},
		}
		Eventually(Object(lease)).Should(SatisfyAll(
			HaveField("Spec.MACAddress", mac.String()),
			HaveField("Spec.IP", out.YourIPAddr.String()),
		))
	})

	It("drops the request when no pool CIDR matches the hint", func(ctx SpecContext) {
		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: "192.168.45.0/24", Gateway: "192.168.45.1"}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		req, resp := discover(mac, net.ParseIP("10.99.99.5"))

		out, drop := h(req, resp)
		Expect(out).To(BeNil())
		Expect(drop).To(BeTrue())
	})

	It("drops the request when the matching pool is exhausted", func(ctx SpecContext) {
		// A /31 has no usable host addresses, so allocation always fails.
		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: "10.0.0.0/31"}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		req, resp := discover(mac, net.ParseIP("10.0.0.0"))

		out, drop := h(req, resp)
		Expect(out).To(BeNil())
		Expect(drop).To(BeTrue())
	})

	It("sets BootFileName from pool bootURL when present", func(ctx SpecContext) {
		const (
			cidr    = "10.1.1.0/24"
			bootURL = "https://boot-operator.example.com/boot"
		)

		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: cidr, Gateway: "10.1.1.1", BootURL: bootURL}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)
		req, resp := discover(mac, net.ParseIP("10.1.1.1"))

		out, drop := h(req, resp)
		Expect(drop).To(BeFalse())
		Expect(out).NotTo(BeNil())
		Expect(out.BootFileName).To(Equal(bootURL))
	})

	It("leaves BootFileName empty when pool has no bootURL", func(ctx SpecContext) {
		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: "10.2.2.0/24", Gateway: "10.2.2.1"}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)
		req, resp := discover(mac, net.ParseIP("10.2.2.1"))

		out, drop := h(req, resp)
		Expect(drop).To(BeFalse())
		Expect(out).NotTo(BeNil())
		Expect(out.BootFileName).To(BeEmpty())
	})

	It("returns the static IP for a configured MAC regardless of pool hint", func(ctx SpecContext) {
		const (
			cidr     = "10.3.3.0/24"
			staticIP = "10.3.3.55"
			staticHN = "bmc-node001"
		)

		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: cidr, Gateway: "10.3.3.1"}},
			StaticLeases: []api.StaticLease{
				{MAC: dellMAC, IP: staticIP, Hostname: staticHN},
			},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)

		// send with no pool hint — should still get the static IP
		req, resp := discover(mac, nil)
		out, drop := h(req, resp)
		Expect(drop).To(BeFalse())
		Expect(out).NotTo(BeNil())
		Expect(out.YourIPAddr.String()).To(Equal(staticIP))

		// DHCPLease should record the hostname
		lease := &metaldhcpv1alpha1.DHCPLease{
			ObjectMeta: metav1.ObjectMeta{Name: helper.NormalizeMAC(mac)},
		}
		Eventually(ctx, Object(lease)).Should(SatisfyAll(
			HaveField("Spec.IP", staticIP),
			HaveField("Spec.Hostname", staticHN),
		))
	})

	It("emits a Warning event with reason NoPoolFound when no pool matches the giaddr", func(ctx SpecContext) {
		ns := newNamespace(ctx)

		cs, err := k8sclientset.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())

		broadcaster := record.NewBroadcaster()
		broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: cs.CoreV1().Events(ns.Name)})
		DeferCleanup(broadcaster.Shutdown)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "metaldhcp-0", Namespace: ns.Name},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "metaldhcp", Image: "metaldhcp"}},
			},
		}
		Expect(crClient.Create(ctx, pod)).To(Succeed())
		DeferCleanup(crClient.Delete, pod)

		recorder := broadcaster.NewRecorder(k8sscheme.Scheme, corev1.EventSource{Component: "metaldhcp"})
		SetRecorder(recorder, pod)
		DeferCleanup(func() { SetRecorder(nil, nil) })

		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: "192.168.45.0/24", Gateway: "192.168.45.1"}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		req, resp := discoverRelay(mac, net.ParseIP("10.99.99.1"))

		out, drop := h(req, resp)
		Expect(out).To(BeNil())
		Expect(drop).To(BeTrue())

		eventList := &corev1.EventList{}
		Eventually(func() []corev1.Event {
			_ = crClient.List(ctx, eventList, client.InNamespace(ns.Name))
			return eventList.Items
		}).Should(ContainElement(SatisfyAll(
			HaveField("Reason", "NoPoolFound"),
			HaveField("Type", corev1.EventTypeWarning),
		)))
	})

	It("stores vendor annotation on DHCPLease when vendor-lookup is enabled", func(ctx SpecContext) {
		const cidr = "10.10.10.0/24"

		vendorLookup = true
		DeferCleanup(func() { vendorLookup = false })

		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: cidr, Gateway: "10.10.10.1"}},
		})

		for _, tc := range []struct {
			mac    string
			vendor string
		}{
			{dellMAC, "Dell"},
			{hpeMAC, "Hewlett Packard Enterprise"},
			{lenovoMAC, "Lenovo"},
		} {
			mac, err := net.ParseMAC(tc.mac)
			Expect(err).NotTo(HaveOccurred())
			deferDeleteLease(mac)
			req, resp := discover(mac, net.ParseIP("10.10.10.1"))

			out, drop := h(req, resp)
			Expect(drop).To(BeFalse(), "mac=%s", tc.mac)
			Expect(out).NotTo(BeNil())

			lease := &metaldhcpv1alpha1.DHCPLease{
				ObjectMeta: metav1.ObjectMeta{Name: helper.NormalizeMAC(mac)},
			}
			Eventually(ctx, Object(lease)).Should(
				HaveField("Annotations", HaveKeyWithValue("dhcp.metal.ironcore.dev/vendor", tc.vendor)),
				"mac=%s expected vendor %q", tc.mac, tc.vendor,
			)
		}
	})

	It("does not store vendor annotation when vendor-lookup is disabled", func(ctx SpecContext) {
		const cidr = "10.10.11.0/24"

		// vendorLookup defaults false; confirm no annotation written
		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: cidr, Gateway: "10.10.11.1"}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)
		req, resp := discover(mac, net.ParseIP("10.10.11.1"))

		out, drop := h(req, resp)
		Expect(drop).To(BeFalse())
		Expect(out).NotTo(BeNil())

		lease := &metaldhcpv1alpha1.DHCPLease{
			ObjectMeta: metav1.ObjectMeta{Name: helper.NormalizeMAC(mac)},
		}
		Eventually(ctx, Object(lease)).Should(
			HaveField("Annotations", Not(HaveKey("dhcp.metal.ironcore.dev/vendor"))),
		)
	})
})
