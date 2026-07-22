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
command -v tcpdump >/dev/null

mkdir -p "$evidence_dir"
work=$(mktemp -d /var/tmp/routerd-pppoe-runtime.XXXXXX)
epair_a=
epair_b=
jail_name="routerd-pppoe-vnet-$$"
jail_created=0
mpd_pid=
client_pid=
discovery_capture_pid=
session_capture_pid=

cleanup() {
  rc=$?
  tail -n 200 /var/log/messages | grep -i '[m]pd' >"$evidence_dir/mpd5-syslog.log" 2>/dev/null || true
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
  if [ -n "$discovery_capture_pid" ]; then
    kill -TERM "$discovery_capture_pid" 2>/dev/null || true
    wait "$discovery_capture_pid" 2>/dev/null || true
  fi
  if [ -n "$session_capture_pid" ]; then
    kill -TERM "$session_capture_pid" 2>/dev/null || true
    wait "$session_capture_pid" 2>/dev/null || true
  fi
  if [ "$jail_created" -eq 1 ]; then
    if [ -n "$mpd_pid" ]; then
      jexec "$jail_name" kill -TERM "$mpd_pid" 2>/dev/null || true
    fi
    jail -r "$jail_name" >>"$evidence_dir/cleanup.log" 2>&1 || rc=1
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
jail -c name="$jail_name" path=/ host.hostname="$jail_name" persist vnet \
  vnet.interface="$epair_b"
jail_created=1
jexec "$jail_name" ifconfig lo0 up
jexec "$jail_name" ifconfig "$epair_b" up
# The AC endpoint is VNET-owned after jail creation, so capture both traffic
# directions from the host-owned endpoint. Preserve PPPoE discovery without
# exposing PAP/CHAP payloads; the session capture includes only Ethernet,
# PPPoE, and the PPP FSM header.
tcpdump -n -e -l -s 128 -i "$epair_a" 'ether proto 0x8863' >"$evidence_dir/pppoe-discovery.log" 2>&1 &
discovery_capture_pid=$!
tcpdump -n -e -vv -l -s 26 -i "$epair_a" 'ether proto 0x8864' >"$evidence_dir/pppoe-session-headers.log" 2>&1 &
session_capture_pid=$!
sleep 1
kill -0 "$discovery_capture_pid"
kill -0 "$session_capture_pid"
# ng_pppoe is a FreeBSD KMOD. Load it before the disposable access
# concentrator starts; routerd performs the same idempotent load before its
# own mpd5 client command.
kldload -n ng_pppoe
kldstat -q -m ng_pppoe

cat >"$work/mpd.conf" <<EOF
default:
  load routerd_pppoe_server

routerd_pppoe_server:
  set log +all
  set console self 127.0.0.1 5005
  set console disable auth
  set console open
  create bundle template B_routerd
  set ipcp ranges 198.18.10.1/32 198.18.10.2/32
  create link template L_routerd pppoe
  set link action bundle B_routerd
  set link disable chap eap
  set link enable pap
  set auth enable internal
  # This isolated peer intentionally has no accounting backend. mpd5 defaults
  # accounting-start to mandatory, which closes an otherwise authenticated
  # link when no backend can accept the start record.
  set auth disable acct-mandatory
  set pppoe service "routerd-lifecycle"
  create link template $epair_b L_routerd
  set pppoe iface $epair_b
  set link max-children 1
  set link enable incoming
EOF
# The optional third mpd.secret field pins the address assigned to this
# disposable authenticated peer. Quotes are valid mpd.secret field syntax.
printf '%s\n' 'routerd "pppoe-secret" 198.18.10.2' >"$work/mpd.secret"
chmod 0600 "$work/mpd.secret"
jexec "$jail_name" mpd5 -b -d "$work" -f mpd.conf -p "$work/mpd.pid" routerd_pppoe_server >"$evidence_dir/mpd5.log" 2>&1
for _ in $(jot 30); do
  [ -s "$work/mpd.pid" ] && break
  sleep 1
done
[ -s "$work/mpd.pid" ]
mpd_pid=$(cat "$work/mpd.pid")
jexec "$jail_name" kill -0 "$mpd_pid"

# mpd5 writes its PID before it has necessarily read the selected label and
# registered the ng_pppoe listener.  Its client opens a PPPoE connection once
# and then waits on the connect timer, so starting routerd from the PID alone
# can lose that initial PADI.  Prove the selected template has both an
# incoming action and its netgraph interface node before starting the client.
for _ in $(jot 30); do
  {
    printf 'link %s\n' "$epair_b"
    printf '%s\n' 'show link' 'show device'
  } | jexec "$jail_name" nc -N -w 2 127.0.0.1 5005 >"$evidence_dir/mpd5-ready.log" 2>&1 || true
  if awk '{ gsub(/\r/, "") } $1 == "incoming" && $2 == "enable" { found=1 } END { exit !found }' "$evidence_dir/mpd5-ready.log" && \
    awk -v want="$epair_b:" '{ gsub(/\r/, "") } $1 == "Iface" && $2 == "Node" && $4 == want { found=1 } END { exit !found }' "$evidence_dir/mpd5-ready.log"; then
    break
  fi
  jexec "$jail_name" kill -0 "$mpd_pid" 2>/dev/null || break
  sleep 1
done
awk '{ gsub(/\r/, "") } $1 == "incoming" && $2 == "enable" { found=1 } END { exit !found }' "$evidence_dir/mpd5-ready.log"
awk -v want="$epair_b:" '{ gsub(/\r/, "") } $1 == "Iface" && $2 == "Node" && $4 == want { found=1 } END { exit !found }' "$evidence_dir/mpd5-ready.log"

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
# IPCP succeeds only after mpd5 attaches the negotiated bundle to ng_iface.
# routerd must own this preflight rather than relying on an ambient module.
kldstat -q -m ng_iface
for _ in $(jot 30); do
  curl --fail --silent --show-error --unix-socket "$work/pppoe.sock" http://localhost/v1/status >"$evidence_dir/pppoe-status-initial.json" || true
  jq -e '.resources[0].phase == "Connected" and .resources[0].observed.currentAddress == "198.18.10.2" and .resources[0].observed.peerAddress == "198.18.10.1"' "$evidence_dir/pppoe-status-initial.json" >/dev/null 2>&1 && break
  sleep 1
done
{
  printf '%s\n' \
    'show summary' \
    'bundle B_routerd' \
    'show bundle' \
    "link $epair_b" \
    'show link' \
    'show device' \
    'show lcp' \
    'show auth'
} | jexec "$jail_name" nc -N -w 2 127.0.0.1 5005 >"$evidence_dir/mpd5-console.log" 2>&1 || true
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
