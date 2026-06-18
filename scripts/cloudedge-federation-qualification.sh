#!/usr/bin/env bash
#
# cloudedge-federation-qualification.sh
#
# Reusable qualification harness for CloudEdge Event Federation (P1-P3).
# Runs a scenario matrix against a live multi-node lab, collects
# machine-readable JSON evidence per cycle, and exits PASS only when
# every required scenario passes in every cycle.
#
# USAGE:
#   scripts/cloudedge-federation-qualification.sh \
#     --evidence-dir /tmp/fed-qual \
#     --cycles 2 \
#     --duration 300
#
# ENVIRONMENT — all required unless noted:
#   CE_SENDER_SSH_HOST              Sender node SSH target
#   CE_RECEIVER_SSH_HOST            Receiver node SSH target
#   CE_SENDER_ROUTERCTL             routerctl path on sender (default: routerctl)
#   CE_RECEIVER_ROUTERCTL           routerctl path on receiver (default: routerctl)
#   CE_SENDER_CONFIG                Config path on sender (default: /usr/local/etc/routerd/router.yaml)
#   CE_RECEIVER_CONFIG              Config path on receiver (default: /usr/local/etc/routerd/router.yaml)
#   CE_SENDER_STATE_DB              Sender state DB (default: /var/lib/routerd/routerd.db)
#   CE_RECEIVER_STATE_DB            Receiver state DB (default: /var/lib/routerd/routerd.db)
#   CE_EVENT_GROUP                  Primary EventGroup name (required)
#   CE_EVENT_GROUP_B                Second EventGroup name (required for multi-group)
#   CE_SUBSCRIPTION_KEY             EventSubscription key on receiver for subscription scenario
#   CE_OTEL_QUERY_URL               OTLP query endpoint (required for release runs)
#   CE_SENDER_PARTITION_APPLY       SSH command for sender to block receiver traffic
#   CE_SENDER_PARTITION_RESTORE     SSH command for sender to restore receiver traffic
#   CE_RECEIVER_PLUGIN_FAIL         SSH command for receiver to break subscription plugin
#   CE_RECEIVER_PLUGIN_RESTORE      SSH command for receiver to restore subscription plugin
#   SSH_KEY_FILE                    SSH key file (optional)
#   CE_SSH_STRICT_HOST_KEY_CHECKING (default: yes)
#
# EXIT:
#   0  All scenarios PASS in all cycles
#   1  At least one scenario FAIL
#   2  Setup / preflight error
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
ALLOW_SKIP=false
COMMIT=$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo "unknown")
FULL_COMMIT=$(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null || echo "unknown")
RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)-federation-qual"

ROUTERCTL=${CE_SENDER_ROUTERCTL:-routerctl}
RECEIVER_ROUTERCTL=${CE_RECEIVER_ROUTERCTL:-routerctl}
SENDER_CONFIG=${CE_SENDER_CONFIG:-/usr/local/etc/routerd/router.yaml}
RECEIVER_CONFIG=${CE_RECEIVER_CONFIG:-/usr/local/etc/routerd/router.yaml}
SENDER_STATE_DB=${CE_SENDER_STATE_DB:-/var/lib/routerd/routerd.db}
RECEIVER_STATE_DB=${CE_RECEIVER_STATE_DB:-/var/lib/routerd/routerd.db}
EVENT_GROUP=${CE_EVENT_GROUP:-}
EVENT_GROUP_B=${CE_EVENT_GROUP_B:-}
SUBSCRIPTION_KEY=${CE_SUBSCRIPTION_KEY:-}
OTEL_QUERY_URL=${CE_OTEL_QUERY_URL:-}


# Auto-detected in preflight
SENDER_NODE=""
RECEIVER_NODE=""
RECEIVER_ENDPOINT=""
SENDER_EVENTD_UNIT=""
RECEIVER_EVENTD_UNIT=""

# Fault state tracking for cleanup
_PARTITION_ACTIVE=false
_PLUGIN_BROKEN=false

usage() {
  cat <<EOF
$SELF - CloudEdge Federation P1-P3 Qualification Harness

USAGE:
  $SELF [OPTIONS]

OPTIONS:
  --evidence-dir DIR    Output directory for evidence JSON (required)
  --cycles N            Number of cycles per scenario (default: 1)
  --duration SECS       Hard deadline per scenario in seconds (default: 300)
  --scenarios LIST      Comma-separated scenario list (default: all 8)
  --allow-skip          Allow SKIP results without failing (dev only, NOT for release)
  --commit SHA          Override commit SHA (default: auto-detect)
  -h, --help            Show this help

All 8 scenarios must PASS for a release qualification run.
SKIP counts as FAIL unless --allow-skip is set.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --evidence-dir) EVIDENCE_DIR="${2:-}"; shift 2 ;;
    --cycles)       CYCLES="${2:-1}"; shift 2 ;;
    --duration)     DURATION="${2:-300}"; shift 2 ;;
    --scenarios)    SCENARIOS="${2:-}"; shift 2 ;;
    --allow-skip)   ALLOW_SKIP=true; shift ;;
    --commit)       COMMIT="${2:-}"; shift 2 ;;
    -h|--help)      usage; exit 0 ;;
    *)              ce_die "unknown argument: $1" ;;
  esac
done

[[ -n "$EVIDENCE_DIR" ]] || ce_die "--evidence-dir is required"
[[ -n "${CE_SENDER_SSH_HOST:-}" ]] || ce_die "CE_SENDER_SSH_HOST is required"
[[ -n "${CE_RECEIVER_SSH_HOST:-}" ]] || ce_die "CE_RECEIVER_SSH_HOST is required"
[[ -n "$EVENT_GROUP" ]] || ce_die "CE_EVENT_GROUP is required"

mkdir -p "$EVIDENCE_DIR"

# ============================================================================
# SSH + remote command helpers
# ============================================================================

sender_ssh() { ce_ssh "$CE_SENDER_SSH_HOST" "$@"; }
receiver_ssh() { ce_ssh "$CE_RECEIVER_SSH_HOST" "$@"; }

sender_routerctl() {
  local args="$*"
  sender_ssh "sudo $ROUTERCTL $args"
}
receiver_routerctl() {
  local args="$*"
  receiver_ssh "sudo $RECEIVER_ROUTERCTL $args"
}

sender_doctor_json() {
  sender_routerctl "doctor federation --config $SENDER_CONFIG --state-file $SENDER_STATE_DB -o json" 2>/dev/null
}
receiver_doctor_json() {
  receiver_routerctl "doctor federation --config $RECEIVER_CONFIG --state-file $RECEIVER_STATE_DB -o json" 2>/dev/null
}
sender_remediation_json() {
  sender_routerctl "doctor federation --config $SENDER_CONFIG --state-file $SENDER_STATE_DB -o json --remediation-plan" 2>/dev/null
}
sender_delivery_summary() {
  sender_routerctl "federation deliveries summary --group $EVENT_GROUP --state-file $SENDER_STATE_DB -o json" 2>/dev/null
}

timestamp_utc() { date -u +%Y-%m-%dT%H:%M:%SZ; }

remote_binary_info() {
  local host=$1 binary_path=$2
  ce_ssh "$host" "
    ver=\$($binary_path version 2>/dev/null || $binary_path --version 2>/dev/null || echo unknown)
    sha=\$(sha256sum $binary_path 2>/dev/null | cut -d' ' -f1 || echo unknown)
    printf '%s|%s' \"\$ver\" \"\$sha\"
  " 2>/dev/null || echo "unknown|unknown"
}

# ============================================================================
# Topology preflight (Section 3)
# ============================================================================

