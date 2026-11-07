#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"
require_common
require_cmd nft
require_cmd curl
require_cmd go

ROUTER="$TEST_ID-router"
WAN="$TEST_ID-wan"
LAN_SERVER="$TEST_ID-lan-server"
LAN_CLIENT="$TEST_ID-lan-client"
NFT_FILE="$WORKDIR/nat44.nft"
BACKEND_LOG="$WORKDIR/backend.log"
BACKEND_PID=""
PERSISTENT_PID=""
VETH_COUNTER=0

create_ns "$ROUTER"
create_ns "$WAN"
create_ns "$LAN_SERVER"
create_ns "$LAN_CLIENT"

ip -n "$ROUTER" link add lan0 type bridge
ip -n "$ROUTER" addr add 198.18.15.1/24 dev lan0
ip -n "$ROUTER" link set lan0 up
create_veth_pair "$ROUTER" wan0 198.18.16.2/24 "$WAN" eth0 198.18.16.1/24
ip -n "$WAN" route add 198.18.15.0/24 via 198.18.16.2

attach_lan_peer() {
	local peer_ns="$1"
	local address="$2"
	VETH_COUNTER=$((VETH_COUNTER + 1))
	local router_link="$TEST_IF_ID"v"$VETH_COUNTER"a
	local peer_link="$TEST_IF_ID"v"$VETH_COUNTER"b

	ip link add "$router_link" type veth peer name "$peer_link"
	add_cleanup "if ip link show '$router_link' >/dev/null 2>&1; then ip link delete '$router_link'; fi"
	add_cleanup "if ip link show '$peer_link' >/dev/null 2>&1; then ip link delete '$peer_link'; fi"
	ip link set "$router_link" netns "$ROUTER"
	ip link set "$peer_link" netns "$peer_ns"
	ip -n "$ROUTER" link set "$router_link" master lan0
	ip -n "$ROUTER" link set "$router_link" up
	ip -n "$peer_ns" link set "$peer_link" name eth0
	ip -n "$peer_ns" addr add "$address/24" dev eth0
	ip -n "$peer_ns" link set eth0 up
	ip -n "$peer_ns" route add default via 198.18.15.1
}

attach_lan_peer "$LAN_SERVER" 198.18.15.100
attach_lan_peer "$LAN_CLIENT" 198.18.15.101
ip netns exec "$ROUTER" sysctl -qw net.ipv4.ip_forward=1

(
	cd "$ROOT_DIR"
	go build -o "$WORKDIR/routerd" ./cmd/routerd
	go build -o "$WORKDIR/nat44-controller" ./tests/netns/nat44-portforward-runtime
)
"$WORKDIR/routerd" validate --config "$SCRIPT_DIR/nat44-portforward-runtime/config.yaml"

cat >"$WORKDIR/http-backend.py" <<'PY'
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

log_path = sys.argv[1]

class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        with open(log_path, "a") as output:
            output.write(self.path + " " + self.client_address[0] + "\n")
        body = (self.client_address[0] + "\n").encode()
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Connection", "keep-alive")
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        pass

HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
PY

stop_backend() {
	if [[ -n "$BACKEND_PID" ]] && kill -0 "$BACKEND_PID" 2>/dev/null; then
		kill -TERM "$BACKEND_PID"
		wait "$BACKEND_PID" 2>/dev/null || true
	fi
	BACKEND_PID=""
}

stop_persistent_client() {
	if [[ -n "$PERSISTENT_PID" ]] && kill -0 "$PERSISTENT_PID" 2>/dev/null; then
		kill -TERM "$PERSISTENT_PID"
		wait "$PERSISTENT_PID" 2>/dev/null || true
	fi
	PERSISTENT_PID=""
}

ip netns exec "$LAN_SERVER" python3 "$WORKDIR/http-backend.py" "$BACKEND_LOG" >"$WORKDIR/backend.stderr" 2>&1 &
BACKEND_PID=$!
add_cleanup stop_backend

backend_ready() {
	[[ "$(ip netns exec "$LAN_CLIENT" curl --noproxy '*' --fail --silent --max-time 1 http://198.18.15.100:8080/ready 2>/dev/null)" == "198.18.15.101" ]]
}
wait_for 10 backend_ready || {
	cat "$WORKDIR/backend.stderr" >&2 || true
	fail "independent LAN backend did not answer"
}

http() {
	local client_ns="$1"
	local address="$2"
	local path="$3"
	ip netns exec "$client_ns" curl --noproxy '*' --fail --silent --show-error --max-time 3 "http://$address:8080/$path"
}

if http "$WAN" 198.18.16.2 before-apply >"$WORKDIR/before-apply.txt" 2>&1; then
	fail "WAN reached the forwarding address before PortForward was applied"
fi

