#!/usr/bin/env bash
#
# cloudedge-labctl.sh - single-command harness to run CloudEdge SAM failover labs.
#
# An agent (or operator) drives the full lab lifecycle without reading runbooks:
#   up -> deploy -> smoke -> failover -> evidence collect -> down
#
# Non-cloud logic (run-id/tag convention, TTL/teardown guard, evidence assembly,
# connectivity matrix orchestration, fault primitives) is fully implemented here.
# Real per-provider provisioning either WRAPS the existing lab package
# (examples/cloudedge-mobility-demo/*) or is clearly marked with TODO(lab-operator)
# stubs for Terraform/CLI wiring. Running --help / dry paths needs NO credentials.
#
# Human gates (NOT automated here): budget approval, credential/permission grant,
# final merge approval, production rollout. See docs/how-to/cloudedge-autonomous-lab.md.
#
set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)

# Where lab state (run manifests) lives. Overridable for tests.
CE_STATE_DIR="${CE_STATE_DIR:-${TMPDIR:-/tmp}/cloudedge-labctl}"
# DEMO_DIR is referenced by provider wrappers (run-demo.sh reuse) once wired.
# shellcheck disable=SC2034
DEMO_DIR="${CE_DEMO_DIR:-$REPO_ROOT/examples/cloudedge-mobility-demo}"
SCHEMA_FILE="$SCRIPT_DIR/cloudedge-evidence-schema.json"
MATRIX_SCRIPT="$SCRIPT_DIR/cloudedge-connectivity-matrix.sh"

# Identity used for resource tags / ownership. No secrets.
CE_OWNER="${CE_OWNER:-${USER:-cloudedge-agent}}"
CE_PURPOSE="${CE_PURPOSE:-cloudedge-sam-failover-lab}"

# DRY_RUN=1 (default for safety on cloud-touching ops unless --commit/credentials).
# When set, provider mutations only print what they WOULD do.
DRY_RUN="${CE_DRY_RUN:-1}"

log()  { printf '%s %s\n' "[$(date -u +%H:%M:%SZ)]" "$*" >&2; }
die()  { printf '%s: %s\n' "$SELF" "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

# -----------------------------------------------------------------------------
# run-id + tag convention (DELIVERABLE 2)
# -----------------------------------------------------------------------------
# run-id: <UTCdate>T<time>-cloudedge-<scenario>  e.g. 20260601T031500Z-cloudedge-d3
cloudedge_run_id() {
  local scenario=${1:-lab}
  scenario=$(printf '%s' "$scenario" | tr -cs 'A-Za-z0-9._-' '-' | sed 's/^-//; s/-$//')
  printf '%sT%sZ-cloudedge-%s' \
    "$(date -u +%Y%m%d)" "$(date -u +%H%M%S)" "$scenario"
}

# ttl_expires_at as an absolute UTC RFC3339 timestamp, from a duration like 4h/90m.
cloudedge_ttl_expires_at() {
  local ttl=${1:-4h} secs
  secs=$(duration_to_secs "$ttl")
  date -u -d "@$(( $(date -u +%s) + secs ))" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null \
    || date -u -r "$(( $(date -u +%s) + secs ))" +%Y-%m-%dT%H:%M:%SZ
}

duration_to_secs() {
  local d=$1 n unit
  [[ "$d" =~ ^([0-9]+)([smhd]?)$ ]] || die "bad duration: $d (want e.g. 90m, 4h, 2d)"
  n=${BASH_REMATCH[1]}; unit=${BASH_REMATCH[2]:-s}
  case "$unit" in
    s) echo "$n" ;;
    m) echo "$((n * 60))" ;;
    h) echo "$((n * 3600))" ;;
    d) echo "$((n * 86400))" ;;
  esac
}

# Emit the mandatory tag set for a cloud resource. Args: run_id ttl_expires provider
# Prints newline-separated key=value pairs; provider wrappers translate to native tags.
cloudedge_tags() {
  local run_id=$1 ttl_expires=$2 provider=$3
  cat <<EOF
routerd.cloudedge.run_id=${run_id}
routerd.cloudedge.owner=${CE_OWNER}
routerd.cloudedge.ttl_expires_at=${ttl_expires}
routerd.cloudedge.provider=${provider}
routerd.cloudedge.purpose=${CE_PURPOSE}
EOF
}

run_manifest_path() { printf '%s/%s.manifest' "$CE_STATE_DIR" "$1"; }

write_manifest() {
  local run_id=$1 ttl=$2 ttl_expires=$3 profile=$4 providers=$5
  mkdir -p "$CE_STATE_DIR"
  cat > "$(run_manifest_path "$run_id")" <<EOF
run_id=${run_id}
owner=${CE_OWNER}
purpose=${CE_PURPOSE}
profile=${profile}
providers=${providers}
ttl=${ttl}
ttl_expires_at=${ttl_expires}
created_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
EOF
}

manifest_get() { sed -n "s/^$2=//p" "$(run_manifest_path "$1")" 2>/dev/null | head -n1; }

# -----------------------------------------------------------------------------
# teardown trap (DELIVERABLE 2): tear down the active run on unexpected exit.
# Only armed by `up` (which may keep the lab alive on success up to its TTL).
# -----------------------------------------------------------------------------
ACTIVE_RUN_ID=""
TEARDOWN_ON_EXIT="0"
teardown_trap() {
  local rc=$?
  if [[ "$TEARDOWN_ON_EXIT" == "1" && -n "$ACTIVE_RUN_ID" ]]; then
    log "EXIT trap: tearing down $ACTIVE_RUN_ID (rc=$rc)"
    TEARDOWN_ON_EXIT="0"   # avoid recursion
    cmd_down --run-id "$ACTIVE_RUN_ID" --force || log "teardown trap: down failed"
  fi
}
trap teardown_trap EXIT

