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
#   CE_BINARY_PROVENANCE_FILE       Optional existing QA contract JSON with routerdArtifact + candidate_binary_hashes
#   CE_OTEL_QUERY_URL               Optional diagnostic query endpoint; missing/error is reported
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
#   2  Invalid setup/input
#   3  Observation or provenance incomplete; no release qualification
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
EXPECTED_PRODUCT_COMMIT=""
COMMIT=$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo "unknown")
FULL_COMMIT=$(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null || echo "unknown")
RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)-federation-qual-$(python3 -c 'import uuid;print(uuid.uuid4().hex[:12])')"

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
BINARY_PROVENANCE_FILE=${CE_BINARY_PROVENANCE_FILE:-}


# Auto-detected in preflight
SENDER_NODE=""
SENDER_NODE_B=""
RECEIVER_NODE=""
RECEIVER_NODE_B=""
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
  --commit SHA          Expected deployed product source commit (QA source recorded separately)
  -h, --help            Show this help

All 8 scenarios must PASS for a release qualification run.
SKIP counts as FAIL unless --allow-skip is set.
EOF
}

invalid_setup() { ce_log "$*"; exit 2; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --evidence-dir) EVIDENCE_DIR="${2:-}"; shift 2 ;;
    --cycles)       CYCLES="${2:-1}"; shift 2 ;;
    --duration)     DURATION="${2:-300}"; shift 2 ;;
    --scenarios)    SCENARIOS="${2:-}"; shift 2 ;;
    --allow-skip)   ALLOW_SKIP=true; shift ;;
    --commit)       COMMIT="${2:-}"; EXPECTED_PRODUCT_COMMIT="$COMMIT"; shift 2 ;;
    -h|--help)      usage; exit 0 ;;
    *)              invalid_setup "unknown argument: $1" ;;
  esac
done

[[ -n "$EVIDENCE_DIR" ]] || invalid_setup "--evidence-dir is required"
[[ -n "${CE_SENDER_SSH_HOST:-}" ]] || invalid_setup "CE_SENDER_SSH_HOST is required"
[[ -n "${CE_RECEIVER_SSH_HOST:-}" ]] || invalid_setup "CE_RECEIVER_SSH_HOST is required"
[[ -n "$EVENT_GROUP" ]] || invalid_setup "CE_EVENT_GROUP is required"

[[ "$CYCLES" =~ ^[0-9]+$ && "$CYCLES" -gt 0 ]] || invalid_setup "--cycles must be positive"
[[ "$DURATION" =~ ^[0-9]+$ && "$DURATION" -gt 0 ]] || invalid_setup "--duration must be positive"
command -v timeout >/dev/null || invalid_setup "GNU timeout is required"
# Reject empty, duplicate or partial release selections before the first SSH.
python3 - "$SCENARIOS" "$ALLOW_SKIP" <<'PYSEL'
import sys
selected=sys.argv[1].split(',')
required={'healthy','partition','ttl-refresh','restart','subscription','config-fault','security','multi-group'}
if not selected or len(selected)!=len(set(selected)) or not set(selected)<=required or (sys.argv[2]!='true' and set(selected)!=required):
    print('invalid/incomplete scenario selection',file=sys.stderr);raise SystemExit(2)
PYSEL
# The release dependency order is canonical: multi-group consumes this cycle's
# partition evidence. Development subsets retain their explicitly chosen order.
if [[ "$ALLOW_SKIP" != true ]]; then
  SCENARIOS="healthy,partition,ttl-refresh,restart,subscription,config-fault,security,multi-group"
fi
[[ -z "$EXPECTED_PRODUCT_COMMIT" || "$EXPECTED_PRODUCT_COMMIT" =~ ^[0-9a-f]{7,40}$ ]] || invalid_setup "invalid expected product commit"
for value in "$EVENT_GROUP" "$EVENT_GROUP_B" "$SUBSCRIPTION_KEY" "$ROUTERCTL" "$RECEIVER_ROUTERCTL" "$SENDER_CONFIG" "$RECEIVER_CONFIG" "$SENDER_STATE_DB" "$RECEIVER_STATE_DB"; do
  [[ "$value" != *[!a-zA-Z0-9_./:-]* ]] || invalid_setup "unsafe qualification identity/path"
done
# Reusing a directory must never overwrite a failed attempt or consume old PASS.
mkdir "$EVIDENCE_DIR" || invalid_setup "evidence directory already exists; choose a fresh attempt"
RAW_DIR="$EVIDENCE_DIR/raw"
mkdir "$RAW_DIR"

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

read_snapshot() {
  local label=$1 allow_doctor=$2; shift 2
  local attempt rc=0
  attempt=$(mktemp -d "$RAW_DIR/$label.XXXXXXXX")
  "$@" >"$attempt/stdout" 2>"$attempt/stderr" || rc=$?
  printf '%s\n' "$rc" >"$attempt/exit"
  if [[ "$rc" -ne 0 && ! ( "$allow_doctor" == true && "$rc" -eq 1 ) ]]; then return 3; fi
  python3 - "$attempt/stdout" <<'PYREAD' || return 3
import json,sys
data=json.load(open(sys.argv[1]))
if not isinstance(data,(dict,list)):raise ValueError('not a structured observation')
PYREAD
  cat "$attempt/stdout"
}
sender_doctor_json() {
  read_snapshot sender-doctor true sender_routerctl "doctor federation --config $SENDER_CONFIG --state-file $SENDER_STATE_DB -o json"
}
receiver_doctor_json() {
  read_snapshot receiver-doctor true receiver_routerctl "doctor federation --config $RECEIVER_CONFIG --state-file $RECEIVER_STATE_DB -o json"
}
sender_remediation_json() {
  read_snapshot remediation true sender_routerctl "doctor federation --config $SENDER_CONFIG --state-file $SENDER_STATE_DB -o json --remediation-plan"
}
sender_delivery_summary() {
  read_snapshot delivery-summary false sender_routerctl "federation deliveries summary --group $EVENT_GROUP --state-file $SENDER_STATE_DB -o json"
}
event_snapshot() {
  local role=$1 event_id=$2 group=${3:-$EVENT_GROUP} source=${4:-$SENDER_NODE} data
  if [[ "$role" == sender ]]; then
    data=$(read_snapshot sender-events false sender_routerctl "federation event list --group $group --state-file $SENDER_STATE_DB -o json") || return 3
  else
    data=$(read_snapshot receiver-events false receiver_routerctl "federation event list --group $group --state-file $RECEIVER_STATE_DB -o json") || return 3
  fi
  printf '%s' "$data" | current_observation event "$event_id" "$group" "$source"
}
current_observation() {
  # These predicates replace the existing scenario assertions; they do not
  # introduce measurements or a general-purpose qualification framework.
  python3 -c '
import json,sys,math,datetime
def field(row,key):
    return row.get(key,row.get({"ID":"id","EventID":"eventId"}.get(key,key[0].lower()+key[1:])))
def epoch(value):
    if not isinstance(value,str):raise ValueError("timestamp absent")
    return datetime.datetime.fromisoformat(value.replace("Z","+00:00")).timestamp()
def rows(value):
    if not isinstance(value,list):raise ValueError("list absent")
    if not all(isinstance(x,dict) for x in value):raise ValueError("invalid row")
    return value
def check(value):
    if not value:raise SystemExit(1)
kind,*args=sys.argv[1:]
try:
 d=json.load(sys.stdin)
 if kind=="event":
    matches=[x for x in rows(d) if field(x,"ID")==args[0] and field(x,"Group")==args[1] and field(x,"SourceNode")==args[2]]
    check(len(matches)==1)
    epoch(field(matches[0],"ObservedAt"));epoch(field(matches[0],"ExpiresAt"))
    print(json.dumps(matches[0]))
 elif kind=="expiry":
    check(field(d,"ID")==args[0] and field(d,"Group")==args[1] and field(d,"SourceNode")==args[2])
    check(epoch(field(d,"ExpiresAt"))==float(args[3]) and field(d,"Subject")==args[4])
 elif kind=="delivery":
    matches=[x for x in rows(d) if field(x,"EventID")==args[0] and field(x,"Peer")==args[1]]
    check(len(matches)==1)
    status=field(matches[0],"Status")
    if status not in ("pending","failed","delivered"):raise ValueError("unknown delivery status")
    print(status)
 elif kind=="subscription":
    matches=[x for x in rows(d) if field(x,"EventID")==args[0] and field(x,"Subscription")==args[1] and field(x,"EventGroup")==args[2] and epoch(field(x,"StartedAt"))>=epoch(args[3])]
    check(any(field(x,"Status")==args[4] for x in matches))
 elif kind in ("partition","multi-group"):
    doc=d["doctor"];rem=d["remediation"]
    if not isinstance(doc["checks"],list) or not isinstance(rem["remediationPlan"]["actions"],list):raise ValueError("checks/actions absent")
    ga,peer=args[:2]
    actions={"failed-deliveries":"retry-failed-deliveries","pending-deliveries":"investigate-pending-deliveries","delivery-lag":"check-peer-connectivity"}
    fault=[x for x in doc["checks"] if x.get("name","").split(" ",1)[0]==ga+"/"+peer and x.get("code") in actions and x.get("status") in ("WARN","FAIL","warn","fail")]
    check(bool(fault))
    for x in fault:
        check(any(a.get("action")==actions[x["code"]] and a.get("targetGroup")==ga and a.get("targetPeer")==peer for a in rem["remediationPlan"]["actions"]))
    if kind=="multi-group":
        gb=args[2]
        groups=rows(doc["federation"]["slo"]["groups"])
        chosen={g["group"]:g for g in groups if g.get("group") in (ga,gb)}
        if len(chosen)!=2 or ga==gb:raise ValueError("two scoped groups unavailable")
        before={x["group"]:x for x in rows(d["beforeDoctor"]["federation"]["slo"]["groups"])}
        for name in (ga,gb):
            if name not in before or before[name]["violations"] or before[name]["thresholds"]!=chosen[name]["thresholds"]:raise ValueError("healthy baseline/threshold correspondence absent")
        for g in chosen.values():
            check(g.get("defined") is True)
            thresholds=g["thresholds"]
            for area in ("delivery","subscription"):
                if not isinstance(thresholds[area],dict) or not thresholds[area]:raise ValueError("thresholds absent")
                if any(type(v) is not int or v<0 for v in thresholds[area].values()):raise ValueError("invalid threshold")
            rows(g["violations"])
        # Identical configured thresholds are legitimate. Require the seeded
        # affected A / healthy B result, and resolve each violation threshold.
        check(bool(chosen[ga]["violations"]) and not chosen[gb]["violations"])
        for v in chosen[ga]["violations"]:
            check(v.get("check","").split(" ",1)[0]==ga+"/"+peer and v.get("severity") in ("warn","fail","WARN","FAIL"))
            key,value=v["threshold"].split("=",1)
            configured=next((area[key] for area in chosen[ga]["thresholds"].values() if key in area),None)
            if key=="failedDeliveryCount":check(value=="0" and int(v["actual"])>0)
            elif configured is None:raise ValueError("violation threshold not bound")
            else:check(type(configured) is int and int(value)==configured)
        check(not any(a.get("targetGroup")==gb for a in rem["remediationPlan"]["actions"]))
    print("pass")
 elif kind=="stale":
    matches=[x for x in rows(d) if field(x,"Group")==args[0] and field(x,"Peer")==args[1]]
    if len(matches)!=1 or type(field(matches[0],"StaleTTL")) is not int or field(matches[0],"StaleTTL")<0:raise ValueError("scoped staleTTL missing")
    print(field(matches[0],"StaleTTL"))
 elif kind=="absent":
    values=rows(d)
    if any(not isinstance(field(x,"ID"),str) or not field(x,"ID") for x in values):raise ValueError("receiver record identity absent")
    check(not any(field(x,"ID")==args[0] for x in values))
 elif kind=="otel":
    if d.get("status")!="success":raise ValueError("API unavailable")
    series=rows(d["data"]["result"]);values=[float(v[1]) for x in series for v in x["values"]]
    if not values or not all(math.isfinite(v) for v in values):raise ValueError("samples absent/nonfinite")
    print(max(values))
 elif kind=="labels":
    if d.get("status")!="success" or not isinstance(d["data"],list) or not all(isinstance(x,str) for x in d["data"]):raise ValueError("labels unavailable")
    forbidden={"event_id","subject","address","endpoint","error_message","raw_error","token","secret","credential"}
    check(not any(x.lower() in forbidden for x in d["data"]))
    print("pass")
 else:raise ValueError("unknown predicate")
except (ValueError,KeyError,TypeError,IndexError,AttributeError,OverflowError) as e:
 print("observation_inconclusive: "+str(e),file=sys.stderr);sys.exit(3)
' "$@"
}

