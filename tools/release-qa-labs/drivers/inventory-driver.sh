#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=common.sh
source "$script_dir/common.sh"

requested_run_id=
inventory_evidence=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --run-id) requested_run_id="${2:?missing --run-id value}"; shift 2 ;;
    --evidence-dir) inventory_evidence="${2:?missing --evidence-dir value}"; shift 2 ;;
    -h|--help)
      echo "Usage: $(basename "$0") --run-id ID --evidence-dir DIR"
      exit 0
      ;;
    *) die "unknown argument: $1" ;;
  esac
done
[ -n "$requested_run_id" ] || die "--run-id is required"
[ -n "$inventory_evidence" ] || die "--evidence-dir is required"
load_contract "$default_contract_path"
[ "$requested_run_id" = "$run_id" ] || die "requested run ID does not match contract"
inventory_evidence="$(absolute_path "$inventory_evidence")"
mkdir -p "$inventory_evidence"
status=0
summary="$inventory_evidence/private-inventory.tsv"
printf 'scope\tresult\tdetail\n' >"$summary"

record() {
  printf '%s\t%s\t%s\n' "$1" "$2" "$3" >>"$summary"
  [ "$2" = PASS ] || status=1
}

require_command tofu
require_command aws
require_command az
require_command oci
require_command ssh

if [ -f "$tofu_state_path" ]; then
  tofu -chdir="$tf_dir" state list >"$inventory_evidence/tofu-state-list.txt"
else
  : >"$inventory_evidence/tofu-state-list.txt"
fi
state_count="$(wc -l <"$inventory_evidence/tofu-state-list.txt")"
if [ "$state_count" -eq 0 ]; then
  record tofu-state PASS "resource count=0"
else
  record tofu-state FAIL "resource count=$state_count"
fi

aws_region="$(extract_tfvars_string "$tfvars_path" aws_region)"
aws_profile="$(extract_tfvars_string "$tfvars_path" aws_profile)"
aws_profile="${aws_profile:-default}"
aws_active="$inventory_evidence/aws-active-instances.json"
aws --profile "$aws_profile" ec2 describe-instances \
  --region "$aws_region" \
  --filters "Name=tag:routerd-run-id,Values=$run_id" \
  "Name=instance-state-name,Values=pending,running,shutting-down,stopping,stopped" \
  >"$aws_active"
aws_count="$(jq '[.Reservations[].Instances[]] | length' "$aws_active")"
if [ "$aws_count" -eq 0 ]; then
  record aws-run-resources PASS "active instance count=0"
else
  record aws-run-resources FAIL "active instance count=$aws_count"
fi

aws_tagged="$inventory_evidence/aws-tagged-resources.json"
aws resourcegroupstaggingapi get-resources \
  --profile "$aws_profile" --region "$aws_region" \
  --tag-filters "Key=routerd-run-id,Values=$run_id" \
  --output json >"$aws_tagged"
jq -e '((.PaginationToken // .NextToken // "") == "")' "$aws_tagged" >/dev/null ||
  die "AWS tagged-resource inventory is partial"
aws_tagged_count="$(jq '[.ResourceTagMappingList[]] | length' "$aws_tagged")"
if [ "$aws_tagged_count" -eq 0 ]; then
  record aws-tagged-resources PASS "all paginated run-tagged resources=0"
else
  record aws-tagged-resources FAIL "run-tagged resource count=$aws_tagged_count"
fi

azure_rg="rg-routerd-${run_id}-azure"
azure_exists="$(az group exists --name "$azure_rg")"
printf '%s\n' "$azure_exists" >"$inventory_evidence/azure-group-exists.txt"
if [ "$azure_exists" = false ]; then
  record azure-run-resources PASS "$azure_rg absent"
  azure_contained_count=0
else
  record azure-run-resources FAIL "$azure_rg still exists"
  az resource list --resource-group "$azure_rg" --output json \
    >"$inventory_evidence/azure-contained-resources.json"
  jq -e 'type == "array"' "$inventory_evidence/azure-contained-resources.json" >/dev/null ||
    die "Azure contained-resource inventory is invalid or partial"
  azure_contained_count="$(jq 'length' "$inventory_evidence/azure-contained-resources.json")"
fi
if [ "$azure_contained_count" -eq 0 ]; then
  record azure-contained-resources PASS "contained resource count=0"
else
  record azure-contained-resources FAIL "contained resource count=$azure_contained_count"
fi

oci_profile="$(extract_tfvars_string "$tfvars_path" oci_profile)"
oci_region="$(extract_tfvars_string "$tfvars_path" oci_region)"
oci_compartment_id="$(extract_tfvars_string "$tfvars_path" oci_compartment_id)"
oci_profile="${oci_profile:-DEFAULT}"
oci --profile "$oci_profile" --region "$oci_region" compute instance list \
  --compartment-id "$oci_compartment_id" --all \
  >"$inventory_evidence/oci-instances.json"
# OCI CLI 3.84 emits an empty stdout stream (with exit 0) for an empty list.
# Normalize that successful empty response so jq can evaluate the zero inventory.
if [ ! -s "$inventory_evidence/oci-instances.json" ]; then
  printf '{"data":[]}\n' >"$inventory_evidence/oci-instances.json"
