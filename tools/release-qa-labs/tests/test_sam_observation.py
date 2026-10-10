"""Exercise the real SAM caller with local command doubles; no network or VMs."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


REPO = Path(__file__).resolve().parents[3]
HARNESS = REPO / "tests/e2e/cloudedge/scripts/sam-e2e.sh"


def functions(first, following):
    source = HARNESS.read_text()
    return source.split(first + "() {", 1)[1].split(following + "() {", 1)[0].join(
        [first + "() {", ""])


class SAMObservationTests(unittest.TestCase):
    def run_bash(self, source, *, fixture="healthy"):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "bin").mkdir()
            # These executables run inside the actual remote command wrapper.
            for name, body in {
                "ping": '''echo ping >>"$FIXTURE_LOG"
n=$(wc -l <"$FIXTURE_LOG")
if [ "$FIXTURE" = ping-fails ] || { [ "$FIXTURE" = ping-recovers ] && [ "$n" -eq 1 ]; }; then exit 1; fi
''',
                "ssh": '''echo hostname >>"$FIXTURE_LOG"
if [ "$FIXTURE" = wrong-host ]; then echo wrong-host; else echo client-b; fi
''',
                "ip": 'echo route >>"$FIXTURE_LOG"; echo "192.0.2.2 dev samt0"\n',
                "traceroute": 'echo traceroute >>"$FIXTURE_LOG"\n',
                "tracepath": 'echo tracepath >>"$FIXTURE_LOG"\n',
            }.items():
                path = root / "bin" / name
                path.write_text("#!/bin/sh\n" + body)
                path.chmod(0o755)
            environment = dict(os.environ, PATH=str(root / "bin") + ":/usr/bin:/bin",
                               FIXTURE=fixture, FIXTURE_LOG=str(root / "calls"), TEST_ROOT=str(root))
            script = '''set -euo pipefail
evidence_dir="$TEST_ROOT/evidence"
mkdir -p "$evidence_dir/convergence" "$evidence_dir/deploy" "$evidence_dir/matrix"
touch "$FIXTURE_LOG"
SECONDS=0
validation_deadline=60
flow_evidence_dir="$evidence_dir/matrix/initial/flows"
mkdir -p "$flow_evidence_dir"
stopped_routers=()
node_field() {
 case "$2" in private_ip) [ "$1" = client-a ] && echo 192.0.2.1 || echo 192.0.2.2;; name) echo "$1";; ssh_user) echo fixture;; site) echo aws;; esac
}
ssh_node() {
 if [ "$FIXTURE" = management-missing ]; then return 255; fi
 bash -c "$2"
}
sleep() { SECONDS=$((SECONDS + $1)); }
''' + source
            result = subprocess.run(["bash", "-c", script], text=True, capture_output=True,
                                    env=environment, timeout=15)
            files = {str(p.relative_to(root)): p.read_text() for p in root.rglob("*")
                     if p.is_file() and p.parent.name != "bin"}
            return result, files

    def flow_source(self):
        # Actual caller extraction, following the repository's shell fixture pattern.
        source = HARNESS.read_text()
        if "observe_flow() {" not in source:
            self.fail("SAM caller has no bounded per-flow observation path")
        return functions("observe_flow", "transition_canary_matrix")

    def test_provider_readiness_precedes_traffic(self):
        source = functions("run_validation_set", "collect_diagnostics") + '''
staged_rr_pair=(); skip_matrix=0; transition_canary=0; success_evidence_minimal=1
record_timing() { :; }; record_skipped_success_evidence() { :; }
wait_dataplane_control_gate() { echo control >>"$FIXTURE_LOG"; }
wait_provider_gate() { echo provider >>"$FIXTURE_LOG"; }
client_matrix() { echo traffic >>"$FIXTURE_LOG"; }
router_origin_matrix() { :; }; cloud_ingress_matrix() { :; }
legacy_protocol_matrix() { :; }; performance_matrix() { :; }; collect_load_balance_report() { :; }
collect_convergence_snapshot() { :; }
run_validation_set initial
'''
        result, files = self.run_bash(source)
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = files["calls"].splitlines()
        self.assertLess(calls.index("provider"), calls.index("traffic"))

    def test_healthy_and_duplicate_flow_share_observation_without_diagnostics(self):
        result, files = self.run_bash(self.flow_source() + '''
observe_flow initial client-a client-b client
observe_flow initial client-a client-b client
''')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(files["calls"].splitlines(), ["ping", "hostname"])

    def test_retry_only_failed_component_retains_previous_failure(self):
        result, files = self.run_bash(self.flow_source() + '''
observe_flow initial client-a client-b client
''', fixture="ping-recovers")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(files["calls"].splitlines(), ["ping", "hostname", "ping"])
        attempts = files["evidence/matrix/initial/flows/client-client-a-client-b/attempts.tsv"]
        self.assertIn("FAIL_PING", attempts)
        self.assertIn("PASS", attempts)

    def test_management_failure_is_inconclusive_not_product_ping_failure(self):
        result, files = self.run_bash(self.flow_source() + '''
observe_flow initial client-a client-b client
''', fixture="management-missing")
        self.assertEqual(result.returncode, 3, result.stderr)
        status = files["evidence/matrix/initial/flows/client-client-a-client-b/status"]
        self.assertEqual(status.strip(), "OBSERVATION_INCONCLUSIVE")
        self.assertEqual(files["calls"], "")

    def test_observed_ping_failure_is_product_failure_and_diagnostics_only_on_failure(self):
        result, files = self.run_bash(self.flow_source() + '''
observe_flow initial client-a client-b client
''', fixture="ping-fails")
        self.assertEqual(result.returncode, 1, result.stderr)
        status = files["evidence/matrix/initial/flows/client-client-a-client-b/status"]
        self.assertEqual(status.strip(), "FAIL_PING")
        self.assertEqual(files["calls"].splitlines().count("hostname"), 1)
        self.assertEqual(files["calls"].splitlines().count("traceroute"), 1)

    def test_expired_shared_deadline_does_not_send(self):
        result, files = self.run_bash(self.flow_source() + '''
validation_deadline=0
observe_flow initial client-a client-b client
''')
        self.assertEqual(result.returncode, 3, result.stderr)
        self.assertEqual(files["calls"], "")


if __name__ == "__main__":
    unittest.main()
