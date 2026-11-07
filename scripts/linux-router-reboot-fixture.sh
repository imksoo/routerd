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
wait_for_lease() {
 client=
 deadline=$((SECONDS + 45))
 while (( SECONDS < deadline )); do
  client=$(awk '$1=="bound"{print $2;exit}' "$work/leases" 2>/dev/null || true)
  if [[ "$client" =~ ^198\.18\.15\.(10[0-9]|110)$ ]]; then
   printf '%s\n' "$client"
   return 0
  fi
  sleep 0.5
 done
 echo 'DHCP lease was not acquired within 45 seconds' >&2
 return 1
}
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
  management_iface=${2:-$management}
  [[ "$management_iface" =~ ^[a-zA-Z0-9_.-]{1,15}$ ]]
  ip link show "$management_iface" >/dev/null
  sysctl -n net.ipv6.conf.all.forwarding > "$work/ip6_forward.before"
  sysctl -n net.ipv4.ip_forward > "$work/ip_forward.before"
  case "$(cat "$work/ip_forward.before")" in
   0|1) ;;
   *) echo 'cannot record the initial IPv4 forwarding value' >&2; exit 1 ;;
  esac
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
  test ! -e /etc/systemd/network/10-netplan-rd15-wan.network
  test ! -e /etc/systemd/network/10-netplan-rd15-wan.network.d
  test ! -e /etc/sysctl.d/90-routerd-accept-ra-rd15-wan.conf
  cat > /etc/systemd/network/10-netplan-rd15-wan.network <<'NETWORK'
[Match]
Name=rd15-wan
[Network]
DHCP=no
IPv6AcceptRA=no
LinkLocalAddressing=ipv6
[IPv6AcceptRA]
RouteMetric=50
NETWORK
  systemctl daemon-reload
  systemctl start rd15-links
  networkctl reload
  networkctl reconfigure rd15-wan
  # The product's IPv6RAAddress resource owns enabling RA reception. The boot
  # fixture supplies only ports and the disabled networkd baseline.
  for _ in {1..30}; do
   if networkctl status rd15-wan --no-pager | grep -F /etc/systemd/network/10-netplan-rd15-wan.network >/dev/null &&
      [ "$(sysctl -n net.ipv6.conf.rd15-wan.accept_ra)" = 0 ]; then break; fi
   sleep 1
  done
  networkctl status rd15-wan --no-pager | grep -F /etc/systemd/network/10-netplan-rd15-wan.network >/dev/null
  test "$(sysctl -n net.ipv6.conf.rd15-wan.accept_ra)" = 0
  python3 - "$candidate" "$management_iface" "$work/candidate.json" <<'PY'
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
  router_wan_ll=${1:-}
  kea_root=${2:-}
  if [ -n "$router_wan_ll" ]; then
   test -n "$kea_root"
   test -x "$kea_root/usr/sbin/kea-dhcp6"
   command -v radvd >/dev/null
   python3 - "$router_wan_ll" <<'PY6'
import ipaddress,sys
assert ipaddress.IPv6Address(sys.argv[1]).is_link_local
PY6
   printf '%s\n' "$router_wan_ll" > "$work/peer-ipv6"
  fi
  for unit in rd15-http-lan rd15-http-wan rd15-http-admin rd15-upstream-dns rd15-dhcp-client rd15-upstream-ra rd15-upstream-pd; do
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
  if [ -f "$work/peer-ipv6" ]; then
   ip netns exec rd15-wan-peer ip -6 addr add 2001:db8:16::1/64 dev peer0
   ip netns exec rd15-wan-peer ip -6 addr add 2001:db8:17::1/128 dev lo
   ip netns exec rd15-wan-peer ip -6 route add 2001:db8:1500::/60 via "$router_wan_ll" dev peer0
   ip netns exec rd15-wan-peer sysctl -qw net.ipv6.conf.all.forwarding=1
   ip netns exec rd15-lan-peer sysctl -qw net.ipv6.conf.peer0.accept_ra=2 net.ipv6.conf.peer0.autoconf=1
   # Kea can remain active without any sockets if started before link-local DAD.
   for _ in {1..20}; do
    if ip netns exec rd15-wan-peer ip -6 -j addr show dev peer0 | jq -e 'any(.[0].addr_info[]; .scope=="link") and all(.[0].addr_info[]; .tentative!=true and .dadfailed!=true and ((.flags//[])|index("tentative")|not))' >/dev/null; then break; fi
    sleep 0.5
   done
   ip netns exec rd15-wan-peer ip -6 -j addr show dev peer0 | jq -e 'any(.[0].addr_info[]; .scope=="link") and all(.[0].addr_info[]; .tentative!=true and .dadfailed!=true and ((.flags//[])|index("tentative")|not))' >/dev/null
   cat > "$work/radvd.conf" <<'RA'