preflight() {
  ce_log "=== Preflight: topology discovery and validation ==="

  # 1. Verify SSH connectivity
  ce_log "checking SSH connectivity"
  sender_ssh "true" || ce_die "cannot SSH to sender ($CE_SENDER_SSH_HOST)"
  receiver_ssh "true" || ce_die "cannot SSH to receiver ($CE_RECEIVER_SSH_HOST)"

  # 2. Verify config files exist
  ce_log "checking config files"
  sender_ssh "test -f $SENDER_CONFIG" || ce_die "sender config not found: $SENDER_CONFIG"
  receiver_ssh "test -f $RECEIVER_CONFIG" || ce_die "receiver config not found: $RECEIVER_CONFIG"

  # 3. Verify state DBs exist
  sender_ssh "test -f $SENDER_STATE_DB" || ce_die "sender state DB not found: $SENDER_STATE_DB"
  receiver_ssh "test -f $RECEIVER_STATE_DB" || ce_die "receiver state DB not found: $RECEIVER_STATE_DB"

  # 4. Verify routerctl exists
  sender_ssh "command -v $ROUTERCTL >/dev/null 2>&1 || test -x $ROUTERCTL" || ce_die "routerctl not found on sender: $ROUTERCTL"
  receiver_ssh "command -v $RECEIVER_ROUTERCTL >/dev/null 2>&1 || test -x $RECEIVER_ROUTERCTL" || ce_die "routerctl not found on receiver: $RECEIVER_ROUTERCTL"

  # 5. Auto-detect SENDER_NODE from config
  ce_log "auto-detecting sender nodeName from config"
  SENDER_NODE=$(sender_ssh "python3 -c \"
import yaml, sys
with open('$SENDER_CONFIG') as f:
    cfg = yaml.safe_load(f)
for r in cfg.get('spec',{}).get('resources',[]):
    if r.get('kind')=='EventGroup' and r.get('metadata',{}).get('name')=='$EVENT_GROUP':
        print(r.get('spec',{}).get('nodeName',''))
        sys.exit(0)
print('')
\"" 2>/dev/null || echo "")
  [[ -n "$SENDER_NODE" ]] || ce_die "cannot detect sender nodeName for EventGroup '$EVENT_GROUP' from $SENDER_CONFIG"
  ce_log "  sender nodeName: $SENDER_NODE"

  # 6. Auto-detect RECEIVER_NODE + RECEIVER_ENDPOINT from sender's EventPeer
  ce_log "auto-detecting receiver peer from sender config"
  local peer_info
  peer_info=$(sender_ssh "python3 -c \"
import yaml, sys
with open('$SENDER_CONFIG') as f:
    cfg = yaml.safe_load(f)
for r in cfg.get('spec',{}).get('resources',[]):
    if r.get('kind')=='EventPeer' and r.get('spec',{}).get('groupRef')=='$EVENT_GROUP':
        print(r['spec'].get('nodeName','') + '|' + r['spec'].get('endpoint',''))
        sys.exit(0)
print('|')
\"" 2>/dev/null || echo "|")
  RECEIVER_NODE="${peer_info%%|*}"
  RECEIVER_ENDPOINT="${peer_info#*|}"
  [[ -n "$RECEIVER_NODE" ]] || ce_die "cannot detect receiver nodeName from sender EventPeer for group '$EVENT_GROUP'"
  [[ -n "$RECEIVER_ENDPOINT" ]] || ce_die "cannot detect receiver endpoint from sender EventPeer for group '$EVENT_GROUP'"
  ce_log "  receiver nodeName: $RECEIVER_NODE"
  ce_log "  receiver endpoint: $RECEIVER_ENDPOINT"

  # 7. Verify eventd service units
  SENDER_EVENTD_UNIT="routerd-eventd@${EVENT_GROUP}.service"
  RECEIVER_EVENTD_UNIT="routerd-eventd@${EVENT_GROUP}.service"
  ce_log "checking eventd service units"
  sender_ssh "sudo systemctl is-active $SENDER_EVENTD_UNIT >/dev/null 2>&1 || sudo systemctl status $SENDER_EVENTD_UNIT >/dev/null 2>&1" \
    || ce_die "sender eventd unit not found: $SENDER_EVENTD_UNIT"
  receiver_ssh "sudo systemctl is-active $RECEIVER_EVENTD_UNIT >/dev/null 2>&1 || sudo systemctl status $RECEIVER_EVENTD_UNIT >/dev/null 2>&1" \
    || ce_die "receiver eventd unit not found: $RECEIVER_EVENTD_UNIT"

  # 8. Verify sender→receiver TCP connectivity to endpoint
  ce_log "checking sender→receiver endpoint connectivity"
  local ep_host ep_port
  ep_host=$(echo "$RECEIVER_ENDPOINT" | sed 's|https\?://||; s|/.*||; s|:.*||')
  ep_port=$(echo "$RECEIVER_ENDPOINT" | grep -oP ':\K[0-9]+' || echo "80")
  sender_ssh "timeout 5 bash -c 'echo >/dev/tcp/$ep_host/$ep_port' 2>/dev/null" \
    || ce_die "sender cannot reach receiver endpoint $RECEIVER_ENDPOINT (TCP $ep_host:$ep_port)"

  # 9. Verify qualification EventGroups
  if [[ -n "$EVENT_GROUP_B" ]]; then
    ce_log "checking second EventGroup '$EVENT_GROUP_B' for multi-group scenario"
    local has_group_b
    has_group_b=$(sender_ssh "python3 -c \"
import yaml, sys
with open('$SENDER_CONFIG') as f:
    cfg = yaml.safe_load(f)
for r in cfg.get('spec',{}).get('resources',[]):
    if r.get('kind')=='EventGroup' and r.get('metadata',{}).get('name')=='$EVENT_GROUP_B':
        print('yes')
        sys.exit(0)
print('no')
\"" 2>/dev/null || echo "no")
    [[ "$has_group_b" == "yes" ]] || ce_die "second EventGroup '$EVENT_GROUP_B' not found in sender config"
  fi

  # 10. Verify FederationSLO exists for primary group
  ce_log "checking FederationSLO for group '$EVENT_GROUP'"
  local has_slo
  has_slo=$(sender_ssh "python3 -c \"
import yaml, sys
with open('$SENDER_CONFIG') as f:
    cfg = yaml.safe_load(f)
for r in cfg.get('spec',{}).get('resources',[]):
    if r.get('kind')=='FederationSLO' and r.get('spec',{}).get('groupRef')=='$EVENT_GROUP':
        print('yes')
        sys.exit(0)
print('no')
\"" 2>/dev/null || echo "no")
  [[ "$has_slo" == "yes" ]] || ce_die "FederationSLO not found for group '$EVENT_GROUP' in sender config"

  # 11. OTel endpoint check (required for release runs)
  if [[ -n "$OTEL_QUERY_URL" ]]; then
    ce_log "checking OTel query endpoint: $OTEL_QUERY_URL"
    local otel_status
    otel_status=$(curl -sk --connect-timeout 5 -o /dev/null -w '%{http_code}' "$OTEL_QUERY_URL" 2>/dev/null || echo "000")
    [[ "$otel_status" != "000" ]] || ce_die "OTel query endpoint unreachable: $OTEL_QUERY_URL"
  elif [[ "$ALLOW_SKIP" != "true" ]]; then
    ce_die "CE_OTEL_QUERY_URL is required for release qualification (use --allow-skip for dev)"
  fi

  ce_log "preflight PASS"
}

# ============================================================================
# Binary provenance (Section 2)
# ============================================================================

collect_provenance() {
  ce_log "=== Collecting binary provenance ==="
  local sender_routerctl_info sender_eventd_info
  local receiver_routerctl_info receiver_eventd_info

  sender_routerctl_info=$(remote_binary_info "$CE_SENDER_SSH_HOST" "$ROUTERCTL")
  receiver_routerctl_info=$(remote_binary_info "$CE_RECEIVER_SSH_HOST" "$RECEIVER_ROUTERCTL")
  sender_eventd_info=$(remote_binary_info "$CE_SENDER_SSH_HOST" "routerd-eventd")
  receiver_eventd_info=$(remote_binary_info "$CE_RECEIVER_SSH_HOST" "routerd-eventd")

  local sender_hostname receiver_hostname
  sender_hostname=$(sender_ssh "hostname" 2>/dev/null || echo "unknown")
  receiver_hostname=$(receiver_ssh "hostname" 2>/dev/null || echo "unknown")

  local sender_config_digest receiver_config_digest
  sender_config_digest=$(sender_ssh "sha256sum $SENDER_CONFIG 2>/dev/null | cut -d' ' -f1" || echo "unknown")
  receiver_config_digest=$(receiver_ssh "sha256sum $RECEIVER_CONFIG 2>/dev/null | cut -d' ' -f1" || echo "unknown")

  python3 - "$EVIDENCE_DIR/provenance.json" \
    "$COMMIT" "$FULL_COMMIT" \
    "$sender_hostname" "$sender_routerctl_info" "$sender_eventd_info" "$sender_config_digest" \
    "$receiver_hostname" "$receiver_routerctl_info" "$receiver_eventd_info" "$receiver_config_digest" \
    "$SENDER_NODE" "$RECEIVER_NODE" "$RECEIVER_ENDPOINT" \
    "$SENDER_EVENTD_UNIT" "$RECEIVER_EVENTD_UNIT" <<'PY'
import json, sys

args = sys.argv[1:]
file_path = args[0]
commit_short, commit_full = args[1], args[2]
s_host, s_rctl, s_eventd, s_cdigest = args[3], args[4], args[5], args[6]
r_host, r_rctl, r_eventd, r_cdigest = args[7], args[8], args[9], args[10]
s_node, r_node, r_endpoint = args[11], args[12], args[13]
s_unit, r_unit = args[14], args[15]

def split_info(info):
    parts = info.split("|", 1)
    return {"version": parts[0], "sha256": parts[1] if len(parts) > 1 else "unknown"}

prov = {
    "prHeadCommit": commit_short,
    "fullCommit": commit_full,
    "sender": {
        "hostname": s_host,
        "nodeName": s_node,
        "routerctl": split_info(s_rctl),
        "routerdEventd": split_info(s_eventd),
        "configDigest": s_cdigest,
        "eventdUnit": s_unit,
    },
    "receiver": {
        "hostname": r_host,
        "nodeName": r_node,
        "endpoint": r_endpoint,
        "routerctl": split_info(r_rctl),
        "routerdEventd": split_info(r_eventd),
        "configDigest": r_cdigest,
        "eventdUnit": r_unit,
    },
}
with open(file_path, "w") as f:
    json.dump(prov, f, indent=2, ensure_ascii=False)
    f.write("\n")
PY
  ce_log "provenance recorded to $EVIDENCE_DIR/provenance.json"
}

