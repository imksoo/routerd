#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
set -eu

prefix=/usr/local
command_mode=install
enable_service=0
start_service=0
restart_service=1
dry_run=0
verbose=0
config_update=1
install_deps=1
list_deps=0
deps_only=0
with_tailscale=0
configure_non_interactive=0
configure_yes=0
configure_apply=1
completed=0
backup_dir=
timestamp=$(date +%Y%m%d%H%M%S)

usage()
{
    cat <<'USAGE'
Usage:
  ./install.sh [install options]
  ./install.sh configure [configure options]
  ./install.sh --configure [configure options]

Install options:
  --prefix DIR
  --enable-service
  --start-service
  --no-restart
  --dry-run
  --verbose
  --no-config-update
  --no-install-deps
  --list-deps
  --deps-only
  --with-tailscale

Configure options:
  --prefix DIR
  --non-interactive
  --yes
  --no-apply
  --dry-run
  --verbose

Installs or upgrades routerd binaries, service templates, and a sample configuration.
Existing /usr/local/etc/routerd/router.yaml is never overwritten.
State databases, logs, and runtime files are never modified.

By default, the installer also installs known host package dependencies on
supported package managers. Pass --no-install-deps to skip that step.

The configure subcommand starts a text setup wizard and writes
/usr/local/etc/routerd/router.yaml from the answers. In non-interactive mode,
set ROUTERD_WAN_INTERFACE, ROUTERD_LAN_INTERFACE, and related ROUTERD_* values.
USAGE
}

detect_arch()
{
    machine=$(uname -m)
    case "${machine}" in
        x86_64|amd64)
            echo amd64
            ;;
        aarch64|arm64)
            echo arm64
            ;;
        *)
            echo "${machine}"
            ;;
    esac
}

os_id()
{
    case "${os}" in
        Linux)
            echo linux
            ;;
        FreeBSD)
            echo freebsd
            ;;
        *)
            printf '%s\n' "${os}" | tr '[:upper:]' '[:lower:]'
            ;;
    esac
}

safe_name()
{
    printf '%s\n' "$1" | sed 's#[^A-Za-z0-9._-]#_#g'
}

backup_target()
{
    target=$1
    name=$(safe_name "${target}")
    if [ -e "${target}" ]; then
        backup="${backup_dir}/${name}"
        cp -p "${target}" "${backup}"
        persistent_backup="${target}.backup.${timestamp}"
        cp -p "${target}" "${persistent_backup}"
        echo "backup: ${persistent_backup}"
        printf '%s\t%s\n' "${backup}" "${target}" >> "${backup_dir}/restore.list"
    else
        printf '%s\n' "${target}" >> "${backup_dir}/remove.list"
    fi
}

rollback()
{
    [ -n "${backup_dir}" ] || return 0
    [ -d "${backup_dir}" ] || return 0

    echo "install failed; restoring previous files" >&2
    if [ -f "${backup_dir}/restore.list" ]; then
        while IFS='	' read -r backup target; do
            [ -n "${backup}" ] || continue
            install -d -m 0755 "$(dirname "${target}")"
            cp -p "${backup}" "${target}"
        done < "${backup_dir}/restore.list"
    fi
    if [ -f "${backup_dir}/remove.list" ]; then
        while IFS= read -r target; do
            [ -n "${target}" ] || continue
            rm -f "${target}"
        done < "${backup_dir}/remove.list"
    fi
}

cleanup()
{
    status=$?
    if [ "${completed}" -ne 1 ]; then
        rollback
    fi
    if [ -n "${backup_dir}" ] && [ -d "${backup_dir}" ]; then
        rm -rf "${backup_dir}"
    fi
    exit "${status}"
}

atomic_install()
{
    file_mode=$1
    source=$2
    target=$3
    if [ "${dry_run}" -eq 1 ]; then
        echo "dry-run: install -m ${file_mode} ${source} ${target}"
        return 0
    fi
    install -d -m 0755 "$(dirname "${target}")"
    backup_target "${target}"
    tmp="${target}.tmp.$$"
    rm -f "${tmp}"
    install -m "${file_mode}" "${source}" "${tmp}"
    mv -f "${tmp}" "${target}"
}

detect_package_manager()
{
    case "${os}" in
        Linux)
            if command -v apt-get >/dev/null 2>&1; then
                echo apt
            elif command -v apk >/dev/null 2>&1; then
                echo apk
            elif command -v dnf >/dev/null 2>&1; then
                echo dnf
            elif command -v pacman >/dev/null 2>&1; then
                echo pacman
            elif command -v nix-env >/dev/null 2>&1; then
                echo nix-env
            else
                echo none
            fi
            ;;
        FreeBSD)
            if command -v pkg >/dev/null 2>&1; then
                echo pkg
            else
                echo none
            fi
            ;;
        *)
            echo none
            ;;
    esac
}

dependency_packages()
{
    manager=$1
    case "${manager}" in
        apt)
            packages="ca-certificates curl dnsmasq nftables wireguard-tools chrony bind9-dnsutils tcpdump cron jq ppp pppoe conntrack iproute2 iputils-ping iputils-tracepath net-tools kmod radvd strongswan-swanctl iptables"
            ;;
        dnf)
            packages="ca-certificates curl dnsmasq nftables wireguard-tools chrony bind-utils tcpdump cronie jq ppp rp-pppoe conntrack-tools iproute iputils traceroute kmod radvd strongswan iptables"
            ;;
        apk)
            packages="alpine-conf ca-certificates curl dnsmasq nftables wireguard-tools chrony bind-tools tcpdump cronie jq ppp ppp-pppoe conntrack-tools iproute2 iputils iputils-tracepath kmod radvd strongswan iptables util-linux e2fsprogs dosfstools exfatprogs"
            ;;
        pacman)
            packages="ca-certificates curl dnsmasq nftables wireguard-tools chrony bind tcpdump cronie jq ppp rp-pppoe conntrack-tools iproute2 iputils traceroute kmod radvd strongswan iptables"
            ;;
        pkg)
            packages="ca_root_nss curl dnsmasq wireguard-tools mpd5 bind-tools tcpdump jq chrony strongswan"
            ;;
        nix-env)
            packages=""
            ;;
        *)
            packages=""
            ;;
    esac
    if [ "${with_tailscale}" -eq 1 ] && [ -n "${packages}" ]; then
        packages="${packages} tailscale"
    fi
    echo "${packages}"
}

