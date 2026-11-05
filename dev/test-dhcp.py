#!/usr/bin/env python3
# SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
# SPDX-License-Identifier: Apache-2.0
#
# Smoke-test metaldhcp running in the local kind cluster (make tilt-up).
# Sends relay-style DHCP DISCOVERs via scapy, captures OFFERs, and checks
# the response fields. Requires the metaldhcp pod to be ready.

import ipaddress
import os
import subprocess
import sys
import time

CONTEXT = os.environ.get("KUBE_CONTEXT", "kind-metaldhcp")
NS = "metaldhcp-system"

DELL_MAC   = "18:66:da:00:11:01"   # Dell
HPE_MAC    = "04:09:73:00:11:02"   # Hewlett Packard Enterprise
LENOVO_MAC = "10:c5:95:00:11:03"   # Lenovo — MAC static lease → 192.168.100.200

CID_MAC           = "de:ad:be:ef:00:01"  # MAC not in any macList
CID_STATIC_CLIENT = "ff0102aabb"          # clientId static lease → 192.168.100.201
CID_FILTER_CLIENT = "cc0304ddee"          # in clientIdList, no static lease → dynamic IP
UNKNOWN_MAC       = "de:ad:be:ef:ff:ff"  # not in any macList
UNKNOWN_CLIENT    = "aabbccddeeff"        # not in clientIdList

GIADDR_A = "192.168.100.50"  # relay IP for pool 192.168.100.0/24
GIADDR_B = "10.0.2.50"       # relay IP for pool 10.0.2.0/24

_passed = 0
_failed = 0


def ok(msg):
    global _passed
    print(f"    PASS: {msg}")
    _passed += 1


def fail(msg):
    global _failed
    print(f"    FAIL: {msg}", file=sys.stderr)
    _failed += 1


def kubectl(*args):
    return subprocess.run(
        ["kubectl", "--context", CONTEXT, *args],
        capture_output=True, text=True,
    )


def exec_debug(code, *args):
    """Run code as python3 inside the debug container with args as sys.argv[1:]."""
    r = subprocess.run(
        ["kubectl", "--context", CONTEXT,
         "exec", "-i", "-n", NS, "deploy/metaldhcp", "-c", "debug",
         "--", "python3", "-", *args],
        input=code, capture_output=True, text=True,
    )
    return r.stdout


# ---------------------------------------------------------------------------
# Python code templates executed inside the container (scapy lives there)
# ---------------------------------------------------------------------------

_DISCOVER_CODE = """\
import sys, threading, time
from scapy.all import *

mac_str = sys.argv[1]
giaddr  = sys.argv[2]
cid_hex = sys.argv[3] if len(sys.argv) > 3 else ""

offers  = []
started = threading.Event()

def capture():
    started.set()
    pkts = sniff(iface="veth0-server",
                 filter="udp and src port 67 and dst port 67",
                 count=1, timeout=4)
    offers.extend(pkts)

t = threading.Thread(target=capture, daemon=True)
t.start()
started.wait()
time.sleep(0.05)

opts = [("message-type", "discover")]
if cid_hex:
    opts.append((61, bytes.fromhex(cid_hex)))
opts.append("end")

pkt = (Ether(src=mac_str, dst="ff:ff:ff:ff:ff:ff") /
       IP(src="0.0.0.0", dst="255.255.255.255") /
       UDP(sport=68, dport=67) /
       BOOTP(chaddr=mac2str(mac_str), giaddr=giaddr) /
       DHCP(options=opts))
sendp(pkt, iface="veth0", verbose=False)

t.join(timeout=5)
if not offers or BOOTP not in offers[0]:
    sys.exit(0)

o  = offers[0]
bp = o[BOOTP]
print(f"YIADDR={bp.yiaddr}")
od = {}
for opt in o[DHCP].options:
    if isinstance(opt, (list, tuple)) and len(opt) >= 2 and isinstance(opt[0], str) and opt[0] != "end":
        od[opt[0]] = list(opt[1:]) if len(opt) > 2 else opt[1]
if "router" in od:
    print(f"ROUTER={od['router']}")
if "name_server" in od:
    ns = od["name_server"]
    print(f"DNS={','.join(str(x) for x in (ns if isinstance(ns, list) else [ns]))}")
if "domain" in od:
    d = od["domain"]
    print(f"DOMAIN={d.decode() if isinstance(d, bytes) else d}")
if "NTP_server" in od:
    ntp = od["NTP_server"]
    print(f"NTP={','.join(str(x) for x in (ntp if isinstance(ntp, list) else [ntp]))}")
if "server_id" in od:
    print(f"SERVER_ID={od['server_id']}")
if "lease_time" in od:
    print(f"LEASE_TIME={od['lease_time']}")
"""

