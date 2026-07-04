#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: sam-pve-liveiso-bootstrap.sh --out-dir DIR [options]

Generate minimal routerd bootstrap configs for PVE router/leaf live ISO nodes.
These configs only establish the isolated capture address before the normal SAM
E2E deploy step replaces router.yaml over SSH. Management addressing stays on
the live ISO DHCP path and is discovered through QGA.

Options:
  --out-dir DIR          Output directory for <node>.yaml files.
  --asset-dir DIR        Also write PVE attachable CIDATA/config ISO assets.
  --ssh-public-key KEY   SSH public key for CIDATA user-data.
  --ssh-public-key-file  File containing the SSH public key for CIDATA user-data.
  --topology-scale NAME  single or full (default: full).
  --capture-ifname NAME Live ISO capture interface (default: ens19).
USAGE
}

out_dir=
asset_dir=
ssh_public_key=
topology_scale=full
capture_ifname=ens19

while [ "$#" -gt 0 ]; do
  case "$1" in
    --out-dir)
      out_dir=${2:?missing --out-dir value}
      shift 2
      ;;
    --asset-dir)
      asset_dir=${2:?missing --asset-dir value}
      shift 2
      ;;
    --ssh-public-key)
      ssh_public_key=${2:?missing --ssh-public-key value}
      shift 2
      ;;
    --ssh-public-key-file)
      ssh_public_key=$(sed -n '1p' "${2:?missing --ssh-public-key-file value}")
      shift 2
      ;;
    --topology-scale)
      topology_scale=${2:?missing --topology-scale value}
      shift 2
      ;;
    --capture-ifname)
      capture_ifname=${2:?missing --capture-ifname value}
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

[ -n "$out_dir" ] || { usage >&2; exit 2; }
case "$topology_scale" in
  single|full) ;;
  *) echo "topology-scale must be single or full" >&2; exit 2 ;;
esac

mkdir -p "$out_dir"
if [ -n "$asset_dir" ]; then
  command -v xorriso >/dev/null || { echo "xorriso is required for --asset-dir" >&2; exit 2; }
  [ -n "$ssh_public_key" ] || { echo "--ssh-public-key or --ssh-public-key-file is required with --asset-dir" >&2; exit 2; }
  mkdir -p "$asset_dir"
fi

nodes=()

write_config() {
  local node=$1 capture_cidr=$2
  nodes+=("$node")
  cat >"$out_dir/${node}.yaml" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: ${node}
spec:
  reconcile:
    mode: progressive
    protectedInterfaces:
      - mgmt
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: { name: mgmt }
      spec:
        ifname: ens18
        adminUp: true
        managed: false
        owner: external
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv4Client
      metadata: { name: mgmt-dhcpv4 }
      spec:
        interface: mgmt
        useRoutes: true
        useDNS: true
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: { name: capture }
      spec:
        ifname: ${capture_ifname}
        adminUp: true
        managed: true
        owner: routerd
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticAddress
      metadata: { name: capture-ipv4 }
      spec:
        interface: capture
        address: ${capture_cidr}
EOF
}

write_assets() {
  local node work
  [ -n "$asset_dir" ] || return 0
  for node in "${nodes[@]}"; do
    work="$asset_dir/$node"
    rm -rf "$work"
    mkdir -p "$work/cidata" "$work/config"
    cat >"$work/cidata/user-data" <<EOF
#cloud-config
hostname: $node
ssh_authorized_keys:
  - $ssh_public_key
EOF
    cat >"$work/cidata/meta-data" <<EOF
instance-id: routerd-sam-$node
local-hostname: $node
EOF
    install -m 0600 "$out_dir/$node.yaml" "$work/config/router.yaml"
    xorriso -as mkisofs -quiet -output "$asset_dir/routerd-$node-hostname-only-cidata.iso" -volid CIDATA -joliet -rock "$work/cidata"
    xorriso -as mkisofs -quiet -output "$asset_dir/routerd-$node-config.iso" -volid ROUTERD_CONFIG -joliet -rock "$work/config"
  done
  sha256sum "$asset_dir"/*.iso >"$asset_dir/SHA256SUMS"
}

write_config pve-leaf-a 10.77.60.34/24

if [ "$topology_scale" = full ]; then
  write_config pve-leaf-b 10.77.60.35/24
fi

write_assets

cat >"$out_dir/README.txt" <<EOF
For HTTP bootstrap, serve this directory before tofu apply, then set:

  pve_bootstrap_config_base_url = "http://<server>:<port>"

The PVE Terraform module writes NoCloud user-data snippets that point each live
ISO VM at <base>/<node>.yaml. These bootstrap configs are intentionally minimal:
they do not configure ens18 or any management default route. Management IPs must
come from DHCP and be copied into tofu-output.json from QGA before sam-e2e.sh.

For the PVE live ISO + qnap path, prefer --asset-dir and upload the generated
ISOs to qnap:iso. Then set per-router pve_cloud_init_file_ids to
routerd-<node>-hostname-only-cidata.iso and pve_config_cdrom_file_ids to
routerd-<node>-config.iso. PVE clients are ordinary Ubuntu workload VMs; generate
their CIDATA media with sam-pve-ubuntu-client-autoinstall.sh instead of this
routerd live ISO helper. This path is VMID-independent and works with Terraform
VMID auto-assignment.
EOF
