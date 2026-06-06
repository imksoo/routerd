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
    "${overlay_root}/etc/init.d" \
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
secrets_dir=/usr/local/etc/routerd/secrets
usb_state_file=/run/routerd/live/usb-device
config_source_file=/run/routerd/live-config-source
config_checksum_file=/run/routerd/live-config-sha256
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

device_label()
{
    dev=$1
    if command -v blkid >/dev/null 2>&1; then
        blkid -o value -s LABEL "${dev}" 2>/dev/null || true
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
        iso9660|udf)
            printf '%s\n' "ro,noatime"
            ;;
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
    discover_usb_candidates | sed -n '1p'
}

discover_usb_candidates()
{
    {
        if [ -f "${usb_state_file}" ]; then
            dev=$(sed -n '1p' "${usb_state_file}")
            [ -b "${dev}" ] && printf '%s\n' "${dev}"
        fi
        if dev=$(cmdline_value routerd.usb 2>/dev/null); then
            [ -b "${dev}" ] && printf '%s\n' "${dev}"
        fi
        if command -v blkid >/dev/null 2>&1; then
            dev=$(blkid -L ROUTERD_CONFIG 2>/dev/null || true)
            [ -n "${dev}" ] && [ -b "${dev}" ] && printf '%s\n' "${dev}"
            dev=$(blkid -L ROUTERD 2>/dev/null || true)
            [ -n "${dev}" ] && [ -b "${dev}" ] && printf '%s\n' "${dev}"
        fi
        for dev in /dev/disk/by-label/ROUTERD_CONFIG /dev/disk/by-label/ROUTERD /dev/sd*[0-9] /dev/vd*[0-9] /dev/sr*; do
            [ -b "${dev}" ] || continue
            printf '%s\n' "${dev}"
        done
    } | awk '!seen[$0]++'
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

read_only_config_media()
{
    dev=$1
    case "$(fs_type "${dev}")" in
        iso9660|udf) return 0 ;;
        *) return 1 ;;
    esac
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
    src=$(select_config_source 2>/dev/null || true)
    [ -n "${src}" ] || return 1
    if [ -f "${src}" ]; then
        mkdir -p "$(dirname "${config_file}")"
        install -m 0600 "${src}" "${config_file}"
        restore_secrets "${src}"
        record_config_source "${dev}" "${src}"
        log "restored ${config_file} from ${dev}:${src#${mount_dir}/}"
        return 0
    fi
    return 1
}

restore_secrets()
{
    config_src=$1
    src_parent=$(dirname "${config_src}")
    for src_dir in \
        "${src_parent}/secrets" \
        "${mount_dir}/${persist_dir_name}/secrets" \
        "${mount_dir}/routerd/secrets"; do
        [ -d "${src_dir}" ] || continue
        mkdir -p "${secrets_dir}"
        chmod 0700 "${secrets_dir}" 2>/dev/null || true
        find "${src_dir}" -type f | while IFS= read -r secret; do
            rel=${secret#${src_dir}/}
            dest="${secrets_dir}/${rel}"
            mkdir -p "$(dirname "${dest}")"
            install -m 0600 "${secret}" "${dest}"
        done
        log "restored routerd secrets from ${src_dir#${mount_dir}/}"
    done
}

config_hostnames()
{
    for key in routerd.hostname routerd.live_hostname; do
        value=$(cmdline_value "${key}" 2>/dev/null || true)
        [ -n "${value}" ] && printf '%s\n' "${value}"
    done
    current=$(hostname 2>/dev/null || true)
    [ -n "${current}" ] && [ "${current}" != "localhost" ] && printf '%s\n' "${current}"
    if [ -f /etc/hostname ]; then
        value=$(sed -n '1p' /etc/hostname 2>/dev/null || true)
        [ -n "${value}" ] && [ "${value}" != "localhost" ] && printf '%s\n' "${value}"
    fi
}

config_macs()
{
    for iface in $(ls /sys/class/net 2>/dev/null | grep -E '^(eth|en|ens)' || true); do
        mac=$(cat "/sys/class/net/${iface}/address" 2>/dev/null || true)
        case "${mac}" in
            ""|00:00:00:00:00:00) continue ;;
        esac
        lower=$(printf '%s\n' "${mac}" | tr 'A-F' 'a-f')
        printf '%s\n' "${lower}"
        printf '%s\n' "${lower}" | tr -d ':'
    done
}

