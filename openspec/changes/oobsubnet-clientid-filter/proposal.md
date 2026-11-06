# Proposal

## Why

Some DHCP clients (e.g. iDRAC, DPU BMCs) are known by their DHCP client identifier (option 61) before their MAC address is available — for example from provisioning metadata or vendor documentation. The current `macList` + `mode` mechanism and static leases only match on MAC, so configuration cannot be prepared ahead of time for these clients without first booting the machine and manually extracting the MAC from a live DHCP request.

## What Changes

- **OOBSubnet spec** (`OOBSubnetSpec` and config-layer `Subnet`): add `clientIdList []string` alongside `macList`. The `mode: allow/deny` filter passes a client if its MAC matches `macList` **or** its client ID matches `clientIdList`.
- **Static leases** (`StaticLease` in both API and config layer): add `clientId string` as an alternative to `mac`. Each entry identifies a client by exactly one of `mac` or `clientId`; the other must be empty.
- **Static lease lookup**: extend `lookupStaticLease` to key by client ID in addition to MAC, so a client identified only by client ID receives its reserved IP.
- **CRD regenerated** after API changes (`make check-gen`).

## Capabilities

### New Capabilities

_(none — both changes extend existing capabilities)_

### Modified Capabilities

- `oob-dhcp-server`: the MAC-list filter requirement gains client ID as a parallel matching dimension.
- `oobsubnet-static-leases`: static lease entries gain `clientId` as an alternative identifier to `mac`.

## Impact

- `api/v1alpha1/oobsubnet_types.go` — `OOBSubnetSpec.ClientIdList`, `StaticLease.ClientId`
- `internal/api/config.go` — `Subnet.ClientIdList`, `StaticLease.ClientId`
- `internal/allocator/allocator.go` — `Pool.ClientIdList`
- `plugins/oob/k8s.go` — `oobSubnetPools`, `seedStaticLeases`, `lookupStaticLease`
- `plugins/oob/plugin.go` — filter logic (lines 208–216); `clientID` already in scope
- CRD YAML regenerated; `managedresources/crds.yaml` in `sapcc/helm-charts` rebuilt via `make build-metaldhcp-remote`
