#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause

# Opt-in native acceptance for the production TunnelInterface controller.
# The production endpoint runs in r1; r2 is a separate VNET peer connected by
# one epair. Reciprocal GIF endpoints in one kernel stack are not a valid
# dataplane oracle because inner and outer routes collide locally.
set -eu
LC_ALL=C
export LC_ALL
# Keep diagnostics visible even if a failing compound command redirected output.
exec 3>&1
stage=prepare-native
operation=preflight

routerd=
while [ "$#" -gt 0 ]; do
  case "$1" in
  --routerd)
    routerd=${2:?missing routerd path}
    shift 2
    ;;
  *)
    echo "usage: $0 --routerd PATH" >&2
    exit 64
    ;;
  esac
done
[ -x "$routerd" ] || {
  echo "routerd binary is required" >&2
  exit 64
}
[ "$(uname -s)" = FreeBSD ] || exit 1
command -v jq >/dev/null

work=$(mktemp -d /tmp/routerd-tunnelinterface-smoke.XXXXXX)
state="$work/state.db"
ledger="$work/ledger.json"
outer_a=198.18.89.1
outer_b=198.18.89.2
r1="routerd-tunnel-r1-$$"
r2="routerd-tunnel-r2-$$"
epair_a=
epair_b=
own_epair_module=0
own_gre_module=0
capture_pid=
own_r1=0
own_r2=0
r1_jid=
r2_jid=
epair_valid=0
body_ok=0
module_inventory_ready=0

set_stage() {
  stage=$1
  operation=enter
  printf 'freebsd-tunnelinterface-stage=%s=begin\n' "$stage" >&3
}

# Called by cleanup through the EXIT trap.
# shellcheck disable=SC2317
emit_initial_failure() {
	for evidence in \
		failure-native.log failure-config.yaml failure-objects.json \
		underlay.ping \
		apply-initial.log gif0.add gif0.initial.status gif.ping gif.proto4 gif.outer.before gif.outer.after gre0.add gre0.initial.status gre.ping gre.proto47 \
		apply-second.log gif0.second.status gre0.second.status \
		child-peer-setup.log apply-child-acquire.log child-acquire.receipts.json gif0.child-acquire.ifconfig gre0.child-acquire.ifconfig gif.child-acquire.route gre.child-acquire.route gif.child-acquire.ping gre.child-acquire.ping \
		apply-child-parent-noop.log child-parent-noop.receipts.json gif0.child-parent-noop.ifconfig gre0.child-parent-noop.ifconfig gif.child-parent-noop.route gre.child-parent-noop.route gif.child-parent-noop.ping gre.child-parent-noop.ping \
		apply-child-remove.log child-remove.receipts.json child-remove.routes gif0.child-remove.ifconfig gre0.child-remove.ifconfig gif.child-remove.route gre.child-remove.route \
		apply-change.log gif0.change gre0.change \
		apply-gre-key-zero.log gre0.key-zero.status gre.key-zero.ping gre.key-zero.proto47 \
		apply-restart.log gif0.restart.status gre0.restart.status gre.rekey.ping gre.rekey.proto47 \
		apply-remove.log \
		apply-foreign.log gif0.foreign.before gif0.foreign.status gif0.foreign.after \
		apply-foreign-stale.log gif0.foreign.stale.after; do
		path="$work/$evidence"
		[ -f "$path" ] || continue
		echo "--- tunnelinterface $evidence" >&2
		sed -n '1,160p' "$path" >&2
	done
}

# Helpers run only through the EXIT trap cleanup path.
# shellcheck disable=SC2317
jail_identity() {
  awk -v wanted_id="jid=$1" -v wanted_name="name=$2" '
    { id=0; name=0; for (i=1;i<=NF;i++) { if ($i==wanted_id) id=1; if ($i==wanted_name) name=1 }; if (id && name) found=1 }
    END { exit !found }' "$3"
}
# Called by the EXIT trap cleanup chain.
# shellcheck disable=SC2317
jail_remaining() {
  awk -v wanted_id="jid=$1" -v wanted_name="name=$2" '
    { for (i=1;i<=NF;i++) if ($i==wanted_id || $i==wanted_name) found=1 }
    END { exit !found }' "$3"
}
# Called by the EXIT trap cleanup chain.
# shellcheck disable=SC2317
remove_owned_jail() {
  if [ -z "$1" ]; then cleanup_bad=1; return; fi
  if ! timeout 5 jls -n jid name >"$work/cleanup-jails-$1-before.txt" 2>>"$work/cleanup.log"; then
    cleanup_bad=1
  elif jail_identity "$1" "$2" "$work/cleanup-jails-$1-before.txt"; then
    if ! timeout 10 jail -r "$1" >>"$work/cleanup.log" 2>&1; then cleanup_bad=1; fi
  elif jail_remaining "$1" "$2" "$work/cleanup-jails-$1-before.txt"; then
    echo "owned jail identity changed: $1 $2" >>"$work/cleanup.log"
    cleanup_bad=1
  fi
}

