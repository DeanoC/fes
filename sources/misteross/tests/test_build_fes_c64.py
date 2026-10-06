"""FES Commodore 64 OSS producer contract checks.

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

from scripts import c64_slots
from scripts import build_fes_c64_oss as producer
from scripts.fes_build_common import BuildError, async_m10k_parameter, reject_async_m10k_reads
from scripts.lockfile import load_lock

ROOT = Path(__file__).resolve().parents[1]


class BuildFesC64Tests(unittest.TestCase):
    def test_read_only_m10k_clocks_are_live(self) -> None:
        import tempfile
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "synth.json"
            def cell(name: str, *, write: str = "0") -> tuple[str, dict]:
                return name, {"type": "MISTRAL_M10K", "connections": {
                    "CLK1": ["x"], "CLK2": [42], "A1EN": [write], "B1EN": ["1"]}}
            cells = dict((cell("machine.iec.file_track_rom"),
                          cell("machine.vic.code_q_rom")))
            path.write_text(json.dumps({"modules": {"top": {"cells": cells}}}))
            producer.clock_read_only_memories(path)
            fixed = json.loads(path.read_text())["modules"]["top"]["cells"]
            self.assertTrue(all(c["connections"]["CLK1"] == [42] for c in fixed.values()))
            cells["machine.vic.code_q_rom"]["connections"]["A1EN"] = ["1"]
            path.write_text(json.dumps({"modules": {"top": {"cells": cells}}}))
            with self.assertRaises(BuildError):
                producer.clock_read_only_memories(path)

    def test_make_entrypoint(self) -> None:
        result = subprocess.run(["make", "-n", "build-fes-c64"], cwd=ROOT, text=True,
                                capture_output=True, check=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), f'python3 scripts/build_fes_c64_oss.py --root "{ROOT}"')

    def test_lock_pins_the_expected_tools(self) -> None:
        lock = load_lock(ROOT / producer.C64_TOOLCHAIN_LOCK)
        commits = {name: pin.commit for name, pin in lock.items()}
        for name, commit in producer.C64_TOOL_COMMITS.items():
            self.assertEqual(commits[name], commit)

    def test_pinned_inputs_cover_every_rtl_file_and_exist(self) -> None:
        for relative in producer.PINNED_INPUTS:
            self.assertTrue((ROOT / relative).is_file(), relative)
        rtl = {path.relative_to(ROOT).as_posix()
               for path in (ROOT / "cores/fes-c64/rtl").iterdir()}
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
        self.assertEqual(route[route.index("--qsf") + 1], "build/fes-c64-oss/socket.qsf")
        self.assertEqual(route[route.index("--seed") + 1], "4")
        self.assertEqual(route[route.index("--router") + 1], "gpu")
        with self.assertRaises(BuildError):
            producer.build_commands(ROOT, ROOT / "elsewhere", "0" * 32,
                                    {"yosys": Path("/y"), "nextpnr-mistral": Path("/n")})

    def test_socket_qsf_names_every_region_once(self) -> None:
        base = (ROOT / producer.QSF).read_text()
        qsf = producer.socket_qsf(base)
        self.assertEqual(qsf.count("FES_RESERVED_RECT"), len(c64_slots.SOCKETS))
        for socket in c64_slots.SOCKETS:
            self.assertIn(f'FES_RESERVED_RECT "{socket.placement}"', qsf)
        with self.assertRaises(BuildError):
            producer.socket_qsf(qsf)

    def _routed(self, extra: dict | None = None) -> dict:
        cells = {}
        for socket in c64_slots.SOCKETS:
            for name, bel in c64_slots.boundary_bels(socket).items():
                i = len(cells)
                cells[socket.instance + name] = {
                    "type": "MISTRAL_FF", "attributes": {"NEXTPNR_BEL": bel},
                    "connections": {"CLK": [1], "DATAIN": [1000 + i], "Q": [2000 + i]},
                    "port_directions": {"CLK": "input", "DATAIN": "input", "Q": "output"}}
        count = len(cells)
        cells["machine.cpu.state"] = {"type": "MISTRAL_FF",
                                      "attributes": {"NEXTPNR_BEL": "MISTRAL_FF.10.10.2"}}
        cells.update(extra or {})
        return {"modules": {"top": {"cells": cells, "ports": {"source": {
            "direction": "input", "bits": list(range(1000, 1000 + count))}}}}}

    @staticmethod
    def _add_route_through(design: dict, name: str, bel: str, half: int, bit: int) -> None:
        cells = design["modules"]["top"]["cells"]
        data = cells[name]["connections"]["DATAIN"]
        cells[name]["connections"]["DATAIN"] = [bit]
        cells[name + "$ROUTETHRU"] = {
            "type": "MISTRAL_BUF", "parameters": {},
            "attributes": {"NEXTPNR_BEL": bel, "FES_PINMAP_V1": json.dumps({
                "count": 2, "pins": {"A": [0, "C" if half == 0 else "D"],
                                     "Q": [0, "COMBOUT"]}}).encode().hex()},
            "port_directions": {"A": "input", "Q": "output"},
            "connections": {"A": data, "Q": [bit]}}

    def test_routed_shell_keeps_sockets_vacant(self) -> None:
        evidence = producer.validate_routed_shell(self._routed())
        self.assertEqual(evidence["sockets"], [1, 2])
        self.assertEqual(evidence["pinned_boundary_cells"],
                         sum(len(c64_slots.boundary_bels(socket)) for socket in c64_slots.SOCKETS))
        intruder = {"machine.alu": {"type": "MISTRAL_COMB",
                                    "attributes": {"NEXTPNR_BEL": "MISTRAL_COMB.26.10.0"}}}
        with self.assertRaises(BuildError):
            producer.validate_routed_shell(self._routed(intruder))
        moved = self._routed()
        moved["modules"]["top"]["cells"]["slot2.plug_request_ff_0"]["attributes"]["NEXTPNR_BEL"] = \
            "MISTRAL_FF.24.42.2"
        with self.assertRaises(BuildError):
            producer.validate_routed_shell(moved)

    def test_routed_shell_admits_only_paired_route_through_buffers(self) -> None:
        design = self._routed()
        self.assertEqual(producer.validate_routed_shell(design)["boundary_route_through_cells"], 0)
        # 1656e473 places a route-through on the FF's own combinational half.
        anchor, anchor_bel = "slot1.clock_coverage_ff_0", "MISTRAL_COMB.24.4.54"
        self._add_route_through(design, anchor, anchor_bel, 0, 4000)
        evidence = producer.validate_routed_shell(design)
        self.assertEqual(evidence["boundary_route_through_cells"], 1)
        self.assertIn("scripts/coleco_expansion.py", producer.PINNED_INPUTS)
        buffer = anchor + "$ROUTETHRU"
        for mutate in (
                lambda c: c[buffer].update(type="MISTRAL_ALUT2"),
                lambda c: c[buffer]["attributes"].update(NEXTPNR_BEL="MISTRAL_COMB.24.4.55"),
                lambda c: c[buffer]["attributes"].update(NEXTPNR_BEL="MISTRAL_COMB.24.5.54"),
                lambda c: c[buffer]["attributes"].update(FES_PINMAP_V1="bad"),
                lambda c: c.update(extra={"type": "MISTRAL_BUF", "connections": {"A": [4000]},
                                          "port_directions": {"A": "input"}}),
                lambda c: c.update(extra={"type": "MISTRAL_BUF", "connections": {
                    "A": c[anchor]["connections"]["Q"]}, "port_directions": {"A": "input"}}),
                lambda c: c[buffer]["connections"].update(A=[99999]),
                lambda c: c.update({"shell$ROUTETHRU": {"type": "MISTRAL_BUF", "connections": {},
                                    "attributes": {"NEXTPNR_BEL": "MISTRAL_COMB.25.10.0"}}})):
            bad = json.loads(json.dumps(design))
            mutate(bad["modules"]["top"]["cells"])
            with self.assertRaises(BuildError):
                producer.validate_routed_shell(bad)

    def test_route_log_must_prove_the_1656e473_system_pll(self) -> None:
        log = ("Info: PLL 'system_clock.pll': fractional-N requested 52224000.000000 Hz (output 0), "
               "achieved 52224000.000569507 Hz, error 1.09050546e-05 ppm.\n"
               "Info: PLL 'system_clock.pll': fractional-N requested 12288000.000000 Hz (output 1), "
               "achieved 12288000.000134002 Hz, error 1.09050546e-05 ppm.\n"
               "Info: PLL 'system_clock.pll': 50.000000 MHz -> VCO 417.792000 MHz, fractional-N, "
               "M=8 N=1 K=1528321163, counters C6,7, bel altera_pll.0.14.0\n")
        import re
        self.assertRegex(log, producer.SYSTEM_PLL_ROUTE_RE)
        self.assertRegex(log, producer.SYSTEM_PLL_OUTPUT_RE)
        self.assertRegex(log, producer.AUDIO_PLL_OUTPUT_RE)
        old = ("Info: PLL 'system_clock.pll': second output 12.288 MHz, C7=34.\n"
               "Info: PLL 'system_clock.pll': 50 MHz -> 52.224 MHz, direct, M=8 N=1 C6=8, bel altera_pll.0.31.0\n")
        self.assertIsNone(re.search(producer.SYSTEM_PLL_ROUTE_RE, old))
        self.assertIsNone(re.search(producer.AUDIO_PLL_OUTPUT_RE, old))

    def test_firmware_lanes_must_stay_out_of_sockets(self) -> None:
        inside = {"blocks": [{"bel": "M10K.026.030", "word_bits": [[500 * 7605 + 2000]]}]}
        with self.assertRaises(BuildError):
            producer.check_firmware_outside_sockets(inside)
        outside = {"blocks": [{"bel": "M10K.005.032", "word_bits": [[3000 * 7605 + 300]]}]}
        producer.check_firmware_outside_sockets(outside)

    def test_manifest_declares_the_home_computer_contract(self) -> None:
        record = json.dumps({"recipe_sha256": "a" * 64}).encode()
        evidence = {"rbf": {"size": 2_000_000, "sha256": "b" * 64}, "build_id": "c" * 32,
                    "rom": {"id": "c64-firmware", "role": "firmware", "source_size": 16384,
                            "file": "rom-map.json", "size": 100, "sha256": "d" * 64}}
        manifest = tomllib.loads(producer._manifest(
            record, evidence, "https://github.com/DeanoC/fes.git", "e" * 40,
            {"yosys": "y", "nextpnr": "n", "mistral": "m"}).decode())
        self.assertEqual(manifest["format"], 3)
        self.assertEqual(manifest["core"]["id"], "fes.c64")
        self.assertEqual(manifest["abi"], {"id": "fes.computer", "major": 1, "minor": 0})
        interfaces = {i["id"]: i["required"] for i in manifest["interfaces"]}
        self.assertEqual(interfaces, {
            "fes.video.fixed-720p60": True, "fes.keyboard.hid": True, "fes.gamepad.ports": True,
            "fes.audio.pcm-s16-stereo-48k": True, "fes.media.c64-disk": True,
            "fes.expansion.c64-bus": False,
        })
        self.assertEqual(manifest["rom"]["role"], "firmware")
        self.assertEqual(manifest["rom"]["source_size"], 16384)

    def test_firmware_lanes_match_the_rtl(self) -> None:
        rtl = (ROOT / "cores/fes-c64/rtl/c64_rom.v").read_text()
        for index, row in enumerate(producer.FIRMWARE_LANE_ROWS):
            self.assertIn(f'(* keep, BEL = "MISTRAL_M10K.5.{row}.0" *)', rtl)
            self.assertIn(f") lane{index} (", rtl)
            self.assertIn(".CFG_ASYNC_READ(0)", rtl)
            self.assertIn(".B1EN(1'b1)", rtl)
            self.assertIn(".CLK1(clk)", rtl)
        self.assertNotIn("CFG_ASYNC_READ(1)", rtl)
        self.assertIn("bank_d <= address[13:10]", rtl)
        self.assertIn("sim_stage <= memory[address]", rtl)
        producer_source = (ROOT / "scripts/build_fes_c64_oss.py").read_text()
        self.assertIn("expected_async_read=0", producer_source)

    def test_netlist_check_rejects_any_async_m10k(self) -> None:
        def cell(name: str, async_read: str | None) -> tuple[str, dict]:
            parameters = {} if async_read is None else {"CFG_ASYNC_READ": async_read}
            return name, {"type": "MISTRAL_M10K", "parameters": parameters, "connections": {
                "CLK1": [7], "A1EN": ["1"], "B1EN": ["1"], "ACLR0": ["0"], "ACLR1": ["0"]}}
        lanes = dict(cell(f"machine.rom.lane{index}", "0") for index in range(16))
        design = {"modules": {"top": {"cells": lanes}}}
        reject_async_m10k_reads(design)
        producer.validate_firmware_ports(lanes)
        lanes["machine.rom.lane3"]["parameters"]["CFG_ASYNC_READ"] = "1"
        with self.assertRaisesRegex(BuildError, "synchronous M10K"):
            reject_async_m10k_reads(design)
        lanes["machine.rom.lane3"]["parameters"]["CFG_ASYNC_READ"] = f"{0:032b}"
        reject_async_m10k_reads(design)
        lanes["machine.vic.color"] = {"type": "MISTRAL_M10K", "parameters": {"CFG_ASYNC_READ": 1}}
        with self.assertRaisesRegex(BuildError, "machine.vic.color"):
            reject_async_m10k_reads(design)
        self.assertEqual(async_m10k_parameter(None), 0)
        self.assertEqual(async_m10k_parameter(f"{1:032b}"), 1)
        with self.assertRaises(BuildError):
            async_m10k_parameter("nope")

    def test_color_ram_must_be_one_synchronous_dual_clock_m10k(self) -> None:
        name = "machine.vic.color_ram.ram.0.0"
        cell = {"type": "MISTRAL_M10K_TDP", "connections": {
            "CLK1": [7], "CLK2": [8], "A1EN": ["1"], "B1EN": ["1"],
            "A1WE": [9], "B1WE": ["0"],
        }}
        producer.validate_color_ram({name: cell})
        for field, value in (("CLK2", [7]), ("A1WE", ["0"]), ("B1WE", [10])):
            changed = json.loads(json.dumps(cell))
            changed["connections"][field] = value
            with self.assertRaises(BuildError):
                producer.validate_color_ram({name: changed})
        with self.assertRaises(BuildError):
            producer.validate_color_ram({name: {**cell, "type": "MISTRAL_M10K"}})
        with self.assertRaises(BuildError):
            producer.validate_color_ram({name: cell, name + ".1": cell})

    def test_autonamed_read_only_roms_still_get_clocks(self) -> None:
        """Post-ABC autoname may replace either ROM prefix; both roles still patch."""
        import tempfile
        vic = "machine.main_ram.ram.0.61_B1Q_3_MISTRAL_M10K_B1ADDR"
        iec = "machine.iec.file_track_rom"
        renamed_iec = "machine.iec.disk_addr_track_base"

        def rom(name: str) -> tuple[str, dict]:
            return name, {"type": "MISTRAL_M10K", "parameters": {"CFG_ASYNC_READ": "0"},
                          "connections": {"CLK1": ["x"], "CLK2": [41], "A1EN": ["0"], "B1EN": ["1"]}}

        def patch(names: list[str]) -> dict:
            with tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / "synth.json"
                cells = dict(rom(name) for name in names)
                path.write_text(json.dumps({"modules": {"top": {"cells": cells}}}))
                producer.clock_read_only_memories(path)
                return json.loads(path.read_text())["modules"]["top"]["cells"]

        for names in ([vic, iec], [renamed_iec, "machine.vic.code_q_rom"], [vic, renamed_iec]):
            fixed = patch(names)
            self.assertTrue(all(cell["connections"]["CLK1"] == [41] for cell in fixed.values()), names)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "synth.json"
            cells = dict(rom(name) for name in (vic, iec, "machine.extra_rom"))
            path.write_text(json.dumps({"modules": {"top": {"cells": cells}}}))
            with self.assertRaisesRegex(BuildError, "unexpected disconnected C64 M10K clock"):
                producer.clock_read_only_memories(path)

    def test_read_only_clock_patch_rejects_async_m10k(self) -> None:
        import tempfile
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "synth.json"
            cells = {
                "machine.iec.file_track_rom": {"type": "MISTRAL_M10K", "parameters": {"CFG_ASYNC_READ": "1"},
                                               "connections": {"CLK1": ["x"], "CLK2": [42], "A1EN": ["0"], "B1EN": ["1"]}},
                "machine.vic.code_q_rom": {"type": "MISTRAL_M10K", "connections": {
                    "CLK1": ["x"], "CLK2": [42], "A1EN": ["0"], "B1EN": ["1"]}},
            }
            path.write_text(json.dumps({"modules": {"top": {"cells": cells}}}))
            with self.assertRaisesRegex(BuildError, "synchronous M10K"):
                producer.clock_read_only_memories(path)


def _yosys_binary() -> str | None:
    named = os.environ.get("YOSYS")
    if named and Path(named).is_file() and os.access(named, os.X_OK):
        return named
    return shutil.which("yosys")


@unittest.skipUnless(_yosys_binary(), "Yosys is required to check the C64 M10K netlist")
class C64YosysM10kTests(unittest.TestCase):
    def test_mapped_netlist_has_no_async_m10k(self) -> None:
        yosys = _yosys_binary()
        assert yosys is not None
        sources = " ".join(producer.RTL_SOURCES)
        program = (
            "read_verilog -sv -I cores/fes-c64/rtl -I cores/fes-common/generated "
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
        self.assertEqual(async_cells, [])
        self.assertEqual(objects("SDP"), "18 objects.")
        self.assertEqual(objects("TDP"), "236 objects.")
        self.assertEqual(lanes, sorted(f"machine.rom.lane{index}" for index in range(16)))

    def test_full_synth_patches_autonamed_read_only_clocks(self) -> None:
        """The producer clock patch must accept the names left by ABC and autoname."""
        yosys = _yosys_binary()
        assert yosys is not None
        abc = Path(yosys).with_name("yosys-abc")
        if not abc.is_file():
            self.skipTest("yosys-abc is required for the post-ABC C64 clock patch")
        sources = " ".join(producer.RTL_SOURCES)
        import tempfile
        with tempfile.TemporaryDirectory() as directory:
            synth = Path(directory) / "synth.json"
            log_path = Path(directory) / "yosys.log"
            program = (
                "read_verilog -sv -I cores/fes-c64/rtl -I cores/fes-common/generated "
                f"{sources}; "
                "chparam -set BUILD_ID 128'h" + "0" * 32 + " top; "
                "synth_intel_alm -nolutram -nodsp -top top; "
                f"write_json {synth}"
            )
            result = subprocess.run([yosys, "-l", str(log_path), "-p", program], cwd=ROOT,
                                    text=True, capture_output=True, check=False)
            log = log_path.read_text(encoding="utf-8", errors="replace")
            self.assertEqual(result.returncode, 0, (result.stderr or log)[-4000:])
            design = json.loads(synth.read_text())
            cells = design["modules"]["top"]["cells"]
            open_clocks = sorted(
                name for name, cell in cells.items()
                if cell.get("type") == "MISTRAL_M10K" and cell.get("connections", {}).get("CLK1") == ["x"]
            )
            self.assertEqual(len(open_clocks), 2, open_clocks)
            producer.clock_read_only_memories(synth)
            patched = json.loads(synth.read_text())["modules"]["top"]["cells"]
            for name in open_clocks:
                pins = patched[name]["connections"]
                self.assertEqual(pins["CLK1"], pins["CLK2"], name)
            still_open = [
                name for name, cell in patched.items()
                if cell.get("type") == "MISTRAL_M10K" and cell.get("connections", {}).get("CLK1") == ["x"]
            ]
            self.assertEqual(still_open, [])


if __name__ == "__main__":
    unittest.main()
