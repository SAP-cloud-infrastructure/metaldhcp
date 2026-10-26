#!/usr/bin/env bash
# SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
# SPDX-License-Identifier: Apache-2.0
#
# Smoke-test metaldhcp running in the local kind cluster (make tilt-up).
# Sends relay-style DHCP DISCOVERs via scapy, captures OFFERs, and checks
# the response fields. Requires the metaldhcp pod to be ready.

set -euo pipefail

CONTEXT="${KUBE_CONTEXT:-kind-metaldhcp}"
NS="metaldhcp-system"

DELL_MAC="18:66:da:00:11:01"    # Dell
HPE_MAC="04:09:73:00:11:02"     # Hewlett Packard Enterprise
LENOVO_MAC="10:c5:95:00:11:03"  # Lenovo — has a static lease

GIADDR_A="192.168.100.50"  # relay IP for pool 192.168.100.0/24
GIADDR_B="10.0.2.50"       # relay IP for pool 10.0.2.0/24

PASS=0
FAIL=0

pass() { echo "    PASS: $*"; (( ++PASS )); }
fail() { echo "    FAIL: $*" >&2; (( ++FAIL )); }

exec_debug() {
  kubectl --context "${CONTEXT}" exec -i -n "${NS}" deploy/metaldhcp -c debug -- "$@"
}

# dhcp_relay <mac> <giaddr> [extra_opts]
# Sends a DHCP DISCOVER as a relay agent and prints the OFFER fields as
# KEY=VALUE lines, e.g. YIADDR=192.168.100.12 ROUTER=192.168.100.1 ...
# Prints nothing and exits 0 on timeout (no offer).
dhcp_relay() {
  local mac="$1" giaddr="$2" extra="${3:-}"
  exec_debug python3 - "$mac" "$giaddr" "$extra" <<'PYEOF'
import sys, threading, time
from scapy.all import *

mac_str, giaddr, extra = sys.argv[1], sys.argv[2], sys.argv[3]

offers = []
started = threading.Event()

def capture():
    started.set()
    pkts = sniff(iface='veth0-server',
                 filter='udp and src port 67 and dst port 67',
                 count=1, timeout=4)
    offers.extend(pkts)

t = threading.Thread(target=capture, daemon=True)
t.start()
started.wait()
time.sleep(0.05)  # ensure sniffer entered recv() before we send

opts = [('message-type', 'discover')]
if extra:
    for part in extra.split(','):
        k, _, v = part.partition('=')
        opts.append((k.strip(), v.strip() if v else True))
opts.append('end')

pkt = (Ether(src=mac_str, dst='ff:ff:ff:ff:ff:ff') /
       IP(src='0.0.0.0', dst='255.255.255.255') /
       UDP(sport=68, dport=67) /
       BOOTP(chaddr=mac2str(mac_str), giaddr=giaddr) /
       DHCP(options=opts))
sendp(pkt, iface='veth0', verbose=False)

t.join(timeout=5)
if not offers or BOOTP not in offers[0]:
    sys.exit(0)

o = offers[0]
bp = o[BOOTP]
print(f"YIADDR={bp.yiaddr}")
print(f"ROUTER={bp.siaddr}")
dhcp_opts = {}
for opt in o[DHCP].options:
    if isinstance(opt, (list, tuple)) and len(opt) >= 2 and isinstance(opt[0], str) and opt[0] != 'end':
        dhcp_opts[opt[0]] = list(opt[1:]) if len(opt) > 2 else opt[1]
if 'router' in dhcp_opts:
    print(f"ROUTER={dhcp_opts['router']}")
if 'name_server' in dhcp_opts:
    ns = dhcp_opts['name_server']
    print(f"DNS={','.join(str(x) for x in (ns if isinstance(ns, list) else [ns]))}")
if 'domain' in dhcp_opts:
    d = dhcp_opts['domain']
    print(f"DOMAIN={d.decode() if isinstance(d, bytes) else d}")
if 'NTP_server' in dhcp_opts:
    ntp = dhcp_opts['NTP_server']
    print(f"NTP={','.join(str(x) for x in (ntp if isinstance(ntp, list) else [ntp]))}")
if 'server_id' in dhcp_opts:
    print(f"SERVER_ID={dhcp_opts['server_id']}")
if 'lease_time' in dhcp_opts:
    print(f"LEASE_TIME={dhcp_opts['lease_time']}")
PYEOF
}

