#!/usr/bin/env bash
set -euo pipefail

framework_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
generator="$framework_root/../../tests/e2e/cloudedge/configs/sam-e2e-generate.sh"
work="$(mktemp -d "${TMPDIR:-/tmp}/routerd-qualification-deadline.XXXXXX")"
cleanup() {
  # These are only the fake profile processes created by this test. The real
  # driver must leave live children to its durable supervisor, not kill them.
  local marker pid command_line
  for marker in "$work"/*/profile.pid; do
    [ -f "$marker" ] || continue
    read -r pid <"$marker"
    [ -r "/proc/$pid/cmdline" ] || continue
    command_line="$(tr '\0' ' ' <"/proc/$pid/cmdline")"
    case "$command_line" in
      *"$work/profile.sh"*) kill "$pid" 2>/dev/null || true ;;
    esac
  done
  find "$work" -depth -delete
}
trap cleanup EXIT
mkdir -p "$work/drivers" "$work/bin"
cp "$framework_root/drivers/qualification-driver.sh" "$work/drivers/qualification-driver.sh"

# Execute the complete real driver, including its actual wait loop, with only
# common.sh's contract/environment boundary and the profile executable mocked.
# Interface cases also invoke the real config generator with synthetic QGA
# output and fake WireGuard keys. No real credential, provider, SSH, systemd,
# or network operation is available.
cat >"$work/drivers/common.sh" <<'COMMON'
set -euo pipefail
default_contract_path="$QUALIFICATION_TEST_CASE/contract.json"
load_contract() {
  contract_path="$1"
  artifact_version=test-release run_id=test-run
  evidence_root="$QUALIFICATION_TEST_CASE/evidence"
  active_pid_file="$QUALIFICATION_TEST_CASE/active.pid"
  tofu_output_path="$QUALIFICATION_TEST_ROOT/tofu-output.json"
  artifact_path=unused tfvars_path=unused
  guest_ssh_private_key=unused pve_ssh_private_key=unused pve_ssh_known_hosts=unused
}
die() { echo "$*" >&2; exit 2; }
absolute_path() { printf '%s\n' "$1"; }
routerd_script() { printf '%s\n' "$QUALIFICATION_TEST_ROOT/profile.sh"; }
utc_now() { date -u +%Y-%m-%dT%H:%M:%SZ; }
COMMON
cat >"$work/profile.sh" <<'PROFILE'
#!/usr/bin/env bash
set -euo pipefail
while [ "$#" -gt 0 ]; do
  case "$1" in
    --evidence-root) evidence_root="$2" ;;
    --tofu-output) tofu_output="$2" ;;
  esac
  shift 2
done
printf '%s\n' "$$" >"$QUALIFICATION_TEST_CASE/profile.pid"
cp "$QUALIFICATION_TEST_CASE/pass.json" "$evidence_root/profile-result.json"
case "$QUALIFICATION_TEST_MODE" in
  ifnames-*)
    jq -n --arg management "${PVE_MANAGEMENT_INTERFACE-}" \
      --arg capture "${PVE_CAPTURE_INTERFACE-}" \
      --arg clientCapture "${PVE_CLIENT_CAPTURE_INTERFACE-}" \
      '{management:$management,capture:$capture,clientCapture:$clientCapture}' \
      >"$QUALIFICATION_TEST_CASE/profile-env.json"
    PATH="$QUALIFICATION_TEST_ROOT/bin:$PATH" bash "$QUALIFICATION_TEST_GENERATOR" \
      --tofu-output "$tofu_output" --out-dir "$QUALIFICATION_TEST_CASE/generated"
    exit 0 ;;
  pass) exit 0 ;;
  fail) exit 9 ;;
  hang)
    # Progress keeps the heartbeat fresh, so a stale-heartbeat timeout cannot
    # substitute for the qualification deadline. A valid PASS file also cannot
    # substitute for a completed, timely profile process.
    while :; do printf 'still-running\n'; sleep 0.1; done ;;
esac
PROFILE
chmod +x "$work/profile.sh"
# Reuse the generator offline test's deterministic key boundary. Only these
# local key-generation operations are permitted by the fake command.
cat >"$work/bin/wg" <<'WG'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  genkey) printf '%s\n' 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=' ;;
  pubkey) cat >/dev/null; printf '%s\n' 'BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=' ;;
  *) exit 2 ;;
esac
WG
chmod +x "$work/bin/wg"
# Same QGA-certified PVE fixture shape as sam-e2e-generate-offline-test.sh;
# both RR and leaf configs must remain passive on the management underlay.
jq -n '{
  nodes:{value:{
    "pve-rr-a":{role:"rr",site:"pve",overlay_ip:"10.99.0.21",
      management_ip:"192.0.2.21",pve_management_source:"qga-dhcp"},
    "pve-rr-b":{role:"rr",site:"pve",overlay_ip:"10.99.0.22",
      management_ip:"192.0.2.22",pve_management_source:"qga-dhcp"},
    "pve-leaf-a":{role:"leaf",site:"pve",overlay_ip:"10.99.0.31",
      private_ip:"10.77.60.31",management_ip:"192.0.2.31",
      pve_management_source:"qga-dhcp",capture_mac:"02:00:00:00:00:31"},
    "pve-leaf-b":{role:"leaf",site:"pve",overlay_ip:"10.99.0.32",
      private_ip:"10.77.60.32",management_ip:"192.0.2.32",
      pve_management_source:"qga-dhcp",capture_mac:"02:00:00:00:00:32"}
  }},
  fabric:{value:{mobility_prefix:"10.77.60.0/24",tunnel_inner_prefix:"10.99.0.0/24",
    bgp_asn:64512,wg_port:51820}}
}' >"$work/tofu-output.json"
jq -n '{
  profile:"representative-redundancy",result:"pass",limits:{maxRuntimeSeconds:1},
  topology:{routerCount:10,clientCount:8,cloudClientCount:6,rrFaultDomain:"host-redundant"},
  gates:{rrAStaged:true,rrAJoined:true,rrBStaged:true,rrBJoined:true,rrPairReady:true,
    fullBaseline:true,directedClientMatrix:true,directedCloudIngressMatrix:true,providerReadiness:true,
    rrAFailover:true,rrBControlPlaneContinuity:true,rrBContinuityCanary:true,rrARejoin:true,
    edgeAFailover:true,edgeARejoin:true,edgeAClientMatrix:true,edgeACloudIngressMatrix:true,
    legacyProtocols:false,performance:false,symmetricBFailover:false,provisioning:false,destruction:false},
  edgeScenarios:(["aws-leaf-a","azure-leaf-a","oci-leaf-a","pve-leaf-a"] | map({key:.,value:{
    result:"pass",failoverEvidenceExit:0,rejoinEvidenceExit:0,e2eExit:0}}) | from_entries)
}' >"$work/pass.json"

verify_interfaces() {
  local case_dir="$1" node config
  jq -e '. == {management:"eth0",capture:"ens19",clientCapture:"eth1"}' \
    "$case_dir/profile-env.json" >/dev/null || {
      echo "FAIL: qualification driver did not pin management=eth0 capture=ens19 clientCapture=eth1 (${case_dir##*/})" >&2
      return 1
    }
  for node in pve-leaf-a pve-leaf-b; do
    config="$case_dir/generated/configs/$node.yaml"
    # Check the named resources, not a matching ifname elsewhere in the file.
    awk '
      /^    - apiVersion:/ { resource = "" }
      /metadata: \{ name: mgmt \}/ { resource = "mgmt" }
      /metadata: \{ name: capture \}/ { resource = "capture" }
      /^    protectedInterfaces:/ { protected_list = 1; next }
      protected_list && /^      - mgmt$/ { protected_mgmt = 1 }
      protected_list && !/^      -/ { protected_list = 0 }
      resource == "mgmt" && $1 == "ifname:" && $2 == "eth0" { management = 1 }
      resource == "mgmt" && $1 == "managed:" && $2 == "false" { unmanaged = 1 }
      resource == "mgmt" && $1 == "owner:" && $2 == "external" { external = 1 }
      resource == "capture" && $1 == "ifname:" && $2 == "ens19" { capture = 1 }
      END { exit !(management && unmanaged && external && capture && protected_mgmt) }
    ' "$config" || {
      echo "FAIL: $node management/capture interface or passive ownership changed" >&2
      return 1
    }
    [ "$(grep -Ec '^[[:space:]]+interface: ens19$' "$config")" -eq 3 ] || {
      echo "FAIL: $node capture/ARP discovery does not consistently use ens19" >&2
      return 1
    }
  done
  for node in pve-rr-a pve-rr-b pve-leaf-a pve-leaf-b; do
    config="$case_dir/generated/configs/$node.yaml"
    [ -s "$config" ]
    if grep -Eq '^[[:space:]]*kind:[[:space:]]*(DHCPv[46][[:alnum:]]*|IPv6RAAddress|IPv6RouterAdvertisement|RouterAdvertisement)[[:space:]]*$' "$config"; then
      echo "FAIL: $node generated a DHCP or RA control-plane resource" >&2
      return 1
    fi
  done
}

