"""Check the developer video socket and producer gates before real routing."""

import copy
import json
import re
import tempfile
import tomllib
import unittest
from pathlib import Path

from scripts import build_coleco_sgm as sgm
from scripts import build_fes_coleco_socket_v2 as shell
from scripts import build_video_part as part
from scripts import coleco_expansion, video_parts
from tests.test_coleco_sgm_build import clock_coverage_route_fixture


def video_boundary_fixture(*, routed=False, aliases=True):
    clock = [9000]
    source = list(range(100, 132))
    request = list(range(200, 232))
    response = list(range(300, 328))
    top = {"cells": {}, "netnames": {video_parts.CLOCK: {"bits": clock}}}
    if aliases:
        top["netnames"].update(video_request={"bits": source},
                               video_plug_request={"bits": request},
                               video_response={"bits": response})
    key = "NEXTPNR_BEL" if routed else "BEL"
    for index, (name, bel) in enumerate(video_parts.boundary_bels().items()):
        datain, q = ["0"], [1000 + index]
        if name.startswith("plug_request_ff_"):
            bit = int(name.rsplit("_", 1)[1])
            datain, q = [source[bit]], [request[bit]]
        elif name.startswith("plug_response_ff_"):
            bit = int(name.rsplit("_", 1)[1])
            q = [response[bit]]
        top["cells"][video_parts.PREFIX + name] = {
            "type": "MISTRAL_FF", "attributes": {key: bel},
            "connections": {"CLK": clock, "DATAIN": datain, "Q": q},
        }
    return top


def routed_shell_fixture():
    """Both real socket boundaries plus the qualified PLL replay metadata."""
    top = video_boundary_fixture(routed=True, aliases=False)
    pins = {name: [0, name] for name in ("locked", "outclk", "refclk", "rst")}
    pins.update({f"outclk[{bit}]": [0, f"outclk[{bit}]"] for bit in range(2)})
    top["cells"].update({
        "system_clock.pll": {
            "type": "altera_pll",
            "attributes": {"FES_PINMAP_V1": json.dumps({"count": 6, "pins": pins}).encode().hex()},
            "connections": {"locked": [1], "outclk": [2], "refclk": [3]},
            "port_directions": {"outclk": "output"},
        },
        "system_clock.clocks_MISTRAL_CLKBUF_Q_1": {
            "type": "MISTRAL_CLKBUF", "connections": {"A": [2], "Q": [2107]}},
        "system_clock.clocks_MISTRAL_CLKBUF_Q": {
            "type": "MISTRAL_CLKBUF", "connections": {"A": [901], "Q": [904]}},
    })
    top["cells"].update({
        name: {"type": "MISTRAL_FF", "attributes": {"NEXTPNR_BEL": bel},
               "connections": {"CLK": [2107], "DATAIN": ["0"], "Q": [500 + index]}}
        for index, (name, bel) in enumerate(coleco_expansion.socket_bels_v2().items())
    })
    top["cells"][coleco_expansion.SOCKET_CLOCK_COVERAGE_CELL] = {
        "type": "MISTRAL_FF", "attributes": {
            "NEXTPNR_BEL": coleco_expansion.SOCKET_CLOCK_COVERAGE_BEL},
        "connections": {"CLK": [2107], "DATAIN": ["0"], "Q": [8000]},
    }
    top["netnames"].update({
        "system_clock.pll_outclk_1": {"bits": [901]},
        "system_clock.clocks[1]": {"bits": [904]},
        "system_clock.clocks[0]": {"bits": [2107], "attributes": {
            "ROUTING": clock_coverage_route_fixture()}},
    })
    return {"modules": {"top": top}}


