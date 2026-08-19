#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

[ "$(id -u)" -eq 0 ] || { echo "release QA proxy manager: root is required" >&2; exit 2; }
[ "$#" -eq 2 ] || { echo "Usage: $(basename "$0") start|stop RUN_ID" >&2; exit 2; }
action="$1"
run_id="$2"
[[ "$run_id" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$ ]] || exit 2
run_root="/var/lib/routerd-release-qa/$run_id"
framework="$run_root/repo/tools/release-qa-labs"
run_env="$run_root/runtime/run.env.json"
evidence_root="$run_root/runtime/evidence"
unit="routerd-release-qa-egress-proxy@$run_id.service"
service_user=routerd-release-qa
tracked_unit="$framework/supervisor/routerd-release-qa-egress-proxy@.service"
installed_unit=/etc/systemd/system/routerd-release-qa-egress-proxy@.service

die() { echo "release QA proxy manager: $*" >&2; exit 2; }
port_listening() { ss -H -ltn "sport = :$1" | grep -q .; }
cleanup_failed_start() {
  original_rc="$?"
  trap - ERR
  set +e
  systemctl disable --now "$unit"
  stop_rc="$?"
  active="$(systemctl is-active "$unit" 2>/dev/null || true)"
  listening=0
  port_listening "$proxy_port" && listening=1
  if [ "$stop_rc" -ne 0 ] || [ "$active" != inactive ] || [ "$listening" -ne 0 ]; then
    echo "release QA proxy manager: failed start cleanup left unit or socket state" >&2
    exit 2
  fi
  exit "$original_rc"
}
require_exact_unit() {
  if [ ! -f "$tracked_unit" ] || [ ! -f "$installed_unit" ]; then
    die "tracked proxy unit is not installed"
  fi
  cmp -s "$tracked_unit" "$installed_unit" || die "installed proxy unit differs from tracked unit"
}
require_private_service_directory() {
  local directory="$1" label="$2"
  if [ -e "$directory" ]; then
    if [ -L "$directory" ] || [ ! -d "$directory" ]; then
      die "$label is not a regular directory"
    fi
    if [ "$(stat -c '%U:%G:%a' "$directory")" != "$service_user:$service_user:700" ]; then
      die "$label is not owned privately by $service_user"
    fi
    return
  fi
  install -d -o "$service_user" -g "$service_user" -m 0700 "$directory"
  if [ "$(stat -c '%U:%G:%a' "$directory")" != "$service_user:$service_user:700" ]; then
    die "$label was not created privately for $service_user"
  fi
}
read_port() {
  endpoint="$(jq -r '.httpsProxy // empty' "$run_env")"
  [[ "$endpoint" =~ ^http://127\.0\.0\.1:([0-9]{4,5})$ ]] || return 1
  proxy_port="${BASH_REMATCH[1]}"
  [ "$proxy_port" -ge 1024 ] && [ "$proxy_port" -le 65535 ]
}

case "$action" in
  start)
    require_exact_unit
    [ -f "$run_env" ] || die "run environment is missing"
    [ ! -e "$run_root/runtime/pinned/run.env.json" ] || die "proxy cannot start after inputs are pinned"
    if ! read_port; then
      [ -z "${endpoint:-}" ] || die "httpsProxy is not an IPv4 loopback endpoint"
      first=$((18000 + $(printf '%s' "$run_id" | cksum | awk '{print $1}') % 1000))
      proxy_port=
      for offset in $(seq 0 999); do
        candidate=$((18000 + (first - 18000 + offset) % 1000))
        if ! port_listening "$candidate"; then proxy_port="$candidate"; break; fi
      done
      [ -n "$proxy_port" ] || die "no free proxy port in 18000..18999"
      temporary="$(mktemp "$run_root/runtime/.run.env.proxy.XXXXXX")"
      jq --arg endpoint "http://127.0.0.1:$proxy_port" '.httpsProxy = $endpoint' "$run_env" >"$temporary"
      chown "$service_user:$service_user" "$temporary"
      chmod 0600 "$temporary"
      mv -f -- "$temporary" "$run_env"
    fi
    ! port_listening "$proxy_port" || die "configured proxy port is already listening"
    # `install -d evidence/egress-proxy` leaves an intermediate `evidence`
    # directory owned by the caller on some hosts. Create and verify each
    # boundary explicitly, because the later supervisor writes lifecycle
    # evidence as the unprivileged service user.
    require_private_service_directory "$evidence_root" "release QA evidence root"
    require_private_service_directory "$evidence_root/egress-proxy" "release QA proxy evidence directory"
    trap cleanup_failed_start ERR
    systemctl enable --now "$unit"
    if [ "$(systemctl is-active "$unit")" != active ]; then
      echo "release QA proxy manager: proxy did not become ready" >&2
      false
    fi
    trap - ERR
    ;;
  stop)
    require_exact_unit
    read_port || die "httpsProxy is not an IPv4 loopback endpoint"
    systemctl disable --now "$unit"
    [ "$(systemctl is-active "$unit" 2>/dev/null || true)" = inactive ] || die "proxy unit did not stop"
    ! port_listening "$proxy_port" || die "proxy socket remained after unit stop"
    ;;
  *) die "unknown action: $action" ;;
esac
