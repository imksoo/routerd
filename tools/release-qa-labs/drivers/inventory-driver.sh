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
require_command python3
resource_classifier="$framework_root/inventory_resources.py"

inventory_tofu() {
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
  jq -n --argjson state_count "$state_count" '{scopes:[{name:"tofu-state",count:$state_count,queryStatus:"complete"}]}' >"$inventory_evidence/tofu-scopes.json"
}

inventory_aws() {
  aws_region="$(extract_tfvars_string "$tfvars_path" aws_region)"
  aws_profile="$(extract_tfvars_string "$tfvars_path" aws_profile)"
  aws_profile="${aws_profile:-default}"
  aws_active="$inventory_evidence/aws-active-instances.json"
  aws --profile "$aws_profile" ec2 describe-instances \
    --region "$aws_region" \
    --filters "Name=tag:routerd-run-id,Values=$run_id" \
    "Name=instance-state-name,Values=pending,running,shutting-down,stopping,stopped" \
    --output json >"$aws_active" 2>"$inventory_evidence/aws-active-instances.stderr"

  aws_tagged="$inventory_evidence/aws-tagged-resources.json"
  aws resourcegroupstaggingapi get-resources \
    --profile "$aws_profile" --region "$aws_region" \
    --tag-filters "Key=routerd-run-id,Values=$run_id" \
    --output json >"$aws_tagged" 2>"$inventory_evidence/aws-tagged-resources.stderr"
  # The tagging index can retain terminated instances. Absence from the active
  # query is not proof: look up every exact tagged instance ID and validate its
  # account, region, run tag and lifecycle before excluding a tombstone.
  python3 "$resource_classifier" aws-ids --tagged "$aws_tagged" \
    --run-id "$run_id" --region "$aws_region" >"$inventory_evidence/aws-tagged-instance-ids.json"
  mapfile -t aws_instance_ids < <(jq -r '.[]' "$inventory_evidence/aws-tagged-instance-ids.json")
  aws_lookup="$inventory_evidence/aws-tagged-instance-states.json"
  if [ "${#aws_instance_ids[@]}" -gt 0 ]; then
    aws --profile "$aws_profile" ec2 describe-instances --region "$aws_region" \
      --instance-ids "${aws_instance_ids[@]}" --output json \
      >"$aws_lookup" 2>"$inventory_evidence/aws-tagged-instance-states.stderr"
  else
    printf '{"Reservations":[]}\n' >"$aws_lookup"
  fi
  python3 "$resource_classifier" aws-counts --tagged "$aws_tagged" \
    --lookup "$aws_lookup" --active "$aws_active" --run-id "$run_id" --region "$aws_region" \
    >"$inventory_evidence/aws-resource-counts.json"
  aws_count="$(jq '.activeInstanceCount' "$inventory_evidence/aws-resource-counts.json")"
  aws_tagged_count="$(jq '.count' "$inventory_evidence/aws-resource-counts.json")"
  if [ "$aws_count" -eq 0 ]; then
    record aws-run-resources PASS "active instance count=0"
  else
    record aws-run-resources FAIL "active instance count=$aws_count"
  fi
  if [ "$aws_tagged_count" -eq 0 ]; then
    record aws-tagged-resources PASS "actionable run residue=0; raw and confirmed terminated counts retained"
  else
    record aws-tagged-resources FAIL "actionable run residue=$aws_tagged_count"
  fi
  jq -n --argjson aws_tagged_count "$aws_tagged_count" '{scopes:[{name:"aws-tagged-resources",count:$aws_tagged_count,queryStatus:"complete"}]}' >"$inventory_evidence/aws-scopes.json"
}

inventory_azure() {
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
  azure_group_count=1
  [ "$azure_exists" != false ] || azure_group_count=0

  jq -n --argjson azure_group_count "$azure_group_count" --argjson azure_contained_count "$azure_contained_count" '{scopes:[{name:"azure-resource-group",count:$azure_group_count,queryStatus:"complete"},{name:"azure-contained-resources",count:$azure_contained_count,queryStatus:"complete"}]}' >"$inventory_evidence/azure-scopes.json"
}

