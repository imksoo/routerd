#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
set -eu

prefix=/usr/local
purge_config=0
purge_state=0
assume_yes=0
dry_run=0
verbose=0

usage()
{
    cat <<'USAGE'
Usage: ./uninstall.sh [--prefix DIR] [--purge-config] [--purge-state] [--all] [--yes] [--dry-run] [--verbose]

Removes routerd binaries, service templates, and runtime files.
Configuration and state are preserved unless purge options are specified.
USAGE
}

run()
{
    if [ "${dry_run}" -eq 1 ]; then
        printf 'dry-run:'
        printf ' %s' "$@"
        printf '\n'
    else
        "$@"
    fi
}

rm_path()
{
    path=$1
    if [ -e "${path}" ] || [ -L "${path}" ]; then
        run rm -rf "${path}"
    elif [ "${verbose}" -eq 1 ]; then
        echo "skip missing: ${path}"
    fi
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --prefix)
            shift
            [ "$#" -gt 0 ] || { echo "--prefix requires a value" >&2; exit 2; }
            prefix=$1
            ;;
        --purge-config)
            purge_config=1
            ;;
        --purge-state)
            purge_state=1
            ;;
        --all)
            purge_config=1
            purge_state=1
            ;;
        --yes|-y)
            assume_yes=1
            ;;
        --dry-run)
            dry_run=1
            ;;
        --verbose)
            verbose=1
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

if [ "${assume_yes}" -ne 1 ]; then
    if [ -t 0 ]; then
        echo "This will remove routerd binaries, service templates, and runtime files."
        if [ "${purge_config}" -eq 1 ]; then
            echo "Configuration under ${prefix}/etc/routerd will also be removed."
        fi
        if [ "${purge_state}" -eq 1 ]; then
            echo "State and logs under /var/lib/routerd, /var/db/routerd, and /var/log/otelcol will also be removed."
        fi
        printf 'Continue? [y/N] '
        IFS= read -r answer
        case "${answer}" in
            y|Y|yes|YES) ;;
            *) echo "aborted"; exit 1 ;;
        esac
    else
        echo "refusing to uninstall without a terminal; pass --yes to confirm" >&2
        exit 2
    fi
fi

os=$(uname -s)
manage_host_service=1
if [ "${prefix}" != "/usr/local" ]; then
    manage_host_service=0
    echo "non-default prefix ${prefix}; skipping host service manager and global runtime removal"
fi
case "${os}" in
    Linux)
        if [ "${manage_host_service}" -eq 1 ] && command -v systemctl >/dev/null 2>&1; then
            run systemctl disable --now routerd.service || true
        fi
        if [ "${manage_host_service}" -eq 1 ]; then
            rm_path /etc/systemd/system/routerd.service
            if command -v systemctl >/dev/null 2>&1; then
                run systemctl daemon-reload || true
            fi
            rm_path /run/routerd
        fi
        ;;
    FreeBSD)
        if [ "${manage_host_service}" -eq 1 ] && command -v service >/dev/null 2>&1; then
            run service routerd stop || true
        fi
        if [ "${manage_host_service}" -eq 1 ] && command -v sysrc >/dev/null 2>&1; then
            run sysrc -x routerd_enable || true
        fi
        rm_path "${prefix}/etc/rc.d/routerd"
        if [ "${manage_host_service}" -eq 1 ]; then
            rm_path /var/run/routerd
        fi
        ;;
    *)
        echo "unsupported OS: ${os}" >&2
        exit 1
        ;;
esac

for binary in \
    routerd \
    routerctl \
    routerd-dhcpv4-client \
    routerd-dhcpv6-client \
    routerd-dhcp-event-relay \
    routerd-healthcheck \
    routerd-dns-resolver \
    routerd-firewall-logger \
    routerd-dpi-classifier \
    routerd-pppoe-client
do
    rm_path "${prefix}/sbin/${binary}"
done

if [ "${purge_config}" -eq 1 ]; then
    rm_path "${prefix}/etc/routerd"
else
    echo "configuration preserved: ${prefix}/etc/routerd"
fi

if [ "${purge_state}" -eq 1 ]; then
    if [ "${manage_host_service}" -eq 1 ]; then
        rm_path /var/lib/routerd
        rm_path /var/db/routerd
        rm_path /var/log/otelcol
    else
        echo "global state purge skipped for non-default prefix ${prefix}"
    fi
else
    echo "state preserved: /var/lib/routerd, /var/db/routerd, /var/log/otelcol"
fi

echo "routerd uninstall completed"
