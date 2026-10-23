"""FES Commodore 64 OSS producer contract checks.

The Yosys netlist check runs when a `yosys` binary is on `PATH` or `YOSYS`
names one. A partial pass checks M10K parameters; the full pass checks live
clocks and also runs packing when a sibling `nextpnr-mistral` is installed.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import tomllib
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts import c64_slots
from scripts import build_fes_c64_oss as producer
from scripts.fes_build_common import BuildError, async_m10k_parameter, reject_async_m10k_reads
from scripts.lockfile import load_lock

ROOT = Path(__file__).resolve().parents[1]


class BuildFesC64Tests(unittest.TestCase):
    def test_memory_clocks_must_be_live(self) -> None:
        for kind, clocks in (("MISTRAL_M10K", {"CLK1": [42]}),
                             ("MISTRAL_M10K", {"CLK1": [42], "CLK2": [43]}),
                             ("MISTRAL_M10K_TDP", {"CLK1": [42], "CLK2": [42]})):
            cell = {"type": kind, "connections": clocks}
            producer.validate_memory_clocks({"machine.memory": cell})
            for port in clocks:
                for invalid in (None, [], ["x"], ["0"], [True], [42, 43]):
                    bad = {**cell, "connections": {**clocks, port: invalid}}
                    with self.subTest(kind=kind, port=port, value=invalid), self.assertRaises(BuildError):
                        producer.validate_memory_clocks({"machine.memory": bad})

    def test_make_entrypoint(self) -> None:
        result = subprocess.run(["make", "-n", "build-fes-c64"], cwd=ROOT, text=True,
                                capture_output=True, check=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), f'python3 scripts/build_fes_c64_oss.py --root "{ROOT}"')

    def test_recipe_lock_pins_the_classified_nextpnr(self) -> None:
        from scripts import pnr_recipe
        self.assertEqual(pnr_recipe.lock_path(ROOT, "fes.c64"), ROOT / producer.C64_TOOLCHAIN_LOCK)
        lock = load_lock(ROOT / producer.C64_TOOLCHAIN_LOCK)
        self.assertEqual(lock["nextpnr"].commit, pnr_recipe.nextpnr_pin("fes.c64"))

    def test_cuda_lane_uses_pci_bus_order(self) -> None:
        self.assertIsNone(producer.execution_extra("HIP"))
        self.assertEqual(producer.execution_extra("CUDA"), {"CUDA_DEVICE_ORDER": "PCI_BUS_ID"})
        self.assertEqual(producer.gpu_choice("CUDA")[0], "cuda")
        with patch.dict(os.environ, {}, clear=False):
            os.environ.pop("FES_TOOLCHAIN_CUDA_ARCHITECTURES", None)
            self.assertEqual(
                producer.gpu_choice("CUDA")[2],
                "gpu-router=CUDA; hip-architectures=unused; cuda-architectures=86",
            )
        with patch.dict(os.environ, {"FES_TOOLCHAIN_CUDA_ARCHITECTURES": "89"}):
            self.assertEqual(
                producer.gpu_choice("CUDA")[2],
                "gpu-router=CUDA; hip-architectures=unused; cuda-architectures=89",
            )
        with self.assertRaises(BuildError):
            producer.execution_extra("OFF")

    def test_source_closure_is_the_audited_static_set(self) -> None:
        from scripts.closure_tools.manifest import static
        from scripts.functional_execution import AuditedRoots, source_roots_for_inputs, source_roots_for_producer
        roots = source_roots_for_producer("build_fes_c64_oss", producer.PINNED_INPUTS, ROOT)
        derived = static("build_fes_c64_oss")
        self.assertIsInstance(roots, AuditedRoots)
        self.assertEqual(list(roots), derived)
        broad = source_roots_for_inputs(producer.PINNED_INPUTS)
        self.assertIn("scripts", broad)
        self.assertIn("cores/fes-common", broad)
        self.assertNotIn("scripts", roots)
        self.assertNotIn("cores/fes-common", roots)
        for path in producer.PINNED_INPUTS:
            self.assertTrue(any(path == item or path.startswith(item + "/") for item in roots), path)
        self.assertNotIn("scripts/build_fes_pong.py", roots)
        record = json.loads(producer.create_build_record(
            ROOT, "https://example.invalid/fes.git", "a" * 40, {"yosys": "fixture"},
            execution={"gpu_device": 0, "version": 1}))
        self.assertEqual(record["parameters"]["source_closure_mode"], "audited-v1")
        self.assertEqual(record["source_roots"], derived)
        self.assertIn("scripts/c64_slots.py", record["source_inputs"])
        self.assertIn(producer.C64_TOOLCHAIN_LOCK, record["source_inputs"])
        self.assertNotIn("scripts/build_fes_pong.py", record["source_inputs"])

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
        self.assertIn("synth_intel_alm -nolutram -nodsp -top top -run coarse:", program)
        self.assertIn("t:$mul a:src=cores/fes-common/rtl/fes_display_plane.v:* %i", program)
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
                "count": 2, "pins": {"A": [0, "F0" if half == 0 else "F1"],
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
        producer.validate_pll_outputs(log)

    def test_route_log_rejects_a_pll_output_off_its_requested_frequency(self) -> None:
        log = ("Info: PLL 'system_clock.pll': fractional-N requested 52224000.000000 Hz (output 0), "
               "achieved 52224000.000569507 Hz, error 1.09050546e-05 ppm.\n"
               "Info: PLL 'system_clock.pll': fractional-N requested 12288000.000000 Hz (output 1), "
               "achieved 12288000.000134002 Hz, error 1.09050546e-05 ppm.\n"
               "Info: PLL 'system_clock.pll': 50.000000 MHz -> VCO 417.792000 MHz, fractional-N, "
               "M=8 N=1 K=1528321163, counters C6,7, bel altera_pll.0.14.0\n")
        for good, bad in (("achieved 52224000.000569507", "achieved 50000000.000000000"),
                          ("achieved 12288000.000134002", "achieved 12288002.000000000")):
            with self.subTest(bad=bad), self.assertRaisesRegex(BuildError, "achieved"):
                producer.validate_pll_outputs(log.replace(good, bad))
        requested_only = log.replace(", achieved 12288000.000134002 Hz, error 1.09050546e-05 ppm.", ".")
        with self.assertRaisesRegex(BuildError, "audio PLL"):
            producer.validate_pll_outputs(requested_only)

    def test_firmware_lanes_must_stay_out_of_sockets(self) -> None:
        inside = {"blocks": [{"bel": "M10K.026.030", "word_bits": [[500 * 7605 + 2000]]}]}
        with self.assertRaises(BuildError):
            producer.check_firmware_outside_sockets(inside)
        outside = {"blocks": [{"bel": "M10K.005.032", "word_bits": [[3000 * 7605 + 300]]}]}
        producer.check_firmware_outside_sockets(outside)

    def test_manifest_declares_the_home_computer_contract(self) -> None:
        record = json.dumps({"recipe_sha256": "a" * 64}).encode()
        evidence = {"rbf": {"size": 2_000_000, "sha256": "b" * 64}, "build_id": "c" * 32,
                    "rom": {"id": "c64-firmware", "role": "firmware", "source_size": 36864,
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
            "fes.memory.hps-ddr": True, "fes.video.session-display": True,
            "fes.fabric.video.display-plane": True,
        })
        self.assertEqual(manifest["rom"]["role"], "firmware")
        self.assertEqual(manifest["rom"]["source_size"], 36864)

    def test_session_display_requires_read_only_boot_ddr_layout(self) -> None:
        from scripts import fes_de10nano_evidence as board
        ports = {name: [str((value >> bit) & 1) for bit in range(board.HPS_DDR_CFG_PORTS[name][1])]
                 for name, value in board.hps_ddr_layout(ROOT).items()}
        ports.update({name: ["0"] for name in board.HPS_DDR_COMMAND_INPUTS})
        ports["cmd_data_0"] = [42, "0"] + ["0"] * 58
        good = {"modules": {"top": {"cells": {"display.memory.f2sdram": {
            "type": "cyclonev_hps_interface_fpga2sdram", "connections": ports}}}}}
        self.assertTrue(producer._session_display_evidence(good, "fixture", ROOT)["read_only"])
        for port, bits in (("wr_valid_0", [42]), ("cmd_valid_1", [42]),
                           ("cmd_data_0", [42, "1"] + ["0"] * 58),
                           ("cfg_port_width", ["x"] * 12)):
            changed = json.loads(json.dumps(good))
            changed["modules"]["top"]["cells"]["display.memory.f2sdram"]["connections"][port] = bits
            with self.subTest(port=port), self.assertRaises(BuildError):
                producer._session_display_evidence(changed, "fixture", ROOT)

    def test_only_three_unsigned_plane_products_are_allowed(self) -> None:
        good = {f"display.product{i}": {"type": "MISTRAL_MUL9X9",
                    "parameters": {"A_SIGNED": "0", "B_SIGNED": "0"}} for i in range(3)}
        def graph(cells):
            return {"modules": {"top": {"cells": cells}}}
        producer._plane_dsp_evidence(graph(good))
        for cells in (dict(list(good.items())[:2]),
                      {**good, "machine.extra": good["display.product0"]},
                      {"machine.product0": good["display.product0"], **dict(list(good.items())[1:])},
                      {**good, "display.product0": {**good["display.product0"],
                          "parameters": {"A_SIGNED": "1", "B_SIGNED": "0"}}}):
            with self.assertRaises(BuildError):
                producer._plane_dsp_evidence(graph(cells))

    def test_firmware_lanes_match_the_rtl(self) -> None:
        rtl = (ROOT / "cores/fes-c64/rtl/c64_rom.v").read_text()
        for index, lane in enumerate(producer.FIRMWARE_LANE_ROWS):
            column, row = (5, lane) if isinstance(lane, int) else lane
            self.assertIn(f'(* keep,BEL="MISTRAL_M10K.{column}.{row}.0" *)', rtl)
            self.assertIn(f") lane{index} (", rtl)
            self.assertIn(".CFG_ASYNC_READ(0)", rtl)
            self.assertIn(".B1EN(1'b1)", rtl)
            self.assertIn(".CLK1(clk)", rtl)
        self.assertNotIn("CFG_ASYNC_READ(1)", rtl)
        self.assertIn("bank_d<=address[14:10]", rtl)
        self.assertIn("cpu_stage<=address<20480?memory[address]", rtl)
        producer_source = (ROOT / "scripts/build_fes_c64_oss.py").read_text()
        self.assertIn("expected_async_read=0", producer_source)

    def test_netlist_check_rejects_any_async_m10k(self) -> None:
        def cell(name: str, async_read: str | None) -> tuple[str, dict]:
            parameters = {} if async_read is None else {"CFG_ASYNC_READ": async_read}
            return name, {"type": "MISTRAL_M10K", "parameters": parameters, "connections": {
                "CLK1": [7], "A1EN": ["1"], "B1EN": ["1"], "ACLR0": ["0"], "ACLR1": ["0"]}}
        lanes = dict(cell(f"machine.rom.lane{index}", "0") for index in range(36))
        for index in producer.CHARACTER_ROM_LANES:
            lane=lanes[f"machine.rom.lane{index}"];lane["type"]="MISTRAL_M10K_TDP"
            lane["connections"].update(CLK2=[7],A1WE=["0"],B1WE=["0"])
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

    def test_character_rom_requires_native_read_clocks_and_disabled_writes(self):
        cells={}
        for index in range(36):
            pins={"CLK1":[7],"A1EN":["1"],"B1EN":["1"],"ACLR0":["0"],"ACLR1":["0"]}
            if index in producer.CHARACTER_ROM_LANES:pins.update(CLK2=[7],A1WE=["0"],B1WE=["0"])
            cells[f"machine.rom.lane{index}"]={"type":"MISTRAL_M10K_TDP" if index in producer.CHARACTER_ROM_LANES else "MISTRAL_M10K","connections":pins}
        producer.validate_firmware_ports(cells)
        for field,value in [("A1WE",["1"]),("B1WE",["1"]),("CLK2",["0"]),("A1BE",["1","0"]),("B1BE",["0","0"])]:
            changed=json.loads(json.dumps(cells));changed["machine.rom.lane16"]["connections"][field]=value
            with self.subTest(field=field),self.assertRaises(BuildError):producer.validate_firmware_ports(changed)
        for index in range(16,20):cells[f"machine.rom.lane{index}"]["connections"]["CLK2"]=[8]
        with self.assertRaises(BuildError):producer.validate_firmware_ports(cells)

    def test_color_ram_must_be_one_synchronous_native_clock_m10k(self) -> None:
        name = "machine.vic.color_ram.ram.0.0"
        cell = {"type": "MISTRAL_M10K_TDP", "connections": {
            "CLK1": [7], "CLK2": [7], "A1EN": ["1"], "B1EN": ["1"],
            "A1WE": [9], "B1WE": ["0"],
        }}
        producer.validate_color_ram({name: cell})
        for field, value in (("CLK2", [8]), ("A1WE", ["0"]), ("B1WE", [10])):
            changed = json.loads(json.dumps(cell))
            changed["connections"][field] = value
            with self.assertRaises(BuildError):
                producer.validate_color_ram({name: changed})
        with self.assertRaises(BuildError):
            producer.validate_color_ram({name: {**cell, "type": "MISTRAL_M10K"}})
        with self.assertRaises(BuildError):
            producer.validate_color_ram({name: cell, name + ".1": cell})


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
        program = producer.build_commands(ROOT, ROOT / producer.OUTPUT_RELATIVE, "0" * 32,
            {"yosys": Path(yosys), "nextpnr-mistral": Path("/n")})[0][2]
        program = program.split("stat; write_json", 1)[0].replace("-run coarse:;", "-run coarse:map_luts;")
        program += (
            "autoname; select -module top; "
            "log ---SDP---; select -count t:MISTRAL_M10K; "
            "log ---TDP---; select -count t:MISTRAL_M10K_TDP; "
            "log ---ASYNC---; "
            "select -list t:MISTRAL_M10K r:CFG_ASYNC_READ=1 %i; "
            "select -list t:MISTRAL_M10K_TDP r:CFG_ASYNC_READ=1 %i; "
            "log ---LANES---; select -list t:MISTRAL_M10K t:MISTRAL_M10K_TDP %u r:CFG_ASYNC_READ=0 %i; "
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
        # The default SID audio stage keeps its tables in logic; the opt-in
        # reSID chain's VCR/cutoff/summer ROM lanes are not in the package.
        # Plane FIFO banks may change shape with the memory mapper. Keep the
        # firmware/frame-store contract exact and require a synchronous FIFO.
        fifo = [name for name in lanes if name.startswith("display.scanout.reader.fifo")]
        self.assertTrue(fifo, "session display FIFO did not map to synchronous M10K")
        machine_lanes = [name for name in lanes if name not in fifo]
        self.assertEqual(objects("TDP"), "240 objects.")
        self.assertEqual(machine_lanes, sorted(
            [f"machine.rom.lane{index}" for index in range(36)] +
            [f"machine.vic.frame_store.pages[{page}].banks[{bank}].ram"
             for page in range(3) for bank in range(51)]))

    def test_full_synth_has_live_memory_clocks(self) -> None:
        """The native drive synthesizes without repairing disconnected memory clocks."""
        yosys = _yosys_binary()
        assert yosys is not None
        abc = Path(yosys).with_name("yosys-abc")
        if not abc.is_file():
            self.skipTest("yosys-abc is required for the post-ABC C64 memory check")
        sources = " ".join(producer.RTL_SOURCES)
        import tempfile
        with tempfile.TemporaryDirectory() as directory:
            synth = Path(directory) / "synth.json"
            log_path = Path(directory) / "yosys.log"
            program = producer.build_commands(ROOT, ROOT / producer.OUTPUT_RELATIVE, "0" * 32,
                {"yosys": Path(yosys), "nextpnr-mistral": Path("/n")})[0][2]
            program = program.replace("write_json " + producer.OUTPUT_RELATIVE.as_posix() + "/synth.json",
                                      "write_json " + str(synth))
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
            self.assertEqual(open_clocks, [])
            producer.validate_synth_evidence(Path(directory))
            producer.validate_memory_clocks(cells)
            patched = cells
            producer.validate_firmware_ports(patched)
            producer.validate_color_ram(patched)
            producer.validate_frame_store(patched)
            name = "machine.vic.frame_store.pages[0].banks[0].ram"
            for group, key, value in (
                    ("parameters", "CFG_DBITS", f"{10:032b}"),
                    ("connections", "A1EN", ["0"]),
                    ("connections", "CLK2", patched[name]["connections"]["CLK1"]),
                    ("connections", "B1EN", ["0"])):
                changed = dict(patched)
                changed[name] = {**patched[name], group: {**patched[name][group], key: value}}
                with self.subTest(group=group, key=key), self.assertRaises(BuildError):
                    producer.validate_frame_store(changed)

            still_open = [
                name for name, cell in patched.items()
                if cell.get("type") == "MISTRAL_M10K" and cell.get("connections", {}).get("CLK1") == ["x"]
            ]
            self.assertEqual(still_open, [])

            # Exercise the actual primitive contract on this already-built netlist.
            # Minimal Yosys-only hosts retain the memory checks without packing.
            nextpnr = Path(yosys).resolve().with_name("nextpnr-mistral")
            if nextpnr.is_file() and os.access(nextpnr, os.X_OK):
                qsf = Path(directory) / "socket.qsf"
                qsf.write_text(producer.socket_qsf((ROOT / producer.QSF).read_text()))
                packed = Path(directory) / "packed.json"
                pack = subprocess.run([
                    str(nextpnr), "--json", str(synth), "--device", producer.TARGET,
                    "--qsf", str(qsf), "--sdc", str(ROOT / producer.SDC),
                    "--pack-only", "--write", str(packed),
                ], cwd=ROOT, text=True, capture_output=True, check=False, timeout=300)
                output = pack.stdout + pack.stderr
                self.assertEqual(pack.returncode, 0, output[-4000:])
                self.assertIn("Program finished normally.", output[-4000:])
                self.assertTrue(packed.is_file())


if __name__ == "__main__":
    unittest.main()
