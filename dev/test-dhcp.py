#!/usr/bin/env python3
# SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
# SPDX-License-Identifier: Apache-2.0
#
# Smoke-test metaldhcp running in the local kind cluster (make tilt-up).
# Sends relay-style DHCP DISCOVERs via scapy, captures OFFERs, and checks
# the response fields. Requires the metaldhcp pod to be ready.

import argparse
import fnmatch
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
vclass  = sys.argv[4] if len(sys.argv) > 4 else ""
ipxe    = sys.argv[5] if len(sys.argv) > 5 else ""

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
if vclass:
    opts.append(("vendor_class_id", vclass.encode()))
if ipxe:
    opts.append((175, b"\\x01"))  # iPXE encapsulated options — the phase-2 marker
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
if "tftp_server_name" in od:
    ts = od["tftp_server_name"]
    print(f"TFTP_SERVER={ts.decode() if isinstance(ts, bytes) else ts}")
if "boot-file-name" in od:
    bf = od["boot-file-name"]
    print(f"BOOTFILE={bf.decode() if isinstance(bf, bytes) else bf}")
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


def dhcp_relay(mac, giaddr, *, cid=None, vclass=None, ipxe=False):
    """Send a relay DHCP DISCOVER, return parsed OFFER fields or {}."""
    return parse_offer(exec_debug(
        _DISCOVER_CODE, mac, giaddr, cid or "", vclass or "", "1" if ipxe else ""))


def dhcp_request_relay(mac, giaddr):
    """Full DISCOVER→OFFER→REQUEST→ACK relay exchange, return ACK fields or {}."""
    return parse_offer(exec_debug(_REQUEST_CODE, mac, giaddr))


def cidr_contains(ip, cidr):
    return ipaddress.ip_address(ip) in ipaddress.ip_network(cidr, strict=False)


def wait_for_pod():
    print("==> Waiting for metaldhcp pod to be ready...")
    subprocess.run([
        "kubectl", "--context", CONTEXT, "-n", NS,
        "wait", "--for=condition=ready", "pod",
        "-l", "app.kubernetes.io/name=metaldhcp", "--timeout=60s",
    ], check=True)


def setup_routing():
    print("==> Setup: ensure veth0-server can route responses to both pool subnets")
    exec_debug(_SETUP_CODE, GIADDR_A, GIADDR_B)


def print_leases():
    print()
    print("==> DHCPLeases:")
    print(kubectl("-n", NS, "get", "dhcpleases.dhcp.metal.ironcore.dev").stdout or "(none)")


# ---------------------------------------------------------------------------
# Test scenarios — each is self-contained so it can run in isolation.
# ---------------------------------------------------------------------------

def t_pool_a():
    print("==> Relay DISCOVER — Dell MAC, pool 192.168.100.0/24")
    ip = dhcp_relay(DELL_MAC, GIADDR_A).get("YIADDR", "")
    if not ip:
        fail("no OFFER received")
    elif cidr_contains(ip, "192.168.100.0/24"):
        ok(f"yiaddr {ip} is in 192.168.100.0/24")
    else:
        fail(f"yiaddr {ip} is NOT in 192.168.100.0/24")


def t_pool_b():
    print("==> Relay DISCOVER — HPE MAC, pool 10.0.2.0/24")
    ip = dhcp_relay(HPE_MAC, GIADDR_B).get("YIADDR", "")
    if not ip:
        fail("no OFFER received")
    elif cidr_contains(ip, "10.0.2.0/24"):
        ok(f"yiaddr {ip} is in 10.0.2.0/24")
    else:
        fail(f"yiaddr {ip} is NOT in 10.0.2.0/24")


def t_static_mac():
    print("==> Static lease — Lenovo MAC should get 192.168.100.200")
    ip = dhcp_relay(LENOVO_MAC, GIADDR_A).get("YIADDR", "")
    if not ip:
        fail("no OFFER received")
    elif ip == "192.168.100.200":
        ok(f"yiaddr {ip} matches static lease")
    else:
        fail(f"yiaddr {ip} — expected 192.168.100.200")


def t_lease_idempotent():
    print("==> Re-lease — Dell MAC should get the same IP twice")
    first = dhcp_relay(DELL_MAC, GIADDR_A).get("YIADDR", "")
    if not first:
        fail("no OFFER received on first DISCOVER")
        return
    second = dhcp_relay(DELL_MAC, GIADDR_A).get("YIADDR", "")
    if not second:
        fail("no OFFER received on re-lease")
    elif second == first:
        ok(f"yiaddr {second} matches first allocation (idempotent)")
    else:
        fail(f"yiaddr {second} — expected {first} on re-lease")


