#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/sam-post-destroy-inventory.sh --tofu-output tofu-output.json --evidence-dir DIR

Collects post-destroy provider/PVE inventory from the IDs recorded in
`tofu output -json`. A nonzero exit means a provider command still showed a
non-terminated instance, an existing Azure resource group, or an existing PVE
VMID. Exit 3 means incomplete input/acquisition. Ambiguous absence/auth is
not cleanup proof. Raw stdout, stderr and exits are retained.
USAGE
}

tofu_output=
evidence_dir=

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tofu-output) tofu_output="$2"; shift 2 ;;
    --evidence-dir) evidence_dir="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$tofu_output" ] || { echo "--tofu-output is required" >&2; exit 2; }
[ -n "$evidence_dir" ] || { echo "--evidence-dir is required" >&2; exit 2; }
[ -f "$tofu_output" ] || { echo "tofu output not found: $tofu_output" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }

# Claim a fresh attempt directory before writing any evidence/querying providers.
# Existing directories are never overwritten, including partially failed attempts.
if ! mkdir "$evidence_dir"; then
  echo "INCONCLUSIVE: evidence directory already exists or cannot be created: $evidence_dir" >&2
  exit 3
fi
nodes_json="$evidence_dir/nodes.json"
fabric_json="$evidence_dir/fabric.json"
jq -e '.nodes.value | select(type=="object" and length>0)' "$tofu_output" >"$nodes_json" || exit 3
jq -e '.fabric.value | select(type=="object")' "$tofu_output" >"$fabric_json" || exit 3

# Validate all recorded identities before any provider query.
jq -e 'to_entries | all(.[]; (.key|test("^[A-Za-z0-9_.-]+$")) and
  (if .value.site=="aws" then (.value.instance_id|type=="string") and (.value.instance_id|test("^i-[0-9a-f]+$"))
   elif .value.site=="oci" then (.value.instance_id|type=="string") and (.value.instance_id|startswith("ocid1.instance."))
   elif .value.site=="pve" then (.value.vm_id|tostring|test("^[1-9][0-9]*$"))
   else true end))' "$nodes_json" >/dev/null || exit 3
jq -e 'any(.[]; .site=="aws" or .site=="azure" or .site=="oci" or .site=="pve")' "$nodes_json" >/dev/null || {
  echo "INCONCLUSIVE: no supported recorded provider scope" >&2; exit 3;
}
applicable() { jq -e --arg site "$1" 'any(.[]; .site==$site)' "$nodes_json" >/dev/null; }
query() {
  local out=$1 rc=0; shift
  "$@" >"$out" 2>"$out.stderr" || rc=$?
  printf '%s\n' "$rc" >"$out.exit"
  return "$rc"
}
status=0
summary="$evidence_dir/summary.tsv"
printf 'provider\tcheck\tresult\tdetail\n' >"$summary"

record() {
  local provider="$1" check="$2" result="$3" detail="$4"
  printf '%s\t%s\t%s\t%s\n' "$provider" "$check" "$result" "$detail" >>"$summary"
  if [ "$result" = "FAIL" ]; then
    status=1
  elif [ "$result" = "INCONCLUSIVE" ] && [ "$status" = 0 ]; then
    status=3
  fi
}

aws_inventory() {
  applicable aws || { record aws instances NOT_APPLICABLE "no recorded AWS nodes"; return; }
  command -v aws >/dev/null || { record aws cli INCONCLUSIVE "AWS CLI unavailable"; return; }
  local region node id out state
  region=$(jq -r '.aws.region // empty' "$fabric_json")
  [ -n "$region" ] || { record aws region INCONCLUSIVE "missing AWS region"; return; }
  # A batch NotFound cannot establish absence of all other IDs.
  while read -r node id; do
    out="$evidence_dir/aws-$node.json"
    if query "$out" aws ec2 describe-instances --region "$region" --instance-ids "$id"; then
      if ! state=$(jq -er --arg id "$id" '[.Reservations[].Instances[]|select(.InstanceId==$id)] |
        if length==1 then .[0].State.Name else error("missing/duplicate identity") end' "$out"); then
        record aws "$node" INCONCLUSIVE "malformed/missing identity"; continue
      fi
      case "$state" in
        terminated) record aws "$node" PASS "recorded instance terminated" ;;
        pending|running|shutting-down|stopping|stopped) record aws "$node" FAIL "remaining instance state=$state" ;;
        *) record aws "$node" INCONCLUSIVE "unknown state=$state" ;;
      esac
    elif grep -Eq 'InvalidInstanceID\.NotFound' "$out.stderr"; then
      record aws "$node" PASS "single recorded ID explicitly NotFound"
    else
      record aws "$node" INCONCLUSIVE "query failed; see stdout/stderr/exit"
    fi
  done < <(jq -r 'to_entries[]|select(.value.site=="aws")|[.key,.value.instance_id]|@tsv' "$nodes_json")
}

