#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"

if [[ "${EUID}" -ne 0 ]]; then
  log "SKIP: requires root/CAP_NET_ADMIN"
  exit 0
fi
if ! command -v arping >/dev/null 2>&1; then
  log "SKIP: missing required command: arping"
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

OLD_MAC="$(ip -n "$OLD_NS" -o link show "$OLD_IF" | awk '{for (i=1;i<=NF;i++) if ($i=="link/ether") {print $(i+1); exit}}')"
NEW_MAC="$(ip -o link show "$NEW_IF" | awk '{for (i=1;i<=NF;i++) if ($i=="link/ether") {print $(i+1); exit}}')"

ip -n "$CLIENT_NS" neigh replace "$MOBILE" lladdr "$OLD_MAC" dev "$CLIENT_IF" nud stale
got="$(ip -n "$CLIENT_NS" neigh show "$MOBILE" dev "$CLIENT_IF")"
[[ "$got" == *"$OLD_MAC"* ]] || fail "client neighbor was not seeded with old holder MAC: $got"

arping -U -c 3 -I "$NEW_IF" "$MOBILE" >/dev/null
wait_for 5 bash -c "ip -n '$CLIENT_NS' neigh show '$MOBILE' dev '$CLIENT_IF' | grep -qi '$NEW_MAC'"

ip -n "$OLD_NS" neigh add proxy "$MOBILE" dev "$OLD_IF"
ip -n "$OLD_NS" neigh show proxy "$MOBILE" dev "$OLD_IF" | grep -q "$MOBILE"
ip -n "$OLD_NS" neigh del proxy "$MOBILE" dev "$OLD_IF"
if ip -n "$OLD_NS" neigh show proxy "$MOBILE" dev "$OLD_IF" | grep -q "$MOBILE"; then
  fail "proxy neighbor remained after holder loss cleanup"
fi

log "GARP refreshed client neighbor from $OLD_MAC to $NEW_MAC and proxy neighbor cleanup succeeded"
