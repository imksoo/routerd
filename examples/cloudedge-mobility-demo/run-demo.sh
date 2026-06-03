#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ENV_FILE=${ENV_FILE:-"$ROOT/env"}
WORKDIR=${WORKDIR:-"$ROOT/.rendered"}
INVENTORY_PLUGIN_SRC=${INVENTORY_PLUGIN_SRC:-"$ROOT/plugins/provider-private-ip-inventory"}
DISCOVERY_WAIT_SECONDS=${DISCOVERY_WAIT_SECONDS:-75}

if [[ ! -f "$ENV_FILE" ]]; then
  echo "missing env file: $ENV_FILE (copy env.example to env)" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

require() {
  command -v "$1" >/dev/null || { echo "missing command: $1" >&2; exit 1; }
}

require envsubst
require jq
require ssh
require scp

SSH_OPTS=(-i "$SSH_KEY_FILE" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10)

oci_cli() {
  oci --config-file "$OCI_CONFIG_FILE" --profile "$OCI_PROFILE" --region "$OCI_REGION" --auth security_token "$@" --output json
}

router_host() {
  case "$1" in
    onprem) echo "$ONPREM_ROUTER_SSH_HOST" ;;
    aws-a) echo "$AWS_ROUTER_A_SSH_HOST" ;;
    aws-b) echo "$AWS_ROUTER_B_SSH_HOST" ;;
    azure) echo "$AZURE_ROUTER_SSH_HOST" ;;
    azure-b) echo "$AZURE_ROUTER_B_SSH_HOST" ;;
    oci) echo "$OCI_ROUTER_SSH_HOST" ;;
    oci-b) echo "$OCI_ROUTER_B_SSH_HOST" ;;
    *) echo "unknown router $1" >&2; return 1 ;;
  esac
}

router_user() {
  case "$1" in
    onprem) echo "${ONPREM_SSH_USER:-$SSH_USER}" ;;
    aws-a) echo "${AWS_ROUTER_A_SSH_USER:-$SSH_USER}" ;;
    aws-b) echo "${AWS_ROUTER_B_SSH_USER:-$SSH_USER}" ;;
    azure) echo "${AZURE_ROUTER_SSH_USER:-$SSH_USER}" ;;
    azure-b) echo "${AZURE_ROUTER_B_SSH_USER:-$SSH_USER}" ;;
    oci) echo "${OCI_ROUTER_SSH_USER:-$SSH_USER}" ;;
    oci-b) echo "${OCI_ROUTER_B_SSH_USER:-$SSH_USER}" ;;
  esac
}

router_ssh() {
  local node=$1
  shift
  ssh "${SSH_OPTS[@]}" "$(router_user "$node")@$(router_host "$node")" "$@"
}

router_scp() {
  local src=$1 node=$2 dst=$3
  scp "${SSH_OPTS[@]}" "$src" "$(router_user "$node")@$(router_host "$node"):$dst" >/dev/null
}

render_cloud_config() {
  local template=$1 out=$2 router_name=$3 self_node=$4 self_ip=$5 same_site_peer_node=$6 same_site_peer_ip=$7 self_nic_ref=${8:-}
  DEMO_ROUTER_NAME=$router_name \
    DEMO_SELF_NODE=$self_node \
    DEMO_SELF_IP=$self_ip \
    DEMO_SAMESITE_PEER_NODE=$same_site_peer_node \
    DEMO_SAMESITE_PEER_IP=$same_site_peer_ip \
    DEMO_SELF_NIC_REF=$self_nic_ref \
    envsubst < "$template" > "$out"
}

render_configs() {
  mkdir -p "$WORKDIR"
  envsubst < "$ROOT/onprem.yaml" > "$WORKDIR/onprem.yaml"
  render_cloud_config "$ROOT/aws.yaml" "$WORKDIR/aws-a.yaml" \
    cloudedge-mobility-aws-router-a-demo aws-router-a 10.99.0.2 aws-router-b 10.99.0.5
  render_cloud_config "$ROOT/aws.yaml" "$WORKDIR/aws-b.yaml" \
    cloudedge-mobility-aws-router-b-demo aws-router-b 10.99.0.5 aws-router-a 10.99.0.2
  render_cloud_config "$ROOT/azure.yaml" "$WORKDIR/azure.yaml" \
    cloudedge-mobility-azure-demo azure-router 10.99.0.3 azure-router-b 10.99.0.6
  render_cloud_config "$ROOT/azure.yaml" "$WORKDIR/azure-b.yaml" \
    cloudedge-mobility-azure-b-demo azure-router-b 10.99.0.6 azure-router 10.99.0.3
  render_cloud_config "$ROOT/oci.yaml" "$WORKDIR/oci.yaml" \
    cloudedge-mobility-oci-demo oci-router 10.99.0.4 oci-router-b 10.99.0.7 "${OCI_ROUTER_VNIC_REF:-}"
  render_cloud_config "$ROOT/oci.yaml" "$WORKDIR/oci-b.yaml" \
    cloudedge-mobility-oci-b-demo oci-router-b 10.99.0.7 oci-router 10.99.0.4 "${OCI_ROUTER_B_VNIC_REF:-}"

  cp "$WORKDIR/aws-a.yaml" "$WORKDIR/aws-a-drain.yaml"
  cp "$WORKDIR/onprem.yaml" "$WORKDIR/onprem-drain.yaml"
  cp "$WORKDIR/aws-b.yaml" "$WORKDIR/aws-b-drain.yaml"

  perl -0pi -e 's/(placement: \{ group: aws-edge, priority: 10 \})/$1\n            maintenance: { drain: true }/' \
    "$WORKDIR/aws-a-drain.yaml" "$WORKDIR/aws-b-drain.yaml" "$WORKDIR/onprem-drain.yaml"
}

