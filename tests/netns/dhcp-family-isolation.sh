#!/usr/bin/env bash
set -euo pipefail
# Requirement: completed DHCPv4 and stateless DHCPv6/RA exchange must survive
# the generated input guard, while unwanted IPv4 copies hit its counter.
# Missing replies are observation failures; all evidence and cleanup are kept.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib.sh"
require_common
require_cmd nft
DNSMASQ="${1:?absolute path to tested dnsmasq}"
RULES="${2:?absolute path to generated candidate firewall rules}"
[[ "$DNSMASQ" = /* && "$RULES" = /* ]] || fail "paths must be absolute"
cleanup_preserve() {
  local result=$? c=0 e i
  set +e
  for ((i=${#CLEANUP[@]}-1;i>=0;i--)); do
    printf 'cleanup command=%s\n' "${CLEANUP[$i]}" >>"$WORKDIR/cleanup.log"
    eval "${CLEANUP[$i]}" >>"$WORKDIR/cleanup.log" 2>&1
    e=$?; printf 'cleanup exit=%s\n' "$e" >>"$WORKDIR/cleanup.log"
    [[ "$e" = 0 ]] || c=2
  done
  printf 'test_exit=%s cleanup_exit=%s\n' "$result" "$c" >>"$WORKDIR/cleanup.log"
  log "evidence=$WORKDIR test_exit=$result cleanup_exit=$c"
  [[ "$result" = 0 ]] || exit "$result"
  exit "$c"
}
trap cleanup_preserve EXIT
sha256sum "$DNSMASQ" "$RULES" "$SCRIPT_DIR/dhcp-family-client.py" >"$WORKDIR/source-sha256.txt"
cp "$RULES" "$WORKDIR/firewall.nft"
ROUTER="${TEST_ID}-router"
CLIENT="${TEST_ID}-client"
create_ns "$ROUTER"
create_ns "$CLIENT"
create_veth_pair "$ROUTER" lan-parent 192.0.2.1/24 "$CLIENT" eth0 192.0.2.2/24
ip -n "$ROUTER" link add link lan-parent name lan-vrrp type macvlan mode bridge
ip -n "$ROUTER" link set lan-vrrp up
ip -n "$ROUTER" -6 addr add fd00:1234::1/64 dev lan-vrrp nodad
ip -n "$ROUTER" -6 addr add fe80::1/64 dev lan-vrrp nodad
ip -n "$CLIENT" -6 addr add fe80::2/64 dev eth0 nodad
cat >"$WORKDIR/dnsmasq.conf" <<EOF
port=0
no-resolv
no-hosts
bind-dynamic
interface=lan-parent
interface=lan-vrrp
dhcp-range=192.0.2.100,192.0.2.110,255.255.255.0,10m
dhcp-range=set:v6,::,constructor:lan-vrrp,ra-stateless,64,10m
dhcp-option=tag:v6,option6:dns-server,[fd00:1234::1]
enable-ra
ra-param=lan-vrrp,mtu:1454,high,0,7200
dhcp-leasefile=$WORKDIR/leases
pid-file=$WORKDIR/dnsmasq.pid
EOF
chmod 755 "$WORKDIR"
ip netns exec "$ROUTER" "$DNSMASQ" --test --conf-file="$WORKDIR/dnsmasq.conf" >"$WORKDIR/syntax.log" 2>&1
ip netns exec "$ROUTER" "$DNSMASQ" --keep-in-foreground --conf-file="$WORKDIR/dnsmasq.conf" --log-facility=- >"$WORKDIR/dnsmasq.log" 2>&1 &
DAEMON=$!
add_cleanup "if kill -0 '$DAEMON' 2>/dev/null; then kill '$DAEMON'; wait '$DAEMON'; fi"
wait_for 5 test -s "$WORKDIR/dnsmasq.pid" || fail "daemon readiness not observed"
sleep 2
ip -n "$ROUTER" -j address >"$WORKDIR/router-addresses.json"
ip -n "$CLIENT" -j address >"$WORKDIR/client-addresses.json"
ip netns exec "$ROUTER" tcpdump -ni any -l -vvv 'udp port 67 or udp port 68 or udp port 546 or udp port 547 or icmp6' >"$WORKDIR/packets.log" 2>&1 &
CAPTURE=$!
add_cleanup "if kill -0 '$CAPTURE' 2>/dev/null; then kill -INT '$CAPTURE'; wait '$CAPTURE'; fi"
ip netns exec "$CLIENT" python3 "$SCRIPT_DIR/dhcp-family-client.py" eth0 >"$WORKDIR/before-exchange.json"
sleep 0.2
cp "$WORKDIR/dnsmasq.log" "$WORKDIR/before-dnsmasq.log"
grep -q 'DHCP packet received on lan-vrrp which has no address' "$WORKDIR/before-dnsmasq.log" || fail "baseline warning not reproduced"
LINES=$(wc -l <"$WORKDIR/dnsmasq.log")
ip netns exec "$ROUTER" nft -c -f "$RULES" >"$WORKDIR/nft-check.log" 2>&1
ip netns exec "$ROUTER" nft -f "$RULES"
ip netns exec "$CLIENT" python3 "$SCRIPT_DIR/dhcp-family-client.py" eth0 >"$WORKDIR/after-exchange.json"
sleep 0.2
tail -n +"$((LINES+1))" "$WORKDIR/dnsmasq.log" >"$WORKDIR/after-dnsmasq.log"
ip netns exec "$ROUTER" nft -j list chain inet routerd_filter input >"$WORKDIR/input-counters.json"
python3 - "$WORKDIR" <<'PY'
import json,pathlib,sys
p=pathlib.Path(sys.argv[1]);data=json.loads((p/'input-counters.json').read_text())
rules=[x['rule'] for x in data['nftables'] if 'rule' in x]
rule=next(r for r in rules if r.get('comment')=='routerd DHCPv4 excluded on IPv6-only listener')
count=sum(x['counter']['packets'] for x in rule['expr'] if 'counter' in x)
assert count>=2, 'missing evidence that both DHCPv4 copies reached the guard'
assert 'DHCP packet received on lan-vrrp which has no address' not in (p/'after-dnsmasq.log').read_text()
print(json.dumps({'guard_packets':count,'before':json.loads((p/'before-exchange.json').read_text()),'after':json.loads((p/'after-exchange.json').read_text())}))
PY
