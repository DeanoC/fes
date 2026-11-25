#!/usr/bin/env python3
"""fit_summary.py SEED=PROJECT...: method.md's quantities from each seed's Quartus Pro project
directory (the one holding golden_top.qsf and output_files/), as JSON on stdout.

- alms: the fit report's whole-design `ALMs needed [=A-B+C]` (its first, decimal row);
- system_alms: the same column of the `|system|` hierarchy row, the machine (pc386_system);
- ram_blocks, dsp_blocks: the fit summary's totals;
- seed: golden_top.qsf's SEED;
- netlist_sha256: fes_mailbox/pc386_system.v;
- worst: the least slack of each analysis the timing analyzer's summary reports (setup,
  hold, recovery, removal, minimum pulse width, and the set_max_skew, set_net_delay and
  setup-data-delay checks) over every clock or target and corner, with where it is;
- closes: every worst slack >= 0.
"""
import hashlib
import json
import re
import sys
from pathlib import Path


def summary(project: Path) -> dict:
    out = project / 'output_files'
    fit = (out / 'golden_top.fit.rpt').read_text(errors='replace')
    alms = float(re.search(r'; ALMs needed \[=A-B\+C\]\s*;\s*([\d.,]+) / 46800', fit).group(1).replace(',', ''))
    # The machine: the hierarchy row of the board's `system` instance (pc386_system).
    system_alms = float(re.search(r'^;\s+\|system\|\s+;\s*([\d.]+) \(', fit, re.M).group(1))
    fsum = (out / 'golden_top.fit.summary').read_text()
    ram = int(re.search(r'Total RAM Blocks : (\d+)', fsum).group(1))
    dsp = int(re.search(r'Total DSP Blocks : (\d+)', fsum).group(1))
    seed = int(re.search(r'set_global_assignment -name SEED (\d+)', (project / 'golden_top.qsf').read_text()).group(1))
    text = (out / 'golden_top.sta.summary').read_text()
    rows = re.findall(r"Type\s*: (.+?) '(.+?)'\nSlack : (-?[\d.]+)(?:\nTNS\s*: -?[\d.]+)?(?:\nCorner: (.+))?", text)
    if not rows or len(rows) != text.count('\nType'):
        raise ValueError(f'{project}: {len(rows)} timing summary rows parsed of {text.count(chr(10) + "Type")}')
    worst = {}
    for kind, clock, slack, corner in rows:
        kind = kind.strip()
        # A max-skew row names its corner in the type.
        if kind.endswith(' Max Skew'):
            corner, kind = kind[:-len(' Max Skew')], 'Max Skew'

        if kind not in worst or float(slack) < worst[kind]['slack']:
            worst[kind] = {'slack': float(slack), 'clock': clock, 'corner': corner.strip()}
    netlist = hashlib.sha256((project / 'fes_mailbox/pc386_system.v').read_bytes()).hexdigest()
    return {'seed': seed, 'alms': alms, 'system_alms': system_alms, 'ram_blocks': ram, 'dsp_blocks': dsp, 'netlist_sha256': netlist,
            'worst': worst, 'closes': all(w['slack'] >= 0 for w in worst.values())}


if __name__ == '__main__':
    result = {}
    for arg in sys.argv[1:]:
        name, path = arg.split('=', 1)
        result[name] = summary(Path(path))
    print(json.dumps(result, indent=2, sort_keys=True))
