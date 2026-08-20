#!/usr/bin/env bash
# Revoke one run-scoped PVE API token only after authoritative zero inventory.
# The supervised form runs in the dedicated post-zero phase; a retry there
# never repeats cleanup or a paid mutation. The terminal form is retained for
# an independently verified manual recovery action.
set -euo pipefail
umask 077

die() {
  printf 'PVE token revocation: %s\n' "$1" >&2
  exit 2
}

supervised=false
case "$#" in
  1) run_id="$1" ;;
  2)
    [ "$1" = "--supervised" ] || die "usage: $(basename "$0") [--supervised] RUN_ID"
    supervised=true
    run_id="$2"
    ;;
  *) die "usage: $(basename "$0") [--supervised] RUN_ID" ;;
esac
[[ "$run_id" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$ ]] || die "invalid run ID"

runs_root="/var/lib/routerd-release-qa"
run_root="$runs_root/$run_id"
[ "$(readlink -f "$run_root" 2>/dev/null || true)" = "$run_root" ] ||
  die "run root is not canonical"

script_relative="tools/release-qa-labs/drivers/revoke-pve-run-token.sh"
expected_script="$run_root/repo/$script_relative"
[ "$(readlink -f "$0" 2>/dev/null || true)" = "$expected_script" ] ||
  die "hook must run from the canonical reviewed run checkout"

runtime_root="$run_root/runtime"
pinned_root="$runtime_root/pinned"
state="$runtime_root/evidence/lifecycle/supervisor-state.json"
inventory="$runtime_root/evidence/final-inventory/inventory.json"
contract="$pinned_root/contract.json"
pve_ssh_private_key="$pinned_root/pve_ssh"
guest_ssh_private_key="$pinned_root/guest_ssh"
pve_ssh_known_hosts="$pinned_root/pve-known_hosts"
pve_token_tfvars="$pinned_root/pve-token.tfvars"

canonical_regular_file() {
  local path="$1"
  [ "$(readlink -f "$path" 2>/dev/null || true)" = "$path" ] &&
    [ ! -L "$path" ] && [ -f "$path" ]
}

for path in "$state" "$inventory" "$contract" "$pve_ssh_private_key" "$guest_ssh_private_key" "$pve_ssh_known_hosts" "$pve_token_tfvars"; do
  if ! canonical_regular_file "$path"; then
    die "required lifecycle evidence is missing or unsafe"
  fi
done

current_uid="$(id -u)"
for path in "$state" "$inventory" "$contract" "$pve_ssh_private_key" "$guest_ssh_private_key" "$pve_ssh_known_hosts" "$pve_token_tfvars"; do
  [ "$(stat -c '%a' "$path")" = 600 ] || die "pinned lifecycle input has unsafe mode"
  [ "$(stat -c '%u' "$path")" = "$current_uid" ] ||
    die "hook must run as the release-QA service account"
done

expected_input_names='["contract","runEnv","tfvars","pveSshPrivateKey","guestSshPrivateKey","pveSshKnownHosts","pveTokenTfvars","pveCaPem"]'
if [ "$supervised" = true ]; then
  if ! jq -e --arg run_id "$run_id" --arg run_root "$run_root" --argjson names "$expected_input_names" '
    .runId == $run_id and
    .runRoot == $run_root and
    (.executionMode == "production" or .executionMode == "staging-no-mutation") and
    .phase == "REVOKING_TOKEN" and
    .cleanupExit == 0 and .inventoryExit == 0 and
    (.effectiveLifecycle.executionMode == .executionMode) and
    ((.inputs | keys | sort) == ($names | sort)) and
    (.effectiveLifecycle.contractSha256 == .inputs.contract.sha256)
  ' "$state" >/dev/null; then
    die "supervisor state is not the post-zero revocation phase"
  fi
