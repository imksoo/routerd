import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class CleanupDriverTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.run_root = self.root / "runs/run-1"
        self.repo = self.run_root / "repo"
        self.framework = self.repo / "tools/release-qa-labs"
        self.drivers = self.framework / "drivers"
        self.runtime = self.run_root / "runtime"
        self.drivers.mkdir(parents=True)
        self.runtime.mkdir()
        for name in ("common.sh", "cleanup-driver.sh"):
            shutil.copy2(ROOT / "drivers" / name, self.drivers / name)

        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.calls = self.root / "tofu-calls"
        self.artifact = self.runtime / "artifact"
        self.artifact.write_bytes(b"artifact")
        self.tf = self.runtime / "tf"
        self.tf.mkdir()
        self.state = self.tf / "terraform.tfstate"
        self.tfvars = self.runtime / "terraform.tfvars"
        self.tfvars.write_text(
            'run_id = "run-1"\ncommit = "release-commit"\n'
            'pve_node_name = "pve01"\npve_ssh_host = "pve01.lain.local"\n'
            'pve_endpoint = "https://pve01.lain.local:8006/"\n',
            encoding="utf-8",
        )
        self.tfvars.chmod(0o600)
        key = self.runtime / "secrets/pve_ssh"
        key.parent.mkdir(mode=0o700)
        key.write_text("fixture\n", encoding="utf-8")
        key.chmod(0o600)
        token = self.runtime / "secrets/pve-token.tfvars"
        token.write_text('pve_api_token = "fixture-secret-token"\n', encoding="utf-8")
        token.chmod(0o600)
        self.run_env = self.runtime / "run.env.json"
        self.run_env.write_text(
            json.dumps({"pveSshPrivateKey": str(key), "pveTokenTfvars": str(token)}),
            encoding="utf-8",
        )
        self.run_env.chmod(0o600)
        self.contract_path = self.runtime / "contract.json"

        self.make("git", f'''case " $* " in
 *" rev-parse --show-toplevel "*) echo "{self.repo}";;
 *" rev-parse HEAD "*) echo qa-commit;;
esac
exit 0''')
        self.make("tofu", '''[ "${TF_VAR_pve_api_token:-}" = fixture-secret-token ] || exit 91
printf '%s|%s|token-set|%s\\n' "$TF_DATA_DIR" "$TF_CLI_CONFIG_FILE" "$*" >>"$CALLS"
case " $* " in
 *" output -json "*) echo '{}';;
esac
exit 0''')

    def tearDown(self):
        self.temp.cleanup()

    def make(self, name, body):
        path = self.bin / name
        path.write_text("#!/bin/sh\nset -eu\n" + body + "\n", encoding="utf-8")
        path.chmod(0o755)

    def write_contract(self, mode):
        contract = {
            "runId": "run-1",
            "execution": {"mode": mode},
            "labsCommit": "qa-commit",
            "routerdArtifact": {
                "path": str(self.artifact), "version": "v1", "commit": "release-commit",
            },
            "tofu": {
                "workingDirectory": str(self.tf), "statePath": str(self.state),
                "variablesPath": str(self.tfvars), "outputPath": str(self.tf / "output.json"),
            },
            "lifecycle": {"ttl": "75m", "heartbeatStale": "5m"},
            "pve": {"node": "pve01", "sshHost": "pve01.lain.local"},
        }
        self.contract_path.write_text(json.dumps(contract), encoding="utf-8")
        self.contract_path.chmod(0o600)

    def run_driver(self, mode):
        self.write_contract(mode)
        evidence = self.runtime / f"evidence/{mode}"
        environment = os.environ.copy()
        environment.update(
            PATH=f"{self.bin}:/usr/bin:/bin",
            CALLS=str(self.calls),
            ROUTERD_RELEASE_QA_PINNED_CONTRACT=str(self.contract_path),
            ROUTERD_RELEASE_QA_PINNED_RUN_ENV=str(self.run_env),
        )
        result = subprocess.run(
            [str(self.drivers / "cleanup-driver.sh"), "--run-id", "run-1", "--evidence-dir", str(evidence)],
            text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=environment, check=False,
        )
        calls = self.calls.read_text(encoding="utf-8") if self.calls.exists() else ""
        return result, evidence, calls

    def assert_confined_tofu_environment(self, calls):
        expected = f"{self.runtime}/tofu-data|{self.framework}/tofu.rc|token-set|"
        lines = calls.splitlines()
        self.assertTrue(lines)
        self.assertTrue(all(line.startswith(expected) for line in lines), lines)

    def test_staging_without_state_explicitly_skips_destroy(self):
        result, evidence, calls = self.run_driver("staging-no-mutation")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(calls, "")
        decision = json.loads((evidence / "cleanup-decision.json").read_text(encoding="utf-8"))
        self.assertEqual(decision["action"], "skip-tofu-destroy")
        self.assertEqual(decision["reason"], "staging-no-mutation-no-state")
        self.assertEqual((evidence / "tofu-state-after-destroy.txt").read_text(), "")

    def test_production_without_state_still_attempts_destroy(self):
        result, evidence, calls = self.run_driver("production")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("destroy -auto-approve -input=false", calls)
        self.assert_confined_tofu_environment(calls)
        self.assertEqual(json.loads((evidence / "cleanup-decision.json").read_text())["action"], "tofu-destroy")

    def test_staging_with_state_still_attempts_destroy(self):
        self.state.write_text('{"version": 4}\n', encoding="utf-8")
        result, evidence, calls = self.run_driver("staging-no-mutation")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("state list", calls)
        self.assertIn("destroy -auto-approve -input=false", calls)
        self.assert_confined_tofu_environment(calls)
        self.assertEqual(json.loads((evidence / "cleanup-decision.json").read_text())["action"], "tofu-destroy")

    def test_unknown_mode_fails_closed_without_destroy(self):
        result, _, calls = self.run_driver("unknown")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, "")
        self.assertIn("unsupported execution mode", result.stderr)

    def test_init_validate_and_destroy_share_run_confined_tofu_environment(self):
        self.write_contract("production")
        environment = os.environ.copy()
        environment.update(
            PATH=f"{self.bin}:/usr/bin:/bin",
            CALLS=str(self.calls),
            ROUTERD_RELEASE_QA_PINNED_CONTRACT=str(self.contract_path),
            ROUTERD_RELEASE_QA_PINNED_RUN_ENV=str(self.run_env),
        )
        result = subprocess.run(
            ["bash", "-c", f'''source "{self.drivers / 'common.sh'}"
load_contract "$ROUTERD_RELEASE_QA_PINNED_CONTRACT"
tofu -chdir="$tf_dir" init -input=false
tofu -chdir="$tf_dir" validate
tofu -chdir="$tf_dir" destroy -auto-approve -input=false'''],
            text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=environment, check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.calls.read_text(encoding="utf-8")
        self.assert_confined_tofu_environment(calls)
        commands = [line.split("|", 3)[3] for line in calls.splitlines()]
        self.assertEqual(
            commands,
            [f"-chdir={self.tf} init -input=false", f"-chdir={self.tf} validate",
             f"-chdir={self.tf} destroy -auto-approve -input=false"],
        )

    def test_drivers_do_not_bypass_the_tofu_wrapper(self):
        for name in ("cloud-certification-driver.sh", "cleanup-driver.sh"):
            source = (ROOT / "drivers" / name).read_text(encoding="utf-8")
            self.assertNotIn("env TF_CLI_CONFIG_FILE=", source, name)


if __name__ == "__main__":
    unittest.main()
