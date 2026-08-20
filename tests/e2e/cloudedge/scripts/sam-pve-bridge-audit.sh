#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/sam-pve-bridge-audit.sh --tofu-output tofu-output.json --pve-node-ssh-host HOST --pve-ssh-key FILE --pve-known-hosts FILE [--evidence FILE]

Fails when the PVE capture bridge contains VMs outside the topology described
by tofu-output.json. This protects SAM qualification from shared overlay
segments such as svnet1 where unrelated router VMs may answer ARP or forward
traffic for 10.77.60.x addresses. It also reads each PVE RR's `qm config` on
its own declared PVE host and fails unless the RR has exactly one underlay NIC
and no capture-bridge attachment.
USAGE
}

tofu_output=
evidence=
pve_ssh_key=
pve_known_hosts=
pve_node_ssh_host=

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tofu-output) tofu_output=${2:?missing --tofu-output value}; shift 2 ;;
    --evidence) evidence=${2:?missing --evidence value}; shift 2 ;;
    --pve-ssh-key) pve_ssh_key=${2:?missing --pve-ssh-key value}; shift 2 ;;
    --pve-known-hosts) pve_known_hosts=${2:?missing --pve-known-hosts value}; shift 2 ;;
    --pve-node-ssh-host) pve_node_ssh_host=${2:?missing --pve-node-ssh-host value}; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$tofu_output" ] || { echo "--tofu-output is required" >&2; exit 2; }
[ -f "$tofu_output" ] || { echo "tofu output not found: $tofu_output" >&2; exit 2; }
if [ -z "$pve_ssh_key" ] || [ ! -f "$pve_ssh_key" ]; then
  echo "--pve-ssh-key FILE is required" >&2
  exit 2
fi
if [ -z "$pve_known_hosts" ] || [ ! -f "$pve_known_hosts" ]; then
  echo "--pve-known-hosts FILE is required" >&2
  exit 2
fi
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }
[ -n "$pve_node_ssh_host" ] || { echo "--pve-node-ssh-host HOST is required" >&2; exit 2; }

pve_host="$pve_node_ssh_host"
capture_bridge="$(jq -r '.fabric.value.pve.leaf_capture_bridge // empty' "$tofu_output")"
[ -n "$pve_host" ] || { echo "PVE host not found in $tofu_output" >&2; exit 2; }
[ -n "$capture_bridge" ] || { echo "PVE capture bridge not found in $tofu_output" >&2; exit 2; }

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
# The capture-bridge query below supplies its remote script on stdin, so this
# connection must not use ssh -n.
pve_ssh=(ssh -i "$pve_ssh_key" -o BatchMode=yes -o StrictHostKeyChecking=yes \
  -o UserKnownHostsFile="$pve_known_hosts" -o GlobalKnownHostsFile=/dev/null \
  -o CanonicalizeHostname=no -o IdentitiesOnly=yes -o PasswordAuthentication=no \
  -o KbdInteractiveAuthentication=no -o ConnectTimeout=10)

