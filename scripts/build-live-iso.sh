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
ubuntu_packages=${UBUNTU_LIVE_PACKAGES:-"ca-certificates curl dnsmasq-base nftables wireguard-tools chrony bind9-dnsutils tcpdump cron jq ppp pppoe conntrack iproute2 iputils-ping iputils-tracepath net-tools kmod radvd strongswan-swanctl iptables keepalived openssh-server qemu-guest-agent zstd systemd-resolved"}
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

rootfs_only=false
for arg in "$@"; do
    case "${arg}" in
        --rootfs-only) rootfs_only=true ;;
    esac
done

require debootstrap
if [ "${rootfs_only}" = false ]; then
    require mksquashfs
    require grub-mkrescue
    require xorriso
fi

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

rootfs_cache=${ROOTFS_CACHE_TAR:-}

if [ -n "${rootfs_cache}" ] && [ -f "${rootfs_cache}" ]; then
    tar -xf "${rootfs_cache}" -C "${workdir}"
    mkdir -p "${workdir}/rootfs/dev"
    echo "restored rootfs from cache (skipping debootstrap + apt-get)"
else

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
run_root chown -R "$(id -u):$(id -g)" "${rootfs}"
chmod -R u+w "${rootfs}"

if [ -n "${rootfs_cache}" ]; then
    tar -cf "${rootfs_cache}" -C "${workdir}" --exclude='rootfs/dev/*' rootfs
    echo "saved rootfs cache at ${rootfs_cache}"
fi

fi

if [ "${rootfs_only}" = true ]; then
    echo "rootfs-only mode: done"
    exit 0
fi

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

install -d "${rootfs}/etc/systemd/system"
if [ -f contrib/systemd/routerd.service ]; then
    install -m 0644 contrib/systemd/routerd.service "${rootfs}/etc/systemd/system/routerd.service"
else
    cat > "${rootfs}/etc/systemd/system/routerd.service" <<'EOF'
[Unit]
Description=routerd network router daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/sbin/routerd serve --config /usr/local/etc/routerd/router.yaml --socket /run/routerd/routerd.sock --status-socket /run/routerd/routerd-status.sock --apply-interval 10s
Restart=always
RestartSec=5
RuntimeDirectory=routerd
StateDirectory=routerd

[Install]
WantedBy=multi-user.target
EOF
fi

cat > "${rootfs}/etc/systemd/system/routerd-dns-resolver@.service" <<'EOF'
[Unit]
Description=routerd DNS resolver daemon (%i)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/sbin/routerd-dns-resolver daemon --resource %i --config-file /run/routerd/dns-resolver/%i.json
Restart=always
RestartSec=5
RuntimeDirectory=routerd/dns-resolver
StateDirectory=routerd/dns-resolver

[Install]
WantedBy=multi-user.target
EOF

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

cloudinit_mount_dir=/media/routerd-cloudinit
config_mount_dir=/media/routerd-config
config_file=/usr/local/etc/routerd/router.yaml
config_dir=/usr/local/etc/routerd
provider_userdata_file=/run/routerd/cloudinit-provider-user-data
validated_cache_dir=/var/lib/routerd/validated-config
validated_cache_file=/var/lib/routerd/validated-config/router.yaml

log()
{
    echo "routerd-live: $*"
}

cloudinit_candidates()
{
    {
        if command -v blkid >/dev/null 2>&1; then
            for label in CIDATA cidata; do
                dev=$(blkid -L "${label}" 2>/dev/null || true)
                [ -n "${dev}" ] && [ -b "${dev}" ] && printf '%s\n' "${dev}"
            done
        fi
        for dev in /dev/disk/by-label/CIDATA /dev/disk/by-label/cidata /dev/sr* /dev/vd*[0-9] /dev/sd*[0-9]; do
            [ -b "${dev}" ] || continue
            printf '%s\n' "${dev}"
        done
    } | awk '!seen[$0]++'
}

