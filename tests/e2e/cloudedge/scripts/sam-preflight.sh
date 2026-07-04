#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/sam-preflight.sh --tfvars terraform.tfvars [--artifact routerd.tar.gz] [--evidence-dir DIR]

Checks provider prerequisites that should be verified before OpenTofu apply.
This gates local naming and artifact assumptions before cloud resources are
created, and verifies OCI compartment selection so resources are not applied
to ManagedCompartmentForPaaS by mistake.
USAGE
}

tfvars=
artifact=
evidence_dir=

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tfvars) tfvars="$2"; shift 2 ;;
    --artifact) artifact="$2"; shift 2 ;;
    --evidence-dir) evidence_dir="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$tfvars" ] || { echo "--tfvars is required" >&2; exit 2; }
[ -f "$tfvars" ] || { echo "tfvars not found: $tfvars" >&2; exit 2; }
[ -z "$artifact" ] || [ -f "$artifact" ] || { echo "artifact not found: $artifact" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }

extract_tfvars_string() {
  local key="$1"
  awk -v key="$key" '
    $0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
      sub(/#.*/, "")
      sub("^[^=]*=", "")
      gsub(/^[[:space:]]+|[[:space:]]+$/, "")
      if ($0 ~ /^".*"$/) {
        sub(/^"/, "")
        sub(/"$/, "")
      }
      print
      exit
    }
  ' "$tfvars"
}

check_artifact_binary() {
  [ -n "$artifact" ] || return 0
  local tmp root name path status=0
  tmp="$(mktemp -d "${TMPDIR:-/tmp}/routerd-sam-preflight-artifact.XXXXXX")"
  trap 'rm -rf "$tmp"' RETURN
  tar -xzf "$artifact" -C "$tmp"
  root="$tmp"
  if [ -n "$out_dir" ]; then
    {
      echo "artifact=$artifact"
      sha256sum "$artifact"
    } >"$out_dir/artifact-binaries.txt"
  fi
  for name in routerd routerctl; do
    path="$(find "$root" -type f -name "$name" -perm -100 | sort | head -n 1)"
    if [ -z "$path" ]; then
      echo "FAIL: $name not found as owner-executable in artifact $artifact" >&2
      status=1
      if [ -n "$out_dir" ]; then
        {
          echo "$name=missing-owner-executable"
          find "$root" -type f -name "$name" -printf '%m %p\n' | sort || true
        } >>"$out_dir/artifact-binaries.txt"
      fi
      continue
    fi
    echo "artifact_binary_$name=$path"
    if [ -n "$out_dir" ]; then
      {
        echo "$name=$path"
        stat -c "$name mode=%a owner=%U group=%G path=%n" "$path"
        "$path" version || true
      } >>"$out_dir/artifact-binaries.txt" 2>&1
    fi
  done
  trap - RETURN
  rm -rf "$tmp"
  return "$status"
}

check_run_id_name_budget() {
  local run_id="$1"
  local status=0
  local name len limit resource
  [ -n "$run_id" ] || { echo "run_id missing in $tfvars" >&2; return 1; }

  # Derived from the current CloudEdge OpenTofu modules by grepping every
  # var.run_id interpolation. There is no storage-account style run_id-derived
  # name. The tightest enforced provider limit is Azure Virtual Network:
  # vnet-routerd-${run_id}-azure, max 64 chars, fixed overhead 19, so run_id
  # budget is 45 chars. The other checked Azure/AWS names have wider budgets.
  # If the lab-side OpenTofu naming patterns change, update this copied list in
  # the same review so preflight does not silently validate stale names.
  local checks=(
    "azure resource group|90|rg-routerd-${run_id}-azure"
    "azure virtual network|64|vnet-routerd-${run_id}-azure"
    "azure subnet|80|snet-routerd-${run_id}-azure-leaf"
    "azure network security group|80|nsg-routerd-${run_id}-azure"
    "azure route table|80|rt-routerd-${run_id}-azure-leaf"
    "aws iam role|64|routerd-sam-e2e-${run_id}"
    "aws iam instance profile|128|routerd-sam-e2e-${run_id}"
    "aws iam policy|128|routerd-sam-e2e-capture-${run_id}"
  )

  if [ -n "$out_dir" ]; then
    {
      echo "tfvars=$tfvars"
      echo "run_id=$run_id"
      echo "run_id_length=${#run_id}"
      echo "tightest_budget=45"
      echo "tightest_resource=azure virtual network"
      echo "resource	limit	length	name"
    } >"$out_dir/name-budget.tsv"
  fi

  for entry in "${checks[@]}"; do
    IFS='|' read -r resource limit name <<<"$entry"
    len="${#name}"
    if [ -n "$out_dir" ]; then
      printf '%s\t%s\t%s\t%s\n' "$resource" "$limit" "$len" "$name" >>"$out_dir/name-budget.tsv"
    fi
    if [ "$len" -gt "$limit" ]; then
      echo "FAIL: $resource derived name is $len chars, limit $limit: $name" >&2
      status=1
    fi
  done
  if [ "$status" -eq 0 ]; then
    echo "PASS: run_id name budget fits provider limits (run_id=${run_id}, length=${#run_id})"
  fi
  return "$status"
}

oci_profile="$(extract_tfvars_string oci_profile)"
oci_region="$(extract_tfvars_string oci_region)"
oci_compartment_id="$(extract_tfvars_string oci_compartment_id)"
run_id="$(extract_tfvars_string run_id)"

[ -n "$oci_profile" ] || oci_profile=DEFAULT
[ -n "$oci_region" ] || oci_region=ap-tokyo-1
[ -n "$oci_compartment_id" ] || { echo "oci_compartment_id missing in $tfvars" >&2; exit 1; }

out_dir="${evidence_dir:-}"
if [ -n "$out_dir" ]; then
  mkdir -p "$out_dir"
fi

check_artifact_binary
check_run_id_name_budget "$run_id"

command -v oci >/dev/null || { echo "oci CLI is required" >&2; exit 2; }
compartment_json="$(oci --profile "$oci_profile" --region "$oci_region" iam compartment get --compartment-id "$oci_compartment_id")"
compartment_name="$(printf '%s\n' "$compartment_json" | jq -r '.data.name // .data."display-name" // empty')"

if [ -n "$out_dir" ]; then
  printf '%s\n' "$compartment_json" >"$out_dir/oci-compartment.json"
  {
    echo "tfvars=$tfvars"
    echo "run_id=$run_id"
    echo "evidence_dir=$out_dir"
    echo "oci_profile=$oci_profile"
    echo "oci_region=$oci_region"
    echo "oci_compartment_id=$oci_compartment_id"
    echo "oci_compartment_name=$compartment_name"
  } >"$out_dir/oci-compartment-summary.txt"
fi

echo "oci_compartment_id=$oci_compartment_id"
echo "oci_compartment_name=$compartment_name"

if [ "$compartment_name" = "ManagedCompartmentForPaaS" ]; then
  echo "FAIL: OCI compartment must not be ManagedCompartmentForPaaS" >&2
  exit 1
fi

if [ -z "$compartment_name" ]; then
  echo "FAIL: OCI compartment name is empty; check OCI profile, region, and OCID" >&2
  exit 1
fi

echo "PASS: OCI compartment is not ManagedCompartmentForPaaS"
