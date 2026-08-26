#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause
set -euo pipefail

# shellcheck disable=SC2034
TEST_NAME="graceful-vrrp-activation"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"

require_common
require_cmd arping
if [[ -z "${ROUTERD_GRACEFUL_VRRP_DRIVER:-}" ]]; then
  require_cmd go
fi

NS="${TEST_ID}-router"
PEER="${TEST_ID}-peer"
DRIVER="${ROUTERD_GRACEFUL_VRRP_DRIVER:-$WORKDIR/graceful-vrrp-controller-driver}"
create_ns "$NS"
create_ns "$PEER"
create_veth_pair "$NS" eth0 172.18.0.2/16 "$PEER" eth0 172.18.0.3/16

if [[ -z "${ROUTERD_GRACEFUL_VRRP_DRIVER:-}" ]]; then
  (cd "$REPO_ROOT" && go build -o "$DRIVER" ./tests/netns/graceful-vrrp-controller-driver)
else
  [[ -x "$DRIVER" ]] || fail "missing executable driver: $DRIVER"
fi
ip netns exec "$NS" env ROUTERD_NETNS_TEST_TOKEN="$TEST_ID" "$DRIVER" "$WORKDIR/runtime" "$WORKDIR/keepalived.conf" eth0

log "ok: elected role withheld VIP until readiness and withdrew it on BACKUP"
