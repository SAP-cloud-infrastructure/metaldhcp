# metaldhcp

A DHCP server for bare-metal out-of-band networks. It is built on the [coredhcp](https://github.com/coredhcp/coredhcp) library — not a fork — and adds a single custom plugin, `oob`, for Kubernetes-native address management.

## The oob plugin

The `oob` plugin handles IPv4 address allocation (DHCPv4 `server4`) and lease persistence:

- **Address pools** come from two sources (mutually exclusive):
  - *Inline subnets* — defined in the plugin config file (`subnets:`), managed by the deployment pipeline or Helm. When present, these are authoritative.
  - *OOBSubnet CRs* — selected by label (`subnetLabels:`), synced into the cluster by a separate operator. Used when no inline subnets are configured.
- For each successful allocation the plugin writes a `DHCPLease` CR into the configured namespace, recording the MAC-to-IP binding and lease expiry.

## CRDs

Both resources belong to API group `dhcp.metal.ironcore.dev`, version `v1alpha1`:

| Kind | Description |
|------|-------------|
| `OOBSubnet` | Represents an address pool (CIDR, range, gateway, lease time). |
| `DHCPLease` | Records an active MAC-to-IP binding (MAC, IP, gateway, lease time, client ID) created by the oob plugin. |

CRD manifests are in `config/crd/bases/`.

## Build

```sh
make build        # produces bin/metaldhcp
make generate     # regenerate DeepCopy methods (api/...)
make manifests    # regenerate CRD manifests
make test         # generate + manifests + fmt + vet + envtest
```

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

`example/config.yaml` shows a minimal `server4` setup. The `oob` plugin is configured via a separate file (e.g. `example/oob.yaml`); see the inline comments there for pool source precedence.

A kubeconfig or in-cluster service account is required when the `oob` plugin is active (it reads/writes Kubernetes objects).

## Downstream

A separate metal-onboarding-operator watches `DHCPLease` objects and drives the server onboarding workflow. metaldhcp itself has no onboarding logic.

## Dev environment

See [dev/README.md](dev/README.md) for the local kind + Tilt setup, DHCP triggering, and relay (giaddr) testing.
