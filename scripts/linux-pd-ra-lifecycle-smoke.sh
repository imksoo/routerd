#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Isolated disposable VM only: installed make dist product; independent Kea/SLAAC peers.
set -euo pipefail
if [ "${1:-}" != --disposable-vm ] || [ "$#" -ne 3 ]; then
 echo "usage: $0 --disposable-vm CANDIDATE_JSON KEA_PACKAGE_ROOT" >&2
 echo 'Dedicated empty Linux VM only; changes service configuration and guest networking.' >&2
 exit 2
fi
shift
systemd-detect-virt --vm --quiet
candidate=${1:?candidate config}
fixture=${2:?extracted Kea and ndisc6 package root}
test "$(id -u)" = 0
for x in ip curl jq python3 dig; do command -v "$x" >/dev/null; done
export LD_LIBRARY_PATH="$fixture/usr/lib/x86_64-linux-gnu"
kea=$fixture/usr/sbin/kea-dhcp6
rdisc=$fixture/usr/bin/rdisc6
"$kea" -v
test "$(stat -c %u "$rdisc")" = 0
run_start=$(date --iso-8601=seconds)
work=$(mktemp -d /var/tmp/routerd15-home-v6.XXXXXX)
printf '%s\n' "$work"
lan_ns=rd15-client6
wan_ns=rd15-upstream6
names=$(ip netns list)
for ns in "$lan_ns" "$wan_ns"; do
 if printf '%s\n' "$names" | awk '{print $1}' | grep -Fxq "$ns"; then echo "existing namespace: $ns" >&2; exit 1; fi
done
for dev in rd15-lan rd15-wan; do
 if ip link show "$dev" >/dev/null 2>&1; then echo "existing interface: $dev" >&2; exit 1; fi
done
for unit in rd15-peer-kea rd15-peer-http6 rd15-peer-dns6; do
 test "$(systemctl show -p LoadState --value "$unit.service")" = not-found
done
/usr/local/sbin/routerd validate --config "$candidate"
sysctl -n net.ipv6.conf.all.forwarding > "$work/forward.before"
finish() {
 rc=$?
 trap - EXIT
 systemctl stop rd15-peer-kea.service rd15-peer-http6.service rd15-peer-dns6.service >/dev/null 2>&1 || true
 if [ "$rc" -ne 0 ]; then
   systemctl stop routerd.service
   printf 'FAIL: preserved fixture/output for local reproduction at %s\n' "$work" >&2
 else
   # The procedure and verdict are durable; successful raw output is temporary.
   rm -r -- "$work"
 fi
 exit "$rc"
}
trap finish EXIT
for side in lan wan; do
 ns=$lan_ns; [ "$side" = wan ] && ns=$wan_ns
 ip netns add "$ns"
 ip link add "rd15-$side" type veth peer name peer0 netns "$ns"
 ip link set "rd15-$side" up
 ip netns exec "$ns" ip link set lo up
 ip netns exec "$ns" ip link set peer0 up
done
# Fixed WAN transit belongs to the fixture. This gate does not qualify WAN RA/default-route discovery.
ip -6 addr add 2001:db8:16::2/64 dev rd15-wan
ip -6 route add 2001:db8:17::/64 via 2001:db8:16::1 dev rd15-wan
ip netns exec "$wan_ns" ip -6 addr add 2001:db8:16::1/64 dev peer0
ip netns exec "$wan_ns" ip -6 addr add 2001:db8:17::1/128 dev lo
ip netns exec "$wan_ns" ip -6 route add 2001:db8:1500::/60 via 2001:db8:16::2 dev peer0
ip netns exec "$lan_ns" sysctl -qw net.ipv6.conf.peer0.accept_ra=2 net.ipv6.conf.peer0.autoconf=1
# Wait for real kernel DAD; no product snapshots are injected.
for _ in {1..15}; do
 if ! ip -6 addr show dev rd15-wan | grep -q tentative && ! ip netns exec "$wan_ns" ip -6 addr show dev peer0 | grep -q tentative; then break; fi
 sleep 1
