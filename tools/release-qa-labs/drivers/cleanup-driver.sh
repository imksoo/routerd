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

tofu -chdir="$tf_dir" destroy -help >"$cleanup_evidence/tofu-destroy-help.txt"
if [ -f "$tofu_state_path" ]; then
  tofu -chdir="$tf_dir" state list >"$cleanup_evidence/tofu-state-before-destroy.txt"
else
  : >"$cleanup_evidence/tofu-state-before-destroy.txt"
fi
if tofu -chdir="$tf_dir" output -json >"$cleanup_evidence/tofu-output-before-destroy.json" 2>"$cleanup_evidence/tofu-output-before-destroy.stderr"; then
  install -m 0600 "$cleanup_evidence/tofu-output-before-destroy.json" "$tofu_output_path"
fi

run_with_progress tofu-destroy \
  env TF_CLI_CONFIG_FILE="$framework_root/tofu.rc" \
  tofu -chdir="$tf_dir" destroy -auto-approve -input=false \
  -var-file="$tfvars_path"

if [ -f "$tofu_state_path" ]; then
  tofu -chdir="$tf_dir" state list >"$cleanup_evidence/tofu-state-after-destroy.txt"
else
  : >"$cleanup_evidence/tofu-state-after-destroy.txt"
fi
[ "$(wc -l <"$cleanup_evidence/tofu-state-after-destroy.txt")" -eq 0 ] ||
  die "OpenTofu state is not empty after destroy"
echo "cleanup complete: runId=$run_id"
