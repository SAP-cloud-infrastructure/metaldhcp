# metaldhcp

A DHCP server for bare-metal out-of-band networks. It is built on the [coredhcp](https://github.com/coredhcp/coredhcp) library — not a fork — and adds a single custom plugin, `oob`, for Kubernetes-native address management.

## The oob plugin

The `oob` plugin handles IPv4 address allocation (DHCPv4 `server4`) and lease persistence:

- **Address pools** come from two sources (mutually exclusive):
  - *Inline subnets* — defined in the plugin config file (`subnets:`), managed by the deployment pipeline or Helm. When present, these are authoritative.
  - *OOBSubnet CRs* — selected by label (`subnetLabels:`), synced into the cluster by a separate operator. Used when no inline subnets are configured.
- **DHCP relay (giaddr)** — when a request arrives via a relay agent (non-zero `giaddr`), the plugin uses `giaddr` for pool selection. Exact-IP assignment within the pool still uses `clientIP`/`requestedIP`.
- **Per-pool boot URL** — each pool can declare a `bootURL`. When set, the server returns it as DHCP option 67 (`BootFileName`) for UEFI HTTP boot or iPXE chain-loading.
- **Static leases** — `staticLeases:` in the config binds a MAC address to a fixed IP and optional hostname, bypassing pool allocation entirely.
- For each successful allocation the plugin writes a `DHCPLease` CR into the configured namespace, recording the MAC-to-IP binding, lease expiry, and hostname.

The in-process allocator is seeded from existing `DHCPLease` CRs on startup, so no external IPAM backend is required.

## CRDs

Both resources belong to API group `dhcp.metal.ironcore.dev`, version `v1alpha1`:

| Kind | Description |
|------|-------------|
| `OOBSubnet` | Represents an address pool (CIDR, range, gateway, lease time, optional bootURL). |
| `DHCPLease` | Records an active MAC-to-IP binding (MAC, hostname, IP, gateway, lease time, client ID) created by the oob plugin. |

CRD manifests are in `config/crd/bases/`.

## Build

```sh
make build        # produces bin/metaldhcp
make generate     # regenerate DeepCopy methods (api/...)
make manifests    # regenerate CRD manifests
make test         # generate + manifests + fmt + vet + envtest
```

> **macOS note**: `coredhcp/server` references a Linux-only `sendEthernet` function. `make vet`
> and the main-package build are skipped automatically on Darwin. Use the container image or a
> Linux environment for full validation.

## Run

```sh
# List compiled-in plugins
./bin/metaldhcp -list-plugins

# Start the server
./bin/metaldhcp -config example/config.yaml

# Adjust log verbosity
./bin/metaldhcp -config example/config.yaml -loglevel debug
```

The server requires `CAP_NET_BIND_SERVICE` and `CAP_NET_RAW` to bind port 67. In the container image these capabilities are granted via `setcap` at image build time. Running locally may require `sudo` or the same capabilities.

## Configuration

`example/config.yaml` shows a minimal `server4` setup. The `oob` plugin is configured via a separate file (e.g. `example/oob.yaml`); see the inline comments there for pool source precedence and available fields.

A kubeconfig or in-cluster service account is required when the `oob` plugin is active (it reads/writes Kubernetes objects).

### Pool sources

```yaml
# example/oob.yaml

namespace: metaldhcp-system

# OOBSubnet CR label selector — used when subnets: is empty
subnetLabels:
  - key: dhcp
    value: "true"

# Inline pool definitions — when non-empty, these take precedence over OOBSubnet CRs
subnets:
  - cidr: 192.168.10.0/24
    gateway: 192.168.10.1
    rangeStart: 192.168.10.100
    rangeEnd: 192.168.10.200
    leaseTime: 24h
    # bootURL: https://boot-operator.example.com/boot  # UEFI HTTP boot / iPXE chain URL
```

### Static leases

```yaml
staticLeases:
  - mac: "02:aa:bb:cc:dd:03"
    ip: 192.168.10.200
    hostname: bmc-static-01
```

Static leases always return the configured IP regardless of pool or relay hint. The hostname is recorded in the resulting `DHCPLease` CR.

## Downstream

A separate metal-onboarding-operator watches `DHCPLease` objects and drives the server onboarding workflow. metaldhcp itself has no onboarding logic.

The `Hostname` field in `DHCPLease` (populated from static lease config or DHCP option 12) is available for downstream consumers such as DNS registration.

## Dev environment

See [dev/README.md](dev/README.md) for the local kind + Tilt setup, DHCP triggering, relay (giaddr) testing, and static lease examples. The Tilt setup renders the Helm chart from `chart/metaldhcp/` with dev overrides from `dev/values.yaml`.
