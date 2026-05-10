#!/usr/bin/env bash
set -euo pipefail

version=${VERSION:-$(awk '/^VERSION[[:space:]]*\\?=/{print $3; exit}' Makefile)}
distbase=${DISTBASE:-dist}
workdir=${ROUTERD_LIVE_WORKDIR:-"${distbase}/live/work"}
cachedir=${ROUTERD_LIVE_CACHEDIR:-"${distbase}/live/cache"}
outdir=${ROUTERD_LIVE_OUTDIR:-"${distbase}/iso"}
alpine_mirror=${ALPINE_MIRROR:-https://dl-cdn.alpinelinux.org/alpine}
alpine_branch=${ALPINE_BRANCH:-latest-stable}
alpine_arch=${ALPINE_ARCH:-x86_64}
alpine_iso_url=${ALPINE_ISO_URL:-}

require()
{
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "missing live ISO build dependency: $1" >&2
        exit 2
    fi
}

require curl
require bsdtar
require grub-mkrescue
require xorriso
require tar

if [ -d "${workdir}" ]; then
    chmod -R u+w "${workdir}" 2>/dev/null || true
fi
rm -rf "${workdir}"
mkdir -p "${workdir}" "${cachedir}" "${outdir}"

if [ -z "${alpine_iso_url}" ]; then
    releases="${cachedir}/latest-releases-${alpine_arch}.yaml"
    curl -fsSL "${alpine_mirror}/${alpine_branch}/releases/${alpine_arch}/latest-releases.yaml" -o "${releases}"
    alpine_iso_file=$(awk '/file: alpine-standard-.*-'"${alpine_arch}"'\.iso/ {print $2; exit}' "${releases}")
    if [ -z "${alpine_iso_file}" ]; then
        echo "could not resolve alpine standard ISO from ${releases}" >&2
        exit 2
    fi
    alpine_iso_url="${alpine_mirror}/${alpine_branch}/releases/${alpine_arch}/${alpine_iso_file}"
else
    alpine_iso_file=$(basename "${alpine_iso_url}")
fi

alpine_iso="${cachedir}/${alpine_iso_file}"
if [ ! -f "${alpine_iso}" ]; then
    curl -fL "${alpine_iso_url}" -o "${alpine_iso}"
fi

iso_root="${workdir}/iso-root"
overlay_root="${workdir}/overlay"
mkdir -p "${iso_root}" "${overlay_root}"
bsdtar -C "${iso_root}" -xf "${alpine_iso}"
chmod -R u+w "${iso_root}"
install -d "${iso_root}/boot/grub"

make build-daemons ROUTERD_OS=linux GOARCH=amd64

install -d "${overlay_root}/usr/local/sbin" \
    "${overlay_root}/usr/share/routerd" \
    "${overlay_root}/usr/share/routerd/dist" \
    "${overlay_root}/usr/local/etc/routerd" \
    "${overlay_root}/etc" \
    "${overlay_root}/etc/local.d" \
    "${overlay_root}/etc/runlevels/default" \
    "${overlay_root}/root"

for binary in bin/linux-amd64/*; do
    [ -f "${binary}" ] || continue
    install -m 0755 "${binary}" "${overlay_root}/usr/local/sbin/$(basename "${binary}")"
done
install -m 0755 packaging/install.sh "${overlay_root}/usr/share/routerd/install.sh"
install -m 0755 packaging/uninstall.sh "${overlay_root}/usr/share/routerd/uninstall.sh"
install -m 0644 examples/router-lab.yaml "${overlay_root}/usr/local/etc/routerd/router.yaml.sample"
: > "${overlay_root}/etc/.default_boot_services"

cat > "${overlay_root}/etc/inittab" <<'EOF'
::sysinit:/sbin/openrc sysinit
::sysinit:/sbin/openrc boot
::wait:/sbin/openrc default

tty1::respawn:/sbin/getty 38400 tty1
tty2::respawn:/sbin/getty 38400 tty2
ttyS0::respawn:/sbin/getty -L 115200 ttyS0 vt100

::ctrlaltdel:/sbin/reboot
::shutdown:/sbin/openrc shutdown
EOF

cat > "${overlay_root}/etc/securetty" <<'EOF'
tty1
tty2
ttyS0
EOF

cat > "${overlay_root}/etc/motd" <<EOF
routerd live ${version}

Run the setup wizard:
  /usr/share/routerd/install.sh configure

The wizard writes /usr/local/etc/routerd/router.yaml and can apply it.
For a persistent router, install routerd from the release archive onto disk.
EOF

cat > "${overlay_root}/root/.profile" <<'EOF'
echo
cat /etc/motd
echo
if [ ! -f /usr/local/etc/routerd/router.yaml ]; then
  if command -v udhcpc >/dev/null 2>&1; then
    for iface in $(ls /sys/class/net 2>/dev/null | grep -E '^(eth|en|ens)' || true); do
      ip link set "$iface" up 2>/dev/null || true
      udhcpc -q -n -t 2 -T 3 -i "$iface" && break
    done
  fi
  /usr/share/routerd/install.sh --deps-only || true
  echo "Starting routerd setup wizard. Press Ctrl+C to skip."
  /usr/share/routerd/install.sh configure || true
fi
EOF

cat > "${overlay_root}/etc/local.d/routerd-configure.start" <<'EOF'
#!/bin/sh
cat /etc/motd
EOF
chmod 0755 "${overlay_root}/etc/local.d/routerd-configure.start"
ln -s /etc/init.d/local "${overlay_root}/etc/runlevels/default/local"

tar -C "${overlay_root}" -czf "${iso_root}/routerd.apkovl.tar.gz" .

cat > "${iso_root}/boot/grub/grub.cfg" <<EOF
serial --unit=0 --speed=115200 --word=8 --parity=no --stop=1
terminal_input console serial
terminal_output console serial
set timeout=5
set default=0

menuentry "routerd live ${version}" {
    linux /boot/vmlinuz-lts modules=loop,squashfs,sd-mod,usb-storage,ext4,virtio,virtio_blk,virtio_net quiet alpine_dev=cdrom:iso9660 modloop=/boot/modloop-lts console=tty0 console=ttyS0,115200n8
    initrd /boot/initramfs-lts
}
EOF

iso_versioned="${outdir}/routerd-live-${version}.iso"
iso_alias="${outdir}/routerd-live.iso"
rm -f "${iso_versioned}" "${iso_versioned}.sha256" "${iso_alias}" "${iso_alias}.sha256"
grub-mkrescue -o "${iso_versioned}" "${iso_root}" >/dev/null
cp "${iso_versioned}" "${iso_alias}"
if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${iso_versioned}" > "${iso_versioned}.sha256"
    sha256sum "${iso_alias}" > "${iso_alias}.sha256"
else
    shasum -a 256 "${iso_versioned}" > "${iso_versioned}.sha256"
    shasum -a 256 "${iso_alias}" > "${iso_alias}.sha256"
fi

echo "${iso_versioned}"
