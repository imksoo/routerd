#!/usr/bin/env sh
# SPDX-License-Identifier: BSD-3-Clause

set -eu

if ! command -v git >/dev/null 2>&1; then
	echo "missing git" >&2
	exit 1
fi
root=$(git rev-parse --show-toplevel)
cd "$root"

files=$(
	git ls-files -co --exclude-standard -- \
		'Makefile' \
		'*.sh' \
		'*.bash' \
		'*.zsh' \
		'*.mk' \
		'*.md' \
		'*.py' \
		'*.yaml' \
		'*.yml' \
		'contrib/**' \
		'examples/**' \
		'packaging/**' \
		'scripts/**' \
		'tests/**' \
		'website/i18n/**' \
		':!:scripts/check-tmp-dir-mutations.sh'
)

if [ -z "$files" ]; then
	exit 0
fi

matches=$(
	printf '%s\n' "$files" | while IFS= read -r file; do
		[ -n "$file" ] || continue
		[ -f "$file" ] || continue
		awk '
			function scrub(line) {
				sub(/[[:space:]]*#.*/, "", line)
				return line
			}
			function command_prefix() {
				return "(^|[;&|({[:space:]])(sudo[[:space:]]+)?(env[[:space:]][^;&|()]*[[:space:]]+)?"
			}
			function tmp_target() {
				return "[[:space:]](/tmp/?|tmp)([[:space:];&|)]|$)"
			}
			function risky(line) {
				line = scrub(line)
				if (line ~ command_prefix() "(chmod|chown|chgrp)[[:space:]][^;&|()]*" tmp_target()) {
					return 1
				}
				if (line ~ command_prefix() "(install|mkdir)[[:space:]][^;&|()]*(-d|-m|--mode=)[^;&|()]*" tmp_target()) {
					return 1
				}
				if (line ~ command_prefix() "rm[[:space:]][^;&|()]*" tmp_target() &&
				    line ~ /(^|[[:space:]])-[^;&|()]*[rR]/ &&
				    line ~ /(^|[[:space:]])-[^;&|()]*[fF]/) {
					return 1
				}
				if (line ~ command_prefix() "(mv|ln)[[:space:]][^;&|()]*" tmp_target()) {
					return 1
				}
				return 0
			}
			risky($0) {
				printf "%s:%d:%s\n", FILENAME, FNR, $0
				found = 1
			}
			END { exit found ? 1 : 0 }
		' "$file" || true
	done
)
if [ -n "$matches" ]; then
	echo "dangerous direct /tmp directory mutation found" >&2
	echo "Do not chmod/chown/chgrp or create /tmp itself from routerd scripts/docs." >&2
	echo "Use a child path such as /tmp/routerd-... and preserve /tmp root:root 1777." >&2
	printf '%s\n' "$matches" >&2
	exit 1
fi
