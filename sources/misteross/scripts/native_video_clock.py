"""Preserve native cart clocks across the locked frozen-scaffold importer.

The importer maps the declared input buffer output to the shell clock, but
does not map every transparent buffer alias for SDP CLK2. Canonicalize only
proven clock inputs to that imported output; preserve the memory mode and all
other connections. This is a producer boundary adaptation, not a clock gate.
"""

from __future__ import annotations

import copy
import hashlib
import json
import os
from pathlib import Path
import stat
import tempfile


MAX_CART_JSON = 32 << 20
TRANSPARENT_BUFFERS = {"MISTRAL_CLKBUF", "MISTRAL_BUF"}
CLOCK_PORTS = {"CLK", "CLK1", "CLK2"}


def _signal(value) -> bool:
    return isinstance(value, int) and not isinstance(value, bool) and value >= 2


def _one_signal(bits, label: str) -> int:
    if not isinstance(bits, list) or len(bits) != 1 or not _signal(bits[0]):
        raise ValueError(f"native clock {label} must be one live signal")
    return bits[0]


def _flag(parameters: dict, name: str) -> bool:
    value = parameters.get(name, 0)
    if isinstance(value, str) and value and set(value) <= {"0", "1"}:
        value = int(value, 2)
    if type(value) is not int or value not in (0, 1):
        raise ValueError(f"native clock {name} must be a binary flag")
    return bool(value)


