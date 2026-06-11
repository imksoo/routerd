#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
set -eu

if [ "$#" -eq 0 ]; then
    echo "usage: $0 ARCHIVE.tar.gz [ARCHIVE.tar.gz ...]" >&2
    exit 2
fi

status=0
for archive in "$@"; do
    if [ ! -f "${archive}" ]; then
        echo "archive not found: ${archive}" >&2
        status=1
        continue
    fi
    list=$(mktemp "${TMPDIR:-/tmp}/routerd-tar-paths.XXXXXX") || exit 1
    verbose=$(mktemp "${TMPDIR:-/tmp}/routerd-tar-verbose.XXXXXX") || {
        rm -f "${list}"
        exit 1
    }
    if ! tar -tzf "${archive}" > "${list}"; then
        echo "failed to list archive: ${archive}" >&2
        rm -f "${list}" "${verbose}"
        status=1
        continue
    fi
    if ! tar -tvzf "${archive}" > "${verbose}"; then
        echo "failed to inspect archive metadata: ${archive}" >&2
        rm -f "${list}" "${verbose}"
        status=1
        continue
    fi
    unsafe=0
    while IFS= read -r path; do
        normalized=${path}
        while :; do
            case "${normalized}" in
                ./*) normalized=${normalized#./} ;;
                *) break ;;
            esac
        done
        normalized=${normalized%/}
        [ -n "${normalized}" ] || continue
        case "${normalized}" in
            /*|..|../*|*/..|*/../*)
                echo "unsafe archive path in ${archive}: ${path}" >&2
                unsafe=1
                break
                ;;
            tmp|tmp/*|var/tmp|var/tmp/*)
                echo "archive must not contain top-level temporary path in ${archive}: ${path}" >&2
                unsafe=1
                break
                ;;
        esac
    done < "${list}"
    if [ "${unsafe}" -ne 0 ]; then
        status=1
    fi
    if [ "${unsafe}" -eq 0 ]; then
        if ! awk -v archive="${archive}" '
            function unsafe_target(target) {
                gsub(/^[[:space:]]+|[[:space:]]+$/, "", target)
                while (target ~ /^\.\//) {
                    sub(/^\.\//, "", target)
                }
                sub(/\/$/, "", target)
                return target ~ /^(\/|\.{2}$|\.{2}\/|.*\/\.{2}$|.*\/\.{2}\/|tmp$|tmp\/|var\/tmp$|var\/tmp\/)/
            }
            substr($0, 1, 1) == "l" && / -> / {
                target = $0
                sub(/^.* -> /, "", target)
                if (unsafe_target(target)) {
                    printf "unsafe archive symlink target in %s: %s\n", archive, $0 > "/dev/stderr"
                    bad = 1
                }
            }
            substr($0, 1, 1) == "h" && / link to / {
                target = $0
                sub(/^.* link to /, "", target)
                if (unsafe_target(target)) {
                    printf "unsafe archive hardlink target in %s: %s\n", archive, $0 > "/dev/stderr"
                    bad = 1
                }
            }
            END { exit bad ? 1 : 0 }
        ' "${verbose}"; then
            status=1
        fi
    fi
    rm -f "${list}" "${verbose}"
done

exit "${status}"
