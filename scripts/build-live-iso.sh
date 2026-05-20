#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause
set -euo pipefail

version=${VERSION:-$(awk '/^VERSION[[:space:]]*\\?=/{print $3; exit}' Makefile)}
git_commit=${GIT_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || true)}
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

make build-daemons ROUTERD_OS=linux GOARCH=amd64 GIT_COMMIT="${git_commit}"

install -d "${overlay_root}/usr/local/sbin" \
    "${overlay_root}/usr/share/routerd" \
    "${overlay_root}/usr/share/routerd/dist" \
    "${overlay_root}/usr/share/licenses/routerd" \
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
install -m 0644 LICENSE "${overlay_root}/usr/share/licenses/routerd/LICENSE"
if [ -f THIRD_PARTY_LICENSES.md ]; then
    install -m 0644 THIRD_PARTY_LICENSES.md "${overlay_root}/usr/share/licenses/routerd/THIRD_PARTY_LICENSES.txt"
fi
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

cat > "${overlay_root}/usr/share/routerd/live-persistence.sh" <<'EOF'
#!/bin/sh
set -eu

state_dir=/run/routerd/live
mount_dir=/media/routerd-usb
config_file=/usr/local/etc/routerd/router.yaml
usb_state_file=/run/routerd/live/usb-device
flush_enabled_file=/run/routerd/live/usb-flush-enabled
log_limit_file=/run/routerd/live/log-limit
log_dir=/run/routerd/logs
persist_dir_name=routerd

mkdir -p "${state_dir}"

log()
{
    echo "routerd-live: $*"
}

mounted_source()
{
    awk -v target="${mount_dir}" '$2 == target {print $1; exit}' /proc/mounts 2>/dev/null || true
}

usb_mounted()
{
    grep -q " ${mount_dir} " /proc/mounts 2>/dev/null
}

warn_if_usb_removed()
{
    src=$(mounted_source)
    [ -n "${src}" ] || return 1
    if [ ! -b "${src}" ]; then
        log "warning: USB persistence device ${src} is no longer present; keeping runtime state in RAM"
        return 1
    fi
    return 0
}

cmdline_value()
{
    key=$1
    for item in $(cat /proc/cmdline 2>/dev/null || true); do
        case "${item}" in
            "${key}="*)
                printf '%s\n' "${item#*=}"
                return 0
                ;;
        esac
    done
    return 1
}

normalize_limit_bytes()
{
    value=${1:-100M}
    case "${value}" in
        *[mM])
            n=${value%[mM]}
            echo $((n * 1024 * 1024))
            ;;
        *[kK])
            n=${value%[kK]}
            echo $((n * 1024))
            ;;
        *[gG])
            n=${value%[gG]}
            echo $((n * 1024 * 1024 * 1024))
            ;;
        ''|*[!0-9]*)
            echo 104857600
            ;;
        *)
            echo "${value}"
            ;;
    esac
}

normalize_limit_kbytes()
{
    bytes=$(normalize_limit_bytes "${1:-100M}")
    echo $(((bytes + 1023) / 1024))
}

ensure_log_tmpfs()
{
    limit=${1:-100M}
    mkdir -p "${log_dir}"
    limit_kbytes=$(normalize_limit_kbytes "${limit}")
    if command -v mountpoint >/dev/null 2>&1 && mountpoint -q "${log_dir}"; then
        :
    elif grep -q " ${log_dir} " /proc/mounts 2>/dev/null; then
        :
    else
        mount -t tmpfs -o "mode=0755,size=${limit}" tmpfs "${log_dir}" 2>/dev/null || true
    fi
    printf '%s\n' "${limit}" > "${log_limit_file}"
    while :; do
        used=$(du -sk "${log_dir}" 2>/dev/null | awk '{print $1}')
        [ -n "${used}" ] || used=0
        [ "${used}" -le "${limit_kbytes}" ] && break
        oldest=$(find "${log_dir}" -type f -exec ls -tr {} + 2>/dev/null | sed -n '1p')
        [ -n "${oldest}" ] || break
        rm -f "${oldest}"
    done
}

