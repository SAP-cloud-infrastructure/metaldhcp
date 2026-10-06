// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package oob

import (
	"context"
	"fmt"
	"net"
	"time"

	metaldhcpv1alpha1 "github.com/SAP-cloud-infrastructure/metaldhcp/api/v1alpha1"
	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/allocator"
	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/api"
	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/helper"
	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const unknownIP = "0.0.0.0"

type staticEntry struct {
	ip       net.IP
	gateway  string
	bootURL  string
	hostname string
}

type K8sClient struct {
	Client       client.Client
	Namespace    string
	SubnetLabels []api.SubnetLabel
	configPools  []allocator.Pool
	alloc        *allocator.Allocator
	statics      map[string]staticEntry // normalized MAC key -> static binding
}

func NewK8sClient(ctx context.Context, cfg *api.OOBConfig) (*K8sClient, error) {
	k := &K8sClient{
		Client:       kubernetes.GetClient(),
		Namespace:    cfg.Namespace,
		SubnetLabels: cfg.SubnetLabels,
		configPools:  configPools(cfg.Subnets),
		alloc:        allocator.New(),
		statics:      make(map[string]staticEntry),
	}
	k.seedStaticLeases(cfg.StaticLeases)
	if err := k.seedFromLeases(ctx); err != nil {
		return nil, fmt.Errorf("failed to seed allocator from DHCPLeases: %w", err)
	}
	return k, nil
}

func configPools(subnets []api.Subnet) []allocator.Pool {
	pools := make([]allocator.Pool, 0, len(subnets))
	for _, s := range subnets {
		pools = append(pools, allocator.Pool{
			CIDR:       s.CIDR,
			Gateway:    s.Gateway,
			RangeStart: s.RangeStart,
			RangeEnd:   s.RangeEnd,
			BootURL:    s.BootURL,
		})
	}
	return pools
}

// seedStaticLeases registers static MAC→IP bindings, preventing dynamic allocation from
// handing those addresses to other clients. It also pre-computes gateway/bootURL by
// looking up which config pool (if any) contains each static IP.
func (k *K8sClient) seedStaticLeases(leases []api.StaticLease) {
	for _, sl := range leases {
		mac, err := net.ParseMAC(sl.MAC)
		if err != nil {
			log.Warningf("Skipping static lease with invalid MAC %q: %v", sl.MAC, err)
			continue
		}
		ip := net.ParseIP(sl.IP)
		if ip == nil {
			log.Warningf("Skipping static lease %s with invalid IP %q", sl.MAC, sl.IP)
			continue
		}
		macKey := helper.NormalizeMAC(mac)
		k.alloc.Reserve(macKey, ip)

		entry := staticEntry{ip: ip.To4(), hostname: sl.Hostname}
		for _, p := range k.configPools {
			if helper.CheckIPInCIDR(ip, p.CIDR, log) {
				entry.gateway = p.Gateway
				entry.bootURL = p.BootURL
				break
			}
		}
		k.statics[macKey] = entry
		log.Infof("Static lease: MAC %s → %s", sl.MAC, sl.IP)
	}
}

func (k *K8sClient) lookupStaticLease(macKey string) (staticEntry, bool) {
	e, ok := k.statics[macKey]
	return e, ok
}

// seedFromLeases rebuilds in-memory allocator state from the durable DHCPLease ledger so that
// re-leases survive a server restart.
func (k *K8sClient) seedFromLeases(ctx context.Context) error {
	leases := &metaldhcpv1alpha1.DHCPLeaseList{}
	if err := k.Client.List(ctx, leases, client.InNamespace(k.Namespace)); err != nil {
		return err
	}
	for _, l := range leases.Items {
		mac, err := net.ParseMAC(l.Spec.MACAddress)
		if err != nil {
			log.Warningf("Skipping DHCPLease %s with invalid MAC %q: %v", l.Name, l.Spec.MACAddress, err)
			continue
		}
		ip := net.ParseIP(l.Spec.IP)
		if ip == nil {
			log.Warningf("Skipping DHCPLease %s with invalid IP %q", l.Name, l.Spec.IP)
			continue
		}
		k.alloc.Restore(helper.NormalizeMAC(mac), ip)
	}
	log.Infof("Seeded allocator from %d DHCPLeases", len(leases.Items))
	return nil
}

// getIP selects the pool matching poolHint and allocates an address for mac.
// requested + exactIP come from the client's own address hints (clientIP / requestedIP)
// and are decoupled from poolHint so that relayed packets can use giaddr for pool
// selection while still honoring the client's existing lease.
func (k *K8sClient) getIP(ctx context.Context, poolHint net.IP, mac net.HardwareAddr, requested net.IP, exactIP bool) (net.IP, string, string, error) {
	pool, err := k.selectPool(ctx, poolHint)
	if err != nil {
		return nil, "", "", err
	}

	leaseIP, err := k.alloc.Allocate(*pool, helper.NormalizeMAC(mac), requested, exactIP)
	if err != nil {
		return nil, "", "", fmt.Errorf("allocation failed in pool %s: %w", pool.CIDR, err)
	}
	return leaseIP, pool.Gateway, pool.BootURL, nil
}

// selectPool returns the pool whose CIDR contains ipaddr, drawing from config-defined pools when
// present, otherwise from labeled OOBSubnet CRs. When ipaddr carries no hint (0.0.0.0) it falls
// back to the sole pool, erroring if the choice is ambiguous.
func (k *K8sClient) selectPool(ctx context.Context, ipaddr net.IP) (*allocator.Pool, error) {
	pools := k.configPools
	if len(pools) == 0 {
		crPools, err := k.oobSubnetPools(ctx)
		if err != nil {
			return nil, err
		}
		pools = crPools
	}
	if len(pools) == 0 {
		return nil, fmt.Errorf("no pools configured and no OOBSubnet found in %s", k.Namespace)
	}

	if ipaddr == nil || ipaddr.String() == unknownIP {
		if len(pools) == 1 {
			return &pools[0], nil
		}
		return nil, fmt.Errorf("cannot select pool without an address hint: %d candidates", len(pools))
	}

	for i := range pools {
		if helper.CheckIPInCIDR(ipaddr, pools[i].CIDR, log) {
			return &pools[i], nil
		}
	}
	return nil, fmt.Errorf("no pool CIDR contains %s", ipaddr)
}

func (k *K8sClient) oobSubnetPools(ctx context.Context) ([]allocator.Pool, error) {
	labels := make(map[string]string, len(k.SubnetLabels))
	for _, l := range k.SubnetLabels {
		labels[l.Key] = l.Value
	}

	list := &metaldhcpv1alpha1.OOBSubnetList{}
	if err := k.Client.List(ctx, list, client.InNamespace(k.Namespace), client.MatchingLabels(labels)); err != nil {
		return nil, fmt.Errorf("failed to list OOBSubnets: %w", err)
	}

	pools := make([]allocator.Pool, 0, len(list.Items))
	for _, s := range list.Items {
		pools = append(pools, allocator.Pool{
			CIDR:       s.Spec.CIDR,
			Gateway:    s.Spec.Gateway,
			RangeStart: s.Spec.RangeStart,
			RangeEnd:   s.Spec.RangeEnd,
			BootURL:    s.Spec.BootURL,
		})
	}
	return pools, nil
}

// applyLease upserts the DHCPLease for a MAC. The object name is the normalized MAC so re-leases
// patch the same object and never create duplicates.
func (k *K8sClient) applyLease(ctx context.Context, mac net.HardwareAddr, ip net.IP, gateway, clientID, hostname string, leaseTime time.Duration) error {
	lease := &metaldhcpv1alpha1.DHCPLease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helper.NormalizeMAC(mac),
			Namespace: k.Namespace,
		},
	}

	result, err := controllerutil.CreateOrPatch(ctx, k.Client, lease, func() error {
		lease.Spec.MACAddress = mac.String()
		lease.Spec.IP = ip.String()
		lease.Spec.Gateway = gateway
		lease.Spec.ClientID = clientID
		lease.Spec.Hostname = hostname
		if leaseTime > 0 {
			lease.Spec.LeaseTime = &metav1.Duration{Duration: leaseTime}
		}
		return nil
	})
	if err != nil {
		return err
	}

	log.Infof("DHCPLease %s/%s %s (MAC %s, IP %s)", k.Namespace, lease.Name, result, mac, ip)
	return nil
}
