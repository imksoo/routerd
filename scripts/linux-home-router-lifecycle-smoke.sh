#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Isolated disposable VM only: clean-installed make dist product; independent dual-stack peers.
set -euo pipefail
if [ "${1:-}" != --disposable-vm ] || [ "$#" -ne 4 ]; then
 echo "usage: $0 --disposable-vm CANDIDATE_1_5_JSON PACKAGE_ROOT KEA_PACKAGE_ROOT" >&2
 echo 'Dedicated Linux VM only; performs a clean 1.5 package install with a prepared config, never 1.0 migration.' >&2
 exit 2
fi
shift
systemd-detect-virt --vm --quiet
candidate=${1:?candidate config}
package_root=${2:?extracted routerd package root}
fixture=${3:?extracted Kea and ndisc6 package root}
test "$(id -u)" = 0
for x in ip ss nft curl jq python3 dig busybox dnsmasq systemctl journalctl timeout; do command -v "$x" >/dev/null; done
test -r /proc/sys/net/netfilter/nf_conntrack_count
test -x "$package_root/install.sh"
test -x "$package_root/uninstall.sh"
test -x "$package_root/bin/routerd"
test -x "$package_root/bin/routerctl"
export LD_LIBRARY_PATH="$fixture/usr/lib/x86_64-linux-gnu"
kea=$fixture/usr/sbin/kea-dhcp6
rdisc=$fixture/usr/bin/rdisc6
"$kea" -v
test "$(stat -c %u "$rdisc")" = 0
run_start=$(date --iso-8601=seconds)
lan_ns=rd15-client
wan_ns=rd15-upstream
names=$(ip netns list)
for ns in "$lan_ns" "$wan_ns"; do
 if printf '%s\n' "$names" | awk '{print $1}' | grep -Fxq "$ns"; then echo "existing namespace: $ns" >&2; exit 1; fi
done
for dev in rd15-lan rd15-wan; do
 if ip link show "$dev" >/dev/null 2>&1; then echo "existing interface: $dev" >&2; exit 1; fi
done
for path in /usr/local/etc/routerd /var/lib/routerd /usr/local/libexec/routerd/dhcp-event-relay; do
 if [ -e "$path" ] || [ -L "$path" ]; then echo "clean-install precondition failed: $path exists" >&2; exit 2; fi
done
for binary in "$package_root"/bin/*; do
 path="/usr/local/sbin/${binary##*/}"
 if [ -e "$path" ] || [ -L "$path" ]; then echo "clean-install precondition failed: $path exists" >&2; exit 2; fi
