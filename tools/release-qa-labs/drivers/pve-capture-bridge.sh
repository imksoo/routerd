#!/usr/bin/env bash
set -euo pipefail

# Stage the isolated PVE capture bridge outside both the API-token Terraform
# boundary and PVE's persistent network configuration.  The bridge is a
# live-only, portless host-network object: no physical NIC, address, gateway,
# DHCP service, or IPv6 RA can escape the test fabric through it.
#
# PVE's network endpoint writes /etc/network/interfaces.new and requires a
# host-wide apply to materialize it.  A qualification run must never leave a
# pending host-network change or reload vmbr0, so this driver uses only exact
# `ip link` operations on the run-scoped bridge.  Its link alias is the
# ownership fence used before deletion; the PVE API remains read-only and any
# same-name persistent network configuration is rejected rather than adopted.

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=common.sh
source "$script_dir/common.sh"

usage() {
  echo "Usage: $(basename "$0") (--ensure|--remove) --evidence FILE" >&2
}

action=
evidence=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --ensure|--remove)
      [ -z "$action" ] || { usage; die "exactly one action is required"; }
      action="${1#--}"
      shift
      ;;
    --evidence)
      evidence="${2:?missing --evidence value}"
      shift 2
      ;;
    -h|--help) usage; exit 0 ;;
    *) usage; die "unknown argument: $1" ;;
  esac
done
[ -n "$action" ] || { usage; die "--ensure or --remove is required"; }
[ -n "$evidence" ] || { usage; die "--evidence is required"; }

load_contract "$default_contract_path"
for command in jq ssh timeout; do
  require_command "$command"
done

evidence="$(absolute_path "$evidence")"
require_run_confined "$evidence"
mkdir -p "$(dirname "$evidence")"

capture_bridge="$(jq -er '.pve.captureBridge' "$contract_path")"
underlay_bridge="$(jq -er '.pve.underlayBridge' "$contract_path")"
for value in "$pve_node" "$capture_bridge" "$underlay_bridge"; do
  case "$value" in
    *[!A-Za-z0-9._-]*|'') die "PVE node or bridge contains unsupported characters" ;;
  esac
done
[ "$capture_bridge" != "$underlay_bridge" ] ||
  die "capture bridge must differ from the PVE management/underlay bridge"

# The alias is not a user-visible label: it is a narrow ownership capability
# for the one live link this driver may remove.  Values reach the remote shell
# only through printf %q below, and Linux caps an ifalias at 256 bytes.
expires_at="$(extract_tfvars_string "$tfvars_path" expires_at)"
[ -n "$expires_at" ] || die "tfvars expires_at is required for capture bridge ownership"
expected_alias="routerd-release-qa:run=${run_id}:expires=${expires_at}"
[ "${#expected_alias}" -le 255 ] || die "run-scoped capture bridge ownership alias is too long"

pve_ssh=(ssh -n -i "$pve_ssh_private_key" -o BatchMode=yes -o StrictHostKeyChecking=yes \
  -o UserKnownHostsFile="$pve_ssh_known_hosts" -o GlobalKnownHostsFile=/dev/null \
  -o CanonicalizeHostname=no -o IdentitiesOnly=yes -o PasswordAuthentication=no \
  -o KbdInteractiveAuthentication=no -o ConnectTimeout=10)

remote_run() {
  local command="$1" out="$2"
  timeout 60s "${pve_ssh[@]}" "root@$pve_ssh_host" "$command" >"$out"
}

remote_get() {
  local path="$1" out="$2" command
  printf -v command 'pvesh get %q --output-format json' "$path"
  remote_run "$command" "$out"
}

require_no_pending_network_config() {
  local phase="$1" out="$2"
  remote_run 'test ! -e /etc/network/interfaces.new' "$out" ||
    die "PVE has a pending /etc/network/interfaces.new before/after $phase; refusing host-network mutation"
}

management_snapshot() {
  local out="$1" command
  # Exact text comparison is intentional: the only host-network change this
  # driver may make is a fresh isolated bridge.  A changed management IPv4
  # address or default route is an unsafe, fail-closed result.
  printf -v command 'ip -4 -o addr show dev %q; ip -4 route show default' "$underlay_bridge"
  remote_run "$command" "$out" || die "could not snapshot PVE management address/default route"
}

capture_bridge_is_live() {
  local out="$1" command count
  # Query the complete link set rather than a single device. `ip link show
  # dev` returns the same nonzero status for an absent interface and an
  # unreachable/failed query; only a successful, well-formed full snapshot
  # can authorize creation or removal of a same-named bridge.
  command='ip -j -d link show'
  if ! remote_run "$command" "$out"; then
    return 2
  fi
  if ! jq -e 'type == "array"' "$out" >/dev/null; then
    return 2
  fi
  count="$(jq --arg bridge "$capture_bridge" '[.[] | select(.ifname == $bridge)] | length' "$out")" || return 2
  case "$count" in
    0) return 1 ;;
    1) return 0 ;;
    *) return 2 ;;
  esac
}

