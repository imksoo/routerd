#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause
# Disposable Linux VM only: packaged routerd daemon, systemd-owned dnsmasq, and independent DHCP peers.
set -Eeuo pipefail
on_error() {
	local rc=$?
	printf 'FAIL: line %s: %s (exit %s)\n' "$LINENO" "$BASH_COMMAND" "$rc" >&2
	exit "$rc"
}
trap on_error ERR

if [[ "${ROUTERD_DHCPV4_RELAY_TIMEOUT_CHILD:-}" != 1 ]]; then
	command -v timeout >/dev/null || { echo 'missing required command: timeout' >&2; exit 1; }
	export ROUTERD_DHCPV4_RELAY_TIMEOUT_CHILD=1
	exec timeout --foreground --kill-after=5s 180s "$0" "$@"
fi

if [[ "${1:-}" != --disposable-vm || "$#" -ne 2 ]]; then
	echo "usage: sudo $0 --disposable-vm EXTRACTED_PACKAGE_ROOT" >&2
	echo 'Runs a fresh routerd 1.5 relay config through the real daemon and systemd; does not upgrade or import old state.' >&2
	exit 2
fi
package_root=${2:?extracted package root}
routerd=$package_root/bin/routerd
routerctl=$package_root/bin/routerctl
dnsmasq=$(command -v dnsmasq || true)
busybox=$(command -v busybox || true)
for command in ip systemctl systemd-detect-virt pgrep sha256sum journalctl readlink; do
	command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }
done
[[ -n "$dnsmasq" ]] || { echo 'missing required command: dnsmasq' >&2; exit 1; }
[[ -n "$busybox" ]] || { echo 'missing required command: busybox' >&2; exit 1; }
[[ -x "$routerd" && -x "$routerctl" ]] || { echo 'package root must contain bin/routerd and bin/routerctl' >&2; exit 1; }
systemd-detect-virt --vm --quiet || { echo 'run only inside a disposable VM' >&2; exit 1; }
[[ "$(id -u)" -eq 0 ]] || { echo "run explicitly with sudo: sudo $0 --disposable-vm EXTRACTED_PACKAGE_ROOT" >&2; exit 1; }

test_id="rd15r${BASHPID}"
lan_bridge="${test_id}b"
wan_if="${test_id}w"
lan_port="${test_id}p"
client_peer="${test_id}c"
server_peer="${test_id}s"
client_ns="${test_id}-client"
upstream_ns="${test_id}-upstream"
unit_file=/etc/systemd/system/routerd-dnsmasq.service
activation_file=${unit_file}.routerd-activation.json
unit_wants_link=/etc/systemd/system/multi-user.target.wants/routerd-dnsmasq.service
lease_dir=/var/lib/routerd/dnsmasq
lease_file=$lease_dir/dnsmasq.leases
dnsmasq_pid_file=/run/routerd/dnsmasq.pid
dnsmasq_owner_file=${dnsmasq_pid_file}.routerd-owner.json

for unit in routerd.service dnsmasq.service routerd-dnsmasq.service; do
	if systemctl is-active --quiet "$unit"; then
		echo "refusing to overlap active service: $unit" >&2
		exit 1
	fi
done
[[ "$(systemctl show -p LoadState --value routerd-dnsmasq.service)" == not-found ]] || {
	echo 'routerd-dnsmasq.service is already known to systemd' >&2
	exit 1
}
if pgrep -x routerd >/dev/null || pgrep -x dnsmasq >/dev/null; then
	echo 'refusing to overlap an existing routerd or dnsmasq process' >&2
	exit 1
fi
for path in /run/routerd /var/lib/routerd "$unit_file" "$activation_file" "$unit_wants_link"; do
	if [[ -e "$path" || -L "$path" ]]; then
		echo "refusing to overwrite existing routerd state: $path" >&2
		exit 1
	fi
done
for ns in "$client_ns" "$upstream_ns"; do
	if ip netns list | awk '{print $1}' | grep -Fxq "$ns"; then
		echo "namespace already exists: $ns" >&2
		exit 1
	fi
done
for dev in "$lan_bridge" "$wan_if" "$lan_port" "$client_peer" "$server_peer"; do
	if ip link show dev "$dev" >/dev/null 2>&1; then
		echo "interface already exists: $dev" >&2
		exit 1
	fi
