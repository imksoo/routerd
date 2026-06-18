#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause
set -euo pipefail

version=${VERSION:-$(awk '/^VERSION[[:space:]]*\?=/{print $3; exit}' Makefile)}
git_commit=${GIT_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || true)}
distbase=${DISTBASE:-dist}
workdir=${ROUTERD_LIVE_WORKDIR:-"${distbase}/live/work"}
outdir=${ROUTERD_LIVE_OUTDIR:-"${distbase}/iso"}
ubuntu_suite=${UBUNTU_SUITE:-noble}
ubuntu_mirror=${UBUNTU_MIRROR:-http://archive.ubuntu.com/ubuntu}
ubuntu_arch=${UBUNTU_ARCH:-amd64}
ubuntu_base_packages=${UBUNTU_BASE_PACKAGES:-"linux-image-generic systemd-sysv dbus sudo casper initramfs-tools"}
ubuntu_packages=${UBUNTU_LIVE_PACKAGES:-"ca-certificates curl dnsmasq-base nftables wireguard-tools chrony bind9-dnsutils tcpdump cron jq ppp pppoe conntrack iproute2 iputils-ping iputils-tracepath net-tools kmod radvd strongswan-swanctl iptables keepalived openssh-server"}
read -r -a ubuntu_base_package_list <<< "${ubuntu_base_packages}"
read -r -a ubuntu_package_list <<< "${ubuntu_packages}"

require()
{
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "missing live ISO build dependency: $1" >&2
        exit 2
    fi
}

run_root()
{
    if [ "$(id -u)" -eq 0 ]; then
        "$@"
    else
        sudo "$@"
    fi
}

chroot_run()
{
    run_root chroot "${rootfs}" /usr/bin/env DEBIAN_FRONTEND=noninteractive "$@"
}

checksum_file()
{
    file=$1
    if command -v sha256sum >/dev/null 2>&1; then
        (cd "$(dirname "${file}")" && sha256sum "$(basename "${file}")" > "$(basename "${file}").sha256")
    elif command -v shasum >/dev/null 2>&1; then
        (cd "$(dirname "${file}")" && shasum -a 256 "$(basename "${file}")" > "$(basename "${file}").sha256")
    else
        echo "missing sha256 tool" >&2
        exit 2
    fi
}

cleanup_mounts()
{
    if [ -n "${rootfs:-}" ] && [ -d "${rootfs}" ]; then
        for mountpoint in dev/pts dev proc sys run; do
            if mountpoint -q "${rootfs}/${mountpoint}" 2>/dev/null; then
                run_root umount -lf "${rootfs}/${mountpoint}" || true
            fi
        done
    fi
}

require debootstrap
require mksquashfs
require grub-mkrescue
require xorriso

trap cleanup_mounts EXIT INT TERM

if [ -d "${workdir}" ]; then
    chmod -R u+w "${workdir}" 2>/dev/null || true
fi
rm -rf "${workdir}"
mkdir -p "${workdir}" "${outdir}"

iso_root="${workdir}/iso-root"
rootfs="${workdir}/rootfs"
payload_root="${iso_root}/routerd"
mkdir -p "${iso_root}/casper" "${iso_root}/boot/grub" "${payload_root}"

run_root debootstrap --variant=minbase --arch="${ubuntu_arch}" "${ubuntu_suite}" "${rootfs}" "${ubuntu_mirror}"
run_root chown -R "$(id -u):$(id -g)" "${rootfs}"

install -d "${rootfs}/etc/apt/apt.conf.d"
cat > "${rootfs}/etc/apt/apt.conf.d/99routerd-live" <<'EOF'
APT::Install-Recommends "false";
APT::Install-Suggests "false";
DPkg::Options {
  "--force-confdef";
  "--force-confold";
};
EOF
printf 'routerd-live\n' > "${rootfs}/etc/hostname"
cat > "${rootfs}/etc/apt/sources.list.d/ubuntu.sources" <<EOF
Types: deb
URIs: ${ubuntu_mirror}
Suites: ${ubuntu_suite} ${ubuntu_suite}-updates ${ubuntu_suite}-security
Components: main restricted universe
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg
EOF
rm -f "${rootfs}/etc/resolv.conf"
cp /etc/resolv.conf "${rootfs}/etc/resolv.conf"

for dir in dev proc sys run; do
    run_root mount --bind "/${dir}" "${rootfs}/${dir}"
done
if [ -d "${rootfs}/dev/pts" ]; then
    run_root mount --bind /dev/pts "${rootfs}/dev/pts"
fi

chroot_run apt-get update
chroot_run apt-get install -y --no-install-recommends "${ubuntu_base_package_list[@]}" "${ubuntu_package_list[@]}"
chroot_run apt-get clean
run_root rm -rf "${rootfs}/var/lib/apt/lists/"*
cleanup_mounts

make build-daemons ROUTERD_OS=linux GOARCH=amd64 GIT_COMMIT="${git_commit}"

install -d "${payload_root}/bin" "${payload_root}/etc/routerd" "${payload_root}/share/licenses/routerd"
for binary in bin/linux-amd64/*; do
    [ -f "${binary}" ] || continue
    install -m 0755 "${binary}" "${payload_root}/bin/$(basename "${binary}")"
done
install -m 0755 packaging/install.sh "${payload_root}/install.sh"
install -m 0755 packaging/uninstall.sh "${payload_root}/uninstall.sh"
install -m 0644 examples/router-lab.yaml "${payload_root}/etc/routerd/router.yaml.sample"
install -m 0644 LICENSE "${payload_root}/share/licenses/routerd/LICENSE"
if [ -f THIRD_PARTY_LICENSES.md ]; then
    install -m 0644 THIRD_PARTY_LICENSES.md "${payload_root}/share/licenses/routerd/THIRD_PARTY_LICENSES.txt"
fi

install -d "${rootfs}/opt/routerd-live" "${rootfs}/usr/local/sbin" "${rootfs}/usr/local/etc/routerd" "${rootfs}/usr/local/share/doc/routerd"
cp -a "${payload_root}/." "${rootfs}/opt/routerd-live/"
cp -a "${payload_root}/bin/." "${rootfs}/usr/local/sbin/"
install -m 0644 "${payload_root}/etc/routerd/router.yaml.sample" "${rootfs}/usr/local/etc/routerd/router.yaml.sample"
install -m 0644 "${payload_root}/share/licenses/routerd/LICENSE" "${rootfs}/usr/local/share/doc/routerd/LICENSE"
if [ -f "${payload_root}/share/licenses/routerd/THIRD_PARTY_LICENSES.txt" ]; then
    install -m 0644 "${payload_root}/share/licenses/routerd/THIRD_PARTY_LICENSES.txt" "${rootfs}/usr/local/share/doc/routerd/THIRD_PARTY_LICENSES.txt"
fi

cat > "${payload_root}/README.txt" <<EOF
routerd live payload ${version}

This debootstrap-based Ubuntu live image carries routerd binaries and installer
assets under /cdrom/routerd after boot.

Suggested first steps:
  sudo /cdrom/routerd/install.sh --prefix /usr/local --no-start-service
  sudo /cdrom/routerd/install.sh configure

For a persistent router, install Ubuntu Server to disk and then run the
routerd installer from the release payload.
EOF

cat > "${rootfs}/opt/routerd-live/firstboot.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

install -d /run/routerd /var/lib/routerd /usr/local/etc/routerd
if [ ! -f /usr/local/etc/routerd/router.yaml ] && [ -f /usr/local/etc/routerd/router.yaml.sample ]; then
    cp /usr/local/etc/routerd/router.yaml.sample /usr/local/etc/routerd/router.yaml
fi

if [ -x /usr/local/sbin/routerd ]; then
    systemctl enable routerd.service >/dev/null 2>&1 || true
fi
if [ -x /usr/local/sbin/routerd-dns-resolver ]; then
    systemctl enable routerd-dns-resolver@lan-resolver.service >/dev/null 2>&1 || true
fi
EOF
chmod 0755 "${rootfs}/opt/routerd-live/firstboot.sh"

install -d "${rootfs}/etc/systemd/system" "${rootfs}/etc/systemd/system/multi-user.target.wants"
cat > "${rootfs}/etc/systemd/system/routerd-live-setup.service" <<'EOF'
[Unit]
Description=Prepare routerd live runtime
After=local-fs.target
Before=routerd.service
ConditionPathExists=/opt/routerd-live/firstboot.sh

[Service]
Type=oneshot
ExecStart=/opt/routerd-live/firstboot.sh
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF
ln -sf ../routerd-live-setup.service "${rootfs}/etc/systemd/system/multi-user.target.wants/routerd-live-setup.service"

printf '%s\n' "${version}" > "${rootfs}/etc/routerd-live-version"
printf '%s\n' "${git_commit:-unknown}" > "${rootfs}/etc/routerd-live-commit"
: > "${rootfs}/etc/machine-id"

kernel_image=$(find "${rootfs}/boot" -maxdepth 1 -type f -name 'vmlinuz-*' | sort -V | tail -n 1)
initrd_image=$(find "${rootfs}/boot" -maxdepth 1 -type f -name 'initrd.img-*' | sort -V | tail -n 1)
if [ -z "${kernel_image}" ] || [ -z "${initrd_image}" ]; then
    echo "missing kernel or initrd in ${rootfs}/boot" >&2
    exit 1
fi
install -m 0644 "${kernel_image}" "${iso_root}/casper/vmlinuz"
install -m 0644 "${initrd_image}" "${iso_root}/casper/initrd"

# shellcheck disable=SC2016 # dpkg-query expands this format string inside the chroot.
chroot_run dpkg-query -W --showformat='${Package} ${Version}\n' > "${iso_root}/casper/filesystem.manifest"
printf '%s' "$(du -sx --block-size=1 "${rootfs}" | awk '{print $1}')" > "${iso_root}/casper/filesystem.size"
run_root mksquashfs "${rootfs}" "${iso_root}/casper/filesystem.squashfs" -noappend -comp xz -all-root >/dev/null
(cd "${iso_root}" && find . -type f ! -name md5sum.txt -print0 | sort -z | xargs -0 md5sum > md5sum.txt.new && mv md5sum.txt.new md5sum.txt)

cat > "${iso_root}/boot/grub/grub.cfg" <<EOF
serial --unit=0 --speed=115200 --word=8 --parity=no --stop=1
terminal_input console serial
terminal_output console serial
set timeout=5
set default=0

menuentry "routerd Ubuntu live ${version}" {
    linux /casper/vmlinuz boot=casper quiet console=tty0 console=ttyS0,115200n8 ---
    initrd /casper/initrd
}
EOF

out_iso="${outdir}/routerd-live-${version}.iso"
alias_iso="${outdir}/routerd-live.iso"
rm -f "${out_iso}" "${out_iso}.sha256" "${alias_iso}" "${alias_iso}.sha256"
grub-mkrescue -o "${out_iso}" "${iso_root}" >/dev/null
cp "${out_iso}" "${alias_iso}"
checksum_file "${out_iso}"
checksum_file "${alias_iso}"
echo "wrote ${out_iso}"
