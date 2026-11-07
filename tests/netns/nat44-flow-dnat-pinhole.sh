#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"
require_common
require_cmd nft
if [[ -z "${ROUTERD_TEST_BINARY:-}" || -z "${ROUTERD_NAT44_TEST_DRIVER:-}" ]]; then
	require_cmd go
fi

ROUTER="$TEST_ID-router"
WAN="$TEST_ID-wan"
WAN_ALT="$TEST_ID-wan-alt"
LAN0="$TEST_ID-lan0"
LAN1="$TEST_ID-lan1"
WAN_SERVER_PID=""
LAN_LISTENER_PID=""

create_ns "$ROUTER"
create_ns "$WAN"
create_ns "$WAN_ALT"
create_ns "$LAN0"
create_ns "$LAN1"

create_veth_pair "$ROUTER" wan0 198.18.16.2/24 "$WAN" eth0 198.18.16.53/24
create_veth_pair "$ROUTER" wan1 198.18.26.2/24 "$WAN_ALT" eth0 198.18.26.53/24
create_veth_pair "$ROUTER" lan0 198.18.15.1/24 "$LAN0" eth0 198.18.15.101/24
create_veth_pair "$ROUTER" lan1 198.18.17.1/24 "$LAN1" eth0 198.18.17.101/24
ip -n "$WAN" addr add 198.18.16.54/24 dev eth0
ip -n "$WAN_ALT" route add 198.18.16.2/32 via 198.18.26.2
ip -n "$LAN0" route add default via 198.18.15.1
ip -n "$LAN1" route add default via 198.18.17.1
ip netns exec "$ROUTER" sysctl -qw net.ipv4.ip_forward=1

(
	cd "$ROOT_DIR"
	if [[ -n "${ROUTERD_TEST_BINARY:-}" ]]; then
		cp "$ROUTERD_TEST_BINARY" "$WORKDIR/routerd"
	else
		go build -o "$WORKDIR/routerd" ./cmd/routerd
	fi
	if [[ -n "${ROUTERD_NAT44_TEST_DRIVER:-}" ]]; then
		cp "$ROUTERD_NAT44_TEST_DRIVER" "$WORKDIR/nat44-controller"
	else
		go build -o "$WORKDIR/nat44-controller" ./tests/netns/nat44-portforward-runtime
	fi
)
CONFIG="$SCRIPT_DIR/nat44-flow-dnat-pinhole-runtime/config.yaml"
"$WORKDIR/routerd" validate --config "$CONFIG"

cat >"$WORKDIR/udp-peer.py" <<'PY'
import json
import socket
import sys
import threading
import time

log_path = sys.argv[1]
response_sockets = {}
for port in (32098, 32099):
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.bind(("198.18.16.53", port))
    response_sockets[port] = sock

def serve(destination_port):
    server = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    server.bind(("0.0.0.0", destination_port))
    while True:
        payload, peer = server.recvfrom(2048)
        request = json.loads(payload.decode())
        with open(log_path, "a") as output:
            output.write(json.dumps({"id": request["id"], "source": peer[0], "sport": peer[1]}) + "\n")
        time.sleep(float(request.get("delay", 0)))
        try:
            response_sockets[int(request["responsePort"])].sendto(("reply:" + request["id"]).encode(), peer)
        except OSError as error:
            with open(log_path, "a") as output:
                output.write(json.dumps({"id": request["id"], "sendError": str(error)}) + "\n")

for port in (32100, 32101):
    threading.Thread(target=serve, args=(port,), daemon=True).start()
threading.Event().wait()
PY

cat >"$WORKDIR/udp-client.py" <<'PY'
import json
import socket
import sys

destination = sys.argv[1]
destination_port = int(sys.argv[2])
source_port = int(sys.argv[3])
response_port = int(sys.argv[4])
delay = float(sys.argv[5])
timeout = float(sys.argv[6])
request_id = sys.argv[7]

sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.bind(("0.0.0.0", source_port))
sock.settimeout(timeout)
payload = json.dumps({"id": request_id, "responsePort": response_port, "delay": delay}).encode()
sock.sendto(payload, (destination, destination_port))
try:
    data, _ = sock.recvfrom(2048)
except (socket.timeout, OSError):
    print("NO_RESPONSE")
else:
    print(data.decode())
finally:
    sock.close()
PY

ip netns exec "$WAN" python3 "$WORKDIR/udp-peer.py" "$WORKDIR/peer.log" >"$WORKDIR/peer.stdout" 2>"$WORKDIR/peer.stderr" &
WAN_SERVER_PID=$!
add_cleanup "if kill -0 '$WAN_SERVER_PID' 2>/dev/null; then kill '$WAN_SERVER_PID'; fi"