nocloud_available()
{
    if command -v blkid >/dev/null 2>&1; then
        for label in CIDATA cidata; do
            dev=$(blkid -L "${label}" 2>/dev/null || true)
            [ -n "${dev}" ] && [ -b "${dev}" ] && return 0
        done
    fi
    for dev in /dev/disk/by-label/CIDATA /dev/disk/by-label/cidata; do
        [ -b "${dev}" ] && return 0
    done
    return 1
}

mount_cloudinit()
{
    dev=$1
    [ -b "${dev}" ] || return 1
    install -d "${cloudinit_mount_dir}"
    if grep -q " ${cloudinit_mount_dir} " /proc/mounts 2>/dev/null; then
        umount "${cloudinit_mount_dir}" 2>/dev/null || true
    fi
    mount -o ro,noatime "${dev}" "${cloudinit_mount_dir}" 2>/dev/null || mount -o ro "${dev}" "${cloudinit_mount_dir}"
}

cloudinit_user_data()
{
    for path in \
        "${cloudinit_mount_dir}/user-data" \
        "${cloudinit_mount_dir}/userdata" \
        "${cloudinit_mount_dir}/openstack/latest/user_data" \
        "${cloudinit_mount_dir}/openstack/latest/user-data"; do
        [ -f "${path}" ] && { printf '%s\n' "${path}"; return 0; }
    done
    return 1
}

dmi_value()
{
    name=$1
    path="/sys/class/dmi/id/${name}"
    [ -r "${path}" ] || return 1
    sed -n '1p' "${path}" 2>/dev/null || true
}

imds_get()
{
    url=$1
    shift
    curl -fsSL --connect-timeout 2 --max-time 5 "$@" "${url}"
}

aws_detect()
{
    product=$(dmi_value product_name 2>/dev/null || true)
    asset=$(dmi_value chassis_asset_tag 2>/dev/null || true)
    case "${product} ${asset}" in
        *amazon*|*Amazon*|*"amazon ec2"*|*"Amazon EC2"*) return 0 ;;
    esac
    curl -fsS -X PUT --connect-timeout 2 --max-time 5 \
        -H "X-aws-ec2-metadata-token-ttl-seconds: 300" \
        http://169.254.169.254/latest/api/token >/dev/null 2>&1
}

azure_detect()
{
    asset=$(dmi_value chassis_asset_tag 2>/dev/null || true)
    case "${asset}" in
        *7783-7084-3265-9085-8269-3286-77*) return 0 ;;
    esac
    imds_get "http://169.254.169.254/metadata/instance?api-version=2021-02-01" \
        -H "Metadata: true" >/dev/null 2>&1
}

oci_detect()
{
    asset=$(dmi_value chassis_asset_tag 2>/dev/null || true)
    case "${asset}" in
        *OracleCloud*|*oraclecloud*) return 0 ;;
    esac
    imds_get "http://169.254.169.254/opc/v2/instance/metadata/" \
        -H "Authorization: Bearer Oracle" >/dev/null 2>&1
}

detect_provider()
{
    if nocloud_available; then
        printf '%s\n' nocloud
    elif aws_detect; then
        printf '%s\n' aws
    elif azure_detect; then
        printf '%s\n' azure
    elif oci_detect; then
        printf '%s\n' oci
    else
        printf '%s\n' unknown
    fi
}

