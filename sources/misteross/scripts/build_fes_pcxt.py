#!/usr/bin/env python3
"""Build one autonomous PC/XT board diagnostic with the locked OSS compiler.

No package export, factory selection, deployment or physical-slot qualification.
Every invocation retains a separate directory, including failed routes.
"""
import argparse
from collections import Counter
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import subprocess
import uuid

from build_fes_coleco_socket_v2 import authenticate_tools
from fes_build_common import _require_gpu_backend
from sim_fes_pcxt import SYSTEM_SOURCES, board_inputs, verify_vendor

ROOT = Path(__file__).resolve().parents[1]
RTL = SYSTEM_SOURCES + ('cores/fes-pcxt/rtl/fes_pcxt_top.sv',)
QSF = 'cores/fes-ramtest/constraints.qsf'
SDC = 'boards/de10nano/clocks.sdc'
ROM_CLOCK_MAP = 'cores/fes-pcxt/synth/rom_clock_map.v'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cache-root', required=True, type=Path)
    parser.add_argument('--mda', action='store_true')
    parser.add_argument('--scanlines', action='store_true')
    parser.add_argument('--synth-only', action='store_true')
    parser.add_argument('--route-timeout', type=int, default=1800)
    args = parser.parse_args()
    if args.route_timeout <= 0:
        parser.error('--route-timeout must be positive')
    tools = authenticate_tools(ROOT, args.cache_root)
    for name in ('kf8259', 'kf8253', 'kf8255'):
        verify_vendor(name)
    stamp = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + uuid.uuid4().hex[:8]
    output = ROOT / 'build/diagnostic/fes-pcxt' / ('mda' if args.mda else 'cga') / ('scanlines' if args.scanlines else 'direct') / stamp
    output.mkdir(parents=True)
    print(output, flush=True)
    # Headers, microcode and assembly are part of the diagnostic source record.
    inputs = sorted(set(RTL) | {QSF, SDC, ROM_CLOCK_MAP, 'toolchain.lock', 'scripts/build_fes_pcxt.py',
                    'scripts/sim_fes_pcxt.py', 'cores/fes-pcxt/firmware/diagnostic.S'} |
                    {str(p.relative_to(ROOT)) for p in (ROOT / 'cores/fes-common/generated').glob('*.vh')} |
                    {str(p.relative_to(ROOT)) for p in (ROOT / 'cores/fes-pcxt/vendor').rglob('*')
                     if p.suffix in ('.svh', '.mem', '.json')})
    def hashes():
        return {s: hashlib.sha256((ROOT / s).read_bytes()).hexdigest() for s in inputs}
    sources = hashes()
    receipt = {'classification': 'board-diagnostic-only', 'status': 'building',
               'revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(),
               'working_tree_changes': subprocess.check_output(['git', 'status', '--porcelain', '--', '.'], cwd=ROOT, text=True).splitlines(),
               'sources': sources, 'tools': {k: v.identity for k, v in tools.items()},
               'mda': args.mda, 'scanlines': args.scanlines, 'seed': 1,
               'limits': ['no SDRAM pad setup/hold or retention qualification',
                          'no physical ISA containment', 'no sealed package or runtime ABI',
                          'no hardware acceptance']}
    def save():
        (output / 'diagnostic.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')
    save()
    try:
        board_inputs(output)
        program = ('read_verilog -sv -I ' + str(ROOT / 'cores/fes-common/generated') + ' ' +
                   ' '.join('-I ' + str(ROOT / 'cores/fes-pcxt/vendor' / n) for n in ('kf8259','kf8253','kf8255')) + ' ' +
                   ' '.join(str(ROOT / s) for s in RTL) + '; ' +
                   f'chparam -set MDA {int(args.mda)} -set SCANLINES {int(args.scanlines)} top; ' +
                   'synth_intel_alm -nolutram -nodsp -top top; techmap -map ' + str(ROOT / ROM_CLOCK_MAP) + '; opt_clean; check -assert; stat; write_json synth.json')
        with (output / 'yosys.log').open('w') as log:
            subprocess.run([str(tools['yosys'].path), '-p', program], cwd=output,
                           stdout=log, stderr=subprocess.STDOUT, check=True)
        netlist = json.loads((output / 'synth.json').read_text())['modules']['top']
        receipt['synthesis_cells'] = dict(Counter(c['type'] for c in netlist['cells'].values()))
        receipt['status'] = 'synthesized'
        save()
        if not args.synth_only:
            command = [str(tools['nextpnr-mistral'].path), '--json', 'synth.json',
                       '--device', '5CSEBA6U23I7', '--qsf', str(ROOT / QSF), '--sdc', str(ROOT / SDC),
                       '--freq', '100', '--seed', '1', '--router', 'gpu',
                       '--rbf', 'diagnostic.rbf', '--compress-rbf', '--write', 'routed.json',
                       '--report', 'timing.json', '--detailed-timing-report']
            with (output / 'nextpnr.log').open('w') as log:
                subprocess.run(command, cwd=output, stdout=log, stderr=subprocess.STDOUT,
                               check=True, timeout=args.route_timeout)
            receipt['gpu_backend'] = _require_gpu_backend((output / 'nextpnr.log').read_text(), 'hip')
            receipt['status'] = 'routed-diagnostic-unqualified'
            receipt['timing'] = json.loads((output / 'timing.json').read_text())
            receipt['rbf_sha256'] = hashlib.sha256((output / 'diagnostic.rbf').read_bytes()).hexdigest()
        if hashes() != sources:
            raise ValueError('PC/XT inputs changed during diagnostic build')
    except (subprocess.CalledProcessError, subprocess.TimeoutExpired, ValueError) as exc:
        receipt['status'] = 'failed'
        receipt['error'] = str(exc)
        save()
        raise
    save()
    print(json.dumps({'output': str(output), 'status': receipt['status'], 'cells': receipt['synthesis_cells']}, sort_keys=True))


if __name__ == '__main__':
    main()
