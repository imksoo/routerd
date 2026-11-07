#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause
# Isolated disposable Linux VM only; runs the packaged routerd binary and a real client.
set -euo pipefail

if [[ "${1:-}" != --disposable-vm || "$#" -ne 2 ]]; then
	echo "usage: $0 --disposable-vm PACKAGE_ROOT" >&2
	echo "Dedicated Linux VM with no active routerd resources only." >&2
	exit 2
fi
package_root=${2:?package root}
routerd=$package_root/bin/routerd
routerctl=$package_root/bin/routerctl
installed_routerctl=/usr/local/sbin/routerctl
dhcpcd=$(command -v dhcpcd || true)
for command in ip jq systemctl systemd-detect-virt timeout journalctl tcpdump; do
	command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }
done
[[ -x "$routerd" && -x "$routerctl" ]] || { echo "package root must contain bin/routerd and bin/routerctl" >&2; exit 1; }
[[ -x "$installed_routerctl" ]] || { echo "installed routerctl is required to inspect the existing service" >&2; exit 1; }
[[ -n "$dhcpcd" ]] || { echo "missing required command: dhcpcd" >&2; exit 1; }
systemd-detect-virt --vm --quiet || { echo "run only inside a disposable VM" >&2; exit 1; }
[[ "$(id -u)" -eq 0 ]] || { echo "run explicitly with sudo: sudo $0 --disposable-vm PACKAGE_ROOT" >&2; exit 1; }

client_ns=rd15-dhcp6-client
router_if=rd15-dhcp6-lan
client_peer=rd15-dhcp6-peer
client_if=eth0
unit_file=/etc/systemd/system/routerd-dnsmasq.service
unit_state=$(systemctl show -p LoadState --value routerd-dnsmasq.service)
[[ "$unit_state" == not-found && ! -e "$unit_file" ]] || { echo "routerd-dnsmasq.service already exists" >&2; exit 1; }
systemctl is-active --quiet routerd.service || { echo "routerd.service must be active before the isolated test" >&2; exit 1; }
baseline_status=$("$installed_routerctl" get status -o json) || { echo "cannot read existing routerd status" >&2; exit 1; }
jq -e '(.status.phase == "Healthy") and ((.resources // []) | length == 0)' <<<"$baseline_status" >/dev/null || {
	echo "existing routerd must be Healthy with no active resources; refusing to stop it" >&2
	exit 1
}
ip netns list | awk '{print $1}' | grep -Fxq "$client_ns" && { echo "namespace already exists: $client_ns" >&2; exit 1; }
ip link show "$router_if" >/dev/null 2>&1 && { echo "interface already exists: $router_if" >&2; exit 1; }
ip link show "$client_peer" >/dev/null 2>&1 && { echo "interface already exists: $client_peer" >&2; exit 1; }

work=$(mktemp -d /var/tmp/routerd15-dhcpv6.XXXXXX)
chmod 0755 "$work"
router_socket=$work/routerd.sock
status_socket=$work/routerd-status.sock
routerd_pid=0
client_pid=0
ra_capture_pid=0
base_service_stopped=0
link_created=0
namespace_created=0
run_start=$(date --iso-8601=seconds)

