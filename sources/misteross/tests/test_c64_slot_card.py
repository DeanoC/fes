"""Commodore 64 cartridge producer contract checks (no compiler run)."""

from __future__ import annotations

import json
import unittest
from pathlib import Path
from types import SimpleNamespace

from scripts import c64_slots
from scripts import build_c64_slot_card as card

ROOT = Path(__file__).resolve().parents[1]


def frozen_shell() -> dict:
    pinmap = {"count": 6, "pins": {"locked": [0, "locked"], "outclk": [0, "outclk"],
                                    "outclk[0]": [0, "outclk[0]"], "outclk[1]": [0, "outclk[1]"],
                                    "refclk": [0, "refclk"], "rst": [0, "rst"]}}
    cells = {
        "system_clock.pll": {"type": "altera_pll",
                             "connections": {"outclk": [10], "refclk": [11], "locked": [12]},
                             "port_directions": {"outclk": "output", "refclk": "input", "locked": "output"},
                             "attributes": {"FES_PINMAP_V1": json.dumps(pinmap, sort_keys=True).encode().hex()}},
        "system_clock.clocks_MISTRAL_CLKBUF_Q_1": {"type": "MISTRAL_CLKBUF",
                                                   "connections": {"A": [20], "Q": [21]}},
    }
    for socket in c64_slots.SOCKETS:
        for name, bel in c64_slots.boundary_bels(socket).items():
            cells[socket.instance + name] = {"type": "MISTRAL_FF", "attributes": {"NEXTPNR_BEL": bel}}
    netnames = {"system_clock.pll_outclk_1": {"bits": [20]}, "system_clock.clocks[1]": {"bits": [21]}}
    return {"modules": {"top": {"cells": cells, "netnames": netnames}}}


