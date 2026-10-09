#!/usr/bin/env bash
# SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
# SPDX-License-Identifier: MIT
#
# Debug the PXE boot-URL path by sending scapy packets and checking metaldhcp logs.
#
# Usage:
#   ./dev/test-booturl.sh
#
# Requires: make tilt-up, metaldhcp pod with debug sidecar running.

set -euo pipefail

CONTEXT="${KUBE_CONTEXT:-kind-metaldhcp}"
NS="metaldhcp-system"
IFACE="veth0"
MAC="18:66:da:00:11:bb"    # Dell OUI — distinct from other test scenarios

exec_debug() {
  kubectl --context "${CONTEXT}" exec -n "${NS}" deploy/metaldhcp -c debug -- "$@"
}

send_pkt() {
  exec_debug python3 -c "
from scapy.all import *
$1
"
}

log_since() {
  # $1 = seconds ago; print metaldhcp container logs since that timestamp
  kubectl --context "${CONTEXT}" logs -n "${NS}" \
    "$(kubectl --context "${CONTEXT}" get pod -n "${NS}" -l app.kubernetes.io/name=metaldhcp -o name | head -1)" \
    -c metaldhcp --since="${1}s"
}

check_log() {
  local desc="$1" pattern="$2"
  local out
  out=$(log_since 4)
  if echo "${out}" | grep -qF "${pattern}"; then
    echo "  OK: found ${pattern}"
  else
    echo "  FAIL: pattern not found: ${pattern}"
    echo "  Recent logs:"
    echo "${out}" | tail -10 | sed 's/^/    /'
    exit 1
  fi
}

echo "==> Waiting for metaldhcp pod to be ready..."
kubectl --context "${CONTEXT}" -n "${NS}" wait --for=condition=ready pod \
  -l app.kubernetes.io/name=metaldhcp --timeout=60s

# ---------------------------------------------------------------------------
echo ""
echo "==> 1. Phase 1 PXEClient — expect log: bootURL= and nbp sets snponly.efi"
send_pkt "
mac = '${MAC}'
sendp(
    Ether(src=mac, dst='ff:ff:ff:ff:ff:ff') /
    IP(src='0.0.0.0', dst='255.255.255.255') /
    UDP(sport=68, dport=67) /
    BOOTP(chaddr=mac2str(mac), flags=0x8000) /
    DHCP(options=[
        ('message-type','discover'),
        ('client_id', b'\x01' + mac2str(mac)),
        ('vendor_class_id', b'PXEClient'),
        'end',
    ]),
    iface='${IFACE}', verbose=False
)
"
sleep 1
check_log "phase 1" "class=PXEClient"
check_log "phase 1 nbp" "snponly.efi"

# ---------------------------------------------------------------------------
echo ""
echo "==> 2. Phase 2 iPXE (option 175) — expect log: chainURL with \${uuid} template"
send_pkt "
mac = '${MAC}'
# option 175: iPXE encapsulated options — the reliable iPXE phase marker
sendp(
    Ether(src=mac, dst='ff:ff:ff:ff:ff:ff') /
    IP(src='0.0.0.0', dst='255.255.255.255') /
    UDP(sport=68, dport=67) /
    BOOTP(chaddr=mac2str(mac), flags=0x8000) /
    DHCP(options=[
        ('message-type','discover'),
        ('client_id', b'\x01' + mac2str(mac)),
        ('vendor_class_id', b'PXEClient:Arch:00007:UNDI:003010'),
        (175, b'\x01'),
        'end',
    ]),
    iface='${IFACE}', verbose=False
)
"
sleep 1
check_log "phase 2 class" "UNDI:003010"
check_log "phase 2 chain url" 'chainURL="https://boot-operator.example.com/ipxe/${uuid}"'

# ---------------------------------------------------------------------------
echo ""
echo "==> 3. Phase 2 iPXE without bootURL — expect log: iPXE phase, no bootURL"
send_pkt "
mac = '${MAC}'
sendp(
    Ether(src=mac, dst='ff:ff:ff:ff:ff:ff') /
    IP(src='0.0.0.0', dst='255.255.255.255') /
    UDP(sport=68, dport=67) /
    BOOTP(chaddr=mac2str(mac), flags=0x8000) /
    DHCP(options=[
        ('message-type','discover'),
        ('client_id', b'\x01' + mac2str(mac)),
        ('vendor_class_id', b'PXEClient:Arch:00007:UNDI:003010'),
        (175, b'\x01'),
        'end',
    ]),
    iface='${IFACE}', verbose=False
)
"
sleep 1
check_log "phase 2 no booturl" "iPXE phase, no bootURL"

echo ""
echo "==> All boot-URL scenarios passed."
