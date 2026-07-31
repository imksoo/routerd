from datetime import datetime, timedelta, timezone
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class LauncherRestartTests(unittest.TestCase):
    def test_deleted_and_tampered_sources_restart_through_pinned_cleanup(self):
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
            launcher.chmod(0o755)
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
            for name in ("precheck-driver.sh", "mutation-driver.sh"):
                script = drivers / name
                script.write_text("#!/bin/sh\nexit 99\n", encoding="utf-8")
                script.chmod(0o755)

            sources = {
                "contract": runtime / "contract.json",
                "runEnv": runtime / "run.env.json",
                "tfvars": runtime / "terraform.tfvars",
                "pveSshPrivateKey": runtime / "secrets/pve_ssh",
            }
            payloads = {
                "contract": json.dumps({"runId": "run-1", "lifecycle": {
                    "ttl": "30m", "heartbeatStale": "2m", "cleanupTimeout": "4m",
                    "inventoryTimeout": "1m", "maxCleanupAttempts": 2,
                    "maxPaidLifecycleSeconds": 2400,
                }}).encode() + b"\n",
                "runEnv": json.dumps({"pveSshPrivateKey": str(runtime / "secrets/pve_ssh")}).encode() + b"\n",
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
            first = subprocess.run(command, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   check=False, env=os.environ.copy())
            self.assertEqual(first.returncode, 2, first.stderr)
            state = json.loads(state_path.read_text())
            self.assertEqual(state["effectiveLifecycle"], {
                "ttlSeconds": 1800, "staleSeconds": 120,
                "cleanupTimeoutSeconds": 240, "inventoryTimeoutSeconds": 60,
                "plannedCleanupAttempts": 2, "plannedPaidLifecycleSeconds": 2400,
                "contractSha256": hashlib.sha256(payloads["contract"]).hexdigest(),
            })
            started = datetime.fromisoformat(state["startedAt"].replace("Z", "+00:00"))
            deadline = datetime.fromisoformat(state["deadline"].replace("Z", "+00:00"))
            paid_deadline = datetime.fromisoformat(state["plannedPaidDeadline"].replace("Z", "+00:00"))
            self.assertEqual((deadline - started).total_seconds(), 1800)
            self.assertEqual((paid_deadline - started).total_seconds(), 2400)
            state.update(phase="MUTATING", mutationPgid=None, mutationBootId="old-boot", cleanupAttempts=7)
            state_path.write_text(json.dumps(state), encoding="utf-8")
            state_path.chmod(0o600)

            # Both deletion and tampering of mutable sources must be irrelevant
            # to cleanup command selection and its pinned environment.
            sources["contract"].unlink()
            sources["runEnv"].write_text("tampered\n", encoding="utf-8")
            sources["runEnv"].chmod(0o600)
            sources["tfvars"].unlink()
            sources["pveSshPrivateKey"].unlink()
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


if __name__ == "__main__":
    unittest.main()
