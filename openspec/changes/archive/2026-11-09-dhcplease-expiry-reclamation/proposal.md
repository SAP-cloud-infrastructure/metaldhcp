# Proposal

## Why

`leaseTime` is a DHCP protocol lease, but the `DHCPLease` ledger and the in-memory allocator never act on expiry: `LeaseExpiresAt` is written and never read, the allocator stores no expiry, and there is no release path. Expired addresses are never returned to the pool, so pools only ever shrink and a decommissioned BMC/DPU holds its IP forever. Separately, the restart seed (`Restore`) is conflict-blind and will happily map one IP to two MACs if the ledger ever contains duplicates. Tracked as issue #10.

## What Changes

- Track lease expiry per dynamic allocator entry so the allocator can tell a live lease from an expired one (today `byMAC` has no timestamp).
- Reclaim expired dynamic leases **lazily**, during allocation, when an address is needed or can be reused — no background sweep.
- Apply a **configurable grace period** past `LeaseExpiresAt` before a dynamic lease becomes reclaimable, so a briefly-offline but still-renewing client is not dropped. Non-zero default.
- Reclaiming a dynamic lease removes **both** its in-memory entry and its `DHCPLease` CR, so the restart seed cannot resurrect it.
- Enforce **one `DHCPLease` CR per IP**: on restart seed, if two CRs claim one IP, keep the live lease and delete the expired one (with a log message). Two live leases on one IP is a correctness invariant that must not occur.
- Static/reserved leases stay entirely outside the expiry model — never expire, never reclaimed, never offered to another client.
- Metaldhcp reclaims expired leases regardless of `metal-onboarding-operator` state; no onboarding coordination.

## Capabilities

### New Capabilities

_None._

### Modified Capabilities

- `oob-dhcp-server`: adds expiry-based reclamation of dynamic leases and the one-CR-per-IP invariant; strengthens the existing "Allocation state rebuilt from DHCPLease CRs on restart" requirement to resolve duplicate-IP conflicts instead of double-mapping.

## Impact

- `internal/allocator/allocator.go` — add expiry to dynamic entries; reclaim in the `Allocate`/`firstFree` path; add a release/reclaim operation; make `Restore` conflict-aware (today `byMAC`/`inUse` have no expiry and `Restore` is conflict-blind; there is no `Release`).
- `plugins/oob/k8s.go` — delete the stale `DHCPLease` CR when a lease is reclaimed; `seedFromLeases` enforces one-CR-per-IP (delete the expired duplicate). `LeaseExpiresAt` is already written here.
- Config — expose the reclamation grace period.
- No change to `api/v1alpha1` types (the `LeaseExpiresAt` field already exists).
- Out of scope: the per-subnet `allowMacs` MAC allowlist (unrelated).