ip netns exec "$ROUTER" "$WORKDIR/nat44-controller" "$SCRIPT_DIR/nat44-portforward-runtime/config.yaml" "$NFT_FILE" >"$WORKDIR/apply.log" 2>&1 || {
	cat "$WORKDIR/apply.log" >&2 || true
	fail "production NAT44 controller did not apply PortForward"
}
ip netns exec "$ROUTER" nft list table ip routerd_nat >"$WORKDIR/table-active.txt"

actual="$(http "$WAN" 198.18.16.2 wan-forward)"
[[ "$actual" == "198.18.16.1" ]] || fail "WAN-forward backend source=$actual want=198.18.16.1"
actual="$(http "$LAN_CLIENT" 198.18.16.2 lan-hairpin)"
[[ "$actual" == "198.18.15.1" ]] || fail "hairpin backend source=$actual want=198.18.15.1"
actual="$(http "$LAN_CLIENT" 198.18.15.100 lan-direct)"
[[ "$actual" == "198.18.15.101" ]] || fail "direct LAN backend source=$actual want=198.18.15.101"

cat >"$WORKDIR/persistent-client.py" <<'PY'
import socket
import sys
import time
from pathlib import Path

ready_path = Path(sys.argv[1])
release_path = Path(sys.argv[2])

sock = socket.create_connection(("198.18.16.2", 8080), timeout=5)
sock.settimeout(5)
stream = sock.makefile("rb")

def request(path):
    sock.sendall(("GET /" + path + " HTTP/1.1\r\nHost: port-forward.test\r\n\r\n").encode())
    status = stream.readline().decode().strip()
    if " 200 " not in status:
        raise RuntimeError("unexpected HTTP status: " + status)
    content_length = 0
    while True:
        line = stream.readline()
        if line in (b"\r\n", b"\n", b""):
            break
        name, value = line.decode().split(":", 1)
        if name.lower() == "content-length":
            content_length = int(value)
    body = stream.read(content_length).decode().strip()
    return body

print("before-withdrawal=" + request("persistent-before"), flush=True)
ready_path.touch()
for _ in range(100):
    if release_path.exists():
        break
    time.sleep(0.1)
else:
    raise RuntimeError("timed out waiting for rule withdrawal")
try:
    request("persistent-after")
except ConnectionResetError:
    print("existing-flow-after-withdrawal=reset", flush=True)
else:
    raise RuntimeError("existing flow remained active after PortForward withdrawal")
finally:
    sock.close()
PY

ip netns exec "$WAN" timeout 15 python3 "$WORKDIR/persistent-client.py" "$WORKDIR/persistent-ready" "$WORKDIR/persistent-release" >"$WORKDIR/persistent-client.log" 2>&1 &
PERSISTENT_PID=$!
add_cleanup stop_persistent_client
wait_for 10 test -e "$WORKDIR/persistent-ready" || {
	cat "$WORKDIR/persistent-client.log" >&2 || true
	fail "persistent WAN flow did not establish before withdrawal"
}
grep -Fxq 'before-withdrawal=198.18.16.1' "$WORKDIR/persistent-client.log" ||
	fail "persistent WAN flow did not reach the backend before withdrawal"
cat >"$WORKDIR/empty.yaml" <<'YAML'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: nat44-portforward-runtime
spec: {}
YAML
ip netns exec "$ROUTER" "$WORKDIR/nat44-controller" "$WORKDIR/empty.yaml" "$NFT_FILE" >"$WORKDIR/withdraw.log" 2>&1 || {
	cat "$WORKDIR/withdraw.log" >&2 || true
	fail "production NAT44 controller did not withdraw PortForward"
}
if ip netns exec "$ROUTER" nft list table ip routerd_nat >/dev/null 2>&1; then
	fail "routerd NAT table remained after desired-state withdrawal"
fi

touch "$WORKDIR/persistent-release"
wait "$PERSISTENT_PID" || {
	cat "$WORKDIR/persistent-client.log" >&2 || true
	fail "existing WAN flow did not terminate after PortForward withdrawal"
}
PERSISTENT_PID=""
grep -Fxq 'existing-flow-after-withdrawal=reset' "$WORKDIR/persistent-client.log" ||
	fail "existing WAN flow did not reset after PortForward withdrawal"
if grep -Fq '/persistent-after ' "$BACKEND_LOG"; then
	fail "withdrawn PortForward delivered an established-flow request to the LAN backend"
fi

if http "$WAN" 198.18.16.2 new-after-withdrawal >"$WORKDIR/new-flow.txt" 2>&1; then
	fail "new WAN flow succeeded after PortForward withdrawal"
fi
if grep -Fq '/new-after-withdrawal ' "$BACKEND_LOG"; then
	fail "withdrawn PortForward delivered a new connection to the LAN backend"
fi

printf 'PASS: production NAT controller PortForward, WAN DNAT, LAN hairpin, existing-flow reset and new-flow withdrawal\n'
