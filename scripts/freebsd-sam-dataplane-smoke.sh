#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# FreeBSD amd64-only SAM acceptance.  The guest is a real FreeBSD VM; this
# script creates disposable VNET router-a/router-b/client/overlay stacks.
# It deliberately uses no Linux namespace, mock, arm64, or TCG substitute.
set -eu

routerd=''
evidence=''
while [ "$#" -gt 0 ]; do
  case "$1" in
    --routerd) routerd=${2:?}; shift 2 ;;
    --evidence-dir) evidence=${2:?}; shift 2 ;;
    *) echo "usage: $0 --routerd /absolute/routerd --evidence-dir /absolute/dir" >&2; exit 2 ;;
  esac
done
[ "$(uname -s)" = FreeBSD ] && [ "$(id -u)" -eq 0 ] || exit 2
[ -x "$routerd" ] && [ -n "$evidence" ] || exit 2
case "$routerd:$evidence" in /*:/*) ;; *) exit 2;; esac
for x in ifconfig jail jexec kldload kldstat pfctl tcpdump ping arp; do command -v "$x" >/dev/null; done
mkdir -p "$evidence"
tag="sam-$$"; work=$(mktemp -d /tmp/routerd-sam-vnet.XXXXXX)
ra="$tag-ra"; rb="$tag-rb"; client="$tag-client"; overlay="$tag-overlay"
ra_if=''
rb_if=''
client_if=''
ra_outer=''
rb_outer=''
bridge_created=''
ra_pid=''
rb_pid=''
arp_capture=''
carp_capture=''
stage='setup'
cleanup() {
  rc=$?; set +e
  [ -n "$ra_pid" ] && kill "$ra_pid" 2>/dev/null
  [ -n "$rb_pid" ] && kill "$rb_pid" 2>/dev/null
  [ -n "$arp_capture" ] && kill "$arp_capture" 2>/dev/null
  [ -n "$carp_capture" ] && kill "$carp_capture" 2>/dev/null
  [ -n "$ra_pid" ] && wait "$ra_pid" 2>/dev/null
  [ -n "$rb_pid" ] && wait "$rb_pid" 2>/dev/null
  [ -n "$arp_capture" ] && wait "$arp_capture" 2>/dev/null
  [ -n "$carp_capture" ] && wait "$carp_capture" 2>/dev/null
  {
    jail -r "$ra"
    jail -r "$rb"
    jail -r "$client"
    jail -r "$overlay"
  } >>"$evidence/cleanup.log" 2>&1
  [ -n "$bridge_created" ] && ifconfig "$bridge_created" destroy >>"$evidence/cleanup.log" 2>&1
  for i in "$ra_if" "$rb_if" "$client_if" "$ra_outer" "$rb_outer"; do [ -n "$i" ] && ifconfig "$i" destroy >>"$evidence/cleanup.log" 2>&1; done
  if [ "$rc" -ne 0 ]; then
    printf 'freebsd-sam-dataplane-failed-stage=%s rc=%s\n' "$stage" "$rc" >&2
    for log in \
      "$evidence/router-a-validate.log" "$evidence/router-b-validate.log" \
      "$evidence/router-a.log" "$evidence/router-b.log" \
      "$evidence/router-a-arp.log" "$evidence/router-b-arp.log" "$evidence/router-b-backup.log" \
      "$evidence/router-a-carp.log" "$evidence/router-b-carp.log" \
      "$evidence/client-arp.log" \
      "$evidence/router-a-pf-bootstrap.log" "$evidence/router-b-pf-bootstrap.log" \
      "$evidence/router-a-status.json" "$evidence/router-b-status.json" \
      "$evidence/bridge-carp.log" "$evidence/bridge-ifconfig.log" \
      "$evidence/router-a-pf-main.log" "$evidence/router-a-pf-anchor.log" \
      "$evidence/client-ping.log" "$evidence/collision-status.log"; do
      [ -s "$log" ] || continue
      printf '%s\n' "--- ${log##*/} ---" >&2
      tail -80 "$log" >&2
    done
  fi
  rm -rf "$work"
  exit "$rc"
}
trap cleanup EXIT HUP INT TERM

