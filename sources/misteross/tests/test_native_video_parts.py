"""Physical native socket checks, independent of an expensive FPGA route."""
import copy
import json
import re
import tempfile
import unittest
from pathlib import Path

from scripts import build_video_part, coleco_expansion, native_video_parts as native, video_parts
from scripts import build_coleco_sgm as sgm
from scripts.cyclonev_rbf import SX120F, tile_column_cram_x
from tests.test_video_parts_build import routed_shell_fixture


def boundary_fixture(*, routed=False, aliases=True):
    top = {"cells": {}, "netnames": {native.CLOCK: {"bits": [9000]}}}
    if aliases:
        top["netnames"].update(video_request={"bits": list(range(100, 132))},
                               video_plug_request={"bits": list(range(200, 232))},
                               video_response={"bits": list(range(300, 328))})
    key = "NEXTPNR_BEL" if routed else "BEL"
    for index, (name, bel) in enumerate(native.boundary_bels().items()):
        data, q = ["0"], [1000 + index]
        if name.startswith("plug_request_ff_"):
            bit = int(name.rsplit("_", 1)[1])
            data, q = [100 + bit], [200 + bit]
        elif name.startswith("plug_response_ff_"):
            q = [300 + int(name.rsplit("_", 1)[1])]
        top["cells"][native.PREFIX + name] = {
            "type": "MISTRAL_FF", "attributes": {key: bel},
            "connections": {"CLK": [9000], "DATAIN": data, "Q": q}}
    return top


