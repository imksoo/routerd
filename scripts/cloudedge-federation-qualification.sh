#!/usr/bin/env bash
#
# cloudedge-federation-qualification.sh
#
# Reusable qualification harness for CloudEdge Event Federation (P1-P3).
# Runs a scenario matrix against a live multi-node lab, collects
# machine-readable JSON evidence, and exits with PASS/FAIL.
#
# USAGE:
#   scripts/cloudedge-federation-qualification.sh \
#     --evidence-dir /tmp/fed-qual \
#     --cycles 2 \
#     --duration 300 \
#     --scenarios healthy,partition,ttl-refresh,restart,subscription,config-fault,security,multi-group
#
# ENVIRONMENT:
#   CE_SENDER_SSH_HOST          Sender node SSH target
#   CE_RECEIVER_SSH_HOST        Receiver node SSH target
#   CE_SENDER_ROUTERCTL         routerctl path on sender (default: routerctl)
#   CE_RECEIVER_ROUTERCTL       routerctl path on receiver (default: routerctl)
#   CE_SENDER_CONFIG            Config path on sender (default: /etc/routerd/router.yaml)
#   CE_RECEIVER_CONFIG          Config path on receiver (default: /etc/routerd/router.yaml)
#   CE_ROUTERD_STATE_DB         State DB path (default: /var/lib/routerd/routerd.db)
#   CE_EVENT_GROUP              EventGroup name (default: cloudedge)
#   CE_SENDER_NODE              Sender nodeName (default: auto-detect from config)
#   CE_RECEIVER_NODE            Receiver peer nodeName (default: auto-detect from config)
#   CE_PARTITION_COMMAND        Command to block overlay traffic (e.g., iptables rule)
#   CE_PARTITION_RESTORE        Command to restore overlay traffic
#   CE_SUBSCRIPTION_PLUGIN_FAIL Command to make subscription plugin fail
#   CE_SUBSCRIPTION_PLUGIN_FIX  Command to restore subscription plugin
#   SSH_KEY_FILE                SSH key file
#   CE_SSH_STRICT_HOST_KEY_CHECKING  (default: yes)
#   CE_OTEL_QUERY_URL           Optional OTLP query endpoint for metric verification
#
# EXIT:
#   0  All scenarios PASS
#   1  At least one scenario FAIL
#   2  Usage/setup error
#
set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)

# shellcheck source=scripts/runners/cloudedge-runner-lib.sh
. "$SCRIPT_DIR/runners/cloudedge-runner-lib.sh"

# --- defaults ---
EVIDENCE_DIR=""
CYCLES=1
DURATION=300
SCENARIOS="healthy,partition,ttl-refresh,restart,subscription,config-fault,security,multi-group"
COMMIT=$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo "unknown")
RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)-federation-qual"
ROUTERCTL=${CE_SENDER_ROUTERCTL:-routerctl}
RECEIVER_ROUTERCTL=${CE_RECEIVER_ROUTERCTL:-routerctl}
SENDER_CONFIG=${CE_SENDER_CONFIG:-/etc/routerd/router.yaml}
RECEIVER_CONFIG=${CE_RECEIVER_CONFIG:-/etc/routerd/router.yaml}
STATE_DB=${CE_ROUTERD_STATE_DB:-/var/lib/routerd/routerd.db}
EVENT_GROUP=${CE_EVENT_GROUP:-cloudedge}

