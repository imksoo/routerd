import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
SCOPES = (
    "tofu-state",
    "aws-tagged-resources",
    "azure-resource-group",
    "azure-contained-resources",
    "oci-tagged-resources",
    "pve-vms",
    "pve-bridges",
)
REVOCATION_RELATIVE = "tools/release-qa-labs/drivers/revoke-pve-run-token.sh"


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_private(path: Path, value: str | dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if isinstance(value, dict):
        value = json.dumps(value, sort_keys=True)
    path.write_text(value, encoding="utf-8")
    path.chmod(0o600)


class PveTokenRevocationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.base = Path(self.temp.name)
        self.runs_root = self.base / "runs"
        self.run_id = "run-1"
        self.run_root = self.runs_root / self.run_id
        self.runtime = self.run_root / "runtime"
        self.pinned = self.runtime / "pinned"
        self.script = self.run_root / "repo" / REVOCATION_RELATIVE
        self.script.parent.mkdir(parents=True)
        self.script.write_text(
            (ROOT / "drivers/revoke-pve-run-token.sh").read_text(encoding="utf-8").replace(
                "/var/lib/routerd-release-qa", str(self.runs_root)
            ),
            encoding="utf-8",
        )
        self.script.chmod(0o755)
        self.token_secret = "fixture-token-secret-must-not-leak"
        self.token = self.pinned / "pve-token.tfvars"
        write_private(
            self.token,
            f'pve_api_token = "release-qa@pve!{self.run_id}={self.token_secret}"\n',
        )
        self.key = self.pinned / "pve_ssh"
        self.guest_key = self.pinned / "guest_ssh"
        self.known_hosts = self.pinned / "pve-known_hosts"
        self.run_env = self.pinned / "run.env.json"
        self.tfvars = self.pinned / "terraform.tfvars"
        self.ca = self.pinned / "pve-ca.pem"
        write_private(self.key, "fixture private key\n")
        write_private(self.guest_key, "fixture guest private key\n")
        write_private(
            self.known_hosts,
            "pve01.example.test ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEZpeHR1cmUtcHZlLWhvc3Qta2V5\n",
        )
        write_private(self.run_env, {"releaseRepo": str(self.run_root / "repo")})
        write_private(self.tfvars, 'run_id = "run-1"\n')
        write_private(self.ca, "-----BEGIN CERTIFICATE-----\nfixture\n-----END CERTIFICATE-----\n")
        self.contract = self.pinned / "contract.json"
        write_private(
            self.contract,
            {
                "runId": self.run_id,
                "execution": {"mode": "production"},
                "pve": {
                    "sshHost": "pve01.example.test",
                    "tokenOwner": "release-qa@pve",
                },
                "qaImplementation": {
                    "scriptBlobs": {REVOCATION_RELATIVE: digest(self.script)},
                },
            },
        )
        self.inventory = self.runtime / "evidence/final-inventory/inventory.json"
        self.write_inventory()
        self.state = self.runtime / "evidence/lifecycle/supervisor-state.json"
        pins = {
            "contract": self.contract,
            "runEnv": self.run_env,
            "tfvars": self.tfvars,
            "pveSshPrivateKey": self.key,
            "guestSshPrivateKey": self.guest_key,
            "pveSshKnownHosts": self.known_hosts,
            "pveTokenTfvars": self.token,
            "pveCaPem": self.ca,
        }
        write_private(
            self.state,
            {
                "runId": self.run_id,
                "runRoot": str(self.run_root),
                "phase": "DONE",
                "executionMode": "production",
                "cleanupExit": 0,
                "inventoryExit": 0,
                "effectiveLifecycle": {
                    "executionMode": "production",
                    "contractSha256": digest(self.contract),
                },
                "inputs": {
                    name: {"pinned": str(path), "sha256": digest(path)}
                    for name, path in pins.items()
                },
            },
        )
        self.fake_bin = self.base / "bin"
        self.fake_bin.mkdir()
        self.calls = self.base / "ssh-calls.txt"
        self.write_fake_commands()

    def tearDown(self):
        self.temp.cleanup()

    def write_inventory(self, *, count: int = 0, status: str = "complete") -> None:
        write_private(
            self.inventory,
            {
                "scopes": [
                    {"name": name, "count": count if name == "tofu-state" else 0, "queryStatus": status}
                    for name in SCOPES
                ],
            },
        )

    def write_fake_commands(self) -> None:
        systemctl = self.fake_bin / "systemctl"
        systemctl.write_text(
            "#!/bin/sh\nprintf '%s\\n' \"${UNIT_STATE:-inactive}\"\n",
            encoding="utf-8",
        )
        systemctl.chmod(0o755)
        ssh = self.fake_bin / "ssh"
        ssh.write_text(
            "#!/bin/sh\n"
            "printf '%s\\n' \"$*\" >> \"$CALLS\"\n"
            "case \"$*\" in\n"
            "  *'pveum user token list'*)\n"
            "    if [ -n \"${SSH_TOKEN_LIST_JSON:-}\" ]; then\n"
            "      printf '%s\\n' \"$SSH_TOKEN_LIST_JSON\"\n"
            "    else\n"
            "      printf '%s\\n' '[{\"tokenid\":\"release-qa@pve!run-1\"}]'\n"
            "    fi\n"
            "    ;;\n"
            "  *'pveum user token delete'*) exit 0 ;;\n"
            "  *) exit 77 ;;\n"
            "esac\n",
            encoding="utf-8",
        )
        ssh.chmod(0o755)

    def hook(self, *, supervised: bool = False, **overrides: str) -> subprocess.CompletedProcess[str]:
        env = os.environ.copy()
        env["PATH"] = f"{self.fake_bin}:{env['PATH']}"
        env["CALLS"] = str(self.calls)
        env.update(overrides)
        command = [str(self.script)]
        if supervised:
            command.append("--supervised")
        command.append(self.run_id)
        return subprocess.run(
            command,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env=env,
            check=False,
        )

    def set_contract_ssh_host(self, ssh_host: str) -> None:
        contract = json.loads(self.contract.read_text(encoding="utf-8"))
        contract["pve"]["sshHost"] = ssh_host
        write_private(self.contract, contract)
        state = json.loads(self.state.read_text(encoding="utf-8"))
        contract_sha = digest(self.contract)
        state["effectiveLifecycle"]["contractSha256"] = contract_sha
        state["inputs"]["contract"]["sha256"] = contract_sha
        write_private(self.state, state)

    def test_terminal_zero_run_revokes_only_the_run_scoped_identity_without_secret(self):
        result = self.hook()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('"tokenRevocation":"revoked"', result.stdout)
        calls = self.calls.read_text(encoding="utf-8")
        self.assertIn("StrictHostKeyChecking=yes", calls)
        self.assertIn(f"UserKnownHostsFile={self.known_hosts}", calls)
        self.assertIn("GlobalKnownHostsFile=/dev/null", calls)
        self.assertIn("root@pve01.example.test", calls)
        self.assertIn("pveum user token list release-qa@pve --output-format json", calls)
        self.assertIn("pveum user token delete release-qa@pve run-1", calls)
        self.assertNotIn(self.token_secret, result.stdout + result.stderr + calls)
        receipt = self.runtime / "evidence/final-token-revocation/revocation.json"
        self.assertEqual(receipt.stat().st_mode & 0o777, 0o600)
        receipt_text = receipt.read_text(encoding="utf-8")
        self.assertNotIn(self.token_secret, receipt_text)
        self.assertEqual(json.loads(receipt_text)["status"], "revoked")

        first_calls = calls
        again = self.hook(SSH_TOKEN_LIST_JSON="[]")
        self.assertEqual(again.returncode, 0, again.stderr)
        self.assertIn("already-recorded", again.stdout)
        all_calls = self.calls.read_text(encoding="utf-8")
        self.assertEqual(all_calls.count("pveum user token list"), 2)
        self.assertEqual(all_calls.count("pveum user token delete"), 1)
        self.assertNotEqual(all_calls, first_calls)

    def test_supervised_post_zero_revokes_while_the_supervisor_unit_is_active(self):
        state = json.loads(self.state.read_text(encoding="utf-8"))
        state["phase"] = "REVOKING_TOKEN"
        write_private(self.state, state)
        result = self.hook(supervised=True, UNIT_STATE="active")
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.calls.read_text(encoding="utf-8")
        self.assertIn("pveum user token delete release-qa@pve run-1", calls)

    def test_failed_but_proven_zero_terminal_run_revokes_the_run_token(self):
        state = json.loads(self.state.read_text(encoding="utf-8"))
        state["phase"] = "FAILED"
        write_private(self.state, state)
        result = self.hook()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('"tokenRevocation":"revoked"', result.stdout)

    def test_ipv4_literal_pve_host_refuses_before_ssh(self):
        self.set_contract_ssh_host("192.0.2.4")
        result = self.hook()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not a DNS FQDN", result.stderr)
        self.assertFalse(self.calls.exists())

    def test_nonzero_or_inflight_lifecycle_refuses_before_ssh(self):
        self.write_inventory(count=1)
        result = self.hook()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.calls.exists())

        self.write_inventory()
        state = json.loads(self.state.read_text(encoding="utf-8"))
        state["phase"] = "CLEANING"
        write_private(self.state, state)
        result = self.hook()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.calls.exists())

    def test_tampered_token_refuses_without_exposing_or_using_secret(self):
        write_private(
            self.token,
            f'pve_api_token = "release-qa@pve!other-run={self.token_secret}"\n',
        )
        result = self.hook()
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn(self.token_secret, result.stdout + result.stderr)
        self.assertFalse(self.calls.exists())

    def test_token_owner_mismatch_refuses_even_with_an_updated_pin_digest(self):
        write_private(
            self.token,
            f'pve_api_token = "other-owner@pve!{self.run_id}={self.token_secret}"\n',
        )
        state = json.loads(self.state.read_text(encoding="utf-8"))
        state["inputs"]["pveTokenTfvars"]["sha256"] = digest(self.token)
        write_private(self.state, state)
        result = self.hook()
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn(self.token_secret, result.stdout + result.stderr)
        self.assertFalse(self.calls.exists())

    def test_missing_exact_token_on_pve_never_issues_a_delete(self):
        result = self.hook(SSH_TOKEN_LIST_JSON="[]")
        self.assertNotEqual(result.returncode, 0)
        calls = self.calls.read_text(encoding="utf-8")
        self.assertIn("pveum user token list", calls)
        self.assertNotIn("pveum user token delete", calls)

    def test_hook_source_disallows_unpinned_or_api_based_token_revocation(self):
        source = (ROOT / "drivers/revoke-pve-run-token.sh").read_text(encoding="utf-8")
        self.assertIn("StrictHostKeyChecking=yes", source)
        self.assertIn('UserKnownHostsFile="$pve_ssh_known_hosts"', source)
        self.assertIn("GlobalKnownHostsFile=/dev/null", source)
        self.assertIn('"root@$pve_ssh_host"', source)
        self.assertIn("pveum user token delete", source)
        self.assertNotIn("pveum user token remove", source)
        self.assertNotIn("StrictHostKeyChecking=no", source)
        self.assertNotIn("accept-new", source)
        self.assertNotIn("curl ", source)
        self.assertNotIn("tofu ", source)
