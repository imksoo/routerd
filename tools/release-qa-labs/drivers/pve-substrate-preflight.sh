#!/usr/bin/env bash
set -euo pipefail

# Read-only PVE admission gate for the supervised release-QA lifecycle.  This
# runs in PRECHECK, before the mutation subprocess exists.  It deliberately
# does not attempt a clone: a cross-host template clone can only be proven by
# the PVE-first, supervisor-owned certification apply, which must succeed
# before the cloud certification is allowed to start.

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=common.sh
source "$script_dir/common.sh"

usage() {
  echo "Usage: $(basename "$0") --contract FILE" >&2
}

contract_arg=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --contract) contract_arg="${2:?missing --contract value}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) usage; die "unknown argument: $1" ;;
  esac
done
[ -n "$contract_arg" ] || { usage; die "--contract is required"; }
load_contract "$contract_arg"

for command in curl jq ssh timeout; do
  require_command "$command"
done

preflight_dir="$evidence_root/precheck/pve-substrate"
mkdir -p "$preflight_dir"
chmod 700 "$preflight_dir"

# Accept exactly one simple HCL scalar assignment.  The production contract
# guard pins tfvars before this script executes; rejecting duplicates here also
# makes direct invocation fail closed rather than selecting an ambiguous value.
tfvars_quoted() {
  local key="$1"
  local -a matches=()
  mapfile -t matches < <(
    awk -v key="$key" '
      $0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
        line=$0
        sub(/^[^=]*=[[:space:]]*/, "", line)
        sub(/[[:space:]]*(#.*)?$/, "", line)
        if (line ~ /^"[^"]*"$/) {
          sub(/^"/, "", line)
          sub(/"$/, "", line)
          print line
        } else {
          print "__invalid__"
        }
      }
    ' "$tfvars_path"
  )
  [ "${#matches[@]}" -eq 1 ] || die "OpenTofu $key must have exactly one quoted value"
  [ "${matches[0]}" != "__invalid__" ] || die "OpenTofu $key must be a quoted string"
  [ -n "${matches[0]}" ] || die "OpenTofu $key must not be empty"
  printf '%s\n' "${matches[0]}"
}

tfvars_integer() {
  local key="$1"
  local -a matches=()
  mapfile -t matches < <(
    awk -v key="$key" '
      $0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
        line=$0
        sub(/^[^=]*=[[:space:]]*/, "", line)
        sub(/[[:space:]]*(#.*)?$/, "", line)
        if (line ~ /^[1-9][0-9]*$/) print line; else print "__invalid__"
      }
    ' "$tfvars_path"
  )
  [ "${#matches[@]}" -eq 1 ] || die "OpenTofu $key must have exactly one positive integer value"
  [ "${matches[0]}" != "__invalid__" ] || die "OpenTofu $key must be a positive integer"
  printf '%s\n' "${matches[0]}"
}

tfvars_boolean_or_default() {
  local key="$1" default="$2"
  local -a matches=()
  mapfile -t matches < <(
    awk -v key="$key" '
      $0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
        line=$0
        sub(/^[^=]*=[[:space:]]*/, "", line)
        sub(/[[:space:]]*(#.*)?$/, "", line)
        if (line == "true" || line == "false") print line; else print "__invalid__"
      }
    ' "$tfvars_path"
  )
  [ "${#matches[@]}" -le 1 ] || die "OpenTofu $key must not be duplicated"
  if [ "${#matches[@]}" -eq 0 ]; then
    printf '%s\n' "$default"
    return
  fi
  [ "${matches[0]}" != "__invalid__" ] || die "OpenTofu $key must be true or false"
  printf '%s\n' "${matches[0]}"
}

contract_boot_source="$(jq -er '.pve.bootSource' "$contract_path")"
case "$contract_boot_source" in
  template|iso) ;;
  *) die "contract pve.bootSource must be template or iso" ;;
esac
tfvars_boot_source="$(tfvars_quoted pve_boot_source)"
[ "$tfvars_boot_source" = "$contract_boot_source" ] ||
  die "OpenTofu pve_boot_source does not equal contract pve.bootSource"

datastore="$(jq -er '.pve.datastore' "$contract_path")"
[ "$datastore" = "$(tfvars_quoted pve_datastore_id)" ] ||
  die "OpenTofu pve_datastore_id does not equal contract pve.datastore"
case "$datastore" in
  *[!A-Za-z0-9._-]*|'') die "PVE datastore name contains unsupported characters" ;;
esac

underlay_bridge="$(jq -er '.pve.underlayBridge' "$contract_path")"
capture_bridge="$(jq -er '.pve.captureBridge' "$contract_path")"
[ "$underlay_bridge" = "$(tfvars_quoted pve_underlay_bridge)" ] ||
  die "OpenTofu pve_underlay_bridge does not equal contract pve.underlayBridge"
[ "$capture_bridge" = "$(tfvars_quoted pve_capture_bridge)" ] ||
  die "OpenTofu pve_capture_bridge does not equal contract pve.captureBridge"
[ "$underlay_bridge" != "$capture_bridge" ] ||
  die "PVE underlay bridge must differ from the run capture bridge"
case "$underlay_bridge" in
  *[!A-Za-z0-9._-]*|'') die "PVE underlay bridge name contains unsupported characters" ;;
