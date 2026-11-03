#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Clean apt installation on a disposable VM; four independent kernel AFTRs.
set -euo pipefail
if [ "${1:-}" != --disposable-vm ] || [ "$#" -ne 5 ]; then
 echo 'usage: --disposable-vm FIXTURE.json PRIOR.deb CURRENT.deb MANAGEMENT_INTERFACE' >&2
 exit 2
fi
shift
fixture=$(realpath "$1"); prior=$(realpath "$2"); current=$(realpath "$3"); management=$4
test "$(id -u)" = 0
systemd-detect-virt --vm --quiet
[[ "$management" =~ ^[a-zA-Z0-9_.-]{1,15}$ && "$management" != rd15-* && "$management" != lo ]]
for tool in ip jq curl python3 nft conntrack tcpdump dpkg-deb apt-get timeout; do command -v "$tool" >/dev/null; done
ip link show "$management" >/dev/null
test ! -e /usr/sbin/routerd
test ! -e /etc/routerd/router.yaml
test ! -e /var/lib/routerd/routerd.db
test "$(dpkg-deb -f "$prior" Package)" = routerd
test "$(dpkg-deb -f "$current" Package)" = routerd
dpkg --compare-versions "$(dpkg-deb -f "$current" Version)" gt "$(dpkg-deb -f "$prior" Version)"
for ns in rd15-mclient rd15-aftr-{a,b,c,d}; do
 ! ip netns list | awk '{print $1}' | grep -Fxq "$ns"
done
for dev in rd15-mwan rd15-mlan ds-{a,b,c,d}; do
 ! ip link show "$dev" >/dev/null 2>&1
