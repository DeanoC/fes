"""The routed HPS DDR layout check resolves only explicit constants."""
import json
import unittest
from pathlib import Path

from scripts import fes_de10nano_evidence as evidence
from scripts.fes_build_common import BuildError

ROOT = Path(__file__).resolve().parents[1]
LAYOUT = evidence.hps_ddr_layout(ROOT)


def _bits(value, width):
    return [str((value >> index) & 1) for index in range(width)]


def _pinmap(states):
    pins = {name: [state, name] for name, state in states.items()}
    return json.dumps({"count": len(pins), "pins": pins}).encode().hex()


def _routed(*, style, state_override=None, drive_first=False, pinmap=True):
    """Build a minimal routed graph whose fpga2sdram cfg bits use one style."""
    cells, connections, states = {}, {}, {}
    net = 100
    zero, one = 1, 2
    if style == "const":
        for name, bit in (("gnd", zero), ("vcc", one)):
            cells[name] = {"type": "MISTRAL_CONST", "connections": {"Q": [bit]},
                           "port_directions": {"Q": "output"},
                           "parameters": {"LUT": format(int(name == "vcc"), "032b")}}
    for port, (_, width) in evidence.HPS_DDR_CFG_PORTS.items():
        values = _bits(LAYOUT[port], width)
        if style == "const":
            connections[port] = [one if bit == "1" else zero for bit in values]
            continue
        connections[port] = list(range(net, net + width))
        net += width
        for index, bit in enumerate(values):
            states[f"{port}[{index}]"] = 2 if bit == "1" else 1
    if state_override is not None:
        states["cfg_port_width[0]"] = state_override
    if drive_first:
        cells["lut"] = {"type": "MISTRAL_ALUT4",
                        "connections": {"Q": [connections["cfg_port_width"][0]]},
                        "port_directions": {"Q": "output"}}
    sdram = {"type": "cyclonev_hps_interface_fpga2sdram", "connections": connections,
             "port_directions": {port: "input" for port in connections}}
    if pinmap and style == "pinmap":
        sdram["attributes"] = {"FES_PINMAP_V1": _pinmap(states)}
    cells["memory.f2sdram"] = sdram
    return {"modules": {"top": {"cells": cells, "ports": {}}}}


def _check(graph):
    return evidence.hps_ddr_layout_evidence(
        evidence.layout_graph_with_constants(graph), "routed", ROOT, idle=False)


class HpsDdrLayoutConstantTests(unittest.TestCase):
    def test_mistral_const_cells_resolve_the_layout(self):
        self.assertEqual(_check(_routed(style="const"))["cfg"]["cfg_port_width"],
                         f"0x{LAYOUT['cfg_port_width']:x}")

    def test_undriven_bits_resolve_from_pin_map_ties(self):
        result = _check(_routed(style="pinmap"))
        self.assertEqual(result["cfg"], {port: f"0x{value:x}" for port, value in LAYOUT.items()})

    def test_a_driven_net_does_not_take_its_pin_map_state(self):
        with self.assertRaisesRegex(BuildError, "cfg_port_width must be a 12-bit constant"):
            _check(_routed(style="pinmap", drive_first=True))

    def test_signal_or_inverted_pin_states_stay_nonconstant(self):
        for state in (0, 3):
            with self.subTest(state=state), self.assertRaisesRegex(
                    BuildError, "cfg_port_width must be a 12-bit constant"):
                _check(_routed(style="pinmap", state_override=state))

    def test_undriven_bits_without_a_pin_map_fail(self):
        with self.assertRaisesRegex(BuildError, "must be a 12-bit constant"):
            _check(_routed(style="pinmap", pinmap=False))

    def test_a_tie_to_the_wrong_value_fails_the_layout(self):
        flipped = 1 if LAYOUT["cfg_port_width"] & 1 else 2
        with self.assertRaisesRegex(BuildError, "cfg_port_width is 0x"):
            _check(_routed(style="pinmap", state_override=flipped))

    def test_a_malformed_pin_map_is_refused(self):
        graph = _routed(style="pinmap")
        graph["modules"]["top"]["cells"]["memory.f2sdram"]["attributes"]["FES_PINMAP_V1"] = "zz"
        with self.assertRaisesRegex(BuildError, "not a valid pin map"):
            evidence.layout_graph_with_constants(graph)


if __name__ == "__main__":
    unittest.main()
