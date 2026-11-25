#!/usr/bin/env python3
"""Check whether a signoff-review revision can reuse the retained simulation run."""
import argparse
import ast
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
BOARD_BUILD = MISTEROSS + 'boards/de25nano/pc386/build.py'
SIGNOFF_TABLE = MISTEROSS + 'boards/de25nano/pc386/signoff_bases.py'
RECORD = 'docs/validation/2026-11-25-de25-486dx-clock/'
# AST of e255d4e3's RegisteredBoard class. Its import-time constructor stores
# the delegate, and the ROOT lookup uses the unchanged delegate attribute.
# The class's other method is the registered Quartus compile path.
REGISTERED_BOARD_AST_SHA256 = '9495bbef66d52666a0d86062c270f2e2f42bc18fe21c14c566bdd409b52306c5'
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

calls = set()
module_path = str(Path.cwd() / 'boards/de25nano/pc386/build.py')

def profile(frame, event, _arg):
    if event == 'call' and frame.f_code.co_filename == module_path:
        calls.add(frame.f_code.co_name)

sys.setprofile(profile)
from boards.de25nano.pc386 import build
from boards.de25nano.pc386.i486dx import derive

derive.sim_sources(Path(sys.argv[1]), Path.cwd())
clock = build.model_clock('486dx')
rates = build.guest_rates('486dx')
sys.setprofile(None)
print(json.dumps({'clock': {'c0': clock['c0'], 'clock_hz': str(clock['clock']), 'guest_rate_parameters': rates},
                  'board_setup_calls': sorted(calls)}))
'''
    root = checkout / MISTEROSS
    regenerated = json.loads(subprocess.check_output([sys.executable, '-B', '-c', program, staging],
                                                    cwd=root, text=True))
    clock = regenerated['clock']
    adapt_bench(root / 'cores/fes-pc386/sim', staging, Fraction(clock['clock_hz']), clock['guest_rate_parameters'])
    return regenerated


def board_projection_equal(original, candidate):
    """Remove only the reviewed compile adapter and normalize its two uses."""
    before = [ast.dump(node) for node in ast.parse(original.read_text()).body]
    after = [ast.dump(node) for node in ast.parse(candidate.read_text()).body]
    wrappers = [node for node in after if hashlib.sha256(node.encode()).hexdigest() == REGISTERED_BOARD_AST_SHA256]
    if len(wrappers) != 1:
        return False
    after.remove(wrappers[0])
    load = 'load(HERE.parent / "machine/board.py", "de25_pc386_machine_board")'
    for old, new in (
        (f'board = {load}', f'board = RegisteredBoard({load})'),
        ('if __name__ == "__main__": board.main(sys.modules[__name__])',
         'if __name__ == "__main__": board.delegate.main(sys.modules[__name__])'),
    ):
        old_node, new_node = (ast.dump(ast.parse(statement).body[0]) for statement in (old, new))
        if after.count(new_node) != 1:
            return False
        after[after.index(new_node)] = old_node
    return before == after


def compare(run, candidate):
    checkout = (run / 'wt-games').resolve()
    games = run / 'games'
    inputs = json.loads((games / 'inputs.json').read_text())
    before, after = source_revision(checkout), source_revision(candidate)
    if before != inputs['revision'] or before != source_revision(run / 'wt'):
        raise ValueError('retained worktrees differ from the source named by the run')
    paths = retained_inputs(run, checkout)
    changed = set(git(candidate, 'diff', '--name-only', '-z', before, after).decode().split('\0')) - {''}
    candidate_board = candidate / BOARD_BUILD
    projected = (BOARD_BUILD in changed and candidate_board.is_file()
                 and board_projection_equal(checkout / BOARD_BUILD, candidate_board))
    # Nothing in these two directories is used by the retained runners. The
    # pc386 board build records the signoff table, and only the Quartus
    # producer's reviewed-finding pass invokes it.
    review_only = {SIGNOFF_TABLE, BOARD_BUILD} if projected else {SIGNOFF_TABLE}
    unexplained = sorted(path for path in changed if path not in review_only
                         and not path.startswith((RECORD, MISTEROSS + 'tests/')))
    modified_inputs = sorted(path for path, expected in paths.items()
                             if not (candidate / path).is_file() or sha(candidate / path) != expected)
    exercised_changes = set(modified_inputs) - ({BOARD_BUILD} if projected else set())
    with tempfile.TemporaryDirectory(dir=run) as directory:
        staging = Path(directory)
        setup = regenerate(candidate, staging)
        clock, calls = setup['clock'], setup['board_setup_calls']
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
    equivalent = (not unexplained and not exercised_changes and clock == old_clock
                  and regenerated == expected and stimulus_equal and 'compile_board' not in calls
                  and {'model_clock', 'guest_rates'}.issubset(calls))
    return {'built_revision': before, 'candidate_revision': after, 'simulation_inputs_equal': equivalent,
            'changes': sorted(changed), 'changes_outside_review_scope': unexplained,
            'changed_retained_inputs': modified_inputs, 'changed_exercised_inputs': sorted(exercised_changes),
            'retained_input_count': len(paths), 'registered_board_projection_equal': projected,
            'pc386_build_sha256': {'built': paths[BOARD_BUILD],
                                   'candidate': sha(candidate_board) if candidate_board.is_file() else None},
            'board_setup_calls': calls,
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
