# Design

## Approach

Use **ExternalDNS `DNSEndpoint` CRs** rather than direct DNS API calls. This keeps
metaldhcp free of DNS provider dependencies and re-uses the ExternalDNS deployment
already present in the cluster. The alternative (direct Designate/PowerDNS API calls)
was ruled out: it introduces provider-specific credentials and breaks portability.

## Controller

A standard controller-runtime reconciler, registered in `main.go` behind `--dns-enabled`.

```
DHCPLease (all clusters) → Reconciler → DNSEndpoint (same cluster)
```

The reconciler lives in a new package `controllers/dnsendpoint/`. It receives a
`ctrl.Manager` (the same manager used by the existing `K8sClient`—or a new manager
started in `main.go`).

### Reconcile loop

1. Fetch the `DHCPLease` by name. If not found (deleted), ensure the corresponding
   `DNSEndpoint` is deleted (by name) and return.
2. If `spec.hostname == ""`, delete any existing `DNSEndpoint` and return.
3. Build the desired `DNSEndpoint` (see spec for fields).
4. `CreateOrPatch` the `DNSEndpoint`.

### Owner reference

Set `DHCPLease` as the owner of the `DNSEndpoint`. controller-runtime GC will clean up
the endpoint when the lease is deleted, even if the controller is down at deletion time.

## ExternalDNS CRD dependency

Import only the types via `sigs.k8s.io/external-dns/endpoint` package (already in the
Go module ecosystem). Do not vendor the full ExternalDNS binary. The import provides
`endpoint.DNSEndpoint` and `endpoint.Endpoint` structs.

If the CRD is absent, the controller fails to register and logs a fatal error at startup
(no silent degradation).

## Helm chart

- Add `dns.enabled` (default `false`) value.
- When enabled, add `--dns-enabled` to the container args.
- Extend the ClusterRole with `dnsendpoints.externaldns.k8s.io` verbs.

## Testing

envtest: register both `DHCPLease` and `DNSEndpoint` CRDs, start the reconciler,
create/update/delete leases, assert endpoint presence and content.