fetch_aws_userdata()
{
    dest=$1
    token=$(curl -fsS -X PUT --connect-timeout 2 --max-time 5 \
        -H "X-aws-ec2-metadata-token-ttl-seconds: 300" \
        http://169.254.169.254/latest/api/token 2>/dev/null || true)
    [ -n "${token}" ] || return 1
    imds_get "http://169.254.169.254/latest/user-data" \
        -H "X-aws-ec2-metadata-token: ${token}" > "${dest}"
}

fetch_azure_userdata()
{
    dest=$1
    tmp="${dest}.b64"
    imds_get "http://169.254.169.254/metadata/instance?api-version=2021-02-01" \
        -H "Metadata: true" >/dev/null
    imds_get "http://169.254.169.254/metadata/instance/compute/userData?api-version=2021-02-01&format=text" \
        -H "Metadata: true" > "${tmp}"
    base64 -d "${tmp}" > "${dest}"
    rm -f "${tmp}"
}

fetch_oci_userdata()
{
    dest=$1
    tmp="${dest}.b64"
    imds_get "http://169.254.169.254/opc/v2/instance/metadata/" \
        -H "Authorization: Bearer Oracle" >/dev/null
    imds_get "http://169.254.169.254/opc/v2/instance/metadata/user_data" \
        -H "Authorization: Bearer Oracle" > "${tmp}"
    base64 -d "${tmp}" > "${dest}"
    rm -f "${tmp}"
}

fetch_provider_userdata()
{
    dest=${1:-${provider_userdata_file}}
    provider=$(detect_provider)
    case "${provider}" in
        nocloud|unknown)
            return 1
            ;;
        aws)
            fetch_aws_userdata "${dest}"
            ;;
        azure)
            fetch_azure_userdata "${dest}"
            ;;
        oci)
            fetch_oci_userdata "${dest}"
            ;;
        *)
            return 1
            ;;
    esac
    [ -s "${dest}" ] || return 1
    log "read cloud-init user-data from ${provider} IMDS"
}

cloudinit_hostname_value()
{
    file=$1
    value=$(sed -n 's/^[[:space:]]*hostname:[[:space:]]*//p' "${file}" 2>/dev/null | sed -n '1p')
    [ -n "${value}" ] || return 1
    value=${value%%[[:space:]]#*}
    value=$(printf '%s\n' "${value}" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//")
    case "${value}" in
        ''|'|'|'>') return 1 ;;
        *[!A-Za-z0-9.-]*|.*|*..*|*.) return 1 ;;
    esac
    printf '%s\n' "${value}"
}

