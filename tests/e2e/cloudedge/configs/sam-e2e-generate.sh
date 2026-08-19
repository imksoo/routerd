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

PVE overrides for mixed-OS qualification:
  PVE_MANAGEMENT_INTERFACE=vtnet0
  PVE_CAPTURE_INTERFACE=vtnet1
  PVE_OWNERSHIP_GATE=carp
  PVE_CARP_ADDRESS=10.77.60.254/32

The defaults preserve the Ubuntu PVE template fixture (host-provided ens18
management, ens19 capture, and the single-router ownership gate). CARP mode assigns priority 151
to PVE_CARP_PRIMARY_NODE (pve-leaf-a by default) and 100 to the other PVE
leaves. The shared SAMNodeSet deliberately omits all PVE WireGuard endpoints.
Each PVE router adds same-site static peer overrides that use only the peer
guest's management IPv4 address; cloud configs never receive those addresses.
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
harness_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# The release supervisor intentionally makes its checkout root-owned. Git's
# safe.directory exemption must name that worktree root, not this harness
# subdirectory, while the commands themselves still operate on the harness.
repo_root="$(cd "$harness_root/../../.." && pwd -P)"
harness_git() {
  git -c "safe.directory=$repo_root" -C "$harness_root" "$@"
}
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
pve_capture_interface="${PVE_CAPTURE_INTERFACE:-ens19}"
pve_management_interface="${PVE_MANAGEMENT_INTERFACE:-ens18}"
pve_ownership_gate="${PVE_OWNERSHIP_GATE:-single-router}"
pve_carp_address="${PVE_CARP_ADDRESS:-}"
pve_carp_primary_node="${PVE_CARP_PRIMARY_NODE:-pve-leaf-a}"
pve_carp_vrid="${PVE_CARP_VRID:-77}"
pve_carp_authentication="${PVE_CARP_AUTHENTICATION:-routerd-sam-e2e}"
capture_max_secondary_ips="${SAM_E2E_MAX_SECONDARY_IPS:-128}"
run1_verification_pool_nodes="${SAM_E2E_RUN1_VERIFICATION_POOL_NODES:-pve-leaf-a}"

case "$pve_ownership_gate" in
  single-router) ;;
  carp)
    [ -n "$pve_carp_address" ] || {
      echo "PVE_CARP_ADDRESS is required when PVE_OWNERSHIP_GATE=carp" >&2
      exit 2
    }
    ;;
  *) echo "PVE_OWNERSHIP_GATE must be single-router or carp" >&2; exit 2 ;;
esac

shared_wireguard_endpoint() {
  local node="$1" site="$2" public_ip

  # PVE guest management addresses are local bootstrap inputs, not public
  # topology data. The shared NodeSet must remain endpoint-less for every PVE
  # router so cloud configs cannot accidentally learn an on-prem address.
  if [ "$site" = "pve" ]; then
    return 0
  fi

  public_ip="$(jq_node "$node" ".[\$node].public_ip // empty")"
  if [ -n "$public_ip" ] && [ "$public_ip" != "null" ]; then
    printf '%s:%s\n' "$public_ip" "$wg_port"
  fi
}

pve_management_ip() {
  local node="$1" management_ip management_source octet first_octet
  # The PVE certification path records the QGA-discovered guest address as
  # management_ip. This helper is never called for cloud nodes and does not
  # accept the generic public_ip field as a second source of topology truth.
  management_ip="$(jq_node "$node" ".[\$node].management_ip // empty")"
  management_source="$(jq_node "$node" ".[\$node].pve_management_source // empty")"
  if [ "$management_source" != "qga-dhcp" ]; then
    echo "PVE router $node requires pve_management_source=qga-dhcp before local WireGuard bootstrap" >&2
    return 1
  fi
  if [ -z "$management_ip" ] || [ "$management_ip" = "null" ]; then
    echo "PVE router $node requires a QGA-discovered management_ip for local WireGuard bootstrap" >&2
    return 1
  fi
  if [[ ! "$management_ip" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then
    echo "PVE router $node management_ip must be an IPv4 address, got: $management_ip" >&2
    return 1
  fi
  IFS=. read -r -a octets <<<"$management_ip"
  for octet in "${octets[@]}"; do
    if { [ "${#octet}" -gt 1 ] && [ "${octet:0:1}" = 0 ]; } || [ "$((10#$octet))" -gt 255 ]; then
      echo "PVE router $node management_ip must be an IPv4 address, got: $management_ip" >&2
      return 1
    fi
  done
  first_octet="$((10#${octets[0]}))"
  if [ "$first_octet" -eq 0 ] || [ "$first_octet" -eq 127 ] || [ "$first_octet" -ge 224 ] || [[ "$management_ip" == 169.254.* ]]; then
    echo "PVE router $node management_ip must be a unicast IPv4 address, got: $management_ip" >&2
    return 1
  fi
  printf '%s\n' "$management_ip"
}

run1_pool_role() {
  local node="$1"
  case " $run1_verification_pool_nodes " in
    *" $node "*) echo "verification" ;;
    *) echo "control" ;;
  esac
}

