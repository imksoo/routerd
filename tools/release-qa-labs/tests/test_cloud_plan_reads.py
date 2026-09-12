"""Offline guard regression for the one-client-per-site closed profile.

The fixture preserves only addresses, modes, types, actions and VM flavors from
a derived closed-profile plan: 38 managed creates and three deferred OCI reads.
The original larger cloud plan remains a rejection fixture. Raw plan
values, variables, provider IDs, credentials and network addresses stay private.
These tests never run Terraform, provider commands, SSH or systemd.
"""

import contextlib
import copy
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest


SPEC = importlib.util.spec_from_file_location("cloud_plan_qa_guard", Path(__file__).parents[1] / "qa_guard.py")
qa_guard = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(qa_guard)
FIXTURE = Path(__file__).parent / "fixtures/cloud-plan-one-client-per-site.json"
KEYS = ("client", "oci-leaf-b", "router")
READ_ADDRESSES = {f'module.oci_leaf.data.oci_core_vnic_attachments.node["{key}"]' for key in KEYS}


class CloudPlanReadTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "plan.json"
        self.plan = json.loads(FIXTURE.read_text())

    def records(self, section, plan=None):
        plan = self.plan if plan is None else plan
        if section == "planned":
            return plan["planned_values"]["root_module"]["resources"]
        return plan["resource_changes"]

    def reads(self, section, plan=None):
        return [r for r in self.records(section, plan) if r["mode"] == "data"]

    def verify(self, plan=None, phase="cloud", ceiling=1.60):
        self.path.write_text(json.dumps(self.plan if plan is None else plan))
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            qa_guard.verify_plan(self.path, phase, ceiling)
        return json.loads(output.getvalue())

    def reject(self, plan=None, phase="cloud", ceiling=1.60):
        with self.assertRaises(qa_guard.GuardError):
            self.verify(plan, phase, ceiling)

    def test_derived_plan_projection_passes_without_counting_reads_as_managed(self):
        result = self.verify()
        self.assertEqual(result["resourceCounts"], qa_guard.PLAN_COUNTS["cloud"])
        self.assertEqual(sum(result["resourceCounts"].values()), 38)

    def test_projection_has_only_public_allowlisted_fields_and_real_shape(self):
        self.assertEqual(set(self.plan), {"planned_values", "resource_changes"})
        for section in ("planned", "changes"):
            records = self.records(section)
            self.assertEqual(len(records), 41)
            self.assertEqual({r["address"] for r in self.reads(section)}, READ_ADDRESSES)
            self.assertEqual(len(self.reads(section)), 3)
            for record in records:
                with self.subTest(section=section, address=record["address"]):
                    field = "values" if section == "planned" else "change"
                    self.assertEqual(set(record), {"address", "mode", "type", field})
                    if section == "planned":
                        allowed = {"aws_instance": {"instance_type"},
                                   "azurerm_linux_virtual_machine": {"size"},
                                   "oci_core_instance": {"shape", "shape_config"}}
                        self.assertEqual(set(record["values"]), allowed.get(record["type"], set()))
                    else:
                        self.assertEqual(set(record["change"]), {"actions"})
                        self.assertEqual(record["change"]["actions"], ["read"] if record["mode"] == "data" else ["create"])

    def test_nested_module_plan_also_passes(self):
        resources = self.records("planned")
        self.plan["planned_values"]["root_module"] = {"child_modules": [{"child_modules": [{"resources": resources}]}]}
        self.verify()

    def test_historical_two_clients_per_site_projection_is_rejected(self):
        historic = json.loads((FIXTURE.parent / "cloud-plan-vnic-reads.json").read_text())
        self.reject(historic)

    def test_all_three_reads_are_mandatory(self):
        for section in ("planned", "changes"):
            self.records(section)[:] = [r for r in self.records(section) if r["mode"] == "managed"]
        self.reject()

    def test_partial_and_one_sided_read_sets_are_rejected(self):
        for sections in (("planned",), ("changes",), ("planned", "changes")):
            for count in (1, 2, 3):
                with self.subTest(sections=sections, count=count):
                    plan = copy.deepcopy(self.plan)
                    for section in sections:
                        for record in self.reads(section, plan)[:count]:
                            self.records(section, plan).remove(record)
                    self.reject(plan)

    def test_duplicate_read_in_either_representation_is_rejected(self):
        for section in ("planned", "changes"):
            with self.subTest(section=section):
                plan = copy.deepcopy(self.plan)
                self.records(section, plan).append(copy.deepcopy(self.reads(section, plan)[0]))
                self.reject(plan)

    def test_invalid_read_address_type_or_mode_is_rejected(self):
        invalid = (
            ("address", 'module.oci_leaf.data.oci_core_vnic_attachments.node["extra"]'),
            ("address", 'module.other.data.oci_core_vnic_attachments.node["client"]'),
            ("address", 'module.oci_leaf.data.oci_core_vnic_attachments.other["client"]'),
            ("address", None), ("type", "oci_core_vnics"),
            ("mode", "managed"), ("mode", "unknown"), ("mode", None),
        )
        for section in ("planned", "changes"):
            for field, value in invalid:
                with self.subTest(section=section, field=field, value=value):
                    plan = copy.deepcopy(self.plan)
                    self.reads(section, plan)[0][field] = value
                    self.reject(plan)

    def test_unknown_or_excess_data_cannot_be_ignored(self):
        for kind in ("oci_core_vnics", "oci_core_vnic_attachments", "aws_instance"):
            with self.subTest(kind=kind):
                plan = copy.deepcopy(self.plan)
                for section in ("planned", "changes"):
                    extra = copy.deepcopy(self.reads(section, plan)[0])
                    extra.update(address=f"data.{kind}.extra", type=kind)
                    self.records(section, plan).append(extra)
                self.reject(plan)

    def test_read_action_must_be_exactly_one_read(self):
        for actions in ([], ["no-op"], ["create"], ["delete"], ["read", "create"], ["read", "read"], "read", None):
            with self.subTest(actions=actions):
                plan = copy.deepcopy(self.plan)
                self.reads("changes", plan)[0]["change"]["actions"] = actions
                self.reject(plan)

    def test_read_requires_same_key_managed_oci_instance(self):
        for section in ("planned", "changes"):
            for field, value in (("address", 'module.oci_leaf.oci_core_instance.node["extra"]'),
                                 ("mode", "data"), ("type", "aws_instance")):
                with self.subTest(section=section, field=field, value=value):
                    plan = copy.deepcopy(self.plan)
                    instance = next(r for r in self.records(section, plan) if r["type"] == "oci_core_instance")
                    instance[field] = value
                    self.reject(plan)

    def test_duplicate_instance_key_cannot_substitute_for_missing_key(self):
        for section in ("planned", "changes"):
            with self.subTest(section=section):
                plan = copy.deepcopy(self.plan)
                instances = [r for r in self.records(section, plan) if r["type"] == "oci_core_instance"]
                instances[1]["address"] = instances[0]["address"]
                self.reject(plan)

    def test_managed_type_allowlist_and_counts_still_apply(self):
        for kind in ("aws_nat_gateway", "oci_core_vnic_attachments", "aws_instance"):
            with self.subTest(kind=kind):
                plan = copy.deepcopy(self.plan)
                self.records("planned", plan).append({"mode": "managed", "type": kind, "address": f"{kind}.extra"})
                self.reject(plan)
        for kind in qa_guard.PLAN_COUNTS["cloud"]:
            with self.subTest(missing_kind=kind):
                plan = copy.deepcopy(self.plan)
                resource = next(r for r in self.records("planned", plan) if r["mode"] == "managed" and r["type"] == kind)
                self.records("planned", plan).remove(resource)
                self.reject(plan)

    def test_managed_flavor_limits_still_apply(self):
        for kind, field in (("aws_instance", "instance_type"), ("azurerm_linux_virtual_machine", "size"), ("oci_core_instance", "shape")):
            with self.subTest(kind=kind):
                plan = copy.deepcopy(self.plan)
                resource = next(r for r in self.records("planned", plan) if r["type"] == kind)
                resource["values"][field] = "unapproved-large-flavor"
                self.reject(plan)

    def test_managed_destructive_actions_still_rejected(self):
        for actions in (["delete"], ["delete", "create"], ["update"]):
            with self.subTest(actions=actions):
                plan = copy.deepcopy(self.plan)
                next(r for r in self.records("changes", plan) if r["mode"] == "managed")["change"]["actions"] = actions
                self.reject(plan)

    def test_oci_flex_requires_explicit_one_ocpu_one_gib(self):
        for block in (None, [], [{}], [{"ocpus": 1}], [{"ocpus": 1, "memory_in_gbs": 16}],
                      [{"ocpus": 2, "memory_in_gbs": 1}], [{"ocpus": True, "memory_in_gbs": 1}],
                      [{"ocpus": 1, "memory_in_gbs": "1"}], [{"ocpus": 1, "memory_in_gbs": 1}] * 2):
            with self.subTest(block=block):
                plan = copy.deepcopy(self.plan)
                resource = next(r for r in self.records("planned", plan) if r["type"] == "oci_core_instance")
                if block is None:
                    resource["values"].pop("shape_config")
                else:
                    resource["values"]["shape_config"] = block
                self.reject(plan)

    def test_pve_workload_size_is_exact_and_stage_is_separate(self):
        plan = json.loads((FIXTURE.parent / "pve-plan-one-client-per-site.json").read_text())
        self.verify(plan, phase="pve")
        for field, block in (("cpu", []), ("cpu", [{"cores": 2, "sockets": 1}]),
                             ("cpu", [{"cores": 1, "sockets": 2}]),
                             ("cpu", [{"cores": True, "sockets": 1}]),
                             ("memory", [{"dedicated": 2048}]), ("memory", [])):
            with self.subTest(field=field, block=block):
                bad = copy.deepcopy(plan)
                self.records("planned", bad)[2]["values"][field] = block
                self.reject(bad, phase="pve")

    def test_cost_ceiling_still_applies_to_realistic_plan(self):
        self.reject(ceiling=0.10)

    def test_pve_still_requires_zero_data_reads(self):
        plan = json.loads((FIXTURE.parent / "pve-plan-one-client-per-site.json").read_text())
        self.verify(plan, phase="pve")
        for section in ("planned", "changes"):
            with self.subTest(section=section):
                invalid = copy.deepcopy(plan)
                self.records(section, invalid).append(copy.deepcopy(self.reads(section)[0]))
                self.reject(invalid, phase="pve")


if __name__ == "__main__":
    unittest.main()
