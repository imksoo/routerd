#!/usr/bin/env bash
set -euo pipefail

TEST_NAME="bgp-event-ordering"
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_common
require_cmd vtysh
require_cmd grep

R1="${TEST_ID}-r1"
R2="${TEST_ID}-r2"
PS1="${TEST_ID}-r1"
PS2="${TEST_ID}-r2"
create_ns "$R1"
create_ns "$R2"
create_veth_pair "$R1" eth0 10.91.0.1/30 "$R2" eth0 10.91.0.2/30
ip -n "$R2" route add blackhole 203.0.113.0/24

basic_zebra_conf r1 >"$WORKDIR/r1-zebra.conf"
basic_zebra_conf r2 >"$WORKDIR/r2-zebra.conf"
cat >"$WORKDIR/r1-bgpd.conf" <<EOF
hostname r1
password zebra
log file $WORKDIR/r1-bgpd.log
router bgp 64512
 bgp router-id 10.91.0.1
 no bgp ebgp-requires-policy
 neighbor 10.91.0.2 remote-as 64513
 timers bgp 1 3
exit
EOF
cat >"$WORKDIR/r2-bgpd.conf" <<EOF
hostname r2
password zebra
log file $WORKDIR/r2-bgpd.log
router bgp 64513
 bgp router-id 10.91.0.2
 no bgp ebgp-requires-policy
 neighbor 10.91.0.1 remote-as 64512
 timers bgp 1 3
 address-family ipv4 unicast
  network 203.0.113.0/24
 exit-address-family
exit
EOF

start_frr "$R1" "$PS1" "$WORKDIR/r1-zebra.conf" "$WORKDIR/r1-bgpd.conf"
start_frr "$R2" "$PS2" "$WORKDIR/r2-zebra.conf" "$WORKDIR/r2-bgpd.conf"

r1_established() {
  vtysh_ns "$R1" "$PS1" -c "show bgp summary" | grep -q Established
}

r1_not_established() {
  ! r1_established
}

observe() {
  local state="down"
  local prefixes=0
  if vtysh_ns "$R1" "$PS1" -c "show bgp summary" | grep -q Established; then
    state="established"
  fi
  if vtysh_ns "$R1" "$PS1" -c "show bgp ipv4 unicast" | grep -q "203.0.113.0/24"; then
    prefixes=1
  fi
  printf '%(%s)T\t%s\t%s\n' -1 "$state" "$prefixes" >>"$WORKDIR/events.tsv"
}

wait_for 30 r1_established
observe
for _ in 1 2 3; do
  stop_bgpd "$PS2"
  wait_for 8 r1_not_established
  observe
  ip netns exec "$R2" "$(frr_cmd bgpd)" -N "$PS2" -f "$WORKDIR/r2-bgpd.conf" -i "$WORKDIR/$PS2-bgpd.pid" -d
  wait_for 30 r1_established
  observe
done

python3 - "$WORKDIR/events.tsv" <<'PY'
import sys
rows = [line.strip().split("\t") for line in open(sys.argv[1], encoding="utf-8") if line.strip()]
if len(rows) < 7:
    raise SystemExit("not enough observations")
for idx, (_, state, prefixes) in enumerate(rows):
    if prefixes == "1" and state != "established":
        raise SystemExit(f"prefix observed before established at row {idx}: {rows[idx]}")
states = [row[1] for row in rows]
if states.count("down") < 3 or states.count("established") < 4:
    raise SystemExit(f"missing expected flap sequence: {states}")
PY

log "ok: peer flaps produced ordered down/established observations without prefix-before-peer"
