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
import os
import re
from pathlib import Path
import subprocess
import sys
import time
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from fes_build_common import (
    FES_GPU_ARCHITECTURES, FES_TOOLCHAIN_CONFIGURATION,
    _authenticate_tools, _require_gpu_backend, _run_command,
    cuda_toolchain_configuration, gpu_backend_for_router,
)
from sim_fes_pcxt import SYSTEM_SOURCES, CPU_MUX_MAP, board_inputs, verify_vendor, vendor_dir, CHIP_VENDORS, SHARED_VENDORS

ROOT = Path(__file__).resolve().parents[1]
TOOLCHAIN_LOCK = 'cores/fes-pcxt/toolchain.lock'
RTL = SYSTEM_SOURCES + ('cores/fes-pcxt/rtl/fes_pcxt_top.sv',)
QSF = 'cores/fes-ramtest/constraints.qsf'
SDC = 'boards/de10nano/clocks.sdc'
ROM_CLOCK_MAP = 'cores/fes-pcxt/synth/rom_clock_map.v'
PLACER_TIMING_WEIGHT = 2000
PLACER_CRITICALITY_EXPONENT = 5
SYNTHESIS = 'synth_intel_alm -nolutram -nodsp -top top'


def synthesis_command():
    # Transfer register intent before dff_map introduces polarity inverters.
    # After full mapping attrmvcp would annotate those inverters, not the FFs.
    # The end label is exclusive: stop at map_ffs so memory_map and its final
    # optimization still run before the attribute transfer.
    return (SYNTHESIS + ' -run begin:map_ffs; attrmvcp -copy -attr async_reg; ' +
            SYNTHESIS + ' -run map_ffs:')


def yosys_program(output, mda, scanlines, synthesis):
    """Complete board Yosys script; run with cwd=output after board_inputs(output)."""
    return ('read_verilog -sv -I ' + str(ROOT / 'cores/fes-common/generated') + ' -I ' + str(output) + ' ' +
            ' '.join('-I ' + str(vendor_dir(n)) for n in CHIP_VENDORS) + ' ' +
            ' '.join(str(ROOT / s) for s in RTL) + '; ' +
            f'chparam -set MDA {int(mda)} -set SCANLINES {int(scanlines)} top; ' +
            synthesis + '; techmap -map ' + str(ROOT / CPU_MUX_MAP) +
            '; techmap -map ' + str(ROOT / ROM_CLOCK_MAP) + '; hierarchy -top top; opt_clean; check -assert; stat; write_json synth.json')


def gpu_choice(gpu_router):
    backend = gpu_backend_for_router(gpu_router)
    if gpu_router == 'HIP':
        return backend, FES_GPU_ARCHITECTURES, FES_TOOLCHAIN_CONFIGURATION
    return backend, 'unused', cuda_toolchain_configuration()


