// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package oob

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	metaldhcpv1alpha1 "github.com/SAP-cloud-infrastructure/metaldhcp/api/v1alpha1"
	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/allocator"
	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/api"
	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/helper"
	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/kubernetes"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// allocationError carries a structured event reason alongside the underlying error so
// handler4 can emit the right Kubernetes Event reason without string matching.
type allocationError struct {
	reason string
	err    error
}

func (e *allocationError) Error() string { return e.err.Error() }
func (e *allocationError) Unwrap() error { return e.err }

var (
	globalRecorder record.EventRecorder
	globalPodRef   *corev1.Pod
)

// SetRecorder wires the Kubernetes event recorder used to emit Warning events when a
// DISCOVER/REQUEST cannot be served. Call once from main after InitClient.
func SetRecorder(r record.EventRecorder, pod *corev1.Pod) {
	globalRecorder = r
	globalPodRef = pod
}

type staticEntry struct {
	ip       net.IP
	gateway  string
	bootURL  string
	hostname string
}

type K8sClient struct {
	Client       client.Client
	SubnetLabels []api.SubnetLabel
	configPools  []allocator.Pool
	alloc        *allocator.Allocator
	statics      map[string]staticEntry // normalized MAC key -> static binding
	dnsServers   []net.IP
	domain       string
	ntpServers   []net.IP
}

func NewK8sClient(ctx context.Context, cfg *api.OOBConfig) (*K8sClient, error) {
	k := &K8sClient{
		Client:       kubernetes.GetClient(),
		SubnetLabels: cfg.SubnetLabels,
		alloc:        allocator.New(),
		statics:      make(map[string]staticEntry),
		domain:       cfg.Domain,
	}
	for _, s := range cfg.DNSServers {
		if ip := net.ParseIP(s); ip != nil {
			k.dnsServers = append(k.dnsServers, ip)
		}
	}
	for _, s := range cfg.NTPServers {
		if ip := net.ParseIP(s); ip != nil {
			k.ntpServers = append(k.ntpServers, ip)
		}
	}
	k.configPools = k.seedConfigSubnets(cfg.Subnets)
	if err := k.seedFromLeases(ctx); err != nil {
		return nil, fmt.Errorf("failed to seed allocator from DHCPLeases: %w", err)
	}
	return k, nil
}

// seedConfigSubnets converts inline Subnet config entries into allocator Pools and seeds
// each subnet's static leases. Gateway and bootURL for static entries come directly from
// the parent subnet — no pool scan needed.
func (k *K8sClient) seedConfigSubnets(subnets []api.Subnet) []allocator.Pool {
	pools := make([]allocator.Pool, 0, len(subnets))
	for _, s := range subnets {
		var lt time.Duration
		if s.LeaseTime != "" {
			if d, err := time.ParseDuration(s.LeaseTime); err == nil {
				lt = d
			} else {
				log.Warningf("Ignoring invalid leaseTime %q for subnet %s: %v", s.LeaseTime, s.CIDR, err)
			}
		}
		pool := allocator.Pool{
			CIDR:       s.CIDR,
			Gateway:    s.Gateway,
			RangeStart: s.RangeStart,
			RangeEnd:   s.RangeEnd,
			BootURL:    s.BootURL,
			LeaseTime:  lt,
		}
		pools = append(pools, pool)
		k.seedStaticLeases(s.StaticLeases, pool.Gateway, pool.BootURL)
	}
	return pools
}