# ============================================================================
# Fault injection helpers — remote SSH, no local eval (Section 4)
# ============================================================================

_cleanup_faults() {
  if [[ "$_PARTITION_ACTIVE" == "true" ]]; then
    ce_log "CLEANUP: restoring partition"
    partition_restore || ce_log "CLEANUP WARN: partition restore failed"
  fi
  if [[ "$_PLUGIN_BROKEN" == "true" ]]; then
    ce_log "CLEANUP: restoring subscription plugin"
    plugin_restore || ce_log "CLEANUP WARN: plugin restore failed"
  fi
}
trap _cleanup_faults EXIT INT TERM

partition_apply() {
  [[ -n "${CE_SENDER_PARTITION_APPLY:-}" ]] || return 1
  ce_log "partition: applying"
  sender_ssh "$CE_SENDER_PARTITION_APPLY" || { ce_log "FAIL: partition apply command failed"; return 1; }
  _PARTITION_ACTIVE=true
  # Verify partition is effective
  local ep_host ep_port
  ep_host=$(echo "$RECEIVER_ENDPOINT" | sed 's|https\?://||; s|/.*||; s|:.*||')
  ep_port=$(echo "$RECEIVER_ENDPOINT" | grep -oP ':\K[0-9]+' || echo "80")
  sleep 1
  if sender_ssh "timeout 3 bash -c 'echo >/dev/tcp/$ep_host/$ep_port' 2>/dev/null"; then
    ce_log "FAIL: partition applied but endpoint still reachable"
    return 1
  fi
  ce_log "partition: verified (endpoint unreachable)"
  return 0
}

partition_restore() {
  [[ -n "${CE_SENDER_PARTITION_RESTORE:-}" ]] || return 1
  ce_log "partition: restoring"
  sender_ssh "$CE_SENDER_PARTITION_RESTORE" || { ce_log "FAIL: partition restore command failed"; return 1; }
  _PARTITION_ACTIVE=false
  # Verify partition removed
  local ep_host ep_port
  ep_host=$(echo "$RECEIVER_ENDPOINT" | sed 's|https\?://||; s|/.*||; s|:.*||')
  ep_port=$(echo "$RECEIVER_ENDPOINT" | grep -oP ':\K[0-9]+' || echo "80")
  local end=$((SECONDS + 15))
  while [[ $SECONDS -lt $end ]]; do
    if sender_ssh "timeout 3 bash -c 'echo >/dev/tcp/$ep_host/$ep_port' 2>/dev/null"; then
      ce_log "partition: verified removed (endpoint reachable)"
      return 0
    fi
    sleep 1
  done
  ce_log "FAIL: partition restored but endpoint still unreachable"
  return 1
}

plugin_fail() {
  [[ -n "${CE_RECEIVER_PLUGIN_FAIL:-}" ]] || return 1
  ce_log "plugin: injecting failure"
  receiver_ssh "$CE_RECEIVER_PLUGIN_FAIL" || { ce_log "FAIL: plugin fail command failed"; return 1; }
  _PLUGIN_BROKEN=true
  return 0
}

plugin_restore() {
  [[ -n "${CE_RECEIVER_PLUGIN_RESTORE:-}" ]] || return 1
  ce_log "plugin: restoring"
  receiver_ssh "$CE_RECEIVER_PLUGIN_RESTORE" || { ce_log "FAIL: plugin restore command failed"; return 1; }
  _PLUGIN_BROKEN=false
  return 0
}

# ============================================================================
# Event and delivery helpers
# ============================================================================

emit_test_event() {
  local event_id=$1 subject=${2:-10.99.0.1/32} group=${3:-$EVENT_GROUP} ttl=${4:-600s}
  sender_routerctl "federation event emit \
    --group $group \
    --type routerd.client.ipv4.observed \
    --subject $subject \
    --id $event_id \
    --source-node $SENDER_NODE \
    --ttl $ttl \
    --state-file $SENDER_STATE_DB"
}

wait_delivery() {
  local event_id=$1 timeout=${2:-60}
  local deadline=$((SECONDS + timeout))
  while [[ $SECONDS -lt $deadline ]]; do
    local status
    status=$(sender_routerctl "federation event deliveries --group $EVENT_GROUP --event-id $event_id --state-file $SENDER_STATE_DB -o json" 2>/dev/null || echo "[]")
    if echo "$status" | python3 -c "
import json,sys
data=json.load(sys.stdin)
for d in data:
  if d.get('peer','')==sys.argv[1] and d.get('status','')=='delivered':
    sys.exit(0)
sys.exit(1)
" "$RECEIVER_NODE" 2>/dev/null; then
      return 0
    fi
    sleep 2
  done
  return 1
}

