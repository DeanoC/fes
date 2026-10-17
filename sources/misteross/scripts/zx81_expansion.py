"""Fixed ZX81 expansion-edge contract for the existing freeze-scaffold linker.

Request 46 bits: A[15:0], Dwr[7:0], /MREQ /IORQ /RD /WR /M1 /RFSH, peek_a[13:0], CPU clock, /RESET.
Response 20 bits: Drd[7:0], peek_d[7:0], DSEL, ROMCS, WAIT, RAM_PRESENT.
Vacant response FFs hold 0, so cart-to-CPU controls are active-high.
CRAM map `fes.zx81-bus.socket/2` is the reserved rectangle for any compatible
ZX81 bus cart. It is not the old pre-decoded 14-bit RAM window.

The cart interior stays columns 25–27. The request and response flip-flops
live on columns 24 and 28, outside that interior, and each request flip-flop
has one fabric exit (``GIN.24.{row}.{index}``). Sealed cores such as C64 and
Spectrum reserve the whole column span, including those boundary LABs, so the
shell placer cannot share them. ZX81 keeps the interior as the unnamed
``cart`` region (the cart merge's default) and reserves the two plug columns
for the socket height as separate regions. Unconstrained shell cells are
rejected there; BEL-locked socket flip-flops still place, and cart cells stay
in the interior. The CRAM fence is unchanged.
"""

from __future__ import annotations

import json
import re
from pathlib import Path


SOCKET_CLOCK = "clk_sys"
# Unnamed, so nextpnr calls this region "cart". Cart merge uses that default
# and must not gain the plug columns: frozen boundary LABs reject cart cells,
# and the CRAM fence is the interior.
SOCKET_RECT = "25 1 27 32"
# Plug columns for the full socket height. Separate region names, so they are
# keep-outs for the shell and not extra cart placement space.
BOUNDARY_RECTS = (
    "plug_addr 24 1 24 32",
    "plug_rdata 28 1 28 32",
)
SOCKET_PREFIX = "expansion.socket."
REQUEST_BITS = 46
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
    for name, clock_index in ((SOCKET_CLOCK, 0), ("audio_clk", 1)):
        pll = cells.get("system_clock.pll")
        if pll is None:
            continue  # Older fixture has a named system clock but no PLL cell.
        if pll.get("type") != "altera_pll":
            raise ValueError(f"ZX81 {name} producer is not a PLL")
        outputs = pll.get("connections", {}).get("outclk")
        if not isinstance(outputs, list) or len(outputs) != 2 or any(type(bit) is not int for bit in outputs):
            raise ValueError(f"ZX81 {name} PLL output is malformed")
        output = [outputs[clock_index]]
        buffers = [cell.get("connections", {}).get("Q") for cell in cells.values()
                   if cell.get("type") == "MISTRAL_CLKBUF"
                   and cell.get("connections", {}).get("A") == output]
        if len(buffers) > 1 or (buffers and
                (not isinstance(buffers[0], list) or len(buffers[0]) != 1
                 or type(buffers[0][0]) is not int)):
            raise ValueError(f"ZX81 {name} clock buffer is malformed")
        clock_output = buffers[0] if buffers else output
        if name in nets and nets[name].get("bits") != clock_output:
            raise ValueError(f"ZX81 {name} alias does not match its PLL output")
        nets.setdefault(name, {"hide_name": 0, "bits": clock_output, "attributes": {}})
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
    lines = [base.rstrip(), f'set_global_assignment -name FES_RESERVED_RECT "{SOCKET_RECT}"']
    lines.extend(
        f'set_global_assignment -name FES_RESERVED_RECT "{rect}"' for rect in BOUNDARY_RECTS)
    return "\n".join(lines) + "\n"


