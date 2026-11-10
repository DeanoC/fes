#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-or-later
"""Replay scripts recorded with the Sinclair ZX80 ROM on the FES ZX80 RTL
(ref/*.txt, made with zx80_probe_tb.cpp: memory and screen dumps, no ROM
bytes) on the open ROM in the fast harness; report differences in program,
variables, edit line and screen."""
import io
import sys
sys.path.insert(0, sys.path[0])

REF = sys.path[0] + '/ref/'
FILES = ['p1', 'p2', 'p3', 'p4', 'p5', 'pb', 'pc', 'pd', 'pe', 'pf', 'pg', 'ph', 'pr']

# Recordings where this ROM deliberately differs (README): what it gives
# instead. After an error Sinclair's ROM carries on through the statement with
# its effects suppressed, so PRINT's comma still moves on and a later error
# replaces the report; this ROM stops at the first error.
DELIBERATE = {
    'D C SHIFT+I 3 0 0 SHIFT+O NL SHIFT+9 K C SHIFT+I 2 9 9 SHIFT+O SHIFT+L 5 NL '
    'SHIFT+9 O C SHIFT+I 2 9 9 SHIFT+O SHIFT+. C SHIFT+I 4 3 SHIFT+O NL':
        {'screen': {23: '2/-2'}},       # Sinclair: 8 spaces on row 0, 3/-2
}


def blocks(path):
    out, cur = [], None
    for line in open(path):
        line = line.rstrip('\n')
        if line.startswith('=== '):
            cur = {'script': line[4:], 'screen': {}}
            out.append(cur)
        elif cur is None:
            continue
        elif line.startswith(('prog:', 'vars:', 'eline:')):
            k, v = line.split(':', 1)
            cur[k] = v.strip()
        elif line.startswith('  ') and '|' in line:
            n, rest = line.split('|', 1)
            cur['screen'][int(n)] = rest[:-1]
    for cur in out:
        cur.update(DELIBERATE.get(cur['script'], {}))
    return out


def mine(rom, sym, script, ram_top=0x8000, tape=None):
    from harness80 import Machine
    m = Machine(rom, sym, ram_top)
    m.tape = tape
    m.type('WAIT10 ' + script)
    m.run()
    out = io.StringIO()
    m.dump(out)
    got = {'screen': {}}
    for line in out.getvalue().splitlines():
        if line.startswith(('prog:', 'vars:', 'eline:')):
            k, v = line.split(':', 1)
            got[k] = v.strip()
        elif line.startswith('  ') and '|' in line:
            n, rest = line.split('|', 1)
            got['screen'][int(n)] = rest[:-1]
    return got, m


def main():
    from harness80 import load_symbols
    rom = open(sys.argv[1], 'rb').read()
    sym = load_symbols(sys.argv[2])
    files = sys.argv[3:] or FILES
    bad = 0
    for f in files:
        for ref in blocks(REF + f + '.txt'):
            try:
                got, m = mine(rom, sym, ref['script'])
            except Exception as exc:
                print(f'{f}: {ref["script"]}\n   CRASH {exc}')
                bad += 1
                continue
            diffs = []
            ref['eline'] = ' '.join(b for b in ref.get('eline', '').split() if b != 'b0')
            for k in ('prog', 'vars', 'eline'):
                if k == 'eline' and not ref.get(k):
                    continue            # a report: this ROM leaves the old line
                if ref.get(k, '') != got.get(k, ''):
                    diffs.append(f'{k}: want [{ref.get(k)}] got [{got.get(k)}]')
            rs = {r: t for r, t in ref['screen'].items() if t}
            gs = {r: t for r, t in got['screen'].items() if t}
            if rs != gs:
                diffs.append('screen want ' + repr(rs) + '\n          got  ' + repr(gs))
            if diffs:
                bad += 1
                print(f'{f}: {ref["script"]}')
                for d in diffs:
                    print('   ' + d)
    print('differences:', bad)
    sys.exit(1 if bad else 0)


if __name__ == '__main__':
    main()
