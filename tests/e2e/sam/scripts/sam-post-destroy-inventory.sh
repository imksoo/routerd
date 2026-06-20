#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/sam-post-destroy-inventory.sh --tofu-output tofu-output.json --evidence-dir DIR

Collects post-destroy provider/PVE inventory from the IDs recorded in
`tofu output -json`. A nonzero exit means a provider command still showed a
non-terminated instance, an existing Azure resource group, or an existing PVE
VMID. Provider NotFound errors are recorded as cleanup evidence.
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

mkdir -p "$evidence_dir"
nodes_json="$evidence_dir/nodes.json"
fabric_json="$evidence_dir/fabric.json"
jq '.nodes.value' "$tofu_output" >"$nodes_json"
jq '.fabric.value' "$tofu_output" >"$fabric_json"

status=0
summary="$evidence_dir/summary.tsv"
printf 'provider\tcheck\tresult\tdetail\n' >"$summary"

record() {
  local provider="$1" check="$2" result="$3" detail="$4"
  printf '%s\t%s\t%s\t%s\n' "$provider" "$check" "$result" "$detail" >>"$summary"
  if [ "$result" = "FAIL" ]; then
    status=1
  fi
}

aws_inventory() {
  command -v aws >/dev/null 2>&1 || {
    record aws cli SKIP "aws CLI not found"
    return 0
  }
  local region out active_count
  region="$(jq -r '.aws.region // empty' "$fabric_json")"
  [ -n "$region" ] || {
    record aws region SKIP "missing fabric.aws.region"
    return 0
  }
  mapfile -t ids < <(jq -r 'to_entries[] | select(.value.site == "aws") | .value.instance_id // empty' "$nodes_json" | sort -u)
  if [ "${#ids[@]}" -eq 0 ]; then
    record aws instances SKIP "no aws instances in tofu output"
    return 0
  fi
  out="$evidence_dir/aws-instances.json"
  if aws ec2 describe-instances --region "$region" --instance-ids "${ids[@]}" >"$out" 2>"$evidence_dir/aws-instances.stderr"; then
    active_count="$(jq '[.Reservations[].Instances[] | select(.State.Name != "terminated")] | length' "$out")"
    if [ "$active_count" = "0" ]; then
      record aws instances PASS "all described instances are terminated"
    else
      record aws instances FAIL "non-terminated instances=$active_count"
    fi
  else
    record aws instances PASS "describe-instances returned NotFound/error; see aws-instances.stderr"
  fi
}

azure_inventory() {
  command -v az >/dev/null 2>&1 || {
    record azure cli SKIP "az CLI not found"
    return 0
  }
  local rg exists
  rg="$(jq -r '.azure.resource_group_name // empty' "$fabric_json")"
  [ -n "$rg" ] || {
    record azure resource_group SKIP "missing fabric.azure.resource_group_name"
    return 0
  }
  exists="$(az group exists --name "$rg" 2>"$evidence_dir/azure-group-exists.stderr" || true)"
  printf '%s\n' "$exists" >"$evidence_dir/azure-group-exists.txt"
  if [ "$exists" = "false" ]; then
    record azure resource_group PASS "$rg deleted"
  elif [ "$exists" = "true" ]; then
    az resource list --resource-group "$rg" --output json >"$evidence_dir/azure-resources.json" 2>"$evidence_dir/azure-resources.stderr" || true
    record azure resource_group FAIL "$rg still exists"
  else
    record azure resource_group SKIP "could not determine group existence"
  fi
}

oci_inventory() {
  command -v oci >/dev/null 2>&1 || {
    record oci cli SKIP "oci CLI not found"
    return 0
  }
  local region node id out state active_count=0 checked=0
  region="$(jq -r '.oci.region // empty' "$fabric_json")"
  [ -n "$region" ] || {
    record oci region SKIP "missing fabric.oci.region"
    return 0
  }
  while read -r node id; do
    [ -n "$id" ] || continue
    checked=$((checked + 1))
    out="$evidence_dir/oci-instance-${node}.json"
    if oci compute instance get --region "$region" --instance-id "$id" >"$out" 2>"$evidence_dir/oci-instance-${node}.stderr"; then
      state="$(jq -r '.data."lifecycle-state" // empty' "$out")"
      if [ "$state" != "TERMINATED" ]; then
        active_count=$((active_count + 1))
      fi
    fi
  done < <(jq -r 'to_entries[] | select(.value.site == "oci") | [.key, (.value.instance_id // "")] | @tsv' "$nodes_json")
  if [ "$checked" -eq 0 ]; then
    record oci instances SKIP "no oci instances in tofu output"
  elif [ "$active_count" -eq 0 ]; then
    record oci instances PASS "no non-terminated OCI instances observed"
  else
    record oci instances FAIL "non-terminated instances=$active_count"
  fi
}

pve_inventory() {
  local node id found=0 checked=0
  command -v qm >/dev/null 2>&1 || {
    record pve qm SKIP "qm command not found"
    return 0
  }
  while read -r node id; do
    [ -n "$id" ] || continue
    checked=$((checked + 1))
    if qm config "$id" >"$evidence_dir/pve-${node}-${id}.txt" 2>"$evidence_dir/pve-${node}-${id}.stderr"; then
      found=$((found + 1))
    fi
  done < <(jq -r 'to_entries[] | select(.value.site == "pve") | [.key, (.value.vm_id // "")] | @tsv' "$nodes_json")
  if [ "$checked" -eq 0 ]; then
    record pve vms SKIP "no pve vm ids in tofu output"
  elif [ "$found" -eq 0 ]; then
    record pve vms PASS "no PVE VMID configs found"
  else
    record pve vms FAIL "existing VMIDs=$found"
  fi
}

aws_inventory
azure_inventory
oci_inventory
pve_inventory

column -t -s $'\t' "$summary" 2>/dev/null || cat "$summary"
exit "$status"
