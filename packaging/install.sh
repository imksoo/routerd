#!/bin/sh
set -eu

prefix=/usr/local
enable_service=0
start_service=0

usage()
{
    cat <<'USAGE'
Usage: ./install.sh [--prefix DIR] [--enable-service] [--start-service]

Installs routerd binaries, service templates, and a sample configuration.
Existing /usr/local/etc/routerd/router.yaml is never overwritten.
USAGE
}

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

os=$(uname -s)
bindir="${prefix}/sbin"
sysconfdir="${prefix}/etc/routerd"

install -d -m 0755 "${bindir}"
for binary in bin/*; do
    [ -f "${binary}" ] || continue
    install -m 0755 "${binary}" "${bindir}/$(basename "${binary}")"
done

install -d -m 0755 "${sysconfdir}"
if [ -f etc/routerd/router.yaml.sample ]; then
    install -m 0644 etc/routerd/router.yaml.sample "${sysconfdir}/router.yaml.sample"
fi

case "${os}" in
    Linux)
        if [ -d systemd ]; then
            install -d -m 0755 /etc/systemd/system
            for unit in systemd/*.service; do
                [ -f "${unit}" ] || continue
                install -m 0644 "${unit}" "/etc/systemd/system/$(basename "${unit}")"
            done
            if command -v systemctl >/dev/null 2>&1; then
                systemctl daemon-reload
                if [ "${enable_service}" -eq 1 ]; then
                    systemctl enable routerd.service
                fi
                if [ "${start_service}" -eq 1 ]; then
                    systemctl restart routerd.service
                fi
            fi
        fi
        ;;
    FreeBSD)
        if [ -d rc.d ]; then
            install -d -m 0755 "${prefix}/etc/rc.d"
            for script in rc.d/*; do
                [ -f "${script}" ] || continue
                install -m 0555 "${script}" "${prefix}/etc/rc.d/$(basename "${script}")"
            done
            if [ "${enable_service}" -eq 1 ] && command -v sysrc >/dev/null 2>&1; then
                sysrc routerd_enable=YES >/dev/null
            fi
            if [ "${start_service}" -eq 1 ] && command -v service >/dev/null 2>&1; then
                service routerd onestart
            fi
        fi
        ;;
    *)
        echo "unsupported OS: ${os}" >&2
        exit 1
        ;;
esac

echo "routerd installed under ${prefix}"
echo "sample config: ${sysconfdir}/router.yaml.sample"
if [ ! -f "${sysconfdir}/router.yaml" ]; then
    echo "create ${sysconfdir}/router.yaml before starting routerd"
fi