fs_type()
{
    dev=$1
    if command -v blkid >/dev/null 2>&1; then
        blkid -o value -s TYPE "${dev}" 2>/dev/null || true
    fi
}

mount_policy()
{
    policy=$(cmdline_value routerd.usb_mount 2>/dev/null || true)
    case "${policy}" in
        sync|async) printf '%s\n' "${policy}" ;;
        *) printf '%s\n' async ;;
    esac
}

mount_options()
{
    dev=$1
    fstype=$(fs_type "${dev}")
    policy=$(mount_policy)
    case "${fstype}" in
        ext2|ext3|ext4)
            printf '%s\n' "rw,${policy},noatime"
            ;;
        vfat|msdos)
            printf '%s\n' "rw,${policy},noatime,utf8,shortname=mixed"
            ;;
        exfat)
            printf '%s\n' "rw,${policy},noatime"
            ;;
        *)
            printf '%s\n' "rw,${policy},noatime"
            ;;
    esac
}

mount_usb()
{
    dev=$1
    [ -b "${dev}" ] || {
        log "USB device is not a block device: ${dev}"
        return 1
    }
    mkdir -p "${mount_dir}"
    if usb_mounted; then
        warn_if_usb_removed || return 1
        return 0
    fi
    opts=$(mount_options "${dev}")
    if mount -o "${opts}" "${dev}" "${mount_dir}" 2>/dev/null; then
        log "mounted ${dev} on ${mount_dir} with ${opts}"
        return 0
    fi
    log "warning: mount with ${opts} failed for ${dev}; retrying with kernel defaults"
    mount "${dev}" "${mount_dir}" 2>/dev/null || mount -o rw "${dev}" "${mount_dir}"
}

unmount_usb()
{
    if ! usb_mounted; then
        log "USB persistence mount is not active"
        return 0
    fi
    src=$(mounted_source)
    sync || true
    if umount "${mount_dir}" 2>/dev/null; then
        log "unmounted USB persistence device ${src:-unknown}"
        return 0
    fi
    log "warning: could not unmount ${mount_dir}; stop routerd services or close files and retry"
    return 1
}

discover_usb()
{
    if [ -f "${usb_state_file}" ]; then
        dev=$(sed -n '1p' "${usb_state_file}")
        [ -b "${dev}" ] && {
            printf '%s\n' "${dev}"
            return 0
        }
    fi
    if dev=$(cmdline_value routerd.usb 2>/dev/null); then
        [ -b "${dev}" ] && {
            printf '%s\n' "${dev}"
            return 0
        }
    fi
    if command -v blkid >/dev/null 2>&1; then
        dev=$(blkid -L ROUTERD 2>/dev/null || true)
        [ -n "${dev}" ] && [ -b "${dev}" ] && {
            printf '%s\n' "${dev}"
            return 0
        }
    fi
    for dev in /dev/disk/by-label/ROUTERD /dev/sd*[0-9] /dev/vd*[0-9]; do
        [ -b "${dev}" ] || continue
        printf '%s\n' "${dev}"
        return 0
    done
    return 1
}

install_flush_job()
{
    enabled=${1:-yes}
    printf '%s\n' "${enabled}" > "${flush_enabled_file}"
    if [ "${enabled}" != "yes" ]; then
        rm -f /etc/periodic/daily/routerd-usb-flush
        return 0
    fi
    mkdir -p /etc/periodic/daily
    cat > /etc/periodic/daily/routerd-usb-flush <<'EOS'
#!/bin/sh
/usr/share/routerd/live-persistence.sh flush >/run/routerd/logs/usb-flush.log 2>&1 || true
EOS
    chmod 0755 /etc/periodic/daily/routerd-usb-flush
}

setup_lbu()
{
    command -v lbu >/dev/null 2>&1 || return 0
    for path in /usr/local/etc/routerd /var/lib/routerd /var/db/routerd /etc/periodic/daily/routerd-usb-flush; do
        lbu include "${path}" >/dev/null 2>&1 || true
    done
}

