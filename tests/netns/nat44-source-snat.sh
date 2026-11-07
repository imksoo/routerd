#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"
require_common
require_cmd nft
require_cmd curl
if [[ -z "${ROUTERD_TEST_BINARY:-}" || -z "${ROUTERD_NAT44_TEST_DRIVER:-}" ]]; then
	require_cmd go
fi

ROUTER="$TEST_ID-router"
WAN="$TEST_ID-wan"
LAN0="$TEST_ID-lan0"
LAN1="$TEST_ID-lan1"
BACKEND_PID=""

create_ns "$ROUTER"
create_ns "$WAN"
create_ns "$LAN0"
create_ns "$LAN1"

create_veth_pair "$ROUTER" wan0 198.18.16.2/24 "$WAN" eth0 198.18.16.53/24
ip -n "$ROUTER" addr add 198.18.16.10/32 dev wan0
ip -n "$ROUTER" addr add 198.18.16.11/32 dev wan0
ip -n "$ROUTER" addr add 198.18.16.20/32 dev wan0
ip -n "$WAN" addr add 198.18.16.54/24 dev eth0

create_veth_pair "$ROUTER" lan0 198.18.15.1/24 "$LAN0" eth0 198.18.15.101/24
create_veth_pair "$ROUTER" lan1 198.18.17.1/24 "$LAN1" eth0 198.18.17.101/24
ip -n "$LAN0" route add default via 198.18.15.1
ip -n "$LAN1" route add default via 198.18.17.1
ip -n "$WAN" route add 198.18.15.0/24 via 198.18.16.2
ip -n "$WAN" route add 198.18.17.0/24 via 198.18.16.2
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
CONFIG="$SCRIPT_DIR/nat44-source-snat-runtime/config.yaml"
"$WORKDIR/routerd" validate --config "$CONFIG"

cat >"$WORKDIR/http-backend.py" <<'PY'
from http.server import BaseHTTPRequestHandler, HTTPServer

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body = (("%s:%d" % self.client_address) if self.path == "/tuple" else self.client_address[0]).encode() + b"\n"
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Connection", "close")
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        pass

HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
PY

ip netns exec "$WAN" python3 "$WORKDIR/http-backend.py" >"$WORKDIR/backend.stdout" 2>"$WORKDIR/backend.stderr" &
BACKEND_PID=$!
add_cleanup "if kill -0 '$BACKEND_PID' 2>/dev/null; then kill '$BACKEND_PID'; fi"

http_source() {
	local client_ns="$1" address="$2"
	ip netns exec "$client_ns" curl --noproxy '*' --fail --silent --show-error --max-time 3 "http://$address:8080/source"
}

expect_source() {
	local client_ns="$1" address="$2" want="$3" label="$4"
	local actual
	if ! actual="$(http_source "$client_ns" "$address" 2>"$WORKDIR/curl.stderr")"; then
		cat "$WORKDIR/curl.stderr" "$WORKDIR/backend.stderr" >&2 || true
		fail "$label: HTTP request failed"
	fi
	[[ "$actual" == "$want" ]] || fail "$label: backend saw source $actual, want $want"
}

wait_for 10 bash -c "ip netns exec '$LAN0' curl --noproxy '*' --fail --silent --max-time 1 http://198.18.16.53:8080/ready | grep -Fxq 198.18.15.101" || {
	cat "$WORKDIR/backend.stderr" >&2 || true
	fail "independent WAN backend did not answer before SNAT was applied"
}

expect_source "$LAN0" 198.18.16.53 198.18.15.101 "unconfigured LAN-A flow"

apply_config() {
	local config="$1" label="$2"
	ip netns exec "$ROUTER" "$WORKDIR/nat44-controller" "$config" "$WORKDIR/$label.nft" >"$WORKDIR/$label.apply.log" 2>&1 || {
		cat "$WORKDIR/$label.apply.log" >&2 || true
		fail "production NAT44 controller failed to apply $label"
	}
}

apply_config "$CONFIG" primary
ip netns exec "$ROUTER" nft list table ip routerd_nat >"$WORKDIR/primary-active.txt"
expect_source "$LAN0" 198.18.16.53 198.18.16.10 "matching source and destination"
expect_source "$LAN0" 198.18.16.54 198.18.15.101 "excluded destination"
expect_source "$LAN1" 198.18.16.53 198.18.17.101 "unmatched source range"

python3 - "$CONFIG" "$WORKDIR/secondary.yaml" <<'PY'
import sys
from pathlib import Path

