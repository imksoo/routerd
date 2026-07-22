#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause

# Opt-in native acceptance for the production TunnelInterface controller.
# It owns only the listed loopback aliases and cloned gif/gre interfaces.
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

emit_initial_failure() {
	for evidence in apply-initial.log gre0.add gre0.initial.status; do
		path="$work/$evidence"
		[ -f "$path" ] || continue
		echo "--- tunnelinterface $evidence" >&2
		sed -n '1,160p' "$path" >&2
	done
}

cleanup() {
	rc=$?
	if [ "$rc" -ne 0 ]; then
		emit_initial_failure
	fi
	ifconfig gif0 destroy >/dev/null 2>&1 || true
  ifconfig gif1 destroy >/dev/null 2>&1 || true
  ifconfig gre0 destroy >/dev/null 2>&1 || true
  ifconfig gre1 destroy >/dev/null 2>&1 || true
  ifconfig lo0 inet "$outer_a" -alias >/dev/null 2>&1 || true
  ifconfig lo0 inet "$outer_b" -alias >/dev/null 2>&1 || true
  rm -rf "$work"
  exit "$rc"
}
trap cleanup EXIT HUP INT TERM

for ifname in gif0 gif1 gre0 gre1; do
  if ifconfig "$ifname" >/dev/null 2>&1; then
    echo "fixture requires absent $ifname" >&2
    exit 75
  fi
done

ifconfig lo0 inet "$outer_a"/32 alias
ifconfig lo0 inet "$outer_b"/32 alias

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
    spec: {mode: ipip, local: $outer_a, remote: $outer_b, address: 10.253.89.1/30, mtu: $mtu, trustedUnderlay: true}
  - apiVersion: hybrid.routerd.net/v1alpha1
    kind: TunnelInterface
    metadata: {name: gif1}
    spec: {mode: ipip, local: $outer_b, remote: $outer_a, address: 10.253.89.2/30, mtu: $mtu, trustedUnderlay: true}
  - apiVersion: hybrid.routerd.net/v1alpha1
    kind: TunnelInterface
    metadata: {name: gre0}
    spec: {mode: gre, local: $outer_a, remote: $outer_b, mtu: $mtu, key: $key, trustedUnderlay: true}
  - apiVersion: hybrid.routerd.net/v1alpha1
    kind: TunnelInterface
    metadata: {name: gre1}
    spec: {mode: gre, local: $outer_b, remote: $outer_a, mtu: $mtu, key: $key, trustedUnderlay: true}
EOF
}

apply_once() {
	label=$1
  "$routerd" serve --once --controllers tunnel --config "$work/router.yaml" \
    --state-file "$state" --ledger-file "$ledger" \
    --status-file "$work/status-$label.json" --socket "$work/api-$label.sock" \
    --status-socket "$work/status-$label.sock" >"$work/apply-$label.log" 2>&1
}

status_row() {
	name=$1
	target=$2
	if command -v sqlite3 >/dev/null 2>&1; then
		sqlite3 "$state" "SELECT status FROM objects WHERE api_version='hybrid.routerd.net/v1alpha1' AND kind='TunnelInterface' AND name='$name';" >"$target"
	else
		strings "$state" | grep -F "\"interface\":\"$name\"" >"$target"
	fi
	[ -s "$target" ]
}

write_config 1400
apply_once initial
ifconfig gif0 >"$work/gif0.add"
ifconfig gre0 >"$work/gre0.add"
status_row gre0 "$work/gre0.initial.status"
grep -F "tunnel inet $outer_a --> $outer_b" "$work/gif0.add"
# FreeBSD 14.3 ifconfig prints a configured key as `grekey: 0x2a (42)`.
# Assert the native semantic value rather than the stale decimal-only form.
grep -E 'grekey:[[:space:]]+0x2a[[:space:]]+\(42\)' "$work/gre0.add"
jq -e '.phase == "Up" and .key == 42 and .interfaceOwned == true' "$work/gre0.initial.status" >/dev/null
ping -n -c 3 -S 10.253.89.1 10.253.89.2 >"$work/gif.ping"
grep -F '3 packets transmitted, 3 packets received' "$work/gif.ping"

# A new serve --once process using the persisted state is a controller restart;
# it must be a no-op, not an adoption of a different kernel object.
apply_once second
status_row gif0 "$work/gif0.second.status"
jq -e '.phase == "Up" and .reason == "AlreadyConfigured" and .interfaceOwned == true' "$work/gif0.second.status" >/dev/null

write_config 1300
apply_once change
ifconfig gif0 >"$work/gif0.change"
ifconfig gre0 >"$work/gre0.change"
grep -F 'mtu 1300' "$work/gif0.change"
grep -F 'mtu 1300' "$work/gre0.change"

# FreeBSD has no native-verified safe clear token for a configured GRE key;
# reject this shape rather than silently retaining key 42.
write_config 1300 0
apply_once gre-key-zero
status_row gre0 "$work/gre0.key-zero.status"
jq -e '.phase == "Error" and (.error | contains("clear FreeBSD GRE key")) and .interfaceOwned == true' "$work/gre0.key-zero.status" >/dev/null

write_config 1300
apply_once restart
status_row gif0 "$work/gif0.restart.status"
jq -e '.phase == "Up" and .reason == "AlreadyConfigured" and .interfaceOwned == true' "$work/gif0.restart.status" >/dev/null

cat >"$work/router.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-tunnelinterface-smoke}
spec: {resources: []}
EOF
apply_once remove
for ifname in gif0 gif1 gre0 gre1; do
  if ifconfig "$ifname" >/dev/null 2>&1; then
    echo "stale tunnel interface remains: $ifname" >&2
    exit 1
  fi
done

# A pre-existing administrator gif must be rejected and remain unchanged; the
# fixture alone destroys this disposable foreign interface afterward.
ifconfig gif0 create
ifconfig gif0 mtu 1234
ifconfig gif0 >"$work/gif0.foreign.before"
write_config 1300
apply_once foreign
status_row gif0 "$work/gif0.foreign.status"
jq -e '.phase == "Error" and .reason == "ForeignInterface" and (.interfaceOwned != true)' "$work/gif0.foreign.status" >/dev/null
ifconfig gif0 >"$work/gif0.foreign.after"
cmp "$work/gif0.foreign.before" "$work/gif0.foreign.after"

cat >"$work/router.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-tunnelinterface-smoke}
spec: {resources: []}
EOF
apply_once foreign-stale
ifconfig gif0 >"$work/gif0.foreign.stale.after"
cmp "$work/gif0.foreign.before" "$work/gif0.foreign.stale.after"
ifconfig gif0 destroy
echo "freebsd-tunnelinterface=ok"
