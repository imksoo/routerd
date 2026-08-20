import hashlib
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
        # The bridge boundary has its own fake-SSH test.  Here a tiny exact
        # stub keeps this cleanup-order test focused on destroy-before-bridge
        # invocation rather than recreating a PVE API fixture.
        bridge_driver = self.drivers / "pve-capture-bridge.sh"
        bridge_driver.write_text(
            "#!/bin/sh\nset -eu\nprintf '%s\\n' bridge-remove >>\"$ORDER_CALLS\"\n",
            encoding="utf-8",
        )
        bridge_driver.chmod(0o755)
        # The exact PVE-identity logic has dedicated offline coverage.  This
        # fixture keeps cleanup ordering focused on OpenTofu destroy followed
        # by the recovery boundary and then bridge removal.
        orphan_driver = self.drivers / "pve-orphan-cleanup.sh"
        orphan_driver.write_text(
            "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$*\" >>\"$ORPHAN_CALLS\"\nprintf '%s\\n' orphan-recovery >>\"$ORDER_CALLS\"\n",
            encoding="utf-8",
        )
        orphan_driver.chmod(0o755)

        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.calls = self.root / "tofu-calls"
        self.orphan_calls = self.root / "orphan-calls"
        self.order_calls = self.root / "cleanup-order-calls"
        self.init_marker = self.root / "tofu-initialized"
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
        guest_key = self.runtime / "secrets/guest_ssh"
        guest_key.write_text("fixture guest key\n", encoding="utf-8")
        guest_key.chmod(0o600)
        known_hosts = self.runtime / "secrets/pve-known_hosts"
        known_hosts.write_text(
            "pve01.lain.local ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEZpeHR1cmUtcHZlLWhvc3Qta2V5\n",
            encoding="utf-8",
        )
        known_hosts.chmod(0o600)
        self.pinned_known_hosts = self.runtime / "pinned/pve-known_hosts"
        self.pinned_known_hosts.parent.mkdir(mode=0o700)
        self.pinned_known_hosts.write_text(known_hosts.read_text(encoding="utf-8"), encoding="utf-8")
        self.pinned_known_hosts.chmod(0o600)
        token = self.runtime / "secrets/pve-token.tfvars"
        token.write_text('pve_api_token = "mutable-source-token"\n', encoding="utf-8")
        token.chmod(0o600)
        self.pinned_token = self.runtime / "pinned/pve-token.tfvars"
        self.pinned_token.parent.mkdir(mode=0o700, exist_ok=True)
        self.pinned_token.write_text('pve_api_token = "fixture-secret-token"\n', encoding="utf-8")
        self.pinned_token.chmod(0o600)
        ca = self.runtime / "secrets/pve-ca.pem"
        ca.write_text(
            "-----BEGIN CERTIFICATE-----\nfixture\n-----END CERTIFICATE-----\n",
            encoding="utf-8",
        )
        ca.chmod(0o600)
        self.pinned_ca = self.runtime / "pinned/pve-ca.pem"
        self.pinned_ca.write_text(ca.read_text(encoding="utf-8"), encoding="utf-8")
        self.pinned_ca.chmod(0o600)
        self.run_env = self.runtime / "run.env.json"
        self.run_env.write_text(
            json.dumps({"pveSshPrivateKey": str(key), "guestSshPrivateKey": str(guest_key),
                        "pveSshKnownHosts": str(known_hosts), "pveTokenTfvars": str(token),
                        "pveCaPem": str(ca),
                        "httpsProxy": "http://127.0.0.1:18081"}),
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
[ "${SSL_CERT_FILE:-}" = "$PINNED_PVE_CA" ] || exit 93
[ "${PROXMOX_VE_INSECURE:-}" = false ] || exit 94
printf '%s|%s|token-set|ca-pinned|%s\\n' "$TF_DATA_DIR" "$TF_CLI_CONFIG_FILE" "$*" >>"$CALLS"
case " $* " in
 *" init "*) touch "$INIT_MARKER";;
 *" destroy -auto-approve "*) [ -f "$INIT_MARKER" ] || { echo "Backend initialization required" >&2; exit 92; }; [ -z "${ORDER_CALLS:-}" ] || printf '%s\\n' tofu-destroy >>"$ORDER_CALLS"; [ "${FAIL_DESTROY:-0}" != 1 ] || exit 96;;
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

    def run_driver(self, mode, *, contract_path=None, tfvars_path=None, destroy_fails=False):
        if contract_path is None:
            self.write_contract(mode)
            contract_path = self.contract_path
        if tfvars_path is None:
            tfvars_path = self.tfvars
        evidence = self.runtime / f"evidence/{mode}"
        environment = os.environ.copy()
        environment.update(
            PATH=f"{self.bin}:/usr/bin:/bin",
            CALLS=str(self.calls),
            INIT_MARKER=str(self.init_marker),
            ROUTERD_RELEASE_QA_PINNED_CONTRACT=str(contract_path),
            ROUTERD_RELEASE_QA_PINNED_RUN_ENV=str(self.run_env),
            ROUTERD_RELEASE_QA_PINNED_TFVARS=str(tfvars_path),
            ROUTERD_RELEASE_QA_PINNED_PVE_TOKEN_TFVARS=str(self.pinned_token),
            ROUTERD_RELEASE_QA_PINNED_PVE_CA_PEM=str(self.pinned_ca),
            ROUTERD_RELEASE_QA_PINNED_PVE_SSH_KNOWN_HOSTS=str(self.pinned_known_hosts),
            PINNED_PVE_CA=str(self.pinned_ca),
            ORPHAN_CALLS=str(self.orphan_calls),
            ORDER_CALLS=str(self.order_calls),
            FAIL_DESTROY="1" if destroy_fails else "0",
        )
        result = subprocess.run(
            [str(self.drivers / "cleanup-driver.sh"), "--run-id", "run-1", "--evidence-dir", str(evidence)],
            text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=environment, check=False,
        )
        calls = self.calls.read_text(encoding="utf-8") if self.calls.exists() else ""
        return result, evidence, calls

    def pin_staging_precheck_failure(self, *, phase="CLEANING", mutation=False,
                                     wrong_endpoint=False):
        self.write_contract("staging-no-mutation")
        pinned = self.runtime / "pinned"
        pinned_contract = pinned / "contract.json"
        pinned_tfvars = pinned / "terraform.tfvars"
        stale_tfvars = self.tfvars.read_text(encoding="utf-8").replace(
            'commit = "release-commit"', 'commit = "stale-commit"'
        )
        if wrong_endpoint:
            stale_tfvars = stale_tfvars.replace(
                'pve_endpoint = "https://pve01.lain.local:8006/"',
                'pve_endpoint = "https://wrong.example.test:8006/"',
            )
        pinned_contract.write_bytes(self.contract_path.read_bytes())
        pinned_tfvars.write_text(stale_tfvars, encoding="utf-8")
        pinned_contract.chmod(0o600)
        pinned_tfvars.chmod(0o600)
        lifecycle = self.runtime / "evidence/lifecycle"
        lifecycle.mkdir(parents=True, exist_ok=True)
        state = {
            "runId": "run-1",
            "runRoot": str(self.run_root),
            "phase": phase,
            "stopReason": "precheck-failed",
            "executionMode": "staging-no-mutation",
            "effectiveLifecycle": {
                "executionMode": "staging-no-mutation",
                "contractSha256": hashlib.sha256(pinned_contract.read_bytes()).hexdigest(),
            },
            "mutationCommandExecuted": mutation,
            "mutationPgid": None,
            "precheckExit": 2,
            "cleanupAttempts": 1,
            "history": ([] if not mutation else [{"to": "MUTATING"}]),
            "inputs": {
                "contract": {
                    "pinned": str(pinned_contract),
                    "sha256": hashlib.sha256(pinned_contract.read_bytes()).hexdigest(),
                },
                "tfvars": {
                    "pinned": str(pinned_tfvars),
                    "sha256": hashlib.sha256(pinned_tfvars.read_bytes()).hexdigest(),
                },
                "runEnv": {}, "pveSshPrivateKey": {}, "guestSshPrivateKey": {},
                "pveSshKnownHosts": {}, "pveTokenTfvars": {}, "pveCaPem": {},
            },
        }
        state_path = lifecycle / "supervisor-state.json"
        state_path.write_text(json.dumps(state), encoding="utf-8")
        state_path.chmod(0o600)
        return pinned_contract, pinned_tfvars

    def assert_confined_tofu_environment(self, calls):
        expected = f"{self.runtime}/tofu-data|{self.framework}/tofu.rc|token-set|ca-pinned|"
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

    def test_stale_commit_recovers_only_the_pinned_unmutated_staging_zero_path(self):
        contract, tfvars = self.pin_staging_precheck_failure()
        result, evidence, calls = self.run_driver(
            "staging-no-mutation", contract_path=contract, tfvars_path=tfvars,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(calls, "")
        self.assertEqual(
            json.loads((evidence / "cleanup-decision.json").read_text())["action"],
            "skip-tofu-destroy",
        )

    def test_stale_commit_recovery_rejects_mutation_or_nonrecovery_state(self):
        for phase, mutation in (("PRECHECK", False), ("CLEANING", True)):
            with self.subTest(phase=phase, mutation=mutation):
                contract, tfvars = self.pin_staging_precheck_failure(
                    phase=phase, mutation=mutation,
                )
                result, _, calls = self.run_driver(
                    "staging-no-mutation", contract_path=contract, tfvars_path=tfvars,
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(calls, "")
                self.assertIn("tfvars commit does not equal exact artifact commit", result.stderr)

    def test_stale_commit_recovery_keeps_pve_endpoint_validation_strict(self):
        contract, tfvars = self.pin_staging_precheck_failure(wrong_endpoint=True)
        result, _, calls = self.run_driver(
            "staging-no-mutation", contract_path=contract, tfvars_path=tfvars,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, "")
        self.assertIn("pve_endpoint does not use contract pve.sshHost", result.stderr)

    def test_production_without_state_initializes_before_destroy(self):
        result, evidence, calls = self.run_driver("production")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("destroy -auto-approve -input=false", calls)
        commands = [line.split("|", 4)[4] for line in calls.splitlines()]
        init = (
            f"-chdir={self.tf} init -input=false -lockfile=readonly -reconfigure "
            f"-backend-config=path={self.state}"
        )
        self.assertLess(
            commands.index(init),
            commands.index(
                f"-chdir={self.tf} destroy -auto-approve -input=false "
                f"-backup={evidence}/tofu-pre-destroy.tfstate "
                f"-var-file={self.tfvars}"
            ),
        )
        self.assertEqual(commands.count(init), 1)
        self.assert_confined_tofu_environment(calls)
        self.assertEqual(json.loads((evidence / "cleanup-decision.json").read_text())["action"], "tofu-destroy")
        self.assertEqual(
            self.order_calls.read_text(encoding="utf-8").splitlines(),
            ["tofu-destroy", "orphan-recovery", "bridge-remove"],
        )
        self.assertEqual(
            self.orphan_calls.read_text(encoding="utf-8"),
            f"--evidence {evidence}/pve-orphan-recovery.json\n",
        )

    def test_staging_with_state_still_attempts_destroy(self):
        self.state.write_text('{"version": 4}\n', encoding="utf-8")
        result, evidence, calls = self.run_driver("staging-no-mutation")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("state list", calls)
        self.assertIn("destroy -auto-approve -input=false", calls)
        self.assert_confined_tofu_environment(calls)
        self.assertEqual(json.loads((evidence / "cleanup-decision.json").read_text())["action"], "tofu-destroy")

    def test_destroy_failure_still_runs_orphan_recovery_and_bridge_zero_check(self):
        result, evidence, _ = self.run_driver("production", destroy_fails=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("cleanup incomplete: tofu-destroy=96", result.stderr)
        self.assertEqual(
            self.order_calls.read_text(encoding="utf-8").splitlines(),
            ["tofu-destroy", "orphan-recovery", "bridge-remove"],
        )
        self.assertEqual(
            self.orphan_calls.read_text(encoding="utf-8"),
            f"--evidence {evidence}/pve-orphan-recovery.json\n",
        )
        self.assertIn(
            "tofu-destroy\t96\n",
            (evidence / "cleanup-errors.tsv").read_text(encoding="utf-8"),
        )

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
            INIT_MARKER=str(self.init_marker),
            ROUTERD_RELEASE_QA_PINNED_CONTRACT=str(self.contract_path),
            ROUTERD_RELEASE_QA_PINNED_RUN_ENV=str(self.run_env),
            ROUTERD_RELEASE_QA_PINNED_PVE_TOKEN_TFVARS=str(self.pinned_token),
            ROUTERD_RELEASE_QA_PINNED_PVE_CA_PEM=str(self.pinned_ca),
            ROUTERD_RELEASE_QA_PINNED_PVE_SSH_KNOWN_HOSTS=str(self.pinned_known_hosts),
            PINNED_PVE_CA=str(self.pinned_ca),
            ORPHAN_CALLS=str(self.orphan_calls),
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
        commands = [line.split("|", 4)[4] for line in calls.splitlines()]
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
