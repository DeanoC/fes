#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-or-later
"""Regression scripts for the open ZX80 ROM in the fast harness.

  regress.py ROM SYM record   write regress.expected from this ROM
  regress.py ROM SYM          compare with regress.expected
"""
import json
import sys
sys.path.insert(0, sys.path[0])
from harness80 import Machine, load_symbols

HERE = sys.path[0]
D = 'SHIFT+9'   # any key dismisses a report


def q(text):
    """Keys for a string literal's characters (letters, digits, space)."""
    out = []
    for ch in text:
        out.append({' ': 'SP'}.get(ch, ch))
    return ' '.join(out)


TESTS = {
    'for': f'1 0 F I SHIFT+L 1 SHIFT+4 5 NL 2 0 O I SHIFT+X NL 3 0 N I NL R NL',
    'gosub': f'1 0 V 1 0 0 NL 2 0 O SHIFT+Y B SHIFT+Y NL 3 0 S NL 1 0 0 O SHIFT+Y A SHIFT+Y NL 1 1 0 B NL R NL',
    'cont': f'1 0 S NL 2 0 O SHIFT+Y Z SHIFT+Y NL R NL {D} T NL',
    'if': f'1 0 K A SHIFT+L 5 NL 2 0 U A SHIFT+L 5 SHIFT+3 O SHIFT+Y Y SHIFT+Y NL '
          f'3 0 U A SHIFT+N 5 SHIFT+3 O SHIFT+Y N SHIFT+Y NL 4 0 U A SHIFT+M 4 SHIFT+2 A SHIFT+N 6 SHIFT+3 O 7 NL R NL',
    'strings': f'K A SHIFT+U SHIFT+L SHIFT+Y A B SHIFT+Y NL {D} '
               f'O A SHIFT+U SHIFT+X T L SHIFT+U SHIFT+I A SHIFT+U SHIFT+O SHIFT+X C O D E SHIFT+I A SHIFT+U SHIFT+O '
               f'SHIFT+X C H R SHIFT+U SHIFT+I 4 0 SHIFT+O SHIFT+X S T R SHIFT+U SHIFT+I SHIFT+J 5 SHIFT+O '
               f'SHIFT+X A SHIFT+U SHIFT+N SHIFT+Y B SHIFT+Y SHIFT+X A SHIFT+U SHIFT+L SHIFT+Y A B SHIFT+Y NL',
    'strlet': f'K A SHIFT+U SHIFT+L SHIFT+Y X SHIFT+Y NL {D} K A SHIFT+U SHIFT+L A SHIFT+U NL {D} '
              f'K B SHIFT+U SHIFT+L S T R SHIFT+U SHIFT+I 1 2 SHIFT+O NL {D} K A SHIFT+U SHIFT+L B SHIFT+U NL {D} '
              f'O A SHIFT+U SHIFT+X B SHIFT+U NL',
    'array': f'1 0 D A SHIFT+I 5 SHIFT+O NL 2 0 F I SHIFT+L 0 SHIFT+4 5 NL 3 0 K A SHIFT+I I SHIFT+O SHIFT+L I SHIFT+P I NL '
             f'4 0 N I NL 5 0 O A SHIFT+I 4 SHIFT+O SHIFT+X A SHIFT+I 5 SHIFT+O NL 6 0 O A SHIFT+I 6 SHIFT+O NL R NL',
    'errors': f'G 1 0 0 0 NL {D} B NL {D} N A NL {D} O Q NL {D} O 1 SHIFT+V 0 NL {D} I A NL',
    'input': f'1 0 I A NL 2 0 I B SHIFT+U NL 3 0 O A SHIFT+P 2 SHIFT+X B SHIFT+U NL R NL 2 1 NL Q Q NL',
    'names': f'K A B C SHIFT+L 1 NL {D} K A B SHIFT+L 2 NL {D} K A SHIFT+L 3 NL {D} O A B C SHIFT+X A B SHIFT+X A NL',
    'arith': f'O SHIFT+J 7 SHIFT+V 2 SHIFT+X 7 SHIFT+H 2 SHIFT+X 1 SHIFT+K 2 SHIFT+P 3 SHIFT+X SHIFT+I 1 SHIFT+K 2 SHIFT+O SHIFT+P 3 '
             f'SHIFT+X SHIFT+1 0 SHIFT+X 5 SHIFT+2 3 SHIFT+X 5 SHIFT+B 2 SHIFT+X 3 2 7 6 7 SHIFT+J 1 NL',
    'edit': f'1 0 K A SHIFT+L 1 NL 2 0 K A SHIFT+L 2 NL 3 0 K A SHIFT+L 3 NL SHIFT+7 SHIFT+NL SHIFT+0 9 NL',
    'delete': f'1 0 K A SHIFT+L 1 NL 2 0 K A SHIFT+L 2 NL 2 0 NL',
    'cursor': f'O SHIFT+Y A C SHIFT+Y SHIFT+5 SHIFT+5 B SHIFT+8 SHIFT+8 SHIFT+0 NL',
    'goto': f'1 0 O 1 NL 2 0 G 4 0 NL 3 0 O 3 NL 4 0 O 4 NL R NL',
    'rnd': f'J 7 NL {D} O R N D SHIFT+I 1 0 0 0 SHIFT+O SHIFT+X R N D SHIFT+I 1 0 SHIFT+O NL',
    'poke': f'H 1 6 3 8 4 SHIFT+. 4 NL',
    'rem': f'1 0 Y SHIFT+Y SP SHIFT+H A NL 2 0 O SHIFT+Y R E M SHIFT+Y NL R NL',
    'clear': f'K A SHIFT+L 1 NL {D} X NL {D} O A NL',
    'list': f'1 0 Y NL 2 0 Y NL 3 0 Y NL A 2 0 NL',
    'break': f'1 0 G 1 0 NL R NL HOLD20:SHIFT+SP',
    'tab': f'O 1 SHIFT+. 2 SHIFT+. SHIFT+. 3 NL',
    'cls': f'1 0 O 1 NL 2 0 C NL 3 0 O 2 NL R NL',
    'syntax': f'K A SHIFT+L SHIFT+K',
    'for2': f'1 0 F I SHIFT+L 1 SHIFT+4 3 NL 2 0 F J SHIFT+L 1 SHIFT+4 2 NL 3 0 O I SHIFT+P J SHIFT+X NL 4 0 N J NL 5 0 N I NL R NL',
}

