#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-or-later
"""Fast harness for the open ZX80 ROM (not cycle accurate).

The display is not run: a call of `frame` returns at once with E = the key
index the ROM's own keyboard read would produce, and FRAMES counted. LOAD
behaves like the FES core's trap. Screens print like the RTL probe.
"""
import sys
import z80

ROWS = [['SHIFT', 'Z', 'X', 'C', 'V'], ['A', 'S', 'D', 'F', 'G'],
        ['Q', 'W', 'E', 'R', 'T'], ['1', '2', '3', '4', '5'],
        ['0', '9', '8', '7', '6'], ['P', 'O', 'I', 'U', 'Y'],
        ['NL', 'L', 'K', 'J', 'H'], ['SP', '.', 'M', 'N', 'B']]
KEYS = {name: (r, c) for r, row in enumerate(ROWS) for c, name in enumerate(row)}
CH = " \"##########L$:?()-+*/=><;,.0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
assert len(CH) == 64


def text(c):
    if c < 64:
        return CH[c]
    if 128 <= c < 192:
        return '[' + CH[c & 63] + ']'
    return '<%02x>' % c


def load_symbols(path):
    out = {}
    for line in open(path):
        a, name = line.split()
        out[name] = int(a, 16)
    return out


class Machine:
    def __init__(self, rom, sym, ram_top=0x8000):
        self.sym = sym
        self.m = z80.Z80Machine()
        mem = bytearray(0x10000)
        mem[:len(rom)] = rom
        self.m.set_memory_block(0, bytes(mem))
        self.ram_top = ram_top
        self.pressed = set()
        self.script = []
        self.frames = 0
        self.tape = None
        self.ticks = 0
        self.m.set_input_callback(lambda port: 0xff)
        self.m.set_output_callback(lambda port, value: None)
        for name in ('frame', 'key_scan', 'load_entry', 'break_key'):
            self.m.set_breakpoint(sym[name])
        # RAM above ram_top reads $FF / ignores writes; 1 KiB echoes.
        size = ram_top - 0x4000
        mv = self.m.memory
        if ram_top < 0x8000:
            def rd(a):
                if 0x4000 <= a < 0x8000:
                    return mv[0x4000 + (a - 0x4000) % size]
                if a >= 0xc000:
                    return mv[0x4000 + (a - 0xc000) % size]
                return 0xff
            def wr(a, v):
                if 0x4000 <= a < 0x8000:
                    mv[0x4000 + (a - 0x4000) % size] = v
            self.m.set_read_callback(rd)
            self.m.set_write_callback(wr)
            self.m.mark_addrs(0x4000 + size, 0x10000 - 0x4000 - size,
                              z80.Z80Machine.READ_MARK | z80.Z80Machine.WRITE_MARK)

    def peek(self, a):
        return self.m.memory[a]

    def word(self, a):
        return self.m.memory[a] | self.m.memory[a + 1] << 8

    def key_index(self):
        e = 0xff
        base = 40 if 'SHIFT' in self.pressed else 0
        for r in range(8):
            cols = [c for k in self.pressed if k != 'SHIFT' for (rr, c) in [KEYS[k]] if rr == r]
            if cols:
                e = base + r * 5 + min(cols)
        return e

    def ret(self):
        sp = self.m.sp
        self.m.pc = self.word(sp)
        self.m.sp = (sp + 2) & 0xffff

    def next_frame(self):
        self.frames += 1
        if self.script:
            keys, n = self.script[0]
            self.pressed = keys
            if n <= 1:
                self.script.pop(0)
            else:
                self.script[0] = (keys, n - 1)
        else:
            self.pressed = set()
        k = self.key_index()
        self.m.de = (k * 257 + 1) & 0xffff      # a fingerprint of the keys
        f = (self.word(0x401e) + 1) & 0xffff
        self.m.memory[0x401e] = f & 0xff
        self.m.memory[0x401f] = f >> 8
        self.ret()

    def run(self, max_ticks=2_000_000_000):
        """Until the key script is used up and the ROM is idle in wait_key."""
        idle = 0
        while True:
            self.m.ticks_to_stop = 1_000_000
            events = self.m.run()
            self.ticks += 1_000_000 - self.m.ticks_to_stop
            if self.ticks > max_ticks:
                raise RuntimeError('tick limit, pc=%04x' % self.m.pc)
            if not events & 1:
                continue
            pc = self.m.pc
            if pc == self.sym['frame']:
                idle = 0 if self.script else idle + 1
                self.next_frame()
                if idle > 4:
                    return
            elif pc == self.sym['key_scan']:
                self.m.a = self.key_index()
                self.ret()
            elif pc == self.sym['load_entry']:
                ok = bool(self.tape)
                if ok:
                    for i, b in enumerate(self.tape):
                        self.m.memory[0x4000 + i] = b
                self.m.a = 0
                self.m.f |= 1
                self.m.pc = self.sym['load_back']
            elif pc == self.sym['break_key']:
                self.lines = getattr(self, 'lines', 0) + 1
                if self.lines % 64 == 0 and self.script:
                    keys, n = self.script[0]   # running lines take time too
                    self.pressed = keys
                    if n <= 1:
                        self.script.pop(0)
                    else:
                        self.script[0] = (keys, n - 1)
                brk = 'SHIFT' in self.pressed and 'SP' in self.pressed
                self.m.f = (self.m.f & ~1) | (0 if brk else 1)
                self.ret()
            else:
                self.m.step_over_breakpoint()

    def type(self, script):
        for w in script.split():
            hold = 3
            if w.startswith('HOLD'):
                n, w = w[4:].split(':')
                hold = int(n)
            if w.startswith('WAIT'):
                self.script.append((set(), int(w[4:])))
                continue
            self.script.append((set(w.split('+')), hold))
            self.script.append((set(), 3))

    def dump(self, out=sys.stdout):
        sysv = ' '.join('%02x' % self.peek(a) for a in range(0x4000, 0x4028))
        vars_, eline, dfile = self.word(0x4008), self.word(0x400a), self.word(0x400c)
        dfea, dfend = self.word(0x400e), self.word(0x4010)
        print('sys:', sysv, file=out)
        print('VARS=%x E_LINE=%x D_FILE=%x DF_EA=%x DF_END=%x' % (vars_, eline, dfile, dfea, dfend), file=out)
        print('prog:' + ''.join(' %02x' % self.peek(a) for a in range(0x4028, vars_)), file=out)
        print('vars:' + ''.join(' %02x' % self.peek(a) for a in range(vars_, eline)), file=out)
        print('eline:' + ''.join(' %02x' % self.peek(a) for a in range(eline, dfile)), file=out)
        print('screen:', file=out)
        for r, row in enumerate(self.screen()):
            print('  %2d|%s|' % (r, row), file=out)

    def screen(self):
        a = self.word(0x400c) + 1
        rows, row = [], ''
        end = self.word(0x4010)
        while a < end and len(rows) < 24:
            c = self.peek(a)
            a += 1
            if c == 0x76:
                rows.append(row)
                row = ''
            else:
                row += text(c)
        return rows


def probe(rom, sym, scripts, ram_top=0x8000, tape=None, out=sys.stdout):
    for s in scripts.split('|'):
        m = Machine(rom, sym, ram_top)
        m.tape = tape
        m.type('WAIT10 ' + s)
        m.run()
        print('===', s, file=out)
        m.dump(out)
        yield m


if __name__ == '__main__':
    rom = open(sys.argv[1], 'rb').read()
    sym = load_symbols(sys.argv[2])
    for _ in probe(rom, sym, sys.argv[3]):
        pass