restore_config()
{
    dev=$1
    mount_usb "${dev}" || return 1
    src="${mount_dir}/${persist_dir_name}/router.yaml"
    if [ -f "${src}" ]; then
        mkdir -p "$(dirname "${config_file}")"
        install -m 0600 "${src}" "${config_file}"
        log "restored ${config_file} from ${dev}"
        return 0
    fi
    return 1
}

save_config()
{
    dev=$1
    src=$2
    flush_enabled=${3:-yes}
    log_limit=${4:-100M}
    mount_usb "${dev}" || return 1
    mkdir -p "${mount_dir}/${persist_dir_name}/logs" "${mount_dir}/${persist_dir_name}/state"
    install -m 0600 "${src}" "${mount_dir}/${persist_dir_name}/router.yaml"
    printf '%s\n' "${dev}" > "${usb_state_file}"
    printf '%s\n' "${dev}" > "${mount_dir}/${persist_dir_name}/usb-device"
    printf '%s\n' "${flush_enabled}" > "${mount_dir}/${persist_dir_name}/usb-flush-enabled"
    printf '%s\n' "${log_limit}" > "${mount_dir}/${persist_dir_name}/log-limit"
    install_flush_job "${flush_enabled}"
    setup_lbu
    if command -v lbu >/dev/null 2>&1; then
        lbu commit -d "${mount_dir}" >/dev/null 2>&1 || lbu commit >/dev/null 2>&1 || true
    fi
    sync
    log "saved routerd config to ${dev}"
}

flush_to_usb()
{
    dev=$(discover_usb 2>/dev/null || true)
    [ -n "${dev}" ] || {
        log "no USB persistence device found"
        return 0
    }
    mount_usb "${dev}" || {
        log "warning: USB persistence unavailable; runtime logs remain in tmpfs"
        return 1
    }
    mkdir -p "${mount_dir}/${persist_dir_name}/logs" "${mount_dir}/${persist_dir_name}/state"
    [ -f "${config_file}" ] && install -m 0600 "${config_file}" "${mount_dir}/${persist_dir_name}/router.yaml"
    if [ -d /var/lib/routerd ]; then
        tar -C /var/lib -czf "${mount_dir}/${persist_dir_name}/state/routerd-varlib.tgz" routerd 2>/dev/null || true
    fi
    if [ -d /var/db/routerd ]; then
        tar -C /var/db -czf "${mount_dir}/${persist_dir_name}/state/routerd-vardb.tgz" routerd 2>/dev/null || true
    fi
    if [ -d "${log_dir}" ]; then
        stamp=$(date +%Y%m%d-%H%M%S)
        tar -C "${log_dir}" -czf "${mount_dir}/${persist_dir_name}/logs/${stamp}.tgz" . 2>/dev/null || true
    fi
    if command -v lbu >/dev/null 2>&1; then
        lbu commit -d "${mount_dir}" >/dev/null 2>&1 || lbu commit >/dev/null 2>&1 || true
    fi
    sync
    log "flushed routerd config, state, and log archive to ${dev}"
}