done
test "$(systemctl show -p LoadState --value routerd.service)" = not-found
"$package_root/bin/routerd" validate --config "$candidate"
package_version=$("$package_root/bin/routerd" --version)
work=$(mktemp -d /var/tmp/routerd15-home.XXXXXX)
active_prefix=2001:db8:1500
printf '%s\n' "$work"
printf '%s\n' "$package_version" > "$work/package-version.txt"
sysctl -n net.ipv4.ip_forward > "$work/forward4.before"
sysctl -n net.ipv6.conf.all.forwarding > "$work/forward.before"
installed=0
dhcp_pid=
peer_kea_pid=
peer_http_pid=
peer_dns_pid=
client_http_pid=
stop_pid() {
 local pid=$1
 [ -n "$pid" ] || return 0
 kill "$pid" >/dev/null 2>&1 || true
 wait "$pid" >/dev/null 2>&1 || true
}
stop_kea() {
 stop_pid "$peer_kea_pid"
 peer_kea_pid=
}
start_kea() {
 ip netns exec "$wan_ns" "$kea" -d -c "$work/kea.json" >> "$work/kea.log" 2>&1 &
 peer_kea_pid=$!
}
finish() {
 rc=$?
 trap - EXIT
 if [ "$rc" -ne 0 ]; then
   # Keep bounded diagnostics from the failing run, then restore the disposable VM.
   cp "$candidate" "$work/failure-candidate.json" 2>/dev/null || true
   ip -j addr show dev rd15-lan > "$work/failure-router-addresses.json" 2>&1 || true
   ip -j addr show dev rd15-wan > "$work/failure-wan-addresses.json" 2>&1 || true
   ip netns exec "$lan_ns" ip -6 -j addr show dev peer0 > "$work/failure-client-addresses.json" 2>&1 || true
   ip netns exec "$lan_ns" ip -4 -j addr show dev peer0 > "$work/failure-client4-addresses.json" 2>&1 || true
   ip netns exec "$lan_ns" ip -6 route show > "$work/failure-client-routes.txt" 2>&1 || true
   timeout 5 /usr/local/sbin/routerctl get status -o json > "$work/failure-status.json" 2>&1 || true
   journalctl -u routerd.service --since "$run_start" --no-pager -n 100 > "$work/failure-routerd-journal.txt" 2>&1 || true
   tail -100 "$work/kea.log" > "$work/failure-kea.txt" 2>/dev/null || true
   tail -100 "$work/udhcpc.log" > "$work/failure-dhcpv4.txt" 2>/dev/null || true
 fi
 stop_pid "$dhcp_pid"
 stop_pid "$peer_kea_pid"
 stop_pid "$peer_http_pid"
 stop_pid "$peer_dns_pid"
 stop_pid "$client_http_pid"
 if [ "$rc" -ne 0 ]; then
   systemctl stop routerd.service >/dev/null 2>&1 || true
 fi
 for ns in "$lan_ns" "$wan_ns"; do ip netns del "$ns" >/dev/null 2>&1 || true; done
 for dev in rd15-lan rd15-wan; do ip link del "$dev" >/dev/null 2>&1 || true; done
 sysctl -w "net.ipv4.ip_forward=$(cat "$work/forward4.before")" >/dev/null 2>&1 || true
 sysctl -w "net.ipv6.conf.all.forwarding=$(cat "$work/forward.before")" >/dev/null 2>&1 || true
 if [ "$installed" -eq 1 ]; then
   systemctl stop routerd.service >/dev/null 2>&1 || true
   if "$package_root/uninstall.sh" --prefix /usr/local --all --yes > "$work/uninstall.log" 2>&1; then
     installed=0
   else
     printf 'routerd package cleanup failed; see %s/uninstall.log\n' "$work" >&2
     rc=1
   fi
 fi
 if [ "$rc" -eq 0 ]; then
   for binary in "$package_root"/bin/*; do
     path="/usr/local/sbin/${binary##*/}"
     if [ -e "$path" ] || [ -L "$path" ]; then echo "clean-install cleanup left $path" >&2; rc=1; fi
   done
   for path in /usr/local/etc/routerd /usr/local/libexec/routerd/dhcp-event-relay /var/lib/routerd /etc/systemd/system/routerd.service; do
     if [ -e "$path" ] || [ -L "$path" ]; then echo "clean-install cleanup left $path" >&2; rc=1; fi
   done
   if [ "$(systemctl show -p LoadState --value routerd.service)" != not-found ]; then echo 'routerd.service remains loaded after uninstall' >&2; rc=1; fi
 fi
 if [ "$rc" -eq 0 ]; then
   # Keep the procedure and verdict; successful raw output is disposable.
   rm -r -- "$work"
 else
   cp "$candidate" "$work/failure-candidate.json" 2>/dev/null || true
   printf 'FAIL: fixture cleaned; bounded diagnostics retained at %s\n' "$work" >&2
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
# The WAN transit is fixture input; routing, forwarding, NAT, DHCP, DNS, PD and
# RA must be supplied by the routerd 1.5 candidate.
ip netns exec "$wan_ns" ip -4 addr add 198.18.16.1/24 dev peer0
ip netns exec "$wan_ns" ip -4 route add 198.18.15.0/24 via 198.18.16.2 dev peer0
ip netns exec "$wan_ns" ip -4 addr add 198.18.17.1/32 dev lo
ip -6 addr add 2001:db8:16::2/64 dev rd15-wan
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
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
class Handler(BaseHTTPRequestHandler):
 def do_GET(self):
  data=(self.client_address[0]+'\n').encode()
  self.send_response(200); self.send_header('Content-Length',str(len(data))); self.end_headers(); self.wfile.write(data)
class V6Server(ThreadingHTTPServer): address_family = socket.AF_INET6
for server in (ThreadingHTTPServer(('198.18.17.1',8080),Handler), V6Server(('2001:db8:17::1',8080),Handler)):
 threading.Thread(target=server.serve_forever,daemon=True).start()
