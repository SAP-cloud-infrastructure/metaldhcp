# Tasks

## 1. Config types

- [x] 1.1 Remove `StaticLeases []StaticLease` from `OOBConfig` in `internal/api/config.go` and add `StaticLeases []StaticLease` to `Subnet`; verify `go build ./...` passes.
- [x] 1.2 Update `plugins/oob/k8s.go`: change `seedStaticLeases` to accept `(subnet api.Subnet, pool allocator.Pool)` instead of `[]api.StaticLease`; derive gateway/bootURL from the pool argument instead of scanning all config pools; call it once per subnet inside `configPools` conversion; verify `go build ./...` passes.
- [x] 1.3 Update `plugins/oob/plugin_test.go`: move all existing static lease test fixtures from `OOBConfig.StaticLeases` into their containing `Subnet.StaticLeases`; verify `go test ./plugins/oob/...` passes with no changes to assertions.

## 2. CRD types

- [x] 2.1 Add `StaticLease` struct to `api/v1alpha1/` (fields `MAC`, `IP`, `Hostname` with kubebuilder validation markers) and add `StaticLeases []StaticLease` to `OOBSubnetSpec` in `oobsubnet_types.go`; run `make manifests` and verify the generated CRD YAML includes the new field.
- [x] 2.2 Run `make generate` (deepcopy) and verify `zz_generated.deepcopy.go` covers the new `StaticLease` type; verify `go build ./...` passes.

## 3. Plugin internals — CR path

- [x] 3.1 In `plugins/oob/k8s.go`, extend `oobSubnetPools` to iterate each CR's `StaticLeases` after building the pool list; for entries not already in `k.statics`, call `alloc.Reserve()` and insert into `k.statics` with gateway/bootURL from the parent CR; verify `go build ./...` passes.
- [x] 3.2 Add a `plugin_test.go` test: build a plugin with `SubnetLabels` (no static config), send a DISCOVER → expect drop (no CR); create an `OOBSubnet` CR with a `StaticLeases` entry; send the same DISCOVER → expect the reserved IP; verify `go test ./plugins/oob/...` passes.
- [x] 3.3 Add a `plugin_test.go` test confirming that the static IP from a CR entry is not offered to a different MAC; verify `go test ./plugins/oob/...` passes.

## 4. Validating webhook

- [x] 4.1 Create `api/v1alpha1/oobsubnet_webhook.go` implementing `admission.CustomValidator` for `OOBSubnet`; `ValidateCreate` and `ValidateUpdate` iterate `spec.staticLeases` and collect all IPs outside `spec.cidr`, returning a combined error listing each offending entry as `static lease <MAC> → <IP> is outside subnet CIDR <CIDR>`; verify `go build ./...` passes.
- [x] 4.2 Add unit tests in `api/v1alpha1/oobsubnet_webhook_test.go`: valid CR accepted; single out-of-CIDR IP rejected with expected message; multiple violations all enumerated in the error; verify `go test ./api/...` passes.
- [x] 4.3 Wire the webhook server into `main.go`: add a `--webhook-addr` flag (default `:9443`), load TLS cert/key from paths configurable via flags, register `/validate-oobsubnet` on a TLS `http.Server`, start it in a goroutine alongside the existing health server; verify `go build ./...` passes and the server starts with valid test certs.

## 5. Helm chart

- [x] 5.1 Add `webhook.enabled` (default `false`), `webhook.port` (default `9443`), `webhook.certSecretName` Helm values; add webhook port to `service.yaml` and mount the cert secret in `deployment.yaml` when `webhook.enabled`; verify `helm template` renders correctly with both `webhook.enabled: true` and `false`.
- [x] 5.2 Add `validatingwebhookconfiguration.yaml` template (rendered only when `webhook.enabled`) pointing to the webhook service, with `failurePolicy: Ignore` as the default and `cert-manager.io/inject-ca-from` annotation; verify `helm template --set webhook.enabled=true` includes the resource.
- [x] 5.3 Add optional cert-manager `Issuer` and `Certificate` templates (rendered when `webhook.enabled` and `webhook.certManager.enabled`); verify `helm template --set webhook.enabled=true,webhook.certManager.enabled=true` renders both.

## 6. Integration verification

- [x] 6.1 Run the full test suite (`go test ./...`) and confirm all tests pass including the new webhook and incremental-seeding tests.
- [x] 6.2 Run `make manifests` and confirm the committed CRD YAML in `config/crd/` (or equivalent) is up-to-date with no diff.

## 7. Per-subnet DNS servers and domain

- [x] 7.1 Add `DNSServers []string` and `Domain string` to `internal/api/config.go` `Subnet` struct (global fields on `OOBConfig` remain as fallback); add `DNSServers []net.IP` and `Domain string` to `allocator.Pool` and `staticEntry`; propagate from `seedConfigSubnets`; in `handler4` use subnet-level DNS/domain when non-empty, else fall back to `k.dnsServers`/`k.domain`; verify `go build ./...` passes.
- [x] 7.2 Add `DNSServers []string` and `Domain string` to `OOBSubnetSpec` in `api/v1alpha1/oobsubnet_types.go`; run `make manifests` and `make generate`; propagate from `oobSubnetPools` into Pool and staticEntry; verify `go build ./...` passes.
- [x] 7.3 Add `plugin_test.go` tests: subnet with `DNSServers`/`Domain` override → OFFER carries subnet values; subnet without override → OFFER carries global values; verify `go test ./plugins/oob/...` passes.
- [x] 7.4 Update Helm `configmap.yaml` to render `dnsServers` and `domain` per subnet when set; verify `helm template` renders correctly with and without per-subnet overrides.
- [x] 7.5 Run `go test ./...` and `make manifests`; confirm all tests pass and CRD YAML is up-to-date.

## Workflow follow-up

- Open a PR; the breaking change to `OOBConfig.staticLeases` must be noted in the PR description so deployers know to update their Helm values.
- Archive the change after the PR is merged.
