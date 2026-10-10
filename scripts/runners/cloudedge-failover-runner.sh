#!/usr/bin/env bash
# Live FAILOVER_TIMING_RUNNER implementation.

set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/runners/cloudedge-runner-lib.sh
. "$SCRIPT_DIR/cloudedge-runner-lib.sh"

usage() {
  cat <<EOF
$SELF - CloudEdge live FAILOVER_TIMING_RUNNER

USAGE:
  FAILOVER_TIMING_RUNNER=$SCRIPT_DIR/$SELF scripts/cloudedge-failover-timing.sh ...
  $SELF inject  <provider> <fault>
  $SELF observe <provider> detection|switchover|recovery
  $SELF detail  <provider> <stage>

DESIGN:
  inject stops/drains the active VM/node. observe only reads routerd state,
  action journal, and dataplane health. It never calls routerctl action approve
  or action execute.

ENV, common:
  CE_ROUTERD_STATE_DB=/var/lib/routerd/routerd.db
  CE_FAILOVER_RECOVERY_SRC=aws
  CE_FAILOVER_RECOVERY_DST_IP=10.77.60.10
  MATRIX_RUNNER=scripts/runners/cloudedge-matrix-runner.sh
  CE_<PROVIDER>_<STAGE>_COMMAND  Optional local override for observe stage.

ENV, injection:
  CE_<PROVIDER>_INJECT_COMMAND   Optional local override for inject.
  AWS_REGION, AWS_PROFILE, AWS_ROUTER_A_INSTANCE_ID or CE_AWS_ACTIVE_INSTANCE_ID
  AZURE_RESOURCE_GROUP, AZURE_ROUTER_VM_NAME or CE_AZURE_ACTIVE_VM_ID
  OCI_ROUTER_INSTANCE_REF or CE_OCI_ACTIVE_INSTANCE_ID
  CE_ONPREM_INJECT_COMMAND or CE_ONPREM_ACTIVE_ROUTER_SSH_HOST

ENV, observation hints:
  CE_<PROVIDER>_EXPECTED_STANDBY_NODE  Expected ownership/capture holder.
  CE_<PROVIDER>_STOPPED_NODE           Node expected to disappear as holder.
  CE_<PROVIDER>_CAPTURE_ADDRESS        Logical captured address to inspect.
EOF
}

provider_upper() { ce_upper "$1"; }

inject_override_command() {
  local provider=$1 upper cmd
  upper=$(provider_upper "$provider")
  cmd=$(ce_env_first "CE_${upper}_INJECT_COMMAND" "CE_INJECT_COMMAND" 2>/dev/null || true)
  printf '%s' "$cmd"
}

inject_aws() {
  local id
  id=$(ce_env_first CE_AWS_ACTIVE_INSTANCE_ID AWS_ROUTER_A_INSTANCE_ID 2>/dev/null || true)
  [[ -n "$id" ]] || ce_die "AWS active instance id missing"
  ce_have aws || ce_die "aws CLI not found"
  local args=()
  [[ -n "${AWS_PROFILE:-}" ]] && args+=(--profile "$AWS_PROFILE")
  [[ -n "${AWS_REGION:-}" ]] && args+=(--region "$AWS_REGION")
  aws "${args[@]}" ec2 stop-instances --instance-ids "$id" --output json >/dev/null
  printf 'stopped_instance=%s\n' "$id"
}

inject_azure() {
  ce_have az || ce_die "az CLI not found"
  if [[ -n "${CE_AZURE_ACTIVE_VM_ID:-}" ]]; then
    az vm stop --ids "$CE_AZURE_ACTIVE_VM_ID" --output json >/dev/null
    printf 'stopped_vm_id=%s\n' "$CE_AZURE_ACTIVE_VM_ID"
    return
  fi
  [[ -n "${AZURE_RESOURCE_GROUP:-}" && -n "${AZURE_ROUTER_VM_NAME:-}" ]] \
    || ce_die "Azure VM identity missing"
  az vm stop --resource-group "$AZURE_RESOURCE_GROUP" --name "$AZURE_ROUTER_VM_NAME" --output json >/dev/null
  printf 'stopped_vm=%s/%s\n' "$AZURE_RESOURCE_GROUP" "$AZURE_ROUTER_VM_NAME"
}

inject_oci() {
  ce_have oci || ce_die "oci CLI not found"
  local id
  id=$(ce_env_first CE_OCI_ACTIVE_INSTANCE_ID OCI_ROUTER_INSTANCE_REF 2>/dev/null || true)
  [[ -n "$id" ]] || ce_die "OCI active instance id missing"
  local args=()
  [[ -n "${OCI_CONFIG_FILE:-}" ]] && args+=(--config-file "$OCI_CONFIG_FILE")
  [[ -n "${OCI_PROFILE:-}" ]] && args+=(--profile "$OCI_PROFILE")
  [[ -n "${OCI_REGION:-}" ]] && args+=(--region "$OCI_REGION")
  oci "${args[@]}" compute instance action --action STOP --instance-id "$id" --output json >/dev/null
  printf 'stopped_instance=%s\n' "$id"
}

inject_onprem() {
  local cmd=${CE_ONPREM_REMOTE_INJECT_COMMAND:-"sudo systemctl stop keepalived || sudo service keepalived stop"}
  ce_router_ssh onprem active "$cmd"
  printf 'onprem_inject=remote-vrrp-stop\n'
}

