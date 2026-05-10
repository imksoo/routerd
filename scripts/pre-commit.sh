#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
set -eu

if [ "${ROUTERD_SKIP_PRE_COMMIT:-}" = "1" ]; then
	echo "routerd pre-commit: skipped by ROUTERD_SKIP_PRE_COMMIT=1"
	exit 0
fi

repo_root=$(git rev-parse --show-toplevel 2>/dev/null)
cd "$repo_root"

echo "routerd pre-commit: go test ./..."
go test ./...

echo "routerd pre-commit: make check-schema"
make check-schema

echo "routerd pre-commit: ok"
