"""Actual-CPU controls for the default-off 286 REP I/O timing experiment.

The explicit full run compares source-bound baseline/candidate testbench
binaries with the diagnostic disabled and enabled. It checks the current
reference surrogate's guard; it does not validate assigned-device timing or
physical 286 behavior. Run with --help for the offline inputs.
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

FIXTURES = Path(__file__).resolve().parent / 'fixtures/i386-rep-io'
CASES = {
    'alignment': (8, [19, 18.96, 18.96, -0.04, 17, 16.96, 16.96, -0.04],
                  'd6f5e44d1bdd1269c9b7d63f8579372a88b796d4c501db33d442dc518146e6a9'),
    'unreal': (4, [17, 0, 19, 0],
               '9245b05bf214115c7ed2af59888d7083354efc22e7b69a10e17a1843be9c4c29'),
}


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def parse_output(text, windows):
    marks = {}
    for match in re.finditer(r'^MARK 80 (\d+) \d+ (\d+)$', text, re.M):
        key = int(match[1])
        if key in marks:
            raise ValueError('duplicate window marker')
        marks[key] = int(match[2])
    signature = [line for line in text.splitlines() if line.startswith(('MARK ', 'STATE ', 'GOVERNOR '))]
    state = [line for line in signature if line.startswith('STATE ')]
    if (set(marks) != set(range(2 * windows)) or len(state) != 1 or
            not any(line.startswith('MARK 82 85 ') for line in signature) or
            not any(line.startswith('MARK 8f 85 ') for line in signature) or
            not any(line.startswith('GOVERNOR ') and 'halted=1' in line for line in signature)):
        raise ValueError('missing window, guest control, state or halt')
    return marks, state[0], signature


def run_full(args):
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    binaries = {(side, enabled): getattr(args, f'{side}_{"on" if enabled else "off"}').resolve()
                for side in ('baseline', 'candidate') for enabled in (0, 1)}
    identities = {str(p): digest(p) for p in binaries.values()}
    results = {'binaries': identities, 'fixtures': {}}
    for name, (windows, expected, program_hash) in CASES.items():
        source = FIXTURES / f'{name}.asm'
        program = output / f'{name}.bin'
        subprocess.run([str(args.nasm), '-f', 'bin', '-o', str(program), str(source)], check=True, timeout=30)
        if digest(program) != program_hash:
            raise ValueError(f'{name}: assembled encoding changed')
        results['fixtures'][name] = {'source_sha256': digest(source), 'program_sha256': digest(program)}
        runs = {}
        for side in ('baseline', 'candidate'):
            for enabled in (0, 1):
                env = dict(os.environ, FES_I386_MODE='governor', FES_I386_LATENCY='0',
                           FES_I386_PROGRAM=str(program), FES_I386_PRESET='1', FES_I386_FREQUENCY='125',
                           FES_I386_TURBO='0', FES_I386_IRQ_PERIOD='0', FES_I386_CYCLE_LIMIT='10000000')
                log = output / f'{name}-{side}-on{enabled}.log'
                with log.open('x') as stream:
                    subprocess.run([str(binaries[side, enabled])], env=env, stdout=stream,
                                   stderr=subprocess.STDOUT, check=True, timeout=180)
                runs[side, enabled] = parse_output(log.read_text(), windows)
        if runs['baseline', 0][2] != runs['candidate', 0][2]:
            raise ValueError(f'{name}: default-off signature changed')
        if runs['baseline', 1][1] != runs['candidate', 1][1]:
            raise ValueError(f'{name}: architectural state changed')
        before, after = runs['baseline', 1][0], runs['candidate', 1][0]
        delta = [((after[2*i+1] - after[2*i]) - (before[2*i+1] - before[2*i])) / 100
                 for i in range(windows)]
        if any(abs(a-b) > 0.01 for a,b in zip(delta, expected, strict=True)):
            raise ValueError(f'{name}: unexpected charge deltas {delta}')
        results[name + '_delta'] = delta
        print(f'PASS {name}', flush=True)
    if any(digest(Path(p)) != h for p, h in identities.items()):
        raise ValueError('CPU testbench changed during the run')
    (output / 'report.json').write_text(json.dumps(results, indent=2, sort_keys=True) + '\n')


class MarkerParsingTest(unittest.TestCase):
    def test_requires_markers_and_halt(self):
        good = 'MARK 80 0 1 2\nMARK 80 1 3 4\nMARK 82 85 4 5\nMARK 8f 85 5 6\nSTATE 0\nGOVERNOR halted=1\n'
        self.assertEqual(parse_output(good, 1)[0], {0: 2, 1: 4})
        for bad in (good + 'MARK 80 0 9 9\n', good.replace('MARK 82 85', 'MARK 82 0'),
                    good.replace('MARK 80 1 3 4\n', '')):
            with self.assertRaises(ValueError):
                parse_output(bad, 1)


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