# parse_offer: read KEY=VALUE lines from dhcp_relay output into variables.
# Usage: eval "$(parse_offer "$output")"  →  $YIADDR, $DNS, etc.
parse_offer() {
  local out="$1"
  # emit shell-safe assignments
  while IFS='=' read -r key val; do
    [[ -n "$key" ]] && printf '%s="%s"\n' "$key" "$val"
  done <<< "$out"
}

# cidr_contains <ip> <cidr>  — true if ip is in cidr range
cidr_contains() {
  exec_debug python3 -c "
import ipaddress, sys
print('yes' if ipaddress.ip_address('$1') in ipaddress.ip_network('$2', strict=False) else 'no')
" 2>/dev/null | grep -q yes
}

# ---------------------------------------------------------------------------

echo "==> Waiting for metaldhcp pod to be ready..."
kubectl --context "${CONTEXT}" -n "${NS}" wait --for=condition=ready pod \
  -l app.kubernetes.io/name=metaldhcp --timeout=60s

echo ""
echo "==> Setup: ensure veth0-server can route responses to both pool subnets"
exec_debug sh -c "
  ip addr show veth0-server | grep -q '10\.0\.2\.' \
    || ip addr add 10.0.2.1/24 dev veth0-server
  VETH0_MAC=\$(cat /sys/class/net/veth0/address)
  ip neigh replace ${GIADDR_A} lladdr \"\$VETH0_MAC\" dev veth0-server nud permanent
  ip neigh replace ${GIADDR_B} lladdr \"\$VETH0_MAC\" dev veth0-server nud permanent
" 2>/dev/null || true

# ---------------------------------------------------------------------------
echo ""
echo "==> 1. Relay DISCOVER — Dell MAC, pool 192.168.100.0/24"
offer1="$(dhcp_relay "${DELL_MAC}" "${GIADDR_A}")"
eval "$(parse_offer "$offer1")"
if [[ -z "${YIADDR:-}" ]]; then
  fail "no OFFER received"
elif cidr_contains "$YIADDR" "192.168.100.0/24"; then
  pass "yiaddr $YIADDR is in 192.168.100.0/24"
else
  fail "yiaddr $YIADDR is NOT in 192.168.100.0/24"
fi
DELL_IP="${YIADDR:-}"

# ---------------------------------------------------------------------------
echo ""
echo "==> 2. Relay DISCOVER — HPE MAC, pool 10.0.2.0/24"
offer2="$(dhcp_relay "${HPE_MAC}" "${GIADDR_B}")"
eval "$(parse_offer "$offer2")"
if [[ -z "${YIADDR:-}" ]]; then
  fail "no OFFER received"
elif cidr_contains "$YIADDR" "10.0.2.0/24"; then
  pass "yiaddr $YIADDR is in 10.0.2.0/24"
else
  fail "yiaddr $YIADDR is NOT in 10.0.2.0/24"
fi

# ---------------------------------------------------------------------------
echo ""
echo "==> 3. Static lease — Lenovo MAC should get 192.168.100.200"
offer3="$(dhcp_relay "${LENOVO_MAC}" "${GIADDR_A}")"
eval "$(parse_offer "$offer3")"
if [[ -z "${YIADDR:-}" ]]; then
  fail "no OFFER received"
elif [[ "$YIADDR" == "192.168.100.200" ]]; then
  pass "yiaddr $YIADDR matches static lease"
else
  fail "yiaddr $YIADDR — expected 192.168.100.200"
fi

# ---------------------------------------------------------------------------
echo ""
echo "==> 4. Re-lease — Dell MAC should get same IP as test 1"
offer4="$(dhcp_relay "${DELL_MAC}" "${GIADDR_A}")"
eval "$(parse_offer "$offer4")"
if [[ -z "${YIADDR:-}" ]]; then
  fail "no OFFER received on re-lease"