threading.Event().wait()
PY
export LD_LIBRARY_PATH
start_kea
ip netns exec "$wan_ns" python3 "$work/peer_http.py" > "$work/peer-http.log" 2>&1 &
peer_http_pid=$!
ip netns exec "$wan_ns" dnsmasq --keep-in-foreground --conf-file=/dev/null --user=root --no-resolv --bind-dynamic --interface=peer0 --interface=lo --listen-address=198.18.17.1 --listen-address=2001:db8:17::1 --address=/peer.test/198.18.17.1 --address=/peer.test/2001:db8:17::1 > "$work/peer-dns.log" 2>&1 &
peer_dns_pid=$!
for _ in {1..15}; do
 if kill -0 "$peer_kea_pid" 2>/dev/null && ip netns exec "$wan_ns" ss -unl | grep -q ':547 ' && ip netns exec "$wan_ns" curl --noproxy '*' --silent --fail --max-time 1 'http://[2001:db8:17::1]:8080/' >/dev/null; then break; fi
 sleep 1
done
ip netns exec "$wan_ns" ss -unl | grep ':547 '
ip netns exec "$wan_ns" curl --noproxy '*' --fail --max-time 2 'http://[2001:db8:17::1]:8080/'
test "$(ip netns exec "$wan_ns" dig +short +time=2 +tries=1 @2001:db8:17::1 peer.test AAAA)" = 2001:db8:17::1
test "$(ip netns exec "$wan_ns" dig +short +time=2 +tries=1 @198.18.17.1 peer.test A)" = 198.18.17.1
printf 'Environment fixtures ready: Kea, dual-stack HTTP/DNS peers, and WAN return routes.\n'
install -D -m 0600 "$candidate" /usr/local/etc/routerd/router.yaml
installed=1
(cd "$package_root" && ./install.sh --prefix /usr/local --no-install-deps --no-config-update --enable-service --start-service) > "$work/install.log" 2>&1
test "$(/usr/local/sbin/routerd --version)" = "$package_version"
systemctl is-active --quiet routerd.service
/usr/local/sbin/routerd validate --config /usr/local/etc/routerd/router.yaml
/usr/local/sbin/routerctl apply -f "$candidate" --replace > "$work/apply.txt"
cat > "$work/udhcpc-hook" <<EOF
#!/bin/sh
set -eu
case "\$1" in
  bound|renew)
    ip addr replace "\$ip/\$mask" dev "\$interface"
    ip route replace default via "\${router%% *}" dev "\$interface"
    printf '%s ip=%s router=%s dns=%s\\n' "\$1" "\$ip" "\$router" "\$dns" >> "$work/dhcpv4-events.txt"
    [ "\$router" = 198.18.15.1 ] && [ "\$dns" = 198.18.15.1 ]
    ;;
esac
EOF
chmod 700 "$work/udhcpc-hook"
ip netns exec "$lan_ns" busybox udhcpc -f -i peer0 -T 2 -t 5 -A 5 -s "$work/udhcpc-hook" > "$work/udhcpc.log" 2>&1 &
dhcp_pid=$!
for _ in {1..45}; do
 if grep -q '^bound ' "$work/dhcpv4-events.txt" 2>/dev/null && ip netns exec "$lan_ns" ip -4 -j addr show dev peer0 | jq -e 'any(.[0].addr_info[]; .local | test("^198\\.18\\.15\\.(10[0-9]|110)$"))' >/dev/null; then break; fi
 sleep 1
