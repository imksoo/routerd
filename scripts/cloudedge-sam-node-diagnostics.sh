#!/usr/bin/env bash
#
# Collect the repeatable CloudEdge SAM evidence set from cloud/onprem routers.
# This script is read-only: it runs provider describe/show calls and SSH
# diagnostics, but does not mutate cloud resources or guest network state.
#
set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")

die() { printf '%s: %s\n' "$SELF" "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

usage() {
  cat <<EOF
$SELF - collect CloudEdge SAM node diagnostics

USAGE:
  $SELF collect   --env <env.sh> --out <dir> [--label <name>] [--nodes <list>]
  $SELF preflight --env <env.sh> --out <dir> [--label <name>] [--nodes <list>]

Default nodes:
  onprem,aws-a,aws-b,azure,azure-b,oci,oci-b

The output includes:
  provider/*.json|*.err
  nodes/<node>.log
  summary/preflight-findings.txt

Use preflight before deploy/T1/A1/A2 to catch leftover provider or OS state.
Use collect after convergence and after failures so runs can be compared with
the same evidence shape.
EOF
}

sub=${1:-}
[[ "$sub" == "-h" || "$sub" == "--help" || "$sub" == "help" ]] && { usage; exit 0; }
case "$sub" in collect|preflight) ;; *) usage >&2; die "expected subcommand: collect or preflight" ;; esac
shift

env_file=""
out=""
label="$sub"
nodes_csv="onprem,aws-a,aws-b,azure,azure-b,oci,oci-b"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --env) env_file="${2:-}"; shift 2 ;;
    --out) out="${2:-}"; shift 2 ;;
    --label) label="${2:-}"; shift 2 ;;
    --nodes) nodes_csv="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

[[ -n "$env_file" ]] || die "--env is required"
[[ -f "$env_file" ]] || die "env file not found: $env_file"
[[ -n "$out" ]] || die "--out is required"

# shellcheck disable=SC1090
. "$env_file"

mkdir -p "$out/provider" "$out/nodes" "$out/summary"

run_provider() {
  local name=$1
  shift
  {
    printf '$'
    printf ' %q' "$@"
    printf '\n'
  } >"$out/provider/${name}.cmd"
  "$@" >"$out/provider/${name}.json" 2>"$out/provider/${name}.err" || true
}

collect_provider_state() {
  if have aws && [[ -n "${AWS_REGION:-}" ]]; then
    local aws_args=(--region "$AWS_REGION")
    [[ -n "${AWS_PROFILE:-}" ]] && aws_args=(--profile "$AWS_PROFILE" "${aws_args[@]}")
    local enis=()
    [[ -n "${AWS_ROUTER_A_ENI_REF:-}" ]] && enis+=("$AWS_ROUTER_A_ENI_REF")
    [[ -n "${AWS_ROUTER_B_ENI_REF:-}" ]] && enis+=("$AWS_ROUTER_B_ENI_REF")
    [[ -n "${AWS_CLIENT_ENI:-}" ]] && enis+=("$AWS_CLIENT_ENI")
    [[ "${#enis[@]}" -gt 0 ]] && run_provider aws-network-interfaces aws ec2 describe-network-interfaces "${aws_args[@]}" --network-interface-ids "${enis[@]}"
    [[ -n "${AWS_ROUTE_TABLE_REF:-}" ]] && run_provider aws-route-table aws ec2 describe-route-tables "${aws_args[@]}" --route-table-ids "$AWS_ROUTE_TABLE_REF"
  fi

  if have az && [[ -n "${AZURE_RESOURCE_GROUP:-}" ]]; then
    [[ -n "${AZURE_SUBSCRIPTION_ID:-}" ]] && run_provider azure-account az account show --subscription "$AZURE_SUBSCRIPTION_ID"
    [[ -n "${AZURE_ROUTER_NIC_REF:-}" ]] && run_provider azure-router-nic az network nic show --ids "$AZURE_ROUTER_NIC_REF"
    [[ -n "${AZURE_ROUTER_B_NIC_REF:-}" ]] && run_provider azure-router-b-nic az network nic show --ids "$AZURE_ROUTER_B_NIC_REF"
    [[ -n "${AZURE_ROUTE_TABLE_REF:-}" ]] && run_provider azure-route-table az network route-table show --ids "$AZURE_ROUTE_TABLE_REF"
    [[ -n "${AZURE_ROUTER_VM_NAME:-}" ]] && run_provider azure-router-vm az vm get-instance-view -g "$AZURE_RESOURCE_GROUP" -n "$AZURE_ROUTER_VM_NAME"
    [[ -n "${AZURE_ROUTER_B_VM_NAME:-}" ]] && run_provider azure-router-b-vm az vm get-instance-view -g "$AZURE_RESOURCE_GROUP" -n "$AZURE_ROUTER_B_VM_NAME"
  fi

  if have oci; then
    local oci_args=()
    [[ -n "${OCI_CONFIG_FILE:-}" ]] && oci_args+=(--config-file "$OCI_CONFIG_FILE")
    [[ -n "${OCI_PROFILE:-}" ]] && oci_args+=(--profile "$OCI_PROFILE")
    [[ -n "${OCI_REGION:-}" ]] && oci_args+=(--region "$OCI_REGION")
    [[ -n "${OCI_ROUTER_VNIC_REF:-}" ]] && run_provider oci-router-vnic oci network vnic get "${oci_args[@]}" --vnic-id "$OCI_ROUTER_VNIC_REF"
    [[ -n "${OCI_ROUTER_B_VNIC_REF:-}" ]] && run_provider oci-router-b-vnic oci network vnic get "${oci_args[@]}" --vnic-id "$OCI_ROUTER_B_VNIC_REF"
    [[ -n "${OCI_ROUTER_VNIC_REF:-}" ]] && run_provider oci-router-private-ips oci network private-ip list "${oci_args[@]}" --vnic-id "$OCI_ROUTER_VNIC_REF"
    [[ -n "${OCI_ROUTER_B_VNIC_REF:-}" ]] && run_provider oci-router-b-private-ips oci network private-ip list "${oci_args[@]}" --vnic-id "$OCI_ROUTER_B_VNIC_REF"
  fi
  return 0
}

node_target() {
  local node=$1
  case "$node" in
    onprem) printf '%s|%s|%s\n' "${ONPREM_SSH_USER:-nwadmin}" "${ONPREM_ROUTER_SSH_HOST:-}" "${ONPREM_SSH_JUMP:-root@pve05}" ;;
    aws-a) printf '%s|%s|\n' "${AWS_ROUTER_A_SSH_USER:-ubuntu}" "${AWS_ROUTER_A_SSH_HOST:-}" ;;
    aws-b) printf '%s|%s|\n' "${AWS_ROUTER_B_SSH_USER:-ubuntu}" "${AWS_ROUTER_B_SSH_HOST:-}" ;;
    azure) printf '%s|%s|\n' "${AZURE_ROUTER_SSH_USER:-azureuser}" "${AZURE_ROUTER_SSH_HOST:-}" ;;
    azure-b) printf '%s|%s|\n' "${AZURE_ROUTER_B_SSH_USER:-azureuser}" "${AZURE_ROUTER_B_SSH_HOST:-}" ;;
    oci) printf '%s|%s|\n' "${OCI_ROUTER_SSH_USER:-ubuntu}" "${OCI_ROUTER_SSH_HOST:-}" ;;
    oci-b) printf '%s|%s|\n' "${OCI_ROUTER_B_SSH_USER:-ubuntu}" "${OCI_ROUTER_B_SSH_HOST:-}" ;;
    *) return 1 ;;
  esac
}

collect_node() {
  local node=$1
  local user host jump
  IFS='|' read -r user host jump < <(node_target "$node")
  if [[ -z "$host" ]]; then
    printf 'missing host for %s\n' "$node" >"$out/nodes/${node}.log"
    return 0
  fi

  local ssh_base=(-o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=15 -o ServerAliveInterval=15 -o ServerAliveCountMax=3)
  [[ -n "${SSH_KEY_FILE:-}" ]] && ssh_base+=(-i "$SSH_KEY_FILE")
  [[ -n "$jump" ]] && ssh_base+=(-J "$jump")

  ssh "${ssh_base[@]}" "$user@$host" 'set +e
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
SUDO=""
if command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then
  SUDO="sudo -n"
fi
echo "== diagnostic label =="; echo "'"$label"'"
echo "== identity =="; hostname; date -u; uname -a; id; pwd
echo "== versions =="; routerd --version 2>&1 || true; routerctl --version 2>&1 || true
echo "== service-active =="; systemctl is-active routerd.service routerd-eventd@cloudedge.service routerd-bgp.service 2>&1 || true
echo "== service-status =="; systemctl status routerd.service routerd-eventd@cloudedge.service routerd-bgp.service --no-pager -l 2>&1 | sed -n "1,260p"
echo "== routerctl status json =="; $SUDO routerctl get status -o json 2>&1 || true
echo "== routerctl status phase grep =="; $SUDO routerctl get status -o json 2>/dev/null | jq "{phase: .status.phase, generation: .status.generation, samConvergencePhase, cloudClaimPhase, osCapturePhase, forwardingPhase, fibConvergencePhase, advertisementGatePhase, blockingReasons}" 2>&1 || true
echo "== mobilitypool cloudedge json =="; $SUDO routerctl describe MobilityPool/cloudedge -o json 2>&1 || true
echo "== mobilitypool phase grep =="; $SUDO routerctl describe MobilityPool/cloudedge -o json 2>/dev/null | jq ".status // . | {samConvergencePhase,cloudClaimPhase,osCapturePhase,forwardingPhase,fibConvergencePhase,advertisementGatePhase,blockingReasons,ownershipResolverOwnerTable,ownershipResolverFIBVerdicts,ownershipResolverConflicts}" 2>&1 || true
echo "== doctor sam json =="; $SUDO routerctl doctor sam -o json 2>&1 || true
echo "== doctor sam text =="; $SUDO routerctl doctor sam 2>&1 || true
echo "== doctor all json =="; $SUDO routerctl doctor -o json 2>&1 || true
echo "== mobility paths =="; for p in 10.77.60.10/32 10.77.60.11/32 10.77.60.12/32 10.77.60.13/32; do echo "-- $p"; $SUDO routerctl mobility paths --prefix "$p" -o json 2>&1 || $SUDO routerctl mobility paths --prefix "$p" 2>&1 || true; done
echo "== bgp =="; if command -v vtysh >/dev/null 2>&1; then vtysh -c "show bgp summary" 2>&1 || true; vtysh -c "show bgp ipv4 unicast json" 2>&1 || true; vtysh -c "show ip route json" 2>&1 || true; else echo "vtysh not found"; fi
echo "== os ip addr/link =="; ip -br addr; ip -j addr show 2>&1 || true; ip -d -j link show 2>&1 || true; ip -d link show 2>&1 || true
echo "== os routes/rules =="; ip -4 route show table main; ip -4 route show table local; ip -4 route show table all; ip rule show
echo "== route get mobility IPs =="; for d in 10.77.60.10 10.77.60.11 10.77.60.12 10.77.60.13; do echo "-- $d"; ip -4 route get "$d" || true; done
echo "== neighbor/arp =="; ip neigh show; arp -an 2>&1 || true
echo "== sysctl forwarding/rpf/proxy/promisc hints =="; sysctl net.ipv4.ip_forward 2>&1 || true; for f in /proc/sys/net/ipv4/conf/*/rp_filter /proc/sys/net/ipv4/conf/*/proxy_arp /proc/sys/net/ipv4/conf/*/arp_ignore /proc/sys/net/ipv4/conf/*/arp_announce; do echo "$f=$(cat "$f" 2>/dev/null)"; done
echo "== nft/iptables =="; nft list ruleset 2>&1 | sed -n "1,260p" || true; iptables-save 2>&1 | sed -n "1,220p" || true
echo "== tmp perms =="; stat -c "%a %U %G %n" /tmp /var/tmp 2>&1 || true; mount | grep -E " on /(tmp|var/tmp) " || true
echo "== recent logs =="; journalctl -u routerd.service -u routerd-eventd@cloudedge.service -u routerd-bgp.service --since "30 minutes ago" --no-pager -n 800 2>&1 || true
' >"$out/nodes/${node}.log" 2>&1 || true
}

summarize_preflight() {
  {
    echo "# $label preflight findings"
    echo
    echo "Generated: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo
    echo "## Provider command errors"
    for f in "$out"/provider/*.err; do
      [[ -s "$f" ]] || continue
      echo "- $(basename "$f"): non-empty stderr"
    done
    echo
    echo "## Guest warning grep"
    for f in "$out"/nodes/*.log; do
      [[ -f "$f" ]] || continue
      node=$(basename "$f" .log)
      if grep -Eiq 'samConvergencePhase.*(Failed|Degraded)|cloudClaimPhase.*(Failed|Degraded)|osCapturePhase.*(Failed|Degraded)|fibConvergencePhase.*(Failed|Degraded)|advertisementGatePhase.*(Failed|Degraded)|split.?brain|blockingReasons.*[^][]' "$f"; then
        echo "- $node: SAM phase/blocking warning present"
      fi
      if grep -Eq '10\.77\.60\.(10|11|12|13)/32' "$f"; then
        echo "- $node: mobility /32 appears in OS/routerd output; verify it is intended for this phase"
      fi
      if grep -Eiq 'Permission denied|command not found|No such file|failed|error' "$f"; then
        echo "- $node: generic error text present; inspect nodes/$node.log"
      fi
    done
  } >"$out/summary/preflight-findings.txt"
}

collect_provider_state

IFS=',' read -r -a nodes <<<"$nodes_csv"
for node in "${nodes[@]}"; do
  collect_node "$node"
done

summarize_preflight
printf '%s\n' "$out"