clean_yaml_scalar()
{
    value=${1:-}
    value=${value%%[[:space:]]#*}
    printf '%s\n' "${value}" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//"
}

cloudinit_value()
{
    file=$1
    key=$2
    sed_key=$(printf '%s\n' "${key}" | sed 's/[][\/.^$*]/\\&/g')
    value=$(sed -n "s/^[[:space:]]*${sed_key}:[[:space:]]*//p" "${file}" 2>/dev/null | sed -n '1p')
    if [ -z "${value}" ]; then
        value=$(awk -v want="${key}" '
            function trim(s) {
                sub(/^[[:space:]]+/, "", s)
                sub(/[[:space:]]+$/, "", s)
                return s
            }
            /^[[:space:]]*routerd:[[:space:]]*$/ { in_routerd = 1; next }
            in_routerd && /^[^[:space:]#][^:]*:/ { in_routerd = 0 }
            in_routerd {
                line = $0
                sub(/^[[:space:]]+/, "", line)
                item = line
                sub(/:.*/, "", item)
                if (item == want) {
                    sub(/^[^:]*:[[:space:]]*/, "", line)
                    print trim(line)
                    exit
                }
            }
        ' "${file}" 2>/dev/null || true)
    fi
    [ -n "${value}" ] || return 1
    clean_yaml_scalar "${value}"
}

cloudinit_first_value()
{
    file=$1
    shift
    for key in "$@"; do
        value=$(cloudinit_value "${file}" "${key}" 2>/dev/null || true)
        [ -n "${value}" ] && { printf '%s\n' "${value}"; return 0; }
    done
    return 1
}

cloudinit_ssh_authorized_keys()
{
    file=$1
    awk '
        /^[[:space:]]*ssh_authorized_keys:[[:space:]]*$/ { in_keys = 1; next }
        in_keys && /^[^[:space:]#][^:]*:/ { in_keys = 0 }
        in_keys {
            line = $0
            sub(/^[[:space:]]*-[[:space:]]*/, "", line)
            if (line != $0 && line != "") {
                gsub(/^[[:space:]]+|[[:space:]]+$/, "", line)
                gsub(/^"|"$/, "", line)
                gsub(/^'\''|'\''$/, "", line)
                print line
            }
        }
    ' "${file}" 2>/dev/null
}

set_live_hostname()
{
    host=$1
    printf '%s\n' "${host}" > /etc/hostname
    if command -v hostnamectl >/dev/null 2>&1; then
        hostnamectl set-hostname "${host}" || hostname "${host}" || true
    else
        hostname "${host}" || true
    fi
}

apply_cloudinit_hostname()
{
    command -v udevadm >/dev/null 2>&1 && udevadm settle --timeout=10 2>/dev/null || true
    for candidate in $(cloudinit_candidates 2>/dev/null || true); do
        [ -b "${candidate}" ] || continue
        mount_cloudinit "${candidate}" || continue
        user_data=$(cloudinit_user_data 2>/dev/null || true)
        if [ -n "${user_data}" ]; then
            host=$(cloudinit_hostname_value "${user_data}" 2>/dev/null || true)
            if [ -n "${host}" ]; then
                set_live_hostname "${host}"
                log "set hostname ${host} from NoCloud user-data on ${candidate}"
                umount "${cloudinit_mount_dir}" 2>/dev/null || true
                return 0
            fi
        fi
        umount "${cloudinit_mount_dir}" 2>/dev/null || true
    done
    if fetch_provider_userdata "${provider_userdata_file}"; then
        host=$(cloudinit_hostname_value "${provider_userdata_file}" 2>/dev/null || true)
        if [ -n "${host}" ]; then
            set_live_hostname "${host}"
            log "set hostname ${host} from IMDS user-data"
            return 0
        fi
    fi
    return 1
}

regenerate_ssh_host_keys()
{
    rm -f /etc/ssh/ssh_host_*
    if command -v ssh-keygen >/dev/null 2>&1; then
        ssh-keygen -A
        log "regenerated SSH host keys"
    fi
}

install_authorized_keys()
{
    src=$1
    keys=$(cloudinit_ssh_authorized_keys "${src}" 2>/dev/null || true)
    [ -n "${keys}" ] || return 1
    install -d -m 0700 /root/.ssh
    {
        [ -f /root/.ssh/authorized_keys ] && cat /root/.ssh/authorized_keys
        printf '%s\n' "${keys}"
    } | awk 'NF && !seen[$0]++' > /root/.ssh/authorized_keys.new
    install -m 0600 /root/.ssh/authorized_keys.new /root/.ssh/authorized_keys
    rm -f /root/.ssh/authorized_keys.new
    chown root:root /root/.ssh /root/.ssh/authorized_keys
    log "installed SSH authorized_keys from cloud-init user-data"
}

apply_cloudinit_authorized_keys()
{
    for candidate in $(cloudinit_candidates 2>/dev/null || true); do
        [ -b "${candidate}" ] || continue
        mount_cloudinit "${candidate}" || continue
        user_data=$(cloudinit_user_data 2>/dev/null || true)
        if [ -n "${user_data}" ] && install_authorized_keys "${user_data}"; then
            umount "${cloudinit_mount_dir}" 2>/dev/null || true
            return 0
        fi
        umount "${cloudinit_mount_dir}" 2>/dev/null || true
    done
    if fetch_provider_userdata "${provider_userdata_file}"; then
        install_authorized_keys "${provider_userdata_file}" && return 0
    fi
    return 1
}

enable_sshd()
{
    if systemctl list-unit-files ssh.service >/dev/null 2>&1; then
        systemctl enable --now ssh.service >/dev/null 2>&1 || true
    elif systemctl list-unit-files sshd.service >/dev/null 2>&1; then
        systemctl enable --now sshd.service >/dev/null 2>&1 || true
    fi
}

enable_qemu_guest_agent()
{
    if systemctl list-unit-files qemu-guest-agent.service >/dev/null 2>&1; then
        systemctl enable --now qemu-guest-agent.service >/dev/null 2>&1 || true
    fi
}

apply_ssh_bootstrap()
{
    regenerate_ssh_host_keys
    apply_cloudinit_authorized_keys || true
    enable_sshd
}

config_disk_candidates()
{
    {
        if command -v blkid >/dev/null 2>&1; then
            dev=$(blkid -L ROUTERD_CONFIG 2>/dev/null || true)
            [ -n "${dev}" ] && [ -b "${dev}" ] && printf '%s\n' "${dev}"
        fi
        for dev in /dev/disk/by-label/ROUTERD_CONFIG; do
            [ -b "${dev}" ] || continue
            printf '%s\n' "${dev}"
        done
    } | awk '!seen[$0]++'
}

mount_config_disk()
{
    dev=$1
    [ -b "${dev}" ] || return 1
    install -d "${config_mount_dir}"
    if grep -q " ${config_mount_dir} " /proc/mounts 2>/dev/null; then
        umount "${config_mount_dir}" 2>/dev/null || true
    fi
    mount -o ro,noatime "${dev}" "${config_mount_dir}" 2>/dev/null || mount -o ro "${dev}" "${config_mount_dir}"
}

config_disk_router_yaml()
{
    for path in \
        "${config_mount_dir}/router.yaml" \
        "${config_mount_dir}/router.yml" \
        "${config_mount_dir}/routerd/router.yaml" \
        "${config_mount_dir}/routerd/router.yml"; do
        [ -f "${path}" ] && { printf '%s\n' "${path}"; return 0; }
    done
    return 1
}

restore_config_disk_config()
{
    command -v udevadm >/dev/null 2>&1 && udevadm settle --timeout=10 2>/dev/null || true
    for candidate in $(config_disk_candidates 2>/dev/null || true); do
        [ -b "${candidate}" ] || continue
        mount_config_disk "${candidate}" || continue
        src=$(config_disk_router_yaml 2>/dev/null || true)
        if [ -n "${src}" ]; then
            install -m 0600 "${src}" "${config_file}"
            log "restored ${config_file} from ROUTERD_CONFIG media ${candidate}"
            umount "${config_mount_dir}" 2>/dev/null || true
            return 0
        fi
        umount "${config_mount_dir}" 2>/dev/null || true
    done
    return 1
}

fetch_url()
{
    url=$1
    dest=$2
    curl -fsSL --connect-timeout 30 --max-time 300 --retry 3 --retry-delay 2 "${url}" -o "${dest}"
}

verify_sha256()
{
    file=$1
    want=$2
    [ -n "${want}" ] || return 0
    got=$(sha256sum "${file}" 2>/dev/null | awk '{print $1}' || true)
    if [ "${got}" != "${want}" ]; then
        log "cloud-init config_url sha256 mismatch: got ${got:-unknown} want ${want}"
        return 1
    fi
}

install_config_bundle()
{
    file=$1
    url=$2
    work=/run/routerd/cloudinit-bundle
    rm -rf "${work}"
    install -d "${work}"
    case "${url}" in
        *.tar.zst|*.tzst)
            tar --use-compress-program=zstd -xf "${file}" -C "${work}"
            ;;
        *.tar.gz|*.tgz)
            tar -xzf "${file}" -C "${work}"
            ;;
        *.tar)
            tar -xf "${file}" -C "${work}"
            ;;
        *)
            install -m 0600 "${file}" "${config_file}"
            return 0
            ;;
    esac
    if [ ! -f "${work}/router.yaml" ]; then
        log "cloud-init config bundle missing router.yaml"
        return 1
    fi
    install -m 0600 "${work}/router.yaml" "${config_file}"
    if [ -d "${work}/secrets" ]; then
        rm -rf "${config_dir}/secrets"
        install -d -m 0700 "${config_dir}/secrets"
        cp -a "${work}/secrets/." "${config_dir}/secrets/"
        chown -R root:root "${config_dir}/secrets"
        chmod -R go-rwx "${config_dir}/secrets"
    fi
    if [ -f "${work}/metadata.json" ]; then
        install -m 0600 "${work}/metadata.json" "${config_dir}/metadata.json"
    fi
}

