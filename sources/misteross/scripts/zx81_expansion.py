"""Fixed ZX81 RAM socket contract for the existing freeze-scaffold linker."""

from __future__ import annotations

import json
from pathlib import Path


SOCKET_CLOCK = "clk_sys"
SOCKET_RECT = "25 1 27 32"
SOCKET_PREFIX = "expansion.socket."
REQUEST_BITS = 37
RESPONSE_BITS = 17


def socket_bels() -> dict[str, str]:
    result = {}
    for bit in range(REQUEST_BITS):
        row, index = divmod(bit, 20)
        z = (index // 2) * 6 + (4 if index % 2 else 2)
        result[f"plug_addr_ff_{bit}"] = f"MISTRAL_FF.24.{row + 1}.{z}"
    for bit in range(RESPONSE_BITS):
        result[f"plug_rdata_ff_{bit}"] = f"MISTRAL_FF.28.{bit + 1}.2"
    return result


def prepare_shell_netlist(path: Path) -> None:
    """Give retained boundary FFs the existing compiler's canonical names.

    Only names change, never connectivity, placement or logic. Require the
    full physical socket and its one declared system clock before routing.
    """
    design = json.loads(path.read_text())
    top = design["modules"]["top"]
    cells = top["cells"]
    clock = top["netnames"][SOCKET_CLOCK]["bits"]
    requests = top["netnames"]["plug_addr"]["bits"]
    if len(clock) != 1 or len(requests) != REQUEST_BITS:
        raise ValueError("ZX81 socket clock or request bus is malformed")
    for name, bel in socket_bels().items():
        original = SOCKET_PREFIX + name
        cell = cells.get(original)
        if name in cells or not isinstance(cell, dict):
            raise ValueError(f"ZX81 socket boundary is missing or collides: {name}")
        if cell.get("type") != "MISTRAL_FF" or cell.get("attributes", {}).get("BEL") != bel:
            raise ValueError(f"ZX81 socket boundary has changed physical placement: {name}")
        if cell.get("connections", {}).get("CLK") != clock:
            raise ValueError(f"ZX81 socket boundary has the wrong clock: {name}")
        if name.startswith("plug_addr_ff_"):
            bit = int(name.removeprefix("plug_addr_ff_"))
            if cell.get("connections", {}).get("Q") != [requests[bit]]:
                raise ValueError(f"ZX81 socket request bus has changed wiring: {name}")
    for name in socket_bels():
        cells[name] = cells.pop(SOCKET_PREFIX + name)
    path.write_text(json.dumps(design, indent=2) + "\n")


def shell_qsf(base: str) -> str:
    return base.rstrip() + f'\nset_global_assignment -name FES_RESERVED_RECT "{SOCKET_RECT}"\n'