# shellcheck disable=SC2317
cleanup() {
  rc=$?
  trap - EXIT HUP INT TERM
  cleanup_bad=0
  if [ -n "$capture_pid" ]; then
    kill "$capture_pid" >/dev/null 2>&1 || true
    capture_exit=0
    wait "$capture_pid" >/dev/null 2>&1 || capture_exit=$?
    case "$capture_exit" in 0|124|137|143) ;; *) cleanup_bad=1 ;; esac
  fi
  if [ "$rc" -ne 0 ]; then
    printf 'freebsd-tunnelinterface-failure stage=%s exit=%s operation=%s\n' "$stage" "$rc" "$operation" >&3
    {
      jls
      for failed_jail in "$r1" "$r2"; do
        if jls -j "$failed_jail" >/dev/null 2>&1; then
          printf '\n--- %s interfaces\n' "$failed_jail"
          jexec "$failed_jail" ifconfig -a
          printf '\n--- %s routes\n' "$failed_jail"
          jexec "$failed_jail" netstat -rn -f inet
        fi
      done
    } >"$work/failure-native.log" 2>&1 || true
    if [ -f "$work/router.yaml" ]; then cp "$work/router.yaml" "$work/failure-config.yaml" || true; fi
    if [ -f "$state" ]; then sqlite3 -json "$state" 'SELECT api_version,kind,name,status FROM objects;' >"$work/failure-objects.json" 2>&1 || true; fi
    emit_initial_failure >&3 2>&3 || true
  fi
  if [ "$own_r1" -eq 1 ]; then remove_owned_jail "$r1_jid" "$r1"; fi
  if [ "$own_r2" -eq 1 ]; then remove_owned_jail "$r2_jid" "$r2"; fi
  if ! timeout 5 jls -n jid name >"$work/cleanup-jails-after.txt" 2>>"$work/cleanup.log"; then
    cleanup_bad=1
  else
    if [ "$own_r1" -eq 1 ] && jail_remaining "$r1_jid" "$r1" "$work/cleanup-jails-after.txt"; then cleanup_bad=1; fi
    if [ "$own_r2" -eq 1 ] && jail_remaining "$r2_jid" "$r2" "$work/cleanup-jails-after.txt"; then cleanup_bad=1; fi
  fi
  if [ "$epair_valid" -eq 1 ]; then
    for owned_iface in "$epair_a" "$epair_b"; do
      if ! timeout 5 ifconfig -l >"$work/cleanup-ifconfig-before.txt" 2>>"$work/cleanup.log"; then
        cleanup_bad=1
      elif ! tr '[:space:]' '\n' <"$work/cleanup-ifconfig-before.txt" | grep -Fxq lo0; then
        cleanup_bad=1
      elif tr '[:space:]' '\n' <"$work/cleanup-ifconfig-before.txt" | grep -Fxq "$owned_iface"; then
        if ! timeout 10 ifconfig "$owned_iface" destroy >>"$work/cleanup.log" 2>&1; then cleanup_bad=1; fi
      fi
    done
    if ! timeout 5 ifconfig -l >"$work/cleanup-ifconfig-after.txt" 2>>"$work/cleanup.log"; then
      cleanup_bad=1
    elif ! tr '[:space:]' '\n' <"$work/cleanup-ifconfig-after.txt" | grep -Fxq lo0; then
      cleanup_bad=1
    else
      for owned_iface in "$epair_a" "$epair_b"; do
        if tr '[:space:]' '\n' <"$work/cleanup-ifconfig-after.txt" | grep -Fxq "$owned_iface"; then cleanup_bad=1; fi
      done
    fi
  fi
  if [ "$own_epair_module" -eq 1 ] && ! timeout 10 kldunload if_epair >>"$work/cleanup.log" 2>&1; then cleanup_bad=1; fi
  if [ "$own_gre_module" -eq 1 ] && ! timeout 10 kldunload if_gre >>"$work/cleanup.log" 2>&1; then cleanup_bad=1; fi
  if [ "$module_inventory_ready" -eq 1 ]; then
    if ! timeout 5 kldstat -v >"$work/modules-after.txt" 2>>"$work/cleanup.log"; then
      cleanup_bad=1
    elif ! grep -Eq '^Id[[:space:]]' "$work/modules-after.txt"; then
      cleanup_bad=1
    else
      for module in if_epair if_gre; do
        module_before=absent
        module_after=absent
        if grep -Eq "(^|[[:space:]])$module(\.ko)?([[:space:]]|$)" "$work/modules-before.txt"; then module_before=present; fi
        if grep -Eq "(^|[[:space:]])$module(\.ko)?([[:space:]]|$)" "$work/modules-after.txt"; then module_after=present; fi
        if [ "$module_before" != "$module_after" ]; then cleanup_bad=1; fi
      done
    fi
  fi
  if [ "$rc" -eq 0 ] && { [ "$cleanup_bad" -ne 0 ] || [ "$body_ok" -ne 1 ]; }; then rc=70; fi
  if [ "$rc" -eq 0 ]; then
    rm -rf "$work"
    echo 'freebsd-tunnelinterface=ok' >&3
  else
    printf '%s\n' "$cleanup_bad" >"$work/cleanup.exit"
    printf '%s\n' "$rc" >"$work/native.exit"
    printf 'freebsd-tunnelinterface-failure-evidence=%s cleanup-failed=%s\n' "$work" "$cleanup_bad" >&3
  fi
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

