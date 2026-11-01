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
tables=$(nft -j list tables)
if jq -e '.nftables[].table? | select(.family=="inet" and .name=="routerd_filter")' <<< "$tables" >/dev/null; then echo 'existing product firewall; empty dedicated guest required' >&2; exit 1; fi
/usr/local/sbin/routerd validate --config "$candidate"
work=$(mktemp -d /var/tmp/routerd15-dhcp-firewall.XXXXXX)
printf '%s\n' "$work"
sysctl -n net.ipv4.ip_forward > "$work/forward.before"
cat > "$work/empty.json" <<'JSON'
{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"home-readiness"},"spec":{"resources":[]}}
JSON
stage=setup
finish() {
 rc=$?
 trap - EXIT
 systemctl stop "${units[@]}" >/dev/null 2>&1 || true
 if [ "$rc" -ne 0 ]; then
  # A disposable-VM failure must not strand the fixture or require a manual
  # cleanup before a locally repaired candidate can be checked again.
  /usr/local/sbin/routerctl apply -f "$work/empty.json" --replace >/dev/null 2>&1 || true
  systemctl stop routerd >/dev/null 2>&1 || true
 fi
 for side in lan wan admin; do
  ip netns del "rd15-$side-peer" >/dev/null 2>&1 || true
  ip link del "rd15-$side" >/dev/null 2>&1 || true
 done
 sysctl -w "net.ipv4.ip_forward=$(cat "$work/forward.before")" >/dev/null 2>&1 || true
 rm -r -- "$work"
 if [ "$rc" -ne 0 ]; then printf 'FAIL at %s: temporary fixture and output removed; reproduce locally before rerun\n' "$stage" >&2; fi
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
dynamic=json.loads(json.dumps(base))
dynamic['spec']['resources']=[resource for resource in dynamic['spec']['resources'] if resource.get('kind') != 'DHCPv4Reservation']
(work/'dynamic.json').write_text(json.dumps(dynamic))
for name,role,interfaces in [('lan','trust',['lan']),('wan','untrust',['wan']),('admin','mgmt',['admin','management'])]:
 r.append({'apiVersion':'firewall.routerd.net/v1alpha1','kind':'FirewallZone','metadata':{'name':name},'spec':{'role':role,'interfaces':interfaces}})
r.append({'apiVersion':'firewall.routerd.net/v1alpha1','kind':'FirewallPolicy','metadata':{'name':'default'},'spec':{}})
(work/'filtered.json').write_text(json.dumps(base))
r.append({'apiVersion':'firewall.routerd.net/v1alpha1','kind':'FirewallRule','metadata':{'name':'lan-to-admin-http'},'spec':{'fromZone':'lan','toZone':'admin','protocol':'tcp','port':8080,'action':'accept'}})
(work/'exception.json').write_text(json.dumps(base))
PY
/usr/local/sbin/routerd validate --config "$work/filtered.json"
/usr/local/sbin/routerd validate --config "$work/exception.json"
/usr/local/sbin/routerd validate --config "$work/dynamic.json"
for side in lan wan admin; do
 ns=rd15-$side-peer
 ip netns add "$ns"
 ip link add "rd15-$side" type veth peer name peer0 netns "$ns"
 ip link set "rd15-$side" up
 ip netns exec "$ns" ip link set lo up
 if [ "$side" = lan ]; then ip netns exec "$ns" ip link set dev peer0 address 02:00:00:15:00:01; fi
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
reservation_declared() {
 /usr/local/sbin/routerctl get DHCPv4Reservation/printer -o json | jq -e '
  .items | length == 1 and
  .[0].apiVersion == "net.routerd.net/v1alpha1" and
  .[0].kind == "DHCPv4Reservation" and
  .[0].spec.hostname == "printer" and
  .[0].spec.ipAddress == "198.18.15.150" and
  .[0].spec.macAddress == "02:00:00:15:00:01" and
  .[0].spec.server == "lan"' >/dev/null
}
stage=baseline
/usr/local/sbin/routerctl apply -f "$work/base.json" --replace > "$work/base-apply.txt"
stage=reservation-declared
for _ in {1..20}; do if reservation_declared; then break; fi; sleep 1; done
reservation_declared
stage=reservation-runtime
for _ in {1..20}; do
 if jq -e '.zones[] | select(.name == "home") | .dhcpHostRecords[] | select(.hostname == "printer" and .ip == "198.18.15.150" and .mac == "02:00:00:15:00:01" and .source == "DHCPv4Server/lan" and .origin == "reservation")' /var/lib/routerd/dns-resolver/lan/config.json >/dev/null 2>&1; then break; fi
 sleep 1
done
jq -e '.zones[] | select(.name == "home") | .dhcpHostRecords[] | select(.hostname == "printer" and .ip == "198.18.15.150" and .mac == "02:00:00:15:00:01" and .source == "DHCPv4Server/lan" and .origin == "reservation")' /var/lib/routerd/dns-resolver/lan/config.json >/dev/null
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
 deconfig)
  ip -4 addr flush dev "$interface"
  ip route del default dev "$interface" >/dev/null 2>&1 || true
  printf '%s %s\n' "$1" "$(date +%s)" >> "$0.events"
  ;;
