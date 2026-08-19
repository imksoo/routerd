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

pve_dir="$evidence_root/certification/pve"
mkdir -p "$pve_dir"
mapfile -t pve_hosts < <(jq -er '([.pve.sshHost] + [.pve.rrNodes["pve-rr-a"].sshHost, .pve.rrNodes["pve-rr-b"].sshHost]) | unique[]' "$contract_path")
[ "${#pve_hosts[@]}" -eq 3 ] || fail_driver "PVE RR contract must name the leaf host and two distinct RR hosts"
pve_ssh=(ssh -n -i "$pve_ssh_private_key" -o BatchMode=yes -o StrictHostKeyChecking=yes \
  -o UserKnownHostsFile="$pve_ssh_known_hosts" -o GlobalKnownHostsFile=/dev/null \
  -o CanonicalizeHostname=no -o IdentitiesOnly=yes -o PasswordAuthentication=no \
  -o KbdInteractiveAuthentication=no -o ConnectTimeout=10)
for pve_host in "${pve_hosts[@]}"; do
  if "${pve_ssh[@]}" "root@$pve_host" \
    'pveversion && qm list' >"$pve_dir/pve-auth-inventory-${pve_host}.txt" 2>"$pve_dir/pve-auth-${pve_host}.stderr"; then
    :
  else
    fail_driver "PVE authentication or inventory failed for $pve_host"
  fi
done
record_check pve pve "PVE authenticated provider contexts" pass "PVE version and VM inventory resolved over the leaf host and both distinct RR hosts"
touch_heartbeat

# The PVE phase is deliberately first.  Initialize its run-confined local
# backend here, before creating even the isolated capture bridge; otherwise
# the first PVE plan fails before Cloud certification has a chance to run its
# later initialization step.
if ! run_with_progress tofu-pve-init \
  tofu -chdir="$tf_dir" init -input=false -lockfile=readonly \
    -backend-config="path=$tofu_state_path"; then
  fail_driver "PVE OpenTofu initialization failed"
fi

capture_bridge_driver="$script_dir/pve-capture-bridge.sh"
if ! run_with_progress pve-capture-bridge-ensure "$capture_bridge_driver" \
  --ensure --evidence "$pve_dir/capture-bridge-ensure.json"; then
  fail_driver "strict root PVE capture bridge staging failed"
fi

# The source image is an immutable local template.  Stage it alone first as a
# full-copy template on qnap, prove the resulting PVE config is a template
# backed by that shared datastore, then and only then permit six leaf/RR
# clones to be planned.  This is intentionally two applies: a graph edge
# prevents a race, while this intervening inspection proves the cross-host
# clone precondition rather than merely assuming it.
stage_plan="$plan_root/pve-template-stage.tfplan"
stage_state_backup="$pve_dir/tofu-template-stage-pre-apply.tfstate"
if ! run_with_progress tofu-pve-template-stage-plan \
  tofu -chdir="$tf_dir" plan -input=false -out="$stage_plan" \
  -var-file="$tfvars_path" \
  -target=proxmox_virtual_environment_vm.pve_shared_template_stage; then
  fail_driver "PVE shared-template stage plan failed"
fi
tofu -chdir="$tf_dir" show -json "$stage_plan" >"$pve_dir/pve-template-stage-plan.json"
stage_plan_count="$(jq '[.planned_values.root_module | recurse(.child_modules[]?) | .resources[]? | select(.type == "proxmox_virtual_environment_vm" and .name == "pve_shared_template_stage")] | length' "$pve_dir/pve-template-stage-plan.json")"
[ "$stage_plan_count" -eq 1 ] || fail_driver "PVE shared-template stage plan does not contain exactly one stage VM"
stage_actions_valid="$(jq '
  [.resource_changes[]? |
    select(.type == "proxmox_virtual_environment_vm" and .name == "pve_shared_template_stage") |
    .change.actions] as $stage |
  ([.resource_changes[]? | select(.type != "proxmox_virtual_environment_vm" or .name != "pve_shared_template_stage")] | length == 0) and
  ($stage | length == 1) and $stage[0] == ["create"]
' "$pve_dir/pve-template-stage-plan.json")"
[ "$stage_actions_valid" = true ] ||
  fail_driver "PVE shared-template stage plan must contain exactly one create action and no other resource actions"
