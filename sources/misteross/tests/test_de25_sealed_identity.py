import copy
import importlib.util
import json
import unittest
from pathlib import Path
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("de25_seal", ROOT / "prototypes/de25_board_control/seal.py")
seal = importlib.util.module_from_spec(spec)
spec.loader.exec_module(seal)


class SealedIdentityTests(unittest.TestCase):
    def test_record_binds_board_snapshot_vendor_tools_and_abi(self):
        inputs = {"device": seal.DEVICE, "diagnostic_only": True, "board_fes_revision": "a" * 40,
                  "overlay_source_state": "committed", "vendor_archive_sha256": "b" * 64,
                  "functional_snapshot_sha256": {"source": "c" * 64}}
        closure = {seal.RECIPE: "d" * 64, seal.DEFINITION: "e" * 64}
        tools = {"quartus": "pinned diagnostic executable"}
        with patch.object(seal, "source_input_closure", return_value=closure):
            def record(i=inputs, t=tools):
                return seal.make_record(i, "f" * 40, "https://example.invalid/fes", t)
            first = record()
            self.assertEqual(first, seal.encode_build_record(json.loads(first)))
            self.assertEqual(json.loads(first)["parameters"]["snapshot_sha256"], seal.snapshot_digest(inputs["functional_snapshot_sha256"]))
            for key, value in (("board_fes_revision", "1" * 40), ("vendor_archive_sha256", "2" * 64),
                               ("functional_snapshot_sha256", {"source": "3" * 64})):
                changed = copy.deepcopy(inputs)
                changed[key] = value
                self.assertNotEqual(seal.build_identity(first), seal.build_identity(record(changed)))
            hps_first = dict(inputs, computer={"hps_initialization": "HPS FIRST"})
            self.assertNotEqual(seal.build_identity(first), seal.build_identity(record(hps_first)))
            self.assertEqual(seal.profile_for(hps_first), seal.HPS_PROFILE)
            self.assertEqual(json.loads(record(hps_first))["parameters"]["conversion"], "hps-first-split-rbf-v1")
            self.assertNotEqual(seal.build_identity(first), seal.build_identity(record(t={"quartus": "other"})))
            with patch.object(seal, "CONTROL_ABI", seal.CONTROL_ABI + 1):
                self.assertNotEqual(seal.build_identity(first), seal.build_identity(record()))
            dirty = dict(inputs, overlay_source_state="working-tree")
            with self.assertRaises(ValueError):
                record(dirty)

    def test_split_hash_evidence_requires_one_device_and_one_hash(self):
        value = "AB" * 32
        info = f"Device name: {seal.DEVICE}\nHPS IO hash: {value}\n"
        report = f"; HPS IO hash ; {value} ;\n"
        self.assertEqual(seal.parse_pfg_info(info), value.lower())
        self.assertEqual(seal.parse_assembler_hash(report), value.lower())
        for bad in ("", info + "HPS IO hash: " + value + "\n", info.replace(seal.DEVICE, "other"), info.replace(value, "x" * 64)):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                seal.parse_pfg_info(bad)
        for bad in ("", report + report, report.replace(value, "x" * 64)):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                seal.parse_assembler_hash(bad)
