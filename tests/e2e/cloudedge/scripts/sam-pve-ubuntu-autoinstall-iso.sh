#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: sam-pve-ubuntu-autoinstall-iso.sh --source-iso ISO --out ISO

Create a generic Ubuntu Server ISO that boots directly into autoinstall and
uses a separate NoCloud CIDATA device for per-VM answers. Use this as the
primary ISO for reusable PVE clients; attach the per-client CIDATA ISO on a
second CD-ROM.
USAGE
}

source_iso=
out=

while [ "$#" -gt 0 ]; do
  case "$1" in
    --source-iso) source_iso=${2:?missing --source-iso value}; shift 2 ;;
    --out) out=${2:?missing --out value}; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$source_iso" ] || { usage >&2; exit 2; }
[ -n "$out" ] || { usage >&2; exit 2; }
[ -f "$source_iso" ] || { echo "source ISO not found: $source_iso" >&2; exit 2; }
command -v xorriso >/dev/null || { echo "xorriso is required" >&2; exit 2; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

mkdir -p "$work/boot/grub"
cat >"$work/boot/grub/grub.cfg" <<'EOF'
set timeout=1

loadfont unicode

set menu_color_normal=white/black
set menu_color_highlight=black/light-gray

menuentry "Autoinstall Ubuntu Server for routerd SAM PVE client" {
	set gfxpayload=keep
	linux	/casper/vmlinuz autoinstall ds=nocloud ---
	initrd	/casper/initrd
}
EOF

xorriso \
  -indev "$source_iso" \
  -outdev "$out" \
  -boot_image any replay \
  -map "$work/boot/grub/grub.cfg" /boot/grub/grub.cfg \
  -volid UBUNTU_ROUTERD_AUTOINSTALL
