# Proposal

## Why

metaldhcp bootstraps its subnet pool state at startup and only re-reads Subnet CRs when DHCP
traffic arrives. New Subnet CRs synced externally by argora after startup, and updated static
lease entries in existing Subnet CRs, are therefore invisible until a restart or until the first
DHCP packet from a client on that subnet triggers a re-read. Operators have worked around this
by forcing metaldhcp restarts, which is fragile and disruptive.

## What Changes

- Add `resyncInterval` duration field to `OOBConfig` (default `"5m"`, `"0"` disables).
- Add a background goroutine in `NewK8sClient` that fires `oobSubnetPools` on the configured
  interval, picking up new Subnet CRs and new static lease entries in existing ones, and
  publishing Lease CRs for any entries not yet recorded.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `oob-dhcp-server`: add requirement — when `publishStaticLeases` is enabled and `resyncInterval`
  is non-zero, the server SHALL periodically re-read all Subnet CRs and publish Lease CRs for
  any static lease entries not yet recorded; this covers both newly created Subnet CRs and
  updated static lease lists in existing ones.

## Impact

- `internal/api/config.go` — `OOBConfig` gains `ResyncInterval string`
- `plugins/oob/k8s.go` — `NewK8sClient` or its caller starts a background ticker goroutine;
  goroutine stopped on context cancellation
- Helm chart values / README — document the new `resyncInterval` field
