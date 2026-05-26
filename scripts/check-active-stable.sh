#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Verify that homepage / intro pages / announcement bar all advertise
# the same "current recommended stable" version. The source of truth
# is the STABLE_VERSION constant in website/src/pages/index.tsx.
#
# Files known to legitimately reference *historic* stable versions
# (release changelog and stable-milestone narrative) are intentionally
# excluded — they keep "supersedes v..." and "carry-forward from v..."
# style references on purpose.
#
# This guard catches the failure mode where the announcement bar /
# stable.md were promoted but the homepage hero, the introduction
# tip, or the docusaurus.config.ts forgot to track along.

set -eu

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "${repo_root}"

source_file="website/src/pages/index.tsx"
if [ ! -f "${source_file}" ]; then
    echo "check-active-stable: ${source_file} missing" >&2
    exit 2
fi

active=$(grep -oE "^const STABLE_VERSION = 'v[0-9]{8}\.[0-9]{4}'" "${source_file}" | grep -oE "v[0-9]{8}\.[0-9]{4}" | head -n 1)
if [ -z "${active}" ]; then
    echo "check-active-stable: could not extract STABLE_VERSION from ${source_file}" >&2
    exit 2
fi

echo "check-active-stable: active stable = ${active}"

# Files where every mentioned vYYYYMMDD.HHmm must equal the active
# stable. Anything else means the homepage/announcement and the
# introduction tip have drifted from each other.
must_match_files="
docs/intro.md
website/i18n/ja/docusaurus-plugin-content-docs/current/intro.md
website/i18n/zh-Hans/docusaurus-plugin-content-docs/current/intro.md
website/i18n/zh-Hant/docusaurus-plugin-content-docs/current/intro.md
website/docusaurus.config.ts
website/src/pages/index.tsx
"

fail=0
for f in ${must_match_files}; do
    [ -f "${f}" ] || continue
    bad=$(grep -nE "v[0-9]{8}\.[0-9]{4}" "${f}" | grep -v "${active}" || true)
    if [ -n "${bad}" ]; then
        echo "check-active-stable: ${f} mentions a stable version other than ${active}:" >&2
        printf '%s\n' "${bad}" >&2
        fail=1
    fi
done

if [ "${fail}" -ne 0 ]; then
    echo "check-active-stable: FAIL — homepage / intro / announcement bar are out of sync with ${source_file}." >&2
    echo "check-active-stable: promote the new stable consistently or update ${source_file}." >&2
    exit 1
fi

echo "check-active-stable: OK"