kldstat -q -m pf || kldload pf
kldstat -q -m carp || kldload carp
stage='vnet-topology'
jail -c name="$ra" vnet persist; jail -c name="$rb" vnet persist
jail -c name="$client" vnet persist; jail -c name="$overlay" vnet persist

# One L2 bridge is unnecessary: an epair fanout is enough for the first
# native production pass, and keeps client ARP capture on the real client NIC.
ra_if=$(ifconfig epair create); ra_b=${ra_if%a}b
rb_if=$(ifconfig epair create); rb_b=${rb_if%a}b
client_if=$(ifconfig epair create); client_b=${client_if%a}b
ra_outer=$(ifconfig epair create); ra_outer_b=${ra_outer%a}b
rb_outer=$(ifconfig epair create); rb_outer_b=${rb_outer%a}b
ifconfig "$ra_b" vnet "$ra"; ifconfig "$rb_b" vnet "$rb"; ifconfig "$client_b" vnet "$client"
# Each router/overlay epair must have both endpoints in the intended VNETs:
# endpoint a is the router interface named in the production config; endpoint
# b is its remote peer in the overlay VNET.  Keeping a on the host makes the
# later jexec ifconfig target nonexistent.
ifconfig "$ra_outer" vnet "$ra"; ifconfig "$ra_outer_b" vnet "$overlay"
ifconfig "$rb_outer" vnet "$rb"; ifconfig "$rb_outer_b" vnet "$overlay"
# Host bridge gives the two routers and client one real Ethernet collision domain.
br=$(ifconfig bridge create); bridge_created=$br
ifconfig "$ra_if" up; ifconfig "$rb_if" up; ifconfig "$client_if" up
ifconfig "$br" addm "$ra_if" addm "$rb_if" addm "$client_if" up
for j in "$ra" "$rb" "$client" "$overlay"; do jexec "$j" ifconfig lo0 127.0.0.1/8 up; done
jexec "$ra" ifconfig "$ra_b" inet 198.18.250.11/24 up
jexec "$rb" ifconfig "$rb_b" inet 198.18.250.12/24 up
jexec "$client" ifconfig "$client_b" inet 198.18.250.20/24 up
jexec "$ra" ifconfig "$ra_outer" inet 198.18.251.1/30 up
jexec "$overlay" ifconfig "$ra_outer_b" inet 198.18.251.2/30 up
jexec "$rb" ifconfig "$rb_outer" inet 198.18.251.5/30 up
jexec "$overlay" ifconfig "$rb_outer_b" inet 198.18.251.6/30 up

# The remote stack is the real return endpoint.  gif is configured manually
# only there; router-a/b get their production gif from routerd.
jexec "$overlay" ifconfig gif0 create
jexec "$overlay" ifconfig gif0 tunnel 198.18.251.2 198.18.251.1
jexec "$overlay" ifconfig gif0 inet 10.254.250.2 10.254.250.1 netmask 255.255.255.252 up
jexec "$overlay" ifconfig lo0 alias 198.18.250.99/32

# PF anchors are only effective when the active main ruleset calls them.
# Install the explicit operator-side anchor call in each isolated router VNET
# before routerd starts; routerd itself owns only the anchor's rules.
if ! jexec "$ra" pfctl -e >"$evidence/router-a-pf-bootstrap.log" 2>&1; then
  jexec "$ra" pfctl -s info >>"$evidence/router-a-pf-bootstrap.log" 2>&1
  grep -q 'Status: Enabled' "$evidence/router-a-pf-bootstrap.log"
fi
jexec "$ra" pfctl -f - >>"$evidence/router-a-pf-bootstrap.log" 2>&1 <<'EOF_PF'
anchor "routerd_sam_forward"
EOF_PF
if ! jexec "$rb" pfctl -e >"$evidence/router-b-pf-bootstrap.log" 2>&1; then
  jexec "$rb" pfctl -s info >>"$evidence/router-b-pf-bootstrap.log" 2>&1
  grep -q 'Status: Enabled' "$evidence/router-b-pf-bootstrap.log"
