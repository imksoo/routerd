import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class PVESubstratePreflightTests(unittest.TestCase):
    """Offline-only: curl and SSH are private executable fixtures."""

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
        for name in ("common.sh", "pve-substrate-preflight.sh"):
            shutil.copy2(ROOT / "drivers" / name, self.drivers / name)
        (self.drivers / "pve-substrate-preflight.sh").chmod(0o755)

        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.calls = self.root / "calls.log"
        self.artifact = self.runtime / "routerd-v1-linux-amd64.tar.gz"
        self.artifact.write_bytes(b"artifact")
        self.tf = self.runtime / "tf"
        self.tf.mkdir()
        self.tfvars = self.runtime / "terraform.tfvars"
        self.write_tfvars("template")
        self.token = self.runtime / "secrets/pve-token.tfvars"
        self.token.parent.mkdir(mode=0o700)
        self.token.write_text('pve_api_token = "mutable-source-token"\n', encoding="utf-8")
        self.token.chmod(0o600)
        self.pinned_token = self.runtime / "pinned/pve-token.tfvars"
        self.pinned_token.parent.mkdir(mode=0o700)
        self.pinned_token.write_text('pve_api_token = "fixture-pinned-token"\n', encoding="utf-8")
        self.pinned_token.chmod(0o600)
        self.ca = self.runtime / "secrets/pve-ca.pem"
        self.ca.write_text(
            "-----BEGIN CERTIFICATE-----\nfixture\n-----END CERTIFICATE-----\n",
            encoding="utf-8",
        )
        self.ca.chmod(0o600)
        self.pinned_ca = self.runtime / "pinned/pve-ca.pem"
        self.pinned_ca.write_text(self.ca.read_text(encoding="utf-8"), encoding="utf-8")
        self.pinned_ca.chmod(0o600)
        self.ssh_key = self.runtime / "secrets/pve_ssh"
        self.ssh_key.write_text("fixture private key\n", encoding="utf-8")
        self.ssh_key.chmod(0o600)
        self.guest_ssh_key = self.runtime / "secrets/guest_ssh"
        self.guest_ssh_key.write_text("fixture guest private key\n", encoding="utf-8")
        self.guest_ssh_key.chmod(0o600)
        self.pve_known_hosts = self.runtime / "secrets/pve-known_hosts"
        self.pve_known_hosts.write_text(
            "\n".join((
                "pve01.lain.local ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEZpeHR1cmUtcHZlLWhvc3Qta2V5LTE=",
                "pve02.lain.local ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEZpeHR1cmUtcHZlLWhvc3Qta2V5LTI=",
                "pve03.lain.local ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEZpeHR1cmUtcHZlLWhvc3Qta2V5LTM=",
            )) + "\n",
            encoding="utf-8",
        )
        self.pve_known_hosts.chmod(0o600)
        self.pinned_known_hosts = self.runtime / "pinned/pve-known_hosts"
        self.pinned_known_hosts.write_text(self.pve_known_hosts.read_text(encoding="utf-8"), encoding="utf-8")
        self.pinned_known_hosts.chmod(0o600)
        run_env = self.runtime / "run.env.json"
        run_env.write_text(json.dumps({
            "pveTokenTfvars": str(self.token), "pveCaPem": str(self.ca),
            "pveSshPrivateKey": str(self.ssh_key), "guestSshPrivateKey": str(self.guest_ssh_key),
            "pveSshKnownHosts": str(self.pve_known_hosts),
        }), encoding="utf-8")
        run_env.chmod(0o600)
        self.contract = self.runtime / "contract.json"
        self.write_contract("template")

        self.make("git", f'''case " $* " in
 *" rev-parse --show-toplevel "*) echo "{self.repo}";;
 *" rev-parse HEAD "*) echo qa-commit;;
esac
exit 0''')
        self.make("timeout", '''echo "timeout $*" >>"$CALLS"
shift
exec "$@"''')
        self.make("curl", '''echo "curl $*" >>"$CALLS"
[ "${FAILURE:-}" = api ] && exit 22
out= header= ca=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) out=$2; shift 2;;
    --header) header=${2#@}; shift 2;;
    --cacert) ca=$2; shift 2;;
    *) shift;;
  esac
done
[ -n "$out" ] || exit 97
[ "$ca" = "$PINNED_PVE_CA" ] || exit 95
[ -n "$header" ] && [ -f "$header" ] &&
  grep -Fqx 'Authorization: PVEAPIToken=fixture-pinned-token' "$header" || exit 96
case "${FAILURE:-}" in
  api-malformed) printf '%s\\n' '{"data":{}}' >"$out";;
  planned-vmid) printf '%s\\n' '{"data":[{"vmid":9600},{"vmid":9000,"node":"pve01","template":1}]}' >"$out";;
  stage-vmid) printf '%s\\n' '{"data":[{"vmid":9599},{"vmid":9000,"node":"pve01","template":1}]}' >"$out";;
  missing-template) printf '%s\\n' '{"data":[{"vmid":9000,"node":"pve01","template":0}]}' >"$out";;
  *) printf '%s\\n' '{"data":[{"vmid":9000,"node":"pve01","template":1}]}' >"$out";;
esac''')
        self.make("ssh", '''echo "ssh $*" >>"$CALLS"
[ "${FAILURE:-}" = ssh ] && exit 255
command=${!#}
case "$command" in
  *"ip -j -d link show"*)
    case "${FAILURE:-}" in
      existing-live-capture) printf '%s\\n' '[{"ifname":"rsam-run-1","linkinfo":{"info_kind":"bridge"}}]';;
      *) printf '%s\\n' '[{"ifname":"vmbr0","linkinfo":{"info_kind":"bridge"}}]';;
    esac;;
  *"/network"*)
    case "${FAILURE:-}" in
      missing-underlay) printf '%s\\n' '[]';;
      existing-capture) printf '%s\\n' '[{"iface":"rsam-run-1"}]';;
      *) case "$command" in
           *"/nodes/pve01/"*) printf '%s\\n' '[{"iface":"vmbr0"}]';;
           *"/nodes/pve02/"*) printf '%s\\n' '[{"iface":"vmbr1"}]';;
           *"/nodes/pve03/"*) printf '%s\\n' '[{"iface":"vmbr2"}]';;
         esac;;
    esac;;
  *"/content"*)
    if [ "${FAILURE:-}" = missing-iso ]; then printf '%s\\n' '[]';
    else printf '%s\\n' '[{"volid":"local:iso/routerd.iso"}]'; fi;;
  *"/storage"*)
    if [ "${FAILURE:-}" = nonshared-store ]; then
      printf '%s\\n' '[{"storage":"qnap","active":1,"enabled":1,"shared":0}]';
    else
      printf '%s\\n' '[{"storage":"qnap","active":1,"enabled":1,"shared":1}]';
    fi;;
  *) exit 98;;
esac''')

    def tearDown(self):
        self.temp.cleanup()

    def make(self, name, body):
        path = self.bin / name
        path.write_text("#!/usr/bin/env bash\nset -eu\n" + body + "\n", encoding="utf-8")
        path.chmod(0o755)

    def write_tfvars(self, boot_source):
        lines = [
            'run_id = "run-1"',
            'commit = "release-commit"',
            'pve_node_name = "pve01"',
            'pve_ssh_host = "pve01.lain.local"',
            'pve_endpoint = "https://pve01.lain.local:8006/"',
            'pve_insecure = false',
            f'pve_boot_source = "{boot_source}"',
            'pve_datastore_id = "qnap"',
            'pve_underlay_bridge = "vmbr0"',
            'pve_capture_bridge = "rsam-run-1"',
        ]
        if boot_source == "template":
            lines.extend((
                "pve_template_vm_id = 9000",
                'pve_template_source_node = "pve01"',
                "pve_template_stage_vm_id = 9599",
                "pve_clone_full = true",
            ))
        else:
            lines.append('pve_iso_file_id = "local:iso/routerd.iso"')
        self.tfvars.write_text("\n".join(lines) + "\n", encoding="utf-8")
        self.tfvars.chmod(0o600)

    def write_contract(self, boot_source):
        contract = {
            "runId": "run-1", "labsCommit": "qa-commit",
            "routerdArtifact": {
                "path": str(self.artifact), "version": "v1", "commit": "release-commit",
            },
            "tofu": {
                "workingDirectory": str(self.tf), "statePath": str(self.tf / "state"),
                "variablesPath": str(self.tfvars), "outputPath": str(self.tf / "output"),
            },
            "lifecycle": {"ttl": "55m", "heartbeatStale": "5m"},
            "pve": {
                "node": "pve01", "sshHost": "pve01.lain.local", "datastore": "qnap",
                "bootSource": boot_source, "underlayBridge": "vmbr0", "captureBridge": "rsam-run-1",
                "managementAddressSource": "qga-dhcp",
                "vmids": {
                    "pve-leaf-a": 9600, "pve-client-a": 9601,
                    "pve-leaf-b": 9602, "pve-client-b": 9603,
                    "pve-rr-a": 9604, "pve-rr-b": 9605,
                },
                "rrNodes": {
                    "pve-rr-a": {
                        "node": "pve02", "sshHost": "pve02.lain.local", "underlayBridge": "vmbr1",
                    },
                    "pve-rr-b": {
                        "node": "pve03", "sshHost": "pve03.lain.local", "underlayBridge": "vmbr2",
                    },
                },
            },
        }
        if boot_source == "template":
            contract["pve"]["templateStage"] = {
                "sourceNode": "pve01", "sourceTemplateVMID": 9000,
                "vmid": 9599, "datastore": "qnap",
            }
        self.contract.write_text(json.dumps(contract), encoding="utf-8")
        self.contract.chmod(0o600)

    def run_preflight(self, failure=""):
        environment = os.environ.copy()
        for key in ("HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "ALL_PROXY", "all_proxy"):
            environment.pop(key, None)
        environment.update(
            PATH=f"{self.bin}:/usr/bin:/bin", CALLS=str(self.calls), FAILURE=failure,
            ROUTERD_RELEASE_QA_PINNED_PVE_TOKEN_TFVARS=str(self.pinned_token),
            ROUTERD_RELEASE_QA_PINNED_PVE_CA_PEM=str(self.pinned_ca),
            ROUTERD_RELEASE_QA_PINNED_PVE_SSH_KNOWN_HOSTS=str(self.pinned_known_hosts),
            PINNED_PVE_CA=str(self.pinned_ca),
            TF_VAR_pve_api_token="ambient-token-must-not-be-used",
        )
        return subprocess.run(
            [str(self.drivers / "pve-substrate-preflight.sh"), "--contract", str(self.contract)],
            text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=environment, check=False,
        )

    def test_template_read_only_preflight_passes_without_exposing_token(self):
        result = self.run_preflight()
        self.assertEqual(result.returncode, 0, result.stderr)
        evidence = self.runtime / "evidence/precheck/pve-substrate"
        output = json.loads((evidence / "result.json").read_text())
        self.assertEqual(output["status"], "pass")
        boot = json.loads((evidence / "boot-source.json").read_text())
        self.assertEqual(boot["mode"], "template")
        self.assertIn("PVE certification", output["mutationBoundary"])
        calls = self.calls.read_text()
        self.assertIn("root@pve01.lain.local", calls)
        self.assertIn("root@pve02.lain.local", calls)
        self.assertIn("root@pve03.lain.local", calls)
        self.assertIn("-n -i", calls)
        self.assertIn("BatchMode=yes", calls)
        self.assertIn("StrictHostKeyChecking=yes", calls)
        self.assertIn(f"UserKnownHostsFile={self.pinned_known_hosts}", calls)
        self.assertIn("GlobalKnownHostsFile=/dev/null", calls)
        self.assertIn("ConnectTimeout=10", calls)
        self.assertIn("--header @", calls)
        self.assertIn(f"--cacert {self.pinned_ca}", calls)
        self.assertNotIn("--insecure", calls)
        self.assertNotIn("fixture-pinned-token", calls)
        self.assertNotIn("mutable-source-token", calls)
        self.assertNotIn("fixture-pinned-token", result.stdout + result.stderr)
        self.assertNotIn("mutable-source-token", result.stdout + result.stderr)
        self.assertTrue(all((path.stat().st_mode & 0o077) == 0 for path in evidence.iterdir()))

    def test_iso_requires_the_exact_image_on_every_target_host(self):
        self.write_tfvars("iso")
        self.write_contract("iso")
        result = self.run_preflight()
        self.assertEqual(result.returncode, 0, result.stderr)
        boot = json.loads((self.runtime / "evidence/precheck/pve-substrate/boot-source.json").read_text())
        self.assertEqual(boot, {
            "isoFileID": "local:iso/routerd.iso",
            "mode": "iso",
            "verification": "ISO-present-on-every-leaf-and-RR-target",
        })

    def test_failures_are_closed_before_any_mutation(self):
        failures = ("api", "api-malformed", "ssh", "planned-vmid", "stage-vmid", "missing-template",
                    "missing-underlay", "existing-capture", "existing-live-capture", "nonshared-store")
        for failure in failures:
            with self.subTest(failure=failure):
                result = self.run_preflight(failure)
                self.assertNotEqual(result.returncode, 0, result.stderr)
                self.assertFalse((self.runtime / "evidence/precheck/pve-substrate/result.json").exists())

    def test_missing_iso_fails_closed(self):
        self.write_tfvars("iso")
        self.write_contract("iso")
        result = self.run_preflight("missing-iso")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.runtime / "evidence/precheck/pve-substrate/result.json").exists())

    def test_insecure_pve_tls_or_missing_pinned_ca_fails_before_api_access(self):
        self.tfvars.write_text(
            self.tfvars.read_text(encoding="utf-8").replace(
                "pve_insecure = false", "pve_insecure = true"
            ),
            encoding="utf-8",
        )
        result = self.run_preflight()
        self.assertNotEqual(result.returncode, 0)
        calls = self.calls.read_text(encoding="utf-8") if self.calls.exists() else ""
        self.assertNotIn("curl ", calls)

        self.write_tfvars("template")
        self.pinned_ca.unlink()
        result = self.run_preflight()
        self.assertNotEqual(result.returncode, 0)
        calls = self.calls.read_text(encoding="utf-8") if self.calls.exists() else ""
        self.assertNotIn("curl ", calls)

    def test_precheck_invokes_pve_gate_before_remote_cloud_egress_or_inventory(self):
        source = (ROOT / "drivers/precheck-driver.sh").read_text(encoding="utf-8")
        self.assertLess(source.index("pve-substrate-preflight.sh"), source.index("remote-egress-preflight.sh"))
        self.assertLess(source.index("pve-substrate-preflight.sh"), source.index("inventory-driver.sh"))


if __name__ == "__main__":
    unittest.main()