case "${1:-init}" in
    init)
        log_limit=$(cmdline_value routerd.log_size 2>/dev/null || true)
        [ -n "${log_limit}" ] || log_limit=100M
        ensure_log_tmpfs "${log_limit}"
        dev=$(discover_usb 2>/dev/null || true)
        if [ -n "${dev}" ]; then
            printf '%s\n' "${dev}" > "${usb_state_file}"
            restore_config "${dev}" || true
            if mount_usb "${dev}" 2>/dev/null; then
                enabled=$(sed -n '1p' "${mount_dir}/${persist_dir_name}/usb-flush-enabled" 2>/dev/null || true)
                [ -n "${enabled}" ] || enabled=yes
                install_flush_job "${enabled}"
            fi
        else
            log "no USB persistence device found; running in ephemeral mode"
        fi
        ;;
    list-devices)
        if command -v lsblk >/dev/null 2>&1; then
            lsblk -rpno NAME,SIZE,FSTYPE,LABEL,TYPE 2>/dev/null | awk '$5 == "part" {print "  - " $1 " " $2 " " $3 " " $4}'
        elif command -v blkid >/dev/null 2>&1; then
            for dev in $(blkid -o device 2>/dev/null); do
                [ -b "${dev}" ] && echo "  - ${dev} $(blkid "${dev}" 2>/dev/null)"
            done
        else
            for dev in /dev/sd*[0-9] /dev/vd*[0-9]; do
                [ -b "${dev}" ] && echo "  - ${dev}"
            done
        fi
        ;;
    status)
        if usb_mounted; then
            src=$(mounted_source)
            if warn_if_usb_removed; then
                log "USB persistence mounted: ${src} -> ${mount_dir}"
            else
                exit 1
            fi
        else
            log "USB persistence is not mounted"
        fi
        ;;
    umount|unmount)
        unmount_usb
        ;;
    save-config)
        [ "$#" -ge 3 ] || {
            echo "usage: live-persistence.sh save-config DEVICE CONFIG [FLUSH yes|no] [LOG_LIMIT]" >&2
            exit 2
        }
        save_config "$2" "$3" "${4:-yes}" "${5:-100M}"
        ;;
    flush)
        flush_to_usb
        ;;
    ensure-logs)
        ensure_log_tmpfs "${2:-100M}"
        ;;
    *)
        echo "usage: live-persistence.sh {init|list-devices|status|umount|save-config|flush|ensure-logs}" >&2
        exit 2
        ;;
esac
EOF
chmod 0755 "${overlay_root}/usr/share/routerd/live-persistence.sh"

cat > "${overlay_root}/usr/share/routerd/live-dhcp.sh" <<'EOF'
#!/bin/sh
set -eu

state_dir=/run/routerd/live
config_file=/usr/local/etc/routerd/router.yaml
log_dir=/run/routerd/logs

mkdir -p "${state_dir}" "${log_dir}"

cmdline_value()
{
    key=$1
    for item in $(cat /proc/cmdline 2>/dev/null || true); do
        case "${item}" in
            "${key}="*)
                printf '%s\n' "${item#*=}"
                return 0
                ;;
        esac
    done
    return 1
}

config_router_name()
{
    [ -f "${config_file}" ] || return 1
    awk '
        /^metadata:[[:space:]]*$/ { in_meta = 1; next }
        in_meta && /^spec:[[:space:]]*$/ { exit }
        in_meta && /^[[:space:]]+name:[[:space:]]*/ {
            sub(/^[[:space:]]+name:[[:space:]]*/, "")
            gsub(/["'\'']/, "")
            print
            exit
        }
    ' "${config_file}"
}

first_mac()
{
    for iface in $(candidate_interfaces); do
        mac=$(cat "/sys/class/net/${iface}/address" 2>/dev/null || true)
        case "${mac}" in
            ""|00:00:00:00:00:00) ;;
            *) printf '%s\n' "${mac}"; return 0 ;;
        esac
    done
    return 1
}

