#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"
require_common
require_cmd dnsmasq
require_cmd busybox
require_cmd tcpdump

DNSMASQ="$(command -v dnsmasq)"
BUSYBOX="$(command -v busybox)"
RENDERER="${ROUTERD_DHCPV4_RELAY_RENDERER:-}"
if [[ -z "$RENDERER" ]]; then
	require_cmd go
fi

ROUTER="$TEST_ID-router"
SERVER="$TEST_ID-server"
INITIAL_CLIENT="$TEST_ID-initial"
SECOND_CLIENT="$TEST_ID-second"
OUTAGE_CLIENT="$TEST_ID-outage"
WITHDRAWN_CLIENT="$TEST_ID-withdrawn"
RECOVERY_CLIENT="$TEST_ID-recovery"
RESTART_CLIENT="$TEST_ID-restart"
RELAY_CONFIG="$WORKDIR/relay.conf"
WITHDRAWN_CONFIG="$WORKDIR/withdrawn.conf"
SERVER_CONFIG="$WORKDIR/server.conf"
SERVER_LOG="$WORKDIR/server.log"
RELAY_LOG="$WORKDIR/relay.log"
ROUTER_CAPTURE="$WORKDIR/router-dhcp.txt"
SERVER_CAPTURE="$WORKDIR/server-dhcp.txt"
SERVER_PID=""
RELAY_PID=""
CLIENT_PID=""
ROUTER_CAPTURE_PID=""
SERVER_CAPTURE_PID=""
VETH_COUNTER=0

