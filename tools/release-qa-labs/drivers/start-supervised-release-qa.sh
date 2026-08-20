#!/usr/bin/env bash
set -euo pipefail
umask 077

[ "$#" -eq 1 ] || { echo "Usage: $(basename "$0") CONTRACT" >&2; exit 2; }
contract_path="$(readlink -m "$1")"
runtime_root="$(dirname "$contract_path")"
run_root="$(dirname "$runtime_root")"
run_id="$(basename "$run_root")"
expected_root="/var/lib/routerd-release-qa/$run_id"
[ "$run_root" = "$expected_root" ] || {
  echo "release QA launcher: contract must be $expected_root/runtime/contract.json" >&2; exit 2;
}
[ "$contract_path" = "$run_root/runtime/contract.json" ] || {
  echo "release QA launcher: noncanonical contract path" >&2; exit 2;
}

framework_root="$run_root/repo/tools/release-qa-labs"
state="$runtime_root/evidence/lifecycle/supervisor-state.json"
heartbeat="$runtime_root/evidence/lifecycle/heartbeat"

launcher_run_env="$runtime_root/run.env.json"
if [ -f "$state" ] && [ -f "$runtime_root/pinned/run.env.json" ]; then
  launcher_run_env="$runtime_root/pinned/run.env.json"
fi
pve_token_tfvars="$(jq -er '.pveTokenTfvars' "$launcher_run_env")"
pve_token_expected="$runtime_root/secrets/pve-token.tfvars"
[ "$pve_token_tfvars" = "$pve_token_expected" ] || {
  echo "release QA launcher: PVE token source is not canonical" >&2; exit 2;
}
pve_ca_pem="$(jq -er '.pveCaPem' "$launcher_run_env")"
pve_ca_expected="$runtime_root/secrets/pve-ca.pem"
[ "$pve_ca_pem" = "$pve_ca_expected" ] || {
  echo "release QA launcher: PVE CA source is not canonical" >&2; exit 2;
}
guest_ssh_private_key="$(jq -er '.guestSshPrivateKey' "$launcher_run_env")"
guest_ssh_private_key_expected="$runtime_root/secrets/guest_ssh"
[ "$guest_ssh_private_key" = "$guest_ssh_private_key_expected" ] || {
  echo "release QA launcher: guest SSH private key source is not canonical" >&2; exit 2;
}
pve_ssh_known_hosts="$(jq -er '.pveSshKnownHosts' "$launcher_run_env")"
pve_ssh_known_hosts_expected="$runtime_root/secrets/pve-known_hosts"
[ "$pve_ssh_known_hosts" = "$pve_ssh_known_hosts_expected" ] || {
  echo "release QA launcher: PVE SSH known_hosts source is not canonical" >&2; exit 2;
}
azure_source="$(jq -er '.azureAuthSource' "$launcher_run_env")"
azure_expected="$runtime_root/secrets/azure-auth-source"
azure_state="$runtime_root/provider-state/azure"
azure_digest_pin="/var/lib/routerd-release-qa-sealed/$run_id/azure-auth-source.sha256"
azure_snapshot="/var/lib/routerd-release-qa-sealed/$run_id/azure-auth-snapshot"
tamper_args=()

auth_tree_digest() {
  local root="$1" dir_mode="$2" file_mode="$3" uid="$4" gid="$5" unsafe
  [ "$(readlink -m "$root")" = "$root" ] && [ -d "$root" ] || return 1
  [ -z "$(find "$root" -type l -print -quit)" ] || return 1
  [ -z "$(find "$root" ! -type d ! -type f -print -quit)" ] || return 1
  unsafe="$(find "$root" -printf '%m %y\n' | awk '
    ($2 == "d" && $1 != d) || ($2 == "f" && $1 != f) { print; exit }
  ' d="$dir_mode" f="$file_mode")"
  [ -z "$unsafe" ] || return 1
  [ -z "$(find "$root" \( ! -uid "$uid" -o ! -gid "$gid" \) -print -quit)" ] || return 1
  [ -n "$(find "$root" -type f -print -quit)" ] || return 1
  { cd "$root" && find . -type f -print0 | sort -z | xargs -0 sha256sum; } |
    sha256sum | awk '{print $1}'
}
source_digest() { auth_tree_digest "$1" 700 600 "$(id -u)" "$(id -g)"; }
snapshot_digest() { auth_tree_digest "$1" 750 640 0 "$(id -g)"; }

