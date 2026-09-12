"""Offline certification boundary: real schema/manifest code, no provider I/O."""

import contextlib
import copy
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock


QA_ROOT = Path(__file__).resolve().parents[1]
REPO_ROOT = QA_ROOT.parents[1]
SPEC = importlib.util.spec_from_file_location("schema_certification", REPO_ROOT / "scripts/release_certification.py")
certification = importlib.util.module_from_spec(SPEC)
# scripts/ is outside this test directory's bytecode ignore rule. Do not make
# the reviewed checkout dirty merely by loading the certification module.
original_bytecode_policy = sys.dont_write_bytecode
try:
    sys.dont_write_bytecode = True
    SPEC.loader.exec_module(certification)
finally:
    sys.dont_write_bytecode = original_bytecode_policy


class CertificationRunSchemaTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        artifact = self.root / "routerd-v20990101.0000-linux-amd64.tar.gz"
        artifact.write_bytes(b"local artifact fixture")
        self.contract = json.loads((QA_ROOT / "contract.example.json").read_text())
        self.contract.update(runId="fixture-local-certification", environment="offline", topology="full")
        self.contract["execution"].update(hostPolicy="local-supervised", sourcePolicy="local-pinned",
                                          host="offline.example.test", requireRemote=False)
        self.contract["routerdArtifact"].update(path=str(artifact), version="v20990101.0000",
            sha256=hashlib.sha256(artifact.read_bytes()).hexdigest(), commit="a" * 40,
            parentMainCommit="b" * 40)
        self.contract["qaImplementation"]["commit"] = "a" * 40
        for section in ("routerdArtifact", "qaImplementation"):
            self.contract[section]["scriptBlobs"] = {
                path: "c" * 64 for path in self.contract[section]["scriptBlobs"]}
        self.contract["pve"]["vmids"] = {
            "pve-leaf-a": 9011, "pve-client-a": 9012, "pve-leaf-b": 9013,
            "pve-rr-a": 9014, "pve-rr-b": 9015}
        self.contract["pve"]["templateStage"].update(sourceTemplateVMID=9000, vmid=9010)
        for name in ("pve-rr-a", "pve-rr-b"):
            self.contract["pve"]["rrNodes"][name]["vmid"] = self.contract["pve"]["vmids"][name]

    def run_certification(self, contract):
        path = self.root / "contract.json"
        path.write_text(json.dumps(contract))
        output = self.root / "certification.json"
        result = {"status": "pass", "repairs": [], "checks": [{
            "component": "pve", "provider": "pve", "name": "offline contract only",
            "result": "pass", "checkedAt": "2099-01-01T00:00:00Z"}]}
        with mock.patch.object(certification, "run_driver", return_value=result) as driver, \
             mock.patch.object(certification.subprocess, "run", side_effect=AssertionError("no external process permitted")), \
             contextlib.redirect_stdout(io.StringIO()):
            rc = certification.command_certify("pve", [
                "--environment", "offline", "--topology", "full", "--contract", str(path),
                "--driver", str(self.root / "never-executed-driver"), "--out", str(output)])
        self.assertEqual(rc, 0)
        driver.assert_called_once()
        return json.loads(output.read_text())

    def test_five_guest_local_contract_passes_before_mock_driver_and_is_preserved(self):
        manifest = self.run_certification(self.contract)
        self.assertEqual(manifest["run"]["pve"]["vmids"], self.contract["pve"]["vmids"])
        self.assertEqual(manifest["run"]["execution"]["sourcePolicy"], "local-pinned")
        self.assertFalse(manifest["run"]["execution"]["requireRemote"])

    def test_historical_six_guest_manifest_remains_readable(self):
        self.contract["pve"]["vmids"]["pve-client-b"] = 9016
        self.contract["execution"].update(hostPolicy="approved-remote", sourcePolicy="canonical-remote",
                                          requireRemote=True)
        manifest = self.run_certification(self.contract)
        self.assertEqual(len(manifest["run"]["pve"]["vmids"]), 6)

    def test_required_five_nodes_cannot_be_omitted(self):
        for node in self.contract["pve"]["vmids"]:
            contract = copy.deepcopy(self.contract)
            del contract["pve"]["vmids"][node]
            with self.subTest(node=node), self.assertRaises(certification.ContractError):
                self.run_certification(contract)

    def test_unknown_or_invalid_historical_extra_node_is_rejected(self):
        for name, value in (("pve-client-c", 9016), ("pve-client-b", 0), ("pve-client-b", "9016")):
            contract = copy.deepcopy(self.contract)
            contract["pve"]["vmids"][name] = value
            with self.subTest(name=name, value=value), self.assertRaises(certification.ContractError):
                self.run_certification(contract)


if __name__ == "__main__":
    unittest.main()
