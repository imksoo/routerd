#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: sam-pve-freebsd-bootstrap-iso.sh \
  --artifact FILE \
  --ssh-public-key FILE \
  --out-dir DIR \
  --node NAME:CAPTURE_CIDR [--node NAME:CAPTURE_CIDR ...]

Build one read-only bootstrap ISO per FreeBSD VM. The ISO installs an exact
qualification artifact, configures vtnet0 as the isolated capture interface,
enables DHCP on management interface vtnet1, and enables key-only root SSH.

The source VM and its disk are never modified by this generator. Mount an ISO
only on a stopped, disposable clone, boot the clone to a root shell, then run:

  mount -uw /
  mkdir -p /cdrom
  mount -t cd9660 -o ro /dev/cd0 /cdrom
  /bin/sh /cdrom/bootstrap.sh
  umount /cdrom
  reboot
EOF
}

artifact=
ssh_public_key=
out_dir=
declare -a nodes=()

while (($#)); do
  case "$1" in
    --artifact) artifact=${2:?missing --artifact value}; shift 2 ;;
    --ssh-public-key) ssh_public_key=${2:?missing --ssh-public-key value}; shift 2 ;;
    --out-dir) out_dir=${2:?missing --out-dir value}; shift 2 ;;
    --node) nodes+=("${2:?missing --node value}"); shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -f "$artifact" ] || { echo "artifact not found: $artifact" >&2; exit 2; }
[ -f "$ssh_public_key" ] || { echo "SSH public key not found: $ssh_public_key" >&2; exit 2; }
[ -n "$out_dir" ] || { echo "--out-dir is required" >&2; exit 2; }
((${#nodes[@]} > 0)) || { echo "at least one --node is required" >&2; exit 2; }
command -v xorriso >/dev/null || { echo "xorriso is required" >&2; exit 2; }
command -v sha256sum >/dev/null || { echo "sha256sum is required" >&2; exit 2; }
gzip -t "$artifact"

key=$(tr -d '\r\n' <"$ssh_public_key")
case "$key" in
  ssh-ed25519\ *|ssh-rsa\ *|ecdsa-sha2-*\ *) ;;
  *) echo "SSH public key must be one supported single-line public key" >&2; exit 2 ;;
esac

mkdir -p "$out_dir"
work=$(mktemp -d)
cleanup() {
  rm -rf -- "$work"
}
trap cleanup EXIT

artifact_sha=$(sha256sum "$artifact" | awk '{print $1}')
artifact_name=routerd-freebsd-amd64-qualification.tar.gz

for spec in "${nodes[@]}"; do
  name=${spec%%:*}
  capture_cidr=${spec#*:}
  if [ "$name" = "$spec" ]; then
    echo "invalid --node value (expected NAME:CAPTURE_CIDR): $spec" >&2
    exit 2
  fi
  case "$name" in
    *[!a-zA-Z0-9.-]*|'') echo "invalid node name: $name" >&2; exit 2 ;;
  esac
  if ! printf '%s\n' "$capture_cidr" |
      grep -Eq '^([0-9]{1,3}\.){3}[0-9]{1,3}/(8|9|[12][0-9]|3[0-2])$'; then
    echo "invalid IPv4 capture CIDR: $capture_cidr" >&2
    exit 2
  fi

  iso_root="$work/$name"
  mkdir -p "$iso_root"
  cp "$artifact" "$iso_root/$artifact_name"
  printf '%s  %s\n' "$artifact_sha" "$artifact_name" >"$iso_root/SHA256"
  {
    printf '%s\n' '#!/bin/sh' 'set -eu'
    printf 'node=%s\n' "$(printf '%q' "$name")"
    printf 'capture_cidr=%s\n' "$(printf '%q' "$capture_cidr")"
    printf 'artifact_sha=%s\n' "$(printf '%q' "$artifact_sha")"
    printf 'authorized_key=%s\n' "$(printf '%q' "$key")"
    cat <<'EOF'

test "$(id -u)" -eq 0
test -r "/cdrom/routerd-freebsd-amd64-qualification.tar.gz"
actual_sha=$(sha256 -q /cdrom/routerd-freebsd-amd64-qualification.tar.gz)
test "$actual_sha" = "$artifact_sha"

mount -uw /
install -d -m 0755 /usr/local/bin /usr/local/sbin
tar -xzf /cdrom/routerd-freebsd-amd64-qualification.tar.gz -C /usr/local
for daemon in /usr/local/bin/routerd /usr/local/bin/routerd-*; do
  test -f "$daemon" || continue
  install -m 0755 "$daemon" "/usr/local/sbin/${daemon##*/}"
done
test -x /usr/local/sbin/routerd
test -x /usr/local/sbin/routerd-bgp
test -x /usr/local/bin/routerctl

hostname "$node"
sysrc "hostname=$node"
sysrc "ifconfig_vtnet0=inet $capture_cidr"
sysrc 'ifconfig_vtnet1=DHCP'
sysrc gateway_enable=YES
sysrc sshd_enable=YES

install -d -m 0700 /root/.ssh
printf '%s\n' "$authorized_key" >/root/.ssh/authorized_keys
chmod 0600 /root/.ssh/authorized_keys
if grep -Eq '^[#[:space:]]*PermitRootLogin[[:space:]]' /etc/ssh/sshd_config; then
  sed -i '' -E \
    's|^[#[:space:]]*PermitRootLogin[[:space:]].*$|PermitRootLogin prohibit-password|' \
    /etc/ssh/sshd_config
else
  printf '%s\n' 'PermitRootLogin prohibit-password' >>/etc/ssh/sshd_config
fi
if grep -Eq '^[#[:space:]]*PasswordAuthentication[[:space:]]' /etc/ssh/sshd_config; then
  sed -i '' -E \
    's|^[#[:space:]]*PasswordAuthentication[[:space:]].*$|PasswordAuthentication no|' \
    /etc/ssh/sshd_config
else
  printf '%s\n' 'PasswordAuthentication no' >>/etc/ssh/sshd_config
fi

install -d -m 0755 /var/db/routerd-qualification
{
  printf 'node=%s\n' "$node"
  printf 'capture_cidr=%s\n' "$capture_cidr"
  printf 'artifact_sha256=%s\n' "$artifact_sha"
  printf 'installed_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
} >/var/db/routerd-qualification/bootstrap.env
sync
printf 'routerd-freebsd-bootstrap=ok node=%s capture=%s sha256=%s\n' \
  "$node" "$capture_cidr" "$artifact_sha"
EOF
  } >"$iso_root/bootstrap.sh"
  chmod 0755 "$iso_root/bootstrap.sh"

  iso="$out_dir/routerd-$name-freebsd-bootstrap.iso"
  xorriso -as mkisofs -quiet -output "$iso" -volid ROUTERD_BOOT -joliet -rock "$iso_root"
done

sha256sum "$out_dir"/routerd-*-freebsd-bootstrap.iso >"$out_dir/SHA256SUMS"
printf 'artifact_sha256=%s\n' "$artifact_sha"
printf 'wrote=%s\n' "$out_dir"