select_config_source()
{
    for host in $(config_hostnames | awk '!seen[$0]++'); do
        for path in \
            "${mount_dir}/${persist_dir_name}/hosts/${host}.yaml" \
            "${mount_dir}/${persist_dir_name}/hosts/${host}.yml" \
            "${mount_dir}/routerd/hosts/${host}.yaml" \
            "${mount_dir}/routerd/hosts/${host}.yml"; do
            [ -f "${path}" ] && { printf '%s\n' "${path}"; return 0; }
        done
    done
    for mac in $(config_macs | awk '!seen[$0]++'); do
        for path in \
            "${mount_dir}/${persist_dir_name}/hosts/${mac}.yaml" \
            "${mount_dir}/${persist_dir_name}/hosts/${mac}.yml" \
            "${mount_dir}/routerd/hosts/${mac}.yaml" \
            "${mount_dir}/routerd/hosts/${mac}.yml"; do
            [ -f "${path}" ] && { printf '%s\n' "${path}"; return 0; }
        done
    done
    for path in \
        "${mount_dir}/${persist_dir_name}/router.yaml" \
        "${mount_dir}/${persist_dir_name}/router.yml" \
        "${mount_dir}/routerd/router.yaml" \
        "${mount_dir}/routerd/router.yml"; do
        [ -f "${path}" ] && { printf '%s\n' "${path}"; return 0; }
    done
    return 1
}

