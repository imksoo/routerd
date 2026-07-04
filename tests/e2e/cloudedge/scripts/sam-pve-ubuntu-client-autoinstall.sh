#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: sam-pve-ubuntu-client-autoinstall.sh --asset-dir DIR --ssh-public-key-file FILE [options]

Generate NoCloud CIDATA media for ordinary Ubuntu PVE client VMs. These clients
are workload VMs, not routerd live ISO nodes: ens18 uses DHCP for management,
and ens19 is assigned the fixed overlay/client address used by the SAM matrix.

Options:
  --asset-dir DIR         Output directory for per-client CIDATA ISO assets.
  --ssh-public-key KEY    SSH public key for the ubuntu user.
  --ssh-public-key-file   File containing the SSH public key.
  --topology-scale NAME   single or full (default: full).
  --username NAME         Installed login user (default: ubuntu).
  --password-hash HASH    Password hash for autoinstall identity (required).
  --management-ifname IF  DHCP management interface (default: ens18).
  --overlay-ifname IF     Static overlay/client interface (default: ens19).
  --packages LIST         Comma-separated extra packages (default: qemu-guest-agent).
  --install-proxy URL     Installer apt/snapd proxy/cache URL (default: http://cache.lain.local:3142/).
USAGE
}

asset_dir=
ssh_public_key=
topology_scale=full
username=ubuntu
password_hash=
management_ifname=ens18
overlay_ifname=ens19
packages_csv=qemu-guest-agent
install_proxy=http://cache.lain.local:3142/

while [ "$#" -gt 0 ]; do
  case "$1" in
    --asset-dir) asset_dir=${2:?missing --asset-dir value}; shift 2 ;;
    --ssh-public-key) ssh_public_key=${2:?missing --ssh-public-key value}; shift 2 ;;
    --ssh-public-key-file) ssh_public_key=$(sed -n '1p' "${2:?missing --ssh-public-key-file value}"); shift 2 ;;
    --topology-scale) topology_scale=${2:?missing --topology-scale value}; shift 2 ;;
    --username) username=${2:?missing --username value}; shift 2 ;;
    --password-hash) password_hash=${2:?missing --password-hash value}; shift 2 ;;
    --management-ifname) management_ifname=${2:?missing --management-ifname value}; shift 2 ;;
    --overlay-ifname) overlay_ifname=${2:?missing --overlay-ifname value}; shift 2 ;;
    --packages) packages_csv=${2:?missing --packages value}; shift 2 ;;
    --install-proxy|--apt-http-proxy) install_proxy=${2:?missing --install-proxy value}; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$asset_dir" ] || { usage >&2; exit 2; }
[ -n "$ssh_public_key" ] || { echo "--ssh-public-key or --ssh-public-key-file is required" >&2; exit 2; }
[ -n "$password_hash" ] || { echo "--password-hash is required" >&2; exit 2; }
case "$topology_scale" in
  single|full) ;;
  *) echo "topology-scale must be single or full" >&2; exit 2 ;;
esac
command -v xorriso >/dev/null || { echo "xorriso is required" >&2; exit 2; }

mkdir -p "$asset_dir"
nodes=(pve-client-a:10.77.60.15/24)
[ "$topology_scale" = single ] || nodes+=(pve-client-b:10.77.60.19/24)

for entry in "${nodes[@]}"; do
  IFS=: read -r node overlay_cidr <<<"$entry"
  work="$asset_dir/$node"
  rm -rf "$work"
  mkdir -p "$work/cidata"
  cat >"$work/cidata/meta-data" <<EOF
instance-id: routerd-sam-$node
local-hostname: $node
EOF
  cat >"$work/cidata/user-data" <<EOF
#cloud-config
autoinstall:
  version: 1
  refresh-installer:
    update: false
  locale: en_US.UTF-8
  keyboard:
    layout: us
  identity:
    hostname: $node
    username: $username
    password: '$password_hash'
  ssh:
    install-server: true
    allow-pw: false
    authorized-keys:
      - $ssh_public_key
  storage:
    layout:
      name: direct
  proxy: $install_proxy
  apt:
    geoip: false
  early-commands:
    - mkdir -p /etc/systemd/resolved.conf.d
    - printf '[Resolve]\nDNSStubListener=no\n' >/etc/systemd/resolved.conf.d/routerd-no-stub.conf
    - systemctl restart systemd-resolved || true
    - ln -sf /run/systemd/resolve/resolv.conf /etc/resolv.conf || true
  network:
    version: 2
    ethernets:
      $management_ifname:
        dhcp4: true
      $overlay_ifname:
        dhcp4: false
        addresses:
          - $overlay_cidr
  packages:
EOF
  IFS=, read -r -a packages <<<"$packages_csv"
  for package in "${packages[@]}"; do
    package=${package//[[:space:]]/}
    [ -n "$package" ] || continue
    printf '    - %s\n' "$package" >>"$work/cidata/user-data"
  done
  cat >>"$work/cidata/user-data" <<EOF
  late-commands:
    - curtin in-target --target=/target -- mkdir -p /etc/apt/apt.conf.d /etc/systemd/resolved.conf.d
    - curtin in-target --target=/target -- sh -c "printf 'Acquire::http::Proxy \\"$install_proxy\\";\\n' >/etc/apt/apt.conf.d/99-routerd-apt-cache"
    - curtin in-target --target=/target -- sh -c "printf '[Resolve]\\nDNSStubListener=no\\n' >/etc/systemd/resolved.conf.d/routerd-no-stub.conf"
    - curtin in-target --target=/target -- sh -c "printf '$username ALL=(ALL) NOPASSWD:ALL\\n' >/etc/sudoers.d/90-routerd-$username && chmod 0440 /etc/sudoers.d/90-routerd-$username"
    - curtin in-target --target=/target -- ln -sf /run/systemd/resolve/resolv.conf /etc/resolv.conf
    - curtin in-target --target=/target -- systemctl enable qemu-guest-agent.service
    - curtin in-target --target=/target -- systemctl enable ssh.service
  shutdown: poweroff
EOF
  xorriso -as mkisofs -quiet -output "$asset_dir/routerd-$node-ubuntu-autoinstall-cidata.iso" -volid CIDATA -joliet -rock "$work/cidata"
done

sha256sum "$asset_dir"/*.iso >"$asset_dir/SHA256SUMS"
