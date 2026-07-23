#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause

# Opt-in native acceptance for the production TunnelInterface controller.
# The production endpoint runs in r1; r2 is a separate VNET peer connected by
# one epair. Reciprocal GIF endpoints in one kernel stack are not a valid
# dataplane oracle because inner and outer routes collide locally.
set -eu

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

emit_initial_failure() {
	for evidence in \
		underlay.ping \
		apply-initial.log gif0.add gif0.initial.status gif.ping gif.proto4 gif.outer.before gif.outer.after gre0.add gre0.initial.status gre.ping gre.proto47 \
		apply-second.log gif0.second.status gre0.second.status \
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

cleanup() {
	rc=$?
	if [ -n "$capture_pid" ]; then
		kill "$capture_pid" >/dev/null 2>&1 || true
		wait "$capture_pid" >/dev/null 2>&1 || true
	fi
	if [ "$rc" -ne 0 ]; then
		emit_initial_failure
	fi
	if jls -j "$r1" >/dev/null 2>&1; then
		jail -r "$r1" >/dev/null 2>&1 || true
	fi
	if jls -j "$r2" >/dev/null 2>&1; then
		jail -r "$r2" >/dev/null 2>&1 || true
	fi
	if [ -n "$epair_a" ] && ifconfig "$epair_a" >/dev/null 2>&1; then
		ifconfig "$epair_a" destroy >/dev/null 2>&1 || true
	fi
	if [ "$own_epair_module" -eq 1 ]; then
		kldunload if_epair >/dev/null 2>&1 || true
	fi
	if [ "$own_gre_module" -eq 1 ]; then
		kldunload if_gre >/dev/null 2>&1 || true
	fi
  rm -rf "$work"
  exit "$rc"
}
trap cleanup EXIT HUP INT TERM

r1cmd() { jexec "$r1" "$@"; }
r2cmd() { jexec "$r2" "$@"; }

if ! kldstat -q -m if_epair; then
	kldload if_epair
	own_epair_module=1
fi
epair_a=$(ifconfig epair create)
epair_b="${epair_a%a}b"
jail -c name="$r1" path=/ host.hostname="$r1" persist vnet allow.raw_sockets=1 \
	vnet.interface="$epair_a"
jail -c name="$r2" path=/ host.hostname="$r2" persist vnet allow.raw_sockets=1 \
	vnet.interface="$epair_b"
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
  r1cmd "$routerd" serve --once --controllers tunnel --config "$work/router.yaml" \
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
apply_once second
status_row gif0 "$work/gif0.second.status"
status_row gre0 "$work/gre0.second.status"
jq -e '.phase == "Up" and .reason == "AlreadyConfigured" and .interfaceOwned == true' "$work/gif0.second.status" >/dev/null
jq -e '.phase == "Up" and .reason == "AlreadyConfigured" and .key == 42 and .interfaceOwned == true' "$work/gre0.second.status" >/dev/null
echo 'freebsd-tunnelinterface-stage=second-noop=ok'

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
echo "freebsd-tunnelinterface=ok"