class VideoPartsBoundaryTest(unittest.TestCase):
    def test_synthesis_and_routed_boundary_accept_loss_of_net_aliases(self):
        video_parts.validate_boundary(video_boundary_fixture(), routed=False)
        top = video_boundary_fixture(routed=True, aliases=False)
        video_parts.validate_boundary(top, routed=True)
        design = json.loads(video_parts.prepare_scaffold(json.dumps({"modules": {"top": top}}).encode()))
        prepared = design["modules"]["top"]
        self.assertNotIn("video_plug_request", prepared["netnames"])
        # The compiler finds each request through its pinned FF Q port even
        # after nextpnr has discarded the unused aggregate bus aliases.
        for bit in range(video_parts.REQUEST_BITS):
            self.assertEqual(prepared["cells"][f"plug_addr_ff_{bit}"]["connections"]["Q"], [200 + bit])
        for bit in range(video_parts.RESPONSE_BITS):
            self.assertEqual(prepared["cells"][f"plug_rdata_ff_{bit}"]["connections"]["Q"], [300 + bit])

    def test_boundary_rejects_wrong_clock_source_and_vacant_response(self):
        for routed in (False, True):
            with self.subTest(routed=routed):
                top = video_boundary_fixture(routed=routed, aliases=not routed)
                top["cells"][video_parts.PREFIX + "plug_request_ff_7"]["connections"]["CLK"] = [2107]
                with self.assertRaisesRegex(ValueError, "placement/clock"):
                    video_parts.validate_boundary(top, routed=routed)
        for name, pin, value, error in (
                ("plug_request_ff_7", "DATAIN", [999], "source boundary input"),
                ("plug_request_ff_7", "Q", [999], "source boundary changed"),
                ("plug_response_ff_7", "DATAIN", ["1"], "vacant video response"),
                ("plug_response_ff_7", "Q", [999], "response boundary changed")):
            with self.subTest(name=name, pin=pin):
                top = video_boundary_fixture()
                top["cells"][video_parts.PREFIX + name]["connections"][pin] = value
                with self.assertRaisesRegex(ValueError, error):
                    video_parts.validate_boundary(top, routed=False)

    def test_routed_reservation_rejects_an_unrelated_cell(self):
        top = video_boundary_fixture(routed=True, aliases=False)
        top["cells"]["machine.intruder"] = {
            "type": "MISTRAL_ALUT4", "attributes": {"NEXTPNR_BEL": "MISTRAL_COMB.28.38.0"}}
        with self.assertRaisesRegex(ValueError, "machine.intruder"):
            video_parts.validate_boundary(top, routed=True)
        top["cells"]["machine.intruder"]["attributes"]["NEXTPNR_BEL"] = "MISTRAL_COMB.29.38.0"
        video_parts.validate_boundary(top, routed=True)

    def test_scaffold_preserves_cpu_clock_route_and_exposes_only_video(self):
        original = routed_shell_fixture()
        original_bytes = json.dumps(original).encode()
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary) / "routed.json"
            destination = Path(temporary) / "scaffold.json"
            source.write_bytes(original_bytes)
            produced = part.prepare_scaffold(source, destination)
            self.assertEqual(source.read_bytes(), original_bytes)
            self.assertEqual(destination.read_bytes(), produced)
        top = json.loads(produced)["modules"]["top"]
        old = original["modules"]["top"]
        self.assertEqual(top["netnames"], old["netnames"])
        self.assertEqual(top["cells"][coleco_expansion.SOCKET_CLOCK_COVERAGE_CELL],
                         old["cells"][coleco_expansion.SOCKET_CLOCK_COVERAGE_CELL])
        for name in coleco_expansion.socket_bels_v2():
            self.assertEqual(top["cells"]["cpu_" + name], old["cells"][name])
        self.assertFalse(any(name.startswith(video_parts.PREFIX) for name in top["cells"]))
        self.assertEqual(sum(name.startswith("plug_addr_ff_") for name in top["cells"]), 32)
        self.assertEqual(sum(name.startswith("plug_rdata_ff_") for name in top["cells"]), 28)
        self.assertFalse(any(name.startswith("clock_coverage_ff_") for name in top["cells"]))
        pll = top["cells"]["system_clock.pll"]
        self.assertEqual(pll["connections"]["outclk[1]"], [901])
        pins = json.loads(bytes.fromhex(pll["attributes"]["FES_PINMAP_V1"]))
        self.assertNotIn("outclk[0]", pins["pins"])
        self.assertEqual(pins["pins"]["outclk[1]"], [0, "outclk[1]"])

    def test_scaffold_rejects_cpu_alias_collision(self):
        design = routed_shell_fixture()
        cells = design["modules"]["top"]["cells"]
        cells["cpu_plug_addr_ff_0"] = copy.deepcopy(cells["plug_addr_ff_0"])
        with self.assertRaisesRegex(ValueError, "CPU boundary alias collision"):
            video_parts.prepare_scaffold(json.dumps(design).encode())

    def test_cpu_and_video_qsf_and_cram_reservations_are_disjoint(self):
        base = coleco_expansion.shell_qsf("", version=2)
        qsf = video_parts.shell_qsf(base)
        rectangles = re.findall(r'FES_RESERVED_RECT "([^"]+)"', qsf)
        self.assertEqual(rectangles, ["24 1 28 19", "video 24 23 28 38"])
        cpu = tuple(map(int, rectangles[0].split()))
        video = tuple(map(int, rectangles[1].split()[1:]))
        self.assertLess(cpu[3], video[1])
        self.assertEqual(sgm.CRAM_REGION[3], video_parts.CRAM[1])
        for malformed in ("", base + base, qsf):
            with self.subTest(base=malformed), self.assertRaisesRegex(ValueError, "unchanged Coleco"):
                video_parts.shell_qsf(malformed)


