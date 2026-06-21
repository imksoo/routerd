#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  sam-e2e-generate.sh --tofu-output tofu-output.json --out-dir DIR [--event-secret-file FILE]

Generates routerd configs for all router nodes in the SAM E2E topology.
Router, leaf, and client identities are read from `nodes.value` in the
OpenTofu output; do not keep a second hardcoded topology list here.

The input must be `tofu output -json` from cloudedge-mobility/terraform/envs/sam-e2e.
WireGuard private keys and the event federation HMAC secret are generated under
OUT_DIR/secrets and are intentionally not checked into git.
USAGE
}

tofu_output=
out_dir=
event_secret_file=

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tofu-output) tofu_output="$2"; shift 2 ;;
    --out-dir) out_dir="$2"; shift 2 ;;
    --event-secret-file) event_secret_file="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$tofu_output" ] || { echo "--tofu-output is required" >&2; exit 2; }
[ -n "$out_dir" ] || { echo "--out-dir is required" >&2; exit 2; }
[ -f "$tofu_output" ] || { echo "tofu output not found: $tofu_output" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }
command -v wg >/dev/null || { echo "wg is required to generate WireGuard keys" >&2; exit 2; }

mkdir -p "$out_dir/configs" "$out_dir/secrets"
secret_path="${event_secret_file:-$out_dir/secrets/eventd-cloudedge.key}"
if [ ! -s "$secret_path" ]; then
  umask 077
  openssl rand -base64 32 >"$secret_path"
fi

nodes_json="$out_dir/nodes.json"
fabric_json="$out_dir/fabric.json"
jq '.nodes.value' "$tofu_output" >"$nodes_json"
jq '.fabric.value' "$tofu_output" >"$fabric_json"

mapfile -t routers < <(jq -r 'to_entries[] | select(.value.role == "rr" or .value.role == "leaf") | .key' "$nodes_json" | sort)
mapfile -t rr_nodes < <(jq -r 'to_entries[] | select(.value.role == "rr") | .key' "$nodes_json" | sort)
mapfile -t leaf_nodes < <(jq -r 'to_entries[] | select(.value.role == "leaf") | .key' "$nodes_json" | sort)
mapfile -t cloud_leaf_nodes < <(jq -r 'to_entries[] | select(.value.role == "leaf" and .value.site != "pve") | .key' "$nodes_json" | sort)
mapfile -t pve_leaf_nodes < <(jq -r 'to_entries[] | select(.value.role == "leaf" and .value.site == "pve") | .key' "$nodes_json" | sort)
mapfile -t clients < <(jq -r 'to_entries[] | select(.value.role == "client") | .key' "$nodes_json" | sort)

[ "${#routers[@]}" -gt 0 ] || { echo "no router nodes found in $nodes_json" >&2; exit 2; }
[ "${#leaf_nodes[@]}" -gt 0 ] || { echo "no leaf nodes found in $nodes_json" >&2; exit 2; }

for node in "${routers[@]}"; do
  key_file="$out_dir/secrets/${node}.wg.key"
  pub_file="$out_dir/secrets/${node}.wg.pub"
  if [ ! -s "$key_file" ]; then
    umask 077
    wg genkey >"$key_file"
  fi
  wg pubkey <"$key_file" >"$pub_file"
done

jq_node() {
  local node="$1"
  local expr="$2"
  jq -r --arg node "$node" "$expr" "$nodes_json"
}

fabric() {
  jq -r "$1" "$fabric_json"
}

site_provider() {
  case "$1" in
    aws|azure|oci) printf '%s\n' "$1" ;;
    *) return 1 ;;
  esac
}

site_interface() {
  case "$1" in
    aws) printf 'ens5\n' ;;
    azure) printf 'eth0\n' ;;
    oci) printf 'ens3\n' ;;
    *) return 1 ;;
  esac
}

