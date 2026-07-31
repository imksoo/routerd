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
  record_check tooling "" "cloud certification execution" fail "$summary"
  write_driver_result "$out_arg" fail "$summary"
  exit 1
}

for command in tofu jq grep aws az oci ssh; do
  require_command "$command"
done
require_supervisor_mutating || fail_driver "durable lifecycle supervisor is not running before cloud apply"

aws_profile="$(extract_tfvars_string "$tfvars_path" aws_profile)"
aws_region="$(extract_tfvars_string "$tfvars_path" aws_region)"
oci_profile="$(extract_tfvars_string "$tfvars_path" oci_profile)"
oci_region="$(extract_tfvars_string "$tfvars_path" oci_region)"
oci_compartment_id="$(extract_tfvars_string "$tfvars_path" oci_compartment_id)"
aws_profile="${aws_profile:-default}"
oci_profile="${oci_profile:-DEFAULT}"

auth_dir="$evidence_root/certification/cloud/auth"
mkdir -p "$auth_dir"
if aws --profile "$aws_profile" --region "$aws_region" sts get-caller-identity \
  >"$auth_dir/aws.json" 2>"$auth_dir/aws.stderr"; then
  record_check cloud aws "AWS authenticated provider context" pass "STS identity resolved for declared profile and region"
else
  fail_driver "AWS authentication failed"
fi
if az account show --output json >"$auth_dir/azure.json" 2>"$auth_dir/azure.stderr"; then
  record_check cloud azure "Azure authenticated provider context" pass "active subscription resolved"
else
  fail_driver "Azure authentication failed"
fi
if oci --profile "$oci_profile" --region "$oci_region" iam region-subscription list \
  >"$auth_dir/oci.json" 2>"$auth_dir/oci.stderr"; then
  record_check cloud oci "OCI authenticated provider context" pass "region subscriptions resolved for declared profile"
else
  fail_driver "OCI authentication failed"
fi
touch_heartbeat

preflight="$(routerd_script tests/e2e/cloudedge/scripts/sam-preflight.sh)"
preflight_dir="$evidence_root/certification/cloud/preflight"
mkdir -p "$preflight_dir"
if ! run_with_progress cloud-sam-preflight "$preflight" \
  --tfvars "$tfvars_path" \
  --artifact "$artifact_path" \
  --evidence-dir "$preflight_dir"; then
  fail_driver "CloudEdge provider/artifact preflight failed"
fi
record_check tooling "" "CloudEdge pre-apply contract" pass "artifact, name budget, and OCI compartment preflight passed"

tofu -chdir="$tf_dir" apply -help >"$preflight_dir/tofu-apply-help.txt"
if ! run_with_progress tofu-init \
  tofu -chdir="$tf_dir" init -input=false -lockfile=readonly \
    -backend-config="path=$tofu_state_path"; then
  fail_driver "OpenTofu init failed"
fi
if ! tofu -chdir="$tf_dir" providers >"$preflight_dir/tofu-providers.txt"; then
  fail_driver "OpenTofu provider graph inspection failed"
fi
if grep -Fq 'provider[registry.opentofu.org/hashicorp/oci]' \
  "$preflight_dir/tofu-providers.txt"; then
  fail_driver "OCI module resolved the unconfigured hashicorp/oci provider"
fi
record_check tooling oci "OCI provider inheritance" pass "all OCI resources resolve oracle/oci"
if ! run_with_progress tofu-validate tofu -chdir="$tf_dir" validate; then
  fail_driver "OpenTofu validate failed"
fi
if ! run_with_progress tofu-oci-auth-plan \
  tofu -chdir="$tf_dir" plan -input=false -refresh-only \
  -var-file="$tfvars_path" \
  -target=data.oci_identity_availability_domains.provider_auth; then
  fail_driver "OCI provider read-only authentication plan failed"
fi
record_check tooling oci "OCI provider API authentication" pass "oracle/oci read availability domains using the declared profile"

pre_inventory="$evidence_root/certification/pre-apply-inventory"
if ! "$script_dir/inventory-driver.sh" \
  --run-id "$run_id" --evidence-dir "$pre_inventory"; then
  fail_driver "pre-apply inventory is not zero"
fi
record_check tooling "" "fresh run inventory" pass "OpenTofu, cloud run-id, and exact PVE VM inventory are zero"

plan="$plan_root/cloud.tfplan"
if ! run_with_progress tofu-cloud-plan \
  tofu -chdir="$tf_dir" plan -input=false -out="$plan" \
  -var-file="$tfvars_path" \
  -target=module.aws_rr \
  -target=module.aws_leaf \
  -target=module.azure_leaf \
  -target=module.oci_leaf; then
  fail_driver "targeted cloud OpenTofu plan failed"
fi
tofu -chdir="$tf_dir" show -json "$plan" >"$preflight_dir/cloud-plan.json"
if ! python3 "$framework_root/qa_guard.py" plan \
  --plan-json "$preflight_dir/cloud-plan.json" --phase cloud \
  --cost-ceiling "$(jq -er '.limits.maxEstimatedCostUsd' "$contract_path")"; then
  fail_driver "cloud plan exceeds closed topology or cost policy"
fi
if ! run_with_progress tofu-cloud-apply \
  tofu -chdir="$tf_dir" apply -input=false -auto-approve "$plan"; then
  fail_driver "targeted cloud OpenTofu apply failed"
fi

tofu -chdir="$tf_dir" show -json >"$evidence_root/certification/cloud/tofu-state-after-cloud.json"
touch_heartbeat

aws --profile "$aws_profile" --region "$aws_region" ec2 describe-instances \
  --filters "Name=tag:routerd-run-id,Values=$run_id" \
  >"$evidence_root/certification/cloud/aws-instances.json"
aws_running="$(jq '[.Reservations[].Instances[] | select(.State.Name == "running")] | length' "$evidence_root/certification/cloud/aws-instances.json")"
[ "$aws_running" -eq 6 ] ||
  fail_driver "not all AWS nodes are running"
record_check cloud aws "AWS full substrate inventory" pass "six exact run instances are running"

azure_rg="rg-routerd-${run_id}-azure"
az vm list --resource-group "$azure_rg" --show-details --output json \
  >"$evidence_root/certification/cloud/azure-vms.json"
azure_running="$(jq '[.[] | select(.powerState == "VM running")] | length' "$evidence_root/certification/cloud/azure-vms.json")"
[ "$azure_running" -eq 4 ] ||
  fail_driver "not all four Azure nodes are running"
record_check cloud azure "Azure full substrate inventory" pass "four exact run VMs are running"

oci --profile "$oci_profile" --region "$oci_region" compute instance list \
  --compartment-id "$oci_compartment_id" --all \
  >"$evidence_root/certification/cloud/oci-instances.json"
oci_running="$(
  jq --arg runId "$run_id" \
    '[.data[] | select(."freeform-tags".RouterdRunId == $runId and ."lifecycle-state" == "RUNNING")] | length' \
    "$evidence_root/certification/cloud/oci-instances.json"
)"
[ "$oci_running" -eq 4 ] ||
  fail_driver "not all four OCI nodes are running"
record_check cloud oci "OCI full substrate inventory" pass "four exact run instances are running"

record_check cross-substrate "" "cloud provider inventory convergence" pass "all fourteen fresh cloud nodes are running in the declared provider contexts"
write_driver_result "$out_arg" pass "Fresh AWS/Azure/OCI substrate applied from the pinned OpenTofu source without repair."
echo "cloud certification driver: pass"
