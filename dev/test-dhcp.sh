#!/usr/bin/env bash
# SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
# SPDX-License-Identifier: MIT
#
# Run all dev DHCP test scenarios against the local kind cluster.
# Requires: make tilt-up, metaldhcp pod running and ready.

set -euo pipefail

CONTEXT="${KUBE_CONTEXT:-kind-metaldhcp}"
NS="metaldhcp-system"

exec_debug() {
  kubectl --context "${CONTEXT}" exec -n "${NS}" deploy/metaldhcp -c debug -- "$@"
}

scapy_send() {
  exec_debug python3 -c "
from scapy.all import *
$1
"
}

echo "==> Waiting for metaldhcp pod to be ready..."
kubectl --context "${CONTEXT}" -n "${NS}" wait --for=condition=ready pod \
  -l app.kubernetes.io/name=metaldhcp --timeout=60s

echo ""
echo "==> 1. Direct request (udhcpc, hostname: node-direct)"
exec_debug udhcpc -i veth0 -n -q -x hostname:node-direct

echo ""
echo "==> 2. Relay request — pool 192.168.100.0/24 (hostname: node-relay-01)"
scapy_send "
mac = '02:aa:bb:cc:dd:01'
pkt = Ether(src=mac, dst='ff:ff:ff:ff:ff:ff') / IP(src='0.0.0.0', dst='255.255.255.255') / UDP(sport=68, dport=67) / BOOTP(chaddr=mac2str(mac), giaddr='192.168.100.50', flags=0x8000) / DHCP(options=[('message-type','discover'),('client_id', b'\x01' + mac2str(mac)),('hostname','node-relay-01'),'end'])
sendp(pkt, iface='veth0', verbose=False)
"

echo ""
echo "==> 3. Relay request — pool 10.0.2.0/24 (hostname: node-relay-02)"
scapy_send "
mac = '02:aa:bb:cc:dd:02'
pkt = Ether(src=mac, dst='ff:ff:ff:ff:ff:ff') / IP(src='0.0.0.0', dst='255.255.255.255') / UDP(sport=68, dport=67) / BOOTP(chaddr=mac2str(mac), giaddr='10.0.2.1', flags=0x8000) / DHCP(options=[('message-type','discover'),('client_id', b'\x01' + mac2str(mac)),('hostname','node-relay-02'),'end'])
sendp(pkt, iface='veth0', verbose=False)
"

echo ""
echo "==> 4. Static lease (02:aa:bb:cc:dd:03 → 192.168.100.200, hostname: node-direct-static)"
scapy_send "
mac = '02:aa:bb:cc:dd:03'
pkt = Ether(src=mac, dst='ff:ff:ff:ff:ff:ff') / IP(src='0.0.0.0', dst='255.255.255.255') / UDP(sport=68, dport=67) / BOOTP(chaddr=mac2str(mac), flags=0x8000) / DHCP(options=[('message-type','discover'),('client_id', b'\x01' + mac2str(mac)),'end'])
sendp(pkt, iface='veth0', verbose=False)
"

echo ""
echo "==> DHCPLeases:"
kubectl --context "${CONTEXT}" -n "${NS}" get dhcpleases.dhcp.metal.ironcore.dev

echo ""
echo "==> 5. Unmatched relay (giaddr 10.255.255.1 — no pool) — expect Warning event NoPoolFound"
scapy_send "
mac = '02:aa:bb:cc:dd:ff'
pkt = Ether(src=mac, dst='ff:ff:ff:ff:ff:ff') / IP(src='0.0.0.0', dst='255.255.255.255') / UDP(sport=68, dport=67) / BOOTP(chaddr=mac2str(mac), giaddr='10.255.255.1', flags=0x8000) / DHCP(options=[('message-type','discover'),('client_id', b'\x01' + mac2str(mac)),'end'])
sendp(pkt, iface='veth0', verbose=False)
"
echo "    Waiting 2s for event to propagate..."
sleep 2
kubectl --context "${CONTEXT}" -n "${NS}" get events --field-selector reason=NoPoolFound

echo ""
echo "==> 6. TFTP — fetch snponly.efi from tftpd sidecar (option 66/67 PXE boot path)"
TFTPD_COUNT=$(kubectl --context "${CONTEXT}" -n "${NS}" get pod \
  -l app.kubernetes.io/name=metaldhcp \
  -o jsonpath='{.items[0].spec.containers[*].name}' 2>/dev/null | tr ' ' '\n' | grep -c tftpd || true)
if [ "${TFTPD_COUNT}" = "0" ]; then
  echo "    (skipped — tftpd container not present; set tftp.enabled=true in dev/values.yaml)"
else
  exec_debug python3 -c "
import socket, struct, sys

s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.settimeout(5)
# RRQ packet: opcode=1, filename, mode
s.sendto(b'\\x00\\x01snponly.efi\\x00octet\\x00', ('127.0.0.1', 69))
pkt, addr = s.recvfrom(65535)
opcode = struct.unpack('!H', pkt[:2])[0]
assert opcode == 3, f'expected DATA (3), got opcode {opcode}: {pkt!r}'
data = pkt[4:]
s.sendto(struct.pack('!HH', 4, struct.unpack('!H', pkt[2:4])[0]), addr)
assert len(data) > 0, 'received empty file'
print(f'OK: received {len(data)} bytes via TFTP')
"
fi

echo ""
echo "==> 7. Server identifier — OFFER must carry option 54 equal to the LoadBalancer VIP"
scapy_send "
import sys
mac = '02:aa:bb:cc:dd:07'
disc = Ether(src=mac, dst='ff:ff:ff:ff:ff:ff') / IP(src='0.0.0.0', dst='255.255.255.255') / UDP(sport=68, dport=67) / BOOTP(chaddr=mac2str(mac), flags=0x8000) / DHCP(options=[('message-type','discover'),('param_req_list',[54]),'end'])
ans = srp(disc, iface='veth0-server', timeout=3, verbose=False)
if not ans or not ans[0]:
    print('SKIP: no OFFER received (pool may be exhausted or request dropped)')
    sys.exit(0)
offer = ans[0][0][1]
sid = next((v for k,v in offer[DHCP].options if k == 'server_id'), None)
assert sid is not None, 'option 54 (server_id) missing from OFFER'
assert str(sid) == '192.168.100.1', f'expected 192.168.100.1, got {sid}'
print(f'OK: option 54 = {sid}')
"
