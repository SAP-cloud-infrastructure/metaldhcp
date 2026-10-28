// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package helper

import (
	"net"
	"strings"

	"github.com/sirupsen/logrus"
)

// NormalizeMAC renders a MAC as a lowercase, separator-free string suitable for use
// as a Kubernetes object name and allocator key (e.g. "3c:ec:ef:aa:bb:cc" -> "3cecefaabbcc").
func NormalizeMAC(mac net.HardwareAddr) string {
	return NormalizeMACString(mac.String())
}

// NormalizeMACString canonicalizes any MAC string form to the same lowercase, separator-free
// key as NormalizeMAC, tolerating ":", "-" and "." separators and mixed case (or none, if the
// value is already normalized). Use it to compare config/CR MAC values against a normalized key.
func NormalizeMACString(s string) string {
	return strings.ToLower(strings.NewReplacer(":", "", "-", "", ".", "").Replace(s))
}

// CheckIPInCIDR reports whether ip falls within cidrStr.
func CheckIPInCIDR(ip net.IP, cidrStr string, log *logrus.Entry) bool {
	_, cidrNet, err := net.ParseCIDR(cidrStr)
	if err != nil {
		log.Errorf("Failed to parse CIDR %q: %v", cidrStr, err)
		return false
	}
	return cidrNet.Contains(ip)
}