done
for prefix in 192.0.2.0/24 198.51.100.0/24; do
	if [[ -n "$(ip -4 route show exact "$prefix")" ]]; then
		echo "test subnet already has a host route: $prefix" >&2
		exit 1
	fi
done

work=$(mktemp -d /var/tmp/routerd15-dhcpv4-relay.XXXXXX)
chmod 0755 "$work"
router_socket=$work/routerd.sock
status_socket=$work/routerd-status.sock
router_config=$work/router-config.yaml
relay_input=$work/relay-input.yaml
routerd_pid=0
upstream_pid=0
client_pid=0
client_ns_created=0
upstream_ns_created=0
bridge_created=0
lan_pair_created=0
wan_pair_created=0
dnsmasq_unit_owned=0
run_start=$(date --iso-8601=seconds)

stop_pid() {
	local pid=${1:-0}
	if [[ "$pid" -gt 0 ]] && kill -0 "$pid" 2>/dev/null; then
		kill -TERM "$pid" 2>/dev/null || true
		for _ in {1..30}; do
			kill -0 "$pid" 2>/dev/null || break
			sleep 0.1
		done
		if kill -0 "$pid" 2>/dev/null; then
			kill -KILL "$pid" 2>/dev/null || true
		fi
		wait "$pid" 2>/dev/null || true
	fi
}