r1cmd() { operation="r1: $*"; jexec "$r1" "$@"; }
r2cmd() { operation="r2: $*"; jexec "$r2" "$@"; }

for tool in timeout jail jexec jls ifconfig kldstat kldload kldunload; do command -v "$tool" >/dev/null; done
kldstat -v >"$work/modules-before.txt"
grep -Eq '^Id[[:space:]]' "$work/modules-before.txt"
module_inventory_ready=1

if ! kldstat -q -m if_epair; then
	kldload if_epair
	own_epair_module=1
fi
epair_a=$(ifconfig epair create)
case "$epair_a" in
  epair*a) epair_number=${epair_a#epair}; epair_number=${epair_number%a} ;;
  *) echo 'unexpected epair identity' >&2; exit 65 ;;
esac
case "$epair_number" in ''|*[!0-9]*) echo 'nonnumeric epair identity' >&2; exit 65 ;; esac
epair_valid=1
epair_b="${epair_a%a}b"
jail -c name="$r1" path=/ host.hostname="$r1" persist vnet allow.raw_sockets=1 \
	vnet.interface="$epair_a"
own_r1=1
r1_jid=$(timeout 5 jls -n -j "$r1" jid)
r1_jid=${r1_jid#jid=}
case "$r1_jid" in ''|*[!0-9]*) exit 65 ;; esac
jail -c name="$r2" path=/ host.hostname="$r2" persist vnet allow.raw_sockets=1 \
	vnet.interface="$epair_b"
own_r2=1
r2_jid=$(timeout 5 jls -n -j "$r2" jid)
r2_jid=${r2_jid#jid=}
case "$r2_jid" in ''|*[!0-9]*) exit 65 ;; esac
r1cmd ifconfig lo0 up
r2cmd ifconfig lo0 up
r1cmd ifconfig "$epair_a" inet "$outer_a/30" up
r2cmd ifconfig "$epair_b" inet "$outer_b/30" up
r1cmd ping -n -c 1 "$outer_b" >"$work/underlay.ping" 2>&1

# VNET jails cannot autoload if_gre. Load it after both VNETs exist so its
# VNET constructor attaches to r1, and unload only when this fixture owns it.
if ! kldstat -q -m if_gre; then
	kldload if_gre
	own_gre_module=1
fi

for ifname in gif0 gre0; do
	if r1cmd ifconfig "$ifname" >/dev/null 2>&1; then
		echo "fixture requires absent r1 $ifname" >&2
		exit 75
	fi
