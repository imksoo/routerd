#!/usr/bin/env bash
#
# cloudedge-l2-loop-probe.sh - L2 loop / broadcast storm stability probe.
#
# Live labs provide L2_LOOP_RUNNER. This wrapper records before/after snapshots
# around a failover and evaluates loop-free acceptance assertions without
# mutating cloud or host network state by itself.
#
set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")

die() { printf '%s: %s\n' "$SELF" "$*" >&2; exit 1; }
log() { printf '%s %s\n' "[$(date -u +%H:%M:%SZ)]" "$*" >&2; }
have() { command -v "$1" >/dev/null 2>&1; }

usage() {
  cat <<EOF
$SELF - observe L2 loop / STP-RSTP stability

USAGE:
  $SELF --out <file> --phase before|after --provider onprem
        [--broadcast-threshold-pps 100] [--stp-tcn-threshold 5]
        [--ping-loss-threshold-percent 1]

ENV:
  L2_LOOP_RUNNER  Required runner. Contract:
    \$RUNNER observe <phase> <provider>

The runner should print key=value lines:
  broadcast_pps=<number>
  stp_tcn_delta=<TCN BPDU or TC-flag frames during the sample; excludes ordinary BPDU/ACK-only>\n  bpdu_count=<all sampled BPDUs; diagnostic only>
  mac_flap_count=<number>
  ping_loss_percent=<number>
  blocked_ports=<number>
  bpdu_seen=true|false
  mechanism=<vrrp-single-master|stp-blocking|bpdu-transparency|...>
  detail=<free form>

The desired state is loop-free: only the VRRP master performs SAM proxy-ARP
capture, non-masters fail closed, and any physical L2 redundancy is handled by
BPDU-transparent STP/RSTP blocking. This script records those observations.
EOF
}

kv_to_json() {
  local kv=$1
  python3 - "$kv" <<'PY'
import json, sys
out = {}
for line in sys.argv[1].splitlines():
    if "=" not in line:
        continue
    k, v = line.split("=", 1)
    k = k.strip().replace("-", "_")
    v = v.strip()
    if not k:
        continue
    if v.lower() in ("true", "false"):
        out[k] = v.lower() == "true"
        continue
    try:
        if v.isdigit():
            out[k] = int(v)
        else:
            out[k] = float(v)
    except ValueError:
        out[k] = v
print(json.dumps(out, sort_keys=True))
PY
}

out=""
phase=""
provider="onprem"
broadcast_threshold=100
stp_tcn_threshold=5
ping_loss_threshold=1

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out) out="${2:-}"; shift 2 ;;
    --phase) phase="${2:-}"; shift 2 ;;
    --provider) provider="${2:-}"; shift 2 ;;
    --broadcast-threshold-pps) broadcast_threshold="${2:-}"; shift 2 ;;
    --stp-tcn-threshold) stp_tcn_threshold="${2:-}"; shift 2 ;;
    --ping-loss-threshold-percent) ping_loss_threshold="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

[[ -n "$out" ]] || die "--out is required"
case "$phase" in before|after) ;; *) die "--phase must be before|after" ;; esac
case "$provider" in aws|azure|oci|onprem) ;; *) die "bad provider: $provider" ;; esac
[[ "$broadcast_threshold" =~ ^[0-9]+([.][0-9]+)?$ ]] || die "bad --broadcast-threshold-pps"
[[ "$stp_tcn_threshold" =~ ^[0-9]+([.][0-9]+)?$ ]] || die "bad --stp-tcn-threshold"
[[ "$ping_loss_threshold" =~ ^[0-9]+([.][0-9]+)?$ ]] || die "bad --ping-loss-threshold-percent"
have python3 || die "python3 is required"
[[ -n "${L2_LOOP_RUNNER:-}" && -x "${L2_LOOP_RUNNER:-}" ]] \
  || die "L2_LOOP_RUNNER must point to an executable runner"

mkdir -p "$(dirname "$out")"
log "l2-loop: observing phase=$phase provider=$provider"

attempt_dir=$(mktemp -d "$out.evidence.XXXXXXXX")
if [[ -f "$out" ]]; then cp -- "$out" "$attempt_dir/previous.json"; fi
observe_result="pass"
observe_exit=0
observe_output=""
observe_output=$("$L2_LOOP_RUNNER" observe "$phase" "$provider" 2>"$attempt_dir/stderr.txt") || observe_exit=$?
printf '%s\n' "$observe_output" >"$attempt_dir/stdout.txt"
[[ "$observe_exit" -eq 0 ]] || observe_result="fail"
metrics_json=$(kv_to_json "$observe_output")

