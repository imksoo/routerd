#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"
require_common
require_cmd dnsmasq
require_cmd dhcpcd
require_cmd go
require_cmd unshare
require_cmd mount
require_cmd xargs

DNSMASQ="$(command -v dnsmasq)"
DHCPCD="$(command -v dhcpcd)"
VETH_COUNTER=0

run_case() {
	local mode="$1"
	local router="${TEST_ID}-${mode}-router"
	local client="${TEST_ID}-${mode}-client"
	local config="$WORKDIR/dnsmasq-${mode}.conf"
	local server_log="$WORKDIR/dnsmasq-${mode}.log"
	local client_log="$WORKDIR/dhcpcd-${mode}.log"
	local client_conf="$WORKDIR/dhcpcd-${mode}.conf"
	local lease_file="$WORKDIR/leases-${mode}"
	local pid_file="$WORKDIR/dnsmasq-${mode}.pid"
	local address=""
	local i

	create_ns "$router"
	create_ns "$client"
	VETH_COUNTER=$((VETH_COUNTER + 1))
	local link_router="${TEST_IF_ID}v${VETH_COUNTER}a"
	local link_client="${TEST_IF_ID}v${VETH_COUNTER}b"
	ip link add "$link_router" type veth peer name "$link_client"
	add_cleanup "if ip link show '$link_router' >/dev/null 2>&1; then ip link delete '$link_router'; fi"
	add_cleanup "if ip link show '$link_client' >/dev/null 2>&1; then ip link delete '$link_client'; fi"
	ip link set "$link_router" netns "$router"
	ip link set "$link_client" netns "$client"
	ip -n "$router" link set "$link_router" name lan0
	ip -n "$client" link set "$link_client" name eth0
	ip -n "$router" -6 addr add fd00:1234::1/64 dev lan0 nodad
	ip -n "$router" link set lan0 up
	ip -n "$client" link set eth0 up

	(
		cd "$ROOT_DIR"
		go run ./tests/netns/dhcpv6-stateful-render "$lease_file" "$mode"
	) >"$config"
	dnsmasq --test --conf-file="$config" >"$WORKDIR/dnsmasq-${mode}-syntax.log" 2>&1
	ip netns exec "$router" "$DNSMASQ" --keep-in-foreground \
		--conf-file="$config" --pid-file="$pid_file" --log-facility="$server_log" &
	local server_pid=$!
	add_cleanup "if kill -0 '$server_pid' 2>/dev/null; then kill -TERM '$server_pid'; wait '$server_pid' 2>/dev/null || true; fi"
	wait_for 5 test -s "$pid_file" || fail "dnsmasq did not start for mode=$mode"

	cat >"$client_conf" <<'EOF'
noipv6rs
interface eth0
  ia_na 42
  noipv6rs
EOF
	# shellcheck disable=SC2016 # evaluated inside the client's private mount namespace
	ip netns exec "$client" unshare -m sh -ec '
		mount --make-rprivate /
		mount -t tmpfs tmpfs /run
		mount -t tmpfs tmpfs /var/lib
		mkdir -p /run/dhcpcd /var/lib/dhcpcd
		exec "$1" -d -6 -B --waitip=6 -t 25 -f "$2" -c /bin/true eth0
	' sh "$DHCPCD" "$client_conf" >"$client_log" 2>&1 &
	local client_pid=$!
	add_cleanup "if kill -0 '$client_pid' 2>/dev/null; then kill -TERM '$client_pid'; wait '$client_pid' 2>/dev/null || true; fi"
	add_cleanup "ip netns pids '$client' | xargs -r kill -TERM; ip netns pids '$router' | xargs -r kill -TERM; sleep 0.2"

	for _ in $(seq 1 100); do
		address="$(ip -n "$client" -6 -o addr show dev eth0 scope global | awk '$4 ~ /^fd00:1234::/ && $4 !~ /tentative|dadfailed/ {print $4; exit}')"
		[[ -n "$address" ]] && break
		kill -0 "$client_pid" 2>/dev/null || { cat "$client_log" "$server_log" >&2; fail "dhcpcd exited before IA_NA assignment for mode=$mode"; }
		sleep 0.2
	done
	[[ -n "$address" ]] || { cat "$client_log" "$server_log" >&2; fail "no IA_NA address was assigned for mode=$mode"; }
	python3 - "$address" <<'PY'
import ipaddress
import sys

address = ipaddress.ip_interface(sys.argv[1]).ip
start = ipaddress.ip_address("fd00:1234::100")
end = ipaddress.ip_address("fd00:1234::110")
if not start <= address <= end:
    raise SystemExit(f"assigned address outside configured pool: {address}")
PY
	grep -q 'DHCPREQUEST(lan0)' "$server_log"
	grep -q 'DHCPREPLY(lan0)' "$server_log"
	grep -q 'ADV fd00:1234::' "$client_log"
	grep -q 'REPLY6 received' "$client_log"
	printf 'PASS: DHCPv6Server mode=%s completed IA_NA and assigned %s\n' "$mode" "${address%/*}"
}

run_case stateful
run_case both
