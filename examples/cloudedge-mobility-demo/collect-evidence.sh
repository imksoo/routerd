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
STAGE=${CE_EVIDENCE_STAGE:-manual}
COLLECT_EVENTS=${CE_EVIDENCE_COLLECT_EVENTS:-1}
DUMP_EVENT_DB=${CE_EVIDENCE_DUMP_EVENT_DB:-1}
DRY_RUN=${CE_EVIDENCE_DRY_RUN:-0}
ROUTERD_STATE_DB=${CE_ROUTERD_STATE_DB:-/var/lib/routerd/routerd.db}

ssh_router() {
  local user=${2:-$SSH_USER}
  if [[ "$DRY_RUN" == "1" ]]; then
    printf '[dry-run] ssh %s@%s -- %s\n' "$user" "$1" "$3"
    return 0
  fi
  # shellcheck disable=SC2029
  ssh "${SSH_OPTS[@]}" "$user@$1" "$3"
}

collect_events_command() {
  cat <<EOF
echo "stage=$STAGE"
echo "captured_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "--- all-events"
sudo $ROUTERCTL_BIN get events --limit 2000 -o json || true
echo "--- routerd.mobility.holder.transition"
sudo $ROUTERCTL_BIN get events --topic routerd.mobility.holder.transition --limit 2000 -o json || true
EOF
}

dump_events_db_command() {
  cat <<EOF
echo "stage=$STAGE"
echo "state_db=$ROUTERD_STATE_DB"
if command -v sqlite3 >/dev/null 2>&1 && sudo test -r "$ROUTERD_STATE_DB"; then
  sudo sqlite3 -readonly -json "$ROUTERD_STATE_DB" "SELECT * FROM events ORDER BY id;"
else
  echo "events_db_dump_unavailable"
fi
EOF
}

event_retention_command() {
  cat <<EOF
echo "stage=$STAGE"
sudo $ROUTERCTL_BIN get status -o json 2>/dev/null | grep -Ei '"event|retention|maxAge|maxEvents"' || true
sudo $ROUTERCTL_BIN dynamic render -o json 2>/dev/null | grep -Ei '"EventGroup"|"retention"|"maxAge"|"maxEvents"' || true
EOF
}

collect_router() {
  local name=$1 host=$2 user=${3:-$SSH_USER}
  {
    echo "## $name"
    echo "stage=$STAGE"
    ssh_router "$host" "$user" "hostname; ip -br addr; ip route; sudo wg show || true; sudo nft list table inet routerd_mss || true"
    echo "--- doctor"
    ssh_router "$host" "$user" "sudo $ROUTERCTL_BIN doctor hybrid -o json || true"
    echo "--- dynamic"
    ssh_router "$host" "$user" "sudo $ROUTERCTL_BIN dynamic list -o json || true; sudo $ROUTERCTL_BIN dynamic render -o json || true"
    echo "--- actions"
    ssh_router "$host" "$user" "sudo $ROUTERCTL_BIN action list -o json || true"
    echo "--- mobility"
    ssh_router "$host" "$user" "sudo $ROUTERCTL_BIN mobility paths -o json || true; sudo $ROUTERCTL_BIN mobility traps -o json || true"
    if [[ "$COLLECT_EVENTS" == "1" ]]; then
      echo "--- events"
      ssh_router "$host" "$user" "$(collect_events_command)"
      echo "--- event-retention"
      ssh_router "$host" "$user" "$(event_retention_command)"
    fi
    if [[ "$DUMP_EVENT_DB" == "1" ]]; then
      echo "--- state-db-events"
      ssh_router "$host" "$user" "$(dump_events_db_command)"
    fi
  } > "$OUT/$name.txt" 2>&1
}

collect_router onprem "$ONPREM_ROUTER_SSH_HOST" "${ONPREM_SSH_USER:-$SSH_USER}"
collect_router aws-a "$AWS_ROUTER_A_SSH_HOST" "${AWS_ROUTER_A_SSH_USER:-$SSH_USER}"
collect_router aws-b "$AWS_ROUTER_B_SSH_HOST" "${AWS_ROUTER_B_SSH_USER:-$SSH_USER}"
collect_router azure "$AZURE_ROUTER_SSH_HOST" "${AZURE_ROUTER_SSH_USER:-$SSH_USER}"
collect_router oci "$OCI_ROUTER_SSH_HOST" "${OCI_ROUTER_SSH_USER:-$SSH_USER}"

if command -v aws >/dev/null && [[ -n "${AWS_ROUTER_A_ENI_REF:-}" && -n "${AWS_ROUTER_B_ENI_REF:-}" ]]; then
  aws ec2 describe-network-interfaces --profile "$AWS_PROFILE" --region "$AWS_REGION" \
    --network-interface-ids "$AWS_ROUTER_A_ENI_REF" "$AWS_ROUTER_B_ENI_REF" \
    > "$OUT/aws-network-interfaces.json" 2>&1 || true
fi

if command -v az >/dev/null && [[ -n "${AZURE_ROUTER_NIC_REF:-}" ]]; then
  az network nic show --ids "$AZURE_ROUTER_NIC_REF" > "$OUT/azure-router-nic.json" 2>&1 || true
fi

if command -v oci >/dev/null && [[ -n "${OCI_ROUTER_VNIC_REF:-}" ]]; then
  oci network vnic get --vnic-id "$OCI_ROUTER_VNIC_REF" --config-file "$OCI_CONFIG_FILE" --profile "$OCI_PROFILE" --auth security_token \
    > "$OUT/oci-router-vnic.json" 2>&1 || true
fi

cat > "$OUT/summary.md" <<EOF
# CloudEdge Mobility Demo Evidence

Collected: $TS
Stage: $STAGE

- Router evidence: onprem, aws-a, aws-b, azure, oci.
- Provider evidence: AWS ENIs, Azure NIC, OCI VNIC when matching CLIs are available.
- Inspect BGP mobility paths, provider trap action plans, and action journals to confirm D5 migration.
- Events: each router file includes all-topic routerctl get events, focused
  routerd.mobility.holder.transition events, and state DB events table dump
  when available.
- Event retention check: verify retained EventGroup/eventd settings cover the
  full run length before relying on timing-event absence.
EOF

echo "wrote $OUT"
