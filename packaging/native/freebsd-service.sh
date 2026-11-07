#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
set -eu
# Each native pkg hook embeds this body and assigns its own phase.
# shellcheck disable=SC2154
: "$phase"

# An image/root-directory installation must never operate the build host.
case "${PKG_ROOTDIR:-/}" in /) ;; *) exit 0 ;; esac
state=/var/run/routerd-package
rcdir=/usr/local/etc/rc.d

capture_active()
{
    install -d -m 0700 "$state"
    temporary=$state/restart-units.new
    trap 'rm -f "$temporary"' EXIT
    : > "$temporary"
    if [ -f "$state/restart-units" ]; then cat "$state/restart-units" > "$temporary"; fi
    for path in "$rcdir/routerd" "$rcdir"/routerd_*; do
        [ -x "$path" ] || continue
        # Only the native daemon and generated helpers executing our binaries.
        if [ "${path##*/}" != routerd ]; then
            grep -Fxq '# Managed by routerd.' "$path" || continue
            grep -Fq /usr/local/sbin/routerd "$path" || continue
        fi
        unit=${path##*/}
        result=0
        service "$unit" onestatus >/dev/null 2>&1 || result=$?
        case "$result" in
            0) printf '%s\n' "$unit" >> "$temporary" ;;
            1) ;;
            *) echo "routerd: cannot establish $unit service ownership/status" >&2; return 1 ;;
        esac
    done
    sort -u -o "$temporary" "$temporary"
    mv -f "$temporary" "$state/restart-units"
}

stop_one()
{
    unit=$1
    service "$unit" onestop
    result=0
    service "$unit" onestatus >/dev/null 2>&1 || result=$?
    case "$result" in
        1) ;;
        0) echo "routerd: $unit is still active; refusing replacement" >&2; return 1 ;;
        *) echo "routerd: cannot confirm $unit stopped" >&2; return 1 ;;
    esac
}

stop_services()
{
    capture_active
    if grep -Fxq routerd "$state/restart-units"; then stop_one routerd; fi
    # Main is stopped and cannot create a new helper while we snapshot it.
    capture_active
    while IFS= read -r unit; do
        case "$unit" in
            routerd|'') continue ;;
            routerd_*) stop_one "$unit" ;;
            *) echo "routerd: unexpected service name $unit" >&2; return 1 ;;
        esac
    done < "$state/restart-units"
}

restore_services()
{
    [ -f "$state/restart-units" ] || return 0
    while IFS= read -r unit; do
        case "$unit" in
            routerd|'') continue ;;
            routerd_*) service "$unit" onestart ;;
            *) echo "routerd: unexpected service name $unit" >&2; return 1 ;;
        esac
    done < "$state/restart-units"
    rm -f "$state/restart-units"
    rmdir "$state"
}

case "$phase" in
    pre-install)
        # pkg treats a failing new pre-install as fatal before replacing files.
        # No discovery/adoption of source or manual installations is performed.
        if [ -x "$rcdir/routerd" ]; then stop_services; fi
        ;;
    # pkg reaps descendants started inside hooks. Native HANDLE_RC_SCRIPTS
    # starts enabled rc.d services after script cleanup; rc.d restores helpers.
    post-install) : ;;
    post-start) restore_services ;;
    pre-deinstall)
        if [ "${PKG_UPGRADE:-false}" != true ]; then stop_services; fi
        ;;
    post-deinstall)
        if [ "${PKG_UPGRADE:-false}" != true ]; then
            for path in "$rcdir"/routerd_*; do
                [ -f "$path" ] || continue
                grep -Fxq '# Managed by routerd.' "$path" || continue
                grep -Fq /usr/local/sbin/routerd "$path" || continue
                rm -f "$path"
            done
            rm -f "$state/restart-units"
            if [ -d "$state" ]; then rmdir "$state"; fi
        fi
        ;;
esac
