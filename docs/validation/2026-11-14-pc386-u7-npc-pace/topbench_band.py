#!/usr/bin/env python3
"""topbench_band.py DATABASE.INI — the real-386DX class, scaled to 25 MHz.

fes#1853. TopBench 0.38a ships a database of submissions from real machines
(`DATABASE.INI`, on the benchmark drive the kit's presets were measured from).
It holds no 386DX at 25 MHz at all, so `method.md` reads #1838's "mid-range
among real machines of its class" rule as: take every AT-class real 386DX
entry at whatever clock and scale its score to 25 MHz in proportion to its
clock. This prints that distribution, and is committed with the record so the
figures in it can be rederived.

"AT-class" excludes an accelerator card in an XT, as #1838 excluded one from
its 286 class; here that is the Inboard 386 in an IBM PC/XT. 386SX and the
cached 386SLC are different classes and are excluded by the CPU string.

The database is not committed: it is the copy on the benchmark drive.
"""
import json
import re
import statistics
import sys

TARGET_MHZ = 25.0


def entries(path):
    text = open(path, encoding='latin-1').read()
    for block in re.split(r'\n\[', text)[1:]:
        fields = {}
        for line in block.splitlines()[1:]:
            if '=' in line:
                key, value = line.split('=', 1)
                fields[key.strip()] = value.strip()
        yield fields


def clock(text):
    match = re.match(r'([\d.]+)', text or '')
    return float(match.group(1)) if match else None


def main(path):
    rows = []
    for fields in entries(path):
        cpu = fields.get('CPU', '')
        if '386' not in cpu or 'SX' in cpu.upper() or 'SLC' in cpu.upper():
            continue
        machine = fields.get('Machine', '')
        rows.append({'cpu': cpu, 'mhz': clock(fields.get('CPUspeed', '')),
                     'score': int(fields['Score']), 'machine': machine,
                     'at_class': 'Inboard' not in machine})
    at = [r for r in rows if r['at_class']]
    scaled = sorted(r['score'] / r['mhz'] * TARGET_MHZ for r in at)
    quart = statistics.quantiles(scaled, n=4)
    out = {'target_mhz': TARGET_MHZ, 'entries': rows,
           'at_class_count': len(at),
           'scaled_to_target': [round(v, 1) for v in scaled],
           'min': round(scaled[0], 1), 'q1': round(quart[0], 1),
           'median': round(statistics.median(scaled), 1),
           'q3': round(quart[2], 1), 'max': round(scaled[-1], 1)}
    json.dump(out, sys.stdout, indent=1)
    print()


if __name__ == '__main__':
    main(sys.argv[1])