done
ip -6 addr show dev rd15-wan > "$work/wan-addresses.txt"
ip netns exec "$wan_ns" ip -6 addr show dev peer0 > "$work/upstream-addresses.txt"
if grep -q tentative "$work/wan-addresses.txt" "$work/upstream-addresses.txt"; then echo 'fixture DAD incomplete' >&2; exit 1; fi
cat > "$work/kea.json" <<EOF
{"Dhcp6": {
 "interfaces-config":{"interfaces":["peer0"]},
 "lease-database":{"type":"memfile","persist":false,"name":"$work/leases.csv"},
 "data-directory":"$work", "renew-timer":20, "rebind-timer":40,
 "preferred-lifetime":60, "valid-lifetime":90,
 "subnet6":[{"id":15,"subnet":"2001:db8:16::/64","interface":"peer0","pools":[],
 "pd-pools":[{"prefix":"2001:db8:1500::","prefix-len":60,"delegated-len":60}]}]
}}
EOF
ip netns exec "$wan_ns" "$kea" -t "$work/kea.json"
cat > "$work/peer_http.py" <<'PY'
import socket
from http.server import BaseHTTPRequestHandler, HTTPServer
class Server(HTTPServer): address_family = socket.AF_INET6
class Handler(BaseHTTPRequestHandler):
 def do_GET(self):
  data=(self.client_address[0]+'\n').encode()
  self.send_response(200); self.send_header('Content-Length',str(len(data))); self.end_headers(); self.wfile.write(data)
Server(('2001:db8:17::1',8080),Handler).serve_forever()
PY
test ! -e /run/kea
systemd-run --unit=rd15-peer-kea --property=Type=exec --property=RuntimeDirectory=kea --setenv="LD_LIBRARY_PATH=$LD_LIBRARY_PATH" ip netns exec "$wan_ns" "$kea" -d -c "$work/kea.json"
systemd-run --unit=rd15-peer-http6 --property=Type=exec ip netns exec "$wan_ns" python3 "$work/peer_http.py"
systemd-run --unit=rd15-peer-dns6 --property=Type=exec ip netns exec "$wan_ns" dnsmasq --keep-in-foreground --conf-file=/dev/null --user=root --no-resolv --bind-interfaces --interface=lo --listen-address=2001:db8:17::1 --address=/peer.test/2001:db8:17::1
for _ in {1..15}; do
 if ip netns exec "$wan_ns" ss -unl | grep -q ':547 ' && ip netns exec "$wan_ns" curl --noproxy '*' --silent --fail --max-time 1 'http://[2001:db8:17::1]:8080/' >/dev/null; then break; fi
 sleep 1
