"""Closed development producer contract without invoking the physical compiler."""
import json
import os
import shutil
import subprocess
import tempfile
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
        self.assertEqual(rtl.count(".CFG_ASYNC_READ(0)"), 136)
        self.assertNotIn("CFG_ASYNC_READ(1)", rtl)
        self.assertEqual(rtl.count(".B1EN(1'b1)"), 136)
        self.assertIn("subbank_d <= source_addr[12:10]", rtl)
        self.assertIn("group_index_d2 <= group_index_d", rtl)
        self.assertIn("data_q <= source_data", rtl)
        self.assertIn("expected_async_read=0", (producer.ROOT / producer.RECIPE).read_text())
        self.assertIn("reject_async_m10k_reads", (producer.ROOT / producer.RECIPE).read_text())


def _yosys_binary() -> str | None:
    named = os.environ.get("YOSYS")
    if named and Path(named).is_file() and os.access(named, os.X_OK):
        return named
    return shutil.which("yosys")


@unittest.skipUnless(_yosys_binary(), "Yosys is required to check the MegaCart M10K netlist")
class ColecoMegacartYosysM10kTests(unittest.TestCase):
    def test_mapped_netlist_has_no_async_m10k(self) -> None:
        yosys = _yosys_binary()
        assert yosys is not None
        sources = " ".join(producer.RTL_SOURCES)
        program = (
            "read_verilog -sv -DTV80_REFRESH=1 -DFES_COLECO_OSS=1 "
            "-DFES_COLECO_EXPANSION_V2_DEV=1 -DFES_COLECO_MEGACART_LINK=1 "
            "-I cores/fes-common/generated -I cores/fes-coleco/rtl "
            f"{sources}; "
            "chparam -set BUILD_ID 128'h" + "0" * 32 + " top; "
            "synth_intel_alm -nolutram -nodsp -top top -run :map_luts; "
            "autoname; select -module top; "
            "log ---SDP---; select -count t:MISTRAL_M10K; "
            "log ---TDP---; select -count t:MISTRAL_M10K_TDP; "
            "log ---ASYNC---; "
            "select -list t:MISTRAL_M10K r:CFG_ASYNC_READ=1 %i; "
            "select -list t:MISTRAL_M10K_TDP r:CFG_ASYNC_READ=1 %i; "
            "log ---LANES---; select -list t:MISTRAL_M10K r:CFG_ASYNC_READ=0 %i; "
            "log ---END---"
        )
        with tempfile.TemporaryDirectory() as directory:
            log_path = Path(directory) / "yosys.log"
            result = subprocess.run([yosys, "-l", str(log_path), "-p", program], cwd=producer.ROOT,
                                    text=True, capture_output=True, check=False)
            log = log_path.read_text(encoding="utf-8", errors="replace")
        self.assertEqual(result.returncode, 0, (result.stderr or log)[-4000:])
        sections: dict[str, list[str]] = {}
        current = None
        for line in log.splitlines():
            if line.startswith("---") and line.endswith("---") and line.strip("-"):
                current = line.strip("-")
                sections[current] = []
            elif current is not None and line.strip():
                sections[current].append(line.strip())
        async_cells = [line for line in sections["ASYNC"] if line.startswith("top/")]
        lanes = sorted(line.removeprefix("top/") for line in sections["LANES"] if "machine.rom.lane" in line)
        sdp = [line for line in sections["SDP"] if line.endswith(" objects.")]
        tdp = [line for line in sections["TDP"] if line.endswith(" objects.")]
        self.assertEqual(async_cells, [])
        self.assertEqual(lanes, sorted(f"machine.rom.lane{index}" for index in range(136)))
        self.assertEqual(len(sdp), 1)
        self.assertEqual(len(tdp), 1)


if __name__ == "__main__":
    unittest.main()
