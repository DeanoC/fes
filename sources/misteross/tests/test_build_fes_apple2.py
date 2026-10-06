"""FES Apple II OSS producer contract checks.

The Yosys netlist check runs when a `yosys` binary is on `PATH` or `YOSYS`
names one. It stops before ABC, which is enough to see mapped M10K parameters.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import tomllib
import unittest
from pathlib import Path

from scripts import apple2_slots
from scripts import build_fes_apple2_oss as producer
from scripts.fes_build_common import BuildError
from scripts.lockfile import load_lock

ROOT = Path(__file__).resolve().parents[1]


class BuildFesApple2Tests(unittest.TestCase):
    def test_make_entrypoint(self) -> None:
        result = subprocess.run(["make", "-n", "build-fes-apple2"], cwd=ROOT, text=True,
                                capture_output=True, check=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), f'python3 scripts/build_fes_apple2_oss.py --root "{ROOT}"')

    def test_lock_pins_the_expected_tools(self) -> None:
        lock = load_lock(ROOT / producer.APPLE2_TOOLCHAIN_LOCK)
        commits = {name: pin.commit for name, pin in lock.items()}
        for name, commit in producer.APPLE2_TOOL_COMMITS.items():
            self.assertEqual(commits[name], commit)

    def test_pinned_inputs_cover_every_rtl_file_and_exist(self) -> None:
        for relative in producer.PINNED_INPUTS:
            self.assertTrue((ROOT / relative).is_file(), relative)
        rtl = {path.relative_to(ROOT).as_posix()
               for path in (ROOT / "cores/fes-apple2/rtl").iterdir()}
        self.assertTrue(rtl <= set(producer.PINNED_INPUTS), rtl - set(producer.PINNED_INPUTS))

    def test_commands_read_every_source_and_route_in_the_socket_qsf(self) -> None:
        yosys, route = producer.build_commands(
            ROOT, ROOT / producer.OUTPUT_RELATIVE, "0" * 32,
            {"yosys": Path("/y"), "nextpnr-mistral": Path("/n")}, seed=4)
        program = yosys[2]
        for source in producer.RTL_SOURCES:
            self.assertIn(source, program)
        self.assertIn("chparam -set BUILD_ID 128'h" + "0" * 32 + " top", program)
        self.assertIn("synth_intel_alm -nolutram -nodsp -top top", program)
        self.assertEqual(route[route.index("--qsf") + 1], "build/fes-apple2-oss/socket.qsf")
        self.assertEqual(route[route.index("--seed") + 1], "4")
        self.assertEqual(route[route.index("--router") + 1], "gpu")
        with self.assertRaises(BuildError):
            producer.build_commands(ROOT, ROOT / "elsewhere", "0" * 32,
                                    {"yosys": Path("/y"), "nextpnr-mistral": Path("/n")})

    def test_socket_qsf_names_every_region_once(self) -> None:
        base = (ROOT / producer.QSF).read_text()
        qsf = producer.socket_qsf(base)
        self.assertEqual(qsf.count("FES_RESERVED_RECT"), len(apple2_slots.SOCKETS))
        for socket in apple2_slots.SOCKETS:
            self.assertIn(f'FES_RESERVED_RECT "{socket.placement}"', qsf)
        with self.assertRaises(BuildError):
            producer.socket_qsf(qsf)

    def _routed(self, extra: dict | None = None) -> dict:
        cells = {}
        for socket in apple2_slots.SOCKETS:
            for name, bel in apple2_slots.boundary_bels(socket).items():
                cells[socket.instance + name] = {"type": "MISTRAL_FF",
                                                 "attributes": {"NEXTPNR_BEL": bel}}
        cells["machine.cpu.state"] = {"type": "MISTRAL_FF",
                                      "attributes": {"NEXTPNR_BEL": "MISTRAL_FF.10.10.2"}}
        cells.update(extra or {})
        return {"modules": {"top": {"cells": cells}}}

    def test_routed_shell_keeps_sockets_vacant(self) -> None:
        evidence = producer.validate_routed_shell(self._routed())
        self.assertEqual(evidence["sockets"], [2, 4, 5, 7])
        self.assertEqual(evidence["pinned_boundary_cells"], 4 * (60 + 33))
        intruder = {"machine.alu": {"type": "MISTRAL_COMB",
                                    "attributes": {"NEXTPNR_BEL": "MISTRAL_COMB.26.30.0"}}}
        with self.assertRaises(BuildError):
            producer.validate_routed_shell(self._routed(intruder))
        moved = self._routed()
        moved["modules"]["top"]["cells"]["slot5.plug_request_ff_0"]["attributes"]["NEXTPNR_BEL"] = \
            "MISTRAL_FF.24.42.2"
        with self.assertRaises(BuildError):
            producer.validate_routed_shell(moved)

    def test_firmware_lanes_must_stay_out_of_sockets(self) -> None:
        inside = {"blocks": [{"bel": "M10K.026.030", "word_bits": [[500 * 7605 + 2000]]}]}
        with self.assertRaises(BuildError):
            producer.check_firmware_outside_sockets(inside)
        outside = {"blocks": [{"bel": "M10K.005.032", "word_bits": [[3000 * 7605 + 300]]}]}
        producer.check_firmware_outside_sockets(outside)

    def test_manifest_declares_the_home_computer_contract(self) -> None:
        record = json.dumps({"recipe_sha256": "a" * 64}).encode()
        evidence = {"rbf": {"size": 2_000_000, "sha256": "b" * 64}, "build_id": "c" * 32,
                    "rom": {"id": "apple2-firmware", "role": "firmware", "source_size": 16384,
                            "file": "rom-map.json", "size": 100, "sha256": "d" * 64}}
        manifest = tomllib.loads(producer._manifest(
            record, evidence, "https://github.com/DeanoC/fes.git", "e" * 40,
            {"yosys": "y", "nextpnr": "n", "mistral": "m"}).decode())
        self.assertEqual(manifest["format"], 3)
        self.assertEqual(manifest["core"]["id"], "fes.apple2")
        self.assertEqual(manifest["abi"], {"id": "fes.computer", "major": 1, "minor": 0})
        interfaces = {i["id"]: i["required"] for i in manifest["interfaces"]}
        self.assertEqual(interfaces, {
            "fes.video.fixed-720p60": True, "fes.keyboard.hid": True, "fes.gamepad.ports": True,
            "fes.audio.pcm-s16-stereo-48k": True, "fes.media.apple2-floppy": True,
            "fes.expansion.apple2-bus": False,
        })
        self.assertEqual(manifest["rom"]["role"], "firmware")
        self.assertEqual(manifest["rom"]["source_size"], 16384)

    def test_firmware_lanes_match_the_rtl(self) -> None:
        rtl = (ROOT / "cores/fes-apple2/rtl/apple2_rom.v").read_text()
        for index, row in enumerate(producer.FIRMWARE_LANE_ROWS):
            self.assertIn(f'(* keep, BEL = "MISTRAL_M10K.5.{row}.0" *)', rtl)
            self.assertIn(f") lane{index} (", rtl)
        self.assertEqual(rtl.count(".CFG_ASYNC_READ(0)"), 16)
        self.assertNotIn("CFG_ASYNC_READ(1)", rtl)
        self.assertIn("subbank_d", rtl)
        self.assertIn("group_d2", rtl)
        self.assertIn(".B1EN(1'b1)", rtl)
        self.assertIn(".CLK1(clk)", rtl)
        self.assertNotIn(".CLK1(1'b0)", rtl)
        video = (ROOT / "cores/fes-apple2/rtl/apple2_video.v").read_text()
        self.assertIn(".CFG_ASYNC_READ(0)", video)
        self.assertNotIn("CFG_ASYNC_READ(1)", video)
        self.assertIn(".CLK1(pixel_clk)", video)
        self.assertIn(".B1EN(1'b1)", video)
        self.assertIn("font_stage", video)
        recipe = (ROOT / "scripts/build_fes_apple2_oss.py").read_text()
        self.assertIn("expected_async_read=0", recipe)
        self.assertNotIn("APPLE2_ASYNC_M10K", recipe)
        self.assertNotIn("allow=", recipe)

    def test_shell_rejects_every_async_m10k(self) -> None:
        from scripts.fes_build_common import reject_async_m10k_reads
        cells = {
            name: {"type": "MISTRAL_M10K", "parameters": {"CFG_ASYNC_READ": "0"}}
            for name in [f"machine.rom.lane{index}" for index in range(16)] + ["video.font_rom"]
        }
        design = {"modules": {"top": {"cells": cells}}}
        reject_async_m10k_reads(design)
        cells["video.font_rom"]["parameters"]["CFG_ASYNC_READ"] = "1"
        with self.assertRaisesRegex(BuildError, "video.font_rom"):
            reject_async_m10k_reads(design)


def _yosys_binary() -> str | None:
    named = os.environ.get("YOSYS")
    if named and Path(named).is_file() and os.access(named, os.X_OK):
        return named
    return shutil.which("yosys")


@unittest.skipUnless(_yosys_binary(), "Yosys is required to check the Apple II M10K netlist")
class Apple2YosysM10kTests(unittest.TestCase):
    def test_mapped_netlist_has_no_async_m10k(self) -> None:
        yosys = _yosys_binary()
        assert yosys is not None
        sources = " ".join(producer.RTL_SOURCES)
        program = (
            "read_verilog -sv -I cores/fes-apple2/rtl -I cores/fes-common/generated "
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
        import tempfile
        with tempfile.TemporaryDirectory() as directory:
            log_path = Path(directory) / "yosys.log"
            result = subprocess.run([yosys, "-l", str(log_path), "-p", program], cwd=ROOT,
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

        def objects(name: str) -> str:
            rows = [line for line in sections[name] if line.endswith(" objects.")]
            self.assertEqual(len(rows), 1, sections[name])
            return rows[0]

        async_cells = [line for line in sections["ASYNC"] if line.startswith("top/")]
        lanes = sorted(line.removeprefix("top/") for line in sections["LANES"] if line.startswith("top/"))
        expected = sorted(
            [f"machine.rom.lane{index}" for index in range(16)] + ["video.font_rom"]
        )
        self.assertEqual(async_cells, [])
        self.assertEqual(lanes, expected)
        self.assertIn("objects.", objects("SDP"))
        self.assertIn("objects.", objects("TDP"))


if __name__ == "__main__":
    unittest.main()