interface peer0 {
 AdvSendAdvert on;
 MinRtrAdvInterval 3;
 MaxRtrAdvInterval 5;
 AdvDefaultLifetime 30;
 prefix 2001:db8:16::/64 { AdvOnLink on; AdvAutonomous on; AdvValidLifetime 120; AdvPreferredLifetime 60; };
};
RA
   cat > "$work/kea.json" <<KEA
{"Dhcp6": {
 "interfaces-config":{"interfaces":["peer0"]},
 "lease-database":{"type":"memfile","persist":false,"name":"$work/pd-leases.csv"},
 "data-directory":"$work", "renew-timer":20,"rebind-timer":40,
 "preferred-lifetime":60,"valid-lifetime":90,
 "subnet6":[{"id":15,"subnet":"2001:db8:16::/64","interface":"peer0","pools":[],
 "pd-pools":[{"prefix":"2001:db8:1500::","prefix-len":60,"delegated-len":60}]}]
}}
KEA
   ip netns exec rd15-wan-peer env LD_LIBRARY_PATH="$kea_root/usr/lib/x86_64-linux-gnu" "$kea_root/usr/sbin/kea-dhcp6" -t "$work/kea.json"
   systemd-run --unit=rd15-upstream-ra --property=Type=exec ip netns exec rd15-wan-peer radvd -n -C "$work/radvd.conf" -p "$work/radvd.pid" -m stderr
   systemd-run --unit=rd15-upstream-pd --property=Type=exec --setenv="LD_LIBRARY_PATH=$kea_root/usr/lib/x86_64-linux-gnu" --setenv="KEA_PIDFILE_DIR=$work" --setenv="KEA_LOCKFILE_DIR=$work" ip netns exec rd15-wan-peer "$kea_root/usr/sbin/kea-dhcp6" -d -c "$work/kea.json"
  fi
  cat > "$work/peer_http.py" <<'PY'
import socket,sys,threading
from http.server import BaseHTTPRequestHandler,HTTPServer
class Handler(BaseHTTPRequestHandler):
 def do_GET(self):
  with open(sys.argv[1],'a') as f:f.write(self.path+' '+self.client_address[0]+'\n')
  data=(self.client_address[0]+'\n').encode()
  self.send_response(200);self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data)
 def log_message(self,*args):pass
class V6Server(HTTPServer):
 address_family=socket.AF_INET6
 def server_bind(self):
  self.socket.setsockopt(socket.IPPROTO_IPV6,socket.IPV6_V6ONLY,1)
  super().server_bind()