valid_providers="aws oci azure onprem"
validate_providers() {
  local p
  IFS=',' read -ra _ps <<<"$1"
  for p in "${_ps[@]}"; do
    [[ " $valid_providers " == *" $p "* ]] || die "unknown provider: $p (want: $valid_providers)"
  done
}

# =============================================================================
# up
# =============================================================================
up_usage() {
  cat <<EOF
$SELF up - allocate/start a CloudEdge lab and stamp run-id + cost tags

USAGE:
  $SELF up --profile minimal|provider|full [--provider aws,oci,azure,onprem]
           [--ttl <dur>] [--scenario <name>] [--keep]

  --profile   minimal  : onprem + 1 cloud, smoke only (cheapest).
              provider : single named provider A/B routers + client (provider parity).
              full     : all four sites, 4-site /24 demo.
              (soak is a 'full' run held open for its full TTL; see docs.)
  --provider  Comma list to bring up (default depends on profile).
  --ttl       Lab lifetime, e.g. 90m, 4h, 2d (default 4h). Stamped as ttl_expires_at.
  --scenario  Scenario label used in the run-id (default derived from profile).
  --keep      Do NOT arm the in-progress EXIT teardown trap; leave a partially
              brought-up lab in place for inspection (default: a failed/interrupted
              'up' auto-tears-down; a clean 'up' always persists until 'down'/TTL).

Prints the run-id on stdout. Per-provider start either wraps the existing lab env
or is a clearly-marked TODO(lab-operator) stub. No credentials are needed for --help.
EOF
}

provider_start() {
  # TODO(lab-operator): wire real allocation (Terraform/OpenTofu or provider CLI).
  # First pass: wrap the pre-provisioned lab from examples/cloudedge-mobility-demo,
  # tagging existing resources with the run-id tag set. Stubbed when DRY_RUN=1.
  local provider=$1 run_id=$2 ttl_expires=$3
  local tags; tags=$(cloudedge_tags "$run_id" "$ttl_expires" "$provider")
  if [[ "$DRY_RUN" == "1" ]]; then
    log "[dry] would allocate/start '$provider' and apply tags:"
    printf '        %s\n' "$tags" >&2
    return 0
  fi
  case "$provider" in
    aws)
      have aws || die "aws CLI not found (provider=aws)"
      # TODO(lab-operator): aws ec2 create-tags / run-instances against lab AMIs.
      log "aws: TODO(lab-operator) real allocation not wired; tag-only against env refs"
      ;;
    azure)
      have az || die "az CLI not found (provider=azure)"
      log "azure: TODO(lab-operator) real allocation not wired; tag-only against env refs"
      ;;
    oci)
      have oci || die "oci CLI not found (provider=oci)"
      log "oci: TODO(lab-operator) real allocation not wired; tag-only against env refs"
      ;;
    onprem)
      log "onprem: no cloud allocation; uses pre-provisioned lab node(s)"
      ;;
  esac
}

cmd_up() {
  local profile="" providers="" ttl="4h" scenario="" keep="0"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --profile) profile="${2:-}"; shift 2 ;;
      --provider) providers="${2:-}"; shift 2 ;;
      --ttl) ttl="${2:-}"; shift 2 ;;
      --scenario) scenario="${2:-}"; shift 2 ;;
      --keep) keep="1"; shift ;;
      -h|--help) up_usage; exit 0 ;;
      *) die "up: unknown argument: $1" ;;
    esac
  done
  [[ -n "$profile" ]] || { up_usage >&2; die "up: --profile is required"; }
  case "$profile" in
    minimal)  : "${providers:=onprem,aws}" ;;
    provider) : "${providers:=aws}" ;;
    full)     : "${providers:=aws,oci,azure,onprem}" ;;
    *) die "up: unknown profile: $profile (want minimal|provider|full)" ;;
  esac
  validate_providers "$providers"
  [[ -n "$scenario" ]] || scenario="$profile"

  # Validate --ttl up-front and hard-fail before any provider_start. duration_to_secs
  # die()s on a bad duration, but when called only inside the nested command
  # substitution of cloudedge_ttl_expires_at its exit is swallowed and ttl_expires
  # silently collapses to ~now (an already-expired stamp), letting 'up' continue at
  # exit 0 with a broken cost guard. Calling it here (not in a substitution that
  # masks the status) propagates the die. See cmd_up TTL guard test.
  local ttl_secs
  ttl_secs=$(duration_to_secs "$ttl")
  [[ "$ttl_secs" =~ ^[0-9]+$ && "$ttl_secs" -gt 0 ]] \
    || die "up: invalid --ttl '$ttl' (want a positive duration like 90m, 4h, 2d)"

  local run_id ttl_expires
  run_id=$(cloudedge_run_id "$scenario")
  ttl_expires=$(cloudedge_ttl_expires_at "$ttl")

  log "up: run_id=$run_id profile=$profile providers=$providers ttl=$ttl (expires $ttl_expires)"
  write_manifest "$run_id" "$ttl" "$ttl_expires" "$profile" "$providers"

  ACTIVE_RUN_ID="$run_id"
  # Arm the EXIT teardown trap for the in-progress bring-up window: if a
  # provider_start fails or the chain is interrupted (set -e / signal) before
  # 'up' completes, the partially-allocated run is torn down so a half-built lab
  # cannot leak past its cost guard. On clean success we disarm below so the lab
  # persists until an explicit 'down' or TTL expiry. --keep opts out entirely
  # (leaves the partial state in place for inspection instead of auto-cleaning).
  [[ "$keep" == "1" ]] || TEARDOWN_ON_EXIT="1"

  local p
  IFS=',' read -ra _ps <<<"$providers"
  for p in "${_ps[@]}"; do
    provider_start "$p" "$run_id" "$ttl_expires"
  done

  # Clean success: disarm so we do not auto-teardown a freshly-started lab.
  TEARDOWN_ON_EXIT="0"
  printf '%s\n' "$run_id"
}

