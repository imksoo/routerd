#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Actual installed-service IPv6 non-host-prefix route lifecycle in a disposable VM.
set -euo pipefail

if [ "${1:-}" != --disposable-vm ] || [ "$#" -ne 1 ]; then
  echo "usage: $0 --disposable-vm" >&2
  exit 2
fi
test "$(id -u)" = 0
systemd-detect-virt --vm --quiet
for x in ip jq ping systemctl; do command -v "$x" >/dev/null; done
test ! -e /var/run/netns/rd15-v6p-peer
! ip link show rd15-v6p-host >/dev/null 2>&1

work=$(mktemp -d /var/tmp/routerd15-ipv6-prefix-route.XXXXXX)
empty="$work/empty.json"
cat > "$empty" <<'JSON'
{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"ipv6-prefix-route-smoke"},"spec":{"resources":[]}}
JSON
stage=setup
finish() {
  rc=$?
  trap - EXIT
  if [ "$rc" -ne 0 ]; then
    echo "FAIL at $stage" >&2
    ip -j -d -6 route show fd42:161::/64 >&2 || true
    ip -6 route get fd42:161::99 >&2 || true
  fi
  install -m 0600 "$empty" /usr/local/etc/routerd/router.yaml >/dev/null 2>&1 || true
  systemctl restart routerd >/dev/null 2>&1 || true
  ip netns del rd15-v6p-peer >/dev/null 2>&1 || true
  ip link del rd15-v6p-host >/dev/null 2>&1 || true
  rm -rf -- "$work"
  exit "$rc"
}
trap finish EXIT

ip netns add rd15-v6p-peer
ip -n rd15-v6p-peer link set lo up
ip link add rd15-v6p-host type veth peer name peer0 netns rd15-v6p-peer
ip link set rd15-v6p-host up
ip -6 addr add fd42:160::1/64 nodad dev rd15-v6p-host
ip -n rd15-v6p-peer link set peer0 up
ip -n rd15-v6p-peer addr add fd42:160::2/64 nodad dev peer0
ip -n rd15-v6p-peer addr add fd42:161::99/64 nodad dev lo

cat > "$work/route.json" <<'JSON'
{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"ipv6-prefix-route-smoke"},"spec":{"resources":[
{"apiVersion":"net.routerd.net/v1alpha1","kind":"Interface","metadata":{"name":"upstream"},"spec":{"ifname":"rd15-v6p-host","managed":false,"owner":"external"}},
{"apiVersion":"net.routerd.net/v1alpha1","kind":"IPv6StaticRoute","metadata":{"name":"peer-prefix"},"spec":{"interface":"upstream","destination":"fd42:161::/64","via":"fd42:160::2","metric":78}}
]}}
JSON
/usr/local/sbin/routerd validate --config "$work/route.json"
install -m 0600 "$work/route.json" /usr/local/etc/routerd/router.yaml
systemctl restart routerd
stage=initial-apply
for _ in {1..30}; do
  if ip -j -d -6 route show fd42:161::/64 | jq -e '.[] | select(.dst == "fd42:161::/64" and .gateway == "fd42:160::2" and .dev == "rd15-v6p-host" and .metric == 78 and (.protocol == "boot" or .protocol == "3"))' >/dev/null; then break; fi
  sleep 1
done
ip -j -d -6 route show fd42:161::/64 | jq -e '.[] | select(.dst == "fd42:161::/64" and .gateway == "fd42:160::2" and .dev == "rd15-v6p-host" and .metric == 78 and (.protocol == "boot" or .protocol == "3"))' >/dev/null
stage=fixture-connectivity
ping -6 -c 1 -W 2 fd42:160::2 >/dev/null
ip netns exec rd15-v6p-peer ping -6 -c 1 -W 2 fd42:160::1 >/dev/null
stage=forwarding
ping -6 -c 1 -W 2 fd42:161::99 >/dev/null
stage=restart-reconcile
systemctl restart routerd
systemctl is-active --quiet routerd
ping -6 -c 1 -W 2 fd42:161::99 >/dev/null
stage=withdrawal
install -m 0600 "$empty" /usr/local/etc/routerd/router.yaml
systemctl restart routerd
for _ in {1..30}; do
  if ! ip -j -d -6 route show fd42:161::/64 | jq -e '.[] | select(.dst == "fd42:161::/64" and .gateway == "fd42:160::2" and .dev == "rd15-v6p-host" and .metric == 78)' >/dev/null; then break; fi
  sleep 1
done
if ip -j -d -6 route show fd42:161::/64 | jq -e '.[] | select(.dst == "fd42:161::/64" and .gateway == "fd42:160::2" and .dev == "rd15-v6p-host" and .metric == 78)' >/dev/null; then exit 1; fi
if ping -6 -c 1 -W 1 fd42:161::99 >/dev/null 2>&1; then exit 1; fi
echo 'PASS: IPv6 /64 static route installs, forwards, survives service restart, and withdraws on the installed service'
