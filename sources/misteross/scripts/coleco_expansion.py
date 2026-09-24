"""Frozen Coleco CPU-edge boundary for the development-only OSS shell.

The rectangle is a routing hypothesis until the authenticated empty-shell and
diagnostic module builds prove it is vacant and timing-clean.
"""

from __future__ import annotations

import json
from pathlib import Path


SOCKET_CLOCK = 'system_clock.clocks_MISTRAL_CLKBUF_Q'
SOCKET_RECT = '24 1 28 11'
SOCKET_PREFIX = 'socket.'
REQUEST_BITS = 31
RESPONSE_BITS = 11


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


def raw_cell_name(name: str) -> str:
    if name.startswith('plug_addr_ff_'):
        return SOCKET_PREFIX + name.replace('plug_addr_ff_', 'plug_request_ff_', 1)
    if name.startswith('plug_rdata_ff_'):
        return SOCKET_PREFIX + name.replace('plug_rdata_ff_', 'plug_response_ff_', 1)
    raise ValueError(f'unknown Coleco boundary cell: {name}')


def shell_qsf(base: str) -> str:
    if 'FES_RESERVED_RECT' in base:
        raise ValueError('base QSF already reserves a CRAM rectangle')
    return base.rstrip() + f'\nset_global_assignment -name FES_RESERVED_RECT "{SOCKET_RECT}"\n'


def prepare_shell_netlist(path: Path) -> None:
    design = json.loads(path.read_text())
    top = design['modules']['top']
    cells = top['cells']
    clock_buffer = cells.get(SOCKET_CLOCK, {})
    pll_clock = top['netnames'].get('system_clock.pll_outclk', {}).get('bits')
    if clock_buffer.get('type') != 'MISTRAL_CLKBUF' or \
            clock_buffer.get('connections', {}).get('A') != pll_clock or \
            not isinstance(pll_clock, list) or len(pll_clock) != 1:
        raise ValueError('Coleco socket system clock source changed')
    clock = clock_buffer['connections'].get('Q')
    request = top['netnames']['plug_request']['bits']
    if not isinstance(clock, list) or len(clock) != 1 or len(request) != REQUEST_BITS:
        raise ValueError('Coleco socket clock or request bus is malformed')
    for name, bel in socket_bels().items():
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
    for name in socket_bels():
        cells[name] = cells.pop(raw_cell_name(name))
    path.write_text(json.dumps(design, indent=2) + '\n')


def validate_routed_shell(path: Path) -> None:
    """Require every reserved BEL to be vacant except the 42 pinned edge FFs."""
    cells = json.loads(path.read_text())['modules']['top']['cells']
    expected = socket_bels()
    x0, y0, x1, y1 = map(int, SOCKET_RECT.split())
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
        if x0 <= int(parts[1]) <= x1 and y0 <= int(parts[2]) <= y1 and name not in expected:
            raise ValueError(f'Coleco reserved socket contains shell cell: {name}')
