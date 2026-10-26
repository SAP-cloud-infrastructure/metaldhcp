// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package oob

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
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

const annotationManagedBy = "dhcp.metal.ironcore.dev/managed-by"

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
	ip         net.IP
	gateway    string
	bootURL    string
	hostname   string
	dnsServers []net.IP
	domain     string
}

type K8sClient struct {
	Client       client.Client
	SubnetLabels []api.SubnetLabel
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
	if err := k.bootstrapConfigSubnets(ctx, cfg.Subnets); err != nil {
		return nil, fmt.Errorf("failed to bootstrap config subnets: %w", err)
	}
	if err := k.seedFromLeases(ctx); err != nil {
		return nil, fmt.Errorf("failed to seed allocator from DHCPLeases: %w", err)
	}
	return k, nil
}

// cidrToCRName returns a deterministic Kubernetes object name for a config-bootstrapped
// OOBSubnet CR, e.g. "oob-10-0-1-0-24" for "10.0.1.0/24".
func cidrToCRName(cidr string) string {
	return "oob-" + strings.NewReplacer(".", "-", "/", "-").Replace(cidr)
}

// bootstrapConfigSubnets syncs inline config subnets to OOBSubnet CRs at startup.
// Three-way logic per subnet CIDR:
//   - no CR with that CIDR → create one with annotation managed-by: helm
//   - CR with annotation → update from current config values
//   - CR without annotation (external/netbox) → leave unchanged
func (k *K8sClient) bootstrapConfigSubnets(ctx context.Context, subnets []api.Subnet) error {
	if len(subnets) == 0 {
		return nil
	}

	// list all existing CRs to build a CIDR → CR map (handles any naming convention)
	list := &metaldhcpv1alpha1.OOBSubnetList{}
	if err := k.Client.List(ctx, list); err != nil {
		return fmt.Errorf("failed to list OOBSubnets: %w", err)
	}
	byCIDR := make(map[string]*metaldhcpv1alpha1.OOBSubnet, len(list.Items))
	for i := range list.Items {
		byCIDR[list.Items[i].Spec.CIDR] = &list.Items[i]
	}

	for _, s := range subnets {
		if err := k.bootstrapSubnet(ctx, s, byCIDR[s.CIDR]); err != nil {
			return err
		}
	}
	return nil
}

func (k *K8sClient) bootstrapSubnet(ctx context.Context, s api.Subnet, existing *metaldhcpv1alpha1.OOBSubnet) error {
	if existing == nil {
		cr := &metaldhcpv1alpha1.OOBSubnet{
			ObjectMeta: metav1.ObjectMeta{Name: cidrToCRName(s.CIDR)},
		}
		k.applyConfigToCR(s, cr)
		if err := k.Client.Create(ctx, cr); err != nil {
			return fmt.Errorf("failed to bootstrap OOBSubnet for CIDR %s: %w", s.CIDR, err)
		}
		log.Infof("Bootstrapped OOBSubnet %s from config", cr.Name)
		return nil
	}

	if existing.Annotations[annotationManagedBy] != "helm" {
		log.Infof("Skipping OOBSubnet %s (CIDR %s): not managed-by helm", existing.Name, s.CIDR)
		return nil
	}

	patch := client.MergeFrom(existing.DeepCopy())
	k.applyConfigToCR(s, existing)
	if err := k.Client.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("failed to update OOBSubnet %s: %w", existing.Name, err)
	}
	log.Infof("Updated OOBSubnet %s from config", existing.Name)
	return nil
}

