"""Closed pixel-domain socket of the Atari ST raster presentation socket."""
from __future__ import annotations

import json
from pathlib import Path

from scripts import coleco_expansion, atari_st_slot

INTERFACE = "fes.fabric.video.raster-rgb888"
MAP = "fes.atari-st-video.socket/1"
LAYOUT = "fes.atari-st-video.parts/1"
REGION = "video"
PLACEMENT = "video 24 41 28 58"
CRAM = (1769, 3442, 2806, 5162)
CLOCK = "pixel_clk"
PREFIX = "video_socket."
REQUEST_BITS = 32
RESPONSE_BITS = 28
RTL = "cores/fes-atari-st/rtl/st_video_socket.sv"


def boundary_bels() -> dict[str, str]:
    def z(index: int) -> int:
        return (index // 2) * 6 + (4 if index % 2 else 2)
    result = {}
    for bit in range(REQUEST_BITS):
        row, index = (41, bit) if bit < 20 else (42, bit - 20)
        result[f"plug_request_ff_{bit}"] = f"MISTRAL_FF.24.{row}.{z(index)}"
    for bit in range(RESPONSE_BITS):
        row, index = (42, bit + 12) if bit < 8 else (43, bit - 8)
        result[f"plug_response_ff_{bit}"] = f"MISTRAL_FF.24.{row}.{z(index)}"
    # Keep both horizontal clock branches present throughout the empty region.
    anchors = ([f"MISTRAL_FF.24.{row}.56" for row in range(44, 59)] +
               [f"MISTRAL_FF.28.{row}.56" for row in range(41, 59)])
    result.update({f"clock_coverage_ff_{i}": bel for i, bel in enumerate(anchors)})
    return result


def shell_qsf(base: str) -> str:
    cpu = atari_st_slot.SOCKETS[0].placement
    if base.count(f'FES_RESERVED_RECT "{cpu}"') != 1 or '"video ' in base:
        raise ValueError("ST presentation requires the unchanged CPU reservation")
    return base.rstrip() + f'\nset_global_assignment -name FES_RESERVED_RECT "{PLACEMENT}"\n'



def _object(value, context: str) -> dict:
    if not isinstance(value, dict):
        raise ValueError(f"malformed video {context}")
    return value


def _bits(value, width: int, context: str, *, wires=False) -> list:
    if not isinstance(value, list) or len(value) != width or any(
            not (type(bit) is int and bit >= 0) and
            not (not wires and type(bit) is str and bit in ("0", "1")) for bit in value):
        raise ValueError(f"video {context} must have {width} valid bits")
    return value


def _net_bits(netnames: dict, name: str, width: int, *, wires=False) -> list:
    alias = _object(netnames.get(name), f"required net alias: {name}")
    return _bits(alias.get("bits"), width, f"net {name}", wires=wires)


def validate_boundary(top: dict, *, routed: bool) -> None:
    top = _object(top, "top module")
    cells = _object(top.get("cells"), "cells")
    netnames = _object(top.get("netnames"), "netnames")
    for name, cell in cells.items():
        if not isinstance(name, str):
            raise ValueError("malformed video shell cell name")
        cell = _object(cell, f"shell cell: {name}")
        _object(cell.get("attributes", {}), f"shell attributes: {name}")
        _object(cell.get("connections", {}), f"shell connections: {name}")
    key = "NEXTPNR_BEL" if routed else "BEL"
    clock = _net_bits(netnames, CLOCK, 1, wires=True)
    # Routed JSON omits unused bus aliases; the pinned FFs remain the physical
    # interface and are the compiler's input-bit lookup source.
    # Synthesis must preserve these exact canonical aliases. A hierarchy alias
    # may refer to an earlier technology-mapping net, so it cannot substitute
    # for the word physically driving the boundary FFs.
    if not routed:
        source = _net_bits(netnames, "video_request", REQUEST_BITS)
        request = _net_bits(netnames, "video_plug_request", REQUEST_BITS, wires=True)
        response = _net_bits(netnames, "video_response", RESPONSE_BITS, wires=True)
    for name, bel in boundary_bels().items():
        cell = _object(cells.get(PREFIX + name), f"required boundary cell: {name}")
        attributes = _object(cell.get("attributes"), f"boundary attributes: {name}")
        connections = _object(cell.get("connections"), f"boundary connections: {name}")
        cell_clock = _bits(connections.get("CLK"), 1, f"boundary CLK: {name}", wires=True)
        datain = _bits(connections.get("DATAIN"), 1, f"boundary DATAIN: {name}")
        if cell.get("type") != "MISTRAL_FF" or attributes.get(key) != bel or \
                cell_clock != clock:
            raise ValueError(f"video boundary placement/clock changed: {name}")
        q = _bits(connections.get("Q"), 1, f"boundary Q: {name}", wires=True)
        if name.startswith("plug_request_ff_"):
            bit = int(name.removeprefix("plug_request_ff_"))
            if not routed and q != [request[bit]]:
                raise ValueError("video source boundary changed")
            if not routed and datain != [source[bit]]:
                raise ValueError("video source boundary input changed")
        elif name.startswith("plug_response_ff_"):
            bit = int(name.removeprefix("plug_response_ff_"))
            if not routed and q != [response[bit]]:
                raise ValueError("video response boundary changed")
            if not routed and datain != ["0"]:
                raise ValueError("vacant video response input changed")
    if routed:
        boundary = {PREFIX + name: bel for name, bel in boundary_bels().items()}
        allowed = set(boundary) | coleco_expansion.boundary_route_through_cells(top, boundary)
        coleco_expansion.validate_clock_anchors(top, {
            name: bel for name, bel in boundary.items() if name.startswith(PREFIX + "clock_coverage_ff_")})
        for name, cell in cells.items():
            cell = _object(cell, f"shell cell: {name}")
            attributes = _object(cell.get("attributes", {}), f"shell attributes: {name}")
            placement = attributes.get(key, "")
            if not isinstance(placement, str):
                raise ValueError(f"malformed video shell placement: {name}")
            pieces = placement.split(".")
            if len(pieces) >= 3 and pieces[1].isdigit() and pieces[2].isdigit() and \
                    24 <= int(pieces[1]) <= 28 and 41 <= int(pieces[2]) <= 58 and name not in allowed:
                raise ValueError(f"video reservation contains shell cell: {name}")


def prepare_scaffold(source: bytes) -> bytes:
    """Expose only video under the compiler's packed-port adapter names."""
    design = json.loads(source)
    modules = _object(_object(design, "scaffold design").get("modules"), "scaffold modules")
    top = _object(modules.get("top"), "scaffold top module")
    validate_boundary(top, routed=True)
    cells = top["cells"]
    if any(name.startswith(("plug_addr_ff_", "plug_rdata_ff_")) for name in cells):
        raise ValueError("frozen ST shell already exposes canonical plug cells")
    # Repair only the split second system PLL output, without moving/removing
    # any of the 119 CPU socket FFs or their route-through buffers.
    pll = _object(cells.get("system_clock.pll"), "system PLL")
    connections = _object(pll.get("connections"), "system PLL connections")
    attributes = _object(pll.get("attributes"), "system PLL attributes")
    directions = _object(pll.get("port_directions"), "system PLL port directions")
    if pll.get("type") != "altera_pll" or set(connections) != {"outclk", "refclk", "locked"}:
        raise ValueError("ST system PLL connection contract changed")
    encoded = attributes.get("FES_PINMAP_V1")
    if not isinstance(encoded, str):
        raise ValueError("ST system PLL frozen pin map is missing")
    mapping = _object(json.loads(bytes.fromhex(encoded).decode()), "system PLL pin map")
    pins = _object(mapping.get("pins"), "system PLL mapped pins")
    aliases = {f"outclk[{bit}]": [0, f"outclk[{bit}]"] for bit in range(2)}
    if type(mapping.get("count")) is not int or mapping["count"] != 6 or len(pins) != 6 or \
            any(pins.get(k) != v for k, v in aliases.items()):
        raise ValueError("ST system PLL frozen pin map changed")
    output1 = _net_bits(top["netnames"], "system_clock.pll_outclk_1", 1, wires=True)
    clock1 = _net_bits(top["netnames"], "system_clock.clocks[1]", 1, wires=True)
    buffers = [c for c in cells.values() if c.get("type") == "MISTRAL_CLKBUF" and
               _object(c.get("connections"), "clock buffer connections").get("Q") == clock1]
    if len(buffers) != 1 or buffers[0]["connections"].get("A") != output1:
        raise ValueError("ST frozen audio PLL net changed")
    pll["connections"]["outclk[1]"] = output1
    directions["outclk[1]"] = "output"
    del mapping["pins"]["outclk[0]"]
    mapping["count"] = len(mapping["pins"])
    pll["attributes"]["FES_PINMAP_V1"] = json.dumps(mapping, sort_keys=True).encode().hex()
    # Keep anchors and their constant routes frozen. Removing only their cells
    # leaves occupied input wires at sites that the cart placer would reuse.
    for name in boundary_bels():
        if name.startswith("clock_coverage_ff_"):
            continue
        original = PREFIX + name
        target = name.replace("plug_request_ff_", "plug_addr_ff_").replace(
            "plug_response_ff_", "plug_rdata_ff_")
        for suffix in ("", "$ROUTETHRU"):
            if original + suffix not in cells:
                continue
            if target + suffix in cells:
                raise ValueError("video boundary alias collision")
            cells[target + suffix] = cells.pop(original + suffix)
    return (json.dumps(design, separators=(",", ":")) + "\n").encode()
