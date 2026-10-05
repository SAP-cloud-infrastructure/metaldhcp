// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package helper

import (
	"net"
	"strings"

	"github.com/sirupsen/logrus"
)

// DHCPPacket is the minimal interface the printer needs from a DHCP message.
type DHCPPacket interface {
	Summary() string
}

// NormalizeMAC renders a MAC as a lowercase, separator-free string suitable for use
// as a Kubernetes object name and allocator key (e.g. "3c:ec:ef:aa:bb:cc" -> "3cecefaabbcc").
func NormalizeMAC(mac net.HardwareAddr) string {
	return strings.ToLower(strings.ReplaceAll(mac.String(), ":", ""))
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