inventory_oci() {
  oci_profile="$(extract_tfvars_string "$tfvars_path" oci_profile)"
  oci_region="$(extract_tfvars_string "$tfvars_path" oci_region)"
  oci_compartment_id="$(extract_tfvars_string "$tfvars_path" oci_compartment_id)"
  oci_profile="${oci_profile:-DEFAULT}"
  oci --profile "$oci_profile" --region "$oci_region" compute instance list \
    --compartment-id "$oci_compartment_id" --all \
    >"$inventory_evidence/oci-instances.raw.json" 2>"$inventory_evidence/oci-instances.stderr"
  cp "$inventory_evidence/oci-instances.raw.json" "$inventory_evidence/oci-instances.json"
  # OCI CLI 3.84 emits an empty stdout stream (with exit 0) for an empty list.
  # Normalize that successful empty response so jq can evaluate the zero inventory.
  if [ ! -s "$inventory_evidence/oci-instances.json" ]; then
    printf '{"data":[]}\n' >"$inventory_evidence/oci-instances.json"
  fi
  oci_tagged="$inventory_evidence/oci-tagged-resources.json"
  oci_pages_dir="$inventory_evidence/oci-tagged-resource-pages"
  mkdir -p "$oci_pages_dir"
  # A later retry may have fewer pages. Retain previous raw pages, but aggregate
  # only this query's explicit list; never glob an earlier attempt into the result.
  oci_query_dir="$(mktemp -d "$oci_pages_dir/query.XXXXXXXX")"
  oci_pages=()
  seen_tokens="$oci_query_dir/seen-next-page-tokens.txt"
  : >"$seen_tokens"
  page_token=
  page_number=1
  while :; do
    page="$oci_query_dir/page-${page_number}.json"
    oci_args=(--profile "$oci_profile" --region "$oci_region" search resource structured-search
      --query-text "query all resources where (freeformTags.key = 'RouterdRunId' && freeformTags.value = '$run_id')")
    if [ -n "$page_token" ]; then
      oci_args+=(--page "$page_token")
    fi
    oci "${oci_args[@]}" >"$page" 2>"$oci_query_dir/page-${page_number}.stderr"
    python3 "$resource_classifier" oci-page "$page"
    oci_pages+=("$page")
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
    "${oci_pages[@]}" >"$oci_tagged"
  python3 "$resource_classifier" oci-counts --tagged "$oci_tagged" \
    --compute "$inventory_evidence/oci-instances.json" --run-id "$run_id" \
    --compartment "$oci_compartment_id" >"$inventory_evidence/oci-resource-counts.json"
  oci_active="$(jq '.activeInstanceCount' "$inventory_evidence/oci-resource-counts.json")"
  oci_tagged_count="$(jq '.count' "$inventory_evidence/oci-resource-counts.json")"
  if [ "$oci_active" -eq 0 ]; then
    record oci-run-resources PASS "non-terminated run-tagged instances=0"
  else
    record oci-run-resources FAIL "non-terminated run-tagged instances=$oci_active"
  fi
  if [ "$oci_tagged_count" -eq 0 ]; then
    record oci-tagged-resources PASS "actionable run residue=0; raw and confirmed terminated counts retained"
  else
    record oci-tagged-resources FAIL "actionable run residue=$oci_tagged_count"
  fi
  jq -n --argjson oci_tagged_count "$oci_tagged_count" '{scopes:[{name:"oci-tagged-resources",count:$oci_tagged_count,queryStatus:"complete"}]}' >"$inventory_evidence/oci-scopes.json"
}

