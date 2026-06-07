#!/usr/bin/env bash
set -euo pipefail

version=${1:-vtest-version}
commit=${2:-testcommit}
tmpdir=$(mktemp -d)
trap 'rm -rf "${tmpdir}"' EXIT

make -s build-daemons \
  BUILDDIR="${tmpdir}/bin" \
  ROUTERD_OS="$(go env GOOS)" \
  GOARCH="$(go env GOARCH)" \
  VERSION="${version}" \
  GIT_COMMIT="${commit}"

got=$("${tmpdir}/bin/routerd" --version)
want="routerd ${version} (${commit})"
if [ "${got}" != "${want}" ]; then
  printf 'routerd --version = %s, want %s\n' "${got}" "${want}" >&2
  exit 1
fi
