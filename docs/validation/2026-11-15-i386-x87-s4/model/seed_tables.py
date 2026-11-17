#!/usr/bin/env python3
"""Emit the x87 slice 4 seed tables as SystemVerilog literals.

The locked Yosys rejects `real` in a module body, so the tables cannot be
computed by an `initial` loop the way a Verilator-only design could: the
netlist bench failed with "syntax error, unexpected TOK_REAL" on the first
try. They are literals instead, and this script is what produces them, so the
numbers in the RTL are reproducible rather than magic.

  python3 seed_tables.py            # print the two initial blocks
  python3 seed_tables.py --check f  # verify f holds exactly these values
"""
import math, sys, re

def rcp():
    "1/b for b = 0.5 + (i + 0.5)/256, as v/512: the reciprocal seed."
    return [min(1023, int(512.0 / (0.5 + (i + 0.5) / 256.0) + 0.5)) for i in range(128)]

def rsq():
    "1/sqrt(a) for a = (1 + (i + 0.5)/128) * 2^p, as v/1024."
    out = []
    for p in range(2):
        for i in range(128):
            a = (1.0 + (i + 0.5) / 128.0) * (2.0 if p else 1.0)
            out.append(min(1023, int(1024.0 / math.sqrt(a) + 0.5)))
    return out

def block(name, values, per_line=8):
    lines = []
    for n in range(0, len(values), per_line):
        chunk = values[n:n + per_line]
        lines.append('        ' + ' '.join(
            f"{name}[{n + k}] = 10'd{v};" for k, v in enumerate(chunk)))
    return '\n'.join(lines)

if __name__ == '__main__':
    if len(sys.argv) > 2 and sys.argv[1] == '--check':
        text = open(sys.argv[2]).read()
        bad = 0
        for name, values in (('rcp_tab', rcp()), ('rsq_tab', rsq())):
            for i, v in enumerate(values):
                if f"{name}[{i}] = 10'd{v};" not in text:
                    print(f"{name}[{i}] is not 10'd{v}")
                    bad += 1
        print(f"{bad} mismatches" if bad else
              'both tables in the RTL hold exactly these values')
        sys.exit(1 if bad else 0)
    print('    initial begin')
    print(block('rcp_tab', rcp()))
    print('    end')
    print('    initial begin')
    print(block('rsq_tab', rsq()))
    print('    end')
