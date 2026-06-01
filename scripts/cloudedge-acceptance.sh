#!/usr/bin/env bash
#
# cloudedge-acceptance.sh - declarative CloudEdge 4-site acceptance runner.
#
# This script is deliberately a harness: it does not allocate cloud resources
# directly. It reads examples/cloudedge-acceptance-scenarios.json, delegates
# fault injection / smoke / evidence assembly to cloudedge-labctl.sh, and writes
# one schema-valid evidence bundle per scenario.
#
set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)

SCENARIOS_FILE="${CE_ACCEPTANCE_SCENARIOS:-$REPO_ROOT/examples/cloudedge-acceptance-scenarios.json}"
LABCTL="${CE_LABCTL:-$SCRIPT_DIR/cloudedge-labctl.sh}"

die() { printf '%s: %s\n' "$SELF" "$*" >&2; exit 1; }
log() { printf '%s %s\n' "[$(date -u +%H:%M:%SZ)]" "$*" >&2; }
have() { command -v "$1" >/dev/null 2>&1; }

usage() {
  cat <<EOF
$SELF - CloudEdge 4-site acceptance scenario runner

USAGE:
  $SELF list
  $SELF lint
  $SELF run --scenario <id> --out <dir> [--run-id <id>] [--commit <ref>]
            [--sites "onprem=ip,aws=ip,azure=ip,oci=ip"] [--result pass|fail]

ENV:
  CE_ACCEPTANCE_SCENARIOS  Scenario definition file
  CE_LABCTL                cloudedge-labctl.sh path
  MATRIX_RUNNER            Offline/real matrix runner passed through to labctl
  CE_DRY_RUN               Labctl mutation guard; defaults to labctl's safe dry mode

The runner is cloud-non-touching unless labctl is explicitly configured with
credentials and CE_DRY_RUN=0. Offline tests set MATRIX_RUNNER.
EOF
}

require_python() {
  have python3 || die "python3 is required for scenario lint/run"
}

scenario_py() {
  require_python
  python3 - "$SCENARIOS_FILE" "$@"
}

cmd_list() {
  scenario_py <<'PY'
import json, sys
path = sys.argv[1]
data = json.load(open(path))
for s in data.get("scenarios", []):
    print(f"{s.get('id','')}\t{s.get('demo','')}\t{s.get('description','')}")
PY
}

cmd_lint() {
  scenario_py <<'PY'
import json, sys
path = sys.argv[1]
data = json.load(open(path))
errors = []
if data.get("version") != 1:
    errors.append("version must be 1")
sites = data.get("sites")
if sites != ["onprem", "aws", "azure", "oci"]:
    errors.append("sites must be exactly [onprem, aws, azure, oci]")
if data.get("logicalSubnet") != "10.77.60.0/24":
    errors.append("logicalSubnet must be 10.77.60.0/24")
seen = set()
required = {
    "d3-4site-directed-matrix",
    "d5-aws-maintenance-drain-migration",
    "d6-azure-active-stop-seize",
    "d7-oci-active-stop-seize",
    "d8-onprem-vrrp-master-failover",
    "d9-cross-provider-simultaneous-capture-consistency",
    "d10-event-replay-restart-convergence",
}
valid_providers = {"onprem", "aws", "azure", "oci"}
valid_faults = {"none", "stop-active", "drain", "heartbeat-stop", "executor-fail", "stale-replay"}
for s in data.get("scenarios", []):
    sid = s.get("id")
    if not sid:
        errors.append("scenario missing id")
        continue
    if sid in seen:
        errors.append(f"duplicate scenario id: {sid}")
    seen.add(sid)
    providers = s.get("providers")
    if not providers or any(p not in valid_providers for p in providers):
        errors.append(f"{sid}: invalid providers {providers!r}")
    matrix = s.get("matrix", {})
    if matrix.get("name") != "d3" or matrix.get("expectedDirectedFlows") != 12:
        errors.append(f"{sid}: matrix must be d3 with 12 directed flows")
    fault = s.get("fault", {})
    ftype = fault.get("type")
    if ftype not in valid_faults:
        errors.append(f"{sid}: unsupported fault type {ftype!r}")
    fprovider = fault.get("provider", "")
    if fprovider:
        for p in fprovider.split(","):
            if p not in valid_providers:
                errors.append(f"{sid}: invalid fault provider {p!r}")
    assertions = set(s.get("expectedAssertions", []))
    for name in ("source_ip_preserved", "default_gateway_unchanged"):
        if name not in assertions:
            errors.append(f"{sid}: missing assertion {name}")
missing = required - seen
if missing:
    errors.append("missing scenarios: " + ",".join(sorted(missing)))
if errors:
    print("\n".join(errors), file=sys.stderr)
    sys.exit(1)
print(f"cloudedge acceptance scenarios OK ({len(seen)} scenarios)")
PY
}

