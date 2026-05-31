#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ENV_FILE=${ENV_FILE:-"$ROOT/env"}
set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

TS=$(date -u +%Y%m%dT%H%M%SZ)
OUT=${1:-"${EVIDENCE_ROOT:-$ROOT/evidence}/$TS"}
mkdir -p "$OUT"

SSH_OPTS=(-i "$SSH_KEY_FILE" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10)

ssh_router() {
  local user=${2:-$SSH_USER}
  ssh "${SSH_OPTS[@]}" "$user@$1" "$3"
}

collect_router() {
  local name=$1 host=$2 user=${3:-$SSH_USER}
  {
    echo "## $name"
    ssh_router "$host" "$user" "hostname; ip -br addr; ip route; sudo wg show || true; sudo nft list table inet routerd_mss || true"
    echo "--- doctor"
    ssh_router "$host" "$user" "sudo $ROUTERCTL_BIN doctor hybrid -o json || true"
    echo "--- dynamic"
    ssh_router "$host" "$user" "sudo $ROUTERCTL_BIN dynamic list -o json || true; sudo $ROUTERCTL_BIN dynamic render -o json || true"
    echo "--- actions"
    ssh_router "$host" "$user" "sudo $ROUTERCTL_BIN action list -o json || true"
    echo "--- mobility db"
    ssh_router "$host" "$user" "sudo sqlite3 -header -column /var/lib/routerd/routerd.db 'select * from mobility_capture_epochs;' || true; sudo sqlite3 -header -column /var/lib/routerd/routerd.db 'select * from mobility_deprovision_markers;' || true"
  } > "$OUT/$name.txt" 2>&1
}

collect_router onprem "$ONPREM_ROUTER_SSH_HOST" "${ONPREM_SSH_USER:-$SSH_USER}"
collect_router aws-a "$AWS_ROUTER_A_SSH_HOST" "${AWS_ROUTER_A_SSH_USER:-$SSH_USER}"
collect_router aws-b "$AWS_ROUTER_B_SSH_HOST" "${AWS_ROUTER_B_SSH_USER:-$SSH_USER}"
collect_router azure "$AZURE_ROUTER_SSH_HOST" "${AZURE_ROUTER_SSH_USER:-$SSH_USER}"
collect_router oci "$OCI_ROUTER_SSH_HOST" "${OCI_ROUTER_SSH_USER:-$SSH_USER}"

if command -v aws >/dev/null; then
  aws ec2 describe-network-interfaces --profile "$AWS_PROFILE" --region "$AWS_REGION" \
    --network-interface-ids "$AWS_ROUTER_A_ENI_REF" "$AWS_ROUTER_B_ENI_REF" \
    > "$OUT/aws-network-interfaces.json" 2>&1 || true
fi

if command -v az >/dev/null; then
  az network nic show --ids "$AZURE_ROUTER_NIC_REF" > "$OUT/azure-router-nic.json" 2>&1 || true
fi

if command -v oci >/dev/null; then
  oci network vnic get --vnic-id "$OCI_ROUTER_VNIC_REF" --config-file "$OCI_CONFIG_FILE" --profile "$OCI_PROFILE" --auth security_token \
    > "$OUT/oci-router-vnic.json" 2>&1 || true
fi

cat > "$OUT/summary.md" <<EOF
# CloudEdge Mobility Demo Evidence

Collected: $TS

- Router evidence: onprem, aws-a, aws-b, azure, oci.
- Provider evidence: AWS ENIs, Azure NIC, OCI VNIC when matching CLIs are available.
- Inspect action journals and mobility_capture_epochs to confirm D5 epoch migration.
EOF

echo "wrote $OUT"
