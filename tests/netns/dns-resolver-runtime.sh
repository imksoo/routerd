#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"
require_common
require_cmd dnsmasq
require_cmd dig
require_cmd curl
require_cmd go

DNSMASQ="$(command -v dnsmasq)"
DIG="$(command -v dig)"
CURL="$(command -v curl)"
ROUTER="$TEST_ID-router"
CLIENT="$TEST_ID-client"
UPSTREAM="$TEST_ID-upstream"
CONFIG="$WORKDIR/resolver.json"
SOCKET="$WORKDIR/resolver.sock"
STATE_FILE="$WORKDIR/resolver-state.json"
EVENT_FILE="$WORKDIR/resolver-events.jsonl"
UPSTREAM_LOG="$WORKDIR/upstream.log"
WORKER_LOG="$WORKDIR/resolver.log"
WORKER_PID=""
UPSTREAM_PID=""
VETH_COUNTER=0

create_ns "$ROUTER"
create_ns "$CLIENT"
create_ns "$UPSTREAM"

ip -n "$ROUTER" link add lan0 type bridge
ip -n "$ROUTER" addr add 192.0.2.1/24 dev lan0
ip -n "$ROUTER" link set lan0 up

attach_client() {
	local client_ns="$1"
	local client_address="$2"
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
	ip -n "$client_ns" addr add "$client_address/24" dev eth0
	ip -n "$client_ns" link set eth0 up
}

attach_client "$CLIENT" 192.0.2.2
attach_client "$UPSTREAM" 198.51.100.53
ip -n "$ROUTER" addr add 198.51.100.1/24 dev lan0

(
	cd "$ROOT_DIR"
	go build -o "$WORKDIR/routerd-dns-resolver" ./cmd/routerd-dns-resolver
)

render_config() {
	(
		cd "$ROOT_DIR"
		go run ./tests/netns/dns-resolver-runtime-render "$1"
	) >"$CONFIG"
}

start_upstream() {
	ip netns exec "$UPSTREAM" "$DNSMASQ" --keep-in-foreground --user=root --conf-file=/dev/null --no-resolv --no-hosts --no-poll --interface=eth0 --bind-interfaces --port=53 --address=/example/198.51.100.53 --address=/udp.example/203.0.113.80 --address=/tcp.example/203.0.113.81 --log-queries --log-facility="$UPSTREAM_LOG" >"$WORKDIR/upstream.stderr" 2>&1 &
	UPSTREAM_PID=$!
	add_cleanup stop_upstream
}

stop_upstream() {
	if [[ -n "$UPSTREAM_PID" ]] && kill -0 "$UPSTREAM_PID" 2>/dev/null; then
		kill -TERM "$UPSTREAM_PID"
		wait "$UPSTREAM_PID" 2>/dev/null || true
	fi
	UPSTREAM_PID=""
}

worker_healthy() {
	[[ "$("$CURL" -fsS --unix-socket "$SOCKET" http://localhost/v1/healthz 2>/dev/null)" == "ok" ]]
}

start_worker() {
	ip netns exec "$ROUTER" "$WORKDIR/routerd-dns-resolver" daemon --resource lan --config-file="$CONFIG" --socket="$SOCKET" --state-file="$STATE_FILE" --event-file="$EVENT_FILE" >"$WORKER_LOG" 2>&1 &
	WORKER_PID=$!
	wait_for 10 worker_healthy || {
		cat "$WORKER_LOG" "$UPSTREAM_LOG" 2>/dev/null >&2 || true
		fail "routerd DNS resolver did not become healthy"
	}
}

stop_worker() {
	if [[ -n "$WORKER_PID" ]] && kill -0 "$WORKER_PID" 2>/dev/null; then
		kill -TERM "$WORKER_PID"
		wait "$WORKER_PID" 2>/dev/null || true
	fi
	WORKER_PID=""
}

add_cleanup stop_worker
start_upstream

upstream_responds() {
	[[ "$(ip netns exec "$ROUTER" "$DIG" +time=1 +tries=1 +short @198.51.100.53 dns.example A 2>/dev/null)" == "198.51.100.53" ]]
}
wait_for 5 upstream_responds || {
	ip -n "$ROUTER" -brief addr show >&2 || true
	ip -n "$ROUTER" route show >&2 || true
	ip -n "$UPSTREAM" -brief addr show >&2 || true
	ip netns exec "$ROUTER" "$DIG" +time=1 +tries=1 @198.51.100.53 dns.example A >&2 || true
	cat "$WORKDIR/upstream.stderr" "$UPSTREAM_LOG" 2>/dev/null >&2 || true
	fail "isolated DNS upstream did not answer its health probe"
}

render_config 192.0.2.1
start_worker

check_answer() {
	local transport="$1"
	local qname="$2"
	local expected="$3"
	local actual
	if [[ "$transport" == tcp ]]; then
		actual="$(ip netns exec "$CLIENT" "$DIG" +tcp +time=2 +tries=1 +short @192.0.2.1 "$qname" A 2>"$WORKDIR/dig.log")" || {
			cat "$WORKDIR/dig.log" "$WORKER_LOG" "$UPSTREAM_LOG" 2>/dev/null >&2 || true
			return 1
		}
	else
		actual="$(ip netns exec "$CLIENT" "$DIG" +time=2 +tries=1 +short @192.0.2.1 "$qname" A 2>"$WORKDIR/dig.log")" || {
			cat "$WORKDIR/dig.log" "$WORKER_LOG" "$UPSTREAM_LOG" 2>/dev/null >&2 || true
			return 1
		}
	fi
	[[ "$actual" == "$expected" ]] || {
		printf 'DNS %s %s answer=%q want=%q\n' "$transport" "$qname" "$actual" "$expected" >&2
		return 1
	}
}

check_answer udp router.home.arpa 192.0.2.1 ||
	fail "independent UDP client did not receive the local-zone answer"
check_answer tcp router.home.arpa 192.0.2.1 ||
	fail "independent TCP client did not receive the local-zone answer"
check_answer udp host.udp.example 203.0.113.80 ||
	fail "independent client did not receive the UDP-forwarded answer"
check_answer tcp host.tcp.example 203.0.113.81 ||
	fail "independent TCP client did not receive the TCP-forwarded answer"

render_config 192.0.2.10
kill -HUP "$WORKER_PID"
wait_for 10 check_answer udp router.home.arpa 192.0.2.10 ||
	fail "runtime config reload did not replace the local-zone answer"
check_answer tcp router.home.arpa 192.0.2.10 ||
	fail "TCP listener did not use the reloaded local-zone answer"

old_pid="$WORKER_PID"
stop_worker
start_worker
[[ "$WORKER_PID" != "$old_pid" ]] || fail "resolver worker did not restart"
check_answer udp router.home.arpa 192.0.2.10 ||
	fail "local-zone answer did not recover after worker restart"
check_answer tcp host.tcp.example 203.0.113.81 ||
	fail "TCP upstream answer did not recover after worker restart"

printf 'PASS: actual DNS resolver local and forwarded UDP/TCP answers, reload and restart recovery\n'
