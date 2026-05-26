#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Smoke test for packaging/install.sh: guard against the failure mode
# where install.sh is launched from a working directory that does not
# contain the release payload (bin/, etc/, ...), runs every `bin/*` glob
# unexpanded, and silently exits 0 with "routerd upgrade completed"
# while installing nothing. Reproduced on homert02 on v20260526.2152
# when invoked as `cd /tmp && sudo ./pkg/install.sh ...`.
#
# The script must:
#   1. Exit non-zero when launched from a directory that has no bin/.
#   2. Find the payload when launched from a different cwd, by resolving
#      its own script directory.

set -eu

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
installer="${repo_root}/packaging/install.sh"

if [ ! -f "${installer}" ]; then
    echo "install-sh-cwd-smoke: ${installer} missing" >&2
    exit 2
fi

tmpdir=$(mktemp -d)
trap 'rm -rf "${tmpdir}"' EXIT HUP INT TERM

#
# Case A: payload-less archive must fail loudly.
#
fail_dir="${tmpdir}/no-payload"
mkdir -p "${fail_dir}"
cp "${installer}" "${fail_dir}/install.sh"

set +e
( cd "${fail_dir}" && bash install.sh --dry-run --no-install-deps ) >"${tmpdir}/a.out" 2>&1
rc=$?
set -e

if [ "${rc}" -eq 0 ]; then
    echo "install-sh-cwd-smoke: FAIL (case A) — install.sh exited 0 with no bin/ payload" >&2
    cat "${tmpdir}/a.out" >&2
    exit 1
fi
if ! grep -q "required payload bin/routerd not found" "${tmpdir}/a.out"; then
    echo "install-sh-cwd-smoke: FAIL (case A) — install.sh did not emit the missing-payload diagnostic" >&2
    cat "${tmpdir}/a.out" >&2
    exit 1
fi

#
# Case B: invoked from a different cwd against a populated payload tree
# must resolve its own script directory and find bin/routerd.
#
ok_dir="${tmpdir}/release"
mkdir -p "${ok_dir}/bin"
cp "${installer}" "${ok_dir}/install.sh"
cat >"${ok_dir}/bin/routerd" <<'EOF'
#!/bin/sh
echo "vsmoke-test"
EOF
chmod +x "${ok_dir}/bin/routerd"

set +e
( cd "${tmpdir}" && bash "${ok_dir}/install.sh" --dry-run --no-install-deps --no-config-update ) >"${tmpdir}/b.out" 2>&1
rc=$?
set -e

# We don't require rc=0 here because dry-run still touches things like
# service detection. We *do* require the payload-resolution diagnostic
# AND the "installing: ..." line — both prove script-dir was honored.
if grep -q "required payload bin/routerd not found" "${tmpdir}/b.out"; then
    echo "install-sh-cwd-smoke: FAIL (case B) — install.sh failed to resolve script_dir when launched from a different cwd" >&2
    cat "${tmpdir}/b.out" >&2
    exit 1
fi
if ! grep -q "installing: vsmoke-test" "${tmpdir}/b.out"; then
    echo "install-sh-cwd-smoke: FAIL (case B) — install.sh did not read the stub bin/routerd in script_dir" >&2
    cat "${tmpdir}/b.out" >&2
    exit 1
fi

echo "install-sh-cwd-smoke: OK"
