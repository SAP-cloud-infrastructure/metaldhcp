# Tasks

## 1. API types and CRD

- [x] 1.1 In `api/v1alpha1/oobsubnet_types.go` remove `AllowMacs` and `AllowAllMacs`; add `Mode string` with `+kubebuilder:validation:Enum=allowAll;allow;deny` and `+kubebuilder:default=allowAll`, and `MacList []string`.
- [x] 1.2 Add the validating-webhook rule rejecting a non-empty `MacList` when `Mode == allowAll`, with a clear error message.
- [x] 1.3 `make generate manifests` to regenerate the CRD and `zz_generated.deepcopy.go`.

## 2. Config and allocator

- [x] 2.1 In `internal/api/config.go` replace `AllowMacs`/`AllowAllMacs` on `api.Subnet` with `Mode string` (yaml `mode`) and `MacList []string` (yaml `macList`).
- [x] 2.2 In `internal/allocator/allocator.go` replace `Pool.AllowMacs`/`Pool.AllowAllMacs` with `Pool.Mode`/`Pool.MacList`.

## 3. Filter logic

- [x] 3.1 In `plugins/oob/plugin.go` replace the `allowAllMacs`/`allowMacs` check with a switch on `pool.Mode`: `allowAll` serves; `allow` serves iff the normalized `chaddr` is in `MacList`; `deny` serves iff it is not. Keep normalization via `helper.NormalizeMACString` and the single drop/serve decision point.
- [x] 3.2 In `plugins/oob/k8s.go` update `applyConfigToCR` (config→CR bootstrap) and `oobSubnetPools` (pool construction) to carry `Mode`/`MacList` instead of the old fields.

## 4. Chart and dev config

- [x] 4.1 Update `chart/metaldhcp/templates/configmap.yaml` to render `mode`/`macList` per subnet.
- [x] 4.2 Update `chart/metaldhcp/values.yaml` comments/examples and `dev/values.yaml` to use `mode`/`macList`.

## 5. Tests

- [x] 5.1 Allocator/plugin tests for each mode: `allowAll` serves any MAC; `allow` serves only listed and drops others; empty `allow` serves nobody; `deny` drops listed and serves others; format-insensitive matching (colon/dash/bare).
- [x] 5.2 Webhook test: `macList` set with `mode: allowAll` is rejected; valid combinations admitted.
- [x] 5.3 Update existing filter tests that referenced `allowMacs`/`allowAllMacs` to the new fields.

## 6. Validation

- [x] 6.1 `make test` and the codegen check (`make generate manifests fmt` leaves no diff) and `helm lint` pass.
- [ ] 6.2 `make lint` passes (the golangci-lint CI gate).

## Workflow follow-up

- Archive `oobsubnet-mac-filter` before archiving this change so `oobsubnet-request-filter` exists in the main specs for the MODIFIED delta to apply (or collapse the two per design.md Open Questions). Then `openspec archive oobsubnet-mac-filter-mode`.
- Update downstream consumers in lockstep: `kube-secrets` qa-de-8 values and `metal-onboarding-operator` (separate repos).
