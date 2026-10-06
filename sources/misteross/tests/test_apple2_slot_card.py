"""Apple II slot-card producer contract checks (no compiler run)."""

from __future__ import annotations

import json
import unittest
from pathlib import Path
from types import SimpleNamespace

from scripts import apple2_slots
from scripts import build_apple2_slot_card as card

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
    for socket in apple2_slots.SOCKETS:
        for name, bel in apple2_slots.boundary_bels(socket).items():
            cells[socket.instance + name] = {"type": "MISTRAL_FF", "attributes": {"NEXTPNR_BEL": bel}}
    netnames = {"system_clock.pll_outclk_1": {"bits": [20]}, "system_clock.clocks[1]": {"bits": [21]}}
    return {"modules": {"top": {"cells": cells, "netnames": netnames}}}


class Apple2SlotCardTests(unittest.TestCase):
    def test_probe_rom_is_synchronous_and_the_producer_rejects_async_m10k(self) -> None:
        probe = (ROOT / "cores/fes-apple2/expansions/probe.v").read_text()
        self.assertIn(".CFG_ASYNC_READ(0)", probe)
        self.assertNotIn("CFG_ASYNC_READ(1)", probe)
        self.assertIn(".B1EN(1'b1)", probe)
        self.assertIn(".CLK1(clk)", probe)
        self.assertIn("rom_q", probe)
        producer = (ROOT / "scripts/build_apple2_slot_card.py").read_text()
        self.assertIn("reject_async_m10k_reads", producer)
        self.assertIn("cart.json", producer)
        self.assertIn("cart-routed.json", producer)

    def test_scaffold_exposes_only_the_chosen_slot(self) -> None:
        design = json.loads(card.prepare_scaffold(json.dumps(frozen_shell()).encode(), 5))
        cells = design["modules"]["top"]["cells"]
        self.assertEqual(cells["plug_addr_ff_0"]["attributes"]["NEXTPNR_BEL"], "MISTRAL_FF.24.41.2")
        self.assertEqual(cells["plug_rdata_ff_27"]["attributes"]["NEXTPNR_BEL"], "MISTRAL_FF.24.43.58")
        self.assertNotIn("slot5.plug_request_ff_0", cells)
        self.assertFalse([n for n in cells if n.startswith("slot5.clock_coverage_ff")])
        for other in (2, 4, 7):
            self.assertIn(f"slot{other}.plug_request_ff_31", cells)
            self.assertIn(f"slot{other}.clock_coverage_ff_32", cells)
        pll = cells["system_clock.pll"]
        self.assertEqual(pll["connections"]["outclk[1]"], [20])
        pins = json.loads(bytes.fromhex(pll["attributes"]["FES_PINMAP_V1"]).decode())["pins"]
        self.assertNotIn("outclk[0]", pins)

    def test_scaffold_rejects_moved_boundaries_and_unknown_slots(self) -> None:
        moved = frozen_shell()
        moved["modules"]["top"]["cells"]["slot7.plug_response_ff_3"]["attributes"]["NEXTPNR_BEL"] = \
            "MISTRAL_FF.24.60.2"
        with self.assertRaises(ValueError):
            card.prepare_scaffold(json.dumps(moved).encode(), 2)
        with self.assertRaises(ValueError):
            card.prepare_scaffold(json.dumps(frozen_shell()).encode(), 6)
        with self.assertRaises(ValueError):
            card.card_inputs("videx")

    def test_manifest_is_canonical_with_slot_index(self) -> None:
        package = SimpleNamespace(fields={"build": {"id": "b" * 32}}, package_id="a" * 64,
                                  payload_bytes=b"shell")
        encoded = card.card_manifest(package, 4, b"cart", "c" * 64, "d" * 40)
        manifest = json.loads(encoded)
        self.assertEqual(encoded, json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode())
        self.assertIn(b'"slot":"fes.expansion.apple2-bus","slot_index":4,"slot_major":1,"slot_minor":0',
                      encoded)
        self.assertEqual(manifest["map"], "fes.apple2-bus.slots/1")

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