else
  if ! jq -e --arg run_id "$run_id" --arg run_root "$run_root" --argjson names "$expected_input_names" '
    .runId == $run_id and
    .runRoot == $run_root and
    .executionMode == "production" and
    (.phase == "DONE" or .phase == "FAILED") and
    .cleanupExit == 0 and .inventoryExit == 0 and
    (.effectiveLifecycle.executionMode == "production") and
    ((.inputs | keys | sort) == ($names | sort)) and
    (.effectiveLifecycle.contractSha256 == .inputs.contract.sha256)
  ' "$state" >/dev/null; then
    die "supervisor state is not a terminal production run with successful cleanup and inventory"
  fi
fi

verify_pin() {
  local key="$1" expected_path="$2" pinned expected_sha actual_sha
  pinned="$(jq -er --arg key "$key" '.inputs[$key].pinned | strings' "$state")" ||
    die "supervisor pin metadata is incomplete"
  expected_sha="$(jq -er --arg key "$key" '.inputs[$key].sha256 | strings' "$state")" ||
    die "supervisor pin digest is incomplete"
  [ "$pinned" = "$expected_path" ] || die "supervisor pin escaped its canonical runtime path"
  if ! canonical_regular_file "$pinned"; then
    die "supervisor pin is missing or unsafe"
  fi
  [ "$(stat -c '%a' "$pinned")" = 600 ] || die "supervisor pin has unsafe mode"
  [ "$(stat -c '%u' "$pinned")" = "$current_uid" ] ||
    die "supervisor pin is not owned by the release-QA service account"
  actual_sha="$(sha256sum "$pinned" | awk '{print $1}')"
  [ "$actual_sha" = "$expected_sha" ] || die "supervisor pin was modified after lifecycle start"
}

verify_pin contract "$contract"
verify_pin runEnv "$pinned_root/run.env.json"
verify_pin tfvars "$pinned_root/terraform.tfvars"
verify_pin pveSshPrivateKey "$pve_ssh_private_key"
verify_pin guestSshPrivateKey "$guest_ssh_private_key"
verify_pin pveSshKnownHosts "$pve_ssh_known_hosts"
verify_pin pveTokenTfvars "$pve_token_tfvars"
verify_pin pveCaPem "$pinned_root/pve-ca.pem"

expected_script_sha="$(jq -er --arg path "$script_relative" \
  '.qaImplementation.scriptBlobs[$path] | strings' "$contract")" ||
  die "pinned contract does not bind this revocation hook"
[ "$(sha256sum "$expected_script" | awk '{print $1}')" = "$expected_script_sha" ] ||
  die "reviewed revocation hook was modified"

if ! jq -e --arg run_id "$run_id" --argjson supervised "$supervised" '
  .runId == $run_id and
  ((.execution.mode == "production") or ($supervised and .execution.mode == "staging-no-mutation")) and
  (.pve.sshHost | type == "string") and
  (.pve.tokenOwner | type == "string")
' "$contract" >/dev/null; then
  die "pinned contract is not a matching revocation contract"
fi
pve_ssh_host="$(jq -er '.pve.sshHost' "$contract")"
expected_token_owner="$(jq -er '.pve.tokenOwner' "$contract")"
is_dns_fqdn() {
  local host="$1"
  [[ "$host" =~ ^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+$ ]] || return 1
  [[ "$host" =~ ^[0-9]+(\.[0-9]+){3}$ ]] && return 1
  return 0
}
if ! is_dns_fqdn "$pve_ssh_host"; then
  die "pinned contract PVE SSH host is not a DNS FQDN"
fi
if ! { [[ "$expected_token_owner" =~ ^[A-Za-z0-9._-]+@[A-Za-z0-9._-]+$ ]] &&
  [ "${expected_token_owner%%@*}" != root ]; }; then
  die "pinned contract PVE token owner is not a scoped service account"
fi

if ! python3 - "$inventory" <<'PY'
import json
import sys

