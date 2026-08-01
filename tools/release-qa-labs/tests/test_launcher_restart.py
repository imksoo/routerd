from datetime import datetime, timedelta, timezone
import hashlib
import json
import os
import pwd
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class LauncherRestartTests(unittest.TestCase):
    def test_azure_source_rejects_broad_entry_modes_before_supervisor(self):
        for kind in ("directory", "file", "source-change-during-copy"):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as temporary:
                base = Path(temporary) / "runs"
                run_root = base / "run-1"
                runtime = run_root / "runtime"
                drivers = run_root / "repo/tools/release-qa-labs/drivers"
                drivers.mkdir(parents=True)
                (runtime / "secrets").mkdir(parents=True, mode=0o700)
                source = runtime / "secrets/azure-auth-source"
                source.mkdir(mode=0o700)
                profile = source / "azureProfile.json"
                profile.write_text("{}\n", encoding="utf-8")
                profile.chmod(0o600)
                if kind == "directory":
                    nested = source / "nested"
                    nested.mkdir(mode=0o755)
                    nested.chmod(0o755)
                elif kind == "file":
                    profile.chmod(0o644)
                run_env = runtime / "run.env.json"
                run_env.write_text(json.dumps({"azureAuthSource": str(source)}), encoding="utf-8")
                run_env.chmod(0o600)
                contract = runtime / "contract.json"
                contract.write_text("{}\n", encoding="utf-8")
                contract.chmod(0o600)
                launcher_text = (ROOT / "drivers/start-supervised-release-qa.sh").read_text()
                launcher = drivers / "start-supervised-release-qa.sh"
                launcher.write_text(
                    launcher_text.replace("/var/lib/routerd-release-qa", str(base)), encoding="utf-8"
                )
                launcher.chmod(0o755)
                prepare = drivers / "prepare-provider-auth.sh"
                prepare.write_text(
                    (ROOT / "drivers/prepare-provider-auth.sh").read_text().replace(
                        "/var/lib/routerd-release-qa", str(base)
                    ).replace(f"{base}-sealed", str(Path(temporary) / "sealed"))
                    .replace("service_user=routerd-release-qa", f"service_user={pwd.getpwuid(os.getuid()).pw_name}")
                    .replace("service_group=routerd-release-qa", f"service_group={os.getgid()}"), encoding="utf-8",
                )
                prepare.chmod(0o755)
                env = os.environ.copy()
                if kind == "source-change-during-copy":
                    fake_bin = Path(temporary) / "bin"
                    fake_bin.mkdir()
                    fake_cp = fake_bin / "cp"
                    fake_cp.write_text(
                        "#!/bin/sh\n/usr/bin/cp \"$@\"\n"
                        "printf raced >\"$AZURE_TEST_SOURCE/raced\"\n"
                        "chmod 0600 \"$AZURE_TEST_SOURCE/raced\"\n",
                        encoding="utf-8",
                    )
                    fake_cp.chmod(0o755)
                    env["PATH"] = f"{fake_bin}:{env['PATH']}"
                    env["AZURE_TEST_SOURCE"] = str(source)
                (Path(temporary) / "sealed/run-1").mkdir(parents=True, mode=0o750)
                sudo_env = ["sudo", "-n", "-u", "root", "-g", f"#{os.getgid()}", "env"]
                if kind == "source-change-during-copy":
                    sudo_env += [f"PATH={env['PATH']}", f"AZURE_TEST_SOURCE={source}"]
                result = subprocess.run(sudo_env + [str(prepare), "run-1"], text=True,
                                        stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                        check=False, env=env)
                subprocess.run(["sudo", "-n", "chown", "-R", f"{os.getuid()}:{os.getgid()}", temporary], check=True)
                self.assertEqual(result.returncode, 2, result.stderr)
                if kind == "source-change-during-copy":
                    self.assertIn("changed during snapshot", result.stderr)
                else:
                    self.assertIn("authentication source is unsafe", result.stderr)

    def test_staging_azure_source_only_tamper_recovers_zero_but_cannot_pass(self):
        with tempfile.TemporaryDirectory() as temporary:
            base = Path(temporary) / "runs"
            run_root = base / "run-1"
            framework = run_root / "repo/tools/release-qa-labs"
            drivers = framework / "drivers"
            runtime = run_root / "runtime"
            tf_dir = runtime
            lifecycle_dir = runtime / "evidence/lifecycle"
            pinned = runtime / "pinned"
            for directory in (drivers, tf_dir, lifecycle_dir, pinned):
                directory.mkdir(parents=True, mode=0o700)
                directory.chmod(0o700)

            # Keep production logic exact except for mapping its canonical
            # deployment prefix into this isolated fixture root.
            deployment = "/var/lib/routerd-release-qa"
            launcher_text = (ROOT / "drivers/start-supervised-release-qa.sh").read_text()
            launcher = drivers / "start-supervised-release-qa.sh"
            launcher.write_text(launcher_text.replace(deployment, str(base)), encoding="utf-8")
            launcher.write_text(launcher.read_text().replace(f"{base}-sealed", str(Path(temporary) / "sealed")), encoding="utf-8")
            launcher.chmod(0o755)
            prepare = drivers / "prepare-provider-auth.sh"
            prepare.write_text(
                (ROOT / "drivers/prepare-provider-auth.sh").read_text().replace(
                    deployment, str(base)
                ).replace(f"{base}-sealed", str(Path(temporary) / "sealed"))
                .replace("service_user=routerd-release-qa", f"service_user={pwd.getpwuid(os.getuid()).pw_name}")
                .replace("service_group=routerd-release-qa", f"service_group={os.getgid()}"), encoding="utf-8",
            )
            prepare.chmod(0o755)
            supervisor_text = (ROOT / "lifecycle_supervisor.py").read_text()
            supervisor = framework / "lifecycle_supervisor.py"
            supervisor.write_text(supervisor_text.replace(deployment, str(base)), encoding="utf-8")
            supervisor.chmod(0o755)

            cleanup_marker = run_root / "cleanup-called"
            inventory_marker = run_root / "inventory-called"
            for name, marker in (("supervisor-cleanup.sh", cleanup_marker), ("supervisor-inventory.sh", inventory_marker)):
                script = drivers / name
                script.write_text(f"#!/bin/sh\ntouch '{marker}'\n", encoding="utf-8")
                script.chmod(0o755)
            mutation_marker = run_root / "mutation-called"
            for name in ("precheck-driver.sh", "mutation-driver.sh"):
                script = drivers / name
                body = "#!/bin/sh\nexit 0\n"
                if name == "mutation-driver.sh":
                    body = f"#!/bin/sh\ntouch '{mutation_marker}'\nexit 99\n"
                script.write_text(body, encoding="utf-8")
                script.chmod(0o755)

            sources = {
                "contract": runtime / "contract.json",
                "runEnv": runtime / "run.env.json",
                "tfvars": runtime / "terraform.tfvars",
                "pveSshPrivateKey": runtime / "secrets/pve_ssh",
            }
            azure_source = runtime / "secrets/azure-auth-source"
            azure_source.mkdir(parents=True, mode=0o700)
            azure_profile = azure_source / "azureProfile.json"
            azure_profile.write_text("{}\n", encoding="utf-8")
            azure_profile.chmod(0o600)
            payloads = {
                "contract": json.dumps({"runId": "run-1", "execution": {"mode": "staging-no-mutation"}, "lifecycle": {
                    "ttl": "30m", "heartbeatStale": "2m", "cleanupTimeout": "4m",
                    "inventoryTimeout": "1m", "maxCleanupAttempts": 2,
                    "maxPaidLifecycleSeconds": 2400,
                }}).encode() + b"\n",
                "runEnv": json.dumps({"pveSshPrivateKey": str(runtime / "secrets/pve_ssh"),
                                       "azureAuthSource": str(azure_source)}).encode() + b"\n",
                "tfvars": b"run_id=\"run-1\"\n", "pveSshPrivateKey": b"fixture key\n",
            }
            for key, source in sources.items():
                source.parent.mkdir(parents=True, exist_ok=True)
                if key == "pveSshPrivateKey":
                    source.parent.chmod(0o700)
                source.write_bytes(payloads[key])
                source.chmod(0o600)
            state_path = lifecycle_dir / "supervisor-state.json"
            command = [str(launcher), str(runtime / "contract.json")]
            (Path(temporary) / "sealed/run-1").mkdir(parents=True, mode=0o750)
            prepared = subprocess.run(["sudo", "-n", "-u", "root", "-g", f"#{os.getgid()}", str(prepare), "run-1"], text=True,
                                      stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
            self.assertEqual(prepared.returncode, 0, prepared.stderr)
            first = subprocess.run(command, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   check=False, env=os.environ.copy())
            self.assertEqual(first.returncode, 2, first.stderr)
            self.assertTrue(state_path.exists(), first.stderr)
            state = json.loads(state_path.read_text())
            self.assertEqual(state["effectiveLifecycle"], {
                "ttlSeconds": 1800, "staleSeconds": 120,
                "cleanupTimeoutSeconds": 240, "inventoryTimeoutSeconds": 60,
                "plannedCleanupAttempts": 2, "plannedPaidLifecycleSeconds": 2400,
                "contractSha256": hashlib.sha256(payloads["contract"]).hexdigest(),
                "executionMode": "staging-no-mutation",
            })
            started = datetime.fromisoformat(state["startedAt"].replace("Z", "+00:00"))
            deadline = datetime.fromisoformat(state["deadline"].replace("Z", "+00:00"))
            paid_deadline = datetime.fromisoformat(state["plannedPaidDeadline"].replace("Z", "+00:00"))
            self.assertEqual((deadline - started).total_seconds(), 1800)
            self.assertEqual((paid_deadline - started).total_seconds(), 2400)
            self.assertEqual(state["phase"], "STAGING_ARMED")

            # Azure source tamper alone must not block cleanup, but it must be
            # persisted as a qualification failure rather than STAGING_DONE.
            provider_state = runtime / "provider-state/azure"
            (provider_state / "disposable-tamper").write_text("tampered\n", encoding="utf-8")
            (provider_state / "disposable-tamper").chmod(0o600)
            shutil.rmtree(azure_source)
            result = subprocess.run(command,
                                    text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                    check=False, env=os.environ.copy())
            self.assertEqual(result.returncode, 1, result.stderr)
            self.assertTrue(cleanup_marker.exists())
            self.assertTrue(inventory_marker.exists())
            final = json.loads(state_path.read_text())
            self.assertEqual(final["effectiveLifecycle"], state["effectiveLifecycle"])
            self.assertTrue(final["sourceInputTamperDetected"])
            pinned_key = Path(final["inputs"]["pveSshPrivateKey"]["pinned"])
            self.assertTrue(pinned_key.is_file())
            self.assertEqual(hashlib.sha256(pinned_key.read_bytes()).hexdigest(),
                             final["inputs"]["pveSshPrivateKey"]["sha256"])
            self.assertEqual(final["phase"], "FAILED")
            self.assertEqual(final["inventoryExit"], 0)
            self.assertFalse(mutation_marker.exists())
            self.assertTrue(final["sourceInputTamperDetected"])
            self.assertTrue((lifecycle_dir / "azure-auth-source-tamper.txt").is_file())
            self.assertFalse((provider_state / "disposable-tamper").exists())
            self.assertEqual((provider_state / "azureProfile.json").read_text(), "{}\n")
            subprocess.run(["sudo", "-n", "chown", "-R", f"{os.getuid()}:{os.getgid()}", temporary], check=True)


if __name__ == "__main__":
    unittest.main()
