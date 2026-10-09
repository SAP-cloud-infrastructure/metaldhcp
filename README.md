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
- For each successful allocation the plugin writes a `DHCPLease` CR, recording the MAC-to-IP binding, lease expiry, and hostname.
- **MAC vendor lookup** — the server resolves the IEEE OUI prefix of each client MAC to a vendor name (e.g. "Dell", "Hewlett Packard Enterprise", "Lenovo") using an embedded ~500 KB OUI database. The vendor is logged on every DISCOVER, included in Kubernetes Warning events, and stored in the `dhcp.metal.ironcore.dev/vendor` annotation on the `DHCPLease` CR. Visible as the `VENDOR` column in `kubectl get dhcpleases -o wide`.

The in-process allocator is seeded from existing `DHCPLease` CRs on startup, so no external IPAM backend is required.

## CRDs

Both resources belong to API group `dhcp.metal.ironcore.dev`, version `v1alpha1`, and are cluster-scoped (like `Node` and `PersistentVolume`):

| Kind | Description |
|------|-------------|
| `OOBSubnet` | Represents an address pool (CIDR, range, gateway, lease time, optional bootURL). |
| `DHCPLease` | Records an active MAC-to-IP binding (MAC, hostname, IP, gateway, lease time, client ID, optional vendor). Created by the oob plugin; `kubectl get dhcpleases -o wide` shows the `VENDOR` column. |

CRD manifests are in `config/crd/bases/`.

## Build

```sh
make build        # produces bin/metaldhcp
make generate     # regenerate DeepCopy methods (api/...)
make manifests    # regenerate CRD manifests
make test         # generate + manifests + fmt + vet + envtest
```

> **macOS note**: `coredhcp/server` references a Linux-only `sendEthernet` function.
> A temporary `replace` directive in `go.mod` points at `github.com/damyan/coredhcp`,
> which adds a `sendEthernet_darwin.go` stub so the package compiles on macOS.
> Actual DHCP serving still requires Linux. The replace will be removed once a fix
> is merged upstream.

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

## TFTP / PXE boot

When `tftp.enabled` is set in the Helm values, a `tftpd` sidecar runs in the same Pod
as metaldhcp. Because the Pod uses `hostNetwork: true`, both containers share the same
IP, so the DHCP server IP doubles as the TFTP server address. The `nbp` plugin injects
DHCP options 66 (TFTP server name) and 67 (bootfile name) into every response.

The iPXE binary (`snponly.efi`, UEFI network-boot only) is compiled from source during
the Docker build with `DOWNLOAD_PROTO_HTTPS` enabled and DigiCert root CAs embedded,
so it can fetch the boot image over HTTPS from boot-operator. It is baked into the
image at `/ipxe/snponly.efi`. Set `tftp.ipxeURL` to download a different binary at
Pod startup instead.

**Firewall requirement**: UDP/69 must be permitted from the OOB subnet to the
LoadBalancer IP. This is a deployment prerequisite and is not enforced by the chart.

## Downstream

A separate metal-onboarding-operator watches `DHCPLease` objects and drives the server onboarding workflow. metaldhcp itself has no onboarding logic.

The `Hostname` field in `DHCPLease` (populated from static lease config or DHCP option 12) is available for downstream consumers such as DNS registration.

## Dev environment

See [dev/README.md](dev/README.md) for the local kind + Tilt setup, DHCP triggering, relay (giaddr) testing, and static lease examples. The Tilt setup renders the Helm chart from `chart/metaldhcp/` with dev overrides from `dev/values.yaml`.
