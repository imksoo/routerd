#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Installed make dist product; dedicated empty VM and independently certified RA peer.
set -euo pipefail
if [ "${1:-}" != --disposable-vm ] || [ "$#" != 1 ]; then
 echo "usage: $0 --disposable-vm" >&2
 echo 'Dedicated empty Linux VM only; changes guest service configuration and networkd.' >&2
 exit 2
fi
test "$(id -u)" = 0
systemd-detect-virt --vm --quiet
ns=rd15-ra-upstream
iface=rd15-ra
network=/etc/systemd/network/10-netplan-rd15-ra.network
adoption=$network.d
sysctl_file=/etc/sysctl.d/90-routerd-accept-ra-rd15-ra.conf
rdisc=/opt/routerd15-fixtures/kea/usr/bin/rdisc6
test ! -e "$network"
test ! -e "$adoption"
test ! -e "$sysctl_file"
if ip link show "$iface" >/dev/null 2>&1; then echo "existing trial interface" >&2; exit 1; fi
if ip netns list | awk '{print $1}' | grep -Fxq "$ns"; then echo "existing trial namespace" >&2; exit 1; fi
python3 - <<'PY'
import json
x=json.load(open('/usr/local/etc/routerd/router.yaml')); assert x['spec']['resources']==[]
PY
work=$(mktemp -d /var/tmp/routerd-wan-ra-product.XXXXXX)
printf '%s\n' "$work"
cp /usr/local/etc/routerd/router.yaml "$work/empty.json"
dns='' http=''
finish() {
 local rc=$?
 trap - EXIT
 if [ "$rc" != 0 ]; then
  ip -6 -j addr show dev "$iface" > "$work/failure-address.json" 2>&1 || true
  ip -6 route show dev "$iface" > "$work/failure-route.txt" 2>&1 || true
  sysctl "net.ipv6.conf.$iface.accept_ra" > "$work/failure-sysctl.txt" 2>&1 || true
  routerctl get status -o json > "$work/failure-status.json" 2>&1 || true
  journalctl -u routerd --since "$run_start" --no-pager > "$work/failure-routerd.log" 2>&1 || true
  systemctl stop routerd
 fi
 for pid in "$dns" "$http"; do
  if [ -n "$pid" ]; then kill -CONT "$pid" 2>/dev/null || true; kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; fi
 done
 if [ "$rc" = 0 ]; then rm -r -- "$work"; else echo "FAIL: captured state; fixture retained at $work" >&2; fi
 exit "$rc"
}
run_start=$(date --iso-8601=seconds)
trap finish EXIT
ip netns add "$ns"
ip link add "$iface" type veth peer name ra-peer netns "$ns"
ip link set "$iface" up
peer() { ip netns exec "$ns" "$@"; }
peer ip link set lo up
peer ip link set ra-peer up
peer ip -6 addr add 2001:db8:1600::1/64 dev ra-peer
peer ip -6 addr add 2001:db8:1601::1/128 dev lo
cat > "$network" <<'CONF'
[Match]
Name=rd15-ra
[Network]
DHCP=no
IPv6AcceptRA=no
LinkLocalAddressing=ipv6
CONF
networkctl reload
networkctl reconfigure "$iface"
for _ in {1..30}; do
 if networkctl status "$iface" --no-pager | grep -Fq "$network" && [ "$(sysctl -n net.ipv6.conf.rd15-ra.accept_ra)" = 0 ] && ! peer ip -6 addr show dev ra-peer | grep -q tentative; then break; fi
 sleep 1
done
networkctl status "$iface" --no-pager > "$work/networkd.txt"
grep -F "$network" "$work/networkd.txt"
test "$(sysctl -n net.ipv6.conf.rd15-ra.accept_ra)" = 0
cat > "$work/dnsmasq.conf" <<'CONF'
port=0
no-hosts
no-resolv
user=root
interface=ra-peer
bind-interfaces
enable-ra
dhcp-range=2001:db8:1600::,ra-only,64,120s
ra-param=ra-peer,5,15
CONF
peer dnsmasq --test --conf-file="$work/dnsmasq.conf"
# Launch ip netns exec directly so $! identifies the eventual foreground daemon.
ip netns exec "$ns" dnsmasq --keep-in-foreground --pid-file= --conf-file="$work/dnsmasq.conf" > "$work/peer-ra.log" 2>&1 & dns=$!
cat > "$work/peer_http.py" <<'PY'
import socket
from http.server import BaseHTTPRequestHandler, HTTPServer
class Server(HTTPServer): address_family=socket.AF_INET6
class Handler(BaseHTTPRequestHandler):
 def do_GET(self):
  data=(self.client_address[0]+'\n').encode()
  self.send_response(200); self.send_header('Content-Length',str(len(data))); self.end_headers(); self.wfile.write(data)
