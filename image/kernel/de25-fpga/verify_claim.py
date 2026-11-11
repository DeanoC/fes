#!/usr/bin/env python3
"""Exercise the patched pinned service function with deterministic firmware replies."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile

LANE = Path(__file__).resolve().parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', required=True, type=Path, help='unmodified pinned stratix10-svc.c')
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--cc', default='cc')
    args = parser.parse_args()
    identity = json.loads((LANE / 'source.json').read_text())
    original = args.source.read_bytes()
    if hashlib.sha256(original).hexdigest() != identity['before_sha256']:
        raise ValueError('source differs from pinned kernel file')
    args.output.mkdir(parents=True, exist_ok=False)
    with tempfile.TemporaryDirectory(prefix='fes-de25-claim-') as temporary:
        root = Path(temporary)
        target = root / identity['path']
        target.parent.mkdir(parents=True)
        target.write_bytes(original)
        subprocess.run(['patch', '--batch', '--fuzz=0', '-p1', '-d', str(root), '-i',
                        str(LANE / 'bounded-claim.patch')], check=True)
        patched = target.read_bytes()
        if hashlib.sha256(patched).hexdigest() != identity['after_sha256']:
            raise ValueError('patched kernel file identity differs')
        text = patched.decode()
        start = text.index('static void svc_thread_cmd_data_claim(')
        end = text.index('\n/**', start)
        unit = args.output / 'claim-function.c'
        unit.write_text(text[start:end])
        harness = args.output / 'claim-test.c'
        harness.write_bytes((LANE / 'claim_test.c').read_bytes())
        binary = args.output / 'claim-test'
        command = [args.cc, '-std=c11', '-O2', '-Wall', '-Wextra', '-Werror',
                   str(harness), '-o', str(binary)]
        subprocess.run(command, check=True)
        result = subprocess.run([str(binary.resolve())], capture_output=True, check=True, timeout=10)
        (args.output / 'results.log').write_bytes(result.stdout + result.stderr)
        unit.write_bytes(original[original.index(b'static void svc_thread_cmd_data_claim('):
                                  original.index(b'\n/**', original.index(b'static void svc_thread_cmd_data_claim('))])
        control = args.output / 'claim-control'
        subprocess.run([*command[:-1], str(control)], check=True)
        try:
            subprocess.run([str(control.resolve())], capture_output=True, timeout=1, check=True)
        except subprocess.TimeoutExpired:
            control_result = 'original function hangs on BUSY as expected'
        else:
            raise ValueError('original-source control did not reproduce the hang')
        unit.write_text(text[start:end])
        report = {'control_result': control_result, 'classification': 'host function test with simulated firmware and clock',
                  'kernel': identity, 'compiler_command': command,
                  'cases': result.stdout.decode().splitlines(),
                  'hardware_access': 'none', 'kernel_build': 'not-tested',
                  'transfer_fix': 'not-established',
                  'files_sha256': {p.name: hashlib.sha256(p.read_bytes()).hexdigest()
                                   for p in args.output.iterdir() if p.is_file()}}
        (args.output / 'checks.json').write_text(json.dumps(report, indent=2) + '\n')
        print(result.stdout.decode(), end='')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
