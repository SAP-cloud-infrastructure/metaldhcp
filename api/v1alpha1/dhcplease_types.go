// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DHCPLease records an IPv4 lease issued by metaldhcp. It is the durable allocation
// ledger (the oob plugin rebuilds its in-memory allocator from these on startup) and
// the event that triggers the onboarding flow in the onboarding operator. metaldhcp
// owns these objects; the object name is the normalized MAC (e.g. "3cecefaabbcc") so
// re-leases patch the same object.
//
// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="MAC",type=string,JSONPath=`.spec.macAddress`
// +kubebuilder:printcolumn:name="Hostname",type=string,JSONPath=`.spec.hostname`
// +kubebuilder:printcolumn:name="ClientID",type=string,JSONPath=`.spec.clientID`,priority=1
// +kubebuilder:printcolumn:name="IP",type=string,JSONPath=`.spec.ip`
// +kubebuilder:printcolumn:name="Gateway",type=string,JSONPath=`.spec.gateway`
// +kubebuilder:printcolumn:name="Vendor",type=string,JSONPath=`.metadata.annotations['dhcp\.metal\.ironcore\.dev/vendor']`,priority=1
// +kubebuilder:selectablefield:JSONPath=`.spec.macAddress`
// +kubebuilder:selectablefield:JSONPath=`.spec.ip`
type DHCPLease struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec DHCPLeaseSpec `json:"spec,omitempty"`
}

type DHCPLeaseSpec struct {
	// +kubebuilder:validation:Required
	MACAddress string `json:"macAddress"`

	// +kubebuilder:validation:Required
	IP string `json:"ip"`

	// Gateway is the pool gateway for the assigned address.
	Gateway string `json:"gateway,omitempty"`

	// LeaseTime is the lease duration offered for this address. Updated on re-lease.
	LeaseTime *metav1.Duration `json:"leaseTime,omitempty"`

	// ClientID is the DHCPv4 client identifier (option 61) from the request, if present.
	ClientID string `json:"clientID,omitempty"`

	// Hostname is the client hostname, either from option 12 in the request or from a
	// static lease config entry.
	Hostname string `json:"hostname,omitempty"`
}

// +kubebuilder:object:root=true

type DHCPLeaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DHCPLease `json:"items"`
}
