#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Dedicated empty VM only; installed make dist product and independent Kea peer.
set -euo pipefail
if [ "${1:-}" != --disposable-vm ] || [ "$#" -ne 3 ]; then
 echo "usage: $0 --disposable-vm CANDIDATE_JSON KEA_PACKAGE_ROOT" >&2
 exit 2
fi
shift
candidate=$1
fixture=$2
verifier=$(dirname "$(readlink -f "$0")")/verify-dhcpv4-rebind.py
test "$(id -u)" = 0
systemd-detect-virt --vm --quiet
for tool in ip curl jq python3 dig tcpdump resolvectl dnsmasq; do command -v "$tool" >/dev/null; done
systemctl is-active --quiet systemd-resolved
test "$(readlink -f /etc/resolv.conf)" = /run/systemd/resolve/stub-resolv.conf
test -f "$verifier"
export LD_LIBRARY_PATH="$fixture/usr/lib/x86_64-linux-gnu"
kea=$fixture/usr/sbin/kea-dhcp4
"$kea" -v
/usr/local/sbin/routerd validate --config "$candidate"
/usr/local/sbin/routerctl get status -o json | jq -e '.status.phase == "Healthy" and (.status.resourceCount // 0) == 0' >/dev/null
lan_ns=rd15-lan4-peer
wan_ns=rd15-wan4-peer
for ns in "$lan_ns" "$wan_ns"; do
 if ip netns list | awk '{print $1}' | grep -Fxq "$ns"; then echo "existing namespace: $ns" >&2; exit 1; fi
done
for dev in rd15-lan rd15-wan; do
 if ip link show "$dev" >/dev/null 2>&1; then echo "existing interface: $dev" >&2; exit 1; fi
done
for unit in rd15-peer-kea4 rd15-peer-http4 rd15-peer-dns4 rd15-wan4-capture; do
 test "$(systemctl show -p LoadState --value "$unit.service")" = not-found
