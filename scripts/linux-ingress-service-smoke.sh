#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Actual packaged routerd ingress lifecycle in a dedicated disposable Linux VM.
set -euo pipefail

if [ "${1:-}" != --disposable-vm ] || [ "$#" -ne 1 ]; then
 echo "usage: $0 --disposable-vm" >&2
 exit 2
fi
test "$(id -u)" = 0
systemd-detect-virt --vm --quiet
for x in ip nft jq python3 systemctl; do command -v "$x" >/dev/null; done
if [ "${ROUTERD_INGRESS_DEBUG:-}" = 1 ]; then command -v tcpdump >/dev/null; fi
management=rd15-ing-mgmt
for dev in rd15-ing-lan rd15-ing-wan "$management"; do
 if ip link show "$dev" >/dev/null 2>&1; then echo "existing interface: $dev" >&2; exit 1; fi
done
for ns in rd15-ing-external rd15-ing-client rd15-ing-backend-a rd15-ing-backend-b rd15-ing-management; do
 if ip netns list | awk '{print $1}' | grep -Fxq "$ns"; then echo "existing namespace: $ns" >&2; exit 1; fi
done
if nft -j list tables | jq -e '.nftables[].table? | select(.family=="inet" and .name=="routerd_filter")' >/dev/null; then
 echo 'existing product firewall; empty dedicated guest required' >&2
 exit 1
fi

work=$(mktemp -d /var/tmp/routerd15-ingress.XXXXXX)
stage='setup'
forward_before=$(sysctl -n net.ipv4.ip_forward)
capture_pid=''
port_forward_pid=''
backend_a_pid=''
backend_b_pid=''
empty="$work/empty.json"
cat > "$empty" <<'JSON'
{"apiVersion":"routerd.net/v1alpha1","kind":"Router","metadata":{"name":"ingress-smoke"},"spec":{"resources":[]}}
JSON

