#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause
set -euo pipefail

# Requirement: the generated firewall must enforce the default role matrix,
# while an IPAddressSet-scoped rule admits only the declared management service.
TEST_NAME="firewall-policy-dataplane"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"

require_common
require_cmd nft
require_cmd go

OUTSIDE="${TEST_ID}-outside"
ROUTER="${TEST_ID}-router"
LAN_CLIENT="${TEST_ID}-lan-client"
LAN_WEB="${TEST_ID}-lan-web"
MGMT_CLIENT="${TEST_ID}-mgmt-client"
MGMT_ALLOWED="${TEST_ID}-mgmt-allowed"
MGMT_DENIED="${TEST_ID}-mgmt-denied"
for ns in "$OUTSIDE" "$ROUTER" "$LAN_CLIENT" "$LAN_WEB" "$MGMT_CLIENT" "$MGMT_ALLOWED" "$MGMT_DENIED"; do
  create_ns "$ns"
done

create_veth_pair "$OUTSIDE" eth0 198.18.77.2/24 "$ROUTER" wan0 198.18.77.1/24
create_bridge_segment "${TEST_IF_ID}lan" \
  "$ROUTER" lan0 10.77.0.1/24 \
  "$LAN_CLIENT" eth0 10.77.0.2/24 \
  "$LAN_WEB" eth0 10.77.0.88/24
create_bridge_segment "${TEST_IF_ID}mgmt" \
  "$ROUTER" mgmt0 10.78.0.1/24 \
  "$MGMT_CLIENT" eth0 10.78.0.2/24 \
  "$MGMT_ALLOWED" eth0 10.78.0.88/24 \
  "$MGMT_DENIED" eth0 10.78.0.89/24
for ns in "$OUTSIDE" "$LAN_CLIENT" "$LAN_WEB" "$MGMT_CLIENT" "$MGMT_ALLOWED" "$MGMT_DENIED"; do
  case "$ns" in
    "$OUTSIDE") gateway=198.18.77.1 ;;
    "$LAN_CLIENT"|"$LAN_WEB") gateway=10.77.0.1 ;;
    *) gateway=10.78.0.1 ;;
  esac
  ip -n "$ns" route add default via "$gateway"
done
ip netns exec "$ROUTER" sysctl -qw net.ipv4.ip_forward=1

RENDER="$WORKDIR/firewall-policy-render"
(cd "$REPO_ROOT" && go build -buildvcs=false -o "$RENDER" ./tests/netns/firewall-policy-render)
"$RENDER" >"$WORKDIR/firewall.nft"
ip netns exec "$ROUTER" nft -c -f "$WORKDIR/firewall.nft"
ip netns exec "$ROUTER" nft -f "$WORKDIR/firewall.nft"

start_echo_server "$OUTSIDE" 198.18.77.2 8080 outside-web
start_echo_server "$LAN_WEB" 10.77.0.88 8080 lan-web
start_echo_server "$MGMT_ALLOWED" 10.78.0.88 8080 management-allowed
start_echo_server "$MGMT_DENIED" 10.78.0.89 8080 management-denied

expect_tcp() {
  local namespace="$1" address="$2" expected="$3"
  ip netns exec "$namespace" python3 - "$address" "$expected" <<'PY'
import socket
import sys

address, expected = sys.argv[1:]
with socket.create_connection((address, 8080), timeout=2) as sock:
    actual = sock.recv(100).decode().strip()
if actual != expected:
    raise SystemExit(f"response={actual!r}, expected={expected!r}")
PY
}

expect_blocked() {
  local namespace="$1" address="$2"
  if ip netns exec "$namespace" python3 - "$address" >/dev/null 2>&1 <<'PY'
import socket
import sys

with socket.create_connection((sys.argv[1], 8080), timeout=1):
    pass
PY
  then
    fail "unexpected TCP reachability from $namespace to $address"
  fi
}

# Matrix defaults: trust can use WAN; untrust cannot reach LAN; management can
# reach LAN; trust cannot reach management without an explicit exception.
expect_tcp "$LAN_CLIENT" 198.18.77.2 outside-web
expect_blocked "$OUTSIDE" 10.77.0.88
expect_tcp "$MGMT_CLIENT" 10.77.0.88 lan-web
expect_tcp "$LAN_CLIENT" 10.78.0.88 management-allowed
expect_blocked "$LAN_CLIENT" 10.78.0.89

log "PASS: role matrix denies untrust-to-trust and trust-to-management; IPAddressSet exception is scoped"
