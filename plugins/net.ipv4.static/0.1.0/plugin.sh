#!/usr/bin/env bash
set -euo pipefail

jq -n --arg action "${ROUTERD_ACTION:-unknown}" '{
  ok: false,
  error: {
    code: "NotImplemented",
    message: ("net.ipv4.static " + $action + " is not implemented yet")
  }
}'