Server(('2001:db8:1601::1',8080),Handler).serve_forever()
PY
ip netns exec "$ns" python3 "$work/peer_http.py" > "$work/peer-http.log" 2>&1 & http=$!
for _ in {1..15}; do
 if peer curl --noproxy '*' --silent --fail --max-time 1 'http://[2001:db8:1601::1]:8080/' >/dev/null; then break; fi
 kill -0 "$http"
 sleep 1
done
peer curl --noproxy '*' --silent --fail --max-time 2 'http://[2001:db8:1601::1]:8080/' >/dev/null
"$rdisc" -1 -r 5 -w 2000 "$iface" > "$work/ra.txt"
grep '2001:db8:1600::/64' "$work/ra.txt"
cat > "$work/observe.json" <<'JSON'
{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"wan-ra-readiness"},"spec":{"resources":[{"apiVersion":"net.routerd.net/v1alpha1","kind":"Interface","metadata":{"name":"wan"},"spec":{"ifname":"rd15-ra","managed":true,"owner":"routerd","adminUp":true}},{"apiVersion":"net.routerd.net/v1alpha1","kind":"IPv6RAAddress","metadata":{"name":"wan-ra"},"spec":{"interface":"wan","managed":false}}]}}
JSON
jq '(.spec.resources[]|select(.kind=="IPv6RAAddress").spec.managed)=true' "$work/observe.json" > "$work/managed.json"
routerd validate --config "$work/observe.json"
routerd validate --config "$work/managed.json"
routerctl apply -f "$work/observe.json" --replace > "$work/observe-apply.txt"
test "$(sysctl -n net.ipv6.conf.rd15-ra.accept_ra)" = 0
test ! -e "$adoption"
test -z "$(ip -6 route show default dev "$iface")"
echo 'PASS: fresh observe-only intent leaves networkd/RA acceptance unchanged'
routerctl apply -f "$work/managed.json" --replace > "$work/managed-apply.txt"
ready() {
 ip -6 -j addr show dev "$iface" | jq -e 'any(.[0].addr_info[]; (.local|startswith("2001:db8:1600:")) and .preferred_life_time>0 and .tentative!=true and ((.flags//[])|index("tentative")|not))' >/dev/null &&
 ip -6 route show default dev "$iface" | grep -q 'proto ra'
}
wait_ready() { for _ in {1..30}; do if ready; then return 0; fi; sleep 1; done; return 1; }
probe() {
 local source
 source=$(curl --noproxy '*' --silent --show-error --fail --max-time 3 'http://[2001:db8:1601::1]:8080/')
 [[ "$source" == 2001:db8:1600:* ]]
 printf 'PASS: peer-observed SLAAC source %s\n' "$source"
}
wait_ready
test "$(sysctl -n net.ipv6.conf.rd15-ra.accept_ra)" = 2
probe
router_pid=$(systemctl show routerd -p MainPID --value)
# Freeze the independent RA sender: no zero-lifetime shutdown advertisements.
kill -STOP "$dns"
# Linux can retain an expired row until GC; selection must already reject it.
# Local veth reproduction and Linux v6.8 route.c __find_rr_leaf confirm this.
for _ in {1..25}; do
 if ! ip -6 route get 2001:db8:1601::1 > "$work/expired-route-get.txt" 2>&1; then break; fi
 sleep 1
done
if ip -6 route get 2001:db8:1601::1 > "$work/expired-route-get.txt" 2>&1; then echo 'expired RA route remains selectable' >&2; exit 1; fi
ip -6 route show default dev "$iface" > "$work/expired-route-list.txt"
if curl --noproxy '*' --silent --fail --max-time 2 'http://[2001:db8:1601::1]:8080/' >/dev/null; then echo 'expired RA default still forwards' >&2; exit 1; fi
for _ in {1..125}; do
 if ip -6 -j addr show dev "$iface" | jq -e 'all(.[0].addr_info[]; (.local|startswith("2001:db8:1600:"))|not)' >/dev/null; then break; fi
 sleep 1
done
ip -6 -j addr show dev "$iface" | jq -e 'all(.[0].addr_info[]; (.local|startswith("2001:db8:1600:"))|not)' >/dev/null
echo 'PASS: actual RA default-route and SLAAC lifetimes expired'
kill -CONT "$dns"
wait_ready
probe
test "$(systemctl show routerd -p MainPID --value)" = "$router_pid"
routerctl get status -o json | jq -e 'all(.status.controllers[]; .currentError!=true)' >/dev/null
echo 'PASS: peer-only recovery without routerd restart or manual address/route changes'
routerctl apply -f "$work/empty.json" --replace > "$work/empty-apply.txt"
# Fixture teardown is not evidence of automatic ownership rollback.
rm -- "$network"
if [ -d "$adoption" ]; then rm -r -- "$adoption"; fi
if [ -f "$sysctl_file" ]; then rm -- "$sysctl_file"; fi
ip link delete "$iface"
ip netns delete "$ns"
networkctl reload
echo 'PASS: WAN RA acquisition, natural expiry and recovery; fixture cleaned'
