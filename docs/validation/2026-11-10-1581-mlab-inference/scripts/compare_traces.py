#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Spike S1d (#1581): compare a variant's bench trace with the current form's.

usage: compare_traces.py CURRENT VARIANT LATENCY [SKIP]

Each line is "<requests> <set_ptr> <set_lin> <push_lin> <top>". The inputs
must match line for line (same stimulus); the variant's top at clock t must
equal the current form's top at clock t - LATENCY. The first SKIP clocks are
not compared (a registered form has no defined top before its first edge).
"""
import sys


def load(path):
    rows = []
    with open(path, encoding="ascii") as handle:
        for line in handle:
            parts = line.split()
            if len(parts) == 5 and len(parts[0]) == 5:
                rows.append((" ".join(parts[:4]), parts[4]))
    return rows


def main():
    current, variant = load(sys.argv[1]), load(sys.argv[2])
    latency = int(sys.argv[3])
    skip = int(sys.argv[4]) if len(sys.argv) > 4 else latency + 1
    if not current or len(current) != len(variant):
        sys.exit(f"FAIL: {len(current)} and {len(variant)} clocks")
    if any(a[0] != b[0] for a, b in zip(current, variant)):
        sys.exit("FAIL: the stimulus differs")
    compared = 0
    for t in range(max(skip, latency), len(variant)):
        if variant[t][1] != current[t - latency][1]:
            sys.exit(f"FAIL: clock {t}: top {variant[t][1]}, expected {current[t - latency][1]}")
        compared += 1
    # reset, redirect, set_wr, push, pop
    writes = sum(1 for i, _ in current if i[0] == "0" and (i[2] == "1" if i[1] == "1" else i[3] == "1"))
    resets = sum(1 for i, _ in current if i[0] == "1")
    print(f"PASS: {compared} clocks compared at latency {latency} ({writes} writes, {resets} reset clocks)")


if __name__ == "__main__":
    main()