cleanup() {
	local test_exit=$? cleanup_exit=0
	trap - EXIT ERR INT TERM
	set +e
	stop_pid "$client_pid"
	stop_pid "$upstream_pid"
	if [[ "$routerd_pid" -gt 0 ]] && kill -0 "$routerd_pid" 2>/dev/null; then
		if [[ -S "$router_socket" ]]; then
			if "$routerctl" apply --socket "$router_socket" -f "$work/empty.yaml" --replace >"$work/cleanup-apply.log" 2>&1; then
				# Let the product finish withdrawal before fallback cleanup touches its unit.
				local withdrawn=0
				for _ in {1..100}; do
					if [[ ! -e "$unit_file" && ! -e "$activation_file" && ! -L "$unit_wants_link" && ! -e "$dnsmasq_pid_file" && ! -e "$dnsmasq_owner_file" ]] &&
					   [[ "$(systemctl show -p LoadState --value routerd-dnsmasq.service 2>/dev/null)" == not-found ]]; then
						withdrawn=1
						dnsmasq_unit_owned=0
						break
					fi
					sleep 0.2
				done
				if [[ "$withdrawn" -ne 1 ]]; then
					echo 'cleanup failed: product withdrawal did not finish within 20 seconds' >&2
					cleanup_exit=1
				fi
			else
				echo 'cleanup warning: routerctl could not withdraw the test config' >&2
				cleanup_exit=1
			fi
		fi
		stop_pid "$routerd_pid"
		routerd_pid=0
	fi
	if [[ "$dnsmasq_unit_owned" -eq 1 ]]; then
		if [[ -f "$unit_file" ]] && grep -Fq "$work/dnsmasq.conf" "$unit_file"; then
			systemctl stop routerd-dnsmasq.service >/dev/null 2>&1 || cleanup_exit=1
			systemctl disable routerd-dnsmasq.service >/dev/null 2>&1 || cleanup_exit=1
			rm -f -- "$unit_file" "$activation_file" || cleanup_exit=1
			if [[ -L "$unit_wants_link" ]] && [[ "$(readlink -f "$unit_wants_link")" == "$unit_file" ]]; then
				rm -f -- "$unit_wants_link" || cleanup_exit=1
			fi
			systemctl daemon-reload >/dev/null 2>&1 || cleanup_exit=1
		elif [[ -e "$unit_file" || -L "$unit_file" ]]; then
			echo 'cleanup failed: dnsmasq unit no longer identifies this test config' >&2
			cleanup_exit=1
		elif [[ -e "$activation_file" ]]; then
			rm -f -- "$activation_file" || cleanup_exit=1
			systemctl daemon-reload >/dev/null 2>&1 || cleanup_exit=1
		fi
	fi
	if [[ "$client_ns_created" -eq 1 ]]; then
		ip netns delete "$client_ns" >/dev/null 2>&1 || cleanup_exit=1
	fi
	if [[ "$upstream_ns_created" -eq 1 ]]; then
		ip netns delete "$upstream_ns" >/dev/null 2>&1 || cleanup_exit=1
	fi
	if [[ "$lan_pair_created" -eq 1 ]] && ip link show dev "$lan_port" >/dev/null 2>&1; then
		ip link delete dev "$lan_port" >/dev/null 2>&1 || cleanup_exit=1
	fi
	if [[ "$wan_pair_created" -eq 1 ]] && ip link show dev "$wan_if" >/dev/null 2>&1; then
		ip link delete dev "$wan_if" >/dev/null 2>&1 || cleanup_exit=1
	fi
	if [[ "$bridge_created" -eq 1 ]] && ip link show dev "$lan_bridge" >/dev/null 2>&1; then
		ip link delete dev "$lan_bridge" >/dev/null 2>&1 || cleanup_exit=1
	fi
	if [[ -e "$lease_file" ]]; then
		rm -f -- "$lease_file" || cleanup_exit=1
	fi
	rmdir "$lease_dir" 2>/dev/null || true
	rmdir /var/lib/routerd 2>/dev/null || true
	if [[ -d /run/routerd ]]; then
		if pgrep -x routerd >/dev/null; then
			echo 'cleanup failed: routerd still owns native-operation locks' >&2
			cleanup_exit=1
		else
			shopt -s nullglob
			local lock_file
			for lock_file in /run/routerd/native-operation-*.lock; do
				rm -f -- "$lock_file" || cleanup_exit=1
			done
			shopt -u nullglob
		fi
	fi
	rmdir /run/routerd 2>/dev/null || true
	for path in "$unit_file" "$activation_file" "$unit_wants_link" "$lease_file" "$dnsmasq_pid_file" "$dnsmasq_owner_file" /run/routerd /var/lib/routerd; do
		if [[ -e "$path" || -L "$path" ]]; then
			echo "cleanup failed: test artifact remains: $path" >&2
			cleanup_exit=1
		fi
	done
	if [[ "$test_exit" -eq 0 && "$cleanup_exit" -eq 0 ]]; then
		rm -r -- "$work"
		printf 'PASS: routerd DHCPv4 relay served independent clients through systemd dnsmasq; upstream loss/recovery, relay withdrawal/reapply, and dnsmasq restart\n'
		return 0
	fi
	printf 'FAIL: test_exit=%s cleanup_exit=%s; bounded diagnostics at %s\n' "$test_exit" "$cleanup_exit" "$work" >&2
	if [[ -f "$work/routerd.log" ]]; then tail -n 80 "$work/routerd.log" >&2; fi
	if [[ -n "${upstream_log:-}" && -f "$upstream_log" ]]; then tail -n 60 "$upstream_log" >&2; fi
	if [[ -f "$work/client.log" ]]; then tail -n 60 "$work/client.log" >&2; fi
	if [[ -f "$work/dnsmasq.conf" ]]; then cat "$work/dnsmasq.conf" >&2; fi
	journalctl -u routerd-dnsmasq.service --since "$run_start" --no-pager -n 80 >&2 || true
	if [[ "$test_exit" -ne 0 ]]; then exit "$test_exit"; fi
	exit 1
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

client_hook=$work/client-hook.sh
cat >"$client_hook" <<'EOF'
#!/bin/sh
set -eu
case "$1" in
bound|renew)
	router_addr=$(printf '%s\n' "${router:-}" | awk '{print $1}')
	dns_addr=$(printf '%s\n' "${dns:-}" | awk '{print $1}')
	lease_ip=${ip:-}
	server_id=${serverid:-}
	case "$lease_ip" in 192.0.2.*) lease_octet=${lease_ip##*.} ;; *) exit 40 ;; esac
	case "$lease_octet" in ''|*[!0-9]*) exit 40 ;; esac
	[ "$lease_octet" -ge "$EXPECTED_FIRST" ] && [ "$lease_octet" -le "$EXPECTED_LAST" ] || exit 40
	[ "$router_addr" = 192.0.2.1 ] || exit 41
	[ "$dns_addr" = 192.0.2.53 ] || exit 42
	[ "$server_id" = 198.51.100.2 ] || exit 43
	ip address replace "$lease_ip/24" dev "$interface"
	ip route replace default via "$router_addr" dev "$interface"
	printf '%s %s %s %s %s %s\n' "$1" "$lease_ip" "$router_addr" "$dns_addr" "$server_id" "${lease:-}" >>"$EVENTS"
	;;
esac
EOF
chmod 0755 "$client_hook"

