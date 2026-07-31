#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=common.sh
source "$script_dir/common.sh"
parse_driver_args "$@"
reset_checks

fail_driver() {
  local summary="$1"
  record_check tooling "" "PVE certification execution" fail "$summary"
  write_driver_result "$out_arg" fail "$summary"
  exit 1
}

for command in tofu jq ssh; do
  require_command "$command"
done
require_supervisor_mutating || fail_driver "durable lifecycle supervisor is not running before PVE apply"

pve_host="$pve_ssh_host"
pve_dir="$evidence_root/certification/pve"
mkdir -p "$pve_dir"
if ssh -i "$pve_ssh_private_key" -o BatchMode=yes -o ConnectTimeout=10 "root@$pve_host" \
  'pveversion && qm list' >"$pve_dir/pve-auth-inventory.txt" 2>"$pve_dir/pve-auth.stderr"; then
  record_check pve pve "PVE authenticated provider context" pass "PVE version and VM inventory resolved over the declared host"
else
  fail_driver "PVE authentication or inventory failed"
fi
touch_heartbeat

plan="$plan_root/pve.tfplan"
if ! run_with_progress tofu-pve-plan \
  tofu -chdir="$tf_dir" plan -input=false -out="$plan" \
  -var-file="$tfvars_path" \
  -target=module.pve_leaf; then
  fail_driver "targeted PVE OpenTofu plan failed"
fi
tofu -chdir="$tf_dir" show -json "$plan" >"$pve_dir/pve-plan.json"
if ! python3 "$framework_root/qa_guard.py" plan \
  --plan-json "$pve_dir/pve-plan.json" --phase pve \
  --cost-ceiling "$(jq -er '.limits.maxEstimatedCostUsd' "$contract_path")"; then
  fail_driver "PVE plan exceeds closed topology policy"
fi
if ! run_with_progress tofu-pve-apply \
  tofu -chdir="$tf_dir" apply -input=false -auto-approve "$plan"; then
  fail_driver "targeted PVE OpenTofu apply failed"
fi

if ! run_with_progress tofu-full-output-refresh \
  tofu -chdir="$tf_dir" apply -refresh-only -input=false -auto-approve \
  -var-file="$tfvars_path"; then
  fail_driver "full OpenTofu output refresh failed"
fi

raw_output="$pve_dir/tofu-output-full-raw.json"
if ! tofu -chdir="$tf_dir" output -json >"$raw_output"; then
  fail_driver "could not capture full OpenTofu output"
fi
qga="$(routerd_script tests/e2e/cloudedge/scripts/sam-pve-qga-addresses.sh)"
if ! run_with_progress pve-qga-addresses "$qga" \
  --tofu-output "$raw_output" \
  --out "$tofu_output_path" \
  --pve-node-ssh-host "$pve_host" \
  --ssh-key "$pve_ssh_private_key" \
  --evidence "$pve_dir/qga-addresses.txt"; then
  fail_driver "PVE management-address discovery failed"
fi

bridge_audit="$(routerd_script tests/e2e/cloudedge/scripts/sam-pve-bridge-audit.sh)"
if ! run_with_progress pve-bridge-audit "$bridge_audit" \
  --tofu-output "$tofu_output_path" \
  --pve-node-ssh-host "$pve_host" \
  --ssh-key "$pve_ssh_private_key" \
  --evidence "$pve_dir/bridge-audit.txt"; then
  fail_driver "PVE capture bridge contains missing or unrelated VMs"
fi
record_check pve pve "PVE capture bridge isolation" pass "capture bridge contains only the four exact contract VMIDs"

actual_vmids="$(jq -c '[.nodes.value | to_entries[] | select(.value.site == "pve") | .value.vm_id] | sort' "$tofu_output_path")"
contract_vmids="$(jq -c '.pve.vmids | sort' "$contract_path")"
[ "$actual_vmids" = "$contract_vmids" ] ||
  fail_driver "PVE output VMIDs do not equal contract VMIDs"

ssh_key="$pve_ssh_private_key"
ssh_results="$pve_dir/ssh-hostnames.tsv"
printf 'node\tresult\texpected\tobserved\n' >"$ssh_results"
ssh_failures=0
while IFS=$'\t' read -r node site user address private_address; do
  observed=
  case "$site" in
    aws) expected="ip-${private_address//./-}" ;;
    azure) expected="$node" ;;
    oci) expected="$node-primary-vnic" ;;
    pve) expected="routerd-$run_id-$node" ;;
    *) expected= ;;
  esac
  for _ in $(seq 1 36); do
    if observed="$(ssh -n -i "$ssh_key" -o BatchMode=yes -o StrictHostKeyChecking=no \
      -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 \
      "$user@$address" hostname 2>/dev/null)"; then
      break
    fi
    sleep 5
    touch_heartbeat
  done
  if [ -n "$expected" ] && [ "$observed" = "$expected" ]; then
    printf '%s\tPASS\t%s\t%s\n' "$node" "$expected" "$observed" >>"$ssh_results"
  else
    printf '%s\tFAIL\t%s\t%s\n' \
      "$node" "${expected:-unknown-site}" "${observed:-unreachable}" >>"$ssh_results"
    ssh_failures=$((ssh_failures + 1))
  fi
  touch_heartbeat
done < <(
  jq -r \
    '.nodes.value | to_entries[] |
     [.key, .value.site, .value.ssh_user,
      (.value.public_ip // .value.private_ip), .value.private_ip] | @tsv' \
    "$tofu_output_path"
)
[ "$ssh_failures" -eq 0 ] ||
  fail_driver "full topology SSH hostname readiness failed for $ssh_failures nodes"

record_check pve pve "PVE full substrate inventory" pass "four dedicated VMs exist at the exact contract VMIDs and are SSH-ready"
record_check cross-substrate "" "full topology output and SSH identity" pass "all eighteen fresh cloud and PVE nodes returned their declared hostname"
write_driver_result "$out_arg" pass "Fresh PVE substrate applied from pinned OpenTofu source without repair."
echo "PVE certification driver: pass"
