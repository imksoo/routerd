#!/usr/bin/env bash
# Offline behavioral check for PVE-local WireGuard bootstrap generation.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
generator="$repo_root/tests/e2e/cloudedge/configs/sam-e2e-generate.sh"

bash -n "$generator"

work="$(mktemp -d "${TMPDIR:-/tmp}/routerd-sam-e2e-generate.XXXXXX")"
cleanup() {
  find "$work" -depth -delete
}
trap cleanup EXIT

fake_bin="$work/bin"
mkdir -p "$fake_bin"
cat >"$fake_bin/wg" <<'WG'
#!/usr/bin/env bash
set -euo pipefail

case "${1:-}" in
  genkey) printf '%s\n' 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=' ;;
  pubkey)
    cat >/dev/null
    printf '%s\n' 'BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB='
    ;;
  *) exit 2 ;;
esac
WG
chmod +x "$fake_bin/wg"

tofu_output="$work/tofu-output.json"
# PVE router management addresses must come from QGA-patched output. They are
# used only inside PVE-local peer overrides, never in the shared NodeSet or a
# cloud config.
jq -n '{
  nodes: {value: {
    "aws-leaf-a": {
      role: "leaf", site: "aws", overlay_ip: "10.99.0.11",
      private_ip: "10.77.60.4", public_ip: "203.0.113.11"
    },
    "aws-rr-a": {
      role: "rr", site: "aws", overlay_ip: "10.99.0.12",
      public_ip: "203.0.113.12"
    },
    "pve-rr-a": {
      role: "rr", site: "pve", overlay_ip: "10.99.0.21",
      management_ip: "192.0.2.21",
      pve_management_source: "qga-dhcp",
      wireguard_endpoint: "198.51.100.21:51820"
    },
    "pve-rr-b": {
      role: "rr", site: "pve", overlay_ip: "10.99.0.22",
      management_ip: "192.0.2.22", pve_management_source: "qga-dhcp"
    },
    "pve-leaf-a": {
      role: "leaf", site: "pve", overlay_ip: "10.99.0.31",
      private_ip: "10.77.60.31", management_ip: "192.0.2.31",
      pve_management_source: "qga-dhcp", capture_mac: "02:00:00:00:00:31"
    },
    "pve-leaf-b": {
      role: "leaf", site: "pve", overlay_ip: "10.99.0.32",
      private_ip: "10.77.60.32", management_ip: "192.0.2.32",
      pve_management_source: "qga-dhcp", capture_mac: "02:00:00:00:00:32"
    }
  }},
  fabric: {value: {
    mobility_prefix: "10.77.60.0/24", tunnel_inner_prefix: "10.99.0.0/24",
    bgp_asn: 64512, wg_port: 51820,
    aws: {region: "ap-northeast-1", leaf_subnet_id: "subnet-offline"}
  }}
}' >"$tofu_output"

out_dir="$work/generated"
PATH="$fake_bin:$PATH" "$generator" --tofu-output "$tofu_output" --out-dir "$out_dir" >/dev/null

# The release supervisor deliberately uses a root-owned checkout. Exercise
# Git's different-owner safeguard so the generator proves it trusts only the
# structural worktree root and still records reviewed provenance.
different_owner_out="$work/generated-different-owner"
GIT_TEST_ASSUME_DIFFERENT_OWNER=1 PATH="$fake_bin:$PATH" "$generator" \
  --tofu-output "$tofu_output" --out-dir "$different_owner_out" >/dev/null

die() {
  echo "sam-e2e generator offline test: $*" >&2
  exit 1
}

require_contains() {
  local file="$1" text="$2"
  grep -F -- "$text" "$file" >/dev/null || die "missing $text in $file"
}

require_absent() {
  local file="$1" text="$2"
  if grep -F -- "$text" "$file" >/dev/null; then
    die "unexpected $text in $file"
  fi
}

require_absent "$different_owner_out/configs/harness-version.txt" 'harness_git_head=unmanaged'
require_absent "$different_owner_out/configs/harness-version.txt" 'harness_git_dirty=unknown'

node_set="$out_dir/node-set.yaml"
require_contains "$node_set" 'endpoint: 203.0.113.11:51820'
require_absent "$node_set" '198.51.100.21:51820'
require_contains "$node_set" 'macAddresses: ["02:00:00:00:00:31"]'
require_contains "$node_set" 'macAddresses: ["02:00:00:00:00:32"]'
if ! awk '
  /^[[:space:]]*-[[:space:]]nodeRef:[[:space:]]*/ {
    pve = ($3 ~ /^pve-/)
    next
  }
  pve && /^[[:space:]]*endpoint:[[:space:]]*/ { bad = 1 }
  END { exit bad }
