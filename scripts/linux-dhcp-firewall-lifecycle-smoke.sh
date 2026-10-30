#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Actual packaged systemd service in an explicitly selected disposable Linux VM.
set -euo pipefail
if [ "${1:-}" != --disposable-vm ] || [ "$#" -ne 3 ]; then
 echo "usage: $0 --disposable-vm CANDIDATE_JSON MANAGEMENT_IFNAME" >&2
 exit 2
fi
shift
candidate=$1
management=$2
test "$(id -u)" = 0
systemd-detect-virt --vm --quiet
for x in conntrack dnsmasq nft dig busybox curl python3 jq ip; do command -v "$x" >/dev/null; done
ip link show "$management" >/dev/null
for dev in rd15-lan rd15-wan rd15-admin; do
 if ip link show "$dev" >/dev/null 2>&1; then echo "existing interface: $dev" >&2; exit 1; fi
done
names=$(ip netns list)
for side in lan wan admin; do
 if printf '%s\n' "$names" | awk '{print $1}' | grep -Fxq "rd15-$side-peer"; then echo 'existing namespace' >&2; exit 1; fi
done
units=(rd15-http-lan rd15-http-wan rd15-http-admin rd15-upstream-dns rd15-dhcp-client)
for unit in "${units[@]}"; do test "$(systemctl show -p LoadState --value "$unit.service")" = not-found; done
if nft list table inet routerd_filter >/dev/null 2>&1; then echo 'existing product firewall; empty dedicated guest required' >&2; exit 1; fi
/usr/local/sbin/routerd validate --config "$candidate"
work=$(mktemp -d /var/tmp/routerd15-dhcp-firewall.XXXXXX)
printf '%s\n' "$work"
sysctl -n net.ipv4.ip_forward > "$work/forward.before"
finish() {
 rc=$?
 trap - EXIT
 systemctl stop "${units[@]}" >/dev/null 2>&1 || true
 if [ "$rc" -ne 0 ]; then
  systemctl stop routerd
  printf 'FAIL: fixture retained at %s; reproduce product failures locally before rerun\n' "$work" >&2
 else
  rm -r -- "$work"
 fi
 exit "$rc"
}
trap finish EXIT
python3 - "$candidate" "$management" "$work" <<'PY'
import json,sys
from pathlib import Path
base=json.load(open(sys.argv[1])); work=Path(sys.argv[3])
r=base['spec']['resources']
r.append({'apiVersion':'net.routerd.net/v1alpha1','kind':'Interface','metadata':{'name':'management'},'spec':{'ifname':sys.argv[2],'managed':False,'owner':'external'}})
(work/'base.json').write_text(json.dumps(base))
for name,role,interfaces in [('lan','trust',['lan']),('wan','untrust',['wan']),('admin','mgmt',['admin','management'])]:
 r.append({'apiVersion':'firewall.routerd.net/v1alpha1','kind':'FirewallZone','metadata':{'name':name},'spec':{'role':role,'interfaces':interfaces}})
r.append({'apiVersion':'firewall.routerd.net/v1alpha1','kind':'FirewallPolicy','metadata':{'name':'default'},'spec':{}})
(work/'filtered.json').write_text(json.dumps(base))
r.append({'apiVersion':'firewall.routerd.net/v1alpha1','kind':'FirewallRule','metadata':{'name':'lan-to-admin-http'},'spec':{'fromZone':'lan','toZone':'admin','protocol':'tcp','port':8080,'action':'accept'}})
(work/'exception.json').write_text(json.dumps(base))
PY
/usr/local/sbin/routerd validate --config "$work/filtered.json"
/usr/local/sbin/routerd validate --config "$work/exception.json"
for side in lan wan admin; do
 ns=rd15-$side-peer
 ip netns add "$ns"
 ip link add "rd15-$side" type veth peer name peer0 netns "$ns"
 ip link set "rd15-$side" up
 ip netns exec "$ns" ip link set lo up
 ip netns exec "$ns" ip link set peer0 up