apply_config() {
	local config="$1" label="$2"
	ip netns exec "$ROUTER" "$WORKDIR/nat44-controller" "$config" "$WORKDIR/$label.nft" >"$WORKDIR/$label.apply.log" 2>&1 || {
		cat "$WORKDIR/$label.apply.log" >&2 || true
		fail "production NAT44 controller failed to apply $label"
	}
}

run_client() {
	local ns="$1" address="$2" destination_port="$3" source_port="$4" response_port="$5" delay="$6" timeout="$7" request_id="$8"
	ip netns exec "$ns" python3 "$WORKDIR/udp-client.py" "$address" "$destination_port" "$source_port" "$response_port" "$delay" "$timeout" "$request_id"
}

wait_for_peer_request() {
	local request_id="$1"
	wait_for 3 bash -c "grep -Fq '\"id\": \"$request_id\"' '$WORKDIR/peer.log'"
}

expect_client_result() {
	local ns="$1"
	local address="$2"
	local destination_port="$3"
	local source_port="$4"
	local response_port="$5"
	local delay="$6"
	local wait_seconds="$7"
	local request_id="$8"
	local want="$9"
	shift 9
	local label="$*"
	local actual
	if ! actual="$(run_client "$ns" "$address" "$destination_port" "$source_port" "$response_port" "$delay" "$wait_seconds" "$request_id" 2>"$WORKDIR/client.stderr")"; then
		cat "$WORKDIR/client.stderr" >&2 || true
		fail "$label: UDP client failed"
	fi
	[[ "$actual" == "$want" ]] || fail "$label: result=$actual want=$want"
	wait_for_peer_request "$request_id" || fail "$label: independent WAN peer did not receive the outbound request"
}

apply_config "$CONFIG" active
ip netns exec "$ROUTER" nft list table ip routerd_nat >"$WORKDIR/active-table.txt"
expect_client_result "$LAN0" 198.18.16.53 32100 55001 32099 0 3 allowed "reply:allowed" "authorized client response pinhole"

cat >"$WORKDIR/lan-listener.py" <<'PY'
import socket
import sys
from pathlib import Path

port = int(sys.argv[1])
ready = Path(sys.argv[2])
received = Path(sys.argv[3])
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.bind(("0.0.0.0", port))
ready.touch()
sock.settimeout(2)
try:
    data, peer = sock.recvfrom(2048)
except socket.timeout:
    pass
else:
    received.write_text(data.decode() + " from " + peer[0])
finally:
    sock.close()
PY
ip netns exec "$LAN0" python3 "$WORKDIR/lan-listener.py" 55001 "$WORKDIR/listener-ready" "$WORKDIR/listener-received" >"$WORKDIR/listener.stdout" 2>"$WORKDIR/listener.stderr" &
LAN_LISTENER_PID=$!
add_cleanup "if kill -0 '$LAN_LISTENER_PID' 2>/dev/null; then kill '$LAN_LISTENER_PID'; fi"
wait_for 3 test -e "$WORKDIR/listener-ready" || fail "LAN listener did not start"
ip netns exec "$WAN_ALT" python3 -c 'import socket; s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); s.bind(("0.0.0.0",32099)); s.sendto(b"wrong-interface",("198.18.16.2",55001))'
sleep 0.3
if [[ -e "$WORKDIR/listener-received" ]]; then
	fail "pinhole accepted a matching response arriving on an unselected WAN interface"
fi
wait "$LAN_LISTENER_PID" || {
	cat "$WORKDIR/listener.stderr" >&2 || true
	fail "LAN listener failed while checking the unselected WAN interface"
}
LAN_LISTENER_PID=""

expect_client_result "$LAN1" 198.18.16.53 32100 55002 32099 0 2 unlisted-source NO_RESPONSE "unlisted source client"
expect_client_result "$LAN0" 198.18.16.53 32101 55003 32099 0 2 unlisted-outport NO_RESPONSE "unlisted outbound destination port"
expect_client_result "$LAN0" 198.18.16.54 32100 55004 32099 0 2 unlisted-destination NO_RESPONSE "unlisted outbound destination address"
expect_client_result "$LAN0" 198.18.16.53 32100 55005 32098 0 2 unlisted-response-port NO_RESPONSE "unlisted inbound response source port"
expect_client_result "$LAN0" 198.18.16.53 32100 55006 32099 6 8 expired NO_RESPONSE "expired dynamic pinhole"
expect_client_result "$LAN0" 198.18.16.53 32100 55007 32099 0 3 withdrawal-seed "reply:withdrawal-seed" "seed dynamic pinhole before withdrawal"
ip netns exec "$ROUTER" nft list table ip routerd_nat >"$WORKDIR/seed-table.txt"
seed_map="$(sed -n '/routerd NAT44FlowDNATPinhole authorized-udp-response dnat/s/.*map @\([^ ]*\).*/\1/p' "$WORKDIR/seed-table.txt")"
[[ -n "$seed_map" ]] || fail "active pinhole DNAT rule did not reference a map"
ip netns exec "$ROUTER" nft list map ip routerd_nat "$seed_map" >"$WORKDIR/seed-map.txt"
grep -Fq '55007' "$WORKDIR/seed-map.txt" || fail "active pinhole map did not retain the seeded client tuple"

