// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package api

import "time"

// DefaultReclaimGrace is the grace period past a dynamic lease's expiry before its address
// may be reclaimed for another client, used when OOBConfig.ReclaimGrace is unset.
const DefaultReclaimGrace = time.Hour

type SubnetLabel struct {
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
}

type StaticLease struct {
	MAC      string `yaml:"mac"`
	IP       string `yaml:"ip"`
	Hostname string `yaml:"hostname,omitempty"`
}

// Subnet is an inline pool definition, managed via the deployment pipeline / Helm values. It
// is the config-defined alternative to a synced OOBSubnet CR. At startup, each Subnet is
// bootstrapped to an OOBSubnet CR annotated managed-by: helm; all runtime pool selection then
// uses CRs exclusively.
type Subnet struct {
	CIDR          string        `yaml:"cidr"`
	Gateway       string        `yaml:"gateway"`
	RangeStart    string        `yaml:"rangeStart"`
	RangeEnd      string        `yaml:"rangeEnd"`
	LeaseTime     string        `yaml:"leaseTime"`
	BootURL       string        `yaml:"bootURL"`
	StaticLeases  []StaticLease `yaml:"staticLeases,omitempty"`
	DNSServers    []string      `yaml:"dnsServers,omitempty"`
	Domain        string        `yaml:"domain,omitempty"`
	AllowMacs     []string      `yaml:"allowMacs,omitempty"`
	ResponseDelay string        `yaml:"responseDelay,omitempty"` // parsed with time.ParseDuration at bootstrap
	AllowAllMacs  bool          `yaml:"allowAllMacs,omitempty"`
}

type OOBConfig struct {
	// SubnetLabels selects which OOBSubnet CRs count as pools. Ignored when Subnets is set.
	SubnetLabels []SubnetLabel `yaml:"subnetLabels"`

	// Subnets defines pools inline. When non-empty it is authoritative and OOBSubnet CRs are
	// not consulted.
	Subnets []Subnet `yaml:"subnets"`

	// DNSServers is the list of DNS server IPs sent in option 6.
	DNSServers []string `yaml:"dnsServers,omitempty"`

	// Domain is the DNS search domain sent in option 15.
	Domain string `yaml:"domain,omitempty"`

	// NTPServers is the list of NTP server IPs or hostnames sent in option 42.
	NTPServers []string `yaml:"ntpServers,omitempty"`

	// ReclaimGrace is the grace period past a dynamic lease's expiry before its address may
	// be reclaimed for another client, parsed with time.ParseDuration. Empty uses
	// DefaultReclaimGrace.
	ReclaimGrace string `yaml:"reclaimGrace,omitempty"`
}
