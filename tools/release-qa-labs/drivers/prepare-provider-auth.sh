#!/usr/bin/env bash
set -euo pipefail
umask 077

[ "$#" -eq 1 ] || { echo "Usage: $(basename "$0") RUN_ID" >&2; exit 2; }
run_id="$1"
[[ "$run_id" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$ ]] || {
  echo "provider auth prepare: invalid run id" >&2; exit 2;
}
service_user=routerd-release-qa
service_group=routerd-release-qa
service_uid="$(id -u "$service_user")"
service_gid="$(getent group "$service_group" | cut -d: -f3)"
[ -n "$service_gid" ] || { echo "provider auth prepare: service group is missing" >&2; exit 2; }
[ "$(id -u)" -eq 0 ] || { echo "provider auth prepare: root is required" >&2; exit 2; }
[ "$(id -g)" -eq "$service_gid" ] || {
  echo "provider auth prepare: effective group must be $service_group" >&2; exit 2;
}
runtime_root="/var/lib/routerd-release-qa/$run_id/runtime"
source_root="$runtime_root/secrets/azure-auth-source"
pinned_root="/var/lib/routerd-release-qa-sealed/$run_id"
snapshot="$pinned_root/azure-auth-snapshot"
digest_pin="$pinned_root/azure-auth-source.sha256"

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

source_digest() { auth_tree_digest "$1" 700 600 "$service_uid" "$service_gid"; }
snapshot_digest() { auth_tree_digest "$1" 750 640 0 "$service_gid"; }

chown root:"$service_group" "$pinned_root"
chmod 0750 "$pinned_root"
if [ -d "$snapshot" ] && [ -f "$digest_pin" ]; then
  [ "$(snapshot_digest "$snapshot")" = "$(cat "$digest_pin")" ] || {
    echo "provider auth prepare: pinned Azure snapshot was modified" >&2; exit 2;
  }
  exit 0
fi
if ! { [ ! -e "$snapshot" ] && [ ! -e "$digest_pin" ]; }; then
  echo "provider auth prepare: incomplete pinned Azure snapshot" >&2; exit 2;
fi
source_before="$(source_digest "$source_root")" || {
  echo "provider auth prepare: Azure authentication source is unsafe" >&2; exit 2;
}
snapshot_tmp="$(mktemp -d "$pinned_root/.azure-auth-snapshot.XXXXXX")"
trap 'rm -rf -- "$snapshot_tmp"' EXIT
cp -r --no-dereference --no-preserve=ownership,mode "$source_root/." "$snapshot_tmp/"
find "$snapshot_tmp" -type d -exec chown root:"$service_group" {} + -exec chmod 0750 {} +
find "$snapshot_tmp" -type f -exec chown root:"$service_group" {} + -exec chmod 0640 {} +
copied_digest="$(snapshot_digest "$snapshot_tmp")" || {
  echo "provider auth prepare: Azure authentication snapshot is unsafe" >&2; exit 2;
}
source_after="$(source_digest "$source_root")" || {
  echo "provider auth prepare: Azure authentication source changed during snapshot" >&2; exit 2;
}
if ! { [ "$source_before" = "$source_after" ] && [ "$source_before" = "$copied_digest" ]; }; then
  echo "provider auth prepare: Azure authentication source changed during snapshot" >&2; exit 2
fi
digest_tmp="$(mktemp "$pinned_root/.azure-auth-source.XXXXXX")"
printf '%s\n' "$copied_digest" >"$digest_tmp"
chown root:"$service_group" "$digest_tmp"
chmod 0640 "$digest_tmp"
mv "$snapshot_tmp" "$snapshot"
mv "$digest_tmp" "$digest_pin"
trap - EXIT
