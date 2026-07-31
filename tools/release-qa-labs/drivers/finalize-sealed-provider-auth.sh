#!/usr/bin/env bash
set -euo pipefail
umask 077

[ "$(id -u)" -eq 0 ] || { echo "sealed auth finalize: root is required" >&2; exit 2; }
[ "$#" -eq 1 ] || { echo "Usage: $(basename "$0") RUN_ID" >&2; exit 2; }
run_id="$1"
[[ "$run_id" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$ ]] || {
  echo "sealed auth finalize: invalid run id" >&2; exit 2;
}
run_root="/var/lib/routerd-release-qa/$run_id"
sealed_parent="/var/lib/routerd-release-qa-sealed"
sealed_child="$sealed_parent/$run_id"
state="$run_root/runtime/evidence/lifecycle/supervisor-state.json"
inventory="$run_root/runtime/evidence/final-inventory/inventory.json"
guard="$run_root/repo/tools/release-qa-labs/qa_guard.py"

for unit in "routerd-release-qa@$run_id.service" "routerd-release-qa-prepare@$run_id.service"; do
  [ "$(systemctl is-active "$unit" 2>/dev/null || true)" = inactive ] || {
    echo "sealed auth finalize: $unit is not inactive" >&2; exit 2;
  }
done
phase="$(jq -er '.phase' "$state")"
safe_precheck="$(jq -er '(.phase == "PRECHECK") and (.mutationCommandExecuted == false) and (.mutationPgid == null)' "$state" || true)"
if [[ ! "$phase" =~ ^(STAGING_DONE|DONE|FAILED)$ ]] && [ "$safe_precheck" != true ]; then
  echo "sealed auth finalize: lifecycle is neither terminal nor an unmutated PRECHECK" >&2; exit 2
fi
python3 "$guard" inventory --inventory-json "$inventory" >/dev/null || {
  echo "sealed auth finalize: authoritative inventory is not zero" >&2; exit 2;
}
[ "$(readlink -m "$sealed_child")" = "$sealed_child" ] && [ -d "$sealed_child" ] || exit 2
[ "$(stat -c '%u:%g:%a' "$sealed_parent")" = "0:0:755" ] || {
  echo "sealed auth finalize: sealed parent is unsafe" >&2; exit 2;
}
[ "$(stat -c '%u:%a' "$sealed_child")" = "0:750" ] || {
  echo "sealed auth finalize: sealed child is unsafe" >&2; exit 2;
}
rm -rf -- "$sealed_child"
[ ! -e "$sealed_child" ] && [ -d "$sealed_parent" ]