live_hostname()
{
    value=$(cmdline_value routerd.hostname 2>/dev/null || cmdline_value routerd.live_hostname 2>/dev/null || true)
    [ -n "${value}" ] || value=$(config_router_name 2>/dev/null || true)
    current=$(hostname 2>/dev/null || true)
    if [ -z "${value}" ] && [ -n "${current}" ] && [ "${current}" != "localhost" ]; then
        value=${current}
    fi
    if [ -z "${value}" ]; then
        mac=$(first_mac 2>/dev/null || true)
        suffix=$(printf '%s' "${mac}" | tr -d ':')
        suffix=${suffix#????????}
        [ -n "${suffix}" ] || suffix=live
        value="routerd-${suffix}"
    fi
    printf '%s\n' "${value}" | tr -c 'A-Za-z0-9.-' '-' | sed 's/^-*//; s/-*$//'
}

candidate_interfaces()
{
    ls /sys/class/net 2>/dev/null | grep -E '^(eth|en|ens)' || true
}

client_id_hex()
{
    override=$(cmdline_value routerd.dhcp_client_id 2>/dev/null || true)
    if [ -n "${override}" ]; then
        printf '%s\n' "${override}"
        return 0
    fi
    return 1
}

start_one()
{
    iface=$1
    host=$2
    pidfile="${state_dir}/udhcpc-${iface}.pid"
    logfile="${log_dir}/udhcpc-${iface}.log"
    [ -e "/sys/class/net/${iface}" ] || return 1
    if [ -f "${pidfile}" ] && kill -0 "$(cat "${pidfile}" 2>/dev/null)" 2>/dev/null; then
        return 0
    fi
    ip link set "${iface}" up 2>/dev/null || true
    client_id=$(client_id_hex 2>/dev/null || true)
    options="-x hostname:${host}"
    [ -n "${client_id}" ] && options="${options} -x 0x3d:${client_id}"
    # First get a lease synchronously so boot can continue with working network,
    # then leave a daemon running to renew/rebind it.
    # shellcheck disable=SC2086
    udhcpc -q -n -t 2 -T 3 -i "${iface}" ${options} >>"${logfile}" 2>&1 || return 1
    # shellcheck disable=SC2086
    udhcpc -p "${pidfile}" -i "${iface}" ${options} >>"${logfile}" 2>&1 &
    return 0
}

start()
{
    command -v udhcpc >/dev/null 2>&1 || exit 0
    host=$(live_hostname)
    [ -n "${host}" ] || host=routerd-live
    hostname "${host}" 2>/dev/null || true
    printf '%s\n' "${host}" > /etc/hostname 2>/dev/null || true
    for iface in $(candidate_interfaces); do
        if start_one "${iface}" "${host}"; then
            printf '%s\n' "${iface}" > "${state_dir}/dhcp-interface"
            printf '%s\n' "${host}" > "${state_dir}/dhcp-hostname"
            return 0
        fi
    done
    return 0
}

case "${1:-start}" in
    start) start ;;
    hostname) live_hostname ;;
    *) echo "usage: live-dhcp.sh {start|hostname}" >&2; exit 2 ;;
esac
EOF
chmod 0755 "${overlay_root}/usr/share/routerd/live-dhcp.sh"

cat > "${overlay_root}/usr/share/routerd/live-autostart.sh" <<'EOF'
#!/bin/sh
set -eu

config=/usr/local/etc/routerd/router.yaml
marker=/run/routerd/live-autostart.done
routerd=/usr/local/sbin/routerd
routerctl=/usr/local/sbin/routerctl
socket=/run/routerd/routerd.sock
status_socket=/run/routerd/routerd-status.sock
log_dir=/run/routerd/logs

routerd_serve_running() {
    pids="$(pgrep -x routerd 2>/dev/null || pidof routerd 2>/dev/null || true)"
    for pid in ${pids}; do
        [ -r "/proc/${pid}/cmdline" ] || continue
        cmdline="$(tr '\000' ' ' < "/proc/${pid}/cmdline")"
        case " ${cmdline} " in
            *" ${routerd} serve "*|*" routerd serve "*)
                return 0
                ;;
        esac
    done
    return 1
}

mkdir -p /run/routerd "${log_dir}" /var/lib/routerd
/usr/share/routerd/live-persistence.sh init || true

[ -f "${config}" ] || exit 0
[ -f "${marker}" ] && exit 0

/usr/share/routerd/live-dhcp.sh start || true

/usr/share/routerd/install.sh --deps-only >/run/routerd/logs/deps.log 2>&1 || true
"${routerd}" validate --config "${config}"
"${routerd}" apply --config "${config}" --once
if routerd_serve_running; then
    echo "routerd-live: routerd serve already running; not starting a duplicate" >> "${log_dir}/routerd-live.log"
elif [ ! -S "${socket}" ]; then
    nohup "${routerd}" serve \
        --config "${config}" \
        --socket "${socket}" \
        --status-socket "${status_socket}" \
        > "${log_dir}/routerd-live.log" 2>&1 &
    sleep 1
