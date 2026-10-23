// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"fmt"
	"net"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// ValidateCreate validates a new OOBSubnet.
func (s *OOBSubnet) ValidateCreate() (admission.Warnings, error) {
	return nil, s.validateStaticLeases()
}

// ValidateUpdate validates an updated OOBSubnet.
func (s *OOBSubnet) ValidateUpdate(_ runtime.Object) (admission.Warnings, error) {
	return nil, s.validateStaticLeases()
}

// ValidateDelete is a no-op; deletions are always permitted.
func (s *OOBSubnet) ValidateDelete() (admission.Warnings, error) {
	return nil, nil
}

func (s *OOBSubnet) validateStaticLeases() error {
	if len(s.Spec.StaticLeases) == 0 {
		return nil
	}
	_, cidrNet, err := net.ParseCIDR(s.Spec.CIDR)
	if err != nil {
		return field.Invalid(field.NewPath("spec", "cidr"), s.Spec.CIDR, "invalid CIDR")
	}

	var errs []string
	for _, sl := range s.Spec.StaticLeases {
		ip := net.ParseIP(sl.IP)
		if ip == nil {
			errs = append(errs, fmt.Sprintf("static lease %s has invalid IP %q", sl.MAC, sl.IP))
			continue
		}
		if !cidrNet.Contains(ip) {
			errs = append(errs, fmt.Sprintf("static lease %s → %s is outside subnet CIDR %s", sl.MAC, sl.IP, s.Spec.CIDR))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}
