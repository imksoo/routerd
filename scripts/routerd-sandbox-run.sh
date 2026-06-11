#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
set -eu

if [ "$#" -eq 0 ]; then
    echo "usage: $0 <command> [args...]" >&2
    exit 2
fi

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"

tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/routerd-sandbox.XXXXXX")
root="${tmpdir}/root"
stdout_log="${tmpdir}/serve.stdout"
stderr_log="${tmpdir}/serve.stderr"
pid=

cleanup()
{
    if [ -n "${pid}" ]; then
        kill "${pid}" >/dev/null 2>&1 || true
        wait "${pid}" >/dev/null 2>&1 || true
    fi
    rm -rf "${tmpdir}"
}
trap cleanup EXIT HUP INT TERM

go run ./cmd/routerd serve --sandbox --root "${root}" >"${stdout_log}" 2>"${stderr_log}" &
pid=$!

status_socket="${root}/run/routerd/routerd-status.sock"
ready_timeout="${ROUTERD_SANDBOX_READY_TIMEOUT:-60}"
ready_deadline=$(( $(date +%s) + ready_timeout ))
while [ "$(date +%s)" -lt "${ready_deadline}" ]; do
    if ! kill -0 "${pid}" >/dev/null 2>&1; then
        echo "routerd sandbox serve exited before socket became ready" >&2
        cat "${stdout_log}" >&2 || true
        cat "${stderr_log}" >&2 || true
        exit 1
    fi
    if [ -S "${status_socket}" ]; then
        break
    fi
    sleep 0.1
done
if [ ! -S "${status_socket}" ]; then
    echo "routerd sandbox status socket did not become ready: ${status_socket}" >&2
    cat "${stdout_log}" >&2 || true
    cat "${stderr_log}" >&2 || true
    exit 1
fi

export ROUTERD_SANDBOX_ROOT="${root}"
export ROUTERD_SANDBOX_SOCKET="${root}/run/routerd/routerd.sock"
export ROUTERD_SANDBOX_STATUS_SOCKET="${status_socket}"

"$@"