_REQUEST_CODE = """\
import sys, threading, time
from scapy.all import *

mac_str = sys.argv[1]
giaddr  = sys.argv[2]

def send_recv(opts):
    pkts    = []
    started = threading.Event()
    done    = threading.Event()

    def capture():
        started.set()
        p = sniff(iface="veth0-server",
                  filter="udp and src port 67 and dst port 67",
                  count=1, timeout=4)
        pkts.extend(p)
        done.set()

    t = threading.Thread(target=capture, daemon=True)
    t.start()
    started.wait()
    time.sleep(0.05)

    pkt = (Ether(src=mac_str, dst="ff:ff:ff:ff:ff:ff") /
           IP(src="0.0.0.0", dst="255.255.255.255") /
           UDP(sport=68, dport=67) /
           BOOTP(chaddr=mac2str(mac_str), giaddr=giaddr) /
           DHCP(options=opts))
    sendp(pkt, iface="veth0", verbose=False)
    done.wait(timeout=5)
    return pkts[0] if pkts else None

offer = send_recv([("message-type", "discover"), "end"])
if not offer or BOOTP not in offer:
    sys.exit(0)

offered_ip = offer[BOOTP].yiaddr
oo = {}
for opt in offer[DHCP].options:
    if isinstance(opt, (list, tuple)) and len(opt) >= 2:
        oo[opt[0]] = opt[1]
server_id = oo.get("server_id", "0.0.0.0")

ack = send_recv([("message-type", "request"),
                 ("server_id", server_id),
                 ("requested_addr", offered_ip),
                 "end"])
if not ack or BOOTP not in ack:
    sys.exit(0)

bp = ack[BOOTP]
print(f"YIADDR={bp.yiaddr}")
ad = {}
for opt in ack[DHCP].options:
    if isinstance(opt, (list, tuple)) and len(opt) >= 2 and isinstance(opt[0], str) and opt[0] != "end":
        ad[opt[0]] = list(opt[1:]) if len(opt) > 2 else opt[1]
if "router" in ad:
    print(f"ROUTER={ad['router']}")
if "server_id" in ad:
    print(f"SERVER_ID={ad['server_id']}")
if "lease_time" in ad:
    print(f"LEASE_TIME={ad['lease_time']}")
"""

_SETUP_CODE = """\
import subprocess, sys
giaddr_a, giaddr_b = sys.argv[1], sys.argv[2]
r = subprocess.run(["ip", "addr", "show", "veth0-server"], capture_output=True, text=True)
if "10.0.2." not in r.stdout:
    subprocess.run(["ip", "addr", "add", "10.0.2.1/24", "dev", "veth0-server"])
veth0_mac = open("/sys/class/net/veth0/address").read().strip()
for giaddr in [giaddr_a, giaddr_b]:
    subprocess.run(["ip", "neigh", "replace", giaddr, "lladdr", veth0_mac,
                    "dev", "veth0-server", "nud", "permanent"])
"""

_NO_POOL_CODE = """\
from scapy.all import *
mac = "18:66:da:ff:ff:ff"
pkt = (Ether(src=mac, dst="ff:ff:ff:ff:ff:ff") /
       IP(src="0.0.0.0", dst="255.255.255.255") /
       UDP(sport=68, dport=67) /
       BOOTP(chaddr=mac2str(mac), giaddr="10.255.255.1") /
       DHCP(options=[("message-type", "discover"), "end"]))
sendp(pkt, iface="veth0", verbose=False)
"""

_TFTP_CODE = """\
import socket, struct
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.settimeout(5)
s.sendto(b"\\x00\\x01snponly.efi\\x00octet\\x00", ("127.0.0.1", 69))
pkt, addr = s.recvfrom(65535)
opcode = struct.unpack("!H", pkt[:2])[0]
assert opcode == 3, f"expected DATA (3), got opcode {opcode}"
data = pkt[4:]
s.sendto(struct.pack("!HH", 4, struct.unpack("!H", pkt[2:4])[0]), addr)
assert len(data) > 0, "empty file"
print(f"{len(data)} bytes")
"""


def parse_offer(output):
    result = {}
    for line in output.splitlines():
        if "=" in line:
            k, _, v = line.partition("=")
            result[k.strip()] = v.strip()
    return result


