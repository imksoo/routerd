#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"
require_common
require_cmd nft
require_cmd dig
require_cmd dnsmasq
require_cmd go
require_cmd unshare
require_cmd mount

ROUTER="$TEST_ID-router"
WAN="$TEST_ID-wan"
LAN0="$TEST_ID-lan0"
LAN1="$TEST_ID-lan1"
DNSMASQ="$(command -v dnsmasq)"

create_ns "$ROUTER"
create_ns "$WAN"
create_ns "$LAN0"
create_ns "$LAN1"

create_veth_pair "$ROUTER" wan0 198.18.16.2/24 "$WAN" eth0 198.18.16.53/24
ip -n "$ROUTER" -6 addr add fd42:16::2/64 dev wan0
ip -n "$WAN" addr add 198.18.16.54/24 dev eth0
ip -n "$WAN" -6 addr add fd42:16::53/64 dev eth0
ip -n "$WAN" -6 addr add fd42:16::54/64 dev eth0

create_veth_pair "$ROUTER" lan0 198.18.15.1/24 "$LAN0" eth0 198.18.15.101/24
ip -n "$ROUTER" -6 addr add fd42:15::1/64 dev lan0
ip -n "$LAN0" -6 addr add fd42:15::101/64 dev eth0

create_veth_pair "$ROUTER" lan1 198.18.17.1/24 "$LAN1" eth0 198.18.17.101/24
ip -n "$ROUTER" -6 addr add fd42:17::1/64 dev lan1
ip -n "$LAN1" -6 addr add fd42:17::101/64 dev eth0

ip -n "$LAN0" route add default via 198.18.15.1
ip -n "$LAN1" route add default via 198.18.17.1
ip -n "$LAN0" -6 route add default via fd42:15::1
ip -n "$LAN1" -6 route add default via fd42:17::1
ip -n "$WAN" route add 198.18.15.0/24 via 198.18.16.2
ip -n "$WAN" route add 198.18.17.0/24 via 198.18.16.2
ip -n "$WAN" -6 route add fd42:15::/64 via fd42:16::2
ip -n "$WAN" -6 route add fd42:17::/64 via fd42:16::2

ip netns exec "$ROUTER" sysctl -qw net.ipv4.ip_forward=1
ip netns exec "$ROUTER" sysctl -qw net.ipv6.conf.all.forwarding=1

(
	cd "$ROOT_DIR"
	go build -o "$WORKDIR/routerd" ./cmd/routerd
	go build -o "$WORKDIR/nat44-controller" ./tests/netns/nat44-portforward-runtime
	go build -o "$WORKDIR/ipaddressset-refresh" ./tests/netns/ipaddressset-refresh
)
CONFIG="$SCRIPT_DIR/local-service-redirect-runtime/config.yaml"
"$WORKDIR/routerd" validate --config "$CONFIG"
SELECTED_HOSTS="$WORKDIR/selected.hosts"
printf '198.18.16.53 selected.test\nfd42:16::53 selected.test\n' >"$SELECTED_HOSTS"

start_dnsmasq() {
	local ns="$1" label="$2" port="$3" answer4="$4" answer6="$5"
	local -a interfaces=("${@:6}")
	local -a args=(
		--no-daemon
		--no-resolv
		--no-hosts
		--bind-interfaces
		--domain-needed
		--port="$port"
		--address="/probe.test/$answer4"
		--address="/probe.test/$answer6"
		--addn-hosts="$SELECTED_HOSTS"
		--log-facility="$WORKDIR/$label.log"
		--pid-file="$WORKDIR/$label.pid"
	)
	local interface
	for interface in "${interfaces[@]}"; do
		args+=("--interface=$interface")
	done
	ip netns exec "$ns" "$DNSMASQ" "${args[@]}" >"$WORKDIR/$label.stderr" 2>&1 &
	local pid=$!
	LAST_DNSMASQ_PID="$pid"
	add_cleanup "if kill -0 '$pid' 2>/dev/null; then kill '$pid'; fi"
}

start_dnsmasq "$ROUTER" local-dns 53 192.0.2.11 2001:db8::11 lan0 lan1
start_dnsmasq "$WAN" wan-dns 53 192.0.2.22 2001:db8::22 eth0
WAN_DNSMASQ_PID="$LAST_DNSMASQ_PID"
start_dnsmasq "$WAN" wan-dns-alt 5354 192.0.2.22 2001:db8::22 eth0