done
ip netns exec "$wan_ns" ss -unl | grep ':547 '
ip netns exec "$wan_ns" curl --noproxy '*' --fail --max-time 2 'http://[2001:db8:17::1]:8080/'
test "$(ip netns exec "$wan_ns" dig +short +time=2 +tries=1 @2001:db8:17::1 peer.test AAAA)" = 2001:db8:17::1
printf 'Environment fixtures ready: Kea listener, independent HTTP peer, WAN transit and return route.\n'
systemctl start routerd.service
/usr/local/sbin/routerctl apply -f "$candidate" --replace > "$work/apply.txt"
pdstatus() { curl --fail --silent --max-time 2 --unix-socket /run/routerd/dhcpv6-client/wan-pd.sock http://localhost/v1/status; }
for _ in {1..45}; do
 if pdstatus > "$work/pd.json" && jq -e '.resources[0].phase == "Bound"' "$work/pd.json" >/dev/null && ip -6 addr show dev rd15-lan | grep -q '2001:db8:1500::1/64'; then break; fi
 sleep 1
done
jq -e '.resources[0].phase == "Bound" and .resources[0].observed.currentPrefix == "2001:db8:1500::/60"' "$work/pd.json" >/dev/null
ip -6 addr show dev rd15-lan | grep '2001:db8:1500::1/64'
for _ in {1..30}; do
 if ip netns exec "$lan_ns" ip -6 addr show dev peer0 scope global | grep -v tentative | grep -q 'inet6 2001:db8:1500:' && ip netns exec "$lan_ns" ip -6 route show default | grep -q 'proto ra'; then break; fi
 sleep 1
done
# rdisc6 is an independent RS/RA decoder, Linux kernel handles SLAAC itself.
ip netns exec "$lan_ns" "$rdisc" -1 -r 5 -w 2000 peer0 > "$work/ra.txt"
grep '2001:db8:1500::/64' "$work/ra.txt"
probe() {
 local source answer
 source=$(ip netns exec "$lan_ns" curl --noproxy '*' --fail --silent --show-error --max-time 3 'http://[2001:db8:17::1]:8080/')
 [[ "$source" == 2001:db8:1500:* ]]
 for mode in +notcp +tcp; do
 answer=$(ip netns exec "$lan_ns" dig +time=2 +tries=1 +short "$mode" @2001:db8:1500::1 router.home.arpa AAAA)
 [ "$answer" = 2001:db8:1500::1 ] || { printf "DNS router.home.arpa %s returned: %s\n" "$mode" "$answer" >&2; return 1; }
 answer=$(ip netns exec "$lan_ns" dig +time=2 +tries=1 +short "$mode" @2001:db8:1500::1 peer.test AAAA)
 [ "$answer" = 2001:db8:17::1 ] || { printf "DNS peer.test %s returned: %s\n" "$mode" "$answer" >&2; return 1; }
 done
 printf 'PASS: SLAAC, RA default route, IPv6 DNS UDP/TCP and peer-observed source %s\n' "$source"
}
probe
# Wait for the actual helper's Renew exchange and acknowledged new lease.
lease_file=/var/lib/routerd/dhcpv6-client/wan-pd/lease.json
first_reply=$(jq -r .leaseUpdatedAt "$lease_file")
for _ in {1..40}; do
 if journalctl -u rd15-peer-kea --since "$run_start" --no-pager | grep 'RENEW' >/dev/null && jq -e --arg first "$first_reply" '.state == "bound" and .leaseUpdatedAt > $first' "$lease_file" >/dev/null; then break; fi
 sleep 1
done
journalctl -u rd15-peer-kea --since "$run_start" --no-pager | grep 'RENEW' > "$work/renew.txt"
jq -e --arg first "$first_reply" '.state == "bound" and .leaseUpdatedAt > $first' "$lease_file" >/dev/null
probe
systemctl restart routerd.service
for _ in {1..30}; do
 if pdstatus > "$work/pd-restart.json" && jq -e '.resources[0].phase == "Bound"' "$work/pd-restart.json" >/dev/null; then break; fi
 sleep 1
done
jq -e '.resources[0].phase == "Bound"' "$work/pd-restart.json" >/dev/null
probe
systemctl stop rd15-peer-kea.service
# Verify expiry removes forwarding authority without changing product state manually.
for _ in {1..110}; do
 if ! ip -6 addr show dev rd15-lan | grep -q '2001:db8:1500::1/64' && ! ip netns exec "$lan_ns" ip -6 route show default | grep -q 'proto ra'; then break; fi
 sleep 1
done
ip -6 addr show dev rd15-lan > "$work/expired-router-addresses.txt"
ip netns exec "$lan_ns" ip -6 route show default > "$work/expired-client-routes.txt"
if grep -q '2001:db8:1500::1/64' "$work/expired-router-addresses.txt" || grep -q 'proto ra' "$work/expired-client-routes.txt"; then
 echo 'expired authority remains on router/client' >&2; exit 1
fi
if ip netns exec "$lan_ns" curl --noproxy '*' --fail --silent --max-time 2 'http://[2001:db8:17::1]:8080/'; then echo 'traffic survived expiry' >&2; exit 1; fi
systemctl is-active --quiet routerd
printf 'PASS: expired PD withdrawn from router address, client default route and traffic\n'
cat > "$work/empty.json" <<'JSON'
{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"ipv6-readiness"},"spec":{"resources":[]}}
JSON
/usr/local/sbin/routerctl apply -f "$work/empty.json" --replace > "$work/withdraw.txt"
systemctl stop rd15-peer-http6.service rd15-peer-dns6.service
ip netns del "$lan_ns"
ip netns del "$wan_ns"
sysctl -w "net.ipv6.conf.all.forwarding=$(cat "$work/forward.before")"
printf 'PASS: PD acquisition, SLAAC, DNS, routed peer, renewal, daemon restart, expiry and withdrawal\n'
