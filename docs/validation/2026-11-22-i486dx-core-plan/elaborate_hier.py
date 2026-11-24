#!/usr/bin/env python3
"""Elaborate fes_i386_cpu with Yosys's flatten removed once and plain module names (method.md, amendment 3).

Run from a tree's sources/misteross. Uses the fit runner's own cpu_netlist, so only the flatten differs.

  YOSYS_MAX_THREADS=1 python3 elaborate_hier.py --root <tree>/sources/misteross \
      --cache-root <cache> --output <new dir>
"""
import argparse
import json
import sys
from pathlib import Path


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--cache-root', type=Path, required=True)
    parser.add_argument('--gpu-router', default='HIP')
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    sys.path.insert(0, str(args.root.resolve()))
    from scripts import yosys_netlist
    from scripts import fit_i386_device as fit
    from scripts import quartus_x87_spike as spike
    flattened = yosys_netlist.program

    def kept(*a, **k):
        program = flattened(*a, **k)
        if program.count('flatten; ') != 1:
            raise SystemExit('the CPU program no longer flattens exactly once')
        return program.replace('flatten; ', '')
    yosys_netlist.program = kept
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    netlist, provenance = fit.cpu_netlist(output, args.cache_root.resolve(), args.gpu_router)
    spike.plain_module_names(netlist)
    text = netlist.read_text()
    record = {'hierarchy': True, 'netlist': str(netlist), 'netlist_sha256': fit.sha(netlist.read_bytes()),
              'executed_program_sha256': fit.sha(next(output.rglob('*.ys')).read_bytes()),
              'module_count': sum(1 for line in text.splitlines() if line.startswith('module ')),
              'yosys': provenance.get('yosys') if isinstance(provenance, dict) else None}
    (output / 'elaboration.json').write_text(json.dumps(record, indent=2, sort_keys=True, default=str) + '\n')
    print(json.dumps({k: record[k] for k in ('netlist', 'module_count')}))
    return 0


if __name__ == '__main__':
    sys.exit(main())
