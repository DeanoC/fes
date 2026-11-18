"""Actual-CPU regression for the default-off 286 IMUL diagnostic.

The full check needs four source-bound governor-mode testbenches: baseline and
candidate, each built with GOV_REFERENCE_DIAGNOSTIC 0 and 1. It assembles the
fixtures below and compares the disabled path, eligible forms and fallbacks.
The caller records how those binaries were built; this script does not attest
their source identity. Run with --help for its explicit, offline-only inputs.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import unittest

FIXTURES = Path(__file__).resolve().parent / 'fixtures/i386-imul'
CASES = {
    'short': (8, [-7] * 8),
    'pressure': (8, [-7] * 4 + [-3, -3, -5, -5]),
    'forms': (13, [-3] * 6 + [-5, -5, 0, 0, -5, 0, 0]),
    'modes': (4, [-3, 0, 0, 0]),
    'unreal': (2, [-5, 0]),
    'selected-segment': (3, [-5, -5, 0]),
    'boundary': (4, [-5, 0, 0, 0]),
    'transition': (4, [-7, -7, 0, -7]),
    'paging-imul': (3, [0, 0, 0]),
}
PROGRAM_SHA256 = {
    'short': 'f6f05920d0c8fa525d06e101e424a5cf5820b059967ed9cd0537f8257243587f',
    'pressure': '463f2292189f5ef43c36832750afad466e88f2f2bf34fdc30ee3624569c36d7f',
    'forms': 'c53c64cc5c82595b88d261e84bfbb5d26bf50a5daa729f2b36b35d28a9f8508f',
    'modes': '0d6b3989221f16bb8e8d6364f131bbf4b7d6bfc2d37c5d732d728c623f01dc4d',
    'unreal': '08a3c991c0fca1d7bfcfce226fe8e81efa4cc51e2fc83c8310eb72d95aacf8b9',
    'selected-segment': 'e9027ee990c370a8e01e8ddf35cf196cedaaaea226d728759cbaa45b63835156',
    'boundary': '7e973e4e993b900ce5c9a4741ea463c66839af54e9c08c341b9f210eb9648687',
    'transition': 'd733b81b198237cd544603ce5fb9755b430d367d658ff661710e9cc10fc463f1',
    'paging-imul': 'ca4dbf14024a6f2920b1002184cde5ba6d90314fb1f7cea1f770a96a78bc9def',
}


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def parse_output(text: str, windows: int, *, page_fault: bool = False):
    marks = {}
    for match in re.finditer(r'^MARK 80 (\d+) (\d+) (\d+)$', text, re.M):
        key = int(match[1])
        if key in marks:
            raise ValueError('duplicate window marker')
        marks[key] = int(match[3])
    faults = [int(x) for x in re.findall(r'^MARK 81 (\d+) \d+ \d+$', text, re.M)]
    if (set(marks) != set(range(2 * windows)) or faults != ([85] if page_fault else [])
            or 'MARK 8f 0 ' not in text or 'halted=1' not in text):
        raise ValueError('missing window, fault, arithmetic or halt marker')
    signature = [line for line in text.splitlines() if line.startswith(('MARK ', 'STATE ', 'GOVERNOR '))]
    return marks, signature


def run_full(args):
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    binaries = {(side, enabled): getattr(args, f'{side}_{"on" if enabled else "off"}').resolve()
                for side in ('baseline', 'candidate') for enabled in (0, 1)}
    inputs = {str(path): digest(path) for path in binaries.values()}
    results = {'binaries': inputs, 'fixtures': {}}
    for name, (windows, expected) in CASES.items():
        source = FIXTURES / f'{name}.asm'
        program = output / f'{name}.bin'
        subprocess.run([str(args.nasm), '-f', 'bin', '-o', str(program), str(source)], check=True, timeout=30)
        if digest(program) != PROGRAM_SHA256[name]:
            raise ValueError(f'{name}: assembled program differs from the measured encoding')
        results['fixtures'][name] = {'source_sha256': digest(source), 'program_sha256': digest(program)}

        def execute(side, enabled, latency):
            key = f'{name}-{side}-on{enabled}-latency{latency}'
            env = dict(os.environ, FES_I386_MODE='governor', FES_I386_LATENCY=str(latency),
                       FES_I386_PROGRAM=str(program), FES_I386_PRESET='1', FES_I386_FREQUENCY='125',
                       FES_I386_TURBO='0', FES_I386_IRQ_PERIOD='0', FES_I386_CYCLE_LIMIT='10000000')
            path = output / f'{key}.log'
            with path.open('w') as stream:
                subprocess.run([str(binaries[side, enabled])], env=env, stdout=stream,
                               stderr=subprocess.STDOUT, check=True, timeout=240)
            marks, signature = parse_output(path.read_text(), windows, page_fault=name == 'paging-imul')
            results[key] = {'sha256': digest(path), 'marks': marks}
            return marks, signature

        for latency in ([0, 1] if name in ('forms', 'boundary') else [0]):
            off_base = execute('baseline', 0, latency)
            off_new = execute('candidate', 0, latency)
            if off_base[1] != off_new[1]:
                raise ValueError(f'{name}: default-off signature changed')
            if latency:
                continue
            on_base = execute('baseline', 1, 0)
            on_new = execute('candidate', 1, 0)
            if [x for x in on_base[1] if x.startswith('STATE ')] != [x for x in on_new[1] if x.startswith('STATE ')]:
                raise ValueError(f'{name}: architectural state changed')
            if expected is not None:
                delta = [((on_new[0][2 * i + 1] - on_new[0][2 * i]) -
                          (on_base[0][2 * i + 1] - on_base[0][2 * i])) / 1000 for i in range(windows)]
                if name in ('paging-imul', 'transition'):
                    # These windows contain individual instructions, not 1000 iterations.
                    delta = [x * 1000 for x in delta]
                if any(abs(x - y) > (0.001 if y == 0 else 0.005)
                       for x, y in zip(delta, expected, strict=True)):
                    raise ValueError(f'{name}: unexpected charge deltas {delta}')
                results[name + '-delta'] = delta
        print(f'PASS {name}', flush=True)
    if any(digest(path) != value for path, value in ((p, inputs[str(p)]) for p in binaries.values())):
        raise ValueError('a CPU testbench changed during the run')
    (output / 'report.json').write_text(json.dumps(results, indent=2) + '\n')


class MarkerParsingTest(unittest.TestCase):
    def test_requires_all_windows_and_exact_fault_marker(self):
        good = 'MARK 80 0 1 2\nMARK 80 1 3 4\nMARK 81 85 5 6\nMARK 8f 0 7 8\nGOVERNOR halted=1\n'
        self.assertEqual(parse_output(good, 1, page_fault=True)[0], {0: 2, 1: 4})
        for bad in (good.replace('MARK 8f 0', 'MARK 8f 1'), good.replace('MARK 81 85', 'MARK 81 84'),
                    good + 'MARK 80 0 9 9\n', good.replace('MARK 80 1 3 4\n', '')):
            with self.assertRaises(ValueError):
                parse_output(bad, 1, page_fault=True)


if __name__ == '__main__':
    if any(arg.startswith('--baseline-') for arg in sys.argv[1:]):
        parser = argparse.ArgumentParser(description=__doc__)
        for side in ('baseline', 'candidate'):
            for mode in ('off', 'on'):
                parser.add_argument(f'--{side}-{mode}', type=Path, required=True)
        parser.add_argument('--nasm', type=Path, required=True)
        parser.add_argument('--output', type=Path, required=True)
        run_full(parser.parse_args())
    else:
        unittest.main()