class C64SlotCardTests(unittest.TestCase):
    @staticmethod
    def physical_shell() -> dict:
        design = frozen_shell()
        pll = design["modules"]["top"]["cells"]["system_clock.pll"]
        pll["connections"]["outclk[1]"] = [20]
        pll["port_directions"]["outclk[1]"] = "output"
        mapping = {"count": 5, "pins": {"locked": [0, "locked"], "outclk": [0, "C6"],
                    "outclk[1]": [0, "C7"], "refclk": [0, "refclk"], "rst": [0, "rst"]}}
        pll["attributes"]["FES_PINMAP_V1"] = json.dumps(mapping).encode().hex()
        return design

    def test_scaffold_preserves_physical_pll_outputs(self) -> None:
        source = self.physical_shell()
        result = json.loads(card.prepare_scaffold(json.dumps(source).encode(), 2))
        self.assertEqual(result["modules"]["top"]["cells"]["system_clock.pll"],
                         source["modules"]["top"]["cells"]["system_clock.pll"])
        self.assertIn("slot1.plug_request_ff_0", result["modules"]["top"]["cells"])

    def test_physical_pll_contract_fails_closed(self) -> None:
        for mutation in ("audio_net", "pin", "count", "extra_pin", "direction", "extra_port"):
            with self.subTest(mutation=mutation):
                source = self.physical_shell()
                pll = source["modules"]["top"]["cells"]["system_clock.pll"]
                mapping = json.loads(bytes.fromhex(pll["attributes"]["FES_PINMAP_V1"]))
                if mutation == "audio_net":
                    pll["connections"]["outclk[1]"] = [99]
                elif mutation == "pin":
                    mapping["pins"]["outclk[1]"] = [0, "C6"]
                elif mutation == "count":
                    mapping["count"] = 6
                elif mutation == "extra_pin":
                    mapping["pins"]["outclk[0]"] = [0, "C6"]
                elif mutation == "direction":
                    pll["port_directions"]["outclk[1]"] = "input"
                else:
                    pll["connections"]["outclk[2]"] = [99]
                pll["attributes"]["FES_PINMAP_V1"] = json.dumps(mapping).encode().hex()
                with self.assertRaises(ValueError):
                    card.prepare_scaffold(json.dumps(source).encode(), 1)

    def test_probe_mode_follows_the_physical_socket(self) -> None:
        for slot, mode in ((1, 0), (2, 1)):
            command = card.card_synthesis_script("probe", slot, Path("build/card"))
            self.assertIn(f"chparam -set MODE {mode} cart;", command)
            self.assertIn("write_json build/card/cart.json", command)
        with self.assertRaises(ValueError):
            card.card_synthesis_script("probe", 3, Path("build/card"))
        with self.assertRaises(ValueError):
            card.card_synthesis_script("unknown", 1, Path("build/card"))

    def test_scaffold_exposes_only_the_chosen_slot(self) -> None:
        design = json.loads(card.prepare_scaffold(json.dumps(frozen_shell()).encode(), 1))
        cells = design["modules"]["top"]["cells"]
        self.assertEqual(cells["plug_addr_ff_0"]["attributes"]["NEXTPNR_BEL"], "MISTRAL_FF.24.1.2")
        self.assertEqual(cells["plug_rdata_ff_27"]["attributes"]["NEXTPNR_BEL"], "MISTRAL_FF.24.3.58")
        self.assertNotIn("slot1.plug_request_ff_0", cells)
        self.assertFalse([n for n in cells if n.startswith("slot1.clock_coverage_ff")])
        self.assertIn("slot2.plug_request_ff_31", cells)
        self.assertTrue([n for n in cells if n.startswith("slot2.clock_coverage_ff")])
        pll = cells["system_clock.pll"]
        self.assertEqual(pll["connections"]["outclk[1]"], [20])
        pins = json.loads(bytes.fromhex(pll["attributes"]["FES_PINMAP_V1"]).decode())["pins"]
        self.assertNotIn("outclk[0]", pins)

    def test_scaffold_rejects_moved_boundaries_and_unknown_slots(self) -> None:
        moved = frozen_shell()
        moved["modules"]["top"]["cells"]["slot2.plug_response_ff_3"]["attributes"]["NEXTPNR_BEL"] = \
            "MISTRAL_FF.24.60.2"
        with self.assertRaises(ValueError):
            card.prepare_scaffold(json.dumps(moved).encode(), 1)
        with self.assertRaises(ValueError):
            card.prepare_scaffold(json.dumps(frozen_shell()).encode(), 6)
        with self.assertRaises(ValueError):
            card.card_inputs("videx")

    def test_manifest_is_canonical_with_slot_index(self) -> None:
        package = SimpleNamespace(fields={"build": {"id": "b" * 32}}, package_id="a" * 64,
                                  payload_bytes=b"shell")
        encoded = card.card_manifest(package, 2, b"cart", "c" * 64, "d" * 40)
        manifest = json.loads(encoded)
        self.assertEqual(encoded, json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode())
        self.assertIn(b'"slot":"fes.expansion.c64-bus","slot_index":2,"slot_major":1,"slot_minor":0',
                      encoded)
        self.assertEqual(manifest["map"], "fes.c64-bus.sockets/1")

    def test_clock_constraints_and_timing_gate(self) -> None:
        text = card.cart_clock_constraints(ROOT).decode()
        self.assertIn("[get_nets {FPGA_CLK1_50}]", text)
        self.assertNotIn("get_ports", text)
        for name in card.REQUIRED_CLOCKS_MHZ:
            self.assertIn(f"[get_nets {{{name}}}]", text)
        good = {"fmax": {name: {"constraint": mhz, "achieved": mhz + 1}
                         for name, mhz in card.REQUIRED_CLOCKS_MHZ.items()}}
        good["timing_summary"] = {"final_analogue_model": True, "clocks": {
            name: {"setup_wns_ns": 0.0, "hold_wns_ns": 0.1} for name in card.REQUIRED_CLOCKS_MHZ}}
        self.assertEqual(set(card.validate_cart_timing(good)), set(card.REQUIRED_CLOCKS_MHZ))
        slow = json.loads(json.dumps(good))
        slow["fmax"]["system_clock.clocks[0]"]["achieved"] = 50.0
        with self.assertRaises(Exception):
            card.validate_cart_timing(slow)
        for bad_value in (-0.001, float("nan"), float("inf"), True, None):
            for key in ("setup_wns_ns", "hold_wns_ns"):
                bad = json.loads(json.dumps(good))
                bad["timing_summary"]["clocks"][card.SLOT_CLOCK][key] = bad_value
                with self.assertRaises(ValueError):
                    card.validate_cart_timing(bad)
        for summary in ({}, {"final_analogue_model": False, "clocks": {}}, [],
                        {"final_analogue_model": True, "clocks": {}}):
            bad = dict(good, timing_summary=summary)
            with self.assertRaises(ValueError):
                card.validate_cart_timing(bad)

    def test_clock_guard_rejects_card_pins_off_the_socket_clock(self) -> None:
        def routed(clk2):
            ram = {"type": "MISTRAL_M10K", "connections": {"CLK1": [5], "CLK2": clk2}}
            ff = {"type": "MISTRAL_FF", "connections": {"CLK": [5]}}
            return {"modules": {"top": {"netnames": {card.SLOT_CLOCK: {"bits": [5]}},
                                        "cells": {"fes_cart$ram": ram, "fes_cart$ff": ff,
                                                  "machine.ff": {"type": "MISTRAL_FF",
                                                                 "connections": {"CLK": [9]}}}}}}
        self.assertEqual(card.validate_cart_clocks(routed([5])), 3)
        self.assertEqual(card.validate_cart_clocks(routed([])), 2)
        with self.assertRaisesRegex(ValueError, "fes_cart\\$ram pin CLK2"):
            card.validate_cart_clocks(routed([117]))
        with self.assertRaises(ValueError):
            card.validate_cart_clocks(routed([117]), allow_combinational=True)

    def test_combinational_rom_probe_requires_actual_card_logic(self) -> None:
        design = {"modules": {"top": {"netnames": {card.SLOT_CLOCK: {"bits": [5]}},
                                      "cells": {"fes_cart$rom": {"type": "MISTRAL_ALUT2", "connections": {}}}}}}
        self.assertEqual(card.validate_cart_clocks(design, allow_combinational=True), 0)
        with self.assertRaises(ValueError):
            card.validate_cart_clocks(design)
        design["modules"]["top"]["cells"] = {}
        with self.assertRaises(ValueError):
            card.validate_cart_clocks(design, allow_combinational=True)

    def test_response_drivers_preserve_constants_aliases_and_truth_table(self) -> None:
        inputs = ["0", "1", 3, 3] * 7
        pads = list(range(10, 38))
        cells = {f"ob_{i}": {"type": "MISTRAL_OB", "connections": {"I": [value], "PAD": [pads[i]]}}
                 for i, value in enumerate(inputs)}
        source = {"modules": {"cart": {"ports": {"plug_rdata": {"bits": pads},
                                                 "plug_addr": {"bits": [3]}}, "cells": cells}}}
        result = json.loads(card.materialize_response_drivers(json.dumps(source).encode()))
        cells = result["modules"]["cart"]["cells"]
        outputs = set()
        for i, value in enumerate(inputs):
            lut = cells[f"fes_response_driver_{i}"]
            self.assertEqual(lut["connections"]["A"], [value])
            self.assertEqual(lut["connections"]["Q"], cells[f"ob_{i}"]["connections"]["I"])
            outputs.update(lut["connections"]["Q"])
            for bit in (0, 1):
                self.assertEqual((int(lut["parameters"]["LUT"], 2) >> bit) & 1, bit)
        self.assertEqual(len(outputs), 28)
        source["modules"]["cart"]["cells"].pop("ob_0")
        with self.assertRaises(ValueError):
            card.materialize_response_drivers(json.dumps(source).encode())


if __name__ == "__main__":
    unittest.main()