fi
jexec "$rb" pfctl -f - >>"$evidence/router-b-pf-bootstrap.log" 2>&1 <<'EOF_PF'
anchor "routerd_sam_forward"
EOF_PF

write_config() {
  jail_name=$1 lan=$2 outer=$3 outer_address=$4 priority=$5 out=$6
  cat >"$out" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-sam-$jail_name}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: Interface
    metadata: {name: lan}
    spec: {ifname: $lan, managed: false}
  - apiVersion: net.routerd.net/v1alpha1
    kind: Interface
    metadata: {name: outer}
    spec: {ifname: $outer, managed: false}
  - apiVersion: hybrid.routerd.net/v1alpha1
    kind: TunnelInterface
    metadata: {name: gif0}
    spec: {mode: ipip, local: $outer_address, remote: 198.18.251.2, trustedUnderlay: true, address: 10.254.250.1/30, peerAddress: 10.254.250.2}
  - apiVersion: net.routerd.net/v1alpha1
    kind: IPv4Route
    metadata:
      name: sam-forward-local
      annotations: {mobility.routerd.net/source: bgp-local-inventory}
    spec: {destination: 198.18.250.99/32, device: $lan}
  - apiVersion: net.routerd.net/v1alpha1
    kind: VirtualAddress
    metadata: {name: sam-vip}
    spec:
      family: ipv4
      interface: lan
      address: 198.18.250.254/32
      mode: vrrp
      vrrp: {virtualRouterID: 250, priority: $priority, authentication: sam-vnet-secret}
  - apiVersion: hybrid.routerd.net/v1alpha1
    kind: OverlayPeer
    metadata: {name: overlay}
    spec: {role: cloud, nodeID: overlay, underlay: {type: ipip, interface: gif0}}
  - apiVersion: hybrid.routerd.net/v1alpha1
    kind: AddressMobilityDomain
    metadata: {name: sam-net}
    spec: {prefix: 198.18.250.0/24, mode: selective-address, peerRef: overlay}
  - apiVersion: hybrid.routerd.net/v1alpha1
    kind: RemoteAddressClaim
    metadata: {name: published-host}
    spec:
      domainRef: sam-net
      address: 198.18.250.99/32
      ownerSide: cloud
      capture: {type: proxy-arp, interface: lan, gratuitousARP: true, activeWhen: {type: vrrp-master, virtualAddressRef: sam-vip}}
      delivery: {peerRef: overlay, mode: route, tunnelInterface: gif0}
EOF
}
write_config "$ra" "$ra_b" "$ra_outer" 198.18.251.1 151 "$work/ra.yaml"
write_config "$rb" "$rb_b" "$rb_outer" 198.18.251.5 100 "$work/rb.yaml"
ra_runtime=/tmp/routerd-sam-ra
rb_runtime=/tmp/routerd-sam-rb
rb_delete_runtime=/tmp/routerd-sam-rb-delete
rb_collision_runtime=/tmp/routerd-sam-rb-collision
jexec "$ra" mkdir -p "$ra_runtime"
jexec "$rb" mkdir -p "$rb_runtime" "$rb_delete_runtime" "$rb_collision_runtime"
cp "$work/ra.yaml" "$work/rb.yaml" "$evidence/"
stage='validate-config'
jexec "$ra" "$routerd" validate --config "$work/ra.yaml" >"$evidence/router-a-validate.log" 2>&1
jexec "$rb" "$routerd" validate --config "$work/rb.yaml" >"$evidence/router-b-validate.log" 2>&1

# Capture before either controller starts: this proves the three BPF GARPs are
# visible to a real client, rather than merely constructed in a unit test.
jexec "$client" timeout 20 tcpdump -n -l -i "$client_b" 'arp and host 198.18.250.99' >"$evidence/client-arp.log" 2>&1 & arp_capture=$!
# tcpdump reports readiness on its combined output. Do not start controllers
# until the client-side BPF capture is attached, or the three immediate GARPs
# can race past the observation point.
n=0
while [ "$n" -lt 10 ]; do
  grep -q 'listening on' "$evidence/client-arp.log" && break
  kill -0 "$arp_capture" 2>/dev/null || { wait "$arp_capture" 2>/dev/null || true; false; }
  n=$((n + 1))
  sleep 1