esac
HOOK
chmod 700 "$work/udhcpc-hook"
systemd-run --unit=rd15-dhcp-client --property=Type=exec ip netns exec rd15-lan-peer busybox udhcpc -f -n -R -i peer0 -t 5 -T 2 -s "$work/udhcpc-hook"
for _ in {1..20}; do
 if [ -f "$work/udhcpc-hook.events" ] && grep -q '^bound ' "$work/udhcpc-hook.events"; then break; fi
 sleep 1
done
grep '^bound ' "$work/udhcpc-hook.events"
client=$(awk '$1 == "bound" {print $2; exit}' "$work/udhcpc-hook.events")
test "$client" = 198.18.15.150
http() { ip netns exec "rd15-$1-peer" curl --noproxy '*' --fail --silent --show-error --max-time 3 "http://$2:8080/$3"; }
probe() {
 for mode in +notcp +tcp; do
  test "$(ip netns exec rd15-lan-peer dig +time=2 +tries=1 +short "$mode" @198.18.15.1 router.home.arpa A)" = 198.18.15.1
  test "$(ip netns exec rd15-lan-peer dig +time=2 +tries=1 +short "$mode" @198.18.15.1 printer.home.arpa A)" = "$client"
  test "$(ip netns exec rd15-lan-peer dig +time=2 +tries=1 +short "$mode" @198.18.15.1 peer.test A)" = 198.18.16.1
 done
 test "$(http lan 198.18.16.1 allowed-egress)" = 198.18.16.2
}
for _ in {1..20}; do if probe; then break; fi; sleep 1; done
probe
# Establish that the prospective denied paths actually work before filtering.
test "$(http wan "$client" baseline-wan-lan)" = 198.18.16.1
test "$(http lan 198.18.17.2 baseline-lan-admin)" = "$client"
test "$(ip netns exec rd15-wan-peer dig +time=2 +tries=1 +short @198.18.15.1 router.home.arpa A)" = 198.18.15.1
/usr/local/sbin/routerctl apply -f "$work/filtered.json" --replace > "$work/filter-apply.txt"
stage=firewall-isolation
nft -j list table inet routerd_filter > "$work/filter.json"
jq -e '.nftables[].table? | select(.family=="inet" and .name=="routerd_filter") | .comment=="routerd.owner=routerd routerd.generation=1"' "$work/filter.json" >/dev/null
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
stage=dhcp-renewal
for _ in {1..130}; do
 if grep -q '^renew ' "$work/udhcpc-hook.events"; then break; fi
 sleep 1
done
grep '^renew ' "$work/udhcpc-hook.events"
if grep -q bad-options "$work/udhcpc-hook.events"; then echo 'DHCP options changed unexpectedly' >&2; exit 1; fi
awk -v ip="$client" '$1=="renew" && $2==ip && $3>0 {found=1} END{exit !found}' "$work/udhcpc-hook.events"
probe
systemctl restart routerd
stage=service-restart
probe
isolation
stage=firewall-exception
/usr/local/sbin/routerctl apply -f "$work/exception.json" --replace > "$work/exception-apply.txt"
test "$(http lan 198.18.17.2 explicit-exception)" = "$client"
/usr/local/sbin/routerctl apply -f "$work/filtered.json" --replace > "$work/exception-withdraw.txt"
deny_http lan 198.18.17.2 admin deny-removed-exception
# Removing only the firewall must restore the already-proven paths.
stage=firewall-withdrawal
/usr/local/sbin/routerctl apply -f "$work/base.json" --replace > "$work/filter-withdraw.txt"
nft -j list tables > "$work/tables-withdrawn.json"
jq -e '[.nftables[].table? | select(.family=="inet" and .name=="routerd_filter")] | length==0' "$work/tables-withdrawn.json" >/dev/null
test "$(http wan "$client" filter-withdrawn-wan-lan)" = 198.18.16.1
test "$(http lan 198.18.17.2 filter-withdrawn-lan-admin)" = "$client"
# A RELEASE must not erase a configured reservation. After its config is removed,
# the same physical client becomes an ordinary named lease whose removal must be
# durable across a service restart.
stage=reservation-release
systemctl stop rd15-dhcp-client
# udhcpc removes its address on RELEASE. Keep the probe in the same LAN
# namespace with a non-pool address so this asserts DNS state, not a lost route.
ip netns exec rd15-lan-peer ip addr add 198.18.15.151/24 dev peer0
stage=reservation-post-release-declared
for _ in {1..20}; do if reservation_declared; then break; fi; sleep 1; done
reservation_declared
stage=reservation-post-release-runtime
for _ in {1..20}; do
 if jq -e '.zones[] | select(.name == "home") | .dhcpScopes[] | select(.source == "DHCPv4Server/lan")' /var/lib/routerd/dns-resolver/lan/config.json >/dev/null 2>&1; then break; fi
 sleep 1