cleanup() {
	local test_exit=$? cleanup_exit=0
	trap - EXIT
	set +e
	if [[ "$client_pid" -gt 0 ]] && kill -0 "$client_pid" 2>/dev/null; then
		kill -TERM "$client_pid" 2>/dev/null
		wait "$client_pid" 2>/dev/null
	fi
	if [[ "$ra_capture_pid" -gt 0 ]] && kill -0 "$ra_capture_pid" 2>/dev/null; then
		kill -TERM "$ra_capture_pid" 2>/dev/null
		wait "$ra_capture_pid" 2>/dev/null
	fi
	if [[ "$routerd_pid" -gt 0 ]] && kill -0 "$routerd_pid" 2>/dev/null; then
		cat >"$work/empty.yaml" <<'YAML'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: routerd15-dhcpv6-smoke
spec:
  resources: []
YAML
		if ! "$routerctl" apply --socket "$router_socket" -f "$work/empty.yaml" --replace >"$work/cleanup-apply.log" 2>&1; then
			echo "cleanup failed: routerctl empty apply" >&2
			cleanup_exit=1
		fi
		kill -TERM "$routerd_pid" 2>/dev/null || { echo "cleanup failed: stop test routerd" >&2; cleanup_exit=1; }
		wait "$routerd_pid" 2>/dev/null
	fi
	if [[ -e "$unit_file" ]] && grep -Fq "$work/dnsmasq.conf" "$unit_file"; then
		systemctl stop routerd-dnsmasq.service >/dev/null 2>&1 || { echo "cleanup failed: stop test dnsmasq" >&2; cleanup_exit=1; }
		systemctl disable routerd-dnsmasq.service >/dev/null 2>&1 || { echo "cleanup failed: disable test dnsmasq" >&2; cleanup_exit=1; }
		rm -f -- "$unit_file" "$unit_file.routerd-activation.json" || { echo "cleanup failed: remove test dnsmasq unit" >&2; cleanup_exit=1; }
		systemctl daemon-reload >/dev/null 2>&1 || { echo "cleanup failed: reload systemd" >&2; cleanup_exit=1; }
	fi
	if [[ "$namespace_created" -eq 1 ]]; then
		ip netns delete "$client_ns" >/dev/null 2>&1 || { echo "cleanup failed: remove test namespace" >&2; cleanup_exit=1; }
	fi
	if [[ "$link_created" -eq 1 ]] && ip link show "$router_if" >/dev/null 2>&1; then
		ip link delete "$router_if" >/dev/null 2>&1 || { echo "cleanup failed: remove test link" >&2; cleanup_exit=1; }
	fi
	if [[ "$base_service_stopped" -eq 1 ]]; then
		systemctl start routerd.service >/dev/null 2>&1 || { echo "cleanup failed: restart original routerd service" >&2; cleanup_exit=1; }
		systemctl is-active --quiet routerd.service || { echo "cleanup failed: original routerd service is inactive" >&2; cleanup_exit=1; }
	fi
	if [[ "$test_exit" -eq 0 && "$cleanup_exit" -eq 0 ]]; then
		rm -r -- "$work"
		printf 'PASS: packaged routerd DHCPv6 IA_NA acquire/renew and RA default route\n'
	else
		printf 'FAIL: test_exit=%s cleanup_exit=%s; temporary diagnosis at %s\n' "$test_exit" "$cleanup_exit" "$work" >&2
		if [[ "$test_exit" -ne 0 ]]; then
			journalctl -u routerd-dnsmasq.service --since "$run_start" --no-pager -o cat >"$work/dnsmasq-journal.log" 2>&1
		fi
	fi
	[[ "$test_exit" -eq 0 ]] || exit "$test_exit"
	[[ "$cleanup_exit" -eq 0 ]] || exit "$cleanup_exit"
}
trap cleanup EXIT

"$routerd" --version
cat >"$work/router.yaml" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: routerd15-dhcpv6-smoke
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lan
      spec:
        ifname: $router_if
        managed: false
        owner: external
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv6Server
      metadata:
        name: lan-dhcpv6
      spec:
        interface: lan
        mode: stateful
        defaultRoute: true
        logDHCP: true
        dnsSource: none
        addressPool:
          start: fd00:1234::100
          end: fd00:1234::110
          leaseTime: 2m
        configPath: $work/dnsmasq.conf
        pidFile: $work/dnsmasq.pid
        leaseFile: $work/dnsmasq.leases
EOF
touch "$work/dnsmasq.leases"
chmod 0666 "$work/dnsmasq.leases"
"$routerd" validate --config "$work/router.yaml" >/dev/null

ip netns add "$client_ns"
namespace_created=1
ip link add "$router_if" type veth peer name "$client_peer"
link_created=1
ip link set "$client_peer" netns "$client_ns"
ip -n "$client_ns" link set "$client_peer" name "$client_if"
ip -6 addr add fd00:1234::1/64 dev "$router_if"
ip link set "$router_if" up
ip -n "$client_ns" link set lo up
ip -n "$client_ns" link set "$client_if" up
ip netns exec "$client_ns" sysctl -qw "net.ipv6.conf.$client_if.accept_ra=2" "net.ipv6.conf.$client_if.autoconf=1"
for _ in {1..100}; do
	if ip -6 -o addr show dev "$router_if" scope link | awk '$4 ~ /^fe80:/ && $0 !~ /tentative|dadfailed/ {found=1} END {exit !found}' &&
		ip -6 -o addr show dev "$router_if" | awk '$4 == "fd00:1234::1/64" && $0 !~ /tentative|dadfailed/ {found=1} END {exit !found}'; then
		break
	fi
	sleep 0.1
done
ip -6 -o addr show dev "$router_if" scope link | awk '$4 ~ /^fe80:/ && $0 !~ /tentative|dadfailed/ {found=1} END {exit !found}' || { echo "router link-local DAD did not complete" >&2; exit 1; }
ip -6 -o addr show dev "$router_if" | awk '$4 == "fd00:1234::1/64" && $0 !~ /tentative|dadfailed/ {found=1} END {exit !found}' || { echo "router ULA DAD did not complete" >&2; exit 1; }

systemctl stop routerd.service
base_service_stopped=1
systemctl is-active --quiet routerd.service && { echo "routerd.service did not stop" >&2; exit 1; }
"$routerd" serve --config "$work/router.yaml" --state-file "$work/state.db" \
	--status-file "$work/status.json" --socket "$router_socket" --status-socket "$status_socket" \
	--ledger-file "$work/ledger.db" --dnsmasq-file "$work/dnsmasq.conf" \
	--controllers dhcpv6-server --graceful-stop-timeout 0 >"$work/routerd.log" 2>&1 &
