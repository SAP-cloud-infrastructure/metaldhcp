// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package oob

import (
	"net"
	"os"
	"path/filepath"
	"time"

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
// For each Subnet in cfg a DeferCleanup is registered to delete the bootstrapped OOBSubnet CR.
func buildPlugin(cfg api.OOBConfig) handler.Handler4 {
	data, err := yaml.Marshal(cfg)
	Expect(err).NotTo(HaveOccurred())

	path := filepath.Join(GinkgoT().TempDir(), "config.yaml")
	Expect(os.WriteFile(path, data, 0644)).To(Succeed())

	// register cleanup for CRs that will be bootstrapped from config subnets
	for _, s := range cfg.Subnets {
		name := cidrToCRName(s.CIDR)
		DeferCleanup(func(ctx SpecContext) {
			cr := &metaldhcpv1alpha1.OOBSubnet{ObjectMeta: metav1.ObjectMeta{Name: name}}
			_ = crClient.Delete(ctx, cr)
		})
	}

	h, err := setup4(path)
	Expect(err).NotTo(HaveOccurred())
	Expect(h).NotTo(BeNil())
	return h
}

// discoverDirect builds a DISCOVER req with no relay: giaddr=0.0.0.0, client fields zeroed.
func discoverDirect(mac net.HardwareAddr) (*dhcpv4.DHCPv4, *dhcpv4.DHCPv4) {
	req, err := dhcpv4.NewDiscovery(mac)
	Expect(err).NotTo(HaveOccurred())
	resp, err := dhcpv4.NewReplyFromRequest(req)
	Expect(err).NotTo(HaveOccurred())
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

// discoverIPXE builds a phase-2 iPXE DISCOVER: option 175 present (iPXE encapsulated options marker).
func discoverIPXE(mac net.HardwareAddr, giaddr net.IP) (*dhcpv4.DHCPv4, *dhcpv4.DHCPv4) {
	req, err := dhcpv4.NewDiscovery(mac)
	Expect(err).NotTo(HaveOccurred())
	req.GatewayIPAddr = giaddr
	// option 175 is the reliable iPXE marker; option 60 stays "PXEClient:Arch:00007:UNDI:003010"
	req.Options.Update(dhcpv4.Option{Code: dhcpv4.GenericOptionCode(175), Value: dhcpv4.OptionGeneric{Data: []byte{0x01}}})
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
			Subnets: []api.Subnet{{CIDR: cidr, Gateway: "192.168.42.1", AllowAllMacs: true}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)
		req, resp := discoverRelay(mac, net.ParseIP("192.168.42.10"))

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
			Spec: metaldhcpv1alpha1.OOBSubnetSpec{CIDR: cidr, Gateway: "192.168.43.1", AllowAllMacs: true},
		}
		Expect(crClient.Create(ctx, subnet)).To(Succeed())
		DeferCleanup(crClient.Delete, subnet)

		h := buildPlugin(api.OOBConfig{
			SubnetLabels: []api.SubnetLabel{{Key: "oob", Value: "true"}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)
		req, resp := discoverRelay(mac, net.ParseIP("192.168.43.10"))

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

	It("drops first when no OOBSubnet CR exists, then allocates after the CR is created", func(ctx SpecContext) {
		const cidr = "172.16.10.0/24"

		h := buildPlugin(api.OOBConfig{
			SubnetLabels: []api.SubnetLabel{{Key: "oob", Value: "true"}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())

		req, resp := discoverRelay(mac, net.ParseIP("172.16.10.1"))
		out, drop := h(req, resp)
		Expect(out).To(BeNil())
		Expect(drop).To(BeTrue(), "request must be dropped before the OOBSubnet CR exists")

		subnet := &metaldhcpv1alpha1.OOBSubnet{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "subnet-hot-add",
				Labels: map[string]string{"oob": "true"},
			},
			Spec: metaldhcpv1alpha1.OOBSubnetSpec{CIDR: cidr, Gateway: "172.16.10.1", AllowAllMacs: true},
		}
		Expect(crClient.Create(ctx, subnet)).To(Succeed())
		DeferCleanup(crClient.Delete, subnet)
		deferDeleteLease(mac)

		req2, resp2 := discoverRelay(mac, net.ParseIP("172.16.10.1"))
		out2, drop2 := h(req2, resp2)
		Expect(drop2).To(BeFalse(), "request must succeed after the OOBSubnet CR is created")
		Expect(out2).NotTo(BeNil())
		Expect(contains(cidr, out2.YourIPAddr)).To(BeTrue(), "YourIPAddr %s must be within %s", out2.YourIPAddr, cidr)
	})

	It("allocates the reserved IP from an OOBSubnet CR static lease for the matching MAC", func(ctx SpecContext) {
		const (
			cidr     = "172.16.11.0/24"
			staticIP = "172.16.11.55"
			staticHN = "bmc-cr-node001"
		)

		subnet := &metaldhcpv1alpha1.OOBSubnet{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "subnet-cr-static",
				Labels: map[string]string{"oob": "true"},
			},
			Spec: metaldhcpv1alpha1.OOBSubnetSpec{
				CIDR:    cidr,
				Gateway: "172.16.11.1",
				StaticLeases: []metaldhcpv1alpha1.StaticLease{
					{MAC: dellMAC, IP: staticIP, Hostname: staticHN},
				},
			},
		}
		Expect(crClient.Create(ctx, subnet)).To(Succeed())
		DeferCleanup(crClient.Delete, subnet)

		h := buildPlugin(api.OOBConfig{
			SubnetLabels: []api.SubnetLabel{{Key: "oob", Value: "true"}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)
		req, resp := discoverRelay(mac, net.ParseIP("172.16.11.1"))

		out, drop := h(req, resp)
		Expect(drop).To(BeFalse())
		Expect(out).NotTo(BeNil())
		Expect(out.YourIPAddr.String()).To(Equal(staticIP))

		lease := &metaldhcpv1alpha1.DHCPLease{
			ObjectMeta: metav1.ObjectMeta{Name: helper.NormalizeMAC(mac)},
		}
		Eventually(ctx, Object(lease)).Should(SatisfyAll(
			HaveField("Spec.IP", staticIP),
			HaveField("Spec.Hostname", staticHN),
		))
	})

	It("does not offer the static IP from a CR to a different MAC", func(ctx SpecContext) {
		const (
			cidr     = "172.16.12.0/24"
			staticIP = "172.16.12.77"
		)

		subnet := &metaldhcpv1alpha1.OOBSubnet{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "subnet-cr-static-isolation",
				Labels: map[string]string{"oob": "true"},
			},
			Spec: metaldhcpv1alpha1.OOBSubnetSpec{
				CIDR:         cidr,
				Gateway:      "172.16.12.1",
				AllowAllMacs: true,
				StaticLeases: []metaldhcpv1alpha1.StaticLease{
					{MAC: dellMAC, IP: staticIP},
				},
			},
		}
		Expect(crClient.Create(ctx, subnet)).To(Succeed())
		DeferCleanup(crClient.Delete, subnet)

		h := buildPlugin(api.OOBConfig{
			SubnetLabels: []api.SubnetLabel{{Key: "oob", Value: "true"}},
		})

		mac, err := net.ParseMAC(hpeMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)
		req, resp := discoverRelay(mac, net.ParseIP("172.16.12.1"))

		out, drop := h(req, resp)
		Expect(drop).To(BeFalse())
		Expect(out).NotTo(BeNil())
		Expect(out.YourIPAddr.String()).NotTo(Equal(staticIP), "reserved IP must not be offered to a different MAC")
		Expect(contains(cidr, out.YourIPAddr)).To(BeTrue(), "YourIPAddr %s must still be within %s", out.YourIPAddr, cidr)
	})

	It("returns the same IP on re-lease and patches the single DHCPLease", func(ctx SpecContext) {
		const cidr = "192.168.44.0/24"

		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: cidr, Gateway: "192.168.44.1", AllowAllMacs: true}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)

		req1, resp1 := discoverRelay(mac, net.ParseIP("192.168.44.10"))
		out1, drop1 := h(req1, resp1)
		Expect(drop1).To(BeFalse())
		Expect(out1).NotTo(BeNil())
		firstIP := out1.YourIPAddr.String()

		lease := &metaldhcpv1alpha1.DHCPLease{
			ObjectMeta: metav1.ObjectMeta{Name: helper.NormalizeMAC(mac)},
		}
		Eventually(Object(lease)).Should(HaveField("Spec.IP", firstIP))

		req2, resp2 := discoverRelay(mac, net.ParseIP("192.168.44.10"))
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
				{CIDR: cidr1, Gateway: "10.0.1.1", AllowAllMacs: true},
				{CIDR: cidr2, Gateway: "10.0.2.1", AllowAllMacs: true},
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

	It("drops the request when no pool CIDR matches the relay giaddr", func(ctx SpecContext) {
		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: "192.168.45.0/24", Gateway: "192.168.45.1"}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		// Relay agent reports a giaddr not in any configured pool.
		req, resp := discoverRelay(mac, net.ParseIP("10.99.99.5"))

		out, drop := h(req, resp)
		Expect(out).To(BeNil())
		Expect(drop).To(BeTrue())
	})

	It("drops the request when the matching pool is exhausted", func(ctx SpecContext) {
		// A /31 has no usable host addresses, so allocation always fails.
		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: "10.0.0.0/31", AllowAllMacs: true}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		req, resp := discoverRelay(mac, net.ParseIP("10.0.0.0"))

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
			Subnets: []api.Subnet{{CIDR: cidr, Gateway: "10.1.1.1", BootURL: bootURL, AllowAllMacs: true}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)
		req, resp := discoverRelay(mac, net.ParseIP("10.1.1.1"))

		out, drop := h(req, resp)
		Expect(drop).To(BeFalse())
		Expect(out).NotTo(BeNil())
		Expect(out.BootFileName).To(Equal(bootURL))
	})

	It("leaves BootFileName empty when pool has no bootURL", func(ctx SpecContext) {
		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: "10.2.2.0/24", Gateway: "10.2.2.1", AllowAllMacs: true}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)
		req, resp := discoverRelay(mac, net.ParseIP("10.2.2.1"))

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
			Subnets: []api.Subnet{{CIDR: cidr, Gateway: "10.3.3.1", AllowAllMacs: true, StaticLeases: []api.StaticLease{
				{MAC: dellMAC, IP: staticIP, Hostname: staticHN},
			}}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)

		// relay from the pool subnet — static lease overrides dynamic allocation for this MAC
		req, resp := discoverRelay(mac, net.ParseIP("10.3.3.1"))
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
			Subnets: []api.Subnet{{CIDR: cidr, Gateway: "10.10.10.1", AllowAllMacs: true}},
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
			req, resp := discoverRelay(mac, net.ParseIP("10.10.10.1"))

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
			Subnets: []api.Subnet{{CIDR: cidr, Gateway: "10.10.11.1", AllowAllMacs: true}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)
		req, resp := discoverRelay(mac, net.ParseIP("10.10.11.1"))

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

	It("sets chain URL with ${uuid} template for iPXE phase 2 and stops the chain", func(ctx SpecContext) {
		const (
			cidr    = "10.20.0.0/24"
			bootURL = "https://boot-operator.example.com"
		)

		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: cidr, Gateway: "10.20.0.1", BootURL: bootURL, AllowAllMacs: true}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)

		req, resp := discoverIPXE(mac, net.ParseIP("10.20.0.1"))

		out, stop := h(req, resp)
		Expect(stop).To(BeTrue(), "chain must stop so nbp cannot overwrite option 67")
		Expect(out).NotTo(BeNil())
		Expect(out.BootFileName).To(Equal("https://boot-operator.example.com/ipxe/${uuid}"))
		Expect(out.Options.Get(dhcpv4.OptionBootfileName)).To(Equal([]byte("https://boot-operator.example.com/ipxe/${uuid}")))
	})

	It("stops chain for iPXE phase 2 even when bootURL is empty", func(ctx SpecContext) {
		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: "10.22.0.0/24", Gateway: "10.22.0.1", AllowAllMacs: true}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)

		req, resp := discoverIPXE(mac, net.ParseIP("10.22.0.1"))

		out, stop := h(req, resp)
		Expect(stop).To(BeTrue(), "chain must stop for iPXE phase even without bootURL")
		Expect(out).NotTo(BeNil())
		Expect(out.BootFileName).To(BeEmpty())
	})

	It("drops the request when no relay agent is configured (giaddr=0.0.0.0)", func(ctx SpecContext) {
		const serverVIP = "10.44.8.78"

		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: "10.5.5.0/24", Gateway: "10.5.5.1", AllowAllMacs: true}},
		})

		mac, err := net.ParseMAC(hpeMAC)
		Expect(err).NotTo(HaveOccurred())

		// Direct DISCOVER with no relay: resp.ServerIPAddr is the LB VIP set by server_id plugin.
		// No giaddr means the relay agent is absent; DROP regardless of ServerIPAddr.
		req, resp := discoverDirect(mac)
		resp.ServerIPAddr = net.ParseIP(serverVIP)

		out, drop := h(req, resp)
		Expect(drop).To(BeTrue())
		Expect(out).To(BeNil())
	})

	It("drops the request when giaddr equals server's own IP (misconfigured relay)", func(ctx SpecContext) {
		const serverVIP = "10.44.8.78"

		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: "10.5.6.0/24", Gateway: "10.5.6.1", AllowAllMacs: true}},
		})

		mac, err := net.ParseMAC(hpeMAC)
		Expect(err).NotTo(HaveOccurred())

		// Relay misconfigured: giaddr points back at the DHCP server's own VIP.
		req, resp := discoverRelay(mac, net.ParseIP(serverVIP))
		resp.ServerIPAddr = net.ParseIP(serverVIP)

		out, drop := h(req, resp)
		Expect(drop).To(BeTrue())
		Expect(out).To(BeNil())
	})
	It("sets DNS, domain, and NTP options in the OFFER", func(ctx SpecContext) {
		h := buildPlugin(api.OOBConfig{
			Subnets:    []api.Subnet{{CIDR: "10.30.0.0/24", Gateway: "10.30.0.1", AllowAllMacs: true}},
			DNSServers: []string{"8.8.8.8", "8.8.4.4"},
			Domain:     "example.com",
			NTPServers: []string{"132.163.96.1"},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)
		req, resp := discoverRelay(mac, net.ParseIP("10.30.0.1"))

		out, drop := h(req, resp)
		Expect(drop).To(BeFalse())
		Expect(out).NotTo(BeNil())

		ipStrs := func(ips []net.IP) []string {
			s := make([]string, len(ips))
			for i, ip := range ips {
				s[i] = ip.String()
			}
			return s
		}
		Expect(ipStrs(dhcpv4.GetIPs(dhcpv4.OptionDomainNameServer, out.Options))).To(ConsistOf("8.8.8.8", "8.8.4.4"))
		Expect(out.DomainName()).To(Equal("example.com"))
		Expect(ipStrs(dhcpv4.GetIPs(dhcpv4.OptionNTPServers, out.Options))).To(ConsistOf("132.163.96.1"))
	})

	It("uses per-subnet DNS and domain when set, falling back to global for other subnets", func(ctx SpecContext) {
		const (
			cidrOverride = "10.31.0.0/24"
			cidrFallback = "10.32.0.0/24"
		)
		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{
				{
					CIDR:         cidrOverride,
					Gateway:      "10.31.0.1",
					DNSServers:   []string{"1.1.1.1", "1.0.0.1"},
					Domain:       "override.example.com",
					AllowAllMacs: true,
				},
				{
					CIDR:         cidrFallback,
					Gateway:      "10.32.0.1",
					AllowAllMacs: true,
				},
			},
			DNSServers: []string{"8.8.8.8", "8.8.4.4"},
			Domain:     "global.example.com",
		})

		ipStrs := func(ips []net.IP) []string {
			s := make([]string, len(ips))
			for i, ip := range ips {
				s[i] = ip.String()
			}
			return s
		}

		mac1, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac1)
		req1, resp1 := discoverRelay(mac1, net.ParseIP("10.31.0.1"))
		out1, drop1 := h(req1, resp1)
		Expect(drop1).To(BeFalse())
		Expect(out1).NotTo(BeNil())
		Expect(ipStrs(dhcpv4.GetIPs(dhcpv4.OptionDomainNameServer, out1.Options))).To(ConsistOf("1.1.1.1", "1.0.0.1"), "subnet override DNS")
		Expect(out1.DomainName()).To(Equal("override.example.com"), "subnet override domain")

		mac2, err := net.ParseMAC(hpeMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac2)
		req2, resp2 := discoverRelay(mac2, net.ParseIP("10.32.0.1"))
		out2, drop2 := h(req2, resp2)
		Expect(drop2).To(BeFalse())
		Expect(out2).NotTo(BeNil())
		Expect(ipStrs(dhcpv4.GetIPs(dhcpv4.OptionDomainNameServer, out2.Options))).To(ConsistOf("8.8.8.8", "8.8.4.4"), "global fallback DNS")
		Expect(out2.DomainName()).To(Equal("global.example.com"), "global fallback domain")
	})

	// --- oobsubnet-mac-filter integration tests ---

	It("bootstraps a managed-by-helm OOBSubnet CR from a config subnet at startup", func(ctx SpecContext) {
		const cidr = "10.60.0.0/24"

		buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: cidr, Gateway: "10.60.0.1", BootURL: "https://boot.example.com"}},
		})

		crName := cidrToCRName(cidr)
		cr := &metaldhcpv1alpha1.OOBSubnet{}
		Expect(crClient.Get(ctx, client.ObjectKey{Name: crName}, cr)).To(Succeed())
		Expect(cr.Spec.CIDR).To(Equal(cidr))
		Expect(cr.Spec.BootURL).To(Equal("https://boot.example.com"))
		Expect(cr.Annotations).To(HaveKeyWithValue(annotationManagedBy, "helm"))
	})

	It("updates a managed-by-helm CR on second startup with changed config values", func(ctx SpecContext) {
		const cidr = "10.61.0.0/24"

		buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: cidr, Gateway: "10.61.0.1"}},
		})

		crName := cidrToCRName(cidr)
		cr := &metaldhcpv1alpha1.OOBSubnet{}
		Expect(crClient.Get(ctx, client.ObjectKey{Name: crName}, cr)).To(Succeed())
		Expect(cr.Spec.BootURL).To(BeEmpty())

		// second startup with an updated config value
		buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: cidr, Gateway: "10.61.0.1", BootURL: "https://updated-boot.example.com"}},
		})

		Expect(crClient.Get(ctx, client.ObjectKey{Name: crName}, cr)).To(Succeed())
		Expect(cr.Spec.BootURL).To(Equal("https://updated-boot.example.com"))
	})

	It("leaves an externally-managed OOBSubnet CR unchanged when same CIDR is in config", func(ctx SpecContext) {
		const cidr = "10.62.0.0/24"

		externalCR := &metaldhcpv1alpha1.OOBSubnet{
			ObjectMeta: metav1.ObjectMeta{Name: "external-cr-10-62"},
			Spec: metaldhcpv1alpha1.OOBSubnetSpec{
				CIDR:    cidr,
				Gateway: "10.62.0.1",
				BootURL: "https://external-boot.example.com",
			},
		}
		Expect(crClient.Create(ctx, externalCR)).To(Succeed())
		DeferCleanup(crClient.Delete, externalCR)

		// bootstrap with same CIDR: must not overwrite the external CR
		buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{CIDR: cidr, Gateway: "10.62.0.1", BootURL: "https://other-boot.example.com"}},
		})

		cr := &metaldhcpv1alpha1.OOBSubnet{}
		Expect(crClient.Get(ctx, client.ObjectKey{Name: "external-cr-10-62"}, cr)).To(Succeed())
		Expect(cr.Spec.BootURL).To(Equal("https://external-boot.example.com"), "external CR must not be modified")
	})

	It("silently drops DHCP request from MAC not in allowMacs", func(ctx SpecContext) {
		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{
				CIDR:    "10.51.0.0/24",
				Gateway: "10.51.0.1",
				AllowMacs: []string{helper.NormalizeMAC(func() net.HardwareAddr {
					mac, _ := net.ParseMAC(hpeMAC)
					return mac
				}())},
			}},
		})

		// Dell is not in allowMacs
		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		req, resp := discoverRelay(mac, net.ParseIP("10.51.0.1"))

		out, drop := h(req, resp)
		Expect(out).To(BeNil(), "no offer must be sent to unlisted MAC")
		Expect(drop).To(BeTrue(), "request must be dropped")
	})

	It("serves DHCP request from MAC in allowMacs", func(ctx SpecContext) {
		hpeMACParsed, err := net.ParseMAC(hpeMAC)
		Expect(err).NotTo(HaveOccurred())

		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{
				CIDR:      "10.52.0.0/24",
				Gateway:   "10.52.0.1",
				AllowMacs: []string{helper.NormalizeMAC(hpeMACParsed)},
			}},
		})

		deferDeleteLease(hpeMACParsed)
		req, resp := discoverRelay(hpeMACParsed, net.ParseIP("10.52.0.1"))

		out, drop := h(req, resp)
		Expect(drop).To(BeFalse(), "listed MAC must receive an offer")
		Expect(out).NotTo(BeNil())
		Expect(contains("10.52.0.0/24", out.YourIPAddr)).To(BeTrue())
	})

	It("drops all requests when allowMacs is empty and allowAllMacs is false (deny-all default)", func(ctx SpecContext) {
		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{
				CIDR:    "10.54.0.0/24",
				Gateway: "10.54.0.1",
				// no AllowMacs, no AllowAllMacs — deny-all by default
			}},
		})

		for _, rawMAC := range []string{dellMAC, hpeMAC, lenovoMAC} {
			mac, err := net.ParseMAC(rawMAC)
			Expect(err).NotTo(HaveOccurred())
			req, resp := discoverRelay(mac, net.ParseIP("10.54.0.1"))

			out, drop := h(req, resp)
			Expect(out).To(BeNil(), "no offer must be sent when allowAllMacs=false and allowMacs is empty (mac=%s)", rawMAC)
			Expect(drop).To(BeTrue(), "request must be dropped (mac=%s)", rawMAC)
		}
	})

	It("serves any MAC when allowAllMacs is true regardless of allowMacs", func(ctx SpecContext) {
		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{
				CIDR:         "10.55.0.0/24",
				Gateway:      "10.55.0.1",
				AllowAllMacs: true,
			}},
		})

		for _, rawMAC := range []string{dellMAC, hpeMAC, lenovoMAC} {
			mac, err := net.ParseMAC(rawMAC)
			Expect(err).NotTo(HaveOccurred())
			deferDeleteLease(mac)
			req, resp := discoverRelay(mac, net.ParseIP("10.55.0.1"))

			out, drop := h(req, resp)
			Expect(drop).To(BeFalse(), "any MAC must receive an offer when allowAllMacs=true (mac=%s)", rawMAC)
			Expect(out).NotTo(BeNil())
		}
	})

	It("serves a static lease MAC even when not in allowMacs", func(ctx SpecContext) {
		lenovoMACParsed, err := net.ParseMAC(lenovoMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(lenovoMACParsed)

		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{
				CIDR:    "10.56.0.0/24",
				Gateway: "10.56.0.1",
				// only HPE is in allowMacs; Lenovo is not — but has a static lease
				AllowMacs: []string{helper.NormalizeMAC(func() net.HardwareAddr {
					mac, _ := net.ParseMAC(hpeMAC)
					return mac
				}())},
				StaticLeases: []api.StaticLease{{MAC: lenovoMAC, IP: "10.56.0.200"}},
			}},
		})

		req, resp := discoverRelay(lenovoMACParsed, net.ParseIP("10.56.0.1"))
		out, drop := h(req, resp)
		Expect(drop).To(BeFalse(), "static lease MAC must bypass the allowMacs filter")
		Expect(out).NotTo(BeNil())
		Expect(out.YourIPAddr.String()).To(Equal("10.56.0.200"))
	})

	It("drops a non-static MAC even when another MAC has a static lease on the same subnet", func(ctx SpecContext) {
		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{
				CIDR:    "10.57.0.0/24",
				Gateway: "10.57.0.1",
				// Dell is not listed and has no static lease
				StaticLeases: []api.StaticLease{{MAC: lenovoMAC, IP: "10.57.0.200"}},
			}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		req, resp := discoverRelay(mac, net.ParseIP("10.57.0.1"))

		out, drop := h(req, resp)
		Expect(out).To(BeNil(), "non-static MAC must be dropped even when another MAC has a static lease")
		Expect(drop).To(BeTrue())
	})

	It("delays the offer by responseDelay before sending", func(ctx SpecContext) {
		const delay = 30 * time.Millisecond

		h := buildPlugin(api.OOBConfig{
			Subnets: []api.Subnet{{
				CIDR:          "10.53.0.0/24",
				Gateway:       "10.53.0.1",
				ResponseDelay: delay.String(),
				AllowAllMacs:  true,
			}},
		})

		mac, err := net.ParseMAC(dellMAC)
		Expect(err).NotTo(HaveOccurred())
		deferDeleteLease(mac)
		req, resp := discoverRelay(mac, net.ParseIP("10.53.0.1"))

		start := time.Now()
		out, drop := h(req, resp)
		elapsed := time.Since(start)

		Expect(drop).To(BeFalse())
		Expect(out).NotTo(BeNil())
		Expect(elapsed).To(BeNumerically(">=", delay), "offer must be delayed by at least responseDelay")
	})
})
