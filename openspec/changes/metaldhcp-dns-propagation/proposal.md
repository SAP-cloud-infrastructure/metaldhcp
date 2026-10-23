# Proposal

## Why

`DHCPLease` CRs already record the hostname announced in DHCP requests. Nothing
consumes it today. Populating forward DNS records from these leases would make BMC
hostnames resolvable without a separate DNS management tool, and without hand-editing
zone files.

ExternalDNS is already deployed in the cluster and consumes `DNSEndpoint` CRs. Writing
one `DNSEndpoint` per `DHCPLease` with a hostname keeps metaldhcp free of direct DNS
API dependencies and lets ExternalDNS handle provider-specific reconciliation.

## What Changes

- New **DNS propagation controller** in metaldhcp (or a companion binary) that watches
  `DHCPLease` CRs and manages a corresponding `externaldns.k8s.io/v1alpha1 DNSEndpoint`
  CR per lease.
- `DHCPLease` with a non-empty `hostname` → `DNSEndpoint` with one A-record target.
- `DHCPLease` hostname cleared or CR deleted → `DNSEndpoint` deleted.
- RBAC for reading `DHCPLease` and writing `DNSEndpoint` CRs added to the Helm chart.

## Capabilities

### New Capabilities

- `dns-propagation`: Forward DNS record management driven by `DHCPLease` hostname field
  via ExternalDNS `DNSEndpoint` CRs.

### Modified Capabilities

<!-- oob-dhcp-server capability unchanged; DHCPLease hostname field already populated. -->

## Impact

- **New dependency**: `externaldns.k8s.io/v1alpha1` CRD must be present on the cluster;
  controller is gated behind an `--dns-enabled` flag (default off) so clusters without
  ExternalDNS are unaffected.
- **RBAC expansion**: ClusterRole gains `get/list/watch/create/update/delete` on
  `dnsendpoints.externaldns.k8s.io`.
- **No schema change**: `DHCPLease.hostname` is already persisted; this change only adds
  a consumer.
