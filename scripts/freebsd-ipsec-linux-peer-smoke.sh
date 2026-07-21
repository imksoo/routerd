#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
# FreeBSD production endpoint; its Linux peer is managed by the workflow.
set -eu
exec 3>&2
routerd='' evidence=''
while [ "$#" -gt 0 ]; do case "$1" in --routerd) routerd=$2; shift 2;; --evidence-dir) evidence=$2; shift 2;; *) exit 2;; esac; done
[ "$(uname -s)" = FreeBSD ] && [ -x "$routerd" ] && [ -n "$evidence" ]
mkdir -p "$evidence"; work=$evidence/work; mkdir -p "$work"
peer_addr=${ROUTERD_IPSEC_PEER_ADDR:-10.0.2.2}
host_if=$(route -n get default | awk '/interface:/{print $2;exit}')
host_addr=$(ifconfig "$host_if" inet | awk '/inet /{print $2;exit}')
[ -n "$host_addr" ] && [ "$peer_addr" = "$(route -n get default | awk '/gateway:/{print $2;exit}')" ]
host_ts=10.250.1.1 peer_ts=10.250.2.1 psk=routerd-native-linux-peer-disposable-psk
state=$work/state.db ledger=$work/ledger.db
service_before=0 enable_before=1 cleanup_started=0
emit_failure() { for f in "$evidence"/*.log; do [ -f "$f" ] && { echo "--- ${f#"$evidence"/}" >&3; sed "s/$psk/[REDACTED]/g" "$f" >&3; }; done; }
cleanup() {
  rc=$?; [ "$cleanup_started" -eq 0 ] || return "$rc"; cleanup_started=1
  [ "$rc" -eq 0 ] || emit_failure
  trap - EXIT HUP INT TERM
  service strongswan onestop >>"$evidence/cleanup.log" 2>&1 || true
  if [ "$service_before" -eq 1 ]; then service strongswan onestart >>"$evidence/cleanup.log" 2>&1 || true; fi
  if [ "$enable_before" -eq 0 ]; then sysrc "strongswan_enable=$(cat "$work/enable.before")" >>"$evidence/cleanup.log" 2>&1 || true; else sysrc -x strongswan_enable >>"$evidence/cleanup.log" 2>&1 || true; fi
  rm -f /usr/local/etc/routerd/swanctl/routerd-*.conf /usr/local/etc/routerd/swanctl/routerd.conf /usr/local/etc/routerd/swanctl/.routerd-pending-load
  ifconfig lo0 inet "$host_ts" -alias >/dev/null 2>&1 || true
  printf 'cleanup=complete rc=%s\n' "$rc" >>"$evidence/cleanup.log"; return "$rc"
}
trap cleanup EXIT HUP INT TERM
if service strongswan status >"$evidence/service.before.log" 2>&1; then service_before=1; fi
sysrc -n strongswan_enable >"$work/enable.before" 2>"$evidence/enable.before.log" || enable_before=1
ifconfig lo0 inet "$host_ts/32" alias
cat >"$work/router.yaml" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: native-ipsec-linux-peer}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: IPsecConnection
    metadata: {name: native-tunnel}
    spec: {localAddress: $host_addr, remoteAddress: $peer_addr, preSharedKey: $psk, leftSubnet: $host_ts/32, rightSubnet: $peer_ts/32}
EOF
cat >"$work/invalid.yaml" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: invalid}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: IPsecConnection
    metadata: {name: native-tunnel}
    spec: {localAddress: $host_addr, remoteAddress: $peer_addr, preSharedKey: $psk, phase1Proposals: [invalid-proposal], leftSubnet: $host_ts/32, rightSubnet: $peer_ts/32}
EOF
set +e
timeout -k 2 45 "$routerd" apply --once --config "$work/invalid.yaml" --state-file "$state" --ledger-file "$ledger" >"$evidence/invalid.log" 2>&1; invalid_rc=$?
set -e
[ "$invalid_rc" -ne 0 ] && [ "$invalid_rc" -ne 124 ]; grep -Eiq 'swanctl|proposal|load' "$evidence/invalid.log"; if grep -F "$psk" "$evidence/invalid.log" >/dev/null; then exit 1; fi
timeout -k 2 45 "$routerd" apply --once --config "$work/router.yaml" --state-file "$state" --ledger-file "$ledger" >"$evidence/apply.log" 2>&1
/usr/local/sbin/swanctl --initiate --ike native-tunnel --child net >"$evidence/initiate.log" 2>&1
for _ in $(jot 30); do /usr/local/sbin/swanctl --list-sas --ike native-tunnel >"$evidence/sa.initial.log" && grep -q ESTABLISHED "$evidence/sa.initial.log" && break; sleep 1; done
grep -q ESTABLISHED "$evidence/sa.initial.log"
ping -n -S "$host_ts" -c 2 "$peer_ts" >"$evidence/traffic.host-to-peer.log" 2>&1
/usr/local/sbin/swanctl --rekey --ike native-tunnel >"$evidence/rekey.log" 2>&1
for _ in $(jot 30); do /usr/local/sbin/swanctl --list-sas --ike native-tunnel >"$evidence/sa.rekey.log" && grep -q ESTABLISHED "$evidence/sa.rekey.log" && break; sleep 1; done
grep -q ESTABLISHED "$evidence/sa.rekey.log"
service strongswan onestop >"$evidence/restart.stop.log" 2>&1
timeout -k 2 45 "$routerd" apply --once --config "$work/router.yaml" --state-file "$state" --ledger-file "$ledger" >"$evidence/restart.log" 2>&1
/usr/local/sbin/swanctl --initiate --ike native-tunnel --child net >"$evidence/restart.initiate.log" 2>&1
for _ in $(jot 30); do /usr/local/sbin/swanctl --list-sas --ike native-tunnel >"$evidence/sa.restart.log" && grep -q ESTABLISHED "$evidence/sa.restart.log" && break; sleep 1; done
grep -q ESTABLISHED "$evidence/sa.restart.log"
cat >"$work/empty.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: empty}
spec: {}
EOF
timeout -k 2 45 "$routerd" apply --once --config "$work/empty.yaml" --state-file "$state" --ledger-file "$ledger" >"$evidence/teardown.log" 2>&1
test ! -e /usr/local/etc/routerd/swanctl/routerd.conf
test ! -e /usr/local/etc/routerd/swanctl/.routerd-pending-load
printf 'freebsd-ipsec-linux-peer=ok\n' >"$evidence/result"
