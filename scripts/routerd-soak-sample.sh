#!/usr/bin/env sh
set -eu

LOG_DIR="${ROUTERD_SOAK_LOG_DIR:-/var/log}"
DATE="$(date -u +%Y%m%d)"
LOG_FILE="${ROUTERD_SOAK_LOG_FILE:-$LOG_DIR/routerd-soak-$DATE.log}"
HOST="$(hostname -s 2>/dev/null || hostname 2>/dev/null || echo unknown)"
ROUTERCTL="${ROUTERCTL:-routerctl}"
WG_INTERFACE="${ROUTERD_SOAK_WG_INTERFACE:-wg-mesh}"

timestamp() {
  date -u +%Y-%m-%dT%H:%M:%SZ
}

json_escape() {
  sed 's/\\/\\\\/g; s/"/\\"/g'
}

append_line() {
  section="$1"
  status="$2"
  body="$3"
  printf '{"time":"%s","host":"%s","section":"%s","status":"%s","body":"%s"}\n' \
    "$(timestamp)" "$HOST" "$section" "$status" "$(printf '%s' "$body" | tr '\n' ' ' | json_escape)" >> "$LOG_FILE"
}

capture() {
  section="$1"
  shift
  if out="$("$@" 2>&1)"; then
    append_line "$section" "ok" "$out"
  else
    code="$?"
    append_line "$section" "error:$code" "$out"
  fi
}

mkdir -p "$LOG_DIR"
capture routerctl-status "$ROUTERCTL" get status -o json
capture wireguard "$ROUTERCTL" vpn wireguard show "$WG_INTERFACE" -o json
if "$ROUTERCTL" vpn tailscale peers -o json >/dev/null 2>&1; then
  capture tailscale "$ROUTERCTL" vpn tailscale peers -o json
elif command -v tailscale >/dev/null 2>&1; then
  if tailscale status --json >/dev/null 2>&1; then
    capture tailscale tailscale status --json
  else
    out="$(tailscale status --json 2>&1 || true)"
    append_line tailscale skipped "$out"
  fi
else
  append_line tailscale skipped "tailscale command unavailable"
fi

if command -v ps >/dev/null 2>&1; then
  if pids="$(pgrep -x routerd 2>/dev/null | paste -sd, -)" && [ -n "$pids" ]; then
    capture routerd-process ps -p "$pids" -o pid,rss,etime,comm
  else
    append_line routerd-process skipped "routerd pid not found"
  fi
fi

if command -v ss >/dev/null 2>&1; then
  capture routerd-sockets ss -uapn
fi

append_line "soak-sample" "done" "sample complete"
