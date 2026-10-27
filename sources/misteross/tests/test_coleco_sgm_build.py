"""The SGM build lane has a separate, closed physical socket contract."""

import json
import tempfile
import tomllib
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from scripts import coleco_expansion
from scripts import build_fes_coleco_oss as factory
from scripts import build_fes_coleco_socket_v2 as shell
from scripts import build_coleco_sgm as sgm
from scripts import fes_de10nano_evidence
from scripts.fes_build_common import BuildError


def clock_coverage_route_fixture():
    entries = (
        ("GCLK.0.36.3", "", "5"),
        ("HCLK.16.4.5", "SCLKB2.16.4.5.HCLK.16.4.5", "5"),
        ("HCLKB.16.4.5", "HCLK.16.4.5.HCLKB.16.4.5", "5"),
        ("XCLKB1.24.4.5", "HCLKB.16.4.5.XCLKB1.24.4.5", "5"),
        ("XCLKB2A.24.4.5", "XCLKB1.24.4.5.XCLKB2A.24.4.5", "5"),
        ("TCLK.24.4.0", "XCLKB2A.24.4.5.TCLK.24.4.0", "5"),
        ("WIRE.24.4.CLK0", "TCLK.24.4.0.WIRE.24.4.CLK0", "5"),
        ("WIRE.24.4.CLKT[9]", "WIRE.24.4.CLK0.WIRE.24.4.CLKT[9]", "5"),
    )
    return ";".join(field for entry in entries for field in entry)


def route_through_fixture(name="anchor", *, ff_bel="MISTRAL_FF.24.4.56",
                          buffer_bel="MISTRAL_COMB.24.4.54", pin="F0"):
    return {"cells": {
        name: {"type": "MISTRAL_FF", "attributes": {"NEXTPNR_BEL": ff_bel},
               "connections": {"CLK": [2107], "DATAIN": [10002], "Q": [10003]}},
        name + "$ROUTETHRU": {
            "type": "MISTRAL_BUF", "parameters": {},
            "attributes": {"NEXTPNR_BEL": buffer_bel, "FES_PINMAP_V1": json.dumps({
                "count": 2, "pins": {"A": [0, pin], "Q": [0, "COMBOUT"]}}).encode().hex()},
            "connections": {"A": [10001], "Q": [10002]},
            "port_directions": {"A": "input", "Q": "output"}},
        "ground": {"type": "MISTRAL_CONST", "connections": {"Q": [10001]}},
    }, "netnames": {
        "system_clock.clocks[0]": {"bits": [2107], "attributes": {
            "ROUTING": clock_coverage_route_fixture()}},
        "ground": {"bits": [10001], "attributes": {"ROUTING": "frozen shared constant"}},
        name + "$ROUTETHRU$conn$Q": {"bits": [10002], "attributes": {
            "ROUTING": "WIRE.24.4.FFIN[36];WIRE.24.4.COMBOUT[18].WIRE.24.4.FFIN[36];1;"
                       "WIRE.24.4.COMBOUT[18];;1"}},
    }}