mobility_prefix="$(fabric '.mobility_prefix')"
inner_prefix="$(fabric '.tunnel_inner_prefix')"
bgp_asn="$(fabric '.bgp_asn')"
wg_port="$(fabric '.wg_port')"
pve_capture_interface="${PVE_CAPTURE_INTERFACE:-eth1}"
capture_max_secondary_ips="${SAM_E2E_MAX_SECONDARY_IPS:-128}"

node_set_file="$out_dir/node-set.yaml"
{
  echo "    - apiVersion: mobility.routerd.net/v1alpha1"
  echo "      kind: SAMNodeSet"
  echo "      metadata: { name: cloudedge-nodes }"
  echo "      spec:"
  echo "        nodes:"
  for node in "${routers[@]}"; do
    site="$(jq_node "$node" '.[$node].site')"
    node_role="$(jq_node "$node" '.[$node].role')"
    role="cloud"
    [ "$site" = "pve" ] && role="onprem"
    overlay="$(jq_node "$node" '.[$node].overlay_ip')"
    public_ip="$(jq_node "$node" '.[$node].public_ip')"
    pub_key="$(cat "$out_dir/secrets/${node}.wg.pub")"
    rr=false
    [ "$node_role" = "rr" ] && rr=true
    echo "          - nodeRef: $node"
    echo "            site: $site"
    echo "            role: $role"
    echo "            routeReflector: $rr"
    echo "            eventEndpoint: http://$overlay:9443"
    echo "            samEndpoint: $overlay"
    echo "            wireGuard:"
    echo "              publicKey: $pub_key"
    echo "              endpoint: $public_ip:$wg_port"
    echo "              allowedIPs: [$overlay/32]"
    echo "              persistentKeepalive: 25"
  done
} >"$node_set_file"

render_common() {
  local node="$1"
  local router_id="$2"
  local private_key
  private_key="$(cat "$out_dir/secrets/${node}.wg.key")"
  cat "$node_set_file"
  cat <<EOF

    - apiVersion: federation.routerd.net/v1alpha1
      kind: EventGroup
      metadata: { name: cloudedge }
      spec:
        nodeName: $node
        retention: { maxEvents: 1000, maxAge: 24h }
        auth:
          mode: hmac
          secretFile: /usr/local/etc/routerd/secrets/eventd-cloudedge.key
        listen:
          address: $router_id
          port: 9443
        replayWindow: 5m
        peersFrom:
          - resource: SAMNodeSet/cloudedge-nodes

    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: { name: wg-hybrid }
      spec:
        selfNodeRef: $node
        privateKey: $private_key
        listenPort: $wg_port
        mtu: 1420
        peersFrom:
          - resource: SAMNodeSet/cloudedge-nodes

    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: { name: wg-hybrid }
      spec: { ifname: wg-hybrid, managed: false, mtu: 1420 }

    - apiVersion: net.routerd.net/v1alpha1
      kind: BGPRouter
      metadata: { name: mobility-bgp }
      spec:
        asn: $bgp_asn
        routerID: $router_id
        listen: { port: 179 }
        importPolicy: { allowedPrefixes: [$mobility_prefix, 10.99.0.0/24], nextHopRewrite: unchanged }
        timers: { profile: fast }
        convergenceProfile: fast

    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMTransportProfile
      metadata: { name: cloudedge-transport }
      spec:
        selfNodeRef: $node
        mode: ipip
        encryption: wireguard
        innerPrefix: $inner_prefix
        underlayInterface: wg-hybrid
        localEndpoint: $router_id
        addressingMode: pair-stable
        peersFrom:
          - resource: SAMNodeSet/cloudedge-nodes
        bgp:
          routerRef: BGPRouter/mobility-bgp
          peerASN: $bgp_asn
          timersPreset: fast
          importPolicy:
            allowedPrefixes: [$mobility_prefix]
            nextHopRewrite: peer-address
EOF
}