cmd_inject() {
  local provider=$1 fault=$2 override
  case "$fault" in
    stop-active|drain) ;;
    *) ce_die "unsupported fault: $fault" ;;
  esac
  override=$(inject_override_command "$provider")
  if [[ -n "$override" ]]; then
    bash -lc "$override" || return $?
    return 0
  fi
  case "$provider" in
    aws) inject_aws ;;
    azure) inject_azure ;;
    oci) inject_oci ;;
    onprem) inject_onprem ;;
    *) ce_die "bad provider: $provider" ;;
  esac
}

expected_standby_node() {
  local provider=$1 upper
  upper=$(provider_upper "$provider")
  ce_env "CE_${upper}_EXPECTED_STANDBY_NODE" "$(ce_env "${upper}_ROUTER_B_NODE_REF" "")"
}

stopped_node() {
  local provider=$1 upper
  upper=$(provider_upper "$provider")
  ce_env "CE_${upper}_STOPPED_NODE" "$(ce_env "${upper}_ROUTER_A_NODE_REF" "")"
}

capture_address() {
  local provider=$1 upper
  upper=$(provider_upper "$provider")
  ce_env "CE_${upper}_CAPTURE_ADDRESS" "$(ce_env "${upper}_CLIENT_IP" "")"
}

sql_value() {
  local provider=$1 sql=$2
  ce_router_sql "$provider" observer "$sql" | head -n1
}

current_capture_predicate() {
  local provider=$1 expected address since
  expected=$(expected_standby_node "$provider")
  address=$(capture_address "$provider")
  since=${CE_FAILOVER_STARTED_MS:-}
  [[ -n "$expected" && "$expected" =~ ^[a-zA-Z0-9_.:/-]+$ &&
     -n "$address" && "$address" =~ ^[a-zA-Z0-9_.:/-]+$ &&
     "$since" =~ ^[0-9]+$ ]] || {
    echo 'current fault timestamp, target address or standby identity unavailable' >&2
    return 2
  }
  printf "EXISTS (SELECT 1 FROM json_tree(action_executions.target_json) WHERE atom = '%s') AND json_extract(parameters_json,'$.mobilityCaptureHolder') = '%s' AND julianday(updated_at) >= 2440587.5 + %s / 86400000.0" "$address" "$expected" "$since"
}

observe_detection_default() {
  local provider=$1 predicate count
  predicate=$(current_capture_predicate "$provider") || return 2
  count=$(sql_value "$provider" "SELECT COUNT(*) FROM action_executions WHERE $predicate;") || return 2
  [[ "$count" =~ ^[0-9]+$ ]] || return 2
  printf 'current_target_actions=%s\n' "$count"
  [[ "$count" -gt 0 ]]
}

observe_switchover_default() {
  local provider=$1 predicate count
  predicate=$(current_capture_predicate "$provider") || return 2
  count=$(sql_value "$provider" "SELECT COUNT(*) FROM action_executions WHERE $predicate AND status = 'succeeded' AND action IN ('assign-secondary-ip','ensure-forwarding-enabled');") || return 2
  [[ "$count" =~ ^[0-9]+$ ]] || return 2
  printf 'current_target_succeeded_actions=%s\n' "$count"
  [[ "$count" -gt 0 ]]
}

observe_recovery_default() {
  local provider=$1 src dst runner
  src=${CE_FAILOVER_RECOVERY_SRC:-$provider}
  dst=${CE_FAILOVER_RECOVERY_DST_IP:-}
  [[ -n "$dst" ]] || dst=$(ce_site_ip onprem)
  [[ -n "$dst" ]] || ce_die "recovery destination missing (set CE_FAILOVER_RECOVERY_DST_IP)"
  runner=${MATRIX_RUNNER:-$SCRIPT_DIR/cloudedge-matrix-runner.sh}
  "$runner" ping "$src" "$dst" >/dev/null
  "$runner" ssh "$src" "$dst" >/dev/null
}

cmd_observe() {
  local provider=$1 stage=$2
  local override
  override=$(ce_env_first "CE_$(provider_upper "$provider")_$(ce_upper "$stage")_COMMAND" 2>/dev/null || true)
  if [[ -n "$override" ]]; then
    bash -lc "$override"
    return
  fi
  case "$stage" in
    detection) observe_detection_default "$provider" ;;
    switchover) observe_switchover_default "$provider" ;;
    recovery) observe_recovery_default "$provider" ;;
    *) ce_die "bad stage: $stage" ;;
  esac
}

cmd_detail() {
  local provider=$1 stage=$2 predicate record
  predicate=$(current_capture_predicate "$provider") || return 2
  record=$(sql_value "$provider" "SELECT json_object('action',action,'status',status,'target',json(target_json),'holder',json_extract(parameters_json,'$.mobilityCaptureHolder'),'updatedAt',updated_at) FROM action_executions WHERE $predicate ORDER BY julianday(updated_at) DESC LIMIT 1;") || return 2
  printf 'stage=%s provider=%s current_target_record=%s\\n' "$stage" "$provider" "$record"
}

main() {
  local op=${1:-}
  case "$op" in
    inject)
      [[ $# -eq 3 ]] || ce_die "inject requires <provider> <fault>"
      cmd_inject "$2" "$3"
      ;;
    observe)
      [[ $# -eq 3 ]] || ce_die "observe requires <provider> <stage>"
      cmd_observe "$2" "$3"
      ;;
    detail)
      [[ $# -eq 3 ]] || ce_die "detail requires <provider> <stage>"
      cmd_detail "$2" "$3"
      ;;
    -h|--help|help|"")
      usage
      ;;
    *)
      ce_die "unknown op: $op"
      ;;
  esac
}

main "$@"
