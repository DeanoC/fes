#!/usr/bin/env python3
"""Exercise the opt-in platform's actual service-claim functions."""
import argparse
import json
from pathlib import Path
import subprocess

from verify_write import patched_source, digest, LANE


def function(source, bounded):
    text = source.decode()
    name = 'svc_thread_cmd_data_claim_bounded' if bounded else 'svc_thread_cmd_data_claim'
    start = text.index('static void ' + name + '(')
    return text[start:text.index('\n/**', start)]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--cc', default='cc')
    args = parser.parse_args()
    name = 'drivers/firmware/stratix10-svc.c'
    identity, original, patched = patched_source(args.source, name)
    args.output.mkdir(parents=True, exist_ok=False)
    unit = args.output / 'claim-function.c'
    unit.write_text(function(patched, True))
    harness = args.output / 'claim-test.c'
    harness.write_bytes((LANE / 'claim_test.c').read_bytes())
    binary = args.output / 'claim-test'
    command = [args.cc, '-std=c11', '-O2', '-Wall', '-Wextra', '-Werror', str(harness), '-o', str(binary)]
    subprocess.run(command, check=True)
    result = subprocess.run([str(binary.resolve())], capture_output=True, check=True, timeout=10)
    (args.output / 'results.log').write_bytes(result.stdout + result.stderr)
    unit.write_text(function(original, False))
    control = args.output / 'claim-control'
    subprocess.run([*command[:-1], str(control)], check=True)
    try:
        subprocess.run([str(control.resolve())], capture_output=True, timeout=1, check=True)
    except subprocess.TimeoutExpired:
        control_result = 'original function hangs on BUSY as expected'
    else:
        raise ValueError('original-source control did not reproduce the hang')
    unit.write_text(function(patched, True))
    disabled = args.output / 'claim-disabled'
    subprocess.run([*command[:-1], str(disabled), '-DCONFIG_FES_DE25_DMA_PLATFORM=0'], check=True)
    disabled_result = subprocess.run([str(disabled.resolve())], capture_output=True, check=True, timeout=5)
    (args.output / 'disabled.log').write_bytes(disabled_result.stdout + disabled_result.stderr)
    report = {'control_result': control_result,
              'classification': 'actual platform service functions; injected firmware and clock',
              'kernel_revision': identity['kernel_revision'], 'service': identity['files'][name],
              'patch_sha256': identity['patch_sha256'], 'compiler_command': command,
              'cases': result.stdout.decode().splitlines(), 'disabled_positive': 'passed',
              'hardware_access': False, 'sdm_dma_quiescence': False,
              'files_sha256': {p.name: digest(p.read_bytes()) for p in args.output.iterdir() if p.is_file()}}
    (args.output / 'checks.json').write_text(json.dumps(report, indent=2) + '\n')
    print(result.stdout.decode(), end='')
    print(control_result)


if __name__ == '__main__':
    main()
