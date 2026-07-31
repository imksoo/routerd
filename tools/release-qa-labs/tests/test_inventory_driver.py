import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class InventoryDriverTests(unittest.TestCase):
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
        for name in ("common.sh", "inventory-driver.sh"):
            shutil.copy2(ROOT / "drivers" / name, self.drivers / name)
        shutil.copy2(ROOT / "qa_guard.py", self.framework / "qa_guard.py")
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.calls = self.root / "calls"
        self.artifact = self.runtime / "artifact"
        self.artifact.write_bytes(b"artifact")
        self.tf = self.runtime / "tf"
        self.tf.mkdir()
        self.tfvars = self.runtime / "terraform.tfvars"
        self.tfvars.write_text(
            'run_id = "run-1"\ncommit = "release-commit"\n'
            'aws_region = "ap-northeast-1"\naws_profile = "fixture"\n'
            'oci_region = "ap-tokyo-1"\noci_profile = "fixture"\noci_compartment_id = "ocid.fixture"\n'
            'pve_node_name = "pve01"\npve_ssh_host = "pve01.lain.local"\n'
            'pve_endpoint = "https://pve01.lain.local:8006/"\n',
            encoding="utf-8",
        )
        self.tfvars.chmod(0o600)
        token = self.runtime / "pve-token.tfvars"
        token.write_text('pve_api_token = "fixture"\n', encoding="utf-8")
        token.chmod(0o600)
        ssh_key = self.runtime / "secrets/pve_ssh"
        ssh_key.parent.mkdir(mode=0o700)
        ssh_key.write_text("fixture key\n", encoding="utf-8")
        ssh_key.chmod(0o600)
        run_env_path = self.runtime / "run.env.json"
        run_env_path.write_text(json.dumps({
            "httpsProxy": "http://proxy.invalid:3128", "noProxy": "localhost,pve01",
            "pveTokenTfvars": str(token), "pveSshPrivateKey": str(ssh_key),
        }), encoding="utf-8")
        run_env_path.chmod(0o600)
        contract = {
            "runId": "run-1", "labsCommit": "qa-commit",
            "routerdArtifact": {"path": str(self.artifact), "version": "v1", "commit": "release-commit"},
            "tofu": {"workingDirectory": str(self.tf), "statePath": str(self.tf / "state"),
                     "variablesPath": str(self.tfvars), "outputPath": str(self.tf / "output")},
            "lifecycle": {"ttl": "75m", "heartbeatStale": "5m"},
            "pve": {"node": "pve01", "sshHost": "pve01.lain.local",
                    "vmids": [131, 141, 181, 182], "captureBridge": "vmbr999"},
        }
        contract_path = self.runtime / "contract.json"
        contract_path.write_text(json.dumps(contract), encoding="utf-8")
        contract_path.chmod(0o600)
        self.make("git", f'''case " $* " in
 *" rev-parse --show-toplevel "*) echo "{self.repo}";;
 *" rev-parse HEAD "*) echo qa-commit;;
esac
exit 0''')
        self.make("tofu", 'echo "$*" >>"$CALLS/tofu"; exit 0')

    def tearDown(self):
        self.temp.cleanup()

    def make(self, name, body):
        path = self.bin / name
        path.write_text("#!/bin/sh\nset -eu\n" + body + "\n", encoding="utf-8")
        path.chmod(0o755)

    def install_provider_fixtures(self, *, aws_tagged='{"ResourceTagMappingList":[]}',
                                  azure_exists="false", azure_resources="[]",
                                  oci_tagged='{"data":{"items":[]}}', pve_bridge="[]",
                                  oci_pages=None, pve_vm=False, qm_transient=False, fail=""):
        self.make("aws", f'''echo "$*" >>"$CALLS/aws"
[ "{fail}" = aws ] && exit 7
case " $* " in *" ec2 describe-instances "*) echo '{{"Reservations":[]}}';; *) printf '%s\\n' '{aws_tagged}';; esac''')
        self.make("az", f'''echo "$*" >>"$CALLS/az"
[ "{fail}" = az ] && exit 7
case " $* " in *" group exists "*) echo '{azure_exists}';; *) printf '%s\\n' '{azure_resources}';; esac''')
        pages = oci_pages or [oci_tagged]
        page_cases = []
        for index, page in enumerate(pages[1:], start=1):
            token = json.loads(pages[index - 1]).get("opc-next-page", "")
            page_cases.append(f'*" --page {token} "*) printf \'%s\\n\' \'{page}\';;')
        page_cases_text = "\n".join(page_cases)
        self.make("oci", f'''echo "$*" >>"$CALLS/oci"
[ "{fail}" = oci ] && exit 7
[ "{fail}" = oci_page_2 ] && case " $* " in *" --page "*) exit 7;; esac
case " $* " in
 *" compute instance list "*) echo '{{"data":[]}}';;
 {page_cases_text}
 *" search resource structured-search "*) printf '%s\\n' '{pages[0]}';;
esac''')
        pve_vms = '[{"vmid":131},{"vmid":141},{"vmid":181},{"vmid":182}]' if pve_vm else '[]'
        self.make("ssh", f'''echo "$*" >>"$CALLS/ssh"
[ "{fail}" = ssh ] && exit 7
case " $* " in
  *"pvesh get /cluster/resources"*) [ "{str(qm_transient).lower()}" = true ] && exit 255; printf '%s\\n' '{pve_vms}';;
  *"pvesh get /nodes/"*) printf '%s\\n' '{pve_bridge}';;
esac''')

    def run_driver(self):
        evidence = self.runtime / "evidence/test-inventory"
        environment = os.environ.copy()
        environment.update(PATH=f"{self.bin}:/usr/bin:/bin", CALLS=str(self.calls))
        self.calls.mkdir(exist_ok=True)
        result = subprocess.run(
            [str(self.drivers / "inventory-driver.sh"), "--run-id", "run-1", "--evidence-dir", str(evidence)],
            text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=environment, check=False,
        )
        return result, evidence

    def test_zero_inventory_uses_paginated_cloud_queries_and_exact_pve_queries(self):
        self.install_provider_fixtures()
        result, evidence = self.run_driver()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("resourcegroupstaggingapi get-resources", (self.calls / "aws").read_text())
        search_calls = [line for line in (self.calls / "oci").read_text().splitlines()
                        if "search resource structured-search" in line]
        self.assertTrue(search_calls)
        self.assertTrue(all("--all" not in line for line in search_calls))
        ssh = (self.calls / "ssh").read_text()
        self.assertIn("pvesh get /cluster/resources --type vm", ssh)
        self.assertIn("pvesh get /nodes/pve01/network", ssh)
        self.assertIn("root@pve01.lain.local", ssh)
        scopes = {x["name"]: x for x in json.loads((evidence / "inventory.json").read_text())["scopes"]}
        self.assertTrue(all(x["count"] == 0 and x["queryStatus"] == "complete" for x in scopes.values()))

    def test_azure_existing_group_and_contained_resource_are_nonzero(self):
        self.install_provider_fixtures(azure_exists="true", azure_resources='[{"id":"nic-1"}]')
        result, evidence = self.run_driver()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("resource list --resource-group", (self.calls / "az").read_text())
        data = json.loads((evidence / "inventory.json").read_text())
        counts = {x["name"]: x["count"] for x in data["scopes"]}
        self.assertEqual(counts["azure-resource-group"], 1)
        self.assertEqual(counts["azure-contained-resources"], 1)

    def test_aws_oci_and_pve_leftovers_are_nonzero(self):
        self.install_provider_fixtures(
            aws_tagged='{"ResourceTagMappingList":[{"ResourceARN":"arn:fixture"}]}',
            oci_tagged='{"data":{"items":[{"identifier":"ocid.fixture"}]}}',
            pve_bridge='[{"iface":"vmbr999"}]',
            pve_vm=True,
        )
        result, evidence = self.run_driver()
        self.assertNotEqual(result.returncode, 0)
        counts = {x["name"]: x["count"] for x in json.loads((evidence / "inventory.json").read_text())["scopes"]}
        self.assertEqual(counts["aws-tagged-resources"], 1)
        self.assertEqual(counts["oci-tagged-resources"], 1)
        self.assertEqual(counts["pve-vms"], 4)
        self.assertEqual(counts["pve-bridges"], 1)

    def test_provider_failure_and_invalid_json_fail_closed(self):
        for provider in ("aws", "az", "oci", "ssh"):
            with self.subTest(provider=provider):
                self.install_provider_fixtures(fail=provider)
                result, _ = self.run_driver()
                self.assertNotEqual(result.returncode, 0)
        self.install_provider_fixtures(aws_tagged="not-json")
        result, _ = self.run_driver()
        self.assertNotEqual(result.returncode, 0)

    def test_transient_qm_or_ssh_error_is_not_authoritative_absence(self):
        self.install_provider_fixtures(qm_transient=True)
        result, _ = self.run_driver()
        self.assertNotEqual(result.returncode, 0)

    def test_partial_page_markers_fail_closed(self):
        for field in ("PaginationToken", "NextToken"):
            with self.subTest(provider="aws", field=field):
                self.install_provider_fixtures(aws_tagged=json.dumps({"ResourceTagMappingList": [], field: "more"}))
                result, _ = self.run_driver()
                self.assertNotEqual(result.returncode, 0)

    def test_oci_search_explicit_pagination_aggregates_all_pages(self):
        pages = [
            '{"data":{"items":[]},"opc-next-page":"page-2"}',
            '{"data":{"items":[]}}',
        ]
        self.install_provider_fixtures(oci_pages=pages)
        result, evidence = self.run_driver()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("--page page-2", (self.calls / "oci").read_text())
        aggregate = json.loads((evidence / "oci-tagged-resources.json").read_text())
        self.assertEqual(aggregate["pagination"], {"status": "complete", "pages": 2})

    def test_oci_later_page_nonzero_is_not_zero(self):
        pages = [
            '{"data":{"items":[]},"opc-next-page":"page-2"}',
            '{"data":{"items":[{"identifier":"ocid.later"}]}}',
        ]
        self.install_provider_fixtures(oci_pages=pages)
        result, evidence = self.run_driver()
        self.assertNotEqual(result.returncode, 0)
        scopes = {x["name"]: x for x in json.loads((evidence / "inventory.json").read_text())["scopes"]}
        self.assertEqual(scopes["oci-tagged-resources"]["count"], 1)

    def test_oci_repeated_token_malformed_and_later_transport_fail_closed(self):
        cases = (
            ([
                '{"data":{"items":[]},"opc-next-page":"repeat"}',
                '{"data":{"items":[]},"opc-next-page":"repeat"}',
            ], ""),
            (['{"data":{"items":{}}}'], ""),
            (['{"data":{"items":[]},"next-page":"ambiguous"}'], ""),
            ([
                '{"data":{"items":[]},"opc-next-page":"page-2"}',
                '{"data":{"items":[]}}',
            ], "oci_page_2"),
        )
        for pages, failure in cases:
            with self.subTest(pages=pages, failure=failure):
                self.install_provider_fixtures(oci_pages=pages, fail=failure)
                result, _ = self.run_driver()
                self.assertNotEqual(result.returncode, 0)


if __name__ == "__main__":
    unittest.main()
