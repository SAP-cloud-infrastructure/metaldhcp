# Design

## Context

See proposal.md — Why.

`NewK8sClient` runs startup bootstrap (`bootstrapConfigSubnets`, `seedFromLeases`) then returns
a `*K8sClient` whose `handler4` is called on every DHCP packet. `oobSubnetPools` lists all
Subnet CRs fresh from the API and calls `seedStaticLeases`, which publishes Lease CRs when
`publishStaticLeases` is true. It is currently only called on DHCP traffic.

`setup4` calls `NewK8sClient(context.Background(), oobConfig)` and returns `handler4`. The
returned `K8sClient` lives for the process lifetime.

## Goals / Non-Goals

**Goals:**
- Pick up Subnet CRs created after startup without requiring DHCP traffic or a restart.
- Pick up new static lease entries added to existing Subnet CRs under the same condition.
- Keep the fix self-contained and non-breaking (new config field, default enabled).

**Non-Goals:**
- Watch-based real-time Subnet CR reconciliation (overkill; a 5-minute lag is acceptable).
- Dynamic reconfiguration of `resyncInterval` at runtime.

## Decisions

### Reuse `oobSubnetPools` for the ticker

`oobSubnetPools` already does a fresh `List`, seeds static leases, and publishes Lease CRs.
Calling it from a ticker goroutine reuses all that logic without duplication.
The returned pools are discarded — the goal is the side effect (Lease CR creation).

Alternative considered: add a dedicated `resyncSubnets` method that only seeds, not allocates.
Rejected: more code, harder to keep in sync with `oobSubnetPools` as it evolves.

### Duration string config field `resyncInterval`

Consistent with existing `reclaimGrace` which also uses `time.ParseDuration` with a warning on
bad input. Default `"5m"`; `"0"` or `""` disables the goroutine entirely.

### Goroutine in `NewK8sClient`

`NewK8sClient` owns the client lifecycle. Starting the goroutine there keeps the concern
co-located. The ctx passed in (`context.Background()` from `setup4`) never cancels — the
goroutine stops when the process exits. This is acceptable for a server that runs until killed.

## Risks / Trade-offs

- `oobSubnetPools` is called concurrently with `handler4` calls. Both read Subnet CRs from the
  API (read-only) and call `seedStaticLeases`, which writes Lease CRs. `SeedStatic` and
  `Publish` in the allocator are already called from `handler4` concurrently per the existing
  coredhcp multi-goroutine model. No new synchronization needed beyond what already exists.

- The resync interval adds periodic API load. At the default 5 minutes this is negligible.
