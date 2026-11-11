# Tasks

## 1. API types — add clientId fields

- [x] 1.1 In `api/v1alpha1/oobsubnet_types.go`: add `ClientIdList []string \`json:"clientIdList,omitempty"\`` to `OOBSubnetSpec` with a doc comment; add `ClientId string \`json:"clientId,omitempty"\`` to `StaticLease`. Verify: `go build ./api/...` passes.
- [x] 1.2 In `internal/api/config.go`: add `ClientIdList []string \`yaml:"clientIdList"\`` to `Subnet`; add `ClientId string \`yaml:"clientId"\`` to `StaticLease`. Verify: `go build ./internal/...` passes.
- [x] 1.3 Add `ClientIdList []string` to `internal/allocator/allocator.go` `Pool` struct. Verify: `go build ./internal/...` passes.

## 2. Wire clientIdList through the pool path

- [x] 2.1 In `k8s.go` `oobSubnetPools`: set `pool.ClientIdList = s.Spec.ClientIdList` alongside the existing `MacList` assignment (line ~422). Repeat for the config-layer path (line ~218). Verify: `go build ./plugins/...` passes.

## 3. Extend static lease seeding and lookup

- [x] 3.1 In `k8s.go` `seedStaticLeases`: when `sl.ClientId != ""` (and `sl.MAC == ""`), normalize the client ID to lowercase hex, register it in `k.statics` under the key `"cid:"+normalizedClientID`, and log `Static lease: clientId %s → %s`. When `sl.MAC != ""`, keep existing behavior. Verify: `go build ./plugins/...` passes.
- [x] 3.2 In `k8s.go`: change `lookupStaticLease` signature to `lookupStaticLease(macKey, clientID string)`. Try `k.statics[macKey]` first; if not found and `clientID != ""`, try `k.statics["cid:"+normalizedClientID]`. Verify: `go build ./plugins/...` passes.
- [x] 3.3 Update all `lookupStaticLease` call sites in `plugin.go` to pass both `macKey` and the result of `clientIdentifier(req)`. Extract `clientIdentifier(req)` into a local variable at the top of the handler (before the static-lease check) so it is available to both the lookup and the filter. Verify: `go build ./...` passes.
- [x] 3.4 Add unit tests in `plugins/oob/k8s_test.go` (or a new `static_lease_test.go`):
  - Static lease keyed by MAC still returns correct entry
  - Static lease keyed by `clientId` returns correct entry
  - Lookup by MAC does not match a clientId-keyed entry and vice versa
  Verify: `go test ./plugins/oob/... -run TestStaticLease` passes.

## 4. Extend the allow/deny filter

- [x] 4.1 In `plugin.go` lines 208–218: extend the `allow` branch to also admit when `clientID` is in `pool.ClientIdList`; extend the `deny` branch to also drop when `clientID` is in `pool.ClientIdList`. Preserve the existing MAC-only fast path for pools with no `ClientIdList`. Verify: `go build ./...` passes.
- [x] 4.2 Add unit tests in `plugins/oob/plugin_test.go` covering all filter scenarios from the spec:
  - allow / MAC in macList → admitted
  - allow / MAC not in macList, clientId not in clientIdList → dropped
  - allow / clientId in clientIdList → admitted
  - allow / MAC matches in mixed list → admitted
  - allow / clientId matches in mixed list → admitted
  - allow / neither matches → dropped
  - deny / MAC in macList → dropped
  - deny / MAC not in macList, clientId not in clientIdList → admitted
  - deny / clientId in clientIdList → dropped
  - deny / MAC matches in mixed list → dropped
  - deny / clientId matches in mixed list → dropped
  - deny / neither matches → admitted
  Verify: `go test ./plugins/oob/... -run TestFilter` passes.

## 5. Admission webhook — mutual exclusivity

- [x] 5.1 In the `OOBSubnet` admission webhook, add validation: for each `staticLeases` entry, reject if both `mac` and `clientId` are non-empty, or if both are empty. Return a descriptive error naming the offending entry. Verify: existing webhook unit tests still pass.
- [x] 5.2 Add webhook unit tests:
  - Entry with both `mac` and `clientId` set → rejected with descriptive error
  - Entry with neither set → rejected
  - Entry with only `mac` → admitted
  - Entry with only `clientId` → admitted
  - Multiple violations → all enumerated in error
  Verify: `go test ./... -run TestWebhook` passes.

## 6. CRD regeneration and docs

- [x] 6.1 Run `make check-gen` and commit any resulting changes (updated CRD YAML, generated deepcopy). Verify: `make check-gen` exits 0 with no diff.
- [x] 6.2 Run `make build-metaldhcp-remote` in `sapcc/helm-charts/system/` to regenerate `managedresources/crds.yaml`. Verify: `git diff` shows the new `clientIdList` and `clientId` fields in the CRD output.

## 7. Integration check

- [x] 7.1 Run the full test suite: `make test`. Verify: all tests pass.
