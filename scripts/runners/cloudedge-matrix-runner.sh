#!/usr/bin/env bash
# Live MATRIX_RUNNER implementation for scripts/cloudedge-connectivity-matrix.sh.

set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/runners/cloudedge-runner-lib.sh
. "$SCRIPT_DIR/cloudedge-runner-lib.sh"

usage() {
  cat <<EOF
$SELF - CloudEdge live MATRIX_RUNNER

USAGE:
  MATRIX_RUNNER=$SCRIPT_DIR/$SELF scripts/cloudedge-connectivity-matrix.sh
  $SELF ping <src-site> <dst-ip>
  $SELF ssh  <src-site> <dst-ip>

ENV:
  SSH_KEY_FILE, SSH_USER, CE_SSH_USER, CE_SSH_EXTRA_OPTS
  CE_SSH_KNOWN_HOSTS          Known-hosts file for source VM SSH verification
  <SITE>_CLIENT_SSH_HOST or CE_<SITE>_CLIENT_SSH_HOST
  CLIENT_SSH_USER             User for nested site-to-site SSH (default ubuntu)
  CE_NESTED_SSH_KNOWN_HOSTS   Known-hosts file visible on the source VM
  CE_NESTED_SSH_EXTRA_OPTS    Extra nested SSH options

The runner executes commands on the source client VM over SSH, then uses the
source client to ping/SSH the destination logical IP. It prints peer_ip and
default_gw for the matrix source-IP/default-gateway checks.
EOF
}

cmd_ping() {
  local src=$1 dst_ip=$2
  ce_client_ssh "$src" "ping -c${CE_MATRIX_PING_COUNT:-3} -W${CE_MATRIX_PING_TIMEOUT:-2} $(printf '%q' "$dst_ip") >/dev/null"
}

cmd_ssh() {
  local src=$1 dst_ip=$2 user opts
  user=${CLIENT_SSH_USER:-ubuntu}
  opts=$(nested_ssh_opts)
  ce_client_ssh "$src" "ssh $opts $(printf '%q' "$user@$dst_ip") 'echo peer_ip=\$(echo \$SSH_CONNECTION | awk \"{print \\\$1}\")'; echo default_gw=\$(ip route show default | awk '{print \$3; exit}')"
}

main() {
  local op=${1:-}
  case "$op" in
    ping)
      [[ $# -eq 3 ]] || ce_die "ping requires <src-site> <dst-ip>"
      cmd_ping "$2" "$3"
      ;;
    ssh)
      [[ $# -eq 3 ]] || ce_die "ssh requires <src-site> <dst-ip>"
      cmd_ssh "$2" "$3"
      ;;
    -h|--help|help|"")
      usage
      ;;
    *)
      ce_die "unknown op: $op"
      ;;
  esac
}

main "$@"
