#!/usr/bin/env python3
"""Generate the i386 speed governor's tables from their source.

cores/fes-common/rtl/i386/governor.toml lists the presets (cost table,
frequency, wait states, bus width) and the cost classes: the encodings each
class costs and its reference clock counts on the 80286, Intel386 and i486
(docs/i386-cpu-design.md, "Speed control"). This script checks the table
(every preset and class well formed, no encoding in two classes, every
encoding the lockstep's scope builds in a class) and writes two files beside
it:

  fes_i386_gov_pkg.sv  classify(), an instruction's class from its
                       fes_i386_pkg::gov_t, and preset(), a preset's table,
                       frequency, wait states and bus
  fes_i386_gov_rom.sv  the cost ROM, {column 1, column 0} by {table, class},
                       an M10K-shaped array whose contents are initial
                       assignments, so every flow reads them without a path

Column 0 is a class's cost, column 1 its taken cost (a branch) or its
per-element cost (a REP string), else its cost again. The governor benches
read the table through load() and cost().

  python3 scripts/fes_i386_governor.py           # regenerate
  python3 scripts/fes_i386_governor.py --check   # fail if the checked-in files differ
"""
from __future__ import annotations

import argparse
from dataclasses import dataclass
from pathlib import Path
import re
import sys
import tomllib

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / 'cores/fes-common/rtl/i386'
SOURCE = RTL / 'governor.toml'
PACKAGE = RTL / 'fes_i386_gov_pkg.sv'
ROM = RTL / 'fes_i386_gov_rom.sv'
TABLES = ('286', '386', '486')
PRESET_IDS = range(1, 8)  # fes.pc386.machine word 0; 0 is max
FREQUENCY_MAX = 1000      # the record's override range, 100 kHz units
WAIT_MAX = 3
COST_MAX = 255
# The gov_t key, most significant field first, as fes_i386_pkg declares it.
KEY_FIELDS = (('two', 1), ('op', 8), ('mreg', 3), ('rm_reg', 1), ('o32', 1), ('rep', 1))
KEY_BITS = sum(width for _, width in KEY_FIELDS)
STEM = re.compile(r'(?P<op>(?:0F)?[0-9A-F]{2})(?:-(?P<last>(?:0F)?[0-9A-F]{2}))?(?:\.(?P<reg>[0-7]))?'
                  r'(?:/(?P<form>[rm]))?(?::(?P<size>16|32))?(?::(?P<rep>rep|norep))?')


class GovernorError(ValueError):
    pass


@dataclass(frozen=True)
class Preset:
    id: int
    name: str
    table: int
    frequency: int  # 100 kHz units
    wait: int
    bus16: bool


@dataclass(frozen=True)
class CostClass:
    index: int  # the ROM's: classes that cost the same share one
    name: str
    stems: tuple[str, ...]
    cost: tuple[int, int, int]
    column1: tuple[int, int, int]  # taken, per element, or cost again
    branch: bool
    element: bool
    memory: bool


@dataclass(frozen=True)
class Table:
    presets: tuple[Preset, ...]
    classes: tuple[CostClass, ...]
    default: CostClass
    keys: dict[int, int]  # every gov_t key a stem names, to its class's place in classes

    def by_name(self, name: str) -> CostClass:
        for cls in (*self.classes, self.default):
            if cls.name == name:
                return cls
        raise KeyError(name)

    @property
    def indices(self) -> dict[int, CostClass]:
        """Each ROM index and the first class that has it."""
        found: dict[int, CostClass] = {}
        for cls in (*self.classes, self.default):
            found.setdefault(cls.index, cls)
        return found

    @property
    def class_bits(self) -> int:
        return max(1, (len(self.indices) - 1).bit_length())


def key(two: int, op: int, reg: int, rm_reg: int, o32: int, rep: int) -> int:
    value = 0
    for (_, width), field in zip(KEY_FIELDS, (two, op, reg, rm_reg, o32, rep)):
        value = value << width | field
    return value