jq -r --arg captureBridge "$capture_bridge" '
  .nodes.value
  | to_entries[]
  | select(.value.site == "pve" and .value.capture_bridge == $captureBridge)
  | (.value.vm_id // empty)
' "$tofu_output" | sort -n >"$tmp_dir/expected.txt"

if [ ! -s "$tmp_dir/expected.txt" ]; then
  echo "No PVE VMIDs found in $tofu_output" >&2
  exit 2
fi

# The single-quoted payload is intentionally expanded only by the remote shell.
# shellcheck disable=SC2016
remote_script='
set -eu
bridge="$1"
for id in $(qm list | awk "NR>1 {print \$1}"); do
  cfg="$(qm config "$id" 2>/dev/null || true)"
  if printf "%s\n" "$cfg" | grep -Eq "^[[:space:]]*net[0-9]+: .*bridge=${bridge}([,[:space:]]|$)"; then
    name="$(printf "%s\n" "$cfg" | awk -F": " "/^name:/ {print \$2; exit}")"
    nets="$(printf "%s\n" "$cfg" | awk -v bridge="$bridge" '"'"'
      /^net[0-9]+:/ && $0 ~ "bridge=" bridge "([,[:space:]]|$)" {
        if (out != "") out = out "; "
        out = out $0
      }
      END { print out }
    '"'"')"
    printf "%s\t%s\t%s\n" "$id" "${name:-unknown}" "$nets"
  fi
done
'

"${pve_ssh[@]}" "root@$pve_host" "bash -s -- $(printf '%q' "$capture_bridge")" <<<"$remote_script" | sort -n >"$tmp_dir/attached.tsv"

{
  echo "pve_host=$pve_host"
  echo "capture_bridge=$capture_bridge"
  echo
  echo "## expected_pve_vmids"
  cat "$tmp_dir/expected.txt"
  echo
  echo "## attached_vms"
  cat "$tmp_dir/attached.tsv"
  echo
  echo "## unexpected_vms"
} >"${evidence:-/dev/stdout}"

status=0
cut -f1 "$tmp_dir/attached.tsv" | sort -n >"$tmp_dir/attached-ids.txt"
while IFS=$'\t' read -r id name nets; do
  [ -n "$id" ] || continue
  if ! grep -qx "$id" "$tmp_dir/expected.txt"; then
    printf "%s\t%s\t%s\n" "$id" "$name" "$nets" | tee -a "${evidence:-/dev/stdout}" >/dev/null
    status=1
  fi
done <"$tmp_dir/attached.tsv"

{
  echo
  echo "## missing_expected_vms"
} >>"${evidence:-/dev/stdout}"

while read -r id; do
  [ -n "$id" ] || continue
  if ! grep -qx "$id" "$tmp_dir/attached-ids.txt"; then
    echo "$id" | tee -a "${evidence:-/dev/stdout}" >/dev/null
    status=1
  fi
done <"$tmp_dir/expected.txt"

audit_pve_rr_nics() {
  local rr_expected="$tmp_dir/rr-expected.tsv"
  local node rr_host rr_vmid rr_underlay rr_config rr_stderr
  local net_count has_underlay has_capture result net_config

  bridge_attached() {
    local bridge="$1" config="$2"
    awk -v bridge="$bridge" '
      /^[[:space:]]*net[0-9]+:/ {
        count = split($0, fields, ",")
        for (i = 1; i <= count; i++) {
          value = fields[i]
          sub(/^[[:space:]]+/, "", value)
          sub(/[[:space:]]+$/, "", value)
          if (value == "bridge=" bridge) found = 1
        }
      }
      END { exit found ? 0 : 1 }
    ' "$config"
  }

  if ! jq -er '
    [
      .nodes.value
      | to_entries[]
      | select(.value.site == "pve" and .value.role == "rr")
    ] as $rrs
    | if ($rrs | length) != 2 then
        error("expected exactly two PVE RR nodes")
      else
        $rrs[]
      end
    | [
        .key,
        (.value.pve_ssh_host // error("PVE RR is missing pve_ssh_host")),
        (.value.vm_id // error("PVE RR is missing vm_id")),
        (.value.underlay_bridge // error("PVE RR is missing underlay_bridge"))
      ]
    | @tsv
  ' "$tofu_output" | sort >"$rr_expected"; then
    echo "FAIL: tofu output does not declare exactly two inspectable PVE RRs" >&2
    status=1
    return
  fi
  if [ "$(wc -l <"$rr_expected")" -ne 2 ] || [ "$(cut -f2 "$rr_expected" | sort -u | wc -l)" -ne 2 ]; then
    echo "FAIL: PVE RRs must use exactly two distinct PVE SSH hosts" >&2
    status=1
    return
  fi

  {
    echo
    echo "## pve_rr_nic_audit"
    printf 'node\tpve_ssh_host\tvmid\texpected_underlay_bridge\tcapture_bridge\tnet_count\tresult\tnet_config\n'
  } >>"${evidence:-/dev/stdout}"

  while IFS=$'\t' read -r node rr_host rr_vmid rr_underlay; do
    [ -n "$node" ] || continue
    case "$rr_vmid" in
      ''|*[!0-9]*)
        printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
          "$node" "${rr_host:-missing}" "${rr_vmid:-missing}" "${rr_underlay:-missing}" "$capture_bridge" \
          "0" "FAIL_OUTPUT" "invalid-vmid" >>"${evidence:-/dev/stdout}"
        status=1
        continue
        ;;
    esac
    if [ -z "$rr_host" ] || [ -z "$rr_underlay" ] || [ "$rr_underlay" = "$capture_bridge" ]; then
      printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
        "$node" "${rr_host:-missing}" "$rr_vmid" "${rr_underlay:-missing}" "$capture_bridge" \
        "0" "FAIL_OUTPUT" "invalid-or-capture-bridge-underlay" >>"${evidence:-/dev/stdout}"
      status=1
      continue
    fi

    rr_config="$tmp_dir/${node}-qm-config.txt"
    rr_stderr="$tmp_dir/${node}-qm-config.stderr"
    if ! "${pve_ssh[@]}" "root@$rr_host" "qm config $(printf '%q' "$rr_vmid")" >"$rr_config" 2>"$rr_stderr"; then
      printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
        "$node" "$rr_host" "$rr_vmid" "$rr_underlay" "$capture_bridge" \
        "0" "FAIL_SSH_OR_QM" "$(tr '\n' ' ' <"$rr_stderr")" >>"${evidence:-/dev/stdout}"
      status=1
      continue
    fi

    net_count="$(awk '/^[[:space:]]*net[0-9]+:/ { count++ } END { print count + 0 }' "$rr_config")"
    has_underlay=0
    if bridge_attached "$rr_underlay" "$rr_config"; then
      has_underlay=1
    fi
    has_capture=0
    if bridge_attached "$capture_bridge" "$rr_config"; then
      has_capture=1
    fi
    result=PASS
    if [ "$net_count" -ne 1 ]; then
      result=FAIL_NIC_COUNT
    elif [ "$has_underlay" -ne 1 ]; then
      result=FAIL_UNDERLAY_BRIDGE
    elif [ "$has_capture" -ne 0 ]; then
      result=FAIL_CAPTURE_BRIDGE
    fi
    net_config="$(awk '/^[[:space:]]*net[0-9]+:/ { if (out != "") out = out ";"; out = out $0 } END { print out }' "$rr_config")"
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
      "$node" "$rr_host" "$rr_vmid" "$rr_underlay" "$capture_bridge" \
      "$net_count" "$result" "${net_config:-missing}" >>"${evidence:-/dev/stdout}"
    [ "$result" = PASS ] || status=1
  done <"$rr_expected"
}

audit_pve_rr_nics

if [ "$status" -ne 0 ]; then
  echo "FAIL: PVE capture bridge membership or PVE RR NIC topology does not match the declared topology" >&2
  exit 1
fi

echo "PASS: PVE capture bridge $capture_bridge and both PVE RR underlay-only NICs match the declared topology" | tee -a "${evidence:-/dev/stdout}" >/dev/null
