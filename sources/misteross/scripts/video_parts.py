"""Closed pixel-domain socket of the Coleco presentation development shell."""
from __future__ import annotations

import json
from pathlib import Path

INTERFACE = "fes.fabric.video.raster-rgb888"
MAP = "fes.coleco-video.socket/1"
LAYOUT = "fes.coleco-video.parts/1"
REGION = "video"
PLACEMENT = "video 24 23 28 38"
CRAM = (1769, 1800, 2806, 3442)
CLOCK = "pixel_clk"
PREFIX = "video_socket."
REQUEST_BITS = 32
RESPONSE_BITS = 28
RTL = "cores/fes-coleco/rtl/coleco_video_socket.v"


def boundary_bels() -> dict[str, str]:
    def z(index: int) -> int:
        return (index // 2) * 6 + (4 if index % 2 else 2)
    result = {}
    for bit in range(REQUEST_BITS):
        row, index = (23, bit) if bit < 20 else (24, bit - 20)
        result[f"plug_request_ff_{bit}"] = f"MISTRAL_FF.24.{row}.{z(index)}"
    for bit in range(RESPONSE_BITS):
        row, index = (24, bit + 12) if bit < 8 else (25, bit - 8)
        result[f"plug_response_ff_{bit}"] = f"MISTRAL_FF.24.{row}.{z(index)}"
    # Keep both horizontal clock branches present throughout the empty region.
    anchors = ([f"MISTRAL_FF.24.{row}.56" for row in range(26, 39)] +
               [f"MISTRAL_FF.28.{row}.56" for row in range(23, 39)])
    result.update({f"clock_coverage_ff_{i}": bel for i, bel in enumerate(anchors)})
    return result


def shell_qsf(base: str) -> str:
    if base.count('FES_RESERVED_RECT "24 1 28 19"') != 1 or '"video ' in base:
        raise ValueError("presentation shell requires the unchanged Coleco v2 CPU reservation")
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
        allowed = {PREFIX + name for name in boundary_bels()}
        for name, cell in top["cells"].items():
            pieces = cell.get("attributes", {}).get(key, "").split(".")
            if len(pieces) >= 3 and pieces[1].isdigit() and pieces[2].isdigit() and \
                    24 <= int(pieces[1]) <= 28 and 23 <= int(pieces[2]) <= 38 and name not in allowed:
                raise ValueError(f"video reservation contains shell cell: {name}")


def prepare_scaffold(source: bytes) -> bytes:
    """Expose only video under the compiler's packed-port adapter names."""
    design = json.loads(source)
    top = design["modules"]["top"]
    validate_boundary(top, routed=True)
    cells = top["cells"]
    for name in list(cells):
        if name.startswith(("plug_addr_ff_", "plug_rdata_ff_")):
            target = "cpu_" + name
            if target in cells:
                raise ValueError("CPU boundary alias collision")
            cells[target] = cells.pop(name)
    for name in boundary_bels():
        original = PREFIX + name
        if name.startswith("clock_coverage_ff_"):
            del cells[original]
            continue
        target = name.replace("plug_request_ff_", "plug_addr_ff_").replace(
            "plug_response_ff_", "plug_rdata_ff_")
        if target in cells:
            raise ValueError("video boundary alias collision")
        cells[target] = cells.pop(original)
    return (json.dumps(design, separators=(",", ":")) + "\n").encode()
