#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
# Embedded in each maintainer script: prerm runs before unpack and postrm can
# run after package files have disappeared. No installed helper is required.
set -eu
# The builder assigns phase separately in each emitted maintainer script.
# shellcheck disable=SC2154
: "$phase"

state=/run/routerd-package

invoke_service()
{
    action=$1
    unit=$2
    if command -v policy-rc.d >/dev/null 2>&1; then
        policy=0
        policy-rc.d "$unit" "$action" || policy=$?
        case "$policy" in
            0|104) ;;
            101) return 101 ;;
            *) echo "routerd: service policy failed ($policy)" >&2; return 1 ;;
        esac
    fi
    systemctl "$action" -- "$unit"
}

capture_active()
{
    listing=$(systemctl list-units --all --type=service,timer \
        --state=active,activating,reloading,deactivating --plain --no-legend \
        'routerd*.service' 'routerd-self-restart-*.timer')
    install -d -m 0700 "$state"
    temporary=$state/restart-units.new
    trap 'rm -f "$temporary"' EXIT
    : > "$temporary"
    if [ -f "$state/restart-units" ]; then
        cat "$state/restart-units" > "$temporary"
    fi
    printf '%s\n' "$listing" | awk 'NF {print $1}' | while IFS= read -r unit; do
        case "$unit" in
            routerd.service|routerd-self-restart-*.service|routerd-self-restart-*.timer) ;;
            routerd-*.service)
                fragment=$(systemctl show --property=FragmentPath --value -- "$unit")
                if [ -z "$fragment" ] || [ ! -r "$fragment" ]; then
                    echo "routerd: cannot inspect $unit ownership" >&2
                    exit 1
                fi
                grep -Fxq '# Managed by routerd. Do not edit by hand.' "$fragment" || continue
                ;;
            *) continue ;;
        esac
        printf '%s\n' "$unit"
    done >> "$temporary"
    sort -u -o "$temporary" "$temporary"
    mv -f "$temporary" "$state/restart-units"
}

stop_services()
{
    capture_active
    # Stop pending self-restarts before stopping the daemon. Never replay them.
    for kind in timer main helper; do
        if [ "$kind" = helper ]; then
            # Main is now stopped and cannot create/restart helpers between
            # the initial snapshot and its own shutdown.
            capture_active
        fi
        while IFS= read -r unit; do
            [ -n "$unit" ] || continue
            case "$unit" in
                routerd-self-restart-*.timer) [ "$kind" = timer ] || continue ;;
                routerd.service) [ "$kind" = main ] || continue ;;
                routerd-*.service) [ "$kind" = helper ] || continue ;;
                *) echo "routerd: unexpected service name $unit" >&2; return 1 ;;
            esac
            result=0
            invoke_service stop "$unit" || result=$?
            [ "$result" -eq 0 ] || [ "$result" -eq 101 ] || return "$result"
            observed=$(systemctl show --property=ActiveState --value -- "$unit")
            case "$observed" in
                inactive|failed) ;;
                *) echo "routerd: $unit is still $observed; refusing replacement" >&2; return 1 ;;
            esac
        done < "$state/restart-units"
    done
}

restore_services()
{
    systemctl daemon-reload
    [ -f "$state/restart-units" ] || return 0
    # Main first; it reconciles current desired state and helper definitions.
    if grep -Fxq routerd.service "$state/restart-units"; then
        result=0
        invoke_service start routerd.service || result=$?
        [ "$result" -ne 101 ] || return 0
        [ "$result" -eq 0 ] || return "$result"
    fi
    while IFS= read -r unit; do
        case "$unit" in
            routerd.service|routerd-self-restart-*|'') continue ;;
            routerd-*.service) ;;
            *) echo "routerd: unexpected service name $unit" >&2; return 1 ;;
        esac
        result=0
        invoke_service start "$unit" || result=$?
        [ "$result" -ne 101 ] || return 0
        [ "$result" -eq 0 ] || return "$result"
    done < "$state/restart-units"
    rm -f "$state/restart-units"
    rmdir "$state"
}

case "$phase:${1:-}" in
    prerm:upgrade|prerm:remove)
        if [ -d /run/systemd/system ]; then
            stop_services
            if [ "$1" = remove ]; then systemctl disable -- routerd.service; fi
        fi
        ;;
    postinst:configure|postinst:abort-upgrade|postinst:abort-remove|postinst:abort-deconfigure)
        if [ -d /run/systemd/system ]; then restore_services; fi
        ;;
    postrm:remove|postrm:purge)
        # Keep operator configuration and database, including on purge. Only
        # generated service artifacts are disposable; no manual-install scan.
        for path in /etc/systemd/system/routerd.service /etc/systemd/system/routerd-*.service; do
            [ -f "$path" ] || continue
            grep -Fxq '# Managed by routerd. Do not edit by hand.' "$path" || continue
            unit=${path##*/}
            if [ -d /run/systemd/system ]; then
                systemctl disable -- "$unit"
            fi
            rm -f "$path"
        done
        if [ -d /run/systemd/system ]; then
            systemctl daemon-reload
        fi
        rm -f "$state/restart-units"
        if [ -d "$state" ]; then rmdir "$state"; fi
        ;;
esac
