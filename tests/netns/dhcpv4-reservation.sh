#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"
require_common
require_cmd dnsmasq
require_cmd busybox
require_cmd go

DNSMASQ="$(command -v dnsmasq)"
BUSYBOX="$(command -v busybox)"
ROUTER="$TEST_ID-router"
RESERVED_CLIENT="$TEST_ID-reserved"
DYNAMIC_CLIENT="$TEST_ID-dynamic"
LEASE_FILE="$WORKDIR/dnsmasq.leases"
CONFIG="$WORKDIR/dnsmasq.conf"
PID_FILE="$WORKDIR/dnsmasq.pid"
SERVER_LOG="$WORKDIR/dnsmasq.log"
SERVER_PID=""
VETH_COUNTER=0

# Start from fresh dnsmasq lease state even when reusing WORKDIR to diagnose a failure.
: >"$LEASE_FILE"
: >"$SERVER_LOG"

create_ns "$ROUTER"
create_ns "$RESERVED_CLIENT"
create_ns "$DYNAMIC_CLIENT"

ip -n "$ROUTER" link add lan0 type bridge
ip -n "$ROUTER" addr add 192.0.2.1/24 dev lan0
ip -n "$ROUTER" link set lan0 up

attach_client() {
	local client_ns="$1"
	local mac="$2"
	VETH_COUNTER=$((VETH_COUNTER + 1))
	local router_link="$TEST_IF_ID"v"$VETH_COUNTER"a
	local client_link="$TEST_IF_ID"v"$VETH_COUNTER"b

	ip link add "$router_link" type veth peer name "$client_link"
	add_cleanup "if ip link show '$router_link' >/dev/null 2>&1; then ip link delete '$router_link'; fi"
	add_cleanup "if ip link show '$client_link' >/dev/null 2>&1; then ip link delete '$client_link'; fi"
	ip link set "$router_link" netns "$ROUTER"
	ip link set "$client_link" netns "$client_ns"
	ip -n "$ROUTER" link set "$router_link" master lan0
	ip -n "$ROUTER" link set "$router_link" up
	ip -n "$client_ns" link set "$client_link" name eth0
	ip -n "$client_ns" link set eth0 address "$mac"
	ip -n "$client_ns" link set eth0 up
}

attach_client "$RESERVED_CLIENT" 02:00:00:00:00:50
attach_client "$DYNAMIC_CLIENT" 02:00:00:00:00:51

(
	cd "$ROOT_DIR"
	go run ./tests/netns/dhcpv4-reservation-render "$LEASE_FILE"
) >"$CONFIG"
dnsmasq --test --conf-file="$CONFIG" >"$WORKDIR/dnsmasq-syntax.log" 2>&1

start_server() {
	ip netns exec "$ROUTER" "$DNSMASQ" --keep-in-foreground --user=root \
		--conf-file="$CONFIG" --pid-file="$PID_FILE" --log-facility="$SERVER_LOG" &
	SERVER_PID=$!
	wait_for 5 test -s "$PID_FILE" || {
		cat "$WORKDIR/dnsmasq-syntax.log" "$SERVER_LOG" >&2
		fail "rendered dnsmasq service did not start"
	}
	kill -0 "$SERVER_PID" 2>/dev/null || fail "dnsmasq exited during startup"
}

stop_server() {
	if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
		kill -TERM "$SERVER_PID"
		wait "$SERVER_PID" 2>/dev/null || true
	fi
	SERVER_PID=""
}

add_cleanup stop_server
start_server

start_client() {
	local name="$1"
	local client_ns="$2"
	local expected_ip="$3"
	local events="$WORKDIR/$name.events"
	local client_log="$WORKDIR/$name.log"
	local hook="$WORKDIR/$name-hook.sh"
	: >"$events"

	cat >"$hook" <<'EOF'
#!/bin/sh
set -eu
case "$1" in
bound|renew)
  router_addr=$(printf '%s\n' "${router:-}" | awk '{print $1}')
  dns_addr=$(printf '%s\n' "${dns:-}" | awk '{print $1}')
  lease_ip=${ip:-}
  printf '%s %s %s %s %s\n' "$1" "$lease_ip" "$router_addr" "$dns_addr" "${lease:-}" >>"$EVENTS"
  if [ -n "$EXPECTED_IP" ] && [ "$lease_ip" != "$EXPECTED_IP" ]; then
    exit 40
  fi
  [ "$router_addr" = "192.0.2.1" ] || exit 41
  [ "$dns_addr" = "192.0.2.1" ] || exit 42
  ip address replace "$lease_ip/24" dev "$interface"
  ip route replace default via "$router_addr" dev "$interface"
  ;;
