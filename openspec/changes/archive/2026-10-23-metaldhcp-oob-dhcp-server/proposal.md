# Proposal

## Why

The OOB/BMC management network currently uses dnsmasq, which has no Kubernetes integration and
cannot feed the event-driven onboarding pipeline that replaces argora. A coredhcp-based server
that writes a `DHCPLease` CR per lease gives the downstream metal-onboarding-operator a durable,
watch-able trigger for NetBox gating and BMC creation.

## What Changes

- New binary **metaldhcp** importing `github.com/coredhcp/coredhcp` as a library (no fork).
- Single custom **`oob` plugin** merging allocation and lease recording: selects the subnet from
  DHCP relay info (giaddr) or the candidate IP, allocates a free address in-process, and writes a
  `DHCPLease` CR per lease.
- Two new CRD types in group `dhcp.metal.ironcore.dev`:
  - `OOBSubnet` — read-only pool (cidr, gateway, rangeStart/rangeEnd, leaseTime), selected by
    labels from the oob plugin config.
  - `DHCPLease` — durable allocation ledger (macAddress, ip, leaseTime, clientID); the allocator
    rebuilds from existing objects on restart.
- Pool sources: inline `subnets` list in the oob plugin config (managed via Helm/pipeline) and/or
  labeled `OOBSubnet` CRs synced from NetBox; config-defined pools are authoritative when present.
- In-process, mutex-guarded allocator (single-replica — one server per OOB segment, matching
  dnsmasq today).

## Capabilities

### New Capabilities

- `oob-dhcp-server`: IPv4 DHCP serving for OOB/BMC networks — subnet selection, IP allocation
  from pools, and `DHCPLease` CR ledger.

### Modified Capabilities

<!-- No existing specs to modify — greenfield repo. -->

## Impact

- **Replaces dnsmasq** on OOB/BMC segments; deployment managed via Helm.
- **New CRDs** (`OOBSubnet`, `DHCPLease`) installed cluster-wide; `DHCPLease` is a
  cross-component contract (metal-onboarding-operator imports it).
- **Dependencies**: `github.com/coredhcp/coredhcp`, `github.com/insomniacslk/dhcp`,
  `sigs.k8s.io/controller-runtime`, `k8s.io/{api,apimachinery,client-go}`. No damyan forks
  needed — upstream coredhcp is used directly.
- **macOS build caveat**: `sendEthernet` is undefined on macOS (upstream coredhcp limitation);
  Linux/container builds are clean. Development testing uses the container.
- **Downstream**: metal-onboarding-operator (moo) watches `DHCPLease`; no moo/mmo imports in
  metaldhcp.
