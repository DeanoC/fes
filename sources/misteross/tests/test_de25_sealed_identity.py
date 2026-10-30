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
            self.assertNotEqual(seal.build_identity(first), seal.build_identity(record(t={"quartus": "other"})))
            with patch.object(seal, "CONTROL_ABI", seal.CONTROL_ABI + 1):
                self.assertNotEqual(seal.build_identity(first), seal.build_identity(record()))
            dirty = dict(inputs, overlay_source_state="working-tree")
            with self.assertRaises(ValueError):
                record(dirty)