done
ip netns exec rd15-wan-peer ip addr add 198.18.16.1/24 dev peer0
ip netns exec rd15-wan-peer ip route add 198.18.15.0/24 via 198.18.16.2
ip netns exec rd15-admin-peer ip addr add 198.18.17.2/24 dev peer0
ip netns exec rd15-admin-peer ip route add default via 198.18.17.1
cat > "$work/peer_http.py" <<'PY'
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer
class Handler(BaseHTTPRequestHandler):
 def do_GET(self):
  with open(sys.argv[1],'a') as f: f.write(self.path+' '+self.client_address[0]+'\n')
  data=(self.client_address[0]+'\n').encode()
  self.send_response(200);self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data)
HTTPServer(('0.0.0.0',8080),Handler).serve_forever()
PY
for side in lan wan admin; do
 systemd-run --unit="rd15-http-$side" --property=Type=exec ip netns exec "rd15-$side-peer" python3 "$work/peer_http.py" "$work/$side-http.log"
done
systemd-run --unit=rd15-upstream-dns --property=Type=exec ip netns exec rd15-wan-peer dnsmasq --keep-in-foreground --conf-file=/dev/null --user=root --no-resolv --bind-interfaces --interface=peer0 --listen-address=198.18.16.1 --address=/peer.test/198.18.16.1
for side in lan wan admin; do
 for _ in {1..10}; do
  if ip netns exec "rd15-$side-peer" curl --noproxy '*' --fail --silent --max-time 1 http://127.0.0.1:8080/fixture >/dev/null; then break; fi
  sleep 1
 done
 ip netns exec "rd15-$side-peer" curl --noproxy '*' --fail --silent --max-time 2 http://127.0.0.1:8080/fixture >/dev/null
done
for _ in {1..10}; do
 if [ "$(ip netns exec rd15-wan-peer dig +time=1 +tries=1 +short @198.18.16.1 peer.test A)" = 198.18.16.1 ]; then break; fi
 sleep 1
done
test "$(ip netns exec rd15-wan-peer dig +time=1 +tries=1 +short @198.18.16.1 peer.test A)" = 198.18.16.1
systemctl start routerd
/usr/local/sbin/routerctl apply -f "$work/base.json" --replace > "$work/base-apply.txt"
cat > "$work/udhcpc-hook" <<'HOOK'
#!/bin/sh
set -eu
case "$1" in
 bound|renew)
  ip addr replace "$ip/$mask" dev "$interface"
  ip route replace default via "${router%% *}" dev "$interface"
  [ "$router" = 198.18.15.1 ] && [ "$dns" = 198.18.15.1 ] || { echo bad-options >> "$0.events"; exit 1; }
  printf '%s %s %s %s\n' "$1" "$ip" "$lease" "$(date +%s)" >> "$0.events"
  ;;
esac
HOOK
chmod 700 "$work/udhcpc-hook"
systemd-run --unit=rd15-dhcp-client --property=Type=exec ip netns exec rd15-lan-peer busybox udhcpc -f -n -i peer0 -t 5 -T 2 -s "$work/udhcpc-hook"
for _ in {1..20}; do
 if [ -f "$work/udhcpc-hook.events" ] && grep -q '^bound ' "$work/udhcpc-hook.events"; then break; fi
 sleep 1
