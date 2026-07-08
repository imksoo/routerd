#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/sam-e2e.sh --tofu-output tofu-output.json --artifact routerd.tar.gz --evidence-dir DIR [options]

Options:
  --tfvars FILE           Optional OpenTofu tfvars file; used for provider CLI profiles during inventory
  --ssh-key FILE          Fixed lab SSH key (default: ~/.ssh/routerd-cloudedge-lab-20260529)
  --configs-dir DIR       Use existing generated configs instead of generating into evidence/config-gen
  --skip-deploy           Do not install routerd/configs; useful for diagnostics-only reruns
  --failover-node NODE    Optional router node name; may be repeated. Stops routerd.service and reruns convergence/matrix
  --rejoin-after-failover Restart stopped failover nodes and rerun convergence/matrix
  --load-balance-report   Capture MobilityPool owner-table snapshots after each matrix run
  --skip-matrix           Skip SSH hostname matrix; useful when rerunning performance after a clean matrix
  --skip-legacy-protocols Skip FTP/RPC/NFS/CIFS pseudo-client matrix
  --performance-tests     Run SAM iperf3/ping probes, plus public direct comparison for cross-cloud AWS/Azure/OCI pairs
  --failover-transfer-tests Run a required throttled client-to-client HTTP transfer during each failover stop
  --failover-transfer-observe Run the failover transfer probe but do not fail the scenario when it stalls
  --failover-transfer-smoke Run a throttled client-to-client HTTP transfer without stopping routers
  --success-evidence-minimal Skip expensive diagnostics/provider snapshots after successful checks
  --destroy-cmd CMD       Optional teardown command, for example: 'tofu destroy -auto-approve'

This harness consumes `tofu output -json` from the SAM E2E OpenTofu environment.
Pseudo-client to pseudo-client SSH hostname verification is the PASS authority.
USAGE
}

tofu_output=
artifact=
evidence_dir=
tfvars=
ssh_key="${HOME}/.ssh/routerd-cloudedge-lab-20260529"
configs_dir=
skip_deploy=0
failover_nodes=()
stopped_routers=()
rejoin_after_failover=0
load_balance_report=0
skip_matrix=0
legacy_protocols=1
performance_tests=0
failover_transfer_tests=0
failover_transfer_required=0
failover_transfer_smoke=0
success_evidence_minimal=0
destroy_cmd=
overall=0
validation_started=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tofu-output) tofu_output="$2"; shift 2 ;;
    --artifact) artifact="$2"; shift 2 ;;
    --evidence-dir) evidence_dir="$2"; shift 2 ;;
    --tfvars) tfvars="$2"; shift 2 ;;
    --ssh-key) ssh_key="$2"; shift 2 ;;
    --configs-dir) configs_dir="$2"; shift 2 ;;
    --skip-deploy) skip_deploy=1; shift ;;
    --failover-node) failover_nodes+=("$2"); shift 2 ;;
    --rejoin-after-failover) rejoin_after_failover=1; shift ;;
    --load-balance-report) load_balance_report=1; shift ;;
    --skip-matrix) skip_matrix=1; shift ;;
    --skip-legacy-protocols) legacy_protocols=0; shift ;;
    --performance-tests) performance_tests=1; shift ;;
    --failover-transfer-tests) failover_transfer_tests=1; failover_transfer_required=1; shift ;;
    --failover-transfer-observe) failover_transfer_tests=1; failover_transfer_required=0; shift ;;
    --failover-transfer-smoke) failover_transfer_tests=1; failover_transfer_required=1; failover_transfer_smoke=1; shift ;;
    --success-evidence-minimal) success_evidence_minimal=1; shift ;;
    --destroy-cmd) destroy_cmd="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$tofu_output" ] || { echo "--tofu-output is required" >&2; exit 2; }
