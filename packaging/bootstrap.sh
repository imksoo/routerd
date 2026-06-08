#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
# Bootstrap installer: download a routerd release archive and run install.sh.
#
#   curl -fsSL https://github.com/imksoo/routerd/releases/latest/download/install.sh | sudo bash
#   curl -fsSL .../install.sh | sudo ROUTERD_VERSION=v20260608.0248 bash
#
# Environment variables:
#   ROUTERD_VERSION   Release tag (default: latest)
#   ROUTERD_REPO      GitHub repository (default: imksoo/routerd)
#   ROUTERD_TMPDIR    Temporary directory (default: /tmp)
set -eu

ROUTERD_REPO=${ROUTERD_REPO:-imksoo/routerd}
ROUTERD_TMPDIR=${ROUTERD_TMPDIR:-/tmp}

die() { echo "routerd-bootstrap: $*" >&2; exit 1; }
log() { echo "routerd-bootstrap: $*"; }

detect_os() {
    case "$(uname -s)" in
        Linux)   echo linux ;;
        FreeBSD) echo freebsd ;;
        *)       die "unsupported OS: $(uname -s)" ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)    echo amd64 ;;
        aarch64|arm64)   echo arm64 ;;
        *)               die "unsupported architecture: $(uname -m)" ;;
    esac
}

resolve_version() {
    if [ -n "${ROUTERD_VERSION:-}" ]; then
        echo "${ROUTERD_VERSION}"
        return
    fi
    if command -v curl >/dev/null 2>&1; then
        tag=$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${ROUTERD_REPO}/releases/latest" 2>/dev/null | sed 's|.*/||')
    elif command -v wget >/dev/null 2>&1; then
        tag=$(wget -q --max-redirect=0 -O /dev/null "https://github.com/${ROUTERD_REPO}/releases/latest" 2>&1 | sed -n 's|.*Location: .*/\(v[0-9]*\.[0-9]*\).*|\1|p')
    else
        die "curl or wget required"
    fi
    [ -n "${tag}" ] || die "could not resolve latest release"
    echo "${tag}"
}

fetch() {
    url=$1; dest=$2
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "${dest}" "${url}"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O "${dest}" "${url}"
    else
        die "curl or wget required"
    fi
}

verify_sha256() {
    archive=$1; expected=$2
    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "${archive}" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        actual=$(shasum -a 256 "${archive}" | awk '{print $1}')
    else
        log "warning: no sha256sum/shasum found; skipping checksum verification"
        return 0
    fi
    [ "${actual}" = "${expected}" ] || die "SHA256 mismatch: expected ${expected}, got ${actual}"
}

os=$(detect_os)
arch=$(detect_arch)
version=$(resolve_version)
platform="${os}-${arch}"

base_url="https://github.com/${ROUTERD_REPO}/releases/download/${version}"
archive_name="routerd-${version}-${platform}.tar.gz"
archive_url="${base_url}/${archive_name}"
checksum_url="${archive_url}.sha256"

workdir=$(mktemp -d "${ROUTERD_TMPDIR}/routerd-bootstrap.XXXXXX")
trap 'rm -rf "${workdir}"' EXIT HUP INT TERM

log "installing routerd ${version} for ${platform}"

log "downloading ${archive_name}"
fetch "${archive_url}" "${workdir}/${archive_name}"

log "downloading checksum"
fetch "${checksum_url}" "${workdir}/${archive_name}.sha256"
expected=$(awk '{print $1}' "${workdir}/${archive_name}.sha256")
verify_sha256 "${workdir}/${archive_name}" "${expected}"
log "checksum verified"

log "extracting archive"
tar -C "${workdir}" -xzf "${workdir}/${archive_name}"

log "running installer"
cd "${workdir}"
exec sh ./install.sh "$@"
