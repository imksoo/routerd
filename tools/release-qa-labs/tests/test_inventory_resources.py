"""Saved provider-response contracts; no provider process or network access."""
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class InventoryResourceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec = importlib.util.spec_from_file_location("inventory_resources", ROOT / "inventory_resources.py")
        cls.parser = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.parser)

    def setUp(self):
        self.run = "run-1"
        self.region = "ap-northeast-1"
        self.account = "123456789012"
        self.instance = "i-0123456789abcdef0"
        self.tags = [{"Key": "routerd-run-id", "Value": self.run}]
        self.tagged = {"ResourceTagMappingList": [{
            "ResourceARN": f"arn:aws:ec2:{self.region}:{self.account}:instance/{self.instance}",
            "Tags": self.tags,
        }]}
        self.lookup = {"Reservations": [{"OwnerId": self.account, "Instances": [{
            "InstanceId": self.instance, "Tags": self.tags, "State": {"Name": "terminated"},
        }]}]}
        self.active = {"Reservations": []}
        self.compartment = "ocid1.compartment.fixture"
        self.compute_item = {"id": "ocid1.instance.fixture", "compartment-id": self.compartment,
                             "freeform-tags": {"RouterdRunId": self.run}, "lifecycle-state": "TERMINATED"}
        self.compute = {"data": [self.compute_item]}
        self.search_item = {"identifier": self.compute_item["id"], "resource-type": "Instance",
                            "compartment-id": self.compartment, "freeform-tags": {"RouterdRunId": self.run},
                            "lifecycle-state": "TERMINATED"}
        self.search = {"data": {"items": [self.search_item]}, "pagination": {"status": "complete", "pages": 1}}

    def aws(self):
        return self.parser.aws_counts(self.tagged, self.lookup, self.active, self.run, self.region)

    def oci(self):
        return self.parser.oci_counts(self.search, self.compute, self.run, self.compartment)

    def test_aws_requires_exact_lookup_and_preserves_input(self):
        before = copy.deepcopy((self.tagged, self.lookup, self.active))
        self.assertEqual(self.parser.aws_ids(self.tagged, self.run, self.region), [self.instance])
        self.assertEqual(self.aws(), {"rawTaggedCount": 1, "confirmedTerminatedCount": 1,
                                    "activeInstanceCount": 0, "count": 0})
        self.assertEqual((self.tagged, self.lookup, self.active), before)

    def test_aws_every_nonterminal_state_still_counts(self):
        for state in ("pending", "running", "shutting-down", "stopping", "stopped"):
            with self.subTest(state=state):
                self.lookup["Reservations"][0]["Instances"][0]["State"]["Name"] = state
                self.assertEqual(self.aws()["count"], 1)
                self.assertEqual(self.aws()["confirmedTerminatedCount"], 0)

    def test_aws_noninstance_tags_are_never_tombstones(self):
        for arn in ("arn:aws:ec2:ap-northeast-1:123456789012:volume/vol-123", "arn:unknown:resource"):
            with self.subTest(arn=arn):
                self.tagged["ResourceTagMappingList"][0]["ResourceARN"] = arn
                self.lookup = {"Reservations": []}
                self.assertEqual(self.parser.aws_ids(self.tagged, self.run, self.region), [])
                self.assertEqual(self.aws()["count"], 1)

    def test_aws_lookup_missing_duplicate_extra_or_partial_is_rejected(self):
        for change in ("missing", "duplicate", "extra", "partial", "owner", "tag", "state", "identity"):
            with self.subTest(change=change):
                self.setUp()
                row = self.lookup["Reservations"][0]["Instances"][0]
                if change == "missing": self.lookup["Reservations"] = []
                elif change == "duplicate": self.lookup["Reservations"][0]["Instances"].append(copy.deepcopy(row))
                elif change == "extra":
                    extra = copy.deepcopy(row)
                    extra["InstanceId"] = "i-1123456789abcdef0"
                    self.lookup["Reservations"][0]["Instances"].append(extra)
                elif change == "partial": self.lookup["NextToken"] = "more"
                elif change == "owner": self.lookup["Reservations"][0]["OwnerId"] = "999999999999"
                elif change == "tag": row["Tags"] = [{"Key": "routerd-run-id", "Value": "other-run"}]
                elif change == "state": row["State"]["Name"] = "UNKNOWN"
                else: row["InstanceId"] = "i-1123456789abcdef0"
                with self.assertRaises(self.parser.InventoryError): self.aws()

    def test_aws_region_tag_duplicate_and_malformed_envelopes_fail_closed(self):
        for change in ("region", "tag", "duplicate", "duplicate-tag", "bad-token", "missing", "null", "false"):
            with self.subTest(change=change):
                self.setUp()
                row = self.tagged["ResourceTagMappingList"][0]
                if change == "region": row["ResourceARN"] = row["ResourceARN"].replace(self.region, "us-east-1")
                elif change == "tag": row["Tags"] = []
                elif change == "duplicate": self.tagged["ResourceTagMappingList"].append(copy.deepcopy(row))
                elif change == "duplicate-tag": row["Tags"] = self.tags * 2
                elif change == "bad-token": self.tagged["PaginationToken"] = False
                elif change == "missing": self.tagged = {}
                elif change == "null": self.tagged["ResourceTagMappingList"] = None
                else: self.tagged["ResourceTagMappingList"] = False
                with self.assertRaises(self.parser.InventoryError): self.aws()

    def test_aws_active_list_cannot_be_hidden_by_empty_tag_index(self):
        self.active = copy.deepcopy(self.lookup)
        self.active["Reservations"][0]["Instances"][0]["State"]["Name"] = "stopped"
        self.tagged = {"ResourceTagMappingList": []}
        self.lookup = {"Reservations": []}
        self.assertEqual(self.aws()["count"], 1)
        self.active["NextToken"] = "partial"
        with self.assertRaises(self.parser.InventoryError): self.aws()

    def test_aws_lifecycle_mismatch_between_queries_fails_closed(self):
        self.active = copy.deepcopy(self.lookup)
        self.active["Reservations"][0]["Instances"][0]["State"]["Name"] = "stopped"
        with self.assertRaises(self.parser.InventoryError): self.aws()

    def test_oci_confirmed_tombstone_preserves_input(self):
        before = copy.deepcopy((self.search, self.compute))
        self.assertEqual(self.oci(), {"rawTaggedCount": 1, "confirmedTerminatedCount": 1,
                                    "activeInstanceCount": 0, "count": 0})
        self.assertEqual((self.search, self.compute), before)

    def test_oci_all_nonterminal_states_count(self):
        for state in ("MOVING", "PROVISIONING", "RUNNING", "STARTING", "STOPPING", "STOPPED", "CREATING_IMAGE", "TERMINATING"):
            with self.subTest(state=state):
                self.compute_item["lifecycle-state"] = state
                self.search_item["lifecycle-state"] = state
                self.assertEqual(self.oci()["count"], 1)
                self.assertEqual(self.oci()["confirmedTerminatedCount"], 0)

    def test_oci_noninstance_is_counted_even_if_it_says_terminated(self):
        self.search_item["resource-type"] = "Vcn"
        self.search_item["identifier"] = "ocid1.vcn.fixture"
        self.assertEqual(self.oci()["count"], 1)
        self.assertEqual(self.oci()["confirmedTerminatedCount"], 0)

    def test_oci_missing_duplicate_or_disagreeing_corroboration_rejected(self):
        for change in ("missing", "duplicate", "tag", "compartment", "state", "unknown", "partial"):
            with self.subTest(change=change):
                self.setUp()
                if change == "missing": self.compute["data"] = []
                elif change == "duplicate": self.compute["data"].append(copy.deepcopy(self.compute_item))
                elif change == "tag": self.compute_item["freeform-tags"] = {"RouterdRunId": "other"}
                elif change == "compartment": self.compute_item["compartment-id"] = "ocid1.compartment.other"
                elif change == "state": self.compute_item["lifecycle-state"] = "STOPPED"
                elif change == "unknown": self.search_item["lifecycle-state"] = "UNKNOWN_ENUM_VALUE"
                else: self.compute["opc-next-page"] = "more"
                with self.assertRaises(self.parser.InventoryError): self.oci()

    def test_oci_active_compute_is_not_hidden_by_search_index_delay(self):
        self.search["data"]["items"] = []
        self.compute_item["lifecycle-state"] = "STOPPED"
        self.assertEqual(self.oci()["count"], 1)

    def test_oci_search_shape_tags_duplicates_and_pagination_rejected(self):
        for change in ("tag", "compartment", "duplicate", "incomplete", "bool-pages", "missing-type", "malformed"):
            with self.subTest(change=change):
                self.setUp()
                if change == "tag": self.search_item["freeform-tags"] = {}
                elif change == "compartment": self.search_item["compartment-id"] = "ocid1.compartment.other"
                elif change == "duplicate": self.search["data"]["items"].append(copy.deepcopy(self.search_item))
                elif change == "incomplete": self.search["pagination"]["status"] = "partial"
                elif change == "bool-pages": self.search["pagination"]["pages"] = True
                elif change == "missing-type": del self.search_item["resource-type"]
                else: self.search["data"]["items"] = {}
                with self.assertRaises(self.parser.InventoryError): self.oci()

    def test_raw_page_metadata_and_duplicate_json_keys_fail_closed(self):
        for page in ({"data": {"items": []}, "opc-next-page": False},
                     {"data": {"items": []}, "next-page": "ambiguous"},
                     {"data": {"items": []}, "opc-next-page": "line\nbreak"}):
            with self.subTest(page=page):
                with self.assertRaises(self.parser.InventoryError): self.parser.oci_page(page)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "response.json"
            path.write_text('{"data":[],"data":[]}', encoding="utf-8")
            with self.assertRaises(self.parser.InventoryError): self.parser.load_json(path)


if __name__ == "__main__":
    unittest.main()
