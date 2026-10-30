#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
# Independent drill queries against the actual resolver on private loopback
# sockets. The existing native launcher owns every daemon signal and wait.
set -eu

dns_resolver=
lifecycle_helper=
evidence_dir=
port=15353
while [ "$#" -gt 0 ]; do
  case "$1" in
    --dns-resolver) dns_resolver=$2; shift 2 ;;
    --lifecycle-helper) lifecycle_helper=$2; shift 2 ;;
    --evidence-dir) evidence_dir=$2; shift 2 ;;
    --port) port=$2; shift 2 ;;
    *) echo "usage: $0 --dns-resolver PATH --lifecycle-helper PATH --evidence-dir DIR [--port 15353]" >&2; exit 2 ;;
  esac
done
[ "$(uname -s)" = FreeBSD ] || { echo 'INCONCLUSIVE: native FreeBSD is required' >&2; exit 3; }
[ -x "$dns_resolver" ] && [ -x "$lifecycle_helper" ]
case "$evidence_dir" in /*) ;; *) echo 'an absolute evidence directory is required' >&2; exit 2 ;; esac
case "$port" in ''|*[!0-9]*) exit 2 ;; esac
[ "$port" -ge 1024 ] && [ "$port" -le 65535 ]
for command in drill curl jq timeout sockstat sha256; do
  command -v "$command" >/dev/null || { echo "INCONCLUSIVE: missing $command" >&2; exit 3; }
done

umask 077
mkdir -p "$evidence_dir"
(set -C; printf 'freebsd-dns-resolver=in-progress\n' >"$evidence_dir/result") || {
  echo 'use a fresh evidence directory' >&2; exit 2
}
work=$(mktemp -d "$evidence_dir/work.XXXXXX")
control=
wrapper=
run_label=
started=0

owner_request() {
  curl --connect-timeout 1 --max-time 12 --fail --silent --show-error \
    --unix-socket "$control" -X POST "http://child/$1"
}

stop_resolver() {
  [ -n "$control" ] || return 0
  owner_request term >"$evidence_dir/$run_label-stopped.json" || return 1
  jq -e '.running == false and (.error // "") == ""' "$evidence_dir/$run_label-stopped.json" >/dev/null || return 1
  stop_status=0
  jq -e '.exitCode == 0' "$evidence_dir/$run_label-stopped.json" >/dev/null || stop_status=1
  owner_request close >"$evidence_dir/$run_label-owner-closed.json" || return 1
  # Reap this shell's launcher child. No PID loaded from a file authorizes a signal.
  wait "$wrapper" || stop_status=1
  control=
  wrapper=
  return "$stop_status"
}

observe_gone() {
  sockstat -4 -l >"$evidence_dir/$1-sockets.txt" || return 1
  awk -v suffix=":$port" 'NR > 1 && $6 ~ (suffix "$" ) { found=1 } END { exit found ? 1 : 0 }' \
    "$evidence_dir/$1-sockets.txt" || return 1
  [ ! -e "$work/resolver.sock" ] || return 1
  ps -ax -o pid= -o comm= -o args= >"$work/processes.txt" || return 1
  awk -v owned="$work/" 'index($0, owned) { print }' "$work/processes.txt" >"$evidence_dir/$1-owned-processes.txt" || return 1
  [ ! -s "$evidence_dir/$1-owned-processes.txt" ]
}

cleanup() {
  status=$1
  trap - EXIT HUP INT TERM
  cleanup_ok=1
  stop_resolver || cleanup_ok=0
  if [ "$started" -eq 1 ]; then
    observe_gone final-cleanup || cleanup_ok=0
  fi
  for file in resolver.json state.json events.jsonl; do
    if [ -f "$work/$file" ]; then
      cp "$work/$file" "$evidence_dir/final-$file" || cleanup_ok=0
    fi
  done
  if [ "$cleanup_ok" -eq 1 ]; then
    rm -rf -- "$work" || cleanup_ok=0
  fi
  if [ "$cleanup_ok" -ne 1 ]; then
    echo "cleanup is unconfirmed; retaining private recovery files: $work" >&2
    status=1
  fi
  if [ "$status" -eq 0 ]; then
    printf 'freebsd-dns-resolver=pass\n' >"$evidence_dir/result"
  else
    printf 'freebsd-dns-resolver=not-pass exit=%s\n' "$status" >"$evidence_dir/result"
  fi
  exit "$status"
}
trap 'cleanup $?' EXIT
trap 'cleanup 129' HUP
trap 'cleanup 130' INT
trap 'cleanup 143' TERM

uname -a >"$evidence_dir/uname.txt"
drill -v >"$evidence_dir/drill-version.txt"
sha256 "$dns_resolver" "$lifecycle_helper" >"$evidence_dir/binaries.sha256"
sockstat -4 -l >"$evidence_dir/before-sockets.txt"
awk -v suffix=":$port" 'NR > 1 && $6 ~ (suffix "$" ) { found=1 } END { exit found ? 1 : 0 }' \
  "$evidence_dir/before-sockets.txt" || { echo 'INCONCLUSIVE: selected port is already in use' >&2; exit 3; }

write_config() {
  cat >"$work/resolver.next.json" <<EOF
{"resource":"native-dns-peer","spec":{"listen":[{"name":"loopback","addresses":["127.0.0.1"],"port":$port}],"sources":[{"name":"fixture-zone","kind":"zone","match":["routerd.invalid"],"zoneRef":["DNSZone/fixture-zone"]}]},"zones":[{"name":"fixture-zone","spec":{"zone":"routerd.invalid","records":[{"hostname":"probe","ipv4":"$1"}]}}]}
EOF
  mv "$work/resolver.next.json" "$work/resolver.json"
  "$dns_resolver" selftest --resource native-dns-peer --config-file "$work/resolver.json" >"$evidence_dir/$2-config.json"
}

start_resolver() {
  run_label=$1
  control=$work/$run_label-owner.sock
  started=1
  "$lifecycle_helper" --socket "$control" -- "$dns_resolver" daemon \
    --resource native-dns-peer --config-file "$work/resolver.json" \
    --socket "$work/resolver.sock" --state-file "$work/state.json" --event-file "$work/events.jsonl" \
    >"$evidence_dir/$run_label.stdout.log" 2>"$evidence_dir/$run_label.stderr.log" &
  wrapper=$!
  deadline=$(( $(date +%s) + 30 ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    if curl --connect-timeout 1 --max-time 1 --fail --silent \
      --unix-socket "$control" http://child/status >"$evidence_dir/$run_label-started.json" && \
      jq -e '.running == true and .pid > 0 and (.error // "") == ""' "$evidence_dir/$run_label-started.json" >/dev/null && \
      curl --connect-timeout 1 --max-time 1 --fail --silent \
      --unix-socket "$work/resolver.sock" http://resolver/v1/status >"$evidence_dir/$run_label-status.json" && \
      jq -e '.phase == "Running" and .health == "ok"' "$evidence_dir/$run_label-status.json" >/dev/null; then
      cp "$control.intent.json" "$evidence_dir/$run_label-intent.json"
      sockstat -4 -l >"$evidence_dir/$run_label-sockets.txt"
      resolver_pid=$(jq -r .pid "$evidence_dir/$run_label-started.json")
      awk -v owner="$resolver_pid" -v address="127.0.0.1:$port" \
        '$3 == owner && $6 == address { if ($5 == "udp4") udp++; if ($5 == "tcp4") tcp++ } END { exit udp == 1 && tcp == 1 ? 0 : 1 }' \
        "$evidence_dir/$run_label-sockets.txt"
      return 0
    fi
    sleep 1
  done
  echo 'INCONCLUSIVE: resolver did not acknowledge startup before its deadline' >&2
  return 3
}

query() {
  case "$2" in udp) transport=-u ;; tcp) transport=-t ;; *) return 2 ;; esac
  timeout 8 drill -4 "$transport" -p "$port" probe.routerd.invalid @127.0.0.1 A >"$evidence_dir/$1-$2.txt" 2>&1
  grep -F 'rcode: NOERROR' "$evidence_dir/$1-$2.txt" >/dev/null
  answer=$(awk '$1 == "probe.routerd.invalid." && $3 == "IN" && $4 == "A" { print $5 }' "$evidence_dir/$1-$2.txt")
  [ "$answer" = "$3" ]
}

write_config 192.0.2.10 initial
start_resolver initial
query initial udp 192.0.2.10
query initial tcp 192.0.2.10
printf 'independent-initial-udp-tcp=ok\n'

write_config 192.0.2.11 reloaded
curl --connect-timeout 1 --max-time 5 --fail --silent --show-error \
  --unix-socket "$work/resolver.sock" -X POST http://resolver/v1/reload >"$evidence_dir/reload.json"
jq -e '.reloaded == true and .listeners == 1' "$evidence_dir/reload.json" >/dev/null
query reloaded udp 192.0.2.11
query reloaded tcp 192.0.2.11
printf 'independent-reload-answer-change-udp-tcp=ok\n'

stop_resolver
observe_gone initial-stop
start_resolver restart
query restart udp 192.0.2.11
query restart tcp 192.0.2.11
printf 'independent-restart-udp-tcp=ok\n'
stop_resolver
observe_gone restart-stop
printf 'resolver-process-and-listener-withdrawal=ok\n'