record_config_source()
{
    dev=$1
    src=$2
    {
        printf 'device=%s\n' "${dev}"
        printf 'label=%s\n' "$(device_label "${dev}")"
        printf 'source=%s\n' "${src}"
        printf 'installed=%s\n' "${config_file}"
    } > "${config_source_file}"
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "${src}" | awk '{print $1}' > "${config_checksum_file}"
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "${src}" | awk '{print $1}' > "${config_checksum_file}"
    fi
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
    save_secrets_to_usb
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

save_secrets_to_usb()
{
    [ -d "${secrets_dir}" ] || return 0
    mkdir -p "${mount_dir}/${persist_dir_name}/secrets"
    find "${secrets_dir}" -type f | while IFS= read -r secret; do
        rel=${secret#${secrets_dir}/}
        dest="${mount_dir}/${persist_dir_name}/secrets/${rel}"
        mkdir -p "$(dirname "${dest}")"
        install -m 0600 "${secret}" "${dest}"
    done
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
    save_secrets_to_usb
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
        restored=no
        for candidate in $(discover_usb_candidates 2>/dev/null || true); do
            [ -b "${candidate}" ] || continue
            printf '%s\n' "${candidate}" > "${usb_state_file}"
            if restore_config "${candidate}"; then
                restored=yes
                break
            fi
        done
        dev=$(discover_usb 2>/dev/null || true)
        if [ -n "${dev}" ]; then
            printf '%s\n' "${dev}" > "${usb_state_file}"
            [ "${restored}" = "yes" ] || restore_config "${dev}" || true
            if read_only_config_media "${dev}"; then
                log "config media ${dev} is read-only; USB persistence flush disabled"
            elif mount_usb "${dev}" 2>/dev/null; then
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
            lsblk -rpno NAME,SIZE,FSTYPE,LABEL,TYPE 2>/dev/null | awk '$5 == "part" || $5 == "rom" {print "  - " $1 " " $2 " " $3 " " $4 " " $5}'
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

cat > "${overlay_root}/usr/share/routerd/live-ssh.sh" <<'EOF'
#!/bin/sh
# Opt-in SSH enablement for the routerd live ISO.
# Activated by passing routerd.ssh=1 on the kernel command line.
# Requires authorized_keys on the ROUTERD_CONFIG persistence disk; password
# authentication for root is never enabled.
set -eu

mount_dir=/media/routerd-usb
persist_dir_name=routerd
log_dir=/run/routerd/logs
state_dir=/run/routerd/live

log() {
    echo "routerd-live-ssh: $*"
}

cmdline_value() {
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

ssh_enabled() {
    val=$(cmdline_value routerd.ssh 2>/dev/null || true)
    [ "${val}" = "1" ]
}

usb_mounted() {
    grep -q " ${mount_dir} " /proc/mounts 2>/dev/null
}

hostname_candidates() {
    for key in routerd.hostname routerd.live_hostname; do
        value=$(cmdline_value "${key}" 2>/dev/null || true)
        [ -n "${value}" ] && printf '%s\n' "${value}"
    done
    current=$(hostname 2>/dev/null || true)
    [ -n "${current}" ] && [ "${current}" != "localhost" ] && printf '%s\n' "${current}"
}

mac_candidates() {
    for iface in $(ls /sys/class/net 2>/dev/null | grep -E '^(eth|en|ens)' || true); do
        mac=$(cat "/sys/class/net/${iface}/address" 2>/dev/null || true)
        case "${mac}" in
            ""|00:00:00:00:00:00) continue ;;
        esac
        lower=$(printf '%s\n' "${mac}" | tr 'A-F' 'a-f')
        printf '%s\n' "${lower}"
        printf '%s\n' "${lower}" | tr -d ':'
    done
}

find_authorized_keys() {
    if ! usb_mounted; then
        return 1
    fi
    for host in $(hostname_candidates | awk '!seen[$0]++'); do
        for path in \
            "${mount_dir}/${persist_dir_name}/hosts/${host}/authorized_keys" \
            "${mount_dir}/routerd/hosts/${host}/authorized_keys"; do
            [ -f "${path}" ] && { printf '%s\n' "${path}"; return 0; }
        done
    done
    for mac in $(mac_candidates | awk '!seen[$0]++'); do
        for path in \
            "${mount_dir}/${persist_dir_name}/hosts/${mac}/authorized_keys" \
            "${mount_dir}/routerd/hosts/${mac}/authorized_keys"; do
            [ -f "${path}" ] && { printf '%s\n' "${path}"; return 0; }
        done
    done
    for path in \
        "${mount_dir}/${persist_dir_name}/authorized_keys" \
        "${mount_dir}/routerd/authorized_keys"; do
        [ -f "${path}" ] && { printf '%s\n' "${path}"; return 0; }
    done
    return 1
}

ensure_openssh() {
    command -v sshd >/dev/null 2>&1 && return 0
    if command -v apk >/dev/null 2>&1; then
        log "openssh not found; installing"
        apk add --no-cache openssh >/dev/null 2>&1 || true
    fi
    command -v sshd >/dev/null 2>&1
}

configure_sshd() {
    mkdir -p /etc/ssh
    cat > /etc/ssh/sshd_config << 'SSHD_EOF'
Protocol 2
PermitRootLogin prohibit-password
PubkeyAuthentication yes
AuthorizedKeysFile .ssh/authorized_keys
PasswordAuthentication no
ChallengeResponseAuthentication no
UsePAM no
PrintMotd no
SSHD_EOF
    chmod 0600 /etc/ssh/sshd_config
}

start_sshd() {
    ssh-keygen -A >/dev/null 2>&1 || true
    configure_sshd
    if [ -x /etc/init.d/sshd ]; then
        rc-service sshd restart >>"${log_dir}/routerd-ssh.log" 2>&1 ||
            rc-service sshd start >>"${log_dir}/routerd-ssh.log" 2>&1 || true
    else
        pkill -x sshd 2>/dev/null || true
        sshd 2>>"${log_dir}/routerd-ssh.log" || true
    fi
    log "sshd started (root login: public key only)"
}

mkdir -p "${state_dir}" "${log_dir}"

if ! ssh_enabled; then
    exit 0
fi

if ! ensure_openssh; then
    log "openssh not available; cannot start sshd"
    exit 1
fi

keys_src=$(find_authorized_keys 2>/dev/null || true)
if [ -z "${keys_src}" ]; then
    log "routerd.ssh=1 set but no authorized_keys found on config media; not starting sshd"
    log "place authorized_keys at: ${mount_dir}/${persist_dir_name}/authorized_keys"
    exit 0
fi

mkdir -p /root/.ssh
chmod 0700 /root/.ssh
install -m 0600 "${keys_src}" /root/.ssh/authorized_keys
log "installed authorized_keys from ${keys_src}"

start_sshd
EOF
chmod 0755 "${overlay_root}/usr/share/routerd/live-ssh.sh"

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

log() {
    echo "routerd-live: $*" >> "${log_dir}/routerd-live.log"
}

is_virtual_environment() {
    if command -v systemd-detect-virt >/dev/null 2>&1; then
        if systemd-detect-virt --vm >/dev/null 2>&1; then
            return 0
        fi
    fi

    if grep -q 'hypervisor' /proc/cpuinfo 2>/dev/null; then
        return 0
    fi

    for path in \
        /sys/class/dmi/id/sys_vendor \
        /sys/class/dmi/id/board_vendor \
        /sys/class/dmi/id/product_name \
        /sys/class/dmi/id/product_version \
        /sys/class/dmi/id/chassis_vendor; do
        if [ -r "${path}" ]; then
            value=$(tr -d '\r' < "${path}" 2>/dev/null || true)
            case "${value}" in
                *QEMU*|*KVM*|*VirtualBox*|*VMware*|*Xen*|*innotek*|*OpenStack*|*oVirt*|*RHEV*|*Microsoft*|*Parallels*|*Bochs*)
                    return 0
                    ;;
            esac
        fi
    done

    return 1
}

start_qemu_guest_agent() {
    if ! is_virtual_environment; then
        return 0
    fi

    for service in qemu-ga qemu-guest-agent; do
        if [ -x "/etc/init.d/${service}" ]; then
            log "detected virtual environment; ensuring ${service} service"
            rc-update add "${service}" default >/dev/null 2>&1 || true
            rc-service "${service}" restart >/dev/null 2>&1 || rc-service "${service}" start >/dev/null 2>&1 || true
            return 0
        fi
    done

    if command -v qemu-ga >/dev/null 2>&1; then
        log "detected virtual environment; starting qemu-ga directly"
        qemu-ga --daemonize >/dev/null 2>&1 || true
        return 0
    fi

    log "virtual environment detected but qemu guest agent service or binary not present"
}

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
if [ -x /etc/init.d/routerd ]; then
    if rc-update show default 2>/dev/null | grep -Eq '(^|[[:space:]])routerd([[:space:]]|$)'; then
        if ! rc_update_out=$(rc-update del routerd default 2>&1); then
            echo "routerd-live: failed to remove routerd from default runlevel; relying on stale serve restart path: ${rc_update_out}" >> "${log_dir}/routerd-live.log"
        fi
    fi
fi

[ -f "${config}" ] || exit 0
[ -f "${marker}" ] && exit 0

/usr/share/routerd/live-dhcp.sh start || true

/usr/share/routerd/install.sh --deps-only >/run/routerd/logs/deps.log 2>&1 || true
/usr/share/routerd/live-ssh.sh >> "${log_dir}/routerd-ssh.log" 2>&1 || true
start_qemu_guest_agent
"${routerd}" validate --config "${config}"
# Start the managed GoBGP daemon (routerd-bgp) before routerd serve reconciles BGP.
# On Alpine/OpenRC the daemon is supervised by /etc/init.d/routerd-bgp; routerd
# serve only connects to its socket (the systemd unit it renders is inert here).
# This runs after live-dhcp.sh so the interface has an address; restart is used so a
# clean instance comes up regardless of any earlier state.
if [ -x /etc/init.d/routerd-bgp ] && grep -qE '^[[:space:]]*kind:[[:space:]]*BGPRouter([[:space:]]|$)' "${config}" 2>/dev/null; then
    rc-service routerd-bgp restart >> "${log_dir}/routerd-live.log" 2>&1 || true
fi
"${routerd}" apply --config "${config}" --once
if routerd_serve_running; then
    if [ -x /etc/init.d/routerd ]; then
        echo "routerd-live: routerd serve was already running before config handoff; restarting after restore reason=LiveISOStaleServeRestarted" >> "${log_dir}/routerd-live.log"
        rc-service routerd restart >> "${log_dir}/routerd-live.log" 2>&1 || true
    else
        echo "routerd-live: routerd serve already running; not starting a duplicate" >> "${log_dir}/routerd-live.log"
    fi
elif [ -x /etc/init.d/routerd ]; then
    rc-service routerd start >> "${log_dir}/routerd-live.log" 2>&1 || true
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

cat > "${overlay_root}/etc/init.d/routerd" <<'EOF'
#!/sbin/openrc-run

description="routerd live controller"
command="/usr/local/sbin/routerd"
command_args="serve --config /usr/local/etc/routerd/router.yaml --socket /run/routerd/routerd.sock --status-socket /run/routerd/routerd-status.sock"
command_background="yes"
pidfile="/run/routerd/routerd.pid"
output_log="/run/routerd/logs/routerd-live.log"
error_log="/run/routerd/logs/routerd-live.log"

depend() {
    need localmount
    after networking
}

start_pre() {
    mkdir -p /run/routerd/logs /var/lib/routerd
    [ -f /usr/local/etc/routerd/router.yaml ]
}
EOF
chmod 0755 "${overlay_root}/etc/init.d/routerd"

cat > "${overlay_root}/etc/init.d/routerd-bgp" <<'EOF'
#!/sbin/openrc-run

description="routerd managed GoBGP daemon"
# supervise-daemon gives systemd-unit-like behaviour: keep the foreground daemon
# running and respawn it if it exits (e.g. an early start before the DHCP lease).
supervisor=supervise-daemon
command="/usr/local/sbin/routerd-bgp"
command_args="daemon --socket /run/routerd/bgp/gobgp.sock --control-socket /run/routerd/bgp/control.sock --state-file /var/lib/routerd/bgp/applied.json"
pidfile="/run/routerd/bgp/routerd-bgp.pid"
respawn_delay=3
respawn_max=0
output_log="/run/routerd/logs/routerd-bgp.log"
error_log="/run/routerd/logs/routerd-bgp.log"

depend() {
    need localmount
    after networking
}

start_pre() {
    checkpath -d -m 0755 /run/routerd/bgp /var/lib/routerd/bgp /run/routerd/logs
    [ -x /usr/local/sbin/routerd-bgp ] || return 1
    # Wait for a global IPv4 address before launching GoBGP. On the live image DHCP
    # is brought up by live-dhcp.sh and a lease may still be in flight; routerd-bgp
    # needs a local address to source its BGP sessions and exits early without one.
    i=0
    while [ "${i}" -lt 60 ]; do
        [ -n "$(ip -4 -o addr show scope global 2>/dev/null)" ] && break
        sleep 1
        i=$((i + 1))
    done
}
EOF
chmod 0755 "${overlay_root}/etc/init.d/routerd-bgp"
# Intentionally NOT enabled in the default runlevel: the live image brings up the
# network via live-dhcp.sh inside the local hook, which runs after OpenRC's default
# runlevel. Starting routerd-bgp from the runlevel would race ahead of DHCP and the
# config restore. live-autostart.sh starts it (after DHCP, before routerd serve).

cat > "${overlay_root}/etc/motd" <<EOF
routerd live ${version}

Run the setup wizard:
  /usr/share/routerd/install.sh configure

SSH remote management (opt-in):
  Boot with kernel parameter routerd.ssh=1 and place authorized_keys on the
  config disk at routerd/authorized_keys (alongside router.yaml).
  Password authentication is never enabled.

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
    linux /boot/vmlinuz-lts modules=loop,squashfs,sd-mod,sr-mod,cdrom,isofs,ata_piix,ata_generic,usb-storage,ext4,vfat,exfat,virtio,virtio_blk,virtio_net quiet alpine_dev=cdrom:iso9660 modloop=/boot/modloop-lts console=tty0 console=ttyS0,115200n8
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
APPEND modules=loop,squashfs,sd-mod,sr-mod,cdrom,isofs,ata_piix,ata_generic,usb-storage,ext4,vfat,exfat,virtio,virtio_blk,virtio_net quiet console=tty0 console=ttyS0,115200n8
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