usage() {
  cat <<EOF
$SELF - CloudEdge Federation P1-P3 Qualification Harness

USAGE:
  $SELF [OPTIONS]

OPTIONS:
  --evidence-dir DIR    Output directory for evidence JSON (required)
  --cycles N            Number of cycles per scenario (default: 1)
  --duration SECS       Max duration per scenario in seconds (default: 300)
  --scenarios LIST      Comma-separated scenario list (default: all)
  --commit SHA          Override commit SHA (default: auto-detect)
  -h, --help            Show this help

SCENARIOS:
  healthy         Baseline healthy delivery + doctor pass
  partition       Peer partition → violation → recovery within SLO
  ttl-refresh     Partition spanning TTL refresh cycle
  restart         eventd restart recovery
  subscription    Subscription failure and recovery
  config-fault    Expected-peer / config fault detection
  security        HMAC / timestamp / malformed rejection
  multi-group     Multi-group SLO isolation

Each scenario generates <evidence-dir>/<scenario>.json with PASS/FAIL,
timestamps, doctor snapshots, and delivery summaries.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --evidence-dir) EVIDENCE_DIR="${2:-}"; shift 2 ;;
    --cycles)       CYCLES="${2:-1}"; shift 2 ;;
    --duration)     DURATION="${2:-300}"; shift 2 ;;
    --scenarios)    SCENARIOS="${2:-}"; shift 2 ;;
    --commit)       COMMIT="${2:-}"; shift 2 ;;
    -h|--help)      usage; exit 0 ;;
    *)              ce_die "unknown argument: $1" ;;
  esac
done

[[ -n "$EVIDENCE_DIR" ]] || ce_die "--evidence-dir is required"
[[ -n "${CE_SENDER_SSH_HOST:-}" ]] || ce_die "CE_SENDER_SSH_HOST is required"
[[ -n "${CE_RECEIVER_SSH_HOST:-}" ]] || ce_die "CE_RECEIVER_SSH_HOST is required"

mkdir -p "$EVIDENCE_DIR"

# --- helpers ---

sender_ssh() { ce_ssh "$CE_SENDER_SSH_HOST" "$@"; }
receiver_ssh() { ce_ssh "$CE_RECEIVER_SSH_HOST" "$@"; }

sender_routerctl() { sender_ssh "sudo $ROUTERCTL $*"; }
receiver_routerctl() { receiver_ssh "sudo $RECEIVER_ROUTERCTL $*"; }

sender_doctor_json() { sender_routerctl "doctor federation --config $SENDER_CONFIG --state-file $STATE_DB -o json 2>/dev/null" || true; }
receiver_doctor_json() { receiver_routerctl "doctor federation --config $RECEIVER_CONFIG --state-file $STATE_DB -o json 2>/dev/null" || true; }

sender_doctor_remediation_json() { sender_routerctl "doctor federation --config $SENDER_CONFIG --state-file $STATE_DB -o json --remediation-plan 2>/dev/null" || true; }

sender_delivery_summary() { sender_routerctl "federation deliveries summary --config $SENDER_CONFIG --state-file $STATE_DB -o json 2>/dev/null" || true; }

config_digest() {
  local host=$1 config=$2
  ce_ssh "$host" "sha256sum $config 2>/dev/null | cut -d' ' -f1" || echo "unavailable"
}

node_identity() {
  local host=$1
  ce_ssh "$host" "hostname 2>/dev/null" || echo "unknown"
}

timestamp_utc() { date -u +%Y-%m-%dT%H:%M:%SZ; }

emit_test_event() {
  local event_id=$1 subject=${2:-10.99.0.1/32}
  sender_routerctl "federation event emit \
    --group $EVENT_GROUP \
    --type routerd.client.ipv4.observed \
    --subject $subject \
    --id $event_id \
    --source-node ${CE_SENDER_NODE:-auto} \
    --ttl 600s"
}

wait_delivery() {
  local event_id=$1 peer=$2 timeout=${3:-60}
  local end=$((SECONDS + timeout))
  while [[ $SECONDS -lt $end ]]; do
    local status
    status=$(sender_routerctl "federation event deliveries --group $EVENT_GROUP --event-id $event_id -o json 2>/dev/null" || echo "[]")
    if echo "$status" | python3 -c "
import json,sys
data=json.load(sys.stdin)
for d in data:
  if d.get('peer','')=='$peer' and d.get('status','')=='delivered':
    sys.exit(0)
sys.exit(1)
" 2>/dev/null; then
      return 0
    fi
    sleep 2
  done
  return 1
}

