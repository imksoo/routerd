#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: sam-pve-qga-addresses.sh --tofu-output IN --out OUT [options]

Discover PVE live ISO management addresses from QGA and patch tofu-output.json.
The live ISO path must not use static ens18 addresses; this script copies the
DHCP IPv4 address reported by qemu-guest-agent into each PVE node's public_ip.

Options:
  --tofu-output FILE       Raw `tofu output -json` file.
  --out FILE               Patched output file for sam-e2e.sh.
  --pve-node-ssh-host HOST PVE SSH host; defaults to fabric.value.pve.node_name.
  --management-ifname NAME Management interface reported by QGA (default: ens18).
  --retries N              QGA retry attempts per VM (default: 90).
  --retry-sleep SEC        Delay between QGA retries (default: 20).
  --evidence FILE          Write discovery evidence (default: OUT.qga-addresses.txt).
USAGE
}

tofu_output=
out=
pve_node_ssh_host=
management_ifname=ens18
retries=90
retry_sleep=20
evidence=

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tofu-output) tofu_output=${2:?missing --tofu-output value}; shift 2 ;;
    --out) out=${2:?missing --out value}; shift 2 ;;
    --pve-node-ssh-host) pve_node_ssh_host=${2:?missing --pve-node-ssh-host value}; shift 2 ;;
    --management-ifname) management_ifname=${2:?missing --management-ifname value}; shift 2 ;;
    --retries) retries=${2:?missing --retries value}; shift 2 ;;
    --retry-sleep) retry_sleep=${2:?missing --retry-sleep value}; shift 2 ;;
    --evidence) evidence=${2:?missing --evidence value}; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$tofu_output" ] || { usage >&2; exit 2; }
[ -n "$out" ] || { usage >&2; exit 2; }
[ -f "$tofu_output" ] || { echo "tofu output not found: $tofu_output" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }

if [ -z "$pve_node_ssh_host" ]; then
  pve_node_ssh_host="$(jq -r '.fabric.value.pve.node_ssh_host // .fabric.value.pve.node_name // empty' "$tofu_output")"
fi
[ -n "$pve_node_ssh_host" ] || { echo "PVE SSH host not found; pass --pve-node-ssh-host" >&2; exit 2; }

evidence=${evidence:-"$out.qga-addresses.txt"}
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
cp "$tofu_output" "$tmp"
: >"$evidence"

mapfile -t pve_nodes < <(jq -r '.nodes.value | to_entries[] | select(.value.site == "pve") | [.key, (.value.vm_id | tostring)] | @tsv' "$tofu_output")
[ "${#pve_nodes[@]}" -gt 0 ] || { echo "no PVE nodes found in $tofu_output" >&2; exit 2; }

for entry in "${pve_nodes[@]}"; do
  IFS=$'\t' read -r node vmid <<<"$entry"
  [ -n "$vmid" ] && [ "$vmid" != "null" ] || { echo "missing vm_id for $node" >&2; exit 1; }

  raw=
  for attempt in $(seq 1 "$retries"); do
    if raw="$(ssh "root@$pve_node_ssh_host" "qm agent $vmid ping >/dev/null && qm agent $vmid network-get-interfaces" 2>/dev/null)"; then
      break
    fi
    if [ "$attempt" -eq "$retries" ]; then
      echo "QGA did not become ready for $node vmid=$vmid after $retries attempts" >&2
      exit 1
    fi
    sleep "$retry_sleep"
  done
  ip="$(jq -r --arg ifname "$management_ifname" '
    (if type == "object" and has("result") then .result else . end)[]?
    | select(.name == $ifname)
    | ."ip-addresses"[]?
    | select(."ip-address-type" == "ipv4")
    | ."ip-address"
  ' <<<"$raw" | sed -n '1p')"

  case "$ip" in
    ""|127.*|169.254.*|0.*)
      {
        echo "FAIL node=$node vmid=$vmid ifname=$management_ifname ip=${ip:-<empty>}"
        echo "$raw"
      } >>"$evidence"
      echo "invalid QGA DHCP IPv4 for $node on $management_ifname: ${ip:-<empty>}" >&2
      exit 1
      ;;
  esac

  jq --arg node "$node" --arg ip "$ip" '
    .nodes.value[$node].public_ip = $ip
    | .nodes.value[$node].pve_management_source = "qga-dhcp"
  ' "$tmp" >"$tmp.next"
  mv "$tmp.next" "$tmp"

  {
    echo "PASS node=$node vmid=$vmid ifname=$management_ifname ip=$ip source=qga-dhcp"
    echo "$raw"
    echo
  } >>"$evidence"
done

install -m 0600 "$tmp" "$out"
echo "wrote patched tofu output: $out"
echo "wrote QGA evidence: $evidence"
