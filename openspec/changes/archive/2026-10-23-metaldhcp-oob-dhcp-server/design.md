# Design

## Context

See [proposal.md](proposal.md) for motivation.

The OOB/BMC network is currently served by dnsmasq, which has no Kubernetes integration. The
metaldhcp repo exists but is otherwise empty. fedhcp (ironcore-dev) is the closest reference
implementation: it imports coredhcp as a library and uses two plugins (`oob` for allocation,
`metal` for CR-per-lease). Its `internal/` helpers are not importable; the relevant pieces must
be reimplemented locally.

Key upstream constraint: `github.com/coredhcp/coredhcp` and
`github.com/insomniacslk/dhcp` are used directly — no fork or replace directive is required.
macOS builds fail because `sendEthernet` is not defined on Darwin (upstream coredhcp); this is
accepted, and development uses the Linux container path.

## Goals / Non-Goals

**Goals:**
- Working IPv4 DHCP server for OOB segments, integrated with a Kubernetes cluster.
- `DHCPLease` CR written per lease as the trigger for downstream onboarding (moo).
- Pools from inline config and/or `OOBSubnet` CRs — no external IPAM dependency.
- CI (`make test` green) and a README covering pool sources and component boundaries.

**Non-Goals:**
- NetBox access, BMC creation, or any logic beyond DHCP serving.
- Multi-replica allocation (out of scope for v1; one server per OOB segment).
- DHCPv6.
- Lease reclamation / DHCPLease pruning on expiry (candidate for moo).

## Decisions

### 1. coredhcp as a library, single `oob` plugin

fedhcp's `main.go` pattern: build a `desiredPlugins` slice of coredhcp built-ins
(`server_id`, `lease_time`, `netmask`, `router`, `dns`) plus our custom plugin, register all,
then call `server.Start(cfg).Wait()`. This avoids forking coredhcp and keeps the deployment a
single binary.

The `metal` plugin (CR-per-lease) from fedhcp is folded into `oob` rather than kept separate.
**Why**: a separate `metal` plugin requires a fixed chain order and a matching second config
block / namespace; merging removes both constraints and reduces configuration surface.

**Alternative considered**: fork coredhcp — rejected; the library API is stable and the upstream
compiles without patches.

### 2. giaddr-based pool selection (relay support)

`handler4` checks `req.GatewayIPAddr` (giaddr) first. When non-zero (relayed packet), it is used
as `poolHint` to select the pool whose CIDR contains the relay agent's address — this correctly
identifies the client's subnet. `allocHint` is then taken from `clientIP`/`requestedIP`/
`serverIP` as usual. For direct requests (giaddr zero), `poolHint == allocHint`.

`getIP(ctx, poolHint, mac, allocHint, exactIP)` receives both hints separately; the allocator
uses `poolHint` for pool lookup and `allocHint` to try an exact-IP assignment within that pool.

**Alternative**: select pool from `allocHint` only — fails for relay scenarios where the
packet's candidate IP may be 0.0.0.0 or unrelated to the client subnet.

### 3. In-process allocator seeded from DHCPLease CRs

Allocation state lives in an in-memory `map[MAC]IP` + in-use `map[IP]bool`, guarded by a
single mutex. On startup, the allocator lists all `DHCPLease` objects in the namespace and
rebuilds from them. The `CreateOrPatch` of the `DHCPLease` CR is the durable commit of the
reservation.

Free-IP selection: re-lease first (known MAC → same IP), then exact-IP (requested IP free),
then first-free (respecting `rangeStart`/`rangeEnd`; excluding network/broadcast/gateway).
Pure functions in `internal/allocator` for unit testability.

**Alternative**: external IPAM operator (ironcore-dev/ipam) — archived upstream; rejected.

### 4. DHCPLease object name = normalized MAC

Normalized MAC (lowercase, no separators, e.g., `3cecefaabbcc`) is the object name. This makes
`CreateOrPatch` idempotent for re-leases and provides a stable key without a separate index.
`+kubebuilder:selectablefield` on `.spec.macAddress` and `.spec.ip` for efficient lookup.

**Alternative**: random UUID — no natural key for re-lease idempotency without a secondary index.

### 5. Pool source precedence

Config-defined pools (`subnets` in `oob.yaml`) are authoritative when present; labeled
`OOBSubnet` CRs are consulted only when no config pools exist. A `poolSource` abstraction in the
handler returns `allocator.Pool` slices regardless of origin, keeping the handler agnostic.

### 6. ENVTEST_K8S_VERSION pinned to 1.30.0

`k8s.io/api` resolves to v0.37, which maps to Kubernetes 1.37. The `setup-envtest` binary for
darwin/arm64 does not have 1.37 available; 1.30.0 is the highest available version for that
platform. The Makefile pins `ENVTEST_K8S_VERSION := 1.30.0` rather than deriving it.

## Risks / Trade-offs

- **macOS build fails** (`sendEthernet` undefined) → accepted trade-off; CI runs on Linux.
  Development on macOS uses the container image.
- **Single-replica correctness only** — the in-process mutex + DHCPLease ledger is correct
  for one replica. Multiple metaldhcp replicas per segment would require leader election or
  IP-keyed distributed locking. v1 matches dnsmasq's single-server model. → accepted for v1.
- **DHCPLease schema is a cross-component contract** — `DHCPLease` fields (`macAddress`, `ip`,
  `clientID`, `leaseTime`) and its GVK are frozen once installed cluster-wide; moo imports and
  watches this type. Field selectors on `macAddress`/`ip` must remain stable.
  → freeze spec before first cluster installation; additions to `.status` are safe later.
- **No lease reclamation** — DHCPLease CRs are not pruned on lease expiry; the OOB MAC set is
  bounded, but long-lived churning subnets may accumulate stale leases.
  → accepted for v1; pruning is a candidate for moo.
