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

- [x] 8.1 Add `BootURL string` field to `internal/api/config.go` `Subnet` struct and `internal/allocator/allocator.go` `Pool` struct; propagate through `configPools` and `oobSubnetPools` in `plugins/oob/k8s.go`
- [x] 8.2 Return `BootURL` from `getIP` alongside gateway (or return the resolved `Pool`); in `handler4` set `resp.BootFileName = pool.BootURL` when non-empty; verify the envtest suite still passes
- [x] 8.3 Add `BootURL string` field to `OOBSubnet` spec and regenerate CRD manifest; verify `make manifests` is clean
- [x] 8.4 Add envtest case: pool with `BootURL` set → response `BootFileName` matches; pool without → `BootFileName` empty
- [x] 8.5 Update `example/oob.yaml` and `dev/metaldhcp.yaml` with a commented-out `bootURL` example

## 9. Static leases

- [x] 9.1 Add `StaticLeases []StaticLease` (fields: `mac`, `ip`, `hostname`) to `internal/api/config.go` `OOBConfig`; verify it parses a config with static entries
- [x] 9.2 Extend allocator: add `Reserve(macKey string, ip net.IP)` that marks an address as permanently held (not reassignable to other MACs); seed from `staticLeases` in `NewK8sClient` alongside `seedFromLeases`
- [x] 9.3 In `handler4`, before calling `getIP`, check for a static binding by MAC; if found, use that IP directly and skip pool selection; still call `applyLease` to write/update the `DHCPLease`
- [x] 9.4 Add unit tests for `Reserve`: static IP returned for correct MAC, static IP not reassigned to other MACs, static IP outside any pool CIDR still served, `Restore` does not overwrite a static binding
- [x] 9.5 Add envtest case: static lease configured → response always returns the configured IP; `DHCPLease` records hostname

## 10. CI

- [x] 10.1 Add a CI pipeline (GitHub Actions or equivalent) that runs `make generate manifests` and fails if there are uncommitted changes, then runs `make test`; verify CI passes on a Linux runner with `ENVTEST_K8S_VERSION=1.30.0`

## 11. README

- [x] 11.1 Write `README.md` covering: component boundary (metaldhcp = DHCP layer only; moo watches DHCPLease downstream), pool sources (config vs. OOBSubnet CRs, precedence), `example/config.yaml` and `example/oob.yaml` walkthrough, `make tilt-up` dev environment, macOS build caveat; verify all commands documented in the README run as written

## 12. Helm chart

- [x] 12.1 Add a vanilla Helm chart to `chart/metaldhcp/` covering Namespace, ServiceAccount, ClusterRole + Binding, ConfigMap (`config.yaml` + `oob.yaml` templated from values), Deployment (image, args, capabilities, volume mount); verify `helm lint` passes
- [x] 12.2 Parameterise essential values: `image.repository`, `image.tag`, `namespace`, `oob.namespace`, `oob.subnets`, `oob.staticLeases`; add `values.yaml` with safe defaults
- [x] 12.3 Verify `helm template` renders a manifest equivalent to `dev/metaldhcp.yaml` for the dev configuration

## 13. Prometheus metrics

- [ ] 13.1 Expose a `/metrics` HTTP endpoint (port 8080) via `prometheus/client_golang`
- [ ] 13.2 Add metrics: active lease count per pool (`metaldhcp_leases_active`), total allocations (`metaldhcp_allocations_total`), allocation errors (`metaldhcp_allocation_errors_total`), pool utilization ratio (`metaldhcp_pool_utilization_ratio`)
- [ ] 13.3 Add a `ServiceMonitor` to the Helm chart (optional, gated by a values flag)
- [ ] 13.4 Add unit tests for counter increments; verify the metrics endpoint responds in envtest

## 14. Kubernetes events for unmatched DHCP requests

