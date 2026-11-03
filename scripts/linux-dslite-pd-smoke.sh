#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Formal apt-installed product, dedicated VM, independent Kea and four AFTRs.
set -euo pipefail
if [ "${1:-}" != --disposable-vm ] || [ "$#" -ne 4 ]; then
 echo 'usage: --disposable-vm FIXTURE.json KEA_PACKAGE_ROOT MANAGEMENT_INTERFACE' >&2
 exit 2
fi
shift
fixture=$(realpath "$1"); kea_root=$(realpath "$2"); management=$3
[[ "$management" =~ ^[a-zA-Z0-9_.-]{1,15}$ && "$management" != rd15-* && "$management" != lo ]]
test "$(id -u)" = 0
systemd-detect-virt --vm --quiet
for tool in ip jq curl python3 nft conntrack tcpdump timeout dpkg; do command -v "$tool" >/dev/null; done
ip link show "$management" >/dev/null
test "$(dpkg-query -W -f='${Status}' routerd)" = 'install ok installed'
test -z "$(dpkg -V routerd)"
test ! -e /etc/routerd/router.yaml
test ! -e /var/lib/routerd/routerd.db
if systemctl is-active --quiet routerd.service; then echo 'requires inactive clean installed product' >&2; exit 1; fi
kea=$kea_root/usr/sbin/kea-dhcp6
export LD_LIBRARY_PATH="$kea_root/usr/lib/x86_64-linux-gnu"
"$kea" -v
for ns in rd15-mclient rd15-pdserver rd15-aftr-{a,b,c,d}; do
 if ip netns list | awk '{print $1}' | grep -Fxq "$ns"; then echo "existing namespace: $ns" >&2; exit 1; fi
done
for dev in rd15-mwan rd15-mlan ds-{a,b,c,d}; do
 if ip link show "$dev" >/dev/null 2>&1; then echo "existing interface: $dev" >&2; exit 1; fi