# =============================================================================
# deploy
# =============================================================================
deploy_usage() {
  cat <<EOF
$SELF deploy - build static routerd and push to lab nodes

USAGE:
  $SELF deploy [--build <path>] [--commit HEAD|<sha>] [--run-id <id>]

  --build PATH   Use an already-built dist tarball/dir at PATH (skip build).
  --commit REF   Build a static dist at REF via 'make dist' (CGO_ENABLED=0).
                 Mutually informative with --build; --build wins if both given.
  --run-id ID    Target the nodes of this run (default: latest manifest).

Node push wraps examples/cloudedge-mobility-demo deployment conventions, or is a
TODO(lab-operator) stub. Build is local and needs no credentials; '--help' is dry.
EOF
}

cmd_deploy() {
  local build_path="" commit="" run_id=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --build) build_path="${2:-}"; shift 2 ;;
      --commit) commit="${2:-}"; shift 2 ;;
      --run-id) run_id="${2:-}"; shift 2 ;;
      -h|--help) deploy_usage; exit 0 ;;
      *) die "deploy: unknown argument: $1" ;;
    esac
  done

  if [[ -n "$build_path" ]]; then
    [[ -e "$build_path" ]] || die "deploy: --build path not found: $build_path"
    log "deploy: using prebuilt artifact at $build_path"
  elif [[ -n "$commit" ]]; then
    log "deploy: building static dist at $commit (make dist, CGO_ENABLED=0)"
    if [[ "$DRY_RUN" == "1" ]]; then
      log "[dry] would run: make -C $REPO_ROOT dist  (commit=$commit)"
    else
      ( cd "$REPO_ROOT" && CGO_ENABLED=0 make dist ) || die "deploy: make dist failed"
    fi
  else
    die "deploy: one of --build <path> or --commit <ref> is required"
  fi

  # TODO(lab-operator): push artifact to each node and restart routerd /
  # routerd-eventd. The demo package already encodes this in run-demo.sh
  # (install_secret_and_config); reuse it once node SSH targets are in env.
  if [[ "$DRY_RUN" == "1" ]]; then
    log "[dry] would push dist to lab nodes and restart routerd + routerd-eventd@cloudedge"
  else
    log "deploy: TODO(lab-operator) node push not wired in first pass; reuse run-demo.sh install path"
  fi
}

# =============================================================================
# smoke
# =============================================================================
smoke_usage() {
  cat <<EOF
$SELF smoke - run the connectivity matrix smoke

USAGE:
  $SELF smoke [--matrix d3] [--out <file>] [--sites "site=ip,..."]

  --matrix d3   Run the 12-directed four-site matrix (default).
  --out FILE    Write matrix result JSON to FILE.
  --sites STR   Override sites (passed through to the matrix script).

Delegates to cloudedge-connectivity-matrix.sh. With MATRIX_RUNNER set it is
unit-runnable offline; with a real lab it runs ping+ssh. '--help' is dry.
EOF
}

cmd_smoke() {
  local matrix="d3" out="" sites=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --matrix) matrix="${2:-}"; shift 2 ;;
      --out) out="${2:-}"; shift 2 ;;
      --sites) sites="${2:-}"; shift 2 ;;
      -h|--help) smoke_usage; exit 0 ;;
      *) die "smoke: unknown argument: $1" ;;
    esac
  done
  [[ "$matrix" == "d3" ]] || die "smoke: unsupported matrix: $matrix (only 'd3' in first pass)"
  [[ -x "$MATRIX_SCRIPT" ]] || die "smoke: matrix script not executable: $MATRIX_SCRIPT"

  local args=()
  [[ -n "$out" ]] && args+=(--out "$out")
  [[ -n "$sites" ]] && args+=(--sites "$sites")
  log "smoke: running $matrix connectivity matrix"
  "$MATRIX_SCRIPT" "${args[@]}"
}