esac
case "$capture_bridge" in
  *[!A-Za-z0-9._-]*|'') die "PVE capture bridge name contains unsupported characters" ;;
esac

pve_endpoint="$(tfvars_quoted pve_endpoint)"
[ "$pve_endpoint" = "https://$pve_ssh_host:8006/" ] ||
  die "OpenTofu pve_endpoint does not use the pinned PVE SSH host"
pve_insecure="$(tfvars_boolean_or_default pve_insecure false)"
[ "$pve_insecure" = false ] ||
  die "OpenTofu pve_insecure must be false for release qualification"
[ -n "${pve_ca_pem:-}" ] ||
  die "PVE CA is unavailable from the pinned runtime source"
pve_ssh=(ssh -n -i "$pve_ssh_private_key" -o BatchMode=yes -o StrictHostKeyChecking=yes \
  -o UserKnownHostsFile="$pve_ssh_known_hosts" -o GlobalKnownHostsFile=/dev/null \
  -o CanonicalizeHostname=no -o IdentitiesOnly=yes -o PasswordAuthentication=no \
  -o KbdInteractiveAuthentication=no -o ConnectTimeout=10)

rr_a_node="$(jq -er '.pve.rrNodes["pve-rr-a"].node' "$contract_path")"
rr_a_host="$(jq -er '.pve.rrNodes["pve-rr-a"].sshHost' "$contract_path")"
rr_a_bridge="$(jq -er '.pve.rrNodes["pve-rr-a"].underlayBridge' "$contract_path")"
rr_b_node="$(jq -er '.pve.rrNodes["pve-rr-b"].node' "$contract_path")"
rr_b_host="$(jq -er '.pve.rrNodes["pve-rr-b"].sshHost' "$contract_path")"
rr_b_bridge="$(jq -er '.pve.rrNodes["pve-rr-b"].underlayBridge' "$contract_path")"
for value in "$pve_node" "$rr_a_node" "$rr_b_node" "$rr_a_host" "$rr_b_host" "$rr_a_bridge" "$rr_b_bridge"; do
  case "$value" in
    *[!A-Za-z0-9._-]*|'') die "PVE node, host, or bridge contains unsupported characters" ;;
  esac
done
for bridge in "$rr_a_bridge" "$rr_b_bridge"; do
  [ "$bridge" != "$capture_bridge" ] || die "PVE RR underlay bridge must differ from the run capture bridge"
done