done
r2cmd ifconfig gif0 create
r2cmd ifconfig gif0 tunnel "$outer_b" "$outer_a"
r2cmd ifconfig gif0 inet 10.253.89.2/30 10.253.89.1
r2cmd ifconfig gif0 up
r2cmd ifconfig gre0 create
r2cmd ifconfig gre0 tunnel "$outer_b" "$outer_a"
r2cmd ifconfig gre0 grekey 42
r2cmd ifconfig gre0 inet 10.253.90.2/30 10.253.90.1
r2cmd ifconfig gre0 up

write_config() {
  mtu=$1
  key=${2:-42}
  cat >"$work/router.yaml" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-tunnelinterface-smoke}
spec:
  resources:
  - apiVersion: hybrid.routerd.net/v1alpha1
    kind: TunnelInterface
    metadata: {name: gif0}
    spec: {mode: ipip, local: $outer_a, remote: $outer_b, address: 10.253.89.1/30, peerAddress: 10.253.89.2, mtu: $mtu, trustedUnderlay: true}
  - apiVersion: hybrid.routerd.net/v1alpha1
    kind: TunnelInterface
    metadata: {name: gre0}
    spec: {mode: gre, local: $outer_a, remote: $outer_b, address: 10.253.90.1/30, peerAddress: 10.253.90.2, mtu: $mtu, key: $key, trustedUnderlay: true}
EOF
}

apply_once() {
	label=$1
	controllers=${2:-tunnel}
  r1cmd "$routerd" serve --once --controllers "$controllers" --config "$work/router.yaml" \
    --state-file "$state" --ledger-file "$ledger" \
    --status-file "$work/status-$label.json" --socket "$work/api-$label.sock" \
    --status-socket "$work/status-$label.sock" >"$work/apply-$label.log" 2>&1
}

status_row() {
	name=$1
	target=$2
	command -v sqlite3 >/dev/null 2>&1 || {
		echo "sqlite3 is required for TunnelInterface status evidence" >&2
		return 127
	}
	sqlite3 "$state" "SELECT status FROM objects WHERE api_version='hybrid.routerd.net/v1alpha1' AND kind='TunnelInterface' AND name='$name';" >"$target"
	[ -s "$target" ]
}

set_stage initial
write_config 1400
apply_once initial
r1cmd ifconfig gif0 >"$work/gif0.add"
r1cmd ifconfig gre0 >"$work/gre0.add"
status_row gre0 "$work/gre0.initial.status"
status_row gif0 "$work/gif0.initial.status"
grep -F "tunnel inet $outer_a --> $outer_b" "$work/gif0.add"
# FreeBSD 14.3's plain ifconfig output is not a stable GRE-key query surface.
# The initial persisted status records the requested key; the immediately
# following controller restart/no-op assertion is the kernel-observation
# oracle and must fail if a later reconcile cannot observe key 42.
jq -e '.phase == "Up" and .key == 42 and .address == "10.253.90.1/30" and .peerAddress == "10.253.90.2" and .observedAddress == "10.253.90.1/30" and .observedPeerAddress == "10.253.90.2" and .interfaceOwned == true' "$work/gre0.initial.status" >/dev/null
jq -e '.phase == "Up" and .address == "10.253.89.1/30" and .peerAddress == "10.253.89.2" and .observedAddress == "10.253.89.1/30" and .observedPeerAddress == "10.253.89.2" and .interfaceOwned == true' "$work/gif0.initial.status" >/dev/null
if command -v tcpdump >/dev/null 2>&1; then
  # epair_a is r1's outer interface, so its VNET sees the IPIP frames emitted
  # by production gif0 before they cross to r2.
  timeout 10 jexec "$r1" tcpdump -n -c 1 -i "$epair_a" 'ip proto 4' >"$work/gif.proto4" 2>&1 &
  capture_pid=$!
  sleep 1
else
  # tcpdump is optional in the native image. epair byte counters are the
  # bounded fallback proof that the GIF ping emitted outer traffic.
  r1cmd netstat -I "$epair_a" -b >"$work/gif.outer.before"
fi
if ! r1cmd ping -n -c 3 -S 10.253.89.1 10.253.89.2 >"$work/gif.ping" 2>&1; then
	cat "$work/gif.ping" >&2
	exit 1
fi
grep -F '3 packets transmitted, 3 packets received' "$work/gif.ping"
if [ -n "$capture_pid" ]; then
  wait "$capture_pid"
  capture_pid=
  grep -Eq 'IP .* > .*: IP ' "$work/gif.proto4"