# =============================================================================
# failover (fault injection primitives)
# =============================================================================
failover_usage() {
  cat <<EOF
$SELF failover - inject a fault to drive a SAM failover

USAGE:
  $SELF failover --provider <p> --fault <fault> [--node <name>] [--run-id <id>]

  --fault stop-active     Stop the active router VM/instance (provider-side).
          drain           Apply MobilityPool maintenance drain to the active router.
          heartbeat-stop  Stop routerd-eventd so federation heartbeats cease.
          executor-fail   Make the provider action executor fail (perm/identity).
          stale-replay    Replay a stale-epoch provider action (must be fenced).
  --provider P    Target provider (aws|oci|azure|onprem).
  --node NAME     Specific node (default: the provider's active router).

Each fault is a primitive an agent injects, then re-runs 'smoke' to verify recovery.
Real injection wraps demo/provider tooling or is a TODO(lab-operator) stub; the
fault is always logged. '--help' is dry and needs no credentials.
EOF
}

cmd_failover() {
  local provider="" fault="" node="" run_id=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --provider) provider="${2:-}"; shift 2 ;;
      --fault) fault="${2:-}"; shift 2 ;;
      --node) node="${2:-}"; shift 2 ;;
      --run-id) run_id="${2:-}"; shift 2 ;;
      -h|--help) failover_usage; exit 0 ;;
      *) die "failover: unknown argument: $1" ;;
    esac
  done
  [[ -n "$provider" ]] || { failover_usage >&2; die "failover: --provider is required"; }
  [[ -n "$fault" ]] || { failover_usage >&2; die "failover: --fault is required"; }
  validate_providers "$provider"
  case "$fault" in
    stop-active|drain|heartbeat-stop|executor-fail|stale-replay) ;;
    *) die "failover: unknown fault: $fault" ;;
  esac

  log "failover: provider=$provider fault=$fault node=${node:-<active>}"
  if [[ "$DRY_RUN" == "1" ]]; then
    log "[dry] would inject '$fault' on provider '$provider'"
    return 0
  fi

  # TODO(lab-operator): wire real injection. Mapping for the first pass:
  case "$fault" in
    stop-active)
      # aws ec2 stop-instances / az vm deallocate / oci compute instance action STOP
      log "stop-active: TODO(lab-operator) provider stop not wired; see reset-lab.sh for CLI shape"
      ;;
    drain)
      # Re-apply <provider> config with maintenance.drain=true (run-demo.sh *-drain.yaml).
      log "drain: TODO(lab-operator) reuse run-demo.sh *-drain.yaml apply path"
      ;;
    heartbeat-stop)
      # ssh node: sudo systemctl stop routerd-eventd@cloudedge.service
      log "heartbeat-stop: TODO(lab-operator) ssh stop routerd-eventd not wired"
      ;;
    executor-fail)
      # Revoke/scope-down the instance identity so provider mutation is denied.
      log "executor-fail: TODO(lab-operator) identity scope-down not wired"
      ;;
    stale-replay)
      # Insert a stale-epoch action_executions row (see probe_stale_gate_on_aws_b).
      log "stale-replay: TODO(lab-operator) reuse run-demo.sh probe_stale_gate_on_aws_b"
      ;;
  esac
}

# =============================================================================
# evidence collect (assemble bundle + emit schema-valid result JSON)
# =============================================================================
evidence_usage() {
  cat <<EOF
$SELF evidence - assemble an evidence bundle and emit result JSON

USAGE:
  $SELF evidence collect --out <dir> [--run-id <id>] [--scenario <name>]
                         [--commit <ref>] [--matrix-json <file>]
                         [--provider-state-json <file>] [--timing-json <file>]
                         [--protocol-json <file>] [--l2-loop-json <file>]
                         [--result pass|fail]

  --out DIR        Evidence bundle output directory (required).
  --run-id ID      Run id (default: latest manifest). Sets runId in the JSON.
  --scenario NAME  Scenario label (default: from run-id).
  --commit REF     Commit under test (default: HEAD sha).
  --matrix-json F  Connectivity-matrix JSON to fold into providers/assertions.
  --provider-state-json F
                  Optional provider inventory check states. Accepted shapes:
                  {"aws":"pass"} or {"aws":{"providerState":"pass"}}.
  --timing-json F
                  Auto-failover timing JSON from cloudedge-failover-timing.sh.
  --protocol-json F
                  Protocol transparency JSON from cloudedge-protocol-probe.sh.
  --l2-loop-json F
                  L2 loop/STP stability JSON from cloudedge-l2-loop-probe.sh.
  --result R       Force overall result; default derived from inputs.

Writes <out>/result.json validating against cloudedge-evidence-schema.json plus a
summary. Pure assembly: no credentials needed. '--help' is dry.
EOF
}

derive_check() {
  # Map a matrix flow result presence to a check state. Echoes pass|fail|na.
  local matrix_json=$1 provider=$2
  [[ -z "$matrix_json" || ! -f "$matrix_json" ]] && { echo "na"; return; }
  have python3 || { echo "na"; return; }
  python3 - "$matrix_json" "$provider" <<'PY'
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except Exception:
    print("na")
    raise SystemExit(0)
provider = sys.argv[2]
flows = [
    f for f in data.get("flows", [])
    if f.get("src") == provider or f.get("dst") == provider
]
if not flows:
    print("na")
elif all(f.get("result") == "pass" for f in flows):
    print("pass")
else:
    print("fail")
PY
}

matrix_assertion() {
  local matrix_json=$1 field=$2 expected_total=${3:-0}
  [[ -z "$matrix_json" || ! -f "$matrix_json" ]] && { echo "na"; return; }
  have python3 || { echo "na"; return; }
  python3 - "$matrix_json" "$field" "$expected_total" <<'PY'
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except Exception:
    print("na")
    raise SystemExit(0)
field = sys.argv[2]
expected_total = int(sys.argv[3])
flows = data.get("flows", [])
summary = data.get("summary", {})
if not flows:
    print("na")
    raise SystemExit(0)
if field == "directed":
    ok = summary.get("result") == "pass"
    if expected_total:
        ok = ok and summary.get("total") == expected_total
else:
    ok = all(f.get(field) == "pass" for f in flows)
print("pass" if ok else "fail")
PY
}

matrix_summary_result() {
  local matrix_json=$1
  [[ -z "$matrix_json" || ! -f "$matrix_json" ]] && { echo "na"; return; }
  have python3 || { echo "na"; return; }
  python3 - "$matrix_json" <<'PY'
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except Exception:
    print("na")
    raise SystemExit(0)
result = data.get("summary", {}).get("result")
print(result if result in ("pass", "fail") else "na")
PY
}

provider_state_check() {
  local provider_state_json=$1 provider=$2
  [[ -z "$provider_state_json" || ! -f "$provider_state_json" ]] && { echo "na"; return; }
  have python3 || { echo "na"; return; }
  python3 - "$provider_state_json" "$provider" <<'PY'
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except Exception:
    print("na")
    raise SystemExit(0)
provider = sys.argv[2]
valid = {"pass", "fail", "skip", "na"}
value = data.get(provider, "na") if isinstance(data, dict) else "na"
if isinstance(value, dict):
    value = value.get("providerState", value.get("result", "na"))
print(value if value in valid else "na")
PY
}

