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
# The reviewed RegisteredBoard versions only store the delegate on import
# and forward the ROOT lookup. The proof variant runs the endpoint review
# inside the registered Quartus compile path.
REGISTERED_BOARD_AST = {
    '9495bbef66d52666a0d86062c270f2e2f42bc18fe21c14c566bdd409b52306c5': 'e255d4e3-adapter',
    '5cf925e4f0b94b0771843874fb36f51d54ea7c7e9fe80416ec29ec2b5e4741e2': 'f9844c1b-proof',
    'ba4054b71bd84e93739d18ae355395357449ae23730f572b9992369f801a0cb8': 'd18a5a45-lease',
}
REVIEW_FILE_SHA256 = {
    'f9844c1b-proof': {
        'retimed_tension.py': '9640b71466b7fb9f5a6f90f9f044289dd8ac771dd010bd74aefbad5b73ba474f',
        'retimed_tension.tcl': '387d6f62cce34e76fd72bb45b0ff840a00dfe08ab2a1f0f2ec433685c32a38ac',
    },
    'd18a5a45-lease': {
        'retimed_tension.py': '8a32226adadf63015a0401412fcb005a14b01fb9169303568c6693a1e1a81dc0',
        'retimed_tension.tcl': '387d6f62cce34e76fd72bb45b0ff840a00dfe08ab2a1f0f2ec433685c32a38ac',
    },
    'b6310cd4-structure': {
        'retimed_tension.py': 'be5621fddf33d8024b4daeff25ad3c925a671e624b5e6afc65a6d7226012c908',
        'retimed_tension.tcl': '39dc573010e723a9af4ecd3cd5f20d682c5aca37b2a42c0d3d6ac0d47b5387ac',
        'retimed_structure.tcl': '32a552327af4fbb4974b5d97fbcf59c5fe5c3e8e8464389d200b14697d19cced',
    },
}
REVIEW_FILE_SHA256_OVERRIDES = {
    ('1106c799322514432b4631fc17439be4055043dc', 'b6310cd4-structure'): {
        'retimed_tension.py': '9207dfc1d5db543a0eb29cb3b379a181b0617542a43026bfcb21315888bf50de',
    },
}
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
from hashlib import sha256
from pathlib import Path
import sys

calls = set()
proof_calls = set()
lease_calls = set()
used_sources = set()
root = Path.cwd()
module_path = str(root / 'boards/de25nano/pc386/build.py')
proof_path = str(root / 'boards/de25nano/pc386/retimed_tension.py')
lease_path = str(root / 'scripts/quartus_tools.py')
prefix = str(root) + '/'

def profile(frame, event, _arg):
    if event == 'call':
        filename = frame.f_code.co_filename
        if filename.startswith(prefix):
            used_sources.add(filename[len(prefix):])
        if filename == module_path:
            calls.add(frame.f_code.co_name)
        elif filename == proof_path:
            proof_calls.add(frame.f_code.co_name)
        elif filename == lease_path:
            lease_calls.add(frame.f_code.co_name)

sys.setprofile(profile)
from boards.de25nano.pc386 import build
from boards.de25nano.pc386.i486dx import derive

derive.sim_sources(Path(sys.argv[1]), Path.cwd())
clock = build.model_clock('486dx')
rates = build.guest_rates('486dx')
sys.setprofile(None)
print(json.dumps({'clock': {'c0': clock['c0'], 'clock_hz': str(clock['clock']), 'guest_rate_parameters': rates},
                  'board_setup_calls': sorted(calls), 'registered_retiming_proof_calls': sorted(proof_calls),
                  'quartus_tools_setup_calls': sorted(lease_calls),
                  'setup_sources': {path: sha256((root / path).read_bytes()).hexdigest()
                                    for path in sorted(used_sources)}}))