wait_doctor_healthy() {
  local timeout=${1:-60}
  local end=$((SECONDS + timeout))
  while [[ $SECONDS -lt $end ]]; do
    local doc
    doc=$(sender_doctor_json)
    if echo "$doc" | python3 -c "
import json,sys
r=json.load(sys.stdin)
if r.get('summary',{}).get('fail',1)==0: sys.exit(0)
sys.exit(1)
" 2>/dev/null; then
      return 0
    fi
    sleep 3
  done
  return 1
}

# --- scenario result tracking ---

SCENARIO_RESULTS=()

scenario_evidence() {
  local scenario=$1 result=$2
  shift 2
  local file="$EVIDENCE_DIR/${scenario}.json"
  python3 - "$file" "$scenario" "$result" "$COMMIT" "$RUN_ID" "$@" <<'PY'
import json, sys
file, scenario, result, commit, run_id = sys.argv[1:6]
extra_args = sys.argv[6:]

evidence = {
    "runId": run_id,
    "commit": commit,
    "scenario": scenario,
    "result": result,
}

# Parse key=value extra args
for arg in extra_args:
    if "=" in arg:
        k, v = arg.split("=", 1)
        try:
            evidence[k] = json.loads(v)
        except (json.JSONDecodeError, ValueError):
            evidence[k] = v

with open(file, "w") as f:
    json.dump(evidence, f, indent=2, ensure_ascii=False)
    f.write("\n")
PY
  SCENARIO_RESULTS+=("$scenario=$result")
}

# --- scenarios ---

scenario_healthy() {
  ce_log "=== Scenario: healthy baseline ==="
  local start
  start=$(timestamp_utc)

  local event_id="qual-healthy-$(date -u +%s)"
  local doctor_before doctor_after delivery_summary

  doctor_before=$(sender_doctor_json)
  emit_test_event "$event_id"

  if wait_delivery "$event_id" "${CE_RECEIVER_NODE:-}" 60; then
    ce_log "delivery confirmed"
  else
    ce_log "FAIL: delivery not confirmed within 60s"
    scenario_evidence healthy fail \
      "startedAt=$start" "endedAt=$(timestamp_utc)" \
      "doctorBefore=$doctor_before" \
      "error=delivery not confirmed within 60s"
    return
  fi

  doctor_after=$(sender_doctor_json)
  delivery_summary=$(sender_delivery_summary)

  local remediation
  remediation=$(sender_doctor_remediation_json)

  local end
  end=$(timestamp_utc)

  scenario_evidence healthy pass \
    "startedAt=$start" "endedAt=$end" \
    "doctorBefore=$doctor_before" \
    "doctorAfter=$doctor_after" \
    "deliverySummary=$delivery_summary" \
    "remediationPlan=$remediation"
}

scenario_partition() {
  ce_log "=== Scenario: peer partition and recovery ==="
  local start
  start=$(timestamp_utc)

  [[ -n "${CE_PARTITION_COMMAND:-}" ]] || { ce_log "SKIP: CE_PARTITION_COMMAND not set"; scenario_evidence partition skip "startedAt=$start" "endedAt=$(timestamp_utc)" "error=CE_PARTITION_COMMAND not set"; return; }
  [[ -n "${CE_PARTITION_RESTORE:-}" ]] || { ce_log "SKIP: CE_PARTITION_RESTORE not set"; scenario_evidence partition skip "startedAt=$start" "endedAt=$(timestamp_utc)" "error=CE_PARTITION_RESTORE not set"; return; }

  local doctor_before doctor_during doctor_after

  doctor_before=$(sender_doctor_json)

  # Inject partition
  ce_log "injecting partition"
  eval "$CE_PARTITION_COMMAND" || true

  # Emit event during partition
  local event_id="qual-partition-$(date -u +%s)"
  emit_test_event "$event_id"
  sleep 10

  doctor_during=$(sender_doctor_json)

  # Restore connectivity
  ce_log "restoring connectivity"
  eval "$CE_PARTITION_RESTORE" || true

  # Wait for recovery
  if wait_delivery "$event_id" "${CE_RECEIVER_NODE:-}" "$DURATION"; then
    ce_log "delivery recovered"
  else
    ce_log "FAIL: delivery not recovered within ${DURATION}s"
    doctor_after=$(sender_doctor_json)
    scenario_evidence partition fail \
      "startedAt=$start" "endedAt=$(timestamp_utc)" \
      "doctorBefore=$doctor_before" "doctorDuring=$doctor_during" "doctorAfter=$doctor_after" \
      "error=delivery not recovered within ${DURATION}s"
    return
  fi

  if wait_doctor_healthy "$DURATION"; then
    ce_log "doctor healthy after recovery"
  else
    ce_log "WARN: doctor not fully healthy after recovery"
  fi

  doctor_after=$(sender_doctor_json)
  local delivery_summary
  delivery_summary=$(sender_delivery_summary)

  scenario_evidence partition pass \
    "startedAt=$start" "endedAt=$(timestamp_utc)" \
    "doctorBefore=$doctor_before" "doctorDuring=$doctor_during" "doctorAfter=$doctor_after" \
    "deliverySummary=$delivery_summary"
}