class BoundaryRouteThroughTest(unittest.TestCase):
    def test_matching_comb_halves_and_mlab_buffers(self):
        for ff_bel, buffer_bel, pin in (
                ("MISTRAL_FF.24.4.56", "MISTRAL_COMB.24.4.54", "F0"),
                ("MISTRAL_FF.24.4.58", "MISTRAL_COMB.24.4.55", "F1"),
                ("MISTRAL_FF.28.23.56", "MISTRAL_MCOMB.28.23.54", "F0")):
            with self.subTest(ff_bel=ff_bel):
                top = route_through_fixture(ff_bel=ff_bel, buffer_bel=buffer_bel, pin=pin)
                self.assertEqual(coleco_expansion.boundary_route_through_cells(
                    top, {"anchor": ff_bel}), {"anchor$ROUTETHRU"})

    def test_compiler_pin_selects_the_exact_legacy_or_remapped_input(self):
        cases = (
            (coleco_expansion.NEXTPNR_PRE_ROUTETHRU_REMAP,
             "MISTRAL_FF.24.4.56", "C", coleco_expansion.NEXTPNR_ROUTETHRU_REMAP),
            (coleco_expansion.NEXTPNR_PRE_ROUTETHRU_REMAP,
             "MISTRAL_FF.24.4.58", "D", coleco_expansion.NEXTPNR_ROUTETHRU_REMAP),
            (coleco_expansion.NEXTPNR_ROUTETHRU_REMAP,
             "MISTRAL_FF.24.4.56", "F0", coleco_expansion.NEXTPNR_PRE_ROUTETHRU_REMAP),
            (coleco_expansion.NEXTPNR_ROUTETHRU_REMAP,
             "MISTRAL_FF.24.4.58", "F1", coleco_expansion.NEXTPNR_PRE_ROUTETHRU_REMAP),
            (coleco_expansion.NEXTPNR_CARRY_PINS,
             "MISTRAL_FF.24.4.56", "F0", coleco_expansion.NEXTPNR_PRE_ROUTETHRU_REMAP),
            (coleco_expansion.NEXTPNR_CARRY_PINS,
             "MISTRAL_FF.24.4.58", "F1", coleco_expansion.NEXTPNR_PRE_ROUTETHRU_REMAP),
            (coleco_expansion.NEXTPNR_MAIN_BEC82E36,
             "MISTRAL_FF.24.4.56", "F0", coleco_expansion.NEXTPNR_PRE_ROUTETHRU_REMAP),
            (coleco_expansion.NEXTPNR_MAIN_BEC82E36,
             "MISTRAL_FF.24.4.58", "F1", coleco_expansion.NEXTPNR_PRE_ROUTETHRU_REMAP),
        )
        for commit, ff_bel, input_pin, wrong_commit in cases:
            with self.subTest(commit=commit, ff_bel=ff_bel):
                half = 0 if ff_bel.endswith(".56") else 1
                buffer_bel = "MISTRAL_COMB.24.4." + ("54" if half == 0 else "55")
                top = route_through_fixture(ff_bel=ff_bel, buffer_bel=buffer_bel,
                                            pin=input_pin)
                self.assertEqual(coleco_expansion.boundary_route_through_cells(
                    top, {"anchor": ff_bel}, nextpnr_commit=commit), {"anchor$ROUTETHRU"})
                with self.assertRaisesRegex(ValueError, "pin map changed"):
                    coleco_expansion.boundary_route_through_cells(
                        top, {"anchor": ff_bel}, nextpnr_commit=wrong_commit)

    def test_older_snapshot_without_buffer_is_accepted(self):
        top = route_through_fixture()
        del top["cells"]["anchor$ROUTETHRU"]
        top["cells"]["anchor"]["connections"]["DATAIN"] = ["0"]
        bels = {"anchor": "MISTRAL_FF.24.4.56"}
        self.assertEqual(coleco_expansion.boundary_route_through_cells(top, bels), set())
        for q in ([10003], [], None):
            if q is None:
                del top["cells"]["anchor"]["connections"]["Q"]
            else:
                top["cells"]["anchor"]["connections"]["Q"] = q
            self.assertIsNone(coleco_expansion.validate_clock_anchors(top, bels))

    def test_route_through_rejects_malformed_cell_and_connections(self):
        mutations = (
            (("type",), "MISTRAL_ALUT1"),
            (("parameters",), {"LUT": "10"}),
            (("attributes", "NEXTPNR_BEL"), "MISTRAL_COMB.25.4.54"),
            (("attributes", "NEXTPNR_BEL"), "MISTRAL_COMB.24.5.54"),
            (("attributes", "NEXTPNR_BEL"), "MISTRAL_COMB.24.4.55"),
            (("port_directions", "A"), "output"),
            (("connections", "A"), []),
            (("connections", "A"), [True]),
            (("connections", "A"), [10001, 10004]),
            (("connections", "A"), [10002]),
            (("connections", "Q"), [10004]),
            (("connections", "Q"), 10002),
            (("connections", "extra"), [10004]),
            (("attributes", "FES_PINMAP_V1"), "not hex"),
        )
        for path, value in mutations:
            with self.subTest(path=path, value=value):
                top = route_through_fixture()
                target = top["cells"]["anchor$ROUTETHRU"]
                for key in path[:-1]:
                    target = target[key]
                target[path[-1]] = value
                with self.assertRaises(ValueError):
                    coleco_expansion.boundary_route_through_cells(
                        top, {"anchor": "MISTRAL_FF.24.4.56"})

    def test_route_through_rejects_changed_pin_map(self):
        for mapping in (
                {"count": 2, "pins": {"A": [0, "C"], "Q": [0, "COMBOUT"]}},
                {"count": 2, "pins": {"A": [0, "D"], "Q": [0, "COMBOUT"]}},
                {"count": 2, "pins": {"A": [False, "F0"], "Q": [0, "COMBOUT"]}},
                {"count": 2, "pins": {"A": [0, "F0"], "Q": [0, "Q"]}},
                {"count": 3, "pins": {"A": [0, "F0"], "Q": [0, "COMBOUT"]}},
                {"count": 2, "pins": {"A": [0, "F0"], "Q": [0, "COMBOUT"]}, "extra": 1}):
            with self.subTest(mapping=mapping):
                top = route_through_fixture()
                top["cells"]["anchor$ROUTETHRU"]["attributes"]["FES_PINMAP_V1"] = \
                    json.dumps(mapping).encode().hex()
                with self.assertRaisesRegex(ValueError, "pin map"):
                    coleco_expansion.boundary_route_through_cells(
                        top, {"anchor": "MISTRAL_FF.24.4.56"})

    def test_shared_buffer_output_cannot_be_admitted(self):
        for endpoint in ("sink", "driver", "top-input", "top-output"):
            with self.subTest(endpoint=endpoint):
                top = route_through_fixture()
                if endpoint.startswith("top-"):
                    top["ports"] = {"shared": {"direction": endpoint.removeprefix("top-"),
                                                "bits": [10002]}}
                else:
                    top["cells"]["shared"] = {"connections": {
                        "D" if endpoint == "sink" else "Q": [10002]}}
                with self.assertRaisesRegex(ValueError, "not dedicated"):
                    coleco_expansion.boundary_route_through_cells(
                        top, {"anchor": "MISTRAL_FF.24.4.56"})

    def test_clock_anchor_rejects_live_output(self):
        for endpoint in ("cell", "top"):
            with self.subTest(endpoint=endpoint):
                top = route_through_fixture()
                if endpoint == "cell":
                    top["cells"]["consumer"] = {"connections": {"D": [10003]}}
                else:
                    top["ports"] = {"output": {"direction": "output", "bits": [10003]}}
                before = json.loads(json.dumps(top))
                with self.assertRaisesRegex(ValueError, "anchor output"):
                    coleco_expansion.validate_clock_anchors(top, {"anchor": "MISTRAL_FF.24.4.56"})
                self.assertEqual(top, before)

    def test_anchor_validation_retains_pair_and_all_net_records(self):
        top = route_through_fixture()
        top["netnames"]["another_stub_alias"] = {"bits": [10002]}
        top["netnames"]["packed_alias"] = {"bits": [10004, 10002]}
        before = json.loads(json.dumps(top))
        self.assertIsNone(coleco_expansion.validate_clock_anchors(
            top, {"anchor": "MISTRAL_FF.24.4.56"}))
        self.assertEqual(top, before)


