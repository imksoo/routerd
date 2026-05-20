#!/usr/bin/env bash
set -euo pipefail

example_max_lines="${EXAMPLE_MAX_LINES:-200}"
resource_max_lines="${RESOURCE_MAX_LINES:-50}"
status=0

for file in examples/*.yaml; do
	lines="$(wc -l < "$file")"
	if [ "$lines" -gt "$example_max_lines" ]; then
		printf '%s exceeds %s lines: %s\n' "$file" "$example_max_lines" "$lines" >&2
		status=1
	fi

	awk -v file="$file" -v max="$resource_max_lines" '
		/^[[:space:]]*- apiVersion:/ {
			if (start > 0) {
				size = NR - start
				if (size > max) {
					printf "%s resource starting at line %d exceeds %d lines: %d\n", file, start, max, size > "/dev/stderr"
					status = 1
				}
			}
			start = NR
		}
		END {
			if (start > 0) {
				size = NR - start + 1
				if (size > max) {
					printf "%s resource starting at line %d exceeds %d lines: %d\n", file, start, max, size > "/dev/stderr"
					status = 1
				}
			}
			exit status
		}
	' "$file" || status=1
done

exit "$status"