json_object_or_default() {
  local json_file=$1 default_json=$2
  [[ -n "$json_file" && -f "$json_file" ]] || { printf '%s\n' "$default_json"; return; }
  have python3 || { printf '%s\n' "$default_json"; return; }
  python3 - "$json_file" "$default_json" <<'PY'
import json, sys
try:
    data = json.load(open(sys.argv[1]))
    if not isinstance(data, dict):
        raise ValueError("not an object")
except Exception:
    data = json.loads(sys.argv[2])
print(json.dumps(data, sort_keys=True))
PY
}

timing_recovery_check() {
  local timing_json=$1
  [[ -n "$timing_json" && -f "$timing_json" ]] || { echo "na"; return; }
  have python3 || { echo "na"; return; }
  python3 - "$timing_json" <<'PY'
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except Exception:
    print("na")
    raise SystemExit(0)
events = data.get("events", [])
if not events:
    print("na")
elif data.get("status") == "pass" and all(e.get("recoveryUnderThreshold") == "pass" for e in events):
    print("pass")
else:
    print("fail")
PY
}

protocol_check() {
  local protocol_json=$1 check=$2
  [[ -n "$protocol_json" && -f "$protocol_json" ]] || { echo "na"; return; }
  have python3 || { echo "na"; return; }
  python3 - "$protocol_json" "$check" <<'PY'
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except Exception:
    print("na")
    raise SystemExit(0)
check = sys.argv[2]
summary = data.get("summary", {})
checks = summary.get("checks", {})
if check == "protocol_transparency":
    result = data.get("status", "na")
elif check == "ftp_active_passive":
    vals = [checks.get("ftpActive"), checks.get("ftpPassive")]
    result = "pass" if all(v == "pass" for v in vals) else "fail"
elif check == "nfs_rpc":
    vals = [checks.get("nfs"), checks.get("rpc")]
    result = "pass" if all(v == "pass" for v in vals) else "fail"
elif check == "bulk_transfer_pmtu":
    vals = [checks.get("bulkTransfer"), checks.get("pmtu")]
    result = "pass" if all(v == "pass" for v in vals) else "fail"
elif check == "protocol_source_ip_preserved":
    result = checks.get("sourceIpPreserved", "na")
elif check == "protocol_no_nat":
    result = checks.get("noNat", "na")
else:
    result = "na"
print(result if result in ("pass", "fail", "skip", "na") else "na")
PY
}

l2_loop_check() {
  local l2_loop_json=$1 check=$2
  [[ -n "$l2_loop_json" && -f "$l2_loop_json" ]] || { echo "na"; return; }
  have python3 || { echo "na"; return; }
  python3 - "$l2_loop_json" "$check" <<'PY'
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except Exception:
    print("na")
    raise SystemExit(0)
check = sys.argv[2]
summary = data.get("summary", {})
if check == "l2_loop_free":
    result = data.get("status", "na")
elif check == "broadcast_storm_absent":
    result = summary.get("broadcastStormAbsent", "na")
elif check == "stp_rstp_stable":
    result = summary.get("stpRstpStable", "na")
elif check == "mac_flap_absent":
    result = summary.get("macFlapAbsent", "na")
elif check == "failover_ping_stable":
    result = summary.get("failoverPingStable", "na")
elif check == "l2_suppression_mechanism_recorded":
    result = summary.get("suppressionMechanismRecorded", "na")
else:
    result = "na"
print(result if result in ("pass", "fail", "skip", "na") else "na")
PY
}

