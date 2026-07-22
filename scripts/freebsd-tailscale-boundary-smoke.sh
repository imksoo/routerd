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
started=0
cleanup() {
  rc=$?
  if [ "$started" -eq 1 ]; then
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
grep -F "'service' 'tailscaled' 'onestart'" "$script" >"$evidence_dir/render-service.log"
grep -F '/usr/local/bin/tailscale' "$script" >>"$evidence_dir/render-service.log"

service tailscaled onestart >"$evidence_dir/tailscaled-start.log" 2>&1
started=1
service tailscaled onestatus >"$evidence_dir/tailscaled-status.log" 2>&1
if "$script" onestart >"$evidence_dir/tailscale-up.log" 2>&1; then
  echo "unexpected authenticated Tailscale enrollment in credential-free CI" >&2
  exit 1
fi
if tailscale status --json >"$evidence_dir/tailscale-status.json" 2>"$evidence_dir/tailscale-status.stderr"; then
  echo "unexpected enrolled Tailscale status in credential-free CI" >&2
  exit 1
fi
printf '%s\n' \
  'tailscale-render-service-start-observe-stop=ok' \
  'tailscale-enrollment=external-auth-required-actionable-nonzero' \
  'foreign-running-tailscaled=refuse-before-mutation' >"$evidence_dir/summary.log"
printf 'freebsd-tailscale-boundary=ok\n' >"$evidence_dir/result"
