#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
set -eu

if [ "${ROUTERD_ALPINE_VM:-}" != "1" ]; then
    echo "refusing to run outside an Alpine VM; set ROUTERD_ALPINE_VM=1 inside the test guest" >&2
    exit 2
fi
if [ ! -r /etc/alpine-release ]; then
    echo "this smoke must run on an installed Alpine host" >&2
    exit 2
fi
for cmd in routerd routerctl apk rc-update rc-service; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "missing command: $cmd" >&2
        exit 2
    fi
done

tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/routerd-alpine-vm.XXXXXX")
sandbox_pid=
cleanup()
{
    if [ -n "${sandbox_pid}" ]; then
        kill "${sandbox_pid}" >/dev/null 2>&1 || true
        wait "${sandbox_pid}" >/dev/null 2>&1 || true
    fi
    rc-service routerd_smoke stop >/dev/null 2>&1 || true
    rc-update del routerd_smoke default >/dev/null 2>&1 || true
    rm -f /etc/init.d/routerd_smoke
    rm -rf "$tmpdir"
}
trap cleanup EXIT HUP INT TERM

config="${tmpdir}/router.yaml"
cat > "$config" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: alpine-vm-smoke
spec:
  resources:
    - apiVersion: system.routerd.net/v1alpha1
      kind: Package
      metadata:
        name: alpine-present
      spec:
        packages:
          - os: alpine
            manager: apk
            names:
              - busybox
EOF

sandbox_root="${tmpdir}/sandbox"
routerd serve --sandbox --root "${sandbox_root}" >"${tmpdir}/sandbox.stdout" 2>"${tmpdir}/sandbox.stderr" &
sandbox_pid=$!
status_socket="${sandbox_root}/run/routerd/routerd-status.sock"
for _ in $(seq 1 100); do
    if ! kill -0 "${sandbox_pid}" >/dev/null 2>&1; then
        cat "${tmpdir}/sandbox.stdout" >&2 || true
        cat "${tmpdir}/sandbox.stderr" >&2 || true
        exit 1
    fi
    if [ -S "${status_socket}" ]; then
        break
    fi
    sleep 0.1
done
test -S "${status_socket}"
routerctl validate --socket "${status_socket}" -f "$config" --replace
routerctl plan --socket "${status_socket}" -f "$config" --replace > "${tmpdir}/plan-status.json"
routerctl render alpine --config "$config" --out-dir "${tmpdir}/render" >/dev/null
test -x "${tmpdir}/render/openrc-routerd"

if [ "${ROUTERD_ALPINE_VM_APPLY:-}" != "1" ]; then
    echo "validated Alpine VM smoke inputs; set ROUTERD_ALPINE_VM_APPLY=1 to exercise rc-update/rc-service" >&2
    exit 0
fi

routerd serve --config "$config" --once --status-file "${tmpdir}/status.json" >/dev/null
rc-update show default | awk '{ print $1 }' | grep -qx routerd_smoke
rc-service routerd_smoke status >/dev/null

routerd serve --config "$config" --once --status-file "${tmpdir}/status-2.json" >/dev/null
rc-service routerd_smoke status >/dev/null

echo "Alpine VM OpenRC smoke passed"
