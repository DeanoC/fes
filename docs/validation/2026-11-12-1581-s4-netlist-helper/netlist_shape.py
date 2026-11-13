#!/usr/bin/env python3
"""Compare two Yosys Verilog netlists by structure alone: every identifier
becomes ID, string literals become "S", whitespace goes, the header comment
is dropped, and the remaining lines are compared as multisets. Two netlists
that differ only in identifier spelling and Yosys's auto-numbered names
compare equal.

  python3 netlist_shape.py A.v B.v
"""
import collections
import re
import sys

KEYWORDS = set('module endmodule input output inout wire reg assign always posedge negedge begin end if else '
               'case casez casex endcase default initial signed integer parameter localparam function '
               'endfunction for'.split())


def shape(path):
    text = open(path).read()
    text = re.sub(r'"[^"\n]*"', '"S"', text)
    text = re.sub(r'\\\S+', ' ID ', text)
    text = re.sub(r'\b[A-Za-z_][A-Za-z0-9_$]*\b', lambda m: m.group(0) if m.group(0) in KEYWORDS else 'ID', text)
    lines = (re.sub(r'\s+', '', line) for line in text.splitlines())
    return collections.Counter(line for line in lines if line and not line.startswith('/*IDIDID'))


def main():
    first, second = shape(sys.argv[1]), shape(sys.argv[2])
    only_first, only_second = first - second, second - first
    print(f'lines: {sum(first.values())} vs {sum(second.values())}; '
          f'line shapes only in the first: {sum(only_first.values())}, only in the second: {sum(only_second.values())}')
    for line, count in list(only_first.items())[:10]:
        print('first only', count, line[:160])
    for line, count in list(only_second.items())[:10]:
        print('second only', count, line[:160])
    return 1 if only_first or only_second else 0


if __name__ == '__main__':
    raise SystemExit(main())
