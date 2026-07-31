import os
import json
import pwd
from pathlib import Path
import shutil
import subprocess
import tempfile
import uuid
import unittest

ROOT = Path(__file__).resolve().parents[1]


class SystemdConfinementTests(unittest.TestCase):
    def require_systemd_sudo(self):
        if shutil.which("systemd-run") is None or shutil.which("sudo") is None:
            self.skipTest("systemd-run and sudo are required")
        if subprocess.run(["sudo", "-n", "true"], check=False).returncode:
            self.skipTest("passwordless sudo is required for mount namespace test")

    def test_prepare_namespace_creates_initial_pins_without_network(self):
        self.require_systemd_sudo()
        with tempfile.TemporaryDirectory(dir="/var/tmp") as temporary:
            base = Path(temporary) / "runs"
            sealed = Path(temporary) / "sealed/run-1"
            sealed.mkdir(parents=True, mode=0o750)
            runtime = base / "run-1/runtime"
            source = runtime / "secrets/azure-auth-source"
            source.mkdir(parents=True, mode=0o700)
            source.chmod(0o700)
            profile = source / "profile"
            profile.write_text("auth\n")
            profile.chmod(0o600)
            script = Path(temporary) / "prepare-provider-auth.sh"
            script.write_text(
                (ROOT / "drivers/prepare-provider-auth.sh").read_text().replace(
                    "/var/lib/routerd-release-qa", str(base)
                ).replace(f"{base}-sealed", str(Path(temporary) / "sealed"))
                .replace("service_user=routerd-release-qa", f"service_user={pwd.getpwuid(os.getuid()).pw_name}")
                .replace("service_group=routerd-release-qa", f"service_group={os.getgid()}")
            )
            script.chmod(0o755)
            result = subprocess.run([
                "sudo", "-n", "systemd-run", "--quiet", "--wait", "--pipe",
                "-p", f"Group={os.getgid()}",
                "-p", "ProtectSystem=strict", "-p", "PrivateNetwork=true",
                "-p", f"ReadWritePaths={sealed}",
                "-p", f"ReadOnlyPaths={runtime / 'secrets'}",
                str(script), "run-1",
            ], text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertTrue((sealed / "azure-auth-snapshot/profile").is_file())
            self.assertTrue((sealed / "azure-auth-source.sha256").is_file())
            snapshot = sealed / "azure-auth-snapshot"
            digest = sealed / "azure-auth-source.sha256"
            readable = subprocess.run([
                "sudo", "-n", "-u", f"#{os.getuid()}", "/bin/sh", "-c",
                f"cat '{digest}' >/dev/null && stat '{snapshot}/profile' >/dev/null",
            ], check=False)
            self.assertEqual(readable.returncode, 0)
            for denied_command in (
                f"touch '{snapshot}/denied'",
                f"rm -f '{digest}'",
                f"sha256sum '{snapshot}/profile' >'{digest}'",
            ):
                denied = subprocess.run([
                    "sudo", "-n", "-u", f"#{os.getuid()}", "/bin/sh", "-c", denied_command,
                ], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
                self.assertNotEqual(denied.returncode, 0, denied_command)
            subprocess.run(["sudo", "-n", "chown", "-R", f"{os.getuid()}:{os.getgid()}", temporary], check=True)

    def test_state_directory_reconcile_preserves_prepare_pins_across_three_runs(self):
        self.require_systemd_sudo()
        run_id = f"test-{uuid.uuid4().hex}"
        sealed = Path("/var/lib/routerd-release-qa-sealed") / run_id
        with tempfile.TemporaryDirectory(dir="/var/tmp") as temporary:
            base = Path(temporary) / "runs"
            source = base / run_id / "runtime/secrets/azure-auth-source"
            source.mkdir(parents=True, mode=0o700)
            source.chmod(0o700)
            profile = source / "profile"
            profile.write_text("auth\n")
            profile.chmod(0o600)
            script = Path(temporary) / "prepare-provider-auth.sh"
            script.write_text(
                (ROOT / "drivers/prepare-provider-auth.sh").read_text()
                .replace("/var/lib/routerd-release-qa", str(base))
                .replace(f"{base}-sealed", "/var/lib/routerd-release-qa-sealed")
                .replace("service_user=routerd-release-qa", f"service_user={pwd.getpwuid(os.getuid()).pw_name}")
                .replace("service_group=routerd-release-qa", f"service_group={os.getgid()}")
            )
            script.chmod(0o755)
            command = [
                "sudo", "-n", "systemd-run", "--quiet", "--wait", "--pipe",
                "-p", f"Group={os.getgid()}",
                "-p", f"StateDirectory=routerd-release-qa-sealed/{run_id}",
                "-p", "StateDirectoryMode=0711",
                "-p", "PrivateNetwork=true", "-p", "RestrictAddressFamilies=AF_UNIX",
                str(script), run_id,
            ]
            try:
                first = subprocess.run(command, text=True, stdout=subprocess.PIPE,
                                       stderr=subprocess.PIPE, check=False)
                self.assertEqual(first.returncode, 0, first.stderr)

                def sealed_identity():
                    result = subprocess.run([
                        "sudo", "-n", "find", sealed, "-printf", "%P %u:%G:%m %y\\n",
                    ], text=True, stdout=subprocess.PIPE, check=True)
                    digest = subprocess.run([
                        "sudo", "-n", "sha256sum", sealed / "azure-auth-source.sha256",
                        sealed / "azure-auth-snapshot/profile",
                    ], text=True, stdout=subprocess.PIPE, check=True)
                    return sorted(result.stdout.splitlines()), digest.stdout

                initial = sealed_identity()
                self.assertTrue(initial[0][0].endswith(f"root:{os.getgid()}:750 d"), initial[0])
                self.assertIn(f"azure-auth-source.sha256 root:{os.getgid()}:640 f", initial[0])
                second = subprocess.run(command, text=True, stdout=subprocess.PIPE,
                                        stderr=subprocess.PIPE, check=False)
                self.assertEqual(second.returncode, 0, second.stderr)
                self.assertEqual(sealed_identity(), initial)
                shutil.rmtree(source)
                third = subprocess.run(command, text=True, stdout=subprocess.PIPE,
                                       stderr=subprocess.PIPE, check=False)
                self.assertEqual(third.returncode, 0, third.stderr)
                self.assertEqual(sealed_identity(), initial)
            finally:
                subprocess.run(["sudo", "-n", "rm", "-rf", "--", sealed], check=False)
                subprocess.run(["sudo", "-n", "chown", "-R", f"{os.getuid()}:{os.getgid()}", temporary], check=True)

    def test_state_directory_creates_exact_root_owned_run_and_parent_blocks_rename(self):
        self.require_systemd_sudo()
        run_id = f"test-{uuid.uuid4().hex}"
        child = Path("/var/lib/routerd-release-qa-sealed") / run_id
        try:
            result = subprocess.run([
                "sudo", "-n", "systemd-run", "--quiet", "--wait", "--pipe",
                "-p", f"StateDirectory=routerd-release-qa-sealed/{run_id}",
                "-p", "StateDirectoryMode=0711", "/usr/bin/stat", "-c", "%u:%g:%a", child,
            ], text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("0:0:711", result.stdout)
            self.assertEqual(
                subprocess.run(
                    ["sudo", "-n", "stat", "-c", "%u:%g:%a", child.parent],
                    text=True, stdout=subprocess.PIPE, check=True,
                ).stdout.strip(),
                "0:0:755",
            )
            renamed = subprocess.run([
                "sudo", "-n", "-u", f"#{os.getuid()}", "mv", child, child.with_name(f"{run_id}-moved")
            ], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
            self.assertNotEqual(renamed.returncode, 0)
        finally:
            subprocess.run(["sudo", "-n", "rmdir", child], check=False)

    def test_main_namespace_denies_auth_and_pins_but_allows_provider_state(self):
        self.require_systemd_sudo()
        with tempfile.TemporaryDirectory(dir="/var/tmp") as temporary:
            runtime = Path(temporary) / "runtime"
            sealed = Path(temporary) / "sealed/run-1"
            source = runtime / "secrets/azure-auth-source"
            snapshot = sealed / "azure-auth-snapshot"
            provider = runtime / "provider-state/azure"
            source.mkdir(parents=True)
            snapshot.mkdir(parents=True)
            provider.mkdir(parents=True)
            digest = sealed / "azure-auth-source.sha256"
            (source / "profile").write_text("source\n")
            (snapshot / "profile").write_text("snapshot\n")
            digest.write_text("digest\n")
            subprocess.run(["sudo", "-n", "chown", "-R", f"0:{os.getgid()}", snapshot], check=True)
            subprocess.run(["sudo", "-n", "chown", f"0:{os.getgid()}", digest], check=True)
            subprocess.run(["sudo", "-n", "chmod", "-R", "g-w,o-rwx", snapshot], check=True)
            subprocess.run(["sudo", "-n", "chmod", "0750", snapshot], check=True)
            subprocess.run(["sudo", "-n", "chmod", "0640", snapshot / "profile", digest], check=True)
            command = (
                f"! touch '{source}/denied' && "
                f"! touch '{snapshot}/denied' && "
                f"! sh -c 'echo denied >>\"{digest}\"' && "
                f"touch '{provider}/allowed'"
            )
            result = subprocess.run([
                "sudo", "-n", "systemd-run", "--quiet", "--wait", "--pipe",
                "-p", f"User={os.getuid()}", "-p", f"Group={os.getgid()}",
                "-p", "ProtectSystem=strict",
                "-p", f"ReadWritePaths={runtime}",
                "-p", f"ReadOnlyPaths={runtime / 'secrets'} {sealed}",
                "/bin/sh", "-c", command,
            ], text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertTrue((provider / "allowed").exists())
            self.assertFalse((source / "denied").exists())
            self.assertFalse((snapshot / "denied").exists())
            subprocess.run(["sudo", "-n", "chown", "-R", f"{os.getuid()}:{os.getgid()}", temporary], check=True)

    def test_finalize_refuses_nonzero_then_removes_only_exact_child_at_zero(self):
        self.require_systemd_sudo()
        with tempfile.TemporaryDirectory(dir="/var/tmp") as temporary:
            base = Path(temporary) / "runs"
            sealed_parent = Path(temporary) / "sealed"
            run_id = "run-1"
            run_root = base / run_id
            evidence = run_root / "runtime/evidence"
            inventory = evidence / "final-inventory/inventory.json"
            lifecycle = evidence / "lifecycle/supervisor-state.json"
            run_env = run_root / "runtime/run.env.json"
            framework = run_root / "repo/tools/release-qa-labs"
            inventory.parent.mkdir(parents=True)
            lifecycle.parent.mkdir(parents=True)
            run_env.write_text(json.dumps({"httpsProxy": "http://127.0.0.1:18081"}))
            framework.mkdir(parents=True)
            shutil.copy2(ROOT / "qa_guard.py", framework / "qa_guard.py")
            lifecycle.write_text(json.dumps({
                "phase": "PRECHECK", "mutationCommandExecuted": False, "mutationPgid": None,
            }))
            scopes = [
                "tofu-state", "aws-tagged-resources", "azure-resource-group",
                "azure-contained-resources", "oci-tagged-resources", "pve-vms", "pve-bridges",
            ]
            child = sealed_parent / run_id
            child.mkdir(parents=True, mode=0o750)
            (child / "sealed").write_text("secret")
            fake_bin = Path(temporary) / "bin"
            fake_bin.mkdir()
            systemctl = fake_bin / "systemctl"
            systemctl.write_text("#!/bin/sh\necho inactive\n")
            systemctl.chmod(0o755)
            ss = fake_bin / "ss"
            ss.write_text('#!/bin/sh\n[ "${PROXY_LISTENING:-}" = 1 ] && echo "LISTEN fixture"\nexit 0\n')
            ss.chmod(0o755)
            finalize = Path(temporary) / "finalize.sh"
            text = (ROOT / "drivers/finalize-sealed-provider-auth.sh").read_text()
            text = text.replace("/var/lib/routerd-release-qa-sealed", str(sealed_parent))
            text = text.replace("/var/lib/routerd-release-qa", str(base))
            finalize.write_text(text)
            finalize.chmod(0o755)
            subprocess.run(["sudo", "-n", "chown", "-R", "0:0", sealed_parent], check=True)
            subprocess.run(["sudo", "-n", "chmod", "0755", sealed_parent], check=True)
            subprocess.run(["sudo", "-n", "chmod", "0750", child], check=True)
            env_path = f"PATH={fake_bin}:{os.environ['PATH']}"
            inventory.write_text(json.dumps({"scopes": [
                {"name": name, "count": 1 if name == "tofu-state" else 0, "queryStatus": "complete"}
                for name in scopes
            ]}))
            denied = subprocess.run(["sudo", "-n", "env", env_path, finalize, run_id], check=False)
            self.assertNotEqual(denied.returncode, 0)
            self.assertEqual(subprocess.run(["sudo", "-n", "test", "-d", child]).returncode, 0)
            inventory.write_text(json.dumps({"scopes": [
                {"name": name, "count": 0, "queryStatus": "complete"} for name in scopes
            ]}))
            for phase in ("MUTATING", "STOPPING", "CLEANING", "VERIFYING_ZERO"):
                lifecycle.write_text(json.dumps({
                    "phase": phase, "mutationCommandExecuted": True, "mutationPgid": 123,
                }))
                post_mutation = subprocess.run(
                    ["sudo", "-n", "env", env_path, finalize, run_id], check=False
                )
                self.assertNotEqual(post_mutation.returncode, 0, phase)
                self.assertEqual(subprocess.run(["sudo", "-n", "test", "-d", child]).returncode, 0)
            lifecycle.write_text(json.dumps({
                "phase": "STAGING_ARMED", "mutationCommandExecuted": True, "mutationPgid": 123,
            }))
            armed_mutated = subprocess.run(["sudo", "-n", "env", env_path, finalize, run_id], check=False)
            self.assertNotEqual(armed_mutated.returncode, 0)
            lifecycle.write_text(json.dumps({
                "phase": "STAGING_ARMED", "mutationCommandExecuted": False, "mutationPgid": None,
            }))
            listening = subprocess.run([
                "sudo", "-n", "env", env_path, "PROXY_LISTENING=1", finalize, run_id,
            ], check=False)
            self.assertNotEqual(listening.returncode, 0)
            self.assertEqual(subprocess.run(["sudo", "-n", "test", "-d", child]).returncode, 0)
            allowed = subprocess.run(["sudo", "-n", "env", env_path, finalize, run_id], check=False)
            self.assertEqual(allowed.returncode, 0)
            self.assertNotEqual(subprocess.run(["sudo", "-n", "test", "-e", child]).returncode, 0)
            self.assertEqual(subprocess.run(["sudo", "-n", "test", "-d", sealed_parent]).returncode, 0)
            subprocess.run(["sudo", "-n", "chown", "-R", f"{os.getuid()}:{os.getgid()}", temporary], check=True)
