#!/usr/bin/env bash
# Shared helpers for CloudEdge acceptance live runners.

ce_die() { printf '%s: %s\n' "${SELF:-cloudedge-runner}" "$*" >&2; exit 1; }
ce_log() { printf '%s %s\n' "[$(date -u +%H:%M:%SZ)]" "$*" >&2; }
ce_have() { command -v "$1" >/dev/null 2>&1; }

ce_upper() {
  printf '%s' "$1" | tr '[:lower:]-' '[:upper:]_'
}

ce_env() {
  local name=$1 default=${2:-}
  printf '%s' "${!name:-$default}"
}

ce_env_first() {
  local name
  for name in "$@"; do
    if [[ -n "${!name:-}" ]]; then
      printf '%s' "${!name}"
      return 0
    fi
  done
  return 1
}

ce_site_ip() {
  local site=$1 upper
  upper=$(ce_upper "$site")
  ce_env "${upper}_CLIENT_IP" ""
}

ce_ssh_target() {
  local host=$1 user=$2
  if [[ "$host" == *@* ]]; then
    printf '%s' "$host"
  else
    printf '%s@%s' "$user" "$host"
  fi
}

ce_ssh_opts() {
  local known_hosts=${CE_SSH_KNOWN_HOSTS:-${CE_SSH_USER_KNOWN_HOSTS_FILE:-$HOME/.ssh/known_hosts}}
  local strict=${CE_SSH_STRICT_HOST_KEY_CHECKING:-yes}
  local opts=(-o BatchMode=yes -o StrictHostKeyChecking="$strict"
              -o UserKnownHostsFile="$known_hosts"
              -o ConnectTimeout="${CE_SSH_CONNECT_TIMEOUT:-8}")
  if [[ -n "${SSH_KEY_FILE:-}" ]]; then
    opts=(-i "$SSH_KEY_FILE" "${opts[@]}")
  fi
  if [[ -n "${CE_SSH_EXTRA_OPTS:-}" ]]; then
    # shellcheck disable=SC2206
    local extra=( ${CE_SSH_EXTRA_OPTS} )
    opts+=("${extra[@]}")
  fi
  printf '%q ' "${opts[@]}"
}

nested_ssh_opts() {
  local known_hosts=${CE_NESTED_SSH_KNOWN_HOSTS:-}
  local strict=${CE_NESTED_SSH_STRICT_HOST_KEY_CHECKING:-yes}
  [[ -n "$known_hosts" ]] || known_hosts='$HOME/.ssh/known_hosts'
  local user_opts="-o BatchMode=yes -o StrictHostKeyChecking=$strict -o UserKnownHostsFile=$known_hosts -o ConnectTimeout=${CE_NESTED_SSH_CONNECT_TIMEOUT:-8}"
  if [[ -n "${CE_NESTED_SSH_EXTRA_OPTS:-}" ]]; then
    user_opts="$user_opts ${CE_NESTED_SSH_EXTRA_OPTS}"
  fi
  printf '%s' "$user_opts"
}

ce_ssh() {
  local host=$1; shift
  local user=${CE_SSH_USER:-${SSH_USER:-ubuntu}}
  local target opts
  target=$(ce_ssh_target "$host" "$user")
  # shellcheck disable=SC2207
  opts=($(ce_ssh_opts))
  # Commands are intentionally composed by runner callers and executed remotely.
  # shellcheck disable=SC2029
  ssh "${opts[@]}" "$target" "$@"
}

ce_client_host() {
  local site=$1 upper
  upper=$(ce_upper "$site")
  ce_env_first "CE_${upper}_CLIENT_SSH_HOST" "${upper}_CLIENT_SSH_HOST" || true
}

ce_router_host() {
  local provider=$1 role=${2:-observer} upper
  upper=$(ce_upper "$provider")
  case "$role" in
    active|a)
      ce_env_first "CE_${upper}_ACTIVE_ROUTER_SSH_HOST" "CE_${upper}_ROUTER_A_SSH_HOST" "${upper}_ROUTER_A_SSH_HOST" "${upper}_ROUTER_SSH_HOST" || true
      ;;
    standby|b|observer)
      ce_env_first "CE_${upper}_OBSERVER_ROUTER_SSH_HOST" "CE_${upper}_ROUTER_B_SSH_HOST" "${upper}_ROUTER_B_SSH_HOST" "${upper}_ROUTER_SSH_HOST" || true
      ;;
    *)
      ce_env_first "CE_${upper}_ROUTER_SSH_HOST" "${upper}_ROUTER_SSH_HOST" || true
      ;;
  esac
}

ce_client_ssh() {
  local site=$1; shift
  local host
  host=$(ce_client_host "$site")
  [[ -n "$host" ]] || ce_die "missing client SSH host for site '$site' (set CE_$(ce_upper "$site")_CLIENT_SSH_HOST or $(ce_upper "$site")_CLIENT_SSH_HOST)"
  ce_ssh "$host" "$@"
}

ce_router_ssh() {
  local provider=$1 role=${2:-observer}; shift 2
  local host
  host=$(ce_router_host "$provider" "$role")
  [[ -n "$host" ]] || ce_die "missing router SSH host for provider '$provider' role '$role'"
  ce_ssh "$host" "$@"
}

ce_state_db() {
  printf '%s' "${CE_ROUTERD_STATE_DB:-/var/lib/routerd/routerd.db}"
}

ce_router_sql() {
  local provider=$1 role=${2:-observer} sql=$3 db
  db=$(ce_state_db)
  ce_router_ssh "$provider" "$role" "sudo sqlite3 -readonly '$db' $(printf '%q' "$sql")"
}

ce_run_stage_command() {
  local provider=$1 stage=$2 upper stage_upper cmd
  upper=$(ce_upper "$provider")
  stage_upper=$(ce_upper "$stage")
  cmd=$(ce_env_first "CE_${upper}_${stage_upper}_COMMAND" "CE_${stage_upper}_COMMAND" 2>/dev/null || true)
  [[ -n "$cmd" ]] || return 1
  bash -lc "$cmd"
}

ce_json_get() {
  local file=$1 path=$2 default=${3:-}
  python3 - "$file" "$path" "$default" <<'PY'
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except Exception:
    print(sys.argv[3])
    raise SystemExit(0)
cur = data
for part in sys.argv[2].split("."):
    if isinstance(cur, dict):
        cur = cur.get(part, "")
    else:
        cur = ""
print(cur if cur not in (None, "") else sys.argv[3])
PY
}
