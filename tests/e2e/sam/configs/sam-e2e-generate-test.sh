#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
generator="$script_dir/sam-e2e-generate.sh"

command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin"
cat >"$tmp/bin/wg" <<'WG'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  genkey)
    printf 'test-private-key-%s\n' "$(date +%s%N)"
    ;;
  pubkey)
    key="$(cat)"
    printf 'test-public-for-%s\n' "$key"
    ;;
  *)
    echo "unsupported fake wg command: ${1:-}" >&2
    exit 2
    ;;
esac
WG
chmod +x "$tmp/bin/wg"
export PATH="$tmp/bin:$PATH"

tofu_output="$tmp/tofu-output.json"
cat >"$tofu_output" <<'JSON'
{
  "nodes": {
    "value": {
      "aws-rr-a": {
        "role": "rr",
        "site": "aws",
        "overlay_ip": "10.99.0.1",
        "private_ip": "10.10.0.10",
        "public_ip": "198.51.100.10"
      },
      "oci-leaf-a": {
        "role": "leaf",
        "site": "oci",
        "overlay_ip": "10.99.0.4",
        "private_ip": "10.77.60.24",
        "public_ip": "203.0.113.24"
      },
      "oci-leaf-b": {
        "role": "leaf",
        "site": "oci",
        "overlay_ip": "10.99.0.9",
        "private_ip": "10.77.60.25",
        "public_ip": "203.0.113.25"
      },
      "pve-leaf-b": {
        "role": "leaf",
        "site": "pve",
        "overlay_ip": "10.99.0.10",
        "private_ip": "192.0.2.10",
        "public_ip": "192.0.2.10"
      }
    }
  },
  "fabric": {
    "value": {
      "mobility_prefix": "10.77.60.0/24",
      "tunnel_inner_prefix": "10.255.0.0/24",
      "bgp_asn": 64577,
      "wg_port": 51820,
      "oci": {
        "region": "ap-tokyo-1",
        "compartment_id": "ocid1.compartment.oc1..routerdlab",
        "subnet_id": "ocid1.subnet.oc1.ap-tokyo-1.routerdlab"
      }
    }
  }
}
JSON

first="$tmp/first"
second="$tmp/second"
"$generator" --tofu-output "$tofu_output" --out-dir "$first" >/dev/null
"$generator" --tofu-output "$tofu_output" --out-dir "$second" --secrets-dir "$first/secrets" >/dev/null

assert_grep() {
  local pattern="$1" file="$2"
  if ! grep -qE "$pattern" "$file"; then
    echo "missing pattern $pattern in $file" >&2
    exit 1
  fi
}

assert_node_endpoint() {
  local node_ref="$1" endpoint="$2" file="$3"
  awk -v node_ref="$node_ref" -v endpoint="$endpoint" '
    $0 ~ "nodeRef: " node_ref "$" { in_node = 1; next }
    in_node && /nodeRef:/ { in_node = 0 }
    in_node && index($0, "endpoint: " endpoint) > 0 { found = 1 }
    END { exit found ? 0 : 1 }
  ' "$file" || {
    echo "missing endpoint $endpoint for $node_ref in $file" >&2
    exit 1
  }
}

for node in aws-rr-a oci-leaf-a oci-leaf-b pve-leaf-b; do
  cfg="$second/configs/$node.yaml"
  [ -f "$cfg" ] || { echo "missing generated config: $cfg" >&2; exit 1; }
  assert_grep "^  name: $node$" "$cfg"
  assert_grep "nodeName: $node$" "$cfg"
  assert_grep "selfNodeRef: $node$" "$cfg"
done

assert_node_endpoint oci-leaf-a "10.77.60.24:51820" "$second/configs/oci-leaf-b.yaml"
assert_node_endpoint oci-leaf-b "10.77.60.25:51820" "$second/configs/oci-leaf-a.yaml"
assert_node_endpoint aws-rr-a "198.51.100.10:51820" "$second/configs/oci-leaf-a.yaml"

for node in aws-rr-a oci-leaf-a oci-leaf-b pve-leaf-b; do
  if ! cmp -s "$first/secrets/$node.wg.key" "$second/secrets/$node.wg.key"; then
    echo "WireGuard key changed despite --secrets-dir: $node" >&2
    exit 1
  fi
done
if ! cmp -s "$first/secrets/eventd-cloudedge.key" "$second/secrets/eventd-cloudedge.key"; then
  echo "event secret changed despite --secrets-dir" >&2
  exit 1
fi

echo "sam-e2e-generate-test PASS"