python3 - "$out" "$phase" "$provider" "$observe_result" "$metrics_json" \
  "$broadcast_threshold" "$stp_tcn_threshold" "$ping_loss_threshold" "$observe_exit" "$attempt_dir" <<'PY'
import json, math, os, sys

(
    out, phase, provider, observe_result, metrics_s,
    broadcast_threshold_s, stp_tcn_threshold_s, ping_loss_threshold_s, observe_exit_s, attempt_dir,
) = sys.argv[1:]

metrics = json.loads(metrics_s)
broadcast_threshold = float(broadcast_threshold_s)
stp_tcn_threshold = float(stp_tcn_threshold_s)
ping_loss_threshold = float(ping_loss_threshold_s)

def num(name):
    value = metrics.get(name)
    if isinstance(value, bool):
        return None
    try:
        value = float(value)
        return value if math.isfinite(value) and value >= 0 else None
    except (ValueError, TypeError):
        return None

def passed(value):
    return "pass" if value else "fail"

def measured(name, predicate):
    value = num(name)
    return "inconclusive" if value is None else passed(predicate(value))

phase_checks = {
    "broadcastStormAbsent": measured("broadcast_pps", lambda value: value <= broadcast_threshold),
    "stpRstpStable": measured("stp_tcn_delta", lambda value: value <= stp_tcn_threshold),
    "macFlapAbsent": measured("mac_flap_count", lambda value: value == 0),
    "failoverPingStable": measured("ping_loss_percent", lambda value: value <= ping_loss_threshold),
}
if observe_result != "pass":
    phase_checks = {k: "inconclusive" for k in phase_checks}
phase_checks["suppressionMechanismRecorded"] = "pass" if isinstance(metrics.get("mechanism"), str) and metrics["mechanism"].strip() else "inconclusive"
def aggregate(values):
    values = list(values)
    return "fail" if "fail" in values else "inconclusive" if not values or "inconclusive" in values else "pass"
phase_result = aggregate(phase_checks.values())

try:
    data = json.load(open(out))
    if not isinstance(data, dict):
        raise ValueError("not an object")
except Exception:
    data = {}

thresholds = {"broadcastPps": broadcast_threshold, "stpTcnDelta": stp_tcn_threshold, "pingLossPercent": ping_loss_threshold}
# A before observation begins a new pair. Never reuse an old after snapshot.
previous = data.get("phases", []) if phase == "after" and data.get("thresholds") == thresholds else []
phases = [p for p in previous if p.get("phase") == "before" and p.get("provider") == provider]
phases.append({
    "phase": phase,
    "provider": provider,
    "result": phase_result,
    "checks": phase_checks,
    "metrics": metrics,
    "classification": "observation_inconclusive" if "inconclusive" in phase_checks.values() else "none" if phase_result == "pass" else "measured_threshold_failure",
    "observerExit": int(observe_exit_s),
    "stdout": attempt_dir + "/stdout.txt",
    "stderr": attempt_dir + "/stderr.txt",
})
phases.sort(key=lambda p: {"before": 0, "after": 1}.get(p.get("phase"), 99))

mechanisms = [
    str(p.get("metrics", {}).get("mechanism", "")).strip()
    for p in phases
    if str(p.get("metrics", {}).get("mechanism", "")).strip()
]
complete = [p.get("phase") for p in phases] == ["before", "after"]
summary = {name: aggregate([p.get("checks", {}).get(name, "inconclusive") for p in phases] + ([] if complete else ["inconclusive"])) for name in phase_checks}
data = {
    "status": aggregate(summary.values()),
    "classification": "measured_threshold_failure" if "fail" in summary.values() else "none" if complete and all(v == "pass" for v in summary.values()) else "observation_inconclusive",
    "pairComplete": complete,
    "phaseAcquisitionStatus": phase_result,
    "mechanism": ",".join(sorted(set(mechanisms))),
    "thresholds": thresholds,
    "phases": phases,
    "summary": summary,
}
with open(out, "w") as f:
    json.dump(data, f, indent=2, sort_keys=True)
    f.write("\n")
print(out)
PY

result=$(python3 - "$out" "$phase" <<'PY'
import json, sys
data = json.load(open(sys.argv[1]))
# Successful before acquisition permits the caller to proceed to failover.
# The persisted pair verdict stays inconclusive until after is acquired.
print(data.get("phaseAcquisitionStatus", "inconclusive") if sys.argv[2] == "before" else data.get("status", "inconclusive"))
PY
)
case "$result" in pass) exit 0 ;; fail) exit 1 ;; *) exit 3 ;; esac