threading.Thread(target=V6Server(('::',8080),Handler).serve_forever,daemon=True).start()
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
  dns_args=(--keep-in-foreground --conf-file=/dev/null --user=root --no-resolv --bind-interfaces --interface=peer0 --listen-address=198.18.16.1 --address=/peer.test/198.18.16.1)
  if [ -f "$work/peer-ipv6" ]; then
   dns_args+=(--interface=lo --listen-address=2001:db8:17::1 --address=/peer.test/2001:db8:17::1 '--dhcp-range=198.18.16.2,198.18.16.2,255.255.255.0,2m' '--dhcp-option=option:router,198.18.16.1' '--dhcp-option=option:dns-server,198.18.16.1' --dhcp-leasefile="$work/wan4.leases")
  fi
  systemd-run --unit=rd15-upstream-dns --property=Type=exec ip netns exec rd15-wan-peer dnsmasq "${dns_args[@]}"
  if [ -f "$work/peer-ipv6" ]; then
   for _ in {1..20}; do
    if systemctl is-active --quiet rd15-upstream-ra rd15-upstream-pd rd15-upstream-dns &&
       ip netns exec rd15-wan-peer ss -unl | grep ':547 ' >/dev/null &&
       ip netns exec rd15-wan-peer ss -unl | grep ':67 ' >/dev/null &&
       ip netns exec rd15-wan-peer curl --noproxy '*' --fail --silent --max-time 1 'http://[2001:db8:17::1]:8080/ready6' >/dev/null; then break; fi
    sleep 1
   done
   systemctl is-active --quiet rd15-upstream-ra rd15-upstream-pd rd15-upstream-dns
   ip netns exec rd15-wan-peer ss -unl | grep ':547 ' >/dev/null
   ip netns exec rd15-wan-peer ss -unl | grep ':67 ' >/dev/null
   test "$(ip netns exec rd15-wan-peer dig +time=5 +tries=1 +short @2001:db8:17::1 peer.test AAAA)" = 2001:db8:17::1
   ip netns exec rd15-wan-peer curl --noproxy '*' --fail --silent --max-time 1 'http://[2001:db8:17::1]:8080/ready6' >/dev/null
  fi
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
  client=$(wait_for_lease)
  ip netns exec rd15-lan-peer ip -4 -j addr show dev peer0 | jq -e --arg ip "$client" '.[0].addr_info | any(.local==$ip)' >/dev/null
  for mode in +notcp +tcp; do
   test "$(ip netns exec rd15-lan-peer dig +time=5 +tries=1 +short "$mode" @198.18.15.1 router.home.arpa A)" = 198.18.15.1
   test "$(ip netns exec rd15-lan-peer dig +time=5 +tries=1 +short "$mode" @198.18.15.1 peer.test A)" = 198.18.16.1
  done
  test "$(ip netns exec rd15-lan-peer curl --noproxy '*' --fail --silent --max-time 2 http://198.18.16.1:8080/forward)" = 198.18.16.2
  test "$(ip netns exec rd15-admin-peer curl --noproxy '*' --fail --silent --max-time 2 "http://$client:8080/control")" = 198.18.17.2
  if [ -f "$work/peer-ipv6" ]; then
   deadline=$((SECONDS + 45))
   until ip netns exec rd15-lan-peer ip -6 -j addr show dev peer0 | jq -e 'any(.[0].addr_info[]; (.local|startswith("2001:db8:1500:")) and .preferred_life_time>0 and .tentative!=true and ((.flags//[])|index("tentative")|not))' >/dev/null &&
         ip netns exec rd15-lan-peer ip -6 route show default | grep 'proto ra' >/dev/null; do
    if (( SECONDS >= deadline )); then echo 'Delegated client address/default route not ready within 45 seconds' >&2; exit 1; fi
    sleep 0.5
   done
   for mode in +notcp +tcp; do
    for server in 198.18.15.1 2001:db8:1500::1; do
     test "$(ip netns exec rd15-lan-peer dig +time=5 +tries=1 +short "$mode" "@$server" router.home.arpa A)" = 198.18.15.1
     test "$(ip netns exec rd15-lan-peer dig +time=5 +tries=1 +short "$mode" "@$server" router.home.arpa AAAA)" = 2001:db8:1500::1
     test "$(ip netns exec rd15-lan-peer dig +time=5 +tries=1 +short "$mode" "@$server" peer.test A)" = 198.18.16.1
     test "$(ip netns exec rd15-lan-peer dig +time=5 +tries=1 +short "$mode" "@$server" peer.test AAAA)" = 2001:db8:17::1
    done
   done
   source6=$(ip netns exec rd15-lan-peer curl --noproxy '*' --fail --silent --max-time 3 'http://[2001:db8:17::1]:8080/forward6')
   [[ "$source6" == 2001:db8:1500:* ]]
   echo 'PASS: delegated SLAAC, RA default, dual-stack DNS A/AAAA UDP/TCP and peer-observed IPv6 source'
  fi
  echo 'PASS: live lease, DNS UDP/TCP, NAT and management positive control'
  ;;
 isolation)
  test "$(cat "$work/role")" = setup-peer
  client=$(wait_for_lease)
  tag="deny-$(date +%s%N)"
  if ip netns exec rd15-wan-peer curl --noproxy '*' --fail --silent --max-time 2 "http://$client:8080/$tag"; then echo 'WAN leaked into LAN' >&2; exit 1; fi
  if grep -Fq "/$tag " "$work/lan-http.log"; then echo 'denied packet reached LAN' >&2; exit 1; fi
  if ip netns exec rd15-lan-peer curl --noproxy '*' --fail --silent --max-time 2 "http://198.18.17.2:8080/$tag"; then echo 'LAN leaked into management' >&2; exit 1; fi
  if grep -Fq "/$tag " "$work/admin-http.log"; then echo 'denied packet reached management' >&2; exit 1; fi
  for mode in +notcp +tcp; do
   if ip netns exec rd15-wan-peer dig +time=5 +tries=1 "$mode" @198.18.15.1 router.home.arpa A > "$work/denied-dns" 2>&1; then echo 'WAN reached DNS' >&2; exit 1; fi
  done
  if [ -f "$work/peer-ipv6" ]; then
   client6=$(ip netns exec rd15-lan-peer ip -6 -j addr show dev peer0 | jq -er '[.[0].addr_info[] | select((.local|startswith("2001:db8:1500:")) and .preferred_life_time>0 and .tentative!=true) | .local][0]')
   if ip netns exec rd15-wan-peer curl --noproxy '*' --fail --silent --max-time 2 "http://[$client6]:8080/$tag"; then echo 'IPv6 WAN leaked into LAN' >&2; exit 1; fi
   if grep -Fq "/$tag " "$work/lan-http.log"; then echo 'denied IPv6 packet reached LAN' >&2; exit 1; fi
   for mode in +notcp +tcp; do
    if ip netns exec rd15-wan-peer dig +time=5 +tries=1 "$mode" @2001:db8:1500::1 router.home.arpa AAAA > "$work/denied-dns6" 2>&1; then echo 'WAN reached IPv6 DNS' >&2; exit 1; fi
   done
   echo 'PASS: IPv6 WAN-to-LAN and WAN-to-self DNS isolation'
  fi
  echo 'PASS: WAN/LAN/management and DNS isolation'
  ;;
 cleanup-peer)
  test "$(cat "$work/role")" = setup-peer
  for unit in rd15-dhcp-client rd15-upstream-dns rd15-upstream-pd rd15-upstream-ra rd15-http-lan rd15-http-wan rd15-http-admin; do
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
  install -m 0600 "$work/empty.json" /usr/local/etc/routerd/router.yaml
  nft -j list tables | jq -e '[.nftables[].table? | select(.name=="routerd_filter")] | length==0' >/dev/null
  for side in lan wan admin; do
   ip -4 -j addr show dev "rd15-$side" | jq -e '.[0].addr_info | length==0' >/dev/null
   ip link del "rd15-$side"
  done
  systemctl stop rd15-links
  # Remove the fixture's networkd baseline and its adoption directory only
  # after empty intent; this teardown does not prove automatic RA rollback.
  if [ -d /etc/systemd/network/10-netplan-rd15-wan.network.d ]; then
   rm -r -- /etc/systemd/network/10-netplan-rd15-wan.network.d
  fi
  if [ -f /etc/sysctl.d/90-routerd-accept-ra-rd15-wan.conf ]; then
   rm -- /etc/sysctl.d/90-routerd-accept-ra-rd15-wan.conf
  fi
  rm -- /etc/systemd/network/10-netplan-rd15-wan.network
  networkctl reload
  rm -- /etc/systemd/system/rd15-links.service /etc/systemd/system/routerd.service.d/90-rd15-links.conf
  systemctl daemon-reload
  # Stopping the fixture unit also stops routerd through Requires=. Once that
  # dependency is gone, leave the product running with its withdrawn config.
  systemctl start routerd
  /usr/local/sbin/routerctl get status -o json | jq -e '.status.phase=="Healthy" and (.status.resourceCount // 0)==0 and ([.status.controllers[] | select(.currentError==true)] | length==0)' >/dev/null
  forward_before=$(cat "$work/ip_forward.before")
  case "$forward_before" in
   0|1) sysctl -w "net.ipv4.ip_forward=$forward_before" ;;
   *) echo 'recorded IPv4 forwarding value is invalid' >&2; exit 1 ;;
  esac
  forward6_before=$(cat "$work/ip6_forward.before")
  case "$forward6_before" in
   0|1) sysctl -w "net.ipv6.conf.all.forwarding=$forward6_before" ;;
   *) echo 'recorded IPv6 forwarding value is invalid' >&2; exit 1 ;;
  esac
  rm -r -- "$work"
  ;;
 *) echo 'unknown role' >&2; exit 2;;
esac
