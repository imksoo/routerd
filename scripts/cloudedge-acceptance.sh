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
TIMING_SCRIPT="${CE_FAILOVER_TIMING:-$SCRIPT_DIR/cloudedge-failover-timing.sh}"
PROTOCOL_SCRIPT="${CE_PROTOCOL_PROBE:-$SCRIPT_DIR/cloudedge-protocol-probe.sh}"
L2_LOOP_SCRIPT="${CE_L2_LOOP_PROBE:-$SCRIPT_DIR/cloudedge-l2-loop-probe.sh}"

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
  CE_FAILOVER_TIMING       cloudedge-failover-timing.sh path
  CE_PROTOCOL_PROBE        cloudedge-protocol-probe.sh path
  CE_L2_LOOP_PROBE         cloudedge-l2-loop-probe.sh path
  MATRIX_RUNNER            Offline/real matrix runner passed through to labctl
  FAILOVER_TIMING_RUNNER   Offline/real auto-failover timing runner
  PROTOCOL_PROBE_RUNNER    Offline/real FTP/NFS/RPC/bulk/PMTU runner
  L2_LOOP_RUNNER           Offline/real L2 loop/STP-RSTP observation runner
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
    "d11-protocol-transparency",
    "d12-l2-loop-stp-stability",
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
    timing = s.get("timing", {})
    if timing.get("enabled"):
        if timing.get("provider") not in valid_providers:
            errors.append(f"{sid}: timing.provider invalid")
        if timing.get("fault") not in {"stop-active", "drain"}:
            errors.append(f"{sid}: timing.fault invalid")
        threshold = timing.get("thresholdSeconds")
        if not isinstance(threshold, int) or threshold <= 0:
            errors.append(f"{sid}: timing.thresholdSeconds must be a positive integer")
        if "failover_recovery_under_60s" not in assertions:
            errors.append(f"{sid}: missing timing assertion failover_recovery_under_60s")
    protocol = s.get("protocol", {})
    if protocol.get("enabled"):
        pairs = protocol.get("pairs")
        if not pairs or any("client" not in p or "server" not in p for p in pairs):
            errors.append(f"{sid}: protocol.pairs must contain client/server pairs")
        for name in ("protocol_transparency", "ftp_active_passive", "nfs_rpc", "bulk_transfer_pmtu", "protocol_source_ip_preserved", "protocol_no_nat"):
            if name not in assertions:
                errors.append(f"{sid}: missing protocol assertion {name}")
    l2 = s.get("l2Loop", {})
    if l2.get("enabled"):
        if l2.get("provider", "onprem") not in valid_providers:
            errors.append(f"{sid}: l2Loop.provider invalid")
        for field in ("broadcastThresholdPps", "stpTcnThreshold", "pingLossThresholdPercent"):
            value = l2.get(field)
            if not isinstance(value, (int, float)) or value < 0:
                errors.append(f"{sid}: l2Loop.{field} must be a non-negative number")
        for name in ("l2_loop_free", "broadcast_storm_absent", "stp_rstp_stable", "mac_flap_absent", "failover_ping_stable", "l2_suppression_mechanism_recorded"):
            if name not in assertions:
                errors.append(f"{sid}: missing L2 assertion {name}")
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
if isinstance(cur, bool):
    print("true" if cur else "false")
else:
    print(cur if cur is not None else "")
PY
}

json_protocol_pairs() {
  local json=$1
  python3 - "$json" <<'PY'
import json, sys
data = json.loads(sys.argv[1])
pairs = data.get("protocol", {}).get("pairs", [])
print(",".join(f"{p.get('client','')}:{p.get('server','')}" for p in pairs))
PY
}