elif [[ "$YIADDR" == "$DELL_IP" ]]; then
  pass "yiaddr $YIADDR matches first allocation (idempotent)"
else
  fail "yiaddr $YIADDR — expected $DELL_IP on re-lease"
fi

# ---------------------------------------------------------------------------
echo ""
echo "==> 5. DHCP options — OFFER for Dell MAC must carry DNS, domain, NTP"
eval "$(parse_offer "$offer1")"
[[ -n "${DNS:-}" ]]    && pass "option 6  DNS=$DNS"       || fail "option 6 (DNS) missing"
[[ -n "${DOMAIN:-}" ]] && pass "option 15 domain=$DOMAIN" || fail "option 15 (domain) missing"
[[ -n "${NTP:-}" ]]    && pass "option 42 NTP=$NTP"       || fail "option 42 (NTP) missing"

# ---------------------------------------------------------------------------
echo ""
echo "==> 6. No-relay drop — giaddr=0.0.0.0 must be silently dropped"
nodrop="$(dhcp_relay "${DELL_MAC}" "0.0.0.0" 2>/dev/null || true)"
if [[ -z "$nodrop" ]]; then
  pass "no OFFER for direct request (relay required)"
else
  fail "unexpected OFFER received: $nodrop"
fi

# ---------------------------------------------------------------------------
echo ""
echo "==> 7. No-pool drop — giaddr not in any subnet → NoPoolFound event"
exec_debug python3 - <<'PYEOF'
from scapy.all import *
mac = '18:66:da:ff:ff:ff'
pkt = (Ether(src=mac, dst='ff:ff:ff:ff:ff:ff') /
       IP(src='0.0.0.0', dst='255.255.255.255') /
       UDP(sport=68, dport=67) /
       BOOTP(chaddr=mac2str(mac), giaddr='10.255.255.1') /
       DHCP(options=[('message-type', 'discover'), 'end']))
sendp(pkt, iface='veth0', verbose=False)
PYEOF
sleep 2
event_count=$(kubectl --context "${CONTEXT}" -n "${NS}" \
  get events --field-selector reason=NoPoolFound \
  --no-headers 2>/dev/null | wc -l | tr -d ' ')
if [[ "$event_count" -gt 0 ]]; then
  pass "NoPoolFound event emitted ($event_count event(s))"
else
  fail "NoPoolFound event not found"
fi

# ---------------------------------------------------------------------------
echo ""
echo "==> 8. TFTP — fetch snponly.efi from tftpd sidecar"
tftpd_count=$(kubectl --context "${CONTEXT}" -n "${NS}" get pod \
  -l app.kubernetes.io/name=metaldhcp \
  -o jsonpath='{.items[0].spec.containers[*].name}' 2>/dev/null \
  | tr ' ' '\n' | grep -c tftpd || true)
if [[ "$tftpd_count" == "0" ]]; then
  echo "    SKIP: tftpd container not present (set tftp.enabled=true in dev/values.yaml)"
else
  tftp_out="$(exec_debug python3 - <<'PYEOF'
import socket, struct, sys
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.settimeout(5)
s.sendto(b'\x00\x01snponly.efi\x00octet\x00', ('127.0.0.1', 69))
pkt, addr = s.recvfrom(65535)
opcode = struct.unpack('!H', pkt[:2])[0]
assert opcode == 3, f'expected DATA (3), got opcode {opcode}'
data = pkt[4:]
s.sendto(struct.pack('!HH', 4, struct.unpack('!H', pkt[2:4])[0]), addr)
assert len(data) > 0, 'empty file'
print(f'{len(data)} bytes')
PYEOF
  )"
  pass "TFTP snponly.efi received ($tftp_out)"
fi

# ---------------------------------------------------------------------------
echo ""
echo "==> DHCPLeases:"
kubectl --context "${CONTEXT}" get dhcpleases.dhcp.metal.ironcore.dev \
  --context "${CONTEXT}" 2>/dev/null || true

# ---------------------------------------------------------------------------
echo ""
echo "==> Results: ${PASS} passed, ${FAIL} failed"
[[ "$FAIL" -eq 0 ]]
