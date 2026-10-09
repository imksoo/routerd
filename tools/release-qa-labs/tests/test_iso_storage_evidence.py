import importlib.util
from pathlib import Path
import unittest


SPEC = importlib.util.spec_from_file_location(
    "iso_storage_evidence", Path(__file__).resolve().parents[1] / "iso_storage_evidence.py"
)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)

TARGET = "/mnt/pve/qnap/template/iso/routerd-live-v20261009.2218.iso"
HASHES = {
    TARGET: "2042e5fc6d43f527b2cf6eca1cb2085b5e9d0ca77f191aca2c103c5e6598221c",
    "/mnt/pve/qnap/template/iso/routerd-live-v20261009.1349.iso": "d36b5a4ae0ef7f030ca0878a991e86fb40f1f13346b4aaf6d62ea2c41db0812b",
    "/mnt/pve/qnap/template/iso/routerd-live-v20261009.0902.iso": "1c5ea01e457fb4ff533e1de2cef94beb6bbbebc5324fdc6f1db54ff253f1f49f",
    "/mnt/pve/qnap/template/iso/routerd-live-v20261009.0024.iso": "33e2bddc6b6b4a1c5a611b3f2cb753d3f134cca589137c1eb7bd76ff97b9da67",
}
ROWS = ["2026-10-09T22:49:11Z"] + [f"{digest}  {path}" for path, digest in HASHES.items()] + ["ISO_BYTES 1416194048"]


def validate(rows=None, **kwargs):
    options = dict(expected_hashes=HASHES, target_path=TARGET, expected_target_bytes=1416194048)
    options.update(kwargs)
    return MODULE.validate_iso_storage_evidence("\n".join(ROWS if rows is None else rows) + "\n", **options)


class ISOStorageEvidenceTests(unittest.TestCase):
    def test_actual_four_generation_staging_format(self):
        result = validate()
        self.assertEqual(result["hashes"], HASHES)
        self.assertEqual(result["targetBytes"], 1416194048)
        self.assertEqual(result["targetPath"], TARGET)
        self.assertEqual(result["observedAt"], ROWS[0])

    def test_binary_sha256sum_marker_is_supported(self):
        self.assertEqual(validate([ROWS[0]] + [r.replace("  /", " */") for r in ROWS[1:-1]] + [ROWS[-1]])["hashes"], HASHES)

    def test_original_rows_and_expected_manifest_are_unchanged(self):
        rows, hashes = ROWS.copy(), HASHES.copy()
        result = validate(rows, expected_hashes=hashes)
        result["hashes"].clear()
        self.assertEqual(rows, ROWS)
        self.assertEqual(hashes, HASHES)

    def test_missing_duplicate_or_invalid_timestamp_fails(self):
        for rows in [ROWS[1:], [ROWS[0]] + ROWS, ["2026-02-30T00:00:00Z"] + ROWS[1:],
                     ["2026-10-09T22:49:11+00:00"] + ROWS[1:]]:
            with self.subTest(rows=rows), self.assertRaises(ValueError):
                validate(rows)

    def test_missing_duplicate_or_invalid_size_fails(self):
        for rows in [ROWS[:-1], ROWS + [ROWS[-1]]] + [ROWS[:-1] + [marker] for marker in
                ["ISO_BYTES 0", "ISO_BYTES -1", "ISO_BYTES 1.4e9", "ISO_BYTES 01416194048",
                 "ISO_BYTES 1416194047", "ISO_BYTES 1416194048 extra"]]:
            with self.subTest(rows=rows), self.assertRaises(ValueError):
                validate(rows)

    def test_missing_duplicate_extra_or_wrong_checksum_fails(self):
        for rows in [ROWS[:2] + ROWS[3:], ROWS[:2] + ROWS[1:],
                     ROWS[:-1] + ["a" * 64 + "  /other.iso", ROWS[-1]],
                     [ROWS[0], "a" * 64 + "  " + TARGET] + ROWS[2:]]:
            with self.subTest(rows=rows), self.assertRaises(ValueError):
                validate(rows)

    def test_unknown_or_malformed_lines_are_not_discarded(self):
        for line in ["warning: read failed", "", "ISO_BYTES 1", "a" * 63 + "  " + TARGET,
                     "G" * 64 + "  " + TARGET, "a" * 64 + " " + TARGET]:
            with self.subTest(line=line), self.assertRaises(ValueError):
                validate(ROWS[:-1] + [line, ROWS[-1]])

    def test_size_is_bound_to_first_target_not_another_retained_iso(self):
        with self.assertRaises(ValueError):
            validate([ROWS[0], ROWS[2], ROWS[1]] + ROWS[3:])
        with self.assertRaises(ValueError):
            validate(target_path="/unobserved.iso")

    def test_noncanonical_paths_fail_even_if_expected_manifest_agrees(self):
        for path in ["relative.iso", "/iso/../target.iso", "/iso//target.iso", "/iso/./target.iso",
                     "/target.txt", "/target\t.iso", "/target\\.iso"]:
            rows = [ROWS[0], "a" * 64 + "  " + path, ROWS[-1]]
            with self.subTest(path=path), self.assertRaises(ValueError):
                validate(rows, expected_hashes={path: "a" * 64}, target_path=path)

    def test_invalid_expected_values_cannot_qualify(self):
        for options in [dict(expected_hashes={}), dict(expected_hashes=None),
                        dict(expected_hashes={TARGET: True}), dict(expected_hashes={TARGET: "invalid"}),
                        dict(expected_target_bytes=True), dict(expected_target_bytes=0),
                        dict(expected_target_bytes="1416194048")]:
            with self.subTest(options=options), self.assertRaises(ValueError):
                validate(**options)


if __name__ == "__main__":
    unittest.main()
