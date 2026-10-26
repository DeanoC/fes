#!/usr/bin/env python3
"""Generate the i386 microcode ROM from its source table.

cores/fes-common/rtl/i386/ucode.toml lists the microcode routines. Each
routine is a list of uops; each uop names the fields of fes_i386_pkg's
ucode_t that it sets, the rest taking their defaults. This script checks the
table (every field and value known, every routine non-empty and inside the
ROM, entry names unique), marks each routine's last uop, and writes two files beside it:
fes_i386_ucode_pkg.sv, the entry addresses (UC_<NAME>) for the decode, and
fes_i386_ucode_rom.sv, the ROM, an M10K-shaped array whose contents are
initial assignments, so every flow (Verilator, Yosys, Quartus) reads them
without a file path.

  python3 scripts/fes_i386_ucode.py           # regenerate
  python3 scripts/fes_i386_ucode.py --check   # fail if the checked-in file differs
"""
from __future__ import annotations

import argparse
from pathlib import Path
import sys
import tomllib

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / 'cores/fes-common/rtl/i386'
SOURCE = RTL / 'ucode.toml'
PACKAGE = RTL / 'fes_i386_ucode_pkg.sv'
ROM = RTL / 'fes_i386_ucode_rom.sv'
DEPTH = 256

# ucode_t, most significant field first, as fes_i386_pkg declares it.
FIELDS: tuple[tuple[str, int], ...] = (
    ('last', 1), ('cls', 3), ('op', 5), ('imm', 4), ('size', 2), ('src_size', 2),
    ('s1', 1), ('s1_idx', 3), ('s2', 1), ('s2_idx', 3),
    ('d', 1), ('d_space', 2), ('d_idx', 4),
    ('mkind', 2), ('mbytes', 2), ('in_a', 1), ('stk', 3), ('slot', 2), ('ivt', 1), ('sel_part', 1),
    ('probe', 2), ('b', 2), ('seg', 3),
    ('far_jmp', 1), ('far_chk', 1), ('far_t', 1), ('sp_add', 1),
    ('rd_cf', 1), ('rd_oszap', 1), ('wr_cf', 1), ('wr_oszap', 1),
)
WIDTH = sum(width for _, width in FIELDS)

GPR = {'eax': 0, 'ecx': 1, 'edx': 2, 'ebx': 3, 'esp': 4, 'ebp': 5, 'esi': 6, 'edi': 7}
SEG = {'es': 0, 'cs': 1, 'ss': 2, 'ds': 3, 'fs': 4, 'gs': 5}
# Named values; a field not listed takes an integer.
VALUES: dict[str, dict[str, int]] = {
    'cls': {'alu': 0, 'shift': 1, 'branch': 4, 'segment': 5, 'special': 7},
    'op': {'mov': 9, 'jmp': 1, 'movzx': 15, 'flags': 30, 'flags_read': 29},
    'size': {'osz': 0, '16': 1, '32': 2, '8': 3},
    'src_size': {'osz': 0, '16': 1, '32': 2, '8': 3},
    'mbytes': {'osz': 0, '2': 1, '4': 2, '1': 3},
    'mkind': {'none': 0, 'load': 1, 'store': 2, 'rmw': 3},
    'd_space': {'gpr': 0, 'temp': 1, 'seg': 2},
    'stk': {'none': 0, 'push': 1, 'pop': 2, 'peek': 5, 'retf': 6, 'probe': 7},
    'probe': {'none': 0, 'int': 1, 'pusha': 2, 'popa': 3},
    'b': {'src2': 0, 'eip': 1, 't1': 2, 'sel': 3},
    'seg': SEG,
}
# Shorthands: a register as a source or destination.
REGS = {'s1': 's1_idx', 's2': 's2_idx'}


class UcodeError(ValueError):
    pass