cmd_evidence() {
  local sub="${1:-}"; [[ "$sub" == "-h" || "$sub" == "--help" ]] && { evidence_usage; exit 0; }
  [[ "$sub" == "collect" ]] || { evidence_usage >&2; die "evidence: expected subcommand 'collect'"; }
  shift
  local out="" run_id="" scenario="" commit="" matrix_json="" provider_state_json="" timing_json="" protocol_json="" l2_loop_json="" forced_result=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --out) out="${2:-}"; shift 2 ;;
      --run-id) run_id="${2:-}"; shift 2 ;;
      --scenario) scenario="${2:-}"; shift 2 ;;
      --commit) commit="${2:-}"; shift 2 ;;
      --matrix-json) matrix_json="${2:-}"; shift 2 ;;
      --provider-state-json) provider_state_json="${2:-}"; shift 2 ;;
      --timing-json) timing_json="${2:-}"; shift 2 ;;
      --protocol-json) protocol_json="${2:-}"; shift 2 ;;
      --l2-loop-json) l2_loop_json="${2:-}"; shift 2 ;;
      --result) forced_result="${2:-}"; shift 2 ;;
      -h|--help) evidence_usage; exit 0 ;;
      *) die "evidence: unknown argument: $1" ;;
    esac
  done
  [[ -n "$out" ]] || { evidence_usage >&2; die "evidence collect: --out is required"; }

  [[ -n "$run_id" ]] || run_id=$(latest_run_id || true)
  [[ -n "$run_id" ]] || run_id=$(cloudedge_run_id "${scenario:-adhoc}")
  [[ -n "$scenario" ]] || scenario=$(printf '%s' "$run_id" | sed -n 's/.*-cloudedge-//p')
  [[ -n "$scenario" ]] || scenario="adhoc"
  if [[ -z "$commit" ]]; then
    commit=$(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null || echo "unknown")
  fi
  local ttl_hours teardown_state
  ttl_hours=$(awk -v s="$(duration_to_secs "$(manifest_get "$run_id" ttl 2>/dev/null || echo 4h)")" \
    'BEGIN{printf "%g", s/3600}')
  teardown_state="pending"

  mkdir -p "$out"

  # Per-provider checks from the matrix (dataplane); providerState is na in the
  # first pass unless a real provider inventory is folded in by the lab operator.
  local aws_dp oci_dp az_dp onprem_dp
  aws_dp=$(derive_check "$matrix_json" aws)
  oci_dp=$(derive_check "$matrix_json" oci)
  az_dp=$(derive_check "$matrix_json" azure)
  onprem_dp=$(derive_check "$matrix_json" onprem)

  local aws_ps oci_ps az_ps onprem_ps
  aws_ps=$(provider_state_check "$provider_state_json" aws)
  oci_ps=$(provider_state_check "$provider_state_json" oci)
  az_ps=$(provider_state_check "$provider_state_json" azure)
  onprem_ps=$(provider_state_check "$provider_state_json" onprem)

  # Directed matrix / Source-IP-preserved / default-gw-unchanged / no-NAT come
  # straight from the matrix and are intentionally independent of cloud APIs.
  local directed_matrix src_pres gw_ok no_nat matrix_overall
  directed_matrix=$(matrix_assertion "$matrix_json" directed 12)
  src_pres=$(matrix_assertion "$matrix_json" sourceIpPreserved)
  gw_ok=$(matrix_assertion "$matrix_json" defaultGwUnchanged)
  no_nat=$(matrix_assertion "$matrix_json" noNat)
  matrix_overall=$(matrix_summary_result "$matrix_json")

  local timings_obj protocols_obj l2_loop_obj
  timings_obj=$(json_object_or_default "$timing_json" '{"status":"na","thresholdSeconds":60,"events":[]}')
  protocols_obj=$(json_object_or_default "$protocol_json" '{"status":"na","pairs":[],"summary":{"total":0,"passed":0,"failed":0,"checks":{}}}')
  l2_loop_obj=$(json_object_or_default "$l2_loop_json" '{"status":"na","mechanism":"","phases":[],"summary":{}}')

  local failover_recovery protocol_transparency ftp_active_passive nfs_rpc bulk_transfer_pmtu protocol_src_pres protocol_no_nat
  failover_recovery=$(timing_recovery_check "$timing_json")
  protocol_transparency=$(protocol_check "$protocol_json" protocol_transparency)
  ftp_active_passive=$(protocol_check "$protocol_json" ftp_active_passive)
  nfs_rpc=$(protocol_check "$protocol_json" nfs_rpc)
  bulk_transfer_pmtu=$(protocol_check "$protocol_json" bulk_transfer_pmtu)
  protocol_src_pres=$(protocol_check "$protocol_json" protocol_source_ip_preserved)
  protocol_no_nat=$(protocol_check "$protocol_json" protocol_no_nat)

  local l2_loop_free broadcast_storm_absent stp_rstp_stable mac_flap_absent failover_ping_stable l2_mechanism_recorded
  l2_loop_free=$(l2_loop_check "$l2_loop_json" l2_loop_free)
  broadcast_storm_absent=$(l2_loop_check "$l2_loop_json" broadcast_storm_absent)
  stp_rstp_stable=$(l2_loop_check "$l2_loop_json" stp_rstp_stable)
  mac_flap_absent=$(l2_loop_check "$l2_loop_json" mac_flap_absent)
  failover_ping_stable=$(l2_loop_check "$l2_loop_json" failover_ping_stable)
  l2_mechanism_recorded=$(l2_loop_check "$l2_loop_json" l2_suppression_mechanism_recorded)

  # Fencing/seize assertions: na until a failover scenario records them. Folding
  # real journal/epoch evidence is the lab operator's wire-up (TODO).
  local a_epoch="na" a_reassign="na" a_residue="na" a_stale="na"

  local result="$forced_result"
  if [[ -z "$result" ]]; then
    if [[ "$matrix_overall" == "fail" || "$directed_matrix" == "fail" || "$src_pres" == "fail" || "$gw_ok" == "fail" || "$no_nat" == "fail" || "$failover_recovery" == "fail" || "$protocol_transparency" == "fail" || "$ftp_active_passive" == "fail" || "$nfs_rpc" == "fail" || "$bulk_transfer_pmtu" == "fail" || "$protocol_src_pres" == "fail" || "$protocol_no_nat" == "fail" || "$l2_loop_free" == "fail" || "$broadcast_storm_absent" == "fail" || "$stp_rstp_stable" == "fail" || "$mac_flap_absent" == "fail" || "$failover_ping_stable" == "fail" || "$l2_mechanism_recorded" == "fail" ]]; then
      result="fail"
    elif [[ "$matrix_overall" == "pass" && "$directed_matrix" == "pass" ]]; then
      result="pass"
    else
      result="fail"   # no positive evidence => not a pass
    fi
  fi

  local result_file="$out/result.json"
  cat > "$result_file" <<EOF
{
  "runId": "$run_id",
  "commit": "$commit",
  "scenario": "$scenario",
  "result": "$result",
  "providers": {
    "aws":    { "dataplane": "$aws_dp",    "providerState": "$aws_ps" },
    "oci":    { "dataplane": "$oci_dp",    "providerState": "$oci_ps" },
    "azure":  { "dataplane": "$az_dp",     "providerState": "$az_ps" },
    "onprem": { "dataplane": "$onprem_dp", "providerState": "$onprem_ps" }
  },
  "assertions": [
    { "name": "directed_ping_ssh_matrix", "result": "$directed_matrix" },
    { "name": "ownership_epoch_bumped", "result": "$a_epoch" },
    { "name": "allow_reassignment_maintained_until_success", "result": "$a_reassign" },
    { "name": "source_ip_preserved", "result": "$src_pres" },
    { "name": "default_gateway_unchanged", "result": "$gw_ok" },
    { "name": "no_nat", "result": "$no_nat" },
    { "name": "failover_recovery_under_60s", "result": "$failover_recovery" },
    { "name": "protocol_transparency", "result": "$protocol_transparency" },
    { "name": "ftp_active_passive", "result": "$ftp_active_passive" },
    { "name": "nfs_rpc", "result": "$nfs_rpc" },
    { "name": "bulk_transfer_pmtu", "result": "$bulk_transfer_pmtu" },
    { "name": "protocol_source_ip_preserved", "result": "$protocol_src_pres" },
    { "name": "protocol_no_nat", "result": "$protocol_no_nat" },
    { "name": "l2_loop_free", "result": "$l2_loop_free" },
    { "name": "broadcast_storm_absent", "result": "$broadcast_storm_absent" },
    { "name": "stp_rstp_stable", "result": "$stp_rstp_stable" },
    { "name": "mac_flap_absent", "result": "$mac_flap_absent" },
    { "name": "failover_ping_stable", "result": "$failover_ping_stable" },
    { "name": "l2_suppression_mechanism_recorded", "result": "$l2_mechanism_recorded" },
    { "name": "old_holder_residue_absent", "result": "$a_residue" },
    { "name": "stale_action_fenced", "result": "$a_stale" }
  ],
  "timings": $timings_obj,
  "protocols": $protocols_obj,
  "l2Loop": $l2_loop_obj,
  "costGuard": {
    "ttlHours": $ttl_hours,
    "teardown": "$teardown_state"
  }
}
EOF

  if [[ -n "$matrix_json" && -f "$matrix_json" ]]; then
    cp "$matrix_json" "$out/connectivity-matrix.json" 2>/dev/null || true
  fi
  if [[ -n "$timing_json" && -f "$timing_json" ]]; then
    cp "$timing_json" "$out/failover-timing.json" 2>/dev/null || true
  fi
  if [[ -n "$protocol_json" && -f "$protocol_json" ]]; then
    cp "$protocol_json" "$out/protocol-probe.json" 2>/dev/null || true
  fi
  if [[ -n "$l2_loop_json" && -f "$l2_loop_json" ]]; then
    cp "$l2_loop_json" "$out/l2-loop-probe.json" 2>/dev/null || true
  fi

  cat > "$out/summary.md" <<EOF
