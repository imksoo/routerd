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

sample_packets() {
  local provider=$1 filter=$2 script
  script='set -eu
command -v tcpdump >/dev/null 2>&1 || { echo "tcpdump unavailable" >&2; exit 2; }
packets=$(mktemp); errors=$(mktemp)
trap '\''rm -f "$packets" "$errors"'\'' EXIT
rc=0
LC_ALL=C sudo timeout --signal=INT "$1" tcpdump -nne -i "$2" "$3" >"$packets" 2>"$errors" || rc=$?
cat "$errors" >&2; cat "$packets" >&2
case "$rc" in 0|124) ;; *) exit 2 ;; esac
grep -q "packets captured" "$errors" || exit 2
grep -q "^0 packets dropped by kernel$" "$errors" || exit 2
wc -l <"$packets"'
  l2_ssh "$provider" "bash -c $(printf '%q' "$script") -- $(printf '%q' "$sample") $(printf '%q' "$iface") $(printf '%q' "$filter")"
}

cmd_observe() {
  local phase=$1 provider=$2 cmd iface sample ping_target broadcast stp macflap blocked bpdu loss mechanism errors=0
  cmd=$(ce_env_first CE_L2_METRICS_COMMAND "CE_$(ce_upper "$provider")_L2_METRICS_COMMAND" 2>/dev/null || true)
  if [[ -n "$cmd" ]]; then
    CE_L2_PHASE=$phase CE_L2_PROVIDER=$provider bash -lc "$cmd"
    return
  fi
  iface=${CE_L2_IFACE:-br0}
  sample=${CE_L2_SAMPLE_SECONDS:-5}
  ping_target=${CE_L2_PING_TARGET:-}
  [[ "$sample" =~ ^[0-9]+([.][0-9]+)?$ ]] && awk -v s="$sample" 'BEGIN{exit !(s>0)}' || ce_die "invalid sample duration"
  broadcast=$(sample_packets "$provider" 'ether broadcast') || { broadcast=unavailable; errors=1; }
  stp=$(sample_packets "$provider" 'ether dst 01:80:c2:00:00:00') || { stp=unavailable; errors=1; }
  macflap=$(l2_ssh "$provider" "set -e; raw=\$(journalctl -k --since '-2 min'); printf '%s\n' \"\$raw\" >&2; printf '%s\n' \"\$raw\" | grep -Eic 'flap|moving from|received packet on .* with own address' || test \$? -eq 1") || { macflap=unavailable; errors=1; }
  blocked=$(l2_ssh "$provider" "set -e; raw=\$(bridge link show); printf '%s\n' \"\$raw\" | grep -Eic 'state (blocking|listening)' || test \$? -eq 1") || blocked=unavailable
  bpdu=unknown
  if [[ "$stp" =~ ^[0-9]+$ ]]; then bpdu=false; [[ "$stp" -eq 0 ]] || bpdu=true; fi
  loss=unavailable
  if [[ -n "$ping_target" ]]; then
    loss=$(l2_ssh "$provider" "raw=\$(LC_ALL=C ping -c ${CE_L2_PING_COUNT:-20} -i ${CE_L2_PING_INTERVAL:-0.2} -W1 $(printf '%q' "$ping_target") 2>&1); rc=\$?; printf '%s\n' \"\$raw\" >&2; [ \"\$rc\" -le 1 ] || exit 2; printf '%s\n' \"\$raw\" | awk -F',' '/packet loss/ {gsub(/% packet loss/,\"\",\$3); gsub(/ /,\"\",\$3); print \$3; found=1} END{if(!found) exit 2}'") || errors=1
  else
    echo 'ping target unavailable; loss was not measured' >&2
    errors=1
  fi
  mechanism=${CE_L2_MECHANISM:-vrrp-single-master+non-master-fail-closed+stp-rstp-bpdu-observed}
  if [[ "$broadcast" =~ ^[0-9]+$ ]]; then
    printf 'broadcast_pps=%s\n' "$(awk -v n="$broadcast" -v s="$sample" 'BEGIN{printf "%.3f", n/s}')"
  else printf 'broadcast_pps=unavailable\n'; fi
  printf 'stp_tcn_delta=%s\nmac_flap_count=%s\nping_loss_percent=%s\nblocked_ports=%s\nbpdu_seen=%s\nmechanism=%s\n' "$stp" "$macflap" "$loss" "$blocked" "$bpdu" "$mechanism"
  printf 'detail=phase=%s provider=%s iface=%s sample_seconds=%s measurement_errors=%s\n' "$phase" "$provider" "$iface" "$sample" "$errors"
  [[ "$errors" -eq 0 ]] || return 2
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