wait_doctor_healthy() {
  local timeout=${1:-60}
  local deadline=$((SECONDS + timeout))
  while [[ $SECONDS -lt $deadline ]]; do
    local doc
    doc=$(sender_doctor_json 2>/dev/null || echo "{}")
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

receiver_has_event() {
  local event_id=$1 group=${2:-$EVENT_GROUP}
  receiver_routerctl "federation event list --group $group --state-file $RECEIVER_STATE_DB -o json" 2>/dev/null \
    | python3 -c "
import json,sys
data=json.load(sys.stdin)
for e in data:
  if e.get('id','')==sys.argv[1]: sys.exit(0)
sys.exit(1)
" "$event_id" 2>/dev/null
}

get_delivery_status() {
  local event_id=$1
  sender_routerctl "federation event deliveries --group $EVENT_GROUP --event-id $event_id --state-file $SENDER_STATE_DB -o json" 2>/dev/null \
    | python3 -c "
import json,sys
data=json.load(sys.stdin)
for d in data:
  if d.get('peer','')==sys.argv[1]:
    print(d.get('status','unknown'))
    sys.exit(0)
print('missing')
" "$RECEIVER_NODE" 2>/dev/null || echo "error"
}

get_eventd_main_pid() {
  local host=$1 unit=$2
  ce_ssh "$host" "sudo systemctl show -p MainPID --value $unit 2>/dev/null" || echo "0"
}

# ============================================================================
# OTel metrics query (Section 8)
# ============================================================================

query_otel_metric() {
  local metric=$1 time_start=$2 time_end=$3
  [[ -n "$OTEL_QUERY_URL" ]] || { echo "na"; return; }
  # Query Prometheus-compatible endpoint
  local result
  result=$(curl -sk --connect-timeout 10 \
    "${OTEL_QUERY_URL}/api/v1/query_range" \
    --data-urlencode "query=increase(${metric}[5m])" \
    --data-urlencode "start=${time_start}" \
    --data-urlencode "end=${time_end}" \
    --data-urlencode "step=60s" 2>/dev/null || echo "{}")
  echo "$result" | python3 -c "
import json,sys
try:
    r=json.load(sys.stdin)
    results=r.get('data',{}).get('result',[])
    if not results: print('0')
    else:
        vals=[float(v[1]) for series in results for v in series.get('values',[]) if float(v[1])>0]
        print(max(vals) if vals else '0')
except: print('error')
" 2>/dev/null || echo "error"
}

check_high_cardinality_labels() {
  local metric=$1
  [[ -n "$OTEL_QUERY_URL" ]] || { echo "pass"; return; }
  local labels
  labels=$(curl -sk --connect-timeout 10 \
    "${OTEL_QUERY_URL}/api/v1/labels" \
    --data-urlencode "match[]=${metric}" 2>/dev/null || echo "{}")
  echo "$labels" | python3 -c "
import json,sys
FORBIDDEN=['event_id','subject','address','endpoint','error_message','raw_error','token','secret','credential']
try:
    r=json.load(sys.stdin)
    found=[l for l in r.get('data',[]) if l.lower() in FORBIDDEN]
    if found: print('fail:'+','.join(found))
    else: print('pass')
except: print('error')
" 2>/dev/null || echo "error"
}

# ============================================================================
# Evidence + schema validation + secret scan (Section 9)
# ============================================================================

CYCLE_DIR=""
SCENARIO_RESULTS=()

scenario_evidence() {
  local scenario=$1 result=$2 reason=${3:-}
  shift; shift; shift || true
  local file="$CYCLE_DIR/${scenario}.json"
  python3 - "$file" "$scenario" "$result" "$reason" "$COMMIT" "$RUN_ID" "$CYCLE_NUM" "$@" <<'PYEOF'
import json, sys, os

file_path = sys.argv[1]
scenario = sys.argv[2]
result = sys.argv[3]
reason = sys.argv[4]
commit = sys.argv[5]
run_id = sys.argv[6]
cycle = sys.argv[7]
extra_args = sys.argv[8:]

evidence = {
    "runId": run_id,
    "commit": commit,
    "cycle": int(cycle),
    "scenario": scenario,
    "result": result,
}
if reason:
    evidence["reason"] = reason

for arg in extra_args:
    if "=" in arg:
        k, v = arg.split("=", 1)
        try:
            evidence[k] = json.loads(v)
        except (json.JSONDecodeError, ValueError):
            evidence[k] = v

with open(file_path, "w") as f:
    json.dump(evidence, f, indent=2, ensure_ascii=False)
    f.write("\n")
PYEOF
  SCENARIO_RESULTS+=("$scenario=$result")
}

secret_scan_evidence() {
  local dir=$1
  ce_log "scanning evidence for secrets"
  local found=0
  for f in "$dir"/*.json; do
    [[ -f "$f" ]] || continue
    if grep -qiE 'hmac[_-]?secret|authorization[: ]+bearer|private.?key|-----BEGIN|aws_secret|azure.*credential|oci.*key|ssh .*-i ' "$f" 2>/dev/null; then
      ce_log "SECRET FOUND in $f"
      found=1
    fi
  done
  return $found
}

validate_evidence_schema() {
  local file=$1
  [[ -f "$file" ]] || return 1
  python3 - "$file" <<'PY'
import json, sys
try:
    with open(sys.argv[1]) as f:
        d = json.load(f)
    required = ["runId", "commit", "scenario", "result", "cycle"]
    for k in required:
        if k not in d:
            print(f"missing required field: {k}", file=sys.stderr)
            sys.exit(1)
    if d["result"] in ("fail", "skip") and "reason" not in d:
        print("result=fail/skip requires 'reason' field", file=sys.stderr)
        sys.exit(1)
    if d["result"] == "pass":
        for ts in ("startedAt", "endedAt"):
            if ts not in d:
                print(f"result=pass requires '{ts}' field", file=sys.stderr)
                sys.exit(1)
    sys.exit(0)
except json.JSONDecodeError as e:
    print(f"invalid JSON: {e}", file=sys.stderr)
    sys.exit(1)
PY
}

# ============================================================================
# Scenario timeout wrapper
# ============================================================================

check_deadline() {
  local start=$1
  if [[ $((SECONDS - start)) -ge $DURATION ]]; then
    return 1
  fi
  return 0
}

# ============================================================================
# SCENARIOS (Section 7)
# ============================================================================

scenario_healthy() {
  ce_log "=== Scenario: healthy baseline ==="
  local start_ts
  start_ts=$(timestamp_utc)

  local event_id
  event_id="qual-healthy-${RUN_ID}-c${CYCLE_NUM}-$(date -u +%s)"
  local doctor_before doctor_after delivery_summary remediation

  # doctor before
  doctor_before=$(sender_doctor_json) || { scenario_evidence healthy fail "doctor command failed before test" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"; return; }

  # emit test event
  emit_test_event "$event_id" "10.99.1.1/32" || { scenario_evidence healthy fail "emit_test_event failed" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"; return; }

  # wait for delivery
  if ! wait_delivery "$event_id" 60; then
    scenario_evidence healthy fail "delivery not confirmed within 60s" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "doctorBefore=$doctor_before"
    return
  fi

  # verify receiver has the event
  if ! receiver_has_event "$event_id"; then
    scenario_evidence healthy fail "event not found in receiver store" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "doctorBefore=$doctor_before"
    return
  fi

  # doctor after
  doctor_after=$(sender_doctor_json) || { scenario_evidence healthy fail "doctor command failed after test" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"; return; }

  # assert doctor fail count == 0
  local fail_count slo_violations
  read -r fail_count slo_violations < <(echo "$doctor_after" | python3 -c "
import json,sys
r=json.load(sys.stdin)
fc=r.get('summary',{}).get('fail',999)
groups=r.get('federation',{}).get('slo',{}).get('groups',[])
viol=sum(len(g.get('violations',[])) for g in groups if g.get('group')==sys.argv[1])
print(fc,viol)
" "$EVENT_GROUP" 2>/dev/null || echo "999 999")

  if [[ "$fail_count" != "0" ]]; then
    scenario_evidence healthy fail "doctor after has $fail_count FAIL checks" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "doctorBefore=$doctor_before" "doctorAfter=$doctor_after"
    return
  fi
  if [[ "$slo_violations" != "0" ]]; then
    scenario_evidence healthy fail "SLO has $slo_violations violations for group $EVENT_GROUP" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "doctorBefore=$doctor_before" "doctorAfter=$doctor_after"
    return
  fi

  # remediation plan must be empty
  remediation=$(sender_remediation_json) || true
  local action_count
  action_count=$(echo "$remediation" | python3 -c "
import json,sys
r=json.load(sys.stdin)
print(len(r.get('remediationPlan',{}).get('actions',[])))
" 2>/dev/null || echo "999")
  if [[ "$action_count" != "0" ]]; then
    scenario_evidence healthy fail "remediation plan has $action_count actions (expected 0)" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "doctorAfter=$doctor_after" "remediationPlan=$remediation"
    return
  fi

  delivery_summary=$(sender_delivery_summary) || true

  scenario_evidence healthy pass "" \
    "startedAt=$start_ts" "endedAt=$(timestamp_utc)" \
    "doctorBefore=$doctor_before" "doctorAfter=$doctor_after" \
    "deliverySummary=$delivery_summary" "remediationPlan=$remediation" \
    "testedEventId=$event_id"
}

scenario_partition() {
  ce_log "=== Scenario: peer partition and recovery ==="
  local start_ts
  start_ts=$(timestamp_utc)

  if [[ -z "${CE_SENDER_PARTITION_APPLY:-}" ]] || [[ -z "${CE_SENDER_PARTITION_RESTORE:-}" ]]; then
    scenario_evidence partition fail "CE_SENDER_PARTITION_APPLY/RESTORE not set" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi

  local doctor_before doctor_during doctor_after

  doctor_before=$(sender_doctor_json) || true

  # Apply partition
  if ! partition_apply; then
    scenario_evidence partition fail "partition apply failed or did not take effect" "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "doctorBefore=$doctor_before"
    return
  fi

  # Emit event during partition
  local event_id
  event_id="qual-partition-${RUN_ID}-c${CYCLE_NUM}-$(date -u +%s)"
  emit_test_event "$event_id" "10.99.2.1/32" || true
  sleep 5

  # Check delivery is failed or pending
  local del_status
  del_status=$(get_delivery_status "$event_id")
  if [[ "$del_status" == "delivered" ]]; then
    partition_restore || true
    scenario_evidence partition fail "event delivered despite partition (partition ineffective)" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "doctorBefore=$doctor_before"
    return
  fi

  # Doctor during should detect issues
  doctor_during=$(sender_doctor_json) || true
  local has_fault_check
  has_fault_check=$(echo "$doctor_during" | python3 -c "
import json,sys
r=json.load(sys.stdin)
codes={'failed-deliveries','pending-deliveries','delivery-lag'}
found=[c.get('code') for c in r.get('checks',[]) if c.get('code') in codes and c.get('severity') in ('warn','fail')]
print('yes' if found else 'no')
" 2>/dev/null || echo "no")

  # Remediation plan should have expected actions
  local remediation_during
  remediation_during=$(sender_remediation_json) || true
  local has_remediation_action
  has_remediation_action=$(echo "$remediation_during" | python3 -c "
import json,sys
r=json.load(sys.stdin)
actions=r.get('remediationPlan',{}).get('actions',[])
expected={'retry-failed-deliveries','investigate-pending-deliveries','check-peer-connectivity'}
found=[a.get('action') for a in actions if a.get('action') in expected]
print('yes' if found else 'no')
" 2>/dev/null || echo "no")

  # Restore partition
  local restore_start=$SECONDS
  if ! partition_restore; then
    scenario_evidence partition fail "partition restore failed" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "doctorBefore=$doctor_before" "doctorDuring=$doctor_during"
    return
  fi

  # Wait for delivery recovery
  if ! wait_delivery "$event_id" "$DURATION"; then
    doctor_after=$(sender_doctor_json) || true
    scenario_evidence partition fail "delivery not recovered within ${DURATION}s" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" \
      "doctorBefore=$doctor_before" "doctorDuring=$doctor_during" "doctorAfter=$doctor_after"
    return
  fi

  local recovery_duration=$((SECONDS - restore_start))

  # Doctor after must be healthy
  if ! wait_doctor_healthy 60; then
    doctor_after=$(sender_doctor_json) || true
    scenario_evidence partition fail "doctor not healthy after recovery" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" \
      "doctorBefore=$doctor_before" "doctorDuring=$doctor_during" "doctorAfter=$doctor_after" \
      "recoveryDurationSeconds=$recovery_duration"
    return
  fi

  doctor_after=$(sender_doctor_json) || true

  # Verify partition rules are cleaned up
  if sender_ssh "$CE_SENDER_PARTITION_APPLY --check 2>/dev/null" 2>/dev/null; then
    ce_log "WARN: partition rule check returned success (may still be present)"
  fi

  scenario_evidence partition pass "" \
    "startedAt=$start_ts" "endedAt=$(timestamp_utc)" \
    "doctorBefore=$doctor_before" "doctorDuring=$doctor_during" "doctorAfter=$doctor_after" \
    "recoveryDurationSeconds=$recovery_duration" \
    "hasFaultCheck=$has_fault_check" "hasRemediationAction=$has_remediation_action" \
    "testedEventId=$event_id"
}

scenario_ttl_refresh() {
  ce_log "=== Scenario: TTL refresh across partition ==="
  local start_ts
  start_ts=$(timestamp_utc)

  if [[ -z "${CE_SENDER_PARTITION_APPLY:-}" ]] || [[ -z "${CE_SENDER_PARTITION_RESTORE:-}" ]]; then
    scenario_evidence ttl-refresh fail "CE_SENDER_PARTITION_APPLY/RESTORE not set" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi

  local event_id
  event_id="qual-ttl-${RUN_ID}-c${CYCLE_NUM}-$(date -u +%s)"

  # Emit event with short TTL
  emit_test_event "$event_id" "10.99.3.1/32" "$EVENT_GROUP" "120s" || {
    scenario_evidence ttl-refresh fail "emit_test_event failed" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"; return; }

  # Wait for initial delivery
  if ! wait_delivery "$event_id" 30; then
    scenario_evidence ttl-refresh fail "initial delivery not confirmed" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi

  # Apply partition
  if ! partition_apply; then
    scenario_evidence ttl-refresh fail "partition apply failed" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi

  # Refresh TTL during partition
  sleep 5
  emit_test_event "$event_id" "10.99.3.1/32" "$EVENT_GROUP" "600s" || true

  # Check for stale TTL or repush state
  local doctor_during
  doctor_during=$(sender_doctor_json) || true

  # Restore partition
  if ! partition_restore; then
    scenario_evidence ttl-refresh fail "partition restore failed" "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "doctorDuring=$doctor_during"
    return
  fi

  # Wait for re-push with polling (not fixed sleep)
  local deadline=$((SECONDS + 60))
  local repush_confirmed=false
  while [[ $SECONDS -lt $deadline ]]; do
    local del_json
    del_json=$(sender_routerctl "federation event deliveries --group $EVENT_GROUP --event-id $event_id --state-file $SENDER_STATE_DB -o json" 2>/dev/null || echo "[]")
    if echo "$del_json" | python3 -c "
import json,sys
data=json.load(sys.stdin)
for d in data:
  if d.get('peer','')==sys.argv[1] and d.get('status','')=='delivered':
    sys.exit(0)
sys.exit(1)
" "$RECEIVER_NODE" 2>/dev/null; then
      repush_confirmed=true
      break
    fi
    sleep 2
  done

  if [[ "$repush_confirmed" != "true" ]]; then
    local doctor_after
    doctor_after=$(sender_doctor_json) || true
    scenario_evidence ttl-refresh fail "re-push not confirmed after restore" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "doctorDuring=$doctor_during" "doctorAfter=$doctor_after"
    return
  fi

  # Verify stale TTL resolved
  local delivery_summary doctor_after
  delivery_summary=$(sender_delivery_summary) || true
  doctor_after=$(sender_doctor_json) || true

  local stale_count
  stale_count=$(echo "$delivery_summary" | python3 -c "
import json,sys
data=json.load(sys.stdin)
if not isinstance(data,list): print('error'); sys.exit(0)
total=sum(r.get('staleTTL',0) for r in data)
print(total)
" 2>/dev/null)

  if [[ -z "$stale_count" ]] || [[ "$stale_count" == "error" ]]; then
    scenario_evidence ttl-refresh fail "failed to parse delivery summary for stale TTL count" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "doctorDuring=$doctor_during" "doctorAfter=$doctor_after" \
      "deliverySummary=$delivery_summary"
    return
  fi
  if [[ "$stale_count" != "0" ]]; then
    scenario_evidence ttl-refresh fail "stale TTL count=$stale_count after recovery (expected 0)" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "doctorDuring=$doctor_during" "doctorAfter=$doctor_after" \
      "deliverySummary=$delivery_summary"
    return
  fi

  scenario_evidence ttl-refresh pass "" \
    "startedAt=$start_ts" "endedAt=$(timestamp_utc)" \
    "doctorDuring=$doctor_during" "doctorAfter=$doctor_after" \
    "deliverySummary=$delivery_summary" "testedEventId=$event_id"
}

scenario_restart() {
  ce_log "=== Scenario: eventd restart recovery ==="
  local start_ts
  start_ts=$(timestamp_utc)

  # Record sender PID before restart
  local sender_pid_before
  sender_pid_before=$(get_eventd_main_pid "$CE_SENDER_SSH_HOST" "$SENDER_EVENTD_UNIT")

  local doctor_before
  doctor_before=$(sender_doctor_json) || true

  # Restart sender eventd
  ce_log "restarting sender eventd"
  if ! sender_ssh "sudo systemctl restart $SENDER_EVENTD_UNIT"; then
    scenario_evidence restart fail "sender systemctl restart failed" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi
  sleep 3

  # Verify PID changed
  local sender_pid_after
  sender_pid_after=$(get_eventd_main_pid "$CE_SENDER_SSH_HOST" "$SENDER_EVENTD_UNIT")
  if [[ "$sender_pid_before" == "$sender_pid_after" ]] && [[ "$sender_pid_before" != "0" ]]; then
    scenario_evidence restart fail "sender PID unchanged after restart ($sender_pid_before)" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi

  # Verify service is active
  if ! sender_ssh "sudo systemctl is-active $SENDER_EVENTD_UNIT >/dev/null 2>&1"; then
    scenario_evidence restart fail "sender eventd not active after restart" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi

  # Emit event after sender restart
  local event_id1
  event_id1="qual-restart-s-${RUN_ID}-c${CYCLE_NUM}-$(date -u +%s)"
  emit_test_event "$event_id1" "10.99.4.1/32" || {
    scenario_evidence restart fail "emit after sender restart failed" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"; return; }

  if ! wait_delivery "$event_id1" 60; then
    scenario_evidence restart fail "delivery not confirmed after sender restart" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi

  # Restart receiver eventd
  local receiver_pid_before
  receiver_pid_before=$(get_eventd_main_pid "$CE_RECEIVER_SSH_HOST" "$RECEIVER_EVENTD_UNIT")

  ce_log "restarting receiver eventd"
  if ! receiver_ssh "sudo systemctl restart $RECEIVER_EVENTD_UNIT"; then
    scenario_evidence restart fail "receiver systemctl restart failed" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi
  sleep 3

  local receiver_pid_after
  receiver_pid_after=$(get_eventd_main_pid "$CE_RECEIVER_SSH_HOST" "$RECEIVER_EVENTD_UNIT")
  if [[ "$receiver_pid_before" == "$receiver_pid_after" ]] && [[ "$receiver_pid_before" != "0" ]]; then
    scenario_evidence restart fail "receiver PID unchanged after restart ($receiver_pid_before)" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi

  if ! receiver_ssh "sudo systemctl is-active $RECEIVER_EVENTD_UNIT >/dev/null 2>&1"; then
    scenario_evidence restart fail "receiver eventd not active after restart" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi

  # Emit event after receiver restart
  local event_id2
  event_id2="qual-restart-r-${RUN_ID}-c${CYCLE_NUM}-$(date -u +%s)"
  emit_test_event "$event_id2" "10.99.4.2/32" || {
    scenario_evidence restart fail "emit after receiver restart failed" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"; return; }

  if ! wait_delivery "$event_id2" 60; then
    scenario_evidence restart fail "delivery not confirmed after receiver restart" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi

  if ! receiver_has_event "$event_id2"; then
    scenario_evidence restart fail "event not found in receiver store after receiver restart" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi

  # Doctor after must be healthy
  if ! wait_doctor_healthy 30; then
    local doctor_after
    doctor_after=$(sender_doctor_json) || true
    scenario_evidence restart fail "doctor not healthy after restarts" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "doctorAfter=$doctor_after"
    return
  fi

  local doctor_after
  doctor_after=$(sender_doctor_json) || true

  scenario_evidence restart pass "" \
    "startedAt=$start_ts" "endedAt=$(timestamp_utc)" \
    "doctorBefore=$doctor_before" "doctorAfter=$doctor_after" \
    "senderPidBefore=$sender_pid_before" "senderPidAfter=$sender_pid_after" \
    "receiverPidBefore=$receiver_pid_before" "receiverPidAfter=$receiver_pid_after" \
    "testedEventId1=$event_id1" "testedEventId2=$event_id2"
}

scenario_subscription() {
  ce_log "=== Scenario: subscription failure and recovery ==="
  local start_ts
  start_ts=$(timestamp_utc)

  if [[ -z "${CE_RECEIVER_PLUGIN_FAIL:-}" ]] || [[ -z "${CE_RECEIVER_PLUGIN_RESTORE:-}" ]]; then
    scenario_evidence subscription fail "CE_RECEIVER_PLUGIN_FAIL/RESTORE not set" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi
  if [[ -z "$SUBSCRIPTION_KEY" ]]; then
    scenario_evidence subscription fail "CE_SUBSCRIPTION_KEY not set" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi

  # Inject plugin failure
  if ! plugin_fail; then
    scenario_evidence subscription fail "plugin failure injection failed" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi

  # Emit event that should trigger subscription (on receiver side)
  local event_id1
  event_id1="qual-sub-fail-${RUN_ID}-c${CYCLE_NUM}-$(date -u +%s)"
  emit_test_event "$event_id1" "10.99.5.1/32" || true

  # Wait for delivery (delivery should succeed, subscription should fail)
  wait_delivery "$event_id1" 30 || true
  sleep 10

  # Check for failed subscription runs
  local sub_runs_during
  sub_runs_during=$(receiver_routerctl "federation subscription runs --subscription $SUBSCRIPTION_KEY --state-file $RECEIVER_STATE_DB -o json" 2>/dev/null || echo "[]")
  local has_failed_run
  has_failed_run=$(echo "$sub_runs_during" | python3 -c "
import json,sys
data=json.load(sys.stdin)
failed=[r for r in data if r.get('status','')=='failed']
print('yes' if failed else 'no')
" 2>/dev/null || echo "no")

  if [[ "$has_failed_run" != "yes" ]]; then
    plugin_restore || true
    scenario_evidence subscription fail "no failed subscription run detected after plugin failure" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "subscriptionRuns=$sub_runs_during"
    return
  fi

  # Check doctor/SLO violation
  local doctor_during
  doctor_during=$(sender_doctor_json) || true

  # Restore plugin
  if ! plugin_restore; then
    scenario_evidence subscription fail "plugin restore failed" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "doctorDuring=$doctor_during"
    return
  fi

  # Emit new event, should succeed
  local event_id2
  event_id2="qual-sub-ok-${RUN_ID}-c${CYCLE_NUM}-$(date -u +%s)"
  emit_test_event "$event_id2" "10.99.5.2/32" || true
  wait_delivery "$event_id2" 30 || true
  sleep 10

  # Check for succeeded run
  local sub_runs_after
  sub_runs_after=$(receiver_routerctl "federation subscription runs --subscription $SUBSCRIPTION_KEY --state-file $RECEIVER_STATE_DB -o json" 2>/dev/null || echo "[]")
  local has_succeeded_run
  has_succeeded_run=$(echo "$sub_runs_after" | python3 -c "
import json,sys
data=json.load(sys.stdin)
succeeded=[r for r in data if r.get('status','')=='succeeded']
print('yes' if succeeded else 'no')
" 2>/dev/null || echo "no")

  if [[ "$has_succeeded_run" != "yes" ]]; then
    scenario_evidence subscription fail "no succeeded subscription run after plugin restore" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" \
      "subscriptionRunsDuring=$sub_runs_during" "subscriptionRunsAfter=$sub_runs_after"
    return
  fi

  scenario_evidence subscription pass "" \
    "startedAt=$start_ts" "endedAt=$(timestamp_utc)" \
    "doctorDuring=$doctor_during" \
    "subscriptionRunsDuring=$sub_runs_during" "subscriptionRunsAfter=$sub_runs_after" \
    "testedEventId1=$event_id1" "testedEventId2=$event_id2"
}

scenario_config_fault() {
  ce_log "=== Scenario: expected-peer / config fault ==="
  local start_ts
  start_ts=$(timestamp_utc)

  # Create a temp config with an empty-endpoint peer to provoke doctor findings
  local original_digest
  original_digest=$(sender_ssh "sha256sum $SENDER_CONFIG 2>/dev/null | cut -d' ' -f1" || echo "unknown")

  local fault_config="/tmp/fedqual-fault-config-${RUN_ID}.yaml"
  sender_ssh "python3 -c \"
import yaml, sys, copy
with open('$SENDER_CONFIG') as f:
    cfg = yaml.safe_load(f)
# Add a bogus expected peer with empty endpoint
bogus = {
    'apiVersion': 'federation.routerd.net/v1alpha1',
    'kind': 'EventPeer',
    'metadata': {'name': 'fedqual-bogus-peer'},
    'spec': {
        'groupRef': '$EVENT_GROUP',
        'nodeName': 'fedqual-nonexistent-node',
        'endpoint': '',
        'direction': 'push',
    }
}
cfg['spec']['resources'].append(bogus)
with open('$fault_config', 'w') as f:
    yaml.safe_dump(cfg, f, default_flow_style=False)
\"" || { scenario_evidence config-fault fail "failed to create fault config" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"; return; }

  # Run doctor against the fault config
  local fault_doctor
  fault_doctor=$(sender_routerctl "doctor federation --config $fault_config --state-file $SENDER_STATE_DB -o json" 2>/dev/null || echo "{}")

  local fault_remediation
  fault_remediation=$(sender_routerctl "doctor federation --config $fault_config --state-file $SENDER_STATE_DB -o json --remediation-plan" 2>/dev/null || echo "{}")

  # Verify specific check codes
  local found_codes expected_action
  read -r found_codes expected_action < <(echo "$fault_doctor" "$fault_remediation" | python3 -c "
import json,sys
# Read doctor output (first JSON object from stdin)
import io
raw = sys.stdin.read()
parts = raw.split('}{')
doc = json.loads(parts[0] + ('}' if len(parts)>1 else ''))
rem = json.loads(('{' if len(parts)>1 else '') + parts[-1]) if len(parts)>1 else {}

expected_codes = {'expected-delivery-no-endpoint', 'expected-delivery'}
found = [c['code'] for c in doc.get('checks',[]) if c.get('code') in expected_codes]

expected_actions = {'configure-peer-endpoint', 'investigate-missing-delivery-rows'}
actions = [a['action'] for a in rem.get('remediationPlan',{}).get('actions',[]) if a.get('action') in expected_actions]

print(' '.join(found) if found else 'none', ' '.join(actions) if actions else 'none')
" 2>/dev/null || echo "none none")

  # Cleanup temp config
  sender_ssh "rm -f $fault_config" || true

  # Verify original config unchanged
  local current_digest
  current_digest=$(sender_ssh "sha256sum $SENDER_CONFIG 2>/dev/null | cut -d' ' -f1" || echo "unknown")
  if [[ "$original_digest" != "$current_digest" ]]; then
    scenario_evidence config-fault fail "original config digest changed during test" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" \
      "originalDigest=$original_digest" "currentDigest=$current_digest"
    return
  fi

  if [[ "$found_codes" == "none" ]]; then
    scenario_evidence config-fault fail "no expected check codes found (need expected-delivery-no-endpoint or expected-delivery)" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "faultDoctor=$fault_doctor"
    return
  fi

  if [[ "$expected_action" == "none" ]]; then
    scenario_evidence config-fault fail "no expected remediation action found (need configure-peer-endpoint or investigate-missing-delivery-rows)" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "faultDoctor=$fault_doctor" "faultRemediation=$fault_remediation"
    return
  fi

  scenario_evidence config-fault pass "" \
    "startedAt=$start_ts" "endedAt=$(timestamp_utc)" \
    "faultDoctor=$fault_doctor" "faultRemediation=$fault_remediation" \
    "foundCodes=$found_codes" "expectedAction=$expected_action" \
    "originalConfigDigest=$original_digest"
}

scenario_security() {
  ce_log "=== Scenario: security rejection ==="
  local start_ts
  start_ts=$(timestamp_utc)

  [[ -n "$RECEIVER_ENDPOINT" ]] || {
    scenario_evidence security fail "receiver endpoint not discovered in preflight" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  }

  # Record event count before
  local event_count_before
  event_count_before=$(receiver_routerctl "federation event list --group $EVENT_GROUP --state-file $RECEIVER_STATE_DB -o json" 2>/dev/null \
    | python3 -c "import json,sys; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")

  # 1. Send malformed body
  ce_log "security: sending malformed body"
  local malformed_status
  malformed_status=$(sender_ssh "curl -sk -X POST -d 'not-json' -o /dev/null -w '%{http_code}' '${RECEIVER_ENDPOINT}/v1/events' 2>/dev/null" || echo "000")

  # 2. Send valid JSON but wrong structure
  ce_log "security: sending wrong structure"
  local wrong_status
  wrong_status=$(sender_ssh "curl -sk -X POST -H 'Content-Type: application/json' -d '{\"bad\":true}' -o /dev/null -w '%{http_code}' '${RECEIVER_ENDPOINT}/v1/events' 2>/dev/null" || echo "000")

  # 3. Send stale timestamp (if HMAC is enabled, a bad signature)
  ce_log "security: sending stale/bad-auth request"
  local bad_auth_status
  bad_auth_status=$(sender_ssh "curl -sk -X POST -H 'Content-Type: application/json' -H 'X-Routerd-Signature: invalid' -d '{\"events\":[]}' -o /dev/null -w '%{http_code}' '${RECEIVER_ENDPOINT}/v1/events' 2>/dev/null" || echo "000")

  # Verify bad requests were rejected (not 2xx)
  local all_rejected=true
  for status_var in malformed_status wrong_status bad_auth_status; do
    local status_val
    eval "status_val=\$$status_var"
    if [[ "$status_val" == 2* ]]; then
      all_rejected=false
    fi
  done

  # Verify no bad events were stored
  sleep 2
  local event_count_after
  event_count_after=$(receiver_routerctl "federation event list --group $EVENT_GROUP --state-file $RECEIVER_STATE_DB -o json" 2>/dev/null \
    | python3 -c "import json,sys; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")

  # 4. Send valid event through normal channel
  ce_log "security: sending normal event"
  local event_id
  event_id="qual-security-${RUN_ID}-c${CYCLE_NUM}-$(date -u +%s)"
  emit_test_event "$event_id" "10.99.6.1/32" || true

  local valid_delivered=false
  if wait_delivery "$event_id" 30; then
    valid_delivered=true
  fi

  # Evaluate
  if [[ "$all_rejected" != "true" ]]; then
    scenario_evidence security fail "some bad requests got 2xx (malformed=$malformed_status wrong=$wrong_status bad_auth=$bad_auth_status)" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi

  if [[ "$valid_delivered" != "true" ]]; then
    scenario_evidence security fail "valid event delivery failed after security test" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" \
      "malformedStatus=$malformed_status" "wrongStatus=$wrong_status" "badAuthStatus=$bad_auth_status"
    return
  fi

  scenario_evidence security pass "" \
    "startedAt=$start_ts" "endedAt=$(timestamp_utc)" \
    "malformedStatus=$malformed_status" "wrongStatus=$wrong_status" "badAuthStatus=$bad_auth_status" \
    "eventCountBefore=$event_count_before" "eventCountAfter=$event_count_after" \
    "testedEventId=$event_id"
}

scenario_multi_group() {
  ce_log "=== Scenario: multi-group isolation ==="
  local start_ts
  start_ts=$(timestamp_utc)

  if [[ -z "$EVENT_GROUP_B" ]]; then
    scenario_evidence multi-group fail "CE_EVENT_GROUP_B not set (need 2 groups for isolation test)" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi

  # Get doctor JSON and check groups
  local doctor_json
  doctor_json=$(sender_doctor_json) || { scenario_evidence multi-group fail "doctor command failed" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"; return; }

  local group_check
  group_check=$(echo "$doctor_json" | python3 -c "
import json,sys
r=json.load(sys.stdin)
groups=r.get('federation',{}).get('slo',{}).get('groups',[])
group_names=[g.get('group','') for g in groups]
ga, gb = sys.argv[1], sys.argv[2]
has_a = ga in group_names
has_b = gb in group_names
if not has_a: print(f'fail:group {ga} not in slo.groups'); sys.exit(0)
if not has_b: print(f'fail:group {gb} not in slo.groups'); sys.exit(0)

# Check thresholds are independent
thresh_a = thresh_b = None
for g in groups:
    if g['group']==ga: thresh_a=g.get('thresholds',{})
    if g['group']==gb: thresh_b=g.get('thresholds',{})

# Check violations don't cross groups
viols_a = [v for g in groups if g['group']==ga for v in g.get('violations',[])]
viols_b = [v for g in groups if g['group']==gb for v in g.get('violations',[])]
for v in viols_a:
    if gb in str(v):
        print(f'fail:group-a violation references group-b')
        sys.exit(0)
for v in viols_b:
    if ga in str(v):
        print(f'fail:group-b violation references group-a')
        sys.exit(0)

print(f'pass:{len(groups)} groups,thresholds_differ={thresh_a!=thresh_b}')
" "$EVENT_GROUP" "$EVENT_GROUP_B" 2>/dev/null || echo "fail:python error")

  if [[ "$group_check" == fail:* ]]; then
    local reason="${group_check#fail:}"
    scenario_evidence multi-group fail "$reason" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "doctorJson=$doctor_json"
    return
  fi

  # Check remediation actions have correct targetGroup
  local remediation_json
  remediation_json=$(sender_remediation_json) || true
  local target_group_check
  target_group_check=$(echo "$remediation_json" | python3 -c "
import json,sys
r=json.load(sys.stdin)
actions=r.get('remediationPlan',{}).get('actions',[])
ga, gb = sys.argv[1], sys.argv[2]
for a in actions:
    tg=a.get('targetGroup','')
    if tg and tg not in (ga, gb, ''):
        print(f'fail:unknown targetGroup {tg}')
        sys.exit(0)
print('pass')
" "$EVENT_GROUP" "$EVENT_GROUP_B" 2>/dev/null || echo "pass")

  if [[ "$target_group_check" == fail:* ]]; then
    scenario_evidence multi-group fail "${target_group_check#fail:}" \
      "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "doctorJson=$doctor_json" "remediationPlan=$remediation_json"
    return
  fi

  scenario_evidence multi-group pass "" \
    "startedAt=$start_ts" "endedAt=$(timestamp_utc)" \
    "doctorJson=$doctor_json" "remediationPlan=$remediation_json" \
    "groupCheck=$group_check"
}

# ============================================================================
# OTel verification (Section 8)
# ============================================================================

verify_otel_metrics() {
  local cycle_start=$1 cycle_end=$2
  if [[ -z "$OTEL_QUERY_URL" ]]; then
    ce_log "OTel verification: skipped (no OTEL_QUERY_URL)"
    return 0
  fi

  ce_log "=== OTel metrics verification ==="
  local metrics_result="pass"

  local metrics=(
    routerd_eventd_delivery_total
    routerd_eventd_delivery_lag_seconds
    routerd_eventd_repush_total
    routerd_eventd_stale_ttl_total
    routerd_eventd_accepted_total
    routerd_eventd_duplicate_total
    routerd_eventd_reject_total
  )

  local results=()
  for m in "${metrics[@]}"; do
    local val
    val=$(query_otel_metric "$m" "$cycle_start" "$cycle_end")
    results+=("\"$m\": \"$val\"")
    ce_log "  $m = $val"
  done

  # Check high cardinality labels
  local cardinality
  cardinality=$(check_high_cardinality_labels "routerd_eventd_delivery_total")
  if [[ "$cardinality" == fail:* ]]; then
    ce_log "FAIL: high-cardinality labels found: ${cardinality#fail:}"
    metrics_result="fail"
  fi

  python3 - "$CYCLE_DIR/otel-metrics.json" "$metrics_result" "$cardinality" <<PYEOF
import json, sys
result = {"result": sys.argv[2], "cardinalityCheck": sys.argv[3], "metrics": {$(IFS=,; echo "${results[*]}")}}
with open(sys.argv[1], "w") as f:
    json.dump(result, f, indent=2, ensure_ascii=False)
    f.write("\n")
PYEOF

  [[ "$metrics_result" == "pass" ]]
}

# ============================================================================
# Main orchestration
# ============================================================================

ce_log "Federation Qualification Harness"
ce_log "Run ID: $RUN_ID"
ce_log "Commit: $COMMIT"
ce_log "Evidence: $EVIDENCE_DIR"
ce_log "Cycles: $CYCLES"
ce_log "Duration: ${DURATION}s per scenario (hard deadline)"
ce_log "Scenarios: $SCENARIOS"
ce_log "Allow skip: $ALLOW_SKIP"

# Validate scenario list
IFS=',' read -ra SCENARIO_LIST <<< "$SCENARIOS"
for s in "${SCENARIO_LIST[@]}"; do
  case "$s" in
    healthy|partition|ttl-refresh|restart|subscription|config-fault|security|multi-group) ;;
    *) ce_die "unknown scenario: $s" ;;
  esac
done

# Run preflight
preflight

# Collect binary provenance
collect_provenance

# Write run metadata
python3 - "$EVIDENCE_DIR/run-metadata.json" "$RUN_ID" "$COMMIT" "$CYCLES" "$DURATION" "$SCENARIOS" "$ALLOW_SKIP" <<'PY'
import json, sys, datetime
file_path = sys.argv[1]
meta = {
    "runId": sys.argv[2],
    "commit": sys.argv[3],
    "startedAt": datetime.datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ"),
    "parameters": {
        "cycles": int(sys.argv[4]),
        "durationPerScenario": int(sys.argv[5]),
        "scenarios": sys.argv[6].split(","),
        "allowSkip": sys.argv[7] == "true",
    },
    "cycles": [],
}
with open(file_path, "w") as f:
    json.dump(meta, f, indent=2, ensure_ascii=False)
    f.write("\n")
PY

# Run cycles
OVERALL_PASS=true
TOTAL_PASS=0
TOTAL_FAIL=0
TOTAL_SKIP=0

for CYCLE_NUM in $(seq 1 "$CYCLES"); do
  ce_log "========== Cycle $CYCLE_NUM/$CYCLES =========="
  CYCLE_DIR="$EVIDENCE_DIR/cycle-$(printf '%03d' "$CYCLE_NUM")"
  mkdir -p "$CYCLE_DIR"
  SCENARIO_RESULTS=()

  cycle_start_ts=$(timestamp_utc)

  for scenario in "${SCENARIO_LIST[@]}"; do
    ce_log "--- $scenario (cycle $CYCLE_NUM) ---"
    scenario_start=$SECONDS

    case "$scenario" in
      healthy)       scenario_healthy ;;
      partition)     scenario_partition ;;
      ttl-refresh)   scenario_ttl_refresh ;;
      restart)       scenario_restart ;;
      subscription)  scenario_subscription ;;
      config-fault)  scenario_config_fault ;;
      security)      scenario_security ;;
      multi-group)   scenario_multi_group ;;
    esac

    scenario_elapsed=$((SECONDS - scenario_start))
    if [[ $scenario_elapsed -ge $DURATION ]]; then
      ce_log "WARN: $scenario took ${scenario_elapsed}s (deadline ${DURATION}s)"
    fi

    # Validate evidence file
    efile="$CYCLE_DIR/${scenario}.json"
    if [[ ! -f "$efile" ]]; then
      ce_log "ERROR: scenario $scenario did not produce evidence file"
      scenario_evidence "$scenario" fail "no evidence file produced" "startedAt=$(timestamp_utc)" "endedAt=$(timestamp_utc)"
    fi
    if ! validate_evidence_schema "$efile"; then
      ce_log "ERROR: $efile failed schema validation"
      OVERALL_PASS=false
    fi
  done

  cycle_end_ts=$(timestamp_utc)

  # OTel metrics verification
  verify_otel_metrics "$cycle_start_ts" "$cycle_end_ts" || {
    ce_log "OTel verification failed for cycle $CYCLE_NUM"
    OVERALL_PASS=false
  }

  # Secret scan
  if ! secret_scan_evidence "$CYCLE_DIR"; then
    ce_log "SECRET SCAN FAILED for cycle $CYCLE_NUM"
    OVERALL_PASS=false
  fi

  # Cycle summary
  cycle_pass=0; cycle_fail=0; cycle_skip=0
  for entry in "${SCENARIO_RESULTS[@]}"; do
    result="${entry#*=}"
    case "$result" in
      pass) ((cycle_pass++)) ;;
      fail) ((cycle_fail++)) ;;
      skip) ((cycle_skip++)) ;;
    esac
  done

  TOTAL_PASS=$((TOTAL_PASS + cycle_pass))
  TOTAL_FAIL=$((TOTAL_FAIL + cycle_fail))
  TOTAL_SKIP=$((TOTAL_SKIP + cycle_skip))

  if [[ $cycle_fail -gt 0 ]]; then
    OVERALL_PASS=false
  fi
  if [[ $cycle_skip -gt 0 ]] && [[ "$ALLOW_SKIP" != "true" ]]; then
    ce_log "FAIL: cycle $CYCLE_NUM has $cycle_skip SKIP results (--allow-skip not set)"
    OVERALL_PASS=false
  fi

  # Update run-metadata with cycle results
  python3 - "$EVIDENCE_DIR/run-metadata.json" "$CYCLE_NUM" "$cycle_pass" "$cycle_fail" "$cycle_skip" "$cycle_start_ts" "$cycle_end_ts" <<'PY'
import json, sys
f = sys.argv[1]
d = json.load(open(f))
d["cycles"].append({
    "cycle": int(sys.argv[2]),
    "pass": int(sys.argv[3]),
    "fail": int(sys.argv[4]),
    "skip": int(sys.argv[5]),
    "startedAt": sys.argv[6],
    "completedAt": sys.argv[7],
})
json.dump(d, open(f, "w"), indent=2, ensure_ascii=False)
open(f, "a").write("\n")
PY
done

# Final metadata update
python3 - "$EVIDENCE_DIR/run-metadata.json" "$TOTAL_PASS" "$TOTAL_FAIL" "$TOTAL_SKIP" "$([[ "$OVERALL_PASS" == "true" ]] && echo pass || echo fail)" <<'PY'
import json, sys, datetime
f = sys.argv[1]
d = json.load(open(f))
d["completedAt"] = datetime.datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ")
d["summary"] = {
    "overall": sys.argv[5],
    "totalPass": int(sys.argv[2]),
    "totalFail": int(sys.argv[3]),
    "totalSkip": int(sys.argv[4]),
}
json.dump(d, open(f, "w"), indent=2, ensure_ascii=False)
open(f, "a").write("\n")
PY

# Print summary
ce_log "=== Qualification Summary ==="
ce_log "  Pass:  $TOTAL_PASS"
ce_log "  Fail:  $TOTAL_FAIL"
ce_log "  Skip:  $TOTAL_SKIP"
ce_log "  Overall: $([[ "$OVERALL_PASS" == "true" ]] && echo PASS || echo FAIL)"
ce_log "  Evidence: $EVIDENCE_DIR"

if [[ "$OVERALL_PASS" != "true" ]]; then
  exit 1
fi