# CloudEdge lab evidence: $run_id

- scenario: $scenario
- commit: $commit
- result: $result
- dataplane (matrix): aws=$aws_dp oci=$oci_dp azure=$az_dp onprem=$onprem_dp
- provider state: aws=$aws_ps oci=$oci_ps azure=$az_ps onprem=$onprem_ps
- directed_ping_ssh_matrix=$directed_matrix source_ip_preserved=$src_pres default_gateway_unchanged=$gw_ok no_nat=$no_nat
- failover_recovery_under_60s=$failover_recovery
- protocol_transparency=$protocol_transparency ftp_active_passive=$ftp_active_passive nfs_rpc=$nfs_rpc bulk_transfer_pmtu=$bulk_transfer_pmtu protocol_source_ip_preserved=$protocol_src_pres protocol_no_nat=$protocol_no_nat
- l2_loop_free=$l2_loop_free broadcast_storm_absent=$broadcast_storm_absent stp_rstp_stable=$stp_rstp_stable mac_flap_absent=$mac_flap_absent failover_ping_stable=$failover_ping_stable l2_suppression_mechanism_recorded=$l2_mechanism_recorded

TODO(lab-operator): fold provider inventory (routerctl get status, BGP mobility paths,
provider trap action plans, action journal, wg show, packet capture) into this
bundle, and record the seize/fencing assertions from the failover scenario. See
collect-evidence.sh.
EOF

  # Validate against the schema if python3 is available (no jsonschema dep needed
  # for well-formedness; do a structural check we can rely on).
  validate_evidence "$result_file" || die "evidence: result.json failed validation"

  log "evidence: wrote $result_file (result=$result)"
  printf '%s\n' "$result_file"
}

validate_evidence() {
  local f=$1
  have python3 || { log "validate: python3 absent, skipping deep validation"; return 0; }
  python3 - "$f" "$SCHEMA_FILE" <<'PY'
import json, sys
data = json.load(open(sys.argv[1]))
schema = json.load(open(sys.argv[2]))
errs = []
try:
    import jsonschema
except Exception:
    jsonschema = None
if jsonschema is not None:
    try:
        jsonschema.validate(instance=data, schema=schema)
    except Exception as e:
        errs.append(f"jsonschema validation failed: {e}")
for k in schema["required"]:
    if k not in data:
        errs.append(f"missing top-level key: {k}")
if data.get("result") not in ("pass", "fail"):
    errs.append("result must be pass|fail")
provs = data.get("providers", {})
for p in ("aws", "oci", "azure", "onprem"):
    if p not in provs:
        errs.append(f"missing provider {p}")
        continue
    for fld in ("dataplane", "providerState"):
        if provs[p].get(fld) not in ("pass", "fail", "skip", "na"):
            errs.append(f"providers.{p}.{fld} invalid")
need = {"ownership_epoch_bumped","allow_reassignment_maintained_until_success",
        "source_ip_preserved","default_gateway_unchanged",
        "old_holder_residue_absent","stale_action_fenced"}
got = {a.get("name") for a in data.get("assertions", [])}
miss = need - got
if miss:
    errs.append("missing assertions: " + ",".join(sorted(miss)))
cg = data.get("costGuard", {})
if not isinstance(cg.get("ttlHours"), (int, float)):
    errs.append("costGuard.ttlHours must be a number")
if cg.get("teardown") not in ("completed","pending","failed","skipped"):
    errs.append("costGuard.teardown invalid")
if errs:
    print("\n".join(errs), file=sys.stderr); sys.exit(1)
print("evidence schema OK")
PY
}

