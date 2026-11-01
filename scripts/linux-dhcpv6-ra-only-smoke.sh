#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Isolated disposable VM only: installed product, native RA, and independent rdisc6 decoder.
set -euo pipefail
if [ "${1:-}" != --disposable-vm ] || [ "$#" -ne 2 ]; then
  echo "usage: $0 --disposable-vm CANDIDATE_JSON" >&2
  exit 2
fi
candidate=$2
rdisc=/opt/routerd15-fixtures/kea/usr/bin/rdisc6
systemd-detect-virt --vm --quiet
test "$(id -u)" = 0
for command in ip jq systemctl; do command -v "$command" >/dev/null; done
test -x "$rdisc"
/usr/local/sbin/routerd validate --config "$candidate"
work=$(mktemp -d /var/tmp/routerd15-dhcpv6-ra-only.XXXXXX)
client_ns=rd15-ra-only-client
router_if=rd15-lan
client_if=rd15ra${work##*.}
finish() {
  rc=$?
  trap - EXIT
  set +e
  ip netns del "$client_ns" >/dev/null 2>&1
  ip link del "$router_if" >/dev/null 2>&1
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
systemctl start routerd.service
/usr/local/sbin/routerctl apply -f "$candidate" --replace >"$work/apply.txt"
for _ in {1..30}; do
  /usr/local/sbin/routerctl get status -o json >"$work/status.json"
  if jq -e '.status.phase == "Healthy" and (.status.resourcePhaseIssues // [] | length) == 0' "$work/status.json" >/dev/null; then break; fi
  sleep 1
done
jq -e '.status.phase == "Healthy" and (.status.resourcePhaseIssues // [] | length) == 0' "$work/status.json" >/dev/null
/usr/local/sbin/routerctl describe DHCPv6Server/lan-ra-only -o json >"$work/ra-only-status.json"
jq -e '.resource.status.mode == "ra-only" and .resource.status.phase == "Applied"' "$work/ra-only-status.json" >/dev/null
! systemctl is-active --quiet routerd-dnsmasq.service
ip netns exec "$client_ns" "$rdisc" -1 -r 5 -w 2000 "$client_if" >"$work/ra.txt"
grep -F 'fd00:1234::/64' "$work/ra.txt"
cat >"$work/empty.json" <<'JSON'
{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"dhcpv6-ra-only-smoke"},"spec":{"resources":[]}}
JSON
/usr/local/sbin/routerctl apply -f "$work/empty.json" --replace >"$work/withdraw.txt"
for _ in {1..30}; do
  /usr/local/sbin/routerctl get status -o json >"$work/empty-status.json"
  if jq -e '.status.phase == "Healthy" and (.status.resourceCount // 0) == 0 and (.status.resourcePhaseIssues // [] | length) == 0' "$work/empty-status.json" >/dev/null; then break; fi
  sleep 1
done
jq -e '.status.phase == "Healthy" and (.status.resourceCount // 0) == 0 and (.status.resourcePhaseIssues // [] | length) == 0' "$work/empty-status.json" >/dev/null
printf 'PASS: actual routerd ra-only service emitted native RA without dnsmasq; empty apply withdrew it\n'
