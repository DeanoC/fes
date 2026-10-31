#!/usr/bin/env python3
"""Compile the pinned manager's actual write loop with injected service outcomes."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile

LANE = Path(__file__).resolve().parent
PROFILE = LANE.parent / 'de25-platform'


def digest(data):
    return hashlib.sha256(data).hexdigest()


def function(source):
    text = source.decode()
    start = text.index('static int s10_ops_write(struct')
    return text[start:text.index('\nstatic int s10_ops_write_complete', start)]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--cc', default='cc')
    args = parser.parse_args()
    identity = json.loads((PROFILE / 'source.json').read_text())
    name = 'drivers/fpga/stratix10-soc.c'
    original = args.source.read_bytes()
    if digest(original) != identity['files'][name]['before_sha256']:
        raise ValueError('manager differs from the locked complete source')
    patch = (PROFILE / 'permanent-suppliers.patch').read_bytes()
    if digest(patch) != identity['patch_sha256']:
        raise ValueError('platform patch differs from its source receipt')
    sections = patch.decode().split('--- a/')
    selected = [part for part in sections if part.startswith(name + '\n')]
    if len(selected) != 1:
        raise ValueError('expected one complete manager patch')
    args.output.mkdir(parents=True, exist_ok=False)
    with tempfile.TemporaryDirectory(prefix='fes-de25-write-') as temp:
        root = Path(temp)
        target = root / name
        target.parent.mkdir(parents=True)
        target.write_bytes(original)
        subprocess.run(['patch', '--batch', '--fuzz=0', '-p1', '-d', str(root)],
                       input=('--- a/' + selected[0]).encode(), check=True, capture_output=True)
        patched = target.read_bytes()
        if digest(patched) != identity['files'][name]['after_sha256']:
            raise ValueError('complete patched manager identity differs')
        unit = args.output / 'write-function.c'
        unit.write_text(function(patched))
        harness = args.output / 'write-test.c'
        harness.write_bytes((LANE / 'write_test.c').read_bytes())
        binary = args.output / 'write-test'
        command = [args.cc, '-std=c11', '-O2', '-Wall', '-Wextra', '-Werror', str(harness), '-o', str(binary)]
        subprocess.run(command, check=True)
        result = subprocess.run([str(binary.resolve())], check=True, capture_output=True, timeout=10)
        (args.output / 'results.log').write_bytes(result.stdout + result.stderr)
        unit.write_text(function(original))
        control = args.output / 'write-control'
        subprocess.run([*command[:-1], str(control)], check=True)
        try:
            subprocess.run([str(control.resolve()), 'permanent-error'], check=True,
                           capture_output=True, timeout=1)
        except subprocess.TimeoutExpired:
            control_result = 'unmodified manager hangs on permanent ENOMEM'
        else:
            raise ValueError('original control did not reproduce the error spin')
        unit.write_text(function(patched))
        disabled = args.output / 'write-disabled'
        subprocess.run([*command[:-1], str(disabled), '-DCONFIG_FES_DE25_DMA_PLATFORM=0'], check=True)
        disabled_result = subprocess.run([str(disabled.resolve()), 'positive'], check=True,
                                         capture_output=True, timeout=5)
        (args.output / 'disabled.log').write_bytes(disabled_result.stdout + disabled_result.stderr)
        report = {'classification': 'actual pinned write function; injected service and clock',
                  'kernel_revision': identity['kernel_revision'], 'manager': identity['files'][name],
                  'patch_sha256': identity['patch_sha256'], 'command': command,
                  'control': control_result, 'disabled_profile_positive': 'passed', 'cases': result.stdout.decode().splitlines(),
                  'hardware_access': False, 'whole_programming_bound': False,
                  'sdm_dma_quiescence': False,
                  'sha256': {p.name: digest(p.read_bytes()) for p in args.output.iterdir() if p.is_file()}}
        (args.output / 'checks.json').write_text(json.dumps(report, indent=2) + '\n')
        print(result.stdout.decode(), end='')
        print(control_result)


if __name__ == '__main__':
    main()
