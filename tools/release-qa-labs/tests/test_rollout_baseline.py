import copy
import importlib.util
from pathlib import Path
import unittest


SPEC = importlib.util.spec_from_file_location(
    "rollout_baseline", Path(__file__).resolve().parents[1] / "rollout_baseline.py"
)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def fixture():
    guest = dict(at="2026-01-01T00:00:00+00:00", hostname="router-1", version="routerd test (test)",
                 config_sha256="a" * 64, boot_id="00000000-0000-4000-8000-000000000001",
                 management=[dict(device="eth0", phase="Bound", addressPresent=True,
                                  currentAddress="192.0.2.10")], addresses=["192.0.2.10", "192.0.2.254"])
    media = dict(name="router-1", boot="order=ide2;sata2;net0", media=[
        dict(slot="ide2", reference="storage:iso/release.iso", path="/iso/release.iso"),
        dict(slot="sata2", reference="storage:iso/hostname.iso", path="/iso/hostname.iso",
             media_sha256="b" * 64, media_bytes=4096),
        dict(slot="scsi1", reference="storage:config.raw", path="/config.raw", media_sha256="c" * 64,
             media_bytes=8192, persistent_config_path="/router.yaml", persistent_config_sha256="a" * 64,
             persistent_config_bytes=1024, debugfs_exitcode=0),
    ])
    return guest, media


def build(guest=None, media=None, **options):
    original_guest, original_media = fixture()
    defaults = dict(node="node1", host="host1", vmid=101, expected_version="routerd test (test)", management_device="eth0")
    defaults.update(options)
    return MODULE.build_rollout_baseline(original_guest if guest is None else guest,
                                         original_media if media is None else media, **defaults)


class RolloutBaselineTests(unittest.TestCase):
    def test_complete_observation_retains_management_and_config_identity(self):
        result = build()
        self.assertEqual(result["management_address"], ["192.0.2.10"])
        self.assertEqual(result["runtime_sha256"], "a" * 64)
        self.assertEqual(result["media"]["media"][2]["persistent_config_sha256"], result["runtime_sha256"])
        self.assertEqual(result["vmid"], 101)

    def test_source_observations_are_not_mutated_or_aliased(self):
        guest, media = fixture()
        original = copy.deepcopy((guest, media))
        result = build(guest, media)
        result["media"]["media"][0]["reference"] = "changed"
        result["management_address"].append("192.0.2.20")
        self.assertEqual((guest, media), original)

    def test_missing_required_guest_fields_fail(self):
        for key in fixture()[0]:
            with self.subTest(key=key):
                guest, _ = fixture()
                del guest[key]
                with self.assertRaises(ValueError):
                    build(guest=guest)

    def test_empty_management_or_assignment_cannot_create_baseline(self):
        for field in ["management", "addresses"]:
            guest, _ = fixture()
            guest[field] = []
            with self.assertRaises(ValueError):
                build(guest=guest)

    def test_management_must_be_bound_present_and_on_expected_interface(self):
        for key, value in [("device", "eth1"), ("phase", "Pending"), ("addressPresent", False),
                           ("addressPresent", "true"), ("currentAddress", "192.0.2.11"),
                           ("currentAddress", "invalid"), ("currentAddress", 42)]:
            guest, _ = fixture()
            guest["management"][0][key] = value
            with self.assertRaises(ValueError, msg=str((key, value))):
                build(guest=guest)

    def test_duplicate_management_status_is_rejected(self):
        guest, _ = fixture()
        guest["management"].append(copy.deepcopy(guest["management"][0]))
        with self.assertRaises(ValueError):
            build(guest=guest)

    def test_unrelated_virtual_ip_does_not_become_management_address(self):
        self.assertNotIn("192.0.2.254", build()["management_address"])

    def test_invalid_identity_and_version_fail(self):
        for key, value in [("hostname", "wrong"), ("version", "unexpected"), ("boot_id", ""),
                           ("config_sha256", ""), ("at", "2026-01-01T00:00:00")]:
            guest, _ = fixture()
            guest[key] = value
            with self.assertRaises(ValueError, msg=key):
                build(guest=guest)
        for options in [dict(node=""), dict(host=""), dict(vmid=True), dict(vmid=0), dict(management_device="")]:
            with self.assertRaises(ValueError):
                build(**options)

    def test_missing_duplicate_or_unexpected_media_fail(self):
        for mutate in [lambda rows: rows.pop(), lambda rows: rows.append(copy.deepcopy(rows[0])),
                       lambda rows: rows[0].update(slot="unexpected")]:
            _, media = fixture()
            mutate(media["media"])
            with self.assertRaises(ValueError):
                build(media=media)

    def test_persistent_config_must_match_runtime_config(self):
        for key, value in [("persistent_config_sha256", "d" * 64), ("persistent_config_bytes", 0),
                           ("persistent_config_path", "/other.yaml"), ("debugfs_exitcode", 1),
                           ("debugfs_exitcode", False), ("media_bytes", 0), ("media_sha256", "")]:
            _, media = fixture()
            media["media"][2][key] = value
            with self.assertRaises(ValueError, msg=key):
                build(media=media)

    def test_missing_media_attributes_fail(self):
        for slot_index, key in [(0, "reference"), (1, "media_sha256"), (2, "persistent_config_sha256")]:
            _, media = fixture()
            del media["media"][slot_index][key]
            with self.assertRaises(ValueError):
                build(media=media)


if __name__ == "__main__":
    unittest.main()
