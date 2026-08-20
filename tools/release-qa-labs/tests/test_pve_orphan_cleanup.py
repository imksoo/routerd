import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class PVEOrphanCleanupTests(unittest.TestCase):
    """Offline-only tests for the exact identity fence before `qm destroy`."""

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        root = Path(self.temp.name)
        self.run_root = root / "runs/run-1"
        self.repo = self.run_root / "repo"
        self.framework = self.repo / "tools/release-qa-labs"
        self.drivers = self.framework / "drivers"
        self.runtime = self.run_root / "runtime"
        self.drivers.mkdir(parents=True)
        self.runtime.mkdir()
        for name in ("common.sh", "pve-orphan-cleanup.sh"):
            shutil.copy2(ROOT / "drivers" / name, self.drivers / name)
            (self.drivers / name).chmod(0o755)
        self.bin = root / "bin"
        self.bin.mkdir()
        self.calls = root / "ssh-calls"
        self.qm_calls = root / "qm-calls"
        self.qm_operations = root / "qm-operations"
        self.state = root / "state.json"
        self.state.write_text("{}\n", encoding="utf-8")
        artifact = self.runtime / "routerd.tar.gz"
        artifact.write_bytes(b"artifact")
        tf_dir = self.runtime / "tf"
        tf_dir.mkdir()
        tfvars = self.runtime / "terraform.tfvars"
        tfvars.write_text(
            'run_id = "run-1"\ncommit = "release-commit"\n'
            'pve_node_name = "pve01"\npve_ssh_host = "pve01.lain.local"\n'
            'pve_endpoint = "https://pve01.lain.local:8006/"\n',
            encoding="utf-8",
        )
        tfvars.chmod(0o600)
        secrets = self.runtime / "secrets"
        secrets.mkdir(mode=0o700)
        for name, content in (
            ("pve_ssh", "pve-fixture-key\n"),
            ("guest_ssh", "guest-fixture-key\n"),
            ("pve-known_hosts", "pve-fixture-host-key\n"),
        ):
            path = secrets / name
            path.write_text(content, encoding="utf-8")
            path.chmod(0o600)
        env = self.runtime / "run.env.json"
        env.write_text(json.dumps({
            "pveSshPrivateKey": str(secrets / "pve_ssh"),
            "guestSshPrivateKey": str(secrets / "guest_ssh"),
            "pveSshKnownHosts": str(secrets / "pve-known_hosts"),
        }), encoding="utf-8")
        env.chmod(0o600)
        self.contract = self.runtime / "contract.json"
        self.contract.write_text(json.dumps({
            "runId": "run-1", "qaImplementation": {"commit": "qa-commit"},
            "routerdArtifact": {"path": str(artifact), "version": "v1", "commit": "release-commit"},
            "tofu": {"workingDirectory": str(tf_dir), "statePath": str(tf_dir / "state"),
                     "variablesPath": str(tfvars), "outputPath": str(tf_dir / "output")},
            "lifecycle": {"ttl": "55m", "heartbeatStale": "5m"},
            "pve": {
                "node": "pve01", "sshHost": "pve01.lain.local",
                "templateStage": {"sourceNode": "pve01", "vmid": 9599},
                "vmids": {"pve-leaf-a": 9600, "pve-client-a": 9601,
                            "pve-leaf-b": 9602, "pve-client-b": 9603,
                            "pve-rr-a": 9604, "pve-rr-b": 9605},
                "rrNodes": {"pve-rr-a": {"node": "pve05", "sshHost": "pve05.lain.local"},
                            "pve-rr-b": {"node": "pve06", "sshHost": "pve06.lain.local"}},
            },
        }), encoding="utf-8")
        self.contract.chmod(0o600)
        self._write_fake("git", """case \" $* \" in
  *\" rev-parse --show-toplevel \"*) echo \"%s\";;
  *\" rev-parse HEAD \"*) echo qa-commit;;
esac
""" % self.repo)
        self._write_fake("qm", """operation=${1:?missing operation}
vmid=${2:?missing vmid}
host=${FAKE_REMOTE_HOST:?missing fake remote host}
state=$(jq -r --arg host "$host" --arg vmid "$vmid" '.[$host + ":" + $vmid] // "absent"' "$VM_STATE")
printf '%s:%s:%s\\n' "$operation" "$host" "$vmid" >>"$QM_OPERATIONS"
case "$vmid" in
  9599) label=pve-template-stage; role=template-stage ;;
  9600) label=pve-leaf-a; role=leaf ;;
  9601) label=pve-client-a; role=client ;;
  9602) label=pve-leaf-b; role=leaf ;;
  9603) label=pve-client-b; role=client ;;
  9604) label=pve-rr-a; role=rr ;;
  9605) label=pve-rr-b; role=rr ;;
  *) exit 94 ;;
esac
case "$operation" in
  config)
    [ "$state" != absent ] || exit 92
    printf '%s:%s\\n' "$host" "$vmid" >>"$QM_CALLS"
    config_count=$(grep -Fc "$host:$vmid" "$QM_CALLS")
    if [ "$state" = foreign ] || { [ "$state" = swap ] && [ "$config_count" -ge 3 ]; }; then
      printf 'name: foreign\\ndescription: foreign\\ntags: unrelated\\n'
    else
      printf 'name: routerd-run-1-%s\\ndescription: test; run=run-1;\\ntags: routerd;sam-e2e;run-1;%s\\n' "$label" "$role"
    fi
    ;;
  status) [ "$state" != absent ] || exit 92; printf 'status: stopped\\n' ;;
  stop) [ "$state" = exact ] || exit 93 ;;
  destroy)
    [ "$state" = exact ] || exit 93
    jq --arg host "$host" --arg vmid "$vmid" 'del(.[$host + ":" + $vmid])' "$VM_STATE" >"$VM_STATE.next"
    mv "$VM_STATE.next" "$VM_STATE"
    ;;
  *) exit 96 ;;
esac
""")
        self._write_fake("ssh", """echo \"$*\" >>\"$CALLS\"
command=${!#}
bash -n -c \"$command\"
host=$(printf '%s\\n' \"$*\" | sed -n 's/.*root@\\([^ ]*\\).*/\\1/p')
if printf '%s' \"$command\" | grep -Fq 'pvesh get /cluster/resources'; then
  if jq -e 'to_entries[] | .value == \"cluster-error\"' \"$VM_STATE\" >/dev/null; then exit 91; fi
  jq -c 'to_entries | map(.key as $key | ($key | capture("^(?<host>[^:]+):(?<vmid>[0-9]+)$")) as $id | {
    vmid: ($id.vmid | tonumber), node: (if .value == \"wrong-node\" then \"wrong\" else ($id.host | split(\".\")[0]) end),
    type: (if .value == \"lxc\" then \"lxc\" else \"qemu\" end)
  })' \"$VM_STATE\"
  exit 0
fi
FAKE_REMOTE_HOST="$host" PATH="$REMOTE_BIN:$PATH" bash -c "$command"
""")

    def tearDown(self):
        self.temp.cleanup()

    def _write_fake(self, name, body):
        path = self.bin / name
        path.write_text("#!/usr/bin/env bash\nset -euo pipefail\n" + body, encoding="utf-8")
        path.chmod(0o755)

    def run_cleanup(self, state):
        self.state.write_text(json.dumps(state), encoding="utf-8")
        evidence = self.runtime / "evidence/orphan.json"
        env = os.environ.copy()
        env.update({
            "PATH": f"{self.bin}:/usr/bin:/bin", "CALLS": str(self.calls),
            "VM_STATE": str(self.state), "QM_CALLS": str(self.qm_calls),
            "QM_OPERATIONS": str(self.qm_operations),
            "REMOTE_BIN": str(self.bin),
            "ROUTERD_RELEASE_QA_PINNED_CONTRACT": str(self.contract),
            "ROUTERD_RELEASE_QA_PINNED_RUN_ENV": str(self.runtime / "run.env.json"),
        })
        result = subprocess.run(
            [str(self.drivers / "pve-orphan-cleanup.sh"), "--evidence", str(evidence)],
            text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env, check=False,
        )
        calls = self.calls.read_text(encoding="utf-8") if self.calls.exists() else ""
        return result, evidence, calls

    def test_absent_targets_are_not_destroyed(self):
        result, evidence, calls = self.run_cleanup({})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn("qm destroy", calls)
        self.assertEqual(len(json.loads(evidence.read_text())["targets"]), 7)

    def test_exact_identity_is_destroyed(self):
        result, evidence, calls = self.run_cleanup({"pve05.lain.local:9604": "exact"})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("qm destroy 9604 --purge 1", calls)
        self.assertNotIn("pve05.lain.local:9604", json.loads(self.state.read_text()))
        actions = [item["action"] for item in json.loads(evidence.read_text())["targets"]]
        self.assertIn("destroyed-after-identity-match", actions)

    def test_foreign_identity_is_not_destroyed(self):
        result, evidence, calls = self.run_cleanup({"pve05.lain.local:9604": "foreign"})
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn(
            "destroy:pve05.lain.local:9604",
            self.qm_operations.read_text(encoding="utf-8"),
        )
        actions = [item["action"] for item in json.loads(evidence.read_text())["targets"]]
        self.assertIn("refused-identity-mismatch", actions)

    def test_cluster_failure_is_not_treated_as_absence(self):
        result, _, calls = self.run_cleanup({"pve05.lain.local:9604": "cluster-error"})
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("qm config", calls)
        self.assertNotIn("qm destroy", calls)

    def test_misplaced_or_non_qemu_target_blocks_all_deletes(self):
        for rejected in ("wrong-node", "lxc"):
            with self.subTest(rejected=rejected):
                result, evidence, calls = self.run_cleanup({
                    "pve01.lain.local:9600": rejected,
                    "pve05.lain.local:9604": "exact",
                })
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn("qm destroy", calls)
                actions = [item["action"] for item in json.loads(evidence.read_text())["targets"]]
                self.assertIn("refused-cluster-target-mismatch", actions)

    def test_workloads_are_destroyed_before_the_template_stage(self):
        state = {
            "pve01.lain.local:9600": "exact",
            "pve01.lain.local:9601": "exact",
            "pve01.lain.local:9602": "exact",
            "pve01.lain.local:9603": "exact",
            "pve05.lain.local:9604": "exact",
            "pve06.lain.local:9605": "exact",
            "pve01.lain.local:9599": "exact",
        }
        result, _, calls = self.run_cleanup(state)
        self.assertEqual(result.returncode, 0, result.stderr)
        positions = [calls.index(f"qm destroy {vmid} --purge 1") for vmid in (9600, 9601, 9602, 9603, 9604, 9605, 9599)]
        self.assertEqual(positions, sorted(positions))

    def test_destroy_rechecks_identity_after_admission(self):
        result, evidence, calls = self.run_cleanup({"pve05.lain.local:9604": "swap"})
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn(
            "destroy:pve05.lain.local:9604",
            self.qm_operations.read_text(encoding="utf-8"),
        )
        self.assertGreaterEqual(
            self.qm_calls.read_text(encoding="utf-8").splitlines().count("pve05.lain.local:9604"),
            3,
        )
        actions = [item["action"] for item in json.loads(evidence.read_text())["targets"]]
        self.assertIn("destroy-failed", actions)


if __name__ == "__main__":
    unittest.main()
