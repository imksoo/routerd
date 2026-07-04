#!/usr/bin/env bash
set -euo pipefail

SELF=$(basename "$0")
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/cloudedge-evidence.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

die() {
  printf '%s: %s\n' "$SELF" "$*" >&2
  exit 1
}

env_file="$tmp/env"
cat >"$env_file" <<EOF
SSH_KEY_FILE=/tmp/nonexistent
SSH_USER=ubuntu
ROUTERCTL_BIN=/usr/local/sbin/routerctl
ONPREM_ROUTER_SSH_HOST=onprem.example
AWS_ROUTER_A_SSH_HOST=aws-a.example
AWS_ROUTER_B_SSH_HOST=aws-b.example
AZURE_ROUTER_SSH_HOST=azure.example
OCI_ROUTER_SSH_HOST=oci.example
EOF

out="$tmp/evidence"
ENV_FILE="$env_file" \
CE_EVIDENCE_DRY_RUN=1 \
CE_EVIDENCE_STAGE=after-failover \
CE_EVIDENCE_COLLECT_EVENTS=1 \
CE_EVIDENCE_DUMP_EVENT_DB=1 \
  "$ROOT/examples/cloudedge-mobility-demo/collect-evidence.sh" "$out" >/dev/null

for node in onprem aws-a aws-b azure oci; do
  file="$out/${node}.txt"
  [[ -f "$file" ]] || die "missing router evidence file: $file"
  grep -q -- "--- events" "$file" || die "$node missing events section"
  grep -q "routerctl get events --limit 2000 -o json" "$file" || die "$node missing all-topic events command"
  grep -q "routerctl get events --topic routerd.mobility.holder.transition --limit 2000 -o json" "$file" || die "$node missing transition-topic events command"
  grep -q -- "--- state-db-events" "$file" || die "$node missing state DB events section"
  grep -q "SELECT \\* FROM events" "$file" || die "$node missing events table dump command"
done

grep -q "Stage: after-failover" "$out/summary.md" || die "summary missing stage"
grep -q "Event retention check" "$out/summary.md" || die "summary missing retention reminder"

printf 'cloudedge evidence offline test passed\n'