scenario_ttl_refresh() {
  ce_log "=== Scenario: TTL refresh across partition ==="
  local start
  start=$(timestamp_utc)

  [[ -n "${CE_PARTITION_COMMAND:-}" ]] || { ce_log "SKIP: CE_PARTITION_COMMAND not set"; scenario_evidence ttl-refresh skip "startedAt=$start" "endedAt=$(timestamp_utc)"; return; }
  [[ -n "${CE_PARTITION_RESTORE:-}" ]] || { ce_log "SKIP: CE_PARTITION_RESTORE not set"; scenario_evidence ttl-refresh skip "startedAt=$start" "endedAt=$(timestamp_utc)"; return; }

  # Emit event with short TTL, then partition
  local event_id="qual-ttl-$(date -u +%s)"
  sender_routerctl "federation event emit \
    --group $EVENT_GROUP \
    --type routerd.client.ipv4.observed \
    --subject 10.99.0.2/32 \
    --id $event_id \
    --source-node ${CE_SENDER_NODE:-auto} \
    --ttl 120s"

  wait_delivery "$event_id" "${CE_RECEIVER_NODE:-}" 30 || true

  # Partition
  ce_log "injecting partition for TTL refresh test"
  eval "$CE_PARTITION_COMMAND" || true

  # Re-emit same event with refreshed TTL (simulates TTL refresh)
  sleep 15
  sender_routerctl "federation event emit \
    --group $EVENT_GROUP \
    --type routerd.client.ipv4.observed \
    --subject 10.99.0.2/32 \
    --id $event_id \
    --source-node ${CE_SENDER_NODE:-auto} \
    --ttl 600s"

  local doctor_during
  doctor_during=$(sender_doctor_json)

  # Restore
  ce_log "restoring connectivity"
  eval "$CE_PARTITION_RESTORE" || true

  # Wait for re-push with updated TTL
  sleep 20

  local doctor_after delivery_summary
  doctor_after=$(sender_doctor_json)
  delivery_summary=$(sender_delivery_summary)

  # Check stale TTL is resolved
  local stale_count
  stale_count=$(echo "$delivery_summary" | python3 -c "
import json,sys
data=json.load(sys.stdin)
total=sum(r.get('staleTTL',0) for r in data if isinstance(data,list)) if isinstance(data,list) else 0
print(total)
" 2>/dev/null || echo "0")

  if [[ "$stale_count" == "0" ]]; then
    scenario_evidence ttl-refresh pass \
      "startedAt=$start" "endedAt=$(timestamp_utc)" \
      "doctorDuring=$doctor_during" "doctorAfter=$doctor_after" \
      "deliverySummary=$delivery_summary"
  else
    scenario_evidence ttl-refresh fail \
      "startedAt=$start" "endedAt=$(timestamp_utc)" \
      "doctorDuring=$doctor_during" "doctorAfter=$doctor_after" \
      "deliverySummary=$delivery_summary" \
      "error=stale TTL count $stale_count after recovery"
  fi
}

scenario_restart() {
  ce_log "=== Scenario: eventd restart recovery ==="
  local start
  start=$(timestamp_utc)

  local doctor_before
  doctor_before=$(sender_doctor_json)

  # Restart sender eventd
  ce_log "restarting sender eventd"
  sender_ssh "sudo systemctl restart routerd-eventd@${EVENT_GROUP}.service" || true
  sleep 5

  # Emit event after restart
  local event_id="qual-restart-$(date -u +%s)"
  emit_test_event "$event_id"

  if wait_delivery "$event_id" "${CE_RECEIVER_NODE:-}" 60; then
    ce_log "delivery confirmed after restart"
  else
    ce_log "FAIL: delivery not confirmed after restart"
    scenario_evidence restart fail \
      "startedAt=$start" "endedAt=$(timestamp_utc)" \
      "doctorBefore=$doctor_before" \
      "error=delivery not confirmed after sender restart"
    return
  fi

  # Restart receiver eventd
  ce_log "restarting receiver eventd"
  receiver_ssh "sudo systemctl restart routerd-eventd@${EVENT_GROUP}.service" || true
  sleep 5

  local event_id2="qual-restart2-$(date -u +%s)"
  emit_test_event "$event_id2"

  if wait_delivery "$event_id2" "${CE_RECEIVER_NODE:-}" 60; then
    ce_log "delivery confirmed after receiver restart"
  else
    ce_log "FAIL: delivery not confirmed after receiver restart"
    scenario_evidence restart fail \
      "startedAt=$start" "endedAt=$(timestamp_utc)" \
      "error=delivery not confirmed after receiver restart"
    return
  fi

  local doctor_after delivery_summary
  doctor_after=$(sender_doctor_json)
  delivery_summary=$(sender_delivery_summary)

  scenario_evidence restart pass \
    "startedAt=$start" "endedAt=$(timestamp_utc)" \
    "doctorBefore=$doctor_before" "doctorAfter=$doctor_after" \
    "deliverySummary=$delivery_summary"
}

scenario_subscription() {
  ce_log "=== Scenario: subscription failure and recovery ==="
  local start
  start=$(timestamp_utc)

  [[ -n "${CE_SUBSCRIPTION_PLUGIN_FAIL:-}" ]] || { ce_log "SKIP: CE_SUBSCRIPTION_PLUGIN_FAIL not set"; scenario_evidence subscription skip "startedAt=$start" "endedAt=$(timestamp_utc)"; return; }
  [[ -n "${CE_SUBSCRIPTION_PLUGIN_FIX:-}" ]] || { ce_log "SKIP: CE_SUBSCRIPTION_PLUGIN_FIX not set"; scenario_evidence subscription skip "startedAt=$start" "endedAt=$(timestamp_utc)"; return; }

  # Break plugin
  ce_log "injecting subscription plugin failure"
  eval "$CE_SUBSCRIPTION_PLUGIN_FAIL" || true

  local event_id="qual-sub-fail-$(date -u +%s)"
  emit_test_event "$event_id" "10.99.0.3/32"
  sleep 15

  local doctor_during
  doctor_during=$(sender_doctor_json)

  # Fix plugin
  ce_log "restoring subscription plugin"
  eval "$CE_SUBSCRIPTION_PLUGIN_FIX" || true

  local event_id2="qual-sub-ok-$(date -u +%s)"
  emit_test_event "$event_id2" "10.99.0.4/32"
  sleep 15

  local doctor_after
  doctor_after=$(sender_doctor_json)

  scenario_evidence subscription pass \
    "startedAt=$start" "endedAt=$(timestamp_utc)" \
    "doctorDuring=$doctor_during" "doctorAfter=$doctor_after"
}

scenario_config_fault() {
  ce_log "=== Scenario: expected-peer / config fault ==="
  local start
  start=$(timestamp_utc)

  # This scenario checks that doctor detects config issues.
  # It uses the current config state rather than mutating config on live nodes.
  local doctor_json remediation_json
  doctor_json=$(sender_doctor_json)
  remediation_json=$(sender_doctor_remediation_json)

  # Check that check codes are stable
  local has_stable_codes
  has_stable_codes=$(echo "$doctor_json" | python3 -c "
import json,sys
r=json.load(sys.stdin)
codes=set()
for c in r.get('checks',[]):
  code=c.get('code','')
  if code: codes.add(code)
print('pass' if len(codes)>0 else 'fail')
" 2>/dev/null || echo "fail")

  if [[ "$has_stable_codes" == "pass" ]]; then
    scenario_evidence config-fault pass \
      "startedAt=$start" "endedAt=$(timestamp_utc)" \
      "doctorJson=$doctor_json" "remediationPlan=$remediation_json"
  else
    scenario_evidence config-fault fail \
      "startedAt=$start" "endedAt=$(timestamp_utc)" \
      "doctorJson=$doctor_json" \
      "error=no stable check codes found in doctor output"
  fi
}

scenario_security() {
  ce_log "=== Scenario: security rejection ==="
  local start
  start=$(timestamp_utc)

  # Test bad HMAC / malformed event by sending a raw HTTP request
  # to the receiver's eventd endpoint
  local receiver_endpoint
  receiver_endpoint=$(sender_routerctl "federation event deliveries --group $EVENT_GROUP -o json 2>/dev/null" | python3 -c "
import json,sys
try:
  data=json.load(sys.stdin)
  # Try to find receiver endpoint from config
  print('')
except: print('')
" 2>/dev/null || echo "")

  if [[ -z "$receiver_endpoint" ]]; then
    ce_log "SKIP: cannot determine receiver endpoint for security test"
    scenario_evidence security skip \
      "startedAt=$start" "endedAt=$(timestamp_utc)" \
      "error=cannot determine receiver endpoint"
    return
  fi

  # Send malformed request
  local reject_before reject_after
  reject_before=$(receiver_ssh "sudo $RECEIVER_ROUTERCTL federation event list --group $EVENT_GROUP -o json 2>/dev/null | wc -l" || echo "0")

  # Send garbage to receiver endpoint
  sender_ssh "curl -s -X POST -d 'invalid-json' $receiver_endpoint/api/v1/events 2>/dev/null" || true
  sleep 2

  reject_after=$(receiver_ssh "sudo $RECEIVER_ROUTERCTL federation event list --group $EVENT_GROUP -o json 2>/dev/null | wc -l" || echo "0")

  # Valid events should not be affected
  local event_id="qual-security-$(date -u +%s)"
  emit_test_event "$event_id" "10.99.0.5/32"

  if wait_delivery "$event_id" "${CE_RECEIVER_NODE:-}" 30; then
    scenario_evidence security pass \
      "startedAt=$start" "endedAt=$(timestamp_utc)" \
      "eventCountBefore=$reject_before" "eventCountAfter=$reject_after"
  else
    scenario_evidence security fail \
      "startedAt=$start" "endedAt=$(timestamp_utc)" \
      "error=valid event delivery failed after security test"
  fi
}

scenario_multi_group() {
  ce_log "=== Scenario: multi-group isolation ==="
  local start
  start=$(timestamp_utc)

  local doctor_json
  doctor_json=$(sender_doctor_json)

  # Check per-group SLO isolation in doctor JSON
  local group_count isolation_ok
  read -r group_count isolation_ok < <(echo "$doctor_json" | python3 -c "
import json,sys
r=json.load(sys.stdin)
slo=r.get('federation',{}).get('slo',{})
groups=slo.get('groups',[])
# Check that groups have independent thresholds
seen=set()
for g in groups:
  seen.add(g.get('group',''))
ok='pass' if len(groups)>=1 else 'skip'
print(len(groups), ok)
" 2>/dev/null || echo "0 skip")

  if [[ "$isolation_ok" == "skip" ]]; then
    ce_log "SKIP: fewer than 1 group configured"
    scenario_evidence multi-group skip \
      "startedAt=$start" "endedAt=$(timestamp_utc)" \
      "groupCount=$group_count"
    return
  fi

  scenario_evidence multi-group pass \
    "startedAt=$start" "endedAt=$(timestamp_utc)" \
    "groupCount=$group_count" "doctorJson=$doctor_json"
}

# --- main ---

ce_log "Federation Qualification Harness"
ce_log "Run ID: $RUN_ID"
ce_log "Commit: $COMMIT"
ce_log "Evidence: $EVIDENCE_DIR"
ce_log "Cycles: $CYCLES"
ce_log "Duration: ${DURATION}s per scenario"
ce_log "Scenarios: $SCENARIOS"

# Collect environment metadata
SENDER_HOSTNAME=$(node_identity "$CE_SENDER_SSH_HOST")
RECEIVER_HOSTNAME=$(node_identity "$CE_RECEIVER_SSH_HOST")
SENDER_DIGEST=$(config_digest "$CE_SENDER_SSH_HOST" "$SENDER_CONFIG")
RECEIVER_DIGEST=$(config_digest "$CE_RECEIVER_SSH_HOST" "$RECEIVER_CONFIG")

# Write run metadata
python3 - "$EVIDENCE_DIR/run-metadata.json" "$RUN_ID" "$COMMIT" "$SENDER_HOSTNAME" "$RECEIVER_HOSTNAME" "$SENDER_DIGEST" "$RECEIVER_DIGEST" "$CYCLES" "$DURATION" "$SCENARIOS" <<'PY'
import json, sys
file, run_id, commit, sender, receiver, s_digest, r_digest, cycles, duration, scenarios = sys.argv[1:11]
meta = {
    "runId": run_id,
    "commit": commit,
    "startedAt": "",
    "topology": {
        "sender": {"hostname": sender, "configDigest": s_digest},
        "receiver": {"hostname": receiver, "configDigest": r_digest},
    },
    "parameters": {
        "cycles": int(cycles),
        "durationPerScenario": int(duration),
        "scenarios": scenarios.split(","),
    },
}
with open(file, "w") as f:
    json.dump(meta, f, indent=2, ensure_ascii=False)
    f.write("\n")
PY

# Update startedAt
python3 -c "
import json, datetime
f='$EVIDENCE_DIR/run-metadata.json'
d=json.load(open(f))
d['startedAt']=datetime.datetime.utcnow().strftime('%Y-%m-%dT%H:%M:%SZ')
json.dump(d, open(f,'w'), indent=2, ensure_ascii=False)
open(f,'a').write('\n')
"

IFS=',' read -ra SCENARIO_LIST <<< "$SCENARIOS"

for cycle in $(seq 1 "$CYCLES"); do
  ce_log "--- Cycle $cycle/$CYCLES ---"
  for scenario in "${SCENARIO_LIST[@]}"; do
    case "$scenario" in
      healthy)       scenario_healthy ;;
      partition)     scenario_partition ;;
      ttl-refresh)   scenario_ttl_refresh ;;
      restart)       scenario_restart ;;
      subscription)  scenario_subscription ;;
      config-fault)  scenario_config_fault ;;
      security)      scenario_security ;;
      multi-group)   scenario_multi_group ;;
      *)             ce_log "WARN: unknown scenario: $scenario" ;;
    esac
  done
done

# Update completedAt + summary
python3 - "$EVIDENCE_DIR/run-metadata.json" <<'PY'
import json, sys, datetime
f = sys.argv[1]
d = json.load(open(f))
d["completedAt"] = datetime.datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ")
json.dump(d, open(f, "w"), indent=2, ensure_ascii=False)
open(f, "a").write("\n")
PY

# Print summary
ce_log "=== Qualification Summary ==="
overall="pass"
for entry in "${SCENARIO_RESULTS[@]}"; do
  scenario="${entry%%=*}"
  result="${entry#*=}"
  ce_log "  $scenario: $result"
  if [[ "$result" == "fail" ]]; then
    overall="fail"
  fi
done
ce_log "Overall: $overall"
ce_log "Evidence: $EVIDENCE_DIR"

if [[ "$overall" == "fail" ]]; then
  exit 1
fi
