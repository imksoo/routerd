#!/usr/bin/env python3
"""Exercise real harness functions with synthetic SSH commands; no live mutations."""
import os
from pathlib import Path
import re
import shlex
import subprocess
import tempfile
import unittest

SOURCE = Path(__file__).with_name("sam-e2e.sh").read_text()

def function(name):
    start = SOURCE.index(name + "() {")
    end = SOURCE.index("\n}\n", start) + 3
    return SOURCE[start:end]

class EvidenceTests(unittest.TestCase):
    def run_shell(self, names, body):
        with tempfile.TemporaryDirectory() as tmp:
            script = "set -e\n" + "\n".join(function(n) for n in names) + "\n" + body
            result = subprocess.run(["bash", "-c", script], cwd=tmp,
                                    text=True, capture_output=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_failed_lock_wait_cannot_be_overwritten_by_apt_success(self):
        self.run_shell(["apt_update", "apt_install"], r'''
wait_for_apt() { return 17; }
sudo() { echo apt-ran >apt-marker; return 0; }
rc=0; apt_update || rc=$?
[ "$rc" = 17 ]
test ! -e apt-marker
rc=0; apt_install curl || rc=$?
[ "$rc" = 17 ]
test ! -e apt-marker
''')

    def test_apt_retries_only_transient_failure_and_keeps_attempts(self):
        self.run_shell(["apt_retry"], r"""
mktemp() { mkdir -p apt-log; printf '%s\n' "$PWD/apt-log"; }
sleep() { :; }
calls=0
operation() {
  n=$(cat calls 2>/dev/null || echo 0); n=$((n+1)); echo "$n" >calls
  if [ "$n" = 1 ]; then echo 'Could not get lock' >&2; return 100; fi
  echo apt-success
}
apt_retry operation
test "$(cat calls)" = 2
grep -q 'exit=100' apt-log/attempt-1.result.txt
grep -q 'Could not get lock' apt-log/attempt-1.stderr
grep -q 'exit=0' apt-log/attempt-2.result.txt
rm calls
operation() { echo called >>calls; echo 'Unable to locate package' >&2; return 100; }
rc=0; apt_retry operation || rc=$?
test "$rc" = 100
test "$(wc -l <calls)" = 1
""")

    def test_required_protocol_setup_failures_do_not_report_success(self):
        source = (Path(__file__).resolve().parents[4] / "scripts/runners/cloudedge-protocol-runner.sh").read_text()
        start = source.index("configure_server() {")
        end = source.index("\n}\n", start) + 3
        function_source = source[start:end].replace("/etc/vsftpd.conf", "missing-vsftpd.conf").replace("/tmp/cloudedge-iperf3.log", "iperf3.log")
        for failure in ["exportfs", "vsftpd", "iperf3", "none"]:
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as tmp:
                stub = r"""
set -e
sudo() {
  printf '%s\n' "$*" >>calls
  case "$FAILURE:$*" in
    exportfs:exportfs*) return 17 ;;
    vsftpd:*vsftpd*) return 27 ;;
  esac
  return 0
}
pkill() { :; }
iperf3() { echo iperf-start >>calls; [ "$FAILURE" != iperf3 ] || return 25; }
ce_client_ssh() { bash -c "$2"; }
export -f sudo pkill iperf3
"""
                env=dict(os.environ,FAILURE=failure)
                result=subprocess.run(["bash","-c",stub+function_source+'\nconfigure_server server 1024\necho setup-success\n'],cwd=tmp,env=env,text=True,capture_output=True)
                self.assertEqual(result.returncode, {"exportfs":17,"vsftpd":27,"iperf3":25,"none":0}[failure], result.stdout+result.stderr)
                self.assertEqual("setup-success" in result.stdout, failure=="none")
                if failure in ["exportfs","vsftpd"]:
                    self.assertNotIn("iperf-start", (Path(tmp)/"calls").read_text())

    def test_capture_keeps_nonzero_raw_outputs_and_timestamps(self):
        self.run_shell(["capture_ssh_evidence"], r"""
ssh_node() { echo raw-out; echo raw-error >&2; return 19; }
rc=0
capture_ssh_evidence leaf stdout stderr result.tsv ignored || rc=$?
[ "$rc" = 19 ]
[ "$(cat stdout)" = raw-out ]
[ "$(cat stderr)" = raw-error ]
awk -F '\t' 'NR==2 {exit !($1=="leaf" && $2~/T/ && $3~/T/ && $4==19)}' result.tsv
""")

    def test_required_cli_runs_in_service_path_and_missing_cli_stops_prepare(self):
        self.run_shell(["verify_provider_cli"], r"""
mkdir bin
ln -s /bin/sh bin/sh
sudo() {
  [ "$1" = env ]
  [ "$2" = PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin ]
  shift 2
  /usr/bin/env PATH="$PWD/bin" "$@"
}
ROUTERD_E2E_PROVIDER_CLI=oci
rc=0
verify_provider_cli >missing.out 2>missing.err || rc=$?
[ "$rc" = 127 ]
grep -q provider_cli=oci missing.out
for cli in aws az oci; do
  printf '#!/bin/sh\nprintf "args=%%s\\n" "$*"\n' >"bin/$cli"
  chmod +x "bin/$cli"
  ROUTERD_E2E_PROVIDER_CLI="$cli" verify_provider_cli >"$cli.out"
done
grep -q 'args=version --output json' az.out
grep -q 'args=--version' aws.out
grep -q 'args=--version' oci.out
""")

    def test_missing_guest_cli_aborts_before_product_deployment(self):
        prepare = SOURCE.split("cat <<'REMOTE_PREPARE'\n", 1)[1].split("\nREMOTE_PREPARE", 1)[0]
        with tempfile.TemporaryDirectory() as tmp:
            prepare = prepare.replace("/tmp/routerd-cloud-init-status.txt", "$PWD/cloud-init-status.txt")
            stub = r'''
cloud-init() { :; }
sudo() {
  case "$1" in
    cloud-init) echo "status: done"; return 0 ;;
    tail) return 0 ;;
    env) echo "synthetic oci executable unavailable" >&2; return 127 ;;
    *) echo "unexpected preparation command" >&2; return 99 ;;
  esac
}
ROUTERD_E2E_REQUIRE_AWS_CLI=0
ROUTERD_E2E_PROVIDER_CLI=oci
'''
            result = subprocess.run(["bash", "-c", stub + prepare + "\necho PRODUCT_INSTALL >product-marker\n"], cwd=tmp, capture_output=True, text=True)
            self.assertEqual(result.returncode, 127, result.stdout + result.stderr)
            self.assertFalse((Path(tmp) / "product-marker").exists())
            self.assertIn("status: done", result.stdout)
            self.assertIn("oci executable unavailable", result.stderr)

    def test_warmup_failure_is_evidence_without_changing_acceptance(self):
        self.run_shell(["capture_ssh_evidence", "warm_onprem_discovery"], r"""
evidence_dir="$PWD"; mkdir convergence
nodes_json="$PWD/nodes.json"
echo '{"client":{"site":"pve","role":"client","private_ip":"10.0.0.15"}}' >"$nodes_json"
pve_leaf_routers=(leaf); pve_capture_interface=ens19
node_is_stopped() { return 1; }
ssh_node() { bash -c "$2"; }
timeout() { shift; "$@"; }
ping() { echo synthetic-ping-failure >&2; return 1; }
ip() { echo synthetic-neighbor; }
export -f timeout ping ip
warm_onprem_discovery initial
grep -q 'destination=10.0.0.15.*exit=1' convergence/initial-leaf-onprem-warmup.txt
grep -q synthetic-ping-failure convergence/initial-leaf-onprem-warmup.stderr
""")

    def test_failed_route_predicate_keeps_target_and_real_route(self):
        self.run_shell(["capture_ssh_evidence", "wait_dataplane_control_gate"], r"""
evidence_dir="$PWD"; mkdir convergence
nodes_json="$PWD/nodes.json"
echo '{"a":{"site":"aws","role":"client","private_ip":"10.0.0.11"},"b":{"site":"oci","role":"client","private_ip":"10.0.0.13"}}' >"$nodes_json"
leaf_routers=(leaf)
node_field() { echo aws; }
node_is_stopped() { return 1; }
warm_onprem_discovery() { :; }
sleep() { SECONDS=1000; }
ssh_node() { bash -c "$2"; }
sudo() { "$@"; }
routerctl() {
  if [ "$1" = describe ]; then
    echo '{"resource":{"status":{"ownershipResolverControlPlaneOwnerTable":[{"address":"10.0.0.11/32"}]}}}'
  else echo '{}'; fi
}
ip() { echo "$4 dev ens5 src 10.0.0.4"; }
export -f sudo routerctl ip
rc=0; wait_dataplane_control_gate initial || rc=$?
[ "$rc" != 0 ]
grep -q 'CHECK route-via-sam target=10.0.0.13' convergence/initial-control/1-leaf-routes.stdout
grep -q 'dev ens5' convergence/initial-control/1-leaf-routes.stdout
awk -F '\t' 'NR==2 {exit !($4==1)}' convergence/initial-control/1-leaf-routes.result.tsv
""")

    def test_applied_curl_error_is_not_hidden_by_a_json_pipeline(self):
        self.run_shell(["capture_ssh_evidence", "collect_diagnostics"], r'''
evidence_dir="$PWD"; routers=(leaf)
ssh_node() {
  printf '%s\n' "$2" >>requested-commands
  if [[ "$2" == *'http://routerd-bgp/v1/applied'* ]]; then
    echo synthetic-curl-error >&2; return 22
  fi
  echo '{}'
}
collect_diagnostics failure
! grep -q 'http://routerd-bgp/v1/applied | jq' requested-commands
test ! -s diagnostics/failure/leaf.bgp-applied.json
grep -q synthetic-curl-error diagnostics/failure/leaf.bgp-applied.stderr
grep -q 'unavailable exit=22' diagnostics/failure/leaf.bgp-applied-summary.txt
awk -F '\t' 'NR==2 {exit !($4==22)}' diagnostics/failure/leaf.bgp-applied.result.tsv
test ! -e diagnostics/failure/leaf.bgp-applied-summary.stderr
''')

    def test_diagnostics_preserve_missing_gobgp_and_request_privileged_public_fields(self):
        self.run_shell(["capture_ssh_evidence", "collect_diagnostics"], r"""
evidence_dir="$PWD"; routers=(leaf)
ssh_node() {
  printf '%s\n' "$2" >>requested-commands
  if [[ "$2" == *'command -v gobgp'* ]]; then
    echo gobgp_cli_unavailable >&2; return 127
  fi
  echo '{}'
}
collect_diagnostics failure
grep -q 'sudo journalctl' requested-commands
grep -q 'latest-handshakes transfer endpoints allowed-ips' requested-commands
! grep -q 'sudo wg show all dump' requested-commands
test ! -s diagnostics/failure/leaf.gobgp-global-rib.json
grep -q gobgp_cli_unavailable diagnostics/failure/leaf.gobgp-global-rib.stderr
grep -q 'unavailable exit=127' diagnostics/failure/leaf.gobgp-global-rib-summary.txt
test ! -e diagnostics/failure/leaf.gobgp-global-rib-summary.stderr
""")

if __name__ == "__main__":
    unittest.main()
