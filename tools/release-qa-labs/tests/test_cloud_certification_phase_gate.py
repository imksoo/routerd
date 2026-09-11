"""Run real certification/inventory drivers with strictly local fake I/O."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class CloudCertificationPhaseGateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.repo = self.root / "run-1/repo"
        self.framework = self.repo / "tools/release-qa-labs"
        self.drivers = self.framework / "drivers"
        self.runtime = self.root / "run-1/runtime"
        self.pinned = self.runtime / "pinned"
        self.bin = self.root / "bin"
        for directory in (self.drivers, self.pinned, self.bin, self.runtime / "tf"):
            directory.mkdir(parents=True)
        for name in ("common.sh", "cloud-certification-driver.sh", "inventory-driver.sh"):
            shutil.copy2(ROOT / "drivers" / name, self.drivers / name)
        shutil.copy2(ROOT / "qa_guard.py", self.framework / "qa_guard.py")
        self.state = self.runtime / "terraform.tfstate"
        self.calls = self.runtime / "calls"
        self.plan_marker = self.runtime / "cloud-plan-reached"
        self.populated = self.runtime / "pve-populated"
        artifact = self.runtime / "artifact"
        self.write(artifact, "fixture artifact")
        self.contract = self.pinned / "contract.json"
        self.write(self.contract, {
            "runId": "run-1", "qaImplementation": {"commit": "fixture-commit"},
            "routerdArtifact": {"path": str(artifact), "version": "fixture", "commit": "fixture-commit"},
            "tofu": {"workingDirectory": str(self.runtime / "tf"), "statePath": str(self.state),
                     "variablesPath": str(self.pinned / "terraform.tfvars"),
                     "outputPath": str(self.runtime / "tofu-output.json")},
            "lifecycle": {"ttl": "115m", "heartbeatStale": "5m"},
            "limits": {"maxEstimatedCostUsd": 1.60},
            "pve": {"node": "pve-test", "sshHost": "pve-test.example.test", "captureBridge": "vmbr999",
                    "templateStage": {"vmid": 100},
                    "vmids": {name: 101 + i for i, name in enumerate((
                        "pve-leaf-a", "pve-client-a", "pve-leaf-b", "pve-client-b", "pve-rr-a", "pve-rr-b"))}},
        })
        self.write(self.pinned / "terraform.tfvars", '\n'.join((
            'run_id = "run-1"', 'commit = "fixture-commit"',
            'aws_profile = "fixture"', 'aws_region = "ap-northeast-1"',
            'oci_profile = "fixture"', 'oci_region = "ap-tokyo-1"', 'oci_compartment_id = "ocid.fixture"',
            'pve_node_name = "pve-test"', 'pve_ssh_host = "pve-test.example.test"',
            'pve_endpoint = "https://pve-test.example.test:8006/"',
        )) + '\n')
        for name, content in {
            "pve_ssh": "fake PVE key", "guest_ssh": "different fake guest key",
            "pve-known_hosts": "fixture host key", "pve-token.tfvars": 'pve_api_token = "fixture"\n',
            "pve-ca.pem": "-----BEGIN CERTIFICATE-----\nfixture\n-----END CERTIFICATE-----\n",
        }.items():
            self.write(self.pinned / name, content)
        self.write(self.pinned / "run.env.json", {
            "releaseRepo": str(self.repo), "httpsProxy": "http://127.0.0.1:18081",
            "pveSshPrivateKey": str(self.pinned / "pve_ssh"),
            "guestSshPrivateKey": str(self.pinned / "guest_ssh"),
            "pveSshKnownHosts": str(self.pinned / "pve-known_hosts"),
        })
        self.supervisor_state = self.runtime / "evidence/lifecycle/supervisor-state.json"
        self.supervisor_state.parent.mkdir(parents=True)
        self.write(self.supervisor_state, {"runId": "run-1", "phase": "MUTATING", "mutationPgid": 1234})
        self.stub("git", '''case " $* " in
 *" rev-parse --show-toplevel "*) printf '%s\\n' "$MOCK_REPO";;
 *" rev-parse HEAD "*) echo fixture-commit;;
 *" status "*) :;;
 *) exit 97;;
esac''')
        self.stub("tofu", '''printf 'tofu %s\\n' "$*" >>"$MOCK_CALLS"
case " $* " in
 *" state list "*) cat "$MOCK_STATE";;
 *" plan "*" -out="*) touch "$MOCK_CLOUD_PLAN"; exit 42;;
 *" providers "*) echo 'provider[registry.opentofu.org/oracle/oci]';;
 *" version "*) echo fixture;;
 *" init "*|*" validate "*|*" plan "*" -refresh-only "*|*" apply -help "*) :;;
 *) echo 'unexpected or mutating tofu invocation' >&2; exit 97;;
esac''')
        self.stub("aws", '''case " $* " in
 *" ec2 describe-instances "*) echo '{"Reservations":[]}';;
 *" resourcegroupstaggingapi get-resources "*) echo '{"ResourceTagMappingList":[]}';;
 *" sts get-caller-identity "*) echo '{}';;
 *" --version "*) echo fixture;;
 *) exit 97;;
esac''')
        self.stub("az", '''case " $* " in
 *" group exists "*) echo false;;
 *" account show "*) echo '{}';;
 *" version "*) echo '{"azure-cli":"fixture"}';;
 *) exit 97;;
esac''')
        self.stub("oci", '''case " $* " in
 *" iam region-subscription list "*|*" compute instance list "*) echo '{"data":[]}';;
 *" search resource structured-search "*) echo '{"data":{"items":[]}}';;
 *" --version "*) echo fixture;;
 *) exit 97;;
esac''')
        self.stub("ssh", '''case " $* " in
 *"pvesh get /cluster/resources "*)
   if [ -e "$MOCK_POPULATED" ]; then echo '[{"vmid":100},{"vmid":101},{"vmid":102},{"vmid":103},{"vmid":104},{"vmid":105},{"vmid":106}]'; else echo '[]'; fi;;
 *"pvesh get /nodes/"*) echo '[]';;
 *"ip -j -d link show"*)
   if [ -e "$MOCK_POPULATED" ]; then echo '[{"ifname":"vmbr999","linkinfo":{"info_kind":"bridge"}}]'; else echo '[]'; fi;;
 *) exit 97;;
esac''')
        # Accelerate polling only. The real run_with_progress and wait still run.
        self.stub("sleep", '/bin/sleep 0.01')
        preflight = self.repo / "tests/e2e/cloudedge/scripts/sam-preflight.sh"
        preflight.parent.mkdir(parents=True)
        self.write(preflight, "#!/bin/sh\nexit 0\n")
        preflight.chmod(0o755)
        self.environment = os.environ.copy()
        self.environment.update({
            "PATH": f"{self.bin}:/usr/bin:/bin", "MOCK_REPO": str(self.repo),
            "MOCK_STATE": str(self.state), "MOCK_CALLS": str(self.calls),
            "MOCK_POPULATED": str(self.populated), "MOCK_CLOUD_PLAN": str(self.plan_marker),
        })
        for key, name in {
            "CONTRACT": "contract.json", "RUN_ENV": "run.env.json", "TFVARS": "terraform.tfvars",
            "PVE_SSH_PRIVATE_KEY": "pve_ssh", "GUEST_SSH_PRIVATE_KEY": "guest_ssh",
            "PVE_SSH_KNOWN_HOSTS": "pve-known_hosts", "PVE_TOKEN_TFVARS": "pve-token.tfvars",
            "PVE_CA_PEM": "pve-ca.pem",
        }.items():
            self.environment[f"ROUTERD_RELEASE_QA_PINNED_{key}"] = str(self.pinned / name)
        self.set_pve_populated(True)

    def tearDown(self):
        self.temp.cleanup()

    def write(self, path, value):
        path.write_text(json.dumps(value) if isinstance(value, dict) else value, encoding="utf-8")
        path.chmod(0o600)

    def stub(self, name, body):
        path = self.bin / name
        self.write(path, "#!/bin/sh\nset -eu\n" + body + "\n")
        path.chmod(0o755)

    def set_pve_populated(self, present):
        if present:
            self.populated.touch()
            self.write(self.state, "proxmox_virtual_environment_vm.pve_shared_template_stage\n"
                       + "\n".join(f'module.pve.proxmox_virtual_environment_vm.node["{i}"]' for i in range(6))
                       + "\nmodule.pve_rr.terraform_data.rr_topology\n")
        else:
            self.populated.unlink(missing_ok=True)
            self.write(self.state, "")

    def run_driver(self, name, *args, environment=None):
        return subprocess.run([str(self.drivers / name), *args], env=environment or self.environment,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=20, check=False)

    def run_cloud(self, environment=None):
        result_path = self.runtime / "cloud-driver-result.json"
        result = self.run_driver("cloud-certification-driver.sh", "--contract", str(self.contract),
                                 "--out", str(result_path), environment=environment)
        return result, json.loads(result_path.read_text()) if result_path.exists() else {}

    def test_cloud_plan_accepts_the_already_certified_pve_phase(self):
        result, report = self.run_cloud()
        self.assertTrue(self.plan_marker.exists(),
                        f"cloud plan was blocked before its fake I/O boundary: {report.get('notes')}; {result.stderr}")
        # The fake plan deliberately fails before any apply. Reaching it, not
        # claiming a complete cloud certification, is this regression's AC.
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(report["notes"], "targeted cloud OpenTofu plan failed")
        self.assertTrue(self.populated.exists())
        self.assertEqual(len(self.state.read_text().splitlines()), 8)
        self.assertNotIn("apply -input=false", self.calls.read_text())

    def test_baseline_and_final_inventory_still_require_all_seven_scopes_zero(self):
        for populated in (True, False):
            self.set_pve_populated(populated)
            for phase in ("baseline", "final"):
                with self.subTest(populated=populated, phase=phase):
                    evidence = self.runtime / f"evidence/{phase}-{populated}"
                    result = self.run_driver("inventory-driver.sh", "--run-id", "run-1", "--evidence-dir", str(evidence))
                    scopes = {item["name"]: item for item in json.loads((evidence / "inventory.json").read_text())["scopes"]}
                    self.assertEqual(len(scopes), 7)
                    self.assertTrue(all(item["queryStatus"] == "complete" for item in scopes.values()))
                    self.assertEqual(scopes["pve-vms"]["count"], 7 if populated else 0)
                    self.assertEqual(scopes["pve-bridges"]["count"], 1 if populated else 0)
                    self.assertEqual(scopes["tofu-state"]["count"], 8 if populated else 0)
                    self.assertEqual(result.returncode == 0, not populated, result.stderr)
                    if populated:
                        self.assertIn("inventory is not exhaustive and zero", result.stderr)

    def test_cloud_keeps_supervisor_and_pinned_input_guards(self):
        self.write(self.supervisor_state, {"runId": "run-1", "phase": "PRECHECK", "mutationPgid": 1234})
        result, report = self.run_cloud()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does not own the active mutation phase", result.stderr)
        self.assertFalse(self.plan_marker.exists())
        self.write(self.supervisor_state, {"runId": "run-1", "phase": "MUTATING", "mutationPgid": 1234})
        environment = {**self.environment, "ROUTERD_RELEASE_QA_PINNED_PVE_TOKEN_TFVARS": str(self.runtime / "wrong-token")}
        result, _ = self.run_cloud(environment)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("PVE token source is not the pinned canonical input", result.stderr)
        self.assertFalse(self.plan_marker.exists())


if __name__ == "__main__":
    unittest.main()
