# metaldhcp dev environment

A local [kind](https://kind.sigs.k8s.io/) + [Tilt](https://tilt.dev/) setup that runs
**metaldhcp** against a kind cluster so you can trigger DHCP leases and inspect the resulting
`DHCPLease` CRs.

```
udhcpc / scapy (debug sidecar) --IPv4 DISCOVER--> metaldhcp :67
    metaldhcp oob plugin: allocate IP from the config pool, write DHCPLease CR
```

## Prerequisites

- Docker running (Docker Desktop / colima / podman machine). **kind needs a running Docker.**
- `kind`, `tilt`, `kubectl`, `go` on `PATH`.

## Bring it up

```sh
# from the metaldhcp repo root
make tilt-up   # creates the kind cluster + local registry if needed, then starts Tilt
```

Tilt builds the metaldhcp image from source, then applies the CRDs and metaldhcp. Wait until the `metaldhcp` resource is green.

## Trigger a lease

The `debug` sidecar creates a veth pair on startup: `veth0-server` (server side,
`192.168.100.1/24`) and `veth0` (client side). Broadcasts sent on `veth0`
traverse the pair and arrive at `veth0-server`, so coredhcp can send Ethernet replies back.

```sh
# interactive shell in the debug sidecar
kubectl exec -it -n metaldhcp-system deploy/metaldhcp -c debug -- bash

# from inside: full DHCP exchange
udhcpc -i veth0 -n -q

# send a hostname with the request (recorded in DHCPLease.spec.hostname)
udhcpc -i veth0 -n -q -x hostname:node-direct

# watch the wire while doing it
tcpdump -i veth0-server -n port 67 or port 68 &
udhcpc -i veth0 -n -q

# simulate different MACs (dhcping sends unicast DISCOVER, gets ACK back)
dhcping -v -s 192.168.100.1 -h 02:aa:bb:cc:dd:ee -t 3
```

## Testing relay (giaddr), static leases, and TFTP

Run all scenarios at once:

```sh
make test-dhcp   # or ./dev/test-dhcp.sh
```

This sends: a direct udhcpc request, two scapy packets with `giaddr` set (one per pool),
a static-lease request, an unmatched relay (expects a `NoPoolFound` Warning event), a
raw TFTP fetch of `snponly.efi` from the tftpd sidecar, and a DISCOVER that asserts
option 54 (server identifier) equals the LoadBalancer VIP. It prints the resulting
`DHCPLease` table after the DHCP scenarios.

For the relay path, the metaldhcp logs should show:

```
Allocating for MAC 02:aa:bb:cc:dd:01 (pool hint 192.168.100.50, alloc hint <nil>, exact false, relay true)
```

The `relay true` flag and `pool hint` confirm the giaddr code path. The static lease
(`02:aa:bb:cc:dd:03`) always returns `192.168.100.200` with hostname `node-direct-static`
regardless of relay or pool hint.

## Observe

```sh
# watch leases appear (name = normalized MAC)
kubectl get dhcpleases -n metaldhcp-system -w

# metaldhcp allocating + recording
kubectl logs -n metaldhcp-system deploy/metaldhcp -c metaldhcp
```

## Tear down

```sh
make tilt-down       # remove Tilt resources
make kind-delete     # delete the kind cluster and registry
```

## Notes / things you may need to tune

- The exact `dhcping` flags vary by build; the goal is simply an IPv4 DISCOVER to `:67`. If
  `dhcping` isn't cooperative, any IPv4 DHCP DISCOVER against the pod works (e.g. `nmap --script
broadcast-dhcp-discover`), or create an `OOBSubnet`/edit `dev/values.yaml`.
- metaldhcp runs as root with `NET_RAW`/`NET_BIND_SERVICE` here — dev convenience, not a
  production posture.
- The pool, lease time, and server-id live in `dev/values.yaml` (rendered into the chart's
  ConfigMap by Tilt).