// applyConfigToCR writes all fields from a config Subnet onto an OOBSubnet CR, including
// the managed-by annotation and any SubnetLabels so the CR is found by oobSubnetPools.
func (k *K8sClient) applyConfigToCR(s api.Subnet, cr *metaldhcpv1alpha1.OOBSubnet) {
	if cr.Labels == nil {
		cr.Labels = make(map[string]string, len(k.SubnetLabels))
	}
	for _, l := range k.SubnetLabels {
		cr.Labels[l.Key] = l.Value
	}
	if cr.Annotations == nil {
		cr.Annotations = make(map[string]string)
	}
	cr.Annotations[annotationManagedBy] = "helm"

	var lt *metav1.Duration
	if s.LeaseTime != "" {
		if d, err := time.ParseDuration(s.LeaseTime); err == nil {
			lt = &metav1.Duration{Duration: d}
		} else {
			log.Warningf("Ignoring invalid leaseTime %q for subnet %s: %v", s.LeaseTime, s.CIDR, err)
		}
	}
	var rd *metav1.Duration
	if s.ResponseDelay != "" {
		if d, err := time.ParseDuration(s.ResponseDelay); err == nil {
			rd = &metav1.Duration{Duration: d}
		} else {
			log.Warningf("Ignoring invalid responseDelay %q for subnet %s: %v", s.ResponseDelay, s.CIDR, err)
		}
	}
	staticLeases := make([]metaldhcpv1alpha1.StaticLease, len(s.StaticLeases))
	for i, sl := range s.StaticLeases {
		staticLeases[i] = metaldhcpv1alpha1.StaticLease{MAC: sl.MAC, IP: sl.IP, Hostname: sl.Hostname}
	}
	cr.Spec = metaldhcpv1alpha1.OOBSubnetSpec{
		CIDR:          s.CIDR,
		Gateway:       s.Gateway,
		RangeStart:    s.RangeStart,
		RangeEnd:      s.RangeEnd,
		LeaseTime:     lt,
		BootURL:       s.BootURL,
		StaticLeases:  staticLeases,
		DNSServers:    s.DNSServers,
		Domain:        s.Domain,
		AllowMacs:     s.AllowMacs,
		ResponseDelay: rd,
		AllowAllMacs:  s.AllowAllMacs,
	}
}

// seedStaticLeases registers static MAC→IP bindings into the allocator and statics map.
// gateway, bootURL, dnsServers, and domain are passed directly from the parent subnet.
// Already-seen MAC keys are skipped so this is safe to call on every request for CR-based subnets.
func (k *K8sClient) seedStaticLeases(leases []api.StaticLease, gateway, bootURL string, dnsServers []net.IP, domain string) {
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
		k.statics[macKey] = staticEntry{ip: ip.To4(), gateway: gateway, bootURL: bootURL, hostname: sl.Hostname, dnsServers: dnsServers, domain: domain}
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

// selectPool returns the pool whose CIDR contains ipaddr from labeled OOBSubnet CRs.
// When ipaddr carries no hint (0.0.0.0) it falls back to the sole pool, erroring if
// the choice is ambiguous.
func (k *K8sClient) selectPool(ctx context.Context, ipaddr net.IP) (*allocator.Pool, error) {
	pools, err := k.oobSubnetPools(ctx)
	if err != nil {
		return nil, err
	}
	if len(pools) == 0 {
		return nil, &allocationError{reason: "NoPoolFound", err: fmt.Errorf("no OOBSubnets found")}
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
		var rd time.Duration
		if s.Spec.ResponseDelay != nil {
			rd = s.Spec.ResponseDelay.Duration
		}
		var dns []net.IP
		for _, srv := range s.Spec.DNSServers {
			if ip := net.ParseIP(srv); ip != nil {
				dns = append(dns, ip)
			}
		}
		pools = append(pools, allocator.Pool{
			CIDR:          s.Spec.CIDR,
			Gateway:       s.Spec.Gateway,
			RangeStart:    s.Spec.RangeStart,
			RangeEnd:      s.Spec.RangeEnd,
			BootURL:       s.Spec.BootURL,
			LeaseTime:     lt,
			DNSServers:    dns,
			Domain:        s.Spec.Domain,
			AllowMacs:     s.Spec.AllowMacs,
			ResponseDelay: rd,
			AllowAllMacs:  s.Spec.AllowAllMacs,
		})
		if len(s.Spec.StaticLeases) > 0 {
			leases := make([]api.StaticLease, len(s.Spec.StaticLeases))
			for i, sl := range s.Spec.StaticLeases {
				leases[i] = api.StaticLease{MAC: sl.MAC, IP: sl.IP, Hostname: sl.Hostname}
			}
			k.seedStaticLeases(leases, s.Spec.Gateway, s.Spec.BootURL, dns, s.Spec.Domain)
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
			expires := metav1.NewTime(time.Now().Add(leaseTime))
			lease.Spec.LeaseExpiresAt = &expires
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
