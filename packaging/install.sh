#!/bin/sh
set -eu

prefix=/usr/local
enable_service=0
start_service=0
restart_service=1
dry_run=0
verbose=0
config_update=1
completed=0
backup_dir=
timestamp=$(date +%Y%m%d%H%M%S)

usage()
{
    cat <<'USAGE'
Usage: ./install.sh [--prefix DIR] [--enable-service] [--start-service] [--no-restart] [--dry-run] [--verbose] [--no-config-update]

Installs or upgrades routerd binaries, service templates, and a sample configuration.
Existing /usr/local/etc/routerd/router.yaml is never overwritten.
State databases, logs, and runtime files are never modified.
USAGE
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

service_active=0
service_touched=0

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
bindir="${prefix}/sbin"
sysconfdir="${prefix}/etc/routerd"
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
        if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet routerd.service; then
            service_active=1
        fi
        ;;
    FreeBSD)
        if command -v service >/dev/null 2>&1 && service routerd status >/dev/null 2>&1; then
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
        if [ -d systemd ]; then
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
        if [ -d rc.d ]; then
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