done
test ! -e /run/kea
work=$(mktemp -d /var/tmp/routerd15-wan-v4.XXXXXX)
printf '%s\n' "$work"
sysctl -n net.ipv4.ip_forward > "$work/forward.before"
stage=environment
finish() {
 rc=$?
 trap - EXIT
 if [ "$rc" -ne 0 ]; then
  timeout 5 /usr/local/sbin/routerctl get status -o json > "$work/failure-status.json" 2>&1 || true
  ip -4 -j addr > "$work/failure-addresses.json" 2>&1 || true
  ip -4 -j route > "$work/failure-routes.json" 2>&1 || true
  resolvectl status rd15-wan > "$work/failure-dns.txt" 2>&1 || true
  cp /var/lib/routerd/dhcpv4-client/wan/lease.json "$work/failure-lease.json" 2>/dev/null || true
 fi
 systemctl stop rd15-peer-kea4 rd15-peer-http4 rd15-peer-dns4 rd15-wan4-capture >/dev/null 2>&1 || true
 if [ "$rc" -ne 0 ]; then
  systemctl stop routerd
  printf 'FAIL at %s: preserved state/output for local reproduction: %s\n' "$stage" "$work" >&2
 else
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
ip netns exec "$lan_ns" ip addr add 198.18.45.2/24 dev peer0
ip netns exec "$lan_ns" ip route add default via 198.18.45.1
ip netns exec "$wan_ns" ip addr add 198.18.46.1/24 dev peer0
ip netns exec "$wan_ns" ip addr add 198.18.47.1/32 dev lo
cat > "$work/kea.json" <<EOF
{"Dhcp4":{
 "interfaces-config":{"interfaces":["peer0"]},
 "lease-database":{"type":"memfile","persist":true,"lfc-interval":0,"name":"$work/leases.csv"},
 "renew-timer":20,"rebind-timer":40,"valid-lifetime":90,
 "subnet4":[{"id":45,"subnet":"198.18.46.0/24","interface":"peer0","pools":[{"pool":"198.18.46.100-198.18.46.100"}],
 "option-data":[{"name":"routers","data":"198.18.46.1"},{"name":"domain-name-servers","data":"198.18.46.1"}]}]
}}
EOF
ip netns exec "$wan_ns" "$kea" -t "$work/kea.json"
cat > "$work/peer_http.py" <<'PY'
from http.server import BaseHTTPRequestHandler, HTTPServer
class Handler(BaseHTTPRequestHandler):
 def do_GET(self):
  data=(self.client_address[0]+'\n').encode()
  self.send_response(200); self.send_header('Content-Length',str(len(data))); self.end_headers(); self.wfile.write(data)
HTTPServer(('198.18.47.1',8080),Handler).serve_forever()
PY
start_kea() {
 systemd-run --unit=rd15-peer-kea4 --property=Type=exec --property=RuntimeDirectory=kea --setenv="LD_LIBRARY_PATH=$LD_LIBRARY_PATH" ip netns exec "$wan_ns" "$kea" -d -c "$work/kea.json"
}
start_kea
systemd-run --unit=rd15-peer-http4 --property=Type=exec ip netns exec "$wan_ns" python3 "$work/peer_http.py"
systemd-run --unit=rd15-peer-dns4 --property=Type=exec ip netns exec "$wan_ns" dnsmasq --keep-in-foreground --conf-file=/dev/null --user=root --no-resolv --bind-interfaces --interface=peer0 --listen-address=198.18.46.1 --address=/peer.test/198.18.47.1
for _ in {1..15}; do
 if ip netns exec "$wan_ns" ss -unl | grep -q ':67 ' && ip netns exec "$wan_ns" curl --noproxy '*' --silent --fail --max-time 1 http://198.18.47.1:8080/ >/dev/null; then break; fi
 sleep 1
done
ip netns exec "$wan_ns" ss -unl | grep ':67 '
ip netns exec "$wan_ns" curl --noproxy '*' --fail --max-time 2 http://198.18.47.1:8080/
test "$(ip netns exec "$wan_ns" dig +short +time=2 +tries=1 @198.18.46.1 peer.test A)" = 198.18.47.1
systemd-run --unit=rd15-wan4-capture --property=Type=exec tcpdump -Z root -U -s 0 -i rd15-wan -w "$work/wan.pcap" 'udp port 67 or udp port 68'
for _ in {1..10}; do test -s "$work/wan.pcap" && break; sleep 1; done
test -s "$work/wan.pcap"
printf 'Environment certified: empty product, independent Kea/DNS/HTTP peers and capture ready\n'
stage=acquisition
lease=/var/lib/routerd/dhcpv4-client/wan/lease.json
/usr/local/sbin/routerctl apply -f "$candidate" --replace > "$work/apply.txt"
bound() { jq -e '.state == "Bound" and .currentAddress == "198.18.46.100"' "$lease" >/dev/null 2>&1; }
for _ in {1..45}; do bound && break; sleep 1; done
bound
native_ready() {
 ip -4 -j addr show rd15-wan | jq -e 'any(.[0].addr_info[]?; .local == "198.18.46.100" and .prefixlen == 24)' >/dev/null &&
 ip -4 -j route show default | jq -e 'any(.[]; .gateway == "198.18.46.1" and .dev == "rd15-wan" and .metric == 4242)' >/dev/null &&
 resolvectl dns rd15-wan | grep -q '198.18.46.1' &&
 resolvectl domain rd15-wan | grep -q '~\.'
}
probe() {
 local source answer mode
 source=$(ip netns exec "$lan_ns" curl --noproxy '*' --fail --silent --show-error --max-time 3 http://198.18.47.1:8080/) || return
 [ "$source" = 198.18.46.100 ] || return 1
 for mode in +notcp +tcp; do
  answer=$(ip netns exec "$lan_ns" dig +short +time=2 +tries=1 "$mode" @198.18.45.1 peer.test A) || return
  [ "$answer" = 198.18.47.1 ] || return 1
 done
 printf 'PASS: LAN HTTP peer observes leased NAT source; learned upstream DNS answers UDP/TCP\n'
}
for _ in {1..30}; do if native_ready && probe; then break; fi; sleep 1; done
native_ready
probe
printf 'PASS: leased address, metric-qualified default route and systemd-resolved DNS/domain\n'
router_pid=$(systemctl show -p MainPID --value routerd)
stage=renew
first=$(jq -r .acquiredAt "$lease")
for _ in {1..35}; do
 if jq -e --arg first "$first" '.state == "Bound" and .acquiredAt > $first' "$lease" >/dev/null; then break; fi
 sleep 1
done
jq -e --arg first "$first" '.state == "Bound" and .acquiredAt > $first' "$lease" >/dev/null
probe
printf 'PASS: T1 renewal advances acknowledged lease epoch\n'
stage=rebind
before_rebind=$(jq -r .acquiredAt "$lease")
systemctl stop rd15-peer-kea4
for _ in {1..35}; do
 if jq -e '.state == "Renewing"' "$lease" >/dev/null; then break; fi
 sleep 1
done
jq -e '.state == "Renewing"' "$lease" >/dev/null
# T1 was unanswered. Restore the server before T2; the client's next exchange
# must be broadcast Rebind (renew retries have a minimum 60-second interval).
start_kea
for _ in {1..35}; do
 if jq -e --arg first "$before_rebind" '.state == "Bound" and .acquiredAt > $first' "$lease" >/dev/null; then break; fi
 sleep 1
done
jq -e --arg first "$before_rebind" '.state == "Bound" and .acquiredAt > $first' "$lease" >/dev/null
# libpcap can still hold the final exchange in its kernel buffer when the
# daemon has already acknowledged it. Observe the saved exchange before stop.
for _ in {1..10}; do
 if python3 "$verifier" "$work/wan.pcap" 198.18.46.100 198.18.46.1 > "$work/capture-verdict.txt" 2> "$work/capture-error.txt"; then break; fi
 sleep 1
done
systemctl stop rd15-wan4-capture
python3 "$verifier" "$work/wan.pcap" 198.18.46.100 198.18.46.1
probe
stage=expiry
systemctl stop rd15-peer-kea4
withdrawn() {
 ! ip -4 addr show rd15-wan | grep -q '198.18.46.100/' &&
 ! ip -4 route show default | grep -q 'via 198.18.46.1 dev rd15-wan' &&
 ! resolvectl dns rd15-wan | grep -q '198.18.46.1' &&
 ! resolvectl domain rd15-wan | grep -q '~\.'
}
for _ in {1..110}; do if withdrawn; then break; fi; sleep 1; done
withdrawn
if ip netns exec "$lan_ns" curl --noproxy '*' --fail --silent --max-time 2 http://198.18.47.1:8080/; then echo 'expired traffic still forwards' >&2; exit 1; fi
systemctl is-active --quiet routerd
printf 'PASS: real lease expiry withdraws address, default route, learned DNS/domain and forwarding\n'
stage=recovery
start_kea
for _ in {1..90}; do if bound && native_ready; then break; fi; sleep 1; done
bound
native_ready
for _ in {1..30}; do if probe; then break; fi; sleep 1; done
probe
test "$(systemctl show -p MainPID --value routerd)" = "$router_pid"
printf 'PASS: server return restores address/route/DNS/NAT without product restart\n'
stage=removal
cat > "$work/empty.json" <<'JSON'
{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"wan-readiness"},"spec":{"resources":[]}}
JSON
/usr/local/sbin/routerctl apply -f "$work/empty.json" --replace > "$work/withdraw.txt"
for _ in {1..45}; do
 /usr/local/sbin/routerctl get status -o json > "$work/empty-status.json"
 if withdrawn && jq -e '.status.phase == "Healthy" and (.status.resourceCount // 0) == 0 and ((.status.resourcePhaseIssues // []) | length) == 0 and all(.status.controllers[]?; .currentError == false)' "$work/empty-status.json" >/dev/null; then break; fi
 sleep 1
done
withdrawn
jq -e '.status.phase == "Healthy" and (.status.resourceCount // 0) == 0 and ((.status.resourcePhaseIssues // []) | length) == 0 and all(.status.controllers[]?; .currentError == false)' "$work/empty-status.json" >/dev/null
systemctl stop rd15-peer-kea4 rd15-peer-http4 rd15-peer-dns4
ip netns del "$lan_ns"
ip netns del "$wan_ns"
sysctl -w "net.ipv4.ip_forward=$(cat "$work/forward.before")"
printf 'PASS: WAN DHCPv4 acquire/renew/rebind/expiry/recovery and empty withdrawal\n'