azure_inventory() {
  applicable azure || { record azure scope NOT_APPLICABLE "no recorded azure nodes"; return; }
  command -v az >/dev/null 2>&1 || {
    record azure cli INCONCLUSIVE "az CLI not found"
    return 0
  }
  local rg exists
  rg="$(jq -r '.azure.resource_group_name // empty' "$fabric_json")"
  [ -n "$rg" ] || {
    record azure resource_group INCONCLUSIVE "missing fabric.azure.resource_group_name"
    return 0
  }
  if ! query "$evidence_dir/azure-group-exists.txt" az group exists --name "$rg"; then
    record azure resource_group INCONCLUSIVE "existence query failed"; return
  fi
  exists="$(tr -d '\r\n' <"$evidence_dir/azure-group-exists.txt")"
  if [ "$exists" = "false" ]; then
    record azure resource_group PASS "$rg deleted"
  elif [ "$exists" = "true" ]; then
    query "$evidence_dir/azure-resources.json" az resource list --resource-group "$rg" --output json || true
    record azure resource_group FAIL "$rg still exists"
  else
    record azure resource_group INCONCLUSIVE "could not determine group existence"
  fi
}

oci_inventory() {
  applicable oci || { record oci scope NOT_APPLICABLE "no recorded oci nodes"; return; }
  command -v oci >/dev/null 2>&1 || {
    record oci cli INCONCLUSIVE "oci CLI not found"
    return 0
  }
  local region node id out state active_count=0 checked=0 unknown_count=0
  region="$(jq -r '.oci.region // empty' "$fabric_json")"
  [ -n "$region" ] || {
    record oci region INCONCLUSIVE "missing fabric.oci.region"
    return 0
  }
  while read -r node id; do
    [ -n "$id" ] || continue
    checked=$((checked + 1))
    out="$evidence_dir/oci-instance-${node}.json"
    if query "$out" oci compute instance get --region "$region" --instance-id "$id"; then
      if ! state="$(jq -er --arg id "$id" 'select(.data.id==$id)|.data."lifecycle-state"' "$out")"; then
        unknown_count=$((unknown_count + 1)); continue
      fi
      case "$state" in
        TERMINATED) ;;
        PROVISIONING|RUNNING|STARTING|STOPPING|STOPPED|CREATING_IMAGE|MOVING|TERMINATING)
          active_count=$((active_count + 1)) ;;
        *) unknown_count=$((unknown_count + 1)) ;;
      esac
    else
      # NotAuthorizedOrNotFound/404 cannot distinguish absence from auth loss.
      unknown_count=$((unknown_count + 1))
    fi
  done < <(jq -r 'to_entries[] | select(.value.site == "oci") | [.key, (.value.instance_id // "")] | @tsv' "$nodes_json")
  if [ "$checked" -eq 0 ]; then
    record oci instances INCONCLUSIVE "no oci instances in tofu output"
  elif [ "$active_count" -gt 0 ]; then
    record oci instances FAIL "non-terminated instances=$active_count"
  elif [ "$unknown_count" -gt 0 ]; then
    record oci instances INCONCLUSIVE "could not verify instances=$unknown_count"
  else
    record oci instances PASS "all recorded OCI instances observed terminated"
  fi
}

pve_inventory() {
  applicable pve || { record pve scope NOT_APPLICABLE "no recorded pve nodes"; return; }
  local node id found=0 checked=0 unknown_count=0
  command -v qm >/dev/null 2>&1 || {
    record pve qm INCONCLUSIVE "qm command not found"
    return 0
  }
  while read -r node id; do
    [ -n "$id" ] || continue
    checked=$((checked + 1))
    if query "$evidence_dir/pve-$node-$id.txt" qm config "$id"; then
      found=$((found + 1))
    elif grep -Eq "Configuration file .*qemu-server/${id}[.]conf.*does not exist" "$evidence_dir/pve-$node-$id.txt.stderr"; then
      true
    else
      unknown_count=$((unknown_count + 1))
    fi
  done < <(jq -r 'to_entries[] | select(.value.site == "pve") | [.key, (.value.vm_id // "")] | @tsv' "$nodes_json")
  if [ "$checked" -eq 0 ]; then
    record pve vms INCONCLUSIVE "no pve vm ids in tofu output"
  elif [ "$found" -gt 0 ]; then
    record pve vms FAIL "existing VMIDs=$found"
  elif [ "$unknown_count" -gt 0 ]; then
    record pve vms INCONCLUSIVE "VMID acquisition failed=$unknown_count"
  else
    record pve vms PASS "all recorded PVE configs explicitly absent"
  fi
}

aws_inventory
azure_inventory
oci_inventory
pve_inventory

column -t -s $'\t' "$summary" 2>/dev/null || cat "$summary"
exit "$status"
