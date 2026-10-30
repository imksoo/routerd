#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Two disposable VMs on an isolated Ethernet segment carrying VLANs 3131-3133.
set -euo pipefail
if [ "${1:-}" != --disposable-vm ]; then echo 'explicit --disposable-vm required' >&2; exit 2; fi
shift
role=${1:?setup-router|setup-peer|probe|isolation|cleanup-peer|cleanup-router}
shift
work=/var/tmp/rd15-reboot
test "$(id -u)" = 0
systemd-detect-virt --vm --quiet
case "$role" in
 setup-router|setup-peer)
  management=${1:?management interface}; shift
  [[ "$management" =~ ^[a-zA-Z0-9_.-]{1,15}$ ]]
  ip link show "$management" >/dev/null
  test ! -e "$work"
  mkdir -m 700 "$work"
  printf '%s\n' "$role" > "$work/role"
  ;;
 *) test -d "$work";;
esac
case "$role" in
 setup-router)
  candidate=${1:?candidate JSON}
  test ! -e /etc/systemd/system/rd15-links.service
  test ! -e /etc/systemd/system/routerd.service.d/90-rd15-links.conf
  cat > "$work/links" <<'LINKS'
#!/bin/bash
set -euo pipefail
parent=$1
for pair in lan:3131 wan:3132 admin:3133; do
 side=${pair%:*}; vid=${pair#*:}
 ip link add link "$parent" name "rd15-$side" type vlan id "$vid"
 ip link set "rd15-$side" up
done
LINKS
  chmod 700 "$work/links"
  cat > /etc/systemd/system/rd15-links.service <<UNIT
[Unit]
Description=Disposable reboot fixture Ethernet ports
After=network.target
Before=routerd.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=$work/links $management
UNIT
  mkdir -p /etc/systemd/system/routerd.service.d
  cat > /etc/systemd/system/routerd.service.d/90-rd15-links.conf <<'UNIT'
[Unit]
Requires=rd15-links.service
After=rd15-links.service
UNIT
  systemctl daemon-reload
  systemctl start rd15-links
  python3 - "$candidate" "$management" "$work/candidate.json" <<'PY'
import json,sys
x=json.load(open(sys.argv[1]));r=x['spec']['resources']
r.append({'apiVersion':'net.routerd.net/v1alpha1','kind':'Interface','metadata':{'name':'management'},'spec':{'ifname':sys.argv[2],'managed':False,'owner':'external'}})
for name,role,interfaces in [('lan','trust',['lan']),('wan','untrust',['wan']),('admin','mgmt',['admin','management'])]:
 r.append({'apiVersion':'firewall.routerd.net/v1alpha1','kind':'FirewallZone','metadata':{'name':name},'spec':{'role':role,'interfaces':interfaces}})
r.append({'apiVersion':'firewall.routerd.net/v1alpha1','kind':'FirewallPolicy','metadata':{'name':'default'},'spec':{}})
json.dump(x,open(sys.argv[3],'w'))
PY
  /usr/local/sbin/routerd validate --config "$work/candidate.json"
  echo 'Router ports ready; apply candidate.json after peer setup.'
  ;;
 setup-peer)
  for unit in rd15-http-lan rd15-http-wan rd15-http-admin rd15-upstream-dns rd15-dhcp-client; do
   test "$(systemctl show -p LoadState --value "$unit.service")" = not-found
  done
  for pair in lan:3131 wan:3132 admin:3133; do
   side=${pair%:*};vid=${pair#*:};ns=rd15-$side-peer
   ip netns add "$ns"
   ip link add link "$management" name "rd15-$side" type vlan id "$vid"
   ip link set "rd15-$side" netns "$ns"
   ip netns exec "$ns" ip link set "rd15-$side" name peer0
   ip netns exec "$ns" ip link set lo up
   ip netns exec "$ns" ip link set peer0 up
  done
  ip netns exec rd15-wan-peer ip addr add 198.18.16.1/24 dev peer0
  ip netns exec rd15-wan-peer ip route add 198.18.15.0/24 via 198.18.16.2
  ip netns exec rd15-admin-peer ip addr add 198.18.17.2/24 dev peer0
  ip netns exec rd15-admin-peer ip route add default via 198.18.17.1
  cat > "$work/peer_http.py" <<'PY'
import sys
from http.server import BaseHTTPRequestHandler,HTTPServer
class Handler(BaseHTTPRequestHandler):
 def do_GET(self):
  with open(sys.argv[1],'a') as f:f.write(self.path+' '+self.client_address[0]+'\n')
  data=(self.client_address[0]+'\n').encode()
  self.send_response(200);self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data)
 def log_message(self,*args):pass
HTTPServer(('0.0.0.0',8080),Handler).serve_forever()
PY
  for side in lan wan admin; do
   systemd-run --unit="rd15-http-$side" --property=Type=exec ip netns exec "rd15-$side-peer" python3 "$work/peer_http.py" "$work/$side-http.log"
   for _ in {1..10}; do
    if ip netns exec "rd15-$side-peer" curl --noproxy '*' --fail --silent --max-time 1 http://127.0.0.1:8080/ready >/dev/null; then break; fi
    sleep 1
   done
   ip netns exec "rd15-$side-peer" curl --noproxy '*' --fail --silent --max-time 1 http://127.0.0.1:8080/ready >/dev/null
  done
  systemd-run --unit=rd15-upstream-dns --property=Type=exec ip netns exec rd15-wan-peer dnsmasq --keep-in-foreground --conf-file=/dev/null --user=root --no-resolv --bind-interfaces --interface=peer0 --listen-address=198.18.16.1 --address=/peer.test/198.18.16.1
  cat > "$work/lease-hook" <<'HOOK'
