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

work=$(mktemp -d /tmp/routerd-tunnelinterface-smoke.XXXXXX)
state="$work/state.db"
ledger="$work/ledger.json"
outer_a=198.18.89.1
outer_b=198.18.89.2

cleanup() {
  rc=$?
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
    spec: {mode: gre, local: $outer_a, remote: $outer_b, mtu: $mtu, key: 42, trustedUnderlay: true}
  - apiVersion: hybrid.routerd.net/v1alpha1
    kind: TunnelInterface
    metadata: {name: gre1}
    spec: {mode: gre, local: $outer_b, remote: $outer_a, mtu: $mtu, key: 42, trustedUnderlay: true}
EOF
}

apply_once() {
  "$routerd" serve --once --controllers tunnel --config "$work/router.yaml" \
    --state-file "$state" --ledger-file "$ledger" \
    --status-file "$work/status.json" --socket "$work/api.sock" \
    --status-socket "$work/status.sock" >>"$work/apply.log" 2>&1
}

write_config 1400
apply_once
ifconfig gif0 >"$work/gif0.add"
ifconfig gre0 >"$work/gre0.add"
grep -F "tunnel inet $outer_a --> $outer_b" "$work/gif0.add"
grep -F 'grekey: 42' "$work/gre0.add"
ping -n -c 3 -S 10.253.89.1 10.253.89.2 >"$work/gif.ping"
grep -F '3 packets transmitted, 3 packets received' "$work/gif.ping"

write_config 1300
apply_once
ifconfig gif0 >"$work/gif0.change"
ifconfig gre0 >"$work/gre0.change"
grep -F 'mtu 1300' "$work/gif0.change"
grep -F 'mtu 1300' "$work/gre0.change"

cat >"$work/router.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-tunnelinterface-smoke}
spec: {resources: []}
EOF
apply_once
for ifname in gif0 gif1 gre0 gre1; do
  if ifconfig "$ifname" >/dev/null 2>&1; then
    echo "stale tunnel interface remains: $ifname" >&2
    exit 1
  fi
done
echo "freebsd-tunnelinterface=ok"
