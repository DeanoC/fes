"""Spectrum reservation admits only dedicated physical FF companions."""
import copy
import json
import unittest

from scripts import build_fes_spectrum_oss as producer, spectrum_slots
from scripts.fes_build_common import BuildError


def shell_fixture():
    cells = {}
    for socket in spectrum_slots.SOCKETS:
        for local, bel in spectrum_slots.boundary_bels(socket).items():
            i = len(cells)
            cells[socket.instance + local] = {
                "type": "MISTRAL_FF", "attributes": {"NEXTPNR_BEL": bel},
                "connections": {"CLK": [1], "DATAIN": [1000 + i], "Q": [2000 + i]},
                "port_directions": {"CLK": "input", "DATAIN": "input", "Q": "output"}}
    return {"modules": {"top": {"cells": cells, "ports": {"source": {
        "direction": "input", "bits": list(range(1000, 1000 + len(cells)))}}}}}


def add_buffer(design, name, bel, half, bit):
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


class SpectrumSocketRouteThroughTests(unittest.TestCase):
    def test_old_and_both_physical_halves_and_clock_anchor(self):
        design = shell_fixture()
        self.assertEqual(producer.validate_routed_shell(design)["boundary_route_through_cells"], 0)
        add_buffer(design, "slot1.plug_request_ff_0", "MISTRAL_COMB.24.1.0", 0, 4000)
        add_buffer(design, "slot1.plug_request_ff_1", "MISTRAL_COMB.24.1.1", 1, 4001)
        add_buffer(design, "slot4.clock_coverage_ff_32", "MISTRAL_MCOMB.28.78.54", 0, 4002)
        result = producer.validate_routed_shell(design)
        self.assertEqual(result["pinned_boundary_cells"], 372)
        self.assertEqual(result["boundary_route_through_cells"], 3)

    def test_rejects_damaged_or_name_only_companions(self):
        design = shell_fixture()
        name = "slot4.clock_coverage_ff_32"
        add_buffer(design, name, "MISTRAL_MCOMB.28.78.54", 0, 4000)
        buffer_name = name + "$ROUTETHRU"
        for field, value in [("type", "MISTRAL_ALUT2"), ("parameters", {"LUT": "01"}),
                             ("port_directions", {"A": "input", "Q": "input"}),
                             ("connections", {"A": [1000], "Q": [4000, 4001]}),
                             ("connections", {"A": [True], "Q": [4000]})]:
            with self.subTest(field=field, value=value):
                bad = copy.deepcopy(design)
                bad["modules"]["top"]["cells"][buffer_name][field] = value
                with self.assertRaises(BuildError): producer.validate_routed_shell(bad)
        for field, value in [("NEXTPNR_BEL", "MISTRAL_MCOMB.28.78.55"),
                             ("NEXTPNR_BEL", "MISTRAL_MCOMB.28.77.54"),
                             ("FES_PINMAP_V1", "bad"),
                             ("FES_PINMAP_V1", json.dumps({"count": 2, "pins": {
                                 "A": [0, "D"], "Q": [0, "COMBOUT"]}}).encode().hex())]:
            with self.subTest(field=field, value=value):
                bad = copy.deepcopy(design)
                bad["modules"]["top"]["cells"][buffer_name]["attributes"][field] = value
                with self.assertRaises(BuildError): producer.validate_routed_shell(bad)
        bad = copy.deepcopy(design)
        cells = bad["modules"]["top"]["cells"]
        cells[buffer_name + "_fake"] = cells.pop(buffer_name)
        with self.assertRaises(BuildError): producer.validate_routed_shell(bad)

    def test_rejects_extra_fanout_top_port_and_anchor_use(self):
        design = shell_fixture()
        add_buffer(design, "slot4.clock_coverage_ff_32", "MISTRAL_MCOMB.28.78.54", 0, 4000)
        for bit in (4000, design["modules"]["top"]["cells"]["slot4.clock_coverage_ff_32"]["connections"]["Q"][0]):
            for top_port in (False, True):
                with self.subTest(bit=bit, top_port=top_port):
                    bad = copy.deepcopy(design); top = bad["modules"]["top"]
                    if top_port:
                        top["ports"]["leaked"] = {"direction": "output", "bits": [bit]}
                    else:
                        top["cells"]["extra"] = {"type": "MISTRAL_BUF", "connections": {"A": [bit]}}
                    with self.assertRaises(BuildError): producer.validate_routed_shell(bad)

    def test_unrelated_inside_cell_and_changed_ff_are_rejected(self):
        design = shell_fixture(); cells = design["modules"]["top"]["cells"]
        cells["shell$ROUTETHRU"] = {"type": "MISTRAL_BUF", "attributes": {
            "NEXTPNR_BEL": "MISTRAL_MCOMB.25.65.0"}, "connections": {}}
        with self.assertRaisesRegex(BuildError, "inside the slot 4 socket"):
            producer.validate_routed_shell(design)
        del cells["shell$ROUTETHRU"]
        cells["slot1.plug_request_ff_0"]["attributes"]["NEXTPNR_BEL"] = "MISTRAL_FF.24.2.2"
        with self.assertRaisesRegex(BuildError, "slot boundary cell"):
            producer.validate_routed_shell(design)

    def test_helper_is_in_functional_source_closure(self):
        self.assertIn("scripts/coleco_expansion.py", producer.PINNED_INPUTS)

    def test_rejects_orphan_and_multiple_input_drivers(self):
        design = shell_fixture()
        name = "slot4.clock_coverage_ff_32"
        add_buffer(design, name, "MISTRAL_MCOMB.28.78.54", 0, 4000)
        for mutation in ("missing_buffer", "missing_source", "second_source", "direct_orphan"):
            with self.subTest(mutation=mutation):
                bad = copy.deepcopy(design); top = bad["modules"]["top"]
                if mutation == "missing_buffer":
                    del top["cells"][name + "$ROUTETHRU"]
                elif mutation == "missing_source":
                    top["cells"][name + "$ROUTETHRU"]["connections"]["A"] = [99999]
                elif mutation == "second_source":
                    top["ports"]["second"] = {"direction": "input", "bits":
                        top["cells"][name + "$ROUTETHRU"]["connections"]["A"]}
                else:
                    top["cells"]["slot1.plug_request_ff_0"]["connections"]["DATAIN"] = [99999]
                with self.assertRaisesRegex(BuildError, "no unique driver"):
                    producer.validate_routed_shell(bad)

    def test_legacy_literal_and_disconnected_unused_anchor(self):
        design = shell_fixture(); cells = design["modules"]["top"]["cells"]
        cells["slot1.plug_response_ff_0"]["connections"]["DATAIN"] = ["0"]
        cells["slot4.clock_coverage_ff_32"]["connections"]["DATAIN"] = []
        producer.validate_routed_shell(design)


if __name__ == "__main__":
    unittest.main()
