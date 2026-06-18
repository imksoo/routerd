#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"

assert_contains()
{
    haystack=$1
    needle=$2
    label=$3
    case "${haystack}" in
        *"${needle}"*) ;;
        *)
            echo "missing ${label}: ${needle}" >&2
            echo "${haystack}" >&2
            exit 1
            ;;
    esac
}

apt_deps=$(ROUTERD_INSTALL_PACKAGE_MANAGER=apt ./packaging/install.sh --list-deps)
assert_contains "${apt_deps}" "package manager: apt" "apt package manager selection"
assert_contains "${apt_deps}" "- dnsmasq-base" "apt dnsmasq package"
assert_contains "${apt_deps}" "- nftables" "apt nftables package"
assert_contains "${apt_deps}" "- keepalived" "apt VRRP package"

pkg_deps=$(ROUTERD_INSTALL_PACKAGE_MANAGER=pkg ./packaging/install.sh --list-deps)
assert_contains "${pkg_deps}" "package manager: pkg" "FreeBSD package manager selection"
assert_contains "${pkg_deps}" "- dnsmasq" "FreeBSD dnsmasq package"
assert_contains "${pkg_deps}" "- mpd5" "FreeBSD PPPoE package"

echo "install dependency smoke checks passed"