class NativeVideoPartsTest(unittest.TestCase):
    def test_native_policy_is_distinct_and_has_disjoint_cpu_fence(self):
        self.assertEqual(native.INTERFACE, "fes.fabric.video.native-pixels")
        self.assertEqual(native.MAP, "fes.coleco-native-video.socket/1")
        self.assertEqual(native.LAYOUT, "fes.coleco-native-video.parts/1")
        for key in ("INTERFACE", "MAP", "LAYOUT", "PLACEMENT", "CRAM", "RTL"):
            self.assertNotEqual(getattr(native, key), getattr(video_parts, key))
        qsf = native.shell_qsf(coleco_expansion.shell_qsf("", version=2))
        self.assertEqual(re.findall(r'FES_RESERVED_RECT "([^"]+)"', qsf),
                         ["24 1 28 19", "video 5 23 38 38"])
        self.assertEqual(native.CRAM, (124, 1800, 3906, 3442))
        self.assertEqual(sgm.CRAM_REGION[3], native.CRAM[1])
        base = coleco_expansion.shell_qsf("", version=2)
        for bad in ("", base + base, qsf, video_parts.shell_qsf(base)):
            with self.subTest(base=bad), self.assertRaisesRegex(ValueError, "unchanged Coleco"):
                native.shell_qsf(bad)

    def test_cram_fence_contains_ram_configuration_and_matches_device_columns(self):
        self.assertEqual(tile_column_cram_x(SX120F, 2)[0], native.CRAM[0])
        self.assertEqual(tile_column_cram_x(SX120F, 44)[0], native.CRAM[2])
        # Locked Mistral M10K table gives offsets x0..258/y0..85.  Col5
        # has no BELs in rows23..31; the other three RAM columns span all16.
        sites = [(column, row) for column in (5, 14, 26, 38)
                 for row in range(32 if column == 5 else 23, 39)]
        self.assertEqual(len(sites), 55)
        for column, row in sites:
            x = tile_column_cram_x(SX120F, column)[0]
            y = 2 + 86 * row
            self.assertGreaterEqual(x, native.CRAM[0])
            self.assertLess(x + 258, native.CRAM[2])
            self.assertGreaterEqual(y, native.CRAM[1])
            self.assertLess(y + 85, native.CRAM[3])

    def test_rtl_pins_match_helper_and_cover_six_columns_without_collisions(self):
        source = (Path(__file__).parents[1] / native.RTL).read_text()
        physical = dict(re.findall(
            r'BEL = "([^"]+)" \*\) MISTRAL_FF (\w+)', source))
        expected = {bel: name for name, bel in native.boundary_bels().items()}
        self.assertEqual(physical, expected)
        self.assertEqual(len(expected), len(native.boundary_bels()))
        self.assertEqual(len(expected), 153)
        for name, bel in native.boundary_bels().items():
            pattern = re.escape(f'MISTRAL_FF {name} (') + r'\s*\.CLK\(clock\), \.DATAIN\(([^)]+)\), \.Q\(([^)]+)\)'
            match = re.search(pattern, source)
            self.assertIsNotNone(match, name)
            bit = int(name.rsplit("_", 1)[1])
            if name.startswith("plug_request_ff_"):
                self.assertEqual(match.groups(), (f"request[{bit}]", f"plug_request[{bit}]"))
            elif name.startswith("plug_response_ff_"):
                self.assertEqual(match.groups(), (f"plug_response[{bit}]", f"response[{bit}]"))
            else:
                self.assertEqual(match.groups(), ("1'b0", f"clock_coverage_unused[{bit}]"))
        anchors = {bel for name, bel in native.boundary_bels().items()
                   if name.startswith("clock_coverage_ff_")}
        for column in (10, 18, 24, 28, 35, 37):
            for row in range(26 if column == 24 else 23, 39):
                self.assertIn(f"MISTRAL_FF.{column}.{row}.56", anchors)

    def test_accepts_synth_and_routed_net_alias_loss(self):
        native.validate_boundary(boundary_fixture(), routed=False)
        for aliases in (False, True):
            native.validate_boundary(boundary_fixture(routed=True, aliases=aliases), routed=True)

    def test_rejects_wrong_clock_bel_type_and_missing_cell(self):
        for routed in (False, True):
            for change in ("clock", "bel", "type", "missing"):
                with self.subTest(routed=routed, change=change):
                    top = boundary_fixture(routed=routed, aliases=not routed)
                    name = native.PREFIX + "plug_request_ff_7"
                    cell = top["cells"][name]
                    if change == "clock": cell["connections"]["CLK"] = [2107]
                    elif change == "bel": cell["attributes"]["NEXTPNR_BEL" if routed else "BEL"] = "MISTRAL_FF.24.23.52"
                    elif change == "type": cell["type"] = "MISTRAL_ALUT4"
                    else: del top["cells"][name]
                    with self.assertRaisesRegex(ValueError, "placement/clock"):
                        native.validate_boundary(top, routed=routed)

    def test_rejects_source_response_and_clock_anchor_changes(self):
        for name, pin, value, error in (
                ("plug_request_ff_7", "DATAIN", [999], "source boundary input"),
                ("plug_request_ff_7", "Q", [999], "source boundary changed"),
                ("plug_response_ff_7", "Q", [999], "response boundary changed"),
                ("plug_response_ff_7", "DATAIN", ["1"], "vacant native video response"),
                ("clock_coverage_ff_7", "DATAIN", ["1"], "clock anchor input")):
            with self.subTest(name=name, pin=pin):
                top = boundary_fixture()
                top["cells"][native.PREFIX + name]["connections"][pin] = value
                with self.assertRaisesRegex(ValueError, error):
                    native.validate_boundary(top, routed=False)
        for alias in (native.CLOCK, "video_request", "video_plug_request", "video_response"):
            top = boundary_fixture()
            top["netnames"][alias]["bits"].pop()
            with self.subTest(alias=alias), self.assertRaisesRegex(ValueError, "clock or width"):
                native.validate_boundary(top, routed=False)

    def test_routed_q_must_identify_distinct_physical_bits(self):
        for value, error in (([], "boundary Q"), (["0"], "boundary Q"), ([200], "Q aliases")):
            top = boundary_fixture(routed=True, aliases=False)
            top["cells"][native.PREFIX + "plug_response_ff_7"]["connections"]["Q"] = value
            with self.subTest(value=value), self.assertRaisesRegex(ValueError, error):
                native.validate_boundary(top, routed=True)
        top = boundary_fixture(routed=True)
        top["netnames"]["video_plug_request"]["bits"][7] = 999
        with self.assertRaisesRegex(ValueError, "request boundary changed"):
            native.validate_boundary(top, routed=True)

    def test_rejects_shell_occupancy_across_wider_reserved_region(self):
        for column, row, kind in ((5, 32, "MISTRAL_M10K"), (14, 23, "MISTRAL_M10K"),
                                  (38, 38, "MISTRAL_M10K"), (10, 23, "MISTRAL_COMB")):
            top = boundary_fixture(routed=True, aliases=False)
            top["cells"]["machine.intruder"] = {"attributes": {
                "NEXTPNR_BEL": f"{kind}.{column}.{row}.0"}}
            with self.subTest(column=column, row=row), self.assertRaisesRegex(ValueError, "machine.intruder"):
                native.validate_boundary(top, routed=True)
        for column, row in ((4, 32), (39, 38), (14, 22), (14, 39), (24, 19)):
            top = boundary_fixture(routed=True, aliases=False)
            top["cells"]["machine.outside"] = {"attributes": {
                "NEXTPNR_BEL": f"MISTRAL_COMB.{column}.{row}.0"}}
            native.validate_boundary(top, routed=True)

    def test_real_producer_scaffold_repair_retains_cpu_and_frozen_clock_routes(self):
        design = routed_shell_fixture()
        top = design["modules"]["top"]
        for name in list(top["cells"]):
            if name.startswith(video_parts.PREFIX): del top["cells"][name]
        top["cells"].update(boundary_fixture(routed=True, aliases=False)["cells"])
        source_bytes = json.dumps(design).encode()
        with tempfile.TemporaryDirectory() as directory:
            source, destination = Path(directory) / "routed.json", Path(directory) / "scaffold.json"
            source.write_bytes(source_bytes)
            result = build_video_part.prepare_scaffold(source, destination, layout=native)
            self.assertEqual(source.read_bytes(), source_bytes)
            self.assertEqual(result, destination.read_bytes())
        prepared = json.loads(result)["modules"]["top"]
        self.assertEqual(prepared["netnames"], top["netnames"])
        self.assertEqual(prepared["cells"][coleco_expansion.SOCKET_CLOCK_COVERAGE_CELL],
                         top["cells"][coleco_expansion.SOCKET_CLOCK_COVERAGE_CELL])
        for name in coleco_expansion.socket_bels_v2():
            self.assertEqual(prepared["cells"]["cpu_" + name], top["cells"][name])
        self.assertFalse(any(name.startswith(native.PREFIX) for name in prepared["cells"]))
        for kind, count, offset in (("addr", 32, 200), ("rdata", 28, 300)):
            for bit in range(count):
                self.assertEqual(prepared["cells"][f"plug_{kind}_ff_{bit}"]["connections"]["Q"], [offset + bit])

    def test_scaffold_rejects_cpu_alias_collision(self):
        top = boundary_fixture(routed=True, aliases=False)
        cpu = {"type": "MISTRAL_FF", "connections": {"CLK": [2107]},
               "attributes": {"NEXTPNR_BEL": "MISTRAL_FF.24.1.2"}}
        top["cells"].update(plug_addr_ff_0=cpu, cpu_plug_addr_ff_0=copy.deepcopy(cpu))
        with self.assertRaisesRegex(ValueError, "CPU boundary alias collision"):
            native.prepare_scaffold(json.dumps({"modules": {"top": top}}).encode())


if __name__ == "__main__":
    unittest.main()