timestamp_utc() { date -u +%Y-%m-%dT%H:%M:%SZ; }

remote_binary_info() {
  local host=$1 binary_path=$2
  ce_ssh "$host" "python3 - '$binary_path' <<'PYBIN'
import pathlib,subprocess,shutil,hashlib,json,re,sys
name=sys.argv[1];path=shutil.which(name) or name
p=pathlib.Path(path);digest=hashlib.sha256(p.read_bytes()).hexdigest()
version=None
if p.name=='routerctl':
 r=subprocess.run([str(p),'version'],text=True,capture_output=True,timeout=5)
 if r.returncode==0:version=r.stdout.strip()
build=''
if shutil.which('go'):
 r=subprocess.run(['go','version','-m',str(p)],text=True,capture_output=True,timeout=5)
 if r.returncode==0:build=r.stdout
match=re.search(r'vcs.revision=([0-9a-f]{40})',build) or re.search(r'(?:routerd/pkg/version|routerversion)[.]Commit=([0-9a-f]{40})',build) or re.search(r'\(([0-9a-f]{40})\)',version or '')
label=re.search(r'\(([0-9a-f]{7,40})\)',version or '')
print(json.dumps({'path':str(p),'sha256':digest,'version':version,'sourceCommit':match.group(1) if match else None,'runtimeCommitLabel':label.group(1) if label else None,'buildMetadata':build or None}))
PYBIN"
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
  [[ "$SENDER_NODE" =~ ^[a-zA-Z0-9_.:-]+$ && "$RECEIVER_NODE" =~ ^[a-zA-Z0-9_.:-]+$ && "$RECEIVER_ENDPOINT" =~ ^https?://[a-zA-Z0-9_.:/-]+$ ]] || ce_die "invalid discovered topology identity/endpoint"

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
  if [[ ",$SCENARIOS," == *,multi-group,* ]]; then
    [[ -n "$EVENT_GROUP_B" ]] || ce_die "selected multi-group requires CE_EVENT_GROUP_B"
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

  # OTel query availability is diagnostic and is recorded per cycle.
  ce_log "preflight PASS"
}

# ============================================================================
# Binary provenance (Section 2)
# ============================================================================

collect_provenance() {
  local sender_routerctl_info receiver_routerctl_info sender_eventd_info receiver_eventd_info sender_digest receiver_digest
  sender_routerctl_info=$(read_snapshot sender-routerctl-binary false remote_binary_info "$CE_SENDER_SSH_HOST" "$ROUTERCTL") || return 3
  receiver_routerctl_info=$(read_snapshot receiver-routerctl-binary false remote_binary_info "$CE_RECEIVER_SSH_HOST" "$RECEIVER_ROUTERCTL") || return 3
  sender_eventd_info=$(read_snapshot sender-eventd-binary false remote_binary_info "$CE_SENDER_SSH_HOST" routerd-eventd) || return 3
  receiver_eventd_info=$(read_snapshot receiver-eventd-binary false remote_binary_info "$CE_RECEIVER_SSH_HOST" routerd-eventd) || return 3
  sender_digest=$(sender_ssh "sha256sum $SENDER_CONFIG") || return 3
  receiver_digest=$(receiver_ssh "sha256sum $RECEIVER_CONFIG") || return 3
  python3 - "$EVIDENCE_DIR/provenance.json" "$FULL_COMMIT" "$EXPECTED_PRODUCT_COMMIT" \
    "$CE_SENDER_SSH_HOST" "$CE_RECEIVER_SSH_HOST" "$SENDER_NODE" "$RECEIVER_NODE" \
    "$sender_routerctl_info" "$receiver_routerctl_info" "$sender_eventd_info" "$receiver_eventd_info" "$sender_digest" "$receiver_digest" "$BINARY_PROVENANCE_FILE" <<'PYPROV'
import json,re,sys,pathlib,hashlib
path,qa,expected,sh,rh,sn,rn,*data=sys.argv[1:]
binaries=[json.loads(x) for x in data[:4]]
digest=lambda x:x.split()[0] if x.split() else ''
config=[digest(x) for x in data[4:6]]
valid=lambda x:isinstance(x,str) and re.fullmatch('[0-9a-f]{64}',x) is not None
artifact=all(valid(b.get('sha256')) for b in binaries) and all(valid(x) for x in config)
# Reuse the existing prepared/release contract representation. The manifest is
# local coordinator evidence; remote Go is optional. Bind its declared source
# to each CURRENT binary digest, and preserve conflicts rather than guessing.
manifest={'available':False,'classification':'not provided'}
if data[6]:
 try:
  raw=pathlib.Path(data[6]).read_bytes();contract=json.loads(raw)
  release=contract['routerdArtifact'];hashes=contract['execution']['candidate_binary_hashes']
  source=release['commit']
  if not re.fullmatch('[0-9a-f]{40}',source) or not valid(release['sha256']) or not isinstance(hashes,dict):raise ValueError('source/artifact binding absent')
  target=contract['execution'].get('target_release_artifact')
  if target is not None and (target['commit']!=source or target['sha256']!=release['sha256']):raise ValueError('artifact declarations conflict')
  selected={k:v for k,v in hashes.items() if pathlib.PurePosixPath(k).name in ('routerctl','routerd-eventd')}
  if not selected or not all(valid(v) for v in selected.values()):raise ValueError('binary digest binding absent')
  manifest={'available':True,'path':data[6],'sha256':hashlib.sha256(raw).hexdigest(),'sourceCommit':source,'artifactSha256':release['sha256'],'binaryHashes':selected,'classification':'available'}
 except (OSError,ValueError,KeyError,TypeError) as e:manifest={'available':False,'classification':'observation_inconclusive','reason':str(e)}
sources=[]
for i,b in enumerate(binaries):
 observed=b.get('sourceCommit');label=b.get('runtimeCommitLabel');bound=None;hash_conflict=False
 if manifest['available']:
  name=('routerctl','routerctl','routerd-eventd','routerd-eventd')[i]
  expected_hashes={v for k,v in manifest['binaryHashes'].items() if pathlib.PurePosixPath(k).name==name}
  if expected_hashes=={b.get('sha256')}:bound=manifest['sourceCommit']
  elif expected_hashes:hash_conflict=True
 conflict=hash_conflict or bool(bound and ((observed and observed!=bound) or (label and not bound.startswith(label))))
 b['manifestBinaryMatch']=False if hash_conflict else True if bound else None
 resolved=None if conflict else observed or bound
 b['sourceIdentityMethod']='conflict' if conflict else 'binary metadata' if observed else 'existing manifest + current binary SHA-256' if bound else 'unconfirmed'
 b['resolvedSourceCommit']=resolved;sources.append(resolved)
source=all(isinstance(x,str) and re.fullmatch('[0-9a-f]{40}',x) for x in sources) and len(set(sources))==1
match=bool(source and (not expected or sources[0].startswith(expected)))
qualified=bool(artifact and match)
result={'manifestBinding':manifest,'qaSourceCommit':qa,'expectedProductCommit':expected or None,'artifactIdentityObserved':artifact,'sourceIdentityVerified':match,'qualifiedProvenance':qualified,'classification':'pass' if qualified else 'observation_inconclusive','sender':{'sshTarget':sh,'nodeName':sn,'routerctl':binaries[0],'routerdEventd':binaries[2],'configDigest':config[0]},'receiver':{'sshTarget':rh,'nodeName':rn,'routerctl':binaries[1],'routerdEventd':binaries[3],'configDigest':config[1]}}
json.dump(result,open(path,'w'),indent=2)
raise SystemExit(0 if qualified else 3)
PYPROV
}

# ============================================================================
# Fault injection helpers — remote SSH, no local eval (Section 4)
# ============================================================================

_CLEANUP_RAN=false
_cleanup_faults() {
  [[ "$_CLEANUP_RAN" == false ]] || return 0
  _CLEANUP_RAN=true
  local partition_rc=0 plugin_rc=0
  if [[ "$_PARTITION_ACTIVE" == true || -f "$CYCLE_DIR/partition-attempted" ]]; then partition_restore || partition_rc=$?; fi
  if [[ "$_PLUGIN_BROKEN" == true || -f "$CYCLE_DIR/plugin-attempted" ]]; then plugin_restore || plugin_rc=$?; fi
  python3 - "$CYCLE_DIR/cleanup.json" "$partition_rc" "$plugin_rc" <<'PYCLEAN'
import json,sys
json.dump({'partitionExit':int(sys.argv[2]),'pluginExit':int(sys.argv[3]),'confirmed':sys.argv[2:]==['0','0']},open(sys.argv[1],'w'),indent=2)
PYCLEAN
}
# Traps are installed only inside the bounded worker, which owns these faults.

partition_apply() {
  [[ -n "${CE_SENDER_PARTITION_APPLY:-}" ]] || return 1
  ce_log "partition: applying"
  : >"$CYCLE_DIR/partition-attempted"
  sender_ssh "$CE_SENDER_PARTITION_APPLY" || { ce_log "FAIL: partition apply command failed"; return 1; }
  _PARTITION_ACTIVE=true
  local observation
  sleep 1
  observation=$(endpoint_reachability) || return 3
  [[ "$observation" == unreachable ]] || { ce_log "partition effect not observed"; return 1; }
  ce_log "partition: verified (endpoint unreachable)"
}

# Print the measured TCP result only after SSH acknowledged the probe. An SSH
# error is acquisition failure, never evidence that the partition took effect.
endpoint_reachability() {
  local endpoint_host endpoint_port observation
  read -r endpoint_host endpoint_port < <(python3 -c 'import urllib.parse,sys;p=urllib.parse.urlsplit(sys.argv[1]);assert p.scheme in ("http","https") and p.hostname;print(p.hostname,p.port or (443 if p.scheme=="https" else 80))' "$RECEIVER_ENDPOINT") || return 3
  [[ "$endpoint_host" =~ ^[a-zA-Z0-9_.:-]+$ && "$endpoint_port" =~ ^[0-9]+$ ]] || return 3
  observation=$(sender_ssh "python3 - '$endpoint_host' '$endpoint_port' <<'PYTCP'
import socket,sys
try:
 with socket.create_connection((sys.argv[1],int(sys.argv[2])),timeout=3):pass
 print('reachable')
except (TimeoutError,ConnectionRefusedError,ConnectionResetError):print('unreachable')
except OSError as e:
 if e.errno in (101,113):print('unreachable')
 else:raise
PYTCP") || return 3
  [[ "$observation" == reachable || "$observation" == unreachable ]] || return 3
  printf '%s\n' "$observation"
}

partition_restore() {
  [[ -n "${CE_SENDER_PARTITION_RESTORE:-}" ]] || return 1
  ce_log "partition: restoring"
  sender_ssh "$CE_SENDER_PARTITION_RESTORE" || return 3
  local end=$((SECONDS + 15)) observation
  while [[ $SECONDS -lt $end ]]; do
    observation=$(endpoint_reachability) || return 3
    if [[ "$observation" == reachable ]]; then
      _PARTITION_ACTIVE=false
      rm -f "$CYCLE_DIR/partition-attempted"
      ce_log "partition: verified removed (endpoint reachable)"
      return 0
    fi
    sleep 1
  done
  return 3
}

plugin_fail() {
  [[ -n "${CE_RECEIVER_PLUGIN_FAIL:-}" ]] || return 1
  ce_log "plugin: injecting failure"
  : >"$CYCLE_DIR/plugin-attempted"
  receiver_ssh "$CE_RECEIVER_PLUGIN_FAIL" || { ce_log "FAIL: plugin fail command failed"; return 1; }
  _PLUGIN_BROKEN=true
  return 0
}

plugin_restore() {
  [[ -n "${CE_RECEIVER_PLUGIN_RESTORE:-}" ]] || return 1
  ce_log "plugin: restoring"
  receiver_ssh "$CE_RECEIVER_PLUGIN_RESTORE" || { ce_log "FAIL: plugin restore command failed"; return 1; }
  _PLUGIN_BROKEN=false
  rm -f "$CYCLE_DIR/plugin-attempted"
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
    --source-node ${5:-$SENDER_NODE} \
    --ttl $ttl \
    --state-file $SENDER_STATE_DB"
}

wait_delivery() {
  local event_id=$1 timeout=${2:-60} group=${3:-$EVENT_GROUP} status rc
  local deadline=$((SECONDS + timeout))
  while [[ $SECONDS -lt $deadline ]]; do
    rc=0;status=$(get_delivery_status "$event_id" "$group") || rc=$?
    [[ "$rc" -ne 3 ]] || return 3
    [[ "$rc" -ne 0 || "$status" != delivered ]] || return 0
    sleep 2
  done
  return 3
}

wait_doctor_healthy() {
  local timeout=${1:-60} doc rc
  local deadline=$((SECONDS + timeout))
  while [[ $SECONDS -lt $deadline ]]; do
    doc=$(sender_doctor_json) || return 3
    rc=0
    python3 -c 'import json,sys;d=json.loads(sys.argv[1]);g=[x for x in d["federation"]["slo"]["groups"] if x["group"]==sys.argv[2]];assert len(g)==1 and isinstance(g[0]["violations"],list);sys.exit(0 if not g[0]["violations"] else 1)' "$doc" "$EVENT_GROUP" || rc=$?
    [[ "$rc" -ne 0 ]] || return 0
    [[ "$rc" -eq 1 ]] || return 3
    sleep 3
  done
  return 3
}

receiver_has_event() { event_snapshot receiver "$@"; }

get_delivery_status() {
  local event_id=$1 group=${2:-$EVENT_GROUP} data peer=$RECEIVER_NODE
  if [[ "$group" == "$EVENT_GROUP_B" ]]; then peer=$RECEIVER_NODE_B; fi
  [[ -n "$peer" ]] || return 3
  data=$(read_snapshot deliveries false sender_routerctl "federation event deliveries --group $group --event-id $event_id --state-file $SENDER_STATE_DB -o json") || return 3
  printf '%s' "$data" | current_observation delivery "$event_id" "$peer"
}

get_eventd_main_pid() {
  local host=$1 unit=$2 pid
  pid=$(ce_ssh "$host" "sudo systemctl show -p MainPID --value $unit") || return 3
  [[ "$pid" =~ ^[0-9]+$ && "$pid" -gt 0 ]] || return 3
  printf '%s\n' "$pid"
}

# ============================================================================
# OTel metrics query (Section 8)
# ============================================================================

query_otel_metric() {
  local metric=$1 time_start=$2 time_end=$3 data
  [[ -n "$OTEL_QUERY_URL" ]] || return 3
  data=$(read_snapshot "otel-$metric" false curl -fsS --connect-timeout 5 --max-time 10 \
    "$OTEL_QUERY_URL/api/v1/query_range" --data-urlencode "query=increase(${metric}[5m])" \
    --data-urlencode "start=$time_start" --data-urlencode "end=$time_end" --data-urlencode step=60s) || return 3
  printf '%s' "$data" | current_observation otel
}
check_high_cardinality_labels() {
  local data
  [[ -n "$OTEL_QUERY_URL" ]] || return 3
  data=$(read_snapshot otel-labels false curl -fsS --connect-timeout 5 --max-time 10 "$OTEL_QUERY_URL/api/v1/labels" --data-urlencode "match[]=$1") || return 3
  printf '%s' "$data" | current_observation labels
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
    "classification": "observation_inconclusive" if result == "inconclusive" else "none" if result == "pass" else "assertion_failure",
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
  python3 - "$file" "$RUN_ID" "$COMMIT" "$CYCLE_NUM" <<'PY'
import json, sys, pathlib
try:
    with open(sys.argv[1]) as f:
        d = json.load(f)
    required = ["runId", "commit", "scenario", "result", "cycle"]
    for k in required:
        if k not in d:
            print(f"missing required field: {k}", file=sys.stderr)
            sys.exit(1)
    if d["runId"]!=sys.argv[2] or d["commit"]!=sys.argv[3] or type(d["cycle"]) is not int or d["cycle"]!=int(sys.argv[4]) or d["scenario"]!=pathlib.Path(sys.argv[1]).stem: raise ValueError("current attempt/cycle/scenario identity mismatch")
    if d["result"] not in ("pass","fail","skip","inconclusive"): raise ValueError("unknown result")
    if d["result"] in ("fail", "skip", "inconclusive") and "reason" not in d:
        print("result=fail/skip requires 'reason' field", file=sys.stderr)
        sys.exit(1)
    if d["result"] == "pass":
        for ts in ("startedAt", "endedAt"):
            if ts not in d:
                print(f"result=pass requires '{ts}' field", file=sys.stderr)
                sys.exit(1)
    sys.exit(0)
except (json.JSONDecodeError, ValueError) as e:
    print(f"invalid JSON: {e}", file=sys.stderr)
    sys.exit(1)
PY
}

# The existing per-scenario budget covers the worker and its cleanup. TERM
# allows a final cleanup attempt; forced termination is recorded, and pending
# fault ownership stops later scenarios/cycles rather than adding retries.
run_bounded() {
  local label=$1 function_name=$2 rc=0 budget started ended worker
  started=$(python3 -c 'import time;print(time.monotonic())')
  worker=$(mktemp -d "$RAW_DIR/worker-$label.XXXXXXXX")
  {
    printf 'set -euo pipefail\n'
    declare -f
    declare -p EVIDENCE_DIR RAW_DIR CYCLES DURATION SCENARIOS ALLOW_SKIP COMMIT FULL_COMMIT EXPECTED_PRODUCT_COMMIT RUN_ID \
      ROUTERCTL RECEIVER_ROUTERCTL SENDER_CONFIG RECEIVER_CONFIG SENDER_STATE_DB RECEIVER_STATE_DB \
      EVENT_GROUP EVENT_GROUP_B SUBSCRIPTION_KEY OTEL_QUERY_URL BINARY_PROVENANCE_FILE SENDER_NODE SENDER_NODE_B RECEIVER_NODE RECEIVER_NODE_B RECEIVER_ENDPOINT \
      SENDER_EVENTD_UNIT RECEIVER_EVENTD_UNIT CYCLE_DIR CYCLE_NUM cycle_start_ts cycle_end_ts SCENARIO_RESULTS _PARTITION_ACTIVE _PLUGIN_BROKEN _CLEANUP_RAN 2>/dev/null
    printf '_CLEANUP_RAN=false\ntrap _cleanup_faults EXIT TERM INT\n%s\n' "$function_name"
  } >"$worker/source.sh"
  budget=$(python3 -c 'import sys,time;print(max(.01,float(sys.argv[1])+float(sys.argv[2])-time.monotonic()-.1))' "$started" "$DURATION")
  timeout --signal=TERM --kill-after=.1s "${budget}s" bash "$worker/source.sh" >"$worker/stdout" 2>"$worker/stderr" || rc=$?
  ended=$(python3 -c 'import time;print(time.monotonic())')
  python3 - "$worker/budget.json" "$started" "$ended" "$DURATION" "$rc" <<'PYBUDGET'
import json,sys
start,end,budget=float(sys.argv[2]),float(sys.argv[3]),int(sys.argv[4])
json.dump({'startedMonotonic':start,'endedMonotonic':end,'budgetSeconds':budget,'elapsedSeconds':end-start,'remainingSeconds':max(0,budget-(end-start)),'exit':int(sys.argv[5]),'retryCount':0},open(sys.argv[1],'w'),indent=2)
PYBUDGET
  return "$rc"
}
preflight_and_provenance() {
  preflight
  collect_provenance
  # declare -p quotes discovered config strings as data. No secret value is read
  # into this context file; parent uses this attempt only.
  declare -p SENDER_NODE RECEIVER_NODE RECEIVER_ENDPOINT SENDER_EVENTD_UNIT RECEIVER_EVENTD_UNIT >"$EVIDENCE_DIR/preflight-context.sh"
}
run_otel_diagnostics() { verify_otel_metrics "$cycle_start_ts" "$cycle_end_ts"; }

inconclusive() {
  local scenario=$1 start=$2 reason=$3; shift 3
  scenario_evidence "$scenario" inconclusive "$reason" "startedAt=$start" "endedAt=$(timestamp_utc)" "$@"
}
wait_subscription() {
  local event_id=$1 anchor=$2 expected=$3 qualified=$4 deadline=$((SECONDS + 30)) data rc
  while [[ $SECONDS -lt $deadline ]]; do
    data=$(read_snapshot subscription-runs false receiver_routerctl "federation subscription runs --subscription '$qualified' --state-file $RECEIVER_STATE_DB -o json") || return 3
    rc=0
    printf '%s' "$data" | current_observation subscription "$event_id" "$qualified" "$EVENT_GROUP" "$anchor" "$expected" >/dev/null || rc=$?
    [[ "$rc" -ne 0 ]] || { printf '%s' "$data"; return 0; }
    [[ "$rc" -ne 3 ]] || return 3
    sleep 2
  done
  return 3
}

# ============================================================================
# SCENARIOS (Section 7)
# ============================================================================

scenario_healthy() {
  local start_ts event_id doctor remediation receiver_record rc=0
  start_ts=$(timestamp_utc);event_id="qual-healthy-$RUN_ID-c$CYCLE_NUM"
  emit_test_event "$event_id" "10.99.1.1/32" || { inconclusive healthy "$start_ts" "current event emit failed"; return; }
  wait_delivery "$event_id" 60 || { inconclusive healthy "$start_ts" "current delivery unavailable"; return; }
  receiver_record=$(event_snapshot receiver "$event_id") || { inconclusive healthy "$start_ts" "current receiver record unavailable"; return; }
  doctor=$(sender_doctor_json) || { inconclusive healthy "$start_ts" "doctor acquisition unavailable"; return; }
  remediation=$(sender_remediation_json) || { inconclusive healthy "$start_ts" "remediation acquisition unavailable"; return; }
  python3 - "$doctor" "$remediation" "$EVENT_GROUP" <<'PYHEALTH' || rc=$?
import json,sys
try:
 d=json.loads(sys.argv[1]);r=json.loads(sys.argv[2]);name=sys.argv[3]
 groups=[x for x in d['federation']['slo']['groups'] if x['group']==name]
 if len(groups)!=1 or not isinstance(groups[0]['violations'],list) or not isinstance(r['remediationPlan']['actions'],list):raise ValueError('scoped observations absent')
 failed=bool(groups[0]['violations'] or any(x.get('targetGroup')==name for x in r['remediationPlan']['actions']))
 raise SystemExit(1 if failed else 0)
except (ValueError,KeyError,TypeError):raise SystemExit(3)
PYHEALTH
  if [[ "$rc" -eq 3 ]]; then inconclusive healthy "$start_ts" "scoped health observation invalid"
  elif [[ "$rc" -eq 1 ]]; then scenario_evidence healthy fail "scoped group violations/remediation observed" "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "doctorAfter=$doctor" "remediationPlan=$remediation"
  else scenario_evidence healthy pass "" "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "doctorAfter=$doctor" "remediationPlan=$remediation" "receiverRecord=$receiver_record" "testedEventId=$event_id"; fi
}

scenario_partition() {
  local start_ts event_id event_b="" b_during="" doctor_before doctor_during remediation_during fault_data status rc proof=false isolation=false
  start_ts=$(timestamp_utc)
  if [[ -z "${CE_SENDER_PARTITION_APPLY:-}" || -z "${CE_SENDER_PARTITION_RESTORE:-}" ]]; then
    inconclusive partition "$start_ts" "scoped partition not configured"; return
  fi
  [[ ",$SCENARIOS," != *,multi-group,* ]] || isolation=true
  if [[ "$isolation" == true && -z "$EVENT_GROUP_B" ]]; then
    inconclusive partition "$start_ts" "selected multi-group isolation requires group B"; return
  fi
  if [[ "$isolation" == true ]]; then
  # Read B's declared sender identity; no extra host or fault is added.
  SENDER_NODE_B=$(sender_ssh "python3 -c \"import yaml;d=yaml.safe_load(open('$SENDER_CONFIG'));print(next(r['spec']['nodeName'] for r in d['spec']['resources'] if r['kind']=='EventGroup' and r['metadata']['name']=='$EVENT_GROUP_B'))\"") || { inconclusive partition "$start_ts" "group B identity unavailable"; return; }
  [[ "$SENDER_NODE_B" =~ ^[a-zA-Z0-9_.:-]+$ ]] || { inconclusive partition "$start_ts" "group B identity invalid"; return; }
  RECEIVER_NODE_B=$(sender_ssh "python3 -c \"import yaml;d=yaml.safe_load(open('$SENDER_CONFIG'));print(next(r['spec']['nodeName'] for r in d['spec']['resources'] if r['kind']=='EventPeer' and r['spec']['groupRef']=='$EVENT_GROUP_B'))\"") || { inconclusive partition "$start_ts" "group B peer identity unavailable"; return; }
  [[ "$RECEIVER_NODE_B" =~ ^[a-zA-Z0-9_.:-]+$ ]] || { inconclusive partition "$start_ts" "group B peer identity invalid"; return; }
  local configured_receiver_b
  configured_receiver_b=$(receiver_ssh "python3 -c \"import yaml;d=yaml.safe_load(open('$RECEIVER_CONFIG'));print(next(r['spec']['nodeName'] for r in d['spec']['resources'] if r['kind']=='EventGroup' and r['metadata']['name']=='$EVENT_GROUP_B'))\"") || { inconclusive partition "$start_ts" "declared B receiver unavailable on selected receiver"; return; }
  [[ "$configured_receiver_b" == "$RECEIVER_NODE_B" ]] || { inconclusive partition "$start_ts" "B peer does not identify this receiver; no fault injected"; return; }
  fi
  doctor_before=$(sender_doctor_json) || { inconclusive partition "$start_ts" "baseline doctor unavailable"; return; }
  local baseline_groups=("$EVENT_GROUP")
  [[ "$isolation" != true ]] || baseline_groups+=("$EVENT_GROUP_B")
  python3 -c 'import json,sys;d=json.loads(sys.argv[1]);g={x["group"]:x for x in d["federation"]["slo"]["groups"]};assert all(g[n]["defined"] and g[n]["violations"]==[] for n in sys.argv[2:])' "$doctor_before" "${baseline_groups[@]}" || { inconclusive partition "$start_ts" "selected healthy baseline not established; no fault injected"; return; }
  partition_apply || { inconclusive partition "$start_ts" "partition command/effect not confirmed"; return; }
  event_id="qual-partition-$RUN_ID-c$CYCLE_NUM"
  event_b="qual-isolation-$RUN_ID-c$CYCLE_NUM"
  emit_test_event "$event_id" "10.99.2.1/32" || { inconclusive partition "$start_ts" "current A event emit failed"; return; }
  if [[ "$isolation" == true ]]; then
  emit_test_event "$event_b" "10.99.2.2/32" "$EVENT_GROUP_B" 600s "$SENDER_NODE_B" || { inconclusive partition "$start_ts" "current B event emit failed"; return; }
  # Confirm the healthy group during the fault, rather than an empty violation set.
  wait_delivery "$event_b" 30 "$EVENT_GROUP_B" || { inconclusive partition "$start_ts" "current group B delivery not observed during scoped A fault"; return; }
  b_during=$(event_snapshot receiver "$event_b" "$EVENT_GROUP_B" "$SENDER_NODE_B") || { inconclusive partition "$start_ts" "current healthy B receiver record unavailable"; return; }
  fi
  while true; do
    rc=0;status=$(get_delivery_status "$event_id") || rc=$?
    [[ "$rc" -ne 3 ]] || { inconclusive partition "$start_ts" "current A delivery acquisition unavailable"; return; }
    if [[ "$rc" -eq 1 ]]; then sleep 2; continue; fi
    if [[ "$status" == delivered ]]; then
      scenario_evidence partition fail "current event delivered during confirmed partition" "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "testedEventId=$event_id"; return
    fi
    doctor_during=$(sender_doctor_json) || { inconclusive partition "$start_ts" "fault doctor unavailable"; return; }
    remediation_during=$(sender_remediation_json) || { inconclusive partition "$start_ts" "fault remediation unavailable"; return; }
    fault_data=$(python3 -c 'import json,sys;print(json.dumps({"doctor":json.loads(sys.argv[1]),"remediation":json.loads(sys.argv[2]),"beforeDoctor":json.loads(sys.argv[3])}))' "$doctor_during" "$remediation_during" "$doctor_before")
    rc=0
    if [[ "$isolation" == true ]]; then
      printf '%s' "$fault_data" | current_observation multi-group "$EVENT_GROUP" "$RECEIVER_NODE" "$EVENT_GROUP_B" >/dev/null || rc=$?
    else
      printf '%s' "$fault_data" | current_observation partition "$EVENT_GROUP" "$RECEIVER_NODE" >/dev/null || rc=$?
    fi
    if [[ "$rc" -eq 0 ]]; then proof=true; break; fi
    [[ "$rc" -ne 3 ]] || { inconclusive partition "$start_ts" "scoped fault/remediation observation malformed"; return; }
    sleep 2
  done
  # Immutable current-cycle snapshot for multi-group: no second fault/re-query
  # after recovery can silently substitute an all-healthy historical report.
  printf '%s\n' "$fault_data" >"$CYCLE_DIR/partition-isolation-snapshot.json"
  partition_restore || { inconclusive partition "$start_ts" "partition recovery not confirmed"; return; }
  wait_delivery "$event_id" 60 || { inconclusive partition "$start_ts" "current A delivery recovery not observed"; return; }
  receiver_has_event "$event_id" >/dev/null || { inconclusive partition "$start_ts" "recovered A receiver record unavailable"; return; }
  scenario_evidence partition pass "" "startedAt=$start_ts" "endedAt=$(timestamp_utc)" \
    "doctorDuring=$doctor_during" "remediationDuring=$remediation_during" "hasFaultCheck=$proof" \
    "testedEventId=$event_id" "healthyEventId=$event_b" "healthyReceiverDuring=$b_during" "multiGroupIsolationSelected=$isolation"
}

scenario_ttl_refresh() {
  local start_ts event_id initial refreshed initial_expiry expected_expiry receiver_record rc deadline diagnostic summary="" stale=""
  start_ts=$(timestamp_utc);event_id="qual-ttl-$RUN_ID-c$CYCLE_NUM"
  emit_test_event "$event_id" "10.99.3.1/32" "$EVENT_GROUP" 120s || { inconclusive ttl-refresh "$start_ts" "initial event emit failed"; return; }
  wait_delivery "$event_id" 30 || { inconclusive ttl-refresh "$start_ts" "initial current delivery unavailable"; return; }
  initial=$(event_snapshot receiver "$event_id") || { inconclusive ttl-refresh "$start_ts" "initial receiver expiry unavailable"; return; }
  initial_expiry=$(printf '%s' "$initial" | python3 -c 'import json,sys,datetime;r=json.load(sys.stdin);print(datetime.datetime.fromisoformat(r["ExpiresAt"].replace("Z","+00:00")).timestamp())') || { inconclusive ttl-refresh "$start_ts" "initial expiry invalid"; return; }
  partition_apply || { inconclusive ttl-refresh "$start_ts" "partition not confirmed"; return; }
  emit_test_event "$event_id" "10.99.3.1/32" "$EVENT_GROUP" 600s || { inconclusive ttl-refresh "$start_ts" "current refresh emit failed"; return; }
  refreshed=$(event_snapshot sender "$event_id") || { inconclusive ttl-refresh "$start_ts" "current sender refresh record unavailable"; return; }
  expected_expiry=$(python3 -c 'import json,sys,datetime;r=json.loads(sys.argv[1]);e=lambda k:datetime.datetime.fromisoformat(r[k].replace("Z","+00:00")).timestamp();x=e("ExpiresAt");assert x>float(sys.argv[2]) and x-e("ObservedAt")==600;print(x)' "$refreshed" "$initial_expiry") || { inconclusive ttl-refresh "$start_ts" "expected extended source expiry not established"; return; }
  partition_restore || { inconclusive ttl-refresh "$start_ts" "partition restore unavailable"; return; }
  deadline=$((SECONDS + 60))
  while [[ $SECONDS -lt $deadline ]]; do
    rc=0
    receiver_record=$(event_snapshot receiver "$event_id") || rc=$?
    [[ "$rc" -ne 3 ]] || { inconclusive ttl-refresh "$start_ts" "receiver refresh acquisition unavailable"; return; }
    if [[ "$rc" -eq 0 ]]; then
      printf '%s' "$receiver_record" | current_observation expiry "$event_id" "$EVENT_GROUP" "$SENDER_NODE" "$expected_expiry" "10.99.3.1/32" >/dev/null || rc=$?
      [[ "$rc" -ne 3 ]] || { inconclusive ttl-refresh "$start_ts" "receiver expiry malformed"; return; }
      [[ "$rc" -ne 0 ]] || break
    fi
    sleep 2
  done
  [[ "$rc" -eq 0 ]] || { inconclusive ttl-refresh "$start_ts" "current extended expiry not observed; old delivered row is insufficient" "initialReceiver=$initial" "refreshedSender=$refreshed"; return; }
  # Aggregate staleTTL is diagnostic. Never substitute zero for missing data or
  # require a global counter to settle before accepting the actual event expiry.
  diagnostic=observation_inconclusive
  if summary=$(sender_delivery_summary) && stale=$(printf '%s' "$summary" | current_observation stale "$EVENT_GROUP" "$RECEIVER_NODE"); then diagnostic=observed; fi
  scenario_evidence ttl-refresh pass "" "startedAt=$start_ts" "endedAt=$(timestamp_utc)" \
    "initialReceiver=$initial" "refreshedSender=$refreshed" "refreshedReceiver=$receiver_record" "expectedExpiryEpoch=$expected_expiry" \
    "staleTTLDiagnosticStatus=$diagnostic" "staleTTLDiagnostic=$stale" "deliverySummary=$summary" "testedEventId=$event_id"
}

scenario_restart() {
  ce_log "=== Scenario: eventd restart recovery ==="
  local start_ts
  start_ts=$(timestamp_utc)

  # Record sender PID before restart
  local sender_pid_before
  sender_pid_before=$(get_eventd_main_pid "$CE_SENDER_SSH_HOST" "$SENDER_EVENTD_UNIT") || { inconclusive restart "$start_ts" "PID acquisition unavailable"; return; }

  local doctor_before
  doctor_before=$(sender_doctor_json) || true

  # Restart sender eventd
  ce_log "restarting sender eventd"
  if ! sender_ssh "sudo systemctl restart $SENDER_EVENTD_UNIT"; then
    scenario_evidence restart inconclusive "sender systemctl restart failed" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi
  sleep 3

  # Verify PID changed
  local sender_pid_after
  sender_pid_after=$(get_eventd_main_pid "$CE_SENDER_SSH_HOST" "$SENDER_EVENTD_UNIT") || { inconclusive restart "$start_ts" "PID acquisition unavailable"; return; }
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
    scenario_evidence restart inconclusive "emit after sender restart failed" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"; return; }

  if ! wait_delivery "$event_id1" 60; then
    scenario_evidence restart inconclusive "delivery not confirmed after sender restart" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi

  # Restart receiver eventd
  local receiver_pid_before
  receiver_pid_before=$(get_eventd_main_pid "$CE_RECEIVER_SSH_HOST" "$RECEIVER_EVENTD_UNIT") || { inconclusive restart "$start_ts" "PID acquisition unavailable"; return; }

  ce_log "restarting receiver eventd"
  if ! receiver_ssh "sudo systemctl restart $RECEIVER_EVENTD_UNIT"; then
    scenario_evidence restart inconclusive "receiver systemctl restart failed" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi
  sleep 3

  local receiver_pid_after
  receiver_pid_after=$(get_eventd_main_pid "$CE_RECEIVER_SSH_HOST" "$RECEIVER_EVENTD_UNIT") || { inconclusive restart "$start_ts" "PID acquisition unavailable"; return; }
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
    scenario_evidence restart inconclusive "emit after receiver restart failed" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"; return; }

  if ! wait_delivery "$event_id2" 60; then
    scenario_evidence restart inconclusive "delivery not confirmed after receiver restart" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi

  if ! receiver_has_event "$event_id2"; then
    scenario_evidence restart inconclusive "event not found in receiver store after receiver restart" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"
    return
  fi

  # Doctor after must be healthy
  if ! wait_doctor_healthy 30; then
    local doctor_after
    doctor_after=$(sender_doctor_json) || true
    scenario_evidence restart inconclusive "doctor not healthy after restarts" \
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
  local start_ts event_id1 event_id2 injection_at restore_at during after qualified
  start_ts=$(timestamp_utc);qualified="EventSubscription/${SUBSCRIPTION_KEY#EventSubscription/}"
  [[ -n "$SUBSCRIPTION_KEY" ]] || { inconclusive subscription "$start_ts" "subscription not configured"; return; }
  plugin_fail || { inconclusive subscription "$start_ts" "plugin fault not acknowledged"; return; }
  injection_at=$(receiver_ssh "date -u +%Y-%m-%dT%H:%M:%S.%NZ") || { inconclusive subscription "$start_ts" "receiver fault clock unavailable"; return; }
  event_id1="qual-sub-fail-$RUN_ID-c$CYCLE_NUM"
  emit_test_event "$event_id1" "10.99.5.1/32" || { inconclusive subscription "$start_ts" "current failure event emit failed"; return; }
  wait_delivery "$event_id1" 30 || { inconclusive subscription "$start_ts" "current failure event delivery unavailable"; return; }
  during=$(wait_subscription "$event_id1" "$injection_at" failed "$qualified") || { inconclusive subscription "$start_ts" "no current post-injection failed run observed"; return; }
  plugin_restore || { inconclusive subscription "$start_ts" "plugin recovery unavailable"; return; }
  restore_at=$(receiver_ssh "date -u +%Y-%m-%dT%H:%M:%S.%NZ") || { inconclusive subscription "$start_ts" "receiver recovery clock unavailable"; return; }
  event_id2="qual-sub-ok-$RUN_ID-c$CYCLE_NUM"
  emit_test_event "$event_id2" "10.99.5.2/32" || { inconclusive subscription "$start_ts" "current recovery event emit failed"; return; }
  wait_delivery "$event_id2" 30 || { inconclusive subscription "$start_ts" "current recovery delivery unavailable"; return; }
  after=$(wait_subscription "$event_id2" "$restore_at" succeeded "$qualified") || { inconclusive subscription "$start_ts" "no current post-restore succeeded run observed"; return; }
  scenario_evidence subscription pass "" "startedAt=$start_ts" "endedAt=$(timestamp_utc)" \
    "injectionAtReceiver=$injection_at" "restoreAtReceiver=$restore_at" "subscriptionRunsDuring=$during" "subscriptionRunsAfter=$after" \
    "testedEventId1=$event_id1" "testedEventId2=$event_id2"
}

scenario_config_fault() {
  ce_log "=== Scenario: expected-peer / config fault ==="
  local start_ts
  start_ts=$(timestamp_utc)

  # Create a temp config with an empty-endpoint peer to provoke doctor findings
  local original_digest
  original_digest=$(sender_ssh "sha256sum $SENDER_CONFIG") || { inconclusive config-fault "$start_ts" "config digest acquisition unavailable"; return; }
  original_digest=${original_digest%% *}
  [[ "$original_digest" =~ ^[0-9a-f]{64}$ ]] || { inconclusive config-fault "$start_ts" "config digest invalid"; return; }

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
\"" || { scenario_evidence config-fault inconclusive "failed to create fault config" "startedAt=$start_ts" "endedAt=$(timestamp_utc)"; return; }

  # Run doctor against the fault config
  local fault_doctor
  fault_doctor=$(read_snapshot config-fault-doctor true sender_routerctl "doctor federation --config $fault_config --state-file $SENDER_STATE_DB -o json" ) || { inconclusive config-fault "$start_ts" "fault doctor/remediation acquisition unavailable"; return; }

  local fault_remediation
  fault_remediation=$(read_snapshot config-fault-remediation true sender_routerctl "doctor federation --config $fault_config --state-file $SENDER_STATE_DB -o json --remediation-plan" ) || { inconclusive config-fault "$start_ts" "fault doctor/remediation acquisition unavailable"; return; }

  python3 -c 'import json,sys;d=json.loads(sys.argv[1]);r=json.loads(sys.argv[2]);assert isinstance(d["checks"],list) and all(isinstance(x,dict) and "code" in x and "name" in x for x in d["checks"]);assert isinstance(r["remediationPlan"]["actions"],list) and all(isinstance(x,dict) and "action" in x for x in r["remediationPlan"]["actions"])' "$fault_doctor" "$fault_remediation" || { inconclusive config-fault "$start_ts" "required fault checks/actions absent"; return; }

  # Verify specific check codes
  local found_codes expected_action
  read -r found_codes expected_action < <(python3 -c "
import json,sys
decoder = json.JSONDecoder()
def first_json(s):
    s = s.strip()
    if not s: return {}
    obj, _ = decoder.raw_decode(s)
    return obj
doc = first_json(sys.argv[1])
rem = first_json(sys.argv[2])

expected_codes = {'expected-delivery-no-endpoint', 'expected-delivery'}
found = [c['code'] for c in doc.get('checks',[]) if c.get('code') in expected_codes and c.get('name','').split(' ',1)[0]=='$EVENT_GROUP/fedqual-nonexistent-node']

expected_actions = {'configure-peer-endpoint', 'investigate-missing-delivery-rows'}
actions = [a['action'] for a in rem.get('remediationPlan',{}).get('actions',[]) if a.get('action') in expected_actions and a.get('targetGroup')=='$EVENT_GROUP' and a.get('targetPeer')=='fedqual-nonexistent-node']

print(' '.join(found) if found else 'none', ' '.join(actions) if actions else 'none')
" "$fault_doctor" "$fault_remediation" 2>/dev/null || echo "none none")

  # Cleanup temp config
  sender_ssh "rm -f $fault_config" || true

  # Verify original config unchanged
  local current_digest
  current_digest=$(sender_ssh "sha256sum $SENDER_CONFIG") || { inconclusive config-fault "$start_ts" "config digest acquisition unavailable"; return; }
  current_digest=${current_digest%% *}
  [[ "$current_digest" =~ ^[0-9a-f]{64}$ ]] || { inconclusive config-fault "$start_ts" "config digest invalid"; return; }
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

security_probe() {
  local kind=$1 marker=$2
  sender_ssh "sudo python3 - '$SENDER_CONFIG' '$EVENT_GROUP' '$SENDER_NODE' '$RECEIVER_ENDPOINT' '$kind' '$marker' <<'PYHTTP'
import json,sys,datetime,time,hmac,hashlib,urllib.request,urllib.error,pathlib,yaml
config,group,node,endpoint,kind,marker=sys.argv[1:]
cfg=yaml.safe_load(open(config))
spec=next(r['spec'] for r in cfg['spec']['resources'] if r['kind']=='EventGroup' and r['metadata']['name']==group)
keyfile=spec.get('auth',{}).get('secretFile')
if not keyfile:raise SystemExit('signed malformed-body fixture unavailable: secretFile not configured')
keypath=pathlib.Path(keyfile)
if not keypath.is_absolute():keypath=pathlib.Path(config).parent/keypath
key=keypath.read_bytes().strip()
if not key:raise SystemExit('empty signing key')
now=int(time.time());stamp=str(now)
dt=lambda t:datetime.datetime.fromtimestamp(t,datetime.timezone.utc).isoformat().replace('+00:00','Z')
event={'id':marker,'group':group,'sourceNode':node,'type':'routerd.client.ipv4.observed','subject':'10.99.6.9/32','dedupeKey':marker,'observedAt':dt(now),'expiresAt':dt(now+600)}
body=b'not-json' if kind=='malformed' else json.dumps({'id':marker,'group':group,'bad':True} if kind=='structure' else event).encode()
signature=hmac.new(key,stamp.encode()+b'\n'+body,hashlib.sha256).hexdigest()
if kind=='auth':signature='0'*64
request=urllib.request.Request(endpoint.rstrip('/')+'/v1/events',data=body,method='POST',headers={'Content-Type':'application/json','X-Routerd-Timestamp':stamp,'X-Routerd-Signature':signature})
try:
 response=urllib.request.urlopen(request,timeout=10);status=response.status;reply=response.read().decode()
except urllib.error.HTTPError as e:status=e.code;reply=e.read().decode()
print(json.dumps({'kind':kind,'marker':marker,'status':status,'body':reply}))
PYHTTP"
}
scenario_security() {
  local start_ts kind marker before after observation status results='[]' event_id rc=0
  start_ts=$(timestamp_utc)
  for kind in malformed structure auth; do
    marker="qual-rejected-$kind-$RUN_ID-c$CYCLE_NUM"
    before=$(read_snapshot security-before false receiver_routerctl "federation event list --group $EVENT_GROUP --state-file $RECEIVER_STATE_DB -o json") || { inconclusive security "$start_ts" "receiver baseline acquisition unavailable"; return; }
    printf '%s' "$before" | current_observation absent "$marker" || { inconclusive security "$start_ts" "negative event marker not fresh"; return; }
    observation=$(read_snapshot security-http false security_probe "$kind" "$marker") || { inconclusive security "$start_ts" "HTTP acquisition unavailable; no rejection proved"; return; }
    status=$(python3 -c 'import json,sys;d=json.loads(sys.argv[1]);assert d["marker"]==sys.argv[2] and type(d["status"]) is int;print(d["status"])' "$observation" "$marker") || { inconclusive security "$start_ts" "HTTP status/identity invalid"; return; }
    results=$(python3 -c 'import json,sys;a=json.loads(sys.argv[1]);a.append(json.loads(sys.argv[2]));print(json.dumps(a))' "$results" "$observation")
    case "$kind:$status" in
      malformed:400|structure:400|auth:401) ;;
      *:2??) scenario_evidence security fail "invalid request accepted" "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "httpObservations=$results"; return ;;
      *) inconclusive security "$start_ts" "expected body/auth rejection not observed; transport/server/other rejection is insufficient" "httpObservations=$results"; return ;;
    esac
    after=$(read_snapshot security-after false receiver_routerctl "federation event list --group $EVENT_GROUP --state-file $RECEIVER_STATE_DB -o json") || { inconclusive security "$start_ts" "receiver post-request acquisition unavailable"; return; }
    rc=0;printf '%s' "$after" | current_observation absent "$marker" || rc=$?
    if [[ "$rc" -eq 1 ]]; then scenario_evidence security fail "negative marker persisted despite rejection" "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "httpObservations=$results" "receiverAfter=$after"; return
    elif [[ "$rc" -ne 0 ]]; then inconclusive security "$start_ts" "nonacceptance observation invalid"; return; fi
  done
  event_id="qual-security-$RUN_ID-c$CYCLE_NUM"
  emit_test_event "$event_id" "10.99.6.1/32" || { inconclusive security "$start_ts" "positive event emit failed"; return; }
  if ! wait_delivery "$event_id" 30 || ! receiver_has_event "$event_id" >/dev/null; then
    inconclusive security "$start_ts" "current valid event acceptance not observed"; return
  fi
  scenario_evidence security pass "" "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "httpObservations=$results" "testedEventId=$event_id"
}

scenario_multi_group() {
  local start_ts rc=0 snapshot partition
  start_ts=$(timestamp_utc)
  [[ -f "$CYCLE_DIR/partition-isolation-snapshot.json" && -f "$CYCLE_DIR/partition.json" ]] || { inconclusive multi-group "$start_ts" "current seeded partition evidence unavailable"; return; }
  partition=$(cat "$CYCLE_DIR/partition.json")
  python3 -c 'import json,sys;d=json.loads(sys.argv[1]);assert d["runId"]==sys.argv[2] and d["cycle"]==int(sys.argv[3]) and d["result"]=="pass" and d["healthyEventId"]=="qual-isolation-"+sys.argv[2]+"-c"+sys.argv[3] and d["healthyReceiverDuring"]' "$partition" "$RUN_ID" "$CYCLE_NUM" || { inconclusive multi-group "$start_ts" "seeded current-cycle healthy/affected scope not established"; return; }
  snapshot=$(cat "$CYCLE_DIR/partition-isolation-snapshot.json")
  printf '%s' "$snapshot" | current_observation multi-group "$EVENT_GROUP" "$RECEIVER_NODE" "$EVENT_GROUP_B" >/dev/null || rc=$?
  if [[ "$rc" -ne 0 ]]; then
    inconclusive multi-group "$start_ts" "affected A/healthy B typed threshold/violation/action correspondence not proven" "faultSnapshot=$snapshot"; return
  fi
  scenario_evidence multi-group pass "" "startedAt=$start_ts" "endedAt=$(timestamp_utc)" "faultSnapshot=$snapshot" "partitionEvidence=$partition"
}

# ============================================================================
# OTel verification (Section 8)
# ============================================================================

verify_otel_metrics() {
  local cycle_start=$1 cycle_end=$2 m val rc classification result='{}'
  for m in routerd_eventd_outbox_delivery_total routerd_eventd_outbox_delivery_lag_seconds_sum routerd_eventd_outbox_repush_total routerd_eventd_outbox_stale_ttl_delivery_total routerd_eventd_receiver_accepted_total routerd_eventd_receiver_duplicate_total routerd_eventd_receiver_reject_total; do
    rc=0;val=$(query_otel_metric "$m" "$cycle_start" "$cycle_end") || rc=$?
    classification=observed;[[ "$rc" -eq 0 ]] || classification=observation_inconclusive
    result=$(python3 -c 'import json,sys;d=json.loads(sys.argv[1]);d[sys.argv[2]]={"classification":sys.argv[3],"value":float(sys.argv[4]) if sys.argv[3]=="observed" else None};print(json.dumps(d))' "$result" "$m" "$classification" "$val")
  done
  rc=0;val=$(check_high_cardinality_labels routerd_eventd_outbox_delivery_total) || rc=$?
  python3 - "$CYCLE_DIR/otel-metrics.json" "$result" "$rc" <<'PYOTEL'
import json,sys
metrics=json.loads(sys.argv[2]);rc=int(sys.argv[3])
labels='pass' if rc==0 else 'fail' if rc==1 else 'inconclusive'
result='fail' if labels=='fail' else 'inconclusive' if labels=='inconclusive' or any(v['classification']!='observed' for v in metrics.values()) else 'pass'
json.dump({'result':result,'scope':'diagnostic only; does not alter product verdict','cardinalityCheck':labels,'metrics':metrics},open(sys.argv[1],'w'),indent=2)
PYOTEL
}

# ============================================================================
# Main orchestration
# ============================================================================
IFS=',' read -ra SCENARIO_LIST <<< "$SCENARIOS"
CYCLE_NUM=0
cycle_start_ts=""
cycle_end_ts=""
CYCLE_DIR="$EVIDENCE_DIR/preflight"
mkdir "$CYCLE_DIR"
setup_rc=0
run_bounded setup preflight_and_provenance || setup_rc=$?
if [[ "$setup_rc" -ne 0 || ! -f "$EVIDENCE_DIR/preflight-context.sh" ]]; then
  python3 - "$EVIDENCE_DIR/run-metadata.json" "$RUN_ID" "$FULL_COMMIT" "$setup_rc" <<'PYSETUP'
import json,sys
json.dump({'runId':sys.argv[2],'qaSourceCommit':sys.argv[3],'summary':{'overall':'inconclusive','qualifiedRelease':False,'stopReason':'preflight/provenance incomplete or budget exhausted; zero scenario/fault calls','setupExit':int(sys.argv[4])},'cycles':[]},open(sys.argv[1],'w'),indent=2)
PYSETUP
  exit 3
fi
# shellcheck source=/dev/null
. "$EVIDENCE_DIR/preflight-context.sh"
STOP=false
STOP_REASON=""
for ((CYCLE_NUM=1; CYCLE_NUM<=CYCLES; CYCLE_NUM++)); do
  CYCLE_DIR="$EVIDENCE_DIR/cycle-$(printf '%03d' "$CYCLE_NUM")"
  mkdir "$CYCLE_DIR"
  cycle_start_ts=$(timestamp_utc)
  for scenario in "${SCENARIO_LIST[@]}"; do
    worker_rc=0
    run_bounded "$scenario" "scenario_${scenario//-/_}" || worker_rc=$?
    efile="$CYCLE_DIR/$scenario.json"
    if [[ "$worker_rc" -ne 0 || ! -f "$efile" ]] || ! validate_evidence_schema "$efile"; then
      if [[ -f "$efile" ]]; then cp "$efile" "$CYCLE_DIR/$scenario.partial.json"; fi
      # Preserve a directly recorded failure if a later observation/cleanup
      # also failed. Otherwise the incomplete worker cannot establish PASS.
      previous_fail=false
      if [[ -f "$efile" ]] && validate_evidence_schema "$efile" && python3 -c 'import json,sys;d=json.load(open(sys.argv[1]));sys.exit(0 if d.get("result")=="fail" else 1)' "$efile"; then previous_fail=true; fi
      if [[ "$previous_fail" != true ]]; then
        inconclusive "$scenario" "$cycle_start_ts" "worker incomplete/deadline/schema failure; no retries" "workerExit=$worker_rc"
      fi
      STOP=true;STOP_REASON="worker incomplete or budget exhausted"
    fi
    if [[ -f "$CYCLE_DIR/partition-attempted" || -f "$CYCLE_DIR/plugin-attempted" ]] || ! python3 -c 'import json,sys;sys.exit(0 if json.load(open(sys.argv[1]))["confirmed"] is True else 1)' "$CYCLE_DIR/cleanup.json"; then
      cp "$efile" "$CYCLE_DIR/$scenario.before-cleanup-assessment.json"
      python3 - "$efile" <<'PYPENDING'
import json,sys
p=sys.argv[1];d=json.load(open(p));d['cleanupClassification']='observation_inconclusive';d['requiresOperatorCleanupReview']=True
if d['result']!='fail':d.update(result='inconclusive',classification='observation_inconclusive',reason='owned fault cleanup not confirmed; no further operations')
json.dump(d,open(p,'w'),indent=2)
PYPENDING
      STOP=true;STOP_REASON="owned cleanup unconfirmed; review retained markers before further operations"
    fi
    result=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["result"])' "$efile")
    if [[ "$result" == fail || "$result" == inconclusive || ( "$result" == skip && "$ALLOW_SKIP" != true ) ]]; then STOP=true;STOP_REASON="$scenario $result; no repeated faults or later cycles"; fi
    [[ "$STOP" == false ]] || break
  done
  cycle_end_ts=$(timestamp_utc)
  # Optional metrics remain diagnostic; a missing API/series/label response is
  # explicitly inconclusive and cannot become numeric zero or product failure.
  if [[ "$STOP" == false ]]; then
    otel_rc=0
    run_bounded otel run_otel_diagnostics || otel_rc=$?
    if [[ "$otel_rc" -ne 0 || ! -f "$CYCLE_DIR/otel-metrics.json" ]]; then
      [[ ! -f "$CYCLE_DIR/otel-metrics.json" ]] || cp "$CYCLE_DIR/otel-metrics.json" "$CYCLE_DIR/otel-metrics.partial.json"
      printf '%s\n' '{"result":"inconclusive","scope":"diagnostic only","reason":"diagnostic budget/acquisition incomplete; no retries"}' >"$CYCLE_DIR/otel-metrics.json"
    fi
  else
    printf '%s\n' '{"result":"inconclusive","scope":"diagnostic only","reason":"collection stopped; no additional requests"}' >"$CYCLE_DIR/otel-metrics.json"
  fi
  if ! secret_scan_evidence "$CYCLE_DIR"; then STOP=true;STOP_REASON="evidence redaction review required"; fi
  [[ "$STOP" == false ]] || break
done

python3 - "$EVIDENCE_DIR" "$RUN_ID" "$FULL_COMMIT" "$COMMIT" "$CYCLES" "$DURATION" "$SCENARIOS" "$ALLOW_SKIP" "$STOP_REASON" <<'PYSUMMARY'
import pathlib,json,sys,datetime
root=pathlib.Path(sys.argv[1]);run,qa,commit=sys.argv[2:5];cycles=int(sys.argv[5]);selected=sys.argv[7].split(',');dev=sys.argv[8]=='true'
records=[];missing=[];invalid=[]
for cycle in range(1,cycles+1):
 for name in selected:
  p=root/('cycle-%03d'%cycle)/(name+'.json')
  try:
   d=json.load(open(p))
   if d['runId']!=run or d['cycle']!=cycle or d['scenario']!=name or d['commit']!=commit or d['result'] not in ('pass','fail','skip','inconclusive'):raise ValueError('identity/result mismatch')
   records.append(d)
  except FileNotFoundError:missing.append({'cycle':cycle,'scenario':name})
  except (ValueError,KeyError,TypeError):invalid.append({'cycle':cycle,'scenario':name})
counts={status:sum(d['result']==status for d in records) for status in ('pass','fail','skip','inconclusive')}
complete=not missing and not invalid and len(records)==cycles*len(selected)
overall='fail' if counts['fail'] else 'inconclusive' if not complete or counts['inconclusive'] or (counts['skip'] and not dev) or sys.argv[9] else 'pass'
provenance=json.load(open(root/'provenance.json'))
qualified=overall=='pass' and not dev and set(selected)=={'healthy','partition','ttl-refresh','restart','subscription','config-fault','security','multi-group'} and provenance.get('qualifiedProvenance') is True
result={'runId':run,'qaSourceCommit':qa,'commitLabel':commit,'completedAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'parameters':{'cycles':cycles,'durationPerScenario':int(sys.argv[6]),'scenarios':selected,'allowSkip':dev},'summary':{'overall':overall,'qualifiedRelease':qualified,'scope':'development selected scenarios' if dev else 'all eight declared scenarios','counts':counts,'exactCurrentResultsComplete':complete,'missing':missing,'invalid':invalid,'stopReason':sys.argv[9],'retryCount':0},'cycles':records,'diagnostics':[json.load(open(p)) for p in root.glob('cycle-*/otel-metrics.json')]}
json.dump(result,open(root/'run-metadata.json','w'),indent=2)
raise SystemExit(1 if overall=='fail' else 3 if overall!='pass' else 0)
PYSUMMARY
