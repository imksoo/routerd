#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Smoke test for packaging/install.sh: guard against the failure mode
# where install.sh is launched from a working directory that does not
# contain the release payload (bin/, etc/, ...), runs every `bin/*` glob
# unexpanded, and silently exits 0 with "routerd upgrade completed"
# while installing nothing. Reproduced on homert02 on v20260526.2152
# when invoked as `cd /tmp/routerd-release-vYYYYMMDD.HHmm && sudo
# ./pkg/install.sh ...`.
#
# install.sh is intentionally cwd-relative for its payload (the test
# harness in tests/install relies on that to point the script at a
# scratch package directory) — what must NOT happen is the silent
# no-op when cwd has no payload. So the contract is:
#
#   1. cwd contains no bin/routerd payload  →  exit non-zero with a
#      clear diagnostic, no installation work attempted.
#   2. cwd contains a valid bin/routerd     →  advance past the payload
#      gate and print "installing: <version>".

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
# Case A: cwd has no payload — must fail loudly. Reproduces the
# homert02 "cd /tmp && ./pkg/install.sh" silent no-op.
#
fail_dir="${tmpdir}/no-payload"
mkdir -p "${fail_dir}"

set +e
( cd "${fail_dir}" && bash "${installer}" --dry-run --no-install-deps ) >"${tmpdir}/a.out" 2>&1
rc=$?
set -e

if [ "${rc}" -eq 0 ]; then
    echo "install-sh-cwd-smoke: FAIL (case A) — install.sh exited 0 with no bin/ payload in cwd" >&2
    cat "${tmpdir}/a.out" >&2
    exit 1
fi
if ! grep -q "required payload bin/routerd not found in current directory" "${tmpdir}/a.out"; then
    echo "install-sh-cwd-smoke: FAIL (case A) — install.sh did not emit the missing-payload diagnostic" >&2
    cat "${tmpdir}/a.out" >&2
    exit 1
fi

#
# Case B: cwd contains a stub bin/routerd — must advance past the
# payload gate. We pass --dry-run --no-install-deps --no-config-update
# so the harness does not need a full release tree.
#
ok_dir="${tmpdir}/release"
mkdir -p "${ok_dir}/bin"
cat >"${ok_dir}/bin/routerd" <<'EOF'
#!/bin/sh
echo "vsmoke-test"
EOF
chmod +x "${ok_dir}/bin/routerd"

set +e
( cd "${ok_dir}" && bash "${installer}" --dry-run --no-install-deps --no-config-update ) >"${tmpdir}/b.out" 2>&1
rc=$?
set -e

if grep -q "required payload bin/routerd not found" "${tmpdir}/b.out"; then
    echo "install-sh-cwd-smoke: FAIL (case B) — install.sh reported missing payload even though cwd has bin/routerd" >&2
    cat "${tmpdir}/b.out" >&2
    exit 1
fi
if ! grep -q "installing: vsmoke-test" "${tmpdir}/b.out"; then
    echo "install-sh-cwd-smoke: FAIL (case B) — install.sh did not read the stub bin/routerd in cwd" >&2
    cat "${tmpdir}/b.out" >&2
    exit 1
fi

echo "install-sh-cwd-smoke: OK"
