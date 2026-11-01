#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Isolated disposable VM only: verify DHCPv6Server implicit native RA flags with an independent raw receiver.
set -euo pipefail
if [ "${1:-}" != --disposable-vm ] || [ "$#" -ne 2 ]; then
  echo "usage: $0 --disposable-vm CANDIDATE_JSON" >&2
  exit 2
fi
candidate=$2
receiver=${ROUTERD_RA_FLAG_RECEIVER:-"$(dirname "$0")/../tests/netns/ra-flag-receiver.py"}
systemd-detect-virt --vm --quiet
test "$(id -u)" = 0
for command in ip jq python3 systemctl; do command -v "$command" >/dev/null; done
test -x "$receiver"
/usr/local/sbin/routerd validate --config "$candidate"
mode=$(jq -r '[.spec.resources[] | select(.kind == "DHCPv6Server") | (.spec.mode // "stateless")] | if length == 1 then .[0] else error("fixture must declare exactly one DHCPv6Server") end' "$candidate")
test "$(jq '[.spec.resources[] | select(.kind == "IPv6RouterAdvertisement")] | length' "$candidate")" = 0
case "$mode" in
  stateful|both) managed=true; other=true;;
  stateless) managed=false; other=true;;
  ra-only) managed=false; other=false;;
  *) echo "unsupported smoke mode: $mode" >&2; exit 2;;
esac
autonomous=true
[ "$mode" = stateful ] && autonomous=false
work=$(mktemp -d /var/tmp/routerd15-dhcpv6-implicit-ra.XXXXXX)
cat >"$work/empty.json" <<'JSON'
{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"dhcpv6-implicit-ra-smoke"},"spec":{"resources":[]}}
JSON
# A prior failed gate may have committed a candidate. Clear it before opening the
# receiver so a boot-time RA cannot be mistaken for this candidate's output.
# The candidate may have replaced the on-disk daemon while an older process is
# still running. Restart before reset so the observed control plane is the
# deployed candidate, not that inherited process.
systemctl restart routerd.service
# A stale RA socket from an interrupted older gate can make the first empty
# reconcile fail after canonical replacement. Restart only in that recovery
# case, then require the clean runtime before observing the candidate.
if ! /usr/local/sbin/routerctl apply -f "$work/empty.json" --replace >"$work/reset.txt"; then
  systemctl restart routerd.service
fi
for _ in {1..30}; do
  /usr/local/sbin/routerctl get status -o json >"$work/reset-status.json"
  if jq -e '.status.phase == "Healthy" and (.status.resourceCount // 0) == 0 and (.status.resourcePhaseIssues // [] | length) == 0 and all(.status.controllers[]?; .currentError != true)' "$work/reset-status.json" >/dev/null; then break; fi
  sleep 1
done
jq -e '.status.phase == "Healthy" and (.status.resourceCount // 0) == 0 and (.status.resourcePhaseIssues // [] | length) == 0 and all(.status.controllers[]?; .currentError != true)' "$work/reset-status.json" >/dev/null
client_ns=rd15-implicit-ra-client
router_if=rd15-lan
client_if=rd15i${work##*.}
observer=''
finish() {
  rc=$?
  cleanup_rc=0
  trap - EXIT
  set +e
  if [ -n "$observer" ]; then kill "$observer" >/dev/null 2>&1; wait "$observer" >/dev/null 2>&1; fi
  if [ "$rc" -ne 0 ]; then
    timeout 5 /usr/local/sbin/routerctl get status -o json >"$work/failure-status.json" 2>&1
    journalctl -u routerd.service --since "$(date --iso-8601=seconds -d '10 minutes ago')" --no-pager >"$work/failure-routerd.log" 2>&1
  fi
  # RA final withdrawal owns the live interface; stop it before removing the veth.
  systemctl stop routerd.service >/dev/null 2>&1 || cleanup_rc=1
  ip netns del "$client_ns" >/dev/null 2>&1 || cleanup_rc=1
  ip link del "$router_if" >/dev/null 2>&1 || cleanup_rc=1
  if [ "$rc" -ne 0 ]; then
    printf 'FAIL: fixture diagnostics follow; removing temporary output.\n' >&2
    for diagnostic in "$work"/failure-*; do
      [ -e "$diagnostic" ] || continue
      printf '%s:\n' "${diagnostic##*/}" >&2
      cat "$diagnostic" >&2 || cleanup_rc=1
    done
  fi
  rm -rf -- "$work" || cleanup_rc=1
  if [ "$rc" -eq 0 ] && [ "$cleanup_rc" -ne 0 ]; then rc=$cleanup_rc; fi
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
ip netns exec "$client_ns" python3 "$receiver" "$client_if" 12 >"$work/ra.json" & observer=$!
systemctl start routerd.service
/usr/local/sbin/routerctl apply -f "$candidate" --replace >"$work/apply.txt"
wait "$observer"
observer=''
jq -e --argjson managed "$managed" --argjson other "$other" --argjson autonomous "$autonomous" '.managed == $managed and .other == $other and any(.prefixes[]; .prefix == "fd00:1234::/64" and .autonomous == $autonomous)' "$work/ra.json" >/dev/null
for _ in {1..30}; do
  /usr/local/sbin/routerctl get status -o json >"$work/status.json"
  if jq -e '.status.phase == "Healthy" and (.status.resourcePhaseIssues // [] | length) == 0' "$work/status.json" >/dev/null; then break; fi
  sleep 1
done
jq -e '.status.phase == "Healthy" and (.status.resourcePhaseIssues // [] | length) == 0' "$work/status.json" >/dev/null
/usr/local/sbin/routerctl describe DHCPv6Server/lan-dhcpv6 -o json >"$work/server-status.json"
jq -e --arg mode "$mode" '.resource.status.mode == $mode and .resource.status.phase == "Applied"' "$work/server-status.json" >/dev/null
if [ "$mode" = ra-only ]; then ! systemctl is-active --quiet routerd-dnsmasq.service; else systemctl is-active --quiet routerd-dnsmasq.service; fi
/usr/local/sbin/routerctl apply -f "$work/empty.json" --replace >"$work/withdraw.txt"
for _ in {1..30}; do
  /usr/local/sbin/routerctl get status -o json >"$work/empty-status.json"
  if jq -e '.status.phase == "Healthy" and (.status.resourceCount // 0) == 0 and (.status.resourcePhaseIssues // [] | length) == 0' "$work/empty-status.json" >/dev/null; then break; fi
  sleep 1
done
jq -e '.status.phase == "Healthy" and (.status.resourceCount // 0) == 0 and (.status.resourcePhaseIssues // [] | length) == 0' "$work/empty-status.json" >/dev/null
printf 'PASS: actual routerd implicit DHCPv6 RA mode %s emitted expected flags and prefix autonomy; empty apply withdrew it\n' "$mode"