class VideoPartsProducerTest(unittest.TestCase):
    def test_video_shell_is_explicit_and_keeps_default_output_and_interface(self):
        tools = {"yosys": Path("/tool/yosys"), "nextpnr-mistral": Path("/tool/nextpnr-mistral")}
        default = shell.build_commands(shell.ROOT, shell.ROOT / shell.OUTPUT_RELATIVE, "a" * 32, tools)
        self.assertEqual(default, shell.build_commands(shell.ROOT, shell.ROOT / shell.OUTPUT_RELATIVE,
                                                       "a" * 32, tools, video_socket=False))
        video = shell.build_commands(shell.ROOT, shell.ROOT / shell.VIDEO_OUTPUT_RELATIVE,
                                     "a" * 32, tools, video_socket=True)
        self.assertNotIn("FES_COLECO_VIDEO_PART_DEV", default[0][-1])
        self.assertNotIn(video_parts.RTL, default[0][-1])
        self.assertIn("-DFES_COLECO_VIDEO_PART_DEV=1", video[0][-1])
        self.assertIn(video_parts.RTL, video[0][-1])
        for commands, relative in ((default, shell.OUTPUT_RELATIVE), (video, shell.VIDEO_OUTPUT_RELATIVE)):
            for flag, member in (("--json", "synth.json"), ("--qsf", "socket.qsf"),
                                 ("--rbf", "core.rbf"), ("--write", "routed.json")):
                self.assertEqual(commands[1][commands[1].index(flag) + 1], str(relative / member))
        with self.assertRaisesRegex(ValueError, "output path"):
            shell.build_commands(shell.ROOT, shell.ROOT / shell.OUTPUT_RELATIVE,
                                 "a" * 32, tools, video_socket=True)
        record = b'{"format":2,"parameters":{},"recipe_sha256":"' + b'a' * 64 + b'"}'
        evidence = {"rbf": {"size": 100, "sha256": "b" * 64}, "build_id": "c" * 32}
        identities = {"yosys": "fixture", "nextpnr-mistral": "fixture", "mistral": "fixture"}
        arguments = (record, evidence, "https://example.invalid/fes.git", "d" * 40, identities)
        baseline = shell.manifest(*arguments)
        self.assertEqual(baseline, shell.manifest(*arguments, video_socket=False))
        base_fields = tomllib.loads(baseline.decode())
        video_fields = tomllib.loads(shell.manifest(*arguments, video_socket=True).decode())
        marker = {"id": video_parts.INTERFACE, "major": 1, "minor": 0, "required": False}
        cpu = {"id": "fes.expansion.coleco-bus", "major": 2, "minor": 0, "required": False}
        self.assertNotIn(marker, base_fields["interfaces"])
        self.assertIn(marker, video_fields["interfaces"])
        self.assertIn(cpu, base_fields["interfaces"])
        self.assertIn(cpu, video_fields["interfaces"])
        self.assertEqual(base_fields["format"], 2)
        self.assertEqual(video_fields["format"], 2)

    def test_compiler_output_requires_nonempty_bounded_regular_bytes(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            good = root / "cart.rbf"
            good.write_bytes(b"1234")
            part.require_output(good, 4)
            empty = root / "empty"
            empty.touch()
            link = root / "linked-output"
            link.symlink_to(good)
            for bad, maximum in ((empty, 4), (good, 3), (link, 4), (root, 4), (root / "missing", 4)):
                with self.subTest(path=bad, maximum=maximum), self.assertRaisesRegex(ValueError, "bounded regular"):
                    part.require_output(bad, maximum)

    def test_cram_report_uses_video_region_and_refuses_outside_publication(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            changes = {"bits_inside_slot": 8, "bits_outside_slot": 1,
                       "outside_slot_coordinates": [[1769, 1799]],
                       "outside_slot_coordinates_truncated": False}
            report = part.write_cram_report(output, b"cart bytes", changes)
            observed = json.loads(report.read_bytes())
            self.assertEqual(observed["cram_region"], list(video_parts.CRAM))
            self.assertEqual(observed["map"], video_parts.MAP)
            self.assertEqual(observed["cart_sha256"], sgm.digest(b"cart bytes"))
            self.assertFalse(observed["archive_published"])
            self.assertEqual(observed["route_contract"], "failed")
            with self.assertRaisesRegex(ValueError, "outside-region"):
                part.write_cram_report(output, b"cart bytes", changes, part_id="a" * 64)
            with self.assertRaisesRegex(ValueError, "outside the socket"):
                sgm.enforce_cram_region(changes, report)
            self.assertEqual(json.loads(report.read_bytes()), observed)
            inside = {"bits_inside_slot": 8, "bits_outside_slot": 0}
            part.write_cram_report(output, b"cart bytes", inside, part_id="a" * 64)
            accepted = json.loads(report.read_bytes())
            self.assertTrue(accepted["archive_published"])
            self.assertEqual(accepted["part_id"], "a" * 64)
            self.assertEqual(accepted["route_contract"], "passed")

    def test_only_cart_clock_pins_are_required_on_pixel_clock(self):
        routed = {"modules": {"top": {"netnames": {"pixel_clk": {"bits": [9000]}}, "cells": {
            "system.machine_ff": {"type": "MISTRAL_FF", "connections": {"CLK": [2107]}},
            "fes_cart$filter_ff": {"type": "MISTRAL_FF", "connections": {"CLK": [9000]}},
            "fes_cart$filter_ram": {"type": "MISTRAL_M10K_TDP", "connections": {
                "CLK1": [9000], "CLK2": [9000]}},
        }}}}
        self.assertEqual(part.validate_clocks(routed), 3)
        routed["modules"]["top"]["cells"]["fes_cart$filter_ram"]["connections"]["CLK2"] = [2107]
        with self.assertRaisesRegex(ValueError, "not on the pixel clock"):
            part.validate_clocks(routed)


if __name__ == "__main__":
    unittest.main()
