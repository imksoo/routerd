#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
# FreeBSD production endpoint; its Linux peer is managed by the workflow.
set -eu
exec 3>&2
routerd='' evidence=''
while [ "$#" -gt 0 ]; do case "$1" in --routerd) routerd=$2; shift 2;; --evidence-dir) evidence=$2; shift 2;; *) exit 2;; esac; done
[ "$(uname -s)" = FreeBSD ] && [ -x "$routerd" ] && [ -n "$evidence" ]
mkdir -p "$evidence"; work=$evidence/work; mkdir -p "$work"
peer_addr=${ROUTERD_IPSEC_PEER_ADDR:?ROUTERD_IPSEC_PEER_ADDR is required}
host_if=$(route -n get default | awk '/interface:/{print $2;exit}')
host_addr=$(ifconfig "$host_if" inet | awk '/inet /{print $2;exit}')
[ -n "$host_addr" ] && [ "$peer_addr" = "$(route -n get default | awk '/gateway:/{print $2;exit}')" ]
host_ts=10.250.1.1 peer_ts=10.250.2.1 psk=routerd-native-linux-peer-disposable-psk
state=$work/state.db ledger=$work/ledger.db
sentinel=/usr/local/etc/routerd/swanctl/operator-sentinel.conf
sentinel_created=0
service_before=0 enable_before=1 cleanup_started=0
run_bounded() {
  limit=$1 label=$2 log=$3; shift 3
  printf 'step=%s begin\n' "$label" >&3
  (
    elapsed=0
    while :; do
      sleep 5
      elapsed=$((elapsed + 5))
      printf 'step=%s waiting=%ss\n' "$label" "$elapsed" >&3
    done
  ) & heartbeat=$!
  if timeout -k 2 "$limit" "$@" >"$log" 2>&1; then rc=0; else rc=$?; fi
  kill "$heartbeat" 2>/dev/null || true
  wait "$heartbeat" 2>/dev/null || true
  printf 'step=%s rc=%s\n' "$label" "$rc" >&3
  return "$rc"
}
wait_established() {
  label=$1 log=$2
  for _ in $(jot 30); do
    if run_bounded 5 "${label}-status" "$log" /usr/local/sbin/swanctl --list-sas --ike native-tunnel && grep -q ESTABLISHED "$log"; then return 0; fi
    sleep 1
  done
  return 1
}
ack_phase() {
  phase=$1 marker=$2 ack=$3
  run_bounded 10 "$phase-marker" "$evidence/$phase.marker.log" nc -z -w 5 "$peer_addr" "$marker"
  deadline=$(( $(date +%s) + 30 ))
  : >"$evidence/$phase.ack.log"
  while [ "$(date +%s)" -lt "$deadline" ]; do
    if timeout -k 2 5 nc -w 5 "$peer_addr" "$ack" >>"$evidence/$phase.ack.log" 2>&1 && [ "$(tail -n 1 "$evidence/$phase.ack.log")" = ok ]; then
      printf 'step=%s-ack rc=0\n' "$phase" >&3
      return 0
    fi
    sleep 1
  done
  printf 'step=%s-ack rc=1\n' "$phase" >&3
  return 1
}
emit_failure() { for f in "$evidence"/*.log; do [ -f "$f" ] && { echo "--- ${f#"$evidence"/}" >&3; sed "s/$psk/[REDACTED]/g" "$f" >&3; }; done; }
cleanup() {
  rc=$?; [ "$cleanup_started" -eq 0 ] || return "$rc"; cleanup_started=1
  [ "$rc" -eq 0 ] || emit_failure
  trap - EXIT HUP INT TERM
  if ! run_bounded 20 cleanup-service-stop "$evidence/cleanup.service-stop.log" service strongswan onestop; then :; fi
  if [ "$service_before" -eq 1 ]; then if ! run_bounded 20 cleanup-service-start "$evidence/cleanup.service-start.log" service strongswan onestart; then :; fi; fi
  if [ "$enable_before" -eq 0 ]; then sysrc "strongswan_enable=$(cat "$work/enable.before")" >>"$evidence/cleanup.log" 2>&1 || true; else sysrc -x strongswan_enable >>"$evidence/cleanup.log" 2>&1 || true; fi
  rm -f /usr/local/etc/routerd/swanctl/routerd-*.conf /usr/local/etc/routerd/swanctl/routerd.conf /usr/local/etc/routerd/swanctl/.routerd-pending-load
  [ "$sentinel_created" -eq 1 ] && rm -f "$sentinel"
  ifconfig lo0 inet "$host_ts" -alias >/dev/null 2>&1 || true
  printf 'cleanup=complete rc=%s\n' "$rc" >>"$evidence/cleanup.log"; return "$rc"
}
trap cleanup EXIT HUP INT TERM
if service strongswan status >"$evidence/service.before.log" 2>&1; then service_before=1; fi
if sysrc -n strongswan_enable >"$work/enable.before" 2>"$evidence/enable.before.log"; then enable_before=0; fi
ifconfig lo0 inet "$host_ts/32" alias
install -d -m 0700 /usr/local/etc/routerd/swanctl
test ! -e "$sentinel"
cat >"$sentinel" <<'EOF'
# fixture-owned operator sentinel: routerd must not mutate this file
connections {}
EOF
sentinel_created=1
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
if run_bounded 45 invalid-apply "$evidence/invalid.log" "$routerd" apply --once --config "$work/invalid.yaml" --state-file "$state" --ledger-file "$ledger"; then invalid_rc=0; else invalid_rc=$?; fi
[ "$invalid_rc" -ne 0 ] && [ "$invalid_rc" -ne 124 ]; grep -Eiq 'swanctl|proposal|load' "$evidence/invalid.log"; if grep -F "$psk" "$evidence/invalid.log" >/dev/null; then exit 1; fi
run_bounded 45 valid-apply "$evidence/apply.log" "$routerd" apply --once --config "$work/router.yaml" --state-file "$state" --ledger-file "$ledger"
run_bounded 20 initiate "$evidence/initiate.log" /usr/local/sbin/swanctl --initiate --ike native-tunnel --child net
wait_established initial "$evidence/sa.initial.log"
ack_phase initial 19091 19191
run_bounded 20 initial-host-to-peer "$evidence/traffic.host-to-peer.log" ping -n -S "$host_ts" -c 2 "$peer_ts"
run_bounded 45 idempotent-apply "$evidence/idempotent-apply.log" "$routerd" apply --once --config "$work/router.yaml" --state-file "$state" --ledger-file "$ledger"
wait_established idempotent "$evidence/sa.idempotent.log"
run_bounded 20 idempotent-host-to-peer "$evidence/traffic.idempotent.log" ping -n -S "$host_ts" -c 2 "$peer_ts"
run_bounded 20 rekey "$evidence/rekey.log" /usr/local/sbin/swanctl --rekey --ike native-tunnel
wait_established rekey "$evidence/sa.rekey.log"
ack_phase rekey 19092 19192
run_bounded 20 rekey-host-to-peer "$evidence/traffic.rekey.log" ping -n -S "$host_ts" -c 2 "$peer_ts"
run_bounded 20 restart-service-stop "$evidence/restart.stop.log" service strongswan onestop
run_bounded 45 restart-apply "$evidence/restart.log" "$routerd" apply --once --config "$work/router.yaml" --state-file "$state" --ledger-file "$ledger"
run_bounded 20 restart-initiate "$evidence/restart.initiate.log" /usr/local/sbin/swanctl --initiate --ike native-tunnel --child net
wait_established restart "$evidence/sa.restart.log"
ack_phase restart 19093 19193
run_bounded 20 restart-host-to-peer "$evidence/traffic.restart.log" ping -n -S "$host_ts" -c 2 "$peer_ts"
cat >"$work/empty.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: empty}
spec: {}
EOF
run_bounded 45 teardown-apply "$evidence/teardown.log" "$routerd" apply --once --config "$work/empty.yaml" --state-file "$state" --ledger-file "$ledger"
test ! -e /usr/local/etc/routerd/swanctl/routerd.conf
test ! -e /usr/local/etc/routerd/swanctl/.routerd-pending-load
grep -Fx '# fixture-owned operator sentinel: routerd must not mutate this file' "$sentinel"
run_bounded 20 teardown-list-conns "$evidence/teardown.list-conns.log" /usr/local/sbin/swanctl --list-conns
if grep -F native-tunnel "$evidence/teardown.list-conns.log" >/dev/null; then
  echo 'routerd-owned native-tunnel remained after teardown' >&2
  exit 1
fi
printf '%s\n' \
  'ipsec-invalid-load=actionable-no-secret-leak' \
  'ipsec-apply=ok' \
  'ipsec-psk-auth=ok' \
  'ipsec-bidirectional-traffic=ok' \
  'ipsec-rekey=ok' \
  'ipsec-restart-recovery=ok' \
  'ipsec-teardown=ok' >"$evidence/summary.log"
printf 'freebsd-ipsec-linux-peer=ok\n' >"$evidence/result"