fi
oci_active="$(
  jq --arg runId "$run_id" \
    '[.data[] | select(."freeform-tags".RouterdRunId == $runId and ."lifecycle-state" != "TERMINATED")] | length' \
    "$inventory_evidence/oci-instances.json"
)"
if [ "$oci_active" -eq 0 ]; then
  record oci-run-resources PASS "non-terminated run-tagged instances=0"
else
  record oci-run-resources FAIL "non-terminated run-tagged instances=$oci_active"
fi

oci_tagged="$inventory_evidence/oci-tagged-resources.json"
oci_pages_dir="$inventory_evidence/oci-tagged-resource-pages"
mkdir -p "$oci_pages_dir"
seen_tokens="$oci_pages_dir/seen-next-page-tokens.txt"
: >"$seen_tokens"
page_token=
page_number=1
while :; do
  page="$oci_pages_dir/page-${page_number}.json"
  oci_args=(--profile "$oci_profile" --region "$oci_region" search resource structured-search
    --query-text "query all resources where (freeformTags.key = 'RouterdRunId' && freeformTags.value = '$run_id')")
  if [ -n "$page_token" ]; then
    oci_args+=(--page "$page_token")
  fi
  oci "${oci_args[@]}" >"$page"
  jq -e '
    (.data | type == "object") and (.data.items | type == "array") and
    ((has("opc-next-page") | not) or (."opc-next-page" == null) or (."opc-next-page" | type == "string")) and
    (has("next-page") | not)
  ' "$page" >/dev/null || die "OCI tagged-resource inventory page is malformed or has ambiguous pagination metadata"
  next_token="$(jq -r '."opc-next-page" // empty' "$page")"
  if [ -z "$next_token" ]; then
    break
  fi
  if grep -Fqx -- "$next_token" "$seen_tokens"; then
    die "OCI tagged-resource inventory repeated a pagination token"
  fi
  printf '%s\n' "$next_token" >>"$seen_tokens"
  page_token="$next_token"
  page_number=$((page_number + 1))
done
jq -s '{data:{items:[.[].data.items[]]}, pagination:{status:"complete", pages:length}}' \
  "$oci_pages_dir"/page-*.json >"$oci_tagged"
oci_tagged_count="$(jq '[.data.items[]] | length' "$oci_tagged")"
if [ "$oci_tagged_count" -eq 0 ]; then
  record oci-tagged-resources PASS "all paginated run-tagged resources=0"
else
  record oci-tagged-resources FAIL "run-tagged resource count=$oci_tagged_count"
fi

pve_host="$pve_ssh_host"
pve_vmids="$(jq -c '.pve.vmids' "$contract_path")"
# One authoritative cluster query distinguishes absence from SSH/auth/API
# failure. Any nonzero SSH status or malformed result aborts inventory.
ssh -n -i "$pve_ssh_private_key" -o BatchMode=yes -o ConnectTimeout=10 "root@$pve_host" \
  "pvesh get /cluster/resources --type vm --output-format json" \
  >"$inventory_evidence/pve-cluster-vms.json"
jq -e 'type == "array"' "$inventory_evidence/pve-cluster-vms.json" >/dev/null ||
  die "PVE cluster VM inventory is invalid"
pve_found="$(jq --argjson vmids "$pve_vmids" \
  '[.[] | select((.vmid as $id | $vmids | index($id)) != null)] | length' \
  "$inventory_evidence/pve-cluster-vms.json")"
if [ "$pve_found" -eq 0 ]; then
  record pve-run-vms PASS "existing exact VMIDs=0"
else
  record pve-run-vms FAIL "existing exact VMIDs=$pve_found"
fi

pve_bridge="$(jq -er '.pve.captureBridge' "$contract_path")"
ssh -n -i "$pve_ssh_private_key" -o BatchMode=yes -o ConnectTimeout=10 "root@$pve_host" \
  "pvesh get /nodes/$(printf '%q' "$pve_node")/network --output-format json" \
  >"$inventory_evidence/pve-network.json"
pve_bridge_count="$(jq --arg bridge "$pve_bridge" '[.[] | select(.iface == $bridge)] | length' "$inventory_evidence/pve-network.json")"
if [ "$pve_bridge_count" -eq 0 ]; then
  record pve-bridges PASS "exact capture bridge absent"
else
  record pve-bridges FAIL "exact capture bridge count=$pve_bridge_count"
fi

jq -n \
  --argjson state "$state_count" \
  --argjson aws "$aws_tagged_count" \
  --argjson azureGroup "$([ "$azure_exists" = false ] && echo 0 || echo 1)" \
  --argjson azureContained "$azure_contained_count" \
  --argjson oci "$oci_tagged_count" \
  --argjson pveVms "$pve_found" \
  --argjson pveBridges "$pve_bridge_count" \
  '{scopes:[
    {name:"tofu-state",count:$state,queryStatus:"complete"},
    {name:"aws-tagged-resources",count:$aws,queryStatus:"complete"},
    {name:"azure-resource-group",count:$azureGroup,queryStatus:"complete"},
    {name:"azure-contained-resources",count:$azureContained,queryStatus:"complete"},
    {name:"oci-tagged-resources",count:$oci,queryStatus:"complete"},
    {name:"pve-vms",count:$pveVms,queryStatus:"complete"},
    {name:"pve-bridges",count:$pveBridges,queryStatus:"complete"}
  ]}' >"$inventory_evidence/inventory.json"
python3 "$framework_root/qa_guard.py" inventory \
  --inventory-json "$inventory_evidence/inventory.json"

cat "$summary"
exit "$status"
