#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"

if [[ "${EUID}" -ne 0 ]]; then
  log "SKIP: requires root/CAP_NET_ADMIN"
  exit 0
fi

require_common

OLD_NS="${TEST_ID}-old"
CLIENT_NS="${TEST_ID}-client"
BR="br-${TEST_IF_ID}"
NEW_IF="new-${TEST_IF_ID}"
NEW_PEER="newp-${TEST_IF_ID}"
OLD_IF="old0"
CLIENT_IF="client0"
MOBILE="192.0.2.99"

create_ns "$OLD_NS"
create_ns "$CLIENT_NS"
create_bridge_segment "$BR" \
  "$OLD_NS" "$OLD_IF" "192.0.2.10/24" \
  "$CLIENT_NS" "$CLIENT_IF" "192.0.2.20/24"

ip link add "$NEW_IF" type veth peer name "$NEW_PEER"
add_cleanup "ip link delete '$NEW_IF'"
ip link set "$NEW_PEER" master "$BR"
ip link set "$NEW_PEER" up
ip link set "$NEW_IF" up
ip addr add 192.0.2.30/24 dev "$NEW_IF"
if ip -o addr show dev "$NEW_IF" | grep -q "$MOBILE"; then
  fail "test setup assigned mobile address $MOBILE to $NEW_IF"
fi

OLD_MAC="$(ip -n "$OLD_NS" -o link show "$OLD_IF" | awk '{for (i=1;i<=NF;i++) if ($i=="link/ether") {print $(i+1); exit}}')"
NEW_MAC="$(ip -o link show "$NEW_IF" | awk '{for (i=1;i<=NF;i++) if ($i=="link/ether") {print $(i+1); exit}}')"

ip -n "$CLIENT_NS" neigh replace "$MOBILE" lladdr "$OLD_MAC" dev "$CLIENT_IF" nud stale
got="$(ip -n "$CLIENT_NS" neigh show "$MOBILE" dev "$CLIENT_IF")"
[[ "$got" == *"$OLD_MAC"* ]] || fail "client neighbor was not seeded with old holder MAC: $got"

python3 - "$NEW_IF" "$MOBILE" <<'PY'
import socket
import struct
import sys
import time

ifname = sys.argv[1]
mobile = socket.inet_aton(sys.argv[2])
mac_text = open(f"/sys/class/net/{ifname}/address", encoding="utf-8").read().strip()
src = bytes.fromhex(mac_text.replace(":", ""))
frame = (
    b"\xff" * 6 +
    src +
    struct.pack("!H", 0x0806) +
    struct.pack("!HHBBH", 1, 0x0800, 6, 4, 1) +
    src +
    mobile +
    b"\x00" * 6 +
    mobile
)
sock = socket.socket(socket.AF_PACKET, socket.SOCK_RAW, socket.htons(0x0806))
sock.bind((ifname, 0))
try:
    for _ in range(3):
        sock.send(frame)
        time.sleep(0.1)
finally:
    sock.close()
PY
wait_for 5 bash -c "ip -n '$CLIENT_NS' neigh show '$MOBILE' dev '$CLIENT_IF' | grep -qi '$NEW_MAC'"

ip -n "$OLD_NS" neigh add proxy "$MOBILE" dev "$OLD_IF"
ip -n "$OLD_NS" neigh show proxy "$MOBILE" dev "$OLD_IF" | grep -q "$MOBILE"
ip -n "$OLD_NS" neigh del proxy "$MOBILE" dev "$OLD_IF"
if ip -n "$OLD_NS" neigh show proxy "$MOBILE" dev "$OLD_IF" | grep -q "$MOBILE"; then
  fail "proxy neighbor remained after holder loss cleanup"
fi

log "GARP refreshed client neighbor from $OLD_MAC to $NEW_MAC and proxy neighbor cleanup succeeded"
