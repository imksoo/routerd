#!/usr/bin/env bash
set -euo pipefail

TEST_NAME="keepalived-vip-failover"
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_common
require_cmd keepalived
require_cmd grep

NS1="${TEST_ID}-r1"
NS2="${TEST_ID}-r2"
BR="${TEST_ID}-br"
VIP="10.88.66.100"
create_ns "$NS1"
create_ns "$NS2"
create_bridge_segment "$BR" "$NS1" eth0 10.88.66.1/24 "$NS2" eth0 10.88.66.2/24

cat >"$WORKDIR/r1.conf" <<EOF
global_defs {
  router_id r1
}
vrrp_instance VI_API {
  state BACKUP
  interface eth0
  virtual_router_id 66
  priority 150
  advert_int 1
  preempt
  unicast_src_ip 10.88.66.1
  unicast_peer {
    10.88.66.2
  }
  virtual_ipaddress {
    $VIP/32 dev eth0
  }
}
EOF
cat >"$WORKDIR/r2.conf" <<EOF
global_defs {
  router_id r2
}
vrrp_instance VI_API {
  state BACKUP
  interface eth0
  virtual_router_id 66
  priority 100
  advert_int 1
  nopreempt
  unicast_src_ip 10.88.66.2
  unicast_peer {
    10.88.66.1
  }
  virtual_ipaddress {
    $VIP/32 dev eth0
  }
}
EOF

ip netns exec "$NS1" keepalived -n -l -f "$WORKDIR/r1.conf" >"$WORKDIR/r1.keepalived.log" 2>&1 &
PID1=$!
add_cleanup "kill '$PID1'"
ip netns exec "$NS2" keepalived -n -l -f "$WORKDIR/r2.conf" >"$WORKDIR/r2.keepalived.log" 2>&1 &
PID2=$!
add_cleanup "kill '$PID2'"

wait_for 8 bash -c "ip -n '$NS1' addr show dev eth0 | grep -q '$VIP'"
if ip -n "$NS2" addr show dev eth0 | grep -q "$VIP"; then
  fail "VIP present on both routers before failover"
fi

kill "$PID1"
wait "$PID1" 2>/dev/null || true
wait_for 8 bash -c "ip -n '$NS2' addr show dev eth0 | grep -q '$VIP'"

log "ok: VIP failed over to standby within advert/preempt timing"