cache_validated_config()
{
    [ -f "${config_file}" ] || return 1
    install -d -m 0700 "${validated_cache_dir}"
    install -m 0600 "${config_file}" "${validated_cache_file}"
    log "cached validated config at ${validated_cache_file}"
}

restore_validated_config_cache()
{
    [ -f "${validated_cache_file}" ] || return 1
    install -m 0600 "${validated_cache_file}" "${config_file}"
    log "restored config from validated cache (fetch failed)"
}

restore_cloudinit_config()
{
    dev=$1
    mount_cloudinit "${dev}" || return 1
    user_data=$(cloudinit_user_data 2>/dev/null || true)
    [ -n "${user_data}" ] || { umount "${cloudinit_mount_dir}" 2>/dev/null || true; return 1; }
    config_url=$(cloudinit_first_value "${user_data}" config_url config-url configUrl routerd_config_url routerd-config-url 2>/dev/null || true)
    [ -n "${config_url}" ] || { umount "${cloudinit_mount_dir}" 2>/dev/null || true; return 1; }
    config_sha256=$(cloudinit_first_value "${user_data}" config_sha256 config-sha256 configSha256 routerd_config_sha256 routerd-config-sha256 2>/dev/null || true)
    umount "${cloudinit_mount_dir}" 2>/dev/null || true

    tmp=/run/routerd/routerd-config.cloudinit
    log "fetching routerd config from cloud-init config_url"
    fetch_url "${config_url}" "${tmp}" || { restore_validated_config_cache && return 0; return 1; }
    verify_sha256 "${tmp}" "${config_sha256}" || { rm -f "${tmp}"; return 1; }
    install_config_bundle "${tmp}" "${config_url}" || { rm -f "${tmp}"; return 1; }
    cache_validated_config || true
    rm -f "${tmp}"
    bootstrap_config_url=${config_url}
    log "restored ${config_file} from cloud-init config_url"
    return 0
}

