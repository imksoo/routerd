#!/usr/bin/env bash
set -euo pipefail

# routerd-arp-observer policy gate for #731. This script runs the real
# routerd-arp-observer daemon inside netns, pushes the SAM member sender MAC
# ignore set through the daemon socket command, and verifies that configured
# member MACs are ignored at the observation chokepoint.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"

if [[ "${EUID}" -ne 0 ]]; then
  log "SKIP: requires root/CAP_NET_ADMIN"
  exit 0
fi

require_common

OBSERVER="${TEST_ID}-observer"
OBSERVER_SCAN="${TEST_ID}-observer-scan"
MEMBER="${TEST_ID}-member"
CLIENT="${TEST_ID}-client"
BRIDGE="${TEST_IF_ID}br"
MOBILE_IP="10.95.0.50"
CLIENT_IP="10.95.0.51"

BIN="$WORKDIR/routerd-arp-observer"
(cd "$REPO_ROOT" && go build -o "$BIN" ./cmd/routerd-arp-observer)

create_ns "$OBSERVER"
create_ns "$OBSERVER_SCAN"
create_ns "$MEMBER"
create_ns "$CLIENT"
create_bridge_segment "$BRIDGE" \
  "$OBSERVER" eth0 10.95.0.2/24 \
  "$OBSERVER_SCAN" eth0 10.95.0.3/24 \
  "$MEMBER" eth0 10.95.0.10/24 \
  "$CLIENT" eth0 10.95.0.11/24

mac_of() {
  ip netns exec "$1" cat "/sys/class/net/$2/address"
}

MEMBER_MAC="$(mac_of "$MEMBER" eth0)"
CLIENT_MAC="$(mac_of "$CLIENT" eth0)"
PASSIVE_SOCKET="$WORKDIR/passive.sock"
PASSIVE_EVENTS="$WORKDIR/passive-events.jsonl"
SCAN_SOCKET="$WORKDIR/scan.sock"
SCAN_EVENTS="$WORKDIR/scan-events.jsonl"

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

cat >"$WORKDIR/status_unix.py" <<'PY'
import json
import socket
import sys

sock_path = sys.argv[1]
sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
sock.connect(sock_path)
sock.sendall(b"GET /v1/status HTTP/1.1\r\nHost: routerd-arp-observer\r\nConnection: close\r\n\r\n")
data = b""
while True:
    chunk = sock.recv(65536)
    if not chunk:
        break
    data += chunk
body = data.split(b"\r\n\r\n", 1)[1]
print(json.dumps(json.loads(body), sort_keys=True))
PY

cat >"$WORKDIR/set_ignored_sender_macs.py" <<'PY'
import json
import socket
import sys

sock_path, macs = sys.argv[1], sys.argv[2]
body = json.dumps({
    "apiVersion": "daemon.routerd.net/v1alpha1",
    "kind": "CommandRequest",
    "command": "set-ignored-sender-macs",
    "attributes": {"macAddresses": macs},
}).encode()
req = (
    b"POST /v1/commands HTTP/1.1\r\n"
    b"Host: routerd-arp-observer\r\n"
    b"Content-Type: application/json\r\n"
    b"Connection: close\r\n"
    b"Content-Length: " + str(len(body)).encode() + b"\r\n\r\n" +
    body
)
sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
sock.connect(sock_path)
sock.sendall(req)
data = b""
while True:
    chunk = sock.recv(65536)
    if not chunk:
        break
    data += chunk
body = data.split(b"\r\n\r\n", 1)[1]
result = json.loads(body)
if not result.get("accepted"):
    raise SystemExit(f"set-ignored-sender-macs rejected: {result}")
PY

jsonl_event_count_for_mac() {
  local file="$1" mac="$2"
  python3 - "$file" "$mac" <<'PY'
import json, sys
path, mac = sys.argv[1], sys.argv[2].lower()
count = 0
try:
    with open(path, encoding="utf-8") as f:
        for line in f:
            if not line.strip():
                continue
            event = json.loads(line)
            attrs = event.get("attributes") or {}
            if str(attrs.get("mac", "")).lower() == mac:
                count += 1
except FileNotFoundError:
    pass
print(count)
PY
}

