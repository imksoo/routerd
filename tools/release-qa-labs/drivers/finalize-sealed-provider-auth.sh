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

for unit in "routerd-release-qa@$run_id.service" "routerd-release-qa-prepare@$run_id.service" \
  "routerd-release-qa-egress-proxy@$run_id.service"; do
  [ "$(systemctl is-active "$unit" 2>/dev/null || true)" = inactive ] || {
    echo "sealed auth finalize: $unit is not inactive" >&2; exit 2;
  }
done
run_env="$run_root/runtime/pinned/run.env.json"
[ -f "$run_env" ] || run_env="$run_root/runtime/run.env.json"
proxy="$(jq -er '.httpsProxy' "$run_env")"
[[ "$proxy" =~ ^http://127\.0\.0\.1:([0-9]{4,5})$ ]] || {
  echo "sealed auth finalize: invalid tracked proxy endpoint" >&2; exit 2;
}
proxy_port="${BASH_REMATCH[1]}"
if ss -H -ltn "sport = :$proxy_port" | grep -q .; then
  echo "sealed auth finalize: tracked proxy socket is still listening" >&2; exit 2
fi
phase="$(jq -er '.phase' "$state")"
if [[ ! "$phase" =~ ^(STAGING_DONE|DONE|FAILED)$ ]]; then
  echo "sealed auth finalize: lifecycle is not terminal after post-zero token revocation" >&2; exit 2
fi
python3 "$guard" inventory --inventory-json "$inventory" >/dev/null || {
  echo "sealed auth finalize: authoritative inventory is not zero" >&2; exit 2;
}
contract="$run_root/runtime/pinned/contract.json"
receipt="$run_root/runtime/evidence/final-token-revocation/revocation.json"
if ! { [ -f "$contract" ] && [ ! -L "$contract" ] && [ "$(stat -c '%a' "$contract")" = 600 ]; }; then
  echo "sealed auth finalize: pinned contract is missing or unsafe" >&2; exit 2;
fi
if ! { [ -f "$receipt" ] && [ ! -L "$receipt" ] && [ "$(stat -c '%a' "$receipt")" = 600 ]; }; then
  echo "sealed auth finalize: post-zero token revocation receipt is missing or unsafe" >&2; exit 2;
fi
token_owner="$(jq -er '.pve.tokenOwner' "$contract")"
if ! { [[ "$token_owner" =~ ^[A-Za-z0-9._-]+@[A-Za-z0-9._-]+$ ]] && [ "${token_owner%%@*}" != root ]; }; then
  echo "sealed auth finalize: pinned contract token owner is unsafe" >&2; exit 2;
fi
token_identity_sha256="$(printf '%s!%s' "$token_owner" "$run_id" | sha256sum | awk '{print $1}')"
jq -e --arg run_id "$run_id" --arg identity_sha "$token_identity_sha256" '
  .runId == $run_id and .status == "revoked" and
  .tokenIdentitySha256 == $identity_sha and (.revokedAt | type == "string")
' "$receipt" >/dev/null || {
  echo "sealed auth finalize: post-zero token revocation receipt does not match this run" >&2; exit 2;
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