def encode(uop: dict, last: bool, where: str) -> int:
    values = {name: 0 for name, _ in FIELDS}
    for key, value in uop.items():
        if key in REGS:
            if value not in GPR:
                raise UcodeError(f'{where}: {key} = {value!r} is not a GPR')
            values[key] = 1
            values[REGS[key]] = GPR[value]
        elif key == 'd':
            # A GPR, T0 or T1, or a segment register.
            if value in GPR:
                values.update(d=1, d_space=0, d_idx=GPR[value])
            elif value in ('t0', 't1'):
                values.update(d=1, d_space=1, d_idx=8 if value == 't1' else 0)
            elif value in SEG:
                values.update(d=1, d_space=2, d_idx=SEG[value])
            else:
                raise UcodeError(f'{where}: d = {value!r} is not a register')
        elif key in values and key != 'last':
            if isinstance(value, bool):
                values[key] = int(value)
            elif isinstance(value, int):
                values[key] = value
            elif key in VALUES and str(value) in VALUES[key]:
                values[key] = VALUES[key][str(value)]
            else:
                raise UcodeError(f'{where}: {key} = {value!r} is not a known value')
        else:
            raise UcodeError(f'{where}: unknown field {key!r}')
    values['last'] = int(last)
    word = 0
    for name, width in FIELDS:
        if not 0 <= values[name] < 1 << width:
            raise UcodeError(f'{where}: {name} = {values[name]} does not fit {width} bits')
        word = word << width | values[name]
    return word


def assemble(table: dict) -> tuple[list[int], dict[str, int]]:
    words: list[int] = []
    entries: dict[str, int] = {}
    routines = table.get('routine', [])
    if not routines:
        raise UcodeError('no routines')
    for routine in routines:
        name = routine.get('name')
        if not isinstance(name, str) or not name.isidentifier():
            raise UcodeError(f'bad routine name {name!r}')
        uops = routine.get('uops', [])
        if not uops:
            raise UcodeError(f'{name}: no uops')
        # An entry may fall into the routine after it (INTO into INT).
        falls = bool(routine.get('falls_through', False))
        if name.upper() in entries:
            raise UcodeError(f'{name}: entry defined twice')
        entries[name.upper()] = len(words)
        for n, uop in enumerate(uops):
            words.append(encode(uop, n == len(uops) - 1 and not falls, f'{name}[{n}]'))
    if len(words) > DEPTH:
        raise UcodeError(f'{len(words)} words do not fit the {DEPTH}-word ROM')
    if routines[-1].get('falls_through', False):
        raise UcodeError(f'{routines[-1]["name"]}: falls through past the last routine')
    return words, entries


HEADER = ['// SPDX-License-Identifier: MIT',
          '// Generated by scripts/fes_i386_ucode.py from ucode.toml; do not edit.', '//']


def render(words: list[int], entries: dict[str, int]) -> dict[Path, str]:
    digits = (WIDTH + 3) // 4
    abits = (DEPTH - 1).bit_length()
    package = HEADER + [
        '// The microcode routines\' entry addresses in fes_i386_ucode_rom.',
        'package fes_i386_ucode_pkg;',
        f'    localparam int UC_ABITS = {abits};',
    ]
    package += [f'    localparam logic [{abits - 1}:0] UC_{name} = {abits}\'d{address};' for name, address in entries.items()]
    package += ['endpackage', '']
    lines = HEADER + [
        '// The microcode ROM: fes_i386_pkg::ucode_t words, read as an M10K with a',
        '// registered output.',
        'module fes_i386_ucode_rom (',
        '    input  logic                                     clk,',
        '    input  logic [fes_i386_ucode_pkg::UC_ABITS-1:0] addr,',
        '    output fes_i386_pkg::ucode_t                     q',
        ');',
        f'    logic [{WIDTH - 1}:0] rom [0:{DEPTH - 1}];',
        '    initial begin',
    ]
    lines += [f'        rom[{n}] = {WIDTH}\'h{word:0{digits}x};' for n, word in enumerate(words)]
    lines += [f'        for (int n = {len(words)}; n < {DEPTH}; n++) rom[n] = \'0;',
              '    end',
              '    always_ff @(posedge clk) q <= rom[addr];',
              'endmodule',
              '']
    return {PACKAGE: '\n'.join(package), ROM: '\n'.join(lines)}


def generate() -> dict[Path, str]:
    with SOURCE.open('rb') as stream:
        table = tomllib.load(stream)
    words, entries = assemble(table)
    return render(words, entries)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--check', action='store_true', help='fail if the checked-in ROM differs')
    args = parser.parse_args()
    try:
        files = generate()
    except UcodeError as error:
        print(f'ucode.toml: {error}', file=sys.stderr)
        return 1
    for path, text in files.items():
        if args.check:
            if not path.exists() or path.read_text() != text:
                print(f'{path.relative_to(ROOT)} is out of date; run scripts/fes_i386_ucode.py', file=sys.stderr)
                return 1
        else:
            path.write_text(text)
    return 0


if __name__ == '__main__':
    sys.exit(main())
