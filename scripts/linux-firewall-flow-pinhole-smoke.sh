#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Actual packaged FirewallFlowPinhole lifecycle in a dedicated disposable Linux VM.
set -euo pipefail

if [ "${1:-}" != --disposable-vm ] || [ "$#" -ne 1 ]; then
 echo "usage: $0 --disposable-vm" >&2
 exit 2
fi
test "$(id -u)" = 0
systemd-detect-virt --vm --quiet
for x in ip nft jq python3 systemctl; do command -v "$x" >/dev/null; done
host_management=$(ip -4 route show default | awk 'NR == 1 { print $5 }')
if [ -z "$host_management" ]; then
 host_management=$(ip -6 route show default | awk 'NR == 1 { print $5 }')
fi
if [ -z "$host_management" ]; then
 host_management=$(ip -6 route show | awk '$1 == "fe80::/64" { print $3; exit }')
fi
test -n "$host_management"
ip link show "$host_management" >/dev/null
for dev in rd15-fph-lan rd15-fph-wan; do
 if ip link show "$dev" >/dev/null 2>&1; then echo "existing interface: $dev" >&2; exit 1; fi
done
for ns in rd15-fph-client rd15-fph-server; do
 if ip netns list | awk '{print $1}' | grep -Fxq "$ns"; then echo "existing namespace: $ns" >&2; exit 1; fi
done
if nft -j list tables | jq -e '.nftables[].table? | select(.family=="inet" and .name=="routerd_filter")' >/dev/null; then
 echo 'existing product firewall; empty dedicated guest required' >&2
 exit 1
fi

work=$(mktemp -d /var/tmp/routerd15-flow-pinhole.XXXXXX)
stage=setup
forward_before=$(sysctl -n net.ipv4.ip_forward)
empty="$work/empty.json"
cat > "$empty" <<'JSON'
{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"flow-pinhole-smoke"},"spec":{"resources":[]}}
JSON
finish() {
 rc=$?
 trap - EXIT
 if [ "$rc" -ne 0 ]; then
  echo "FAIL at $stage; live state follows before cleanup" >&2
  nft list ruleset >&2 || true
 fi
 /usr/local/sbin/routerctl apply -f "$empty" --replace >/dev/null 2>&1 || true
 pkill -F "$work/server.pid" >/dev/null 2>&1 || true
 ip netns del rd15-fph-client >/dev/null 2>&1 || true
 ip netns del rd15-fph-server >/dev/null 2>&1 || true
 ip link del rd15-fph-lan >/dev/null 2>&1 || true
 ip link del rd15-fph-wan >/dev/null 2>&1 || true
 sysctl -w "net.ipv4.ip_forward=$forward_before" >/dev/null 2>&1 || true
 rm -rf -- "$work"
 if [ "$rc" -ne 0 ]; then echo 'temporary fixture and output removed; repair locally before another VM run' >&2; fi
 exit "$rc"
}
trap finish EXIT

ip netns add rd15-fph-client
ip netns add rd15-fph-server
ip -n rd15-fph-client link set lo up
ip -n rd15-fph-server link set lo up
ip link add rd15-fph-lan type veth peer name peer0 netns rd15-fph-client
ip link add rd15-fph-wan type veth peer name peer0 netns rd15-fph-server
ip link set rd15-fph-lan up
ip link set rd15-fph-wan up
ip -n rd15-fph-client link set peer0 up
ip -n rd15-fph-client addr add 198.18.60.100/24 dev peer0
ip -n rd15-fph-client route add default via 198.18.60.1
ip -n rd15-fph-server link set peer0 up
ip -n rd15-fph-server addr add 198.18.61.10/24 dev peer0
ip -n rd15-fph-server route add 198.18.60.0/24 via 198.18.61.1

cat > "$work/server.py" <<'PY'
import socket, sys
request = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
request.bind(('198.18.61.10', 32100))
reply = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
reply.bind(('198.18.61.10', 32099))
while True:
    data, peer = request.recvfrom(1024)
    reply.sendto(b'opened:' + data, peer)
PY
ip netns exec rd15-fph-server python3 "$work/server.py" &
echo "$!" > "$work/server.pid"
for _ in {1..20}; do
 if ip netns exec rd15-fph-server python3 - <<'PY'
import socket
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.bind(('198.18.61.10', 0))
PY
 then break; fi
 sleep .1
done