cat >"$relay_input" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: dhcpv4-relay-runtime-test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lan
      spec:
        ifname: $lan_bridge
        managed: false
        owner: external
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifname: $wan_if
        managed: false
        owner: external
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv4Relay
      metadata:
        name: lan-relay
      spec:
        interfaces: [lan]
        upstream: 198.51.100.2
EOF
cat >"$work/empty.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: dhcpv4-relay-runtime-test
spec:
  resources: []
EOF
cp -- "$relay_input" "$router_config"
printf 'routerd: %s\n' "$("$routerd" --version)"
sha256sum "$routerd" "$routerctl"
"$routerd" validate --config "$relay_input" >/dev/null
"$routerd" validate --config "$work/empty.yaml" >/dev/null
relay_input_sha=$(sha256sum "$relay_input" | awk '{print $1}')

ip netns add "$client_ns"
client_ns_created=1
ip netns add "$upstream_ns"
upstream_ns_created=1
ip -n "$client_ns" link set lo up
ip -n "$upstream_ns" link set lo up

ip link add name "$lan_bridge" type bridge
bridge_created=1
ip address add 192.0.2.1/24 dev "$lan_bridge"
ip link set dev "$lan_bridge" up
ip link add name "$lan_port" type veth peer name "$client_peer"
lan_pair_created=1
ip link set dev "$lan_port" master "$lan_bridge"
ip link set dev "$lan_port" up
ip link set dev "$client_peer" netns "$client_ns"
ip -n "$client_ns" link set dev "$client_peer" name eth0
ip -n "$client_ns" link set dev eth0 up

ip link add name "$wan_if" type veth peer name "$server_peer"
wan_pair_created=1
ip address add 198.51.100.1/24 dev "$wan_if"
ip link set dev "$wan_if" up
ip link set dev "$server_peer" netns "$upstream_ns"
ip -n "$upstream_ns" link set dev "$server_peer" name eth0
ip -n "$upstream_ns" address add 198.51.100.2/24 dev eth0
ip -n "$upstream_ns" link set dev eth0 up
ip -n "$upstream_ns" route add 192.0.2.0/24 via 198.51.100.1 dev eth0

start_upstream() {
	local instance=$1
	upstream_log=$work/upstream-$instance.log
	cat >"$work/upstream-$instance.conf" <<EOF
port=0
no-resolv
no-hosts
bind-interfaces
interface=eth0
dhcp-authoritative
dhcp-range=192.0.2.100,192.0.2.110,255.255.255.0,2m
dhcp-option=option:router,192.0.2.1
dhcp-option=option:dns-server,192.0.2.53
dhcp-leasefile=$work/upstream-$instance.leases
log-dhcp
EOF
	dnsmasq --test --conf-file="$work/upstream-$instance.conf" >"$work/upstream-$instance-syntax.log" 2>&1
	ip netns exec "$upstream_ns" "$dnsmasq" --keep-in-foreground --user=root --group=root \
		--conf-file="$work/upstream-$instance.conf" --pid-file="$work/upstream-$instance.pid" \
		--log-facility="$upstream_log" &
	upstream_pid=$!
	for _ in {1..50}; do
		[[ -s "$work/upstream-$instance.pid" ]] && break
		kill -0 "$upstream_pid" 2>/dev/null || { tail -n 60 "$upstream_log" >&2; return 1; }
		sleep 0.1
	done
	[[ -s "$work/upstream-$instance.pid" ]] || { echo 'upstream DHCP server did not start within 5 seconds' >&2; return 1; }
}

clear_client() {
	local mac=$1
	ip -n "$client_ns" -4 address flush dev eth0
	ip -n "$client_ns" route flush dev eth0
	ip -n "$client_ns" link set dev eth0 address "$mac"
}