required = {
    "tofu-state",
    "aws-tagged-resources",
    "azure-resource-group",
    "azure-contained-resources",
    "oci-tagged-resources",
    "pve-vms",
    "pve-bridges",
}
try:
    data = json.load(open(sys.argv[1], encoding="utf-8"))
    scopes = data["scopes"]
    if not isinstance(scopes, list):
        raise ValueError
    by_name = {entry["name"]: entry for entry in scopes if isinstance(entry, dict)}
    if len(by_name) != len(scopes) or set(by_name) != required:
        raise ValueError
    if any(entry.get("count") != 0 or entry.get("queryStatus") != "complete" for entry in by_name.values()):
        raise ValueError
except (OSError, ValueError, KeyError, TypeError, json.JSONDecodeError):
    raise SystemExit(1)
PY
then
  die "authoritative final inventory is not complete and zero"
fi

if [ "$supervised" = false ]; then
  for unit in "routerd-release-qa@$run_id.service" \
    "routerd-release-qa-prepare@$run_id.service" \
    "routerd-release-qa-egress-proxy@$run_id.service"; do
    unit_state="$(systemctl is-active "$unit" 2>/dev/null || true)"
    case "$unit_state" in
      inactive|failed) ;;
      *) die "release-QA unit is still active or its state is unknown" ;;
    esac
  done
fi

token_metadata=
if ! token_metadata="$(python3 - "$pve_token_tfvars" "$run_id" "$expected_token_owner" <<'PY'
import re
import sys

path, run_id, expected_owner = sys.argv[1:]
try:
    text = open(path, encoding="utf-8").read()
except (OSError, UnicodeError):
    raise SystemExit(1)
assignments = re.findall(
    r'^\s*pve_api_token\s*=\s*"([^"\r\n]+)"\s*(?:(?:#|//).*)?$',
    text,
    flags=re.MULTILINE,
)
if len(assignments) != 1:
    raise SystemExit(1)
identity, separator, secret = assignments[0].partition("=")
user, bang, token_name = identity.partition("!")
if (
    not separator or not secret or not bang or user != expected_owner or token_name != run_id
    or re.fullmatch(r"[A-Za-z0-9._-]+@[A-Za-z0-9._-]+", user) is None
    or re.fullmatch(r"[A-Za-z0-9._-]{1,64}", token_name) is None
):
    raise SystemExit(1)
print(user)
print(token_name)
PY
 )"; then
  die "pinned PVE token is not a valid run-scoped token"
fi
mapfile -t token_parts <<<"$token_metadata"
[ "${#token_parts[@]}" -eq 2 ] || die "pinned PVE token is not a valid run-scoped token"
token_user="${token_parts[0]}"
token_name="${token_parts[1]}"
token_identity_sha256="$(printf '%s!%s' "$token_user" "$token_name" | sha256sum | awk '{print $1}')"

evidence_dir="$runtime_root/evidence/final-token-revocation"
if ! { [ "$(readlink -f "$runtime_root/evidence" 2>/dev/null || true)" = "$runtime_root/evidence" ] &&
  [ -d "$runtime_root/evidence" ]; }; then
  die "lifecycle evidence root is unsafe"
fi
if [ -e "$evidence_dir" ]; then
  if ! { [ "$(readlink -f "$evidence_dir" 2>/dev/null || true)" = "$evidence_dir" ] &&
    [ ! -L "$evidence_dir" ] && [ -d "$evidence_dir" ] &&
    [ "$(stat -c '%a' "$evidence_dir")" = 700 ] &&
    [ "$(stat -c '%u' "$evidence_dir")" = "$current_uid" ]; }; then
    die "token revocation evidence directory is unsafe"
  fi
else
  mkdir -m 0700 "$evidence_dir"
