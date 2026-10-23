#!/usr/bin/env bash
set -euo pipefail
# Requirement: excluding IPv6 service on an IPv4-only parent removes that
# parent's DHCPv6 warning while completed IPv4/DHCPv6/RA/DNS exchanges survive.
# Missing observations remain incomplete, not product failures or successes.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib.sh"
require_common
require_cmd dig
DNSMASQ="${1:?absolute dnsmasq path}"
RULES="${2:?generated firewall path}"
CONFIG="${3:?generated dnsmasq configuration path}"
require_cmd nft
[[ "$DNSMASQ" = /* && "$RULES" = /* && "$CONFIG" = /* ]] || fail "absolute paths required"
DAEMON=''
cleanup_preserve() {
  local result=$? c=0 e i
  set +e
  if [[ -n "$DAEMON" ]] && kill -0 "$DAEMON" 2>/dev/null; then
    kill "$DAEMON"
    wait "$DAEMON"
    e=$?
    printf 'daemon_stop_exit=%s\n' "$e" >>"$WORKDIR/cleanup.log"
    [[ "$e" == 0 || "$e" == 143 ]] || c=2
  fi
  for ((i=${#CLEANUP[@]}-1;i>=0;i--)); do
    printf 'cleanup command=%s\n' "${CLEANUP[$i]}" >>"$WORKDIR/cleanup.log"
    eval "${CLEANUP[$i]}" >>"$WORKDIR/cleanup.log" 2>&1
    e=$?; printf 'cleanup exit=%s\n' "$e" >>"$WORKDIR/cleanup.log"
    [[ "$e" == 0 ]] || c=2
  done
  printf 'trial_exit=%s cleanup_exit=%s\n' "$result" "$c" >>"$WORKDIR/cleanup.log"
  log "evidence=$WORKDIR trial_exit=$result cleanup_exit=$c"
  [[ "$result" == 0 ]] || exit "$result"
  exit "$c"
}
trap cleanup_preserve EXIT
sha256sum "$DNSMASQ" "$SCRIPT_DIR/dhcp-family-client.py" "$0" "$SCRIPT_DIR/lib.sh" "$RULES" "$CONFIG" >"$WORKDIR/source-sha256.txt"
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
chmod 755 "$WORKDIR"
sleep 2
ip -n "$ROUTER" -j address >"$WORKDIR/router-addresses.json"
ip -n "$CLIENT" -j address >"$WORKDIR/client-addresses.json"
ip netns exec "$ROUTER" nft -c -f "$RULES" >"$WORKDIR/nft-check.log" 2>&1
ip netns exec "$ROUTER" nft -f "$RULES"
for CASE in baseline candidate restored; do
  # The generated service configuration is used in all cases. Adapt only
  # filesystem paths, the notification hook (outside this test's scope), and
  # the DNS fixture. Baseline/restored remove just the candidate exclusion.
  python3 - "$CONFIG" "$WORKDIR" "$CASE" <<'PY'
import pathlib,sys
source,work,case=sys.argv[1:]; work=pathlib.Path(work)
lines=pathlib.Path(source).read_text().splitlines()
lines=[l for l in lines if not l.startswith(('port=','pid-file=','dhcp-leasefile=','dhcp-script='))]
if case!='candidate': lines=[l for l in lines if not l.startswith('no-dhcpv6-interface=')]
lines += ['address=/fixture.invalid/192.0.2.1','log-dhcp','pid-file='+str(work/(case+'.pid')),'dhcp-leasefile='+str(work/(case+'.leases'))]
(work/(case+'.conf')).write_text('\n'.join(lines)+'\n')
PY
  ip netns exec "$ROUTER" "$DNSMASQ" --test --conf-file="$WORKDIR/$CASE.conf" >"$WORKDIR/$CASE-syntax.log" 2>&1
  ip netns exec "$ROUTER" "$DNSMASQ" --keep-in-foreground --conf-file="$WORKDIR/$CASE.conf" --log-facility=- >"$WORKDIR/$CASE-dnsmasq.log" 2>&1 &
  DAEMON=$!
  wait_for 5 test -s "$WORKDIR/$CASE.pid" || { log 'incomplete: daemon readiness missing'; exit 3; }
  sleep 1
  set +e
  ip netns exec "$CLIENT" timeout 35 python3 "$SCRIPT_DIR/dhcp-family-client.py" eth0 >"$WORKDIR/$CASE-exchange.json" 2>"$WORKDIR/$CASE-exchange.err"
  e=$?
  set -e
  printf '%s\n' "$e" >"$WORKDIR/$CASE-exchange.exit"
  [[ "$e" == 0 ]] || { log "incomplete exchange: $CASE exit=$e"; exit 3; }
  ip netns exec "$CLIENT" dig +time=3 +tries=1 +short @192.0.2.1 fixture.invalid A >"$WORKDIR/$CASE-dns-v4.txt"
  ip netns exec "$CLIENT" dig +time=3 +tries=1 +short @fd00:1234::1 fixture.invalid A >"$WORKDIR/$CASE-dns-v6.txt"
  sleep 0.2
  kill "$DAEMON"
  set +e
  wait "$DAEMON"
  e=$?
  set -e
  printf '%s\n' "$e" >"$WORKDIR/$CASE-daemon-exit.txt"
  [[ "$e" == 0 || "$e" == 143 ]] || exit 3
  DAEMON=''
done
ip netns exec "$ROUTER" nft -j list chain inet routerd_filter input >"$WORKDIR/guard-counters.json"
python3 - "$WORKDIR" <<'PY'
import datetime,json,pathlib,sys
p=pathlib.Path(sys.argv[1]); out={'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'production_changed':False,'cases':{}}
for case in ['baseline','candidate','restored']:
    log=(p/(case+'-dnsmasq.log')).read_text()
    warning='no address range available for DHCPv6 request via lan-parent'
    exchange=json.loads((p/(case+'-exchange.json')).read_text())
    dns=[(p/(case+'-dns-'+family+'.txt')).read_text().strip() for family in ['v4','v6']]
    out['cases'][case]={'warning_count':log.count(warning),'exchange':exchange,'dns_answers':dns}
    assert 'DHCP packet received on lan-vrrp which has no address' not in log
    assert exchange['dhcpv4']=='offer-and-ack' and exchange['dhcpv6']=='information-reply-with-DNS'
    assert exchange['ra']=='positive-router-lifetime' and exchange['ra_lifetime_seconds']==7200
    assert dns==['192.0.2.1','192.0.2.1'], ('missing required DNS observation',case,dns)
assert out['cases']['baseline']['warning_count']>0
assert out['cases']['candidate']['warning_count']==0
assert out['cases']['restored']['warning_count']>0
rules=json.loads((p/'guard-counters.json').read_text())['nftables']
guard=next(x['rule'] for x in rules if x.get('rule',{}).get('comment')=='routerd DHCPv4 excluded on IPv6-only listener')
out['guard_packets']=sum(e['counter']['packets'] for e in guard['expr'] if 'counter' in e)
assert out['guard_packets']>=6
(p/'result.json').write_text(json.dumps(out,indent=2)+'\n')
print(json.dumps(out))
PY
