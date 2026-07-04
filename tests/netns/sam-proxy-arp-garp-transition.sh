#!/usr/bin/env bash
set -euo pipefail

# Scenario/mechanism check only: this script drives proxy-neighbor and GARP
# behavior itself inside netns. It does not verify routerd's SAM policy wiring.
# The policy gate is the Go test
# TestSAMControllerGARPPolicyCaptureSilentHolderTransitionOnly.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"

if [[ "${EUID}" -ne 0 ]]; then
  log "SKIP: requires root/CAP_NET_ADMIN"
  exit 0
fi

require_common

LEAF_A="${TEST_ID}-leaf-a"
LEAF_B="${TEST_ID}-leaf-b"
CLIENT_A="${TEST_ID}-client-a"
CLIENT_B="${TEST_ID}-client-b"
BRIDGE="${TEST_IF_ID}br"
MOBILE_IP="10.94.0.50"

create_ns "$LEAF_A"
create_ns "$LEAF_B"
create_ns "$CLIENT_A"
create_ns "$CLIENT_B"
create_bridge_segment "$BRIDGE" \
  "$LEAF_A" eth0 10.94.0.2/24 \
  "$LEAF_B" eth0 10.94.0.3/24 \
  "$CLIENT_A" eth0 10.94.0.10/24 \
  "$CLIENT_B" eth0 10.94.0.11/24

ip netns exec "$LEAF_A" sysctl -qw net.ipv4.conf.eth0.proxy_arp=1
ip netns exec "$LEAF_B" sysctl -qw net.ipv4.conf.eth0.proxy_arp=1

cat >"$WORKDIR/arp_watch.py" <<'PY'
import json
import socket
import struct
import sys
import time

iface, target, seconds = sys.argv[1], sys.argv[2], float(sys.argv[3])
target_bytes = socket.inet_aton(target)
sock = socket.socket(socket.AF_PACKET, socket.SOCK_RAW, socket.htons(0x0806))
sock.bind((iface, 0))
sock.settimeout(0.1)
deadline = time.time() + seconds
events = []
while time.time() < deadline:
    try:
        frame = sock.recv(2048)
    except TimeoutError:
        continue
    if len(frame) < 42 or frame[12:14] != b"\x08\x06":
        continue
    htype, ptype, hlen, plen, op = struct.unpack("!HHBBH", frame[14:22])
    if (htype, ptype, hlen, plen) != (1, 0x0800, 6, 4):
        continue
    sha = frame[22:28]
    spa = frame[28:32]
    tha = frame[32:38]
    tpa = frame[38:42]
    if spa == target_bytes and tpa == target_bytes:
        events.append({
            "op": op,
            "sha": ":".join(f"{b:02x}" for b in sha),
            "tha": ":".join(f"{b:02x}" for b in tha),
        })
print(json.dumps({"count": len(events), "events": events}, sort_keys=True))
PY

cat >"$WORKDIR/send_garp.py" <<'PY'
import socket
import struct
import sys

iface, ip = sys.argv[1], sys.argv[2]
with open(f"/sys/class/net/{iface}/address", encoding="utf-8") as f:
    mac = bytes.fromhex(f.read().strip().replace(":", ""))
addr = socket.inet_aton(ip)
frame = (
    b"\xff" * 6 +
    mac +
    struct.pack("!H", 0x0806) +
    struct.pack("!HHBBH", 1, 0x0800, 6, 4, 1) +
    mac +
    addr +
    b"\x00" * 6 +
    addr
)
sock = socket.socket(socket.AF_PACKET, socket.SOCK_RAW, socket.htons(0x0806))
sock.bind((iface, 0))
for _ in range(3):
    sock.send(frame)
PY

json_count() {
  python3 - "$1" <<'PY'
import json, sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["count"])
PY
}

mac_of() {
  ip netns exec "$1" cat "/sys/class/net/$2/address"
}

leaf_a_mac="$(mac_of "$LEAF_A" eth0)"
leaf_b_mac="$(mac_of "$LEAF_B" eth0)"

# Remote capture is represented by proxy-neighbor state only. It must not emit
# a gratuitous ARP, because that would make sibling leaves observe false local
# ownership for the remote address.
ip netns exec "$LEAF_B" python3 "$WORKDIR/arp_watch.py" eth0 "$MOBILE_IP" 1.5 >"$WORKDIR/capture-leaf-b.json" &
watch_leaf_pid=$!
ip netns exec "$CLIENT_B" python3 "$WORKDIR/arp_watch.py" eth0 "$MOBILE_IP" 1.5 >"$WORKDIR/capture-client-b.json" &
watch_client_pid=$!
sleep 0.2
ip -n "$LEAF_A" neigh replace proxy "$MOBILE_IP" dev eth0
wait "$watch_leaf_pid"
wait "$watch_client_pid"

if [[ "$(json_count "$WORKDIR/capture-leaf-b.json")" != "0" ]]; then
  fail "proxy-neighbor capture emitted GARP visible to sibling leaf: $(cat "$WORKDIR/capture-leaf-b.json")"
fi
if [[ "$(json_count "$WORKDIR/capture-client-b.json")" != "0" ]]; then
  fail "proxy-neighbor capture emitted GARP visible to sibling client: $(cat "$WORKDIR/capture-client-b.json")"
fi
if ip -n "$LEAF_B" neigh show to "$MOBILE_IP" | grep -q "$MOBILE_IP"; then
  fail "sibling leaf neighbor table was polluted during remote capture"
fi

# Holder transition is represented by the address becoming local to leaf-a's
# authority. Only this transition may refresh L2 caches with a gratuitous ARP.
ip -n "$CLIENT_A" neigh replace "$MOBILE_IP" lladdr "$leaf_b_mac" dev eth0 nud reachable
ip -n "$CLIENT_A" neigh show to "$MOBILE_IP" | grep -qi "$leaf_b_mac" || fail "failed to seed stale client neighbor"

ip netns exec "$CLIENT_A" python3 "$WORKDIR/arp_watch.py" eth0 "$MOBILE_IP" 2 >"$WORKDIR/holder-client-a.json" &
watch_holder_pid=$!
sleep 0.2
ip netns exec "$LEAF_A" python3 "$WORKDIR/send_garp.py" eth0 "$MOBILE_IP"
wait "$watch_holder_pid"

if [[ "$(json_count "$WORKDIR/holder-client-a.json")" == "0" ]]; then
  fail "holder transition did not emit a GARP visible to client"
fi
wait_for 3 bash -c "ip -n '$CLIENT_A' neigh show to '$MOBILE_IP' | grep -qi '$leaf_a_mac'" \
  || fail "client neighbor cache did not refresh to new holder MAC"

log "proxy-neighbor capture stayed silent; holder transition GARP refreshed client neighbor"
