#!/usr/bin/env python3
"""Compare a second measurement pass against the committed first pass.

Only cores the second pass actually wrote are considered: comparing a file
against its own copy proves nothing.
"""
import json, os, sys

HERE = os.path.dirname(os.path.abspath(__file__))
REPORTS = os.path.join(HERE, "reports")
# Both default to the committed reports so the command runs from any checkout.
PASS2 = sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, "reports-pass2")
first = {}
for name in os.listdir(REPORTS):
    path = os.path.join(REPORTS, name, "netlist.json")
    if os.path.exists(path):
        first[name] = json.load(open(path))["netlist_sha256"]
rows, bad = [], 0
for name in sorted(os.listdir(PASS2)):
    path = os.path.join(PASS2, name, "netlist.json")
    if not os.path.exists(path):
        continue
    second = json.load(open(path))["netlist_sha256"]
    was = first.get(name)
    if was is None:
        rows.append(f"  {name:26s} pass1 MISSING   {second[:16]}"); bad += 1
    elif was == second:
        rows.append(f"  {name:26s} AGREES          {second[:16]}")
    else:
        rows.append(f"  {name:26s} ** DISAGREES ** pass1={was[:16]} pass2={second[:16]}"); bad += 1
print("\n".join(rows))
print(f"\n{len(rows)} cores compared, {bad} problem(s)")
sys.exit(1 if bad else 0)