python3 - "$CONFIG" "$WORKDIR/without-pinhole.yaml" <<'PY'
import sys
from pathlib import Path

source = Path(sys.argv[1]).read_text()
marker = "    - apiVersion: net.routerd.net/v1alpha1\n      kind: NAT44FlowDNATPinhole\n"
if source.count(marker) != 1:
    raise SystemExit("expected exactly one NAT44FlowDNATPinhole resource")
Path(sys.argv[2]).write_text(source.split(marker, 1)[0].rstrip() + "\n")
PY
"$WORKDIR/routerd" validate --config "$WORKDIR/without-pinhole.yaml"
apply_config "$WORKDIR/without-pinhole.yaml" withdrawn
ip netns exec "$ROUTER" nft list table ip routerd_nat >"$WORKDIR/withdrawn-table.txt"
if grep -Fq 'routerd NAT44FlowDNATPinhole authorized-udp-response dnat' "$WORKDIR/withdrawn-table.txt"; then
	fail "active DNAT rule remained after NAT44FlowDNATPinhole withdrawal"
fi
expect_client_result "$LAN0" 198.18.16.53 32100 55008 32099 0 2 withdrawn NO_RESPONSE "withdrawn pinhole"

python3 - "$CONFIG" "$WORKDIR/readded-with-changed-source.yaml" <<'PY'
import sys
from pathlib import Path

source = Path(sys.argv[1]).read_text()
if source.count("198.18.15.101") != 1:
    raise SystemExit("expected exactly one authorized source address")
Path(sys.argv[2]).write_text(source.replace("198.18.15.101", "198.18.17.101"))
PY
"$WORKDIR/routerd" validate --config "$WORKDIR/readded-with-changed-source.yaml"
apply_config "$WORKDIR/readded-with-changed-source.yaml" readded
ip netns exec "$ROUTER" nft list table ip routerd_nat >"$WORKDIR/readded-table.txt"
readded_map="$(sed -n '/routerd NAT44FlowDNATPinhole authorized-udp-response dnat/s/.*map @\([^ ]*\).*/\1/p' "$WORKDIR/readded-table.txt")"
[[ -n "$readded_map" ]] || fail "re-added pinhole DNAT rule did not reference a map"
ip netns exec "$ROUTER" nft list map ip routerd_nat "$readded_map" >"$WORKDIR/readded-map.txt"
if grep -Fq '55007' "$WORKDIR/readded-map.txt"; then
	fail "old learned client tuple returned when the pinhole was re-added with a different source set"
fi

ip netns exec "$LAN0" python3 "$WORKDIR/lan-listener.py" 55007 "$WORKDIR/stale-listener-ready" "$WORKDIR/stale-listener-received" >"$WORKDIR/stale-listener.stdout" 2>"$WORKDIR/stale-listener.stderr" &
LAN_LISTENER_PID=$!
add_cleanup "if kill -0 '$LAN_LISTENER_PID' 2>/dev/null; then kill '$LAN_LISTENER_PID'; fi"
wait_for 3 test -e "$WORKDIR/stale-listener-ready" || fail "stale-flow listener did not start"
ip netns exec "$WAN" python3 -c 'import socket; s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); s.bind(("198.18.16.54",32099)); s.sendto(b"stale-flow",("198.18.16.2",55007))'
wait "$LAN_LISTENER_PID" || {
	cat "$WORKDIR/stale-listener.stderr" >&2 || true
	fail "LAN listener failed while checking the re-added pinhole"
}
LAN_LISTENER_PID=""
[[ ! -e "$WORKDIR/stale-listener-received" ]] || fail "stale dynamic pinhole tuple reached the previously authorized LAN client after policy change"

expect_client_result "$LAN1" 198.18.16.53 32100 55009 32099 0 3 changed-source "reply:changed-source" "newly authorized source after policy replacement"
expect_client_result "$LAN0" 198.18.16.53 32100 55010 32099 0 2 removed-source NO_RESPONSE "previous source denied after policy replacement"

printf 'PASS: production NAT44 controller dynamic UDP flow pinhole, source/destination/interface/port matching, expiry, withdrawal and changed-policy re-add\n'
