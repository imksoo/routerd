import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class RemoteEgressPreflightTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.run_root = self.root / "runs/run-1"
        self.repo = self.run_root / "repo"
        self.framework = self.repo / "tools/release-qa-labs"
        self.runtime = self.run_root / "runtime"
        self.drivers = self.framework / "drivers"
        self.drivers.mkdir(parents=True)
        self.runtime.mkdir()
        for name in ("common.sh", "remote-egress-preflight.sh"):
            shutil.copy2(ROOT / "drivers" / name, self.drivers / name)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.calls = self.root / "calls"
        self.calls.mkdir()
        self.artifact = self.runtime / "artifact"
        self.artifact.write_bytes(b"artifact")
        self.tf = self.runtime / "tf"
        self.tf.mkdir()
        self.tfvars = self.runtime / "terraform.tfvars"
        self.tfvars.write_text(
            'run_id = "run-1"\ncommit = "release-commit"\n'
            'aws_region = "ap-northeast-1"\naws_profile = "fixture"\n'
            'oci_region = "ap-tokyo-1"\noci_profile = "fixture"\n', encoding="utf-8")
        with self.tfvars.open("a", encoding="utf-8") as handle:
            handle.write('pve_node_name = "pve01"\npve_ssh_host = "pve01.lain.local"\n'
                         'pve_endpoint = "https://pve01.lain.local:8006/"\n')
        self.tfvars.chmod(0o600)
        self.token = self.runtime / "pve-token.tfvars"
        self.token.write_text('pve_api_token = "fixture"\n', encoding="utf-8")
        self.token.chmod(0o600)
        self.ssh_key = self.runtime / "secrets/pve_ssh"
        self.ssh_key.parent.mkdir(mode=0o700)
        self.ssh_key.write_text("fixture key\n", encoding="utf-8")
        self.ssh_key.chmod(0o600)
        self.mirror = self.root / "mirror"
        (self.mirror / "registry.opentofu.org/hashicorp/aws/1.2.3/linux_amd64").mkdir(parents=True)
        contract = {
            "runId": "run-1", "labsCommit": "qa-commit",
            "routerdArtifact": {"path": str(self.artifact), "version": "v1", "commit": "release-commit"},
            "tofu": {"workingDirectory": str(self.tf), "statePath": str(self.tf / "state"),
                     "variablesPath": str(self.tfvars), "outputPath": str(self.tf / "output")},
            "lifecycle": {"ttl": "75m", "heartbeatStale": "5m"},
            "execution": {"host": "chatty", "providerMirror": str(self.mirror),
                          "providerVersions": {"hashicorp/aws": "1.2.3"}},
            "pve": {"node": "pve01", "sshHost": "pve01.lain.local"},
        }
        self.contract = self.runtime / "contract.json"
        self.contract.write_text(json.dumps(contract), encoding="utf-8")
        self.contract.chmod(0o600)
        self.make("hostname", 'echo chatty')
        self.make("git", f'''case " $* " in
 *" rev-parse --show-toplevel "*) echo "{self.repo}";;
 *" rev-parse HEAD "*) echo qa-commit;;
esac
exit 0''')
        self.make("getent", 'echo "getent $*" >>"$CALLS"; [ "${FAILURE:-}" = dns ] && exit 9; exit 0')
        self.make("timeout", '''echo "timeout $*" >>"$CALLS"
case "${FAILURE:-}:$*" in tcp:*proxy.invalid*) exit 9;; pve_tcp:*pve01.lain.local/8006*) exit 9;; direct_tcp:*443*) exit 9;; esac
shift
[ "$1" = bash ] && exit 0
exec "$@"''')
        self.make("curl", 'echo "curl $*" >>"$CALLS"; [ "${FAILURE:-}" = proxy ] && exit 9; exit 0')
        self.make("openssl", 'echo "openssl $*" >>"$CALLS"; [ "${FAILURE:-}" = tls ] && exit 9; exit 0')
        for name in ("aws", "az", "oci", "ssh"):
            self.make(name, f'echo "{name} $*" >>"$CALLS"; [ "${{FAILURE:-}}" = {name} ] && exit 9; echo "{{}}"')

    def tearDown(self):
        self.temp.cleanup()

    def make(self, name, body):
        path = self.bin / name
        path.write_text("#!/bin/sh\nset -eu\n" + body + "\n", encoding="utf-8")
        path.chmod(0o755)

    def run_preflight(self, failure="", proxy=True, mirror_present=True):
        run_env = {"noProxy": "127.0.0.1,localhost,pve01", "pveTokenTfvars": str(self.token),
                   "pveSshPrivateKey": str(self.ssh_key)}
        if proxy:
            run_env["httpsProxy"] = "http://proxy.invalid:3128"
        run_env_path = self.runtime / "run.env.json"
        run_env_path.write_text(json.dumps(run_env), encoding="utf-8")
        run_env_path.chmod(0o600)
        mirror_entry = self.mirror / "registry.opentofu.org/hashicorp/aws/1.2.3/linux_amd64"
        if mirror_present:
            mirror_entry.mkdir(parents=True, exist_ok=True)
        elif self.mirror.exists():
            shutil.rmtree(self.mirror)
        output = self.runtime / "evidence" / "preflight" / "remote-egress" / "result.json"
        output.unlink(missing_ok=True)
        call_log = self.calls / f"{failure or 'success'}-{'proxy' if proxy else 'direct'}"
        environment = os.environ.copy()
        for key in ("HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "ALL_PROXY", "all_proxy"):
            environment.pop(key, None)
        environment.update(PATH=f"{self.bin}:/usr/bin:/bin", CALLS=str(call_log), FAILURE=failure)
        result = subprocess.run(
            [str(self.drivers / "remote-egress-preflight.sh"), "--contract", str(self.contract)],
            text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=environment, check=False)
        return result, call_log, output

    def assert_failed(self, failure, *, proxy=True, mirror_present=True):
        result, calls, output = self.run_preflight(failure, proxy, mirror_present)
        self.assertNotEqual(result.returncode, 0, f"{failure} unexpectedly passed; calls={calls.read_text() if calls.exists() else ''}")
        self.assertFalse(output.exists(), f"{failure} must not emit pass result")
        return calls.read_text() if calls.exists() else ""

    def test_proxy_path_success_requires_dns_tcp_connect_tls_auth_pve_and_mirror(self):
        result, calls, output = self.run_preflight(proxy=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        log = calls.read_text()
        for command in ("getent", "timeout", "curl", "aws", "az", "oci", "ssh"):
            self.assertIn(command, log)
        result_data = json.loads(output.read_text())
        self.assertEqual(result_data["status"], "pass")
        self.assertEqual(result_data["runId"], "run-1")
        self.assertEqual(result_data["executionHost"], "chatty")
        self.assertRegex(result_data["contractSha256"], r"^[0-9a-f]{64}$")
        self.assertIn("pve01.lain.local", log)
        self.assertNotIn("root@pve01 ", log)

    def test_proxy_negative_matrix_is_fail_closed(self):
        for failure in ("dns", "tcp", "proxy", "aws", "az", "oci", "ssh", "pve_tcp"):
            with self.subTest(failure=failure):
                self.assert_failed(failure, proxy=True)
        self.assert_failed("mirror", proxy=True, mirror_present=False)

    def test_mid_auth_failure_leaves_no_group_or_world_readable_evidence(self):
        self.assert_failed("az", proxy=True)
        evidence = self.runtime / "evidence" / "preflight" / "remote-egress"
        for path in evidence.iterdir():
            if path.is_file():
                self.assertEqual(path.stat().st_mode & 0o077, 0, f"broad evidence mode: {path}")

    def test_direct_tcp_and_tls_negative_matrix(self):
        success, calls, output = self.run_preflight(proxy=False)
        self.assertEqual(success.returncode, 0, success.stderr)
        self.assertIn("openssl", calls.read_text())
        self.assertTrue(output.exists())
        for failure in ("direct_tcp", "tls"):
            with self.subTest(failure=failure):
                self.assert_failed(failure, proxy=False)


if __name__ == "__main__":
    unittest.main()