scenario_json() {
  local scenario=$1
  scenario_py "$scenario" <<'PY'
import json, sys
path, wanted = sys.argv[1], sys.argv[2]
data = json.load(open(path))
for s in data.get("scenarios", []):
    if s.get("id") == wanted:
        print(json.dumps(s, separators=(",", ":")))
        sys.exit(0)
print(f"scenario not found: {wanted}", file=sys.stderr)
sys.exit(1)
PY
}

json_field() {
  local json=$1 field=$2
  python3 - "$json" "$field" <<'PY'
import json, sys
data = json.loads(sys.argv[1])
cur = data
for part in sys.argv[2].split("."):
    cur = cur.get(part, "") if isinstance(cur, dict) else ""
print(cur if cur is not None else "")
PY
}

cmd_run() {
  local scenario="" out="" run_id="" commit="" sites="" forced_result=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --scenario) scenario="${2:-}"; shift 2 ;;
      --out) out="${2:-}"; shift 2 ;;
      --run-id) run_id="${2:-}"; shift 2 ;;
      --commit) commit="${2:-}"; shift 2 ;;
      --sites) sites="${2:-}"; shift 2 ;;
      --result) forced_result="${2:-}"; shift 2 ;;
      -h|--help) usage; exit 0 ;;
      *) die "run: unknown argument: $1" ;;
    esac
  done
  [[ -n "$scenario" ]] || die "run: --scenario is required"
  [[ -n "$out" ]] || die "run: --out is required"
  [[ -x "$LABCTL" ]] || die "labctl not executable: $LABCTL"

  local spec fault_type fault_provider matrix_name matrix_json result_arg=()
  spec=$(scenario_json "$scenario")
  fault_type=$(json_field "$spec" fault.type)
  fault_provider=$(json_field "$spec" fault.provider)
  matrix_name=$(json_field "$spec" matrix.name)
  [[ "$matrix_name" == "d3" ]] || die "run: unsupported matrix $matrix_name"

  mkdir -p "$out"
  matrix_json="$out/connectivity-matrix.json"

  if [[ "$fault_type" != "none" ]]; then
    [[ -n "$fault_provider" ]] || die "run: scenario $scenario fault $fault_type lacks provider"
    log "injecting fault: provider=$fault_provider fault=$fault_type"
    local failover_args=(failover --provider "$fault_provider" --fault "$fault_type")
    [[ -n "$run_id" ]] && failover_args+=(--run-id "$run_id")
    "$LABCTL" "${failover_args[@]}"
  fi

  local smoke_args=(smoke --matrix "$matrix_name" --out "$matrix_json")
  [[ -n "$sites" ]] && smoke_args+=(--sites "$sites")
  log "running matrix: $matrix_name"
  local matrix_status=0
  "$LABCTL" "${smoke_args[@]}" || matrix_status=$?

  local evidence_args=(evidence collect --out "$out" --scenario "$scenario" --matrix-json "$matrix_json")
  [[ -n "$run_id" ]] && evidence_args+=(--run-id "$run_id")
  [[ -n "$commit" ]] && evidence_args+=(--commit "$commit")
  [[ -n "$forced_result" ]] && result_arg=(--result "$forced_result")
  log "collecting evidence: $out"
  "$LABCTL" "${evidence_args[@]}" "${result_arg[@]}"
  return "$matrix_status"
}

main() {
  local cmd="${1:-help}"
  if [[ $# -gt 0 ]]; then shift; fi
  case "$cmd" in
    list) cmd_list "$@" ;;
    lint) cmd_lint "$@" ;;
    run) cmd_run "$@" ;;
    help|-h|--help) usage ;;
    *) usage >&2; die "unknown command: $cmd" ;;
  esac
}

main "$@"