def _parse_rect(spec: str) -> tuple[str, int, int, int, int]:
    parts = spec.split()
    if len(parts) == 4 and parts[0].isdigit():
        name, numbers = "cart", parts
    elif len(parts) == 5 and not parts[0][:1].isdigit():
        name, numbers = parts[0], parts[1:]
    else:
        raise ValueError(f"ZX81 reserved rect is malformed: {spec}")
    try:
        x0, y0, x1, y1 = (int(number) for number in numbers)
    except ValueError as exc:
        raise ValueError(f"ZX81 reserved rect is malformed: {spec}") from exc
    return name, x0, y0, x1, y1


def _in_rect(rect: tuple[str, int, int, int, int], x: int, y: int) -> bool:
    _, x0, y0, x1, y1 = rect
    return x0 <= x <= x1 and y0 <= y <= y1


def _bel_xy(bel: str) -> tuple[int, int] | None:
    parts = bel.split(".")
    if len(parts) != 4 or not all(part.isdigit() for part in parts[1:]):
        return None
    return int(parts[1]), int(parts[2])


def paired_route_through_bels(ff_bel: str) -> set[str]:
    """The combinational half paired with one pinned socket flip-flop."""
    parts = ff_bel.split(".")
    if len(parts) != 4 or parts[0] != "MISTRAL_FF" or not all(part.isdigit() for part in parts[1:]):
        return set()
    z = int(parts[3])
    if z % 6 not in (2, 4):
        return set()
    half = 0 if z % 6 == 2 else 1
    paired = f"{parts[1]}.{parts[2]}.{(z // 6) * 6 + half}"
    return {f"MISTRAL_COMB.{paired}", f"MISTRAL_MCOMB.{paired}", f"MISTRAL_BUF.{paired}"}


def _pinned_socket_cell(name: str, cell: dict, bel: str, expected: dict[str, str]) -> bool:
    if name in expected:
        return cell.get("type") == "MISTRAL_FF" and bel == expected[name]
    if not name.endswith("$ROUTETHRU"):
        return False
    stem = name.removesuffix("$ROUTETHRU")
    return (
        stem in expected
        and cell.get("type") in {"MISTRAL_COMB", "MISTRAL_MCOMB", "MISTRAL_BUF"}
        and bel in paired_route_through_bels(expected[stem])
    )


def shell_cells_in_socket(design: object) -> list[tuple[str, str]]:
    """Shell cells placed in the cart interior or on the plug columns.

    A socket flip-flop may sit on its plug column only as ``MISTRAL_FF`` at
    the BEL from ``socket_bels``. Its ``$ROUTETHRU`` buffer may sit only on
    that flip-flop's paired combinational half, as ``MISTRAL_COMB``,
    ``MISTRAL_MCOMB``, or ``MISTRAL_BUF``. A matching name elsewhere is still
    shell logic. Nothing else may occupy the plug columns, and the cart
    interior stays empty in the shell.
    """
    modules = design.get("modules") if isinstance(design, dict) else None
    top = modules.get("top") if isinstance(modules, dict) else None
    cells = top.get("cells") if isinstance(top, dict) else None
    if not isinstance(cells, dict):
        return []
    cart = _parse_rect(SOCKET_RECT)
    boundary = tuple(_parse_rect(spec) for spec in BOUNDARY_RECTS)
    expected = socket_bels()
    intruders: list[tuple[str, str]] = []
    for name, cell in cells.items():
        if not isinstance(name, str) or not isinstance(cell, dict):
            continue
        attributes = cell.get("attributes")
        bel = ""
        if isinstance(attributes, dict):
            raw = attributes.get("NEXTPNR_BEL") or attributes.get("BEL") or ""
            if isinstance(raw, str):
                bel = raw
        stem = name.removesuffix("$ROUTETHRU") if name.endswith("$ROUTETHRU") else ""
        if name in expected or stem in expected:
            if _pinned_socket_cell(name, cell, bel, expected):
                continue
            intruders.append((name, bel or "<unplaced>"))
            continue
        xy = _bel_xy(bel)
        if xy is None:
            continue
        if _in_rect(cart, *xy) or any(_in_rect(rect, *xy) for rect in boundary):
            intruders.append((name, bel))
    intruders.sort()
    return intruders


