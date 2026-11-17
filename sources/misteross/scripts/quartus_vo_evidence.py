"""DE10-Nano HPS DDR and session-display evidence from a Quartus post-fit netlist.

The OSS producers prove the fes.memory.hps-ddr layout and the session-display
port use on Yosys/nextpnr JSON (scripts/fes_de10nano_evidence.py). A Quartus
build has no such JSON. `quartus_eda --simulation --tool=modelsim
--format=verilog` writes the fitted design as a Verilog netlist (.vo) whose
`cyclonev_hps_interface_fpga2sdram` instance shows every port connection: `gnd`,
`vcc`, a net, or a concatenation of those, most-significant bit first.

This module turns that one instance into the cell/connection shape the
existing evidence functions read, with the same bit order (index 0 is the
least significant bit), and runs those functions unchanged. Constants become
"0" and "1"; every distinct net becomes a distinct integer, so no net can pass
for a constant.
"""
from __future__ import annotations

import re
from pathlib import Path

from scripts.fes_build_common import BuildError, TOP
from scripts import fes_de10nano_evidence

FPGA2SDRAM = "cyclonev_hps_interface_fpga2sdram"
_TOKEN = re.compile(r"\s*(\\\S+\s|[A-Za-z_][A-Za-z0-9_$]*(?:\[[0-9]+\])?|[{}(),.;])")


def _tokens(text: str):
    position = 0
    while position < len(text):
        if text[position].isspace():
            position += 1
            continue
        if text.startswith("//", position):
            end = text.find("\n", position)
            position = len(text) if end < 0 else end
            continue
        match = _TOKEN.match(text, position)
        if match is None:
            raise BuildError(f"cannot read post-fit netlist at offset {position}")
        token = match.group(1)
        yield token.strip() if token.startswith("\\") else token
        position = match.end()


def _instance_text(netlist: str, cell_type: str) -> tuple[str, str]:
    """The one instance of cell_type: its name and its port-list text."""
    starts = [m for m in re.finditer(rf"^\s*{re.escape(cell_type)}\s+(\\\S+|\S+)\s*\(", netlist, re.M)]
    if len(starts) != 1:
        raise BuildError(f"post-fit netlist must contain exactly one {cell_type}, found {len(starts)}")
    start = starts[0]
    depth, position = 1, start.end()
    while depth:
        if position >= len(netlist):
            raise BuildError(f"post-fit {cell_type} instance is not closed")
        character = netlist[position]
        if character == "\\":
            while position < len(netlist) and not netlist[position].isspace():
                position += 1
            continue
        depth += {"(": 1, ")": -1}.get(character, 0)
        position += 1
    return start.group(1).lstrip("\\"), netlist[start.end():position - 1]


def instance_connections(netlist: str, cell_type: str = FPGA2SDRAM) -> tuple[str, dict[str, list[str]]]:
    """Each port's connection as a list of signal names, least-significant bit first."""
    name, body = _instance_text(netlist, cell_type)
    tokens = list(_tokens(body))
    connections: dict[str, list[str]] = {}
    index = 0
    while index < len(tokens):
        if tokens[index] == ",":
            index += 1
            continue
        if tokens[index] != "." or index + 2 >= len(tokens) or tokens[index + 2] != "(":
            raise BuildError(f"post-fit {cell_type} uses an unexpected port connection form")
        port = tokens[index + 1]
        index += 3
        signals: list[str] = []
        if tokens[index] == "{":
            index += 1
            while tokens[index] != "}":
                if tokens[index] != ",":
                    signals.append(tokens[index])
                index += 1
            index += 1
        elif tokens[index] != ")":
            signals.append(tokens[index])
            index += 1
        if tokens[index] != ")":
            raise BuildError(f"post-fit {cell_type} port {port} is not a plain connection")
        index += 1
        if port in connections:
            raise BuildError(f"post-fit {cell_type} connects port {port} twice")
        connections[port] = list(reversed(signals))
    return name, connections


def fpga2sdram_design(netlist: str) -> dict:
    """The fpga2sdram instance as a one-cell design for fes_de10nano_evidence."""
    name, connections = instance_connections(netlist)
    nets: dict[str, int] = {}

    def bit(signal: str):
        if signal == "gnd":
            return "0"
        if signal == "vcc":
            return "1"
        return nets.setdefault(signal, len(nets) + 2)

    cell = {"type": FPGA2SDRAM,
            "connections": {port: [bit(s) for s in signals] for port, signals in connections.items()}}
    return {"modules": {TOP: {"cells": {name: cell}}}}


def session_display_evidence(netlist: str, source_root: Path) -> dict:
    """fes.memory.hps-ddr layout plus session-display port use from a post-fit .vo."""
    return fes_de10nano_evidence.session_display_evidence(fpga2sdram_design(netlist), "post-fit", source_root)
