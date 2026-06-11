#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ENV_FILE=${ENV_FILE:-"$ROOT/env"}
set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

echo "Best-effort CloudEdge Mobility demo reset. No provisioning is performed."

if command -v aws >/dev/null; then
  for eni in "${AWS_ROUTER_A_ENI_REF:-}" "${AWS_ROUTER_B_ENI_REF:-}"; do
    [[ -n "$eni" ]] || continue
    aws ec2 unassign-private-ip-addresses --profile "$AWS_PROFILE" --region "$AWS_REGION" \
      --network-interface-id "$eni" \
      --private-ip-addresses 10.77.60.10 10.77.60.11 10.77.60.12 10.77.60.13 || true
    aws ec2 modify-network-interface-attribute --profile "$AWS_PROFILE" --region "$AWS_REGION" \
      --network-interface-id "$eni" --source-dest-check Value=true || true
  done
  aws_instances=()
  [[ -z "${AWS_ROUTER_A_INSTANCE_ID:-}" ]] || aws_instances+=("$AWS_ROUTER_A_INSTANCE_ID")
  [[ -z "${AWS_CLIENT_INSTANCE_ID:-}" ]] || aws_instances+=("$AWS_CLIENT_INSTANCE_ID")
  if [[ -n "${AWS_ROUTER_B_INSTANCE_ID:-}" ]]; then
    aws_instances+=("$AWS_ROUTER_B_INSTANCE_ID")
  fi
  if ((${#aws_instances[@]})); then
    aws ec2 stop-instances --profile "$AWS_PROFILE" --region "$AWS_REGION" \
      --instance-ids "${aws_instances[@]}" || true
  fi
fi

if command -v az >/dev/null; then
  if [[ -n "${AZURE_ROUTER_NIC_NAME:-}" ]]; then
    for name in ${AZURE_DEMO_IPCONFIG_NAMES:-}; do
      az network nic ip-config delete --resource-group "$AZURE_RESOURCE_GROUP" \
        --nic-name "$AZURE_ROUTER_NIC_NAME" --name "$name" || true
    done
  fi
  if [[ -n "${AZURE_ROUTER_NIC_REF:-}" ]]; then
    az network nic update --ids "$AZURE_ROUTER_NIC_REF" --ip-forwarding false || true
  fi
  az vm deallocate --resource-group "$AZURE_RESOURCE_GROUP" --name "$AZURE_ROUTER_VM_NAME" || true
  az vm deallocate --resource-group "$AZURE_RESOURCE_GROUP" --name "$AZURE_CLIENT_VM_NAME" || true
fi

if command -v oci >/dev/null; then
  for private_ip in ${OCI_DEMO_PRIVATE_IP_REFS:-}; do
    oci network private-ip delete --private-ip-id "$private_ip" --force \
      --config-file "$OCI_CONFIG_FILE" --profile "$OCI_PROFILE" --auth security_token || true
  done
  if [[ -n "${OCI_ROUTER_VNIC_REF:-}" ]]; then
    oci network vnic update --vnic-id "$OCI_ROUTER_VNIC_REF" --skip-source-dest-check false \
      --config-file "$OCI_CONFIG_FILE" --profile "$OCI_PROFILE" --auth security_token || true
  fi
  for instance_ref in ${OCI_ROUTER_INSTANCE_REF:-} ${OCI_ROUTER_B_INSTANCE_REF:-} ${OCI_CLIENT_INSTANCE_REF:-}; do
    oci compute instance action --instance-id "$instance_ref" --action STOP \
      --config-file "$OCI_CONFIG_FILE" --profile "$OCI_PROFILE" --auth security_token || true
  done
fi

echo "reset issued; verify provider consoles/CLI state before leaving the lab"