if [ -z "${TF_VAR_pve_api_token:-}" ]; then
  die "PVE API token is unavailable from the run-confined token source"
fi

# Curl reads the secret only from a private header file.  It never appears in
# argv, evidence, or diagnostics.  The API request is an authenticated,
# read-only cluster inventory and is deliberately forced to bypass HTTP(S)
# proxy settings because PVE is an on-prem endpoint.
api_header="$(mktemp "$runtime_root/.pve-api-header.XXXXXX")"
chmod 600 "$api_header"
cleanup_api_header() {
  rm -f "$api_header"
}
trap cleanup_api_header EXIT INT TERM
printf 'Authorization: PVEAPIToken=%s\n' "$TF_VAR_pve_api_token" >"$api_header"

api_inventory="$preflight_dir/api-cluster-vms.json"
curl_args=(-q --silent --show-error --fail --connect-timeout 10 --max-time 20 --noproxy '*' \
  --cacert "$pve_ca_pem" --header "@$api_header" --output "$api_inventory")
if ! curl "${curl_args[@]}" "${pve_endpoint}api2/json/cluster/resources?type=vm"; then
  die "PVE authenticated API inventory failed"
fi
jq -e '(.data | type) == "array"' "$api_inventory" >/dev/null ||
  die "PVE authenticated API inventory is malformed"

planned_vmids="$(jq -ec '[.pve.vmids[]] | sort' "$contract_path")"
if ! jq -e '
  (.pve.vmids | type) == "object" and
  ([.pve.vmids[]] | length == 6 and
    all(.[]; type == "number" and floor == . and . > 0) and
    (unique | length == 6))
' "$contract_path" >/dev/null; then
  die "contract must pin six distinct positive PVE VMIDs"
fi
stage_vmid=
template_vmid=
template_source_node=
if [ "$contract_boot_source" = template ]; then
  template_source_node="$(jq -er '.pve.templateStage.sourceNode' "$contract_path")"
  template_vmid="$(jq -er '.pve.templateStage.sourceTemplateVMID' "$contract_path")"
  stage_vmid="$(jq -er '.pve.templateStage.vmid' "$contract_path")"
  stage_datastore="$(jq -er '.pve.templateStage.datastore' "$contract_path")"
  [ "$template_source_node" = "$pve_node" ] ||
    die "PVE shared template stage source node must equal the PVE leaf/source node"
  [ "$stage_datastore" = "$datastore" ] ||
    die "PVE shared template stage datastore must equal contract pve.datastore"
  for value in "$template_source_node" "$template_vmid" "$stage_vmid" "$stage_datastore"; do
    case "$value" in
      *[!A-Za-z0-9._-]*|'') die "PVE shared template stage identity contains unsupported characters" ;;
    esac
  done
  [ "$template_source_node" = "$(tfvars_quoted pve_template_source_node)" ] ||
    die "OpenTofu pve_template_source_node does not equal contract pve.templateStage.sourceNode"
  [ "$template_vmid" = "$(tfvars_integer pve_template_vm_id)" ] ||
    die "OpenTofu pve_template_vm_id does not equal contract pve.templateStage.sourceTemplateVMID"
  [ "$stage_vmid" = "$(tfvars_integer pve_template_stage_vm_id)" ] ||
    die "OpenTofu pve_template_stage_vm_id does not equal contract pve.templateStage.vmid"
  [ "$stage_vmid" != "$template_vmid" ] ||
    die "PVE shared template stage VMID must differ from its immutable source template"
  if jq -e --argjson stage "$stage_vmid" --argjson source "$template_vmid" '
      ([.pve.vmids[]] | index($stage) != null) or
      ([.pve.vmids[]] | index($source) != null)
    ' "$contract_path" >/dev/null; then
    die "PVE shared template source/stage VMID must not overlap a workload VMID"
  fi
  [ "$(tfvars_boolean_or_default pve_clone_full false)" = true ] ||
    die "OpenTofu pve_clone_full must be true for the shared-template qualification"
  planned_vmids="$(jq -ec --argjson stage "$stage_vmid" '[.pve.vmids[]] + [$stage] | sort' "$contract_path")"
