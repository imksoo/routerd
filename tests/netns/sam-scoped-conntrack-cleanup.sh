#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"

if [[ "${EUID}" -ne 0 ]]; then
  log "SKIP: requires root/CAP_NET_ADMIN"
  exit 0
fi
if ! command -v conntrack >/dev/null 2>&1; then
  log "SKIP: missing required command: conntrack"
  exit 0
fi

require_common
require_cmd nc

CLIENT="${TEST_ID}-client"
ROUTER="${TEST_ID}-router"
TARGET="${TEST_ID}-target"
OTHER="${TEST_ID}-other"
create_ns "$CLIENT"
create_ns "$ROUTER"
create_ns "$TARGET"
create_ns "$OTHER"
create_veth_pair "$CLIENT" eth0 10.93.0.2/24 "$ROUTER" c0 10.93.0.1/24
create_veth_pair "$ROUTER" t0 10.93.1.1/24 "$TARGET" eth0 10.93.1.2/24
create_veth_pair "$ROUTER" o0 10.93.2.1/24 "$OTHER" eth0 10.93.2.2/24
ip -n "$CLIENT" route add default via 10.93.0.1
ip -n "$TARGET" route add default via 10.93.1.1
ip -n "$OTHER" route add default via 10.93.2.1
ip netns exec "$ROUTER" sysctl -qw net.ipv4.ip_forward=1

ip netns exec "$TARGET" sh -c "while true; do nc -l -p 8080 >/dev/null; done" &
TARGET_PID=$!
add_cleanup "kill '$TARGET_PID'"
ip netns exec "$OTHER" sh -c "while true; do nc -l -p 8081 >/dev/null; done" &
OTHER_PID=$!
add_cleanup "kill '$OTHER_PID'"

wait_for 5 bash -c "ip netns exec '$CLIENT' nc -z -w 1 10.93.1.2 8080"
wait_for 5 bash -c "ip netns exec '$CLIENT' nc -z -w 1 10.93.2.2 8081"

wait_for 5 bash -c "ip netns exec '$ROUTER' conntrack -L -f ipv4 2>/dev/null | grep -q '10.93.1.2'"
wait_for 5 bash -c "ip netns exec '$ROUTER' conntrack -L -f ipv4 2>/dev/null | grep -q '10.93.2.2'"

ip netns exec "$ROUTER" conntrack -D -f ipv4 -d 10.93.1.2 >/dev/null 2>&1 || true
ip netns exec "$ROUTER" conntrack -D -f ipv4 -s 10.93.1.2 >/dev/null 2>&1 || true

if ip netns exec "$ROUTER" conntrack -L -f ipv4 2>/dev/null | grep -q '10.93.1.2'; then
  fail "target conntrack entry survived scoped cleanup"
fi
ip netns exec "$ROUTER" conntrack -L -f ipv4 2>/dev/null | grep -q '10.93.2.2' || fail "unrelated conntrack entry was removed"

log "scoped conntrack cleanup removed target flow only"