done
grep -q 'listening on' "$evidence/client-arp.log"
# CARP is protocol 112; `-T carp` is the FreeBSD carp(4)-documented decoder.
# This is retained on failure to distinguish a topology/configuration mismatch
# from an unobserved state transition.
timeout 20 tcpdump -n -l -T carp -i "$br" 'ip proto 112' >"$evidence/bridge-carp.log" 2>&1 & carp_capture=$!
stage='start-controllers'
jexec "$ra" "$routerd" serve --config "$work/ra.yaml" --state-file "$ra_runtime/state.db" --status-file "$ra_runtime/status.json" --socket "$ra_runtime/api.sock" --status-socket "$ra_runtime/status.sock" --controllers all >"$evidence/router-a.log" 2>&1 & ra_pid=$!
jexec "$rb" "$routerd" serve --config "$work/rb.yaml" --state-file "$rb_runtime/state.db" --status-file "$rb_runtime/status.json" --socket "$rb_runtime/api.sock" --status-socket "$rb_runtime/status.sock" --controllers all >"$evidence/router-b.log" 2>&1 & rb_pid=$!

wait_for() { jail_name=$1 command=$2 file=$3; n=0; while [ "$n" -lt 45 ]; do if jexec "$jail_name" sh -c "$command" >"$file" 2>&1; then return 0; fi; n=$((n+1)); sleep 1; done; return 1; }
wait_published_arp() { jail_name=$1 address=$2 file=$3; n=0; while [ "$n" -lt 45 ]; do jexec "$jail_name" arp -n "$address" >"$file" 2>&1 || true; if grep -q published "$file"; then return 0; fi; n=$((n+1)); sleep 1; done; return 1; }
wait_carp_state() { jail_name=$1 ifname=$2 state_name=$3 file=$4; n=0; while [ "$n" -lt 45 ]; do jexec "$jail_name" ifconfig "$ifname" >"$file" 2>&1 || return 1; if grep -Eq "carp:[[:space:]]+${state_name}([[:space:]]|$)" "$file"; then return 0; fi; n=$((n+1)); sleep 1; done; return 1; }
snapshot_carp_state() {
  jexec "$ra" ifconfig "$ra_b" >"$evidence/router-a-carp.log" 2>&1 || true
  jexec "$rb" ifconfig "$rb_b" >"$evidence/router-b-carp.log" 2>&1 || true
  jexec "$ra" cat "$ra_runtime/status.json" >"$evidence/router-a-status.json" 2>&1 || true
  jexec "$rb" cat "$rb_runtime/status.json" >"$evidence/router-b-status.json" 2>&1 || true
  ifconfig "$br" >"$evidence/bridge-ifconfig.log" 2>&1 || true
  if [ -n "$carp_capture" ]; then kill "$carp_capture" 2>/dev/null || true; wait "$carp_capture" 2>/dev/null || true; carp_capture=''; fi
}
wait_published_arp "$ra" 198.18.250.99 "$evidence/router-a-arp.log"
stage='carp-master-arp'
if ! wait_carp_state "$ra" "$ra_b" MASTER "$evidence/router-a-carp.log"; then snapshot_carp_state; exit 1; fi
if ! wait_carp_state "$rb" "$rb_b" BACKUP "$evidence/router-b-backup.log"; then snapshot_carp_state; exit 1; fi
snapshot_carp_state
wait "$arp_capture" || true; arp_capture=
stage='garp-observation'
grep -c '198\.18\.250\.99' "$evidence/client-arp.log" | awk '$1 >= 3 {exit 0} {exit 1}'
grep -F 'published' "$evidence/router-a-arp.log"
if jexec "$rb" arp -n 198.18.250.99 >"$evidence/router-b-arp.log" 2>&1 && grep -q published "$evidence/router-b-arp.log"; then echo 'CARP backup published ARP' >&2; exit 1; fi
printf 'sam-client-observed-three-garps=ok\n' >"$evidence/summary.log"
printf 'sam-carp-backup-silent-master-published=ok\n' >>"$evidence/summary.log"

