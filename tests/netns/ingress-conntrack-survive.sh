#!/usr/bin/env bash
set -euo pipefail

TEST_NAME="ingress-conntrack-survive"
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_common
require_cmd nft
require_cmd grep

CLIENT="${TEST_ID}-client"
ROUTER="${TEST_ID}-router"
B1="${TEST_ID}-b1"
B2="${TEST_ID}-b2"
create_ns "$CLIENT"
create_ns "$ROUTER"
create_ns "$B1"
create_ns "$B2"
create_veth_pair "$CLIENT" eth0 10.92.0.2/24 "$ROUTER" c0 10.92.0.1/24
create_veth_pair "$ROUTER" b10 10.92.1.1/24 "$B1" eth0 10.92.1.2/24
create_veth_pair "$ROUTER" b20 10.92.2.1/24 "$B2" eth0 10.92.2.2/24
ip -n "$CLIENT" route add default via 10.92.0.1
ip -n "$B1" route add default via 10.92.1.1
ip -n "$B2" route add default via 10.92.2.1
ip netns exec "$ROUTER" sysctl -qw net.ipv4.ip_forward=1

start_echo_server "$B1" 10.92.1.2 8080 backend1
start_echo_server "$B2" 10.92.2.2 8080 backend2
wait_for 5 bash -c "ip netns exec '$CLIENT' python3 - <<'PY'
import socket
s=socket.create_connection(('10.92.1.2',8080),timeout=1)
print(s.recv(100).decode().strip())
PY" >/dev/null

cat >"$WORKDIR/nft-backend1.nft" <<'EOF'
flush ruleset
table ip routerd_nat {
  chain prerouting {
    type nat hook prerouting priority dstnat; policy accept;
    tcp dport 6443 dnat to 10.92.1.2:8080
  }
  chain postrouting {
    type nat hook postrouting priority srcnat; policy accept;
    masquerade
  }
}
EOF
cat >"$WORKDIR/nft-backend2.nft" <<'EOF'
flush ruleset
table ip routerd_nat {
  chain prerouting {
    type nat hook prerouting priority dstnat; policy accept;
    tcp dport 6443 dnat to 10.92.2.2:8080
  }
  chain postrouting {
    type nat hook postrouting priority srcnat; policy accept;
    masquerade
  }
}
EOF
ip netns exec "$ROUTER" nft -f "$WORKDIR/nft-backend1.nft"

cat >"$WORKDIR/client.py" <<'PY'
import socket
import subprocess
import sys
import time

s = socket.create_connection(("10.92.0.1", 6443), timeout=3)
first = s.recv(100).decode().strip()
if first != "backend1":
    raise SystemExit(f"first flow went to {first!r}")
subprocess.check_call(["ip", "netns", "exec", sys.argv[1], "nft", "-f", sys.argv[2]])
time.sleep(0.5)
s.sendall(b"old\n")
old = s.recv(100).decode().strip()
if old != "backend1:old":
    raise SystemExit(f"old flow moved unexpectedly: {old!r}")
n = socket.create_connection(("10.92.0.1", 6443), timeout=3)
new = n.recv(100).decode().strip()
if new != "backend2":
    raise SystemExit(f"new flow did not use backend2: {new!r}")
PY

ip netns exec "$CLIENT" python3 "$WORKDIR/client.py" "$ROUTER" "$WORKDIR/nft-backend2.nft"

log "ok: existing conntrack flow stayed on backend1 and new flow used backend2"