def t_options():
    print("==> DHCP options — OFFER for Dell MAC must carry DNS, domain, NTP")
    r = dhcp_relay(DELL_MAC, GIADDR_A)
    if r.get("DNS"):
        ok(f"option 6  DNS={r['DNS']}")
    else:
        fail("option 6 (DNS) missing")
    if r.get("DOMAIN"):
        ok(f"option 15 domain={r['DOMAIN']}")
    else:
        fail("option 15 (domain) missing")
    if r.get("NTP"):
        ok(f"option 42 NTP={r['NTP']}")
    else:
        fail("option 42 (NTP) missing")


def t_relay_required():
    print("==> No-relay drop — giaddr=0.0.0.0 must be silently dropped")
    r = dhcp_relay(DELL_MAC, "0.0.0.0")
    if not r:
        ok("no OFFER for direct request (relay required)")
    else:
        fail(f"unexpected OFFER received: {r}")


def t_pool_nomatch():
    print("==> No-pool drop — giaddr not in any subnet → NoPoolFound event")
    exec_debug(_NO_POOL_CODE)
    time.sleep(2)
    ev = kubectl("-n", NS, "get", "events",
                 "--field-selector", "reason=NoPoolFound", "--no-headers")
    count = sum(1 for line in ev.stdout.splitlines() if line.strip())
    if count > 0:
        ok(f"NoPoolFound event emitted ({count} event(s))")
    else:
        fail("NoPoolFound event not found")


def t_lease_record():
    print("==> DHCPLease — REQUEST for each MAC must create a DHCPLease CR named after the IP")

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
        lease_name = ip.replace(".", "-")
        r = kubectl("-n", NS, "get", "dhcplease", lease_name, "-o", "jsonpath={.spec.ip}")
        lease_ip = r.stdout.strip()
        if lease_ip == ip:
            ok(f"{label}: DHCPLease {lease_name} .spec.ip={lease_ip}")
        else:
            fail(f"{label}: DHCPLease {lease_name} not found or ip mismatch"
                 f" (got '{lease_ip}', expected '{ip}')")

    check_lease("Dell",   DELL_MAC,   GIADDR_A)
    check_lease("HPE",    HPE_MAC,    GIADDR_B)
    check_lease("Lenovo", LENOVO_MAC, GIADDR_A, "192.168.100.200")


def t_tftp_fetch():
    print("==> TFTP — fetch snponly.efi from tftpd sidecar")
    r = kubectl("-n", NS, "get", "pod",
                "-l", "app.kubernetes.io/name=metaldhcp",
                "-o", "jsonpath={.items[0].spec.containers[*].name}")
    if "tftpd" not in r.stdout.split():
        print("    SKIP: tftpd container not present (set tftp.enabled=true in dev/values.yaml)")
    else:
        out = exec_debug(_TFTP_CODE).strip()
        ok(f"TFTP snponly.efi received ({out})")


def t_filter_admit():
    print("==> clientIdList admit — MAC not in macList, clientId in clientIdList → OFFER")
    ip = dhcp_relay(CID_MAC, GIADDR_A, cid=CID_FILTER_CLIENT).get("YIADDR", "")
    if not ip:
        fail("no OFFER — expected admit via clientIdList")
    elif cidr_contains(ip, "192.168.100.0/24"):
        ok(f"yiaddr {ip} — admitted via clientIdList (MAC not in macList)")
    else:
        fail(f"yiaddr {ip} is NOT in 192.168.100.0/24")


def t_static_cid():
    print("==> clientId static lease — clientId ff0102aabb should get 192.168.100.201")
    ip = dhcp_relay(CID_MAC, GIADDR_A, cid=CID_STATIC_CLIENT).get("YIADDR", "")
    if not ip:
        fail("no OFFER for clientId static lease")
    elif ip == "192.168.100.201":
        ok(f"yiaddr {ip} matches clientId static lease")
    else:
        fail(f"yiaddr {ip} — expected 192.168.100.201")


def t_filter_drop():
    print("==> clientId drop — MAC not in macList, clientId not in clientIdList → no OFFER")
    r = dhcp_relay(UNKNOWN_MAC, GIADDR_A, cid=UNKNOWN_CLIENT)
    if not r:
        ok("no OFFER for unknown MAC/clientId (allow mode, not in any list)")
    else:
        fail(f"unexpected OFFER received: {r}")


