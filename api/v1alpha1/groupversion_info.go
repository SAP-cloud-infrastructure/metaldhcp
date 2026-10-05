// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

// +kubebuilder:object:generate=true
// +groupName=dhcp.metal.ironcore.dev
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	GroupVersion  = schema.GroupVersion{Group: "dhcp.metal.ironcore.dev", Version: "v1alpha1"}
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	AddToScheme   = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&OOBSubnet{},
		&OOBSubnetList{},
		&DHCPLease{},
		&DHCPLeaseList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