LOAD_TESTS = {
    'load': ('prog3.o', 'W NL {D} R NL'),
}


def run(rom, sym, script, tape=None, ram_top=0x8000):
    m = Machine(rom, sym, ram_top)
    m.tape = tape
    m.type('WAIT10 ' + script.replace('{D}', D))
    m.run()
    vars_, eline = m.word(0x4008), m.word(0x400a)
    return {
        'prog': ' '.join('%02x' % m.peek(a) for a in range(0x4028, vars_)),
        'vars': ' '.join('%02x' % m.peek(a) for a in range(vars_, eline)),
        'screen': [r for r in m.screen()],
    }


def main():
    rom = open(sys.argv[1], 'rb').read()
    sym = load_symbols(sys.argv[2])
    got = {}
    for name, script in TESTS.items():
        try:
            got[name] = run(rom, sym, script)
        except Exception as exc:
            got[name] = {'crash': str(exc)}
    ref_dir = HERE + '/ref/'
    for name, (tape, script) in LOAD_TESTS.items():
        try:
            got[name] = run(rom, sym, script, open(ref_dir + tape, 'rb').read())
        except Exception as exc:
            got[name] = {'crash': str(exc)}
    for ram in (0x4400,):
        name = 'small'
        script = ' '.join(f'{n} 0 O SHIFT+Y A B C D E F G H I J K L M N O P Q R S T U V W X Y Z SHIFT+Y NL'
                          for n in range(1, 10)) + ' R NL'
        try:
            got[name] = run(rom, sym, script, ram_top=ram)
        except Exception as exc:
            got[name] = {'crash': str(exc)}
        # Nesting deeper than the stack has room for is a wrong symbol, and
        # the edit line survives: RUBOUT it all and type another.
        name = 'deep'
        script = 'O ' + 'SHIFT+I ' * 150 + '1 NL ' + 'SHIFT+0 ' * 160 + 'O 7 NL'
        try:
            got[name] = run(rom, sym, script, ram_top=ram)
        except Exception as exc:
            got[name] = {'crash': str(exc)}
    path = HERE + '/regress.expected'   # JSON (the tree ignores *.json)
    if len(sys.argv) > 3 and sys.argv[3] == 'record':
        json.dump(got, open(path, 'w'), indent=1)
        for name, r in got.items():
            print('==', name, r.get('crash', ''))
            for row in r.get('screen', []):
                if row:
                    print('   |' + row + '|')
            print('   prog', r.get('prog'), '\n   vars', r.get('vars'))
        return
    want = json.load(open(path))
    bad = 0
    for name in want:
        if want[name] != got.get(name):
            bad += 1
            print('DIFF', name)
            print('  want', want[name])
            print('  got ', got.get(name))
    print('regressions:', bad, 'of', len(want))
    sys.exit(1 if bad else 0)


if __name__ == '__main__':
    main()
