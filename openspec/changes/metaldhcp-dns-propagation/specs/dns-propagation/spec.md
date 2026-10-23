# Spec: dns-propagation

## Behavior

The DNS propagation controller runs within the metaldhcp process (gated by `--dns-enabled`).
It watches `DHCPLease` CRs cluster-wide and manages a `DNSEndpoint` CR per lease.

### Record lifecycle

- **Create / Update**: When a `DHCPLease` is created or updated and `spec.hostname` is
  non-empty, the controller ensures a `DNSEndpoint` CR exists with:
  - `metadata.name`: same as the `DHCPLease` name (normalized MAC)
  - `spec.endpoints[0].dnsName`: `<hostname>` (as stored)
  - `spec.endpoints[0].recordType`: `A`
  - `spec.endpoints[0].targets`: `[<spec.ip>]`
  - `spec.endpoints[0].recordTTL`: 300 (fixed; not configurable in v1)
  - Owner reference pointing to the `DHCPLease` (for automatic GC if the lease is deleted
    while the controller is running)
- **Hostname cleared**: If `spec.hostname` is set to `""` on an existing lease, the
  controller deletes the corresponding `DNSEndpoint` (if any).
- **Delete**: When a `DHCPLease` is deleted, its `DNSEndpoint` is deleted (owner reference
  GC handles this; explicit delete is a belt-and-suspenders measure).
- **No hostname**: `DHCPLease` objects where `spec.hostname` is empty at creation are
  ignored; no `DNSEndpoint` is created.

### Enabling

The controller is disabled by default. Enable with `--dns-enabled`. No additional flags
are required in v1 (TTL and zone are fixed or derived from the ExternalDNS configuration).

### Error handling

Transient errors (API server unavailable) are retried with exponential back-off via
controller-runtime's standard reconciliation loop. Permanent errors (e.g., ExternalDNS
CRD missing) surface as reconciler errors in logs.

## Out of scope (v1)

- Reverse DNS (PTR) records
- Configurable TTL per lease
- Non-ExternalDNS DNS backends
- Multi-record hostnames (one A record per lease is the invariant)
