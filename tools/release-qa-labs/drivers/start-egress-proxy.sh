#!/usr/bin/env bash
set -euo pipefail
umask 077

[ "$#" -eq 1 ] || { echo "Usage: $(basename "$0") RUN_ID" >&2; exit 2; }
run_id="$1"
[[ "$run_id" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$ ]] || exit 2
run_root="/var/lib/routerd-release-qa/$run_id"
run_env="$run_root/runtime/run.env.json"
framework="$run_root/repo/tools/release-qa-labs"
[ "$(stat -c '%U:%a' "$run_env")" = "routerd-release-qa:600" ] || {
  echo "release QA egress proxy: unsafe run environment" >&2; exit 2;
}
endpoint="$(jq -er '.httpsProxy' "$run_env")"
[[ "$endpoint" =~ ^http://127\.0\.0\.1:([0-9]{4,5})$ ]] || {
  echo "release QA egress proxy: httpsProxy must be an IPv4 loopback endpoint" >&2; exit 2;
}
port="${BASH_REMATCH[1]}"
[ "$port" -ge 1024 ] && [ "$port" -le 65535 ] || exit 2
exec "$framework/egress_proxy.py" --run-root "$run_root" --port "$port"
