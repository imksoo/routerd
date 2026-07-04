#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/sam-pve-bridge-audit.sh --tofu-output tofu-output.json [--evidence FILE]

Fails when the PVE capture bridge contains VMs outside the topology described
by tofu-output.json. This protects SAM qualification from shared overlay
segments such as svnet1 where unrelated router VMs may answer ARP or forward
traffic for 10.77.60.x addresses.
USAGE
}

tofu_output=
evidence=

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tofu-output) tofu_output=${2:?missing --tofu-output value}; shift 2 ;;
    --evidence) evidence=${2:?missing --evidence value}; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$tofu_output" ] || { echo "--tofu-output is required" >&2; exit 2; }
[ -f "$tofu_output" ] || { echo "tofu output not found: $tofu_output" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }

pve_host="$(jq -r '.fabric.value.pve.node_ssh_host // .fabric.value.pve.node_name // empty' "$tofu_output")"
capture_bridge="$(jq -r '.fabric.value.pve.capture_bridge // empty' "$tofu_output")"
[ -n "$pve_host" ] || { echo "PVE host not found in $tofu_output" >&2; exit 2; }
[ -n "$capture_bridge" ] || { echo "PVE capture bridge not found in $tofu_output" >&2; exit 2; }

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

jq -r '
  .nodes.value
  | to_entries[]
  | select(.value.site == "pve")
  | (.value.vm_id // empty)
' "$tofu_output" | sort -n >"$tmp_dir/expected.txt"

if [ ! -s "$tmp_dir/expected.txt" ]; then
  echo "No PVE VMIDs found in $tofu_output" >&2
  exit 2
fi

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

ssh "root@$pve_host" "bash -s -- $(printf '%q' "$capture_bridge")" <<<"$remote_script" | sort -n >"$tmp_dir/attached.tsv"

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

if [ "$status" -ne 0 ]; then
  echo "FAIL: PVE capture bridge $capture_bridge membership does not match expected topology VMIDs" >&2
  exit 1
fi

echo "PASS: PVE capture bridge $capture_bridge contains only expected topology VMIDs" | tee -a "${evidence:-/dev/stdout}" >/dev/null