#!/bin/sh
set -eu
case "$1" in
 bound|renew)
  test "$router" = 198.18.15.1
  test "$dns" = 198.18.15.1
  ip addr replace "$ip/$mask" dev "$interface"
  ip route replace default via "$router" dev "$interface"
  printf '%s %s %s %s\n' "$1" "$ip" "$lease" "$(date +%s)" >> /var/tmp/rd15-reboot/leases
  ;;
 deconfig) ip -4 addr flush dev "$interface";;
esac
HOOK
  chmod 700 "$work/lease-hook"
  systemd-run --unit=rd15-dhcp-client --property=Type=exec ip netns exec rd15-lan-peer busybox udhcpc -f -i peer0 -t 5 -T 2 -s "$work/lease-hook"
  ;;
 probe)
  test "$(cat "$work/role")" = setup-peer
  client=$(awk '$1=="bound"{print $2;exit}' "$work/leases")
  [[ "$client" =~ ^198\.18\.15\.(10[0-9]|110)$ ]]
  ip netns exec rd15-lan-peer ip -4 -j addr show dev peer0 | jq -e --arg ip "$client" '.[0].addr_info | any(.local==$ip)' >/dev/null
  for mode in +notcp +tcp; do
   test "$(ip netns exec rd15-lan-peer dig +time=1 +tries=1 +short "$mode" @198.18.15.1 router.home.arpa A)" = 198.18.15.1
   test "$(ip netns exec rd15-lan-peer dig +time=1 +tries=1 +short "$mode" @198.18.15.1 peer.test A)" = 198.18.16.1
  done
  test "$(ip netns exec rd15-lan-peer curl --noproxy '*' --fail --silent --max-time 2 http://198.18.16.1:8080/forward)" = 198.18.16.2
  test "$(ip netns exec rd15-admin-peer curl --noproxy '*' --fail --silent --max-time 2 "http://$client:8080/control")" = 198.18.17.2
  echo 'PASS: live lease, DNS UDP/TCP, NAT and management positive control'
  ;;
 isolation)
  client=$(awk '$1=="bound"{print $2;exit}' "$work/leases")
  tag="deny-$(date +%s%N)"
  if ip netns exec rd15-wan-peer curl --noproxy '*' --fail --silent --max-time 2 "http://$client:8080/$tag"; then echo 'WAN leaked into LAN' >&2; exit 1; fi
  if grep -Fq "/$tag " "$work/lan-http.log"; then echo 'denied packet reached LAN' >&2; exit 1; fi
  if ip netns exec rd15-lan-peer curl --noproxy '*' --fail --silent --max-time 2 "http://198.18.17.2:8080/$tag"; then echo 'LAN leaked into management' >&2; exit 1; fi
  if grep -Fq "/$tag " "$work/admin-http.log"; then echo 'denied packet reached management' >&2; exit 1; fi
  for mode in +notcp +tcp; do
   if ip netns exec rd15-wan-peer dig +time=1 +tries=1 "$mode" @198.18.15.1 router.home.arpa A > "$work/denied-dns" 2>&1; then echo 'WAN reached DNS' >&2; exit 1; fi
  done
  echo 'PASS: WAN/LAN/management and DNS isolation'
  ;;
 cleanup-peer)
  test "$(cat "$work/role")" = setup-peer
  for unit in rd15-dhcp-client rd15-upstream-dns rd15-http-lan rd15-http-wan rd15-http-admin; do
   if [ "$(systemctl show -p LoadState --value "$unit.service")" != not-found ]; then
    systemctl stop "$unit"
    if systemctl is-failed --quiet "$unit"; then systemctl reset-failed "$unit"; fi
   fi
  done
  for side in lan wan admin; do ip netns del "rd15-$side-peer"; done
  rm -r -- "$work"
  ;;
 cleanup-router)
  test "$(cat "$work/role")" = setup-router
  cat > "$work/empty.json" <<'JSON'
{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"home-readiness"},"spec":{"resources":[]}}
JSON
  /usr/local/sbin/routerctl apply -f "$work/empty.json" --replace > "$work/withdraw.json"
  nft -j list tables | jq -e '[.nftables[].table? | select(.name=="routerd_filter")] | length==0' >/dev/null
  for side in lan wan admin; do
   ip -4 -j addr show dev "rd15-$side" | jq -e '.[0].addr_info | length==0' >/dev/null
   ip link del "rd15-$side"
  done
  systemctl stop rd15-links
  rm -- /etc/systemd/system/rd15-links.service /etc/systemd/system/routerd.service.d/90-rd15-links.conf
  systemctl daemon-reload
  sysctl -w net.ipv4.ip_forward=0
  rm -r -- "$work"
  ;;
 *) echo 'unknown role' >&2; exit 2;;
esac
