#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause
set -euo pipefail

# shellcheck disable=SC2034
TEST_NAME="portforward-dataplane"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"

require_common
require_cmd nft
require_cmd go

OUTSIDE="${TEST_ID}-outside"
ROUTER="${TEST_ID}-router"
CLIENT="${TEST_ID}-client"
BACKEND="${TEST_ID}-backend"

for ns in "$OUTSIDE" "$ROUTER" "$CLIENT" "$BACKEND"; do
  create_ns "$ns"
done
create_veth_pair "$OUTSIDE" eth0 198.18.77.2/24 "$ROUTER" wan0 198.18.77.1/24
create_bridge_segment "${TEST_IF_ID}br" \
  "$ROUTER" lan0 10.77.0.1/24 \
  "$CLIENT" eth0 10.77.0.2/24 \
  "$BACKEND" eth0 10.77.0.88/24
ip -n "$OUTSIDE" route add default via 198.18.77.1
ip -n "$CLIENT" route add default via 10.77.0.1
ip -n "$BACKEND" route add default via 10.77.0.1
ip netns exec "$ROUTER" sysctl -qw net.ipv4.ip_forward=1

RENDER="$WORKDIR/portforward-render"
(cd "$REPO_ROOT" && go build -o "$RENDER" ./tests/netns/portforward-render)
"$RENDER" nat >"$WORKDIR/nat44.nft"
"$RENDER" firewall >"$WORKDIR/firewall.nft"
for ruleset in firewall nat44; do
  ip netns exec "$ROUTER" nft -c -f "$WORKDIR/$ruleset.nft"
  ip netns exec "$ROUTER" nft -f "$WORKDIR/$ruleset.nft"
done

cat >"$WORKDIR/backend.py" <<'PY'
import socket

sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
sock.bind(("10.77.0.88", 443))
sock.listen(8)
while True:
    conn, peer = sock.accept()
    with conn:
        conn.sendall(("backend peer=" + peer[0] + "\n").encode())
        while True:
            data = conn.recv(1024)
            if not data:
                break
            conn.sendall(b"backend:" + data)
PY
ip netns exec "$BACKEND" python3 "$WORKDIR/backend.py" &
BACKEND_PID=$!
add_cleanup "if kill -0 '$BACKEND_PID' 2>/dev/null; then kill '$BACKEND_PID'; fi"

probe() {
  local namespace="$1" expected="$2"
  ip netns exec "$namespace" python3 - "$expected" <<'PY'
import socket
import sys

expected = sys.argv[1]
sock = socket.create_connection(("198.18.77.1", 8443), timeout=3)
with sock:
    actual = sock.recv(100).decode().strip()
if actual != expected:
    raise SystemExit(f"response={actual!r}, expected={expected!r}")
PY
}

probe "$OUTSIDE" "backend peer=198.18.77.2"

cat >"$WORKDIR/hairpin-client.py" <<'PY'
import pathlib
import socket
import sys

ready, release, result = map(pathlib.Path, sys.argv[1:])
sock = socket.create_connection(("198.18.77.1", 8443), timeout=3)
first = sock.recv(100).decode().strip()
if first != "backend peer=10.77.0.1":
    raise SystemExit(f"hairpin source was not masqueraded: {first!r}")
ready.touch()
for _ in range(50):
    if release.exists():
        break
    import time
    time.sleep(0.1)
else:
    raise SystemExit("timed out waiting for nftables reapply")
sock.sendall(b"still-connected\n")
reply = sock.recv(100).decode().strip()
sock.close()
if reply != "backend:still-connected":
    raise SystemExit(f"established hairpin flow did not survive reapply: {reply!r}")
result.touch()
PY

READY="$WORKDIR/hairpin-ready"
RELEASE="$WORKDIR/hairpin-release"
RESULT="$WORKDIR/hairpin-result"
ip netns exec "$CLIENT" python3 "$WORKDIR/hairpin-client.py" "$READY" "$RELEASE" "$RESULT" &
HAIRPIN_PID=$!
add_cleanup "if kill -0 '$HAIRPIN_PID' 2>/dev/null; then kill '$HAIRPIN_PID'; fi"
wait_for 3 test -e "$READY" || fail "hairpin connection was not established"
for ruleset in firewall nat44; do
  ip netns exec "$ROUTER" nft -f "$WORKDIR/$ruleset.nft"
done
touch "$RELEASE"
wait "$HAIRPIN_PID"
test -e "$RESULT" || fail "hairpin flow did not complete after nftables reapply"

log "PASS: generated PortForward rules provide external DNAT, hairpin SNAT, and conntrack continuity"