else
  r1cmd netstat -I "$epair_a" -b >"$work/gif.outer.after"
  if cmp -s "$work/gif.outer.before" "$work/gif.outer.after"; then
    echo 'GIF ping did not advance epair outer counters' >&2
    exit 1
  fi
fi
capture_pid=
timeout 10 jexec "$r1" tcpdump -n -v -c 1 -i "$epair_a" "ip proto 47 and src host $outer_a" >"$work/gre.proto47" 2>&1 &
capture_pid=$!
sleep 1
if ! r1cmd ping -n -c 3 -S 10.253.90.1 10.253.90.2 >"$work/gre.ping" 2>&1; then
	cat "$work/gre.ping" >&2
	exit 1
fi
grep -F '3 packets transmitted, 3 packets received' "$work/gre.ping"
wait "$capture_pid"
capture_pid=
# releng/14.3 contrib/tcpdump/print-gre.c prints this when GRE_KP is set.
grep -F "$outer_a > $outer_b: GREv0" "$work/gre.proto47"
grep -F 'key=0x2a' "$work/gre.proto47"
echo 'freebsd-tunnelinterface-stage=initial-dataplane=ok'

# A new serve --once process using the persisted state is a controller restart;
# it must be a no-op, not an adoption of a different kernel object.
set_stage second-noop
apply_once second
status_row gif0 "$work/gif0.second.status"
status_row gre0 "$work/gre0.second.status"
jq -e '.phase == "Up" and .reason == "AlreadyConfigured" and .interfaceOwned == true' "$work/gif0.second.status" >/dev/null
jq -e '.phase == "Up" and .reason == "AlreadyConfigured" and .key == 42 and .interfaceOwned == true' "$work/gre0.second.status" >/dev/null
echo 'freebsd-tunnelinterface-stage=second-noop=ok'

# Acquire separate child aliases and routes through the production controllers.
# r2 owns independent destinations and explicit return routes for those aliases.
set_stage child-peer-setup
r2cmd ifconfig lo0 inet 192.0.2.2/32 alias >"$work/child-peer-setup.log" 2>&1
# Keep redirects scoped to each command, separate from EXIT-trap diagnostics.
# shellcheck disable=SC2129
r2cmd ifconfig lo0 inet 192.0.2.10/32 alias >>"$work/child-peer-setup.log" 2>&1
r2cmd route -n add -host 192.0.2.1 10.253.89.1 >>"$work/child-peer-setup.log" 2>&1
r2cmd route -n add -host 192.0.2.9 10.253.90.1 >>"$work/child-peer-setup.log" 2>&1
cat >>"$work/router.yaml" <<'EOF'
  - apiVersion: net.routerd.net/v1alpha1
    kind: IPv4StaticAddress
    metadata: {name: gif-child-address}
    spec: {interface: gif0, address: 192.0.2.1/32}
  - apiVersion: net.routerd.net/v1alpha1
    kind: IPv4StaticAddress
    metadata: {name: gre-child-address}
    spec: {interface: gre0, address: 192.0.2.9/32}
  - apiVersion: net.routerd.net/v1alpha1
    kind: IPv4Route
    metadata: {name: gif-child-route}
    spec: {destination: 192.0.2.2/32, gateway: 10.253.89.2, device: gif0}
  - apiVersion: net.routerd.net/v1alpha1
    kind: IPv4Route
    metadata: {name: gre-child-route}
    spec: {destination: 192.0.2.10/32, gateway: 10.253.90.2, device: gre0}
EOF

child_receipts() {
  operation="read $1 child ownership receipts from SQLite"
  sqlite3 "$state" "SELECT json_group_array(json_object('kind',kind,'name',name,'status',json(status))) FROM objects WHERE api_version='net.routerd.net/v1alpha1' AND name IN ('gif-child-address','gre-child-address','gif-child-route','gre-child-route');" >"$work/$1.receipts.json"
}