[ -n "$artifact" ] || { echo "--artifact is required" >&2; exit 2; }
[ -n "$evidence_dir" ] || { echo "--evidence-dir is required" >&2; exit 2; }
[ -f "$tofu_output" ] || { echo "tofu output not found: $tofu_output" >&2; exit 2; }
[ -f "$artifact" ] || { echo "artifact not found: $artifact" >&2; exit 2; }
[ -z "$tfvars" ] || [ -f "$tfvars" ] || { echo "tfvars not found: $tfvars" >&2; exit 2; }
[ -f "$ssh_key" ] || { echo "ssh key not found: $ssh_key" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
config_generator="$(cd "$script_dir/.." && pwd)/configs/sam-e2e-generate.sh"
[ -x "$config_generator" ] || { echo "config generator not found or not executable: $config_generator" >&2; exit 2; }

mkdir -p "$evidence_dir"/{preflight,deploy,convergence,matrix,legacy,performance,failover-transfer,provider,diagnostics,cleanup,ssh}
cp "$tofu_output" "$evidence_dir/tofu-output.json"
timing_file="$evidence_dir/timing.tsv"
printf 'label\tphase\telapsed_seconds\n' >"$timing_file"
nodes_json="$evidence_dir/nodes.json"
fabric_json="$evidence_dir/fabric.json"
jq '.nodes.value' "$tofu_output" >"$nodes_json"
jq '.fabric.value' "$tofu_output" >"$fabric_json"

mapfile -t routers < <(jq -r 'to_entries[] | select(.value.role == "rr" or .value.role == "leaf") | .key' "$nodes_json" | sort)
mapfile -t leaf_routers < <(jq -r 'to_entries[] | select(.value.role == "leaf") | .key' "$nodes_json" | sort)
mapfile -t pve_leaf_routers < <(jq -r 'to_entries[] | select(.value.site == "pve" and .value.role == "leaf") | .key' "$nodes_json" | sort)
mapfile -t clients < <(jq -r 'to_entries[] | select(.value.role == "client") | .key' "$nodes_json" | sort)
mapfile -t pve_dataplane_nodes < <(jq -r 'to_entries[] | select(.value.site == "pve" and (.value.role == "leaf" or .value.role == "client")) | .key' "$nodes_json" | sort)
pve_boot_source="$(jq -r '.pve.boot_source // "template"' "$fabric_json")"
pve_capture_interface="${PVE_CAPTURE_INTERFACE:-$([ "$pve_boot_source" = "iso" ] && printf ens19 || printf eth1)}"
aws_profile="${AWS_PROFILE:-}"
oci_profile="${OCI_PROFILE:-}"

[ "${#routers[@]}" -gt 0 ] || { echo "no router nodes found in $nodes_json" >&2; exit 2; }
[ "${#leaf_routers[@]}" -gt 0 ] || { echo "no leaf router nodes found in $nodes_json" >&2; exit 2; }
[ "${#clients[@]}" -gt 1 ] || { echo "at least two client nodes are required in $nodes_json" >&2; exit 2; }

known_hosts="$evidence_dir/ssh/known_hosts"
: >"$known_hosts"

extract_tfvars_string() {
  local key="$1"
  [ -n "$tfvars" ] || return 0
  awk -v key="$key" '
    $0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
      sub(/#.*/, "")
      sub("^[^=]*=", "")
      gsub(/^[[:space:]]+|[[:space:]]+$/, "")
      if ($0 ~ /^".*"$/) {
        sub(/^"/, "")
        sub(/"$/, "")
      }
      print
      exit
    }
  ' "$tfvars"
}

if [ -n "$tfvars" ]; then
  aws_profile="${aws_profile:-$(extract_tfvars_string aws_profile)}"
  oci_profile="${oci_profile:-$(extract_tfvars_string oci_profile)}"
fi

{
  echo "tfvars=${tfvars:-}"
  echo "aws_profile=${aws_profile:-}"
  echo "oci_profile=${oci_profile:-}"
} >"$evidence_dir/provider/profiles.txt"

aws_cli() {
  if [ -n "$aws_profile" ]; then
    AWS_PROFILE="$aws_profile" aws "$@"
  else
    aws "$@"
  fi
}

oci_cli() {
  if [ -n "$oci_profile" ]; then
    OCI_PROFILE="$oci_profile" oci "$@"
  else
    oci "$@"
  fi
}

node_field() {
  local node="$1" field="$2"
  jq -r --arg node "$node" --arg field "$field" '.[$node][$field]' "$nodes_json"
}

node_ssh_host() {
  local node="$1" public_ip private_ip
  public_ip="$(node_field "$node" public_ip)"
  if [ -n "$public_ip" ] && [ "$public_ip" != "null" ]; then
    printf '%s\n' "$public_ip"
    return 0
  fi
  private_ip="$(node_field "$node" private_ip)"
  if [ -n "$private_ip" ] && [ "$private_ip" != "null" ]; then
    printf '%s\n' "$private_ip"
    return 0
  fi
  return 1
}

node_qga_eligible() {
  local node="$1" site role public_ip vm_id
  site="$(node_field "$node" site)"
  role="$(node_field "$node" role)"
  public_ip="$(node_field "$node" public_ip)"
  vm_id="$(node_field "$node" vm_id)"
  [ "$site" = "pve" ] || return 1
  [ "$role" = "client" ] || return 1
  [ -n "$vm_id" ] && [ "$vm_id" != "null" ] || return 1
  [ -z "$public_ip" ] || [ "$public_ip" = "null" ]
}

pve_qga_exec() {
  local node="$1" command="$2" vm_id pve_host raw exitcode
  vm_id="$(node_field "$node" vm_id)"
  pve_host="$(jq -r '.pve.node_ssh_host // .pve.node_name' "$fabric_json")"
  raw="$(ssh "root@$pve_host" "qm guest exec $vm_id --timeout 600 -- /bin/sh -lc $(printf '%q' "$command")")"
  printf '%s\n' "$raw" | jq -r '."out-data" // empty'
  printf '%s\n' "$raw" | jq -r '."err-data" // empty' >&2
  exitcode="$(printf '%s\n' "$raw" | jq -r '.exitcode // 255')"
  [ "$exitcode" = "0" ]
}

pve_qga_copy() {
  local src="$1" node="$2" dst="$3" vm_id pve_host raw exitcode quoted_dst
  vm_id="$(node_field "$node" vm_id)"
  pve_host="$(jq -r '.pve.node_ssh_host // .pve.node_name' "$fabric_json")"
  quoted_dst="$(printf '%q' "$dst")"
  raw="$(ssh "root@$pve_host" "qm guest exec $vm_id --pass-stdin 1 --timeout 120 -- /bin/sh -lc 'cat > $quoted_dst'" <"$src")"
  printf '%s\n' "$raw" | jq -r '."out-data" // empty'
  printf '%s\n' "$raw" | jq -r '."err-data" // empty' >&2
  exitcode="$(printf '%s\n' "$raw" | jq -r '.exitcode // 255')"
  [ "$exitcode" = "0" ]
}

node_is_stopped() {
  local want="$1" node
  for node in "${stopped_routers[@]}"; do
    [ "$node" = "$want" ] && return 0
  done
  return 1
}

record_timing() {
  local label="$1" phase="$2" started="$3"
  printf '%s\t%s\t%s\n' "$label" "$phase" "$((SECONDS - started))" >>"$timing_file"
}

record_skipped_success_evidence() {
  local kind="$1" label="$2" dir
  dir="$evidence_dir/$kind/$label"
  mkdir -p "$dir"
  {
    echo "skipped=success-evidence-minimal"
    echo "label=$label"
    date -u '+timestamp=%Y-%m-%dT%H:%M:%SZ'
  } >"$dir/SKIPPED.txt"
}

mark_node_running() {
  local want="$1" node
  local next=()
  for node in "${stopped_routers[@]}"; do
    [ "$node" = "$want" ] || next+=("$node")
  done
  stopped_routers=("${next[@]}")
}

ssh_base=(-i "$ssh_key" -o UserKnownHostsFile="$known_hosts" -o StrictHostKeyChecking=yes -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=10 -o ServerAliveCountMax=3)

ssh_node() {
  local node="$1"; shift
  local user host
  if node_qga_eligible "$node"; then
    pve_qga_exec "$node" "$*"
    return
  fi
  user="$(node_field "$node" ssh_user)"
  host="$(node_ssh_host "$node")"
  ssh -n "${ssh_base[@]}" "$user@$host" "$@"
}

scp_node() {
  local src="$1" node="$2" dst="$3"
  local user host
  if node_qga_eligible "$node"; then
    pve_qga_copy "$src" "$node" "$dst"
    return
  fi
  user="$(node_field "$node" ssh_user)"
  host="$(node_ssh_host "$node")"
  scp -i "$ssh_key" -o UserKnownHostsFile="$known_hosts" -o StrictHostKeyChecking=yes -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=10 -o ServerAliveCountMax=3 "$src" "$user@$host:$dst"
}

remote_prepare_script() {
  cat <<'REMOTE_PREPARE'
set -e
if command -v cloud-init >/dev/null 2>&1; then
  if ! sudo cloud-init status --wait >/tmp/routerd-cloud-init-status.txt 2>&1; then
    sudo cloud-init status --long 2>&1 | tee -a /tmp/routerd-cloud-init-status.txt
    if [ -f /var/lib/cloud/instance/boot-finished ]; then
      echo "warning: cloud-init reported an error after boot-finished; continuing after external repair" >&2
    elif ! grep -q '^status: done' /tmp/routerd-cloud-init-status.txt; then
      cat /tmp/routerd-cloud-init-status.txt >&2
      exit 1
    fi
  fi
fi
wait_for_apt() {
  local deadline=$((SECONDS + 300))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if ! sudo fuser /var/lib/dpkg/lock-frontend /var/lib/dpkg/lock /var/lib/apt/lists/lock /var/cache/apt/archives/lock >/dev/null 2>&1; then
      return 0
    fi
    sleep 5
  done
  echo "timed out waiting for apt/dpkg locks" >&2
  sudo fuser -v /var/lib/dpkg/lock-frontend /var/lib/dpkg/lock /var/lib/apt/lists/lock /var/cache/apt/archives/lock >&2 || true
  return 1
}
apt_update() {
  wait_for_apt
  sudo apt-get -o DPkg::Lock::Timeout=300 update
}
apt_install() {
  wait_for_apt
  sudo apt-get -o DPkg::Lock::Timeout=300 install -y --no-install-recommends "$@"
}
apt_retry() {
  local attempt
  for attempt in 1 2 3; do
    if "$@"; then
      return 0
    fi
    echo "apt command failed on attempt ${attempt}: $*" >&2
    sleep $((attempt * 5))
  done
  "$@"
}
ensure_aws_cli() {
  if command -v aws >/dev/null 2>&1; then
    aws --version
    return 0
  fi

  apt_retry apt_update
  apt_retry apt_install ca-certificates curl unzip

  local tmpdir
  tmpdir="$(mktemp -d)"
  curl --retry 5 --retry-delay 2 --retry-all-errors -fsSL \
    https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip \
    -o "$tmpdir/awscliv2.zip"
  unzip -q "$tmpdir/awscliv2.zip" -d "$tmpdir"
  sudo "$tmpdir/aws/install" --bin-dir /usr/local/bin --install-dir /usr/local/aws-cli --update
  rm -rf "$tmpdir"
  command -v aws
  aws --version
}
if [ "${ROUTERD_E2E_REQUIRE_AWS_CLI:-0}" = "1" ]; then
  ensure_aws_cli
fi
REMOTE_PREPARE
}

record_note() {
  {
    date -u '+timestamp=%Y-%m-%dT%H:%M:%SZ'
    echo "run_id=$(jq -r '.fabric.value.run_id // empty' "$tofu_output")"
    echo "evidence_dir=$evidence_dir"
    echo "artifact=$artifact"
    sha256sum "$artifact"
    echo "ssh_key=$ssh_key"
    ssh-keygen -lf "${ssh_key}.pub" 2>/dev/null || ssh-keygen -y -f "$ssh_key" | ssh-keygen -lf -
    echo "legacy_protocols=$legacy_protocols"
    echo "performance_tests=$performance_tests"
    echo "failover_transfer_tests=$failover_transfer_tests"
    echo "failover_transfer_required=$failover_transfer_required"
    echo "failover_transfer_smoke=$failover_transfer_smoke"
    echo "rejoin_after_failover=$rejoin_after_failover"
    echo "success_evidence_minimal=$success_evidence_minimal"
    echo "policy_read=cloudedge-mobility/LAB_POLICY.md and ~/routerd-orchestration.md must be reread before real-machine validation"
  } >"$evidence_dir/run-note.txt"
}

mark_failed() {
  overall=1
  echo "FAIL: $*" >&2
}

merge_validation_status() {
  local current="$1" next="$2"
  if [ "$current" -eq 1 ] || [ "$next" -eq 1 ]; then
    printf '1\n'
  elif [ "$current" -eq 2 ] || [ "$next" -eq 2 ]; then
    printf '2\n'
  else
    printf '0\n'
  fi
}

scan_host_key() {
  local node="$1" host="$2"
  local attempt keys
  : >"$evidence_dir/ssh/${node}.keyscan.err"
  for attempt in $(seq 1 30); do
    keys="$(ssh-keyscan -H -T 10 "$host" 2>>"$evidence_dir/ssh/${node}.keyscan.err" || true)"
    if [ -n "$keys" ]; then
      printf '%s\n' "$keys" >>"$known_hosts"
      printf '%s\n' "$keys" >"$evidence_dir/ssh/${node}.keyscan"
      return 0
    fi
    sleep 2
  done
  echo "ssh-keyscan did not return a host key for $node host=$host" >>"$evidence_dir/ssh/${node}.keyscan.err"
  return 1
}

run_preflight_probe() {
  local node="$1"
  local attempt
  for attempt in $(seq 1 12); do
    {
      echo "## $node"
      echo "attempt=$attempt"
      ssh_node "$node" 'hostname; id; ip -br addr; ip route; pgrep -a routerd || true; command -v routerd || true'
    } >"$evidence_dir/preflight/${node}.txt" 2>"$evidence_dir/preflight/${node}.attempt-${attempt}.stderr" && return 0
    cp "$evidence_dir/preflight/${node}.txt" "$evidence_dir/preflight/${node}.attempt-${attempt}.txt"
    sleep 5
  done
  return 1
}

preflight() {
  echo "== preflight =="
  local node host
  if jq -e '.fabric.value.pve.capture_bridge? and (.nodes.value | to_entries[] | select(.value.site == "pve"))' "$tofu_output" >/dev/null; then
    "$script_dir/sam-pve-bridge-audit.sh" \
      --tofu-output "$tofu_output" \
      --evidence "$evidence_dir/preflight/pve-bridge-audit.txt" || return 1
  fi
  for node in "${routers[@]}" "${clients[@]}"; do
    host="$(node_field "$node" public_ip)"
    [ -n "$host" ] && [ "$host" != "null" ] || continue
    scan_host_key "$node" "$host" || return 1
  done
  for node in "${routers[@]}" "${clients[@]}"; do
    run_preflight_probe "$node" || {
      echo "$node preflight failed" >&2
      return 1
    }
  done
}

collect_provider_inventory() {
  local label="$1"
  local dir="$evidence_dir/provider/$label"
  local aws_region azure_rg azure_route_table oci_region oci_compartment_id oci_route_table_id
  local pve_node pve_capture_bridge node id iface
  local status=0 oci_compartment_name
  mkdir -p "$dir"

  cp "$fabric_json" "$dir/fabric.json"
  jq -r '
    to_entries[]
    | [
        .key,
        (.value.site // ""),
        (.value.role // ""),
        (.value.instance_id // ""),
        (.value.interface_id // ""),
        (.value.vm_id // "")
      ]
    | @tsv
  ' "$nodes_json" >"$dir/nodes.tsv"

  aws_region="$(jq -r '.aws.region // empty' "$fabric_json")"
  if [ -n "$aws_region" ]; then
    if ! command -v aws >/dev/null 2>&1; then
      echo "FAIL: aws CLI is required for AWS provider inventory" >"$dir/aws.txt"
      status=1
    else
    {
      echo "## aws caller identity"
      if ! aws_cli sts get-caller-identity; then
        echo "FAIL: aws CLI authentication failed; check AWS_PROFILE/credentials"
        status=1
      fi
      echo "## aws instances"
      mapfile -t ids < <(jq -r 'to_entries[] | select(.value.site == "aws") | .value.instance_id // empty' "$nodes_json" | sort -u)
      if [ "${#ids[@]}" -gt 0 ]; then
        if ! aws_cli ec2 describe-instances --region "$aws_region" --instance-ids "${ids[@]}"; then
          echo "FAIL: aws EC2 instance inventory failed"
          status=1
        fi
      fi
      echo "## aws network interfaces"
      mapfile -t ifaces < <(jq -r 'to_entries[] | select(.value.site == "aws") | .value.interface_id // empty' "$nodes_json" | sort -u)
      if [ "${#ifaces[@]}" -gt 0 ]; then
        if ! aws_cli ec2 describe-network-interfaces --region "$aws_region" --network-interface-ids "${ifaces[@]}"; then
          echo "FAIL: aws EC2 network-interface inventory failed"
          status=1
        fi
      fi
      echo "## aws route tables"
      if ! aws_cli ec2 describe-route-tables --region "$aws_region" --route-table-ids \
        "$(jq -r '.aws.rr_route_table // empty' "$fabric_json")" \
        "$(jq -r '.aws.leaf_route_table_id // empty' "$fabric_json")"; then
        echo "FAIL: aws EC2 route-table inventory failed"
        status=1
      fi
    } >"$dir/aws.txt" 2>&1
    fi
  fi

  azure_rg="$(jq -r '.azure.resource_group_name // empty' "$fabric_json")"
  azure_route_table="$(jq -r '.azure.route_table_name // empty' "$fabric_json")"
  if [ -n "$azure_rg" ]; then
    if ! command -v az >/dev/null 2>&1; then
      echo "FAIL: az CLI is required for Azure provider inventory" >"$dir/azure.txt"
      status=1
    else
    {
      echo "## azure account"
      if ! az account show --output json; then
        echo "FAIL: azure CLI authentication failed; check az login/subscription"
        status=1
      fi
      echo "## azure vm list"
      if ! az vm list --resource-group "$azure_rg" --show-details --output json; then
        echo "FAIL: azure VM inventory failed"
        status=1
      fi
      echo "## azure nic list"
      if ! az network nic list --resource-group "$azure_rg" --output json; then
        echo "FAIL: azure NIC inventory failed"
        status=1
      fi
      echo "## azure route table"
      if [ -n "$azure_route_table" ]; then
        if ! az network route-table show --resource-group "$azure_rg" --name "$azure_route_table" --output json; then
          echo "FAIL: azure route-table inventory failed"
          status=1
        fi
      fi
      echo "## azure role assignments"
      if ! az role assignment list --resource-group "$azure_rg" --output json; then
        echo "FAIL: azure role-assignment inventory failed"
        status=1
      fi
    } >"$dir/azure.txt" 2>&1
    fi
  fi

  oci_region="$(jq -r '.oci.region // empty' "$fabric_json")"
  oci_compartment_id="$(jq -r '.oci.compartment_id // empty' "$fabric_json")"
  oci_route_table_id="$(jq -r '.oci.route_table_id // empty' "$fabric_json")"
  if [ -n "$oci_region" ] && [ -n "$oci_compartment_id" ]; then
    if ! command -v oci >/dev/null 2>&1; then
      echo "FAIL: oci CLI is required for OCI provider inventory" >"$dir/oci.txt"
      status=1
    else
    {
      echo "## oci compartment"
      if ! oci_cli iam compartment get --region "$oci_region" --compartment-id "$oci_compartment_id"; then
        echo "FAIL: oci CLI authentication or compartment lookup failed"
        status=1
      fi
      oci_compartment_name="$(oci_cli iam compartment get --region "$oci_region" --compartment-id "$oci_compartment_id" --query 'data.name' --raw-output 2>/dev/null || true)"
      echo "oci_compartment_name=$oci_compartment_name"
      if [ "$oci_compartment_name" = "ManagedCompartmentForPaaS" ]; then
        echo "FAIL: OCI compartment must not be ManagedCompartmentForPaaS"
        status=1
      elif [ -z "$oci_compartment_name" ]; then
        echo "FAIL: OCI compartment name is empty; check OCI profile, region, and OCID"
        status=1
      fi
      echo "## oci instances"
      while read -r node id; do
        [ -n "$id" ] || continue
        echo "### $node $id"
        if ! oci_cli compute instance get --region "$oci_region" --instance-id "$id"; then
          echo "FAIL: oci instance inventory failed for $node"
          status=1
        fi
      done < <(jq -r 'to_entries[] | select(.value.site == "oci") | [.key, (.value.instance_id // "")] | @tsv' "$nodes_json")
      echo "## oci vnics"
      while read -r node iface; do
        [ -n "$iface" ] || continue
        echo "### $node $iface"
        if ! oci_cli network vnic get --region "$oci_region" --vnic-id "$iface"; then
          echo "FAIL: oci VNIC inventory failed for $node"
          status=1
        fi
      done < <(jq -r 'to_entries[] | select(.value.site == "oci") | [.key, (.value.interface_id // "")] | @tsv' "$nodes_json")
      echo "## oci route table"
      if [ -n "$oci_route_table_id" ]; then
        if ! oci_cli network route-table get --region "$oci_region" --rt-id "$oci_route_table_id"; then
          echo "FAIL: oci route-table inventory failed"
          status=1
        fi
      fi
    } >"$dir/oci.txt" 2>&1
    fi
  fi

  {
    pve_node="$(jq -r '.pve.node_name // empty' "$fabric_json")"
    pve_capture_bridge="$(jq -r '.pve.capture_bridge // empty' "$fabric_json")"
    echo "pve_node=$pve_node"
    echo "pve_capture_bridge=$pve_capture_bridge"
    jq -r 'to_entries[] | select(.value.site == "pve") | [.key, (.value.role // ""), (.value.vm_id // ""), (.value.private_ip // ""), (.value.public_ip // "")] | @tsv' "$nodes_json"
    if command -v qm >/dev/null 2>&1; then
      while read -r node id; do
        [ -n "$id" ] || continue
        echo "### qm config $node $id"
        qm config "$id" || true
      done < <(jq -r 'to_entries[] | select(.value.site == "pve") | [.key, (.value.vm_id // "")] | @tsv' "$nodes_json")
    fi
  } >"$dir/pve.txt" 2>&1

  return "$status"
}

generate_configs() {
  if [ -n "$configs_dir" ]; then
    echo "$configs_dir"
    return
  fi
  local gen_dir="$evidence_dir/config-gen"
  PVE_CAPTURE_INTERFACE="$pve_capture_interface" "$config_generator" --tofu-output "$tofu_output" --out-dir "$gen_dir" >"$evidence_dir/deploy/config-generate.log" 2>&1
  echo "$gen_dir/configs"
}

find_artifact_binary() {
  local root="$1" name="$2" path
  path="$(find "$root" -type f -name "$name" -perm -100 | sort | head -n 1)"
  [ -n "$path" ] || { echo "$name not found in artifact $artifact" >&2; return 1; }
  printf '%s\n' "$path"
}

wait_for_sandbox_socket() {
  local pid="$1" socket="$2" stdout_log="$3" stderr_log="$4"
  local i
  for i in $(seq 1 300); do
    if ! kill -0 "$pid" >/dev/null 2>&1; then
      echo "routerd sandbox exited before status socket became ready" >&2
      cat "$stdout_log" >&2 || true
      cat "$stderr_log" >&2 || true
      return 1
    fi
    [ -S "$socket" ] && return 0
    sleep 0.1
  done
  echo "routerd sandbox status socket did not become ready: $socket" >&2
  cat "$stdout_log" >&2 || true
  cat "$stderr_log" >&2 || true
  return 1
}

validate_generated_configs() {
  local cfg_dir="$1"
  local out_dir="$evidence_dir/config-validate"
  local artifact_root="$out_dir/artifact"
  local routerd_bin routerctl_bin sandbox_root status_socket sandbox_pid cfg name rc
  mkdir -p "$out_dir"
  rm -rf "$artifact_root"
  mkdir -p "$artifact_root"
  tar -xzf "$artifact" -C "$artifact_root"
  routerd_bin="$(find_artifact_binary "$artifact_root" routerd)"
  routerctl_bin="$(find_artifact_binary "$artifact_root" routerctl)"
  {
    echo "artifact=$artifact"
    echo "routerd_bin=$routerd_bin"
    "$routerd_bin" version || true
    echo "routerctl_bin=$routerctl_bin"
    "$routerctl_bin" version || true
  } >"$out_dir/binaries.txt" 2>&1

  sandbox_root="$(mktemp -d "${TMPDIR:-/tmp}/routerd-sam-sandbox.XXXXXX")"
  echo "$sandbox_root" >"$out_dir/sandbox-root.txt"
  "$routerd_bin" serve --sandbox --root "$sandbox_root" >"$out_dir/sandbox.stdout" 2>"$out_dir/sandbox.stderr" &
  sandbox_pid=$!
  trap 'kill "$sandbox_pid" >/dev/null 2>&1 || true; rm -rf "$sandbox_root"; trap - RETURN' RETURN
  status_socket="$sandbox_root/run/routerd/routerd-status.sock"
  wait_for_sandbox_socket "$sandbox_pid" "$status_socket" "$out_dir/sandbox.stdout" "$out_dir/sandbox.stderr"

  for cfg in "$cfg_dir"/*.yaml; do
    [ -f "$cfg" ] || continue
    name="$(basename "$cfg" .yaml)"
    rc=0
    "$routerctl_bin" validate --socket "$status_socket" -f "$cfg" --replace >"$out_dir/$name.routerctl-validate.json" 2>"$out_dir/$name.routerctl-validate.stderr" || rc=$?
    echo "$rc" >"$out_dir/$name.routerctl-validate.rc"
    if [ "$rc" -ne 0 ]; then
      echo "routerctl validate failed for $cfg with rc=$rc" >&2
      cat "$out_dir/$name.routerctl-validate.stderr" >&2 || true
      return 1
    fi
    if ! jq -e '.valid == true' "$out_dir/$name.routerctl-validate.json" >/dev/null; then
      echo "routerctl validate returned JSON .valid != true for $cfg" >&2
      cat "$out_dir/$name.routerctl-validate.json" >&2 || true
      return 1
    fi
    local load_root
    load_root="$(mktemp -d "${TMPDIR:-/tmp}/routerd-sam-load.XXXXXX")"
    echo "$load_root" >"$out_dir/$name.routerd-load-root.txt"
    rc=0
    "$routerd_bin" serve --sandbox --root "$load_root" --config "$cfg" --once >"$out_dir/$name.routerd-validate.txt" 2>"$out_dir/$name.routerd-validate.stderr" || rc=$?
    rm -rf "$load_root"
    echo "$rc" >"$out_dir/$name.routerd-validate.rc"
    if [ "$rc" -ne 0 ]; then
      echo "routerd canonical load failed for $cfg with rc=$rc" >&2
      cat "$out_dir/$name.routerd-validate.stderr" >&2 || true
      return 1
    fi
  done
  kill "$sandbox_pid" >/dev/null 2>&1 || true
  wait "$sandbox_pid" >/dev/null 2>&1 || true
  rm -rf "$sandbox_root"
  trap - RETURN
}

deploy() {
  [ "$skip_deploy" -eq 0 ] || return 0
  local cfg_dir="$1"
  local node
  for node in "${routers[@]}"; do
    deploy_one_router "$cfg_dir" "$node" || return 1
  done
}

deploy_one_router() {
  local cfg_dir="$1" node="$2"
  local cfg require_aws_cli
  {
    cfg="$cfg_dir/$node.yaml"
    [ -f "$cfg" ] || { echo "missing config for $node: $cfg" >&2; return 1; }
    require_aws_cli=0
    case "$node" in
      aws-*) require_aws_cli=1 ;;
    esac
    echo "## install $node"
    scp_node "$artifact" "$node" /tmp/routerd-sam-e2e.tar.gz
    scp_node "$cfg" "$node" /tmp/router.yaml
    if [ -f "$evidence_dir/config-gen/secrets/eventd-cloudedge.key" ]; then
      scp_node "$evidence_dir/config-gen/secrets/eventd-cloudedge.key" "$node" /tmp/eventd-cloudedge.key
    fi
    ssh_node "$node" "$(printf 'export ROUTERD_E2E_REQUIRE_AWS_CLI=%q\n' "$require_aws_cli"; remote_prepare_script; cat <<'REMOTE_DEPLOY'
set -e
rm -rf /tmp/routerd-sam-e2e
mkdir -p /tmp/routerd-sam-e2e
tar -xzf /tmp/routerd-sam-e2e.tar.gz -C /tmp/routerd-sam-e2e
cd /tmp/routerd-sam-e2e
sudo ./install.sh --yes --prefix /usr/local
if [ -f systemd/routerd-bgp.service ] && ! systemctl list-unit-files routerd-bgp.service --no-legend 2>/dev/null | grep -q '^routerd-bgp\.service'; then
  sudo install -m 0644 systemd/routerd-bgp.service /etc/systemd/system/routerd-bgp.service
  sudo systemctl daemon-reload
fi
sudo mkdir -p /usr/local/etc/routerd/secrets
sudo install -m 0600 /tmp/router.yaml /usr/local/etc/routerd/router.yaml
if [ -f /tmp/eventd-cloudedge.key ]; then
  sudo install -m 0600 /tmp/eventd-cloudedge.key /usr/local/etc/routerd/secrets/eventd-cloudedge.key
fi
sudo systemctl restart routerd.service
if systemctl list-unit-files routerd-bgp.service --no-legend 2>/dev/null | grep -q '^routerd-bgp\.service'; then
  sudo systemctl restart routerd-bgp.service
  sudo systemctl is-active routerd-bgp.service
fi
sudo systemctl is-active routerd.service
command -v routerd
command -v routerctl
command -v jq
ready=0
deadline=$((SECONDS + 60))
while [ "$SECONDS" -lt "$deadline" ]; do
  if sudo routerctl get status -o json >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 2
done
if [ "$ready" -ne 1 ]; then
  sudo systemctl status routerd.service routerd-bgp.service --no-pager -l || true
  ls -la /run/routerd 2>&1 || true
  sudo routerctl get status -o json >/dev/null
fi
REMOTE_DEPLOY
)"
  } >"$evidence_dir/deploy/${node}.txt" 2>&1
}

setup_pve_dataplane() {
  local node ip
  for node in "${pve_dataplane_nodes[@]}"; do
    ip="$(node_field "$node" private_ip)"
    ssh_node "$node" "set -e; if ! ip -4 addr show dev '$pve_capture_interface' | grep -qw '$ip/24'; then sudo ip addr add '$ip/24' dev '$pve_capture_interface'; fi; ip -br addr show dev '$pve_capture_interface'" \
      >"$evidence_dir/preflight/${node}-dataplane-ip.txt" 2>&1
  done
}

warm_onprem_discovery() {
  local label="$1"
  local node pve_client_ips_text
  [ "${#pve_leaf_routers[@]}" -gt 0 ] || return 0
  pve_client_ips_text="$(jq -r 'to_entries[] | select(.value.site == "pve" and .value.role == "client") | .value.private_ip' "$nodes_json")"
  [ -n "$pve_client_ips_text" ] || return 0
  for node in "${pve_leaf_routers[@]}"; do
    node_is_stopped "$node" && continue
    ssh_node "$node" "while read -r ip; do
  [ -n \"\$ip\" ] || continue
  timeout 3 ping -I '$pve_capture_interface' -c 2 -W 1 \"\$ip\" >/dev/null 2>&1 || true
done <<'IPS'
$pve_client_ips_text
IPS
ip neigh show dev '$pve_capture_interface' || true" >"$evidence_dir/convergence/${label}-${node}-onprem-warmup.txt" 2>&1 || true
  done
}

wait_dataplane_control_gate() {
  local label="$1"
  local deadline=$((SECONDS + 300))
  local ok=0
  local client_ips_text local_client_ips_text remote_client_ips_text node node_site
  client_ips_text="$(jq -r 'to_entries[] | select(.value.role == "client") | .value.private_ip' "$nodes_json")"
  warm_onprem_discovery "$label"
  while [ "$SECONDS" -lt "$deadline" ]; do
    ok=1
    for node in "${routers[@]}"; do
      node_is_stopped "$node" && continue
      ssh_node "$node" 'sudo routerctl doctor sam >/tmp/routerd-sam-doctor.txt 2>&1' >/dev/null 2>&1 || true
    done
    for node in "${leaf_routers[@]}"; do
      node_is_stopped "$node" && continue
      node_site="$(node_field "$node" site)"
      local_client_ips_text="$(jq -r --arg site "$node_site" 'to_entries[] | select(.value.role == "client" and .value.site == $site) | .value.private_ip' "$nodes_json")"
      remote_client_ips_text="$(jq -r --arg site "$node_site" 'to_entries[] | select(.value.role == "client" and .value.site != $site) | .value.private_ip' "$nodes_json")"
      if ! ssh_node "$node" "set -e
command -v routerctl >/dev/null
sudo routerctl get status -o json >/dev/null
while read -r ip; do
  [ -n \"\$ip\" ] || continue
  ip route get \"\$ip\" >/dev/null
done <<'IPS'
$client_ips_text
IPS
while read -r ip; do
  [ -n \"\$ip\" ] || continue
  ip route get \"\$ip\" | grep -q ' dev samt'
done <<'IPS'
$remote_client_ips_text
IPS"; then
        ok=0
      fi
      if ! ssh_node "$node" "set -e
status=\"\$(sudo routerctl describe MobilityPool/cloudedge -o json)\"
while read -r ip; do
  [ -n \"\$ip\" ] || continue
  printf '%s\n' \"\$status\" | jq -e --arg addr \"\$ip/32\" '
    any(.resource.status.ownershipResolverOwnerTable[]?; .address == \$addr)
  ' >/dev/null
done <<'IPS'
$local_client_ips_text
IPS"; then
        ok=0
      fi
    done
    [ "$ok" -eq 1 ] && break
    sleep 10
  done
  [ "$ok" -eq 1 ]
}

wait_provider_gate() {
  local label="$1"
  local started="$SECONDS"
  local deadline=$((SECONDS + 900))
  local ok=0
  local status_text=TIMEOUT
  local node
  while [ "$SECONDS" -lt "$deadline" ]; do
    ok=1
    for node in "${leaf_routers[@]}"; do
      node_is_stopped "$node" && continue
      if ! ssh_node "$node" "set -e
status=\"\$(sudo routerctl describe MobilityPool/cloudedge -o json)\"
printf '%s\n' \"\$status\" | jq -e '
  .resource.status as \$s
  | ((\$s.phase // \"\") != \"Degraded\")
  and ((\$s.phase // \"\") != \"Failed\")
  and ((\$s.providerActionPendingCount // 0) == 0)
  and ((\$s.providerActionFailedCount // 0) == 0)
  and ((\$s.providerObservationPendingCount // 0) == 0)
  and ((\$s.ownershipResolverConflictCount // 0) == 0)
' >/dev/null"; then
        ok=0
      fi
    done
    [ "$ok" -eq 1 ] && break
    sleep 10
  done
  [ "$ok" -eq 1 ] && status_text=PASS
  printf '%s\t%s\t%s\n' "${label}-provider" "$status_text" "$((SECONDS - started))" >>"$evidence_dir/convergence/summary.tsv"
  [ "$ok" -eq 1 ]
}

collect_convergence_snapshot() {
  local label="$1"
  local node
  for node in "${routers[@]}"; do
    node_is_stopped "$node" && continue
    ssh_node "$node" 'sudo routerctl doctor sam; sudo routerctl get status -o json; ip -br addr; ip route' >"$evidence_dir/convergence/${label}-${node}.txt" 2>&1 || true
  done
}

client_matrix() {
  local label="$1"
  local out="$evidence_dir/matrix/$label"
  local src dst src_ip dst_ip dst_host dst_user result actual attempt
  mkdir -p "$out"
  : >"$out/summary.tsv"
  for src in "${clients[@]}"; do
    for dst in "${clients[@]}"; do
      [ "$src" != "$dst" ] || continue
      src_ip="$(node_field "$src" private_ip)"
      dst_ip="$(node_field "$dst" private_ip)"
      dst_host="$(node_field "$dst" name)"
      dst_user="$(node_field "$dst" ssh_user)"
      result=PASS
      {
        echo "=== $src -> $dst ==="
        echo "SRC=$src SRCIP=$src_ip DST=$dst DSTIP=$dst_ip"
        echo "## route-get"
        ssh_node "$src" "ip route get '$dst_ip' from '$src_ip'" || true
        echo "## ping"
        ssh_node "$src" "ping -I '$src_ip' -c 3 -W 2 '$dst_ip'" || true
        echo "## traceroute"
        ssh_node "$src" "timeout 20s sh -c \"traceroute -n -w 2 -q 1 '$dst_ip' || tracepath '$dst_ip'\" || true"
        echo "## ssh-hostname"
        actual=
        for attempt in 1 2 3; do
          actual="$(ssh_node "$src" "ssh -i ~/.ssh/routerd-cloudedge-lab-20260529 -o UserKnownHostsFile=~/.ssh/routerd-e2e-known_hosts -o StrictHostKeyChecking=yes -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=10 -o ServerAliveCountMax=3 '$dst_user@$dst_ip' hostname 2>/dev/null" 2>"$out/${src}_to_${dst}.nested-ssh.stderr" | tail -n 1)" || true
          [ "$actual" = "$dst_host" ] && break
          sleep 2
        done
        [ "$actual" = "$dst_host" ] || result=FAIL_HOSTNAME
        printf '%s\n' "$actual"
      } >"$out/${src}_to_${dst}.txt" 2>&1 || result=FAIL
      printf '%s\t%s\t%s\n' "$src" "$dst" "$result" >>"$out/summary.tsv"
    done
  done
  ! grep -qv $'\tPASS$' "$out/summary.tsv"
}

cloud_ingress_matrix() {
  local label="$1"
  local out="$evidence_dir/matrix/$label"
  local failed_node failed_site target_filter_site=
  local src dst src_ip dst_ip dst_host dst_user src_site dst_site result actual attempt status=0
  mkdir -p "$out"
  : >"$out/cloud-ingress-summary.tsv"

  case "$label" in
    after-failover-*)
      failed_node="${label#after-failover-}"
      if jq -e --arg node "$failed_node" '.[$node]? | .role == "leaf"' "$nodes_json" >/dev/null; then
        failed_site="$(node_field "$failed_node" site)"
        is_cloud_site "$failed_site" && target_filter_site="$failed_site"
      fi
      ;;
  esac

  for src in "${clients[@]}"; do
    src_site="$(node_field "$src" site)"
    is_cloud_site "$src_site" || continue
    src_ip="$(node_field "$src" private_ip)"
    for dst in "${clients[@]}"; do
      [ "$src" != "$dst" ] || continue
      dst_site="$(node_field "$dst" site)"
      [ -z "$target_filter_site" ] || [ "$dst_site" = "$target_filter_site" ] || continue
      dst_ip="$(node_field "$dst" private_ip)"
      dst_host="$(node_field "$dst" name)"
      dst_user="$(node_field "$dst" ssh_user)"
      result=PASS
      {
        echo "=== cloud ingress $src -> $dst ==="
        echo "SRC=$src SRCSITE=$src_site SRCIP=$src_ip DST=$dst DSTSITE=$dst_site DSTIP=$dst_ip"
        echo "## route-get"
        ssh_node "$src" "ip route get '$dst_ip' from '$src_ip'" || true
        echo "## ping"
        ssh_node "$src" "ping -I '$src_ip' -c 3 -W 2 '$dst_ip'" || result=FAIL_PING
        echo "## ssh-hostname"
        actual=
        for attempt in 1 2 3; do
          actual="$(ssh_node "$src" "ssh -i ~/.ssh/routerd-cloudedge-lab-20260529 -o UserKnownHostsFile=~/.ssh/routerd-e2e-known_hosts -o StrictHostKeyChecking=yes -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=10 -o ServerAliveCountMax=3 '$dst_user@$dst_ip' hostname 2>/dev/null" 2>"$out/${src}_to_${dst}.cloud-ingress-nested-ssh.stderr" | tail -n 1)" || true
          [ "$actual" = "$dst_host" ] && break
          sleep 2
        done
        [ "$actual" = "$dst_host" ] || result=FAIL_HOSTNAME
        printf '%s\n' "$actual"
      } >"$out/${src}_to_${dst}.cloud-ingress.txt" 2>&1 || result=FAIL
      printf '%s\t%s\t%s\n' "$src" "$dst" "$result" >>"$out/cloud-ingress-summary.tsv"
      [ "$result" = "PASS" ] || status=1
    done
  done

  if [ ! -s "$out/cloud-ingress-summary.tsv" ]; then
    printf 'none\tnone\tSKIP_NO_CLOUD_INGRESS_TARGETS\n' >>"$out/cloud-ingress-summary.tsv"
  fi
  return "$status"
}

setup_client_ssh() {
  local client_known_hosts="$evidence_dir/ssh/client_known_hosts"
  local dst dst_ip dst_public client client_name client_site remote_client_ips_text local_leaf_ips_text
  : >"$client_known_hosts"
  for dst in "${clients[@]}"; do
    dst_ip="$(node_field "$dst" private_ip)"
    dst_public="$(node_field "$dst" public_ip)"
    if [ -n "$dst_public" ] && [ "$dst_public" != "null" ]; then
      ssh-keyscan -T 10 "$dst_public" 2>"$evidence_dir/ssh/${dst}.client-keyscan.err" \
        | awk -v host="$dst_ip" 'NF >= 3 {$1 = host; print}' >>"$client_known_hosts"
    else
      ssh_node "$dst" "ssh-keyscan -T 10 localhost" 2>"$evidence_dir/ssh/${dst}.client-keyscan.err" \
        | awk -v host="$dst_ip" 'NF >= 3 {$1 = host; print}' >>"$client_known_hosts"
    fi
  done
  for client in "${clients[@]}"; do
    client_name="$(node_field "$client" name)"
    client_site="$(node_field "$client" site)"
    remote_client_ips_text="$(jq -r --arg site "$client_site" 'to_entries[] | select(.value.role == "client" and .value.site != $site) | .value.private_ip' "$nodes_json")"
    local_leaf_ips_text="$(jq -r --arg site "$client_site" 'to_entries[] | select(.value.role == "leaf" and .value.site == $site) | .value.private_ip' "$nodes_json")"
    scp_node "$ssh_key" "$client" /tmp/routerd-cloudedge-lab-20260529
    scp_node "$client_known_hosts" "$client" /tmp/routerd-e2e-known_hosts
    ssh_node "$client" "set -e
sudo hostnamectl set-hostname '$client_name'
mkdir -p ~/.ssh
install -m 0600 /tmp/routerd-cloudedge-lab-20260529 ~/.ssh/routerd-cloudedge-lab-20260529
install -m 0644 /tmp/routerd-e2e-known_hosts ~/.ssh/routerd-e2e-known_hosts
leaf_nexthops=
while read -r gw; do
  [ -n \"\$gw\" ] || continue
  leaf_nexthops=\"\$leaf_nexthops nexthop via \$gw weight 1\"
done <<'LEAFS'
$local_leaf_ips_text
LEAFS
[ -n \"\$leaf_nexthops\" ]
while read -r ip; do
  [ -n \"\$ip\" ] || continue
  sudo ip route replace \"\$ip/32\" \$leaf_nexthops
done <<'IPS'
$remote_client_ips_text
IPS
ip route" >"$evidence_dir/preflight/${client}-client-routes.txt" 2>&1
  done
}

setup_legacy_protocol_services() {
  [ "$legacy_protocols" -eq 1 ] || return 0
  local node
  for node in "${clients[@]}"; do
    {
      echo "## setup legacy protocol services on $node"
      ssh_node "$node" "$(remote_prepare_script; cat <<'REMOTE_LEGACY'
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
if command -v apt-get >/dev/null 2>&1; then
  echo "iperf3 iperf3/start_daemon boolean false" | sudo debconf-set-selections || true
  apt_update
  apt_install curl rpcbind nfs-kernel-server nfs-common samba smbclient cifs-utils vsftpd iperf3
fi
sudo mkdir -p /srv/routerd-e2e/ftp/pub /srv/routerd-e2e/nfs /srv/routerd-e2e/cifs /srv/routerd-e2e/http
printf "ftp probe from %s\n" "$(hostname)" | sudo tee /srv/routerd-e2e/ftp/pub/probe.txt >/dev/null
        printf "nfs probe from %s\n" "$(hostname)" | sudo tee /srv/routerd-e2e/nfs/probe.txt >/dev/null
        printf "cifs probe from %s\n" "$(hostname)" | sudo tee /srv/routerd-e2e/cifs/probe.txt >/dev/null
        sudo chmod 0755 /srv/routerd-e2e /srv/routerd-e2e/ftp
        sudo chmod -R 0777 /srv/routerd-e2e/ftp/pub /srv/routerd-e2e/nfs /srv/routerd-e2e/cifs /srv/routerd-e2e/http

        sudo mkdir -p /etc/exports.d
        printf "/srv/routerd-e2e/nfs 10.77.60.0/24(rw,sync,no_subtree_check,no_root_squash,insecure)\n" | sudo tee /etc/exports.d/routerd-e2e.exports >/dev/null
        sudo mkdir -p /etc/nfs.conf.d
        printf "[mountd]\nport=20048\n" | sudo tee /etc/nfs.conf.d/routerd-e2e.conf >/dev/null
        if [ -f /etc/default/nfs-kernel-server ]; then
          if grep -q "^RPCMOUNTDOPTS=" /etc/default/nfs-kernel-server; then
            sudo sed -i "s/^RPCMOUNTDOPTS=.*/RPCMOUNTDOPTS=\"--port 20048\"/" /etc/default/nfs-kernel-server
          else
            printf "RPCMOUNTDOPTS=\"--port 20048\"\n" | sudo tee -a /etc/default/nfs-kernel-server >/dev/null
          fi
        fi
        sudo systemctl enable --now rpcbind >/dev/null 2>&1 || sudo systemctl restart rpcbind
        sudo exportfs -ra
        sudo systemctl restart nfs-server || sudo systemctl restart nfs-kernel-server

        if ! grep -q "^\[routerd_e2e\]" /etc/samba/smb.conf; then
          sudo tee -a /etc/samba/smb.conf >/dev/null <<'SMBEOF'

[routerd_e2e]
   path = /srv/routerd-e2e/cifs
   browseable = yes
   read only = no
   guest ok = yes
   force user = nobody
SMBEOF
        fi
        sudo systemctl restart smbd || true
        sudo systemctl restart nmbd || true
        sudo modprobe cifs >/dev/null 2>&1 || true

        sudo tee /etc/vsftpd.conf >/dev/null <<'VSFTPEOF'
listen=YES
listen_ipv6=NO
anonymous_enable=YES
anon_root=/srv/routerd-e2e/ftp
no_anon_password=YES
write_enable=YES
anon_upload_enable=YES
anon_mkdir_write_enable=YES
anon_other_write_enable=YES
local_enable=NO
dirmessage_enable=NO
xferlog_enable=YES
connect_from_port_20=YES
seccomp_sandbox=NO
pasv_enable=YES
pasv_min_port=30000
pasv_max_port=30010
VSFTPEOF
        sudo systemctl restart vsftpd
        sudo pkill iperf3 >/dev/null 2>&1 || true
        sudo iperf3 -s -D </dev/null >/dev/null 2>&1
        if command -v iptables >/dev/null 2>&1; then
          for rule in \
            "-p tcp --dport 21" \
            "-p tcp --dport 111" \
            "-p udp --dport 111" \
            "-p tcp --dport 139" \
            "-p tcp --dport 445" \
            "-p tcp --dport 2049" \
            "-p udp --dport 2049" \
            "-p tcp --dport 20048" \
            "-p udp --dport 20048" \
            "-p tcp --dport 30000:30010" \
            "-p tcp --dport 5201" \
            "-p udp --dport 5201"; do
            sudo sh -c "iptables -C INPUT $rule -j ACCEPT 2>/dev/null || iptables -I INPUT 1 $rule -j ACCEPT"
          done
        fi
        sudo systemctl --no-pager --plain is-active rpcbind || true
sudo systemctl --no-pager --plain is-active nfs-server nfs-kernel-server smbd vsftpd 2>/dev/null || true
ss -lntup | grep -E ":(21|111|139|445|2049|20048|5201)\b" || true
REMOTE_LEGACY
)"
    } >"$evidence_dir/legacy/setup-${node}.txt" 2>&1 || return 1
  done
}

setup_performance_services() {
  [ "$performance_tests" -eq 1 ] || return 0
  local node
  for node in "${clients[@]}"; do
    {
      echo "## setup performance services on $node"
      ssh_node "$node" "$(remote_prepare_script; cat <<'REMOTE_PERF'
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
if command -v apt-get >/dev/null 2>&1; then
  echo "iperf3 iperf3/start_daemon boolean false" | sudo debconf-set-selections || true
  apt_update
  apt_install iperf3
fi
sudo pkill iperf3 >/dev/null 2>&1 || true
sudo iperf3 -s -D </dev/null >/dev/null 2>&1
if command -v iptables >/dev/null 2>&1; then
  for rule in "-p tcp --dport 5201" "-p udp --dport 5201"; do
    sudo sh -c "iptables -C INPUT $rule -j ACCEPT 2>/dev/null || iptables -I INPUT 1 $rule -j ACCEPT"
  done
fi
ss -lntup | grep -E ":5201\b" || true
REMOTE_PERF
)"
    } >"$evidence_dir/performance/setup-${node}.txt" 2>&1 || return 1
  done
}

setup_failover_transfer_services() {
  [ "$failover_transfer_tests" -eq 1 ] || return 0
  local node
  for node in "${clients[@]}"; do
    {
      echo "## setup failover transfer service on $node"
      ssh_node "$node" "$(remote_prepare_script; cat <<'REMOTE_TRANSFER'
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
if command -v apt-get >/dev/null 2>&1; then
  apt_update
  apt_install curl python3
fi
        sudo mkdir -p /srv/routerd-e2e/http
        if [ ! -f /srv/routerd-e2e/http/failover-transfer.bin ]; then
          sudo dd if=/dev/zero of=/srv/routerd-e2e/http/failover-transfer.bin bs=1M count=64 status=none
        fi
        sudo chmod -R 0755 /srv/routerd-e2e/http
        if [ -s /tmp/routerd-e2e-http.pid ]; then
          sudo kill "$(cat /tmp/routerd-e2e-http.pid)" >/dev/null 2>&1 || true
        fi
        rm -f /tmp/routerd-e2e-http.pid
        sleep 1
nohup python3 -m http.server 8080 --bind 0.0.0.0 --directory /srv/routerd-e2e/http >/tmp/routerd-e2e-http.log 2>&1 &
echo $! >/tmp/routerd-e2e-http.pid
sleep 1
ss -lntp | grep -E ":8080\b"
REMOTE_TRANSFER
)"
    } >"$evidence_dir/failover-transfer/setup-${node}.txt" 2>&1 || return 1
  done
}

is_global_ipv4() {
  local ip="$1" a b c d
  IFS=. read -r a b c d <<EOF
$ip
EOF
  case "$a.$b.$c.$d" in
    ""|*[!0-9.]*)
      return 1
      ;;
  esac
  [ "$a" -ge 1 ] 2>/dev/null && [ "$a" -le 223 ] || return 1
  [ "$b" -ge 0 ] 2>/dev/null && [ "$b" -le 255 ] || return 1
  [ "$c" -ge 0 ] 2>/dev/null && [ "$c" -le 255 ] || return 1
  [ "$d" -ge 0 ] 2>/dev/null && [ "$d" -le 255 ] || return 1
  [ "$a" -eq 10 ] && return 1
  [ "$a" -eq 127 ] && return 1
  [ "$a" -eq 169 ] && [ "$b" -eq 254 ] && return 1
  [ "$a" -eq 172 ] && [ "$b" -ge 16 ] && [ "$b" -le 31 ] && return 1
  [ "$a" -eq 192 ] && [ "$b" -eq 168 ] && return 1
  [ "$a" -eq 100 ] && [ "$b" -ge 64 ] && [ "$b" -le 127 ] && return 1
  [ "$a" -ge 224 ] && return 1
  return 0
}

is_cloud_site() {
  case "$1" in
    aws|azure|oci)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

legacy_protocol_matrix() {
  [ "$legacy_protocols" -eq 1 ] || return 0
  local label="$1"
  local out="$evidence_dir/legacy/$label"
  local status=0 src dst src_ip dst_ip result
  mkdir -p "$out"
  : >"$out/summary.tsv"
  for src in "${clients[@]}"; do
    for dst in "${clients[@]}"; do
      [ "$src" != "$dst" ] || continue
      src_ip="$(node_field "$src" private_ip)"
      dst_ip="$(node_field "$dst" private_ip)"
      result=PASS
      {
        echo "=== legacy $src -> $dst ==="
        echo "SRC=$src SRCIP=$src_ip DST=$dst DSTIP=$dst_ip"
        echo "## rpcinfo"
        ssh_node "$src" "timeout 15s rpcinfo -p '$dst_ip'" || result=FAIL_RPC
        echo "## ftp read"
        ssh_node "$src" "timeout 20s curl -fsS --connect-timeout 10 'ftp://$dst_ip/pub/probe.txt'" || result=FAIL_FTP
        echo "## ftp write"
        ssh_node "$src" "printf 'ftp upload from $src to $dst\n' | timeout 20s curl -fsS --connect-timeout 10 -T - 'ftp://$dst_ip/pub/upload-${src}.txt'" || result=FAIL_FTP
        echo "## nfs mount/read/write"
        ssh_node "$src" "set -e; mnt=\$(mktemp -d); trap 'sudo umount \"\$mnt\" >/dev/null 2>&1 || true; rmdir \"\$mnt\" >/dev/null 2>&1 || true' EXIT; sudo timeout 25s mount -t nfs -o vers=3,proto=tcp,timeo=5,retrans=1,mountport=20048 '$dst_ip:/srv/routerd-e2e/nfs' \"\$mnt\"; cat \"\$mnt/probe.txt\"; printf 'nfs write from $src to $dst\n' | sudo tee \"\$mnt/write-${src}.txt\" >/dev/null; test -s \"\$mnt/write-${src}.txt\"" || result=FAIL_NFS
        echo "## cifs mount/read/write"
        ssh_node "$src" "set -e; sudo modprobe cifs >/dev/null 2>&1 || true; mnt=\$(mktemp -d); trap 'sudo umount \"\$mnt\" >/dev/null 2>&1 || true; rmdir \"\$mnt\" >/dev/null 2>&1 || true' EXIT; sudo timeout 25s mount -t cifs '//$dst_ip/routerd_e2e' \"\$mnt\" -o guest,vers=3.0; cat \"\$mnt/probe.txt\"; printf 'cifs write from $src to $dst\n' | sudo tee \"\$mnt/write-${src}.txt\" >/dev/null; test -s \"\$mnt/write-${src}.txt\"" || result=FAIL_CIFS
      } >"$out/${src}_to_${dst}.txt" 2>&1 || result=FAIL
      printf '%s\t%s\t%s\n' "$src" "$dst" "$result" >>"$out/summary.tsv"
      [ "$result" = "PASS" ] || status=1
    done
  done
  return "$status"
}

performance_matrix() {
  [ "$performance_tests" -eq 1 ] || return 0
  local label="$1"
  local out="$evidence_dir/performance/$label"
  local status=0 src dst src_ip dst_ip src_public dst_public src_site dst_site result public_result
  local ok attempt sam_tcp_bps sam_udp_bps sam_ping_loss public_tcp_bps public_udp_bps public_ping_loss
  mkdir -p "$out"
  : >"$out/summary.tsv"
  : >"$out/public-summary.tsv"
  : >"$out/comparison.tsv"
  printf 'src\tdst\tsam_tcp_bps\tsam_udp_bps\tsam_ping_loss_pct\tpublic_tcp_bps\tpublic_udp_bps\tpublic_ping_loss_pct\tpublic_status\n' >"$out/comparison.tsv"
  for src in "${clients[@]}"; do
    for dst in "${clients[@]}"; do
      [ "$src" != "$dst" ] || continue
      src_ip="$(node_field "$src" private_ip)"
      dst_ip="$(node_field "$dst" private_ip)"
      src_public="$(node_field "$src" public_ip)"
      dst_public="$(node_field "$dst" public_ip)"
      src_site="$(node_field "$src" site)"
      dst_site="$(node_field "$dst" site)"
      result=PASS
      {
        echo "=== sam private performance $src -> $dst ==="
        echo "SRC=$src SRCIP=$src_ip DST=$dst DSTIP=$dst_ip"
        echo "## tcp iperf3"
        ok=0
        for attempt in 1 2 3; do
          ssh_node "$dst" 'sudo pkill -9 iperf3 >/dev/null 2>&1 || true; sleep 2; sudo iperf3 -s -D </dev/null >/dev/null 2>&1; sleep 2'
          ssh_node "$src" "timeout 20s iperf3 -J -c '$dst_ip' -B '$src_ip' -t 5" >"$out/${src}_to_${dst}.iperf3-tcp.json" && { ok=1; break; }
          sleep 5
        done
        [ "$ok" -eq 1 ] || result=FAIL_TCP
        echo "## udp iperf3"
        ok=0
        for attempt in 1 2 3; do
          ssh_node "$dst" 'sudo pkill -9 iperf3 >/dev/null 2>&1 || true; sleep 2; sudo iperf3 -s -D </dev/null >/dev/null 2>&1; sleep 2'
          ssh_node "$src" "timeout 20s iperf3 -J -u -b 10M -c '$dst_ip' -B '$src_ip' -t 5" >"$out/${src}_to_${dst}.iperf3-udp.json" && { ok=1; break; }
          sleep 5
        done
        [ "$ok" -eq 1 ] || result=FAIL_UDP
        echo "## small packet ping sample"
        ok=0
        for attempt in 1 2 3; do
          ssh_node "$src" "timeout 20s ping -I '$src_ip' -s 56 -c 100 -i 0.01 '$dst_ip'" >"$out/${src}_to_${dst}.ping-pps.txt" && { ok=1; break; }
          sleep 2
        done
        [ "$ok" -eq 1 ] || result=FAIL_PING_PPS
      } >"$out/${src}_to_${dst}.txt" 2>&1 || result=FAIL
      printf '%s\t%s\t%s\n' "$src" "$dst" "$result" >>"$out/summary.tsv"
      [ "$result" = "PASS" ] || status=1

      public_result=PASS
      if ! is_cloud_site "$src_site" || ! is_cloud_site "$dst_site"; then
        public_result=SKIP_PUBLIC_CLOUD_ONLY
        printf '%s\t%s\t%s\n' "$src" "$dst" "$public_result" >>"$out/public-summary.tsv"
      elif [ "$src_site" = "$dst_site" ]; then
        public_result=SKIP_PUBLIC_SAME_CLOUD
        printf '%s\t%s\t%s\n' "$src" "$dst" "$public_result" >>"$out/public-summary.tsv"
      elif ! is_global_ipv4 "$dst_public"; then
        public_result=SKIP_PUBLIC_NON_GLOBAL_DST
        printf '%s\t%s\t%s\n' "$src" "$dst" "$public_result" >>"$out/public-summary.tsv"
      else
        {
          echo "=== public direct performance $src -> $dst ==="
          echo "SRC=$src SRCPUBLIC=$src_public DST=$dst DSTPUBLIC=$dst_public"
          echo "## tcp iperf3"
          ok=0
          for attempt in 1 2 3; do
            ssh_node "$dst" 'sudo pkill -9 iperf3 >/dev/null 2>&1 || true; sleep 2; sudo iperf3 -s -D </dev/null >/dev/null 2>&1; sleep 2'
            ssh_node "$src" "timeout 20s iperf3 -J -c '$dst_public' -t 5" >"$out/${src}_to_${dst}.public-iperf3-tcp.json" && { ok=1; break; }
            sleep 5
          done
          [ "$ok" -eq 1 ] || public_result=FAIL_PUBLIC_TCP
          echo "## udp iperf3"
          ok=0
          for attempt in 1 2 3; do
            ssh_node "$dst" 'sudo pkill -9 iperf3 >/dev/null 2>&1 || true; sleep 2; sudo iperf3 -s -D </dev/null >/dev/null 2>&1; sleep 2'
            ssh_node "$src" "timeout 20s iperf3 -J -u -b 10M -c '$dst_public' -t 5" >"$out/${src}_to_${dst}.public-iperf3-udp.json" && { ok=1; break; }
            sleep 5
          done
          [ "$ok" -eq 1 ] || public_result=FAIL_PUBLIC_UDP
          echo "## public ping sample"
          ok=0
          for attempt in 1 2 3; do
            ssh_node "$src" "timeout 20s ping -c 20 -W 2 '$dst_public'" >"$out/${src}_to_${dst}.public-ping.txt" && { ok=1; break; }
            sleep 2
          done
          [ "$ok" -eq 1 ] || public_result=FAIL_PUBLIC_PING
        } >"$out/${src}_to_${dst}.public.txt" 2>&1 || public_result=FAIL_PUBLIC
        printf '%s\t%s\t%s\n' "$src" "$dst" "$public_result" >>"$out/public-summary.tsv"
        [ "$public_result" = "PASS" ] || status=1
      fi

      sam_tcp_bps="$(jq -r '.end.sum_received.bits_per_second // .end.sum.bits_per_second // empty' "$out/${src}_to_${dst}.iperf3-tcp.json" 2>/dev/null || true)"
      sam_udp_bps="$(jq -r '.end.sum.bits_per_second // empty' "$out/${src}_to_${dst}.iperf3-udp.json" 2>/dev/null || true)"
      sam_ping_loss="$(awk -F, '/packet loss/ {gsub(/^[[:space:]]+|% packet loss.*/, "", $3); print $3}' "$out/${src}_to_${dst}.ping-pps.txt" 2>/dev/null || true)"
      public_tcp_bps=
      public_udp_bps=
      public_ping_loss=
      if [ "$public_result" = "PASS" ]; then
        public_tcp_bps="$(jq -r '.end.sum_received.bits_per_second // .end.sum.bits_per_second // empty' "$out/${src}_to_${dst}.public-iperf3-tcp.json" 2>/dev/null || true)"
        public_udp_bps="$(jq -r '.end.sum.bits_per_second // empty' "$out/${src}_to_${dst}.public-iperf3-udp.json" 2>/dev/null || true)"
        public_ping_loss="$(awk -F, '/packet loss/ {gsub(/^[[:space:]]+|% packet loss.*/, "", $3); print $3}' "$out/${src}_to_${dst}.public-ping.txt" 2>/dev/null || true)"
      fi
      printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$src" "$dst" "$sam_tcp_bps" "$sam_udp_bps" "$sam_ping_loss" "$public_tcp_bps" "$public_udp_bps" "$public_ping_loss" "$public_result" >>"$out/comparison.tsv"
    done
  done
  return "$status"
}

run_validation_set() {
  local label="$1"
  local status=0 provider_status=0 dataplane_status=PASS dataplane_started="$SECONDS"
  local phase_started

  phase_started="$SECONDS"
  if ! wait_dataplane_control_gate "$label"; then
    dataplane_status=TIMEOUT
    record_timing "$label" dataplane-control-gate "$phase_started"
  elif [ "$skip_matrix" -eq 1 ]; then
    dataplane_status=PASS
    record_timing "$label" dataplane-control-gate "$phase_started"
  elif ! client_matrix "$label"; then
    dataplane_status=FAIL_MATRIX
    record_timing "$label" dataplane-control-and-client-matrix "$phase_started"
  elif ! cloud_ingress_matrix "$label"; then
    dataplane_status=FAIL_CLOUD_INGRESS
    record_timing "$label" dataplane-control-client-matrix-cloud-ingress "$phase_started"
  else
    record_timing "$label" dataplane-control-client-matrix-cloud-ingress "$phase_started"
  fi

  printf '%s\t%s\t%s\n' "${label}-dataplane" "$dataplane_status" "$((SECONDS - dataplane_started))" >>"$evidence_dir/convergence/summary.tsv"
  if [ "$dataplane_status" != "PASS" ]; then
    phase_started="$SECONDS"
    collect_convergence_snapshot "$label"
    record_timing "$label" convergence-snapshot-after-dataplane-fail "$phase_started"
    echo "DATAPLANE-CONVERGENCE-FAIL: $label $dataplane_status" >&2
    return 1
  elif [ "$success_evidence_minimal" -eq 1 ]; then
    record_skipped_success_evidence convergence "$label"
  else
    phase_started="$SECONDS"
    collect_convergence_snapshot "$label"
    record_timing "$label" convergence-snapshot "$phase_started"
  fi

  phase_started="$SECONDS"
  if ! wait_provider_gate "$label"; then
    provider_status=2
    echo "PROVIDER-CONVERGENCE-FAIL: $label" >&2
  fi
  record_timing "$label" provider-gate "$phase_started"
  if [ "$provider_status" -ne 0 ]; then
    phase_started="$SECONDS"
    collect_convergence_snapshot "${label}-provider"
    record_timing "$label" convergence-snapshot-after-provider-fail "$phase_started"
  elif [ "$success_evidence_minimal" -eq 1 ]; then
    record_skipped_success_evidence convergence "${label}-provider"
  else
    phase_started="$SECONDS"
    collect_convergence_snapshot "${label}-provider"
    record_timing "$label" convergence-snapshot-provider "$phase_started"
  fi

  phase_started="$SECONDS"
  legacy_protocol_matrix "$label" || status=1
  record_timing "$label" legacy-protocol-matrix "$phase_started"
  phase_started="$SECONDS"
  performance_matrix "$label" || status=1
  record_timing "$label" performance-matrix "$phase_started"
  phase_started="$SECONDS"
  collect_load_balance_report "$label"
  record_timing "$label" load-balance-report "$phase_started"
  [ "$status" -eq 0 ] || return "$status"
  return "$provider_status"
}

collect_diagnostics() {
  local label="$1"
  local dir="$evidence_dir/diagnostics/$label"
  local node
  mkdir -p "$dir"
  for node in "${routers[@]}"; do
    ssh_node "$node" "$(cat <<REMOTE_DIAG
echo "stage=$label"
echo "captured_at=\$(date -u +%Y-%m-%dT%H:%M:%SZ)"
hostname
sudo routerctl doctor sam || true
sudo routerctl get status -o json || true
sudo routerctl describe MobilityPool/cloudedge -o json || true
sudo routerctl action list || true
ip -br addr
ip route
ip rule
ip neigh show || true
sysctl net.ipv4.ip_forward net.ipv4.conf.all.rp_filter net.ipv4.conf.default.rp_filter net.ipv4.conf.all.proxy_arp net.ipv4.conf.all.accept_local 2>/dev/null || true
echo "--- events"
# Mirrored from routerd 1f77532a examples/cloudedge-mobility-demo/collect-evidence.sh.
sudo routerctl get events -o json || true
echo "--- routerd.mobility.holder.transition"
sudo routerctl get events --topic routerd.mobility.holder.transition -o json || true
echo "--- event-retention"
sudo routerctl get status -o json 2>/dev/null | grep -Ei '"event|retention|maxAge|maxEvents"' || true
sudo routerctl dynamic render -o json 2>/dev/null | grep -Ei '"EventGroup"|"retention"|"maxAge"|"maxEvents"' || true
echo "--- state-db-events"
if command -v sqlite3 >/dev/null 2>&1 && sudo test -r /var/lib/routerd/routerd.db; then
  sudo sqlite3 -readonly -json /var/lib/routerd/routerd.db "SELECT * FROM events ORDER BY id;" || true
else
  echo "events_db_dump_unavailable"
fi
echo "--- journals"
journalctl -u routerd.service -u routerd-bgp.service --since "30 minutes ago" --no-pager -n 500
REMOTE_DIAG
)" >"$dir/${node}.txt" 2>&1 || true
    ssh_node "$node" 'sudo routerctl get status -o json 2>/dev/null | jq '"'"'
      [
        .. | objects
        | select((.kind? == "BGPRouter") or (.resource.kind? == "BGPRouter") or has("prefixes") or has("livenessMarkers"))
        | {
            name: (.metadata.name? // .resource.metadata.name? // .name? // ""),
            kind: (.kind? // .resource.kind? // "BGPRouter"),
            prefixes: (.status.prefixes? // .resource.status.prefixes? // .prefixes? // []),
            livenessMarkers: (.status.livenessMarkers? // .resource.status.livenessMarkers? // .livenessMarkers? // {})
          }
      ]
    '"'"'' >"$dir/${node}.bgp-prefixes-liveness.json" 2>"$dir/${node}.bgp-prefixes-liveness.stderr" || true
    ssh_node "$node" 'sudo routerctl get status -o json 2>/dev/null | jq -r '"'"'
      [
        .. | objects
        | select((.kind? == "BGPRouter") or (.resource.kind? == "BGPRouter") or has("prefixes") or has("livenessMarkers"))
        | {
            name: (.metadata.name? // .resource.metadata.name? // .name? // ""),
            prefixCount: ((.status.prefixes? // .resource.status.prefixes? // .prefixes? // []) | length),
            truncated: (((.status.prefixes? // .resource.status.prefixes? // .prefixes? // []) | map((.prefix? // .Prefix? // "") == "truncated") | any) // false),
            livenessMarkerCount: ((.status.livenessMarkers? // .resource.status.livenessMarkers? // .livenessMarkers? // {}) | length)
          }
      ]
      | .[]
      | "status_bgp_router name=\(.name) prefixes=\(.prefixCount) truncated=\(.truncated) livenessMarkers=\(.livenessMarkerCount)"
    '"'"'' >"$dir/${node}.bgp-status-summary.txt" 2>"$dir/${node}.bgp-status-summary.stderr" || true
    ssh_node "$node" 'if sudo test -S /run/routerd/bgp/control.sock; then sudo curl --silent --show-error --unix-socket /run/routerd/bgp/control.sock http://routerd-bgp/v1/applied | jq .; else echo "routerd_bgp_control_socket_missing"; fi' >"$dir/${node}.bgp-applied.json" 2>"$dir/${node}.bgp-applied.stderr" || true
    ssh_node "$node" 'if sudo test -S /run/routerd/bgp/control.sock; then sudo curl --silent --show-error --unix-socket /run/routerd/bgp/control.sock http://routerd-bgp/v1/applied | jq -r '"'"'
      "routerd_bgp_applied paths=\((.paths // []) | length) static=\((.paths // []) | map(select((.source // "") == "static")) | length) mobility=\((.paths // []) | map(select((.source // "") | startswith("MobilityPool/"))) | length)"
    '"'"'; else echo "routerd_bgp_applied unavailable"; fi' >"$dir/${node}.bgp-applied-summary.txt" 2>"$dir/${node}.bgp-applied-summary.stderr" || true
    ssh_node "$node" 'if command -v gobgp >/dev/null 2>&1; then sudo gobgp -u /run/routerd/bgp/gobgp.sock global rib -j 2>/dev/null | jq .; else echo "gobgp_cli_unavailable"; fi' >"$dir/${node}.gobgp-global-rib.json" 2>"$dir/${node}.gobgp-global-rib.stderr" || true
    ssh_node "$node" 'if command -v gobgp >/dev/null 2>&1; then sudo gobgp -u /run/routerd/bgp/gobgp.sock global rib -j 2>/dev/null | jq -r '"'"'
      if type == "array" then "gobgp_global_rib entries=\(length)" else "gobgp_global_rib entries=unknown" end
    '"'"'; else echo "gobgp_global_rib unavailable"; fi' >"$dir/${node}.gobgp-global-rib-summary.txt" 2>"$dir/${node}.gobgp-global-rib-summary.stderr" || true
  done
}

collect_success_optional_diagnostics() {
  local label="$1" phase_started
  if [ "$success_evidence_minimal" -eq 1 ]; then
    record_skipped_success_evidence diagnostics "$label"
    return 0
  fi
  phase_started="$SECONDS"
  collect_diagnostics "$label"
  record_timing "$label" diagnostics "$phase_started"
}

collect_success_optional_provider_inventory() {
  local label="$1" phase_started rc
  if [ "$success_evidence_minimal" -eq 1 ]; then
    record_skipped_success_evidence provider "$label"
    return 0
  fi
  phase_started="$SECONDS"
  collect_provider_inventory "$label" || rc=$?
  rc="${rc:-0}"
  record_timing "$label" provider-inventory "$phase_started"
  return "$rc"
}

collect_load_balance_report() {
  [ "$load_balance_report" -eq 1 ] || return 0
  local label="$1"
  local dir="$evidence_dir/diagnostics/load-balance-$label"
  local node
  mkdir -p "$dir"
  : >"$dir/owner-table.tsv"
  : >"$dir/owner-summary.tsv"
  : >"$dir/owner-site-summary.tsv"
  : >"$dir/owner-distribution.tsv"
  for node in "${leaf_routers[@]}"; do
    node_is_stopped "$node" && continue
    ssh_node "$node" 'sudo routerctl describe MobilityPool/cloudedge -o json' >"$dir/${node}.json" 2>"$dir/${node}.stderr" || continue
    jq -r --arg node "$node" '
      (.resource.status.ownershipResolverOwnerTable // [])[]
      | [
          $node,
          (.address // ""),
          (.ownerNodeRef // .owner // .ownerNode // ""),
          (.site // .ownerSite // ""),
          (.source // .reason // "")
        ]
      | @tsv
    ' "$dir/${node}.json" >>"$dir/owner-table.tsv" || true
  done
  if [ -s "$dir/owner-table.tsv" ]; then
    {
      echo $'observer\towner\tsite\tcount'
      awk -F '\t' '
      {
        owner = ($3 == "" ? "<unknown>" : $3)
        site = ($4 == "" ? "<unknown>" : $4)
        key = $1 "\t" owner "\t" site
        count[key]++
      }
      END {
        for (key in count) {
          print key "\t" count[key]
        }
      }
      ' "$dir/owner-table.tsv" | sort
    } >"$dir/owner-summary.tsv"
    {
      echo $'observer_site\towner_site\tcount'
      awk -F '\t' '
      function site_from_node(node) {
        if (node ~ /^aws-/) return "aws"
        if (node ~ /^azure-/) return "azure"
        if (node ~ /^oci-/) return "oci"
        if (node ~ /^pve-/) return "pve"
        if (node == "" || node == "<unknown>") return "<unknown>"
        return "other"
      }
      {
        observer_site = site_from_node($1)
        owner = ($3 == "" ? "<unknown>" : $3)
        owner_site = site_from_node(owner)
        key = observer_site "\t" owner_site
        count[key]++
      }
      END {
        for (key in count) {
          print key "\t" count[key]
        }
      }
      ' "$dir/owner-table.tsv" | sort
    } >"$dir/owner-site-summary.tsv"
    {
      echo $'owner_site\towner\tunique_addresses\tobserver_rows'
      awk -F '\t' '
      function site_from_node(node) {
        if (node ~ /^aws-/) return "aws"
        if (node ~ /^azure-/) return "azure"
        if (node ~ /^oci-/) return "oci"
        if (node ~ /^pve-/) return "pve"
        if (node == "" || node == "<unknown>") return "<unknown>"
        return "other"
      }
      {
        owner = ($3 == "" ? "<unknown>" : $3)
        owner_site = site_from_node(owner)
        owner_key = owner_site "\t" owner
        rows[owner_key]++
        addresses[owner_key SUBSEP $2] = 1
      }
      END {
        for (k in addresses) {
          split(k, parts, SUBSEP)
          unique[parts[1]]++
        }
        for (owner_key in rows) {
          print owner_key "\t" unique[owner_key] "\t" rows[owner_key]
        }
      }
      ' "$dir/owner-table.tsv" | sort
    } >"$dir/owner-distribution.tsv"
  fi
}

failover_transfer_pair() {
  local failed_node="$1"
  local failed_role failed_site dst="" src="" dst_site client
  failed_role="$(node_field "$failed_node" role)"
  failed_site="$(node_field "$failed_node" site)"
  if [ "$failed_role" = "leaf" ]; then
    for client in "${clients[@]}"; do
      [ "$(node_field "$client" site)" = "$failed_site" ] || continue
      dst="$client"
      break
    done
  fi
  [ -n "$dst" ] || dst="${clients[0]}"
  dst_site="$(node_field "$dst" site)"
  for client in "${clients[@]}"; do
    [ "$client" != "$dst" ] || continue
    [ "$(node_field "$client" site)" != "$dst_site" ] || continue
    src="$client"
    break
  done
  if [ -z "$src" ]; then
    for client in "${clients[@]}"; do
      [ "$client" = "$dst" ] || { src="$client"; break; }
    done
  fi
  printf '%s %s\n' "$src" "$dst"
}

start_failover_transfer() {
  [ "$failover_transfer_tests" -eq 1 ] || return 0
  local label="$1" failed_node="$2"
  local src dst src_ip dst_ip out remote_pid
  read -r src dst < <(failover_transfer_pair "$failed_node")
  [ -n "$src" ] && [ -n "$dst" ] || return 1
  src_ip="$(node_field "$src" private_ip)"
  dst_ip="$(node_field "$dst" private_ip)"
  out="$evidence_dir/failover-transfer/$label"
  mkdir -p "$out"
  {
    echo "label=$label"
    echo "failed_node=$failed_node"
    echo "src=$src"
    echo "src_ip=$src_ip"
    echo "dst=$dst"
    echo "dst_ip=$dst_ip"
    echo "url=http://$dst_ip:8080/failover-transfer.bin"
  } >"$out/metadata.txt"
  remote_pid="$(ssh_node "$src" "rm -f /tmp/routerd-${label}.log /tmp/routerd-${label}.bin; (date -u '+started=%Y-%m-%dT%H:%M:%SZ'; timeout 150s curl -fS --limit-rate 512k --connect-timeout 10 --max-time 150 -o /tmp/routerd-${label}.bin 'http://$dst_ip:8080/failover-transfer.bin'; rc=\$?; date -u '+finished=%Y-%m-%dT%H:%M:%SZ'; echo \"rc=\$rc\"; ls -l /tmp/routerd-${label}.bin 2>/dev/null || true; exit \$rc) >/tmp/routerd-${label}.log 2>&1 & echo \$!")"
  printf '%s %s\n' "$src" "$remote_pid"
}

finish_failover_transfer() {
  [ "$failover_transfer_tests" -eq 1 ] || return 0
  local label="$1" src="$2" remote_pid="$3"
  local out="$evidence_dir/failover-transfer/$label"
  [ -n "$src" ] && [ -n "$remote_pid" ] || return 1
  mkdir -p "$out"
  {
    echo "## wait remote transfer"
    ssh_node "$src" "deadline=\$((SECONDS + 170)); while kill -0 '$remote_pid' >/dev/null 2>&1 && [ \"\$SECONDS\" -lt \"\$deadline\" ]; do sleep 2; done; if kill -0 '$remote_pid' >/dev/null 2>&1; then echo still-running; kill '$remote_pid' >/dev/null 2>&1 || true; fi"
    echo "## transfer log"
    ssh_node "$src" "cat /tmp/routerd-${label}.log; rm -f /tmp/routerd-${label}.bin"
  } >"$out/result.txt" 2>&1 || return 1
  grep -q '^rc=0$' "$out/result.txt"
}

record_observed_failover_transfer() {
  local label="$1" result="$2"
  local out="$evidence_dir/failover-transfer/$label"
  mkdir -p "$out"
  {
    date -u '+timestamp=%Y-%m-%dT%H:%M:%SZ'
    echo "required=$failover_transfer_required"
    echo "result=$result"
    if [ "$result" != "PASS" ]; then
      echo "classification=observed-failure"
      echo "note=in-flight transfer did not complete; normal post-failover E2E is assessed separately by convergence/matrix/performance evidence"
    fi
  } >"$out/status.txt"
}

run_failover_transfer_smoke() {
  [ "$failover_transfer_smoke" -eq 1 ] || return 0
  local node src remote_pid
  node="${leaf_routers[0]}"
  read -r src remote_pid < <(start_failover_transfer "smoke" "$node")
  finish_failover_transfer "smoke" "$src" "$remote_pid"
}

run_failover() {
  local status=0
  local failover_node transfer_src transfer_pid validation_rc
  [ "${#failover_nodes[@]}" -gt 0 ] || return 0
  for failover_node in "${failover_nodes[@]}"; do
    collect_success_optional_diagnostics "before-failover-${failover_node}"
    collect_success_optional_provider_inventory "before-failover-${failover_node}" || status=1
    transfer_src=
    transfer_pid=
    if [ "$failover_transfer_tests" -eq 1 ]; then
      read -r transfer_src transfer_pid < <(start_failover_transfer "during-failover-${failover_node}" "$failover_node") || status=1
      sleep 3
    fi
    ssh_node "$failover_node" 'sudo systemctl stop routerd.service; if systemctl list-unit-files routerd-bgp.service --no-legend 2>/dev/null | grep -q "^routerd-bgp\\.service"; then sudo systemctl stop routerd-bgp.service; fi' >"$evidence_dir/convergence/failover-stop-${failover_node}.txt" 2>&1
    stopped_routers+=("$failover_node")
    validation_rc=0
    run_validation_set "after-failover-${failover_node}" || validation_rc=$?
    status="$(merge_validation_status "$status" "$validation_rc")"
    if [ "$failover_transfer_tests" -eq 1 ]; then
      if finish_failover_transfer "during-failover-${failover_node}" "$transfer_src" "$transfer_pid"; then
        record_observed_failover_transfer "during-failover-${failover_node}" PASS
      else
        record_observed_failover_transfer "during-failover-${failover_node}" FAIL
        [ "$failover_transfer_required" -eq 0 ] || status=1
      fi
    fi
    if [ "$validation_rc" -eq 0 ]; then
      collect_success_optional_diagnostics "after-failover-${failover_node}"
      collect_success_optional_provider_inventory "after-failover-${failover_node}" || status=1
    else
      collect_diagnostics "after-failover-${failover_node}"
      collect_provider_inventory "after-failover-${failover_node}" || status=1
    fi
  done
  return "$status"
}

run_rejoin() {
  local status=0 failover_node validation_rc
  [ "$rejoin_after_failover" -eq 1 ] || return 0
  [ "${#failover_nodes[@]}" -gt 0 ] || return 0
  for failover_node in "${failover_nodes[@]}"; do
    collect_success_optional_diagnostics "before-rejoin-${failover_node}"
    collect_success_optional_provider_inventory "before-rejoin-${failover_node}" || status=1
    ssh_node "$failover_node" 'if systemctl list-unit-files routerd-bgp.service --no-legend 2>/dev/null | grep -q "^routerd-bgp\\.service"; then sudo systemctl start routerd-bgp.service; sudo systemctl is-active routerd-bgp.service; fi; sudo systemctl start routerd.service; sudo systemctl is-active routerd.service' >"$evidence_dir/convergence/rejoin-start-${failover_node}.txt" 2>&1 || status=1
    mark_node_running "$failover_node"
    validation_rc=0
    run_validation_set "after-rejoin-${failover_node}" || validation_rc=$?
    status="$(merge_validation_status "$status" "$validation_rc")"
    if [ "$validation_rc" -eq 0 ]; then
      collect_success_optional_diagnostics "after-rejoin-${failover_node}"
      collect_success_optional_provider_inventory "after-rejoin-${failover_node}" || status=1
    else
      collect_diagnostics "after-rejoin-${failover_node}"
      collect_provider_inventory "after-rejoin-${failover_node}" || status=1
    fi
  done
  return "$status"
}

teardown() {
  [ -n "$destroy_cmd" ] || return 0
  bash -lc "$destroy_cmd" >"$evidence_dir/cleanup/destroy.txt" 2>&1
}

record_note
printf 'label\tstatus\telapsed_seconds\n' >"$evidence_dir/convergence/summary.tsv"
preflight || mark_failed "preflight"
if [ "$overall" -eq 0 ]; then
  collect_provider_inventory "preflight" || mark_failed "provider inventory preflight"
fi
if [ "$overall" -eq 0 ]; then
  setup_pve_dataplane || mark_failed "PVE dataplane IP setup"
fi
if [ "$overall" -eq 0 ]; then
  cfg_dir="$(generate_configs)" || mark_failed "config generation"
fi
if [ "$overall" -eq 0 ]; then
  validate_generated_configs "$cfg_dir" || mark_failed "config validation"
fi
if [ "$overall" -eq 0 ]; then
  deploy "$cfg_dir" || mark_failed "deploy"
fi
if [ "$overall" -eq 0 ]; then
  setup_client_ssh || mark_failed "client SSH setup"
fi
if [ "$overall" -eq 0 ]; then
  setup_legacy_protocol_services || mark_failed "legacy protocol service setup"
fi
if [ "$overall" -eq 0 ]; then
  setup_performance_services || mark_failed "performance service setup"
fi
if [ "$overall" -eq 0 ]; then
  setup_failover_transfer_services || mark_failed "failover transfer service setup"
fi
if [ "$overall" -eq 0 ]; then
  run_failover_transfer_smoke || mark_failed "failover transfer smoke"
fi
if [ "$overall" -eq 0 ]; then
  validation_started=1
  initial_validation_rc=0
  run_validation_set "initial" || initial_validation_rc=$?
  if [ "$initial_validation_rc" -eq 2 ]; then
    mark_failed "initial validation set PROVIDER-CONVERGENCE-FAIL"
  elif [ "$initial_validation_rc" -ne 0 ]; then
    mark_failed "initial validation set"
  fi
fi
if [ "$validation_started" -eq 1 ]; then
  if [ "$overall" -eq 0 ]; then
    collect_success_optional_diagnostics "post-matrix"
    collect_success_optional_provider_inventory "post-matrix" || mark_failed "provider inventory post-matrix"
  else
    collect_diagnostics "post-matrix"
    collect_provider_inventory "post-matrix" || mark_failed "provider inventory post-matrix"
  fi
fi
if [ "$overall" -eq 0 ]; then
  failover_status=0
  rejoin_status=0
  run_failover || failover_status=$?
  if [ "$rejoin_after_failover" -eq 1 ] && [ "${#failover_nodes[@]}" -gt 0 ]; then
    run_rejoin || rejoin_status=$?
  fi
  if [ "$failover_status" -eq 2 ]; then
    mark_failed "failover PROVIDER-CONVERGENCE-FAIL"
  elif [ "$failover_status" -ne 0 ]; then
    mark_failed "failover"
  fi
  if [ "$rejoin_status" -eq 2 ]; then
    mark_failed "rejoin PROVIDER-CONVERGENCE-FAIL"
  elif [ "$rejoin_status" -ne 0 ]; then
    mark_failed "rejoin"
  fi
fi
teardown || mark_failed "teardown"

echo "evidence: $evidence_dir"
exit "$overall"