fi
if ! jq -e --argjson vmids "$planned_vmids" '
  [.data[] | (.vmid? // null) as $id |
   select($id != null and ($vmids | index($id) != null))] | length == 0
' "$api_inventory" >/dev/null; then
  die "PVE cluster already contains one or more exact planned VMIDs"
fi

ssh_pvesh_get() {
  local host="$1" api_path="$2" out="$3" remote_command
  printf -v remote_command 'pvesh get %q --output-format json' "$api_path"
  if ! timeout 20s "${pve_ssh[@]}" \
    "root@$host" "$remote_command" >"$out"; then
    die "PVE root SSH or read-only API access failed for $host"
  fi
}

ssh_pvesh_get_iso_inventory() {
  local host="$1" node="$2" storage="$3" out="$4" api_path remote_command
  api_path="/nodes/$node/storage/$storage/content"
  printf -v remote_command 'pvesh get %q --content iso --output-format json' "$api_path"
  if ! timeout 20s "${pve_ssh[@]}" \
    "root@$host" "$remote_command" >"$out"; then
    die "PVE root SSH or read-only ISO inventory access failed for $host"
  fi
}

ssh_live_links_get() {
  local host="$1" out="$2"
  if ! timeout 20s "${pve_ssh[@]}" "root@$host" \
    'ip -j -d link show' >"$out"; then
    die "PVE root SSH or live link inventory failed for $host"
  fi
}

check_host() {
  local node="$1" host="$2" bridge="$3" shared_required="$4" network storage live_links
  network="$preflight_dir/network-${node}.json"
  storage="$preflight_dir/storage-${node}.json"
  live_links="$preflight_dir/live-links-${node}.json"
  ssh_pvesh_get "$host" "/nodes/$node/network" "$network"
  jq -e 'type == "array"' "$network" >/dev/null ||
    die "PVE network inventory is malformed for $node"
  jq -e --arg bridge "$bridge" 'any(.[]; .iface == $bridge)' "$network" >/dev/null ||
    die "PVE underlay bridge is absent on $node"
  if jq -e --arg bridge "$capture_bridge" 'any(.[]; .iface == $bridge)' "$network" >/dev/null; then
    die "run-scoped PVE capture bridge already exists on $node"
  fi
  ssh_live_links_get "$host" "$live_links"
  jq -e 'type == "array"' "$live_links" >/dev/null ||
    die "PVE live link inventory is malformed for $node"
  if jq -e --arg bridge "$capture_bridge" 'any(.[]; .ifname == $bridge)' "$live_links" >/dev/null; then
    die "run-scoped PVE capture bridge is already live on $node"
  fi

  ssh_pvesh_get "$host" "/nodes/$node/storage" "$storage"
  jq -e 'type == "array"' "$storage" >/dev/null ||
    die "PVE storage inventory is malformed for $node"
  jq -e --arg datastore "$datastore" '
    any(.[]; .storage == $datastore and
      ((.active // false) == true or (.active // 0) == 1) and
      ((.enabled // false) == true or (.enabled // 0) == 1))
  ' "$storage" >/dev/null ||
    die "PVE datastore is not active and enabled on $node"
  if [ "$shared_required" = true ]; then
    jq -e --arg datastore "$datastore" '
      any(.[]; .storage == $datastore and
        ((.shared // false) == true or (.shared // 0) == 1))
    ' "$storage" >/dev/null ||
      die "PVE datastore must be shared on $node for cross-host template clones"
  fi
}

# PVE RR template IDs are cluster-global.  Do not require a copied template
# config on every RR host; that would reject valid cross-host clone workflows.
# Every target host must still expose the configured datastore and underlay.
shared_required=false
[ "$contract_boot_source" = template ] && shared_required=true
check_host "$pve_node" "$pve_ssh_host" "$underlay_bridge" "$shared_required"
check_host "$rr_a_node" "$rr_a_host" "$rr_a_bridge" "$shared_required"
check_host "$rr_b_node" "$rr_b_host" "$rr_b_bridge" "$shared_required"

boot_evidence="$preflight_dir/boot-source.json"
case "$contract_boot_source" in
  template)
    if ! jq -e --argjson vmid "$template_vmid" --arg node "$template_source_node" '
      [.data[] | select(.vmid == $vmid and .node == $node and
        ((.template // false) == true or (.template // 0) == 1))]
      | length == 1
    ' "$api_inventory" >/dev/null; then
      die "PVE source template VMID is absent, on the wrong source node, or is not a cluster template"
    fi
    jq -n --arg mode template --arg sourceNode "$template_source_node" \
      --arg datastore "$stage_datastore" --argjson templateVMID "$template_vmid" \
      --argjson stageVMID "$stage_vmid" \
      --arg verification "source-template-on-leaf-and-shared-datastore-on-all-targets" \
      --arg crossHostClone "stage-only full qnap copy is applied and inspected before six target clones" \
      '{mode:$mode,sourceNode:$sourceNode,templateVMID:$templateVMID,stageVMID:$stageVMID,
        sharedDatastore:$datastore,verification:$verification,crossHostClone:$crossHostClone}' \
      >"$boot_evidence"
    ;;
  iso)
    iso_file_id="$(tfvars_quoted pve_iso_file_id)"
    iso_storage="${iso_file_id%%:*}"
    iso_path="${iso_file_id#*:}"
    if [ "$iso_storage" = "$iso_file_id" ] || [ "$iso_path" = "$iso_file_id" ]; then
      die "PVE ISO file ID must use storage:iso/path form"
    fi
    case "$iso_storage" in
      *[!A-Za-z0-9._-]*|'') die "PVE ISO storage name contains unsupported characters" ;;
    esac
    case "$iso_path" in
      iso/[A-Za-z0-9][A-Za-z0-9._/@+-]*) ;;
      *) die "PVE ISO file ID must reference a safe iso/ path" ;;
    esac
    for pair in "$pve_node:$pve_ssh_host" "$rr_a_node:$rr_a_host" "$rr_b_node:$rr_b_host"; do
      node="${pair%%:*}"
      host="${pair#*:}"
      iso_inventory="$preflight_dir/iso-${node}.json"
      ssh_pvesh_get_iso_inventory "$host" "$node" "$iso_storage" "$iso_inventory"
      jq -e 'type == "array"' "$iso_inventory" >/dev/null ||
        die "PVE ISO inventory is malformed for $node"
      jq -e --arg file "$iso_file_id" 'any(.[]; .volid == $file)' "$iso_inventory" >/dev/null ||
        die "PVE ISO is unavailable on $node"
    done
    jq -n --arg mode iso --arg isoFileID "$iso_file_id" \
      --arg verification "ISO-present-on-every-leaf-and-RR-target" \
      '{mode:$mode,isoFileID:$isoFileID,verification:$verification}' >"$boot_evidence"
    ;;
esac

jq -n \
  --arg status pass --arg runId "$run_id" --arg checkedAt "$(utc_now)" \
  --arg bootSource "$contract_boot_source" --arg apiInventory "$api_inventory" \
  --arg bootEvidence "$boot_evidence" \
  '{status:$status,runId:$runId,checkedAt:$checkedAt,bootSource:$bootSource,
    authenticatedAPIInventory:$apiInventory,bootEvidence:$bootEvidence,
    mutationBoundary:"PVE certification runs before cloud certification"}' \
  >"$preflight_dir/result.json"
chmod 600 "$preflight_dir"/*
trap - EXIT INT TERM
cleanup_api_header
echo "PVE substrate preflight: pass"