' "$node_set"; then
  die "shared SAMNodeSet contains a PVE WireGuard endpoint"
fi

pve_router_names=(pve-rr-a pve-rr-b pve-leaf-a pve-leaf-b)
declare -A pve_management_ips=(
  [pve-rr-a]=192.0.2.21
  [pve-rr-b]=192.0.2.22
  [pve-leaf-a]=192.0.2.31
  [pve-leaf-b]=192.0.2.32
)

for node in "${pve_router_names[@]}"; do
  config="$out_dir/configs/$node.yaml"
  for peer in "${pve_router_names[@]}"; do
    [ "$peer" = "$node" ] && continue
    require_contains "$config" "metadata: { name: $peer }"
    require_contains "$config" "endpoint: ${pve_management_ips[$peer]}:51820"
  done
  forbidden_kind="$(awk '
    /^[[:space:]]*kind:[[:space:]]*/ {
      value = $0
      sub(/^[[:space:]]*kind:[[:space:]]*/, "", value)
      sub(/[[:space:]]*(#.*)?$/, "", value)
      quote = substr(value, 1, 1)
      if ((quote == "\"" || quote == sprintf("%c", 39)) &&
          substr(value, length(value), 1) == quote) {
        value = substr(value, 2, length(value) - 2)
      }
      if (value ~ /^DHCPv4[[:alnum:]]*$/ ||
          value ~ /^DHCPv6[[:alnum:]]*$/ ||
          value == "IPv6RAAddress" ||
          value == "IPv6RouterAdvertisement" ||
          value == "RouterAdvertisement") {
        print value
        exit
      }
    }
  ' "$config")"
  [ -z "$forbidden_kind" ] || die "generated PVE router $node contains forbidden $forbidden_kind"
done

# Only PVE leaves own the isolated capture NIC; the host-redundant RR pair
# remains underlay-only. The default must match the Ubuntu template's ens19
# NIC rather than the retired eth1 convention.
for node in pve-leaf-a pve-leaf-b; do
  require_contains "$out_dir/configs/$node.yaml" 'ifname: ens19'
done

cloud_config="$out_dir/configs/aws-leaf-a.yaml"
cloud_rr_config="$out_dir/configs/aws-rr-a.yaml"
for management_ip in "${pve_management_ips[@]}"; do
  require_absent "$cloud_config" "$management_ip"
  require_absent "$cloud_rr_config" "$management_ip"
done
if rg -n 'wireguard_endpoint|wireGuardEndpoint' "$node_set" "$out_dir/configs" >/dev/null; then
  die "legacy PVE WireGuard endpoint contract leaked into generated output"
fi
if rg -F '198.51.100.21:51820' "$node_set" "$out_dir/configs" >/dev/null; then
  die "legacy PVE RR endpoint leaked into generated output"
fi

# A raw or hand-edited management IP is not a valid topology input. The
# certification driver must have completed QGA discovery for every PVE router
# before the generator can create any local bootstrap peer.
unverified_output="$work/tofu-output-unverified.json"
jq 'del(.nodes.value["pve-rr-b"].pve_management_source)' "$tofu_output" >"$unverified_output"
if PATH="$fake_bin:$PATH" "$generator" --tofu-output "$unverified_output" --out-dir "$work/unverified" >"$work/unverified.stdout" 2>"$work/unverified.stderr"; then
  die "generator accepted a PVE management address without QGA provenance"
fi
require_contains "$work/unverified.stderr" 'pve_management_source=qga-dhcp'

missing_capture_mac_output="$work/tofu-output-missing-capture-mac.json"
jq 'del(.nodes.value["pve-leaf-a"].capture_mac)' "$tofu_output" >"$missing_capture_mac_output"
if PATH="$fake_bin:$PATH" "$generator" --tofu-output "$missing_capture_mac_output" --out-dir "$work/missing-capture-mac" >"$work/missing-capture-mac.stdout" 2>"$work/missing-capture-mac.stderr"; then
  die "generator accepted a PVE leaf without a QGA capture MAC"
fi
require_contains "$work/missing-capture-mac.stderr" 'requires a QGA-derived capture_mac'

invalid_output="$work/tofu-output-invalid-management.json"
jq '.nodes.value["pve-rr-b"].management_ip = "224.0.0.1"' "$tofu_output" >"$invalid_output"
if PATH="$fake_bin:$PATH" "$generator" --tofu-output "$invalid_output" --out-dir "$work/invalid-management" >"$work/invalid-management.stdout" 2>"$work/invalid-management.stderr"; then
  die "generator accepted a non-unicast PVE management address"
fi
require_contains "$work/invalid-management.stderr" 'must be a unicast IPv4 address'

echo "sam-e2e PVE-local WireGuard bootstrap offline test passed"
