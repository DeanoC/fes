#!/usr/bin/env python3
"""Replay the retained PC/XT placement control or exact two-cell swap. Diagnostic only."""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time


def prepare(variant):
    here = Path(__file__).resolve().parent
    fixture = json.loads((here / 'fixture.json').read_text())
    packed = (here / 'placed.json.gz').read_bytes()
    assert hashlib.sha256(packed).hexdigest() == fixture['checkpoint_gzip_sha256']
    raw = gzip.decompress(packed)
    assert hashlib.sha256(raw).hexdigest() == fixture['checkpoint_sha256']
    design = json.loads(raw)
    if variant == 'swap':
        cells = design['modules']['top']['cells']
        for name, bel in fixture['swap'].items():
            assert cells[name]['type'] == 'MISTRAL_ALUT5'
            cells[name]['attributes']['NEXTPNR_BEL'] = bel
    return fixture, design


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--nextpnr', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--variant', choices=('control', 'swap'), required=True)
    args = parser.parse_args()
    fixture, design = prepare(args.variant)
    binary = args.nextpnr.resolve()
    assert hashlib.sha256(binary.read_bytes()).hexdigest() == fixture['nextpnr_sha256'], 'Wrong compiler bytes'
    for key in ('GPU_LEASE_IDS', 'GPU_LEASE_VENDOR', 'HIP_VISIBLE_DEVICES', 'CUDA_VISIBLE_DEVICES', 'ROCR_VISIBLE_DEVICES'):
        assert not os.environ.get(key), key
    here = Path(__file__).resolve().parent
    repo = here.parents[2]
    sys.path[:0] = [str(repo / 'sources/misteross'), str(repo / 'sources/misteross/scripts')]
    from fes_build_common import _run_command
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=False)
    checkpoint = out / 'placed.json'
    checkpoint.write_text(json.dumps(design) + '\n')
    command = fixture['original_command'].copy()
    command[0] = str(binary)
    for option, path in (('--json', checkpoint), ('--qsf', here / 'constraints.qsf'), ('--sdc', here / 'clocks.sdc')):
        command[command.index(option) + 1] = str(path)
    env = dict(os.environ, FES_GPU_LEASE_OWNER='pcxt-agent', GPU_LEASE_MAX_PER_OWNER='2', NEXTPNR_REQUIRE_GPU='1')
    receipt = {'variant': args.variant, 'command': command, 'status': 'running', 'timeout_seconds': 600,
               'nextpnr_sha256': fixture['nextpnr_sha256'], 'checkpoint_sha256': hashlib.sha256(checkpoint.read_bytes()).hexdigest(),
               'environment': {k: env[k] for k in ('FES_GPU_LEASE_OWNER', 'GPU_LEASE_MAX_PER_OWNER', 'NEXTPNR_REQUIRE_GPU')}}
    result_file = out / 'replay.json'
    result_file.write_text(json.dumps(receipt, indent=2) + '\n')
    start = time.monotonic()
    try:
        with (out / 'nextpnr.log').open('w') as log:
            result = _run_command(command, cwd=out, env=env, stdout=log, stderr=subprocess.STDOUT,
                                  timeout=600, gpu_label='pcxt840-local-' + args.variant)
        receipt.update(status='completed' if result.returncode == 0 else 'failed', returncode=result.returncode)
    except subprocess.TimeoutExpired:
        receipt['status'] = 'timeout'
    except Exception as error:
        receipt.update(status='error', error=str(error))
    receipt['wall_seconds'] = time.monotonic() - start
    receipt['artifacts'] = {name: hashlib.sha256((out / name).read_bytes()).hexdigest()
                            for name in ('routed.json', 'timing.json', 'diagnostic.rbf', 'nextpnr.log') if (out / name).exists()}
    if (out / 'timing.json').exists():
        timing = json.loads((out / 'timing.json').read_text())
        receipt.update(fmax=timing['fmax'], timing_summary=timing['timing_summary'])
    result_file.write_text(json.dumps(receipt, indent=2) + '\n')
    print(receipt['status'])
    return 0 if receipt['status'] == 'completed' else 1


if __name__ == '__main__':
    raise SystemExit(main())
