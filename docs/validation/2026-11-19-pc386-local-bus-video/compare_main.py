#!/usr/bin/env python3
"""Method.md host gate 3: max and presets 1-5 are charged as on main.

usage: python3 compare_main.py MAIN_REPORT.json BRANCH_REPORT.json > compare-main.json

Both are scripts/test_fes_i386_governor.py report.json files, main's from the merge base. Every loop's
`charged` must be equal under max and presets 1-5, except `vram`, which this branch's bench extends by
two stores in each of its 200 passes (`mov [es:di + 3], ax`, two 16-bit halves and two 32-bit dwords of
VGA memory, and `mov [fs:0], ax`, one half and one dword of the EMS frame, method.md amendment 1):
there the difference must be exactly 200 times those stores' ISA charges, as the bench computes them.
Run from the repository root (it imports the branch's bench and governor table).
"""
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path('sources/misteross').resolve()))
from scripts import fes_i386_governor as governor  # noqa: E402
from scripts import test_fes_i386_governor as bench  # noqa: E402

ADDED = [bench.m('mov_store', 1, vga=((2, 2),)), bench.m('mov_store', 1, vga=((1, 1, False),))]
PASSES = 200


def main() -> int:
    main_report, branch = (json.load(open(path)) for path in sys.argv[1:3])
    table = governor.load()
    out = {'main': sys.argv[1], 'branch': sys.argv[2], 'presets': {}, 'failures': []}
    rows = [('max', [s['charged'] for s in main_report['max']['spans']], [s['charged'] for s in branch['max']['spans']],
             [s['name'] for s in branch['max']['spans']], 0)]
    for preset in table.presets[:5]:
        a, b = main_report['presets'][preset.name]['loops'], branch['presets'][preset.name]['loops']
        rows.append((preset.name, [loop['charged'] for loop in a], [loop['charged'] for loop in b],
                     [loop['name'] for loop in b], PASSES * sum(bench.charge(table, preset, op) for op in ADDED)))
    for name, before, after, loops, vram_delta in rows:
        entry = {'loops': len(after), 'vram_delta': None, 'vram_delta_expected': vram_delta, 'equal_elsewhere': True}
        if len(before) != len(after):
            out['failures'].append(f'{name}: {len(before)} loops on main, {len(after)} here')
        for loop, x, y in zip(loops, before, after):
            if loop == 'vram':
                entry['vram_delta'] = y - x
                if y - x != vram_delta:
                    out['failures'].append(f'{name} vram: charged {y - x} more than main, expected {vram_delta}')
            elif x != y:
                entry['equal_elsewhere'] = False
                out['failures'].append(f'{name} {loop}: charged {y}, main {x}')
        out['presets'][name] = entry
    out['verdict'] = 'FAIL' if out['failures'] else 'PASS'
    json.dump(out, sys.stdout, indent=1)
    print()
    return 1 if out['failures'] else 0


if __name__ == '__main__':
    sys.exit(main())
