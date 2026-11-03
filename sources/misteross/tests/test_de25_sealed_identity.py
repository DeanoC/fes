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
                  "compile_processors": 2, "computer_model": "zx81",
                  "reader_memory":seal.reader_memory.selection('onchip',0,0),
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
                               ("compile_processors", 16), ("computer_model", "zx80"),
                               ("functional_snapshot_sha256", {"source": "3" * 64})):
                changed = copy.deepcopy(inputs)
                changed[key] = value
                self.assertNotEqual(seal.build_identity(first), seal.build_identity(record(changed)))
            hps_first = dict(inputs, computer={"hps_initialization": "HPS FIRST"})
            self.assertNotEqual(seal.build_identity(first), seal.build_identity(record(hps_first)))
            self.assertEqual(seal.profile_for(hps_first), seal.HPS_PROFILE)
            self.assertEqual(json.loads(record(hps_first))["parameters"]["conversion"], "hps-first-split-rbf-v1")
            self.assertNotEqual(seal.build_identity(first), seal.build_identity(record(t={"quartus": "other"})))
            with patch.object(seal.reader_memory, "CONTROL_ABI", seal.reader_memory.CONTROL_ABI + 2):
                self.assertNotEqual(seal.build_identity(first), seal.build_identity(record()))
            ddr=seal.reader_memory.selection('hps-ddr',0x1000,0x2000)
            for value in (ddr,dict(ddr,bus_base=0x2000),dict(ddr,bytes=0x3000)):
                changed=dict(inputs,reader_memory=value)
                self.assertNotEqual(seal.build_identity(first),seal.build_identity(record(changed)))
                self.assertEqual(json.loads(record(changed))['parameters']['control_abi'],0x10203041)
            window=seal.reader_memory.selection('hps-ddr-window',0x95000000,4<<20)
            changed=dict(inputs,reader_memory=window)
            self.assertNotEqual(seal.build_identity(record(dict(inputs,reader_memory=ddr))),seal.build_identity(record(changed)))
            self.assertEqual(json.loads(record(changed))['parameters']['control_abi'],0x10203042)
            self.assertEqual(seal.profile_for(changed),seal.WINDOW_PROFILE)
            self.assertEqual(seal.profile_for(dict(hps_first,reader_memory=window)),seal.HPS_WINDOW_PROFILE)
            for backend,base,size in (('unknown',0,0),('onchip',4096,0),('onchip',0,4096),
                 ('hps-ddr',0,0),('hps-ddr',1,4096),('hps-ddr',4096,16),
                 ('hps-ddr',-4096,4096),('hps-ddr',0xfffff000,8192),('hps-ddr',True,4096),
                 ('hps-ddr-window',0x95000000,2<<20),('hps-ddr-window',0x95000000,8<<20)):
                with self.subTest(backend=backend,base=base,size=size),self.assertRaises(ValueError):
                    seal.reader_memory.selection(backend,base,size)
            dirty = dict(inputs, overlay_source_state="working-tree")
            with self.assertRaises(ValueError):
                record(dirty)
            for model in (True, None, 'unknown', 'ZX80'):
                with self.subTest(model=model), self.assertRaises(ValueError):
                    record(dict(inputs, computer_model=model))

    def test_split_hash_evidence_requires_one_device_and_one_hash(self):
        value = "AB" * 32
        info = f"Device name: {seal.DEVICE}\nHPS IO hash: {value}\n"
        report = f"; HPS IO hash ; {value} ;\n"
        self.assertEqual(seal.parse_pfg_info(info.encode() + b"Temperature threshold: 120\xb0C\n"), value.lower())
        self.assertEqual(seal.parse_assembler_hash(report), value.lower())
        for bad in ("", info + "HPS IO hash: " + value + "\n", info.replace(seal.DEVICE, "other"), info.replace(value, "x" * 64)):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                seal.parse_pfg_info(bad.encode())
        for bad in ("", report + report, report.replace(value, "x" * 64)):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                seal.parse_assembler_hash(bad)