start_observer() {
  local ns="$1" socket="$2" event_file="$3" source_type="$4"
  setsid ip netns exec "$ns" "$BIN" daemon \
    --resource "$source_type" \
    --interface eth0 \
    --event-interface eth0 \
    --socket "$socket" \
    --event-file "$event_file" \
    --pool svnet1 \
    --prefix 10.95.0.0/24 \
    --source-type "$source_type" \
    --observe \
    --scan-interval 200ms >/dev/null 2>"$WORKDIR/$source_type.err" &
  local pid=$!
  add_cleanup "kill -TERM -$pid"
  wait_for 5 test -S "$socket" || {
    cat "$WORKDIR/$source_type.err" >&2 || true
    fail "observer $source_type did not start"
  }
}

push_ignored_sender_macs() {
  local socket="$1"
  python3 "$WORKDIR/set_ignored_sender_macs.py" "$socket" "$MEMBER_MAC"
}

start_observer "$OBSERVER" "$PASSIVE_SOCKET" "$PASSIVE_EVENTS" "arp-observer"

ip netns exec "$CLIENT" python3 "$WORKDIR/send_garp.py" eth0 "$CLIENT_IP"
sleep 0.5
if [[ "$(jsonl_event_count_for_mac "$PASSIVE_EVENTS" "$CLIENT_MAC")" != "0" ]]; then
  fail "passive ARP path emitted ownership observation before initial ignore-set push opened the gate"
fi

push_ignored_sender_macs "$PASSIVE_SOCKET"
ip netns exec "$MEMBER" python3 "$WORKDIR/send_garp.py" eth0 "$MOBILE_IP"
ip netns exec "$CLIENT" python3 "$WORKDIR/send_garp.py" eth0 "$CLIENT_IP"
sleep 0.5

if [[ "$(jsonl_event_count_for_mac "$PASSIVE_EVENTS" "$MEMBER_MAC")" != "0" ]]; then
  fail "passive ARP path emitted ownership observation for ignored SAM member MAC"
fi
if [[ "$(jsonl_event_count_for_mac "$PASSIVE_EVENTS" "$CLIENT_MAC")" == "0" ]]; then
  fail "passive ARP path failed to emit ownership observation for real client MAC"
fi

python3 "$WORKDIR/status_unix.py" "$PASSIVE_SOCKET" >"$WORKDIR/passive-status.json"
python3 - "$WORKDIR/passive-status.json" "$MEMBER_MAC" <<'PY'
import json, sys
status = json.load(open(sys.argv[1], encoding="utf-8"))
observed = status.get("observed") or {}
mac = sys.argv[2].lower()
if observed.get("ignoredSenderMACs") != mac:
    raise SystemExit(f"ignoredSenderMACs={observed.get('ignoredSenderMACs')!r}, want {mac!r}")
if observed.get("ignoredSenderMACCount") != "1":
    raise SystemExit(f"ignoredSenderMACCount={observed.get('ignoredSenderMACCount')!r}, want '1'")
if observed.get("ignoredSenderMACsConfigured") != "true":
    raise SystemExit(f"ignoredSenderMACsConfigured={observed.get('ignoredSenderMACsConfigured')!r}, want 'true'")
PY

start_observer "$OBSERVER_SCAN" "$SCAN_SOCKET" "$SCAN_EVENTS" "pve-svnet"

ip -n "$OBSERVER_SCAN" neigh replace "$CLIENT_IP" lladdr "$CLIENT_MAC" dev eth0 nud reachable
sleep 0.8
if [[ "$(jsonl_event_count_for_mac "$SCAN_EVENTS" "$CLIENT_MAC")" != "0" ]]; then
  fail "ARP table scan path emitted ownership observation before initial ignore-set push opened the gate"
fi

push_ignored_sender_macs "$SCAN_SOCKET"
ip -n "$OBSERVER_SCAN" neigh replace "$MOBILE_IP" lladdr "$MEMBER_MAC" dev eth0 nud reachable
ip -n "$OBSERVER_SCAN" neigh replace "$CLIENT_IP" lladdr "$CLIENT_MAC" dev eth0 nud reachable
sleep 0.8

if [[ "$(jsonl_event_count_for_mac "$SCAN_EVENTS" "$MEMBER_MAC")" != "0" ]]; then
  fail "ARP table scan path emitted ownership observation for ignored SAM member MAC"
fi
if [[ "$(jsonl_event_count_for_mac "$SCAN_EVENTS" "$CLIENT_MAC")" == "0" ]]; then
  fail "ARP table scan path failed to emit ownership observation for real client MAC"
fi

log "arp-observer ignored SAM member MACs on passive packet and ARP table scan paths"
