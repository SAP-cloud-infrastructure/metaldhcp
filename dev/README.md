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

# watch the wire while doing it
tcpdump -i veth0-server -n port 67 or port 68 &
udhcpc -i veth0 -n -q

# simulate different MACs (dhcping sends unicast DISCOVER, gets ACK back)
dhcping -v -s 192.168.100.1 -h 02:aa:bb:cc:dd:ee -t 3
```

## Testing relay (giaddr)

In production, DHCP packets from BMC/OOB ports arrive via a relay agent (DHCP helper address)
which sets `giaddr` to its own address on the client subnet. metaldhcp uses `giaddr` as the
pool-selection hint when it is non-zero; the actual allocation hint (`clientIP`/`requestedIP`)
still drives exact-IP assignment within that pool.

**Quick smoke-test (single pool, existing config)**

`scapy` is in the netshoot image. From the `debug` sidecar:

```sh
scapy -H << 'EOF'
mac = '02:aa:bb:cc:dd:01'
pkt = Ether(src=mac, dst='ff:ff:ff:ff:ff:ff') / IP(src='0.0.0.0', dst='255.255.255.255') / UDP(sport=68, dport=67) / BOOTP(chaddr=mac2str(mac), giaddr='192.168.100.50', flags=0x8000) / DHCP(options=[('message-type','discover'),'end'])
sendp(pkt, iface='veth0')
EOF
```

In the metaldhcp logs (`kubectl logs -n metaldhcp-system deploy/metaldhcp -c metaldhcp`) you
should see a debug line like:

```
Allocating for MAC 02:aa:bb:cc:dd:01 (pool hint 192.168.100.50, alloc hint <nil>, exact false, relay true)
```

The `relay true` flag and `pool hint 192.168.100.50` confirm the giaddr path. A `DHCPLease` is written.

**Multi-pool routing test**

The dev config already includes a second pool (`10.0.2.0/24`). Send with `giaddr` in that
subnet to verify the correct pool is selected:

```sh
scapy -H << 'EOF'
mac = '02:aa:bb:cc:dd:01'
pkt = Ether(src=mac, dst='ff:ff:ff:ff:ff:ff') / IP(src='0.0.0.0', dst='255.255.255.255') / UDP(sport=68, dport=67) / BOOTP(chaddr=mac2str(mac), giaddr='10.0.2.1', flags=0x8000) / DHCP(options=[('message-type','discover'),'end'])
sendp(pkt, iface='veth0')
EOF
```

The resulting `DHCPLease` should have an IP in `10.0.2.0/24`:

```sh
kubectl get dhcpleases -n metaldhcp-system -o wide
```

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
  broadcast-dhcp-discover`), or create an `OOBSubnet`/edit `dev/metaldhcp.yaml`'s pool.
- metaldhcp runs as root with `NET_RAW`/`NET_BIND_SERVICE` here — dev convenience, not a
  production posture.
- The pool, lease time, and server-id live in the `metaldhcp-config` ConfigMap in
  `dev/metaldhcp.yaml`.
