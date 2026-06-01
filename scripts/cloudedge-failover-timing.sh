#!/usr/bin/env bash
#
# cloudedge-failover-timing.sh - auto-failover timing probe for CloudEdge labs.
#
# The script itself does not call cloud provider APIs. Live labs provide
# FAILOVER_TIMING_RUNNER, which performs the provider-specific VM/node stop and
# observes routerd state. Offline tests provide a stub runner with the same
# contract.
#
set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")

die() { printf '%s: %s\n' "$SELF" "$*" >&2; exit 1; }
log() { printf '%s %s\n' "[$(date -u +%H:%M:%SZ)]" "$*" >&2; }
have() { command -v "$1" >/dev/null 2>&1; }

usage() {
  cat <<EOF
$SELF - measure CloudEdge automatic failover convergence

USAGE:
  $SELF --provider <aws|azure|oci|onprem> --fault <stop-active|drain>
        --out <file> [--threshold-seconds 60] [--timeout-seconds 120]
        [--poll-seconds 2]

ENV:
  FAILOVER_TIMING_RUNNER  Required runner. Contract:
    \$RUNNER inject  <provider> <fault>
    \$RUNNER observe <provider> detection
    \$RUNNER observe <provider> switchover
    \$RUNNER observe <provider> recovery
    \$RUNNER detail  <provider> <stage>    # optional

The runner must inject a real lab fault (normally VM/node stop, not a process
kill) and observe the daemon-driven convergence path. This script only measures
and serializes timings; it never manually approves or executes provider actions.
EOF
}

now_ms() {
  python3 - <<'PY'
import time
print(int(time.time() * 1000))
PY
}

json_escape() {
  python3 - "$1" <<'PY'
import json, sys
print(json.dumps(sys.argv[1]))
PY
}

run_detail() {
  local provider=$1 stage=$2
  if [[ -z "${FAILOVER_TIMING_RUNNER:-}" ]]; then
    printf ''
    return 0
  fi
  "$FAILOVER_TIMING_RUNNER" detail "$provider" "$stage" 2>/dev/null || true
}

wait_stage() {
  local provider=$1 stage=$2 start_ms=$3 timeout_seconds=$4 poll_seconds=$5
  local deadline_ms now detail
  deadline_ms=$((start_ms + timeout_seconds * 1000))
  while true; do
    if "$FAILOVER_TIMING_RUNNER" observe "$provider" "$stage" >/dev/null 2>&1; then
      now=$(now_ms)
      detail=$(run_detail "$provider" "$stage")
      printf '%s\tpass\t%s\n' "$now" "$detail"
      return 0
    fi
    now=$(now_ms)
    if [[ "$now" -ge "$deadline_ms" ]]; then
      detail=$(run_detail "$provider" "$stage")
      printf '%s\tfail\t%s\n' "$now" "$detail"
      return 1
    fi
    sleep "$poll_seconds"
  done
}

provider=""
fault=""
out=""
threshold_seconds=60
timeout_seconds=120
poll_seconds=2

while [[ $# -gt 0 ]]; do
  case "$1" in
    --provider) provider="${2:-}"; shift 2 ;;
    --fault) fault="${2:-}"; shift 2 ;;
    --out) out="${2:-}"; shift 2 ;;
    --threshold-seconds) threshold_seconds="${2:-}"; shift 2 ;;
    --timeout-seconds) timeout_seconds="${2:-}"; shift 2 ;;
    --poll-seconds) poll_seconds="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

[[ -n "$provider" ]] || die "--provider is required"
[[ -n "$fault" ]] || die "--fault is required"
[[ -n "$out" ]] || die "--out is required"
case "$provider" in aws|azure|oci|onprem) ;; *) die "bad provider: $provider" ;; esac
case "$fault" in stop-active|drain) ;; *) die "bad fault: $fault" ;; esac
[[ "$threshold_seconds" =~ ^[0-9]+$ && "$threshold_seconds" -gt 0 ]] || die "bad --threshold-seconds"
[[ "$timeout_seconds" =~ ^[0-9]+$ && "$timeout_seconds" -gt 0 ]] || die "bad --timeout-seconds"
[[ "$poll_seconds" =~ ^[0-9]+$ && "$poll_seconds" -gt 0 ]] || die "bad --poll-seconds"
have python3 || die "python3 is required"
[[ -n "${FAILOVER_TIMING_RUNNER:-}" && -x "${FAILOVER_TIMING_RUNNER:-}" ]] \
  || die "FAILOVER_TIMING_RUNNER must point to an executable runner"