# =============================================================================
# down (teardown guard)
# =============================================================================
down_usage() {
  cat <<EOF
$SELF down - tear down tagged lab resources (cost guard)

USAGE:
  $SELF down --run-id <id>      Tear down resources tagged with this run.
  $SELF down --expired          Tear down ANY run whose ttl_expires_at is in the past.
  $SELF down --force            Skip confirmation (implied for automation).

Safe with no lab present: '--expired' is a no-op exit 0. Real teardown wraps
reset-lab.sh / provider CLIs filtered by the run-id tag, or is a TODO(lab-operator)
stub. '--help' is dry and needs no credentials.
EOF
}

latest_run_id() {
  # newest manifest by mtime; manifest names are run-ids (no special chars)
  local newest=""
  local f
  for f in "$CE_STATE_DIR"/*.manifest; do
    [[ -f "$f" ]] || continue
    if [[ -z "$newest" || "$f" -nt "$newest" ]]; then newest="$f"; fi
  done
  [[ -n "$newest" ]] || return 1
  basename "$newest" .manifest
}

teardown_run() {
  local run_id=$1
  local mf; mf=$(run_manifest_path "$run_id")
  log "down: tearing down run_id=$run_id"
  if [[ "$DRY_RUN" == "1" ]]; then
    log "[dry] would filter cloud resources by tag routerd.cloudedge.run_id=$run_id and delete/stop them"
  else
    # TODO(lab-operator): for each provider, query resources by the run-id tag and
    # stop/delete them. The CLI shapes are in reset-lab.sh (aws ec2 stop-instances,
    # az vm deallocate, oci compute instance action STOP, secondary-IP unassign).
    log "down: TODO(lab-operator) tag-filtered provider teardown not wired; reuse reset-lab.sh shapes"
  fi
  [[ -f "$mf" ]] && rm -f "$mf" && log "down: removed manifest for $run_id"
  return 0
}

cmd_down() {
  local run_id="" expired="0" force="0"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --run-id) run_id="${2:-}"; shift 2 ;;
      --expired) expired="1"; shift ;;
      --force) force="1"; shift ;;
      -h|--help) down_usage; exit 0 ;;
      *) die "down: unknown argument: $1" ;;
    esac
  done

  if [[ "$expired" == "1" ]]; then
    local now mf rid exp any="0"
    now=$(date -u +%s)
    if ! ls "$CE_STATE_DIR"/*.manifest >/dev/null 2>&1; then
      log "down --expired: no lab manifests; nothing to do"
      return 0
    fi
    for mf in "$CE_STATE_DIR"/*.manifest; do
      [[ -f "$mf" ]] || continue
      rid=$(sed -n 's/^run_id=//p' "$mf" | head -n1)
      exp=$(sed -n 's/^ttl_expires_at=//p' "$mf" | head -n1)
      local exp_s
      exp_s=$(date -u -d "$exp" +%s 2>/dev/null || date -u -j -f %Y-%m-%dT%H:%M:%SZ "$exp" +%s 2>/dev/null || echo 0)
      if [[ "$exp_s" != "0" && "$exp_s" -lt "$now" ]]; then
        log "down --expired: $rid expired at $exp"
        teardown_run "$rid"; any="1"
      fi
    done
    [[ "$any" == "1" ]] || log "down --expired: no expired runs"
    return 0
  fi

  if [[ -z "$run_id" ]]; then
    if [[ "$force" == "1" ]]; then
      run_id=$(latest_run_id || true)
      [[ -n "$run_id" ]] || { log "down --force: no active run; nothing to do"; return 0; }
    else
      down_usage >&2; die "down: provide --run-id <id>, or --expired, or --force"
    fi
  fi
  teardown_run "$run_id"
}

# =============================================================================
# top-level dispatch
# =============================================================================
version() {
  local v
  v=$(git -C "$REPO_ROOT" describe --tags --always 2>/dev/null || echo "unknown")
  printf '%s (routerd repo %s)\n' "cloudedge-labctl/0.1.0" "$v"
}

usage() {
  cat <<EOF
$SELF - CloudEdge SAM failover lab harness (one command, agent-drivable)

USAGE:
  $SELF <command> [options]

COMMANDS:
  up        Allocate/start a lab; stamp run-id + cost tags.
  deploy    Build static routerd (make dist) and push to nodes.
  smoke     Run the connectivity matrix (--matrix d3).
  failover  Inject a fault (stop-active|drain|heartbeat-stop|executor-fail|stale-replay).
  evidence  collect --out <dir>: assemble bundle + emit schema-valid result JSON.
  down      Teardown guard (--run-id <id> | --expired | --force).
  version   Print version.
  help      Show this help.

Run '$SELF <command> --help' for command-specific options.

SAFETY: cloud mutations are DRY by default (CE_DRY_RUN=1). Human gates (budget,
credentials, merge, production) are NOT automated. See
docs/how-to/cloudedge-autonomous-lab.md.
EOF
}

main() {
  local cmd="${1:-help}"
  if [[ $# -gt 0 ]]; then shift; fi
  case "$cmd" in
    up) cmd_up "$@" ;;
    deploy) cmd_deploy "$@" ;;
    smoke) cmd_smoke "$@" ;;
    failover) cmd_failover "$@" ;;
    evidence) cmd_evidence "$@" ;;
    down) cmd_down "$@" ;;
    version|--version|-V) version ;;
    help|-h|--help) usage ;;
    *) usage >&2; die "unknown command: $cmd" ;;
  esac
}

main "$@"
