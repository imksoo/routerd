#!/usr/bin/env bash
#
# Offline acceptance harness test for CloudEdge 4-site scaffolding.
#
# This intentionally does not touch cloud APIs. It stubs MATRIX_RUNNER and
# verifies the scenario catalog, directed matrix JSON, and evidence result shape.
#
set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)

die() { printf '%s: %s\n' "$SELF" "$*" >&2; exit 1; }

tmp=$(mktemp -d "${TMPDIR:-/tmp}/cloudedge-acceptance.XXXXXX")
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT

runner="$tmp/matrix-runner"
cat > "$runner" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
op=$1
src=$2
case "$src" in
  onprem) src_ip=10.77.60.10 ;;
  aws) src_ip=10.77.60.11 ;;
  azure) src_ip=10.77.60.12 ;;
  oci) src_ip=10.77.60.13 ;;
  *) exit 2 ;;
esac
case "$op" in
  ping) exit 0 ;;
  ssh)
    printf 'peer_ip=%s\n' "$src_ip"
    printf 'default_gw=10.77.60.1\n'
    ;;
  *) exit 2 ;;
esac
SH
chmod +x "$runner"

"$SCRIPT_DIR/cloudedge-acceptance.sh" lint >/dev/null

matrix_json="$tmp/connectivity-matrix.json"
MATRIX_RUNNER="$runner" "$SCRIPT_DIR/cloudedge-connectivity-matrix.sh" --out "$matrix_json" >/dev/null

python3 - "$matrix_json" <<'PY'
import json, sys
data = json.load(open(sys.argv[1]))
summary = data.get("summary", {})
if summary.get("total") != 12:
    raise SystemExit(f"matrix total = {summary.get('total')}, want 12")
if summary.get("passed") != 12 or summary.get("result") != "pass":
    raise SystemExit(f"matrix did not pass: {summary!r}")
for flow in data.get("flows", []):
    for field in ("ping", "sourceIpPreserved", "defaultGwUnchanged", "noNat", "result"):
        if flow.get(field) != "pass":
            raise SystemExit(f"flow {flow.get('src')}->{flow.get('dst')} {field}={flow.get(field)!r}")
PY

out="$tmp/evidence"
run_id="20260601T000000Z-cloudedge-d3-4site-directed-matrix"
MATRIX_RUNNER="$runner" CE_STATE_DIR="$tmp/state" \
  "$SCRIPT_DIR/cloudedge-acceptance.sh" run \
    --scenario d3-4site-directed-matrix \
    --out "$out" \
    --run-id "$run_id" \
    --commit offline \
    --result pass >/dev/null

python3 - "$out/result.json" "$REPO_ROOT/scripts/cloudedge-evidence-schema.json" <<'PY'
import json, sys
result = json.load(open(sys.argv[1]))
schema = json.load(open(sys.argv[2]))
try:
    import jsonschema
except Exception:
    jsonschema = None
if jsonschema is not None:
    jsonschema.validate(instance=result, schema=schema)
if set(result.get("providers", {})) != {"aws", "oci", "azure", "onprem"}:
    raise SystemExit("providers object is not normalized")
assertions = {a.get("name"): a.get("result") for a in result.get("assertions", [])}
for name in (
    "directed_ping_ssh_matrix",
    "source_ip_preserved",
    "default_gateway_unchanged",
    "no_nat",
):
    if assertions.get(name) != "pass":
        raise SystemExit(f"{name}={assertions.get(name)!r}, want pass")
for name in (
    "ownership_epoch_bumped",
    "allow_reassignment_maintained_until_success",
    "old_holder_residue_absent",
    "stale_action_fenced",
):
    if name not in assertions:
        raise SystemExit(f"missing assertion {name}")
PY

fail_runner="$tmp/matrix-runner-fail"
cat > "$fail_runner" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
op=$1
src=$2
dst_ip=$3
case "$src" in
  onprem) src_ip=10.77.60.10 ;;
  aws) src_ip=10.77.60.11 ;;
  azure) src_ip=10.77.60.12 ;;
  oci) src_ip=10.77.60.13 ;;
  *) exit 2 ;;
esac
if [[ "$op" == "ping" && "$src" == "onprem" && "$dst_ip" == "10.77.60.11" ]]; then
  exit 1
fi
case "$op" in
  ping) exit 0 ;;
  ssh)
    printf 'peer_ip=%s\n' "$src_ip"
    printf 'default_gw=10.77.60.1\n'
    ;;
  *) exit 2 ;;
esac
SH
chmod +x "$fail_runner"

fail_out="$tmp/evidence-fail"
set +e
MATRIX_RUNNER="$fail_runner" CE_STATE_DIR="$tmp/state-fail" \
  "$SCRIPT_DIR/cloudedge-acceptance.sh" run \
    --scenario d3-4site-directed-matrix \
    --out "$fail_out" \
    --run-id "$run_id" \
    --commit offline >/dev/null
fail_status=$?
set -e
if [[ "$fail_status" -eq 0 ]]; then
  die "failing matrix unexpectedly returned success"
fi
python3 - "$fail_out/result.json" <<'PY'
import json, sys
result = json.load(open(sys.argv[1]))
if result.get("result") != "fail":
    raise SystemExit(f"result={result.get('result')!r}, want fail")
assertions = {a.get("name"): a.get("result") for a in result.get("assertions", [])}
if assertions.get("directed_ping_ssh_matrix") != "fail":
    raise SystemExit("directed matrix failure was not preserved in evidence")
PY

printf 'cloudedge acceptance offline OK\n'