// seedStaticLeases registers static MAC→IP bindings into the allocator and statics map.
// gateway and bootURL are passed directly from the parent subnet — no pool scan needed.
// Already-seen MAC keys are skipped so this is safe to call on every request for CR-based subnets.
func (k *K8sClient) seedStaticLeases(leases []api.StaticLease, gateway, bootURL string) {
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
		if _, ok := k.statics[macKey]; ok {
			continue
		}
		k.alloc.Reserve(macKey, ip)
		k.statics[macKey] = staticEntry{ip: ip.To4(), gateway: gateway, bootURL: bootURL, hostname: sl.Hostname}
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
	if err := k.Client.List(ctx, leases); err != nil {
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
func (k *K8sClient) getIP(ctx context.Context, poolHint net.IP, mac net.HardwareAddr, requested net.IP, exactIP bool) (net.IP, string, string, time.Duration, error) {
	pool, err := k.selectPool(ctx, poolHint)
	if err != nil {
		return nil, "", "", 0, err
	}

	leaseIP, err := k.alloc.Allocate(*pool, helper.NormalizeMAC(mac), requested, exactIP)
	if err != nil {
		return nil, "", "", 0, &allocationError{reason: "PoolExhausted", err: fmt.Errorf("allocation failed in pool %s: %w", pool.CIDR, err)}
	}
	return leaseIP, pool.Gateway, pool.BootURL, pool.LeaseTime, nil
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
		return nil, &allocationError{reason: "NoPoolFound", err: fmt.Errorf("no pools configured and no OOBSubnets found")}
	}

	if ipaddr == nil || ipaddr.IsUnspecified() {
		if len(pools) == 1 {
			return &pools[0], nil
		}
		return nil, &allocationError{reason: "NoPoolFound", err: fmt.Errorf("cannot select pool without an address hint: %d candidates", len(pools))}
	}

	for i := range pools {
		if helper.CheckIPInCIDR(ipaddr, pools[i].CIDR, log) {
			return &pools[i], nil
		}
	}
	return nil, &allocationError{reason: "NoPoolFound", err: fmt.Errorf("no pool CIDR contains %s", ipaddr)}
}

func (k *K8sClient) oobSubnetPools(ctx context.Context) ([]allocator.Pool, error) {
	labels := make(map[string]string, len(k.SubnetLabels))
	for _, l := range k.SubnetLabels {
		labels[l.Key] = l.Value
	}

	list := &metaldhcpv1alpha1.OOBSubnetList{}
	if err := k.Client.List(ctx, list, client.MatchingLabels(labels)); err != nil {
		return nil, fmt.Errorf("failed to list OOBSubnets: %w", err)
	}

	pools := make([]allocator.Pool, 0, len(list.Items))
	for _, s := range list.Items {
		var lt time.Duration
		if s.Spec.LeaseTime != nil {
			lt = s.Spec.LeaseTime.Duration
		}
		pools = append(pools, allocator.Pool{
			CIDR:       s.Spec.CIDR,
			Gateway:    s.Spec.Gateway,
			RangeStart: s.Spec.RangeStart,
			RangeEnd:   s.Spec.RangeEnd,
			BootURL:    s.Spec.BootURL,
			LeaseTime:  lt,
		})
		if len(s.Spec.StaticLeases) > 0 {
			leases := make([]api.StaticLease, len(s.Spec.StaticLeases))
			for i, sl := range s.Spec.StaticLeases {
				leases[i] = api.StaticLease{MAC: sl.MAC, IP: sl.IP, Hostname: sl.Hostname}
			}
			k.seedStaticLeases(leases, s.Spec.Gateway, s.Spec.BootURL)
		}
	}
	return pools, nil
}

// applyLease upserts the DHCPLease for a MAC. The object name is the normalized MAC so re-leases
// patch the same object and never create duplicates. vendor is stored as an annotation when
// vendor-lookup is enabled and non-empty.
func (k *K8sClient) applyLease(ctx context.Context, mac net.HardwareAddr, ip net.IP, gateway, clientID, hostname, vendor string, leaseTime time.Duration) error {
	mutate := func(lease *metaldhcpv1alpha1.DHCPLease) {
		lease.Spec.MACAddress = mac.String()
		lease.Spec.IP = ip.String()
		lease.Spec.Gateway = gateway
		lease.Spec.ClientID = clientID
		lease.Spec.Hostname = hostname
		if leaseTime > 0 {
			lease.Spec.LeaseTime = &metav1.Duration{Duration: leaseTime}
		}
		if vendor != "" {
			if lease.Annotations == nil {
				lease.Annotations = make(map[string]string)
			}
			lease.Annotations["dhcp.metal.ironcore.dev/vendor"] = vendor
		}
	}

	newLease := func() *metaldhcpv1alpha1.DHCPLease {
		return &metaldhcpv1alpha1.DHCPLease{
			ObjectMeta: metav1.ObjectMeta{
				Name: helper.NormalizeMAC(mac),
			},
		}
	}

	lease := newLease()
	result, err := controllerutil.CreateOrPatch(ctx, k.Client, lease, func() error {
		mutate(lease)
		return nil
	})
	if apierrors.IsAlreadyExists(err) {
		lease = newLease()
		result, err = controllerutil.CreateOrPatch(ctx, k.Client, lease, func() error {
			mutate(lease)
			return nil
		})
	}
	if err != nil {
		return err
	}

	log.Infof("DHCPLease %s %s (MAC %s, IP %s)", lease.Name, result, mac, ip)
	return nil
}

// emitEvent fires a Warning Kubernetes event on the metaldhcp Pod when a DHCP request
// cannot be served. It is a no-op when no recorder has been wired (e.g. in tests that
// don't test events).
func (k *K8sClient) emitEvent(mac net.HardwareAddr, giaddr net.IP, vendor string, err error) {
	if globalRecorder == nil || globalPodRef == nil {
		return
	}
	reason := "AllocationFailed"
	var ae *allocationError
	if errors.As(err, &ae) {
		reason = ae.reason
	}
	globalRecorder.Eventf(globalPodRef, corev1.EventTypeWarning, reason,
		"unmatched DHCP request: MAC %s%s giaddr %s: %v", mac, vendorTag(vendor), giaddr, err)
}