- [x] 14.1 Wire a `record.EventRecorder` in `main.go` (via `record.NewEventRecorder`) and pass it into `K8sClient` at init time
- [x] 14.2 In `handler4`, emit a `Warning` event on the metaldhcp Pod for each DISCOVER/REQUEST that cannot be served: no pool found (`NoPoolFound`), pool exhausted (`PoolExhausted`), allocation error (`AllocationFailed`); include MAC and giaddr in the message
- [x] 14.3 Add envtest case: DISCOVER with unknown giaddr → Warning event emitted on Pod with reason `NoPoolFound`

## 15. TFTP server for iPXE binary delivery

BIOS PXE boot fetches the iPXE binary via TFTP before it can chainload HTTP boot.
metaldhcp must serve (or proxy) TFTP so the full PXE boot chain works without a
separate dnsmasq instance. UDP/69 from the OOB subnet to the DHCP server IP must
be permitted in the network firewall (a deployment prerequisite, not a code task).

- [ ] 15.1 Evaluate integration options: coredhcp TFTP plugin (serves files from a
  ConfigMap-mounted directory) vs. dedicated TFTP sidecar container in the Pod
- [ ] 15.2 Implement chosen approach; wire the iPXE binary path via a Helm value
  (`tftp.enabled`, `tftp.rootDir` or equivalent)
- [ ] 15.3 Add the TFTP port (UDP/69) to the Service and, where applicable, to the
  LoadBalancer annotation so the port is reachable from the OOB subnet
- [ ] 15.4 Update the Helm chart README / deployment notes with the required firewall
  rule: allow UDP/69 from the OOB /26 (or site-specific) subnet to the LoadBalancer IP

## 16. DHCP option 54 (server identifier) set to LoadBalancer VIP

coredhcp responds with the pod IP as the DHCP server identifier (option 54 /
`siaddr`). Clients use this address for unicast RENEW and REQUEST messages; if it
resolves to the pod IP (not the LoadBalancer VIP) renewals bypass the LB and break
when the pod restarts or moves. Observed during qa-de-8 buildup (helm-charts #12235).

- [ ] 16.1 Add a `server.externalIP` value (reuse the existing `externalIP` value if
  appropriate) and pass it into the coredhcp `serverid` plugin config and as the
  `siaddr` field in OFFER/ACK responses
- [ ] 16.2 Verify with a test DISCOVER that the OFFER carries option 54 equal to the
  configured external IP, not the pod IP
- [ ] 16.3 Update `example/oob.yaml` and the Helm chart values with a comment
  explaining why this must match the LoadBalancer IP

## 18. MAC vendor lookup

Add optional hardware vendor enrichment using the embedded IEEE OUI database
(`github.com/endobit/oui`). The lookup is a pure in-memory map operation (no
network, no latency) so it is safe in the DHCP hot path.

- [ ] 18.1 Add `github.com/endobit/oui` dependency; gate enrichment behind an
  `--vendor-lookup` flag (default off) so the ~500 KB embedded database is opt-in
- [ ] 18.2 In `handler4`, look up the vendor from the client MAC and attach it to
  the compact debug log line (e.g. `→ DISCOVER mac=aa:bb:cc:dd:ee:01 (Dell) giaddr=…`)
- [ ] 18.3 Include the vendor string in Kubernetes Warning event messages
  (`NoPoolFound`, `PoolExhausted`) to ease triage
- [ ] 18.4 Optionally store the vendor in a `DHCPLease` annotation
  (`dhcp.metal.ironcore.dev/vendor`) so downstream operators have it without a
  separate lookup; document that the value reflects the OUI at the time of lease
  creation and may become stale if the database is not kept up to date

## 17. DNS propagation

- [ ] 17.1 Decide integration approach: controller writing `externaldns.k8s.io/v1alpha1 DNSEndpoint` CRs (ExternalDNS) vs direct DNS API calls; document the decision in the PR
- [ ] 17.2 Implement a controller watching `DHCPLease` CRs — on create/update write a forward A record; on delete remove it; skip leases with empty hostname
- [ ] 17.3 Add RBAC for the DNS resource (DNSEndpoint or equivalent) to the Helm chart
- [ ] 17.4 Add envtest coverage: lease create with hostname → DNS record created; hostname empty → no record; lease delete → record removed
