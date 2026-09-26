"""Frozen Coleco CPU-edge boundary for the development-only OSS shell.

The rectangle is a routing hypothesis until the authenticated empty-shell and
diagnostic module builds prove it is vacant and timing-clean.
"""

from __future__ import annotations

import json
from pathlib import Path


SOCKET_CLOCK = 'system_clock.clocks_MISTRAL_CLKBUF_Q'
SOCKET_RECT = '24 1 28 11'
SOCKET_RECT_V2 = '24 1 28 19'
SOCKET_PREFIX = 'socket.'
REQUEST_BITS = 31
RESPONSE_BITS = 11
RESPONSE_BITS_V2 = 28
SOCKET_CLOCK_COVERAGE_CELL = 'clock_coverage_ff'
SOCKET_CLOCK_COVERAGE_RAW_CELL = 'socket.clock_coverage_ff'
SOCKET_CLOCK_COVERAGE_BEL = 'MISTRAL_FF.24.4.56'
SOCKET_CLOCK_COVERAGE_NET = 'system_clock.clocks[0]'
SOCKET_CLOCK_COVERAGE_ARCS = (
    'HCLK.16.4.5.HCLKB.16.4.5',
    'HCLKB.16.4.5.XCLKB1.24.4.5',
    'XCLKB1.24.4.5.XCLKB2A.24.4.5',
    'XCLKB2A.24.4.5.TCLK.24.4.0',
    'TCLK.24.4.0.WIRE.24.4.CLK0',
    'WIRE.24.4.CLK0.WIRE.24.4.CLKT[9]',
)