def dhcp_relay(mac, giaddr, *, cid=None):
    """Send a relay DHCP DISCOVER, return parsed OFFER fields or {}."""
    args = [mac, giaddr]
    if cid:
        args.append(cid)
    return parse_offer(exec_debug(_DISCOVER_CODE, *args))


def dhcp_request_relay(mac, giaddr):
    """Full DISCOVER→OFFER→REQUEST→ACK relay exchange, return ACK fields or {}."""
    return parse_offer(exec_debug(_REQUEST_CODE, mac, giaddr))


def cidr_contains(ip, cidr):
    return ipaddress.ip_address(ip) in ipaddress.ip_network(cidr, strict=False)


def main():
    print("==> Waiting for metaldhcp pod to be ready...")
    subprocess.run([
        "kubectl", "--context", CONTEXT, "-n", NS,
        "wait", "--for=condition=ready", "pod",
        "-l", "app.kubernetes.io/name=metaldhcp", "--timeout=60s",
    ], check=True)

    print()
    print("==> Setup: ensure veth0-server can route responses to both pool subnets")
    exec_debug(_SETUP_CODE, GIADDR_A, GIADDR_B)

    # 1. Dell MAC → pool A
    print()
    print("==> 1. Relay DISCOVER — Dell MAC, pool 192.168.100.0/24")
    r1 = dhcp_relay(DELL_MAC, GIADDR_A)
    dell_ip = r1.get("YIADDR", "")
    if not dell_ip:
        fail("no OFFER received")
    elif cidr_contains(dell_ip, "192.168.100.0/24"):
        ok(f"yiaddr {dell_ip} is in 192.168.100.0/24")
    else:
        fail(f"yiaddr {dell_ip} is NOT in 192.168.100.0/24")

    # 2. HPE MAC → pool B
    print()
    print("==> 2. Relay DISCOVER — HPE MAC, pool 10.0.2.0/24")
    r2 = dhcp_relay(HPE_MAC, GIADDR_B)
    ip2 = r2.get("YIADDR", "")
    if not ip2:
        fail("no OFFER received")
    elif cidr_contains(ip2, "10.0.2.0/24"):
        ok(f"yiaddr {ip2} is in 10.0.2.0/24")
    else:
        fail(f"yiaddr {ip2} is NOT in 10.0.2.0/24")

    # 3. Lenovo static lease
    print()
    print("==> 3. Static lease — Lenovo MAC should get 192.168.100.200")
    r3 = dhcp_relay(LENOVO_MAC, GIADDR_A)
    ip3 = r3.get("YIADDR", "")
    if not ip3:
        fail("no OFFER received")
    elif ip3 == "192.168.100.200":
        ok(f"yiaddr {ip3} matches static lease")
    else:
        fail(f"yiaddr {ip3} — expected 192.168.100.200")

    # 4. Re-lease idempotency
    print()
    print("==> 4. Re-lease — Dell MAC should get same IP as test 1")
    r4 = dhcp_relay(DELL_MAC, GIADDR_A)
    ip4 = r4.get("YIADDR", "")
    if not ip4:
        fail("no OFFER received on re-lease")
    elif ip4 == dell_ip:
        ok(f"yiaddr {ip4} matches first allocation (idempotent)")
    else:
        fail(f"yiaddr {ip4} — expected {dell_ip} on re-lease")

    # 5. DHCP options
    print()
    print("==> 5. DHCP options — OFFER for Dell MAC must carry DNS, domain, NTP")
    if r1.get("DNS"):
        ok(f"option 6  DNS={r1['DNS']}")
    else:
        fail("option 6 (DNS) missing")
    if r1.get("DOMAIN"):
        ok(f"option 15 domain={r1['DOMAIN']}")
    else:
        fail("option 15 (domain) missing")
    if r1.get("NTP"):
        ok(f"option 42 NTP={r1['NTP']}")
    else:
        fail("option 42 (NTP) missing")

    # 6. No-relay drop
    print()
    print("==> 6. No-relay drop — giaddr=0.0.0.0 must be silently dropped")
    r6 = dhcp_relay(DELL_MAC, "0.0.0.0")
    if not r6:
        ok("no OFFER for direct request (relay required)")
    else:
        fail(f"unexpected OFFER received: {r6}")

    # 7. No-pool event
    print()
    print("==> 7. No-pool drop — giaddr not in any subnet → NoPoolFound event")
    exec_debug(_NO_POOL_CODE)
    time.sleep(2)
    ev = kubectl("-n", NS, "get", "events",
                 "--field-selector", "reason=NoPoolFound", "--no-headers")
    count = sum(1 for line in ev.stdout.splitlines() if line.strip())
    if count > 0:
        ok(f"NoPoolFound event emitted ({count} event(s))")
    else:
        fail("NoPoolFound event not found")

    # 8. DHCPLease creation
    print()
    print("==> 8. DHCPLease — REQUEST for each MAC must create a DHCPLease CR")

    def check_lease(label, mac, giaddr, expected_ip=None):
        ack = dhcp_request_relay(mac, giaddr)
        ip = ack.get("YIADDR", "")
        if not ip:
            fail(f"{label}: no ACK received for REQUEST")
            return
        if expected_ip and ip != expected_ip:
            fail(f"{label}: ACK yiaddr={ip} — expected {expected_ip}")
            return
        ok(f"{label}: ACK yiaddr={ip}")
        lease_name = mac.replace(":", "")
        r = kubectl("get", "dhcplease", lease_name, "-o", "jsonpath={.spec.ip}")
        lease_ip = r.stdout.strip()
        if lease_ip == ip:
            ok(f"{label}: DHCPLease {lease_name} .spec.ip={lease_ip}")
        else:
            fail(f"{label}: DHCPLease {lease_name} not found or ip mismatch"
                 f" (got '{lease_ip}', expected '{ip}')")

    check_lease("Dell",   DELL_MAC,   GIADDR_A)
    check_lease("HPE",    HPE_MAC,    GIADDR_B)
    check_lease("Lenovo", LENOVO_MAC, GIADDR_A, "192.168.100.200")

    # 9. TFTP
    print()
    print("==> 9. TFTP — fetch snponly.efi from tftpd sidecar")
    r = kubectl("-n", NS, "get", "pod",
                "-l", "app.kubernetes.io/name=metaldhcp",
                "-o", "jsonpath={.items[0].spec.containers[*].name}")
    if "tftpd" not in r.stdout.split():
        print("    SKIP: tftpd container not present (set tftp.enabled=true in dev/values.yaml)")
    else:
        out = exec_debug(_TFTP_CODE).strip()
        ok(f"TFTP snponly.efi received ({out})")

    print()
    print("==> DHCPLeases:")
    print(kubectl("-n", NS, "get", "dhcpleases.dhcp.metal.ironcore.dev").stdout or "(none)")

    # 10. clientIdList admit
    print()
    print("==> 10. clientIdList admit — MAC not in macList, clientId in clientIdList → OFFER")
    r10 = dhcp_relay(CID_MAC, GIADDR_A, cid=CID_FILTER_CLIENT)
    ip10 = r10.get("YIADDR", "")
    if not ip10:
        fail("no OFFER — expected admit via clientIdList")
    elif cidr_contains(ip10, "192.168.100.0/24"):
        ok(f"yiaddr {ip10} — admitted via clientIdList (MAC not in macList)")
    else:
        fail(f"yiaddr {ip10} is NOT in 192.168.100.0/24")

    # 11. clientId static lease
    print()
    print("==> 11. clientId static lease — clientId ff0102aabb should get 192.168.100.201")
    r11 = dhcp_relay(CID_MAC, GIADDR_A, cid=CID_STATIC_CLIENT)
    ip11 = r11.get("YIADDR", "")
    if not ip11:
        fail("no OFFER for clientId static lease")
    elif ip11 == "192.168.100.201":
        ok(f"yiaddr {ip11} matches clientId static lease")
    else:
        fail(f"yiaddr {ip11} — expected 192.168.100.201")

    # 12. clientId drop
    print()
    print("==> 12. clientId drop — MAC not in macList, clientId not in clientIdList → no OFFER")
    r12 = dhcp_relay(UNKNOWN_MAC, GIADDR_A, cid=UNKNOWN_CLIENT)
    if not r12:
        ok("no OFFER for unknown MAC/clientId (allow mode, not in any list)")
    else:
        fail(f"unexpected OFFER received: {r12}")

    print()
    print("==> DHCPLeases:")
    print(kubectl("-n", NS, "get", "dhcpleases.dhcp.metal.ironcore.dev").stdout or "(none)")
    print()
    print(f"==> Results: {_passed} passed, {_failed} failed")
    sys.exit(0 if _failed == 0 else 1)


if __name__ == "__main__":
    main()
