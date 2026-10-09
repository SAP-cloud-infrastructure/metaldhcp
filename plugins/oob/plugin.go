// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package oob

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/api"
	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/helper"
	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	ouipkg "github.com/endobit/oui"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"gopkg.in/yaml.v3"
)

var log = logger.GetLogger("plugins/oob")

// vendorLookup enables IEEE OUI vendor enrichment. Set via EnableVendorLookup before
// the first DHCP request arrives.
var vendorLookup bool

// EnableVendorLookup switches on MAC-to-vendor lookup for log lines, events, and
// DHCPLease annotations. Uses ~500 KB of embedded OUI data; off by default.
func EnableVendorLookup() { vendorLookup = true }

// vendorTag returns " (vendor)" when v is non-empty, "" otherwise.
func vendorTag(v string) string {
	if v == "" {
		return ""
	}
	return " (" + v + ")"
}

var Plugin = plugins.Plugin{
	Name:   "oob",
	Setup4: setup4,
	Setup6: setup6,
}

// args[0] = path to config file
func parseArgs(args ...string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("exactly one argument must be passed to the plugin, got %d", len(args))
	}
	return args[0], nil
}

func loadConfig(args ...string) (*api.OOBConfig, error) {
	path, err := parseArgs(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	log.Debugf("Reading config file %s", path)
	configData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	config := &api.OOBConfig{}
	if err = yaml.Unmarshal(configData, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}
	return config, nil
}

func setup4(args ...string) (handler.Handler4, error) {
	oobConfig, err := loadConfig(args...)
	if err != nil {
		return nil, err
	}

	client, err := NewK8sClient(context.Background(), oobConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	log.Print("Loaded oob plugin for DHCPv4.")
	return client.handler4, nil
}

// setup6 registers the plugin for DHCPv6 but OOB allocation is IPv4-only; handler6 is a
// pass-through so a mixed config does not fail to load.
func setup6(args ...string) (handler.Handler6, error) {
	if _, err := loadConfig(args...); err != nil {
		return nil, err
	}
	log.Print("Loaded oob plugin for DHCPv6 (pass-through; IPv4-only allocation).")
	return handler6, nil
}

func handler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	return resp, false
}

func (k *K8sClient) handler4(req, resp *dhcpv4.DHCPv4) (*dhcpv4.DHCPv4, bool) {
	if req == nil {
		log.Error("Received nil IPv4 request")
		return nil, true
	}

	mac := req.ClientHWAddr
	clientIP := req.ClientIPAddr
	requestedIP := dhcpv4.GetIP(dhcpv4.OptionRequestedIPAddress, req.Options)
	giaddr := req.GatewayIPAddr

	var vendor string
	if vendorLookup {
		vendor = ouipkg.VendorFromMAC(mac)
	}
	classID := req.ClassIdentifier()
	var classTag string
	if classID != "" {
		classTag = " class=" + classID
	}
	log.Infof("→ %s mac=%s%s%s giaddr=%s ciaddr=%s", req.MessageType(), mac, vendorTag(vendor), classTag, giaddr, clientIP)

	// poolHint selects which subnet pool to allocate from.
	// allocHint + exactIP control whether a specific address is honored.
	var poolHint, allocHint net.IP
	var exactIP bool

	if giaddr != nil && !giaddr.IsUnspecified() {
		// Relayed packet: giaddr is on the client's subnet — use it for pool selection.
		// The client's own address hints still drive exact-IP allocation within that pool.
		poolHint = giaddr
		switch {
		case clientIP != nil && !clientIP.IsUnspecified():
			exactIP = true
			allocHint = clientIP
		case requestedIP != nil && !requestedIP.IsUnspecified():
			exactIP = true
			allocHint = requestedIP
		}
	} else {
		// Direct packet: same IP drives both pool selection and allocation.
		switch {
		case clientIP != nil && !clientIP.IsUnspecified():
			exactIP = true
			allocHint = clientIP
		case requestedIP != nil && !requestedIP.IsUnspecified():
			exactIP = true
			allocHint = requestedIP
		case resp.ServerIPAddr != nil && !resp.ServerIPAddr.IsUnspecified():
			allocHint = resp.ServerIPAddr
		default:
			allocHint = net.ParseIP(unknownIP)
		}
		poolHint = allocHint
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	macKey := helper.NormalizeMAC(mac)

	var leaseIP net.IP
	var gateway, bootURL, hostname string
	var poolLeaseTime time.Duration

	if entry, ok := k.lookupStaticLease(macKey); ok {
		log.Debugf("Static lease for MAC %s → %s", mac, entry.ip)
		leaseIP, gateway, bootURL, hostname = entry.ip, entry.gateway, entry.bootURL, entry.hostname
	} else {
		log.Debugf("Allocating for MAC %s (pool hint %s, alloc hint %s, exact %t, relay %t)",
			mac, poolHint, allocHint, exactIP, giaddr != nil && !giaddr.IsUnspecified())
		var err error
		leaseIP, gateway, bootURL, poolLeaseTime, err = k.getIP(ctx, poolHint, mac, allocHint, exactIP)
		if err != nil {
			log.Infof("← DROP %s mac=%s%s: %s", req.MessageType(), mac, vendorTag(vendor), err)
			k.emitEvent(mac, giaddr, vendor, err)
			return nil, true
		}
		hostname = requestHostname(req)
	}

	leaseTime := resp.IPAddressLeaseTime(0)
	if poolLeaseTime > 0 {
		leaseTime = poolLeaseTime
		resp.Options.Update(dhcpv4.OptIPAddressLeaseTime(poolLeaseTime))
	}

	resp.YourIPAddr = leaseIP
	if gw := net.ParseIP(gateway); gw != nil {
		resp.Options.Update(dhcpv4.OptRouter(gw))
	}
	if len(k.dnsServers) > 0 {
		resp.Options.Update(dhcpv4.OptDNS(k.dnsServers...))
	}
	if k.domain != "" {
		resp.Options.Update(dhcpv4.OptDomainName(k.domain))
	}
	if len(k.ntpServers) > 0 {
		resp.Options.Update(dhcpv4.OptNTPServers(k.ntpServers...))
	}
	log.Debugf("  dns=%v domain=%q ntp=%v", k.dnsServers, k.domain, k.ntpServers)
	if hostname != "" {
		resp.Options.Update(dhcpv4.OptHostName(hostname))
	}
	if err := k.applyLease(ctx, mac, leaseIP, gateway, clientIdentifier(req), hostname, vendor, leaseTime); err != nil {
		log.Errorf("Failed to record DHCPLease for MAC %s: %s", mac, err)
	}

	// iPXE phase 2: snponly.efi sends option 175 (iPXE encapsulated options) —
	// this is the reliable iPXE marker. option 60 stays "PXEClient:Arch:00007:UNDI:003010"
	// so checking classIdentifier for "iPXE" does not work. Respond with the
	// boot-operator chain URL using the iPXE ${uuid} template variable (substituted
	// client-side by iPXE from its SMBIOS UUID) and stop the chain so nbp cannot
	// overwrite option 67 with the TFTP filename, which would cause an infinite boot loop.
	if len(req.Options.Get(dhcpv4.GenericOptionCode(175))) > 0 {
		if bootURL != "" {
			chainURL := bootURL + "/ipxe/${uuid}"
			resp.BootFileName = chainURL
			resp.Options.Update(dhcpv4.OptBootFileName(chainURL))
			log.Infof("← OFFER %s mac=%s yiaddr=%s chainURL=%q (iPXE phase)", req.MessageType(), mac, leaseIP, chainURL)
		} else {
			log.Infof("← OFFER %s mac=%s yiaddr=%s (iPXE phase, no bootURL)", req.MessageType(), mac, leaseIP)
		}
		return resp, true // stop chain; nbp must not run for iPXE phase
	}

	// Phase 1 (PXEClient) or plain DHCP: nbp handles TFTP options downstream.
	if bootURL != "" {
		resp.BootFileName = bootURL
	}
	log.Infof("← OFFER %s mac=%s yiaddr=%s bootURL=%q", req.MessageType(), mac, leaseIP, bootURL)
	return resp, false
}

// clientIdentifier returns the DHCPv4 client identifier (option 61) as a hex string, or "" when
// absent.
func clientIdentifier(req *dhcpv4.DHCPv4) string {
	raw := req.Options.Get(dhcpv4.OptionClientIdentifier)
	if len(raw) == 0 {
		return ""
	}
	return hex.EncodeToString(raw)
}

// requestHostname returns the hostname from option 12 in the request, or "" when absent.
func requestHostname(req *dhcpv4.DHCPv4) string {
	return req.HostName()
}
