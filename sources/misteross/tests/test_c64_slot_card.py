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
        for name in card.REQUIRED_CLOCKS_MHZ:
            self.assertIn(f"[get_nets {{{name}}}]", text)
        good = {"fmax": {name: {"constraint": mhz, "achieved": mhz + 1}
                         for name, mhz in card.REQUIRED_CLOCKS_MHZ.items()}}
        self.assertEqual(set(card.validate_cart_timing(good)), set(card.REQUIRED_CLOCKS_MHZ))
        slow = json.loads(json.dumps(good))
        slow["fmax"]["system_clock.clocks[0]"]["achieved"] = 50.0
        with self.assertRaises(Exception):
            card.validate_cart_timing(slow)

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


if __name__ == "__main__":
    unittest.main()