[ "$azure_source" = "$azure_expected" ] || {
  echo "release QA launcher: Azure authentication source is not canonical" >&2; exit 2;
}
if ! { [ -d "$azure_snapshot" ] && [ -f "$azure_digest_pin" ]; }; then
  echo "release QA launcher: pinned Azure recovery state is missing" >&2; exit 2
fi
[ "$(stat -c '%u:%g:%a' "$azure_digest_pin")" = "0:$(id -g):640" ] || {
  echo "release QA launcher: pinned Azure digest ownership or mode is unsafe" >&2; exit 2;
}
[ "$(snapshot_digest "$azure_snapshot")" = "$(cat "$azure_digest_pin")" ] || {
  echo "release QA launcher: pinned Azure authentication snapshot was modified" >&2; exit 2;
}
current_digest="$(source_digest "$azure_source" 2>/dev/null || true)"
if [ -z "$current_digest" ] || [ "$current_digest" != "$(cat "$azure_digest_pin")" ]; then
  tamper_args=(--source-input-tamper-detected)
  install -d -m 0700 "$runtime_root/evidence/lifecycle"
  printf 'Azure authentication source changed after snapshot; recovery uses pinned provider state\n' \
    >"$runtime_root/evidence/lifecycle/azure-auth-source-tamper.txt"
  chmod 0600 "$runtime_root/evidence/lifecycle/azure-auth-source-tamper.txt"
fi

# The writable CLI state is disposable. Rebuild it atomically from the pinned
# snapshot on every service start so deletion or tamper cannot block cleanup.
working_tmp="$(mktemp -d "$runtime_root/provider-state.azure.XXXXXX")"
cp -r --no-dereference --no-preserve=ownership,mode "$azure_snapshot/." "$working_tmp/"
find "$working_tmp" -type d -exec chmod 0700 {} +
find "$working_tmp" -type f -exec chmod 0600 {} +
[ "$(source_digest "$working_tmp")" = "$(cat "$azure_digest_pin")" ] || {
  echo "release QA launcher: Azure working-state reconstruction failed" >&2; exit 2;
}
install -d -m 0700 "$(dirname "$azure_state")"
if [ -e "$azure_state" ]; then
  old_state="$runtime_root/provider-state.azure.old.$$"
  mv "$azure_state" "$old_state"
  mv "$working_tmp" "$azure_state"
  rm -rf -- "$old_state"
else
  mv "$working_tmp" "$azure_state"
fi
export AZURE_CONFIG_DIR="$azure_state"

# On restart the immutable authentication source must still match its pinned
# digest. Supervisor separately authenticates durable state and runtime inputs.
exec python3 "$framework_root/lifecycle_supervisor.py" \
  --run-id "$run_id" --run-root "$run_root" --state "$state" --heartbeat "$heartbeat" \
  --contract "$contract_path" --run-env "$runtime_root/run.env.json" \
  --tfvars "$runtime_root/terraform.tfvars" \
  --pve-ssh-private-key "$runtime_root/secrets/pve_ssh" \
  --guest-ssh-private-key "$guest_ssh_private_key" \
  --pve-ssh-known-hosts "$pve_ssh_known_hosts" \
  --pve-token-tfvars "$pve_token_tfvars" \
  --pve-ca-pem "$pve_ca_pem" \
  "${tamper_args[@]}" \
  --precheck-command "$framework_root/drivers/precheck-driver.sh" \
  --mutation-command "$framework_root/drivers/mutation-driver.sh" \
  --cleanup-command "$framework_root/drivers/supervisor-cleanup.sh" \
  --inventory-command "$framework_root/drivers/supervisor-inventory.sh" \
  --post-zero-command "$framework_root/drivers/revoke-pve-run-token.sh" --supervised "$run_id"