run_client() {
	local name=$1 mac=$2
	local events=$work/$name.events
	local log=$work/client.log
	: >"$events"
	: >"$log"
	clear_client "$mac"
	EXPECTED_FIRST=100 EXPECTED_LAST=110 EVENTS="$events" ip netns exec "$client_ns" \
		"$busybox" udhcpc -f -n -t 5 -T 1 -i eth0 -s "$client_hook" >"$log" 2>&1 &
	client_pid=$!
	for _ in {1..75}; do
		if grep -q '^bound ' "$events"; then break; fi
		kill -0 "$client_pid" 2>/dev/null || { tail -n 60 "$log" >&2; return 1; }
		sleep 0.2
	done
	if ! grep -q '^bound ' "$events"; then
		stop_pid "$client_pid"
		client_pid=0
		tail -n 60 "$log" >&2
		return 1
	fi
	stop_pid "$client_pid"
	client_pid=0
	local address
	address=$(awk '$1 == "bound" { print $2; exit }' "$events")
	grep -Fq "$address/24" < <(ip -n "$client_ns" -4 -o address show dev eth0) || {
		echo "$name client did not install its DHCP address" >&2
		return 1
	}
	ip -n "$client_ns" route show default | grep -Fq 'via 192.0.2.1' || {
		echo "$name client did not install the DHCP router option" >&2
		return 1
	}
	printf '%s: %s\n' "$name" "$(cat "$events")"
}

expect_no_lease() {
	local name=$1 mac=$2
	local events=$work/$name.events
	local log=$work/client.log
	: >"$events"
	: >"$log"
	clear_client "$mac"
	EXPECTED_FIRST=100 EXPECTED_LAST=110 EVENTS="$events" ip netns exec "$client_ns" \
		"$busybox" udhcpc -f -n -t 3 -T 1 -i eth0 -s "$client_hook" >"$log" 2>&1 &
	client_pid=$!
	local exited=0
	for _ in {1..75}; do
		if grep -q '^bound ' "$events"; then
			stop_pid "$client_pid"
			client_pid=0
			echo "$name client received a lease while none was expected" >&2
			return 1
		fi
		if ! kill -0 "$client_pid" 2>/dev/null; then exited=1; break; fi
		sleep 0.2
	done
	if [[ "$exited" -eq 0 ]]; then
		stop_pid "$client_pid"
		client_pid=0
		echo "$name client exceeded its 15-second retry bound" >&2
		return 1
	fi
	if wait "$client_pid"; then
		client_pid=0
		echo "$name DHCP client exited successfully without a recorded lease" >&2
		return 1
	fi
	client_pid=0
	[[ ! -s "$events" ]] || { echo "$name unexpectedly received DHCP options" >&2; return 1; }
	if [[ -n "${upstream_log:-}" ]] && grep -Fq "$mac" "$upstream_log"; then
		echo "$name request reached upstream despite expected relay withdrawal" >&2
		return 1
	fi
}

start_upstream initial
"$routerd" serve --config "$router_config" --state-file "$work/state.db" \
	--status-file "$work/status.json" --socket "$router_socket" --status-socket "$status_socket" \
	--ledger-file "$work/ledger.db" --dnsmasq-file "$work/dnsmasq.conf" \
	--controllers link,dhcpv6-server --graceful-stop-timeout 0 >"$work/routerd.log" 2>&1 &
routerd_pid=$!
for _ in {1..150}; do
	if [[ -f "$unit_file" ]] && grep -Fq "$work/dnsmasq.conf" "$unit_file"; then dnsmasq_unit_owned=1; fi
	if [[ -S "$router_socket" ]] && systemctl is-active --quiet routerd-dnsmasq.service; then break; fi
	kill -0 "$routerd_pid" 2>/dev/null || { tail -n 80 "$work/routerd.log" >&2; exit 1; }
	sleep 0.2
done
[[ -S "$router_socket" ]] || { echo 'routerd did not open its control socket within 30 seconds' >&2; exit 1; }
systemctl is-active --quiet routerd-dnsmasq.service || { tail -n 80 "$work/routerd.log" >&2; echo 'routerd did not start managed dnsmasq within 30 seconds' >&2; exit 1; }
if [[ -f "$unit_file" ]] && grep -Fq "$work/dnsmasq.conf" "$unit_file"; then
	dnsmasq_unit_owned=1
else
	echo 'systemd dnsmasq unit does not reference the test config' >&2
	exit 1
fi
grep -Fqx 'dhcp-relay=192.0.2.1,198.51.100.2' "$work/dnsmasq.conf" || {
	cat "$work/dnsmasq.conf" >&2
	echo 'routerd did not render relay from the observed LAN address' >&2
	exit 1
}
dnsmasq --test --conf-file="$work/dnsmasq.conf" >"$work/relay-syntax.log" 2>&1