def normalize_native_clock_inputs(netlist: dict) -> tuple[dict, dict]:
    """Return an independent netlist and a receipt; leave the input untouched."""
    if not isinstance(netlist, dict) or not isinstance(netlist.get("modules"), dict):
        raise ValueError("native clock netlist requires modules")
    top = netlist["modules"].get("cart")
    if not isinstance(top, dict) or not isinstance(top.get("cells"), dict) or not isinstance(top.get("ports"), dict):
        raise ValueError("native clock netlist requires the cart module")
    port = top["ports"].get("FPGA_CLK1_50")
    if not isinstance(port, dict) or port.get("direction") != "input":
        raise ValueError("native clock requires the declared FPGA_CLK1_50 input")
    pad = _one_signal(port.get("bits"), "FPGA_CLK1_50")
    cells = top["cells"]
    drivers: dict[int, list[tuple[str, str]]] = {}
    for name, cell in cells.items():
        if not isinstance(name, str) or not isinstance(cell, dict) or not isinstance(cell.get("type"), str):
            raise ValueError("native clock netlist has a malformed cell")
        connections, directions = cell.get("connections"), cell.get("port_directions")
        if not isinstance(connections, dict) or not isinstance(directions, dict) or set(connections) != set(directions):
            raise ValueError(f"native clock cell {name} has malformed ports")
        if not isinstance(cell.get("parameters", {}), dict):
            raise ValueError(f"native clock cell {name} has malformed parameters")
        for pin, bits in connections.items():
            if not isinstance(pin, str) or directions[pin] not in ("input", "output", "inout") or not isinstance(bits, list):
                raise ValueError(f"native clock cell {name}.{pin} has malformed wiring")
            for bit in bits:
                if not (_signal(bit) or isinstance(bit, str) and bit in ("0", "1", "x", "z")):
                    raise ValueError(f"native clock cell {name}.{pin} has malformed signal")
                if _signal(bit) and directions[pin] in ("output", "inout"):
                    drivers.setdefault(bit, []).append((name, pin))

    inputs = []
    for name, cell in cells.items():
        if cell["type"] == "MISTRAL_IB" and pad in cell["connections"].get("PAD", []):
            if cell["connections"].get("PAD") != [pad] or cell["port_directions"] != {"PAD": "input", "O": "output"} or cell.get("parameters", {}):
                raise ValueError("native clock input buffer has malformed wiring")
            imported = _one_signal(cell["connections"].get("O"), f"{name}.O")
            inputs.append((name, imported))
    if len(inputs) != 1:
        raise ValueError("native clock requires exactly one declared input MISTRAL_IB")
    input_name, imported = inputs[0]
    if imported == pad or drivers.get(pad) or drivers.get(imported) != [(input_name, "O")]:
        raise ValueError("native clock imported input must have one unambiguous driver")

    buffers: dict[int, tuple[str, int]] = {}
    for name, cell in cells.items():
        if cell["type"] not in TRANSPARENT_BUFFERS:
            continue
        if set(cell["connections"]) != {"A", "Q"} or cell.get("parameters", {}) or cell["port_directions"] != {"A": "input", "Q": "output"}:
            raise ValueError(f"native clock buffer {name} is not transparent")
        source = _one_signal(cell["connections"]["A"], f"{name}.A")
        output = _one_signal(cell["connections"]["Q"], f"{name}.Q")
        if output in buffers or drivers.get(output) != [(name, "Q")]:
            raise ValueError("native clock buffer alias has ambiguous drivers")
        buffers[output] = (name, source)

    proven = {imported}
    def prove(bit: int) -> None:
        path = set()
        while bit not in proven:
            if bit in path:
                raise ValueError("native clock transparent buffer graph has a cycle")
            path.add(bit)
            if bit not in buffers:
                raise ValueError("native clock does not trace to the declared input; derived or different clocks are unsupported")
            bit = buffers[bit][1]
        proven.update(path)
    for output in buffers:
        prove(output)

    normalized = copy.deepcopy(netlist)
    rewritten = clock_pins = ram_blocks = 0
    clock_cells: dict[str, int] = {}
    for name, cell in cells.items():
        kind = cell["type"]
        connections = cell["connections"]
        if kind == "MISTRAL_FF":
            pins = ("CLK",)
        elif kind in ("MISTRAL_M10K", "MISTRAL_M10K_TDP"):
            parameters = cell.get("parameters", {})
            dual = _flag(parameters, "CFG_DUAL_CLOCK")
            mixed = _flag(parameters, "CFG_MIXED_WIDTH")
            tdp = kind == "MISTRAL_M10K_TDP" or _flag(parameters, "CFG_TDP")
            if mixed and not dual and not tdp:
                raise ValueError("native mixed-width SDP requires dual-clock semantics")
            if not (dual or tdp) and "CLK2" in connections:
                raise ValueError("native single-clock SDP must not declare CLK2")
            pins = ("CLK1", "CLK2") if dual or tdp else ("CLK1",)
            ram_blocks += 1
        else:
            if CLOCK_PORTS & connections.keys():
                raise ValueError(f"native clock cell {name} has unsupported sequential ports")
            continue
        clock_cells[kind] = clock_cells.get(kind, 0) + 1
        if CLOCK_PORTS & connections.keys() != set(pins):
            raise ValueError(f"native clock cell {name} has missing or unsupported clock pins")
        for pin in pins:
            if cell["port_directions"].get(pin) != "input":
                raise ValueError(f"native clock {name}.{pin} must be an input")
            bit = _one_signal(connections.get(pin), f"{name}.{pin}")
            prove(bit)
            normalized["modules"]["cart"]["cells"][name]["connections"][pin] = [imported]
            rewritten += bit != imported
            clock_pins += 1
    if not clock_pins:
        raise ValueError("native clock netlist has no supported sequential cells")
    return normalized, {"version": 1, "clock_port": "FPGA_CLK1_50", "input_buffer": input_name,
                        "imported_bit": imported, "transparent_buffers": len(buffers),
                        "clock_pins": clock_pins, "rewritten_clock_pins": rewritten,
                        "ram_blocks": ram_blocks, "clock_cells": clock_cells}


def _object(pairs):
    result = {}
    for name, value in pairs:
        if name in result:
            raise ValueError("native clock JSON contains a duplicate field")
        result[name] = value
    return result


def prepare_native_clock(path: Path) -> dict:
    """Atomically adapt a native cart JSON only after the complete proof passes."""
    path = Path(path)
    if path.is_symlink() or not path.is_file():
        raise ValueError("native cart JSON must be a regular non-symlink file")
    before = path.read_bytes()
    if not 0 < len(before) <= MAX_CART_JSON:
        raise ValueError("native cart JSON exceeds its size bound")
    normalized, receipt = normalize_native_clock_inputs(json.loads(before, object_pairs_hook=_object))
    after = (json.dumps(normalized, sort_keys=True, separators=(",", ":")) + "\n").encode()
    if len(after) > MAX_CART_JSON:
        raise ValueError("normalized native cart JSON exceeds its size bound")
    receipt.update({"synth_sha256": hashlib.sha256(before).hexdigest(),
                    "prepared_sha256": hashlib.sha256(after).hexdigest()})
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(dir=path.parent, prefix=".native-clock-", delete=False) as stream:
            temporary = Path(stream.name)
            os.fchmod(stream.fileno(), stat.S_IMODE(path.stat().st_mode))
            stream.write(after)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)
    return receipt
