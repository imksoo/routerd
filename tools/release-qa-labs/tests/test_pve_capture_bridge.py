import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class PVECaptureBridgeTests(unittest.TestCase):
    """Offline-only coverage of the root-PVE live-bridge safety boundary."""

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
        for name in ("common.sh", "pve-capture-bridge.sh"):
            shutil.copy2(ROOT / "drivers" / name, self.drivers / name)
        (self.drivers / "pve-capture-bridge.sh").chmod(0o755)

        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.calls = self.root / "calls.log"
        self.bridge_state = self.root / "bridge-state"
        self.bridge_state.write_text("absent\n", encoding="utf-8")
        self.artifact = self.runtime / "artifact"
        self.artifact.write_bytes(b"fixture artifact")
        self.tf = self.runtime / "tf"
        self.tf.mkdir()
        self.tfvars = self.runtime / "terraform.tfvars"
        self.tfvars.write_text(
            "\n".join((
                'run_id = "run-1"',
                'commit = "release-commit"',
                'expires_at = "2030-01-01T00:00:00Z"',
                'pve_node_name = "pve01"',
                'pve_ssh_host = "pve01.lain.local"',
                'pve_endpoint = "https://pve01.lain.local:8006/"',
            )) + "\n",
            encoding="utf-8",
        )
        self.tfvars.chmod(0o600)
        secrets = self.runtime / "secrets"
        secrets.mkdir(mode=0o700)
        key = secrets / "pve_ssh"
        key.write_text("fixture private key\n", encoding="utf-8")
        key.chmod(0o600)
        guest_key = secrets / "guest_ssh"
        guest_key.write_text("fixture guest private key\n", encoding="utf-8")
        guest_key.chmod(0o600)
        known_hosts = secrets / "pve-known_hosts"
        known_hosts.write_text(
            "pve01.lain.local ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEZpeHR1cmU=\n",
            encoding="utf-8",
        )
        known_hosts.chmod(0o600)
        run_env = self.runtime / "run.env.json"
        run_env.write_text(json.dumps({
            "pveSshPrivateKey": str(key),
            "guestSshPrivateKey": str(guest_key),
            "pveSshKnownHosts": str(known_hosts),
        }), encoding="utf-8")
        run_env.chmod(0o600)
        self.contract = self.runtime / "contract.json"
        self.contract.write_text(json.dumps({
            "runId": "run-1", "labsCommit": "qa-commit",
            "routerdArtifact": {
                "path": str(self.artifact), "version": "v1", "commit": "release-commit",
            },
            "tofu": {
                "workingDirectory": str(self.tf), "statePath": str(self.tf / "state"),
                "variablesPath": str(self.tfvars), "outputPath": str(self.tf / "output.json"),
            },
            "lifecycle": {"ttl": "55m", "heartbeatStale": "5m"},
            "pve": {
                "node": "pve01", "sshHost": "pve01.lain.local", "datastore": "qnap",
                "underlayBridge": "vmbr0", "captureBridge": "rsam-run-1",
                "templateStage": {
                    "sourceNode": "pve01", "sourceTemplateVMID": 9000,
                    "vmid": 9599, "datastore": "qnap",
                },
                "vmids": {
                    "pve-leaf-a": 9600, "pve-client-a": 9601,
                    "pve-leaf-b": 9602, "pve-client-b": 9603,
                    "pve-rr-a": 9604, "pve-rr-b": 9605,
                },
            },
        }), encoding="utf-8")
        self.contract.chmod(0o600)

        self.make("git", f'''case " $* " in
  *" rev-parse --show-toplevel "*) echo "{self.repo}";;
  *" rev-parse HEAD "*) echo qa-commit;;
esac
exit 0''')
        self.make("ssh", '''echo "ssh $*" >>"$CALLS"
command=${!#}
state=$(cat "$BRIDGE_STATE")
alias='routerd-release-qa:run=run-1:expires=2030-01-01T00:00:00Z'
case "$command" in
  "test ! -e /etc/network/interfaces.new")
    [ "${PENDING_NETWORK:-0}" = 0 ] || exit 1;;
  *"ip -4 -o addr show dev"*)
    printf '%s\\n' '2: vmbr0    inet 192.0.2.10/24 brd 192.0.2.255 scope global vmbr0'
    printf '%s\\n' 'default via 192.0.2.1 dev vmbr0';;
  *"pvesh get /cluster/resources"*)
    if [ "${LIVE_VM:-0}" = 1 ]; then printf '%s\\n' '[{"vmid":9599}]'; else printf '%s\\n' '[]'; fi;;
  *"pvesh get /nodes/pve01/network"*)
    case "$state" in
      persistent) printf '%s\\n' '[{"iface":"vmbr0"},{"iface":"rsam-run-1"}]';;
      malformed) printf '%s\\n' '{}';;
      *) printf '%s\\n' '[{"iface":"vmbr0"}]';;
    esac;;
  *"pvesh create /nodes/pve01/network"*|*"pvesh delete /nodes/pve01/network"*)
    echo 'persistent PVE network writes are forbidden' >&2; exit 98;;
  *"ip -j -d link show"*)
    case "$state" in
      absent|persistent) printf '%s\\n' '[{"ifname":"vmbr0"}]';;
      safe|unsafe|foreign) printf '%s\\n' '[{"ifname":"vmbr0"},{"ifname":"rsam-run-1","linkinfo":{"info_kind":"bridge"}}]';;
      malformed) printf '%s\\n' '{}';;
    esac;;
  *"ip -j addr show dev rsam-run-1"*)
    case "$state" in
      unsafe) printf '%s\\n' '[{"addr_info":[{"family":"inet"}]}]';;
      safe|foreign) printf '%s\\n' '[{"addr_info":[]}]';;
      *) exit 1;;
    esac;;
  *"bridge -j link show master rsam-run-1"*)
    case "$state" in
      unsafe) printf '%s\\n' '[{"ifname":"eno1"}]';;
      safe|foreign) printf '%s\\n' '[]';;
      *) exit 1;;
    esac;;
  *"cat /proc/sys/net/ipv6/conf/rsam-run-1/disable_ipv6"*)
    case "$state" in safe|unsafe|foreign) printf '%s' 1;; *) exit 1;; esac;;
  *"cat /sys/class/net/rsam-run-1/ifalias"*)
    case "$state" in safe) printf '%s\\n' "$alias";; foreign) printf '%s\\n' foreign-owner;; *) exit 1;; esac;;
  *"ip link add name rsam-run-1 type bridge"*)
    [ "$state" = absent ] || exit 1
    case "$command" in *" alias $alias "*"printf 1 > /proc/sys/net/ipv6/conf/rsam-run-1/disable_ipv6"*"ip link set dev rsam-run-1 up"*) :;; *) exit 97;; esac
    echo safe >"$BRIDGE_STATE";;
  *"ip link delete dev rsam-run-1 type bridge"*)
    [ "$state" = safe ] || exit 1
    echo absent >"$BRIDGE_STATE";;
  *) echo "unhandled ssh command: $command" >&2; exit 99;;
esac''')

    def tearDown(self):
        self.temp.cleanup()

    def make(self, name, body):
        path = self.bin / name
        path.write_text("#!/usr/bin/env bash\nset -eu\n" + body + "\n", encoding="utf-8")
        path.chmod(0o755)

    def run_driver(self, action, *, state=None, live_vm=False, pending=False):
        if state is not None:
            self.bridge_state.write_text(state + "\n", encoding="utf-8")
        evidence = self.runtime / f"evidence/{action}.json"
        environment = os.environ.copy()
        environment.update({
            "PATH": f"{self.bin}:/usr/bin:/bin",
            "CALLS": str(self.calls),
            "BRIDGE_STATE": str(self.bridge_state),
            "LIVE_VM": "1" if live_vm else "0",
            "PENDING_NETWORK": "1" if pending else "0",
            "ROUTERD_RELEASE_QA_PINNED_CONTRACT": str(self.contract),
            "ROUTERD_RELEASE_QA_PINNED_RUN_ENV": str(self.runtime / "run.env.json"),
        })
        return subprocess.run(
            [str(self.drivers / "pve-capture-bridge.sh"), f"--{action}", "--evidence", str(evidence)],
            text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=environment, check=False,
        ), evidence

    def assert_no_persistent_network_writes(self):
        calls = self.calls.read_text(encoding="utf-8")
        self.assertNotIn("pvesh create /nodes/pve01/network", calls)
        self.assertNotIn("pvesh delete /nodes/pve01/network", calls)

    def test_ensure_creates_owned_live_bridge_then_remove_waits_for_zero_vms(self):
        result, evidence = self.run_driver("ensure")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.bridge_state.read_text().strip(), "safe")
        proof = json.loads(evidence.read_text())
        self.assertEqual(proof["safety"], "portless-no-address-no-gateway-no-persistent-config")
        self.assertEqual(proof["ownershipAlias"], "routerd-release-qa:run=run-1:expires=2030-01-01T00:00:00Z")
        calls = self.calls.read_text()
        create = next(line for line in calls.splitlines() if "ip link add name rsam-run-1 type bridge" in line)
        self.assertLess(create.index(" alias routerd-release-qa:"), create.index("printf 1 > /proc"))
        self.assertLess(create.index("printf 1 > /proc"), create.index("ip link set dev rsam-run-1 up"))
        self.assertIn("StrictHostKeyChecking=yes", calls)
        self.assertIn("UserKnownHostsFile=", calls)
        self.assertIn("GlobalKnownHostsFile=/dev/null", calls)
        self.assert_no_persistent_network_writes()

        calls_before_remove = self.calls.read_text()
        result, _ = self.run_driver("remove", live_vm=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.bridge_state.read_text().strip(), "safe")
        self.assertNotIn(
            "ip link delete dev rsam-run-1",
            self.calls.read_text()[len(calls_before_remove):],
        )

        result, evidence = self.run_driver("remove")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.bridge_state.read_text().strip(), "absent")
        self.assertEqual(json.loads(evidence.read_text())["result"], "deleted-after-authoritative-zero-vm-inventory")
        self.assert_no_persistent_network_writes()

    def test_existing_persistent_configuration_is_never_adopted_or_deleted(self):
        result, _ = self.run_driver("ensure", state="persistent")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("ip link add name rsam-run-1", self.calls.read_text())

        self.calls.write_text("", encoding="utf-8")
        result, _ = self.run_driver("remove", state="persistent")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("ip link delete dev rsam-run-1", self.calls.read_text())
        self.assert_no_persistent_network_writes()

    def test_ensure_and_remove_fail_closed_for_foreign_unsafe_or_malformed_live_state(self):
        for action, state in (("ensure", "safe"), ("remove", "foreign"),
                              ("remove", "unsafe"), ("ensure", "malformed")):
            with self.subTest(action=action, state=state):
                self.calls.write_text("", encoding="utf-8")
                result, _ = self.run_driver(action, state=state)
                self.assertNotEqual(result.returncode, 0)
                self.assert_no_persistent_network_writes()

    def test_pending_pve_network_configuration_refuses_root_mutation(self):
        result, _ = self.run_driver("ensure", pending=True)
        self.assertNotEqual(result.returncode, 0)
        calls = self.calls.read_text() if self.calls.exists() else ""
        self.assertNotIn("ip link add name rsam-run-1", calls)
        self.assert_no_persistent_network_writes()


if __name__ == "__main__":
    unittest.main()