def socket_occupancy_message(intruders: list[tuple[str, str]]) -> str:
    shown = intruders[:4]
    details = "; ".join(f"{name} at {bel}" for name, bel in shown)
    extra = len(intruders) - len(shown)
    if extra:
        details += f"; and {extra} more"
    return f"socket boundary contains shell logic: {details}"


def plug_addr_fabric_exit(bit: int) -> str:
    """The one column-24 GIN a request flip-flop can drive.

    Each socket FF has a single output mux. A cart route leaves that mux as
    FFOUT into GIN.24.{row}.{(bit % 20) * 2}. Another net on that GIN makes
    the request bit unroutable.
    """
    if bit not in range(REQUEST_BITS):
        raise ValueError(f"ZX81 plug_addr bit {bit} is outside 0..{REQUEST_BITS - 1}")
    row, index = divmod(bit, 20)
    return f"GIN.24.{row + 1}.{index * 2}"


def _net_routing(net: object) -> str:
    if not isinstance(net, dict):
        return ""
    attributes = net.get("attributes")
    if not isinstance(attributes, dict):
        return ""
    routing = attributes.get("ROUTING")
    return routing if isinstance(routing, str) else ""


def _routing_uses(routing: str, wire: str) -> bool:
    # A pip token is src.dest. The next name starts with a letter, so a
    # longer index such as GIN.24.2.380 does not match GIN.24.2.38.
    return re.search(re.escape(wire) + r"(?=$|;|\.[A-Za-z])", routing) is not None


def blocked_plug_addr_exits(design: object) -> list[tuple[int, str]]:
    """Request bits whose only fabric exit is already owned by another net.

    Missing netnames are an empty shell for this check. A net named
    plug_addr[bit] may use its own exit; a cart extends that net.
    """
    modules = design.get("modules") if isinstance(design, dict) else None
    top = modules.get("top") if isinstance(modules, dict) else None
    netnames = top.get("netnames") if isinstance(top, dict) else None
    if not isinstance(netnames, dict):
        return []
    blocked: list[tuple[int, str]] = []
    for bit in range(REQUEST_BITS):
        wire = plug_addr_fabric_exit(bit)
        owner = f"plug_addr[{bit}]"
        for name, net in netnames.items():
            if name == owner or not isinstance(name, str):
                continue
            if _routing_uses(_net_routing(net), wire):
                blocked.append((bit, name))
                break
    return blocked


def blocked_plug_addr_message(blocked: list[tuple[int, str]]) -> str:
    shown = blocked[:4]
    details = "; ".join(f"plug_addr[{bit}] exit used by {net}" for bit, net in shown)
    extra = len(blocked) - len(shown)
    if extra:
        details += f"; and {extra} more"
    return f"socket boundary leaves plug_addr unroutable: {details}"


def socket_design_reason(design: object) -> str | None:
    """Reject a routed shell whose connector or socket floorplan is wrong.

    A socket flip-flop is legal only as ``MISTRAL_FF`` at its pinned BEL. A
    ``$ROUTETHRU`` is legal only on that flip-flop's paired half. Any other
    cell in the cart interior or on the plug columns is shell logic, and a
    foreign net on a request exit still makes that bit unroutable.
    """
    intruders = shell_cells_in_socket(design)
    if intruders:
        return socket_occupancy_message(intruders)
    blocked = blocked_plug_addr_exits(design)
    if not blocked:
        return None
    return blocked_plug_addr_message(blocked)


def socket_route_reason(routed: Path) -> str | None:
    if not routed.is_file():
        return "routed design is missing"
    try:
        design = json.loads(routed.read_text())
    except (OSError, json.JSONDecodeError) as exc:
        return f"routed design is unreadable: {exc}"
    return socket_design_reason(design)


def accept_socket_route(candidate: object) -> str | None:
    """QoR accept hook: reject a timing-passing shell that blocks plug_addr."""
    run_dir = getattr(candidate, "run_dir", "") or ""
    if not run_dir:
        return "routed design is missing"
    return socket_route_reason(Path(run_dir) / "routed.json")
