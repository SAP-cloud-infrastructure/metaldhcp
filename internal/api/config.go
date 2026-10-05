// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package api

type SubnetLabel struct {
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
}

// Subnet is an inline pool definition, managed via the deployment pipeline / Helm values. It
// is the config-defined alternative to a synced OOBSubnet CR.
type Subnet struct {
	CIDR       string `yaml:"cidr"`
	Gateway    string `yaml:"gateway"`
	RangeStart string `yaml:"rangeStart"`
	RangeEnd   string `yaml:"rangeEnd"`
	LeaseTime  string `yaml:"leaseTime"`
	BootURL    string `yaml:"bootURL"`
}

type OOBConfig struct {
	// Namespace is used both to read pools (OOBSubnet CRs) and to write DHCPLease objects.
	Namespace string `yaml:"namespace"`

	// SubnetLabels selects which OOBSubnet CRs count as pools. Ignored when Subnets is set.
	SubnetLabels []SubnetLabel `yaml:"subnetLabels"`

	// Subnets defines pools inline. When non-empty it is authoritative and OOBSubnet CRs are
	// not consulted.
	Subnets []Subnet `yaml:"subnets"`
}
