# Design

## Context

See proposal.md — Why for motivation.

Two pool sources exist in parallel:
- **Config pools** (`OOBConfig.subnets`): loaded once at startup from a YAML file; `seedStaticLeases` is called once at init.
- **CR pools** (`OOBSubnet`): listed live from the API server on every request in `oobSubnetPools`; allocator state is seeded at startup from `DHCPLease` CRs but never refreshed afterward.

`K8sClient.statics` (a `map[string]staticEntry`) is the runtime lookup table for MAC→fixed-IP. `alloc.Reserve(mac, ip)` marks an address as unavailable for dynamic allocation. Both must be populated before a request arrives for static leases to work correctly.

The server binary (`main.go`) currently runs only a DHCP listener and a plain HTTP `/healthz` endpoint — no controller-manager, no existing webhook server.

## Goals / Non-Goals

**Goals:**
- Static leases defined at the subnet level, not globally, for both config and CRD paths.
- CRD static leases take effect without a server restart (hot-add).
- Admission webhook rejects `OOBSubnet` writes with static IPs outside the subnet CIDR.

**Non-Goals:**
- Hostname uniqueness validation (not enforced today for global statics; unchanged).
- Webhook coverage for config-file statics (those are not Kubernetes resources).
- Converting the server to a controller-manager-based operator.

## Decisions

### 1. Static lease struct: reuse `api.StaticLease` in the CRD type

**Decision:** Define `StaticLease` in `api/v1alpha1/` as a CRD-native embedded struct (with `+kubebuilder` validation markers). The `internal/api.StaticLease` struct remains for the YAML config. They are structurally identical but separate types; no shared dependency between the CRD package and the internal config package.

**Alternative:** A single shared type imported by both. Rejected — importing `internal/api` from `api/v1alpha1/` inverts the dependency direction and couples the CRD package to server-internal types.

### 2. Seeding CR static leases: incremental at request time, idempotent via `k.statics`

**Decision:** In `oobSubnetPools`, after building the pool list from CR items, also iterate each CR's `staticLeases`. For each entry not yet in `k.statics`, call `alloc.Reserve()` and insert into `k.statics`. Already-seen entries are skipped (the map key is the normalized MAC; gateway/bootURL come from the parent CR, so no pool scan is needed).

**Alternative A: Seed at init only (one-time CR list).** Simpler, but CRs added after startup are invisible until restart — directly contradicts the hot-add requirement.

**Alternative B: Controller-runtime reconciler / watcher.** A proper watch would push updates instead of polling on every request. Rejected for now — it requires a controller-manager, a non-trivial refactor of the binary, and the per-request overhead of one extra map lookup is negligible.

**Implication:** `alloc.Reserve()` must be safe to call more than once for the same MAC. The allocator already stores reservations in a map, so a second `Reserve()` call for the same MAC is a no-op.

### 3. Config-subnet seeding: per-subnet at init, gateway/bootURL from parent

**Decision:** Change `seedStaticLeases` signature to accept a single `api.Subnet` and a resolved `allocator.Pool`. Call it once per subnet in `configPools`. The gateway and bootURL for the static entry come directly from the parent `Subnet`, not from a pool scan.

**Alternative:** Keep the global scan approach but pass per-subnet lists. Functionally equivalent, but the scan was only needed because the old global list had no subnet context — removing the global list removes the need for the scan entirely.

### 4. Webhook server: extend the existing HTTP server with a TLS listener

**Decision:** Add a second `http.Server` in `main.go` on a configurable port (default `:9443`) serving `POST /validate-oobsubnet`. TLS certificate and key are mounted from a Kubernetes `Secret` (managed by cert-manager). The `ValidatingWebhookConfiguration` points to this endpoint via the webhook `Service`.

**Alternative:** Use controller-runtime's `manager.Webhook()` integration. Rejected — it requires adopting the full manager lifecycle, which is out of scope.

**Alternative:** Sidecar webhook proxy. Rejected — operational complexity for no benefit.

### 5. Helm cert-manager integration: self-signed `Issuer` + `Certificate`

**Decision:** Add a self-signed cert-manager `Issuer` and `Certificate` to the chart, mounting the resulting `Secret` into the pod. The `ValidatingWebhookConfiguration` uses `caBundle` injected via cert-manager's `cert-manager.io/inject-ca-from` annotation.

**Alternative:** Pre-provisioned TLS secret passed via values. Kept as a fallback: if cert-manager is not installed, operators can disable the webhook and supply their own cert secret.

## Risks / Trade-offs

- **Hot-add reservation race**: Between when a CR is first listed and when `alloc.Reserve()` is called, a dynamic allocation could hand the static IP to another MAC. This window exists today for all allocations and is not introduced by this change; it closes once the CR has been observed in `oobSubnetPools`.
- **Webhook availability**: If the webhook pod is down, `OOBSubnet` writes fail (webhook is configured as `failurePolicy: Fail` by default). Mitigation: set `failurePolicy: Ignore` to allow writes through when the webhook is unavailable; CI validation or NetBox-side checks are the backstop.
- **cert-manager dependency**: Adds a cluster-level dependency. Mitigation: make the webhook opt-in via a `webhook.enabled` Helm value; when disabled, admission validation is skipped entirely (runtime skip-and-warn remains as a last line of defense).

## Migration Plan

1. Deploy the new chart version: the webhook is disabled by default (`webhook.enabled: false`).
2. Move `staticLeases` entries from the top-level config into their matching `subnets[]` entries in Helm values.
3. Roll the deployment to pick up the new config.
4. Enable the webhook (`webhook.enabled: true`) once cert-manager is confirmed present.

Rollback: redeploy the previous chart version; old config shape (global `staticLeases`) is gone — a downgrade requires restoring the previous values file.
