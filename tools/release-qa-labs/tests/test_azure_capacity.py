"""Offline quota regressions: neither Azure nor a supervisor service is used."""

import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
SUBSCRIPTION = "00000000-0000-0000-0000-000000000001"


def contract():
    return {
        "runId": "fixture-quota",
        "qualification": {"profile": "representative-redundancy", "runScope": "full-representative"},
        "providers": [{"name": "azure", "profile": "active-az-account", "region": "japaneast",
                       "authMode": "az-cli-session"}],
        "limits": {"providerCounts": {"azure": 3}, "instanceTypes": {"azure": {"Standard_B1s": 3}},
                   "regions": {"azure": "japaneast"}},
    }


def tfvars():
    return f'azure_subscription_id = "{SUBSCRIPTION}"\nazure_location = "japaneast"\n'


def usage(current="7", limit="10", family_current="7", family_limit="10"):
    return [
        {"name": {"value": "cores", "localizedValue": "Total Regional vCPUs"},
         "currentValue": current, "limit": limit, "unit": "Count"},
        {"name": {"value": "standardBSFamily", "localizedValue": "Standard BS Family vCPUs"},
         "currentValue": family_current, "limit": family_limit, "unit": "Count"},
        {"name": {"value": "virtualMachines"}, "currentValue": "8", "limit": "25000", "unit": "Count"},
    ]


class AzureCapacityTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec = importlib.util.spec_from_file_location("azure_capacity", ROOT / "azure_capacity.py")
        cls.module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.module)

    def test_closed_profile_request_uses_pinned_subscription_and_region(self):
        result = self.module.quota_request(contract(), tfvars())
        self.assertEqual(result, {"subscription": SUBSCRIPTION, "region": "japaneast", "requiredCores": 3})

    def test_pve_only_needs_no_azure_context(self):
        value = {"qualification": {"profile": "representative-redundancy", "runScope": "pve-certification-only"}}
        self.assertIsNone(self.module.quota_request(value, ""))

    def test_bad_context_cannot_fall_back_to_ambient_provider(self):
        contexts = []
        for scope in (None, "", "unknown"):
            value = contract()
            value["qualification"]["runScope"] = scope
            contexts.append(value)
        for field, replacement in (("region", "westus"), ("profile", "other"), ("authMode", "other")):
            value = contract()
            value["providers"][0][field] = replacement
            contexts.append(value)
        for providers in ([], contract()["providers"] * 2):
            value = contract()
            value["providers"] = providers
            contexts.append(value)
        for group, replacement in (("providerCounts", 4), ("instanceTypes", {"Standard_B2s": 4}),
                                   ("regions", "westus"), ("providerCounts", 3.0),
                                   ("instanceTypes", {"Standard_B1s": 3.0})):
            value = contract()
            value["limits"][group]["azure"] = replacement
            contexts.append(value)
        for value in contexts:
            with self.subTest(value=value), self.assertRaises(self.module.CapacityError):
                self.module.quota_request(value, tfvars())

    def test_tfvars_missing_duplicate_or_nonliteral_context_is_rejected(self):
        for text in ("", tfvars().replace("japaneast", "westus"),
                     tfvars() + 'azure_location = "japaneast"\n',
                     tfvars() + 'azure_subscription_id = var.other\n',
                     tfvars().replace(f'"{SUBSCRIPTION}"', '"not-a-subscription"'),
                     tfvars().replace('"japaneast"', 'var.location'),
                     tfvars().replace('"japaneast"', '"${var.location}"')):
            with self.subTest(text=text), self.assertRaises(self.module.CapacityError):
                self.module.quota_request(contract(), text)

    def test_actual_digit_string_shape_and_integer_values_accept_exact_boundary(self):
        for values in (usage(), usage(7, 10, 7, 10)):
            with self.subTest(values=values):
                result = self.module.verify_usage(values)
                self.assertEqual(result["cores"], {"current": 7, "limit": 10, "required": 3, "remainingAfter": 0})
                self.assertEqual(result["standardBSFamily"]["remainingAfter"], 0)

    def test_eight_existing_plus_three_new_rejects_either_quota(self):
        for values in (usage("8", "10", "0", "20"), usage("0", "20", "8", "10")):
            with self.subTest(values=values), self.assertRaisesRegex(self.module.CapacityError, "insufficient"):
                self.module.verify_usage(values)

    def test_partial_duplicate_or_malformed_usage_rejects(self):
        cases = [None, {}, [], usage()[:1], usage()[1:], usage() + [usage()[0]], [False],
                 [{"name": "cores"}], [{"name": {"value": "cores"}}]]
        for bad in (None, False, True, -1, 1.5, "-1", "+8", "8.0", " 8", "８", "", [], {}):
            for index in (0, 1):
                for key in ("currentValue", "limit"):
                    value = usage()
                    value[index][key] = bad
                    cases.append(value)
        for index in (0, 1):
            value = usage()
            value[index]["unit"] = "Bytes"
            cases.append(value)
        for value in cases:
            with self.subTest(value=value), self.assertRaises(self.module.CapacityError):
                self.module.verify_usage(value)

    def test_cli_query_is_explicit_read_only_and_bounded(self):
        request = self.module.quota_request(contract(), tfvars())
        with mock.patch.object(self.module.subprocess, "run", side_effect=[
                mock.Mock(returncode=0, stdout=json.dumps({"id": SUBSCRIPTION}), stderr=""),
                mock.Mock(returncode=0, stdout=json.dumps(usage()), stderr="")]) as run:
            self.assertEqual(self.module.query_usage(request), usage())
        self.assertEqual(run.call_args_list[0].args[0],
                         ["az", "account", "show", "--output", "json", "--only-show-errors"])
        args, kwargs = run.call_args_list[1]
        self.assertEqual(args[0], ["az", "vm", "list-usage", "--subscription", SUBSCRIPTION,
                                  "--location", "japaneast", "--output", "json", "--only-show-errors"])
        self.assertEqual(kwargs["timeout"], 20)
        self.assertNotIn("start_new_session", kwargs)

    def test_active_subscription_mismatch_or_missing_stops_before_usage(self):
        request = self.module.quota_request(contract(), tfvars())
        for account in ({"id": "00000000-0000-0000-0000-000000000002"}, {}, [], {"id": False}):
            with self.subTest(account=account), mock.patch.object(self.module.subprocess, "run",
                    return_value=mock.Mock(returncode=0, stdout=json.dumps(account), stderr="")) as run:
                with self.assertRaisesRegex(self.module.CapacityError, "active Azure subscription"):
                    self.module.query_usage(request)
                self.assertEqual(run.call_count, 1)

    def test_duplicate_json_keys_cannot_hide_usage_or_account_mismatch(self):
        request = self.module.quota_request(contract(), tfvars())
        bad_usage = json.dumps(usage()).replace('"currentValue": "7"', '"currentValue": "8", "currentValue": "0"', 1)
        account = json.dumps({"id": SUBSCRIPTION})
        for replies in ([f'{{"id":"other","id":"{SUBSCRIPTION}"}}'], [account, bad_usage]):
            with self.subTest(replies=len(replies)), mock.patch.object(self.module.subprocess, "run",
                    side_effect=[mock.Mock(returncode=0, stdout=text, stderr="") for text in replies]):
                with self.assertRaisesRegex(self.module.CapacityError, "duplicate JSON"):
                    self.module.query_usage(request)

    def test_query_errors_are_fail_closed_without_echoing_credentials(self):
        request = self.module.quota_request(contract(), tfvars())
        for result in (mock.Mock(returncode=1, stdout="", stderr="fixture-secret"),
                       mock.Mock(returncode=0, stdout="not json fixture-secret", stderr=""),
                       subprocess.TimeoutExpired("az", 20), FileNotFoundError("fixture-secret")):
            patch = {"side_effect": result} if isinstance(result, Exception) else {"return_value": result}
            with self.subTest(result=type(result).__name__), mock.patch.object(self.module.subprocess, "run", **patch):
                with self.assertRaises(self.module.CapacityError) as caught:
                    self.module.query_usage(request)
                self.assertNotIn("fixture-secret", str(caught.exception))