validate_rendered() {
  for cfg in "$WORKDIR"/*.yaml; do
    "$ROUTERD_BIN" validate --config "$cfg"
  done
}

install_secret_and_config() {
  local node=$1 cfg=$2
  router_scp "$cfg" "$node" /tmp/router.yaml
  router_scp "$INVENTORY_PLUGIN_SRC" "$node" /tmp/provider-private-ip-inventory
  router_ssh "$node" "set -euo pipefail
    sudo install -d -m 0700 \$(dirname '$EVENT_HMAC_SECRET_FILE')
    printf '%s\n' '$EVENT_HMAC_SECRET_VALUE' | sudo tee '$EVENT_HMAC_SECRET_FILE' >/dev/null
    sudo chmod 0600 '$EVENT_HMAC_SECRET_FILE'
    sudo install -d -m 0755 /usr/local/libexec/routerd/plugins
    sudo install -m 0755 /tmp/provider-private-ip-inventory /usr/local/libexec/routerd/plugins/provider-private-ip-inventory
    sudo install -m 0600 /tmp/router.yaml /usr/local/etc/routerd/router.yaml
    command -v python3 >/dev/null
    sudo systemctl restart routerd
    sudo systemctl restart routerd-eventd@cloudedge.service
    sudo systemctl is-active routerd
    sudo systemctl is-active routerd-eventd@cloudedge.service"
}

oci_client_vnic_ref() {
  if [[ -n "${OCI_CLIENT_VNIC_REF:-}" ]]; then
    echo "$OCI_CLIENT_VNIC_REF"
    return 0
  fi
  oci_cli compute vnic-attachment list \
    --compartment-id "$OCI_COMPARTMENT_REF" \
    --instance-id "$OCI_CLIENT_INSTANCE_REF" |
    jq -r '.data[] | select(."lifecycle-state" == "ATTACHED") | ."vnic-id"' |
    head -n1
}

preflight_oci_private_ip() {
  local vnic_ref
  vnic_ref=$(oci_client_vnic_ref)
  if [[ -z "$vnic_ref" || "$vnic_ref" == "null" ]]; then
    echo "OCI preflight failed: could not resolve OCI client VNIC" >&2
    return 1
  fi
  if ! oci_cli network private-ip list --vnic-id "$vnic_ref" |
    jq -e --arg ip "$OCI_CLIENT_IP" '.data[] | select(."ip-address" == $ip)' >/dev/null; then
    echo "OCI preflight failed: client VNIC $vnic_ref does not have private IP $OCI_CLIENT_IP" >&2
    return 1
  fi
}

preflight_oci_wireguard() {
  router_ssh oci "set -euo pipefail
    if ! sudo ss -lun | awk '{print \$5}' | grep -Eq '(^|:)51820$'; then
      echo 'OCI preflight failed: UDP/51820 listener missing' >&2
      exit 1
    fi
    latest=0
    for _ in \$(seq 1 12); do
      latest=\$(sudo wg show wg-hybrid latest-handshakes | awk -v key='$ONPREM_WG_PUBLIC_KEY' '\$1 == key {print \$2; found=1} END {if (!found) print 0}')
      now=\$(date +%s)
      if [ \"\$latest\" != 0 ] && [ \$((now - latest)) -le 180 ]; then
        exit 0
      fi
      sleep 5
    done
    echo \"OCI preflight failed: wg-hybrid has no recent onprem handshake (latest=\$latest)\" >&2
    exit 1"
}

preflight_oci_forwarding() {
  router_ssh oci "set -euo pipefail
    if [ \"\$(sysctl -n net.ipv4.ip_forward)\" != 1 ]; then
      echo 'OCI preflight failed: net.ipv4.ip_forward is not enabled' >&2
      exit 1
    fi
    if command -v iptables >/dev/null 2>&1; then
      sudo iptables -C FORWARD -i ens3 -o wg-hybrid -j ACCEPT 2>/dev/null ||
        sudo iptables -I FORWARD 1 -i ens3 -o wg-hybrid -j ACCEPT
      sudo iptables -C FORWARD -i wg-hybrid -o ens3 -j ACCEPT 2>/dev/null ||
        sudo iptables -I FORWARD 1 -i wg-hybrid -o ens3 -j ACCEPT
    else
      echo 'OCI preflight failed: iptables command unavailable; cannot assert ens3<->wg-hybrid FORWARD allow' >&2
      exit 1
    fi"
}

preflight_mesh() {
  echo "Preflight OCI mesh prerequisites"
  require oci
  preflight_oci_private_ip
  preflight_oci_wireguard
  preflight_oci_forwarding
}

execute_provider_actions() {
  local node=$1
  router_ssh "$node" "sudo $ROUTERCTL_BIN action import"
  local ids
  ids=$(router_ssh "$node" "sudo $ROUTERCTL_BIN action list --status pending -o json" | jq -r '.[].id')
  for id in $ids; do
    router_ssh "$node" "sudo $ROUTERCTL_BIN action approve $id --by cloudedge-demo && sudo $ROUTERCTL_BIN action execute $id --approved"
  done
}

client_jump() {
  case "$1" in
    onprem) echo "$(router_user onprem)@$(router_host onprem)" ;;
    aws) echo "$(router_user aws-a)@$(router_host aws-a)" ;;
    azure) echo "$(router_user azure)@$(router_host azure)" ;;
    oci) echo "$(router_user oci)@$(router_host oci)" ;;
    aws-b) echo "$(router_user aws-b)@$(router_host aws-b)" ;;
  esac
}

client_host() {
  case "$1" in
    onprem) echo "$ONPREM_CLIENT_SSH_HOST" ;;
    aws) echo "$AWS_CLIENT_SSH_HOST" ;;
    azure) echo "$AZURE_CLIENT_SSH_HOST" ;;
    oci) echo "$OCI_CLIENT_SSH_HOST" ;;
  esac
}

client_exec() {
  local site=$1
  shift
  ssh "${SSH_OPTS[@]}" -J "$(client_jump "$site")" "$CLIENT_SSH_USER@$(client_host "$site")" "$@"
}

client_mobility_ip() {
  case "$1" in
    onprem) echo "$ONPREM_CLIENT_IP" ;;
    aws) echo "$AWS_CLIENT_IP" ;;
    azure) echo "$AZURE_CLIENT_IP" ;;
    oci) echo "$OCI_CLIENT_IP" ;;
    *) echo "unknown client site $1" >&2; return 1 ;;
  esac
}

preflight_client_mobility_ip() {
  local site=$1 ip
  ip=$(client_mobility_ip "$site")
  echo "Preflight $site client mobility IP $ip"
  client_exec "$site" "set -euo pipefail
    dev=\$(ip -4 route show default | awk '{for (i=1;i<=NF;i++) if (\$i == \"dev\") {print \$(i+1); exit}}')
    if [ -z \"\$dev\" ]; then
      echo '$site client preflight failed: default-route interface not found' >&2
      exit 1
    fi
    sudo ip addr replace '$ip/32' dev \"\$dev\"
    if ! ip -4 addr show dev \"\$dev\" | grep -F '$ip/32' >/dev/null; then
      echo '$site client preflight failed: mobility IP $ip/32 is not present on' \"\$dev\" >&2
      exit 1
    fi
    if systemctl list-unit-files ssh.service >/dev/null 2>&1; then
      sudo systemctl is-active ssh >/dev/null
    elif systemctl list-unit-files sshd.service >/dev/null 2>&1; then
      sudo systemctl is-active sshd >/dev/null
    elif ! pgrep -x sshd >/dev/null 2>&1; then
      echo '$site client preflight failed: ssh/sshd service is not active' >&2
      exit 1
    fi
    if ! ss -ltn | awk 'NR > 1 {print \$4}' | grep -Eq '(^|:)22$'; then
      echo '$site client preflight failed: TCP/22 listener missing' >&2
      exit 1
    fi"
}

preflight_cloud_client_mobility_ips() {
  preflight_client_mobility_ip aws
  preflight_client_mobility_ip azure
  preflight_client_mobility_ip oci
}

run_d3_matrix() {
  local sites=(onprem aws azure oci)
  local ips=("$ONPREM_CLIENT_IP" "$AWS_CLIENT_IP" "$AZURE_CLIENT_IP" "$OCI_CLIENT_IP")
  for i in "${!sites[@]}"; do
    for j in "${!sites[@]}"; do
      [[ "$i" == "$j" ]] && continue
      local src=${sites[$i]} src_ip dst_ip=${ips[$j]}
      src_ip=$(client_mobility_ip "$src")
      echo "D3 $src -> $dst_ip ping"
      client_exec "$src" "ping -I $src_ip -c3 -W2 $dst_ip"
      echo "D3 $src -> $dst_ip ssh source"
      client_exec "$src" "ssh -b $src_ip -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=8 $CLIENT_SSH_USER@$dst_ip 'printenv SSH_CONNECTION; ip route show default'"
    done
  done
}

probe_stale_gate_on_aws_b() {
  router_ssh aws-b "set -euo pipefail
    aws_b_nic=\$(sudo sqlite3 /var/lib/routerd/routerd.db \"select json_extract(status,'$.discoverySelfNICRef') from objects where api_version='mobility.routerd.net/v1alpha1' and kind='MobilityPool' and name='cloudedge';\")
    if [ -z \"\$aws_b_nic\" ]; then
      echo 'stale probe skipped: aws-router-b discoverySelfNICRef is not resolved yet' >&2
      exit 0
    fi
    now=\$(date -u +%Y-%m-%dT%H:%M:%SZ)
    sudo sqlite3 /var/lib/routerd/routerd.db \"insert into action_executions(idempotency_key,source,provider,provider_ref,action,target_json,parameters_json,undo_json,risk_level,status,created_at,updated_at) values('cloudedge-demo-stale-probe-pathsig1','stale-gate-probe','aws','aws-lab','assign-secondary-ip',json_object('provider','aws','providerRef','aws-lab','region','$AWS_REGION','nicRef','\$aws_b_nic','address','10.77.60.10/32'),json_object('mobilityPathSig','prefix=10.77.60.10/32;nextHops=stale','mobilityCaptureHolder','aws-router-a'),'{}','medium','pending','\$now','\$now') on conflict(idempotency_key) do nothing;\"
    sudo $ROUTERCTL_BIN action import
    sudo $ROUTERCTL_BIN action list -o json | jq -r '.[] | select(.idempotencyKey==\"cloudedge-demo-stale-probe-pathsig1\") | [.status,.resultMessage] | @tsv'"
}

main() {
  if [[ ! -x "$INVENTORY_PLUGIN_SRC" ]]; then
    echo "missing executable provider inventory plugin: $INVENTORY_PLUGIN_SRC" >&2
    exit 1
  fi

  render_configs
  validate_rendered

  echo "Deploy initial D3/D5 baseline configs"
  install_secret_and_config onprem "$WORKDIR/onprem.yaml"
  install_secret_and_config aws-a "$WORKDIR/aws-a.yaml"
  install_secret_and_config aws-b "$WORKDIR/aws-b.yaml"
  install_secret_and_config azure "$WORKDIR/azure.yaml"
  install_secret_and_config azure-b "$WORKDIR/azure-b.yaml"
  install_secret_and_config oci "$WORKDIR/oci.yaml"
  install_secret_and_config oci-b "$WORKDIR/oci-b.yaml"
  preflight_mesh
  preflight_cloud_client_mobility_ips

  echo "Wait for D3 cloud ownership discovery"
  sleep "$DISCOVERY_WAIT_SECONDS"

  echo "Execute provider actions for D3"
  execute_provider_actions aws-a
  execute_provider_actions azure
  execute_provider_actions oci

  echo "Run D3 12-directed connectivity matrix"
  run_d3_matrix

  echo "Apply D5 drain for aws-router-a"
  install_secret_and_config onprem "$WORKDIR/onprem-drain.yaml"
  install_secret_and_config aws-a "$WORKDIR/aws-a-drain.yaml"
  install_secret_and_config aws-b "$WORKDIR/aws-b-drain.yaml"
  echo "Wait for D5 cloud ownership discovery after drain"
  sleep "$DISCOVERY_WAIT_SECONDS"

  echo "Execute D5 migration actions"
  execute_provider_actions aws-a
  execute_provider_actions aws-b
  echo "Probe stale pathSig gate on aws-router-b"
  probe_stale_gate_on_aws_b

  echo "Verify D5 dataplane via aws-router-b"
  ssh "${SSH_OPTS[@]}" -J "$(client_jump aws-b)" "$CLIENT_SSH_USER@$AWS_CLIENT_SSH_HOST" "ping -c3 -W2 $ONPREM_CLIENT_IP"
  ssh "${SSH_OPTS[@]}" -J "$(client_jump aws-b),$CLIENT_SSH_USER@$AWS_CLIENT_SSH_HOST" "$CLIENT_SSH_USER@$ONPREM_CLIENT_IP" "printenv SSH_CONNECTION; ip route show default"

  echo "CloudEdge Mobility demo PASS"
}

main "$@"
