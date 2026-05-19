#!/usr/bin/env bash
set -euo pipefail

TEST_NAME="bgp-import-policy-reject"
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
create_veth_pair "$R1" eth0 10.90.0.1/30 "$R2" eth0 10.90.0.2/30
ip -n "$R2" route add blackhole 203.0.113.0/24
ip -n "$R2" route add blackhole 198.51.100.0/24

basic_zebra_conf r1 >"$WORKDIR/r1-zebra.conf"
basic_zebra_conf r2 >"$WORKDIR/r2-zebra.conf"
cat >"$WORKDIR/r1-bgpd.conf" <<EOF
hostname r1
password zebra
log file $WORKDIR/r1-bgpd.log
router bgp 64512
 bgp router-id 10.90.0.1
 no bgp ebgp-requires-policy
 neighbor 10.90.0.2 remote-as 64513
 address-family ipv4 unicast
  neighbor 10.90.0.2 route-map ROUTERD-IN in
 exit-address-family
exit
ip prefix-list ROUTERD-ALLOWED seq 5 permit 203.0.113.0/24
route-map ROUTERD-IN permit 10
 match ip address prefix-list ROUTERD-ALLOWED
route-map ROUTERD-IN deny 65535
EOF
cat >"$WORKDIR/r2-bgpd.conf" <<EOF
hostname r2
password zebra
log file $WORKDIR/r2-bgpd.log
router bgp 64513
 bgp router-id 10.90.0.2
 no bgp ebgp-requires-policy
 neighbor 10.90.0.1 remote-as 64512
 address-family ipv4 unicast
  network 203.0.113.0/24
  network 198.51.100.0/24
 exit-address-family
exit
EOF

start_frr "$R1" "$PS1" "$WORKDIR/r1-zebra.conf" "$WORKDIR/r1-bgpd.conf"
start_frr "$R2" "$PS2" "$WORKDIR/r2-zebra.conf" "$WORKDIR/r2-bgpd.conf"

r1_established() {
  vtysh_ns "$R1" "$PS1" -c "show bgp summary" | grep -q Established
}
r1_has_allowed() {
  vtysh_ns "$R1" "$PS1" -c "show bgp ipv4 unicast" | grep -q "203.0.113.0/24"
}

wait_for 30 r1_established
wait_for 20 r1_has_allowed
if vtysh_ns "$R1" "$PS1" -c "show bgp ipv4 unicast" | grep -q "198.51.100.0/24"; then
  fail "disallowed prefix 198.51.100.0/24 was accepted"
fi

log "ok: allowed prefix accepted and disallowed prefix rejected"
