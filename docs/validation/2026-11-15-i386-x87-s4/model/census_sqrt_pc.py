#!/usr/bin/env python3
"""Derive the square root's and divide's precision mix from the COMMITTED census.

Written because this record quoted "164 of 68,626 square roots at PC 24" from
a figure it could not re-derive from anything in the tree -- Codex could not
reproduce it, and a repo-wide search found it only in this record's own
design.md. The number came from a later census run whose artifact is not here.

This script outputs the claim instead, from
docs/validation/2026-11-12-i386-x87-plan/census.jsonl, so the record's figures
are checker output rather than recollection.
"""
import collections, json, pathlib, sys

census = (pathlib.Path(__file__).resolve().parents[3]
          / 'validation/2026-11-12-i386-x87-plan/census.jsonl')

by_class = collections.defaultdict(collections.Counter)
for line in census.open():
    row = json.loads(line)
    for cls, pcs in (row.get('precision_by_class') or {}).items():
        for pc, n in pcs.items():
            by_class[cls][pc] += n

for cls in ('sqrt', 'div'):
    c = by_class[cls]
    total = sum(c.values())
    pc24 = c.get('24', 0)
    print(f"{cls}: {total} total; "
          + ", ".join(f"PC {k} = {v}" for k, v in sorted(c.items()))
          + f"  -> PC 24 is {pc24}/{total} = {100.0*pc24/total:.2f}%")
