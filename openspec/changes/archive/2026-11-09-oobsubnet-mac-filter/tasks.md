# Tasks

## 1. API types

- [x] 1.1 Add `AllowedMACs []string` and `ResponseDelay *metav1.Duration` to `OOBSubnetSpec` in `api/v1alpha1/oobsubnet_types.go` with kubebuilder markers; verify `make generate manifests` runs clean and the updated CRD YAML contains the new fields
- [x] 1.2 Add `AllowedMACs []string` and `ResponseDelay string` to `api.Subnet` in `internal/api/config.go`; verify existing config unmarshal tests still pass

## 2. allocator.Pool

- [x] 2.1 Add `AllowedMACs []string` and `ResponseDelay time.Duration` to `allocator.Pool` in `internal/allocator/allocator.go`; verify existing allocator tests still pass

## 3. Config bootstrap

- [x] 3.1 In `NewK8sClient`, after listing labeled CRs, apply bootstrap logic per config subnet: no CR → create with annotation `dhcp.metal.ironcore.dev/managed-by: helm`; CR with that annotation → update from config; CR without it → skip. Verify with unit tests covering all three cases.
- [x] 3.2 Expand the ClusterRole / RBAC manifests (or Helm chart RBAC) to grant `create` + `update` on `OOBSubnet`; verify `make manifests` or helm template output reflects the change

## 4. Pool propagation

- [x] 4.1 In `oobSubnetPools`, propagate `AllowedMACs` and `ResponseDelay` from CR spec into `allocator.Pool`; verify a unit test that a CR with both fields produces a pool with those values set
- [x] 4.2 Remove the `configPools` field and the static-config branch from `selectPool` (runtime now uses CRs only); verify existing pool-selection tests pass with CR-backed pools

## 5. Request filtering and delay

- [x] 5.1 In the DHCP request handler (`handler4`), after `selectPool` returns, check `pool.AllowedMACs`: if non-empty and client MAC not in list, log at Debug and return without responding; verify with a unit test that a request from an unlisted MAC produces no offer
- [x] 5.3 Add `CatchAll bool` to `OOBSubnetSpec`, `api.Subnet`, and `allocator.Pool`; regenerate CRD; when `CatchAll=false` (default), requests from MACs not in `AllowedMACs` are silently dropped even when `AllowedMACs` is empty (deny-all default); when `CatchAll=true`, the filter is bypassed for dev/debug use; verify with a test that deny-all is the default behavior- [x] 5.2 After offer construction and before sending, sleep `pool.ResponseDelay` when non-zero; verify with a unit test that the response is delayed by at least the configured duration

## 6. Integration

- [x] 6.1 Write an integration test (or extend an existing one) that covers: config subnet bootstraps a CR; a second startup leaves the CR unchanged; a request from an allowed MAC gets an offer; a request from an unlisted MAC gets no offer; a subnet with `responseDelay` delays the offer

## Workflow follow-up

- Open a PR against `sap-cloud-infrastructure/metaldhcp` once all tasks are complete.
- Add the PR link to the relevant project file in the agent workspace.
