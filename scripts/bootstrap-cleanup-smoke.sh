#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/routerd-bootstrap-cleanup.XXXXXX")
cleanup()
{
    rm -rf "${tmpdir}"
}
trap cleanup EXIT HUP INT TERM

fakebin="${tmpdir}/bin"
payload="${tmpdir}/payload"
mkdir -p "${fakebin}" "${payload}" "${tmpdir}/work"

cat >"${fakebin}/curl" <<'SH'
#!/bin/sh
set -eu
out=
url=
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o)
            out=$2
            shift 2
            ;;
        -fsSL)
            shift
            ;;
        *)
            url=$1
            shift
            ;;
    esac
done
[ -n "${out}" ] || exit 2
case "${url}" in
    *.sha256)
        cp "${ROUTERD_TEST_SHA}" "${out}"
        ;;
    *.tar.gz)
        cp "${ROUTERD_TEST_ARCHIVE}" "${out}"
        ;;
    *)
        exit 3
        ;;
esac
SH
chmod +x "${fakebin}/curl"

cat >"${payload}/install.sh" <<'SH'
#!/bin/sh
set -eu
printf 'ran\n' >"${ROUTERD_TEST_MARKER}"
exit "${ROUTERD_TEST_INSTALL_STATUS:-0}"
SH
chmod +x "${payload}/install.sh"

archive="${tmpdir}/routerd-vtest-linux-amd64.tar.gz"
tar -C "${payload}" -czf "${archive}" install.sh
sha_file="${archive}.sha256"
sha256sum "${archive}" >"${sha_file}"

run_case()
{
    status=$1
    want=$2
    marker="${tmpdir}/marker-${status}"
    rm -f "${marker}"
    set +e
    PATH="${fakebin}:${PATH}" \
        ROUTERD_REPO=fake/routerd \
        ROUTERD_VERSION=vtest \
        ROUTERD_TMPDIR="${tmpdir}/work" \
        ROUTERD_TEST_ARCHIVE="${archive}" \
        ROUTERD_TEST_SHA="${sha_file}" \
        ROUTERD_TEST_MARKER="${marker}" \
        ROUTERD_TEST_INSTALL_STATUS="${status}" \
        sh "${repo_root}/packaging/bootstrap.sh" >/dev/null 2>&1
    got=$?
    set -e
    if [ "${got}" -ne "${want}" ]; then
        echo "bootstrap-cleanup-smoke: exit ${got}, want ${want}" >&2
        exit 1
    fi
    if [ ! -f "${marker}" ]; then
        echo "bootstrap-cleanup-smoke: payload installer did not run" >&2
        exit 1
    fi
    if find "${tmpdir}/work" -maxdepth 1 -name 'routerd-bootstrap.*' | grep -q .; then
        echo "bootstrap-cleanup-smoke: routerd-bootstrap temp dir was not removed" >&2
        find "${tmpdir}/work" -maxdepth 1 -name 'routerd-bootstrap.*' >&2
        exit 1
    fi
}

run_case 0 0
run_case 7 7
echo "bootstrap-cleanup-smoke: OK"
