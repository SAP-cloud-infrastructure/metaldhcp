// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func oobSubnet(cidr string, leases ...StaticLease) *OOBSubnet {
	return &OOBSubnet{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec:       OOBSubnetSpec{CIDR: cidr, StaticLeases: leases},
	}
}

func sl(mac, ip string) StaticLease { return StaticLease{MAC: mac, IP: ip} }

func TestValidateCreate_Valid(t *testing.T) {
	s := oobSubnet("10.0.1.0/24", sl("00:11:22:33:44:55", "10.0.1.5"), sl("aa:bb:cc:dd:ee:ff", "10.0.1.6"))
	_, err := s.ValidateCreate()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateCreate_NoStaticLeases(t *testing.T) {
	s := oobSubnet("10.0.1.0/24")
	_, err := s.ValidateCreate()
	if err != nil {
		t.Fatalf("expected no error for empty static leases, got: %v", err)
	}
}

func TestValidateCreate_SingleOutsideCIDR(t *testing.T) {
	s := oobSubnet("10.0.1.0/24", sl("00:11:22:33:44:55", "10.99.0.5"))
	_, err := s.ValidateCreate()
	if err == nil {
		t.Fatal("expected error for IP outside CIDR, got nil")
	}
	const want = "static lease 00:11:22:33:44:55 → 10.99.0.5 is outside subnet CIDR 10.0.1.0/24"
	if err.Error() != want {
		t.Fatalf("error message mismatch\n got:  %q\nwant: %q", err.Error(), want)
	}
}

func TestValidateCreate_MultipleViolations(t *testing.T) {
	s := oobSubnet("10.0.1.0/24",
		sl("00:11:22:33:44:55", "10.99.0.5"),
		sl("aa:bb:cc:dd:ee:ff", "10.0.1.10"), // valid
		sl("11:22:33:44:55:66", "192.168.1.1"),
	)
	_, err := s.ValidateCreate()
	if err == nil {
		t.Fatal("expected error for multiple violations, got nil")
	}
	msg := err.Error()
	for _, sub := range []string{
		"static lease 00:11:22:33:44:55 → 10.99.0.5 is outside subnet CIDR 10.0.1.0/24",
		"static lease 11:22:33:44:55:66 → 192.168.1.1 is outside subnet CIDR 10.0.1.0/24",
	} {
		if !containsStr(msg, sub) {
			t.Errorf("expected error message to contain %q, got: %q", sub, msg)
		}
	}
	// Valid entry must not appear in the error
	if containsStr(msg, "10.0.1.10") {
		t.Errorf("valid IP 10.0.1.10 must not appear in error message: %q", msg)
	}
}

func TestValidateUpdate_Valid(t *testing.T) {
	s := oobSubnet("10.0.2.0/24", sl("00:11:22:33:44:55", "10.0.2.20"))
	_, err := s.ValidateUpdate(nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateUpdate_OutsideCIDR(t *testing.T) {
	s := oobSubnet("10.0.2.0/24", sl("00:11:22:33:44:55", "10.99.0.5"))
	_, err := s.ValidateUpdate(nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestValidateDelete_AlwaysAllowed(t *testing.T) {
	s := oobSubnet("10.0.3.0/24", sl("00:11:22:33:44:55", "10.99.99.99"))
	_, err := s.ValidateDelete()
	if err != nil {
		t.Fatalf("expected delete to always succeed, got: %v", err)
	}
}

func TestValidateCreate_MacListWithAllowAllRejected(t *testing.T) {
	s := oobSubnet("10.0.1.0/24")
	s.Spec.Mode = "allowAll"
	s.Spec.MacList = []string{"00:11:22:33:44:55"}
	_, err := s.ValidateCreate()
	if err == nil {
		t.Fatal("expected error for macList set with allowAll, got nil")
	}
}

func TestValidateCreate_MacListWithAllowAccepted(t *testing.T) {
	s := oobSubnet("10.0.1.0/24")
	s.Spec.Mode = "allow"
	s.Spec.MacList = []string{"00:11:22:33:44:55"}
	if _, err := s.ValidateCreate(); err != nil {
		t.Fatalf("expected no error for macList with allow, got: %v", err)
	}
}

func TestValidateCreate_EmptyMacListWithAllowAllAccepted(t *testing.T) {
	s := oobSubnet("10.0.1.0/24")
	s.Spec.Mode = "allowAll"
	if _, err := s.ValidateCreate(); err != nil {
		t.Fatalf("expected no error for allowAll with empty macList, got: %v", err)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