if ! run_with_progress tofu-pve-template-stage-apply \
  tofu -chdir="$tf_dir" apply -input=false -auto-approve \
    -backup="$stage_state_backup" "$stage_plan"; then
  fail_driver "PVE shared-template stage apply failed"
fi

stage_node="$(jq -er '.pve.templateStage.sourceNode' "$contract_path")"
stage_vmid="$(jq -er '.pve.templateStage.vmid' "$contract_path")"
stage_datastore="$(jq -er '.pve.templateStage.datastore' "$contract_path")"
for value in "$stage_node" "$stage_vmid" "$stage_datastore"; do
  case "$value" in
    *[!A-Za-z0-9._-]*|'') fail_driver "unsafe PVE shared-template stage identity" ;;
  esac
done
printf -v stage_config_command 'pvesh get %q --output-format json' \
  "/nodes/$stage_node/qemu/$stage_vmid/config"
if ! "${pve_ssh[@]}" "root@$pve_ssh_host" "$stage_config_command" \
  >"$pve_dir/pve-template-stage-config.json" 2>"$pve_dir/pve-template-stage-config.stderr"; then
  fail_driver "could not inspect the PVE shared-template stage configuration"
fi
jq -e --arg datastore "$stage_datastore" '
  ((.template // false) == true or (.template // 0) == 1) and
  ([to_entries[] | select(.key | test("^(scsi|sata|virtio)[0-9]+$")) |
    .value | select(type == "string") | split(",")[0]] as $volumes |
   ($volumes | length) > 0 and
   all($volumes[]; startswith($datastore + ":")))
' "$pve_dir/pve-template-stage-config.json" >/dev/null ||
  fail_driver "PVE shared-template stage is not an actual template with every data disk on the certified shared datastore"
record_check pve pve "PVE shared template stage" pass "unstarted stage template is real and qnap/shared-datastore-backed before any leaf or RR clone"

plan="$plan_root/pve.tfplan"
pve_state_backup="$pve_dir/tofu-pve-pre-apply.tfstate"
if ! run_with_progress tofu-pve-plan \
  tofu -chdir="$tf_dir" plan -input=false -out="$plan" \
  -var-file="$tfvars_path" \
  -target=proxmox_virtual_environment_vm.pve_shared_template_stage \
  -target=module.pve_leaf \
  -target=module.pve_rr; then
  fail_driver "targeted PVE OpenTofu plan failed"
fi
tofu -chdir="$tf_dir" show -json "$plan" >"$pve_dir/pve-plan.json"
if ! python3 "$framework_root/qa_guard.py" plan \
  --plan-json "$pve_dir/pve-plan.json" --phase pve \
  --cost-ceiling "$(jq -er '.limits.maxEstimatedCostUsd' "$contract_path")"; then
  fail_driver "PVE plan exceeds closed topology policy"
fi
if ! run_with_progress tofu-pve-apply \
  tofu -chdir="$tf_dir" apply -input=false -auto-approve \
    -backup="$pve_state_backup" "$plan"; then
  fail_driver "targeted PVE OpenTofu apply failed"
fi

# PVE has its own output boundary.  A full output refresh here would query all
# cloud providers before their resources exist, which both spends credentials
# unnecessarily and makes a PVE-only failure depend on cloud availability.
pve_nodes_output="$pve_dir/tofu-output-pve-nodes.json"
pve_fabric_output="$pve_dir/tofu-output-pve-fabric.json"
pve_raw_output="$pve_dir/tofu-output-pve-raw.json"
if ! tofu -chdir="$tf_dir" output -json pve_nodes >"$pve_nodes_output"; then
  fail_driver "could not capture PVE node output"
fi
if ! tofu -chdir="$tf_dir" output -json pve_fabric >"$pve_fabric_output"; then
  fail_driver "could not capture PVE fabric output"
fi
if ! jq -n --slurpfile nodes "$pve_nodes_output" --slurpfile fabric "$pve_fabric_output" '
  {
    nodes: {value: $nodes[0]},
    fabric: {value: {pve: $fabric[0]}}
  }
  | if (
      (.nodes.value | type == "object") and
      ([.nodes.value | to_entries[] | select(.value.site != "pve")] | length == 0) and
      ([.nodes.value | to_entries[] | select(.value.site == "pve")] | length == 6) and
      (.fabric.value.pve.boot_source | type == "string")
    ) then . else error("PVE output is incomplete or contains a cloud node") end
' >"$pve_raw_output"; then
  fail_driver "PVE-only OpenTofu output is incomplete"
fi
pve_output_path="$pve_dir/tofu-output-pve-qga.json"
qga="$(routerd_script tests/e2e/cloudedge/scripts/sam-pve-qga-addresses.sh)"
pve_guest_known_hosts="$pve_dir/guest-known_hosts"
if ! run_with_progress pve-qga-addresses "$qga" \
  --tofu-output "$pve_raw_output" \
  --out "$pve_output_path" \
  --pve-ssh-key "$pve_ssh_private_key" \
  --pve-known-hosts "$pve_ssh_known_hosts" \
  --guest-known-hosts-out "$pve_guest_known_hosts" \
  --retries 24 --retry-sleep 5 \
  --evidence "$pve_dir/qga-addresses.txt"; then
  fail_driver "PVE management-address discovery failed"
fi
[ -s "$pve_guest_known_hosts" ] || fail_driver "QGA did not produce pinned PVE guest SSH host keys"

bridge_audit="$(routerd_script tests/e2e/cloudedge/scripts/sam-pve-bridge-audit.sh)"
if ! run_with_progress pve-bridge-audit "$bridge_audit" \
  --tofu-output "$pve_output_path" \
  --pve-node-ssh-host "$pve_ssh_host" \
  --pve-ssh-key "$pve_ssh_private_key" \
  --pve-known-hosts "$pve_ssh_known_hosts" \
  --evidence "$pve_dir/bridge-audit.txt"; then
  fail_driver "PVE capture bridge contains missing or unrelated VMs"
fi
capture_vmids="$(jq -c '[.nodes.value | to_entries[] | select(.value.site == "pve" and (.value.capture_bridge? != null)) | .value.vm_id] | sort' "$pve_output_path")"
contract_capture_vmids="$(jq -c '[.pve.vmids | to_entries[] | select(.key != "pve-rr-a" and .key != "pve-rr-b") | .value] | sort' "$contract_path")"
[ "$capture_vmids" = "$contract_capture_vmids" ] ||
  fail_driver "PVE capture bridge VMIDs do not equal the non-RR contract VMIDs"
record_check pve pve "PVE capture bridge isolation" pass "capture bridge contains only the four exact non-RR contract VMIDs; PVE RRs have no capture NIC"

actual_vmids="$(jq -c '[.nodes.value | to_entries[] | select(.value.site == "pve") | .value.vm_id] | sort' "$pve_output_path")"
contract_vmids="$(jq -c '[.pve.vmids[]] | sort' "$contract_path")"
[ "$actual_vmids" = "$contract_vmids" ] ||
  fail_driver "PVE output VMIDs do not equal contract VMIDs"

# PVE root SSH is constrained to hypervisor inspection above.  Verify only
# the six PVE guests here; cloud guests do not exist until the subsequent
# cloud phase and are exercised by the real full-topology qualification.
ssh_key="$guest_ssh_private_key"
ssh_results="$pve_dir/ssh-hostnames.tsv"
pve_guest_ssh=(ssh -n -i "$ssh_key" -o BatchMode=yes -o StrictHostKeyChecking=yes \
  -o UserKnownHostsFile="$pve_guest_known_hosts" -o GlobalKnownHostsFile=/dev/null \
  -o CanonicalizeHostname=no -o IdentitiesOnly=yes -o PasswordAuthentication=no \
  -o KbdInteractiveAuthentication=no -o ConnectTimeout=10)
printf 'node\tresult\texpected\tobserved\n' >"$ssh_results"
ssh_failures=0
while IFS=$'\t' read -r node user address; do
  observed=
  expected="routerd-$run_id-$node"
  for _ in $(seq 1 36); do
    if observed="$("${pve_guest_ssh[@]}" \
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
     select(.value.site == "pve") |
     [.key, .value.ssh_user, (.value.public_ip // .value.management_ip)] | @tsv' \
    "$pve_output_path"
)
[ "$ssh_failures" -eq 0 ] ||
  fail_driver "PVE guest SSH hostname readiness failed for $ssh_failures nodes"

record_check pve pve "PVE substrate inventory" pass "six dedicated PVE VMs, including two host-redundant RRs, exist at the exact contract VMIDs and are SSH-ready"
write_driver_result "$out_arg" pass "Fresh PVE substrate applied from pinned OpenTofu source without repair."
echo "PVE certification driver: pass"
