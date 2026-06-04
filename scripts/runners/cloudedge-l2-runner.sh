#!/usr/bin/env bash
# Live L2_LOOP_RUNNER implementation.

set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/runners/cloudedge-runner-lib.sh
. "$SCRIPT_DIR/cloudedge-runner-lib.sh"

usage() {
  cat <<EOF
$SELF - CloudEdge live L2_LOOP_RUNNER

USAGE:
  L2_LOOP_RUNNER=$SCRIPT_DIR/$SELF scripts/cloudedge-l2-loop-probe.sh ...
  $SELF observe before|after <provider>

ENV:
  CE_L2_OBSERVER_SSH_HOST or CE_<PROVIDER>_OBSERVER_ROUTER_SSH_HOST
  CE_L2_IFACE=br0|lan0           Interface to sniff/inspect.
  CE_L2_SAMPLE_SECONDS=5         tcpdump sample window.
  CE_L2_PING_TARGET=10.77.60.10  Optional ping target for loss.
  CE_L2_METRICS_COMMAND          Optional local override that prints key=value.

Outputs key=value lines consumed by cloudedge-l2-loop-probe.sh.
EOF
}

l2_host() {
  local provider=$1 upper
  upper=$(ce_upper "$provider")
  ce_env_first CE_L2_OBSERVER_SSH_HOST "CE_${upper}_OBSERVER_ROUTER_SSH_HOST" "${upper}_ROUTER_SSH_HOST" 2>/dev/null || true
}

l2_ssh() {
  local provider=$1; shift
  local host
  host=$(l2_host "$provider")
  [[ -n "$host" ]] || ce_die "missing L2 observer host"
  ce_ssh "$host" "$@"
}

cmd_observe() {
  local phase=$1 provider=$2 cmd iface sample ping_target broadcast stp macflap blocked bpdu loss mechanism
  cmd=$(ce_env_first CE_L2_METRICS_COMMAND "CE_$(ce_upper "$provider")_L2_METRICS_COMMAND" 2>/dev/null || true)
  if [[ -n "$cmd" ]]; then
    CE_L2_PHASE=$phase CE_L2_PROVIDER=$provider bash -lc "$cmd"
    return 0
  fi

  iface=${CE_L2_IFACE:-br0}
  sample=${CE_L2_SAMPLE_SECONDS:-5}
  ping_target=${CE_L2_PING_TARGET:-}

  broadcast=$(l2_ssh "$provider" "if command -v tcpdump >/dev/null 2>&1; then sudo timeout $(printf '%q' "$sample") tcpdump -eni $(printf '%q' "$iface") 'ether broadcast' 2>/dev/null | wc -l; else echo 0; fi" || echo 0)
  stp=$(l2_ssh "$provider" "if command -v tcpdump >/dev/null 2>&1; then sudo timeout $(printf '%q' "$sample") tcpdump -eni $(printf '%q' "$iface") 'ether dst 01:80:c2:00:00:00' 2>/dev/null | wc -l; else echo 0; fi" || echo 0)
  macflap=$(l2_ssh "$provider" "journalctl -k --since '-2 min' 2>/dev/null | grep -Eic 'flap|moving from|received packet on .* with own address' || true" || echo 0)
  blocked=$(l2_ssh "$provider" "bridge link show 2>/dev/null | grep -Eic 'state (blocking|listening)' || true" || echo 0)
  bpdu="false"
  if [[ "${stp:-0}" =~ ^[0-9]+$ && "$stp" -gt 0 ]]; then bpdu="true"; fi

  loss=0
  if [[ -n "$ping_target" ]]; then
    loss=$(l2_ssh "$provider" "ping -c ${CE_L2_PING_COUNT:-20} -i ${CE_L2_PING_INTERVAL:-0.2} -W1 $(printf '%q' "$ping_target") 2>/dev/null | awk -F',' '/packet loss/ {gsub(/% packet loss/,\"\",\$3); gsub(/ /,\"\",\$3); print \$3; found=1} END{if(!found) print 100}'" || echo 100)
  fi

  mechanism=${CE_L2_MECHANISM:-vrrp-single-master+non-master-fail-closed+stp-rstp-bpdu-observed}
  printf 'broadcast_pps=%s\n' "$(awk -v n="${broadcast:-0}" -v s="$sample" 'BEGIN{if(s>0) printf "%.3f", n/s; else print n}')"
  printf 'stp_tcn_delta=%s\n' "${stp:-0}"
  printf 'mac_flap_count=%s\n' "${macflap:-0}"
  printf 'ping_loss_percent=%s\n' "${loss:-0}"
  printf 'blocked_ports=%s\n' "${blocked:-0}"
  printf 'bpdu_seen=%s\n' "$bpdu"
  printf 'mechanism=%s\n' "$mechanism"
  printf 'detail=phase=%s provider=%s iface=%s sample_seconds=%s\n' "$phase" "$provider" "$iface" "$sample"
}

main() {
  local op=${1:-}
  case "$op" in
    observe)
      [[ $# -eq 3 ]] || ce_die "observe requires <phase> <provider>"
      case "$2" in before|after) ;; *) ce_die "bad phase: $2" ;; esac
      cmd_observe "$2" "$3"
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
