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



def validate_boundary(top: dict, *, routed: bool) -> None:
    key = "NEXTPNR_BEL" if routed else "BEL"
    clock = top["netnames"][CLOCK]["bits"]
    # Routed JSON omits unused bus aliases; the pinned FFs remain the physical
    # interface and are the compiler's input-bit lookup source.
    request = ([top["cells"][PREFIX + f"plug_request_ff_{i}"]["connections"]["Q"][0]
                for i in range(REQUEST_BITS)] if routed else
               top["netnames"]["video_plug_request"]["bits"])
    response = ([top["cells"][PREFIX + f"plug_response_ff_{i}"]["connections"]["Q"][0]
                 for i in range(RESPONSE_BITS)] if routed else
                top["netnames"]["video_response"]["bits"])
    if len(clock) != 1 or len(request) != REQUEST_BITS or len(response) != RESPONSE_BITS:
        raise ValueError("video boundary clock or width changed")
    for name, bel in boundary_bels().items():
        cell = top["cells"].get(PREFIX + name)
        if not isinstance(cell, dict) or cell.get("type") != "MISTRAL_FF" or \
                cell.get("attributes", {}).get(key) != bel or cell["connections"].get("CLK") != clock:
            raise ValueError(f"video boundary placement/clock changed: {name}")
        if name.startswith("plug_request_ff_"):
            bit = int(name.removeprefix("plug_request_ff_"))
            if cell["connections"].get("Q") != [request[bit]]:
                raise ValueError("video source boundary changed")
            if not routed and cell["connections"].get("DATAIN") != [top["netnames"]["video_request"]["bits"][bit]]:
                raise ValueError("video source boundary input changed")
        elif name.startswith("plug_response_ff_"):
            bit = int(name.removeprefix("plug_response_ff_"))
            if cell["connections"].get("Q") != [response[bit]]:
                raise ValueError("video response boundary changed")
            if not routed and cell["connections"].get("DATAIN") != ["0"]:
                raise ValueError("vacant video response input changed")
    if routed:
        boundary = {PREFIX + name: bel for name, bel in boundary_bels().items()}
        allowed = set(boundary) | coleco_expansion.boundary_route_through_cells(top, boundary)
        coleco_expansion.validate_clock_anchors(top, {
            name: bel for name, bel in boundary.items() if name.startswith(PREFIX + "clock_coverage_ff_")})
        for name, cell in top["cells"].items():
            pieces = cell.get("attributes", {}).get(key, "").split(".")
            if len(pieces) >= 3 and pieces[1].isdigit() and pieces[2].isdigit() and \
                    24 <= int(pieces[1]) <= 28 and 41 <= int(pieces[2]) <= 58 and name not in allowed:
                raise ValueError(f"video reservation contains shell cell: {name}")


def prepare_scaffold(source: bytes) -> bytes:
    """Expose only video under the compiler's packed-port adapter names."""
    design = json.loads(source)
    top = design["modules"]["top"]
    validate_boundary(top, routed=True)
    cells = top["cells"]
    if any(name.startswith(("plug_addr_ff_", "plug_rdata_ff_")) for name in cells):
        raise ValueError("frozen ST shell already exposes canonical plug cells")
    # Repair only the split second system PLL output, without moving/removing
    # any of the 119 CPU socket FFs or their route-through buffers.
    pll = cells["system_clock.pll"]
    if pll["type"] != "altera_pll" or set(pll["connections"]) != {"outclk", "refclk", "locked"}:
        raise ValueError("ST system PLL connection contract changed")
    mapping = json.loads(bytes.fromhex(pll["attributes"]["FES_PINMAP_V1"]).decode())
    aliases = {f"outclk[{bit}]": [0, f"outclk[{bit}]"] for bit in range(2)}
    if mapping.get("count") != 6 or any(mapping["pins"].get(k) != v for k, v in aliases.items()):
        raise ValueError("ST system PLL frozen pin map changed")
    output1 = top["netnames"].get("system_clock.pll_outclk_1", {}).get("bits")
    clock1 = top["netnames"].get("system_clock.clocks[1]", {}).get("bits")
    buffers = [c for c in cells.values() if c.get("type") == "MISTRAL_CLKBUF" and
               c.get("connections", {}).get("Q") == clock1]
    if not isinstance(output1, list) or len(output1) != 1 or len(buffers) != 1 or \
            buffers[0]["connections"].get("A") != output1:
        raise ValueError("ST frozen audio PLL net changed")
    pll["connections"]["outclk[1]"] = output1
    pll["port_directions"]["outclk[1]"] = "output"
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