restore_cloudinit_configs()
{
    command -v udevadm >/dev/null 2>&1 && udevadm settle --timeout=10 2>/dev/null || true
    for candidate in $(cloudinit_candidates 2>/dev/null || true); do
        [ -b "${candidate}" ] || continue
        if restore_cloudinit_config "${candidate}"; then
            return 0
        fi
    done
    return 1
}

restore_provider_config()
{
    fetch_provider_userdata "${provider_userdata_file}" || return 1
    user_data=${provider_userdata_file}
    config_url=$(cloudinit_first_value "${user_data}" config_url config-url configUrl routerd_config_url routerd-config-url 2>/dev/null || true)
    [ -n "${config_url}" ] || return 1
    config_sha256=$(cloudinit_first_value "${user_data}" config_sha256 config-sha256 configSha256 routerd_config_sha256 routerd-config-sha256 2>/dev/null || true)

    tmp=/run/routerd/routerd-config.imds
    log "fetching routerd config from IMDS config_url"
    fetch_url "${config_url}" "${tmp}" || { restore_validated_config_cache && return 0; return 1; }
    verify_sha256 "${tmp}" "${config_sha256}" || { rm -f "${tmp}"; return 1; }
    install_config_bundle "${tmp}" "${config_url}" || { rm -f "${tmp}"; return 1; }
    cache_validated_config || true
    rm -f "${tmp}"
    bootstrap_config_url=${config_url}
    log "restored ${config_file} from IMDS config_url"
    return 0
}

config_url_host()
{
    url=$1
    printf '%s\n' "${url}" | sed -E 's#^[a-zA-Z][a-zA-Z0-9+.-]*://([^/:]+).*#\1#'
}