done
work=$(mktemp -d /var/tmp/routerd-multi-dslite.XXXXXX)
peers=(); capture=; installed=0
cleanup() {
 result=$?
 trap - EXIT
 if [ "$result" != 0 ]; then
  journalctl -u routerd.service --no-pager -n 100 > "$work/routerd-journal.txt" || true
  nft list ruleset > "$work/ruleset.txt" 2>&1 || true
  ip -4 rule show > "$work/rules.txt" || true
  ip -4 route show table all > "$work/routes.txt" || true
  routerctl get status -o json > "$work/status.json" 2>&1 || true
 fi
 if [ -n "$capture" ]; then kill "$capture" 2>/dev/null || true; wait "$capture" 2>/dev/null || true; fi
 if [ "$installed" = 1 ]; then
  timeout 45 routerctl apply -f "$work/empty.json" --replace > "$work/withdraw.txt" 2>&1 || result=1
  systemctl stop routerd.service || result=1
 fi
 for pid in "${peers[@]}"; do kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; done
 for ns in rd15-mclient rd15-aftr-{a,b,c,d}; do
  if ip netns list | awk '{print $1}' | grep -Fxq "$ns"; then ip netns del "$ns" || result=1; fi
 done
 for dev in rd15-mlan rd15-mwan; do
  if ip link show "$dev" >/dev/null 2>&1; then ip link del "$dev" || result=1; fi
 done
 if [ "$result" = 0 ]; then
  rm -r -- "$work"
 else
  echo "FAIL: bounded diagnostics retained at $work; discard the VM after investigation" >&2
 fi
 exit "$result"
}
trap cleanup EXIT
printf '%s\n' '{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"empty"},"spec":{"resources":[]}}' > "$work/empty.json"
jq --arg dev "$management" '.spec.resources |= map(if .kind=="Interface" and .metadata.name=="management" then .spec.ifname=$dev else . end)' "$fixture" > "$work/router.json"
cat > "$work/peer.py" <<'PY'
import http.server,json,sys
class Handler(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  body=json.dumps({'aftr':sys.argv[1],'source':self.client_address[0]}).encode()
  self.send_response(200);self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
 def log_message(self,*args):pass
http.server.HTTPServer(('203.0.113.80',8080),Handler).serve_forever()
PY
# Environment preparation: no routerd is installed or running yet.
modprobe ip6_tunnel
ip link add rd15-mwan type bridge
ip -6 address add 2001:db8:25::1/64 dev rd15-mwan nodad
ip link set rd15-mwan up
index=11
for letter in a b c d; do
 ns=rd15-aftr-$letter
 ip netns add "$ns"
 ip link add "rd15-ap-$letter" type veth peer name uplink netns "$ns"
 ip link set "rd15-ap-$letter" master rd15-mwan
 ip link set "rd15-ap-$letter" up
 ip -n "$ns" link set lo up
 ip -n "$ns" link set uplink up
 ip -n "$ns" -6 address add "2001:db8:25::$index/64" dev uplink nodad
 ip -n "$ns" -6 tunnel add aftr mode ipip6 local "2001:db8:25::$index" remote 2001:db8:25::1 dev uplink encaplimit none
 ip -n "$ns" -4 address add 192.0.0.1/32 peer 192.0.0.2/32 dev aftr
 ip -n "$ns" link set aftr mtu 1460 up
 ip -n "$ns" -4 address add 203.0.113.80/32 dev lo
 ip netns exec "$ns" python3 "$work/peer.py" "$letter" > "$work/peer-$letter.txt" 2>&1 & peers+=("$!")
 for attempt in {1..30}; do
  if ip netns exec "$ns" curl --noproxy '*' -fsS --max-time 1 http://203.0.113.80:8080/ > /dev/null 2>&1; then break; fi
  sleep .1
 done
 ip netns exec "$ns" curl --noproxy '*' -fsS --max-time 2 http://203.0.113.80:8080/ | jq -e --arg letter "$letter" '.aftr==$letter' >/dev/null
 ping -6 -I rd15-mwan -c 1 -W 2 "2001:db8:25::$index" >/dev/null
 index=$((index+1))
done
ip netns add rd15-mclient
ip link add rd15-mlan type veth peer name client0 netns rd15-mclient
ip link set rd15-mlan up
ip -n rd15-mclient link set lo up
ip -n rd15-mclient link set client0 up
for index in {10..73}; do ip -n rd15-mclient address add "198.18.25.$index/24" dev client0; done
ip -n rd15-mclient route add default via 198.18.25.1
echo 'CERTIFIED: clean VM, four independent AFTR HTTP listeners and IPv6 links, separate client'
tcpdump -i rd15-mwan -U -C 1 -W 2 -w "$work/outer.pcap" 'ip6 proto 4' > "$work/capture.txt" 2>&1 & capture=$!
export DEBIAN_FRONTEND=noninteractive
apt-get -o DPkg::Lock::Timeout=180 install -y "$prior" > "$work/apt.txt" 2>&1
installed=1
! systemctl is-active --quiet routerd.service
test -z "$(dpkg -V routerd)"
/usr/sbin/routerd validate --config "$work/router.json"
install -d -m 0700 /etc/routerd
install -m 0600 "$work/router.json" /etc/routerd/router.yaml
systemctl start routerd.service
await_health() {
 local letters=$1 phase=$2 deadline=$((SECONDS+90)) okay letter
 while (( SECONDS < deadline )); do
  okay=1
  for letter in $letters; do
   if ! curl --fail --silent --max-time 2 --unix-socket "/run/routerd/healthcheck/hc-$letter.sock" http://unix/v1/status | jq -e --arg phase "$phase" --arg name "hc-$letter" 'any(.resources[]?; .resource.name==$name and .phase==$phase)' >/dev/null; then okay=0; fi
  done
  if [ "$okay" = 1 ]; then return; fi
  sleep 1
 done
 echo "health did not reach $phase for $letters" >&2
 return 1
}
sample() {
 local expected=$1 label=$2 index first second
 # Health publication precedes the route owner's asynchronous reconciliation.
 # Observe the native marking rules before asserting the resulting traffic.
 local deadline=$((SECONDS+90)) ready=0
 while (( SECONDS < deadline )); do
  nft -nn list table ip routerd_policy > "$work/policy-native.txt" 2>/dev/null || : > "$work/policy-native.txt"
  nft -nn list table ip routerd_default_route > "$work/default-native.txt" 2>/dev/null || : > "$work/default-native.txt"
  if python3 - "$work/policy-native.txt" "$work/default-native.txt" "$expected" <<'PY'
import re,sys
policy=open(sys.argv[1]).read(); default=open(sys.argv[2]).read(); expected=sys.argv[3]
marks={int(x,16) for line in policy.splitlines() if 'jhash' in line for x in re.findall(r':\s*(0x[0-9a-f]+)',line)}
if expected=='d':
 assert not marks and any(int(x,16)==275 for x in re.findall(r'meta mark set (0x[0-9a-f]+)',default))
else:
 assert marks=={272+'abcd'.index(letter) for letter in expected}
PY
  then ready=1; break; fi
  sleep 1
 done
 test "$ready" = 1
 # A working tunnel must not conceal an unrelated bootstrap failure, such as
 # the Package owner failing under the main unit's capability restrictions.
 deadline=$((SECONDS+90)); ready=0
 while (( SECONDS < deadline )); do
  if routerctl get status -o json > "$work/runtime-status.json" &&
     jq -e 'all(.status.controllers[]?; .currentError != true)' "$work/runtime-status.json" >/dev/null; then
   case "$label" in
    initial|recovered|restarted|apt-updated)
     if jq -e '.status.phase=="Healthy"' "$work/runtime-status.json" >/dev/null; then ready=1; break; fi;;
    *) ready=1; break;;
   esac
  fi
  sleep 1
 done
 test "$ready" = 1
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
assert len(rows)==64
assert {x['aftr'] for x in rows}==set(sys.argv[2]),rows
assert all(x['source']=='192.0.0.2' for x in rows),rows
PY
 echo "PASS: $label; source affinity, AFTR selection $expected, translated source observed by peers"
}
await_health 'a b c d' Healthy
sample abc initial
# Fail independent peers; never change routerd state to manufacture failover.
ip -n rd15-aftr-b link set aftr down
await_health b Unhealthy
sample ac one-primary-down
ip -n rd15-aftr-a link set aftr down
ip -n rd15-aftr-c link set aftr down
await_health 'a b c' Unhealthy
sample d all-primary-down
for letter in a b c; do ip -n "rd15-aftr-$letter" link set aftr up; done
await_health 'a b c d' Healthy
sample abc recovered
old=$(systemctl show routerd.service -p MainPID --value)
systemctl restart routerd.service
test "$(systemctl show routerd.service -p MainPID --value)" != "$old"
await_health 'a b c d' Healthy
sample abc restarted
before=$(sha256sum /etc/routerd/router.yaml)
old=$(systemctl show routerd.service -p MainPID --value)
apt-get -o DPkg::Lock::Timeout=180 install -y "$current" >> "$work/apt.txt" 2>&1
test "$(sha256sum /etc/routerd/router.yaml)" = "$before"
test "$(dpkg-query -W -f='${Version}' routerd)" = "$(dpkg-deb -f "$current" Version)"
test -z "$(dpkg -V routerd)"
test "$(systemctl show routerd.service -p MainPID --value)" != "$old"
! kill -0 "$old" 2>/dev/null
await_health 'a b c d' Healthy
sample abc apt-updated
routerctl apply -f "$work/empty.json" --replace > "$work/withdraw.txt"
for letter in a b c d; do ! ip link show "ds-$letter" >/dev/null 2>&1; done
if ip netns exec rd15-mclient curl --noproxy '*' -fsS --max-time 3 http://203.0.113.80:8080/ >/dev/null 2>&1; then
 echo 'withdrawal left the routed path usable' >&2; exit 1
fi
echo 'PASS: clean apt install/update, four DS-Lite paths, source-hash selection, peer failure/recovery, restart and withdrawal'
