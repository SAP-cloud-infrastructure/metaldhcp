# Design

## Context

The MAC-list filter lives in `plugin.go:208–218` and checks only `pool.MacList` against the normalized MAC key. Static leases are seeded into `K8sClient.statics` (a `map[string]staticEntry`) keyed by normalized MAC, and looked up in `lookupStaticLease(macKey string)`. The client ID (option 61) is already extracted from the DHCP request and available as a local variable in the same scope as the MAC filter. See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**
- `pool.ClientIdList` checked alongside `pool.MacList` in the allow/deny filter
- Static leases keyed and looked up by client ID when `clientId` is set instead of `mac`
- Single source of truth: client ID normalization applied consistently on insert and lookup

**Non-Goals:**
- Matching a single static lease entry on both MAC and client ID simultaneously
- Any changes to pool selection (CIDR matching, multi-subnet disambiguation)

## Decisions

### Keying client-ID static leases in the existing statics map

**Decision:** Reuse `K8sClient.statics map[string]staticEntry` with a `cid:` prefix for client ID keys (e.g. `cid:ff0102...`). Lookup tries the MAC key first; if not found, tries `cid:<normalizedClientID>`.

**Alternative considered:** A separate `staticsByClientID map[string]staticEntry` field. Rejected because it duplicates the lookup path and the `Reserve`/`alloc` coordination without benefit; the existing map already has a single-writer guarantee.

**Normalization:** Client IDs are stored and matched as lowercase hex strings (strip `0x` prefix if present). The `clientID` variable in `plugin.go` already holds the raw option-61 value; normalize it the same way `helper.NormalizeMAC` works for MACs.

### Filter logic

**Decision:** The allow/deny check becomes an OR across both lists. A client is admitted (allow) or blocked (deny) if its MAC matches `macList` **or** its client ID matches `clientIdList`. The check in `plugin.go` is extended in-place at lines 208–218; no new function is needed.

**Alternative considered:** Requiring both lists to match (AND). Rejected because the two identifiers refer to the same client — there is never a case where both are set for the same physical device.

### API layer: `clientId` field on StaticLease

**Decision:** Add `ClientId string \`json:"clientId,omitempty"\`` to both `api/v1alpha1/oobsubnet_types.go` and `internal/api/config.go`. The admission webhook validates mutual exclusivity (`mac` XOR `clientId`). `seedStaticLeases` branches on which field is set.

## Risks / Trade-offs

- **Key collision:** A `cid:` prefix is a naming convention; a sufficiently unusual MAC could theoretically start with `cid:`. In practice MAC keys are colon-separated hex (e.g. `aa:bb:cc:...`) so the prefix is unambiguous. A unit test covers both key types in the same map.
- **No index on CR watch:** `lookupStaticLease` is O(1) per lookup (map). Adding a second key type does not change complexity.
- **Webhook scope:** Mutual-exclusivity validation is added to the existing `OOBSubnet` admission webhook. No new webhook required.