inventory_pve() {
  pve_host="$pve_ssh_host"
  # The disposable qnap template stage is a real PVE VM/template resource and
  # must be zeroed with the five workload VMs before cleanup can claim success.
  pve_vmids="$(jq -ec '[.pve.templateStage.vmid] + [.pve.vmids[]] | unique' "$contract_path")"
  pve_ssh=(ssh -n -i "$pve_ssh_private_key" -o BatchMode=yes -o StrictHostKeyChecking=yes \
    -o UserKnownHostsFile="$pve_ssh_known_hosts" -o GlobalKnownHostsFile=/dev/null \
    -o CanonicalizeHostname=no -o IdentitiesOnly=yes -o PasswordAuthentication=no \
    -o KbdInteractiveAuthentication=no -o ConnectTimeout=10)
  # One authoritative cluster query distinguishes absence from SSH/auth/API
  # failure. Any nonzero SSH status or malformed result aborts inventory.
  "${pve_ssh[@]}" "root@$pve_host" \
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
  "${pve_ssh[@]}" "root@$pve_host" \
    "pvesh get /nodes/$(printf '%q' "$pve_node")/network --output-format json" \
    >"$inventory_evidence/pve-network.json"
  jq -e 'type == "array"' "$inventory_evidence/pve-network.json" >/dev/null ||
    die "PVE persistent network inventory is invalid"
  pve_persistent_bridge_count="$(jq --arg bridge "$pve_bridge" '[.[] | select(.iface == $bridge)] | length' "$inventory_evidence/pve-network.json")"
  "${pve_ssh[@]}" "root@$pve_host" "ip -j -d link show" \
    >"$inventory_evidence/pve-live-links.json"
  jq -e 'type == "array"' "$inventory_evidence/pve-live-links.json" >/dev/null ||
    die "PVE live link inventory is invalid"
  pve_live_bridge_count="$(jq --arg bridge "$pve_bridge" '[.[] | select(.ifname == $bridge)] | length' "$inventory_evidence/pve-live-links.json")"
  # Both inventories may describe the same bridge; this scope counts the exact
  # run-scoped target as occupied once while retaining both raw counts in its
  # diagnostic for cleanup forensics.
  pve_bridge_count=$((pve_persistent_bridge_count > 0 || pve_live_bridge_count > 0 ? 1 : 0))
  if [ "$pve_bridge_count" -eq 0 ]; then
    record pve-bridges PASS "exact capture bridge absent from persistent config and live links"
  else
    record pve-bridges FAIL "exact capture bridge persistent=$pve_persistent_bridge_count live=$pve_live_bridge_count"
  fi
  jq -n --argjson pve_found "$pve_found" --argjson pve_bridge_count "$pve_bridge_count" '{scopes:[{name:"pve-vms",count:$pve_found,queryStatus:"complete"},{name:"pve-bridges",count:$pve_bridge_count,queryStatus:"complete"}]}' >"$inventory_evidence/pve-scopes.json"
}

# Each provider is an independent read-only query. Keep raw attempts and do
# not let one failed identity/transport check skip the remaining inventories.
# Lifecycle-only disagreement can reobserve this provider twice; it cannot
# trigger another destroy or restart the enclosing inventory deadline.
query_provider() {
  local provider="$1" names="$2" attempt attempt_dir query_rc pending_file
  jq -n --arg names "$names" '{scopes:($names | split(" ") | map({name:.,count:null,queryStatus:"incomplete"}))}' \
    >"$inventory_evidence/$provider-scopes.json"
  mkdir -p "$inventory_evidence/$provider-attempts"
  for attempt in 1 2 3; do
    attempt_dir="$(mktemp -d "$inventory_evidence/$provider-attempts/query.XXXXXXXX")"
    # Do not invoke this subshell in an if/|| condition: Bash would disable
    # errexit inside every command of the provider function.
    set +e
    (
      set -e
      inventory_evidence="$attempt_dir"
      status=0
      "inventory_$provider"
    )
    query_rc=$?
    set -e
    cp -a "$attempt_dir/." "$inventory_evidence/"
    printf '%s\t%s\t%s\n' "$provider" "$attempt" "$query_rc" >>"$inventory_evidence/query-attempts.tsv"
    [ "$query_rc" -ne 0 ] || return 0
    pending_file="$attempt_dir/$provider-resource-counts.json"
    if [ "$query_rc" -eq 3 ] && [ "$attempt" -lt 3 ] &&
       [ -f "$pending_file" ] && jq -e '.retryable == true and .classification == "observation_inconclusive"' "$pending_file" >/dev/null; then
      sleep 2
      continue
    fi
    record "$provider-query" FAIL "query incomplete; exit=$query_rc; attempts=$attempt"
    return 0
  done
}

: >"$inventory_evidence/query-attempts.tsv"
query_provider tofu 'tofu-state'
query_provider aws 'aws-tagged-resources'
query_provider azure 'azure-resource-group azure-contained-resources'
query_provider oci 'oci-tagged-resources'
query_provider pve 'pve-vms pve-bridges'
jq -s '{scopes:([.[].scopes[]])}' \
  "$inventory_evidence/tofu-scopes.json" "$inventory_evidence/aws-scopes.json" \
  "$inventory_evidence/azure-scopes.json" "$inventory_evidence/oci-scopes.json" \
  "$inventory_evidence/pve-scopes.json" >"$inventory_evidence/inventory.json"
python3 "$framework_root/qa_guard.py" inventory \
  --inventory-json "$inventory_evidence/inventory.json" || status=1

cat "$summary"
exit "$status"
