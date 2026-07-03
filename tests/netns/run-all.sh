#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ "${EUID}" -ne 0 ]]; then
  printf 'run explicitly with sudo: sudo %s\n' "$0" >&2
  exit 1
fi

scripts=(
  keepalived-vip-failover.sh
  keepalived-no-spurious-restart.sh
  ingress-conntrack-survive.sh
  forcefrag-df-forward.sh
  sam-scoped-conntrack-cleanup.sh
)

for script in "${scripts[@]}"; do
  printf '==> %s\n' "$script" >&2
  "$SCRIPT_DIR/$script"
done

printf 'all netns tests passed\n'
