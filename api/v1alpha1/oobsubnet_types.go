// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OOBSubnet defines an IPv4 pool for out-of-band / BMC management ports. Each
// instance selects subnets by label, allocates addresses from them, and optionally
// filters by MAC allowlist or delays offers to let other instances respond first.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="CIDR",type=string,JSONPath=`.spec.cidr`
// +kubebuilder:printcolumn:name="Gateway",type=string,JSONPath=`.spec.gateway`,priority=1
type OOBSubnet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec OOBSubnetSpec `json:"spec,omitempty"`
}

// StaticLease pins a MAC address to a fixed IP within the subnet, bypassing dynamic allocation.
type StaticLease struct {
	// MAC is the hardware address of the client, e.g. "00:11:22:33:44:55".
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$`
	MAC string `json:"mac"`

	// IP is the fixed IPv4 address to offer to this MAC.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Format=ipv4
	IP string `json:"ip"`

	// Hostname is the optional hostname to include in the DHCP offer (option 12).
	Hostname string `json:"hostname,omitempty"`
}

type OOBSubnetSpec struct {
	// CIDR is the IPv4 network from which addresses are allocated, e.g. "10.0.1.0/24".
	// +kubebuilder:validation:Required
	CIDR string `json:"cidr"`

	// Gateway, when set, is excluded from allocation and offered as the router.
	Gateway string `json:"gateway,omitempty"`

	// LeaseTime is the default lease duration for addresses from this subnet. When
	// unset the server-wide lease_time plugin value applies.
	LeaseTime *metav1.Duration `json:"leaseTime,omitempty"`

	// RangeStart and RangeEnd optionally narrow allocation to a window within CIDR
	// (inclusive). When unset the whole usable host range is used.
	RangeStart string `json:"rangeStart,omitempty"`
	RangeEnd   string `json:"rangeEnd,omitempty"`

	// BootURL, when set, is sent as the DHCP boot file URL (BootFileName field) in
	// the offer/ack. Used for per-pool UEFI HTTP boot or iPXE chain-loading.
	BootURL string `json:"bootURL,omitempty"`

	// StaticLeases pins specific MACs to fixed IPs within this subnet, bypassing dynamic allocation.
	StaticLeases []StaticLease `json:"staticLeases,omitempty"`

	// DNSServers overrides the global DNS server list (option 6) for clients in this subnet.
	// When empty the global dnsServers value applies.
	DNSServers []string `json:"dnsServers,omitempty"`

	// Domain overrides the global DNS search domain (option 15) for clients in this subnet.
	// When empty the global domain value applies.
	Domain string `json:"domain,omitempty"`

	// Mode selects the MAC filter posture: allowAll serves every MAC (macList ignored),
	// allow serves only MACs in macList (empty => serve nobody), deny serves every MAC
	// except those in macList.
	// +kubebuilder:validation:Enum=allowAll;allow;deny
	// +kubebuilder:default=allowAll
	Mode string `json:"mode,omitempty"`

	// MacList is the MAC set the filter mode operates on. Entries may be colon-, dash-, or
	// separator-free, any case; they are normalized before matching. Ignored when Mode is
	// allowAll.
	// +kubebuilder:validation:Optional
	MacList []string `json:"macList,omitempty"`

	// ResponseDelay, when non-zero, delays the DHCP offer by the given duration.
	// Use on catch-all / fallback instances so that instances with a tighter filter
	// can win the race on shared BMC subnets.
	// +kubebuilder:validation:Optional
	ResponseDelay *metav1.Duration `json:"responseDelay,omitempty"`
}

// +kubebuilder:object:root=true

type OOBSubnetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OOBSubnet `json:"items"`
}
