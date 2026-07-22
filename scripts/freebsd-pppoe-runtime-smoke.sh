#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Run the current FreeBSD routerd-pppoe-client against a disposable mpd5 PPPoE
# access concentrator.  Both processes, the epair, and credentials are owned by
# this script; no system mpd5 or operator configuration is touched.
set -eu

pppoe_client=
evidence_dir=
while [ "$#" -gt 0 ]; do
  case "$1" in
  --pppoe-client) pppoe_client=$2; shift 2 ;;
  --evidence-dir) evidence_dir=$2; shift 2 ;;
  *) echo "usage: $0 --pppoe-client PATH --evidence-dir DIR" >&2; exit 2 ;;
  esac
done

[ -x "$pppoe_client" ]
[ -n "$evidence_dir" ]
[ "$(uname -s)" = FreeBSD ]
command -v mpd5 >/dev/null
command -v jq >/dev/null
command -v curl >/dev/null

mkdir -p "$evidence_dir"
work=$(mktemp -d /var/tmp/routerd-pppoe-runtime.XXXXXX)
epair_a=
epair_b=
mpd_pid=
client_pid=

cleanup() {
  rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "freebsd-pppoe-runtime failure evidence:" >&2
    for log in "$evidence_dir"/*; do
      [ -f "$log" ] || continue
      echo "--- $log" >&2
      sed -e 's/pppoe-secret/[REDACTED]/g' "$log" >&2 || true
    done
  fi
  if [ -n "$client_pid" ]; then
    kill -TERM "$client_pid" 2>/dev/null || true
    wait "$client_pid" 2>/dev/null || true
  fi
  if [ -n "$mpd_pid" ] && kill -0 "$mpd_pid" 2>/dev/null; then
    kill -TERM "$mpd_pid" 2>/dev/null || true
    wait "$mpd_pid" 2>/dev/null || true
  fi
  if [ -n "$epair_a" ] && ifconfig "$epair_a" >/dev/null 2>&1; then
    ifconfig "$epair_a" destroy >>"$evidence_dir/cleanup.log" 2>&1 || rc=1
  fi
  rm -rf "$work"
  exit "$rc"
}
trap cleanup EXIT HUP INT TERM

epair_a=$(ifconfig epair create)
case "$epair_a" in epair*a) ;; *) echo "unexpected epair name: $epair_a" >&2; exit 1 ;; esac
epair_b=${epair_a%a}b
ifconfig "$epair_a" up
ifconfig "$epair_b" up

cat >"$work/mpd.conf" <<EOF
default:
  load routerd_pppoe_server

routerd_pppoe_server:
  create bundle template B_routerd
  set ipcp ranges 198.18.10.1/32 198.18.10.2/32
  create link template L_routerd pppoe
  set link action bundle B_routerd
  set link disable chap eap
  set link accept pap
  set auth enable internal
  set pppoe service "routerd-lifecycle"
  create link template $epair_b L_routerd
  set pppoe iface $epair_b
  set link max-children 1
  set link enable incoming
EOF
printf '%s\n' 'routerd "pppoe-secret"' >"$work/mpd.secret"
chmod 0600 "$work/mpd.secret"
mpd5 -b -d "$work" -f mpd.conf -p "$work/mpd.pid" routerd_pppoe_server >"$evidence_dir/mpd5.log" 2>&1
for _ in $(jot 30); do
  [ -s "$work/mpd.pid" ] && break
  sleep 1
done
[ -s "$work/mpd.pid" ]
mpd_pid=$(cat "$work/mpd.pid")
kill -0 "$mpd_pid"

"$pppoe_client" daemon --resource lifecycle-pppoe --interface "$epair_a" \
  --username routerd --password pppoe-secret --auth-method pap --service-name routerd-lifecycle \
  --socket "$work/pppoe.sock" --state-file "$evidence_dir/pppoe-state.json" \
  --event-file "$evidence_dir/pppoe-events.jsonl" >"$evidence_dir/pppoe.stdout.log" 2>"$evidence_dir/pppoe.stderr.log" &
client_pid=$!
for _ in $(jot 30); do
  [ -S "$work/pppoe.sock" ] && break
  kill -0 "$client_pid" 2>/dev/null || break
  sleep 1
done
[ -S "$work/pppoe.sock" ]
for _ in $(jot 30); do
  curl --fail --silent --show-error --unix-socket "$work/pppoe.sock" http://localhost/v1/status >"$evidence_dir/pppoe-status-initial.json" || true
  jq -e '.resources[0].phase == "Connected" and .resources[0].observed.currentAddress == "198.18.10.2" and .resources[0].observed.peerAddress == "198.18.10.1"' "$evidence_dir/pppoe-status-initial.json" >/dev/null 2>&1 && break
  sleep 1
done
jq -e '.resources[0].phase == "Connected" and .resources[0].observed.currentAddress == "198.18.10.2" and .resources[0].observed.peerAddress == "198.18.10.1"' "$evidence_dir/pppoe-status-initial.json" >/dev/null

curl --fail --silent --show-error --unix-socket "$work/pppoe.sock" -X POST http://localhost/v1/commands/stop >"$evidence_dir/pppoe-stop.json"
for _ in $(jot 30); do
  curl --fail --silent --show-error --unix-socket "$work/pppoe.sock" http://localhost/v1/status >"$evidence_dir/pppoe-status-stopped.json" || true
  jq -e '.resources[0].phase == "Idle"' "$evidence_dir/pppoe-status-stopped.json" >/dev/null 2>&1 && break
  sleep 1
done
jq -e '.resources[0].phase == "Idle"' "$evidence_dir/pppoe-status-stopped.json" >/dev/null
curl --fail --silent --show-error --unix-socket "$work/pppoe.sock" -X POST http://localhost/v1/commands/start >"$evidence_dir/pppoe-restart.json"
for _ in $(jot 30); do
  curl --fail --silent --show-error --unix-socket "$work/pppoe.sock" http://localhost/v1/status >"$evidence_dir/pppoe-status-restarted.json" || true
  jq -e '.resources[0].phase == "Connected"' "$evidence_dir/pppoe-status-restarted.json" >/dev/null 2>&1 && break
  sleep 1
done
jq -e '.resources[0].phase == "Connected"' "$evidence_dir/pppoe-status-restarted.json" >/dev/null

printf '%s\n' \
  'pppoe-mpd5-peer-configure-start-observe-stop-restart=ok' \
  'pppoe-owned-epair-and-process-cleanup=pending-exit-trap' >"$evidence_dir/summary.log"
printf 'freebsd-pppoe-runtime=ok\n' >"$evidence_dir/result"
