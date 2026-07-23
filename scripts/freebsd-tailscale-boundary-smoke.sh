#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Tailscale enrollment is intentionally external to CI.  This exercises the
# production-generated FreeBSD rc.d lifecycle up to that boundary and refuses
# to touch a pre-existing tailscaled service.
set -eu
exec 3>&2

routerd=
evidence_dir=
while [ "$#" -gt 0 ]; do
  case "$1" in
  --routerd) routerd=$2; shift 2 ;;
  --evidence-dir) evidence_dir=$2; shift 2 ;;
  *) echo "usage: $0 --routerd PATH --evidence-dir DIR" >&2; exit 2 ;;
  esac
done
[ -x "$routerd" ]
[ -n "$evidence_dir" ]
[ "$(uname -s)" = FreeBSD ]
command -v tailscale >/dev/null
pidfile=/var/run/tailscaled.pid
tailscaled_running() {
  [ -r "$pidfile" ] || return 1
  tailscaled_pid=
  IFS= read -r tailscaled_pid <"$pidfile" || [ -n "$tailscaled_pid" ] || return 1
  case "$tailscaled_pid" in ''|*[!0-9]*) return 1 ;; esac
  kill -0 "$tailscaled_pid" 2>/dev/null
}
clear_stale_tailscaled_pidfile() {
  test -e "$pidfile" || return 0
  tailscaled_pid=
  IFS= read -r tailscaled_pid <"$pidfile" || [ -n "$tailscaled_pid" ] || tailscaled_pid=
  case "$tailscaled_pid" in ''|*[!0-9]*) rm -f "$pidfile"; return 0 ;; esac
  kill -0 "$tailscaled_pid" 2>/dev/null || rm -f "$pidfile"
}
clear_stale_tailscaled_pidfile
tailscaled_running && {
  echo "foreign tailscaled service is already running; refusing mutation" >&2
  exit 1
}

mkdir -p "$evidence_dir"
work=$(mktemp -d /var/tmp/routerd-tailscale-boundary.XXXXXX)
script=
script_started=0
foreign_started=0
foreign_launcher=
foreign_launcher_done=0

run_bounded() {
  label=$1
  seconds=$2
  log=$3
  shift 3
  started=$(date +%s)
  printf 'step=%s begin\n' "$label" >>"$evidence_dir/timing.log"
  printf 'step=%s begin\n' "$label" >&3
  if timeout -k 2 "$seconds" "$@" >"$log" 2>&1; then
    command_rc=0
  else
    command_rc=$?
  fi
  ended=$(date +%s)
  printf 'step=%s rc=%s elapsed=%ss\n' "$label" "$command_rc" "$((ended - started))" >>"$evidence_dir/timing.log"
  printf 'step=%s rc=%s elapsed=%ss\n' "$label" "$command_rc" "$((ended - started))" >&3
  if [ "$command_rc" -eq 124 ]; then
    printf 'step=%s timed-out\n' "$label" >&3
  fi
  return "$command_rc"
}

wait_tailscaled_running() {
  seconds=$1
  log=$2
  started=$(date +%s)
  while [ "$(( $(date +%s) - started ))" -lt "$seconds" ]; do
    if tailscaled_running; then
      printf 'tailscaled pid=%s ready\n' "$(cat "$pidfile")" >"$log"
      return 0
    fi
    if [ "$foreign_launcher_done" -eq 0 ] && [ -n "$foreign_launcher" ] && ! kill -0 "$foreign_launcher" 2>/dev/null; then
      if wait "$foreign_launcher"; then
        foreign_launcher_done=1
        foreign_launcher=
      else
        printf 'tailscaled launcher exited nonzero before pidfile readiness\n' >"$log"
        return 1
      fi
    fi
    sleep 1
  done
  printf 'tailscaled pidfile readiness timed out\n' >"$log"
  return 124
}