done
stage=reservation-post-release-source
jq -e '.zones[] | select(.name == "home") | .dhcpScopes[] | select(.source == "DHCPv4Server/lan")' /var/lib/routerd/dns-resolver/lan/config.json >/dev/null
stage=reservation-post-release-runtime
for _ in {1..20}; do
 if jq -e '.zones[] | select(.name == "home") | .dhcpHostRecords[] | select(.hostname == "printer" and .ip == "198.18.15.150" and .mac == "02:00:00:15:00:01" and .source == "DHCPv4Server/lan" and .origin == "reservation")' /var/lib/routerd/dns-resolver/lan/config.json >/dev/null 2>&1; then break; fi
 sleep 1
done
jq -e '.zones[] | select(.name == "home") | .dhcpHostRecords[] | select(.hostname == "printer" and .ip == "198.18.15.150" and .mac == "02:00:00:15:00:01" and .source == "DHCPv4Server/lan" and .origin == "reservation")' /var/lib/routerd/dns-resolver/lan/config.json >/dev/null
for _ in {1..20}; do
 reservation_present=true
 for mode in +notcp +tcp; do
  if [ "$(ip netns exec rd15-lan-peer dig +time=2 +tries=1 +short "$mode" @198.18.15.1 printer.home.arpa A)" != "$client" ]; then reservation_present=false; break; fi
 done
 [ "$reservation_present" = true ] && break
 sleep 1
done
[ "$reservation_present" = true ]
/usr/local/sbin/routerctl apply -f "$work/dynamic.json" --replace > "$work/dynamic-apply.txt"
ip netns exec rd15-lan-peer ip -4 addr flush dev peer0
ip netns exec rd15-lan-peer ip link set peer0 down
ip netns exec rd15-lan-peer ip link set dev peer0 address 02:00:00:15:00:02
ip netns exec rd15-lan-peer ip link set peer0 up
systemd-run --unit=rd15-dhcp-client --property=Type=exec ip netns exec rd15-lan-peer busybox udhcpc -f -n -R -F transient -i peer0 -t 5 -T 2 -s "$work/udhcpc-hook"
for _ in {1..20}; do
 dynamic=$(awk '$1 == "bound" {ip=$2} END {print ip}' "$work/udhcpc-hook.events")
 [ -n "$dynamic" ] && [ "$dynamic" != "$client" ] && break
 sleep 1
done
dynamic=$(awk '$1 == "bound" {ip=$2} END {print ip}' "$work/udhcpc-hook.events")
test "$dynamic" = 198.18.15.100
stage=dynamic-lease
for _ in {1..20}; do
 dynamic_present=true
 for mode in +notcp +tcp; do
  if [ "$(ip netns exec rd15-lan-peer dig +time=2 +tries=1 +short "$mode" @198.18.15.1 transient.home.arpa A)" != "$dynamic" ]; then dynamic_present=false; break; fi
 done
 [ "$dynamic_present" = true ] && break
 sleep 1
done
[ "$dynamic_present" = true ]
stage=dynamic-release
systemctl stop rd15-dhcp-client
ip netns exec rd15-lan-peer ip addr add 198.18.15.151/24 dev peer0
absent() {
 for mode in +notcp +tcp; do
  test -z "$(ip netns exec rd15-lan-peer dig +time=2 +tries=1 +short "$mode" @198.18.15.1 transient.home.arpa A)"
 done
}
for _ in {1..20}; do if absent; then break; fi; sleep 1; done
absent
systemctl restart routerd
stage=dynamic-restart
for _ in {1..20}; do if absent; then break; fi; sleep 1; done
absent
stage=empty-withdrawal
/usr/local/sbin/routerctl apply -f "$work/empty.json" --replace > "$work/withdraw.txt"
for dev in rd15-lan rd15-wan rd15-admin; do
 ip -4 addr show dev "$dev" > "$work/$dev-addresses.txt"
 if grep -q 'inet 198.18.' "$work/$dev-addresses.txt"; then echo "address survived withdrawal: $dev" >&2; exit 1; fi
done
printf 'PASS: DHCP reservation and dynamic-release DNS lifecycle, renewal, NAT, firewall isolation/counters, service restart, exception removal and withdrawal\n'