render_members() {
  local self_node="$1" self_profile="$2"
  for node in "${leaf_nodes[@]}"; do
    site="$(jq_node "$node" '.[$node].site')"
    echo "          - nodeRef: $node"
    echo "            site: $site"
    if [ "$site" = "pve" ]; then
      echo "            role: onprem"
    else
      echo "            role: cloud"
    fi
    if [ "$node" = "$self_node" ] && [ -n "$self_profile" ]; then
      echo "            profileRef: $self_profile"
    fi
    if [ "$site" != "pve" ]; then
      echo "            placement: { group: $site-leaf }"
      echo "            maxSecondaryIPs: $capture_max_secondary_ips"
    fi
  done
}

render_provider_leaf() {
  local node="$1" provider="$2" profile="$3" iface="$4"
  local overlay provider_mode target_values target_from provider_env executor_timeout inventory_timeout
  overlay="$(jq_node "$node" '.[$node].overlay_ip')"
  provider_mode="secondary-ip"
  target_values=""
  target_from=""
  provider_env=""
  executor_timeout="120s"
  inventory_timeout="60s"
  cat <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: $node
spec:
  resources:
EOF
  render_common "$node" "$overlay"
  case "$provider" in
    aws)
      region="$(fabric '.aws.region')"
      subnet="$(fabric '.aws.leaf_subnet_id')"
      target_values="          self.region: $region"
      target_from="                targetFrom:
                  region: self.region"
      provider_env="          AWS_DEFAULT_REGION: $region"
      echo
      cat <<EOF
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: CloudProviderProfile
      metadata: { name: aws-lab }
      spec:
        provider: aws
        region: "$region"
        capabilities: [secondary-ip, source-dest-check-disable]
        auth: { mode: external-command, command: /bin/true }
EOF
      ;;
    azure)
      region="$(fabric '.azure.location')"
      rg="$(fabric '.azure.resource_group_name')"
      subnet="$(fabric '.azure.subnet_id')"
      target_values="          self.region: $region
          self.resourceGroup: $rg"
      target_from="                targetFrom:
                  region: self.region
                  resourceGroup: self.resourceGroup"
      executor_timeout="180s"
      provider_env="          AZURE_CONFIG_DIR: /var/lib/routerd/azure
          ROUTERD_AZURE_EXECUTOR_COMMAND_TIMEOUT: 75s"
      echo
      cat <<EOF
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: CloudProviderProfile
      metadata: { name: azure-lab }
      spec:
        provider: azure
        resourceGroup: "$rg"
        region: "$region"
        capabilities: [secondary-ip, ip-forwarding]
        auth: { mode: external-command, command: /bin/true }
EOF
      ;;
    oci)
      region="$(fabric '.oci.region')"
      compartment="$(fabric '.oci.compartment_id')"
      subnet="$(fabric '.oci.subnet_id')"
      provider_mode="vnic-secondary-ip"
      target_values="          self.region: $region
          self.compartmentId: $compartment"
      target_from="                targetFrom:
                  region: self.region
                  compartmentId: self.compartmentId"
      provider_env="          OCI_REGION: $region
          OCI_AUTH_MODE: instance_principal"
      echo
      cat <<EOF
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: CloudProviderProfile
      metadata: { name: oci-lab }
      spec:
        provider: oci
        region: "$region"
        capabilities: [vnic-secondary-ip, skip-source-dest-check]
        auth: { mode: external-command, command: /bin/true }
EOF
      ;;
  esac
  cat <<EOF

    - apiVersion: mobility.routerd.net/v1alpha1
      kind: MobilityPool
      metadata: { name: cloudedge }
      spec:
        prefix: $mobility_prefix
        groupRef: cloudedge
        values:
$target_values
          self.subnetRef: $subnet
        profiles:
          cloudCaptures:
            $profile:
              capture:
                type: provider-secondary-ip
                interface: $iface
                providerRef: ${provider}-lab
                providerMode: $provider_mode
                captureStrategy: secondary-ip
                configureOSAddress: false
$target_from
              ownershipDiscovery:
                mode: provider-private-ip
                subnetRefFrom: self.subnetRef
                selector:
                  tags:
                    cloudedge-mobility: "true"
                scanInterval: 30s
                leaseTTL: 10m
        deliveryPolicy: { mode: bgp }
        capturePolicy: { mode: all-non-owner-sites }
        ipOwnershipPolicy: { type: centralized, autoFailover: true }
        members:
EOF
  render_members "$node" "$profile"
  cat <<EOF

    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: ProviderActionPolicy
      metadata: { name: ${provider}-live-mutation }
      spec:
        enabled: true
        dryRunOnly: false
        requireApproval: false
        allowedProviders: [$provider]
        allowedProviderRefs: [${provider}-lab]
        allowedActions: [assign-secondary-ip, unassign-secondary-ip, ensure-forwarding-enabled, ensure-forwarding-disabled]
        allowedCIDRs: [$mobility_prefix]
        maxActionsPerRun: 8
        allowUndo: true

    - apiVersion: plugin.routerd.net/v1alpha1
      kind: Plugin
      metadata: { name: ${provider}-executor }
      spec:
        executable: /usr/local/libexec/routerd/plugins/${provider}-provider-executor/bin/${provider}-provider-executor
        timeout: $executor_timeout
        env:
$provider_env
        capabilities: [execute.providerAction]

    - apiVersion: plugin.routerd.net/v1alpha1
      kind: Plugin
      metadata: { name: ${provider}-inventory }
      spec:
        executable: /usr/local/libexec/routerd/plugins/provider-private-ip-inventory
        timeout: $inventory_timeout
        env:
$provider_env
        capabilities: [observe.providerPrivateIPs]
EOF
}

render_rr() {
  local node="$1" overlay
  overlay="$(jq_node "$node" '.[$node].overlay_ip')"
  cat <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: $node
spec:
  resources:
EOF
  render_common "$node" "$overlay"
}

render_pve_leaf() {
  local node="$1" overlay
  overlay="$(jq_node "$node" '.[$node].overlay_ip')"
  cat <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: $node
spec:
  resources:
EOF
  render_common "$node" "$overlay"
  cat <<EOF

    - apiVersion: mobility.routerd.net/v1alpha1
      kind: MobilityPool
      metadata: { name: cloudedge }
      spec:
        prefix: $mobility_prefix
        groupRef: cloudedge
        deliveryPolicy: { mode: bgp }
        capturePolicy: { mode: all-non-owner-sites }
        ipOwnershipPolicy: { type: centralized, autoFailover: true }
        members:
EOF
  for member in "${leaf_nodes[@]}"; do
    site="$(jq_node "$member" '.[$node].site')"
    echo "          - nodeRef: $member"
    echo "            site: $site"
    if [ "$site" = "pve" ]; then
      echo "            role: onprem"
    else
      echo "            role: cloud"
      echo "            placement: { group: $site-leaf }"
      echo "            maxSecondaryIPs: $capture_max_secondary_ips"
    fi
    if [ "$member" = "$node" ]; then
      cat <<EOF
            ownershipDiscovery:
              mode: onprem-l2
              sources:
                - type: arp-observer
                  interface: $pve_capture_interface
            capture:
              type: proxy-arp
              interface: $pve_capture_interface
              gratuitousARP: true
              activeWhen: { type: single-router }
EOF
    fi
  done
}

for node in "${rr_nodes[@]}"; do
  render_rr "$node" >"$out_dir/configs/$node.yaml"
done

for node in "${cloud_leaf_nodes[@]}"; do
  site="$(jq_node "$node" '.[$node].site')"
  provider="$(site_provider "$site")"
  iface="$(site_interface "$site")"
  render_provider_leaf "$node" "$provider" "${node}-self" "$iface" >"$out_dir/configs/$node.yaml"
done

for node in "${pve_leaf_nodes[@]}"; do
  render_pve_leaf "$node" >"$out_dir/configs/$node.yaml"
done

{
  echo "node,config"
  for node in "${routers[@]}"; do
    echo "$node,$out_dir/configs/$node.yaml"
  done
} >"$out_dir/configs/manifest.csv"

echo "generated configs under $out_dir/configs"
echo "generated secrets under $out_dir/secrets"