persistent_bridge_count() {
  local network="$1"
  jq --arg bridge "$capture_bridge" '[.[] | select(.iface == $bridge)] | length' "$network"
}

live_bridge_is_safe() {
  local prefix="$1" link addresses ports ipv6_disabled alias_file alias_expected ipv6_disable_path command
  link="$prefix.link.json"
  addresses="$prefix.addresses.json"
  ports="$prefix.ports.json"
  ipv6_disabled="$prefix.ipv6-disabled.txt"
  alias_file="$prefix.ifalias.txt"
  alias_expected="$prefix.ifalias.expected.txt"
  capture_bridge_is_live "$link" || return 1
  printf -v command 'ip -j addr show dev %q' "$capture_bridge"
  remote_run "$command" "$addresses" || return 1
  printf -v command 'bridge -j link show master %q' "$capture_bridge"
  remote_run "$command" "$ports" || return 1
  ipv6_disable_path="/proc/sys/net/ipv6/conf/$capture_bridge/disable_ipv6"
  printf -v command 'cat %q' "$ipv6_disable_path"
  remote_run "$command" "$ipv6_disabled" || return 1
  printf -v command 'cat %q' "/sys/class/net/$capture_bridge/ifalias"
  remote_run "$command" "$alias_file" || return 1
  # sysfs attributes are line-oriented.  `cat ifalias` therefore returns the
  # exact alias followed by one newline; make the expected fixture match that
  # kernel ABI rather than treating a safely-owned live bridge as foreign.
  printf '%s\n' "$expected_alias" >"$alias_expected"
  jq -e --arg bridge "$capture_bridge" '
    [.[] | select(.ifname == $bridge)] |
    length == 1 and ((.[0].linkinfo.info_kind // "") == "bridge")
  ' "$link" >/dev/null &&
    jq -e '[.[] | .addr_info[]? | select(.family == "inet" or .family == "inet6")] | length == 0' \
      "$addresses" >/dev/null &&
    jq -e 'type == "array" and length == 0' "$ports" >/dev/null &&
    [ "$(tr -d '[:space:]' <"$ipv6_disabled")" = 1 ] &&
    cmp -s "$alias_file" "$alias_expected"
}

create_live_capture_bridge_without_reload() {
  local result="$1" command probe_status ipv6_disable_path
  if capture_bridge_is_live "$result.already-live.json"; then
    die "a live capture bridge exists without a fresh run admission; refusing adoption"
  else
    probe_status=$?
  fi
  [ "$probe_status" -eq 1 ] ||
    die "cannot prove whether the PVE capture bridge is live before direct creation"
  # This creates no persistent PVE network configuration, has no physical
  # bridge port or L3 configuration, and disables IPv6 before link-up.  Do not
  # put `ip link add` in the rollback chain: if another actor wins the exact
  # name race, failed creation must never delete its bridge.
  ipv6_disable_path="/proc/sys/net/ipv6/conf/$capture_bridge/disable_ipv6"
  printf -v command 'ip link add name %q type bridge || exit 1; if ! { ip link set dev %q alias %q && printf 1 > %q && ip link set dev %q up; }; then ip link delete dev %q type bridge 2>/dev/null || true; exit 1; fi' \
    "$capture_bridge" "$capture_bridge" "$expected_alias" "$ipv6_disable_path" "$capture_bridge" "$capture_bridge"
  remote_run "$command" "$result" ||
    die "direct isolated capture bridge creation failed"
}

remove_live_capture_bridge_without_reload() {
  local result="$1" command probe_status
  if capture_bridge_is_live "$result.before-delete.json"; then
    :
  else
    probe_status=$?
    [ "$probe_status" -eq 1 ] && return 0
    die "cannot prove whether the PVE capture bridge is live before direct removal"
  fi
  live_bridge_is_safe "$result.before-delete" ||
    die "refusing to remove a live capture bridge whose ownership or L2/L3 isolation is not exact"
  printf -v command 'ip link delete dev %q type bridge' "$capture_bridge"
  remote_run "$command" "$result" ||
    die "direct isolated capture bridge removal failed"
  if capture_bridge_is_live "$result.after-delete.json"; then
    die "isolated capture bridge remains live after direct removal"
  else
    probe_status=$?
  fi
  [ "$probe_status" -eq 1 ] ||
    die "cannot prove isolated capture bridge absence after direct removal"
}

network="$evidence.network-before.json"
remote_get "/nodes/$pve_node/network" "$network" || die "PVE root SSH or network inventory failed"
jq -e 'type == "array"' "$network" >/dev/null || die "PVE network inventory is malformed"
bridge_count="$(persistent_bridge_count "$network")" || die "PVE network inventory is malformed"

case "$action" in
  ensure)
    [ "$bridge_count" -eq 0 ] ||
      die "run-scoped PVE capture bridge has persistent configuration; refusing adoption"
    if capture_bridge_is_live "$evidence.capture-bridge-live-before-create.json"; then
      die "a live capture bridge exists without matching fresh-run ownership; refusing adoption"
    else
      live_probe_status=$?
    fi
    [ "$live_probe_status" -eq 1 ] ||
      die "cannot prove capture bridge absence from a complete live PVE link snapshot"
    require_no_pending_network_config "capture bridge creation" "$evidence.pending-before-create.txt"
    management_before="$evidence.management-before-create.txt"
    management_snapshot "$management_before"
    create_live_capture_bridge_without_reload "$evidence.create-live.txt"
    network_after="$evidence.network-after-create.json"
    remote_get "/nodes/$pve_node/network" "$network_after" ||
      die "PVE network inventory failed after live capture bridge creation"
    [ "$(persistent_bridge_count "$network_after")" -eq 0 ] ||
      die "direct capture bridge creation unexpectedly changed PVE persistent network configuration"
    require_no_pending_network_config "capture bridge creation" "$evidence.pending-after-create.txt"
    management_after="$evidence.management-after-create.txt"
    management_snapshot "$management_after"
    cmp -s "$management_before" "$management_after" ||
      die "PVE management address or default route changed while staging the capture bridge"
    live_bridge_is_safe "$evidence.capture-bridge-live-after-create" ||
      die "PVE capture bridge is not a live isolated portless bridge after creation"
    jq -n --arg action "$action" --arg runId "$run_id" --arg node "$pve_node" \
      --arg bridge "$capture_bridge" --arg alias "$expected_alias" --arg network "$network_after" \
      '{status:"pass",action:$action,runId:$runId,node:$node,bridge:$bridge,
        ownershipAlias:$alias,safety:"portless-no-address-no-gateway-no-persistent-config",
        networkInventory:$network}' >"$evidence"
    ;;
  remove)
    vmids="$(jq -ec '[.pve.templateStage.vmid] + [.pve.vmids[]] | sort' "$contract_path")" ||
      die "contract must provide the shared template stage and workload VMIDs"
    cluster="$evidence.cluster-vms-before-remove.json"
    remote_command='pvesh get /cluster/resources --type vm --output-format json'
    timeout 30s "${pve_ssh[@]}" "root@$pve_ssh_host" "$remote_command" >"$cluster" ||
      die "PVE root SSH or cluster VM inventory failed before bridge cleanup"
    jq -e 'type == "array"' "$cluster" >/dev/null || die "PVE cluster VM inventory is malformed"
    present="$(jq --argjson vmids "$vmids" '[.[] | select((.vmid as $id | $vmids | index($id)) != null)] | length' "$cluster")"
    [ "$present" -eq 0 ] ||
      die "refusing capture bridge cleanup while $present run VM(s), including a stage template, remain"
    [ "$bridge_count" -eq 0 ] ||
      die "refusing capture bridge cleanup with same-name persistent PVE network configuration"
    require_no_pending_network_config "capture bridge removal" "$evidence.pending-before-remove.txt"
    management_before="$evidence.management-before-remove.txt"
    management_snapshot "$management_before"
    remove_live_capture_bridge_without_reload "$evidence.remove-live.txt"
    network_after="$evidence.network-after-remove.json"
    remote_get "/nodes/$pve_node/network" "$network_after" ||
      die "PVE network inventory failed after live capture bridge cleanup"
    [ "$(persistent_bridge_count "$network_after")" -eq 0 ] ||
      die "capture bridge cleanup unexpectedly changed PVE persistent network configuration"
    require_no_pending_network_config "capture bridge removal" "$evidence.pending-after-remove.txt"
    management_after="$evidence.management-after-remove.txt"
    management_snapshot "$management_after"
    cmp -s "$management_before" "$management_after" ||
      die "PVE management address or default route changed while removing the capture bridge"
    jq -n --arg action "$action" --arg runId "$run_id" --arg node "$pve_node" \
      --arg bridge "$capture_bridge" --arg alias "$expected_alias" --arg cluster "$cluster" \
      '{status:"pass",action:$action,runId:$runId,node:$node,bridge:$bridge,
        ownershipAlias:$alias,result:"deleted-after-authoritative-zero-vm-inventory",
        clusterInventory:$cluster}' >"$evidence"
    ;;
esac