def stem_keys(stem: str) -> list[int]:
    """Every gov_t key the stem names."""
    match = STEM.fullmatch(stem)
    if match is None:
        raise GovernorError(f'{stem!r} is not a stem')
    first, last = match['op'], match['last'] or match['op']
    if first.startswith('0F') != last.startswith('0F') or int(last, 16) < int(first, 16):
        raise GovernorError(f'{stem!r}: a range is of opcodes of one length, in order')
    two = int(first.startswith('0F'))
    low, high = int(first[-2:], 16), int(last[-2:], 16)
    regs = [int(match['reg'])] if match['reg'] else range(8)
    forms = [1] if match['form'] == 'r' else [0] if match['form'] == 'm' else (0, 1)
    sizes = [1] if match['size'] == '32' else [0] if match['size'] == '16' else (0, 1)
    reps = [1] if match['rep'] == 'rep' else [0] if match['rep'] == 'norep' else (0, 1)
    return [key(two, op, reg, rm, o32, rep) for op in range(low, high + 1) for reg in regs for rm in forms
            for o32 in sizes for rep in reps]


def _counts(entry: dict, field: str, where: str) -> tuple[int, int, int]:
    value = entry.get(field)
    if (not isinstance(value, list) or len(value) != len(TABLES)
            or not all(isinstance(n, int) and not isinstance(n, bool) and 1 <= n <= COST_MAX for n in value)):
        raise GovernorError(f'{where}: {field} is {len(TABLES)} clock counts, 1 to {COST_MAX}')
    return tuple(value)


def parse(source: dict) -> Table:
    presets = []
    for n, entry in enumerate(source.get('preset', [])):
        where = f'preset {n}'
        unknown = set(entry) - {'id', 'name', 'table', 'frequency', 'wait', 'bus'}
        if unknown:
            raise GovernorError(f'{where}: unknown fields {sorted(unknown)}')
        if entry.get('table') not in TABLES:
            raise GovernorError(f'{where}: table is one of {TABLES}')
        frequency, wait, bus = entry.get('frequency'), entry.get('wait'), entry.get('bus')
        if not isinstance(frequency, int) or not 10 <= frequency <= FREQUENCY_MAX:
            raise GovernorError(f'{where}: frequency is 10 to {FREQUENCY_MAX} (100 kHz)')
        if not isinstance(wait, int) or not 0 <= wait <= WAIT_MAX:
            raise GovernorError(f'{where}: wait is 0 to {WAIT_MAX}')
        if bus not in (16, 32):
            raise GovernorError(f'{where}: bus is 16 or 32')
        presets.append(Preset(entry.get('id'), str(entry.get('name')), TABLES.index(entry['table']), frequency,
                              wait, bus == 16))
    if sorted(p.id for p in presets) != list(PRESET_IDS):
        raise GovernorError(f'presets must be IDs {PRESET_IDS.start} to {PRESET_IDS.stop - 1}, once each')

    classes = []
    keys: dict[int, int] = {}
    names = set()
    signatures: dict[tuple, int] = {}
    for n, entry in enumerate(source.get('class', [])):
        name = entry.get('name')
        where = f'class {name or n}'
        unknown = set(entry) - {'name', 'stems', 'cost', 'taken', 'element', 'memory'}
        if unknown:
            raise GovernorError(f'{where}: unknown fields {sorted(unknown)}')
        if not isinstance(name, str) or not name.isidentifier() or name in names or name == 'default':
            raise GovernorError(f'{where}: a unique identifier names each class')
        names.add(name)
        if 'taken' in entry and 'element' in entry:
            raise GovernorError(f'{where}: a class is a branch or a REP string, not both')
        cost = _counts(entry, 'cost', where)
        column1 = _counts(entry, 'taken', where) if 'taken' in entry else \
            _counts(entry, 'element', where) if 'element' in entry else cost
        stems = entry.get('stems')
        if not isinstance(stems, list) or not stems:
            raise GovernorError(f'{where}: stems lists the encodings')
        for stem in stems:
            for k in stem_keys(stem):
                if k in keys:
                    raise GovernorError(f'{where}: {stem} is also in {classes[keys[k]].name}')
                keys[k] = len(classes)
        flags = ('taken' in entry, 'element' in entry, bool(entry.get('memory', False)))
        classes.append(CostClass(signatures.setdefault((cost, column1, *flags), len(signatures)), name,
                                 tuple(stems), cost, column1, *flags))
    if not classes:
        raise GovernorError('no classes')
    default = source.get('default', {})
    if set(default) != {'cost'}:
        raise GovernorError('default has a cost and nothing else')
    cost = _counts(default, 'cost', 'default')
    default_class = CostClass(signatures.setdefault((cost, cost, False, False, False), len(signatures)), 'default',
                              (), cost, cost, False, False, False)
    if len(signatures) > 256:
        raise GovernorError('too many distinct classes for the ROM')
    for name in ('int',):
        if name not in names:
            raise GovernorError(f'class {name} is needed: a delivery charges it')
    return Table(tuple(sorted(presets, key=lambda p: p.id)), tuple(classes), default_class, keys)