def t_tftp_x64():
    print("==> tftpFiles — option 60 PXEClient:Arch:00007 (x86-64) → option 67 snponly.efi")
    r = dhcp_relay(DELL_MAC, GIADDR_A, vclass="PXEClient:Arch:00007:UNDI:003000")
    bootfile = r.get("BOOTFILE", "")
    if not r:
        fail("no OFFER received for x86-64 arch DISCOVER")
    elif bootfile == "snponly.efi":
        ok(f"option 67 bootfile={bootfile} selected by x86-64 vendor class")
    else:
        fail(f"option 67 bootfile={bootfile!r} — expected snponly.efi")


def t_tftp_arm64():
    print("==> tftpFiles — option 60 PXEClient:Arch:00014 (ARM64) → option 67 ipxe-arm64.efi")
    r = dhcp_relay(DELL_MAC, GIADDR_A, vclass="PXEClient:Arch:00014:UNDI:003000")
    bootfile = r.get("BOOTFILE", "")
    if not r:
        fail("no OFFER received for ARM64 arch DISCOVER")
    elif bootfile == "ipxe-arm64.efi":
        ok(f"option 67 bootfile={bootfile} selected by ARM64 vendor class")
    else:
        fail(f"option 67 bootfile={bootfile!r} — expected ipxe-arm64.efi (fell through to fallback?)")


def t_tftp_fallback():
    print("==> tftpFiles — unmatched option 60 → option 67 fallback.efi")
    r = dhcp_relay(DELL_MAC, GIADDR_A, vclass="PXEClient:Arch:00099:UNDI:003000")
    bootfile = r.get("BOOTFILE", "")
    if not r:
        fail("no OFFER received for unmatched arch DISCOVER")
    elif bootfile == "fallback.efi":
        ok(f"option 67 bootfile={bootfile} selected via fallback entry")
    else:
        fail(f"option 67 bootfile={bootfile!r} — expected fallback.efi")


def t_ipxe_chain():
    print("==> iPXE phase 2 (option 175) → option 67 chain URL with ${uuid}")
    r = dhcp_relay(DELL_MAC, GIADDR_A, ipxe=True)
    bootfile = r.get("BOOTFILE", "")
    expected = "https://boot-operator.example.com/boot/ipxe/${uuid}"
    if not r:
        fail("no OFFER received for iPXE phase-2 DISCOVER")
    elif bootfile == expected:
        ok(f"option 67 bootfile={bootfile} (iPXE chain URL)")
    else:
        fail(f"option 67 bootfile={bootfile!r} — expected {expected}")


def t_ipxe_nobooturl():
    print("==> iPXE phase 2 without pool bootURL → no option 67")
    r = dhcp_relay(HPE_MAC, GIADDR_B, ipxe=True)  # pool 10.0.2.0/24 has no bootURL
    if not r:
        fail("no OFFER received for iPXE phase-2 DISCOVER (no bootURL)")
    elif r.get("BOOTFILE"):
        fail(f"expected no option 67, got bootfile={r['BOOTFILE']!r}")
    else:
        ok("no option 67 for iPXE phase without a pool bootURL")


def t_publish_static_leases():
    print("==> publishStaticLeases — DHCPLease CRs must exist at startup for static entries")

    def check_static_lease(label, lease_name, expected_mac="", expected_cid=""):
        r = kubectl("-n", NS, "get", "dhcplease", lease_name, "-o",
                    "jsonpath={.spec.macAddress},{.spec.clientID}", "--ignore-not-found")
        out = r.stdout.strip()
        if not out:
            fail(f"{label}: DHCPLease {lease_name} not found")
            return
        parts = out.split(",")
        got_mac = parts[0] if len(parts) > 0 else ""
        got_cid = parts[1] if len(parts) > 1 else ""
        if expected_mac and got_mac != expected_mac:
            fail(f"{label}: DHCPLease {lease_name} macAddress={got_mac!r} — expected {expected_mac!r}")
        elif expected_cid and got_cid != expected_cid:
            fail(f"{label}: DHCPLease {lease_name} clientID={got_cid!r} — expected {expected_cid!r}")
        else:
            ok(f"{label}: DHCPLease {lease_name} present (mac={got_mac!r} cid={got_cid!r})")

    check_static_lease("MAC static",      "192-168-100-200", expected_mac=LENOVO_MAC)
    check_static_lease("clientId static", "192-168-100-201", expected_cid=CID_STATIC_CLIENT)


