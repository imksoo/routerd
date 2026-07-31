#!/usr/bin/env bash
set -euo pipefail
umask 077

[ "$#" -eq 1 ] || { echo "Usage: $(basename "$0") CONTRACT" >&2; exit 2; }
contract_path="$(readlink -m "$1")"
runtime_root="$(dirname "$contract_path")"
run_root="$(dirname "$runtime_root")"
run_id="$(basename "$run_root")"
expected_root="/var/lib/routerd-release-qa/$run_id"
[ "$run_root" = "$expected_root" ] || {
  echo "release QA launcher: contract must be $expected_root/runtime/contract.json" >&2; exit 2;
}
[ "$contract_path" = "$run_root/runtime/contract.json" ] || {
  echo "release QA launcher: noncanonical contract path" >&2; exit 2;
}

framework_root="$run_root/repo/tools/release-qa-labs"
state="$runtime_root/evidence/lifecycle/supervisor-state.json"
heartbeat="$runtime_root/evidence/lifecycle/heartbeat"

# On restart no mutable source input is read here. Supervisor authenticates the
# durable state and pinned contract, including its persisted lifecycle values.
exec python3 "$framework_root/lifecycle_supervisor.py" \
  --run-id "$run_id" --run-root "$run_root" --state "$state" --heartbeat "$heartbeat" \
  --contract "$contract_path" --run-env "$runtime_root/run.env.json" \
  --tfvars "$runtime_root/terraform.tfvars" \
  --pve-ssh-private-key "$runtime_root/secrets/pve_ssh" \
  --precheck-command "$framework_root/drivers/precheck-driver.sh" \
  --mutation-command "$framework_root/drivers/mutation-driver.sh" \
  --cleanup-command "$framework_root/drivers/supervisor-cleanup.sh" \
  --inventory-command "$framework_root/drivers/supervisor-inventory.sh"
