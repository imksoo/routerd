#!/usr/bin/env bash
set -euo pipefail

TEST_NAME="frr-config-rollback"
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_common
require_cmd vtysh
require_cmd grep

NS="${TEST_ID}-r1"
PS="${TEST_ID}-r1"
create_ns "$NS"

ZEBRA_CONF="$WORKDIR/zebra.conf"
BGPD_CONF="$WORKDIR/bgpd.conf"
GOOD_CONF="$WORKDIR/good.conf"
BAD_CONF="$WORKDIR/bad.conf"

basic_zebra_conf r1 >"$ZEBRA_CONF"
cat >"$BGPD_CONF" <<EOF
hostname r1
password zebra
log file $WORKDIR/r1-bgpd.log
service integrated-vtysh-config
router bgp 64512
 bgp router-id 10.0.0.1
 no bgp ebgp-requires-policy
exit
EOF

start_frr "$NS" "$PS" "$ZEBRA_CONF" "$BGPD_CONF"
wait_for 10 vtysh_ns "$NS" "$PS" -c "show running-config" >/dev/null

cat >"$GOOD_CONF" <<EOF
service integrated-vtysh-config
router bgp 64512
 bgp router-id 10.0.0.1
 no bgp ebgp-requires-policy
exit
EOF
vtysh_ns "$NS" "$PS" -f "$GOOD_CONF"
vtysh_ns "$NS" "$PS" -c "show running-config" | grep -q "router bgp 64512"

cat >"$BAD_CONF" <<EOF
service integrated-vtysh-config
router bgp 64512
 this-command-does-not-exist
exit
EOF

if frr_reload_ns "$NS" "$PS" "$BAD_CONF" >"$WORKDIR/reload.out" 2>&1; then
  cat "$WORKDIR/reload.out" >&2
  fail "frr-reload unexpectedly accepted invalid config"
fi

vtysh_ns "$NS" "$PS" -c "show running-config" >"$WORKDIR/running.after"
grep -q "router bgp 64512" "$WORKDIR/running.after"
if grep -q "this-command-does-not-exist" "$WORKDIR/running.after"; then
  fail "invalid command leaked into running config"
fi

log "ok: invalid reload failed and previous running config remained active"