class AzureCapacityPrecheckTests(unittest.TestCase):
    """Execute the actual precheck/helper; fake only its other gates and Azure I/O."""

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.framework = self.root / "framework"
        drivers = self.framework / "drivers"
        drivers.mkdir(parents=True)
        shutil.copy2(ROOT / "drivers/precheck-driver.sh", drivers / "precheck-driver.sh")
        if (ROOT / "azure_capacity.py").exists():
            shutil.copy2(ROOT / "azure_capacity.py", self.framework / "azure_capacity.py")
        self.contract = self.root / "pinned-contract.json"
        self.variables = self.root / "pinned-terraform.tfvars"
        self.calls = self.root / "calls"
        self.run_env = self.root / "run.env.json"
        self.run_env.write_text(json.dumps({"releaseRepo": str(self.root)}))
        (drivers / "common.sh").write_text('''set -eu
framework_root="$FIXTURE_FRAMEWORK"
default_contract_path="$FIXTURE_CONTRACT"
run_env_path="$FIXTURE_RUN_ENV"
load_contract() {
 contract_path="$1"; tfvars_path="$FIXTURE_TFVARS"
 evidence_root="$FIXTURE_EVIDENCE"; run_id=fixture-quota
}
absolute_path() { printf '%s\\n' "$1"; }
require_command() { command -v "$1" >/dev/null; }
die() { echo "$*" >&2; exit 2; }
''')
        (self.framework / "qa_guard.py").write_text(
            'import os\nwith open(os.environ["FIXTURE_CALLS"], "a") as f: f.write("contract\\n")\n')
        for name in ("pve-substrate-preflight", "remote-egress-preflight", "inventory-driver"):
            path = drivers / (name + ".sh")
            path.write_text(f'#!/bin/sh\nprintf "{name}\\n" >>"$FIXTURE_CALLS"\n')
            path.chmod(0o755)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        az = self.bin / "az"
        az.write_text('''#!/bin/sh
printf 'az %s\n' "$*" >>"$FIXTURE_CALLS"
[ "$1 $2" = 'account show' ] && { printf '{"id":"%s"}\n' "$FIXTURE_ACTIVE_SUBSCRIPTION"; exit 0; }
[ "$1 $2" = 'vm list-usage' ] || exit 19
cat "$FIXTURE_USAGE"
exit "${FIXTURE_AZ_EXIT:-0}"
''')
        az.chmod(0o755)

    def run_precheck(self, values, *, scope="full-representative", azure_exit=0, active_subscription=SUBSCRIPTION):
        value = contract()
        value["qualification"]["runScope"] = scope
        self.contract.write_text(json.dumps(value))
        self.variables.write_text(tfvars())
        payload = self.root / "usage.json"
        payload.write_text(json.dumps(values))
        result = subprocess.run(["bash", str(self.framework / "drivers/precheck-driver.sh")],
            capture_output=True, text=True, timeout=10, env={**os.environ,
                "PATH": str(self.bin) + ":" + os.environ["PATH"],
                "FIXTURE_FRAMEWORK": str(self.framework), "FIXTURE_CONTRACT": str(self.contract),
                "FIXTURE_RUN_ENV": str(self.run_env), "FIXTURE_TFVARS": str(self.variables),
                "FIXTURE_CALLS": str(self.calls), "FIXTURE_USAGE": str(payload),
                "FIXTURE_EVIDENCE": str(self.root / "evidence"), "FIXTURE_AZ_EXIT": str(azure_exit),
                "FIXTURE_ACTIVE_SUBSCRIPTION": active_subscription})
        return result, self.calls.read_text().splitlines()

    def test_insufficient_quota_stops_before_pve_or_other_precheck_work(self):
        result, calls = self.run_precheck(usage("8", "10"))
        self.assertNotEqual(result.returncode, 0, "missing capacity gate allowed the known 8+3>10 failure")
        self.assertIn("insufficient", result.stderr)
        self.assertEqual(calls[0], "contract")
        self.assertEqual(len(calls), 3, calls)
        self.assertTrue(calls[2].startswith("az vm list-usage --subscription " + SUBSCRIPTION))
        self.assertNotIn("precheck: pass", result.stdout)
        evidence = json.loads((self.root / "evidence/preflight/azure-capacity/result.json").read_text())
        self.assertEqual(evidence["quotas"]["cores"]["current"], 8)
        self.assertEqual(evidence["quotas"]["standardBSFamily"]["limit"], 10)

    def test_sufficient_quota_preserves_all_existing_gates(self):
        result, calls = self.run_precheck(usage())
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(calls), 6, calls)
        self.assertEqual(calls[0], "contract")
        self.assertIn("--location japaneast", calls[2])
        self.assertEqual(calls[3:], ["pve-substrate-preflight", "remote-egress-preflight", "inventory-driver"])
        evidence = self.root / "evidence/preflight/azure-capacity/result.json"
        value = json.loads(evidence.read_text())
        self.assertEqual(value["status"], "pass")
        self.assertEqual(value["quotas"]["cores"]["required"], 3)
        self.assertNotIn(SUBSCRIPTION, evidence.read_text())
        self.assertEqual(evidence.stat().st_mode & 0o777, 0o600)

    def test_pve_only_skips_azure_query_but_not_other_gates(self):
        result, calls = self.run_precheck(None, scope="pve-certification-only", azure_exit=19)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(calls, ["contract", "pve-substrate-preflight", "remote-egress-preflight", "inventory-driver"])
        evidence = self.root / "evidence/preflight/azure-capacity/result.json"
        self.assertEqual(json.loads(evidence.read_text())["status"], "not-applicable")

    def test_api_failure_cannot_emit_a_pass_result(self):
        result, calls = self.run_precheck(usage(), azure_exit=7)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(len(calls), 3)
        self.assertNotIn("precheck: pass", result.stdout)
        evidence = self.root / "evidence/preflight/azure-capacity/result.json"
        self.assertEqual(json.loads(evidence.read_text())["status"], "fail")

    def test_active_subscription_mismatch_stops_before_usage_or_pve(self):
        result, calls = self.run_precheck(usage(), active_subscription="00000000-0000-0000-0000-000000000002")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("active Azure subscription", result.stderr)
        self.assertEqual(calls, ["contract", "az account show --output json --only-show-errors"])


if __name__ == "__main__":
    unittest.main()