check_child_dataplane() {
  child_label=$1
  child_receipts "$child_label"
  operation="verify $child_label acknowledged child receipts"
  jq -e 'length == 4 and all(.[]; (.status.nativeReceipt // .status.routeOwner) as $receipt | $receipt.pending == false and $receipt.ifIndex > 0 and ($receipt.scope | length) > 0) and ([.[] | select(.kind == "IPv4StaticAddress" and .status.phase == "Applied")] | length) == 2 and ([.[] | select(.kind == "IPv4Route" and .status.phase == "Installed")] | length) == 2' "$work/$child_label.receipts.json" >/dev/null
  for child_mode in gif gre; do
    case "$child_mode" in
      gif) child_if=gif0; child_local=192.0.2.1; child_remote=192.0.2.2; child_gateway=10.253.89.2 ;;
      gre) child_if=gre0; child_local=192.0.2.9; child_remote=192.0.2.10; child_gateway=10.253.90.2 ;;
    esac
    r1cmd ifconfig "$child_if" >"$work/$child_if.$child_label.ifconfig"
    operation="verify $child_label $child_if child alias $child_local/32"
    awk -v address="$child_local" '$1 == "inet" && $2 == address && /netmask 0xffffffff/ { found=1 } END { exit !found }' "$work/$child_if.$child_label.ifconfig"
    r1cmd route -n get -host "$child_remote" >"$work/$child_mode.$child_label.route" 2>&1
    operation="verify $child_label $child_remote route via $child_gateway dev $child_if"
    awk -v destination="$child_remote" -v gateway="$child_gateway" -v device="$child_if" '$1 == "destination:" && $2 == destination { d=1 } $1 == "gateway:" && $2 == gateway { g=1 } $1 == "interface:" && $2 == device { i=1 } /flags:.*STATIC/ { s=1 } END { exit !(d && g && i && s) }' "$work/$child_mode.$child_label.route"
    r1cmd ping -n -c 3 -S "$child_local" "$child_remote" >"$work/$child_mode.$child_label.ping" 2>&1
    operation="verify $child_label $child_mode 3/3 ping replies"
    grep -F '3 packets transmitted, 3 packets received' "$work/$child_mode.$child_label.ping"
  done
}

set_stage child-acquire
apply_once child-acquire tunnel,ipv4-static-address,ipv4-route
check_child_dataplane child-acquire
echo 'freebsd-tunnelinterface-stage=child-acquire=ok'

# Reconcile only the parent owner. Existing child effects must survive unchanged.
set_stage child-parent-noop
apply_once child-parent-noop
check_child_dataplane child-parent-noop
echo 'freebsd-tunnelinterface-stage=child-parent-preservation=ok'

# Keep both parents declared and run only the child owners to withdraw children.
set_stage child-remove
write_config 1400
apply_once child-remove ipv4-route,ipv4-static-address
child_receipts child-remove
jq -e 'length == 0' "$work/child-remove.receipts.json" >/dev/null
r1cmd netstat -rn -f inet >"$work/child-remove.routes"
grep -F 'Internet:' "$work/child-remove.routes" >/dev/null
for child_mode in gif gre; do
  case "$child_mode" in
    gif) child_if=gif0; child_local=192.0.2.1; child_remote=192.0.2.2; parent_local=10.253.89.1 ;;
    gre) child_if=gre0; child_local=192.0.2.9; child_remote=192.0.2.10; parent_local=10.253.90.1 ;;
  esac
  r1cmd ifconfig "$child_if" >"$work/$child_if.child-remove.ifconfig"
  if awk -v address="$child_local" '$1 == "inet" && $2 == address { found=1 } END { exit !found }' "$work/$child_if.child-remove.ifconfig"; then
    echo "removed child address remains on $child_if" >&2
    exit 1
  fi
  awk -v address="$parent_local" '$1 == "inet" && $2 == address { found=1 } END { exit !found }' "$work/$child_if.child-remove.ifconfig"
  r1cmd route -n get -host "$child_remote" >"$work/$child_mode.child-remove.route" 2>&1 || true
  if awk -v destination="$child_remote" '$1 == destination || $1 == destination "/32" { found=1 } END { exit !found }' "$work/child-remove.routes"; then
    echo "removed child route remains in native inventory: $child_remote" >&2
    exit 1
  fi
done
echo 'freebsd-tunnelinterface-stage=child-owned-withdrawal=ok'

set_stage change
write_config 1300
apply_once change
r1cmd ifconfig gif0 >"$work/gif0.change"
r1cmd ifconfig gre0 >"$work/gre0.change"
grep -F 'mtu 1300' "$work/gif0.change"
grep -F 'mtu 1300' "$work/gre0.change"
echo 'freebsd-tunnelinterface-stage=change=ok'

