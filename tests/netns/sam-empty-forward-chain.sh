#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause
set -euo pipefail

TEST_NAME="sam-empty-forward-chain"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"
require_common
require_cmd iptables
require_cmd nft

PROBE="${ROUTERD_SAM_FORWARD_PROBE:-$WORKDIR/sam-forward-probe}"
if [[ -z "${ROUTERD_SAM_FORWARD_PROBE:-}" ]]; then
  require_cmd go
  (cd "$REPO_ROOT" && CGO_ENABLED=0 go build -o "$PROBE" ./tests/netns/sam-forward-probe)
fi
[[ -x "$PROBE" ]] || fail "missing test driver: $PROBE"

NS="${TEST_ID}-router"
create_ns "$NS"
ipt() { ip netns exec "$NS" iptables "$@"; }
nft_ns() { ip netns exec "$NS" nft "$@"; }
probe() { ip netns exec "$NS" "$PROBE"; }

log "$(iptables --version)"
ipt -N unrelated
ipt -A unrelated -s 192.0.2.1/32 -j ACCEPT
before="$(ipt -S)"
# Ubuntu 22.04 iptables-nft 1.8.7 reports an absent named chain as
# "incompatible". The real SAM controller must leave this fresh state alone.
probe
probe
[[ "$(ipt -S)" == "$before" ]] || fail "empty reconcile changed the firewall"
log "PASS: absent SAM chain is a read-only no-op"

if iptables --version | grep -q nf_tables; then
  # A genuinely incompatible existing chain must still fail without mutation.
  nft_ns add chain ip filter routerd_sam_forward
  nft_ns add rule ip filter routerd_sam_forward ct state established accept
  if ipt -S routerd_sam_forward >"$WORKDIR/incompatible.log" 2>&1; then
    fail "fixture did not produce an incompatible chain"
  fi
  grep -q incompatible "$WORKDIR/incompatible.log" || fail "unexpected probe failure"
  before_native="$(nft_ns -j list chain ip filter routerd_sam_forward)"
  if probe >"$WORKDIR/controller-error.log" 2>&1; then
    fail "controller hid a genuinely incompatible chain"
  fi
  [[ "$(nft_ns -j list chain ip filter routerd_sam_forward)" == "$before_native" ]] || fail "incompatible chain was mutated"
  nft_ns flush chain ip filter routerd_sam_forward
  nft_ns delete chain ip filter routerd_sam_forward
  log "PASS: incompatible existing chain remains an error and is preserved"
fi

# The empty desired set must still remove old owned rules and accept_local.
ip -n "$NS" link add cap0 type dummy
ip -n "$NS" link add tun0 type dummy
ip netns exec "$NS" sysctl -qw net.ipv4.conf.cap0.accept_local=1 net.ipv4.conf.tun0.accept_local=1
ipt -N routerd_sam_forward
ipt -I FORWARD 1 -j routerd_sam_forward
ipt -A routerd_sam_forward -i cap0 -o tun0 -d 192.0.2.10/32 -j ACCEPT
ipt -A routerd_sam_forward -i tun0 -o cap0 -s 192.0.2.10/32 -j ACCEPT
foreign_before="$(ipt -S unrelated)"
probe
probe
[[ "$(ipt -S routerd_sam_forward)" == '-N routerd_sam_forward' ]] || fail "stale rules remain"
for interface in cap0 tun0; do
  [[ "$(ip netns exec "$NS" sysctl -n "net.ipv4.conf.$interface.accept_local")" == 0 ]] || fail "stale accept_local remains"
done
[[ "$(ipt -S unrelated)" == "$foreign_before" ]] || fail "unrelated rules changed"
log "PASS: owned stale rules and accept_local cleaned, unrelated rules preserved"