fi
if [ -x "${routerctl}" ]; then
    "${routerctl}" status || true
fi
touch "${marker}"
EOF
chmod 0755 "${overlay_root}/usr/share/routerd/live-autostart.sh"

cat > "${overlay_root}/etc/motd" <<EOF
routerd live ${version}

Run the setup wizard:
  /usr/share/routerd/install.sh configure

License notices:
  /usr/share/licenses/routerd/LICENSE
  /usr/share/licenses/routerd/THIRD_PARTY_LICENSES.txt

The wizard writes /usr/local/etc/routerd/router.yaml and can apply it.
For a persistent router, install routerd from the release archive onto disk.
EOF

cat > "${overlay_root}/root/.profile" <<'EOF'
echo
cat /etc/motd
echo
if [ -x /usr/share/routerd/live-autostart.sh ]; then
  /usr/share/routerd/live-autostart.sh || true
fi
routerd_skip_wizard()
{
  [ -f /usr/local/etc/routerd/router.yaml ] && return 0
  grep -qw 'routerd.skip-wizard=1' /proc/cmdline 2>/dev/null && return 0
  return 1
}
if ! routerd_skip_wizard; then
  /usr/share/routerd/live-dhcp.sh start || true
  /usr/share/routerd/install.sh --deps-only || true
  echo "Starting routerd setup wizard. Press Enter within 5 seconds to continue, or wait to skip."
  if read -r -t 5 _routerd_start_wizard; then
    /usr/share/routerd/install.sh configure || true
  else
    echo "routerd setup wizard skipped; run /usr/share/routerd/install.sh configure to start it manually."
  fi
fi
EOF

cat > "${overlay_root}/etc/local.d/routerd-configure.start" <<'EOF'
#!/bin/sh
cat /etc/motd
/usr/share/routerd/live-autostart.sh || true
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
    linux /boot/vmlinuz-lts modules=loop,squashfs,sd-mod,usb-storage,ext4,vfat,exfat,virtio,virtio_blk,virtio_net quiet alpine_dev=cdrom:iso9660 modloop=/boot/modloop-lts console=tty0 console=ttyS0,115200n8
    initrd /boot/initramfs-lts
}
EOF

if [ -d "${iso_root}/boot/syslinux" ]; then
    cat > "${iso_root}/boot/syslinux/syslinux.cfg" <<EOF
SERIAL 0 115200
TIMEOUT 50
PROMPT 0
DEFAULT routerd

LABEL routerd
MENU LABEL routerd live ${version}
KERNEL /boot/vmlinuz-lts
INITRD /boot/initramfs-lts
FDTDIR /boot/dtbs-lts
APPEND modules=loop,squashfs,sd-mod,usb-storage,ext4,vfat,exfat,virtio,virtio_blk,virtio_net quiet console=tty0 console=ttyS0,115200n8
EOF
fi

iso_versioned="${outdir}/routerd-live-${version}.iso"
iso_alias="${outdir}/routerd-live.iso"
rm -f "${iso_versioned}" "${iso_versioned}.sha256" "${iso_alias}" "${iso_alias}.sha256"
grub-mkrescue -o "${iso_versioned}" "${iso_root}" >/dev/null
cp "${iso_versioned}" "${iso_alias}"
if command -v sha256sum >/dev/null 2>&1; then
    (cd "${outdir}" && sha256sum "$(basename "${iso_versioned}")" > "$(basename "${iso_versioned}").sha256")
    (cd "${outdir}" && sha256sum "$(basename "${iso_alias}")" > "$(basename "${iso_alias}").sha256")
else
    (cd "${outdir}" && shasum -a 256 "$(basename "${iso_versioned}")" > "$(basename "${iso_versioned}").sha256")
    (cd "${outdir}" && shasum -a 256 "$(basename "${iso_alias}")" > "$(basename "${iso_alias}").sha256")
fi

echo "${iso_versioned}"
