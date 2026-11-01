#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Isolated disposable VM only: installed product service and independent dhcpcd.
set -euo pipefail

if [ "${1:-}" != --disposable-vm ] || [ "$#" -ne 2 ]; then
  echo "usage: $0 --disposable-vm CANDIDATE_JSON" >&2
  echo "Dedicated empty Linux VM only; changes service configuration and guest networking." >&2
  exit 2
fi
shift
candidate=${1:?candidate config}
systemd-detect-virt --vm --quiet
test "$(id -u)" = 0
for command in ip jq dhcpcd; do command -v "$command" >/dev/null; done
/usr/local/sbin/routerd validate --config "$candidate"

work=$(mktemp -d /var/tmp/routerd15-dhcpv6-server.XXXXXX)
client_ns=rd15-dhcpv6-client
router_if=rd15-lan
client_if=rd15c${work##*.}
dhcpcd_lease=/var/lib/dhcpcd/${client_if}.lease6

finish() {
  rc=$?
  trap - EXIT
  set +e
  ip netns exec "$client_ns" dhcpcd -x "$client_if" >/dev/null 2>&1
  ip netns del "$client_ns" >/dev/null 2>&1
  ip link del "$router_if" >/dev/null 2>&1
  rm -f -- "$dhcpcd_lease"
  if [ "$rc" -ne 0 ]; then
    timeout 5 /usr/local/sbin/routerctl get status -o json >"$work/failure-status.json" 2>&1
    journalctl -u routerd.service --since "$(date --iso-8601=seconds -d '10 minutes ago')" --no-pager >"$work/failure-routerd.log" 2>&1
    systemctl stop routerd.service
    printf 'FAIL: preserved fixture/output for local reproduction at %s\n' "$work" >&2
  else
    rm -r -- "$work"
  fi
  exit "$rc"
}
trap finish EXIT

test ! -e "/run/netns/$client_ns"
ip link show "$router_if" >/dev/null 2>&1 && { echo "existing interface: $router_if" >&2; exit 1; }

ip netns add "$client_ns"
ip link add "$router_if" type veth peer name "$client_if" netns "$client_ns"
ip link set "$router_if" addrgenmode none
ip netns exec "$client_ns" ip link set "$client_if" addrgenmode none
ip link set "$router_if" up
ip netns exec "$client_ns" ip link set lo up
ip netns exec "$client_ns" ip link set "$client_if" up
ip -6 addr add fe80::1/64 dev "$router_if" nodad
ip -6 addr add fd00:1234::1/64 dev "$router_if" nodad
ip netns exec "$client_ns" ip -6 addr add fe80::2/64 dev "$client_if" nodad

cat >"$work/dhcpcd-hook" <<EOF
#!/bin/sh
env | LC_ALL=C sort > "$work/dhcpcd-\$reason.env"
EOF
chmod 700 "$work/dhcpcd-hook"

systemctl start routerd.service
/usr/local/sbin/routerctl apply -f "$candidate" --replace >"$work/apply.txt"
for _ in {1..30}; do
  /usr/local/sbin/routerctl get status -o json >"$work/status.json"
  if jq -e '.status.phase == "Healthy" and (.status.resourcePhaseIssues // [] | length) == 0' "$work/status.json" >/dev/null; then break; fi
  sleep 1
done
jq -e '.status.phase == "Healthy" and (.status.resourcePhaseIssues // [] | length) == 0' "$work/status.json" >/dev/null

# The kernel must first accept routerd's RA and complete DAD for its SLAAC
# address. Starting the standard client earlier can race its DHCPv6 proxy with
# that address creation and conceal an RA readiness failure.
for _ in {1..30}; do
  if ip netns exec "$client_ns" ip -6 -j addr show dev "$client_if" | jq -e 'any(.[0].addr_info[]; (.local | startswith("fd00:1234:")) and .tentative != true and .dadfailed != true)' >/dev/null; then break; fi
  sleep 1
done
ip netns exec "$client_ns" ip -6 -j addr show dev "$client_if" >"$work/client-addresses.json"
jq -e 'any(.[0].addr_info[]; (.local | startswith("fd00:1234:")) and .tentative != true and .dadfailed != true)' "$work/client-addresses.json" >/dev/null

# dhcpcd waits for routerd's RA M/O flags before opening DHCPv6. Its hook only
# records the independently received lease/options; it does not configure the guest.
ip netns exec "$client_ns" dhcpcd -6 -1 -B -w --waitip=6 -t 30 --noconfigure -c "$work/dhcpcd-hook" \
  -o dhcp6_name_servers -o dhcp6_sntp_servers -o dhcp6_domain_search "$client_if"
lease=$(find "$work" -maxdepth 1 -name 'dhcpcd-BOUND6.env' -type f -print -quit)
test -n "$lease"
grep -Eq "^new_dhcp6_ia_na[0-9]+_ia_addr[0-9]+=fd00:1234::1(0[0-9a-f]|0[a-f])$" "$lease"
grep -Eq '^new_dhcp6_name_servers=.*fd00:1234::1' "$lease"
grep -Eq '^new_dhcp6_sntp_servers=.*fd00:1234::123' "$lease"
grep -Eq '^new_dhcp6_domain_search=.*home\.arpa' "$lease"

router_pid=$(systemctl show -p MainPID --value routerd.service)
systemctl restart routerd.service
test "$(systemctl show -p MainPID --value routerd.service)" != "$router_pid"
ip netns exec "$client_ns" dhcpcd -6 -1 -B -w --waitip=6 -t 30 --noconfigure -c "$work/dhcpcd-hook" \
  -o dhcp6_name_servers -o dhcp6_sntp_servers -o dhcp6_domain_search "$client_if"
restarted_lease=$(find "$work" -maxdepth 1 -name 'dhcpcd-BOUND6.env' -type f -newer "$work/status.json" -print -quit)
test -n "$restarted_lease"
grep -Eq "^new_dhcp6_ia_na[0-9]+_ia_addr[0-9]+=fd00:1234::1(0[0-9a-f]|0[a-f])$" "$restarted_lease"

cat >"$work/empty.json" <<'JSON'
{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"dhcpv6-server-smoke"},"spec":{"resources":[]}}
JSON
/usr/local/sbin/routerctl apply -f "$work/empty.json" --replace >"$work/withdraw.txt"
for _ in {1..30}; do
  /usr/local/sbin/routerctl get status -o json >"$work/empty-status.json"
  if jq -e '.status.phase == "Healthy" and (.status.resourceCount // 0) == 0 and (.status.resourcePhaseIssues // [] | length) == 0' "$work/empty-status.json" >/dev/null; then break; fi
  sleep 1
done
jq -e '.status.phase == "Healthy" and (.status.resourceCount // 0) == 0 and (.status.resourcePhaseIssues // [] | length) == 0' "$work/empty-status.json" >/dev/null
printf 'PASS: actual routerd DHCPv6 server and RA served independent dhcpcd lease/options before and after service restart; empty apply withdrew service\n'