done
grep -q '^bound ' "$work/dhcpv4-events.txt"
ip netns exec "$lan_ns" ip -4 route show default | grep -F 'via 198.18.15.1'
pdstatus() { curl --fail --silent --max-time 2 --unix-socket /run/routerd/dhcpv6-client/wan-pd.sock http://localhost/v1/status; }
client_ready() {
 ip netns exec "$lan_ns" ip -6 -j addr show dev peer0 | jq -e --arg prefix "$active_prefix:" 'any(.[0].addr_info[]; (.local | startswith($prefix)) and .preferred_life_time>0 and .tentative!=true and .dadfailed!=true and ((.flags // []) | index("tentative") | not))' >/dev/null &&
 ip netns exec "$lan_ns" ip -6 route show default | grep -q 'proto ra'
}
for _ in {1..45}; do
 if pdstatus > "$work/pd.json" && jq -e '.resources[0].phase == "Bound"' "$work/pd.json" >/dev/null && ip -6 addr show dev rd15-lan | grep -q '2001:db8:1500::1/64'; then break; fi
 sleep 1
done
jq -e '.resources[0].phase == "Bound" and .resources[0].observed.currentPrefix == "2001:db8:1500::/60"' "$work/pd.json" >/dev/null
ip -6 addr show dev rd15-lan | grep '2001:db8:1500::1/64'
for _ in {1..30}; do
 if client_ready; then break; fi
 sleep 1
done
# rdisc6 is an independent RS/RA decoder, Linux kernel handles SLAAC itself.
# ndisc6 can return a non-zero status after it has decoded an advertisement.
# The decoded prefix below, rather than that implementation-specific status,
# is the assertion for this independent observer.
ip netns exec "$lan_ns" "$rdisc" -1 -r 5 -w 2000 peer0 > "$work/ra.txt" || true
grep '2001:db8:1500::/64' "$work/ra.txt"
probe_v4() {
 local source answer
 for mode in +notcp +tcp; do
   answer=$(ip netns exec "$lan_ns" dig +time=2 +tries=1 +short "$mode" @198.18.15.1 router.home.arpa A)
   [ "$answer" = 198.18.15.1 ] || { printf "IPv4 local DNS A %s returned: %s\n" "$mode" "$answer" >&2; return 1; }
   answer=$(ip netns exec "$lan_ns" dig +time=2 +tries=1 +short "$mode" @198.18.15.1 router.home.arpa AAAA)
   [ "$answer" = "$active_prefix::1" ] || { printf "IPv4 local DNS AAAA %s returned: %s\n" "$mode" "$answer" >&2; return 1; }
   answer=$(ip netns exec "$lan_ns" dig +time=2 +tries=1 +short "$mode" @198.18.15.1 peer.test A)
   [ "$answer" = 198.18.17.1 ] || { printf "IPv4 forwarded DNS A %s returned: %s\n" "$mode" "$answer" >&2; return 1; }
   answer=$(ip netns exec "$lan_ns" dig +time=2 +tries=1 +short "$mode" @198.18.15.1 peer.test AAAA)
   [ "$answer" = 2001:db8:17::1 ] || { printf "IPv4 forwarded DNS AAAA %s returned: %s\n" "$mode" "$answer" >&2; return 1; }
 done
 source=$(ip netns exec "$lan_ns" curl --noproxy '*' --fail --silent --show-error --max-time 3 http://198.18.17.1:8080/)
 [ "$source" = 198.18.16.2 ] || { printf 'IPv4 NAT peer observed source %s\n' "$source" >&2; return 1; }
 printf 'PASS: DHCPv4 client, IPv4 DNS UDP/TCP, peer-observed NAT source %s\n' "$source"
}
probe_v6() {
 local source answer
 source=$(ip netns exec "$lan_ns" curl --noproxy '*' --fail --silent --show-error --max-time 3 'http://[2001:db8:17::1]:8080/')
 [[ "$source" == "$active_prefix:"* ]] || { printf 'IPv6 peer observed unexpected source %s\n' "$source" >&2; return 1; }
 for mode in +notcp +tcp; do
   answer=$(ip netns exec "$lan_ns" dig +time=2 +tries=1 +short "$mode" "@$active_prefix::1" router.home.arpa A)
   [ "$answer" = 198.18.15.1 ] || { printf "IPv6 local DNS A %s returned: %s\n" "$mode" "$answer" >&2; return 1; }
   answer=$(ip netns exec "$lan_ns" dig +time=2 +tries=1 +short "$mode" "@$active_prefix::1" router.home.arpa AAAA)
   [ "$answer" = "$active_prefix::1" ] || { printf "IPv6 local DNS AAAA %s returned: %s\n" "$mode" "$answer" >&2; return 1; }
   answer=$(ip netns exec "$lan_ns" dig +time=2 +tries=1 +short "$mode" "@$active_prefix::1" peer.test A)
   [ "$answer" = 198.18.17.1 ] || { printf "IPv6 forwarded DNS A %s returned: %s\n" "$mode" "$answer" >&2; return 1; }
   answer=$(ip netns exec "$lan_ns" dig +time=2 +tries=1 +short "$mode" "@$active_prefix::1" peer.test AAAA)
   [ "$answer" = 2001:db8:17::1 ] || { printf "IPv6 forwarded DNS AAAA %s returned: %s\n" "$mode" "$answer" >&2; return 1; }
 done
 printf 'PASS: SLAAC, RA default route, IPv6 DNS UDP/TCP and peer-observed source %s\n' "$source"
}
probe() {
 probe_v4
 probe_v6
}
probe
# A listening LAN service proves that inbound failure comes from the active
# trust/untrust policy, with both return routes already installed.
cat > "$work/client_http.py" <<'PY'
import socket
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
class Handler(BaseHTTPRequestHandler):
 def do_GET(self):
  data=(self.client_address[0]+'\n').encode()
  self.send_response(200); self.send_header('Content-Length',str(len(data))); self.end_headers(); self.wfile.write(data)
class V6Server(ThreadingHTTPServer):
 address_family = socket.AF_INET6
 def server_bind(self):
  self.socket.setsockopt(socket.IPPROTO_IPV6,socket.IPV6_V6ONLY,1)
  super().server_bind()
for server in (ThreadingHTTPServer(('0.0.0.0',8090),Handler), V6Server(('::',8090),Handler)):
 threading.Thread(target=server.serve_forever,daemon=True).start()
threading.Event().wait()
PY
ip netns exec "$lan_ns" python3 "$work/client_http.py" > "$work/client-http.log" 2>&1 &
client_http_pid=$!
client_v4=$(ip netns exec "$lan_ns" ip -4 -o addr show dev peer0 | awk '{split($4,a,"/");print a[1];exit}')
client_v6=$(ip netns exec "$lan_ns" ip -6 -j addr show dev peer0 | jq -er --arg prefix "$active_prefix:" '[.[0].addr_info[] | select(.local | startswith($prefix)) | select(.tentative!=true) | .local][0]')
test "$(ip netns exec "$lan_ns" curl --noproxy '*' --fail --silent --max-time 2 "http://$client_v4:8090/")" = "$client_v4"
test "$(ip netns exec "$lan_ns" curl --noproxy '*' --fail --silent --max-time 2 -g "http://[$client_v6]:8090/")" = "$client_v6"
if ip netns exec "$wan_ns" curl --noproxy '*' --fail --silent --max-time 2 "http://$client_v4:8090/"; then echo 'untrusted IPv4 reached the LAN service' >&2; exit 1; fi
if ip netns exec "$wan_ns" curl --noproxy '*' --fail --silent --max-time 2 -g "http://[$client_v6]:8090/"; then echo 'untrusted IPv6 reached the LAN service' >&2; exit 1; fi
printf 'PASS: default firewall policy blocks unsolicited WAN-to-LAN IPv4/IPv6\n'
# The DHCPv4 lease is intentionally short so this observes a natural Renew,
# not a second client start or an injected lease.
for _ in {1..90}; do
 if grep -q '^renew ' "$work/dhcpv4-events.txt" 2>/dev/null; then break; fi
 sleep 1
done
grep '^renew ' "$work/dhcpv4-events.txt" > "$work/dhcpv4-renew.txt"
printf 'PASS: DHCPv4 client renewed the active lease without restart\n'
# Both families must fail when the independent WAN peer is disconnected and
# recover after the same link returns.
ip netns exec "$wan_ns" ip link set peer0 down
sleep 1
if ip netns exec "$lan_ns" curl --noproxy '*' --fail --silent --max-time 2 http://198.18.17.1:8080/; then echo 'IPv4 traffic survived WAN disconnect' >&2; exit 1; fi
if ip netns exec "$lan_ns" curl --noproxy '*' --fail --silent --max-time 2 'http://[2001:db8:17::1]:8080/'; then echo 'IPv6 traffic survived WAN disconnect' >&2; exit 1; fi
ip netns exec "$wan_ns" ip link set peer0 up
for _ in {1..30}; do
 if ip netns exec "$lan_ns" curl --noproxy '*' --fail --silent --max-time 2 http://198.18.17.1:8080/ > "$work/recovered-v4-source" && ip netns exec "$lan_ns" curl --noproxy '*' --fail --silent --max-time 2 'http://[2001:db8:17::1]:8080/' > "$work/recovered-v6-source"; then break; fi
 sleep 1
done
test "$(cat "$work/recovered-v4-source")" = 198.18.16.2
[[ "$(cat "$work/recovered-v6-source")" == "$active_prefix:"* ]]
probe
printf 'PASS: IPv4/IPv6 forwarding stops on upstream loss and recovers on return\n'
# Wait for the actual helper's Renew exchange and acknowledged new lease.
lease_file=/var/lib/routerd/dhcpv6-client/wan-pd/lease.json
first_reply=$(jq -r .leaseUpdatedAt "$lease_file")
for _ in {1..40}; do
 if grep 'RENEW' "$work/kea.log" >/dev/null && jq -e --arg first "$first_reply" '.state == "bound" and .leaseUpdatedAt > $first' "$lease_file" >/dev/null; then break; fi
 sleep 1
done
grep 'RENEW' "$work/kea.log" > "$work/renew.txt"
jq -e --arg first "$first_reply" '.state == "bound" and .leaseUpdatedAt > $first' "$lease_file" >/dev/null
probe
# Change the independent server pool without restarting either product daemon.
# Kea removes out-of-pool leases on Renew and supplies the replacement prefix.
router_pid=$(systemctl show -p MainPID --value routerd)
kea_pid=$peer_kea_pid
ip netns exec "$wan_ns" ip -6 route add 2001:db8:1501::/60 via 2001:db8:16::2 dev peer0
jq '.Dhcp6.subnet6[0]["pd-pools"][0].prefix="2001:db8:1501::"' "$work/kea.json" > "$work/kea-next.json"
ip netns exec "$wan_ns" "$kea" -t "$work/kea-next.json"
mv "$work/kea-next.json" "$work/kea.json"
kill -HUP "$peer_kea_pid"
for _ in {1..60}; do
 if pdstatus > "$work/pd-renumber.json" && jq -e '.resources[0].phase == "Bound" and .resources[0].observed.currentPrefix == "2001:db8:1501::/60"' "$work/pd-renumber.json" >/dev/null && ip -6 addr show dev rd15-lan | grep -q '2001:db8:1501::1/64'; then break; fi
 sleep 1
done
jq -e '.resources[0].phase == "Bound" and .resources[0].observed.currentPrefix == "2001:db8:1501::/60"' "$work/pd-renumber.json" >/dev/null
active_prefix=2001:db8:1501
for _ in {1..30}; do
 if client_ready && ! ip -6 addr show dev rd15-lan | grep -q '2001:db8:1500::1/64'; then break; fi
 sleep 1
done
test "$(systemctl show -p MainPID --value routerd)" = "$router_pid"
test "$peer_kea_pid" = "$kea_pid"
ip netns exec "$lan_ns" "$rdisc" -1 -r 5 -w 2000 peer0 > "$work/ra-renumber.txt" || true
grep '2001:db8:1501::/64' "$work/ra-renumber.txt"
if ip -6 addr show dev rd15-lan | grep -q '2001:db8:1500::1/64'; then echo 'revoked router address remains' >&2; exit 1; fi
# A short-lived SLAAC address can remain until its original expiry under the
# RFC 4862 two-hour rule, but must no longer be preferred for new connections.
ip netns exec "$lan_ns" ip -6 -j addr show dev peer0 | jq -e 'all(.[0].addr_info[]; ((.local | startswith("2001:db8:1500:")) | not) or .preferred_life_time==0)' >/dev/null
probe
for mode in +notcp +tcp; do
 if ip netns exec "$lan_ns" dig +time=1 +tries=1 "$mode" @2001:db8:1500::1 router.home.arpa AAAA > "$work/old-dns.txt" 2>&1; then echo 'revoked DNS endpoint still answers' >&2; exit 1; fi
done
printf 'PASS: independent server renumber, new SLAAC/source/DNS and revoked old router address\n'
systemctl restart routerd.service
for _ in {1..30}; do
 if pdstatus > "$work/pd-restart.json" && jq -e '.resources[0].phase == "Bound"' "$work/pd-restart.json" >/dev/null; then break; fi
 sleep 1
done
jq -e '.resources[0].phase == "Bound"' "$work/pd-restart.json" >/dev/null
probe
# Verify expiry removes forwarding authority without changing product state manually.
wait_expired() {
stop_kea
for _ in {1..110}; do
 if ! ip -6 addr show dev rd15-lan | grep -q "$active_prefix::1/64" && ! ip netns exec "$lan_ns" ip -6 route show default | grep -q 'proto ra'; then break; fi
 sleep 1
done
ip -6 addr show dev rd15-lan > "$work/expired-router-addresses.txt"
ip netns exec "$lan_ns" ip -6 route show default > "$work/expired-client-routes.txt"
if grep -q "$active_prefix::1/64" "$work/expired-router-addresses.txt" || grep -q 'proto ra' "$work/expired-client-routes.txt"; then
 echo 'expired authority remains on router/client' >&2; exit 1
fi
if ip netns exec "$lan_ns" curl --noproxy '*' --fail --silent --max-time 2 'http://[2001:db8:17::1]:8080/'; then echo 'traffic survived expiry' >&2; exit 1; fi
systemctl is-active --quiet routerd
probe_v4
printf 'PASS: expired PD withdrawn from router address, client default route and traffic\n'
}
wait_expired
start_kea
for _ in {1..90}; do
 if pdstatus > "$work/pd-return.json" && jq -e '.resources[0].phase == "Bound" and .resources[0].observed.currentPrefix == "2001:db8:1501::/60"' "$work/pd-return.json" >/dev/null && client_ready; then break; fi
 sleep 1
done
jq -e '.resources[0].phase == "Bound" and .resources[0].observed.currentPrefix == "2001:db8:1501::/60"' "$work/pd-return.json" >/dev/null
client_ready
probe
printf 'PASS: expired upstream returned and forwarding/DNS recovered automatically\n'
cat > "$work/empty.json" <<'JSON'
{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"home-readiness"},"spec":{"resources":[]}}
JSON
/usr/local/sbin/routerctl apply -f "$work/empty.json" --replace > "$work/withdraw.txt"
for _ in {1..45}; do
 /usr/local/sbin/routerctl get status -o json > "$work/empty-status.json"
 if jq -e '.status.phase == "Healthy" and (.status.resourceCount // 0) == 0 and ((.status.resourcePhaseIssues // []) | length) == 0 and all(.status.controllers[]?; (.currentError // "") == "")' "$work/empty-status.json" >/dev/null; then break; fi
 sleep 1
done
jq -e '.status.phase == "Healthy" and (.status.resourceCount // 0) == 0 and ((.status.resourcePhaseIssues // []) | length) == 0 and all(.status.controllers[]?; (.currentError // "") == "")' "$work/empty-status.json" >/dev/null
if ip -4 addr show dev rd15-lan | grep -q '198.18.15.1/'; then echo 'LAN IPv4 address survived withdrawal' >&2; exit 1; fi
if ip -4 addr show dev rd15-wan | grep -q '198.18.16.2/'; then echo 'WAN IPv4 address survived withdrawal' >&2; exit 1; fi
if ip -6 addr show dev rd15-lan | grep -q '2001:db8:1501::1/64'; then echo 'LAN IPv6 address survived withdrawal' >&2; exit 1; fi
if ip -4 route show default | grep -Fq 'via 198.18.16.1'; then echo 'test IPv4 default route survived withdrawal' >&2; exit 1; fi
if ip -6 route show default | grep -Fq 'via 2001:db8:16::1'; then echo 'test IPv6 default route survived withdrawal' >&2; exit 1; fi
if ip netns exec "$lan_ns" curl --noproxy '*' --fail --silent --max-time 2 http://198.18.17.1:8080/; then echo 'IPv4 traffic survived config withdrawal' >&2; exit 1; fi
if ip netns exec "$lan_ns" curl --noproxy '*' --fail --silent --max-time 2 'http://[2001:db8:17::1]:8080/'; then echo 'IPv6 traffic survived config withdrawal' >&2; exit 1; fi
printf 'PASS: 1.5 empty-config withdrawal removes IPv4/IPv6 resources and traffic paths\n'
printf 'PASS: clean-install home-router lifecycle: DHCPv4 renew, DNS UDP/TCP, NAT44, DHCPv6-PD renew/renumber/expiry/reacquire, RA/SLAAC, IPv6 forwarding, restart, upstream loss/return and withdrawal\n'