dependency_commands()
{
    manager=$(detect_package_manager)
    case "${manager}" in
        apk)
            commands="curl dnsmasq nft wg wg-quick chronyc dig tcpdump crond jq pppd conntrack ip ping tracepath modprobe radvd swanctl iptables lbu lsblk blkid mkfs.ext4 mkfs.vfat fsck.exfat"
            ;;
        apt|dnf|pacman|nix-env)
            commands="curl dnsmasq nft wg wg-quick chronyc dig tcpdump cron jq pppd conntrack ip ping tracepath modprobe radvd swanctl iptables"
            ;;
        pkg)
            commands="curl dnsmasq wg mpd5 dig tcpdump jq cron ifconfig pfctl route service sysrc chronyc swanctl"
            ;;
        *)
            case "${os}" in
                Linux)
                    commands="curl dnsmasq nft wg wg-quick chronyc dig tcpdump cron jq pppd conntrack ip ping tracepath modprobe radvd swanctl iptables"
                    ;;
                FreeBSD)
                    commands="curl dnsmasq wg mpd5 dig tcpdump jq cron ifconfig pfctl route service sysrc chronyc swanctl"
                    ;;
                *)
                    commands=""
                    ;;
            esac
            ;;
    esac
    if [ "${with_tailscale}" -eq 1 ]; then
        commands="${commands} tailscale"
    fi
    echo "${commands}"
}

print_dependencies()
{
    manager=$(detect_package_manager)
    packages=$(dependency_packages "${manager}")
    commands=$(dependency_commands)
    echo "routerd dependency plan"
    echo "  OS: ${os}"
    echo "  architecture: ${arch}"
    echo "  package manager: ${manager}"
    if [ -n "${packages}" ]; then
        echo "  packages:"
        for package in ${packages}; do
            echo "    - ${package}"
        done
    elif [ "${manager}" = "nix-env" ]; then
        echo "  packages: declare these tools in NixOS configuration or routerd Package resources"
    else
        echo "  packages: unsupported package manager; install required commands manually"
    fi
    echo "  commands checked after install:"
    for cmd in ${commands}; do
        echo "    - ${cmd}"
    done
    if [ "${with_tailscale}" -eq 1 ]; then
        echo "  optional: tailscale requested"
    fi
}

run_dependency_install()
{
    manager=$(detect_package_manager)
    packages=$(dependency_packages "${manager}")

    if [ "${list_deps}" -eq 1 ]; then
        print_dependencies
        return 0
    fi
    if [ "${install_deps}" -ne 1 ]; then
        echo "dependency installation skipped by --no-install-deps"
        return 0
    fi
    if [ -z "${packages}" ]; then
        if [ "${manager}" = "nix-env" ]; then
            echo "warning: NixOS detected; declare dependencies in the NixOS configuration or routerd Package resources" >&2
        else
            echo "warning: no supported package manager detected; install dependencies manually" >&2
        fi
        verify_dependencies
        return 0
    fi

    case "${manager}" in
        apt)
            if [ "${dry_run}" -eq 1 ]; then
                echo "dry-run: apt-get update"
                echo "dry-run: apt-get install -y ${packages}"
            else
                apt-get update
                # shellcheck disable=SC2086
                apt-get install -y ${packages}
            fi
            ;;
        dnf)
            if [ "${dry_run}" -eq 1 ]; then
                echo "dry-run: dnf install -y ${packages}"
            else
                # shellcheck disable=SC2086
                dnf install -y ${packages}
            fi
            ;;
        apk)
            if [ "${dry_run}" -eq 1 ]; then
                echo "dry-run: ensure Alpine main/community repositories"
                echo "dry-run: apk update"
                echo "dry-run: apk add --no-cache ${packages}"
            else
                if ! grep -q '^https://dl-cdn.alpinelinux.org/alpine/latest-stable/main$' /etc/apk/repositories 2>/dev/null; then
                    echo "https://dl-cdn.alpinelinux.org/alpine/latest-stable/main" >> /etc/apk/repositories
                fi
                if ! grep -q '^https://dl-cdn.alpinelinux.org/alpine/latest-stable/community$' /etc/apk/repositories 2>/dev/null; then
                    echo "https://dl-cdn.alpinelinux.org/alpine/latest-stable/community" >> /etc/apk/repositories
                fi
                apk update
                # shellcheck disable=SC2086
                apk add --no-cache ${packages}
            fi
            ;;
        pacman)
            if [ "${dry_run}" -eq 1 ]; then
                echo "dry-run: pacman -Sy --needed --noconfirm ${packages}"
            else
                # shellcheck disable=SC2086
                pacman -Sy --needed --noconfirm ${packages}
            fi
            ;;
        pkg)
            if [ "${dry_run}" -eq 1 ]; then
                echo "dry-run: pkg install -y ${packages}"
            else
                # shellcheck disable=SC2086
                pkg install -y ${packages}
            fi
            ;;
        *)
            echo "warning: unsupported package manager: ${manager}" >&2
            ;;
    esac
    verify_dependencies
}