dump_diagnostics() {
	for file in "$RELAY_CONFIG" "$WITHDRAWN_CONFIG" "$SERVER_CONFIG" "$ROUTER_CAPTURE" "$SERVER_CAPTURE" "$RELAY_LOG" "$SERVER_LOG"; do
		if [[ -f "$file" ]]; then
			printf '\n--- %s ---\n' "$file" >&2
			case "$file" in
			*dhcp.txt) tail -n 60 "$file" >&2 ;;
			*.log) tail -n 50 "$file" >&2 ;;
			*) cat "$file" >&2 ;;
			esac
		fi
	done
	for file in "$WORKDIR"/*.log "$WORKDIR"/*.events; do
		if [[ -f "$file" && "$file" != "$RELAY_LOG" && "$file" != "$SERVER_LOG" ]]; then
			printf '\n--- %s ---\n' "$file" >&2
			tail -n 30 "$file" >&2
		fi
	done
	if ip netns list | grep -Fq "$ROUTER"; then
		ip -n "$ROUTER" -s link show >&2 || true
		ip -n "$ROUTER" -4 addr show >&2 || true
	fi
}

render_config() {
	local mode="$1"
	local output="$2"
	if [[ -n "$RENDERER" ]]; then
		"$RENDERER" "$mode" "$WORKDIR/$mode.leases" >"$output"
	else
		(
			cd "$ROOT_DIR"
			go run ./tests/netns/dhcpv4-relay-render "$mode" "$WORKDIR/$mode.leases"
		) >"$output"
	fi
}

create_pair() {
	local ns_a="$1"
	local if_a="$2"
	local ns_b="$3"
	local if_b="$4"
	VETH_COUNTER=$((VETH_COUNTER + 1))
	local host_a="${TEST_IF_ID}v${VETH_COUNTER}a"
	local host_b="${TEST_IF_ID}v${VETH_COUNTER}b"
	ip link add "$host_a" type veth peer name "$host_b"
	add_cleanup "if ip link show '$host_a' >/dev/null 2>&1; then ip link delete '$host_a'; fi"
	ip link set "$host_a" netns "$ns_a"
	ip link set "$host_b" netns "$ns_b"
	ip -n "$ns_a" link set "$host_a" name "$if_a"
	ip -n "$ns_b" link set "$host_b" name "$if_b"
	ip -n "$ns_a" link set "$if_a" up
	ip -n "$ns_b" link set "$if_b" up
}

attach_lan_client() {
	local client_ns="$1"
	local mac="$2"
	local bridge="${3:-lan0}"
	local router_port="lanp$((VETH_COUNTER + 1))"
	create_ns "$client_ns"
	create_pair "$ROUTER" "$router_port" "$client_ns" eth0
	ip -n "$ROUTER" link set "$router_port" master "$bridge"
	ip -n "$client_ns" link set eth0 address "$mac"
}

stop_pid() {
	local pid="$1"
	if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
		kill -TERM "$pid"
		wait "$pid" 2>/dev/null || true
	fi
}

stop_server() {
	stop_pid "$SERVER_PID"
	SERVER_PID=""
}

stop_relay() {
	stop_pid "$RELAY_PID"
	RELAY_PID=""
}

stop_client() {
	stop_pid "$CLIENT_PID"
	CLIENT_PID=""
}

start_server() {
	local instance="$1"
	ip netns exec "$SERVER" "$DNSMASQ" --keep-in-foreground --user=root --group=root \
		--conf-file="$SERVER_CONFIG" --pid-file="$WORKDIR/server-$instance.pid" \
		--log-facility="$SERVER_LOG" &
	SERVER_PID=$!
	wait_for 5 test -s "$WORKDIR/server-$instance.pid" || {
		dump_diagnostics
		fail "upstream DHCP server did not start"
	}
	kill -0 "$SERVER_PID" 2>/dev/null || {
		dump_diagnostics
		fail "upstream DHCP server exited during startup"
	}
}

start_relay() {
	local instance="$1"
	ip netns exec "$ROUTER" "$DNSMASQ" --keep-in-foreground --user=root --group=root \
		--conf-file="$RELAY_CONFIG" --pid-file="$WORKDIR/relay-$instance.pid" \
		--log-facility="$RELAY_LOG" &
	RELAY_PID=$!
	wait_for 5 test -s "$WORKDIR/relay-$instance.pid" || {
		dump_diagnostics
		fail "rendered DHCP relay did not start"
	}
	kill -0 "$RELAY_PID" 2>/dev/null || {
		dump_diagnostics
		fail "DHCP relay exited during startup"
	}
}

client_hook() {
	local name="$1"
	local hook="$WORKDIR/$name-hook.sh"
	cat >"$hook" <<'EOF'
#!/bin/sh
set -eu
case "$1" in
bound|renew)
  router_addr=$(printf '%s\n' "${router:-}" | awk '{print $1}')
  dns_addr=$(printf '%s\n' "${dns:-}" | awk '{print $1}')
  lease_ip=${ip:-}
  server_id=${serverid:-}
  case "$lease_ip" in "$EXPECTED_PREFIX".*) lease_octet=${lease_ip##*.} ;; *) exit 40 ;; esac
  case "$lease_octet" in ''|*[!0-9]*) exit 40 ;; esac
  [ "$lease_octet" -ge "$EXPECTED_FIRST" ] && [ "$lease_octet" -le "$EXPECTED_LAST" ] || exit 40
  [ "$router_addr" = "$EXPECTED_PREFIX.1" ] || exit 41
  [ "$dns_addr" = "$EXPECTED_PREFIX.53" ] || exit 42
  [ "$server_id" = "198.51.100.2" ] || exit 43
  ip address replace "$lease_ip/24" dev "$interface"
  ip route replace default via "$router_addr" dev "$interface"
  printf '%s %s %s %s %s %s\n' "$1" "$lease_ip" "$router_addr" "$dns_addr" "$server_id" "${lease:-}" >>"$EVENTS"
  ;;
esac
EOF
	chmod 755 "$hook"
	printf '%s\n' "$hook"
}

start_client() {
	local name="$1"
	local client_ns="$2"
	local prefix="${3:-192.0.2}"
	local hook
	local events="$WORKDIR/$name.events"
	local log_file="$WORKDIR/$name.log"
	local lease_ip
	: >"$events"
	hook="$(client_hook "$name")"
	EXPECTED_PREFIX="$prefix" EXPECTED_FIRST=100 EXPECTED_LAST=110 EVENTS="$events" ip netns exec "$client_ns" \
		"$BUSYBOX" udhcpc -f -n -t 5 -T 1 -i eth0 -s "$hook" >"$log_file" 2>&1 &
	CLIENT_PID=$!
	add_cleanup "stop_pid '$CLIENT_PID'"
	wait_for 15 grep -q '^bound ' "$events" 2>/dev/null || {
		dump_diagnostics
		fail "$name did not complete DHCPv4 acquisition with the expected options"
	}
	kill -0 "$CLIENT_PID" 2>/dev/null || {
		dump_diagnostics
		fail "$name client exited before its lease was checked"
	}
	lease_ip="$(awk '$1 == "bound" { print $2; exit }' "$events")"
	grep -Fq "$lease_ip/24" < <(ip -n "$client_ns" -4 -o addr show dev eth0) || {
		dump_diagnostics
		fail "$name did not install the relayed address on its independent client"
	}
}

expect_no_lease() {
	local name="$1"
	local client_ns="$2"
	local mac="$3"
	local events="$WORKDIR/$name.events"
	local log_file="$WORKDIR/$name.log"
	local hook
	: >"$events"
	hook="$(client_hook "$name")"
	EXPECTED_PREFIX=192.0.2 EXPECTED_FIRST=100 EXPECTED_LAST=110 EVENTS="$events" ip netns exec "$client_ns" \
		"$BUSYBOX" udhcpc -f -n -t 3 -T 1 -i eth0 -s "$hook" >"$log_file" 2>&1 &
	local client_pid=$!
	add_cleanup "stop_pid '$client_pid'"
	client_exited() { ! kill -0 "$client_pid" 2>/dev/null; }
	if ! wait_for 12 client_exited; then
		stop_pid "$client_pid"
		dump_diagnostics
		fail "$name DHCP client exceeded its bounded retry window"
	fi
	if wait "$client_pid"; then
		dump_diagnostics
		fail "$name obtained a lease when no relay/server response was expected"
	fi
	[[ ! -s "$events" ]] || {
		dump_diagnostics
		fail "$name unexpectedly recorded a DHCP lease"
	}
	if [[ -n "$mac" ]] && grep -Fq "$mac" "$SERVER_LOG"; then
		dump_diagnostics
		fail "$name DHCP request reached the upstream server without an active relay"
	fi
}

add_cleanup stop_relay
add_cleanup stop_server
: >"$SERVER_LOG"
: >"$RELAY_LOG"

create_ns "$ROUTER"
create_ns "$SERVER"
ip -n "$ROUTER" link add lan0 type bridge
ip -n "$ROUTER" addr add 192.0.2.1/24 dev lan0
ip -n "$ROUTER" link set lan0 up
ip -n "$ROUTER" link add lan1 type bridge
ip -n "$ROUTER" addr add 198.18.2.1/24 dev lan1
ip -n "$ROUTER" link set lan1 up
create_pair "$ROUTER" wan0 "$SERVER" eth0
ip -n "$ROUTER" addr add 198.51.100.1/24 dev wan0
ip -n "$SERVER" addr add 198.51.100.2/24 dev eth0

ip netns exec "$ROUTER" timeout -s INT 60 tcpdump -ni any -s 512 -c 80 -vv -l 'udp port 67 or udp port 68' >"$ROUTER_CAPTURE" 2>&1 &
ROUTER_CAPTURE_PID=$!
add_cleanup "stop_pid '$ROUTER_CAPTURE_PID'"
wait_for 5 grep -q 'listening on' "$ROUTER_CAPTURE" 2>/dev/null || {
	dump_diagnostics
	fail "router DHCP packet capture did not start"
}
ip netns exec "$SERVER" timeout -s INT 60 tcpdump -ni any -s 512 -c 80 -vv -l 'udp port 67 or udp port 68' >"$SERVER_CAPTURE" 2>&1 &
SERVER_CAPTURE_PID=$!
add_cleanup "stop_pid '$SERVER_CAPTURE_PID'"
wait_for 5 grep -q 'listening on' "$SERVER_CAPTURE" 2>/dev/null || {
	dump_diagnostics
	fail "upstream DHCP packet capture did not start"
}

attach_lan_client "$INITIAL_CLIENT" 02:00:00:00:00:51
attach_lan_client "$SECOND_CLIENT" 02:00:00:00:00:56 lan1
attach_lan_client "$OUTAGE_CLIENT" 02:00:00:00:00:52
attach_lan_client "$WITHDRAWN_CLIENT" 02:00:00:00:00:53
attach_lan_client "$RECOVERY_CLIENT" 02:00:00:00:00:54
attach_lan_client "$RESTART_CLIENT" 02:00:00:00:00:55

render_config relay "$RELAY_CONFIG"
printf 'log-dhcp\n' >>"$RELAY_CONFIG"
render_config withdrawn "$WITHDRAWN_CONFIG"
if grep -q '^dhcp-relay=' "$WITHDRAWN_CONFIG"; then
	fail "withdrawn DHCPv4Relay still rendered a relay directive"
fi
grep -Fq 'dhcp-relay=192.0.2.1,198.51.100.2' "$RELAY_CONFIG" || {
	dump_diagnostics
	fail "renderer did not use the relay interface address and configured server"
}
cat >"$SERVER_CONFIG" <<EOF
port=0
no-resolv
no-hosts
bind-interfaces
interface=eth0
dhcp-authoritative
dhcp-range=set:first,192.0.2.100,192.0.2.110,255.255.255.0,2m
dhcp-option=tag:first,option:router,192.0.2.1
dhcp-option=tag:first,option:dns-server,192.0.2.53
dhcp-range=set:second,198.18.2.100,198.18.2.110,255.255.255.0,2m
dhcp-option=tag:second,option:router,198.18.2.1
dhcp-option=tag:second,option:dns-server,198.18.2.53
dhcp-leasefile=$WORKDIR/server.leases
log-dhcp
EOF

dnsmasq --test --conf-file="$SERVER_CONFIG" >"$WORKDIR/server-syntax.log" 2>&1 || {
	dump_diagnostics
	fail "dnsmasq rejected the upstream DHCP server configuration"
}
dnsmasq --test --conf-file="$RELAY_CONFIG" >"$WORKDIR/relay-syntax.log" 2>&1 || {
	dump_diagnostics
	fail "dnsmasq rejected the rendered relay configuration"
}
dnsmasq --test --conf-file="$WITHDRAWN_CONFIG" >"$WORKDIR/withdrawn-syntax.log" 2>&1 || {
	dump_diagnostics
	fail "dnsmasq rejected the configuration after relay withdrawal"
}

start_server 1
start_relay 1
start_client initial "$INITIAL_CLIENT"
if ! grep -Fq 'DHCPDISCOVER(eth0) 02:00:00:00:00:51' "$SERVER_LOG" || ! grep -Fq 'DHCPOFFER(eth0) 192.0.2.' "$SERVER_LOG"; then
	dump_diagnostics
	fail "upstream server did not offer the relayed address to the independent client"
fi
stop_client
start_client second-lan "$SECOND_CLIENT" 198.18.2
stop_client

stop_server
expect_no_lease upstream-outage "$OUTAGE_CLIENT" 02:00:00:00:00:52
start_server 2
start_client server-recovery "$RECOVERY_CLIENT"
stop_client

stop_relay
expect_no_lease relay-withdrawal "$WITHDRAWN_CLIENT" 02:00:00:00:00:53
start_relay 2
start_client relay-restart "$RESTART_CLIENT"
stop_client

printf 'PASS: independent DHCPv4 client/server exchange on two subnets through one rendered relay; server identity/options, upstream failure/recovery, and relay withdrawal/restart\n'
