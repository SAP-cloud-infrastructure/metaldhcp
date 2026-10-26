# Proposal

## Why

Two metaldhcp instances serving the same BMC subnet (e.g. `10.44.14.64/26` shared by nodes on both rt-qa-de-8 and m-qa-de-8) both receive every DHCPDiscover forwarded by the relay. Without per-MAC filtering each instance would race to answer, producing non-deterministic cluster assignment. This change lets each instance declare which MACs it owns and optionally delay its offer so co-deployed instances with explicit filters can win.

Related: [foundation-issues #1997](https://github.wdf.sap.corp/sap-cloud-infrastructure/foundation-issues/issues/1997).

## What Changes

- `OOBSubnetSpec` gains `allowedMACs []string` — when non-empty, the instance silently ignores requests from MACs not in the list.
- `OOBSubnetSpec` gains `responseDelay *metav1.Duration` — when non-zero, the instance sleeps before sending its offer (catch-all / fallback mode).
- `api.Subnet` (Helm config path) gains the same two fields (`allowedMACs []string`, `responseDelay string`) so filtering works when pools are defined statically.
- **Config bootstrap with managed-by annotation**: at startup, config subnets are synced to OOBSubnet CRs. CRs annotated `dhcp.metal.ironcore.dev/managed-by: helm` are created or updated from config. CRs without that annotation (manually created or netbox-synced) are left untouched. Runtime pool selection uses CRs exclusively.
- `allocator.Pool` carries both fields through to the request handler.
- The request handler applies MAC filtering before allocation and the delay before sending the offer.

## Capabilities

### New Capabilities

- `oobsubnet-request-filter`: Per-subnet MAC allowlist and response delay for DHCP offer arbitration across co-deployed instances.

### Modified Capabilities

- `oob-dhcp-server`: pool selection strategy changes — config and CR pools are now merged rather than mutually exclusive.

## Impact

- `api/v1alpha1/oobsubnet_types.go` — two new fields on `OOBSubnetSpec`
- `internal/api/config.go` — two new fields on `api.Subnet`
- `internal/allocator/allocator.go` (or `Pool` type) — `AllowedMACs` and `ResponseDelay` on `Pool`
- `plugins/oob/k8s.go` — merge config and CR pools (CR fields win on same CIDR); propagate filter fields; apply filter + delay in request path
- CRD manifest regeneration (`make generate manifests`)
- No migration; new fields are optional and default to current behaviour (catch-all, zero delay)