esac
EOF
	chmod 755 "$hook"

	EXPECTED_IP="$expected_ip" EVENTS="$events" ip netns exec "$client_ns" \
		"$BUSYBOX" udhcpc -f -n -t 5 -T 2 -i eth0 -s "$hook" >"$client_log" 2>&1 &
	local client_pid=$!
	add_cleanup "if kill -0 '$client_pid' 2>/dev/null; then ip netns pids '$client_ns' | xargs -r kill -TERM; wait '$client_pid' 2>/dev/null || true; fi"
	wait_for 12 grep -q '^bound ' "$events" 2>/dev/null || {
		cat "$client_log" "$SERVER_LOG" >&2
		fail "$name did not complete DHCPv4 acquisition"
	}
	kill -0 "$client_pid" 2>/dev/null || {
		cat "$client_log" >&2
		fail "$name client exited before lease renewal"
	}
}

start_client reserved "$RESERVED_CLIENT" 192.0.2.50
start_client dynamic "$DYNAMIC_CLIENT" ""

grep -Fq 'DHCPOFFER(lan0) 192.0.2.50 02:00:00:00:00:50' "$SERVER_LOG" ||
	fail "reserved client did not receive its configured address"
reserved_lease="$(awk '$1 == "bound" { print $2; exit }' "$WORKDIR/reserved.events")"
dynamic_lease="$(awk '$1 == "bound" { print $2; exit }' "$WORKDIR/dynamic.events")"
[[ "$reserved_lease" == "192.0.2.50" ]] || fail "reserved client received $reserved_lease"
[[ -n "$dynamic_lease" ]] || fail "unreserved client did not receive a lease"
python3 - "$dynamic_lease" <<'PY'
import ipaddress
import sys

address = ipaddress.ip_address(sys.argv[1])
if not ipaddress.ip_address("192.0.2.100") <= address <= ipaddress.ip_address("192.0.2.110"):
    raise SystemExit(f"dynamic lease is outside configured pool: {address}")
PY
grep -Fq "DHCPOFFER(lan0) $dynamic_lease 02:00:00:00:00:51" "$SERVER_LOG" ||
	fail "unreserved client did not receive the observed dynamic lease"
[[ -s "$LEASE_FILE" ]] || fail "dnsmasq did not persist lease state"
ip -n "$RESERVED_CLIENT" -4 -o addr show dev eth0 | grep -Fq '192.0.2.50/24' ||
	fail "reserved address was not installed on the independent client"
ip -n "$DYNAMIC_CLIENT" -4 -o addr show dev eth0 | grep -Fq "$dynamic_lease/24" ||
	fail "dynamic pool address was not installed on the independent client"

old_pid="$(cat "$PID_FILE")"
stop_server
rm -f "$PID_FILE"
start_server
new_pid="$(cat "$PID_FILE")"
[[ "$new_pid" != "$old_pid" ]] || fail "dnsmasq process did not restart"

renewals_seen() {
	local reserved_renewal dynamic_renewal
	reserved_renewal="$(awk '$1 == "renew" { print $2; exit }' "$WORKDIR/reserved.events" 2>/dev/null)"
	dynamic_renewal="$(awk '$1 == "renew" { print $2; exit }' "$WORKDIR/dynamic.events" 2>/dev/null)"
	[[ "$reserved_renewal" == "$reserved_lease" && "$dynamic_renewal" == "$dynamic_lease" ]]
}
wait_for 90 renewals_seen || {
	cat "$WORKDIR/reserved.log" "$WORKDIR/dynamic.log" "$SERVER_LOG" >&2
	fail "both clients did not renew the same leases after dnsmasq restart"
}

printf 'PASS: rendered DHCPv4 reservation and dynamic leases, router/DNS options, dnsmasq restart and client renewal\n'
