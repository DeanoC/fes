"""ZX Spectrum edge sockets (`fes.spectrum-bus.sockets/1`).

One table describes the four physical sockets: their indexes, placement
rectangles, CRAM rectangles and pinned boundary flip-flops. The expansion Go
linker keeps the same CRAM rectangles in its closed layout table. The
rectangles are the Apple II slot geometry (column 24, the same row bands),
which already routes both horizontal clock segments into every socket row.
`--write-rtl` regenerates `cores/fes-spectrum/rtl/spectrum_slot_sockets.v`.
"""

from __future__ import annotations

import argparse
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / 'cores' / 'fes-spectrum' / 'rtl' / 'spectrum_slot_sockets.v'
LAYOUT = 'fes.spectrum-bus.sockets/1'
INTERFACE = 'fes.expansion.spectrum-bus'
REQUEST_BITS = 32
RESPONSE_BITS = 28
COLUMN = 24


@dataclass(frozen=True)
class Socket:
    slot: int
    first_row: int
    last_row: int
    cram: tuple[int, int, int, int]

    @property
    def region(self) -> str:
        return f'slot{self.slot}'

    @property
    def placement(self) -> str:
        """FES_RESERVED_RECT value: name x0 y0 x1 y1 (inclusive tiles)."""
        return f'{self.region} {COLUMN} {self.first_row} {COLUMN + 4} {self.last_row}'

    @property
    def instance(self) -> str:
        return f'slot{self.slot}.'


SOCKETS = (
    Socket(1, 1, 18, (1769, 32, 2806, 1722)),
    Socket(2, 21, 38, (1769, 1722, 2806, 3442)),
    Socket(3, 41, 58, (1769, 3442, 2806, 5162)),
    Socket(4, 61, 78, (1769, 5162, 2806, 6882)),
)


def _z(index: int) -> int:
    return (index // 2) * 6 + (4 if index % 2 else 2)


def clock_anchor_bels(socket: Socket) -> list[str]:
    """One flip-flop per socket row on each horizontal clock segment.

    Column 24 takes its row clock from the HCLKB.16 segment and columns
    25-28 from HCLKB.25. A frozen shell routes a segment branch only to rows
    where it has a cell, and a card cannot add one: those pips are outside
    its CRAM fence. Anchoring every row of column 24 (below the boundary
    LABs) and of column 28 makes both segments reach every socket row.
    """
    rows24 = range(socket.first_row + 3, socket.last_row + 1)
    rows28 = range(socket.first_row, socket.last_row + 1)
    return ([f'MISTRAL_FF.{COLUMN}.{row}.56' for row in rows24] +
            [f'MISTRAL_FF.{COLUMN + 4}.{row}.56' for row in rows28])


def boundary_bels(socket: Socket) -> dict[str, str]:
    """Cell name (without instance prefix) to pinned BEL."""
    base = socket.first_row
    result = {f'clock_coverage_ff_{index}': bel
              for index, bel in enumerate(clock_anchor_bels(socket))}
    for bit in range(REQUEST_BITS):
        row, index = (base, bit) if bit < 20 else (base + 1, bit - 20)
        result[f'plug_request_ff_{bit}'] = f'MISTRAL_FF.{COLUMN}.{row}.{_z(index)}'
    for bit in range(RESPONSE_BITS):
        row, index = (base + 1, 12 + bit) if bit < 8 else (base + 2, bit - 8)
        result[f'plug_response_ff_{bit}'] = f'MISTRAL_FF.{COLUMN}.{row}.{_z(index)}'
    return result


def socket_rtl() -> str:
    out = ['''// SPDX-License-Identifier: GPL-2.0-or-later
// Registered boundaries of the four physical ZX Spectrum edge sockets
// (fes.spectrum-bus.sockets/1). Each socket pins its 32 request and 28
// response flip-flops in the first three LABs of column 24 of its placement
// rectangle, plus one clock-coverage flip-flop per socket row in columns 24
// and 28 so the frozen shell routes both horizontal clock segments into every
// socket row. A vacant socket's response registers clock in zero. Simulation
// uses plain registers with the same latency.
// Generated from the socket table in scripts/spectrum_slots.py; do not edit.
`include "spectrum_bus.vh"''']
    for socket in SOCKETS:
        bels = boundary_bels(socket)
        out.append(f'''
module spectrum_slot_socket{socket.slot} (
    input  wire clock,
    input  wire [`SP_BUS_REQ-1:0] request,
    output wire [`SP_BUS_RSP-1:0] response,
    output wire [`SP_BUS_REQ-1:0] plug_request,
    input  wire [`SP_BUS_RSP-1:0] plug_response
);
`ifdef VERILATOR
    reg [`SP_BUS_REQ-1:0] request_q = 0;
    reg [`SP_BUS_RSP-1:0] response_q = 0;
    always @(posedge clock) begin
        request_q <= request;
        response_q <= plug_response;
    end
    assign plug_request = request_q;
    assign response = response_q;
`else
`define SP_SOCKET_FF(NAME, SITE, D, QOUT) \\
    (* keep, BEL = SITE *) MISTRAL_FF NAME ( \\
        .CLK(clock), .DATAIN(D), .Q(QOUT), .ACLR(1'b1), .ENA(1'b1), \\
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    (* keep *) wire [{len(clock_anchor_bels(socket)) - 1}:0] clock_coverage_unused;''')
        for index, bel in enumerate(clock_anchor_bels(socket)):
            out.append(f'    `SP_SOCKET_FF(clock_coverage_ff_{index}, "{bel}", 1\'b0, clock_coverage_unused[{index}])')
        for bit in range(REQUEST_BITS):
            out.append(f'    `SP_SOCKET_FF(plug_request_ff_{bit}, "{bels[f"plug_request_ff_{bit}"]}", '
                       f'request[{bit}], plug_request[{bit}])')
        for bit in range(RESPONSE_BITS):
            out.append(f'    `SP_SOCKET_FF(plug_response_ff_{bit}, "{bels[f"plug_response_ff_{bit}"]}", '
                       f'plug_response[{bit}], response[{bit}])')
        out.append('''`undef SP_SOCKET_FF
`endif
endmodule''')
    return '\n'.join(out) + '\n'


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--write-rtl', action='store_true')
    parser.add_argument('--check-rtl', action='store_true')
    args = parser.parse_args()
    if args.write_rtl:
        RTL.write_text(socket_rtl())
    if args.check_rtl and RTL.read_text() != socket_rtl():
        raise SystemExit(f'{RTL.relative_to(ROOT)} is stale; run scripts/spectrum_slots.py --write-rtl')


if __name__ == '__main__':
    main()