cleanup() {
  rc=$?
  if [ "$script_started" -eq 1 ] && [ -n "$script" ]; then
    run_bounded tailscale-owned-stop 45 "$evidence_dir/tailscale-owned-stop.log" "$script" onestop || rc=1
  fi
  if [ "$foreign_started" -eq 1 ]; then
    run_bounded tailscaled-foreign-stop 45 "$evidence_dir/tailscaled-stop.log" service tailscaled onestop || rc=1
    if tailscaled_running; then
      printf 'tailscaled remains live after foreign stop\n' >"$evidence_dir/tailscaled-status-after-stop.log"
      rc=1
    fi
    if [ -n "$foreign_launcher" ] && kill -0 "$foreign_launcher" 2>/dev/null; then
      kill "$foreign_launcher" 2>/dev/null || true
      wait "$foreign_launcher" 2>/dev/null || true
    fi
  fi
  if [ "$rc" -ne 0 ]; then
    printf 'tailscale-boundary failure timing\n' >&3
    if [ -r "$evidence_dir/timing.log" ]; then
      cat "$evidence_dir/timing.log" >&3 || true
    fi
    for log in "$evidence_dir"/*.log; do
      [ -r "$log" ] || continue
      printf 'tailscale-boundary failure log=%s\n' "$log" >&3
      cat "$log" >&3 || true
    done
  fi
  rm -rf "$work"
  exit "$rc"
}
trap cleanup EXIT HUP INT TERM

cat >"$work/router.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: lifecycle-tailscale
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: TailscaleNode
      metadata:
        name: lifecycle
      spec:
        hostname: routerd-lifecycle
EOF
"$routerd" render freebsd --config "$work/router.yaml" --out-dir "$work/rendered" >"$evidence_dir/render.log"
script="$work/rendered/rc.d-routerd_tailscale_lifecycle"
test -x "$script"
grep -F 'foreign tailscaled service is already running; refusing mutation' "$script" >"$evidence_dir/render-service.log"
grep -F 'service tailscaled onestart' "$script" >>"$evidence_dir/render-service.log"
grep -F '/usr/local/bin/tailscale' "$script" >>"$evidence_dir/render-service.log"

# The generated lifecycle owns the start attempt. Credential-free enrollment
# must fail actionably and unwind its service ownership.
# Production's bounded start can consume up to 75 seconds including rollback;
# keep this fixture ceiling above it so it remains an outer safety guard.
if run_bounded tailscale-owned-start 130 "$evidence_dir/tailscale-up.log" "$script" start; then
  echo "unexpected authenticated Tailscale enrollment in credential-free CI" >&2
  exit 1
fi
if tailscaled_running; then
  printf 'tailscaled still live after owned rollback\n' >"$evidence_dir/tailscaled-status-after-owned-failure.log"
  echo "generated Tailscale lifecycle left tailscaled running after failed enrollment" >&2
  exit 1
fi
if run_bounded tailscale-status 15 "$evidence_dir/tailscale-status.json" tailscale status --json; then
  echo "unexpected enrolled Tailscale status in credential-free CI" >&2
  exit 1
fi

# A service started outside routerd is foreign. The generated artifact must
# reject it and leave it running for the fixture owner to stop.
service tailscaled onestart >"$evidence_dir/tailscaled-foreign-start.log" 2>&1 &
foreign_launcher=$!
foreign_started=1
wait_tailscaled_running 15 "$evidence_dir/tailscaled-foreign-status.log"
if run_bounded tailscale-foreign-refusal 45 "$evidence_dir/tailscale-foreign-refusal.log" "$script" start; then
  echo "generated Tailscale lifecycle adopted foreign tailscaled" >&2
  exit 1
fi
tailscaled_running || { echo "generated Tailscale lifecycle did not preserve foreign tailscaled" >&2; exit 1; }
printf 'tailscaled pid=%s preserved\n' "$(cat "$pidfile")" >"$evidence_dir/tailscaled-foreign-preserved.log"
run_bounded tailscaled-foreign-stop 45 "$evidence_dir/tailscaled-foreign-stop.log" service tailscaled onestop
foreign_started=0
printf '%s\n' \
  'tailscale-generated-start-failure-unwinds-owned-service=ok' \
  'tailscale-enrollment=external-auth-required-actionable-nonzero' \
  'foreign-running-tailscaled=generated-refusal-preserved' >"$evidence_dir/summary.log"
printf 'freebsd-tailscale-boundary=ok\n' >"$evidence_dir/result"