route_ifname_for_host()
{
    host=$1
    ip=$(getent ahostsv4 "${host}" 2>/dev/null | awk '{print $1; exit}')
    [ -n "${ip}" ] || ip=${host}
    ip -o -4 route get "${ip}" 2>/dev/null | awk '{for (i=1;i<=NF;i++) if ($i=="dev") {print $(i+1); exit}}'
}

write_uplink_network()
{
    ifname=$1
    [ -n "${ifname}" ] || return 0
    cat > "/etc/systemd/network/10-routerd-uplink-${ifname}.network" <<UPLINKEOF
[Match]
Name=${ifname}

[Network]
DHCP=ipv4
IPv6AcceptRA=yes

[DHCPv4]
ClientIdentifier=mac
UseDNS=yes
UseHostname=no
RouteMetric=100
UPLINKEOF
    log "preserved DHCP/DNS on uplink ${ifname}"
}

disable_bootstrap_dhcp()
{
    if [ -f /etc/systemd/network/80-dhcp.network ]; then
        if [ -n "${bootstrap_config_url:-}" ]; then
            host=$(config_url_host "${bootstrap_config_url}")
            uplink=$(route_ifname_for_host "${host}" 2>/dev/null || true)
            write_uplink_network "${uplink}"
        fi
        rm -f /etc/systemd/network/80-dhcp.network
        systemctl reload-or-restart systemd-networkd >/dev/null 2>&1 || true
        log "disabled bootstrap DHCP; routerd will manage network from here"
    fi
}

install -d /run/routerd /var/lib/routerd "${config_dir}"
apply_cloudinit_hostname || true
enable_qemu_guest_agent
apply_ssh_bootstrap

if ! restore_config_disk_config && ! restore_cloudinit_configs && ! restore_provider_config; then
    if [ ! -f "${config_file}" ] && [ -f /usr/local/etc/routerd/router.yaml.sample ]; then
        cp /usr/local/etc/routerd/router.yaml.sample "${config_file}"
    fi
fi

disable_bootstrap_dhcp

if [ -x /usr/local/sbin/routerd ]; then
    systemctl enable routerd.service >/dev/null 2>&1 || true
    systemctl start --no-block routerd.service >/dev/null 2>&1 || true
fi
if [ -x /usr/local/sbin/routerd-dns-resolver ]; then
    systemctl enable routerd-dns-resolver@lan-resolver.service >/dev/null 2>&1 || true
fi
EOF
chmod 0755 "${rootfs}/opt/routerd-live/firstboot.sh"

install -d "${rootfs}/etc/systemd/network"
cat > "${rootfs}/etc/systemd/network/80-dhcp.network" <<'EOF'
[Match]
Name=en* eth*

[Network]
DHCP=yes

[DHCPv4]
ClientIdentifier=mac
UseDNS=yes
UseHostname=no
EOF
ln -sf /run/systemd/resolve/resolv.conf "${rootfs}/etc/resolv.conf"
install -d "${rootfs}/etc/systemd/system/multi-user.target.wants"
ln -sf /usr/lib/systemd/system/systemd-networkd.service "${rootfs}/etc/systemd/system/multi-user.target.wants/systemd-networkd.service"
ln -sf /usr/lib/systemd/system/systemd-resolved.service "${rootfs}/etc/systemd/system/multi-user.target.wants/systemd-resolved.service"

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

# Enable serial console login (ttyS0 @ 115200 baud).
install -d "${rootfs}/etc/systemd/system/getty.target.wants"
ln -sf /usr/lib/systemd/system/serial-getty@.service "${rootfs}/etc/systemd/system/getty.target.wants/serial-getty@ttyS0.service"

# Allow passwordless root login on local consoles (serial/KVM).
# SSH rejects empty passwords by default (PermitEmptyPasswords no).
sed -i 's/^root:[^:]*:/root::/' "${rootfs}/etc/shadow"
sed -i 's/pam_unix\.so/pam_unix.so nullok/' "${rootfs}/etc/pam.d/common-auth"

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
