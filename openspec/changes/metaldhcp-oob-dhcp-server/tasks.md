# Tasks

## 1. Repository scaffolding and deps

- [x] 1.1 Run `go mod init`; add `github.com/coredhcp/coredhcp`, `github.com/insomniacslk/dhcp`, `sigs.k8s.io/controller-runtime`, `k8s.io/{api,apimachinery,client-go}`, `onsi/ginkgo/v2`, `onsi/gomega`, `sirupsen/logrus`, `gopkg.in/yaml.v3`; verify `go build ./...` succeeds on Linux
- [x] 1.2 Add `PROJECT` file with kubebuilder domain `dhcp.metal.ironcore.dev`; verify `controller-gen` can read it

## 2. CRD types

- [x] 2.1 Create `api/v1alpha1/groupversion_info.go` (group `dhcp.metal.ironcore.dev`, v1alpha1) and `zz_generated.deepcopy.go`; verify `make generate` succeeds
- [x] 2.2 Define `OOBSubnet` type with spec fields `cidr`, `gateway`, `leaseTime`, `rangeStart`, `rangeEnd` and kubebuilder markers; verify `make manifests` generates a valid CRD YAML
- [x] 2.3 Define `DHCPLease` type with spec fields `macAddress`, `ip`, `leaseTime`, `clientID`, object name = normalized MAC, `+kubebuilder:selectablefield` on `macAddress`/`ip`; verify CRD YAML is generated

## 3. Internal packages

- [x] 3.1 Implement `internal/kubernetes/client.go` (register `v1alpha1` scheme, `InitClient`/`GetClient`/`SetClient`/`GetConfig`); verify package compiles
- [x] 3.2 Implement `internal/helper` (MAC normalize, `CheckIPInCIDR`, IPv4 4-byte normalize) and `internal/printer` (VerboseRequest/VerboseResponse); verify package compiles
- [x] 3.3 Implement `internal/api/config.go` (`OOBConfig{namespace, subnetLabels, subnets}`); verify it parses `example/oob.yaml`

## 4. Allocator

- [x] 4.1 Implement `internal/allocator/allocator.go` — in-memory mutex-guarded allocator with re-lease, exact-IP, and first-free (respecting range; excluding network/broadcast/gateway) strategies; verify package compiles
- [x] 4.2 Add allocator unit tests (table-driven): CIDR + in-use set + range/gateway/network/broadcast exclusions, `/30`–`/31`, exhaustion, boundaries; verify `make test` runs the allocator tests green

## 5. oob plugin

- [x] 5.1 Implement `plugins/oob/plugin.go` — `handler4` with giaddr-based `poolHint` / candidate-IP `allocHint` split, pool source abstraction (config → OOBSubnet CR fallback), call `getIP`, set `YourIPAddr`; verify package compiles
- [x] 5.2 Implement `plugins/oob/k8s.go` — `getIP` (pool lookup + allocator mutex + `CreateOrPatch` of DHCPLease), `Setup4`/`Setup6`; verify package compiles

## 6. main.go and binary

- [x] 6.1 Wire `main.go`: `desiredPlugins` list of coredhcp built-ins + `&oob.Plugin`, `shouldSetupKubeClient` helper, conditional `kubernetes.InitClient()`, `server.Start(cfg).Wait()`; verify `go build -o /dev/null .` succeeds on Linux
- [x] 6.2 Add `-list-plugins` flag; verify `go run . -list-plugins` prints `oob`

## 7. envtest and example configs

- [x] 7.1 Add Ginkgo/Gomega envtest suite for the `oob` plugin: config-pool and CR-pool paths, in-CIDR hint → `YourIPAddr` + `DHCPLease` created, re-lease returns same IP and patches (not duplicates) DHCPLease, exact-IP honored, exhaustion → drop, no pool → drop, concurrent requests → distinct IPs; verify suite passes with `KUBEBUILDER_ASSETS=$(pwd)/bin/k8s/1.30.0-$(go env GOOS)-$(go env GOARCH) go test ./plugins/oob/...`
- [x] 7.2 Add `example/config.yaml` (server4 listen + built-in plugins + `oob: oob.yaml`) and `example/oob.yaml` (namespace, subnetLabels, inline subnets list); verify server starts against a kind cluster using the example configs

## 8. Per-pool boot URL (PXE / UEFI HTTP boot)

- [ ] 8.1 Add `BootURL string` field to `internal/api/config.go` `Subnet` struct and `internal/allocator/allocator.go` `Pool` struct; propagate through `configPools` and `oobSubnetPools` in `plugins/oob/k8s.go`
- [ ] 8.2 Return `BootURL` from `getIP` alongside gateway (or return the resolved `Pool`); in `handler4` set `resp.BootFileName = pool.BootURL` when non-empty; verify the envtest suite still passes
- [ ] 8.3 Add `BootURL string` field to `OOBSubnet` spec and regenerate CRD manifest; verify `make manifests` is clean
- [ ] 8.4 Add envtest case: pool with `BootURL` set → response `BootFileName` matches; pool without → `BootFileName` empty
- [ ] 8.5 Update `example/oob.yaml` and `dev/metaldhcp.yaml` with a commented-out `bootURL` example

## 9. CI

- [ ] 9.1 Add a CI pipeline (GitHub Actions or equivalent) that runs `make generate manifests` and fails if there are uncommitted changes, then runs `make test`; verify CI passes on a Linux runner with `ENVTEST_K8S_VERSION=1.30.0`

## 10. README

- [ ] 10.1 Write `README.md` covering: component boundary (metaldhcp = DHCP layer only; moo watches DHCPLease downstream), pool sources (config vs. OOBSubnet CRs, precedence), `example/config.yaml` and `example/oob.yaml` walkthrough, `make tilt-up` dev environment, macOS build caveat; verify all commands documented in the README run as written
