#!/usr/bin/env python3
"""Check whether a signoff-review revision can reuse the retained simulation run."""
import argparse
from fractions import Fraction
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile

from sim_evidence import source_revision
from sim_games import RUNS, adapt_bench, arguments_for, sha


MISTEROSS = 'sources/misteross/'
SIGNOFF_TABLE = MISTEROSS + 'boards/de25nano/pc386/signoff_bases.py'
RECORD = 'docs/validation/2026-11-25-de25-486dx-clock/'
SOURCE_FILES = (
    'boards/de25nano/pc386/build.py',
    'boards/de25nano/pc386/model_clocks.py',
    'boards/de25nano/pc386/i486dx/derive.py',
    'cores/fes-pc386/firmware/firmware.lock',
    'cores/fes-pc386/sim/audio_link.h',
    'cores/fes-pc386/sim/machine_tb.cpp',
    'scripts/build_pc386_firmware.py',
    'scripts/fes_i386_oracle.py',
    'scripts/pc386_cpu_profile.py',
    'scripts/sim_fes_pc386.py',
)


def git(checkout, *args):
    return subprocess.check_output(['git', '-C', checkout, *args])


def retained_inputs(run, checkout):
    paths = {MISTEROSS + name for name in SOURCE_FILES}
    tracked = set(git(checkout, 'ls-files', '-z').decode().split('\0'))
    for report in (run / 'lockstep').glob('*.json'):
        data = json.loads(report.read_text())
        source = (data.get('source') or data.get('i486dx_source')
                  or data.get('unit_report', {}).get('source', {}))
        for path, expected in source.get('files', {}).items():
            if path in tracked:
                if sha(checkout / path) != expected:
                    raise ValueError(f'retained lockstep input differs from its source report: {path}')
                paths.add(path)
    prefix = re.escape(str(checkout) + '/')
    for dependency in (run / 'games/build').rglob('*.d'):
        paths.update(path for path in re.findall(prefix + r'([^\s\\:]+)', dependency.read_text())
                     if path in tracked)
    return {path: sha(checkout / path) for path in sorted(paths)}


def regenerate(checkout, staging):
    program = '''
import json
from pathlib import Path
import sys

from boards.de25nano.pc386 import build
from boards.de25nano.pc386.i486dx import derive

derive.sim_sources(Path(sys.argv[1]), Path.cwd())
clock = build.model_clock('486dx')
print(json.dumps({'c0': clock['c0'], 'clock_hz': str(clock['clock']),
                  'guest_rate_parameters': build.guest_rates('486dx')}))
'''
    root = checkout / MISTEROSS
    clock = json.loads(subprocess.check_output([sys.executable, '-B', '-c', program, staging],
                                              cwd=root, text=True))
    adapt_bench(root / 'cores/fes-pc386/sim', staging, Fraction(clock['clock_hz']), clock['guest_rate_parameters'])
    return clock


def compare(run, candidate):
    checkout = (run / 'wt-games').resolve()
    games = run / 'games'
    inputs = json.loads((games / 'inputs.json').read_text())
    before, after = source_revision(checkout), source_revision(candidate)
    if before != inputs['revision'] or before != source_revision(run / 'wt'):
        raise ValueError('retained worktrees differ from the source named by the run')
    paths = retained_inputs(run, checkout)
    changed = set(git(candidate, 'diff', '--name-only', '-z', before, after).decode().split('\0')) - {''}
    # Nothing in these two directories is used by the retained runners. The
    # pc386 board build records the signoff table, and only the Quartus
    # producer's reviewed-finding pass invokes it.
    unexplained = sorted(path for path in changed if path != SIGNOFF_TABLE
                         and not path.startswith((RECORD, MISTEROSS + 'tests/')))
    modified_inputs = sorted(path for path, expected in paths.items()
                             if not (candidate / path).is_file() or sha(candidate / path) != expected)
    with tempfile.TemporaryDirectory(dir=run) as directory:
        staging = Path(directory)
        clock = regenerate(candidate, staging)
        old_clock = {key: inputs[key] for key in clock}
        regenerated = {name: sha(staging / name) for name in ('machine_tb.cpp', 'audio_link.h', 'fes_i386_cpu.sv')}
        expected = {**inputs['adapted_bench'], 'fes_i386_cpu.sv': sha(games / 'derived/fes_i386_cpu.sv')}
    if source_revision(candidate) != after:
        raise ValueError('candidate changed during the comparison')
    stimulus_equal = True
    for arm, workload in RUNS:
        directory = games / f'{arm}-{workload}'
        command = json.loads((directory / 'command.json').read_text())
        stimulus = arguments_for(workload, directory)
        drive = 'pmode' if workload == 'doom' else 'bench'
        stimulus_equal &= (command[-len(stimulus):] == stimulus and
                           sha(directory / 'disk.img') == inputs['fixtures'][drive])
    equivalent = (not unexplained and not modified_inputs and clock == old_clock
                  and regenerated == expected and stimulus_equal)
    return {'built_revision': before, 'candidate_revision': after, 'simulation_inputs_equal': equivalent,
            'changes': sorted(changed), 'changes_outside_review_scope': unexplained,
            'changed_retained_inputs': modified_inputs, 'retained_input_count': len(paths),
            'retained_input_sha256': hashlib.sha256(json.dumps(paths, sort_keys=True).encode()).hexdigest(),
            'regenerated_inputs_sha256': regenerated, 'generated_inputs_equal': regenerated == expected,
            'clock_and_guest_rates_equal': clock == old_clock, 'candidate_clock': clock,
            'game_stimulus_equal': stimulus_equal}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('run', type=Path, help='retained simulation run root')
    parser.add_argument('candidate', type=Path, help='clean FES checkout of the candidate')
    args = parser.parse_args()
    result = compare(args.run.resolve(), args.candidate.resolve())
    print(json.dumps(result, indent=2))
    return 0 if result['simulation_inputs_equal'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