run_case() {
  local mode="$1" started rc=0 budget=10 case_dir="$work/$1"
  local -a interface_env=()
  case "$mode" in
    ifnames-unset)
      interface_env=(-u PVE_MANAGEMENT_INTERFACE -u PVE_CAPTURE_INTERFACE -u PVE_CLIENT_CAPTURE_INTERFACE) ;;
    ifnames-ambient)
      interface_env=(PVE_MANAGEMENT_INTERFACE=ambient-mgmt PVE_CAPTURE_INTERFACE=ambient-capture
        PVE_CLIENT_CAPTURE_INTERFACE=ambient-client) ;;
  esac
  [ "$mode" != hang ] || budget=1
  mkdir -p "$case_dir/evidence/commands"
  jq -n --argjson budget "$budget" '{qualification:{profile:"representative-redundancy",qualificationBudgetSeconds:$budget},
    safety:{pveManagementControlPlane:"none"}}' >"$case_dir/contract.json"
  jq --argjson budget "$budget" '.limits.maxRuntimeSeconds=$budget' "$work/pass.json" >"$case_dir/pass.json"
  jq -n '{run:{runId:"test-run"}}' >"$case_dir/certification.json"
  started="$SECONDS"
  env "${interface_env[@]}" QUALIFICATION_TEST_ROOT="$work" QUALIFICATION_TEST_CASE="$case_dir" \
    QUALIFICATION_TEST_MODE="$mode" QUALIFICATION_TEST_GENERATOR="$generator" \
    timeout --kill-after=1s 7s bash "$work/drivers/qualification-driver.sh" \
      --certification "$case_dir/certification.json" --release test-release \
      --out "$case_dir/result.json" --heartbeat "$case_dir/heartbeat" \
      >"$case_dir/driver.log" 2>&1 || rc=$?
  if [ "$rc" -eq 124 ] || [ "$rc" -eq 137 ]; then
    echo "FAIL: actual qualification driver wait loop exceeded its deadline test bound ($mode, budget=$budget)" >&2
    return 1
  fi
  if [ "$mode" = pass ] || [[ "$mode" == ifnames-* ]]; then
    [ "$rc" -eq 0 ]
    jq -e '.status == "pass" and .checks[0].result == "pass"' "$case_dir/result.json" >/dev/null
  else
    [ "$rc" -ne 0 ]
    jq -e '.status == "fail" and .checks[0].result == "fail"' "$case_dir/result.json" >/dev/null
  fi
  if [ "$mode" = hang ]; then
    [ "$((SECONDS - started))" -le 3 ] || {
      echo "FAIL: driver did not return promptly at qualification deadline" >&2; return 1;
    }
    jq -e '.checks[0].summary | contains("deadline")' "$case_dir/result.json" >/dev/null
    [ -s "$case_dir/active.pid" ]
    cmp "$case_dir/active.pid" "$case_dir/profile.pid"
    local pid
    read -r pid <"$case_dir/active.pid"
    kill -0 "$pid" # Still owned by the supervisor, not killed by the driver.
  else
    [ ! -e "$case_dir/active.pid" ]
  fi
  if [[ "$mode" == ifnames-* ]]; then
    verify_interfaces "$case_dir"
  fi
}

run_case hang
run_case pass
run_case fail
run_case ifnames-unset
run_case ifnames-ambient
echo "Qualification deadline, supervisor handoff, and PVE interface generation offline checks: PASS"
