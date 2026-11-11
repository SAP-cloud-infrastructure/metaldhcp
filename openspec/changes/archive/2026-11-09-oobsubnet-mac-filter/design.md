# Design

## Context

See proposal.md — Why. The request handler in `plugins/oob/k8s.go` currently calls `selectPool` with the relay agent IP (`giaddr`) to find the matching pool, then allocates an address. The client MAC (`chaddr`) is available in the handler but is only used for allocation and static-lease lookup — it plays no role in deciding whether to respond.

The filtering decision (`allowedMACs`) and the delay (`responseDelay`) must both be resolved per-matched-subnet, not globally.

## Goals / Non-Goals

**Goals:**
- Config subnets bootstrap OOBSubnet CRs at startup; CRs are the sole runtime pool source
- Dynamic per-subnet updates (`allowedMACs`, `responseDelay`, any field) via `kubectl edit` without redeploy
- MAC filtering and response delay on all subnets regardless of origin
- Zero behavioral change on deployments that have no config subnets and no CRs today
- Filter applied before allocation (no wasted work for ignored requests)

**Non-Goals:**
- Reconciling config changes back into existing CRs (CR is authoritative once created)
- Wildcard or prefix-based MAC matching — exact normalized MAC strings only
- IPv6

## Decisions

### Bootstrap CRs from config at startup; CRs are the sole runtime source

At startup, for each config subnet, `NewK8sClient` checks whether an OOBSubnet CR already exists for that CIDR:

- **No CR exists** → create one from config values, annotated `dhcp.metal.ironcore.dev/managed-by: helm`.
- **CR exists with `managed-by: helm`** → update from current config values (Helm changes propagate).
- **CR exists without that annotation** (manually created or synced from netbox) → leave it untouched.

After the bootstrap step, `selectPool` uses labeled CRs exclusively. There is one code path at runtime.

**Write RBAC:** the pod needs `create` + `update` on `OOBSubnet` in addition to the existing `list`/`get`/`watch`.

Alternative considered — merge config and CRs at runtime on every request: rejected. It requires per-field overlay logic and keeps two code paths alive indefinitely.

Alternative considered — always skip existing CRs regardless of origin: rejected. It prevents Helm config changes from propagating to bootstrap-managed CRs, requiring manual CR deletion after every config update.

### Carry filter fields on `allocator.Pool`

`allowedMACs` and `responseDelay` are added to `allocator.Pool`. `oobSubnetPools` already builds `Pool` structs from CRs; propagating the new fields there is a one-liner. The handler already receives a `Pool` from `selectPool`.

Alternative: filter inside `selectPool`. Rejected — `selectPool` does not have the client MAC; passing it down couples pool selection to the request context unnecessarily.

### Filter before allocation, delay before send

In the request handler, after `selectPool` returns the matching pool:
1. Check `pool.AllowedMACs` against the normalized client MAC — if not listed, return without responding (Debug log: MAC + subnet CIDR; silent drop is standard multi-instance DHCP behaviour).
2. Proceed with allocation and offer construction as normal.
3. Sleep `pool.ResponseDelay` immediately before writing the offer to the wire.

### `api.Subnet.ResponseDelay` as `string`

Config is YAML-decoded at startup. `string` + `time.ParseDuration` at init is consistent with how `LeaseTime` is handled on `api.Subnet`. The parsed value is stored on the bootstrapped CR as `*metav1.Duration`.

## Risks / Trade-offs

- **Config changes don't propagate to externally-managed CRs** — intentional. A CR without `managed-by: helm` is owned by the operator or netbox sync; Helm must not overwrite it.
- **Bootstrap is not idempotent across CRD schema changes** — if `OOBSubnetSpec` gains a required field, existing `managed-by: helm` CRs may need a one-time migration. Low risk given the additive-only field policy.
- **Silent drop may hide misconfig** — a misconfigured allowlist causes one instance to silently not answer. Mitigated by Debug-level log on every dropped request.
- **Delay precision** — `time.Sleep` precision is not guaranteed. For the 2 s catch-all use case, imprecision is not critical.

## Migration Plan

No migration needed for deployments without config subnets — behavior is unchanged.

For deployments with config subnets: on the first pod restart after this change, CRs are bootstrapped from config values. Subsequent restarts are no-ops (CRs already exist). Rollback: delete the bootstrapped CRs before rolling back the pod; the old code ignores them since it treats config as authoritative.