done
grep '^bound ' "$work/udhcpc-hook.events"
client=$(awk '$1 == "bound" {print $2; exit}' "$work/udhcpc-hook.events")
[[ "$client" =~ ^198\.18\.15\.(10[0-9]|110)$ ]]
http() { ip netns exec "rd15-$1-peer" curl --noproxy '*' --fail --silent --show-error --max-time 3 "http://$2:8080/$3"; }
probe() {
 for mode in +notcp +tcp; do
  test "$(ip netns exec rd15-lan-peer dig +time=2 +tries=1 +short "$mode" @198.18.15.1 router.home.arpa A)" = 198.18.15.1
  test "$(ip netns exec rd15-lan-peer dig +time=2 +tries=1 +short "$mode" @198.18.15.1 peer.test A)" = 198.18.16.1
 done
 test "$(http lan 198.18.16.1 allowed-egress)" = 198.18.16.2
}
probe
# Establish that the prospective denied paths actually work before filtering.
test "$(http wan "$client" baseline-wan-lan)" = 198.18.16.1
test "$(http lan 198.18.17.2 baseline-lan-admin)" = "$client"
test "$(ip netns exec rd15-wan-peer dig +time=2 +tries=1 +short @198.18.15.1 router.home.arpa A)" = 198.18.15.1
/usr/local/sbin/routerctl apply -f "$work/filtered.json" --replace > "$work/filter-apply.txt"
nft -j list table inet routerd_filter > "$work/filter.json"
deny_http() {
 local source=$1 address=$2 destination=$3 tag=$4
 if http "$source" "$address" "$tag"; then echo "unexpected allowed path: $tag" >&2; return 1; fi
 if grep -Fq "/$tag " "$work/$destination-http.log"; then echo "denied request reached destination: $tag" >&2; return 1; fi
}
isolation() {
 deny_http wan "$client" lan deny-wan-lan
 deny_http lan 198.18.17.2 admin deny-lan-admin
 for mode in +notcp +tcp; do
  if ip netns exec rd15-wan-peer dig +time=1 +tries=1 "$mode" @198.18.15.1 router.home.arpa A > "$work/denied-dns.txt" 2>&1; then echo 'WAN reached local DNS' >&2; return 1; fi
 done
 # Positive control after the denied probes: the same LAN server remains live.
 test "$(http admin "$client" allowed-admin-lan)" = 198.18.17.2
 nft -j list table inet routerd_filter > "$work/filter-after.json"
 for chain in wan_to_lan lan_to_admin wan_to_self; do
  jq -e --arg chain "$chain" '[.nftables[].rule? | select(.chain==$chain) | .expr[]?.counter?.packets // empty] | add > 0' "$work/filter-after.json" >/dev/null
 done
}
probe
isolation
# Wait for the independent client's timer-driven DHCPREQUEST/ACK, not SIGUSR1.
for _ in {1..130}; do
 if grep -q '^renew ' "$work/udhcpc-hook.events"; then break; fi
 sleep 1
done
grep '^renew ' "$work/udhcpc-hook.events"
if grep -q bad-options "$work/udhcpc-hook.events"; then echo 'DHCP options changed unexpectedly' >&2; exit 1; fi
awk -v ip="$client" '$1=="renew" && $2==ip && $3>0 {found=1} END{exit !found}' "$work/udhcpc-hook.events"
probe
systemctl restart routerd
probe
isolation
/usr/local/sbin/routerctl apply -f "$work/exception.json" --replace > "$work/exception-apply.txt"
test "$(http lan 198.18.17.2 explicit-exception)" = "$client"
/usr/local/sbin/routerctl apply -f "$work/filtered.json" --replace > "$work/exception-withdraw.txt"
deny_http lan 198.18.17.2 admin deny-removed-exception
# Removing only the firewall must restore the already-proven paths.
/usr/local/sbin/routerctl apply -f "$work/base.json" --replace > "$work/filter-withdraw.txt"
if nft list table inet routerd_filter >/dev/null 2>&1; then echo 'removed firewall table still exists' >&2; exit 1; fi
test "$(http wan "$client" filter-withdrawn-wan-lan)" = 198.18.16.1
test "$(http lan 198.18.17.2 filter-withdrawn-lan-admin)" = "$client"
cat > "$work/empty.json" <<'JSON'
{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"home-readiness"},"spec":{"resources":[]}}
JSON
/usr/local/sbin/routerctl apply -f "$work/empty.json" --replace > "$work/withdraw.txt"
for dev in rd15-lan rd15-wan rd15-admin; do
 ip -4 addr show dev "$dev" > "$work/$dev-addresses.txt"
 if grep -q 'inet 198.18.' "$work/$dev-addresses.txt"; then echo "address survived withdrawal: $dev" >&2; exit 1; fi
done
systemctl stop "${units[@]}"
for side in lan wan admin; do ip netns del "rd15-$side-peer"; done
sysctl -w "net.ipv4.ip_forward=$(cat "$work/forward.before")"
printf 'PASS: DHCP renewal, DNS UDP/TCP, NAT, firewall isolation/counters, service restart, exception removal and withdrawal\n'
