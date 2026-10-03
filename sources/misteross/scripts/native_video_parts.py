"""Closed native-pixel socket of the Coleco development shell.

This is a separate physical policy from the small HDMI-raster socket.  The
locked Mistral SX120F table maps tile columns 2 and 44 to CRAM x=124 and
x=3906.  Columns 5, 14, 26 and 38 contain 55 M10Ks in rows 23..38;
their configuration offsets (x=0..258, y=0..85) fit this fence.  The lower
edge stays at the CPU socket's exclusive y=1800 edge.  These bounds permit
routing; only a fresh route and the producer's whole-CRAM diff prove fit.
"""
from __future__ import annotations

import json

INTERFACE = "fes.fabric.video.native-pixels"
MAP = "fes.coleco-native-video.socket/1"
LAYOUT = "fes.coleco-native-video.parts/1"
REGION = "video"
PLACEMENT = "video 5 23 38 38"
CRAM = (124, 1800, 3906, 3442)
CLOCK = "pixel_clk"
PREFIX = "video_socket."
REQUEST_BITS = 32
RESPONSE_BITS = 28
RTL = "cores/fes-coleco/rtl/coleco_native_video_socket.v"


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
    # Every selected column is a legal LAB/MLAB across all reserved rows.
    # Keep clock branches on both sides of each of the three full RAM columns;
    # col24 rows23..25 already contain the densely packed boundary FF banks.
    anchors = [f"MISTRAL_FF.{column}.{row}.56"
               for column in (10, 18, 24, 28, 35, 37)
               for row in range(23, 39)
               if column != 24 or row >= 26]
    result.update({f"clock_coverage_ff_{i}": bel for i, bel in enumerate(anchors)})
    return result


def shell_qsf(base: str) -> str:
    if base.count('FES_RESERVED_RECT "24 1 28 19"') != 1 or '"video ' in base:
        raise ValueError("native video shell requires the unchanged Coleco v2 CPU reservation")
    return base.rstrip() + f'\nset_global_assignment -name FES_RESERVED_RECT "{PLACEMENT}"\n'


def _bus(top: dict, name: str, width: int) -> list:
    bits = top.get("netnames", {}).get(name, {}).get("bits")
    if not isinstance(bits, list) or len(bits) != width:
        raise ValueError(f"native video boundary clock or width changed: {name}")
    return bits


def validate_boundary(top: dict, *, routed: bool) -> None:
    key = "NEXTPNR_BEL" if routed else "BEL"
    clock = _bus(top, CLOCK, 1)
    cells = top.get("cells", {})
    for name, bel in boundary_bels().items():
        cell = cells.get(PREFIX + name)
        if not isinstance(cell, dict) or cell.get("type") != "MISTRAL_FF" or \
                cell.get("attributes", {}).get(key) != bel or \
                cell.get("connections", {}).get("CLK") != clock:
            raise ValueError(f"native video boundary placement/clock changed: {name}")

    # nextpnr may remove unused aggregate bus aliases.  Its frozen adapter
    # identifies each packed input/output bit by the corresponding pinned FF Q.
    if routed:
        request, response = [], []
        for kind, width, bits in (("request", REQUEST_BITS, request),
                                  ("response", RESPONSE_BITS, response)):
            for bit in range(width):
                q = cells[PREFIX + f"plug_{kind}_ff_{bit}"]["connections"].get("Q")
                if not isinstance(q, list) or len(q) != 1 or type(q[0]) is not int:
                    raise ValueError(f"native video {kind} boundary Q changed")
                bits.append(q[0])
            alias = "video_plug_request" if kind == "request" else "video_response"
            if alias in top.get("netnames", {}) and _bus(top, alias, width) != bits:
                raise ValueError(f"native video {kind} boundary changed")
        if len(set(request + response)) != REQUEST_BITS + RESPONSE_BITS:
            raise ValueError("native video boundary Q aliases changed")
    else:
        source = _bus(top, "video_request", REQUEST_BITS)
        request = _bus(top, "video_plug_request", REQUEST_BITS)
        response = _bus(top, "video_response", RESPONSE_BITS)
        for name in boundary_bels():
            connections = cells[PREFIX + name]["connections"]
            if name.startswith("plug_request_ff_"):
                bit = int(name.removeprefix("plug_request_ff_"))
                if connections.get("Q") != [request[bit]]:
                    raise ValueError("native video source boundary changed")
                if connections.get("DATAIN") != [source[bit]]:
                    raise ValueError("native video source boundary input changed")
            elif name.startswith("plug_response_ff_"):
                bit = int(name.removeprefix("plug_response_ff_"))
                if connections.get("Q") != [response[bit]]:
                    raise ValueError("native video response boundary changed")
                if connections.get("DATAIN") != ["0"]:
                    raise ValueError("vacant native video response input changed")
            elif connections.get("DATAIN") != ["0"]:
                raise ValueError("native video clock anchor input changed")

    if routed:
        allowed = {PREFIX + name for name in boundary_bels()}
        for name, cell in cells.items():
            pieces = cell.get("attributes", {}).get(key, "").split(".")
            if len(pieces) >= 3 and pieces[1].isdigit() and pieces[2].isdigit() and \
                    5 <= int(pieces[1]) <= 38 and 23 <= int(pieces[2]) <= 38 and name not in allowed:
                raise ValueError(f"native video reservation contains shell cell: {name}")


def prepare_scaffold(source: bytes) -> bytes:
    """Expose native video under the compiler's one packed-port adapter."""
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
            raise ValueError("native video boundary alias collision")
        cells[target] = cells.pop(original)
    return (json.dumps(design, separators=(",", ":")) + "\n").encode()