'''
    root = checkout / MISTEROSS
    regenerated = json.loads(subprocess.check_output([sys.executable, '-B', '-c', program, staging],
                                                    cwd=root, text=True))
    clock = regenerated['clock']
    adapt_bench(root / 'cores/fes-pc386/sim', staging, Fraction(clock['clock_hz']), clock['guest_rate_parameters'])
    return regenerated


def board_projection(original, candidate):
    """Remove only the reviewed compile adapter and its source-capture changes."""
    before = [ast.dump(node) for node in ast.parse(original.read_text()).body]
    text = candidate.read_text()
    after = [ast.dump(node) for node in ast.parse(text).body]
    wrappers = [(node, REGISTERED_BOARD_AST[digest]) for node in after
                if (digest := hashlib.sha256(node.encode()).hexdigest()) in REGISTERED_BOARD_AST]
    if len(wrappers) != 1:
        return None
    wrapper, version = wrappers[0]
    load = 'load(HERE.parent / "machine/board.py", "de25_pc386_machine_board")'
    if version in REVIEW_FILE_SHA256:
        expanded = 'SOURCES = ({BOARD + "pc386/" + name for name in (*LOCAL_FILES, *REVIEW_FILES)}'
        prior = 'SOURCES = ({BOARD + "pc386/" + name for name in LOCAL_FILES}'
        if text.count(expanded) != 1:
            return None
        after = [ast.dump(node) for node in ast.parse(text.replace(expanded, prior)).body]
        variants = (version, 'b6310cd4-structure') if version == 'd18a5a45-lease' else (version,)
        reviews = []
        for variant in variants:
            names = tuple(REVIEW_FILE_SHA256[variant])
            review = ast.dump(ast.parse(f'REVIEW_FILES = {names!r}').body[0])
            if after.count(review) == 1:
                reviews.append((variant, review))
        if len(reviews) != 1:
            return None
        version, review = reviews[0]
        after.remove(review)
    after.remove(wrapper)
    if version in ('d18a5a45-lease', 'b6310cd4-structure'):
        imports = ('from scripts import pc386_quartus as sealer\n'
                   'from scripts import yosys_netlist\n'
                   'from scripts import fit_i386_device as fit\n')
        board = (f'board = RegisteredBoard({load})\n'
                 'dx = load(HERE / "i486dx/derive.py", "de25_pc386_i486dx_derive")\n')
        new = ('sys.path.insert(0, str(HERE.parents[2]))\n' + imports
               + 'from scripts.quartus_tools import quartus_lease\n' + board)
        old = board + 'sys.path.insert(0, str(board.ROOT / MISTEROSS))\n' + imports
        sequence = [ast.dump(node) for node in ast.parse(new).body]
        positions = [index for index in range(len(after)) if after[index:index + len(sequence)] == sequence]
        if len(positions) != 1:
            return None
        position = positions[0]
        after[position:position + len(sequence)] = [ast.dump(node) for node in ast.parse(old).body]
    for old, new in (
        (f'board = {load}', f'board = RegisteredBoard({load})'),
        ('if __name__ == "__main__": board.main(sys.modules[__name__])',
         'if __name__ == "__main__": board.delegate.main(sys.modules[__name__])'),
    ):
        old_node, new_node = (ast.dump(ast.parse(statement).body[0]) for statement in (old, new))
        if after.count(new_node) != 1:
            return None
        after[after.index(new_node)] = old_node
    return version if before == after else None


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
    projection = (board_projection(checkout / BOARD_BUILD, candidate_board)
                  if BOARD_BUILD in changed and candidate_board.is_file() else None)
    # Nothing in these two directories is used by the retained runners. The
    # pc386 board build records the signoff table, and only the Quartus
    # producer's reviewed-finding pass invokes it.
    review_only = {SIGNOFF_TABLE, BOARD_BUILD} if projection else {SIGNOFF_TABLE}
    proof_files = REVIEW_FILE_SHA256.get(projection, {}) | REVIEW_FILE_SHA256_OVERRIDES.get((after, projection), {})
    proof_expected = {MISTEROSS + 'boards/de25nano/pc386/' + name: digest
                      for name, digest in proof_files.items()}
    proof_hashes = {path: sha(candidate / path) if (candidate / path).is_file() else None
                    for path in proof_expected}
    proof_hashes_equal = proof_hashes == proof_expected
    if proof_hashes_equal:
        review_only.update(proof_hashes)
    unexplained = sorted(path for path in changed if path not in review_only
                         and not path.startswith((RECORD, MISTEROSS + 'tests/')))
    modified_inputs = sorted(path for path, expected in paths.items()
                             if not (candidate / path).is_file() or sha(candidate / path) != expected)
    exercised_changes = set(modified_inputs) - ({BOARD_BUILD} if projection else set())
    with tempfile.TemporaryDirectory(dir=run) as directory:
        staging = Path(directory) / 'candidate'
        setup = regenerate(candidate, staging)
        baseline = regenerate(checkout, staging.parent / 'built')
        clock, calls = setup['clock'], setup['board_setup_calls']
        old_clock = {key: inputs[key] for key in clock}
        if baseline['clock'] != old_clock:
            raise ValueError('the retained clock no longer matches its measured setup')
        regenerated = {name: sha(staging / name) for name in ('machine_tb.cpp', 'audio_link.h', 'fes_i386_cpu.sv')}
        expected = {**inputs['adapted_bench'], 'fes_i386_cpu.sv': sha(games / 'derived/fes_i386_cpu.sv')}
    setup_sources, baseline_sources = setup['setup_sources'], baseline['setup_sources']
    setup_changes = sorted(MISTEROSS + path for path in setup_sources.keys() | baseline_sources.keys()
                           if setup_sources.get(path) != baseline_sources.get(path))
    setup_exercised_changes = set(setup_changes) - ({BOARD_BUILD} if projection else set())
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
                  and {'model_clock', 'guest_rates'}.issubset(calls)
                  and not setup['registered_retiming_proof_calls']
                  and 'quartus_lease' not in setup['quartus_tools_setup_calls']
                  and not setup_exercised_changes and proof_hashes_equal)
    return {'built_revision': before, 'candidate_revision': after, 'simulation_inputs_equal': equivalent,
            'changes': sorted(changed), 'changes_outside_review_scope': unexplained,
            'changed_retained_inputs': modified_inputs, 'changed_exercised_inputs': sorted(exercised_changes),
            'retained_input_count': len(paths), 'registered_board_projection_equal': bool(projection),
            'registered_board_projection': projection,
            'pc386_build_sha256': {'built': paths[BOARD_BUILD],
                                   'candidate': sha(candidate_board) if candidate_board.is_file() else None},
            'board_setup_calls': calls, 'review_file_sha256': proof_hashes,
            'registered_retiming_proof_calls': setup['registered_retiming_proof_calls'],
            'quartus_tools_setup_calls': setup['quartus_tools_setup_calls'],
            'setup_source_count': len(setup_sources), 'changed_setup_sources': setup_changes,
            'changed_exercised_setup_sources': sorted(setup_exercised_changes),
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