def authenticate_tools(root, cache_root, gpu_router='HIP'):
    _backend, architectures, configuration = gpu_choice(gpu_router)
    return _authenticate_tools(
        root, lock_path=root / TOOLCHAIN_LOCK,
        toolchain_root=root / 'build/toolchain/fes-pcxt',
        expected_configuration={'nextpnr': configuration},
        gpu_router=gpu_router, hip_architectures=architectures,
        cache_root=cache_root)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    # Omitted: authenticate the local toolchain-fes-pcxt install.
    parser.add_argument('--cache-root', type=Path)
    parser.add_argument('--gpu-router', choices=('HIP', 'CUDA'), default='HIP')
    parser.add_argument('--mda', action='store_true')
    parser.add_argument('--scanlines', action='store_true')
    parser.add_argument('--synth-only', action='store_true')
    parser.add_argument('--route-timeout', type=int, default=1800)
    args = parser.parse_args()
    if args.route_timeout <= 0:
        parser.error('--route-timeout must be positive')
    tools = authenticate_tools(ROOT, args.cache_root, args.gpu_router)
    gpu_backend, _architectures, _configuration = gpu_choice(args.gpu_router)
    for name in CHIP_VENDORS:
        verify_vendor(name)
    stamp = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + uuid.uuid4().hex[:8]
    output = ROOT / 'build/diagnostic/fes-pcxt' / ('mda' if args.mda else 'cga') / ('scanlines' if args.scanlines else 'direct') / stamp
    output.mkdir(parents=True)
    print(output, flush=True)
    # Headers, microcode and assembly are part of the diagnostic source record.
    inputs = sorted(set(RTL) | {QSF, SDC, ROM_CLOCK_MAP, CPU_MUX_MAP, TOOLCHAIN_LOCK, 'scripts/build_fes_pcxt.py',
                    'scripts/sim_fes_pcxt.py', 'cores/fes-pcxt/firmware/diagnostic.S'} |
                    {str(p.relative_to(ROOT)) for p in (ROOT / 'cores/fes-common/generated').glob('*.vh')} |
                    {str(p.relative_to(ROOT)) for d in (ROOT / 'cores/fes-pcxt/vendor', *map(vendor_dir, SHARED_VENDORS))
                     for p in d.rglob('*') if p.suffix in ('.svh', '.mem', '.json')})
    def hashes():
        return {s: hashlib.sha256((ROOT / s).read_bytes()).hexdigest() for s in inputs}
    sources = hashes()
    receipt = {'classification': 'board-diagnostic-only', 'status': 'building',
               'revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(),
               'working_tree_changes': subprocess.check_output(['git', 'status', '--porcelain', '--', '.'], cwd=ROOT, text=True).splitlines(),
               'sources': sources, 'tools': {k: v.identity for k, v in tools.items()},
               'mda': args.mda, 'scanlines': args.scanlines, 'seed': 1,
               'gpu_router': args.gpu_router, 'route_timeout_seconds': args.route_timeout,
               'route_timeout_basis': 'nextpnr work time; GPU lease waits excluded by the common runner',
               'placement': {'timing_weight': PLACER_TIMING_WEIGHT,
                             'criticality_exponent': PLACER_CRITICALITY_EXPONENT},
               'limits': ['no SDRAM pad setup/hold or retention qualification',
                          'no physical ISA containment', 'no sealed package or runtime ABI',
                          'Quartus qualification is a separate manual check',
                          'no hardware acceptance']}
    def save():
        (output / 'diagnostic.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')
    save()
    try:
        board_inputs(output)
        receipt['generated_roms'] = {p.name: hashlib.sha256(p.read_bytes()).hexdigest()
                                     for p in sorted(output.glob('*.mem'))}
        save()
        program = yosys_program(output, args.mda, args.scanlines, synthesis_command())
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
                       '--placer-heap-timingweight', str(PLACER_TIMING_WEIGHT),
                       '--placer-heap-critexp', str(PLACER_CRITICALITY_EXPONENT),
                       '--rbf', 'diagnostic.rbf', '--compress-rbf', '--write', 'routed.json',
                       '--report', 'timing.json', '--detailed-timing-report', '--timing-report-paths', '64']
            env = dict(os.environ)
            env['NEXTPNR_REQUIRE_GPU'] = '1'
            receipt['route_command'] = command
            receipt['gpu_visibility'] = {key: env.get(key) for key in
                                        ('FES_GPU_IDS', 'ROCR_VISIBLE_DEVICES', 'HIP_VISIBLE_DEVICES', 'CUDA_VISIBLE_DEVICES')}
            save()
            started = time.monotonic()
            try:
                with (output / 'nextpnr.log').open('w') as log:
                    _run_command(command, cwd=output, stdout=log, stderr=subprocess.STDOUT,
                                 check=True, timeout=args.route_timeout, env=env,
                                 gpu_label='pcxt-' + ('mda' if args.mda else 'cga') +
                                           '-' + ('scanlines' if args.scanlines else 'direct'))
            finally:
                receipt['route_elapsed_seconds'] = time.monotonic() - started

            route_text = (output / 'nextpnr.log').read_text()
            receipt['gpu_backend'] = _require_gpu_backend(route_text, gpu_backend)
            receipt['gpu_backend_devices'] = sorted(set(re.findall(
                r'\bbackend ((?:hip|cuda):[^\n]*?) ready\b', route_text, re.IGNORECASE)))
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