run_client initial 02:00:00:00:15:01
grep -Fq 'DHCPDISCOVER(eth0) 02:00:00:00:15:01' "$upstream_log" || { echo 'independent upstream did not observe the relayed client' >&2; exit 1; }
grep -Fq 'DHCPOFFER(eth0) 192.0.2.' "$upstream_log" || { echo 'upstream did not offer an address in the configured LAN subnet' >&2; exit 1; }

stop_pid "$upstream_pid"
upstream_pid=0
expect_no_lease upstream-outage 02:00:00:00:15:02
start_upstream recovered
run_client upstream-recovery 02:00:00:00:15:03

"$routerctl" apply --socket "$router_socket" -f "$work/empty.yaml" --replace >"$work/withdraw.log" 2>&1
for _ in {1..100}; do
	if [[ "$(systemctl show -p LoadState --value routerd-dnsmasq.service 2>/dev/null)" == not-found ]]; then break; fi
	sleep 0.2
done
[[ "$(systemctl show -p LoadState --value routerd-dnsmasq.service 2>/dev/null)" == not-found ]] || {
	echo 'routerd did not withdraw the systemd-owned dnsmasq unit' >&2
	exit 1
}
[[ ! -e "$dnsmasq_pid_file" && ! -e "$dnsmasq_owner_file" ]] || { echo 'dnsmasq process ownership files remained after withdrawal' >&2; exit 1; }
for path in "$unit_file" "$activation_file" "$unit_wants_link"; do
	[[ ! -e "$path" && ! -L "$path" ]] || { echo "dnsmasq withdrawal left a service artifact: $path" >&2; exit 1; }
done
dnsmasq_unit_owned=0
[[ "$(sha256sum "$relay_input" | awk '{print $1}')" == "$relay_input_sha" ]] || {
	echo 'relay apply input changed during canonical config replacement' >&2
	exit 1
}
expect_no_lease relay-withdrawal 02:00:00:00:15:04

"$routerctl" apply --socket "$router_socket" -f "$relay_input" --replace >"$work/reapply.log" 2>&1
for _ in {1..100}; do
	if [[ -f "$unit_file" ]] && grep -Fq "$work/dnsmasq.conf" "$unit_file"; then dnsmasq_unit_owned=1; fi
	if systemctl is-active --quiet routerd-dnsmasq.service; then break; fi
	sleep 0.2
done
systemctl is-active --quiet routerd-dnsmasq.service || { echo 'routerd did not recreate systemd dnsmasq after reapply' >&2; exit 1; }
if [[ -f "$unit_file" ]] && grep -Fq "$work/dnsmasq.conf" "$unit_file"; then
	dnsmasq_unit_owned=1
else
	echo 'reapplied systemd dnsmasq unit does not reference the test config' >&2
	exit 1
fi
run_client relay-reapply 02:00:00:00:15:05

systemctl restart routerd-dnsmasq.service
systemctl is-active --quiet routerd-dnsmasq.service
run_client dnsmasq-service-restart 02:00:00:00:15:06

# A changed config file can precede replacement of the old native process.
# Observe the replacement before asking the independent client for a new lease.
previous_relay_pid=$(systemctl show -p MainPID --value routerd-dnsmasq.service)
ip address del 192.0.2.1/24 dev "$lan_bridge"
ip address add 192.0.2.254/24 dev "$lan_bridge"
renumbered=0
for _ in {1..150}; do
 current_relay_pid=$(systemctl show -p MainPID --value routerd-dnsmasq.service)
 if grep -Fqx 'dhcp-relay=192.0.2.254,198.51.100.2' "$work/dnsmasq.conf" &&
    [[ "$current_relay_pid" =~ ^[0-9]+$ && "$current_relay_pid" -gt 0 && "$current_relay_pid" != "$previous_relay_pid" ]] &&
    systemctl is-active --quiet routerd-dnsmasq.service; then
  renumbered=1
  break
 fi
 sleep 0.2
done
[[ "$renumbered" -eq 1 ]] || { echo 'observed address change did not replace the owned relay process within 30 seconds' >&2; exit 1; }
run_client observed-address-change 02:00:00:00:15:07

# Report decisive observations before EXIT cleanup removes all successful-run output.
printf 'Observed routerd 1.5 relay config: %s\n' "$(grep '^dhcp-relay=' "$work/dnsmasq.conf")"
printf 'Independent clients received configured pool/options; upstream loss/recovery and relay withdrawal/reapply behaved as expected.\n'