done
work=$(mktemp -d /var/tmp/routerd-dslite-pd.XXXXXX)
peers=(); capture=; kea_pid=; started=0; withdrawn=0
cleanup() {
 result=$?
 trap - EXIT
 if [ "$result" != 0 ]; then
  journalctl -u routerd.service --no-pager -n 100 > "$work/routerd-journal.txt" || true
  nft list ruleset > "$work/ruleset.txt" 2>&1 || true
  ip -j -6 address show > "$work/addresses.json" || true
  ip -j -d link show > "$work/links.json" || true
  routerctl get status -o json > "$work/status.json" 2>&1 || true
 fi
 if [ -n "$capture" ]; then kill "$capture" 2>/dev/null || true; wait "$capture" 2>/dev/null || true; fi
 if [ "$started" = 1 ]; then
  if [ "$withdrawn" = 0 ]; then timeout 45 routerctl apply -f "$work/empty.json" --replace > "$work/withdraw.txt" 2>&1 || result=1; fi
  systemctl stop routerd.service || result=1
 fi
 if [ -n "$kea_pid" ]; then kill "$kea_pid" 2>/dev/null || true; wait "$kea_pid" 2>/dev/null || true; fi
 for pid in "${peers[@]}"; do kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; done
 for ns in rd15-mclient rd15-pdserver rd15-aftr-{a,b,c,d}; do
  if ip netns list | awk '{print $1}' | grep -Fxq "$ns"; then ip netns del "$ns" || result=1; fi
 done
 for dev in rd15-mlan rd15-mwan; do
  if ip link show "$dev" >/dev/null 2>&1; then ip link del "$dev" || result=1; fi
 done
 if [ "$result" = 0 ]; then rm -r -- "$work"; else echo "FAIL: bounded diagnostics retained at $work; end run before local reproduction" >&2; fi
 exit "$result"
}
trap cleanup EXIT
printf '%s\n' '{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"empty"},"spec":{"resources":[]}}' > "$work/empty.json"
jq --arg dev "$management" '.spec.resources |= map(if .kind=="Interface" and .metadata.name=="management" then .spec.ifname=$dev else . end)' "$fixture" > "$work/router.json"
/usr/sbin/routerd validate --config "$work/router.json"
cat > "$work/aftr-peer.py" <<'PY'
import http.server,json,sys
class Handler(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  body=json.dumps({'aftr':sys.argv[1],'source':self.client_address[0]}).encode()
  self.send_response(200);self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
 def log_message(self,*args):pass
http.server.HTTPServer(('203.0.113.80',8080),Handler).serve_forever()
PY
# Certification prepares only independent peers and guest-local test links.
modprobe ip6_tunnel
ip link add rd15-mwan type bridge
ip -6 address add 2001:db8:25::1/64 dev rd15-mwan nodad
ip link set rd15-mwan up
index=11; suffix=101
for letter in a b c d; do
 ns=rd15-aftr-$letter
 ip netns add "$ns"
 ip link add "rd15-ap-$letter" type veth peer name uplink netns "$ns"
 ip link set "rd15-ap-$letter" master rd15-mwan
 ip link set "rd15-ap-$letter" up
 ip -n "$ns" link set lo up
 ip -n "$ns" link set uplink up
 ip -n "$ns" -6 address add "2001:db8:25::$index/64" dev uplink nodad
 ip -n "$ns" -6 route add 2001:db8:1500::/60 via 2001:db8:25::1 dev uplink
 ip -n "$ns" -6 tunnel add aftr mode ipip6 local "2001:db8:25::$index" remote "2001:db8:1500:1::$suffix" dev uplink encaplimit none
 ip -n "$ns" -4 address add 192.0.0.1/32 peer 192.0.0.2/32 dev aftr
 ip -n "$ns" link set aftr mtu 1460 up
 ip -n "$ns" -4 address add 203.0.113.80/32 dev lo
 ip netns exec "$ns" python3 "$work/aftr-peer.py" "$letter" > "$work/peer-$letter.txt" 2>&1 & peers+=("$!")
 for _ in {1..30}; do
  if ip netns exec "$ns" curl --noproxy '*' -fsS --max-time 1 http://203.0.113.80:8080/ >/dev/null 2>&1; then break; fi
  sleep .1
 done
 ip netns exec "$ns" curl --noproxy '*' -fsS --max-time 2 http://203.0.113.80:8080/ | jq -e --arg letter "$letter" '.aftr==$letter' >/dev/null
 ping -6 -I rd15-mwan -c 1 -W 2 "2001:db8:25::$index" >/dev/null
 index=$((index+1)); suffix=$((suffix+1))
done
ip netns add rd15-pdserver
ip link add rd15-pdlink type veth peer name uplink netns rd15-pdserver
ip link set rd15-pdlink master rd15-mwan
ip link set rd15-pdlink up
ip -n rd15-pdserver link set lo up
ip -n rd15-pdserver link set uplink up
ip -n rd15-pdserver -6 address add 2001:db8:25::2/64 dev uplink nodad
ip netns add rd15-mclient
ip link add rd15-mlan type veth peer name client0 netns rd15-mclient
ip link set rd15-mlan up
ip -n rd15-mclient link set lo up
ip -n rd15-mclient link set client0 up
for index in {10..73}; do ip -n rd15-mclient address add "198.18.25.$index/24" dev client0; done
ip -n rd15-mclient route add default via 198.18.25.1
cat > "$work/kea.json" <<JSON
{"Dhcp6":{"interfaces-config":{"interfaces":["uplink"]},"lease-database":{"type":"memfile","persist":false,"name":"$work/leases.csv"},"data-directory":"$work","renew-timer":20,"rebind-timer":40,"preferred-lifetime":60,"valid-lifetime":90,"subnet6":[{"id":25,"subnet":"2001:db8:25::/64","interface":"uplink","pd-pools":[{"prefix":"2001:db8:1500::","prefix-len":60,"delegated-len":60}]}]}}
JSON
export KEA_PIDFILE_DIR="$work" KEA_LOCKFILE_DIR="$work"
start_kea() {
 ip netns exec rd15-pdserver "$kea" -t "$work/kea.json" > "$work/kea-config.txt" 2>&1
 ip netns exec rd15-pdserver "$kea" -d -c "$work/kea.json" >> "$work/kea.txt" 2>&1 & kea_pid=$!
 for _ in {1..50}; do
  if ip netns exec rd15-pdserver ss -unl | grep -q ':547 '; then break; fi
  kill -0 "$kea_pid"
  sleep .1
 done
 ip netns exec rd15-pdserver ss -unl | grep ':547 ' > "$work/listener.txt"
}
for _ in {1..30}; do
 if ! ip -6 address show dev rd15-mwan | grep -q tentative && ! ip -n rd15-pdserver -6 address show dev uplink | grep -q tentative; then break; fi
 sleep .1
done
ip -6 address show dev rd15-mwan > "$work/bridge-dad.txt"
ip -n rd15-pdserver -6 address show dev uplink > "$work/peer-dad.txt"
if grep -q tentative "$work/bridge-dad.txt" "$work/peer-dad.txt"; then echo 'fixture link-local DAD incomplete' >&2; exit 1; fi
start_kea
ping -6 -I rd15-mwan -c 1 -W 2 2001:db8:25::2 >/dev/null
echo 'CERTIFIED: independent Kea listener, four AFTR HTTP listeners and IPv6 underlay, separate client'
tcpdump -Z root -i rd15-mwan -U -C 1 -W 2 -w "$work/outer.pcap" 'ip6 proto 4 or udp port 546 or udp port 547' > "$work/capture.txt" 2>&1 & capture=$!
sleep .1
kill -0 "$capture"
install -d -m 0700 /etc/routerd
install -m 0600 "$work/router.json" /etc/routerd/router.yaml
started=1
systemctl start routerd.service
lease_file=/var/lib/routerd/dhcpv6-client/wan-pd/lease.json
pd_status() { curl --fail --silent --max-time 2 --unix-socket /run/routerd/dhcpv6-client/wan-pd.sock http://unix/v1/status; }
cat > "$work/check-sources.py" <<'PY'
import ipaddress,json,subprocess,sys
prefix=ipaddress.IPv6Network(sys.argv[1]+'::/60')
wan=json.loads(subprocess.check_output(['ip','-j','-6','address','show','dev','rd15-mwan']))[0]['addr_info']
lan=json.loads(subprocess.check_output(['ip','-j','-6','address','show','dev','rd15-mlan']))[0]['addr_info']
lease=json.load(open(sys.argv[2]));valid=lease['validSeconds']
for index,letter in enumerate('abcd',101):
 address=str(ipaddress.IPv6Address(int(prefix.network_address)+(1<<64)+int(str(index),16)))
 entries=[x for x in wan if x['local']==address]
 assert len(entries)==1 and entries[0]['prefixlen']==128,entries
 entry=entries[0]
 assert isinstance(entry['valid_life_time'],int) and 0<entry['valid_life_time']<=valid,entry
 assert not entry.get('tentative') and not entry.get('dadfailed') and 'tentative' not in entry.get('flags',[]),entry
 tunnel=json.loads(subprocess.check_output(['ip','-j','-d','link','show','ds-'+letter]))[0]
 assert tunnel['linkinfo']['info_data']['local']==address,tunnel
lanaddr=str(ipaddress.IPv6Address(int(prefix.network_address)+(1<<64)+1))
assert any(x['local']==lanaddr and x['prefixlen']==64 and isinstance(x['valid_life_time'],int) and 0<x['valid_life_time']<=valid for x in lan),lan
PY
await_ready() {
 local prefix=$1 deadline=$((SECONDS+120)) letter ready
 while (( SECONDS < deadline )); do
  ready=1
  if ! pd_status > "$work/pd.json" || ! jq -e --arg prefix "$prefix::/60" 'any(.resources[]; .resource.name=="wan-pd" and .phase=="Bound" and .observed.currentPrefix==$prefix)' "$work/pd.json" >/dev/null; then ready=0; fi
  if [ "$ready" = 1 ] && ! python3 "$work/check-sources.py" "$prefix" "$lease_file" 2> "$work/await-source.txt"; then ready=0; fi
  for letter in a b c d; do
   if ! curl --fail --silent --max-time 2 --unix-socket "/run/routerd/healthcheck/hc-$letter.sock" http://unix/v1/status | jq -e 'any(.resources[]; .phase=="Healthy")' >/dev/null; then ready=0; fi
  done
  if [ "$ready" = 1 ]; then return; fi
  sleep 1
 done
 echo "delegated sources did not converge for $prefix" >&2; return 1
}
sample() {
 local expected=$1 label=$2 index first second deadline=$((SECONDS+90))
 while (( SECONDS < deadline )); do
  routerctl get status -o json > "$work/runtime-status.json"
  if jq -e '.status.phase=="Healthy" and all(.status.controllers[]?; .currentError != true)' "$work/runtime-status.json" >/dev/null; then break; fi
  sleep 1
 done
 jq -e '.status.phase=="Healthy" and all(.status.controllers[]?; .currentError != true)' "$work/runtime-status.json" >/dev/null
 : > "$work/sample.jsonl"
 for index in {10..73}; do
  first=$(ip netns exec rd15-mclient curl --noproxy '*' --interface "198.18.25.$index" -fsS --max-time 3 http://203.0.113.80:8080/)
  second=$(ip netns exec rd15-mclient curl --noproxy '*' --interface "198.18.25.$index" -fsS --max-time 3 http://203.0.113.80:8080/)
  test "$first" = "$second"
  printf '%s\n' "$first" >> "$work/sample.jsonl"
 done
 python3 - "$work/sample.jsonl" "$expected" <<'PY'
import json,sys
rows=[json.loads(x) for x in open(sys.argv[1])]
assert len(rows)==64 and {x['aftr'] for x in rows}==set(sys.argv[2]),rows
assert all(x['source']=='192.0.0.2' for x in rows),rows
PY
 echo "PASS: $label; 64-source affinity, AFTR selection $expected and peer-observed NAT source"
}
# Fresh installation can legitimately queue a main-unit handoff. Complete
# that already-observed startup operation before asserting steady-state PID
# preservation during a later independent DHCP pool change.
await_startup_handoff() {
 local deadline=$((SECONDS+90)) pending
 while (( SECONDS < deadline )); do
  pending=$(systemctl list-units --all --plain --no-legend 'routerd-self-restart-*.timer' 'routerd-self-restart-*.service' | awk '$3=="active" || $3=="activating" || $3=="deactivating"{print $1}')
  if [ -z "$pending" ]; then return; fi
  sleep .2
 done
 echo 'initial main-unit handoff did not complete' >&2; return 1
}
await_ready 2001:db8:1500
await_startup_handoff
await_ready 2001:db8:1500
sample abc delegated-initial
first_reply=$(jq -r .leaseUpdatedAt "$lease_file")
deadline=$((SECONDS+60))
while (( SECONDS < deadline )); do
 if jq -e --arg first "$first_reply" '.state=="bound" and .leaseUpdatedAt>$first' "$lease_file" >/dev/null && grep -q RENEW "$work/kea.txt"; then break; fi
 sleep 1
done
jq -e --arg first "$first_reply" '.state=="bound" and .leaseUpdatedAt>$first' "$lease_file" >/dev/null
grep -q RENEW "$work/kea.txt"
await_ready 2001:db8:1500
sample abc delegated-renewed
main_pid=$(systemctl show routerd.service -p MainPID --value)
# Configure independent AFTR return endpoints for the network's replacement
# prefix before the independent DHCP server advertises it. No product state edit.
index=11; suffix=101
for letter in a b c d; do
 ns=rd15-aftr-$letter
 ip -n "$ns" -6 route add 2001:db8:1501::/60 via 2001:db8:25::1 dev uplink
 ip -n "$ns" -6 tunnel change aftr mode ipip6 local "2001:db8:25::$index" remote "2001:db8:1501:1::$suffix" dev uplink encaplimit none
 index=$((index+1)); suffix=$((suffix+1))
done
jq '.Dhcp6.subnet6[0]["pd-pools"][0].prefix="2001:db8:1501::"' "$work/kea.json" > "$work/kea-next.json"
ip netns exec rd15-pdserver "$kea" -t "$work/kea-next.json" > "$work/kea-next-check.txt" 2>&1
mv "$work/kea-next.json" "$work/kea.json"
kill -HUP "$kea_pid"
await_ready 2001:db8:1501
ip -j -6 address show > "$work/renumbered-addresses.json"
python3 - "$work/renumbered-addresses.json" <<'PY'
import json,sys
assert all(not a['local'].startswith('2001:db8:1500:') for link in json.load(open(sys.argv[1])) for a in link['addr_info'])
PY
if [ "$(systemctl show routerd.service -p MainPID --value)" != "$main_pid" ]; then
 echo "main process changed during steady-state renumber (expected $main_pid)" >&2; exit 1
fi
kill -0 "$kea_pid"
sample abc delegated-renumbered
systemctl restart routerd.service
test "$(systemctl show routerd.service -p MainPID --value)" != "$main_pid"
await_ready 2001:db8:1501
sample abc delegated-restarted
# Natural lease expiry: only the independent DHCP server is stopped.
kill "$kea_pid"; wait "$kea_pid"; kea_pid=
deadline=$((SECONDS+120))
while (( SECONDS < deadline )); do
 ip -j -6 address show > "$work/expired-addresses.json"
 if python3 - "$work/expired-addresses.json" 2> "$work/await-expiry.txt" <<'PY'
import json,sys
assert all(not a['local'].startswith(('2001:db8:1500:','2001:db8:1501:')) for link in json.load(open(sys.argv[1])) for a in link['addr_info'])
PY
 then
  remaining=0
  for letter in a b c d; do if ip link show "ds-$letter" >/dev/null 2>&1; then remaining=1; fi; done
  if [ "$remaining" = 0 ]; then break; fi
 fi
 sleep 1
done
for letter in a b c d; do
 if ip link show "ds-$letter" >/dev/null 2>&1; then echo "expired DS-Lite tunnel remains: $letter" >&2; exit 1; fi
done
python3 - "$work/expired-addresses.json" <<'PY'
import json,sys
assert all(not a['local'].startswith(('2001:db8:1500:','2001:db8:1501:')) for link in json.load(open(sys.argv[1])) for a in link['addr_info'])
PY
if ip netns exec rd15-mclient curl --noproxy '*' -fsS --max-time 3 http://203.0.113.80:8080/ > "$work/expired-traffic.txt" 2>&1; then echo 'traffic survived delegated-source expiry' >&2; exit 1; fi
systemctl is-active --quiet routerd.service
echo 'PASS: natural PD expiry withdrew all four /128 sources, LAN address, tunnels and client forwarding'
start_kea
await_ready 2001:db8:1501
sample abc delegated-reacquired
# Explicit declaration withdrawal after reacquisition must leave no stale
# dependencies, sources or controller errors in the canonical state.
routerctl apply -f "$work/empty.json" --replace > "$work/withdraw.txt"
withdrawn=1
for letter in a b c d; do
 if ip link show "ds-$letter" >/dev/null 2>&1; then echo "owned tunnel survived withdrawal: $letter" >&2; exit 1; fi
done
ip -j -6 address show > "$work/withdrawn-addresses.json"
python3 - "$work/withdrawn-addresses.json" <<'PY'
import json,sys
assert all(not a['local'].startswith(('2001:db8:1500:','2001:db8:1501:')) for link in json.load(open(sys.argv[1])) for a in link['addr_info'])
PY
deadline=$((SECONDS+60))
while (( SECONDS < deadline )); do
 routerctl get status -o json > "$work/empty-status.json"
 if jq -e '.status.phase=="Healthy" and (.status.resourceCount // 0)==0 and ((.status.resourcePhaseIssues // [])|length)==0 and all(.status.controllers[]?; .currentError != true)' "$work/empty-status.json" >/dev/null; then break; fi
 sleep 1
done
jq -e '.status.phase=="Healthy" and (.status.resourceCount // 0)==0 and ((.status.resourcePhaseIssues // [])|length)==0 and all(.status.controllers[]?; .currentError != true)' "$work/empty-status.json" >/dev/null
echo 'PASS: delegated-source acquisition, natural renewal, renumber, restart, expiry, peer-only recovery and declaration withdrawal'