# PF reachability/rules and real client-to-overlay request/return path.
stage='pf-overlay-dataplane'
jexec "$ra" pfctl -sr >"$evidence/router-a-pf-main.log"
jexec "$ra" pfctl -a routerd_sam_forward -sr >"$evidence/router-a-pf-anchor.log"
grep -F 'routerd_sam_forward' "$evidence/router-a-pf-main.log"
grep -F '198.18.250.99/32' "$evidence/router-a-pf-anchor.log"
jexec "$client" ping -n -S 198.18.250.20 -c 3 -W 1 198.18.250.99 >"$evidence/client-ping.log"
printf 'sam-pf-32-overlay-return=ok\n' >>"$evidence/summary.log"

# Force the master off the LAN.  Management/controller process stays alive;
# router-b must become master and take over the one published entry.
jexec "$ra" ifconfig "$ra_b" down
stage='carp-failover'
wait_published_arp "$rb" 198.18.250.99 "$evidence/router-b-arp-after-failover.log"
jexec "$client" ping -n -S 198.18.250.20 -c 3 -W 1 198.18.250.99 >"$evidence/client-ping-after-failover.log"
printf 'sam-carp-forced-switchover-converged=ok\n' >>"$evidence/summary.log"

# Delete desired state from current master and require owned ARP/PF cleanup.
stage='owned-delete-cleanup'
kill "$rb_pid"; wait "$rb_pid" || true; rb_pid=
sed '/kind: RemoteAddressClaim/,$d' "$work/rb.yaml" >"$work/rb-delete.yaml"
jexec "$rb" "$routerd" serve --config "$work/rb-delete.yaml" --state-file "$rb_delete_runtime/state.db" --status-file "$rb_delete_runtime/status.json" --socket "$rb_delete_runtime/api.sock" --status-socket "$rb_delete_runtime/status.sock" --controllers sam >"$evidence/router-b-delete.log" 2>&1 & rb_pid=$!
wait_for "$rb" "! arp -n 198.18.250.99 | grep -q published" "$evidence/router-b-owned-cleanup.log"
jexec "$rb" pfctl -a routerd_sam_forward -sr >"$evidence/router-b-pf-cleanup.log"
[ ! -s "$evidence/router-b-pf-cleanup.log" ]
printf 'sam-owned-arp-pf-delete-cleanup=ok\n' >>"$evidence/summary.log"

# Foreign state outside routerd's named anchor remains untouched; a local OS
# address collision must fail closed with controller status evidence.
jexec "$rb" pfctl -a operator_sam -f - <<'EOF_PF'
pass in quick inet from any to 198.18.250.200/32
EOF_PF
jexec "$rb" ifconfig "$rb_b" alias 198.18.250.99/32
stage='collision-foreign-preservation'
kill "$rb_pid"; wait "$rb_pid" || true; rb_pid=
jexec "$rb" "$routerd" serve --config "$work/rb.yaml" --state-file "$rb_collision_runtime/state.db" --status-file "$rb_collision_runtime/status.json" --socket "$rb_collision_runtime/api.sock" --status-socket "$rb_collision_runtime/status.sock" --controllers sam >"$evidence/router-b-collision.log" 2>&1 & rb_pid=$!
wait_for "$rb" "grep -q 'foreign OS address' $rb_collision_runtime/status.json" "$evidence/collision-status.log"
jexec "$rb" pfctl -a operator_sam -sr >"$evidence/foreign-pf-preserved.log"
grep -F '198.18.250.200/32' "$evidence/foreign-pf-preserved.log"
jexec "$rb" ifconfig "$rb_b" inet 198.18.250.99/32 -alias
printf 'sam-collision-fail-closed-status=ok\n' >>"$evidence/summary.log"
printf 'sam-foreign-arp-pf-preservation=ok\n' >>"$evidence/summary.log"
printf 'freebsd-sam-dataplane=ok\n' >"$evidence/result"
