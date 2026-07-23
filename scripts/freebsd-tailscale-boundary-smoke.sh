#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Tailscale enrollment is intentionally external to CI.  This exercises the
# production-generated FreeBSD rc.d lifecycle up to that boundary and refuses
# to touch a pre-existing tailscaled service.
set -eu

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
service tailscaled onestatus >/dev/null 2>&1 && {
  echo "foreign tailscaled service is already running; refusing mutation" >&2
  exit 1
}

mkdir -p "$evidence_dir"
work=$(mktemp -d /var/tmp/routerd-tailscale-boundary.XXXXXX)
script=
script_started=0
foreign_started=0

run_bounded() {
  label=$1
  seconds=$2
  log=$3
  shift 3
  started=$(date +%s)
  printf 'step=%s begin\n' "$label" >>"$evidence_dir/timing.log"
  if timeout -k 2 "$seconds" "$@" >"$log" 2>&1; then
    command_rc=0
  else
    command_rc=$?
  fi
  ended=$(date +%s)
  printf 'step=%s rc=%s elapsed=%ss\n' "$label" "$command_rc" "$((ended - started))" >>"$evidence_dir/timing.log"
  return "$command_rc"
}

cleanup() {
  rc=$?
  if [ "$script_started" -eq 1 ] && [ -n "$script" ]; then
    run_bounded tailscale-owned-stop 45 "$evidence_dir/tailscale-owned-stop.log" "$script" onestop || rc=1
  fi
  if [ "$foreign_started" -eq 1 ]; then
    run_bounded tailscaled-foreign-stop 45 "$evidence_dir/tailscaled-stop.log" service tailscaled onestop || rc=1
    if run_bounded tailscaled-status-after-stop 15 "$evidence_dir/tailscaled-status-after-stop.log" service tailscaled onestatus; then
      rc=1
    fi
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
if run_bounded tailscale-owned-start 45 "$evidence_dir/tailscale-up.log" "$script" start; then
  echo "unexpected authenticated Tailscale enrollment in credential-free CI" >&2
  exit 1
fi
if run_bounded tailscaled-status-after-owned-failure 15 "$evidence_dir/tailscaled-status-after-owned-failure.log" service tailscaled onestatus; then
  echo "generated Tailscale lifecycle left tailscaled running after failed enrollment" >&2
  exit 1
fi
if run_bounded tailscale-status 15 "$evidence_dir/tailscale-status.json" tailscale status --json; then
  echo "unexpected enrolled Tailscale status in credential-free CI" >&2
  exit 1
fi

# A service started outside routerd is foreign. The generated artifact must
# reject it and leave it running for the fixture owner to stop.
run_bounded tailscaled-foreign-start 45 "$evidence_dir/tailscaled-foreign-start.log" service tailscaled onestart
foreign_started=1
run_bounded tailscaled-foreign-status 15 "$evidence_dir/tailscaled-foreign-status.log" service tailscaled onestatus
if run_bounded tailscale-foreign-refusal 45 "$evidence_dir/tailscale-foreign-refusal.log" "$script" start; then
  echo "generated Tailscale lifecycle adopted foreign tailscaled" >&2
  exit 1
fi
run_bounded tailscaled-foreign-preserved 15 "$evidence_dir/tailscaled-foreign-preserved.log" service tailscaled onestatus
run_bounded tailscaled-foreign-stop 45 "$evidence_dir/tailscaled-foreign-stop.log" service tailscaled onestop
foreign_started=0
printf '%s\n' \
  'tailscale-generated-start-failure-unwinds-owned-service=ok' \
  'tailscale-enrollment=external-auth-required-actionable-nonzero' \
  'foreign-running-tailscaled=generated-refusal-preserved' >"$evidence_dir/summary.log"
printf 'freebsd-tailscale-boundary=ok\n' >"$evidence_dir/result"
