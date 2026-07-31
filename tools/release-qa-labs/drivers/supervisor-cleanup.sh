#!/usr/bin/env bash
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=common.sh
source "$script_dir/common.sh"
load_contract "$default_contract_path"
exec "$script_dir/cleanup-driver.sh" --run-id "$run_id" \
  --evidence-dir "$evidence_root/final-cleanup"
