#!/usr/bin/env bash
set -euo pipefail

# Recover only exact, contract-pinned PVE VMs when a provider-side create
# fails before it reaches OpenTofu state. This is deliberately not a general
# garbage collector: an authoritative cluster query admits a VMID only on its
# expected node, all present targets are identity-checked before any deletion,
# and the remote destroy command checks that identity again before stopping
# and immediately before destroying the VM.

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=common.sh
source "$script_dir/common.sh"

usage() {
  echo "Usage: $(basename "$0") --evidence FILE" >&2
}

evidence=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --evidence) evidence="${2:?missing --evidence value}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) usage; die "unknown argument: $1" ;;
  esac
done
[ -n "$evidence" ] || { usage; die "--evidence is required"; }

load_contract "$default_contract_path"
for command in awk head jq sed ssh tail timeout tr grep; do
  require_command "$command"
done

evidence="$(absolute_path "$evidence")"
require_run_confined "$evidence"
mkdir -p "$(dirname "$evidence")"

case "$run_id" in
  *[!A-Za-z0-9._-]*|'') die "run ID has unsupported characters" ;;
esac
run_tag="$(printf '%s' "$run_id" | tr '[:upper:]' '[:lower:]')"

# Workloads must be recovered before the disposable shared template they clone
# from. This matches the Terraform dependency graph and avoids deleting a
# template while a still-live workload may depend on it.
declare -a targets=()
mapfile -t targets < <(jq -er '
  .pve as $p |
  [
    ["pve-leaf-a", $p.sshHost, $p.node, $p.vmids["pve-leaf-a"], "leaf"],
    ["pve-client-a", $p.sshHost, $p.node, $p.vmids["pve-client-a"], "client"],
    ["pve-leaf-b", $p.sshHost, $p.node, $p.vmids["pve-leaf-b"], "leaf"],
    ["pve-client-b", $p.sshHost, $p.node, $p.vmids["pve-client-b"], "client"],
    ["pve-rr-a", $p.rrNodes["pve-rr-a"].sshHost, $p.rrNodes["pve-rr-a"].node, $p.vmids["pve-rr-a"], "rr"],
    ["pve-rr-b", $p.rrNodes["pve-rr-b"].sshHost, $p.rrNodes["pve-rr-b"].node, $p.vmids["pve-rr-b"], "rr"],
    ["pve-template-stage", $p.sshHost, $p.templateStage.sourceNode, $p.templateStage.vmid, "template-stage"]
  ] | .[] | @tsv
' "$contract_path")
[ "${#targets[@]}" -eq 7 ] || die "pinned contract must define exactly seven disposable PVE targets"

pve_ssh=(ssh -n -i "$pve_ssh_private_key" -o BatchMode=yes -o StrictHostKeyChecking=yes \
  -o UserKnownHostsFile="$pve_ssh_known_hosts" -o GlobalKnownHostsFile=/dev/null \
  -o CanonicalizeHostname=no -o IdentitiesOnly=yes -o PasswordAuthentication=no \
  -o KbdInteractiveAuthentication=no -o ConnectTimeout=10)

# Each bounded action is below the cleanup watchdog's budget. The supervisor
# retries failed cleanup; records are atomically written after every action so
# a later attempt retains forensic progress without trusting it for control.
cluster_timeout_seconds=10
inspect_timeout_seconds=10
destroy_timeout_seconds=60

records='[]'
if [ -f "$evidence" ]; then
  previous_records="$(jq -cer --arg runId "$run_id" '
    if .runId == $runId and .action == "pve-orphan-recovery" and (.targets | type == "array")
    then .targets else [] end
  ' "$evidence" 2>/dev/null || true)"
  [ -n "$previous_records" ] && records="$previous_records"
fi

write_records() {
  local temporary
  temporary="$evidence.tmp.$$"
  jq -n --arg runId "$run_id" --argjson targets "$records" \
    '{runId:$runId,action:"pve-orphan-recovery",targets:$targets}' >"$temporary"
  chmod 600 "$temporary"
  mv -f "$temporary" "$evidence"
}

append_record() {
  local label="$1" host="$2" node="$3" vmid="$4" role="$5" action="$6"
  local actual_name="${7:-}" observed_node="${8:-}" observed_type="${9:-}"
  records="$(jq -c \
    --arg label "$label" --arg host "$host" --arg node "$node" --argjson vmid "$vmid" \
    --arg role "$role" --arg action "$action" --arg actualName "$actual_name" \
    --arg observedNode "$observed_node" --arg observedType "$observed_type" '
      . + [{target:$label,pveHost:$host,pveNode:$node,vmid:$vmid,role:$role,
            action:$action,actualName:$actualName,observedNode:$observedNode,
            observedType:$observedType}]
    ' <<<"$records")"
  write_records
}

config_value() {
  local config="$1" key="$2"
  sed -n "s/^${key}: //p" <<<"$config" | head -n 1
}

has_tag() {
  local tags="$1" wanted="$2"
  tr ';' '\n' <<<"$tags" | grep -Fqx -- "$wanted"
}

cluster_inventory() {
  timeout "${cluster_timeout_seconds}s" "${pve_ssh[@]}" "root@$pve_ssh_host" \
    'pvesh get /cluster/resources --type vm --output-format json'
}

valid_cluster_inventory() {
  jq -e 'type == "array"' >/dev/null
}

cluster_target_state() {
  local inventory="$1" vmid="$2"
  jq -cer --argjson vmid "$vmid" '
    [.[] | select((.vmid? // null) == $vmid)] as $matches |
    if ($matches | length) == 0 then
      {state:"absent"}
    elif ($matches | length) == 1 then
      $matches[0] | {state:"present",node:(.node // ""),type:(.type // "")}
    else
      {state:"ambiguous",count:($matches | length)}
    end
  ' <<<"$inventory"
}

inspect_target_config() {
  local host="$1" vmid="$2" command
  printf -v command 'qm config %q' "$vmid"
  timeout "${inspect_timeout_seconds}s" "${pve_ssh[@]}" "root@$host" "$command"
}

destroy_target() {
  local host="$1" vmid="$2" expected_name="$3" expected_run="$4" expected_role="$5" command
  # shellcheck disable=SC2016 # The generated remote shell must expand these values on PVE, not locally.
  printf -v command '
expected_name=%q
expected_run=%q
expected_tag=%q
expected_role=%q
config="$(qm config %q)" || exit 25
config_value() {
  awk -v key="$1" '\''index($0, key ": ") == 1 { print substr($0, length(key) + 3); exit }'\'' <<<"$config"
}
has_tag() {
  tr ";" "\\n" <<<"$tags" | grep -Fqx -- "$1"
}
assert_identity() {
  actual_name="$(config_value name)"
  description="$(config_value description)"
  tags="$(config_value tags)"
  [ "$actual_name" = "$expected_name" ] || exit 31
  case "$description" in *"$expected_run"*) ;; *) exit 32 ;; esac
  has_tag routerd && has_tag sam-e2e && has_tag "$expected_tag" && has_tag "$expected_role" || exit 33
}
assert_identity
status="$(qm status %q | awk "{print \\$2}")" || exit 34
case "$status" in
  running|paused) qm stop %q --timeout 60 || exit 35 ;;
  stopped) ;;
  *) exit 36 ;;
esac
[ "$(qm status %q | awk "{print \\$2}")" = stopped ] || exit 37
config="$(qm config %q)" || exit 38
assert_identity
qm destroy %q --purge 1
' "$expected_name" "$expected_run" "$run_tag" "$expected_role" "$vmid" "$vmid" "$vmid" "$vmid" "$vmid" "$vmid"
  timeout "${destroy_timeout_seconds}s" "${pve_ssh[@]}" "root@$host" "$command"
}

if ! cluster_before="$(cluster_inventory)"; then
  die "could not obtain authoritative PVE cluster VM inventory before orphan recovery"
fi
valid_cluster_inventory <<<"$cluster_before" || die "PVE cluster VM inventory is malformed before orphan recovery"

# Read-only admission completes for every target before the first qm destroy.
# A foreign, misplaced, ambiguous, or temporarily unreadable VM blocks all
# deletions rather than allowing a partially trusted recovery.
declare -A admitted=()
admission_failed=0
for target in "${targets[@]}"; do
  IFS=$'\t' read -r label host node vmid role <<<"$target"
  case "$host:$node:$vmid:$role" in
    *[!A-Za-z0-9._:-]*|*::*) die "pinned PVE orphan target contains unsupported characters" ;;
  esac
  expected_name="routerd-${run_id}-${label}"
  expected_run="run=${run_id}"

  if state_json="$(cluster_target_state "$cluster_before" "$vmid")"; then
    :
  else
    append_record "$label" "$host" "$node" "$vmid" "$role" "admission-cluster-query-invalid"
    admission_failed=1
    continue
  fi
  state="$(jq -r '.state' <<<"$state_json")"
  case "$state" in
    absent)
      append_record "$label" "$host" "$node" "$vmid" "$role" "absent"
      continue
      ;;
    present)
      observed_node="$(jq -r '.node' <<<"$state_json")"
      observed_type="$(jq -r '.type' <<<"$state_json")"
      if [ "$observed_node" != "$node" ] || [ "$observed_type" != qemu ]; then
        append_record "$label" "$host" "$node" "$vmid" "$role" \
          "refused-cluster-target-mismatch" "" "$observed_node" "$observed_type"
        admission_failed=1
        continue
      fi
      ;;
    *)
      append_record "$label" "$host" "$node" "$vmid" "$role" "refused-cluster-target-ambiguous"
      admission_failed=1
      continue
      ;;
  esac

  if config="$(inspect_target_config "$host" "$vmid")"; then
    :
  else
    append_record "$label" "$host" "$node" "$vmid" "$role" "admission-config-unavailable"
    admission_failed=1
    continue
  fi
  actual_name="$(config_value "$config" name)"
  description="$(config_value "$config" description)"
  tags="$(config_value "$config" tags)"
  if [ "$actual_name" != "$expected_name" ] || [[ "$description" != *"$expected_run"* ]] || \
    ! has_tag "$tags" routerd || ! has_tag "$tags" sam-e2e || ! has_tag "$tags" "$run_tag" || ! has_tag "$tags" "$role"; then
    append_record "$label" "$host" "$node" "$vmid" "$role" \
      "refused-identity-mismatch" "$actual_name" "$observed_node" "$observed_type"
    admission_failed=1
    continue
  fi
  admitted["$label"]=1
  append_record "$label" "$host" "$node" "$vmid" "$role" "admitted-identity-match" \
    "$actual_name" "$observed_node" "$observed_type"
