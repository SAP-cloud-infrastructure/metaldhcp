// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package oob

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"os"

	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/api"
	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/helper"
	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/printer"
	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"gopkg.in/yaml.v3"
)

var log = logger.GetLogger("plugins/oob")

var Plugin = plugins.Plugin{
	Name:   "oob",
	Setup4: setup4,
	Setup6: setup6,
}

var k8sClient *K8sClient

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

	k8sClient, err = NewK8sClient(context.Background(), oobConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	log.Print("Loaded oob plugin for DHCPv4.")
	return handler4, nil
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

func handler4(req, resp *dhcpv4.DHCPv4) (*dhcpv4.DHCPv4, bool) {
	if req == nil {
		log.Error("Received nil IPv4 request")
		return nil, true
	}

	printer.VerboseRequest(req, log, printer.IPv4)
	defer printer.VerboseResponse(req, resp, log, printer.IPv4)

	mac := req.ClientHWAddr
	clientIP := req.ClientIPAddr
	requestedIP := dhcpv4.GetIP(dhcpv4.OptionRequestedIPAddress, req.Options)
	giaddr := req.GatewayIPAddr

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	macKey := helper.NormalizeMAC(mac)

	if entry, ok := k8sClient.lookupStaticLease(macKey); ok {
		log.Debugf("Static lease for MAC %s → %s", mac, entry.ip)
		resp.YourIPAddr = entry.ip
		if entry.bootURL != "" {
			resp.BootFileName = entry.bootURL
		}
		if entry.hostname != "" {
			resp.Options.Update(dhcpv4.OptHostName(entry.hostname))
		}
		if err := k8sClient.applyLease(ctx, mac, entry.ip, entry.gateway, clientIdentifier(req), entry.hostname, resp.IPAddressLeaseTime(0)); err != nil {
			log.Errorf("Failed to record DHCPLease for MAC %s: %s", mac, err)
		}
		return resp, false
	}

	log.Debugf("Allocating for MAC %s (pool hint %s, alloc hint %s, exact %t, relay %t)",
		mac, poolHint, allocHint, exactIP, giaddr != nil && !giaddr.IsUnspecified())
	leaseIP, gateway, bootURL, err := k8sClient.getIP(ctx, poolHint, mac, allocHint, exactIP)
	if err != nil {
		log.Errorf("Could not allocate IP: %s", err)
		return nil, true
	}

	resp.YourIPAddr = leaseIP
	if bootURL != "" {
		resp.BootFileName = bootURL
	}

	if err := k8sClient.applyLease(ctx, mac, leaseIP, gateway, clientIdentifier(req), requestHostname(req), resp.IPAddressLeaseTime(0)); err != nil {
		log.Errorf("Failed to record DHCPLease for MAC %s: %s", mac, err)
	}

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