finish() {
 rc=$?
 trap - EXIT
 if [ -n "$capture_pid" ]; then kill "$capture_pid" >/dev/null 2>&1 || true; wait "$capture_pid" >/dev/null 2>&1 || true; capture_pid=''; fi
 if [ "$rc" -ne 0 ]; then
  echo "FAIL at $stage; live state follows before cleanup" >&2
  for event_log in "$work"/*.events; do
   test -e "$event_log" || continue
   printf '%s\n' "backend observation: $(basename "$event_log")" >&2
  cat "$event_log" >&2
  done
  if test -f "$work/ingress-packets.txt"; then
   echo 'ingress packet observation:' >&2
   cat "$work/ingress-packets.txt" >&2
  fi
  nft list ruleset >&2 || true
 fi
 /usr/local/sbin/routerctl apply -f "$empty" --replace >/dev/null 2>&1 || true
 for pid in "$port_forward_pid" "$backend_a_pid" "$backend_b_pid"; do
  test -z "$pid" || kill "$pid" >/dev/null 2>&1 || true
 done
 for pid in "$port_forward_pid" "$backend_a_pid" "$backend_b_pid"; do
  test -z "$pid" || wait "$pid" >/dev/null 2>&1 || true
 done
 for ns in rd15-ing-external rd15-ing-client rd15-ing-backend-a rd15-ing-backend-b rd15-ing-management; do
  ip netns del "$ns" >/dev/null 2>&1 || true
 done
 ip link del rd15-ing-wan >/dev/null 2>&1 || true
 ip link del rd15-ing-lan >/dev/null 2>&1 || true
 sysctl -w "net.ipv4.ip_forward=$forward_before" >/dev/null 2>&1 || true
 rm -rf -- "$work"
 if [ "$rc" -ne 0 ]; then echo 'temporary fixture and output removed; repair locally before another VM run' >&2; fi
 exit "$rc"
}
trap finish EXIT

cat > "$work/backend.py" <<'PY'
import socket, sys
label = sys.argv[1]
events = sys.argv[4]
listener = socket.socket()
listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
listener.bind((sys.argv[2], int(sys.argv[3])))
listener.listen()
while True:
    conn, peer = listener.accept()
    with conn:
        with open(events, 'a') as output:
            output.write(f"accepted {peer[0]}\n")
        try:
            conn.sendall((f"{label}:{peer[0]}\n").encode())
            while data := conn.recv(1024):
                conn.sendall((f"{label}:{data.decode().strip()}\n").encode())
        except ConnectionResetError:
            pass
PY

for ns in rd15-ing-external rd15-ing-client rd15-ing-backend-a rd15-ing-backend-b rd15-ing-management; do
 ip netns add "$ns"
 ip -n "$ns" link set lo up
done
ip link add rd15-ing-lan type bridge
ip link set rd15-ing-lan up
for endpoint in client backend-a backend-b; do
 ns="rd15-ing-$endpoint"
 case "$endpoint" in
  client) host=rd15-ing-c ;;
  backend-a) host=rd15-ing-a ;;
  backend-b) host=rd15-ing-b ;;
 esac
 ip link add "$host" type veth peer name peer0 netns "$ns"
 ip link set "$host" master rd15-ing-lan
 ip link set "$host" up
 ip -n "$ns" link set peer0 up
done
ip link add rd15-ing-wan type veth peer name peer0 netns rd15-ing-external
ip link set rd15-ing-wan up
ip -n rd15-ing-external link set peer0 up
ip -n rd15-ing-external addr add 198.18.52.1/24 dev peer0
ip -n rd15-ing-external route add default via 198.18.52.2
ip link add "$management" type veth peer name peer0 netns rd15-ing-management
ip link set "$management" up
ip -n rd15-ing-management link set peer0 up
ip -n rd15-ing-management addr add 198.18.53.1/24 dev peer0
ip -n rd15-ing-client addr add 198.18.50.100/24 dev peer0
ip -n rd15-ing-client route add default via 198.18.50.1
ip -n rd15-ing-backend-a addr add 198.18.50.10/24 dev peer0
ip -n rd15-ing-backend-a route add default via 198.18.50.1
ip -n rd15-ing-backend-b addr add 198.18.50.20/24 dev peer0
ip -n rd15-ing-backend-b route add default via 198.18.50.1

start_port_forward() {
 ip netns exec rd15-ing-backend-a python3 "$work/backend.py" port-forward 198.18.50.10 8081 "$work/port-forward.events" & port_forward_pid=$!
}
start_backend_a() {
 ip netns exec rd15-ing-backend-a python3 "$work/backend.py" backend-a 198.18.50.10 8080 "$work/backend-a.events" & backend_a_pid=$!
}
start_backend_b() {
 ip netns exec rd15-ing-backend-b python3 "$work/backend.py" backend-b 198.18.50.20 8080 "$work/backend-b.events" & backend_b_pid=$!
}
start_port_forward
start_backend_a
start_backend_b

probe() {
 local ns=$1 address=$2 port=$3 expected=$4 expected_source=$5
 ip netns exec "$ns" python3 - "$address" "$port" "$expected" "$expected_source" <<'PY'
import socket, sys
address, port, expected, source = sys.argv[1], int(sys.argv[2]), sys.argv[3], sys.argv[4]
with socket.create_connection((address, port), timeout=3) as conn:
    got = conn.recv(256).decode().strip()
if got != f"{expected}:{source}":
    raise SystemExit(f"response={got!r}; want {expected}:{source}")
PY
}
nft_nat_has() {
 # Do not use grep -q in a pipe under pipefail: it closes early and makes nft
 # report SIGPIPE even when the requested rule is present.
 nft list table ip routerd_nat | grep -F "$1" >/dev/null
}
capture_ingress_packets() {
 [ "${ROUTERD_INGRESS_DEBUG:-}" = 1 ] || return 0
 timeout 5 tcpdump -nn -l -i rd15-ing-lan 'tcp port 8080' > "$work/ingress-packets.txt" 2>&1 & capture_pid=$!
 sleep .2
}
for _ in {1..20}; do
 if probe rd15-ing-backend-a 198.18.50.10 8081 port-forward 198.18.50.10 >/dev/null 2>&1 && probe rd15-ing-backend-a 198.18.50.10 8080 backend-a 198.18.50.10 >/dev/null 2>&1 && probe rd15-ing-backend-b 198.18.50.20 8080 backend-b 198.18.50.20 >/dev/null 2>&1; then break; fi
 sleep 1
done
probe rd15-ing-backend-a 198.18.50.10 8081 port-forward 198.18.50.10
probe rd15-ing-backend-a 198.18.50.10 8080 backend-a 198.18.50.10
probe rd15-ing-backend-b 198.18.50.20 8080 backend-b 198.18.50.20

cat > "$work/base.json" <<'JSON'
{
  "apiVersion":"routerd.net/v1alpha1", "kind":"Router", "metadata":{"name":"ingress-smoke"},
  "spec":{"resources":[
    {"apiVersion":"net.routerd.net/v1alpha1","kind":"Interface","metadata":{"name":"wan"},"spec":{"ifname":"rd15-ing-wan","managed":false,"owner":"external"}},
    {"apiVersion":"net.routerd.net/v1alpha1","kind":"IPv4StaticAddress","metadata":{"name":"wan-v4"},"spec":{"interface":"wan","address":"198.18.52.2/24"}},
    {"apiVersion":"net.routerd.net/v1alpha1","kind":"Interface","metadata":{"name":"lan"},"spec":{"ifname":"rd15-ing-lan","managed":false,"owner":"external"}},
    {"apiVersion":"net.routerd.net/v1alpha1","kind":"IPv4StaticAddress","metadata":{"name":"lan-v4"},"spec":{"interface":"lan","address":"198.18.50.1/24"}},
    {"apiVersion":"system.routerd.net/v1alpha1","kind":"Sysctl","metadata":{"name":"forward-v4"},"spec":{"key":"net.ipv4.ip_forward","value":"1"}},
    {"apiVersion":"firewall.routerd.net/v1alpha1","kind":"FirewallZone","metadata":{"name":"wan"},"spec":{"role":"untrust","interfaces":["wan"]}},
    {"apiVersion":"firewall.routerd.net/v1alpha1","kind":"FirewallZone","metadata":{"name":"lan"},"spec":{"role":"trust","interfaces":["lan"]}},
    {"apiVersion":"firewall.routerd.net/v1alpha1","kind":"FirewallPolicy","metadata":{"name":"default"},"spec":{}},
    {"apiVersion":"firewall.routerd.net/v1alpha1","kind":"PortForward","metadata":{"name":"fixed"},"spec":{"listen":{"interface":"wan","address":"198.18.52.2","protocol":"tcp","port":18080},"target":{"address":"198.18.50.10","port":8081},"hairpin":{"enabled":true,"interfaces":["lan"]}}},
    {"apiVersion":"firewall.routerd.net/v1alpha1","kind":"IngressService","metadata":{"name":"edge"},"spec":{"listen":{"interface":"wan","address":"198.18.52.2","protocol":"tcp","port":18081},"backends":[{"name":"backend-a","address":"198.18.50.10","port":8080},{"name":"backend-b","address":"198.18.50.20","port":8080}],"hairpin":{"enabled":true,"interfaces":["lan"]},"healthCheck":{"protocol":"tcp","interval":"1s","timeout":"500ms","healthyThreshold":1,"unhealthyThreshold":1},"policy":{"selection":"failover","onNoHealthyBackends":"reject"}}}
  ]}
}
JSON
python3 - "$work/base.json" "$work/no-port-forward.json" <<'PY'
import json, sys
router = json.load(open(sys.argv[1]))
router['spec']['resources'] = [r for r in router['spec']['resources'] if r['kind'] != 'PortForward']
json.dump(router, open(sys.argv[2], 'w'))
PY
python3 - "$work/base.json" "$management" <<'PY'
import json, sys
path, ifname = sys.argv[1:]
router = json.load(open(path))
resources = router['spec']['resources']
resources.append({'apiVersion':'net.routerd.net/v1alpha1','kind':'Interface','metadata':{'name':'management'},'spec':{'ifname':ifname,'managed':False,'owner':'external'}})
resources.append({'apiVersion':'firewall.routerd.net/v1alpha1','kind':'FirewallZone','metadata':{'name':'management'},'spec':{'role':'mgmt','interfaces':['management']}})
resources.append({'apiVersion':'net.routerd.net/v1alpha1','kind':'IPv4StaticAddress','metadata':{'name':'management-v4'},'spec':{'interface':'management','address':'198.18.53.2/24'}})
json.dump(router, open(path, 'w'))
PY
# Keep the no-PortForward candidate otherwise identical, including its
# management-zone safety path.
python3 - "$work/base.json" "$work/no-port-forward.json" <<'PY'
import json, sys
base = json.load(open(sys.argv[1]))
base['spec']['resources'] = [r for r in base['spec']['resources'] if r['kind'] != 'PortForward']
json.dump(base, open(sys.argv[2], 'w'))
PY
/usr/local/sbin/routerd validate --config "$work/base.json"
/usr/local/sbin/routerd validate --config "$work/no-port-forward.json"

systemctl start routerd
stage='initial-apply'
/usr/local/sbin/routerctl apply -f "$work/base.json" --replace >/dev/null
active_backend() {
 /usr/local/sbin/routerctl get IngressService/edge -o json | jq -r '.items[0].status.activeBackend.name // empty'
}
await_backend() {
 local expected=$1 actual=''
 for _ in {1..30}; do
  actual=$(active_backend 2>/dev/null || true)
  if [ "$actual" = "$expected" ]; then return 0; fi
  sleep 1
 done
 printf 'active backend=%q; expected=%q\n' "$actual" "$expected" >&2
 /usr/local/sbin/routerctl get IngressService/edge -o json >&2 || true
 return 1
}
await_address() {
 local device=$1 address=$2
 for _ in {1..30}; do
  if ip -4 -j addr show dev "$device" | jq -e --arg address "$address" '.[0].addr_info | any(.local == $address)' >/dev/null; then return 0; fi
  sleep 1
 done
 ip -4 addr show dev "$device" >&2 || true
 return 1
}
stage='initial-ingress-health'
await_backend backend-a
stage='initial-listener-addresses'
await_address rd15-ing-wan 198.18.52.2
await_address rd15-ing-lan 198.18.50.1
await_address "$management" 198.18.53.2
ip netns exec rd15-ing-management ping -c 1 -W 2 198.18.53.2 >/dev/null
stage='initial-nat-render'
for _ in {1..30}; do
 if nft_nat_has 'routerd PortForward fixed' && nft_nat_has 'routerd IngressService edge'; then break; fi
 sleep 1
done
nft_nat_has 'routerd PortForward fixed'
nft_nat_has 'routerd IngressService edge'
nft_nat_has 'routerd PortForward fixed hairpin'
nft_nat_has 'routerd IngressService edge hairpin'

stage='port-forward-dnat-and-hairpin'
probe rd15-ing-external 198.18.52.2 18080 port-forward 198.18.52.1
probe rd15-ing-client 198.18.52.2 18080 port-forward 198.18.50.1
stage='ingress-dnat-and-hairpin'
capture_ingress_packets
probe rd15-ing-external 198.18.52.2 18081 backend-a 198.18.52.1
if [ -n "$capture_pid" ]; then wait "$capture_pid" || true; capture_pid=''; fi
probe rd15-ing-client 198.18.52.2 18081 backend-a 198.18.50.1

cat > "$work/persistent.py" <<'PY'
import pathlib, socket, sys, time
work = pathlib.Path(sys.argv[1])
with socket.create_connection(('198.18.52.2', 18080), timeout=3) as conn:
    first = conn.recv(256).decode().strip()
    if first != 'port-forward:198.18.52.1':
        raise SystemExit(f'initial response={first!r}')
    (work / 'persistent.ready').touch()
    deadline = time.monotonic() + 30
    while not (work / 'persistent.release').exists():
        if time.monotonic() >= deadline:
            raise SystemExit('timed out waiting for route withdrawal')
        time.sleep(.05)
    conn.sendall(b'old-flow\n')
    got = conn.recv(256).decode().strip()
    if got != 'port-forward:old-flow':
        raise SystemExit(f'old flow response={got!r}')
PY
ip netns exec rd15-ing-external python3 "$work/persistent.py" "$work" & persistent_pid=$!
for _ in {1..30}; do test -e "$work/persistent.ready" && break; sleep .1; done
test -e "$work/persistent.ready"
stage='port-forward-withdrawal-preserves-existing-flow'
/usr/local/sbin/routerctl apply -f "$work/no-port-forward.json" --replace >/dev/null
for _ in {1..20}; do if ! nft_nat_has 'routerd PortForward fixed'; then break; fi; sleep 1; done
if nft_nat_has 'routerd PortForward fixed'; then
 echo 'withdrawn PortForward remains in nftables' >&2
 exit 1
fi
touch "$work/persistent.release"
wait "$persistent_pid"
if probe rd15-ing-external 198.18.52.2 18080 port-forward 198.18.52.1; then
 echo 'withdrawn PortForward admitted a new flow' >&2
 exit 1
fi

stage='ingress-health-failover'
kill "$backend_a_pid"
wait "$backend_a_pid" || true
backend_a_pid=''
await_backend backend-b
probe rd15-ing-external 198.18.52.2 18081 backend-b 198.18.52.1
probe rd15-ing-client 198.18.52.2 18081 backend-b 198.18.50.1

stage='ingress-backend-recovery'
start_backend_a
await_backend backend-a
probe rd15-ing-external 198.18.52.2 18081 backend-a 198.18.52.1

stage='ingress-drain'
/usr/local/sbin/routerctl ingress drain ingress/edge backend=backend-a --duration=2m >/dev/null
await_backend backend-b
probe rd15-ing-external 198.18.52.2 18081 backend-b 198.18.52.1
/usr/local/sbin/routerctl ingress undrain ingress/edge backend=backend-a >/dev/null
await_backend backend-a

stage='ingress-service-restart'
systemctl restart routerd
for _ in {1..30}; do
 if actual=$(active_backend 2>/dev/null) && [ "$actual" = backend-a ] && probe rd15-ing-external 198.18.52.2 18081 backend-a 198.18.52.1 >/dev/null 2>&1; then break; fi
 sleep 1
done
await_backend backend-a
probe rd15-ing-external 198.18.52.2 18081 backend-a 198.18.52.1

stage='full-withdrawal'
/usr/local/sbin/routerctl apply -f "$empty" --replace >/dev/null
for _ in {1..20}; do
 if nft -j list tables | jq -e '[.nftables[].table? | select(.name=="routerd_nat" or .name=="routerd_filter")] | length == 0' >/dev/null; then break; fi
 sleep 1
done
nft -j list tables | jq -e '[.nftables[].table? | select(.name=="routerd_nat" or .name=="routerd_filter")] | length == 0' >/dev/null
if probe rd15-ing-external 198.18.52.2 18081 backend-a 198.18.52.1; then
 echo 'withdrawn IngressService admitted a new flow' >&2
 exit 1
fi
echo 'PASS: PortForward DNAT/hairpin/withdrawal and IngressService failover/drain/restart/withdrawal'