# FreeBSD gre(4) defines key zero as disabling the outgoing key option. The
# releng/14.3 receive path does not enforce incoming keys, so verify the
# emitted r1 GRE header instead of treating peer reply delivery as a reject.
set_stage gre-key-zero
write_config 1300 0
apply_once gre-key-zero
status_row gre0 "$work/gre0.key-zero.status"
jq -e '.phase == "Up" and (.key | not) and .interfaceOwned == true' "$work/gre0.key-zero.status" >/dev/null
capture_pid=
timeout 10 jexec "$r1" tcpdump -n -v -c 1 -i "$epair_a" "ip proto 47 and src host $outer_a" >"$work/gre.key-zero.proto47" 2>&1 &
capture_pid=$!
sleep 1
r1cmd ping -n -c 1 -S 10.253.90.1 10.253.90.2 >"$work/gre.key-zero.ping" 2>&1
wait "$capture_pid"
capture_pid=
grep -F "$outer_a > $outer_b: GREv0" "$work/gre.key-zero.proto47"
if grep -F 'key=' "$work/gre.key-zero.proto47" >/dev/null; then
	echo 'GRE key remained present after grekey 0' >&2
	exit 1
fi
echo 'freebsd-tunnelinterface-stage=gre-key-zero=ok'

# Re-key after the supported clear. This is expected to be Applied rather than
# a no-op; the second stage above is the same-key restart/idempotence oracle.
set_stage restart
write_config 1300 42
apply_once restart
status_row gif0 "$work/gif0.restart.status"
status_row gre0 "$work/gre0.restart.status"
jq -e '.phase == "Up" and .reason == "AlreadyConfigured" and .interfaceOwned == true' "$work/gif0.restart.status" >/dev/null
jq -e '.phase == "Up" and .key == 42 and .interfaceOwned == true' "$work/gre0.restart.status" >/dev/null
capture_pid=
timeout 10 jexec "$r1" tcpdump -n -v -c 1 -i "$epair_a" "ip proto 47 and src host $outer_a" >"$work/gre.rekey.proto47" 2>&1 &
capture_pid=$!
sleep 1
if ! r1cmd ping -n -c 3 -S 10.253.90.1 10.253.90.2 >"$work/gre.rekey.ping" 2>&1; then
	cat "$work/gre.rekey.ping" >&2
	exit 1
fi
grep -F '3 packets transmitted, 3 packets received' "$work/gre.rekey.ping"
wait "$capture_pid"
capture_pid=
grep -F "$outer_a > $outer_b: GREv0" "$work/gre.rekey.proto47"
grep -F 'key=0x2a' "$work/gre.rekey.proto47"
echo 'freebsd-tunnelinterface-stage=restart=ok'

set_stage remove
cat >"$work/router.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-tunnelinterface-smoke}
spec: {resources: []}
EOF
apply_once remove
for ifname in gif0 gre0; do
  if r1cmd ifconfig "$ifname" >/dev/null 2>&1; then
    echo "stale tunnel interface remains: $ifname" >&2
    exit 1
  fi
done
echo 'freebsd-tunnelinterface-stage=owned-cleanup=ok'

# A pre-existing administrator gif must be rejected and remain unchanged; the
# fixture alone destroys this disposable foreign interface afterward.
set_stage foreign-preservation
r1cmd ifconfig gif0 create
r1cmd ifconfig gif0 mtu 1401
r1cmd ifconfig gif0 >"$work/gif0.foreign.before"
write_config 1300
apply_once foreign
status_row gif0 "$work/gif0.foreign.status"
jq -e '.phase == "Error" and .reason == "ForeignInterface" and (.interfaceOwned != true)' "$work/gif0.foreign.status" >/dev/null
r1cmd ifconfig gif0 >"$work/gif0.foreign.after"
cmp "$work/gif0.foreign.before" "$work/gif0.foreign.after"
echo 'freebsd-tunnelinterface-stage=foreign-preservation=ok'

set_stage foreign-stale-preservation
cat >"$work/router.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-tunnelinterface-smoke}
spec: {resources: []}
EOF
apply_once foreign-stale
r1cmd ifconfig gif0 >"$work/gif0.foreign.stale.after"
cmp "$work/gif0.foreign.before" "$work/gif0.foreign.stale.after"
r1cmd ifconfig gif0 destroy
echo 'freebsd-tunnelinterface-stage=foreign-stale-preservation=ok'
body_ok=1
