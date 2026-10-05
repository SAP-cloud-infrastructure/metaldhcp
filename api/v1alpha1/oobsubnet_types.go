// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OOBSubnet defines an IPv4 pool for out-of-band / BMC management ports. It is
// synced into the cluster by an external entity (the onboarding operator / NetBox
// sync) and is read-only to metaldhcp. The oob plugin selects the subnet whose
// CIDR matches the DHCP relay/link information and allocates a free address from it.
//
// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="CIDR",type=string,JSONPath=`.spec.cidr`
// +kubebuilder:printcolumn:name="Gateway",type=string,JSONPath=`.spec.gateway`,priority=1
type OOBSubnet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec OOBSubnetSpec `json:"spec,omitempty"`
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
}

// +kubebuilder:object:root=true

type OOBSubnetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OOBSubnet `json:"items"`
}
