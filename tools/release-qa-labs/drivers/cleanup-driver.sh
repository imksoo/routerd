#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=common.sh
source "$script_dir/common.sh"

requested_run_id=
cleanup_evidence=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --run-id) requested_run_id="${2:?missing --run-id value}"; shift 2 ;;
    --evidence-dir) cleanup_evidence="${2:?missing --evidence-dir value}"; shift 2 ;;
    -h|--help)
      echo "Usage: $(basename "$0") --run-id ID --evidence-dir DIR"
      exit 0
      ;;
    *) die "unknown argument: $1" ;;
  esac
done
[ -n "$requested_run_id" ] || die "--run-id is required"
[ -n "$cleanup_evidence" ] || die "--evidence-dir is required"
load_contract "$default_contract_path"
[ "$requested_run_id" = "$run_id" ] || die "requested run ID does not match contract"
cleanup_evidence="$(absolute_path "$cleanup_evidence")"
mkdir -p "$cleanup_evidence"
require_command tofu

execution_mode="$(jq -er '.execution.mode' "$contract_path")"
case "$execution_mode" in
  production|staging-no-mutation) ;;
  *) die "unsupported execution mode in cleanup: $execution_mode" ;;
esac

# A fresh staging run deliberately never initializes OpenTofu or executes the
# mutation driver.  With no state there is therefore nothing for OpenTofu to
# destroy; authoritative inventory remains the independent proof of zero.  Do
# not extend this exception to production, or to staging recovery with state.
if [ "$execution_mode" = staging-no-mutation ] && [ ! -f "$tofu_state_path" ]; then
  jq -n \
    --arg runId "$run_id" \
    --arg executionMode "$execution_mode" \
    '{runId: $runId, executionMode: $executionMode, action: "skip-tofu-destroy", reason: "staging-no-mutation-no-state"}' \
    >"$cleanup_evidence/cleanup-decision.json"
  : >"$cleanup_evidence/tofu-state-before-destroy.txt"
  : >"$cleanup_evidence/tofu-state-after-destroy.txt"
  echo "cleanup complete: runId=$run_id action=skip-tofu-destroy reason=staging-no-mutation-no-state"
  exit 0
fi

jq -n \
  --arg runId "$run_id" \
  --arg executionMode "$execution_mode" \
  '{runId: $runId, executionMode: $executionMode, action: "tofu-destroy", reason: "state-or-production"}' \
  >"$cleanup_evidence/cleanup-decision.json"
destroy_state_backup="$cleanup_evidence/tofu-pre-destroy.tfstate"
declare -a cleanup_failures=()
cleanup_errors="$cleanup_evidence/cleanup-errors.tsv"
printf 'step\texit\n' >"$cleanup_errors"

record_cleanup_failure() {
  local step="$1" status="$2"
  cleanup_failures+=("$step=$status")
  printf '%s\t%s\n' "$step" "$status" >>"$cleanup_errors"
}

tofu_init_status=0
if tofu -chdir="$tf_dir" init -input=false -lockfile=readonly -reconfigure \
  -backend-config="path=$tofu_state_path" \
  >"$cleanup_evidence/tofu-init.txt" \
  2>"$cleanup_evidence/tofu-init.stderr"; then
  :
else
  tofu_init_status=$?
  record_cleanup_failure tofu-init "$tofu_init_status"
fi

tofu_help_status=0
if tofu -chdir="$tf_dir" destroy -help >"$cleanup_evidence/tofu-destroy-help.txt"; then
  :
else
  tofu_help_status=$?
  record_cleanup_failure tofu-destroy-help "$tofu_help_status"
fi

if [ -f "$tofu_state_path" ]; then
  if tofu -chdir="$tf_dir" state list >"$cleanup_evidence/tofu-state-before-destroy.txt"; then
    :
  else
    record_cleanup_failure tofu-state-before-destroy "$?"
  fi
else
  : >"$cleanup_evidence/tofu-state-before-destroy.txt"
fi
if tofu -chdir="$tf_dir" output -json >"$cleanup_evidence/tofu-output-before-destroy.json" 2>"$cleanup_evidence/tofu-output-before-destroy.stderr"; then
  install -m 0600 "$cleanup_evidence/tofu-output-before-destroy.json" "$tofu_output_path"
fi

if [ "$tofu_init_status" -eq 0 ] && [ "$tofu_help_status" -eq 0 ]; then
  if run_with_progress tofu-destroy \
    tofu -chdir="$tf_dir" destroy -auto-approve -input=false \
    -backup="$destroy_state_backup" \
    -var-file="$tfvars_path"; then
    :
  else
    record_cleanup_failure tofu-destroy "$?"
  fi
else
  record_cleanup_failure tofu-destroy-not-attempted 125
fi

# A provider can create a PVE VM and fail before it records that resource in
# state. Recover only identity-matched run VMs before asking the bridge helper
# to prove the whole run inventory is gone. This guard never searches by name
# or deletes an unpinned VMID.
orphan_cleanup_driver="$script_dir/pve-orphan-cleanup.sh"
if run_with_progress pve-orphan-recovery "$orphan_cleanup_driver" \
  --evidence "$cleanup_evidence/pve-orphan-recovery.json"; then
  :
else
  record_cleanup_failure pve-orphan-recovery "$?"
fi

# The capture bridge is intentionally outside the API-token Terraform
# surface.  It is removed only after destroy has completed; the root-PVE
# helper independently refuses deletion until the cluster inventory confirms
# all six workloads and the disposable shared template stage are absent.
capture_bridge_driver="$script_dir/pve-capture-bridge.sh"
if run_with_progress pve-capture-bridge-remove "$capture_bridge_driver" \
  --remove --evidence "$cleanup_evidence/pve-capture-bridge-remove.json"; then
  :
else
  record_cleanup_failure pve-capture-bridge-remove "$?"
fi

tofu_state_after_status=0
if [ -f "$tofu_state_path" ]; then
  if tofu -chdir="$tf_dir" state list >"$cleanup_evidence/tofu-state-after-destroy.txt"; then
    :
  else
    tofu_state_after_status=$?
    record_cleanup_failure tofu-state-after-destroy "$tofu_state_after_status"
  fi
else
  : >"$cleanup_evidence/tofu-state-after-destroy.txt"
fi
if [ "$tofu_state_after_status" -eq 0 ] &&
  [ "$(wc -l <"$cleanup_evidence/tofu-state-after-destroy.txt")" -ne 0 ]; then
  record_cleanup_failure tofu-state-not-empty 1
fi
[ "${#cleanup_failures[@]}" -eq 0 ] ||
  die "cleanup incomplete: ${cleanup_failures[*]}"
echo "cleanup complete: runId=$run_id"
