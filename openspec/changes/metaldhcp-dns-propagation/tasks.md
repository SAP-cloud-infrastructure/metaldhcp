# Tasks

## 1. Integration approach decision

- [ ] 1.1 Document the decision to use ExternalDNS `DNSEndpoint` CRs (over direct DNS API
  calls) in `docs/dns-propagation.md` or the PR description; add `sigs.k8s.io/external-dns`
  as a Go module dependency and confirm `go build ./...` passes.

## 2. Controller implementation

- [ ] 2.1 Create `controllers/dnsendpoint/` package with a controller-runtime reconciler
  that watches `DHCPLease` CRs; on create/update with non-empty `spec.hostname`, ensure a
  `DNSEndpoint` CR exists (name = lease name, A record target = `spec.ip`); on hostname
  cleared or lease deleted, delete the `DNSEndpoint`; verify `go build ./...` passes.
- [ ] 2.2 Set an owner reference from `DHCPLease` to `DNSEndpoint` so controller-runtime GC
  removes the endpoint when the lease is deleted; verify `go build ./...` passes.
- [ ] 2.3 Register the reconciler in `main.go` behind `--dns-enabled` (default `false`);
  fail fast if `--dns-enabled` is set but ExternalDNS CRD is absent at startup; verify
  `go build ./...` passes.

## 3. Helm chart

- [ ] 3.1 Add `dns.enabled` value (default `false`); when true, append `--dns-enabled` to
  container args and add `get/list/watch/create/update/delete` on
  `dnsendpoints.externaldns.k8s.io` to the ClusterRole; verify `helm lint` and `helm
  template --set dns.enabled=true` render correctly.

## 4. Tests

- [ ] 4.1 Add envtest cases: lease created with hostname → `DNSEndpoint` A record created
  with correct IP; lease with empty hostname → no `DNSEndpoint`; lease deleted →
  `DNSEndpoint` removed; lease hostname updated → endpoint target updated; verify
  `go test ./controllers/...` passes.