routerd_pid=$!
for _ in {1..100}; do
	[[ -S "$router_socket" ]] && break
	kill -0 "$routerd_pid" 2>/dev/null || { cat "$work/routerd.log" >&2; exit 1; }
	sleep 0.1
done
[[ -S "$router_socket" ]] || { cat "$work/routerd.log" >&2; echo "routerd did not open its control socket" >&2; exit 1; }
for _ in {1..100}; do
	systemctl is-active --quiet routerd-dnsmasq.service && break
	kill -0 "$routerd_pid" 2>/dev/null || { cat "$work/routerd.log" >&2; exit 1; }
	sleep 0.1
done
systemctl is-active --quiet routerd-dnsmasq.service || { cat "$work/routerd.log" >&2; echo "routerd did not start managed dnsmasq" >&2; exit 1; }

timeout -k 2 35 tcpdump -i "$router_if" -n -vv -l 'icmp6 and ip6[40] == 134' >"$work/ra.log" 2>&1 &
ra_capture_pid=$!

cat >"$work/dhcpcd.conf" <<'EOF'
interface eth0
  ia_na 42
EOF
# shellcheck disable=SC2016 # evaluated inside the client's private mount namespace
ip netns exec "$client_ns" unshare -m sh -ec '
	mount --make-rprivate /
	mount -t tmpfs tmpfs /run
	mount -t tmpfs tmpfs /var/lib
	mkdir -p /run/dhcpcd /var/lib/dhcpcd
	exec timeout -k 2 150 "$1" -d -6 -B --waitip=6 -t 20 -f "$2" -c /bin/true eth0
' sh "$dhcpcd" "$work/dhcpcd.conf" >"$work/dhcpcd.log" 2>&1 &
client_pid=$!

assigned=
for _ in {1..150}; do
	assigned=$(ip -n "$client_ns" -6 -o addr show dev "$client_if" scope global | awk '$4 ~ /^fd00:1234::/ {print $4; exit}')
	if [[ -n "$assigned" ]] && ip -n "$client_ns" -6 route show default | grep -q 'proto ra'; then break; fi
	kill -0 "$client_pid" 2>/dev/null || { cat "$work/dhcpcd.log" >&2; journalctl -u routerd-dnsmasq.service --since "$run_start" --no-pager -o cat >&2; exit 1; }
	sleep 0.2
done
[[ -n "$assigned" ]] || { cat "$work/dhcpcd.log" >&2; echo "client received no IA_NA address" >&2; exit 1; }
python3 - "$assigned" <<'PY'
import ipaddress
import sys

address = ipaddress.ip_interface(sys.argv[1]).ip
if not ipaddress.ip_address("fd00:1234::100") <= address <= ipaddress.ip_address("fd00:1234::110"):
    raise SystemExit(f"assigned address outside configured pool: {address}")
PY

for _ in {1..450}; do
	journalctl -u routerd-dnsmasq.service --since "$run_start" --no-pager -o cat >"$work/dnsmasq.log" 2>&1
	renew_events=$(grep -Ec "DHCP(RENEW|REBIND|REQUEST)\\($router_if\\)" "$work/dnsmasq.log" || true)
	if [[ "$renew_events" -ge 2 ]]; then break; fi
	kill -0 "$client_pid" 2>/dev/null || { cat "$work/dhcpcd.log" >&2; cat "$work/dnsmasq.log" >&2; exit 1; }
	sleep 0.2
done
renew_events=$(grep -Ec "DHCP(RENEW|REBIND|REQUEST)\\($router_if\\)" "$work/dnsmasq.log" || true)
replies=$(grep -c "DHCPREPLY($router_if)" "$work/dnsmasq.log" || true)
[[ "$renew_events" -ge 2 && "$replies" -ge 2 ]] || { cat "$work/dhcpcd.log" >&2; cat "$work/dnsmasq.log" >&2; echo "DHCPv6 renew exchange not observed" >&2; exit 1; }
renewed_address=$(ip -n "$client_ns" -6 -o addr show dev "$client_if" scope global | awk '$4 ~ /^fd00:1234::/ {print $4; exit}')
[[ "$renewed_address" == "$assigned" ]] || { echo "client address changed during renew: $assigned -> $renewed_address" >&2; exit 1; }
grep -q 'ADV fd00:1234::' "$work/dhcpcd.log"
grep -q 'REPLY6 received' "$work/dhcpcd.log"
grep -qi 'managed' "$work/ra.log" || { cat "$work/ra.log" >&2; echo "stateful RA managed flag not observed" >&2; exit 1; }
ip -n "$client_ns" -6 route show default | grep -q 'proto ra'
printf 'Observed routerd DHCPv6 IA_NA pool address %s, renewal, and RA default route.\n' "${assigned%/*}"