done

[ "$admission_failed" -eq 0 ] ||
  die "refusing PVE orphan recovery because read-only identity admission failed"

for target in "${targets[@]}"; do
  IFS=$'\t' read -r label host node vmid role <<<"$target"
  [ "${admitted[$label]:-0}" = 1 ] || continue
  expected_name="routerd-${run_id}-${label}"
  expected_run="run=${run_id}"

  if destroy_target "$host" "$vmid" "$expected_name" "$expected_run" "$role"; then
    :
  else
    append_record "$label" "$host" "$node" "$vmid" "$role" "destroy-failed" "$expected_name"
    die "could not stop and destroy identity-matched PVE orphan $label on $host vmid=$vmid"
  fi

  if cluster_after="$(cluster_inventory)"; then
    :
  else
    append_record "$label" "$host" "$node" "$vmid" "$role" "post-destroy-cluster-query-failed"
    die "could not verify deletion of PVE orphan $label on $host vmid=$vmid"
  fi
  valid_cluster_inventory <<<"$cluster_after" || {
    append_record "$label" "$host" "$node" "$vmid" "$role" "post-destroy-cluster-inventory-invalid"
    die "PVE cluster VM inventory is malformed after deleting $label"
  }
  post_state="$(cluster_target_state "$cluster_after" "$vmid")" || {
    append_record "$label" "$host" "$node" "$vmid" "$role" "post-destroy-cluster-target-invalid"
    die "could not interpret PVE cluster VM inventory after deleting $label"
  }
  if [ "$(jq -r '.state' <<<"$post_state")" != absent ]; then
    append_record "$label" "$host" "$node" "$vmid" "$role" "post-destroy-still-present" \
      "" "$(jq -r '.node // ""' <<<"$post_state")" "$(jq -r '.type // ""' <<<"$post_state")"
    die "PVE orphan remained after destroy: $label on $host vmid=$vmid"
  fi
  append_record "$label" "$host" "$node" "$vmid" "$role" "destroyed-after-identity-match"
done

write_records
echo "PVE orphan recovery complete: runId=$run_id"