json_l2_args() {
  local json=$1
  python3 - "$json" <<'PY'
import json, sys
data = json.loads(sys.argv[1])
l2 = data.get("l2Loop", {})
print(l2.get("provider", "onprem"))
print(l2.get("broadcastThresholdPps", 100))
print(l2.get("stpTcnThreshold", 5))
print(l2.get("pingLossThresholdPercent", 1))
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

  local spec fault_type fault_provider matrix_name matrix_json timing_json protocol_json l2_loop_json result_arg=()
  local timing_enabled timing_provider timing_fault timing_threshold protocol_enabled protocol_pairs protocol_bytes
  local l2_enabled l2_provider l2_broadcast_threshold l2_stp_tcn_threshold l2_ping_loss_threshold
  local -a l2_args=()
  spec=$(scenario_json "$scenario")
  fault_type=$(json_field "$spec" fault.type)
  fault_provider=$(json_field "$spec" fault.provider)
  matrix_name=$(json_field "$spec" matrix.name)
  timing_enabled=$(json_field "$spec" timing.enabled)
  timing_provider=$(json_field "$spec" timing.provider)
  timing_fault=$(json_field "$spec" timing.fault)
  timing_threshold=$(json_field "$spec" timing.thresholdSeconds)
  protocol_enabled=$(json_field "$spec" protocol.enabled)
  protocol_pairs=$(json_protocol_pairs "$spec")
  protocol_bytes=$(json_field "$spec" protocol.bytes)
  l2_enabled=$(json_field "$spec" l2Loop.enabled)
  readarray -t l2_args < <(json_l2_args "$spec")
  l2_provider=${l2_args[0]:-onprem}
  l2_broadcast_threshold=${l2_args[1]:-100}
  l2_stp_tcn_threshold=${l2_args[2]:-5}
  l2_ping_loss_threshold=${l2_args[3]:-1}
  [[ "$matrix_name" == "d3" ]] || die "run: unsupported matrix $matrix_name"

  mkdir -p "$out"
  matrix_json="$out/connectivity-matrix.json"
  timing_json="$out/failover-timing.json"
  protocol_json="$out/protocol-probe.json"
  l2_loop_json="$out/l2-loop-probe.json"

  local l2_status=0
  if [[ "$l2_enabled" == "true" ]]; then
    [[ -x "$L2_LOOP_SCRIPT" ]] || die "L2 loop probe script not executable: $L2_LOOP_SCRIPT"
    log "observing L2 loop baseline: provider=$l2_provider"
    "$L2_LOOP_SCRIPT" \
      --provider "$l2_provider" \
      --phase before \
      --broadcast-threshold-pps "$l2_broadcast_threshold" \
      --stp-tcn-threshold "$l2_stp_tcn_threshold" \
      --ping-loss-threshold-percent "$l2_ping_loss_threshold" \
      --out "$l2_loop_json" || l2_status=$?
  fi

  local timing_status=0
  if [[ "$timing_enabled" == "true" ]]; then
    [[ -x "$TIMING_SCRIPT" ]] || die "timing script not executable: $TIMING_SCRIPT"
    [[ -n "$timing_provider" ]] || die "run: scenario $scenario timing lacks provider"
    [[ -n "$timing_fault" ]] || die "run: scenario $scenario timing lacks fault"
    [[ -n "$timing_threshold" ]] || timing_threshold=60
    log "measuring auto-failover: provider=$timing_provider fault=$timing_fault threshold=${timing_threshold}s"
    "$TIMING_SCRIPT" \
      --provider "$timing_provider" \
      --fault "$timing_fault" \
      --threshold-seconds "$timing_threshold" \
      --out "$timing_json" || timing_status=$?
  elif [[ "$fault_type" != "none" ]]; then
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

  if [[ "$l2_enabled" == "true" ]]; then
    log "observing L2 loop post-failover: provider=$l2_provider"
    "$L2_LOOP_SCRIPT" \
      --provider "$l2_provider" \
      --phase after \
      --broadcast-threshold-pps "$l2_broadcast_threshold" \
      --stp-tcn-threshold "$l2_stp_tcn_threshold" \
      --ping-loss-threshold-percent "$l2_ping_loss_threshold" \
      --out "$l2_loop_json" || l2_status=$?
  fi

  local protocol_status=0
  if [[ "$protocol_enabled" == "true" ]]; then
    [[ -x "$PROTOCOL_SCRIPT" ]] || die "protocol script not executable: $PROTOCOL_SCRIPT"
    [[ -n "$protocol_pairs" ]] || die "run: scenario $scenario protocol lacks pairs"
    [[ -n "$protocol_bytes" ]] || protocol_bytes=104857600
    log "running protocol probe: pairs=$protocol_pairs bytes=$protocol_bytes"
    "$PROTOCOL_SCRIPT" --pairs "$protocol_pairs" --bytes "$protocol_bytes" --out "$protocol_json" || protocol_status=$?
  fi

  local evidence_args=(evidence collect --out "$out" --scenario "$scenario" --matrix-json "$matrix_json")
  [[ -f "$timing_json" ]] && evidence_args+=(--timing-json "$timing_json")
  [[ -f "$protocol_json" ]] && evidence_args+=(--protocol-json "$protocol_json")
  [[ -f "$l2_loop_json" ]] && evidence_args+=(--l2-loop-json "$l2_loop_json")
  [[ -n "$run_id" ]] && evidence_args+=(--run-id "$run_id")
  [[ -n "$commit" ]] && evidence_args+=(--commit "$commit")
  [[ -n "$forced_result" ]] && result_arg=(--result "$forced_result")
  log "collecting evidence: $out"
  "$LABCTL" "${evidence_args[@]}" "${result_arg[@]}"
  if [[ "$timing_status" -ne 0 || "$matrix_status" -ne 0 || "$protocol_status" -ne 0 || "$l2_status" -ne 0 ]]; then
    return 1
  fi
  return 0
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