dns_query() {
	local ns="$1" family="$2" transport="$3" server="$4" port="$5"
	local -a args=(+time=2 +tries=1 +short)
	args+=("$family")
	[[ "$transport" == "tcp" ]] && args+=(+tcp)
	ip netns exec "$ns" dig "${args[@]}" "@$server" -p "$port" probe.test
}

expect_answer() {
	local ns="$1" family="$2" transport="$3" server="$4" port="$5" want="$6" label="$7"
	local actual
	if ! actual="$(dns_query "$ns" "$family" "$transport" "$server" "$port" 2>"$WORKDIR/query.stderr")"; then
		cat "$WORKDIR/query.stderr" >&2 || true
		fail "$label: DNS query failed"
	fi
	if ! grep -Fxq "$want" <<<"$actual"; then
		fail "$label: answer=$actual want=$want"
	fi
}

wait_for 10 bash -c "ip netns exec '$LAN0' dig +time=1 +tries=1 +short @198.18.16.53 probe.test A | grep -Fxq 192.0.2.22" || {
	cat "$WORKDIR/wan-dns.stderr" "$WORKDIR/wan-dns.log" >&2 || true
	fail "upstream DNS did not answer before redirect was applied"
}
wait_for 10 bash -c "ip netns exec '$LAN0' dig +time=1 +tries=1 +short @198.18.15.1 probe.test A | grep -Fxq 192.0.2.11" || {
	cat "$WORKDIR/local-dns.stderr" "$WORKDIR/local-dns.log" >&2 || true
	fail "local DNS responder did not answer directly"
}
wait_for 10 bash -c "ip netns exec '$LAN0' dig +time=1 +tries=1 +short @fd42:16::54 probe.test AAAA | grep -Fxq 2001:db8::22" || {
	cat "$WORKDIR/wan-dns.stderr" "$WORKDIR/wan-dns.log" >&2 || true
	fail "upstream IPv6 DNS did not answer before redirect was applied"
}

apply_config() {
	local config="$1" label="$2"
	ip netns exec "$ROUTER" "$WORKDIR/nat44-controller" "$config" "$WORKDIR/$label.nft" >"$WORKDIR/$label.apply.log" 2>&1 || {
		cat "$WORKDIR/$label.apply.log" >&2 || true
		fail "production NAT controller failed to apply $label"
	}
}

apply_config "$CONFIG" initial
ip netns exec "$ROUTER" nft list table ip routerd_nat >"$WORKDIR/ipv4-active.txt"
ip netns exec "$ROUTER" nft list table ip6 routerd_nat >"$WORKDIR/ipv6-active.txt"

for transport in udp tcp; do
	expect_answer "$LAN0" A "$transport" 198.18.16.53 53 192.0.2.11 "lan0 IPv4 $transport selected destination"
	expect_answer "$LAN0" AAAA "$transport" fd42:16::53 53 2001:db8::11 "lan0 IPv6 $transport selected destination"
	expect_answer "$LAN0" A "$transport" 198.18.16.54 53 192.0.2.22 "lan0 IPv4 $transport unselected destination"
	expect_answer "$LAN0" AAAA "$transport" fd42:16::54 53 2001:db8::22 "lan0 IPv6 $transport unselected destination"
	expect_answer "$LAN1" A "$transport" 198.18.16.53 53 192.0.2.22 "lan1 IPv4 $transport interface mismatch"
	expect_answer "$LAN1" AAAA "$transport" fd42:16::53 53 2001:db8::22 "lan1 IPv6 $transport interface mismatch"
done
expect_answer "$LAN0" A udp 198.18.16.53 5354 192.0.2.22 "destination port mismatch"

