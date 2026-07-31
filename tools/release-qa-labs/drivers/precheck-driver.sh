#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=common.sh
source "$script_dir/common.sh"

[ "$#" -eq 0 ] || die "Usage: $(basename "$0")"
load_contract "$default_contract_path"
release_repo="$(absolute_path "$(jq -er '.releaseRepo' "$run_env_path")")"
python3 "$framework_root/qa_guard.py" contract \
  --contract "$contract_path" --release-repo "$release_repo" \
  --framework "$framework_root"
"$script_dir/remote-egress-preflight.sh" --contract "$contract_path"
"$script_dir/inventory-driver.sh" --run-id "$run_id" \
  --evidence-dir "$evidence_root/pre-apply-inventory"
echo "release QA precheck: pass"
