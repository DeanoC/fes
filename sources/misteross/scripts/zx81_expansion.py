"""Fixed ZX81 expansion-edge contract for the existing freeze-scaffold linker.

Request 44 bits: A[15:0], Dwr[7:0], /MREQ /IORQ /RD /WR /M1 /RFSH, peek_a[13:0].
Response 20 bits: Drd[7:0], peek_d[7:0], DSEL, ROMCS, WAIT, RAM_PRESENT.
Vacant response FFs hold 0, so cart-to-CPU controls are active-high.
CRAM map `fes.zx81-bus.socket/1` is the reserved rectangle for any compatible
ZX81 bus cart. It is not the old pre-decoded 14-bit RAM window.
"""

from __future__ import annotations

import json
from pathlib import Path


SOCKET_CLOCK = "clk_sys"
SOCKET_RECT = "25 1 27 32"
SOCKET_PREFIX = "expansion.socket."
REQUEST_BITS = 44
RESPONSE_BITS = 20


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
    nets = top["netnames"]
    for name, pll_name in ((SOCKET_CLOCK, "system_clock.pll"),
                           ("audio_clk", "audio_clock.pll")):
        pll = cells.get(pll_name)
        if pll is None:
            continue  # Older fixture has a named system clock but no PLL cell.
        if pll.get("type") != "altera_pll":
            raise ValueError(f"ZX81 {name} producer is not a PLL")
        output = pll.get("connections", {}).get("outclk")
        if not isinstance(output, list) or len(output) != 1 or type(output[0]) is not int:
            raise ValueError(f"ZX81 {name} PLL output is malformed")
        if name in nets and nets[name].get("bits") != output:
            raise ValueError(f"ZX81 {name} alias does not match its PLL output")
        nets.setdefault(name, {"hide_name": 0, "bits": output, "attributes": {}})
    if SOCKET_CLOCK not in nets:
        raise ValueError("ZX81 socket system clock is missing")
    clock = nets[SOCKET_CLOCK]["bits"]
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