node_set_file="$out_dir/node-set.yaml"
{
  echo "    - apiVersion: mobility.routerd.net/v1alpha1"
  echo "      kind: SAMNodeSet"
  echo "      metadata: { name: cloudedge-nodes }"
  echo "      spec:"
  echo "        nodes:"
  for node in "${routers[@]}"; do
    site="$(jq_node "$node" ".[\$node].site")"
    role="cloud"
    [ "$site" = "pve" ] && role="onprem"
    overlay="$(jq_node "$node" ".[\$node].overlay_ip")"
    endpoint="$(shared_wireguard_endpoint "$node" "$site")"
    pub_key="$(cat "$out_dir/secrets/${node}.wg.pub")"
    rr=false
    node_role="$(jq_node "$node" ".[\$node].role")"
    [ "$node_role" = "rr" ] && rr=true
    echo "          - nodeRef: $node"
    echo "            site: $site"
    echo "            role: $role"
    echo "            routeReflector: $rr"
    echo "            eventEndpoint: http://$overlay:9443"
    echo "            samEndpoint: $overlay"
    if [ "$node_role" = "leaf" ] && [ "$site" != "pve" ]; then
      echo "            placement: { group: $site-leaf }"
      echo "            maxSecondaryIPs: $capture_max_secondary_ips"
    fi
    echo "            wireGuard:"
    echo "              publicKey: $pub_key"
    if [ -n "$endpoint" ]; then
      echo "              endpoint: $endpoint"
    fi
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

render_pve_local_wireguard_peers() {
  local node="$1" node_site peer peer_site peer_management peer_overlay peer_public_key

  # Static resources win over peersFrom with the same name. They seed only
  # PVE-to-PVE sessions; PVE-to-cloud still uses cloud public endpoints from
  # the shared NodeSet, while cloud learns PVE endpoints from inbound traffic.
  # Keep the boundary explicit so a future non-PVE RR cannot accidentally
  # render an on-prem management address into a cloud configuration.
  node_site="$(jq_node "$node" ".[\$node].site")"
  [ "$node_site" = "pve" ] || return 0
  for peer in "${routers[@]}"; do
    [ "$peer" != "$node" ] || continue
    peer_site="$(jq_node "$peer" ".[\$node].site")"
    [ "$peer_site" = "pve" ] || continue
    peer_management="$(pve_management_ip "$peer")"
    peer_overlay="$(jq_node "$peer" ".[\$node].overlay_ip")"
    peer_public_key="$(cat "$out_dir/secrets/${peer}.wg.pub")"
    cat <<EOF

    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardPeer
      metadata: { name: $peer }
      spec:
        interface: wg-hybrid
        publicKey: $peer_public_key
        allowedIPs: [$peer_overlay/32]
        endpoint: $peer_management:$wg_port
        persistentKeepalive: 25
EOF
  done
}

render_provider_leaf() {
  local node="$1" provider="$2" profile="$3" iface="$4"
  local overlay target_values target_from provider_env executor_timeout inventory_timeout
  local pool_role
  overlay="$(jq_node "$node" ".[\$node].overlay_ip")"
  pool_role="$(run1_pool_role "$node")"
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
        capabilities: [secondary-ip, ip-forwarding]
        auth: { mode: external-command, command: /bin/true }
EOF
      ;;
    oci)
      region="$(fabric '.oci.region')"
      compartment="$(fabric '.oci.compartment_id')"
      subnet="$(fabric '.oci.subnet_id')"
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
        capabilities: [vnic-secondary-ip, skip-source-dest-check]
        auth: { mode: external-command, command: /bin/true }
EOF
      ;;
  esac
  cat <<EOF

    - apiVersion: mobility.routerd.net/v1alpha1
      kind: MobilityPool
      metadata:
        name: cloudedge
        annotations:
          routerd-labs/run1-pool-role: "$pool_role"
          routerd-labs/run1-pool-note: "control keeps gratuitousARPOnSeize default-off"
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
$target_from
              ownershipDiscovery:
                mode: provider-private-ip
                subnetRefFrom: self.subnetRef
                selector:
                  tags:
                    cloudedge-mobility: "true"
                scanInterval: 30s
                leaseTTL: 10m
        membersFrom:
          - resource: SAMNodeSet/cloudedge-nodes
        members:
          - nodeRef: $node
            profileRef: $profile

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
  overlay="$(jq_node "$node" ".[\$node].overlay_ip")"
  cat <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: $node
spec:
  resources:
EOF
  render_common "$node" "$overlay"
  render_pve_local_wireguard_peers "$node"
}