# Ordered registry: name -> (description, function). "all" runs these in order.
# Names are grouped by feature (feature or feature-detail) so a partial token like
# "tftp", "pool", "static", "filter", "lease" or "ipxe" selects the whole group.
TESTS = {
    "pool-a":           ("Dell MAC allocates from pool 192.168.100.0/24", t_pool_a),
    "pool-b":           ("HPE MAC allocates from pool 10.0.2.0/24", t_pool_b),
    "pool-nomatch":     ("Unknown subnet emits a NoPoolFound event", t_pool_nomatch),
    "relay-required":   ("Direct request (giaddr=0) is dropped", t_relay_required),
    "filter-admit":     ("clientIdList admits a MAC not in macList", t_filter_admit),
    "filter-drop":      ("Unknown MAC and clientId get no OFFER", t_filter_drop),
    "static-mac":       ("MAC static lease returns its IP", t_static_mac),
    "static-cid":       ("clientId static lease returns its IP", t_static_cid),
    "lease-idempotent": ("Dell MAC re-lease returns the same IP", t_lease_idempotent),
    "lease-record":     ("REQUEST creates a DHCPLease CR per MAC", t_lease_record),
    "publish-static":   ("publishStaticLeases creates CRs at startup without DHCP traffic", t_publish_static_leases),
    "options":          ("OFFER carries DNS, domain, NTP options", t_options),
    "tftp-fetch":       ("tftpd sidecar serves snponly.efi", t_tftp_fetch),
    "tftp-x64":         ("Vendor class Arch:00007 selects snponly.efi", t_tftp_x64),
    "tftp-arm64":       ("Vendor class Arch:00014 selects ipxe-arm64.efi", t_tftp_arm64),
    "tftp-fallback":    ("Unmatched vendor class selects fallback.efi", t_tftp_fallback),
    "ipxe-chain":       ("iPXE phase 2 (opt 175) sets the ${uuid} chain URL", t_ipxe_chain),
    "ipxe-nobooturl":   ("iPXE phase 2 with no pool bootURL sets no option 67", t_ipxe_nobooturl),
}


def main():
    parser = argparse.ArgumentParser(
        description="Smoke-test metaldhcp running in the local kind cluster.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=(
            "test selection (each TEST arg is resolved independently):\n"
            "  exact      an exact test name runs just that test     (options)\n"
            "  partial    a substring selects every matching test     (tftp -> tftp-fetch,\n"
            "             i.e. a whole feature group                   tftp-x64, tftp-arm64, ...)\n"
            "  glob       shell-style wildcards * ? []                (tftp-* , pool-?)\n"
            "\n"
            "examples:\n"
            "  ./dev/test-dhcp.py                  # run all tests\n"
            "  ./dev/test-dhcp.py --list           # list test names\n"
            "  ./dev/test-dhcp.py tftp             # run the whole tftp group\n"
            "  ./dev/test-dhcp.py tftp-x64 static  # one test plus the static group\n"
            "  ./dev/test-dhcp.py 'pool-*'         # glob (quote to protect from the shell)\n"
        ),
    )
    parser.add_argument(
        "tests", nargs="*", metavar="TEST",
        help="test(s) to run: exact name, partial/group token, or glob "
             "(see 'test selection' below); omit to run all",
    )
    parser.add_argument(
        "-l", "--list", action="store_true",
        help="list available tests and exit",
    )
    parser.add_argument(
        "--no-setup", action="store_true",
        help="skip pod-ready wait and veth routing setup",
    )
    args = parser.parse_args()

    if args.list:
        print("Available tests:")
        for name, (desc, _) in TESTS.items():
            print(f"  {name:<14} {desc}")
        print()
        print("Select by: exact name (options), partial/group token (tftp), "
              "or glob ('tftp-*').")
        print("Omit all args to run every test. See --help for examples.")
        sys.exit(0)

    if args.tests:
        selected = []
        for pattern in args.tests:
            if pattern in TESTS:                              # exact name wins
                matches = [pattern]
            elif any(c in pattern for c in "*?["):            # explicit glob
                matches = fnmatch.filter(TESTS, pattern)
            else:                                             # partial / substring
                matches = [n for n in TESTS if pattern in n]
            if not matches:
                parser.error(f"no test matches {pattern!r} (run --list to see options)")
            for name in matches:  # keep TESTS order within a pattern, de-dup across patterns
                if name not in selected:
                    selected.append(name)
    else:
        selected = list(TESTS)

    if not args.no_setup:
        wait_for_pod()
        print()
        setup_routing()

    for name in selected:
        desc, fn = TESTS[name]
        print()
        print(f"==> [{name}]")
        fn()

    print_leases()
    print()
    print(f"==> Results: {_passed} passed, {_failed} failed")
    sys.exit(0 if _failed == 0 else 1)


if __name__ == "__main__":
    main()
