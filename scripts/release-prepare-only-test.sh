#!/usr/bin/env sh
# SPDX-License-Identifier: BSD-3-Clause

set -eu

repo_root=$(git rev-parse --show-toplevel)
work=$(mktemp -d "${TMPDIR:-/tmp}/routerd-release-prepare-test.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

git clone --quiet --shared "$repo_root" "$work/repo"
cp "$repo_root/Makefile" "$work/repo/Makefile"
cp "$repo_root/scripts/release.sh" "$work/repo/scripts/release.sh"
cd "$work/repo"
git config user.name "routerd release test"
git config user.email "routerd-release-test@example.invalid"
for changelog in \
	docs/releases/changelog.md \
	website/i18n/ja/docusaurus-plugin-content-docs/current/releases/changelog.md \
	website/i18n/zh-Hant/docusaurus-plugin-content-docs/current/releases/changelog.md \
	website/i18n/zh-Hans/docusaurus-plugin-content-docs/current/releases/changelog.md
do
	printf '## Unreleased\n\nrelease prepare-only fixture\n' >"$changelog"
	git add "$changelog"
done
git add Makefile scripts/release.sh
git commit --quiet --allow-empty -m "test release prepare-only fixture"

initial_commit=$(git rev-parse HEAD)
if scripts/release.sh --prepare-only --no-push >/dev/null 2>&1; then
	echo "expected --prepare-only with --no-push to fail" >&2
	exit 1
fi
if [ "$(git rev-parse HEAD)" != "$initial_commit" ] || [ -n "$(git status --short)" ]; then
	echo "invalid option combination changed the repository" >&2
	exit 1
fi

printf '## Unreleased\n' >website/i18n/ja/docusaurus-plugin-content-docs/current/releases/changelog.md
git add website/i18n/ja/docusaurus-plugin-content-docs/current/releases/changelog.md
git commit --quiet -m "test empty translated changelog fixture"
preflight_commit=$(git rev-parse HEAD)
if scripts/release.sh --date 20991231 --timezone UTC --skip-checks --prepare-only >/dev/null 2>&1; then
	echo "expected empty translated changelog to fail" >&2
	exit 1
fi
if [ "$(git rev-parse HEAD)" != "$preflight_commit" ] || [ -n "$(git status --short)" ]; then
	echo "changelog preflight failure changed the repository" >&2
	git status --short >&2
	exit 1
fi
printf '## Unreleased\n\nrelease prepare-only fixture\n' >website/i18n/ja/docusaurus-plugin-content-docs/current/releases/changelog.md
git add website/i18n/ja/docusaurus-plugin-content-docs/current/releases/changelog.md
git commit --quiet -m "test restore translated changelog fixture"
initial_commit=$(git rev-parse HEAD)

output=$(scripts/release.sh \
	--date 20991231 \
	--timezone UTC \
	--skip-checks \
	--prepare-only)
last_line=$(printf '%s\n' "$output" | tail -n 1)
tag=${last_line#prepared release }
case "$tag" in
	v20991231.[0-9][0-9][0-9][0-9]) ;;
	*)
		printf 'unexpected prepare output:\n%s\n' "$output" >&2
		exit 1
		;;
esac

if [ "$(git rev-parse HEAD^)" != "$initial_commit" ]; then
	echo "prepared release commit has an unexpected parent" >&2
	exit 1
fi
if [ "$(git log -1 --pretty=%s)" != "Release $tag" ]; then
	echo "prepared release commit has an unexpected subject" >&2
	exit 1
fi
if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
	echo "prepare-only created tag $tag" >&2
	exit 1
fi
if ! grep -Fqx "VERSION ?= $tag" Makefile; then
	echo "prepared release did not update Makefile version" >&2
	exit 1
fi
for changelog in \
	docs/releases/changelog.md \
	website/i18n/ja/docusaurus-plugin-content-docs/current/releases/changelog.md \
	website/i18n/zh-Hant/docusaurus-plugin-content-docs/current/releases/changelog.md \
	website/i18n/zh-Hans/docusaurus-plugin-content-docs/current/releases/changelog.md
do
	if ! grep -Fqx "## $tag" "$changelog"; then
		echo "prepared release did not promote $changelog" >&2
		exit 1
	fi
done
if [ -n "$(git status --short)" ]; then
	echo "prepare-only left a dirty working tree" >&2
	git status --short >&2
	exit 1
fi
