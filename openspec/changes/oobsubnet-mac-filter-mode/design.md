# Design

## Context

See proposal.md — Why. The shipped filter is two fields on `OOBSubnet.spec` / `api.Subnet` / `allocator.Pool`: `allowAllMacs bool` and `allowMacs []string`, checked in `plugins/oob/plugin.go` as `if !pool.AllowAllMacs && !slices.ContainsFunc(pool.AllowMacs, func(m string) bool { return helper.NormalizeMACString(m) == macKey })`. `helper.NormalizeMACString` (lowercase, separator-free) already exists. `OOBSubnet` already has a validating webhook (from the static-leases change). Config subnets are bootstrapped to CRs in `k8s.go` `applyConfigToCR`, and pools are built in `oobSubnetPools`.

The `oobsubnet-request-filter` capability that owns these requirements is defined by the complete-but-unarchived `oobsubnet-mac-filter` change, and its spec text is stale (`allowedMACs`, "empty = catch-all") relative to the shipped deny-all code.

## Goals / Non-Goals

**Goals:**
- One explicit `mode` enum + one `macList`, replacing the two-field implicit state machine.
- Illegal/ambiguous combinations rejected at admission rather than silently mis-serving.
- Preserve current runtime behavior for already-deployed subnets (they map to `allowAll`).

**Non-Goals:**
- No migration shim / deprecated aliases (clean break).
- No change to `responseDelay`, static leases, or allocation/bootstrap logic.
- No cross-repo changes (`kube-secrets`, onboarding-operator) in this change.

## Decisions

### `allowAll` is the default
`+kubebuilder:default=allowAll` so an unset `mode` is stamped concrete on the stored object. This keeps deployed subnets serving (they currently run `allowAllMacs: true`) and makes "define a subnet ⇒ serve it" the default; narrowing is opt-in.
_Alternative:_ `allow` default (empty ⇒ serve nobody, fail-safe). Rejected for this deployment: instances are on disjoint subnets so fail-open is not a hazard, and a deny-all default would silently stop all current subnets on upgrade. The webhook rule below guards the one dangerous misconfiguration.

### One `macList`, meaning set by `mode`
A single list avoids keeping two lists consistent; `mode` disambiguates. `allow` serves only the list (empty ⇒ nobody); `deny` serves all but the list; `allowAll` ignores it.
_Alternative:_ separate `allowMacs`/`denyMacs`. Rejected — a list named for one posture is contradictory under the other, which is the implicit confusion this change removes.

### Normalized matching
Each `macList` entry is normalized via `helper.NormalizeMACString` at comparison time (already done for `allowMacs`), so colon/dash/bare and any case all match the normalized request key.

### Webhook rejects `macList` + `allowAll`
`macList` under `allowAll` is dead config; rejecting it also turns "populated a list but left mode at the `allowAll` default" into a loud admission failure instead of a silent serve-everyone. Enforced in the existing `OOBSubnet` validating webhook.
_Alternative:_ silently ignore the list. Rejected — hides a fail-open mistake.

### Plugin check becomes a mode switch
Replace the boolean/list check with a switch on `pool.Mode`: `allowAll` → serve; `allow` → serve iff normalized `chaddr` ∈ `macList`; `deny` → serve iff normalized `chaddr` ∉ `macList`. Keeps the single decision point in `handler4`.

## Risks / Trade-offs

- **`allowAll` default is fail-open on a shared segment** → Mitigated: deployed instances are on disjoint subnets, and the webhook rule catches the common "forgot to set mode" case. If a shared-segment deployment ever appears, that subnet must explicitly set a list mode.
- **Archive ordering vs. `oobsubnet-mac-filter`** → This change's spec delta MODIFIES `oobsubnet-request-filter` requirements that only exist in the unarchived `oobsubnet-mac-filter` change. It must archive after that change; until then `openspec validate` cannot resolve the MODIFIED base. See Open Questions.
- **Clean break breaks downstream config** → `kube-secrets` qa-de-8 values and the onboarding-operator must switch to `mode`/`macList` in lockstep; coordinated out of band since they are separate repos.

## Migration Plan

CRD field replacement with no conversion: deployed `OOBSubnet` CRs and Helm values must be rewritten from `allowMacs`/`allowAllMacs` to `mode`/`macList` as part of rollout. In-repo (`dev/values.yaml`, chart) is updated here; `kube-secrets` qa-de-8 values are updated in that repo alongside the image bump. Rollback is reverting the binary + CRD together.

## Open Questions

- **Archive sequencing with `oobsubnet-mac-filter`:** the cleanest resolution is to archive `oobsubnet-mac-filter` first (landing `oobsubnet-request-filter` in the main specs, stale text and all) and let this change correct it; alternatively, since that change is stale and superseded, collapse the two so only the mode-based filter is ever archived. This is a process decision for the spec owner and does not change the code or tasks below.