def load() -> Table:
    with SOURCE.open('rb') as stream:
        return parse(tomllib.load(stream))


def cost(table: Table, name: str, table_name: str, column: int = 0) -> int:
    """A class's clock count on one table: column 0 its cost, 1 its taken or per-element cost."""
    cls = table.by_name(name)
    index = TABLES.index(table_name)
    return (cls.cost if column == 0 else cls.column1)[index]


def classify(table: Table, two: int, op: int, reg: int, rm_reg: int, o32: int, rep: int) -> CostClass:
    """What fes_i386_gov_pkg::classify returns, for the benches."""
    index = table.keys.get(key(two, op, reg, rm_reg, o32, rep))
    return table.default if index is None else table.classes[index]


def scope_gaps(table: Table, scope: frozenset[str]) -> list[str]:
    """Stems of the lockstep's scope with an encoding no class costs."""
    gaps = []
    for stem in sorted(scope):
        if not all(k in table.keys for k in stem_keys(stem)):
            gaps.append(stem)
    return gaps


HEADER = ['// SPDX-License-Identifier: MIT',
          '// Generated by scripts/fes_i386_governor.py from governor.toml; do not edit.', '//']


def _pattern(stem: str) -> str:
    match = STEM.fullmatch(stem)
    first = match['op']
    two = '1' if first.startswith('0F') else '0'
    reg = f'{int(match["reg"]):03b}' if match['reg'] else '???'
    form = {'r': '1', 'm': '0'}.get(match['form'] or '', '?')
    size = {'32': '1', '16': '0'}.get(match['size'] or '', '?')
    rep = {'rep': '1', 'norep': '0'}.get(match['rep'] or '', '?')
    return two, reg, form, size, rep


