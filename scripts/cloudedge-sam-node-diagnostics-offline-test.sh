#!/usr/bin/env bash
#
# Offline smoke test for the CloudEdge SAM node diagnostics collector.
#
set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

die() { printf '%s: %s\n' "$SELF" "$*" >&2; exit 1; }

tmp=$(mktemp -d "${TMPDIR:-/tmp}/cloudedge-sam-node-diagnostics.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

env_file="$tmp/env.sh"
cat > "$env_file" <<'EOF'
export SSH_KEY_FILE=
export ONPREM_ROUTER_SSH_HOST=
export AWS_ROUTER_A_SSH_HOST=
export AWS_ROUTER_B_SSH_HOST=
export AZURE_ROUTER_SSH_HOST=
export AZURE_ROUTER_B_SSH_HOST=
export OCI_ROUTER_SSH_HOST=
export OCI_ROUTER_B_SSH_HOST=
EOF

out="$tmp/out"
"$SCRIPT_DIR/cloudedge-sam-node-diagnostics.sh" preflight \
  --env "$env_file" \
  --out "$out" \
  --label offline \
  --nodes onprem,aws-a >/dev/null

[[ -d "$out/provider" ]] || die "provider directory missing"
[[ -d "$out/nodes" ]] || die "nodes directory missing"
[[ -f "$out/nodes/onprem.log" ]] || die "onprem log missing"
[[ -f "$out/nodes/aws-a.log" ]] || die "aws-a log missing"
[[ -f "$out/summary/preflight-findings.txt" ]] || die "preflight summary missing"

grep -q 'missing host for onprem' "$out/nodes/onprem.log" || die "onprem missing-host marker absent"
grep -q 'missing host for aws-a' "$out/nodes/aws-a.log" || die "aws-a missing-host marker absent"
grep -q '# offline preflight findings' "$out/summary/preflight-findings.txt" || die "summary header absent"

printf 'cloudedge sam node diagnostics offline OK\n'