fi
receipt="$evidence_dir/revocation.json"
receipt_exists=false
if [ -e "$receipt" ]; then
  if ! { canonical_regular_file "$receipt" && [ "$(stat -c '%a' "$receipt")" = 600 ] &&
    [ "$(stat -c '%u' "$receipt")" = "$current_uid" ]; }; then
    die "existing token revocation receipt is unsafe"
  fi
  if jq -e --arg run_id "$run_id" --arg identity_sha "$token_identity_sha256" '
    .runId == $run_id and .status == "revoked" and .tokenIdentitySha256 == $identity_sha and
    (.revokedAt | type == "string")
  ' "$receipt" >/dev/null; then
    receipt_exists=true
  else
    die "existing token revocation receipt does not match this run"
  fi
fi

ssh_stderr="$evidence_dir/ssh.stderr"
list_output="$(mktemp "$evidence_dir/.pve-token-list.XXXXXX")"
chmod 600 "$list_output"
cleanup_files() {
  rm -f -- "$list_output"
}
trap cleanup_files EXIT

# The token secret is never passed over SSH.  These two remote commands use
# only the non-secret PVE user/token identity and a host key that must already
# be pinned for the release-QA service account.
pve_ssh=(ssh -n -i "$pve_ssh_private_key" -o BatchMode=yes -o StrictHostKeyChecking=yes \
  -o UserKnownHostsFile="$pve_ssh_known_hosts" -o GlobalKnownHostsFile=/dev/null \
  -o CanonicalizeHostname=no -o ConnectTimeout=10 -o PasswordAuthentication=no \
  -o KbdInteractiveAuthentication=no -o IdentitiesOnly=yes "root@$pve_ssh_host")
if ! "${pve_ssh[@]}" "pveum user token list $token_user --output-format json" \
  >"$list_output" 2>"$ssh_stderr"; then
  chmod 600 "$ssh_stderr" 2>/dev/null || true
  die "strict-host-key PVE token listing failed; inspect private SSH evidence"
fi
chmod 600 "$ssh_stderr" 2>/dev/null || true
token_presence="$(python3 - "$list_output" "$token_user" "$token_name" <<'PY'
import json
import sys

path, user, token_name = sys.argv[1:]
try:
    data = json.load(open(path, encoding="utf-8"))
    if isinstance(data, dict):
        data = data.get("data")
    if not isinstance(data, list):
        raise ValueError
    candidates = {token_name, f"{user}!{token_name}"}
    present = any(
        isinstance(item, dict) and str(item.get("tokenid", item.get("id", item.get("token", "")))) in candidates
        for item in data
    )
except (OSError, ValueError, json.JSONDecodeError):
    raise SystemExit(1)
print("present" if present else "absent")
PY
)" || die "PVE token listing is malformed; refusing deletion"
case "$token_presence" in
  present) ;;
  absent)
    if [ "$receipt_exists" = true ]; then
      printf '{"status":"pass","runId":"%s","tokenRevocation":"already-recorded"}\n' "$run_id"
      exit 0
    fi
    die "PVE did not report the exact run-scoped token; refusing deletion"
    ;;
  *) die "PVE token listing is malformed; refusing deletion" ;;
esac
if ! "${pve_ssh[@]}" "pveum user token delete $token_user $token_name" \
  >/dev/null 2>>"$ssh_stderr"; then
  die "strict-host-key PVE token deletion failed; inspect private SSH evidence"
fi

receipt_tmp="$(mktemp "$evidence_dir/.revocation.json.XXXXXX")"
python3 - "$run_id" "$token_identity_sha256" >"$receipt_tmp" <<'PY'
import datetime
import json
import sys

run_id, identity_sha = sys.argv[1:]
print(json.dumps({
    "runId": run_id,
    "status": "revoked",
    "revokedAt": datetime.datetime.now(datetime.timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z"),
    "tokenIdentitySha256": identity_sha,
}, sort_keys=True))
PY
chmod 600 "$receipt_tmp"
mv -f -- "$receipt_tmp" "$receipt"
printf '{"status":"pass","runId":"%s","tokenRevocation":"revoked"}\n' "$run_id"
