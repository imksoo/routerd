#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"

require_common
require_cmd nft
require_cmd ping

SRC_NS="${TEST_ID}-src"
RTR_NS="${TEST_ID}-rtr"
DST_NS="${TEST_ID}-dst"

create_ns "$SRC_NS"
create_ns "$RTR_NS"
create_ns "$DST_NS"

create_veth_pair "$SRC_NS" eth0 10.0.1.2/24 "$RTR_NS" r-src 10.0.1.1/24
create_veth_pair "$RTR_NS" r-dst 10.0.2.1/24 "$DST_NS" eth0 10.0.2.2/24

ip -n "$SRC_NS" route add default via 10.0.1.1
ip -n "$DST_NS" route add default via 10.0.2.1
ip -n "$RTR_NS" link set r-dst mtu 1200
ip -n "$DST_NS" link set eth0 mtu 1200
ip netns exec "$RTR_NS" sysctl -qw net.ipv4.ip_forward=1

cat >"$WORKDIR/forcefrag.nft" <<'EOF'
table ip routerd_forcefrag {
  chain prerouting {
    type filter hook prerouting priority mangle; policy accept;
    iifname "r-src" fib daddr oifname "r-dst" ip length > 1200 ip frag-off 0x4000 ip frag-off set 0
  }
}
EOF

ip netns exec "$RTR_NS" nft -c -f "$WORKDIR/forcefrag.nft"
ip netns exec "$RTR_NS" nft -f "$WORKDIR/forcefrag.nft"

ip netns exec "$SRC_NS" ping -4 -M "do" -s 1300 -c 1 -W 2 10.0.2.2 >/dev/null

log "PASS: oversized IPv4 DF packet crossed low-MTU forwarded path after routerd_forcefrag cleared DF"
