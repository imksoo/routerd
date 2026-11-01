#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Actual packaged IPv6StaticRoute lifecycle in a dedicated disposable Linux VM.
set -euo pipefail

if [ "${1:-}" != --disposable-vm ] || [ "$#" -ne 1 ]; then
 echo "usage: $0 --disposable-vm" >&2
 exit 2
fi
test "$(id -u)" = 0
systemd-detect-virt --vm --quiet
for x in ip jq ping systemctl; do command -v "$x" >/dev/null; done
for dev in rd15-v6r-host; do
 if ip link show "$dev" >/dev/null 2>&1; then echo "existing interface: $dev" >&2; exit 1; fi
done
if ip netns list | awk '{print $1}' | grep -Fxq rd15-v6r-peer; then
 echo 'existing namespace: rd15-v6r-peer' >&2
 exit 1
fi

work=$(mktemp -d /var/tmp/routerd15-ipv6-static-route.XXXXXX)
stage=setup
empty="$work/empty.json"
cat > "$empty" <<'JSON'
{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"ipv6-static-route-smoke"},"spec":{"resources":[]}}
JSON
finish() {
 rc=$?
 trap - EXIT
 if [ "$rc" -ne 0 ]; then
  echo "FAIL at $stage; live state follows before cleanup" >&2
  ip -j -d -6 route show fd42:150::99/128 >&2 || true
  ip -6 route get fd42:150::99 >&2 || true
  systemctl status routerd --no-pager >&2 || true
 fi
 install -m 0600 "$empty" /usr/local/etc/routerd/router.yaml >/dev/null 2>&1 || true
 systemctl restart routerd >/dev/null 2>&1 || true
 ip netns del rd15-v6r-peer >/dev/null 2>&1 || true
 ip link del rd15-v6r-host >/dev/null 2>&1 || true
 rm -rf -- "$work"
 if [ "$rc" -ne 0 ]; then echo 'temporary fixture and output removed; repair locally before another VM run' >&2; fi
 exit "$rc"
}
trap finish EXIT

ip netns add rd15-v6r-peer
ip -n rd15-v6r-peer link set lo up
ip link add rd15-v6r-host type veth peer name peer0 netns rd15-v6r-peer
ip link set rd15-v6r-host up
ip -6 addr add fd42:149::1/64 nodad dev rd15-v6r-host
ip -n rd15-v6r-peer link set peer0 up
ip -n rd15-v6r-peer addr add fd42:149::2/64 nodad dev peer0
ip -n rd15-v6r-peer addr add fd42:150::99/128 nodad dev lo

cat > "$work/route.json" <<'JSON'
{
  "apiVersion":"routerd.net/v1alpha1", "kind":"Router", "metadata":{"name":"ipv6-static-route-smoke"},
  "spec":{"resources":[
    {"apiVersion":"net.routerd.net/v1alpha1","kind":"Interface","metadata":{"name":"upstream"},"spec":{"ifname":"rd15-v6r-host","managed":false,"owner":"external"}},
    {"apiVersion":"net.routerd.net/v1alpha1","kind":"IPv6StaticRoute","metadata":{"name":"peer-loopback"},"spec":{"interface":"upstream","destination":"fd42:150::99/128","via":"fd42:149::2","metric":77}}
  ]}
}
JSON
/usr/local/sbin/routerd validate --config "$work/route.json"
install -m 0600 "$work/route.json" /usr/local/etc/routerd/router.yaml
systemctl restart routerd
stage=initial-apply
for _ in {1..30}; do
 if ip -j -d -6 route show fd42:150::99/128 | jq -e '
   .[] | select((.dst == "fd42:150::99" or .dst == "fd42:150::99/128") and .gateway == "fd42:149::2" and .dev == "rd15-v6r-host" and .metric == 77 and (.protocol == "boot" or .protocol == "3"))' >/dev/null; then break; fi
 sleep 1
done
ip -j -d -6 route show fd42:150::99/128 | jq -e '
 .[] | select((.dst == "fd42:150::99" or .dst == "fd42:150::99/128") and .gateway == "fd42:149::2" and .dev == "rd15-v6r-host" and .metric == 77 and (.protocol == "boot" or .protocol == "3"))' >/dev/null
stage=forwarding
ping -6 -c 1 -W 2 fd42:150::99 >/dev/null
stage=restart-reconcile
systemctl restart routerd
systemctl is-active --quiet routerd
ping -6 -c 1 -W 2 fd42:150::99 >/dev/null
stage=withdrawal
install -m 0600 "$empty" /usr/local/etc/routerd/router.yaml
systemctl restart routerd
for _ in {1..30}; do
 if ! ip -j -d -6 route show fd42:150::99/128 | jq -e '
   .[] | select((.dst == "fd42:150::99" or .dst == "fd42:150::99/128") and .gateway == "fd42:149::2" and .dev == "rd15-v6r-host" and .metric == 77)' >/dev/null; then break; fi
 sleep 1
done
if ip -j -d -6 route show fd42:150::99/128 | jq -e '
 .[] | select((.dst == "fd42:150::99" or .dst == "fd42:150::99/128") and .gateway == "fd42:149::2" and .dev == "rd15-v6r-host" and .metric == 77)' >/dev/null; then
 echo 'empty apply retained the IPv6StaticRoute' >&2
 exit 1
fi
if ping -6 -c 1 -W 1 fd42:150::99 >/dev/null 2>&1; then
 echo 'withdrawn IPv6StaticRoute still forwarded traffic' >&2
 exit 1
fi
printf '%s\n' 'PASS: IPv6StaticRoute installs, forwards, survives service restart, and withdraws on the installed service'
