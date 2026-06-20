#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/sam-preflight.sh --tfvars terraform.tfvars [--evidence-dir DIR]

Checks provider prerequisites that should be verified before OpenTofu apply.
Currently this gates OCI compartment selection so resources are not applied to
ManagedCompartmentForPaaS by mistake.
USAGE
}

tfvars=
evidence_dir=

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tfvars) tfvars="$2"; shift 2 ;;
    --evidence-dir) evidence_dir="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$tfvars" ] || { echo "--tfvars is required" >&2; exit 2; }
[ -f "$tfvars" ] || { echo "tfvars not found: $tfvars" >&2; exit 2; }
command -v oci >/dev/null || { echo "oci CLI is required" >&2; exit 2; }

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

oci_profile="$(extract_tfvars_string oci_profile)"
oci_region="$(extract_tfvars_string oci_region)"
oci_compartment_id="$(extract_tfvars_string oci_compartment_id)"

[ -n "$oci_profile" ] || oci_profile=DEFAULT
[ -n "$oci_region" ] || oci_region=ap-tokyo-1
[ -n "$oci_compartment_id" ] || { echo "oci_compartment_id missing in $tfvars" >&2; exit 1; }

out_dir="${evidence_dir:-}"
if [ -n "$out_dir" ]; then
  mkdir -p "$out_dir"
fi

compartment_json="$(oci --profile "$oci_profile" --region "$oci_region" iam compartment get --compartment-id "$oci_compartment_id")"
display_name="$(printf '%s\n' "$compartment_json" | jq -r '.data."display-name" // empty')"

if [ -n "$out_dir" ]; then
  printf '%s\n' "$compartment_json" >"$out_dir/oci-compartment.json"
  {
    echo "tfvars=$tfvars"
    echo "oci_profile=$oci_profile"
    echo "oci_region=$oci_region"
    echo "oci_compartment_id=$oci_compartment_id"
    echo "oci_compartment_display_name=$display_name"
  } >"$out_dir/oci-compartment-summary.txt"
fi

echo "oci_compartment_id=$oci_compartment_id"
echo "oci_compartment_display_name=$display_name"

if [ "$display_name" = "ManagedCompartmentForPaaS" ]; then
  echo "FAIL: OCI compartment must not be ManagedCompartmentForPaaS" >&2
  exit 1
fi

if [ -z "$display_name" ]; then
  echo "FAIL: OCI compartment display name is empty; check OCI profile, region, and OCID" >&2
  exit 1
fi

echo "PASS: OCI compartment is not ManagedCompartmentForPaaS"