mkdir -p "$(dirname "$out")"

start_ms=$(now_ms)
log "timing: injecting provider=$provider fault=$fault"
inject_result="pass"
inject_detail=""
if ! inject_detail=$("$FAILOVER_TIMING_RUNNER" inject "$provider" "$fault" 2>&1); then
  inject_result="fail"
fi

detection_ms=$start_ms
switchover_ms=$start_ms
recovery_ms=$start_ms
detection_result="fail"
switchover_result="fail"
recovery_result="fail"
detection_detail=""
switchover_detail=""
recovery_detail=""

if [[ "$inject_result" == "pass" ]]; then
  IFS=$'\t' read -r detection_ms detection_result detection_detail < <(wait_stage "$provider" detection "$start_ms" "$timeout_seconds" "$poll_seconds" || true)
  if [[ "$detection_result" == "pass" ]]; then
    IFS=$'\t' read -r switchover_ms switchover_result switchover_detail < <(wait_stage "$provider" switchover "$detection_ms" "$timeout_seconds" "$poll_seconds" || true)
  fi
  if [[ "$switchover_result" == "pass" ]]; then
    IFS=$'\t' read -r recovery_ms recovery_result recovery_detail < <(wait_stage "$provider" recovery "$start_ms" "$timeout_seconds" "$poll_seconds" || true)
  fi
fi

python3 - "$out" \
  "$provider" "$fault" "$threshold_seconds" "$timeout_seconds" \
  "$start_ms" "$detection_ms" "$switchover_ms" "$recovery_ms" \
  "$inject_result" "$detection_result" "$switchover_result" "$recovery_result" \
  "$inject_detail" "$detection_detail" "$switchover_detail" "$recovery_detail" <<'PY'
import json, sys

(
    out, provider, fault, threshold_s, timeout_s,
    start_ms, detection_ms, switchover_ms, recovery_ms,
    inject_result, detection_result, switchover_result, recovery_result,
    inject_detail, detection_detail, switchover_detail, recovery_detail,
) = sys.argv[1:]

threshold = float(threshold_s)
start = int(start_ms)
detection = int(detection_ms)
switchover = int(switchover_ms)
recovery = int(recovery_ms)

def seconds(delta_ms):
    return round(delta_ms / 1000.0, 3)

detection_seconds = seconds(max(0, detection - start))
switchover_seconds = seconds(max(0, switchover - detection))
recovery_seconds = seconds(max(0, recovery - start))
under = recovery_result == "pass" and recovery_seconds < threshold
overall = (
    inject_result == detection_result == switchover_result == recovery_result == "pass"
    and under
)
data = {
    "status": "pass" if overall else "fail",
    "thresholdSeconds": threshold,
    "timeoutSeconds": int(timeout_s),
    "events": [
        {
            "provider": provider,
            "fault": fault,
            "detectionSeconds": detection_seconds,
            "switchoverSeconds": switchover_seconds,
            "recoverySeconds": recovery_seconds,
            "recoveryUnderThreshold": "pass" if under else "fail",
            "stages": {
                "inject": {"result": inject_result, "detail": inject_detail},
                "detection": {"result": detection_result, "detail": detection_detail},
                "switchover": {"result": switchover_result, "detail": switchover_detail},
                "recovery": {"result": recovery_result, "detail": recovery_detail},
            },
        }
    ],
}
with open(out, "w") as f:
    json.dump(data, f, indent=2, sort_keys=True)
    f.write("\n")
print(out)
PY

result=$(python3 - "$out" <<'PY'
import json, sys
print(json.load(open(sys.argv[1])).get("status", "fail"))
PY
)
[[ "$result" == "pass" ]]
