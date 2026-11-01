#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
set -euo pipefail
test "${1:-}" = --disposable-vm && test "$#" = 1
test "$(id -u)" = 0
systemd-detect-virt --vm --quiet
for x in ip jq ping systemctl routerctl; do command -v "$x" >/dev/null; done
test ! -e /var/run/netns/rd15-v6e-peer
! ip link show rd15-v6e-host >/dev/null 2>&1

work=$(mktemp -d /var/tmp/routerd15-ipv6-recovery.XXXXXX)
empty="$work/empty.json"
cat > "$empty" <<'JSON'
{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"ipv6-route-recovery"},"spec":{"resources":[]}}
JSON
stage=setup
finish() {
 rc=$?; trap - EXIT
 if [ "$rc" -ne 0 ]; then echo "FAIL at $stage" >&2; routerctl get IPv6StaticRoute/peer-recovery -o json >&2 || true; ip -j -d -6 route show fd42:171::/64 >&2 || true; fi
 install -m 0600 "$empty" /usr/local/etc/routerd/router.yaml >/dev/null 2>&1 || true
 systemctl restart routerd >/dev/null 2>&1 || true
 ip netns del rd15-v6e-peer >/dev/null 2>&1 || true
 ip link del rd15-v6e-host >/dev/null 2>&1 || true
 rm -rf -- "$work"; exit "$rc"
}
trap finish EXIT
cat > "$work/route.json" <<'JSON'
{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"ipv6-route-recovery"},"spec":{"resources":[
{"apiVersion":"net.routerd.net/v1alpha1","kind":"Interface","metadata":{"name":"recovery-upstream"},"spec":{"ifname":"rd15-v6e-host","managed":false,"owner":"external"}},
{"apiVersion":"net.routerd.net/v1alpha1","kind":"IPv6StaticRoute","metadata":{"name":"peer-recovery"},"spec":{"interface":"recovery-upstream","destination":"fd42:171::/64","via":"fd42:170::2","metric":79}}
]}}
JSON
/usr/local/sbin/routerd validate --config "$work/route.json"
install -m 0600 "$work/route.json" /usr/local/etc/routerd/router.yaml
systemctl restart routerd
stage=initial-failure
for _ in {1..15}; do routerctl get IPv6StaticRoute/peer-recovery -o json 2>/dev/null | jq -e '.items[0].status.phase == "Error" and .items[0].status.reason == "InvalidSpec"' >/dev/null && break; sleep 1; done
routerctl get IPv6StaticRoute/peer-recovery -o json | jq -e '.items[0].status.phase == "Error" and .items[0].status.reason == "InvalidSpec"' >/dev/null
ip netns add rd15-v6e-peer
ip -n rd15-v6e-peer link set lo up
ip link add rd15-v6e-host type veth peer name peer0 netns rd15-v6e-peer
ip link set rd15-v6e-host up
ip -6 addr add fd42:170::1/64 nodad dev rd15-v6e-host
ip -n rd15-v6e-peer link set peer0 up
ip -n rd15-v6e-peer addr add fd42:170::2/64 nodad dev peer0
ip -n rd15-v6e-peer addr add fd42:171::99/64 nodad dev lo
stage=automatic-recovery
for _ in {1..45}; do ip -j -d -6 route show fd42:171::/64 | jq -e '.[] | select(.dst == "fd42:171::/64" and .gateway == "fd42:170::2" and .dev == "rd15-v6e-host" and .metric == 79)' >/dev/null && break; sleep 1; done
ip -j -d -6 route show fd42:171::/64 | jq -e '.[] | select(.dst == "fd42:171::/64" and .gateway == "fd42:170::2" and .dev == "rd15-v6e-host" and .metric == 79)' >/dev/null
routerctl get IPv6StaticRoute/peer-recovery -o json | jq -e '.items[0].status.phase == "Installed"' >/dev/null
stage=forwarding
ping -6 -c 1 -W 2 fd42:171::99 >/dev/null
stage=restart-reconcile
systemctl restart routerd; systemctl is-active --quiet routerd; ping -6 -c 1 -W 2 fd42:171::99 >/dev/null
stage=withdrawal
install -m 0600 "$empty" /usr/local/etc/routerd/router.yaml; systemctl restart routerd
for _ in {1..30}; do ! ip -j -d -6 route show fd42:171::/64 | jq -e '.[] | select(.dst == "fd42:171::/64" and .gateway == "fd42:170::2" and .dev == "rd15-v6e-host" and .metric == 79)' >/dev/null && break; sleep 1; done
! ip -j -d -6 route show fd42:171::/64 | jq -e '.[] | select(.dst == "fd42:171::/64" and .gateway == "fd42:170::2" and .dev == "rd15-v6e-host" and .metric == 79)' >/dev/null
! ping -6 -c 1 -W 1 fd42:171::99 >/dev/null 2>&1
echo 'PASS: IPv6 static route recovers after a missing-interface failure without replacing the configuration'
