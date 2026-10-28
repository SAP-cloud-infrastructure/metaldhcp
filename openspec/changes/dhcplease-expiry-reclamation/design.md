# Design

## Context

See proposal.md — Why. The allocator (`internal/allocator/allocator.go`) holds three maps under one mutex: `byMAC` (`map[string]net.IP`), `inUse` (set of IP strings), and `reserved` (MAC keys pinned by `Reserve` for static leases). `Allocate` returns a MAC's existing in-CIDR entry idempotently, else honors an exact requested IP if free, else `firstFree`. `firstFree` skips every `inUse` address unconditionally. There is no expiry stored and no release path. `plugins/oob/k8s.go` owns all CR I/O: it writes `LeaseExpiresAt` when persisting a lease and `seedFromLeases` restores every `DHCPLease` into the allocator on startup via `Restore`, which is conflict-blind.

The allocator is pure in-memory and holds its mutex during allocation; all Kubernetes I/O (lease CR create/patch/delete) lives in the oob plugin, outside the lock.

## Goals / Non-Goals

**Goals:**
- Expired dynamic leases become reusable addresses, on demand, with a configurable grace.
- Reclamation and `Restore` keep the invariant of one `DHCPLease` CR per IP.
- Keep CR I/O out of the allocator's critical section.

**Non-Goals:**
- No background reclamation loop.
- No coordination with `metal-onboarding-operator`.
- No change to the `DHCPLease` API types (`LeaseExpiresAt` already exists).

## Decisions

### Store expiry per dynamic allocator entry
Replace `byMAC map[string]net.IP` with an entry carrying the IP and an optional expiry (`map[string]entry{ ip; expiresAt time.Time }`), and add a reverse `byIP map[string]string` (IP → MAC key) so `firstFree` can find the holder of an in-use address in O(1). Static entries (`reserved`) carry no expiry and are never reclamation candidates.
_Alternative:_ a parallel `expiry map[string]time.Time`. Rejected — easy to desync from `byMAC`; one struct keeps them together.

### Reclaim lazily in the allocation path, preferring never-used addresses
`firstFree` first returns a genuinely free address. Only when none remain does it reclaim: pick an in-use IP whose holder is a **dynamic** entry (not in `reserved`) with `expiresAt + grace < now`, evict it, and assign it. This bounds exhaustion without churn and never steals from a client still within grace.
_Alternative:_ eagerly prefer reusing expired IPs over consuming fresh ones. Rejected — more churn and risks disrupting a client about to renew; grace already covers brief absence.

### Allocate reports the evicted MAC; the plugin deletes the CR
Reclamation removes the old entry from `byMAC`/`byIP`/`inUse` under the lock, and `Allocate` returns the evicted MAC key (if any) to the caller. The oob plugin deletes that MAC's `DHCPLease` CR **after** `Allocate` returns, outside the lock. This keeps the invariant "reclaim deletes the CR too" while keeping I/O off the critical section and the allocator free of a Kubernetes client.
_Alternative:_ inject a delete callback into the allocator. Rejected — couples the allocator to CR I/O.

### Grace is global config with a non-zero default
Add a single reclamation grace duration to the oob plugin config, defaulting to a non-zero value on the order of one `leaseTime` (roughly a couple of missed renewals, since clients renew at ~T/2). Global rather than per-pool: the grace is an operational safety margin, not a per-subnet policy.
_Alternative:_ per-subnet grace. Rejected as unnecessary complexity for now; can be added later without breaking the spec.

### Restore resolves duplicate IPs by expiry
`seedFromLeases` builds an IP → lease index as it seeds. On a collision it keeps the live lease (future/within-grace `LeaseExpiresAt`), deletes the expired duplicate's CR, and logs the removal, so startup never double-maps an IP. `Restore` is made expiry-aware (takes the lease's `LeaseExpiresAt`) and refuses to overwrite an IP already held by a different live MAC.
_Alternative:_ last-writer-wins. Rejected — silently hides a real data problem and leaves the losing CR behind.

## Risks / Trade-offs

- **Expired CRs linger under low churn** → Because reclamation is lazy (prefer-free), a pool with spare addresses keeps stale `DHCPLease` CRs until pressure forces reuse. Accepted: functionally harmless (no exhaustion, no double-map); the ledger self-cleans as addresses are reused. A sweep can be added later without spec change.
- **Clock skew / long outages vs. grace** → A client offline longer than `expiry + grace` loses its address and may get a new one on return. Accepted and intended; grace is the tunable knob.
- **Two live leases on one IP** → Treated as an invariant that must not occur. `Restore` and the allocator never create it; if encountered at seed time (corrupted ledger), the resolution keeps one and logs, rather than crashing.

## Migration Plan

Behavioral-only change in the DHCP server; no API/CRD migration. On rollout, the first `seedFromLeases` may delete expired duplicate CRs (logged). Rollback is reverting the binary; the grace config key defaults safely if unset.