class ColecoSgmBuildTest(unittest.TestCase):
    def test_v2_shell_accepts_factory_producer_arguments(self):
        output = Path("/tmp/fes-coleco-packages")
        with patch.object(shell, "build", return_value=output) as build:
            self.assertEqual(shell.main(["--root", str(shell.ROOT),
                                         "--package-output", str(output),
                                         "--identity-version", "2"]), 0)
        self.assertEqual(build.call_args.args, (shell.ROOT, output))
        self.assertEqual(build.call_args.kwargs["identity_version"], 2)

    def test_v2_shell_exposes_canonical_record_call(self):
        with patch.object(shell, "functional_record_fields",
                          side_effect=lambda root, fields, *args, **kwargs: fields), \
             patch.object(shell, "encode_build_record", side_effect=lambda fields: fields):
            record = shell.create_build_record(
                Path(__file__).parents[1], "repository", "a" * 40, {},
                identity_version=2, execution={})
        self.assertEqual(record["recipe"], shell.RECIPE)

    def test_shell_closure_and_router_pin(self):
        self.assertIn("cores/fes-coleco/rtl/coleco_expansion_ram.v", shell.PINNED_INPUTS)
        self.assertIn("cores/fes-coleco/rtl/coleco_audio_mix.v", shell.PINNED_INPUTS)
        self.assertIn("cores/fes-coleco/rtl/coleco_expansion_socket_v2.v", shell.PINNED_INPUTS)
        from scripts import pnr_recipe
        self.assertEqual(shell.TOOLCHAIN_LOCK, "cores/fes-coleco/toolchain.lock")
        self.assertEqual(pnr_recipe.lock_path(shell.ROOT, "fes.coleco"), shell.ROOT / shell.TOOLCHAIN_LOCK)
        lock = tomllib.loads((shell.ROOT / shell.TOOLCHAIN_LOCK).read_text())
        self.assertEqual(lock["tool"]["nextpnr"]["commit"], pnr_recipe.nextpnr_pin("fes.coleco"))
        self.assertEqual(pnr_recipe.nextpnr_pin("fes.coleco"), coleco_expansion.NEXTPNR_MAIN_BEC82E36)
        self.assertEqual(coleco_expansion.ROUTETHROUGH_INPUT_PINS[coleco_expansion.NEXTPNR_MAIN_BEC82E36],
                         ("F0", "F1"))
        with patch.object(shell, "functional_record_fields",
                          side_effect=lambda root, fields, *args, **kwargs: fields), \
             patch.object(shell, "encode_build_record", side_effect=lambda fields: fields):
            record = shell.create_build_record(Path(__file__).parents[1],
                                               "repository", "a" * 40, {}, {})
        self.assertEqual(record["parameters"]["expansion_rect"],
                         coleco_expansion.SOCKET_RECT_V2)
        self.assertEqual(record["parameters"]["seed_order"], "3,4,5,1,2,6,7,8,9,10")
        for source in ("sgm.v", "sgm_control.v", "sgm_ay.v"):
            self.assertIn(f"cores/fes-coleco/expansions/{source}", sgm.INPUTS)
        self.assertIn("toolchain.lock", sgm.INPUTS)

    def test_v2_boundary_has_59_unique_physical_ffs(self):
        bels = coleco_expansion.socket_bels_v2()
        self.assertEqual(len(bels), 59)
        self.assertEqual(len(set(bels.values())), 59)
        self.assertTrue(all(bel.startswith("MISTRAL_FF.24.") and
                            int(bel.split(".")[2]) <= 3 for bel in bels.values()))
        self.assertIn(b'FES_RESERVED_RECT "24 1 28 19"',
                      sgm.cart_qsf(coleco_expansion.shell_qsf("", version=2).encode()))
        self.assertEqual(sgm.CRAM_REGION, (1769, 32, 2806, 1800))
        self.assertIn('FES_RESERVED_RECT "24 1 28 11"',
                      coleco_expansion.shell_qsf(""))
        rtl = (Path(__file__).parents[1] /
               "cores/fes-coleco/rtl/coleco_expansion_socket_v2.v").read_text()
        for name, bel in bels.items():
            raw = coleco_expansion.raw_cell_name(name)
            self.assertIn(f'`COLECO_V2_SOCKET_FF({raw.removeprefix("socket.")}, "{bel}",', rtl)

    def test_clock_coverage_accepts_another_global_lane_when_row_4_is_present(self):
        route = clock_coverage_route_fixture().replace("GCLK.0.36.3", "GCLK.0.36.2")
        top = {
            "cells": {"clock_coverage_ff": {"connections": {"CLK": [2107]}}},
            "netnames": {"system_clock.clocks[0]": {
                "bits": [2107], "attributes": {"ROUTING": route}}},
        }
        coleco_expansion.validate_v2_clock_coverage_route(top)
        broken = route.replace("HCLK.16.4.5.HCLKB.16.4.5", "HCLK.16.4.5.OTHER")
        top["netnames"]["system_clock.clocks[0]"]["attributes"]["ROUTING"] = broken
        with self.assertRaisesRegex(ValueError, "row 4 branch"):
            coleco_expansion.validate_v2_clock_coverage_route(top)

    def test_v2_routed_shell_admits_only_exact_boundary_buffers(self):
        top = route_through_fixture("clock_coverage_ff")
        top["cells"].update({name: {"type": "MISTRAL_FF", "attributes": {"NEXTPNR_BEL": bel}}
                             for name, bel in coleco_expansion.socket_bels_v2().items()})
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "routed.json"
            path.write_text(json.dumps({"modules": {"top": top}}))
            coleco_expansion.validate_routed_shell(path, version=2)
            intruder = json.loads(json.dumps(top["cells"]["clock_coverage_ff$ROUTETHRU"]))
            intruder["connections"]["Q"] = [20002]
            intruder["attributes"]["NEXTPNR_BEL"] = "MISTRAL_COMB.24.5.54"
            top["cells"]["clock_coverage_ff$ROUTETHRU$extra"] = intruder
            path.write_text(json.dumps({"modules": {"top": top}}))
            with self.assertRaisesRegex(ValueError, "reserved socket contains shell cell"):
                coleco_expansion.validate_routed_shell(path, version=2)

    def test_v2_netlist_rejects_wrong_response_placement(self):
        bels = coleco_expansion.socket_bels_v2()
        coverage_bel = "MISTRAL_FF.24.4.56"
        coverage_route = clock_coverage_route_fixture()
        design = {"modules": {"top": {"netnames": {
            "system_clock.pll_outclk": {"bits": [902]},
            "bus_request": {"bits": list(range(500, 531))},
            "plug_request": {"bits": list(range(31))},
            "bus_response": {"bits": list(range(100, 128))},
        }, "cells": {"system_clock.clocks_MISTRAL_CLKBUF_Q": {
            "type": "MISTRAL_CLKBUF", "connections": {"A": [902], "Q": [900]},
        }}}}}
        cells = design["modules"]["top"]["cells"]
        for name, bel in bels.items():
            bit = int(name.rsplit("_", 1)[1])
            cells[coleco_expansion.raw_cell_name(name)] = {
                "type": "MISTRAL_FF", "attributes": {"BEL": bel},
                "connections": {"CLK": [900],
                    "DATAIN": [bit + 500] if name.startswith("plug_addr") else ["0"],
                    "Q": [bit if name.startswith("plug_addr") else bit + 100]},
            }
        cells["socket.clock_coverage_ff"] = {
            "type": "MISTRAL_FF", "attributes": {"BEL": coverage_bel},
            "connections": {"CLK": [900], "DATAIN": ["0"], "Q": [1000]},
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(design))
            coleco_expansion.prepare_shell_netlist(path, version=2)
            reordered = json.loads(json.dumps(design))
            reordered['modules']['top']['cells']['system_clock.clocks_MISTRAL_CLKBUF_Q_1'] = \
                reordered['modules']['top']['cells'].pop('system_clock.clocks_MISTRAL_CLKBUF_Q')
            reordered['modules']['top']['cells']['system_clock.clocks_MISTRAL_CLKBUF_Q'] = {
                'type': 'MISTRAL_CLKBUF', 'connections': {'A': [901], 'Q': [899]}}
            path.write_text(json.dumps(reordered))
            coleco_expansion.prepare_shell_netlist(path, version=2)
            changed = json.loads(path.read_text())["modules"]["top"]["cells"]
            self.assertIn("plug_rdata_ff_27", changed)
            self.assertIn("clock_coverage_ff", changed)
            routed = {name: {"type": "MISTRAL_FF", "attributes": {"NEXTPNR_BEL": bel}}
                      for name, bel in bels.items()}
            routed["clock_coverage_ff"] = {
                "type": "MISTRAL_FF", "attributes": {"NEXTPNR_BEL": coverage_bel},
                "connections": {"CLK": [2107]}}
            routed_design = {"modules": {"top": {"cells": routed, "netnames": {
                "system_clock.clocks[0]": {"bits": [2107],
                                            "attributes": {"ROUTING": coverage_route}},
            }}}}
            path.write_text(json.dumps(routed_design))
            coleco_expansion.validate_routed_shell(path, version=2)
            routed["plug_rdata_ff_27"]["attributes"]["NEXTPNR_BEL"] = "MISTRAL_FF.28.6.2"
            path.write_text(json.dumps(routed_design))
            with self.assertRaisesRegex(ValueError, "plug_rdata_ff_27"):
                coleco_expansion.validate_routed_shell(path, version=2)
            routed["plug_rdata_ff_27"]["attributes"]["NEXTPNR_BEL"] = bels["plug_rdata_ff_27"]
            del routed["clock_coverage_ff"]
            path.write_text(json.dumps(routed_design))
            with self.assertRaisesRegex(ValueError, "clock_coverage_ff"):
                coleco_expansion.validate_routed_shell(path, version=2)
            routed["clock_coverage_ff"] = {
                "type": "MISTRAL_FF", "attributes": {"NEXTPNR_BEL": coverage_bel},
                "connections": {"CLK": [2107]}}
            routed_design["modules"]["top"]["netnames"]["system_clock.clocks[0]"]["attributes"]["ROUTING"] = ""
            path.write_text(json.dumps(routed_design))
            with self.assertRaisesRegex(ValueError, "clock coverage"):
                coleco_expansion.validate_routed_shell(path, version=2)
            routed_design["modules"]["top"]["netnames"]["system_clock.clocks[0]"]["attributes"]["ROUTING"] = coverage_route
            routed["intruding_shell_cell"] = {"type": "MISTRAL_COMB", "attributes": {
                "NEXTPNR_BEL": "MISTRAL_COMB.24.19.0"}}
            path.write_text(json.dumps(routed_design))
            with self.assertRaisesRegex(ValueError, "intruding_shell_cell"):
                coleco_expansion.validate_routed_shell(path, version=2)
            del routed["intruding_shell_cell"]
            for field, value in (("Q", [999]), ("DATAIN", [999])):
                malformed = json.loads(json.dumps(design))
                malformed["modules"]["top"]["cells"]["socket.plug_response_ff_27"]["connections"][field] = value
                path.write_text(json.dumps(malformed))
                with self.subTest(field=field), self.assertRaisesRegex(ValueError, "plug_rdata_ff_27"):
                    coleco_expansion.prepare_shell_netlist(path, version=2)
            malformed = json.loads(json.dumps(design))
            malformed["modules"]["top"]["cells"]["socket.plug_request_ff_30"]["connections"]["DATAIN"] = [999]
            path.write_text(json.dumps(malformed))
            with self.assertRaisesRegex(ValueError, "plug_addr_ff_30"):
                coleco_expansion.prepare_shell_netlist(path, version=2)

    def test_sgm_scaffold_retains_clock_anchor_and_all_routes(self):
        bels = coleco_expansion.socket_bels_v2()
        route = clock_coverage_route_fixture()
        pins = {name: [0, name] for name in ("locked", "outclk", "refclk", "rst")}
        pins.update({f"outclk[{bit}]": [0, f"outclk[{bit}]"] for bit in range(2)})
        pll = {
            "type": "altera_pll",
            "attributes": {"FES_PINMAP_V1": json.dumps({"count": 6, "pins": pins}).encode().hex()},
            "connections": {"locked": [1], "outclk": [2], "refclk": [3]},
            "port_directions": {"outclk": "output"},
        }
        cells = {
            "system_clock.pll": pll,
            "system_clock.clocks_MISTRAL_CLKBUF_Q_1": {
                "type": "MISTRAL_CLKBUF", "connections": {"A": [2], "Q": [2107]}},
            "system_clock.clocks_MISTRAL_CLKBUF_Q": {
                "type": "MISTRAL_CLKBUF", "connections": {"A": [901], "Q": [904]}},
        }
        cells.update({name: {"type": "MISTRAL_FF", "attributes": {"NEXTPNR_BEL": bel}}
                      for name, bel in bels.items()})
        cells["clock_coverage_ff"] = {
            "type": "MISTRAL_FF", "attributes": {"NEXTPNR_BEL": "MISTRAL_FF.24.4.56"},
            "connections": {"CLK": [2107]}}
        design = {"modules": {"top": {"cells": cells, "netnames": {
            "system_clock.pll_outclk_1": {"bits": [901]},
            "system_clock.clocks[1]": {"bits": [904]},
            "system_clock.clocks[0]": {"bits": [2107], "attributes": {"ROUTING": route}},
        }}}}
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary) / "routed.json"
            destination = Path(temporary) / "scaffold.json"
            source.write_text(json.dumps(design))
            sgm.prepare_scaffold(source, destination)
            prepared = json.loads(destination.read_text())["modules"]["top"]
            self.assertEqual(prepared["cells"]["clock_coverage_ff"], cells["clock_coverage_ff"])
            self.assertEqual(prepared["netnames"], design["modules"]["top"]["netnames"])
            paired = route_through_fixture("clock_coverage_ff")
            design["modules"]["top"]["cells"].update(paired["cells"])
            design["modules"]["top"]["netnames"].update(paired["netnames"])
            source.write_text(json.dumps(design))
            sgm.prepare_scaffold(source, destination)
            prepared = json.loads(destination.read_text())["modules"]["top"]
            for name in ("clock_coverage_ff", "clock_coverage_ff$ROUTETHRU"):
                self.assertEqual(prepared["cells"][name], paired["cells"][name])
            self.assertEqual(prepared["netnames"], design["modules"]["top"]["netnames"])
            for name in bels:
                self.assertEqual(prepared["cells"][name], cells[name])

    def test_sgm_contract_rejects_unlisted_external_cram(self):
        changes = {"bits_inside_slot": 8, "bits_outside_slot": 1,
                   "outside_slot_coordinates": [[1, 1]],
                   "outside_slot_coordinates_truncated": False}
        with self.assertRaises(ValueError):
            sgm.enforce_cram_region(changes, Path("cram-diff.json"))
        self.assertEqual(sgm.CRAM_REGION, (1769, 32, 2806, 1800))

    def test_v1_shell_rejected_before_compiler(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            producer = root / "shell"
            producer.mkdir()
            (producer / "manifest.toml").write_bytes(b"manifest")
            (producer / "core.rbf").write_bytes(b"payload")
            package = SimpleNamespace(manifest_bytes=b"manifest", payload_bytes=b"payload",
                fields={"format": 2, "interfaces": [{"id": "fes.expansion.coleco-bus",
                    "major": 1, "minor": 0, "required": False}]})
            with patch.object(sgm, "_require_clean_source", return_value=("repo", "a" * 40)), \
                 patch.object(sgm, "read_package", return_value=package), \
                 patch.object(sgm.shell_recipe, "authenticate_tools") as compiler:
                with self.assertRaisesRegex(ValueError, "bus 2.0"):
                    sgm.build(root, producer, root / "package", 0)
                compiler.assert_not_called()


class NativeSessionDisplayTest(unittest.TestCase):
    def test_native_shell_reads_the_plane_sources(self):
        tools = {"yosys": Path("/bin/true"), "nextpnr-mistral": Path("/bin/true")}
        native, _route = shell.build_commands(
            shell.ROOT, shell.ROOT / shell.NATIVE_VIDEO_OUTPUT_RELATIVE, "a" * 32,
            tools, native_video=True)
        program = native[-1]
        for source in ("fes_display_plane.v", "fes_display_cdc.v", "fes_menu_video.v", "fes_hps_ddr.v"):
            self.assertIn(source, program)
        self.assertIn("-DFES_COLECO_NATIVE_VIDEO_PART_DEV=1", program)
        plain = shell.build_commands(
            shell.ROOT, shell.ROOT / shell.OUTPUT_RELATIVE, "a" * 32, tools)[0][-1]
        self.assertNotIn("fes_display_plane.v", plain)

    def test_picture_rectangle_is_the_native_window(self):
        top = (shell.ROOT / "cores/fes-coleco/rtl/top.v").read_text()
        self.assertIn(".PICTURE_LEFT(16'd384), .PICTURE_TOP(16'd168)", top)
        self.assertIn(".PICTURE_WIDTH(16'd512), .PICTURE_HEIGHT(16'd384)", top)

    def test_native_manifest_requires_the_plane_and_keeps_the_part_optional(self):
        record = b'{"format":1,"parameters":{},"recipe_sha256":"' + b"a" * 64 + b'"}'
        evidence = {
            "rbf": {"size": 100, "sha256": "b" * 64},
            "build_id": "c" * 32,
            "rom_map": {"file": "rom-map.json", "size": 10, "sha256": "d" * 64},
        }
        tools = {"yosys": "fixture", "nextpnr-mistral": "fixture", "mistral": "fixture"}
        native = tomllib.loads(shell.manifest(
            record, evidence, "https://example.invalid/fes.git", "e" * 40, tools,
            native_video=True).decode())
        plain = tomllib.loads(shell.manifest(
            record, evidence, "https://example.invalid/fes.git", "e" * 40, tools).decode())
        self.assertEqual(native["core"]["version"], "1.5.0")
        self.assertEqual(plain["core"]["version"], "1.5.0")
        required = {row["id"]: row["required"] for row in native["interfaces"]}
        self.assertTrue(required["fes.memory.hps-ddr"])
        self.assertTrue(required["fes.video.session-display"])
        self.assertTrue(required["fes.fabric.video.display-plane"])
        self.assertFalse(required["fes.fabric.video.native-pixels"])
        self.assertNotIn("fes.media.blob", required)
        self.assertNotIn("fes.firmware.blob", required)
        plain_ids = {row["id"] for row in plain["interfaces"]}
        self.assertNotIn("fes.video.session-display", plain_ids)

    def test_plane_products_are_three_unsigned_display_products(self):
        # Coleco's Yosys 5391eeb1 predates A_SIGNED/B_SIGNED on MISTRAL_MUL.
        unsigned = {"type": "MISTRAL_MUL9X9", "parameters": {}}
        def graph(cells):
            return {"modules": {"top": {"cells": cells}}}
        fes_de10nano_evidence.plane_dsp_evidence(
            graph({f"display.product{i}": unsigned for i in range(3)}), sign_parameters=False)
        bad = (
            {f"display.product{i}": unsigned for i in range(2)},
            {**{f"display.product{i}": unsigned for i in range(3)}, "machine.extra": unsigned},
            {"machine.product0": unsigned, **{f"display.product{i}": unsigned for i in range(1, 3)}},
            {**{f"display.product{i}": unsigned for i in range(3)},
             "display.product0": {"type": "MISTRAL_MUL9X9", "parameters": {"A_SIGNED": "0"}}},
        )
        for cells in bad:
            with self.assertRaises(BuildError):
                fes_de10nano_evidence.plane_dsp_evidence(graph(cells), sign_parameters=False)
        self.assertEqual(factory.SESSION_DISPLAY_RESOURCES, {"cyclonev_hps_interface_fpga2sdram": 1})

if __name__ == "__main__":
    unittest.main()
