// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package oob

import (
	"context"
	"time"

	metaldhcpv1alpha1 "github.com/SAP-cloud-infrastructure/metaldhcp/api/v1alpha1"
	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("periodic subnet resync", func() {
	It("publishes Lease CRs for static leases in a Subnet CR created after startup", func(ctx SpecContext) {
		cfg := &api.OOBConfig{
			PublishStaticLeases: true,
			ResyncInterval:      "200ms",
		}
		k8sCtx, cancel := context.WithCancel(ctx)
		DeferCleanup(cancel)

		_, err := NewK8sClient(k8sCtx, cfg)
		Expect(err).NotTo(HaveOccurred())

		// Create a Subnet CR with a static lease after the client is running.
		subnet := &metaldhcpv1alpha1.Subnet{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "resync-test-"},
			Spec: metaldhcpv1alpha1.SubnetSpec{
				CIDR:    "10.11.12.0/24",
				Gateway: "10.11.12.1",
				StaticLeases: []metaldhcpv1alpha1.StaticLease{
					{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.11.12.200"},
				},
			},
		}
		Expect(crClient.Create(ctx, subnet)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			_ = crClient.Delete(ctx, subnet)
		})

		// Within a couple of resync ticks, the Lease CR should appear.
		leaseList := &metaldhcpv1alpha1.LeaseList{}
		Eventually(func(g Gomega) {
			g.Expect(crClient.List(ctx, leaseList)).To(Succeed())
			g.Expect(leaseList.Items).To(ContainElement(
				HaveField("Spec.MACAddress", "aa:bb:cc:dd:ee:ff"),
			))
		}, 2*time.Second, 50*time.Millisecond).Should(Succeed())

		DeferCleanup(func(ctx SpecContext) {
			for i := range leaseList.Items {
				if leaseList.Items[i].Spec.MACAddress == "aa:bb:cc:dd:ee:ff" {
					_ = crClient.Delete(ctx, &leaseList.Items[i])
				}
			}
		})
	})

	It("publishes Lease CRs when static leases are added to an existing Subnet CR after startup", func(ctx SpecContext) {
		// Create a Subnet CR with no static leases first.
		subnet := &metaldhcpv1alpha1.Subnet{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "resync-update-"},
			Spec: metaldhcpv1alpha1.SubnetSpec{
				CIDR:    "10.11.13.0/24",
				Gateway: "10.11.13.1",
			},
		}
		Expect(crClient.Create(ctx, subnet)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			_ = crClient.Delete(ctx, subnet)
		})

		cfg := &api.OOBConfig{
			PublishStaticLeases: true,
			ResyncInterval:      "200ms",
		}
		k8sCtx, cancel := context.WithCancel(ctx)
		DeferCleanup(cancel)

		_, err := NewK8sClient(k8sCtx, cfg)
		Expect(err).NotTo(HaveOccurred())

		// Now add a static lease to the existing Subnet CR.
		patch := client.MergeFrom(subnet.DeepCopy())
		subnet.Spec.StaticLeases = []metaldhcpv1alpha1.StaticLease{
			{MAC: "11:22:33:44:55:66", IP: "10.11.13.200"},
		}
		Expect(crClient.Patch(ctx, subnet, patch)).To(Succeed())

		leaseList := &metaldhcpv1alpha1.LeaseList{}
		Eventually(func(g Gomega) {
			g.Expect(crClient.List(ctx, leaseList)).To(Succeed())
			g.Expect(leaseList.Items).To(ContainElement(
				HaveField("Spec.MACAddress", "11:22:33:44:55:66"),
			))
		}, 2*time.Second, 50*time.Millisecond).Should(Succeed())

		DeferCleanup(func(ctx SpecContext) {
			for i := range leaseList.Items {
				if leaseList.Items[i].Spec.MACAddress == "11:22:33:44:55:66" {
					_ = crClient.Delete(ctx, &leaseList.Items[i])
				}
			}
		})
	})
})