verify_dependencies()
{
    missing=""
    for cmd in $(dependency_commands); do
        if ! command -v "${cmd}" >/dev/null 2>&1; then
            missing="${missing} ${cmd}"
        fi
    done
    if [ -n "${missing}" ]; then
        echo "warning: missing commands after dependency check:${missing}" >&2
        echo "warning: rerun './install.sh --list-deps' and install the missing packages manually if your OS uses different package names" >&2
    else
        echo "dependency command check passed"
    fi
}

configure_terminal()
{
    case "${TERM:-}" in
        ""|unknown)
            TERM=dumb
            export TERM
            ;;
    esac
    if [ -t 0 ]; then
        stty sane 2>/dev/null || true
    fi
    if [ "${TERM:-dumb}" = "dumb" ]; then
        echo "terminal: dumb mode; using plain text prompts"
    fi
}

show_interfaces()
{
    if command -v ip >/dev/null 2>&1; then
        ip -br link show 2>/dev/null | awk '{print "  - " $1}' | sed 's/@.*//'
    elif [ -d /sys/class/net ]; then
        for iface in /sys/class/net/*; do
            name=$(basename "${iface}")
            [ "${name}" = "lo" ] && continue
            echo "  - ${name}"
        done
    elif command -v ifconfig >/dev/null 2>&1; then
        for name in $(ifconfig -l 2>/dev/null); do
            [ "${name}" = "lo0" ] && continue
            echo "  - ${name}"
        done
    else
        echo "  (no interface listing command found)"
    fi
}

interface_exists()
{
    name=$1
    [ -n "${name}" ] || return 1
    if [ -e "/sys/class/net/${name}" ]; then
        return 0
    fi
    if command -v ip >/dev/null 2>&1 && ip link show "${name}" >/dev/null 2>&1; then
        return 0
    fi
    if command -v ifconfig >/dev/null 2>&1 && ifconfig "${name}" >/dev/null 2>&1; then
        return 0
    fi
    return 1
}

show_usb_devices()
{
    if [ -x /usr/share/routerd/live-persistence.sh ]; then
        /usr/share/routerd/live-persistence.sh list-devices || true
        return 0
    fi
    if command -v lsblk >/dev/null 2>&1; then
        lsblk -rpno NAME,SIZE,FSTYPE,LABEL,TYPE 2>/dev/null | awk '$5 == "part" {print "  - " $1 " " $2 " " $3 " " $4}'
        return 0
    fi
    if command -v blkid >/dev/null 2>&1; then
        for dev in $(blkid -o device 2>/dev/null); do
            [ -b "${dev}" ] && echo "  - ${dev} $(blkid "${dev}" 2>/dev/null)"
        done
        return 0
    fi
    for dev in /dev/sd*[0-9] /dev/vd*[0-9]; do
        [ -b "${dev}" ] && echo "  - ${dev}"
    done
}

save_config_persistence()
{
    usb_enabled=$1
    usb_device=$2
    final_config=$3
    flush_enabled=$4
    log_limit=$5

    [ "${usb_enabled}" = "yes" ] || return 0
    if [ -z "${usb_device}" ]; then
        echo "USB persistence requested but no device was selected" >&2
        return 1
    fi
    if [ ! -b "${usb_device}" ]; then
        echo "USB persistence device is not a block device: ${usb_device}" >&2
        return 1
    fi
    if [ -x /usr/share/routerd/live-persistence.sh ]; then
        /usr/share/routerd/live-persistence.sh save-config "${usb_device}" "${final_config}" "${flush_enabled}" "${log_limit}"
        return 0
    fi
    echo "warning: live persistence helper is not installed; config was not saved to USB" >&2
    return 0
}

prompt_value()
{
    var_name=$1
    label=$2
    default_value=$3
    required=$4
    current=$(eval "printf '%s' \"\${${var_name}:-}\"")
    if [ -n "${current}" ]; then
        printf '%s\n' "${current}"
        return 0
    fi
    if [ "${configure_non_interactive}" -eq 1 ]; then
        if [ "${required}" -eq 1 ] && [ -z "${default_value}" ]; then
            echo "missing required environment variable: ${var_name}" >&2
            exit 2
        fi
        printf '%s\n' "${default_value}"
        return 0
    fi
    while :; do
        if [ -n "${default_value}" ]; then
            printf '%s [%s]: ' "${label}" "${default_value}" >&2
        else
            printf '%s: ' "${label}" >&2
        fi
        IFS= read -r answer
        if [ -z "${answer}" ]; then
            answer=${default_value}
        fi
        if [ "${required}" -eq 0 ] || [ -n "${answer}" ]; then
            printf '%s\n' "${answer}"
            return 0
        fi
        echo "value is required" >&2
    done
}

prompt_choice()
{
    var_name=$1
    label=$2
    default_value=$3
    choices=$4
    while :; do
        value=$(prompt_value "${var_name}" "${label}" "${default_value}" 1)
        for choice in ${choices}; do
            if [ "${value}" = "${choice}" ]; then
                printf '%s\n' "${value}"
                return 0
            fi
        done
        if [ "${configure_non_interactive}" -eq 1 ]; then
            echo "${var_name} must be one of: ${choices}" >&2
            exit 2
        fi
        echo "choose one of: ${choices}" >&2
        eval "${var_name}=''"
    done
}

prompt_bool()
{
    var_name=$1
    label=$2
    default_value=$3
    while :; do
        value=$(prompt_value "${var_name}" "${label}" "${default_value}" 1)
        case "${value}" in
            y|Y|yes|YES|true|TRUE|1)
                echo yes
                return 0
                ;;
            n|N|no|NO|false|FALSE|0)
                echo no
                return 0
                ;;
            *)
                if [ "${configure_non_interactive}" -eq 1 ]; then
                    echo "${var_name} must be yes or no" >&2
                    exit 2
                fi
                echo "answer yes or no" >&2
                eval "${var_name}=''"
                ;;
        esac
    done
}

validate_cidr()
{
    value=$1
    label=$2
    if ! printf '%s\n' "${value}" | grep -Eq '^[0-9A-Fa-f:.]+/[0-9][0-9]?$'; then
        echo "${label} must be CIDR notation, for example 192.168.10.1/24" >&2
        exit 2
    fi
}

address_without_prefix()
{
    value=$1
    printf '%s\n' "${value%%/*}"
}

write_yaml_list()
{
    indent=$1
    values=$2
    for value in ${values}; do
        printf '%s- %s\n' "${indent}" "${value}"
    done
}

write_router_config()
{
    output=$1; shift
    router_name=$1; shift
    wan_interface=$1; shift
    wan_mode=$1; shift
    wan_address=$1; shift
    wan_gateway=$1; shift
    wan_dns=$1; shift
    lan_interface=$1; shift
    lan_address=$1; shift
    lan_cidr=$1; shift
    lan_ipv6_prefix=$1; shift
    dhcp4_enabled=$1; shift
    dhcp4_start=$1; shift
    dhcp4_end=$1; shift
    dhcp6_enabled=$1; shift
    ra_enabled=$1; shift
    dns_enabled=$1; shift
    ntp_enabled=$1; shift
    firewall_enabled=$1; shift
    nat44_enabled=$1; shift
    mgmt_mode=$1; shift
    mgmt_interface=$1; shift
    mgmt_address=$1

    lan_ip=$(address_without_prefix "${lan_address}")
    mgmt_ip=
    if [ -n "${mgmt_address}" ]; then
        mgmt_ip=$(address_without_prefix "${mgmt_address}")
    fi
    dns_upstreams=${wan_dns:-1.1.1.1}

    {
        cat <<EOF
# yaml-language-server: \$schema=../schemas/routerd-config-v1alpha1.schema.json
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: ${router_name}

spec:
  reconcile:
    mode: progressive
    protectedInterfaces:
EOF
        if [ "${mgmt_mode}" = "separate" ]; then
            echo "      - mgmt"
        else
            echo "      - lan"
        fi
        cat <<EOF

  resources:
    - apiVersion: system.routerd.net/v1alpha1
      kind: Sysctl
      metadata:
        name: ipv4-forwarding
      spec:
        key: net.ipv4.ip_forward
        value: "1"
        runtime: true
        persistent: false

    - apiVersion: system.routerd.net/v1alpha1
      kind: Sysctl
      metadata:
        name: ipv6-forwarding
      spec:
        key: net.ipv6.conf.all.forwarding
        value: "1"
        runtime: true
        persistent: false

    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifname: ${wan_interface}
        adminUp: true
        managed: false
        owner: external
EOF
        if [ "${wan_mode}" = "dhcp" ]; then
            cat <<EOF

    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv4Lease
      metadata:
        name: wan-dhcpv4
      spec:
        interface: wan
        useRoutes: true
        useDNS: true
EOF
        else
            cat <<EOF

    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticAddress
      metadata:
        name: wan-ipv4
      spec:
        interface: wan
        address: ${wan_address}
        exclusive: false

    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticRoute
      metadata:
        name: wan-default
      spec:
        interface: wan
        destination: 0.0.0.0/0
        via: ${wan_gateway}
        metric: 100
EOF
        fi
        cat <<EOF

    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lan
      spec:
        ifname: ${lan_interface}
        adminUp: true
        managed: true
        owner: routerd

    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticAddress
      metadata:
        name: lan-ipv4
      spec:
        interface: lan
        address: ${lan_address}
        exclusive: false
EOF
        if [ "${mgmt_mode}" = "separate" ]; then
            cat <<EOF

    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: mgmt
      spec:
        ifname: ${mgmt_interface}
        adminUp: true
        managed: true
        owner: routerd
EOF
            if [ -n "${mgmt_address}" ]; then
                cat <<EOF

    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticAddress
      metadata:
        name: mgmt-ipv4
      spec:
        interface: mgmt
        address: ${mgmt_address}
        exclusive: false
EOF
            fi
        fi
        if [ "${dns_enabled}" = "yes" ]; then
            cat <<EOF

    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSResolver
      metadata:
        name: lan-resolver
      spec:
        listen:
          - name: lan
            addresses:
              - ${lan_ip}
            port: 53
EOF
            if [ "${mgmt_mode}" = "separate" ] && [ -n "${mgmt_ip}" ]; then
                cat <<EOF
          - name: mgmt
            addresses:
              - ${mgmt_ip}
            port: 53
EOF
            fi
            cat <<EOF
        sources:
          - name: default
            kind: upstream
            match:
              - "."
            upstreams:
EOF
            for upstream in ${dns_upstreams}; do
                case "${upstream}" in
                    http://*|https://*|udp://*|tcp://*|tls://*|quic://*)
                        echo "              - ${upstream}"
                        ;;
                    *:*)
                        echo "              - udp://[${upstream}]:53"
                        ;;
                    *)
                        echo "              - udp://${upstream}:53"
                        ;;
                esac
            done
            cat <<EOF
        cache:
          enabled: true
          maxEntries: 10000
EOF
        fi
        if [ "${dhcp4_enabled}" = "yes" ]; then
            cat <<EOF

    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv4Server
      metadata:
        name: lan-dhcpv4
      spec:
        server: dnsmasq
        managed: true
        listenInterfaces:
          - lan
        interface: lan
        addressPool:
          start: ${dhcp4_start}
          end: ${dhcp4_end}
          leaseTime: 12h
        gateway: ${lan_ip}
        dnsServers:
          - ${lan_ip}
        ntpServers:
          - ${lan_ip}
EOF
        fi
        if [ "${dhcp6_enabled}" = "yes" ]; then
            cat <<EOF

    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv6Server
      metadata:
        name: lan-dhcpv6
      spec:
        server: dnsmasq
        managed: true
        listenInterfaces:
          - lan
        interface: lan
        mode: stateless
        leaseTime: 12h
EOF
        fi
        if [ "${ra_enabled}" = "yes" ]; then
            cat <<EOF

    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv6RouterAdvertisement
      metadata:
        name: lan-ra
      spec:
        interface: lan
        oFlag: true
        prefix: ${lan_ipv6_prefix}
        prfPreference: medium
EOF
        fi
        if [ "${ntp_enabled}" = "yes" ]; then
            cat <<EOF

    - apiVersion: system.routerd.net/v1alpha1
      kind: NTPServer
      metadata:
        name: lan-ntp
      spec:
        provider: chrony
        managed: true
        source: static
        listenAddresses:
          - ${lan_ip}
        allowCIDRs:
          - ${lan_cidr}
        servers:
          - ntp.nict.jp
          - ntp.jst.mfeed.ad.jp
EOF
        fi
        if [ "${nat44_enabled}" = "yes" ]; then
            cat <<EOF

    - apiVersion: net.routerd.net/v1alpha1
      kind: NAT44Rule
      metadata:
        name: lan-to-wan
      spec:
        type: masquerade
        egressInterface: wan
        sourceRanges:
          - ${lan_cidr}
        excludeDestinationCIDRs:
          - 10.0.0.0/8
          - 172.16.0.0/12
          - 192.168.0.0/16
EOF
        fi
        if [ "${firewall_enabled}" = "yes" ]; then
            cat <<EOF

    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallZone
      metadata:
        name: wan
      spec:
        role: untrust
        interfaces:
          - wan

    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallZone
      metadata:
        name: lan
      spec:
        role: trust
        interfaces:
          - lan
EOF
            if [ "${mgmt_mode}" = "separate" ]; then
                cat <<EOF

    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallZone
      metadata:
        name: mgmt
      spec:
        role: mgmt
        interfaces:
          - mgmt
EOF
            fi
            cat <<EOF

    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallPolicy
      metadata:
        name: default
      spec:
        logDeny: true
        sameRoleAccept: true
EOF
        fi
        cat <<EOF

    - apiVersion: system.routerd.net/v1alpha1
      kind: WebConsole
      metadata:
        name: local
      spec:
        enabled: true
EOF
        if [ "${mgmt_mode}" = "separate" ] && [ -n "${mgmt_ip}" ]; then
            echo "        listenAddress: ${mgmt_ip}"
        else
            echo "        listenAddress: ${lan_ip}"
        fi
        cat <<EOF
        port: 8080
        title: ${router_name}
EOF
    } > "${output}"
}

maybe_start_live_routerd()
{
    routerd_bin=$1
    final_config=$2
    socket_path=/run/routerd/routerd.sock
    if [ -S "${socket_path}" ]; then
        return 0
    fi
    if [ ! -f /media/cdrom/routerd.apkovl.tar.gz ]; then
        return 0
    fi
    if [ ! -x "${routerd_bin}" ]; then
        return 0
    fi
    mkdir -p /run/routerd /var/lib/routerd
    echo "starting live routerd daemon for Web Console and DNS resolver"
    nohup "${routerd_bin}" serve \
        --config "${final_config}" \
        --socket "${socket_path}" \
        --controller-chain \
        --controller-chain-dry-run-dns-resolver=false \
        > /var/log/routerd-live.log 2>&1 &
    sleep 1
}

run_configure()
{
    configure_terminal
    sysconfdir="${prefix}/etc/routerd"
    candidate="${sysconfdir}/router.yaml.configure"
    final_config="${sysconfdir}/router.yaml"
    routerd_bin="${prefix}/sbin/routerd"
    routerctl_bin="${prefix}/sbin/routerctl"
    if [ -x bin/routerd ]; then
        routerd_bin=bin/routerd
    fi
    if [ -x bin/routerctl ]; then
        routerctl_bin=bin/routerctl
    fi

    echo "routerd initial configuration wizard"
    echo
    echo "Available interfaces:"
    show_interfaces
    echo

    router_name=$(prompt_value ROUTERD_ROUTER_NAME "Router name" "routerd-router" 1)
    wan_interface=$(prompt_value ROUTERD_WAN_INTERFACE "WAN interface" "" 1)
    interface_exists "${wan_interface}" || echo "warning: interface ${wan_interface} was not found on this host" >&2
    wan_mode=$(prompt_choice ROUTERD_WAN_MODE "WAN IPv4 mode (dhcp/static)" "dhcp" "dhcp static")
    wan_address=
    wan_gateway=
    wan_dns=${ROUTERD_WAN_DNS:-}
    if [ "${wan_mode}" = "static" ]; then
        wan_address=$(prompt_value ROUTERD_WAN_ADDRESS "WAN static address/CIDR" "" 1)
        validate_cidr "${wan_address}" "WAN address"
        wan_gateway=$(prompt_value ROUTERD_WAN_GATEWAY "WAN gateway" "" 1)
        wan_dns=$(prompt_value ROUTERD_WAN_DNS "WAN DNS upstreams (space separated)" "1.1.1.1" 1)
    else
        wan_dns=$(prompt_value ROUTERD_WAN_DNS "Default DNS upstreams when DHCP DNS is unavailable" "1.1.1.1" 1)
    fi

    lan_interface=$(prompt_value ROUTERD_LAN_INTERFACE "LAN interface" "" 1)
    interface_exists "${lan_interface}" || echo "warning: interface ${lan_interface} was not found on this host" >&2
    if [ "${lan_interface}" = "${wan_interface}" ]; then
        echo "LAN interface must differ from WAN interface" >&2
        exit 2
    fi
    lan_address=$(prompt_value ROUTERD_LAN_ADDRESS "LAN address/CIDR" "192.168.10.1/24" 1)
    validate_cidr "${lan_address}" "LAN address"
    lan_default_prefix=$(address_without_prefix "${lan_address}")
    lan_cidr=$(prompt_value ROUTERD_LAN_CIDR "LAN client CIDR" "${lan_default_prefix%.*}.0/24" 1)
    lan_pool_prefix=${lan_default_prefix%.*}
    lan_ipv6_prefix=
    dhcp4_enabled=$(prompt_bool ROUTERD_ENABLE_DHCPV4 "Enable DHCPv4 server? (yes/no)" "yes")
    dhcp4_start=
    dhcp4_end=
    if [ "${dhcp4_enabled}" = "yes" ]; then
        dhcp4_start=$(prompt_value ROUTERD_DHCPV4_START "DHCPv4 pool start" "${lan_pool_prefix}.100" 1)
        dhcp4_end=$(prompt_value ROUTERD_DHCPV4_END "DHCPv4 pool end" "${lan_pool_prefix}.200" 1)
    fi
    dhcp6_enabled=$(prompt_bool ROUTERD_ENABLE_DHCPV6 "Enable DHCPv6 stateless service? (yes/no)" "no")
    ra_enabled=$(prompt_bool ROUTERD_ENABLE_RA "Enable IPv6 RA? (yes/no)" "no")
    if [ "${ra_enabled}" = "yes" ]; then
        lan_ipv6_prefix=$(prompt_value ROUTERD_LAN_IPV6_PREFIX "LAN IPv6 prefix for RA" "fd00:10::/64" 1)
        validate_cidr "${lan_ipv6_prefix}" "LAN IPv6 prefix"
    fi
    dns_enabled=$(prompt_bool ROUTERD_ENABLE_DNS "Enable DNS resolver? (yes/no)" "yes")
    ntp_enabled=$(prompt_bool ROUTERD_ENABLE_NTP "Enable NTP server? (yes/no)" "yes")
    firewall_enabled=$(prompt_bool ROUTERD_ENABLE_FIREWALL "Enable 3-role firewall? (yes/no)" "yes")
    nat44_enabled=$(prompt_bool ROUTERD_ENABLE_NAT44 "Enable NAT44 from LAN to WAN? (yes/no)" "yes")

    mgmt_mode=$(prompt_choice ROUTERD_MGMT_MODE "Management placement (separate/lan)" "lan" "separate lan")
    mgmt_interface=
    mgmt_address=
    if [ "${mgmt_mode}" = "separate" ]; then
        mgmt_interface=$(prompt_value ROUTERD_MGMT_INTERFACE "MGMT interface" "" 1)
        interface_exists "${mgmt_interface}" || echo "warning: interface ${mgmt_interface} was not found on this host" >&2
        if [ "${mgmt_interface}" = "${wan_interface}" ] || [ "${mgmt_interface}" = "${lan_interface}" ]; then
            echo "MGMT interface must differ from WAN and LAN interfaces" >&2
            exit 2
        fi
        mgmt_address=$(prompt_value ROUTERD_MGMT_ADDRESS "MGMT address/CIDR (blank to leave external)" "" 0)
        if [ -n "${mgmt_address}" ]; then
            validate_cidr "${mgmt_address}" "MGMT address"
        fi
    fi

    usb_persistence_default=${ROUTERD_ENABLE_USB_PERSISTENCE:-no}
    usb_persistence=$(prompt_bool ROUTERD_ENABLE_USB_PERSISTENCE "Save config to USB for diskless persistence? (yes/no)" "${usb_persistence_default}")
    usb_device=
    usb_flush=yes
    log_limit=100M
    if [ "${usb_persistence}" = "yes" ]; then
        echo "Available USB or block partitions:"
        show_usb_devices
        usb_device=$(prompt_value ROUTERD_USB_DEVICE "USB partition device for routerd persistence" "${ROUTERD_USB_DEVICE:-}" 1)
        usb_flush=$(prompt_bool ROUTERD_USB_FLUSH "Flush tmpfs logs and state to USB once per day? (yes/no)" "${ROUTERD_USB_FLUSH:-yes}")
        log_limit=$(prompt_value ROUTERD_LOG_TMPFS_LIMIT "tmpfs log buffer size" "${ROUTERD_LOG_TMPFS_LIMIT:-100M}" 1)
    fi

    if [ "${dry_run}" -eq 1 ]; then
        echo "dry-run: install -d -m 0755 ${sysconfdir}"
    else
        install -d -m 0755 "${sysconfdir}"
    fi
    if [ "${dry_run}" -eq 1 ]; then
        tmp=$(mktemp "${TMPDIR:-/tmp}/routerd-configure.XXXXXX")
    else
        tmp="${candidate}.$$"
    fi
    write_router_config "${tmp}" "${router_name}" "${wan_interface}" "${wan_mode}" "${wan_address}" "${wan_gateway}" "${wan_dns}" \
        "${lan_interface}" "${lan_address}" "${lan_cidr}" "${lan_ipv6_prefix}" "${dhcp4_enabled}" "${dhcp4_start}" "${dhcp4_end}" \
        "${dhcp6_enabled}" "${ra_enabled}" "${dns_enabled}" "${ntp_enabled}" "${firewall_enabled}" "${nat44_enabled}" \
        "${mgmt_mode}" "${mgmt_interface}" "${mgmt_address}"
    if [ "${dry_run}" -eq 1 ]; then
        echo "dry-run: write ${candidate}"
        cat "${tmp}"
        rm -f "${tmp}"
        completed=1
        return 0
    fi
    mv -f "${tmp}" "${candidate}"
    chmod 0600 "${candidate}"
    echo "generated candidate config: ${candidate}"

    if command -v diff >/dev/null 2>&1 && [ -f "${final_config}" ]; then
        echo "diff against existing router.yaml:"
        diff -u "${final_config}" "${candidate}" || true
    else
        echo "candidate config:"
        sed -n '1,260p' "${candidate}"
    fi

    if [ "${configure_yes}" -ne 1 ] && [ "${configure_non_interactive}" -ne 1 ]; then
        answer=$(prompt_bool ROUTERD_CONFIGURE_CONFIRM "Install this config as router.yaml? (yes/no)" "no")
        if [ "${answer}" != "yes" ]; then
            echo "left candidate config at ${candidate}"
            completed=1
            return 0
        fi
    fi
    if [ -f "${final_config}" ]; then
        cp -p "${final_config}" "${final_config}.backup.${timestamp}"
        echo "backup: ${final_config}.backup.${timestamp}"
    fi
    install -m 0600 "${candidate}" "${final_config}"
    echo "installed config: ${final_config}"
    save_config_persistence "${usb_persistence}" "${usb_device}" "${final_config}" "${usb_flush}" "${log_limit}"

    if [ ! -x "${routerd_bin}" ]; then
        echo "routerd binary not found at ${routerd_bin}; skipping validate/apply" >&2
        completed=1
        return 0
    fi
    "${routerd_bin}" validate --config "${final_config}"
    "${routerd_bin}" plan --config "${final_config}" || true
    if [ "${configure_apply}" -eq 1 ]; then
        "${routerd_bin}" apply --config "${final_config}" --once
        maybe_start_live_routerd "${routerd_bin}" "${final_config}"
        if [ -x "${routerctl_bin}" ]; then
            "${routerctl_bin}" status || true
        fi
    else
        echo "apply skipped by --no-apply"
    fi
    completed=1
}

service_active=0
service_touched=0
manage_host_service=1

if [ "$#" -gt 0 ]; then
    case "$1" in
        configure)
            command_mode=configure
            shift
            ;;
        --configure)
            command_mode=configure
            shift
            ;;
    esac
fi

while [ "$#" -gt 0 ]; do
    case "$1" in
        --prefix)
            shift
            [ "$#" -gt 0 ] || { echo "--prefix requires a value" >&2; exit 2; }
            prefix=$1
            ;;
        --enable-service)
            enable_service=1
            ;;
        --start-service)
            start_service=1
            ;;
        --no-restart)
            restart_service=0
            ;;
        --dry-run)
            dry_run=1
            ;;
        --verbose)
            verbose=1
            ;;
        --no-config-update)
            config_update=0
            ;;
        --no-install-deps)
            install_deps=0
            ;;
        --list-deps)
            list_deps=1
            install_deps=0
            ;;
        --deps-only)
            deps_only=1
            ;;
        --with-tailscale)
            with_tailscale=1
            ;;
        --configure)
            command_mode=configure
            ;;
        --non-interactive)
            configure_non_interactive=1
            ;;
        --yes|-y)
            configure_yes=1
            ;;
        --no-apply)
            configure_apply=0
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
    shift
done

if [ "${verbose}" -eq 1 ]; then
    set -x
fi

os=$(uname -s)
arch=$(detect_arch)
target_os=$(os_id)
target_expected="${target_os}-${arch}"
target_archive=
if [ -f share/doc/TARGET ]; then
    target_archive=$(sed -n '1p' share/doc/TARGET)
fi
bindir="${prefix}/sbin"
sysconfdir="${prefix}/etc/routerd"
if [ "${prefix}" != "/usr/local" ]; then
    manage_host_service=0
    echo "non-default prefix ${prefix}; skipping host service manager"
fi
if [ -n "${target_archive}" ] && [ "${target_archive}" != "${target_expected}" ]; then
    if [ "${manage_host_service}" -eq 1 ]; then
        echo "archive target ${target_archive} does not match host ${target_expected}" >&2
        echo "download the routerd archive for ${target_expected}" >&2
        exit 2
    fi
    echo "warning: archive target ${target_archive} does not match host ${target_expected}; continuing for non-system prefix" >&2
fi

if [ "${command_mode}" = "configure" ]; then
    completed=0
    run_configure
    exit 0
fi

run_dependency_install
if [ "${list_deps}" -eq 1 ] || [ "${deps_only}" -eq 1 ]; then
    completed=1
    exit 0
fi

backup_dir=$(mktemp -d "${TMPDIR:-/tmp}/routerd-install.XXXXXX")
touch "${backup_dir}/restore.list" "${backup_dir}/remove.list"
trap cleanup EXIT HUP INT TERM

if [ -x "${bindir}/routerd" ]; then
    mode=upgrade
    old_version=$("${bindir}/routerd" --version 2>/dev/null || true)
else
    mode=fresh
    old_version=
fi
new_version=
if [ -x bin/routerd ]; then
    new_version=$(bin/routerd --version 2>/dev/null || true)
fi

echo "routerd install mode: ${mode}"
if [ -n "${old_version}" ]; then
    echo "existing: ${old_version}"
fi
if [ -n "${new_version}" ]; then
    echo "installing: ${new_version}"
fi

case "${os}" in
    Linux)
        if [ "${manage_host_service}" -eq 1 ] && command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet routerd.service; then
            service_active=1
        fi
        ;;
    FreeBSD)
        if [ "${manage_host_service}" -eq 1 ] && command -v service >/dev/null 2>&1 && service routerd status >/dev/null 2>&1; then
            service_active=1
        fi
        ;;
esac

if [ "${dry_run}" -eq 1 ]; then
    echo "dry-run: install -d -m 0755 ${bindir}"
else
    install -d -m 0755 "${bindir}"
fi
for binary in bin/*; do
    [ -f "${binary}" ] || continue
    atomic_install 0755 "${binary}" "${bindir}/$(basename "${binary}")"
done

if [ "${config_update}" -eq 1 ]; then
    if [ "${dry_run}" -eq 1 ]; then
        echo "dry-run: install -d -m 0755 ${sysconfdir}"
    else
        install -d -m 0755 "${sysconfdir}"
    fi
    if [ -f etc/routerd/router.yaml.sample ]; then
        atomic_install 0644 etc/routerd/router.yaml.sample "${sysconfdir}/router.yaml.sample"
    fi
else
    echo "config sample update skipped by --no-config-update"
fi

if [ "${config_update}" -eq 1 ] && [ -f "${sysconfdir}/router.yaml" ] && [ -f "${sysconfdir}/router.yaml.sample" ]; then
    echo "existing config preserved: ${sysconfdir}/router.yaml"
    echo "new sample config: ${sysconfdir}/router.yaml.sample"
    if command -v diff >/dev/null 2>&1; then
        echo "config diff against new sample:"
        diff -u "${sysconfdir}/router.yaml" "${sysconfdir}/router.yaml.sample" || true
    fi
elif [ "${config_update}" -eq 0 ] && [ -f "${sysconfdir}/router.yaml" ]; then
    echo "existing config preserved: ${sysconfdir}/router.yaml"
    echo "sample config left unchanged by --no-config-update"
else
    echo "create ${sysconfdir}/router.yaml before starting routerd"
fi

case "${os}" in
    Linux)
        if [ "${manage_host_service}" -eq 1 ] && [ -d systemd ]; then
            if [ "${dry_run}" -eq 1 ]; then
                echo "dry-run: install -d -m 0755 /etc/systemd/system"
            else
                install -d -m 0755 /etc/systemd/system
            fi
            for unit in systemd/*.service; do
                [ -f "${unit}" ] || continue
                atomic_install 0644 "${unit}" "/etc/systemd/system/$(basename "${unit}")"
            done
            if command -v systemctl >/dev/null 2>&1; then
                if [ "${dry_run}" -eq 1 ]; then
                    echo "dry-run: systemctl daemon-reload"
                else
                    systemctl daemon-reload
                fi
                if [ "${enable_service}" -eq 1 ]; then
                    if [ "${dry_run}" -eq 1 ]; then
                        echo "dry-run: systemctl enable routerd.service"
                    else
                        systemctl enable routerd.service
                    fi
                fi
                if [ "${start_service}" -eq 1 ] || { [ "${service_active}" -eq 1 ] && [ "${restart_service}" -eq 1 ]; }; then
                    if [ "${dry_run}" -eq 1 ]; then
                        echo "dry-run: systemctl restart routerd.service"
                    else
                        systemctl restart routerd.service
                        service_touched=1
                    fi
                fi
            fi
        fi
        ;;
    FreeBSD)
        if [ "${manage_host_service}" -eq 1 ] && [ -d rc.d ]; then
            if [ "${dry_run}" -eq 1 ]; then
                echo "dry-run: install -d -m 0755 ${prefix}/etc/rc.d"
            else
                install -d -m 0755 "${prefix}/etc/rc.d"
            fi
            for script in rc.d/*; do
                [ -f "${script}" ] || continue
                atomic_install 0555 "${script}" "${prefix}/etc/rc.d/$(basename "${script}")"
            done
            if [ "${enable_service}" -eq 1 ] && command -v sysrc >/dev/null 2>&1; then
                if [ "${dry_run}" -eq 1 ]; then
                    echo "dry-run: sysrc routerd_enable=YES"
                else
                    sysrc routerd_enable=YES >/dev/null
                fi
            fi
            if [ "${start_service}" -eq 1 ] || { [ "${service_active}" -eq 1 ] && [ "${restart_service}" -eq 1 ]; }; then
                if command -v service >/dev/null 2>&1; then
                    if [ "${service_active}" -eq 1 ]; then
                        if [ "${dry_run}" -eq 1 ]; then
                            echo "dry-run: service routerd restart"
                        else
                            service routerd restart
                            service_touched=1
                        fi
                    else
                        if [ "${dry_run}" -eq 1 ]; then
                            echo "dry-run: service routerd onestart"
                        else
                            service routerd onestart
                            service_touched=1
                        fi
                    fi
                fi
            fi
        fi
        ;;
    *)
        echo "unsupported OS: ${os}" >&2
        exit 1
        ;;
esac

if [ "${dry_run}" -eq 0 ] && [ -x "${bindir}/routerctl" ]; then
    case "${os}" in
        Linux) status_socket=/run/routerd/routerd.sock ;;
        FreeBSD) status_socket=/var/run/routerd/routerd.sock ;;
        *) status_socket= ;;
    esac
    if [ -n "${status_socket}" ] && [ -S "${status_socket}" ]; then
        echo "routerctl status:"
        "${bindir}/routerctl" status --socket "${status_socket}" || {
            if [ "${service_touched}" -eq 1 ]; then
                echo "warning: routerctl status failed after service restart" >&2
            fi
        }
    else
        echo "routerctl status skipped: socket not found"
    fi
fi

completed=1
echo "routerd ${mode} completed under ${prefix}"
echo "state preserved: /var/lib/routerd, /var/db/routerd, /run/routerd, /var/run/routerd, /var/log/otelcol"
