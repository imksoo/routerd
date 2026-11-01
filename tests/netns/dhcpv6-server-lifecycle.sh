#!/usr/bin/env bash
set -euo pipefail
# Product-rendered dnsmasq DHCPv6 stateful/both service with an independent client.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib.sh"
require_common
require_cmd dnsmasq
require_cmd go

router="${TEST_ID}-router"
client="${TEST_ID}-client"
create_ns "$router"
create_ns "$client"
create_veth_pair "$router" lan0 192.0.2.1/24 "$client" client0 192.0.2.2/24
ip -n "$router" link set dev lan0 addrgenmode none
ip -n "$client" link set dev client0 addrgenmode none
ip -n "$router" -6 addr add fe80::1/64 dev lan0 nodad
ip -n "$router" -6 addr add fd00:1234::1/64 dev lan0 nodad
ip -n "$client" -6 addr add fe80::2/64 dev client0 nodad

for mode in stateful both; do
  config="$WORKDIR/$mode.conf"
  ip netns exec "$router" go run "$SCRIPT_DIR/dhcpv6-server-render" "$mode" >"$config"
  python3 - "$config" <<'PY'
import pathlib, sys
path = pathlib.Path(sys.argv[1])
path.write_text('\n'.join(line for line in path.read_text().splitlines() if not line.startswith('dhcp-leasefile=')) + '\n')
PY
  printf 'pid-file=%s\ndhcp-leasefile=%s\nlog-dhcp\n' "$WORKDIR/$mode.pid" "$WORKDIR/$mode.leases" >>"$config"
  ip netns exec "$router" dnsmasq --test --conf-file="$config"
  ip netns exec "$router" dnsmasq --keep-in-foreground --conf-file="$config" --log-facility=- >"$WORKDIR/$mode.log" 2>&1 &
  daemon=$!
  add_cleanup "if kill -0 '$daemon' 2>/dev/null; then kill '$daemon'; wait '$daemon'; fi"
  wait_for 5 test -s "$WORKDIR/$mode.pid" || fail "$mode dnsmasq readiness"
  ip netns exec "$client" timeout 30 python3 "$SCRIPT_DIR/dhcpv6-stateful-client.py" client0 >"$WORKDIR/$mode.json"
  kill "$daemon"
  set +e
  wait "$daemon"
  daemon_exit=$?
  set -e
  [[ "$daemon_exit" -eq 0 || "$daemon_exit" -eq 143 ]] || fail "$mode dnsmasq exit=$daemon_exit"
done

python3 - "$WORKDIR" <<'PY'
import json,pathlib,sys
work=pathlib.Path(sys.argv[1])
for mode in ('stateful','both'):
    result=json.loads((work/(mode+'.json')).read_text())
    assert result['dhcpv6']=='solicit-request-renew-release', result
    assert result['address'].startswith('fd00:1234::1'), result
PY
printf 'PASS: product-rendered DHCPv6 stateful and both server modes completed independent solicit/request/renew/release exchanges\n'
