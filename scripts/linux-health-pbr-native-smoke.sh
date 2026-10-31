#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Explicit, private netns gate using an unmodified make-dist package.
set -euo pipefail
if [ "${1:-}" = --isolated-netns ] && [ "$#" = 4 ]; then
 test "$(id -u)" = 0
 export ROUTERD_HOST_NETNS
 ROUTERD_HOST_NETNS=$(readlink /proc/self/ns/net)
 exec unshare -nm bash "$0" --inside "$2" "$3" "$4"
fi
if [ "${1:-}" != --inside ] || [ "$#" != 4 ] || [ -z "${ROUTERD_HOST_NETNS:-}" ]; then
 echo "usage: sudo $0 --isolated-netns MAKE_DIST_ARCHIVE SHA256 WORK_PARENT" >&2
 exit 64
fi
test "$(readlink /proc/self/ns/net)" != "$ROUTERD_HOST_NETNS"
archive=$(readlink -f "$2")
test "$(sha256sum "$archive" | cut -d ' ' -f1)" = "$3"
parent=$(readlink -f "$4")
test -d "$parent"
for tool in ip nft nsenter jq curl python3 tar; do command -v "$tool" >/dev/null; done
mount --make-rprivate /
mount -t sysfs sysfs /sys
work=$(mktemp -d "$parent/health-pbr.XXXXXX")
stage=certification
peers=() servers=() workers=() router=''
finish() {
 rc=$?
 trap - EXIT
 if [ "$rc" != 0 ]; then
  ip -4 route show table all > "$work/failure-routes.txt" 2>&1 || true
  ip -4 rule show > "$work/failure-rules.txt" 2>&1 || true
  nft list ruleset > "$work/failure-nft.txt" 2>&1 || true
 fi
 for pid in "$router" "${workers[@]}" "${servers[@]}" "${peers[@]}"; do
  if [ -n "$pid" ]; then kill -CONT "$pid" 2>/dev/null || true; kill "$pid" 2>/dev/null || true; fi
 done
 for pid in "$router" "${workers[@]}" "${servers[@]}" "${peers[@]}"; do
  if [ -n "$pid" ]; then wait "$pid" 2>/dev/null || true; fi
 done
 if [ "$rc" = 0 ]; then rm -r -- "$work"; else echo "FAIL stage=$stage; diagnostics=$work" >&2; fi
 exit "$rc"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
ip link set lo up
for i in 0 1 2; do
 unshare -n sleep 600 & peers[i]=$!
 for _ in {1..30}; do
  if [ "$(readlink /proc/"${peers[$i]}"/ns/net)" != "$(readlink /proc/self/ns/net)" ]; then break; fi
  sleep .1
 done
 test "$(readlink /proc/"${peers[$i]}"/ns/net)" != "$(readlink /proc/self/ns/net)"
done
peer_run() { local index=$1; shift; nsenter -t "${peers[$index]}" -n -- "$@"; }
devices=(lan0 wan-a wan-b)
for i in 0 1 2; do
 subnet=$((10+i))
 ip link add "${devices[$i]}" type veth peer name peer0 netns "${peers[$i]}"
 ip link set "${devices[$i]}" up
 ip addr add "198.18.$subnet.1/24" dev "${devices[$i]}"
 peer_run "$i" ip link set lo up
 peer_run "$i" ip link set peer0 up
 peer_run "$i" ip addr add "198.18.$subnet.2/24" dev peer0
 peer_run "$i" ip route add default via "198.18.$subnet.1"
done
echo 1 > /proc/sys/net/ipv4/ip_forward
for path in /proc/sys/net/ipv4/conf/*/rp_filter; do echo 0 > "$path"; done
cat > "$work/server.py" <<'PY'
from http.server import BaseHTTPRequestHandler, HTTPServer
import sys
class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body=sys.argv[1].encode()
        self.send_response(200)
        self.send_header('Content-Length',str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self,*args): pass
HTTPServer(('198.18.100.1',8080),Handler).serve_forever()
PY
for i in 1 2; do
 peer_run "$i" ip addr add 198.18.100.1/32 dev lo
 nsenter -t "${peers[$i]}" -n -- python3 "$work/server.py" "$i" > "$work/server-$i.log" 2>&1 & servers[i]=$!
 for _ in {1..30}; do
  if peer_run "$i" curl --noproxy '*' -fsS --max-time 1 http://198.18.100.1:8080 > /dev/null 2>&1; then break; fi
  kill -0 "${servers[$i]}"; sleep .1
 done
 ip route add 198.18.100.1/32 via "198.18.$((10+i)).2" dev "${devices[$i]}"
 test "$(peer_run 0 curl --noproxy '*' -fsS --max-time 2 http://198.18.100.1:8080)" = "$i"
 ip route del 198.18.100.1/32 via "198.18.$((10+i)).2" dev "${devices[$i]}"
done
echo 'CERTIFIED: independent client and two upstreams, bidirectional routed HTTP'
for path in /run/routerd /var/lib/routerd; do test -d "$path"; mount -t tmpfs tmpfs "$path"; done
tar -xzf "$archive" -C "$work" ./bin
export PATH="$work/bin:$PATH"
routerd version
cat > "$work/empty.yaml" <<'YAML'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: health-pbr-native}
spec: {resources: []}
YAML
cat > "$work/candidate.yaml" <<'YAML'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: health-pbr-native}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: {name: wan-a}
      spec: {ifname: wan-a}
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: {name: wan-b}
      spec: {ifname: wan-b}
    - apiVersion: net.routerd.net/v1alpha1
      kind: HealthCheck
      metadata: {name: internet-a}
      spec: {target: 198.18.100.1, protocol: tcp, addressFamily: ipv4, port: 8080, interval: 1s, timeout: 300ms, healthyThreshold: 1, unhealthyThreshold: 2}
    - apiVersion: net.routerd.net/v1alpha1
      kind: HealthCheck
      metadata: {name: internet-b}
      spec: {target: 198.18.100.1, protocol: tcp, addressFamily: ipv4, port: 8080, interval: 1s, timeout: 300ms, healthyThreshold: 1, unhealthyThreshold: 2}
    - apiVersion: net.routerd.net/v1alpha1
      kind: EgressRoutePolicy
      metadata: {name: internet}
      spec:
        family: ipv4
        mode: priority
        sourceCIDRs: [198.18.10.0/24]
        destinationCIDRs: [198.18.100.1/32]
        selection: highest-weight-ready
        candidates:
          - {name: a, interface: wan-a, gatewaySource: static, gateway: 198.18.11.2, table: 110, priority: 10110, mark: 272, weight: 100, healthCheck: internet-a}
          - {name: b, interface: wan-b, gatewaySource: static, gateway: 198.18.12.2, table: 120, priority: 10120, mark: 288, weight: 50, healthCheck: internet-b}
YAML
routerd validate --config "$work/candidate.yaml"
cp "$work/empty.yaml" "$work/router.yaml"
start_router() {
 routerd serve --config "$work/router.yaml" --controllers daemon-supervisor,daemon-status,ipv4-policy-route \
  --state-file "$work/state.db" --ledger-file "$work/ledger.db" --status-file "$work/status.json" \
  --socket "$work/control.sock" --status-socket "$work/status.sock" --graceful-stop-timeout 0 >> "$work/router.log" 2>&1 & router=$!
 for _ in {1..50}; do if [ -S "$work/control.sock" ]; then return; fi; kill -0 "$router"; sleep .2; done
 return 1
}
apply_config() {
 routerctl plan -f "$1" --replace --socket "$work/control.sock" > "$work/plan.json"
 routerctl apply -f "$1" --replace --socket "$work/control.sock" > "$work/apply.json"
}
start_worker() {
 local i=$1 name=$2 mark=$3
 routerd-healthcheck daemon --resource "internet-$name" --target 198.18.100.1 --protocol tcp \
  --address-family ipv4 --port 8080 --source-interface "wan-$name" --fwmark "$mark" \
  --interval 1s --timeout 300ms --healthy-threshold 1 --unhealthy-threshold 2 \
  --socket "/run/routerd/healthcheck/internet-$name.sock" \
  --state-file "$work/health-$name.json" --event-file "$work/events-$name.jsonl" >> "$work/health-$name.log" 2>&1 & workers[i]=$!
}
state() {
 python3 - "$work/state.db" <<'PY'
import sqlite3,sys
with sqlite3.connect(sys.argv[1]) as c:
    row=c.execute("select status from objects where kind='EgressRoutePolicy' and name='internet'").fetchone()
    print(row[0] if row else '{}')
PY
}
await_path() {
 local name=$1 body=$2 limit=${3:-30}
 for ((attempt=0; attempt<limit; attempt++)); do
  state > "$work/observed.json"
  if jq -e --arg name "$name" '.phase=="Applied" and .selectedCandidate==$name' "$work/observed.json" >/dev/null; then
   if [ "$(peer_run 0 curl --noproxy '*' -fsS --max-time 1 http://198.18.100.1:8080 2>/dev/null || true)" = "$body" ]; then return; fi
  fi
  kill -0 "$router"; sleep 1
 done
 return 1
}
stage=initial-selection
start_router
apply_config "$work/candidate.yaml"
# The fixture owns helper startup. This is not a systemd activation test.
start_worker 1 a 272
start_worker 2 b 288
await_path a 1
echo 'PASS: actual Health worker -> IPC -> policy controller -> client HTTP through WAN A'
stage=upstream-loss
kill -STOP "${servers[1]}"
await_path b 2
echo 'PASS: WAN A outage selects WAN B and forwards independent client traffic'
stage=upstream-recovery
kill -CONT "${servers[1]}"
await_path a 1
echo 'PASS: upstream-only recovery restores preferred WAN without manual reconciliation'
stage=worker-expiry
kill "${workers[1]}"
wait "${workers[1]}"
workers[1]=''
# Health's minimum freshness contract is two minutes, not probe interval * 3.
await_path b 2 145
echo 'PASS: unavailable Health worker expires its old success and selects WAN B'
stage=worker-recovery
start_worker 1 a 272
await_path a 1
stage='router-restart'
kill "$router"; wait "$router"; router=''
start_router
await_path a 1
echo 'PASS: Health recovery and router SQLite restart retain real forwarding'
stage=withdrawal
apply_config "$work/empty.yaml"
for _ in {1..30}; do
 if ! ip -4 rule show | grep -Eq '10110:|10120:' && ! nft list table ip routerd_default_route >/dev/null 2>&1; then break; fi
 sleep 1
done
if ip -4 rule show | grep -Eq '10110:|10120:'; then exit 1; fi
if ip -4 route show table all | grep -Eq 'table (110|120) '; then exit 1; fi
if nft list table ip routerd_default_route >/dev/null 2>&1; then exit 1; fi
if peer_run 0 curl --noproxy '*' -fsS --max-time 2 http://198.18.100.1:8080 >/dev/null 2>&1; then exit 1; fi
echo 'PASS: configuration withdrawal removes owned routing and stops client forwarding'
kill "$router"; wait "$router"; router=''
for i in 1 2; do kill "${workers[$i]}"; wait "${workers[$i]}"; workers[i]=''; done