def socket_bels() -> dict[str, str]:
    result = {}
    for bit in range(REQUEST_BITS):
        row, index = divmod(bit, 20)
        z = (index // 2) * 6 + (4 if index % 2 else 2)
        if bit == 23:
            # Keep this write-data edge out of the congested second row.
            row = 2
        result[f'plug_addr_ff_{bit}'] = f'MISTRAL_FF.24.{row + 1}.{z}'
    for bit in range(RESPONSE_BITS):
        result[f'plug_rdata_ff_{bit}'] = f'MISTRAL_FF.28.{bit + 1}.2'
    return result


def socket_bels_v2() -> dict[str, str]:
    result = {name: bel for name, bel in socket_bels().items()
              if name.startswith('plug_addr_ff_')}
    request_bels = set(result.values())
    vacant = []
    for row in (2, 3):
        for index in range(20):
            z = (index // 2) * 6 + (4 if index % 2 else 2)
            bel = f'MISTRAL_FF.24.{row}.{z}'
            if bel not in request_bels:
                vacant.append(bel)
    for bit, bel in enumerate(vacant[:RESPONSE_BITS_V2]):
        result[f'plug_rdata_ff_{bit}'] = bel
    return result


def raw_cell_name(name: str) -> str:
    if name.startswith('plug_addr_ff_'):
        return SOCKET_PREFIX + name.replace('plug_addr_ff_', 'plug_request_ff_', 1)
    if name.startswith('plug_rdata_ff_'):
        return SOCKET_PREFIX + name.replace('plug_rdata_ff_', 'plug_response_ff_', 1)
    raise ValueError(f'unknown Coleco boundary cell: {name}')


def validate_v2_clock_coverage_route(top: dict) -> None:
    """Require the frozen system clock route to reach the row 4 socket edge."""
    net = top.get('netnames', {}).get(SOCKET_CLOCK_COVERAGE_NET, {})
    clock_bits = net.get('bits')
    anchor = top.get('cells', {}).get(SOCKET_CLOCK_COVERAGE_CELL, {})
    if not isinstance(clock_bits, list) or len(clock_bits) != 1 or not isinstance(anchor, dict) or \
            anchor.get('connections', {}).get('CLK') != clock_bits:
        raise ValueError('Coleco socket clock coverage anchor is disconnected from the system clock')
    route = net.get('attributes', {}).get('ROUTING')
    if not isinstance(route, str):
        raise ValueError('Coleco socket clock coverage route is missing')
    fields = route.split(';')
    if len(fields) % 3:
        raise ValueError('Coleco socket clock coverage route is malformed')
    nodes = set(fields[0::3])
    arcs = set(fields[1::3])
    if 'GCLK.0.36.3' not in nodes or not set(SOCKET_CLOCK_COVERAGE_ARCS).issubset(arcs):
        raise ValueError('Coleco socket clock coverage is missing the required row 4 branch')


def shell_qsf(base: str, *, version: int = 1) -> str:
    if 'FES_RESERVED_RECT' in base:
        raise ValueError('base QSF already reserves a CRAM rectangle')
    if version not in (1, 2):
        raise ValueError('unsupported Coleco socket version')
    rect = SOCKET_RECT_V2 if version == 2 else SOCKET_RECT
    return base.rstrip() + f'\nset_global_assignment -name FES_RESERVED_RECT "{rect}"\n'


def prepare_shell_netlist(path: Path, *, version: int = 1) -> None:
    if version not in (1, 2):
        raise ValueError('unsupported Coleco socket version')
    expected = socket_bels_v2() if version == 2 else socket_bels()
    design = json.loads(path.read_text())
    top = design['modules']['top']
    cells = top['cells']
    pll_clock = top['netnames'].get('system_clock.pll_outclk', {}).get('bits')
    clock_buffers = [cell for name, cell in cells.items()
                     if name.startswith(SOCKET_CLOCK) and cell.get('type') == 'MISTRAL_CLKBUF' and
                     cell.get('connections', {}).get('A') == pll_clock]
    if not isinstance(pll_clock, list) or len(pll_clock) != 1 or len(clock_buffers) != 1:
        raise ValueError('Coleco socket system clock source changed')
    clock = clock_buffers[0]['connections'].get('Q')
    request = top['netnames']['plug_request']['bits']
    if not isinstance(clock, list) or len(clock) != 1 or len(request) != REQUEST_BITS:
        raise ValueError('Coleco socket clock or request bus is malformed')
    if version == 2:
        source_request = top['netnames']['bus_request']['bits']
        response = top['netnames']['bus_response']['bits']
        if len(source_request) != REQUEST_BITS or len(response) != RESPONSE_BITS_V2:
            raise ValueError('Coleco v2 socket source or response bus is malformed')
        coverage = cells.get(SOCKET_CLOCK_COVERAGE_RAW_CELL)
        if SOCKET_CLOCK_COVERAGE_CELL in cells or not isinstance(coverage, dict) or \
                coverage.get('type') != 'MISTRAL_FF' or \
                coverage.get('attributes', {}).get('BEL') != SOCKET_CLOCK_COVERAGE_BEL or \
                coverage.get('connections', {}).get('CLK') != clock or \
                coverage.get('connections', {}).get('DATAIN') != ['0']:
            raise ValueError('Coleco v2 socket clock coverage anchor changed')
    for name, bel in expected.items():
        original = raw_cell_name(name)
        cell = cells.get(original)
        if name in cells or not isinstance(cell, dict):
            raise ValueError(f'Coleco socket boundary is missing or collides: {name}')
        if cell.get('type') != 'MISTRAL_FF' or cell.get('attributes', {}).get('BEL') != bel:
            raise ValueError(f'Coleco socket boundary changed physical placement: {name}')
        if cell.get('connections', {}).get('CLK') != clock:
            raise ValueError(f'Coleco socket boundary has wrong clock: {name}')
        if name.startswith('plug_addr_ff_'):
            bit = int(name.removeprefix('plug_addr_ff_'))
            if cell.get('connections', {}).get('Q') != [request[bit]]:
                raise ValueError(f'Coleco socket request bit changed wiring: {name}')
            if version == 2 and cell['connections'].get('DATAIN') != [source_request[bit]]:
                raise ValueError(f'Coleco socket request input changed wiring: {name}')
        elif version == 2:
            bit = int(name.removeprefix('plug_rdata_ff_'))
            if cell['connections'].get('Q') != [response[bit]] or \
                    cell['connections'].get('DATAIN') != ['0']:
                raise ValueError(f'Coleco socket vacant response changed wiring: {name}')
    for name in expected:
        cells[name] = cells.pop(raw_cell_name(name))
    if version == 2:
        cells[SOCKET_CLOCK_COVERAGE_CELL] = cells.pop(SOCKET_CLOCK_COVERAGE_RAW_CELL)
    path.write_text(json.dumps(design, indent=2) + '\n')


def validate_routed_shell(path: Path, *, version: int = 1) -> None:
    """Require every reserved BEL to be vacant except the pinned edge FFs."""
    if version not in (1, 2):
        raise ValueError('unsupported Coleco socket version')
    top = json.loads(path.read_text())['modules']['top']
    cells = top['cells']
    expected = socket_bels_v2() if version == 2 else socket_bels()
    rect = SOCKET_RECT_V2 if version == 2 else SOCKET_RECT
    allowed = set(expected)
    if version == 2:
        coverage = cells.get(SOCKET_CLOCK_COVERAGE_CELL)
        if not isinstance(coverage, dict) or coverage.get('type') != 'MISTRAL_FF' or \
                coverage.get('attributes', {}).get('NEXTPNR_BEL') != SOCKET_CLOCK_COVERAGE_BEL:
            raise ValueError(f'Coleco routed socket clock coverage anchor changed: {SOCKET_CLOCK_COVERAGE_CELL}')
        validate_v2_clock_coverage_route(top)
        allowed.add(SOCKET_CLOCK_COVERAGE_CELL)
    x0, y0, x1, y1 = map(int, rect.split())
    for name, bel in expected.items():
        cell = cells.get(name)
        if not isinstance(cell, dict) or cell.get('type') != 'MISTRAL_FF' or \
                cell.get('attributes', {}).get('NEXTPNR_BEL') != bel:
            raise ValueError(f'Coleco routed socket boundary changed: {name}')
    for name, cell in cells.items():
        bel = cell.get('attributes', {}).get('NEXTPNR_BEL', '')
        parts = bel.split('.')
        if len(parts) < 3 or not parts[1].isdigit() or not parts[2].isdigit():
            continue
        if x0 <= int(parts[1]) <= x1 and y0 <= int(parts[2]) <= y1 and name not in allowed:
            raise ValueError(f'Coleco reserved socket contains shell cell: {name}')
