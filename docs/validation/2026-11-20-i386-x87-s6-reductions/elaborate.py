#!/usr/bin/env python3
"""Elaborate pc386_system for x87 slice 6 (step 6.0's script, copied for 6.3), flattened or with hierarchy kept.

Run from a tree's sources/misteross with that tree's scripts on the path. It calls the fit
runner's own machine_netlist, so the netlist is the one the runner would write. --hierarchy
drops Yosys's flatten exactly once and gives modules plain names, as
scripts/quartus_x87_spike.py --hierarchy does on the DE10. Writes <output>/netlist/ and
<output>/elaboration.json; no fit.

  python3 elaborate.py --root <tree>/sources/misteross --cache-root /data/fes-toolchain-cache \
      --gpu-router CUDA --output /data/projects/x87s6m/elab-flat [--hierarchy]
"""
import argparse
import json
import sys
from pathlib import Path


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--cache-root', type=Path, required=True)
    parser.add_argument('--gpu-router', default='CUDA')
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--hierarchy', action='store_true')
    args = parser.parse_args()
    sys.path.insert(0, str(args.root.resolve()))
    # The pc386 machine program's module: scripts/pc386_quartus.py on main from
    # #2032, scripts/build_fes_pc386_quartus.py before it (#2037's tree).
    try:
        from scripts import pc386_quartus as sealer
    except ImportError:
        from scripts import build_fes_pc386_quartus as sealer
    from scripts import fit_i386_device as fit
    from scripts import quartus_x87_spike as spike
    if args.hierarchy:
        flattened = sealer.machine_program

        def kept(*a, **k):
            program = flattened(*a, **k)
            if program.count('flatten; ') != 1:
                raise SystemExit('the machine program no longer flattens exactly once')
            return program.replace('flatten; ', '')
        sealer.machine_program = kept
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    netlist, provenance = fit.machine_netlist(output, args.cache_root.resolve(), args.gpu_router)
    if args.hierarchy:
        spike.plain_module_names(netlist)
        provenance['netlist_sha256_after_plain_names'] = fit.sha(netlist.read_bytes())
    # machine_netlist reports the canonical SEAL recipe's digest; with --hierarchy that is not
    # the program Yosys ran. The program actually run is kept beside the netlist as machine.ys.
    provenance['seal_recipe_program_sha256'] = provenance.pop('program_sha256')
    provenance['executed_program_sha256'] = fit.sha((netlist.parent / 'machine.ys').read_bytes())
    text = netlist.read_text()
    provenance.update({'hierarchy': args.hierarchy, 'gpu_router': args.gpu_router,
                       'netlist': str(netlist), 'netlist_lines': text.count('\n'),
                       'module_count': sum(1 for line in text.splitlines() if line.startswith('module '))})
    (output / 'elaboration.json').write_text(json.dumps(provenance, indent=2, sort_keys=True) + '\n')
    print(json.dumps({k: provenance[k] for k in ('hierarchy', 'netlist', 'netlist_lines', 'module_count')}))
    return 0


if __name__ == '__main__':
    sys.exit(main())
