#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
set -eu

usage() {
	cat <<'EOF'
usage: scripts/release.sh [options]

Create a date-and-time-based routerd release.

Options:
  --date YYYYMMDD       Override the release date. Defaults to Asia/Tokyo today.
  --timezone TZ         Override the timezone used for date/time calculation.
  --allow-dirty         Include existing working tree changes in the release commit.
  --skip-checks         Skip local test/schema/example/website checks.
  --no-push             Create the commit and tag locally but do not push.
  --dry-run             Print the computed release tag and exit.
  -h, --help            Show this help.

Environment:
  ROUTERD_RELEASE_DATE  Same as --date.
  ROUTERD_RELEASE_TZ    Same as --timezone. Default: Asia/Tokyo.
EOF
}

timezone=${ROUTERD_RELEASE_TZ:-Asia/Tokyo}
release_date=${ROUTERD_RELEASE_DATE:-}
allow_dirty=0
skip_checks=0
no_push=0
dry_run=0

while [ "$#" -gt 0 ]; do
	case "$1" in
		--date)
			shift
			[ "$#" -gt 0 ] || { echo "--date requires YYYYMMDD" >&2; exit 2; }
			release_date=$1
			;;
		--timezone)
			shift
			[ "$#" -gt 0 ] || { echo "--timezone requires TZ" >&2; exit 2; }
			timezone=$1
			;;
		--allow-dirty)
			allow_dirty=1
			;;
		--skip-checks)
			skip_checks=1
			;;
		--no-push)
			no_push=1
			;;
		--dry-run)
			dry_run=1
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "unknown option: $1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

if [ -z "$release_date" ]; then
	release_date=$(TZ="$timezone" date +%Y%m%d)
fi
release_time=$(TZ="$timezone" date +%H%M)

case "$release_date" in
	[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]) ;;
	*)
		echo "release date must be YYYYMMDD: $release_date" >&2
		exit 2
		;;
esac

release_tag="v${release_date}.${release_time}"

if [ "$dry_run" -eq 1 ]; then
	printf '%s\n' "$release_tag"
	exit 0
fi

if git rev-parse -q --verify "refs/tags/${release_tag}" >/dev/null; then
	echo "tag already exists: $release_tag" >&2
	exit 1
fi

if [ "$allow_dirty" -ne 1 ] && [ -n "$(git status --short)" ]; then
	echo "working tree is not clean; commit or stash changes, or pass --allow-dirty" >&2
	git status --short >&2
	exit 1
fi

current_version=$(sed -n 's/^VERSION ?= //p' Makefile | head -n 1)
if [ -z "$current_version" ]; then
	echo "could not read current version from Makefile" >&2
	exit 1
fi

replace_version() {
	file=$1
	[ -f "$file" ] || return 0
	perl -0pi -e "s/\\Q${current_version}\\E/${release_tag}/g" "$file"
}

replace_version Makefile
replace_version pkg/version/version.go
replace_version docs/install-and-upgrade.md
replace_version website/i18n/ja/docusaurus-plugin-content-docs/current/install-and-upgrade.md

insert_changelog_stub() {
	file=$1
	[ -f "$file" ] || return 0
	if grep -q "^## ${release_tag}$" "$file"; then
		return 0
	fi
	tmp=$(mktemp)
	awk -v tag="$release_tag" '
		BEGIN { inserted = 0 }
		/^## / && inserted == 0 {
			print "## " tag
			print ""
			inserted = 1
		}
		{ print }
		END {
			if (inserted == 0) {
				print ""
				print "## " tag
				print ""
			}
		}
	' "$file" > "$tmp"
	mv "$tmp" "$file"
}

insert_changelog_stub docs/releases/changelog.md
insert_changelog_stub website/i18n/ja/docusaurus-plugin-content-docs/current/releases/changelog.md
insert_changelog_stub website/i18n/zh-Hant/docusaurus-plugin-content-docs/current/releases/changelog.md
insert_changelog_stub website/i18n/zh-Hans/docusaurus-plugin-content-docs/current/releases/changelog.md

if [ "$skip_checks" -ne 1 ]; then
	go test ./...
	make check-schema
	make validate-example
	make website-build
fi

git add -A

if git diff --cached --quiet; then
	echo "no release changes to commit" >&2
	exit 1
fi

git commit -m "Release ${release_tag}"
git tag "$release_tag"

if [ "$no_push" -ne 1 ]; then
	git push
	git push origin "$release_tag"
fi

printf 'created release %s\n' "$release_tag"