cat > "$work/base.json" <<'JSON'
{
  "apiVersion":"routerd.net/v1alpha1", "kind":"Router", "metadata":{"name":"flow-pinhole-smoke"},
  "spec":{"resources":[
    {"apiVersion":"net.routerd.net/v1alpha1","kind":"Interface","metadata":{"name":"lan"},"spec":{"ifname":"rd15-fph-lan","managed":false,"owner":"external"}},
    {"apiVersion":"net.routerd.net/v1alpha1","kind":"IPv4StaticAddress","metadata":{"name":"lan-v4"},"spec":{"interface":"lan","address":"198.18.60.1/24"}},
    {"apiVersion":"net.routerd.net/v1alpha1","kind":"Interface","metadata":{"name":"wan"},"spec":{"ifname":"rd15-fph-wan","managed":false,"owner":"external"}},
    {"apiVersion":"net.routerd.net/v1alpha1","kind":"IPv4StaticAddress","metadata":{"name":"wan-v4"},"spec":{"interface":"wan","address":"198.18.61.1/24"}},
    {"apiVersion":"system.routerd.net/v1alpha1","kind":"Sysctl","metadata":{"name":"forward-v4"},"spec":{"key":"net.ipv4.ip_forward","value":"1"}},
    {"apiVersion":"net.routerd.net/v1alpha1","kind":"IPAddressSet","metadata":{"name":"client"},"spec":{"addresses":["198.18.60.100"]}},
    {"apiVersion":"firewall.routerd.net/v1alpha1","kind":"FirewallZone","metadata":{"name":"lan"},"spec":{"role":"trust","interfaces":["lan"]}},
    {"apiVersion":"firewall.routerd.net/v1alpha1","kind":"FirewallZone","metadata":{"name":"wan"},"spec":{"role":"untrust","interfaces":["wan"]}},
    {"apiVersion":"firewall.routerd.net/v1alpha1","kind":"FirewallPolicy","metadata":{"name":"default"},"spec":{}},
    {"apiVersion":"firewall.routerd.net/v1alpha1","kind":"FirewallFlowPinhole","metadata":{"name":"camera"},"spec":{"fromZone":"lan","toZone":"wan","outbound":{"sourceSetRef":"IPAddressSet/client","protocol":"udp","destinationCIDRs":["198.18.61.10/32"],"destinationPorts":["32100"]},"inbound":{"sourcePorts":["32099"]},"timeout":"2s"}}
  ]}
}
JSON
python3 - "$work/base.json" "$host_management" <<'PY'
import json, sys
path, ifname = sys.argv[1:]
router = json.load(open(path))
resources = router['spec']['resources']
resources.append({'apiVersion':'net.routerd.net/v1alpha1','kind':'Interface','metadata':{'name':'host-management'},'spec':{'ifname':ifname,'managed':False,'owner':'external'}})
resources.append({'apiVersion':'firewall.routerd.net/v1alpha1','kind':'FirewallZone','metadata':{'name':'management'},'spec':{'role':'mgmt','interfaces':['host-management']}})
json.dump(router, open(path, 'w'))
PY
python3 - "$work/base.json" "$work/source-change.json" <<'PY'
import json, sys
router = json.load(open(sys.argv[1]))
for resource in router['spec']['resources']:
    if resource['kind'] == 'IPAddressSet': resource['spec']['addresses'] = ['198.18.60.101']
json.dump(router, open(sys.argv[2], 'w'))
PY
/usr/local/sbin/routerd validate --config "$work/base.json"
/usr/local/sbin/routerd validate --config "$work/source-change.json"
systemctl start routerd
stage=initial-apply
/usr/local/sbin/routerctl apply -f "$work/base.json" --replace >/dev/null
for _ in {1..30}; do
 if nft list table inet routerd_filter | grep -F 'flow_pinhole_camera' >/dev/null; then break; fi
 sleep 1
done
nft list table inet routerd_filter | grep -F 'flow_pinhole_camera' >/dev/null

expect_drop() {
 ip netns exec rd15-fph-client python3 - "$@" <<'PY'
import socket, sys
address, port, payload = sys.argv[1], int(sys.argv[2]), sys.argv[3].encode()
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(('198.18.60.100', 40000))
s.settimeout(1)
s.sendto(payload, (address, port))
try:
    s.recvfrom(1024)
except TimeoutError:
    raise SystemExit(0)
raise SystemExit('unexpected UDP reply')
PY
}
request_reply() {
 ip netns exec rd15-fph-client python3 - "$@" <<'PY'
import socket, sys
address, port, payload = sys.argv[1], int(sys.argv[2]), sys.argv[3].encode()
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(('198.18.60.100', 40000))
s.settimeout(2)
s.sendto(payload, (address, port))
got, peer = s.recvfrom(1024)
if got != b'opened:' + payload or peer != ('198.18.61.10', 32099):
    raise SystemExit(f'reply={got!r} peer={peer!r}')
PY
}
send_server() {
 ip netns exec rd15-fph-server python3 - "$@" <<'PY'
import socket, sys
address, port, payload = sys.argv[1], int(sys.argv[2]), sys.argv[3].encode()
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(('198.18.61.10', 32099))
s.sendto(payload, (address, port))
PY
}
expect_server_drop() {
 local name=$1
 ip netns exec rd15-fph-client python3 - "$work/$name.ready" <<'PY' &
import pathlib, socket, sys
ready = pathlib.Path(sys.argv[1])
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(('198.18.60.100', 40000))
s.settimeout(1)
ready.touch()
try:
    s.recvfrom(1024)
except TimeoutError:
    raise SystemExit(0)
raise SystemExit('unexpected unsolicited UDP reply')
PY
 local receiver_pid=$!
 for _ in {1..20}; do test -e "$work/$name.ready" && break; sleep .1; done
 test -e "$work/$name.ready"
 send_server 198.18.60.100 40000 "$name"
 wait "$receiver_pid"
}

stage=unsolicited-inbound
expect_server_drop unsolicited
stage=learn-and-reply
request_reply 198.18.61.10 32100 opened
stage=expiry
sleep 3
expect_server_drop expired
stage=source-change
/usr/local/sbin/routerctl apply -f "$work/source-change.json" --replace >/dev/null
for _ in {1..20}; do
 if nft list table inet routerd_filter | grep -F '198.18.60.101' >/dev/null; then break; fi
 sleep 1
done
nft list table inet routerd_filter | grep -F '198.18.60.101' >/dev/null
expect_drop 198.18.61.10 32100 source-changed
stage=withdrawal
/usr/local/sbin/routerctl apply -f "$empty" --replace >/dev/null
for _ in {1..20}; do
 if ! nft -j list tables | jq -e '[.nftables[].table? | select(.name=="routerd_filter")] | length > 0' >/dev/null; then break; fi
 sleep 1
done
if nft -j list tables | jq -e '[.nftables[].table? | select(.name=="routerd_filter")] | length > 0' >/dev/null; then
 echo 'empty apply retained routerd_filter' >&2
 exit 1
fi
expect_server_drop withdrawn
printf '%s\n' 'PASS: FirewallFlowPinhole learns, expires, source updates, and withdraws on the installed service'