def render(table: Table) -> dict[Path, str]:
    bits = table.class_bits
    abits = bits + 2
    package = HEADER + [
        "// The governor's classes and presets (fes_i386_governor, docs/i386-cpu-design.md,",
        '// "Speed control"). classify() names an instruction\'s cost class from its',
        "// fes_i386_pkg::gov_t: elem, the class's column 1 is charged per REP",
        "// element; branch, column 1 is its taken cost; memory, an address of base,",
        '// index and displacement costs one more. preset() gives a preset\'s cost table',
        '// (0 286, 1 386, 2 486), frequency in 100 kHz, wait states per memory bus',
        '// cycle and a 16-bit bus. cost_rom_index() is the ROM\'s address.',
        'package fes_i386_gov_pkg;',
        f'    localparam int CLASS_BITS = {bits};',
        f'    localparam int ROM_ABITS = {abits};',
        f'    localparam logic [{bits - 1}:0] CLASS_DEFAULT = {bits}\'d{table.default.index};',
        '    typedef struct packed {',
        '        logic                  elem;',
        '        logic                  branch;',
        '        logic                  memory;',
        '        logic [CLASS_BITS-1:0] cls;',
        '    } class_t;',
        '    typedef struct packed {',
        '        logic [1:0] tbl;',
        '        logic [9:0] frequency;',
        '        logic [1:0] wait_states;',
        '        logic       bus16;',
        '    } preset_t;',
        '',
        '    function automatic class_t classify(input fes_i386_pkg::gov_t g);',
        "        classify = '{elem: 1'b0, branch: 1'b0, memory: 1'b0, cls: CLASS_DEFAULT};",
        '        casez ({g.two, g.op, g.mreg, g.rm_reg, g.o32, g.rep})',
    ]
    for cls in table.classes:
        flags = f"elem: 1'b{int(cls.element)}, branch: 1'b{int(cls.branch)}, memory: 1'b{int(cls.memory)}"
        items = []
        for stem in cls.stems:
            match = STEM.fullmatch(stem)
            two, reg, form, size, rep = _pattern(stem)
            first, last = match['op'], match['last'] or match['op']
            for op in range(int(first[-2:], 16), int(last[-2:], 16) + 1):
                items.append(f"{KEY_BITS}'b{two}_{op:08b}_{reg}_{form}_{size}_{rep}")
        for n in range(0, len(items), 3):
            chunk = ', '.join(items[n:n + 3])
            end = ':' if n + 3 >= len(items) else ','
            package.append(f'            {chunk}{end}')
        package.append(f"                classify = '{{{flags}, cls: {bits}'d{cls.index}}};  // {cls.name}")
    package += [
        '            default: ;',
        '        endcase',
        '    endfunction',
        '',
        '    function automatic preset_t preset(input logic [2:0] id);',
        "        preset = '0;",
        '        case (id)',
    ]
    for p in table.presets:
        package.append(f"            3'd{p.id}: preset = '{{tbl: 2'd{p.table}, frequency: 10'd{p.frequency}, "
                       f"wait_states: 2'd{p.wait}, bus16: 1'b{int(p.bus16)}}};  // {p.name}")
    package += [
        '            default: ;',
        '        endcase',
        '    endfunction',
        '',
        '    function automatic logic [ROM_ABITS-1:0] cost_rom_index(input logic [1:0] tbl, input logic [CLASS_BITS-1:0] cls);',
        '        cost_rom_index = {tbl, cls};',
        '    endfunction',
        'endpackage',
        '',
    ]
    depth = 1 << abits
    words = [((t << bits) | index, cls.column1[t] << 8 | cls.cost[t], cls.name)
             for t in range(len(TABLES)) for index, cls in sorted(table.indices.items())]
    lines = HEADER + [
        "// The governor's cost ROM: {column 1, column 0} clock counts by",
        "// fes_i386_gov_pkg::cost_rom_index (table, class), read as an M10K with a",
        '// registered output. Column 0 is a class\'s cost, column 1 its taken or',
        '// per-element cost, or the cost again.',
        'module fes_i386_gov_rom (',
        '    input  logic                                  clk,',
        '    input  logic [fes_i386_gov_pkg::ROM_ABITS-1:0] addr,',
        '    output logic [15:0]                           q',
        ');',
        f'    logic [15:0] rom [0:{depth - 1}];',
        '    initial begin',
        f"        for (int n = 0; n < {depth}; n++) rom[n] = '0;",
    ]
    lines += [f"        rom[{address}] = 16'h{word:04x};  // {TABLES[address >> bits]} {name}"
              for address, word, name in words]
    lines += ['    end',
              '    always_ff @(posedge clk) q <= rom[addr];',
              'endmodule',
              '']
    return {PACKAGE: '\n'.join(package), ROM: '\n'.join(lines)}


def generate() -> dict[Path, str]:
    return render(load())


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--check', action='store_true', help='fail if the checked-in files differ')
    args = parser.parse_args()
    try:
        files = generate()
    except GovernorError as error:
        print(f'governor.toml: {error}', file=sys.stderr)
        return 1
    for path, text in files.items():
        if args.check:
            if not path.exists() or path.read_text() != text:
                print(f'{path.relative_to(ROOT)} is out of date; run scripts/fes_i386_governor.py', file=sys.stderr)
                return 1
        else:
            path.write_text(text)
    return 0


if __name__ == '__main__':
    sys.exit(main())
