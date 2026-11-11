# Proposal

## Why

The shipped MAC-filter API on `OOBSubnet` is two loosely-coupled fields — `allowAllMacs bool` (a dev/debug "serve everyone" bypass) and `allowMacs []string` (an allowlist, deny-all when empty). Expressing the admin/worker split on a shared BMC segment needs an *exclude* posture too, and bolting a third list onto two booleans-and-lists encodes an implicit state machine with ambiguous combinations. Replacing it with one explicit `mode` enum plus one list makes each posture self-documenting, makes illegal combinations unrepresentable, and adds the missing denylist posture.

## What Changes

- **BREAKING**: Remove `allowMacs []string` and `allowAllMacs bool` from `OOBSubnet.spec`, the inline Helm config subnet (`api.Subnet`), and `allocator.Pool`.
- Add `mode` (enum `allowAll` | `allow` | `deny`, default `allowAll`) and `macList []string` in their place.
- Behaviour: `allowAll` serves every MAC (`macList` ignored); `allow` serves only MACs in `macList` (empty ⇒ serve nobody); `deny` serves every MAC except those in `macList`.
- `macList` entries are normalized before matching (via the existing `helper.NormalizeMACString`), so any MAC format (colon/dash/bare, any case) matches.
- Add an `OOBSubnet` validating-webhook rule: reject a non-empty `macList` together with `mode: allowAll` (dead config, and it turns the "populated a list but forgot to switch mode off the `allowAll` default" mistake into a loud failure rather than a silent serve-everyone).
- Regenerate CRD + deepcopy; update the Helm chart (`configmap.yaml`, `values.yaml`) and `dev/values.yaml` to render/use `mode`/`macList`.

No migration shim or deprecated aliases: the MAC filter is barely deployed, so this is a clean break. `allowAll` is chosen as the default deliberately — it preserves current behaviour (deployed subnets effectively run `allowAllMacs: true`) and makes "define a subnet ⇒ serve it" the intuitive default; narrowing is opt-in. The tradeoff is that `allowAll` is fail-open on a *shared* broadcast segment; acceptable here because the deployed instances sit on disjoint subnets, and the webhook rule above guards the common misconfiguration.

## Capabilities

### New Capabilities

_None._

### Modified Capabilities

- `oobsubnet-request-filter`: the "MAC allowlist filtering per subnet" requirement is replaced by a mode-based filter (`allowAll`/`allow`/`deny` + `macList`, normalized matching); the "filtering applies to both config and CR subnets" requirement is updated to the new field names.

## Impact

- `api/v1alpha1/oobsubnet_types.go` — replace fields with `Mode`/`MacList` + kubebuilder enum/default markers; webhook rule.
- `internal/api/config.go` — `api.Subnet` yaml fields.
- `internal/allocator/allocator.go` — `Pool.AllowMacs`/`AllowAllMacs` → `Pool.Mode`/`Pool.MacList`.
- `plugins/oob/plugin.go` — replace the `allowAllMacs`/`allowMacs` check with a switch on `mode` (normalized match).
- `plugins/oob/k8s.go` — `applyConfigToCR` bootstrap and `oobSubnetPools` pool construction.
- `chart/metaldhcp/templates/configmap.yaml`, `chart/metaldhcp/values.yaml`, `dev/values.yaml` — render/use `mode`/`macList`.
- CRD + `zz_generated.deepcopy.go` regenerated.
- **Dependency / sequencing**: the `oobsubnet-request-filter` capability is defined by the complete-but-unarchived `oobsubnet-mac-filter` change, whose spec text is also stale (it still describes `allowedMACs` with "empty = catch-all", which does not match the shipped deny-all `allowMacs`/`allowAllMacs` code). This change must archive **after** `oobsubnet-mac-filter` so the base requirements exist in the main specs to modify, and it brings that capability in line with both the new API and shipped reality.
- Downstream consumers (`metal-onboarding-operator`, the `kube-secrets` qa-de-8 values) also move from `allowMacs`/`allowAllMacs` to `mode`/`macList`; `kube-secrets` is a separate repo and out of scope here beyond noting it.

Out of scope: DHCPLease expiry-reclamation (separate, already implemented), `responseDelay`, and static leases — all unchanged.
