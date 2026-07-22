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
cleanup() {
  rc=$?
  if [ "$script_started" -eq 1 ] && [ -n "$script" ]; then
    "$script" onestop >>"$evidence_dir/tailscale-owned-stop.log" 2>&1 || rc=1
  fi
  if [ "$foreign_started" -eq 1 ]; then
    service tailscaled onestop >>"$evidence_dir/tailscaled-stop.log" 2>&1 || rc=1
    if service tailscaled onestatus >>"$evidence_dir/tailscaled-status-after-stop.log" 2>&1; then
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
        binaryPath: /usr/local/bin/tailscale
EOF
"$routerd" render freebsd --config "$work/router.yaml" --out-dir "$work/rendered" >"$evidence_dir/render.log"
script="$work/rendered/rc.d-routerd_tailscale_lifecycle"
test -x "$script"
grep -F 'foreign tailscaled service is already running; refusing mutation' "$script" >"$evidence_dir/render-service.log"
grep -F 'service tailscaled onestart' "$script" >>"$evidence_dir/render-service.log"
grep -F '/usr/local/bin/tailscale' "$script" >>"$evidence_dir/render-service.log"

# The generated lifecycle owns the start attempt. Credential-free enrollment
# must fail actionably and unwind its service ownership.
if "$script" onestart >"$evidence_dir/tailscale-up.log" 2>&1; then
  echo "unexpected authenticated Tailscale enrollment in credential-free CI" >&2
  exit 1
fi
if service tailscaled onestatus >"$evidence_dir/tailscaled-status-after-owned-failure.log" 2>&1; then
  echo "generated Tailscale lifecycle left tailscaled running after failed enrollment" >&2
  exit 1
fi
if tailscale status --json >"$evidence_dir/tailscale-status.json" 2>"$evidence_dir/tailscale-status.stderr"; then
  echo "unexpected enrolled Tailscale status in credential-free CI" >&2
  exit 1
fi

# A service started outside routerd is foreign. The generated artifact must
# reject it and leave it running for the fixture owner to stop.
service tailscaled onestart >"$evidence_dir/tailscaled-foreign-start.log" 2>&1
foreign_started=1
service tailscaled onestatus >"$evidence_dir/tailscaled-foreign-status.log" 2>&1
if "$script" onestart >"$evidence_dir/tailscale-foreign-refusal.log" 2>&1; then
  echo "generated Tailscale lifecycle adopted foreign tailscaled" >&2
  exit 1
fi
service tailscaled onestatus >"$evidence_dir/tailscaled-foreign-preserved.log" 2>&1
service tailscaled onestop >"$evidence_dir/tailscaled-foreign-stop.log" 2>&1
foreign_started=0
printf '%s\n' \
  'tailscale-generated-start-failure-unwinds-owned-service=ok' \
  'tailscale-enrollment=external-auth-required-actionable-nonzero' \
  'foreign-running-tailscaled=generated-refusal-preserved' >"$evidence_dir/summary.log"
printf 'freebsd-tailscale-boundary=ok\n' >"$evidence_dir/result"
