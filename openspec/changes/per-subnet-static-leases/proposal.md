# Proposal

## Why

Static DHCP reservations (MAC → fixed IP) belong conceptually and operationally to the subnet they live in, but are currently stored in a top-level `OOBConfig.staticLeases` section separate from the pool definitions. External sync tools (e.g. NetBox) that already write `OOBSubnet` CRs would also need to write static reservations to a different location, splitting the source of truth. Moving static leases under their subnet — both in Helm config and in the CRD — makes each subnet self-contained and eliminates the split.

## What Changes

- **BREAKING**: Remove `OOBConfig.staticLeases` top-level field.
- Add `staticLeases` field to each entry in `OOBConfig.subnets` (the Helm/YAML config path).
- Add `staticLeases` field to `OOBSubnetSpec` (the CRD path).
- Add a validating admission webhook on `OOBSubnet` that rejects any static lease whose IP falls outside the subnet's `cidr`, with a descriptive error (e.g. `static lease 00:11:22:33:44:55 → 10.99.0.5 is outside subnet CIDR 10.0.1.0/24`).
- Update plugin internals: seed config-subnet statics at init (gateway/bootURL come directly from the parent subnet); seed CR-subnet statics incrementally at request time, idempotent via the existing `k.statics` map.

## Capabilities

### New Capabilities

- `oobsubnet-static-leases`: Per-subnet static lease definitions in both the static Helm config and the `OOBSubnet` CRD, with admission validation.

### Modified Capabilities

_(none — no existing specs)_

## Impact

- `internal/api/config.go`: `StaticLease` struct is unchanged; `OOBConfig.StaticLeases` removed; `Subnet.StaticLeases` added.
- `api/v1alpha1/oobsubnet_types.go`: `OOBSubnetSpec.StaticLeases` added; new embedded `StaticLease` type added to the CRD package (or reuse from internal/api with a type alias).
- `plugins/oob/k8s.go`: `seedStaticLeases` updated to accept a pool context so gateway/bootURL come from the parent subnet; `oobSubnetPools` updated to seed CR statics incrementally.
- New: `api/v1alpha1/oobsubnet_webhook.go` — validating webhook implementation.
- `main.go`: webhook server wired in.
- `chart/metaldhcp/`: new `ValidatingWebhookConfiguration` template, webhook Service port, cert-manager `Certificate`/`Issuer` resources.
- CRD YAML regenerated (`make manifests`).
- Tests: existing static lease tests updated; new tests for per-subnet statics and webhook validation.
