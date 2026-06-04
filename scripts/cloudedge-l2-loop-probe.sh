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
  stp_tcn_delta=<number>
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

observe_result="pass"
observe_output=""
if ! observe_output=$("$L2_LOOP_RUNNER" observe "$phase" "$provider" 2>&1); then
  observe_result="fail"
fi
metrics_json=$(kv_to_json "$observe_output")

python3 - "$out" "$phase" "$provider" "$observe_result" "$metrics_json" \
  "$broadcast_threshold" "$stp_tcn_threshold" "$ping_loss_threshold" <<'PY'
import json, os, sys

(
    out, phase, provider, observe_result, metrics_s,
    broadcast_threshold_s, stp_tcn_threshold_s, ping_loss_threshold_s,
) = sys.argv[1:]

metrics = json.loads(metrics_s)
broadcast_threshold = float(broadcast_threshold_s)
stp_tcn_threshold = float(stp_tcn_threshold_s)
ping_loss_threshold = float(ping_loss_threshold_s)

def num(name, default=0.0):
    value = metrics.get(name, default)
    try:
        return float(value)
    except Exception:
        return default

def passed(value):
    return "pass" if value else "fail"

phase_checks = {
    "broadcastStormAbsent": passed(num("broadcast_pps") <= broadcast_threshold),
    "stpRstpStable": passed(num("stp_tcn_delta") <= stp_tcn_threshold),
    "macFlapAbsent": passed(num("mac_flap_count") == 0),
    "failoverPingStable": passed(num("ping_loss_percent") <= ping_loss_threshold),
}
if observe_result != "pass":
    phase_checks = {k: "fail" for k in phase_checks}
phase_result = "pass" if all(v == "pass" for v in phase_checks.values()) else "fail"

try:
    data = json.load(open(out))
    if not isinstance(data, dict):
        raise ValueError("not an object")
except Exception:
    data = {}

phases = [p for p in data.get("phases", []) if p.get("phase") != phase]
phases.append({
    "phase": phase,
    "provider": provider,
    "result": phase_result,
    "checks": phase_checks,
    "metrics": metrics,
})
phases.sort(key=lambda p: {"before": 0, "after": 1}.get(p.get("phase"), 99))

mechanisms = [
    str(p.get("metrics", {}).get("mechanism", "")).strip()
    for p in phases
    if str(p.get("metrics", {}).get("mechanism", "")).strip()
]
summary = {
    "broadcastStormAbsent": passed(all(p.get("checks", {}).get("broadcastStormAbsent") == "pass" for p in phases)),
    "stpRstpStable": passed(all(p.get("checks", {}).get("stpRstpStable") == "pass" for p in phases)),
    "macFlapAbsent": passed(all(p.get("checks", {}).get("macFlapAbsent") == "pass" for p in phases)),
    "failoverPingStable": passed(all(p.get("checks", {}).get("failoverPingStable") == "pass" for p in phases)),
    "suppressionMechanismRecorded": passed(bool(mechanisms)),
}
data = {
    "status": "pass" if phases and all(v == "pass" for v in summary.values()) else "fail",
    "mechanism": ",".join(sorted(set(mechanisms))) if mechanisms else "",
    "thresholds": {
        "broadcastPps": broadcast_threshold,
        "stpTcnDelta": stp_tcn_threshold,
        "pingLossPercent": ping_loss_threshold,
    },
    "phases": phases,
    "summary": summary,
}
with open(out, "w") as f:
    json.dump(data, f, indent=2, sort_keys=True)
    f.write("\n")
print(out)
PY

result=$(python3 - "$out" <<'PY'
import json, sys
data = json.load(open(sys.argv[1]))
print(data.get("phases", [{}])[-1].get("result", "fail"))
PY
)
[[ "$result" == "pass" ]]