render_pve_leaf() {
  local node="$1" overlay dataplane_ip
  local pool_role carp_priority
  overlay="$(jq_node "$node" ".[\$node].overlay_ip")"
  dataplane_ip="$(jq_node "$node" ".[\$node].private_ip")"
  pool_role="$(run1_pool_role "$node")"
  carp_priority=100
  [ "$node" = "$pve_carp_primary_node" ] && carp_priority=151
  cat <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: $node
spec:
  reconcile:
    mode: progressive
    protectedInterfaces:
      - mgmt
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: { name: mgmt }
      spec:
        ifname: $pve_management_interface
        adminUp: true
        managed: false
        owner: external
EOF
  cat <<EOF
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: { name: capture }
      spec:
        ifname: $pve_capture_interface
        adminUp: true
        managed: false
        mtu: 1500

EOF
  render_common "$node" "$overlay"
  render_pve_local_wireguard_peers "$node"
  if [ "$pve_ownership_gate" = carp ]; then
    cat <<EOF

    - apiVersion: net.routerd.net/v1alpha1
      kind: VirtualAddress
      metadata: { name: cloudedge-carp }
      spec:
        family: ipv4
        interface: capture
        address: $pve_carp_address
        mode: vrrp
        vrrp:
          virtualRouterID: $pve_carp_vrid
          priority: $carp_priority
          authentication: $pve_carp_authentication
EOF
  fi
  cat <<EOF

    - apiVersion: mobility.routerd.net/v1alpha1
      kind: MobilityPool
      metadata:
        name: cloudedge
        annotations:
          routerd-labs/run1-pool-role: "$pool_role"
          routerd-labs/run1-pool-note: "verification enables seize-complete GARP"
      spec:
        prefix: $mobility_prefix
        groupRef: cloudedge
        membersFrom:
          - resource: SAMNodeSet/cloudedge-nodes
        members:
          - nodeRef: $node
            ownershipDiscovery:
              mode: onprem-l2
              sources:
                - type: arp-observer
                  interface: $pve_capture_interface
                - type: on-demand-arp
                  interface: $pve_capture_interface
                  probeTimeout: 500ms
                  probeRetries: 2
                  scanInterval: 1s
            capture:
              type: proxy-arp
              interface: $pve_capture_interface
              sourceAddress: $dataplane_ip
EOF
  if [ "$pve_ownership_gate" = carp ]; then
    cat <<EOF
              activeWhen: { type: vrrp-master, virtualAddressRef: cloudedge-carp }
EOF
  else
    cat <<EOF
              activeWhen: { type: single-router }
EOF
  fi
}

for node in "${rr_nodes[@]}"; do
  render_rr "$node" >"$out_dir/configs/$node.yaml"
done

for node in "${cloud_leaf_nodes[@]}"; do
  site="$(jq_node "$node" ".[\$node].site")"
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

{
  echo "node,pool,run1Role"
  for node in "${routers[@]}"; do
    role="$(run1_pool_role "$node")"
    echo "$node,cloudedge,$role"
  done
} >"$out_dir/configs/run1-pool-overlays.csv"

{
  echo "harness_root=$harness_root"
  if harness_git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "harness_git_head=$(harness_git rev-parse --short HEAD)"
    if harness_git diff --quiet -- configs/sam-e2e-generate.sh scripts/sam-e2e.sh &&
       harness_git diff --cached --quiet -- configs/sam-e2e-generate.sh scripts/sam-e2e.sh; then
      echo "harness_git_dirty=0"
    else
      echo "harness_git_dirty=1"
    fi
  else
    echo "harness_git_head=unmanaged"
    echo "harness_git_dirty=unknown"
  fi
} >"$out_dir/configs/harness-version.txt"

echo "generated configs under $out_dir/configs"
echo "generated secrets under $out_dir/secrets"