sed 's/^        interface: lan$/        interface: lan2/' "$CONFIG" >"$WORKDIR/lan1.yaml"
"$WORKDIR/routerd" validate --config "$WORKDIR/lan1.yaml"
apply_config "$WORKDIR/lan1.yaml" switched
for transport in udp tcp; do
	expect_answer "$LAN0" A "$transport" 198.18.16.53 53 192.0.2.22 "switched rule lan0 IPv4 $transport"
	expect_answer "$LAN0" AAAA "$transport" fd42:16::53 53 2001:db8::22 "switched rule lan0 IPv6 $transport"
	expect_answer "$LAN1" A "$transport" 198.18.16.53 53 192.0.2.11 "switched rule lan1 IPv4 $transport"
	expect_answer "$LAN1" AAAA "$transport" fd42:16::53 53 2001:db8::11 "switched rule lan1 IPv6 $transport"
done

# Resolve the production name-backed set through the independent WAN server.
# A private mount namespace supplies resolv.conf only to this controller call.
sed '/        addresses:/,+2c\        names: [selected.test]\n        refreshInterval: 1s' "$CONFIG" >"$WORKDIR/named.yaml"
"$WORKDIR/routerd" validate --config "$WORKDIR/named.yaml"
printf 'nameserver 198.18.16.54\n' >"$WORKDIR/resolv.conf"
refresh_named_set() {
	ip netns exec "$ROUTER" unshare --mount --propagation private sh -ec '
		mount --bind "$1" /etc/resolv.conf
		exec "$2" "$3" "$4"
	' sh "$WORKDIR/resolv.conf" "$WORKDIR/ipaddressset-refresh" "$WORKDIR/named.yaml" "$WORKDIR"
}
apply_config "$WORKDIR/named.yaml" named
refresh_named_set
for transport in udp tcp; do
	expect_answer "$LAN0" A "$transport" 198.18.16.53 53 192.0.2.11 "resolved IPv4 $transport"
	expect_answer "$LAN0" AAAA "$transport" fd42:16::53 53 2001:db8::11 "resolved IPv6 $transport"
done

printf '198.18.16.54 selected.test\nfd42:16::54 selected.test\n' >"$SELECTED_HOSTS"
kill -HUP "$WAN_DNSMASQ_PID"
wait_for 10 bash -c "ip netns exec '$ROUTER' dig +time=1 +tries=1 +short @198.18.16.54 selected.test A | grep -Fxq 198.18.16.54" || fail "independent DNS answer did not change"
# Use the configured refresh interval and persisted status, without editing it
# or recreating NAT tables to force an update.
sleep 1.1
refresh_named_set
for transport in udp tcp; do
	expect_answer "$LAN0" A "$transport" 198.18.16.53 53 192.0.2.22 "old resolved IPv4 $transport retired"
	expect_answer "$LAN0" AAAA "$transport" fd42:16::53 53 2001:db8::22 "old resolved IPv6 $transport retired"
	expect_answer "$LAN0" A "$transport" 198.18.16.54 53 192.0.2.11 "new resolved IPv4 $transport selected"
	expect_answer "$LAN0" AAAA "$transport" fd42:16::54 53 2001:db8::11 "new resolved IPv6 $transport selected"
done

cat >"$WORKDIR/empty.yaml" <<'YAML'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: local-service-redirect-runtime
spec: {}
YAML
"$WORKDIR/routerd" validate --config "$WORKDIR/empty.yaml"
apply_config "$WORKDIR/empty.yaml" withdrawn
for family in ip ip6; do
	if ip netns exec "$ROUTER" nft list table "$family" routerd_nat >/dev/null 2>&1; then
		fail "routerd NAT table remained after LocalServiceRedirect withdrawal ($family)"
	fi
done
for transport in udp tcp; do
	expect_answer "$LAN0" A "$transport" 198.18.16.53 53 192.0.2.22 "withdrawn rule lan0 IPv4 $transport"
	expect_answer "$LAN0" AAAA "$transport" fd42:16::53 53 2001:db8::22 "withdrawn rule lan0 IPv6 $transport"
	expect_answer "$LAN1" A "$transport" 198.18.16.53 53 192.0.2.22 "withdrawn rule lan1 IPv4 $transport"
	expect_answer "$LAN1" AAAA "$transport" fd42:16::53 53 2001:db8::22 "withdrawn rule lan1 IPv6 $transport"
done

printf 'PASS: production LocalServiceRedirect and IPAddressSet, IPv4/IPv6 TCP/UDP, destination/interface/port matching, interface update, DNS answer refresh and withdrawal\n'
