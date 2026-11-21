#!/usr/bin/env python3
"""Fit pc386 with the 486SX identity (I486 = 1) in the DE10 seal's own flow.

The registered pc386 producer seals I486 = 0, the only identity a package
builds. This elaborates the same machine program with I486 = 1 added to its
parameters (the sealed guest rates kept), fits it in the producer's project
(pc386_quartus.write_project: board shell, pins, SDC, the recipe's threads),
BUILD_ID all zeros, at one seed, and applies the seal's timing check
(pc386_quartus.validate_timing_report). Nothing is packaged.

  python3 fit_486sx.py --root <clean checkout>/sources/misteross --seed 1 \\
      --gpu-router CUDA --cache-root <abs> --out <abs new dir> [--i486 0]

--i486 0 fits the 386 the same way, as the control. Needs the pinned
firmware (scripts/build_pc386_firmware.py) and QUARTUS_ROOTDIR.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
import time
from datetime import datetime, timezone
from pathlib import Path


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--seed', type=int, required=True)
    parser.add_argument('--i486', type=int, choices=(0, 1), default=1)
    parser.add_argument('--gpu-router', choices=('HIP', 'CUDA'), default='HIP')
    parser.add_argument('--cache-root', type=Path, required=True)
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    for path in (args.root, args.cache_root, args.out):
        if not path.is_absolute():
            raise SystemExit(f'{path} must be absolute')
    if args.out.exists():
        raise SystemExit(f'{args.out} exists')
    root = args.root
    sys.path.insert(0, str(root))
    sys.path.insert(0, str(root / 'cores/fes-pc386/tools'))
    from scripts import pc386_quartus as q
    from scripts import yosys_netlist
    import build_pc386_product as product
    dirty = subprocess.run(['git', 'status', '--porcelain', '--untracked-files=no'], cwd=root, check=True,
                           capture_output=True, text=True).stdout.strip()
    if dirty:
        raise SystemExit('the checkout has uncommitted changes to tracked files')
    commit = subprocess.run(['git', 'rev-parse', 'HEAD'], cwd=root, check=True, capture_output=True,
                            text=True).stdout.strip()
    rom = q.check_rom(root)
    q._check_seed(args.seed)
    args.out.mkdir(parents=True)
    tools = q._authenticate_tools(root, cache_root=args.cache_root, gpu_router=args.gpu_router)
    threads = product.selected_recipe(root)['quartus']['threads']
    project = q.write_project(root, args.out, '0' * 32, args.seed, threads=threads)
    netlist = project / q.MACHINE_NETLIST
    parameters = {**q.guest_rates(), 'I486': args.i486}
    program = yosys_netlist.program(yosys_netlist.SEAL, top=q.MACHINE_TOP, sources=q.MACHINE_SOURCES,
                                    includes=q.INCLUDE_DIRS, parameters=parameters,
                                    netlist=netlist.relative_to(root).as_posix()
                                    if netlist.is_relative_to(root) else netlist.as_posix())
    started = time.monotonic()
    yosys_netlist.run(tools['yosys'].path, program, cwd=root, program_file=args.out / 'machine.ys',
                      log=args.out / 'yosys.log')
    env = dict(os.environ, QUARTUS_ROOTDIR=str(tools['quartus_root']))
    with (args.out / 'quartus.log').open('w') as log:
        result = subprocess.run([str(tools['quartus_sh']), '--flow', 'compile', q.board.TOP], cwd=project, env=env,
                                stdout=log, stderr=subprocess.STDOUT)
    minutes = (time.monotonic() - started) / 60
    built = project / 'output_files'
    top = q.board.TOP
    summary = {'commit': commit, 'i486': args.i486, 'seed': args.seed, 'gpu_router': args.gpu_router,
               'quartus_exit': result.returncode, 'netlist_sha256': sha(netlist), 'rom_hex_sha256': sha(rom),
               'parameters': parameters, 'quartus_threads': threads, 'quartus': tools['quartus_version'],
               'yosys': tools['identities']['yosys'], 'device': q.board.TARGET, 'host': os.uname().nodename,
               'build_id': '0' * 32, 'minutes': round(minutes, 1),
               'created_utc': datetime.now(timezone.utc).isoformat()}
    if result.returncode == 0:
        sta = (built / f'{top}.sta.rpt').read_text(errors='replace')
        try:
            summary['timing'] = q.validate_timing_report(sta)
            summary['closes'] = True
        except q.board.BuildError as error:
            summary['closes'], summary['timing_failure'] = False, str(error)
        summary['resources'] = q.fit_resources((built / f'{top}.fit.rpt').read_text(errors='replace'))
        for name in (f'{top}.fit.summary', f'{top}.sta.summary', f'{top}.map.summary'):
            shutil.copyfile(built / name, args.out / name)
    (args.out / 'summary.json').write_text(json.dumps(summary, indent=2, sort_keys=True, default=str) + '\n')
    shutil.rmtree(project)
    print(json.dumps({k: summary.get(k) for k in ('i486', 'seed', 'closes', 'quartus_exit', 'minutes')}))
    return 0 if result.returncode == 0 and summary.get('closes') else 1


if __name__ == '__main__':
    raise SystemExit(main())