source = Path(sys.argv[1]).read_text()
source_ref = "resource: IPv4StaticAddress/snat-primary"
if source.count(source_ref) != 1:
    raise SystemExit("expected exactly one primary SNAT reference")
Path(sys.argv[2]).write_text(source.replace(source_ref, "resource: IPv4StaticAddress/snat-secondary"))
PY
"$WORKDIR/routerd" validate --config "$WORKDIR/secondary.yaml"
apply_config "$WORKDIR/secondary.yaml" secondary
expect_source "$LAN0" 198.18.16.53 198.18.16.11 "updated SNAT address reference"
expect_source "$LAN0" 198.18.16.54 198.18.15.101 "excluded destination after update"

# Exercise the translation-shaped resource through the same production
# controller, including the real source port observed by an independent server.
python3 - "$CONFIG" "$WORKDIR/translation.yaml" <<'PYCONFIG'
import sys
from pathlib import Path
source = Path(sys.argv[1]).read_text()
head, separator, _ = source.partition("        type: snat\n")
if not separator:
    raise SystemExit("expected SNAT fixture")
Path(sys.argv[2]).write_text(head + """        outboundInterface: wan
        sourceCIDRs: [198.18.15.0/24]
        translation:
          type: address
          address: 198.18.16.10
          portMapping:
            type: range
            start: 40000
            end: 40010
""")
PYCONFIG
"$WORKDIR/routerd" validate --config "$WORKDIR/translation.yaml"
apply_config "$WORKDIR/translation.yaml" translation
tuple=$(ip netns exec "$LAN0" curl --noproxy '*' --fail --silent --show-error --max-time 3 http://198.18.16.53:8080/tuple)
[[ "${tuple%:*}" == 198.18.16.10 ]] || fail "translation address: $tuple"
port=${tuple##*:}
[[ "$port" =~ ^[0-9]+$ ]] && ((port >= 40000 && port <= 40010)) || fail "translation source port: $tuple"
expect_source "$LAN1" 198.18.16.53 198.18.17.101 "translation unmatched source"

python3 - "$WORKDIR/translation.yaml" "$WORKDIR/pool.yaml" <<'PYPOOL'
import sys
from pathlib import Path
source = Path(sys.argv[1]).read_text()
old = "          type: address\n          address: 198.18.16.10\n"
if source.count(old) != 1:
    raise SystemExit("expected single translation address")
Path(sys.argv[2]).write_text(source.replace(old, "          type: pool\n          addresses: [198.18.16.10, 198.18.16.11, 198.18.16.20]\n"))
PYPOOL
"$WORKDIR/routerd" validate --config "$WORKDIR/pool.yaml"
apply_config "$WORKDIR/pool.yaml" pool
: > "$WORKDIR/pool-seen"
for attempt in {1..6}; do
 tuple=$(ip netns exec "$LAN0" curl --noproxy '*' --fail --silent --show-error --max-time 3 http://198.18.16.53:8080/tuple)
 case "${tuple%:*}" in
  198.18.16.10|198.18.16.11|198.18.16.20) printf '%s\n' "${tuple%:*}" >> "$WORKDIR/pool-seen";;
  *) fail "pool used an unlisted address: $tuple";;
 esac
 port=${tuple##*:}
 [[ "$port" =~ ^[0-9]+$ ]] && ((port >= 40000 && port <= 40010)) || fail "pool source port: $tuple"
done
[[ "$(sort -u "$WORKDIR/pool-seen" | wc -l)" -eq 3 ]] || fail "not every declared pool address forwarded traffic"
expect_source "$LAN1" 198.18.16.53 198.18.17.101 "pool unmatched source"

cat >"$WORKDIR/empty.yaml" <<'YAML'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: nat44-source-snat-runtime
spec: {}
YAML
"$WORKDIR/routerd" validate --config "$WORKDIR/empty.yaml"
apply_config "$WORKDIR/empty.yaml" withdrawn
if ip netns exec "$ROUTER" nft list table ip routerd_nat >/dev/null 2>&1; then
	fail "routerd NAT table remained after NAT44Rule withdrawal"
fi
expect_source "$LAN0" 198.18.16.53 198.18.15.101 "flow after NAT44Rule withdrawal"

printf 'PASS: production NAT44 controller SNAT source/destination selectors, referenced address update, translation address/port range, noncontiguous pool, excluded traffic and withdrawal\n'
