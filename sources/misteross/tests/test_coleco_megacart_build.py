"""Closed development producer contract without invoking the physical compiler."""
import json
import tomllib
import unittest
from pathlib import Path
from scripts import build_fes_coleco_megacart as producer


class ColecoMegaCartBuildTest(unittest.TestCase):
    def test_manifest_requires_two_sources_and_no_mailbox(self):
        record = json.dumps({"recipe_sha256": "a"*64, "parameters": {}}).encode()
        evidence = {"rbf": {"size": 4096, "sha256": "b"*64}, "build_id": "c"*32,
                    "rom_map": {"file": "rom-map.json", "size": 128, "sha256": "d"*64}}
        manifest = tomllib.loads(producer.manifest(record, evidence, "https://example.com/repo", "e"*40, {"yosys": "f"*64}).decode())
        self.assertEqual(manifest["format"], 4)
        self.assertEqual([(r["role"], r["source_size"], r["source_offset"]) for r in manifest["roms"]],
                         [("firmware", 8192, 0), ("cartridge", 131072, 8192)])
        self.assertFalse(any(i["id"].startswith(("fes.media.", "fes.firmware.")) for i in manifest["interfaces"]))
        self.assertEqual(manifest["rom_map"], evidence["rom_map"])

    def test_136_lanes_match_pinned_rtl_and_socket_isolation(self):
        rtl = (producer.ROOT / "cores/fes-coleco/rtl/coleco_megacart_rom.v").read_text()
        self.assertEqual(len(producer.ROM_LANES), 136)
        self.assertEqual(len(set(producer.ROM_LANES)), 136)
        for index, (column, row) in enumerate(producer.ROM_LANES):
            self.assertIn(f'MISTRAL_M10K.{column}.{row}.0" *)\n    MISTRAL_M10K', rtl)
            self.assertIn(f")) lane{index} (", rtl)
        self.assertIn("cores/fes-coleco/rtl/coleco_megacart_rom.v", producer.PINNED_INPUTS)
        self.assertIn("scripts/rom_map.py", producer.PINNED_INPUTS)
        commands = producer.build_commands(producer.ROOT, producer.ROOT / producer.OUTPUT_RELATIVE,
                                           "a"*32, {"yosys": Path("/tool/yosys"),
                                                    "nextpnr-mistral": Path("/tool/nextpnr")})
        self.assertIn("-DFES_COLECO_MEGACART_LINK=1", commands[0][2])
        self.assertNotIn("ENABLE_FIRMWARE 1", commands[0][2])
        self.assertIn("--router", commands[1])


if __name__ == "__main__":
    unittest.main()
