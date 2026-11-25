#!/usr/bin/env python3
"""Collect and check the fitted register proof for pc386's retiming advisory."""
import argparse
from collections import defaultdict
import csv
import json
import math
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

HERE = Path(__file__).resolve().parent
if __name__ == '__main__':
    sys.path.insert(0, str(HERE.parents[2]))

from boards.de25nano.scanout import async_inputs, machine_findings as mf  # noqa: E402

RULE = 'TMC-20602'
REVIEW = 'retimed-machine-control-tension'
POLICY = 'de25-pc386-retimed-tension-v1'
PROJECT = Path('combined/hps/project')
DRC = PROJECT / 'output_files/golden_top.tq.drc.signoff.rpt'
TCL = HERE / 'retimed_tension.tcl'
TIMEOUT_SECONDS = 900
CORNERS = {'Slow fix4 100C Model', 'Slow fix4 0C Model', 'Slow fix4a 0C Model', 'Fast fix4 0C Model'}
CHECKS = {('ownership', ''), ('setup', 'from'), ('setup', 'to'), ('hold', 'from'),
          ('hold', 'to'), ('recovery', 'to'), ('removal', 'to')}
COLUMNS = ['corner', 'node', 'check', 'direction', 'exact_matches', 'owner_clocks', 'async_edges',
           'sync_stage', 'from_clock', 'to_clock', 'slack_ns', 'from', 'to', 'status']


def targets(drc, original_basis=None):
    """List all TMC rows outside the original basis, including unknown rows in a refused fit."""
    summary = re.search(r'^; TMC-20602 - [^;]+; Low\s*;\s*(\d+)\s*;', drc, re.M)
    if summary is None or int(summary[1]) == 0:
        return ()
    if original_basis is None:
        original_basis = mf.bases().CPU_CONTROL_TENSION
    header, rows = mf.table(drc, RULE)
    mf.require(len(rows) == int(summary[1]), RULE + ' detail rows differ from its violation count')
    for row in rows:
        mf.require(len(row) == len(header) and not row[-1], RULE + ' row changed shape or is waived')
    return tuple(row[0] for row in rows
                 if not all(re.fullmatch(pattern, row[column]) for column, pattern in original_basis['match'].items()))


def reviewed_targets(drc, control_basis, retimed_basis):
    """Require each TMC row to match one basis before choosing retimed subjects."""
    entries = mf.review(drc, '', (control_basis, retimed_basis))
    if not entries:
        return ()
    [entry] = entries
    subject_names = targets(drc, control_basis)
    expected = next(group['rows'] for group in entry['groups'] if group['name'] == REVIEW)
    mf.require(len(subject_names) == expected, 'retimed tension target count differs from the reviewed basis')
    return subject_names


def check(targets_path, paths_path):
    """Verify exact coverage, ownership and worst paths independently of the Tcl process status."""
    subjects = Path(targets_path).read_text().splitlines()
    mf.require(subjects and len(subjects) == len(set(subjects)) and
               all(re.fullmatch(mf.S + r'\S+', name) for name in subjects),
               'retimed tension targets are empty, repeated or outside pc386')
    expected = {(corner, node, kind, direction)
                for corner in CORNERS for node in subjects for kind, direction in CHECKS}
    seen = set()
    issues = []
    minima = defaultdict(list)
    with Path(paths_path).open() as stream:
        reader = csv.DictReader(stream, delimiter='\t')
        mf.require(reader.fieldnames == COLUMNS, 'retimed tension trace columns changed')
        for row in reader:
            mf.require(set(row) == set(COLUMNS) and None not in row.values(),
                       'retimed tension trace row changed shape')
            corner, node, kind, direction = (row[name] for name in ('corner', 'node', 'check', 'direction'))
            key = corner, node, kind, direction
            if key in seen:
                issues.append(dict(reason='duplicate evidence row', row=row))
            seen.add(key)
            if key not in expected:
                continue
            owner = tuple(row[name] for name in ('exact_matches', 'owner_clocks', 'async_edges', 'sync_stage'))
            if owner != ('1', mf.MACHINE, '0', '0'):
                issues.append(dict(reason='unproved synchronous machine-clock register ownership', row=row))
            if row['status'] == 'n/a' and kind in ('recovery', 'removal'):
                continue
            if row['status'] != 'ok':
                issues.append(dict(reason='missing or unresolved evidence', row=row))
                continue
            if kind == 'ownership':
                continue
            try:
                slack = float(row['slack_ns'])
            except ValueError:
                slack = math.nan
            if (row[direction] != node or row[direction + '_clock'] != mf.MACHINE
                    or not math.isfinite(slack) or slack < 0):
                issues.append(dict(reason='endpoint or timing result does not qualify', row=row))
                continue
            if kind in ('setup', 'hold') and (row['from_clock'], row['to_clock']) != (mf.MACHINE, mf.MACHINE):
                issues.append(dict(reason='setup or hold path crosses a clock domain', row=row))
                continue
            minima[kind].append((slack, corner, node))
    return {'pass': expected == seen and not issues, 'expected_checks': len(expected), 'observed_checks': len(seen),
            'missing_checks': sorted(expected - seen), 'extra_checks': sorted(seen - expected),
            'minimum_slacks': {name: {'count': len(slacks), 'worst': min(slacks)} for name, slacks in minima.items()},
            'issues': issues}


def collect(selected, quartus_sta, subjects):
    """Bind a successful trace to the compiled database and exact package candidate."""
    selected = Path(selected).resolve()
    tool = Path(quartus_sta).resolve(strict=True)
    output = selected / 'retimed-tension'
    output.mkdir(exist_ok=False)
    before = async_inputs.bindings(selected)
    tool_sha = async_inputs.sha(tool)
    sources = {path.relative_to(HERE.parents[2]).as_posix(): async_inputs.sha(path)
               for path in (Path(__file__).resolve(), TCL)}
    record = json.loads((selected.parent / 'build-inputs.json').read_bytes())
    mf.require(all(record['source_inputs'].get(name) == digest for name, digest in sources.items()),
               'retimed tension review tools differ from the captured build inputs')
    subjects = tuple(subjects)
    mf.require(subjects and subjects == targets((selected / DRC).read_text()),
               'retimed tension target list differs from the fitted DRC')
    target_file = output / 'targets.txt'
    target_file.write_text(''.join(name + '\n' for name in subjects))
    target_sha = async_inputs.sha(target_file)
    paths = output / 'paths.tsv'
    command = [str(tool), '-t', str(TCL), 'golden_top', str(target_file), str(paths)]
    # TimeQuest writes private report caches into its open project. Copy the
    # compiled input tree so the package's original QDB remains intact.
    with tempfile.TemporaryDirectory(prefix='fitted-project-', dir=output) as work:
        copied = Path(work) / 'signoff'
        shutil.copytree(selected / 'combined', copied / 'combined')
        shutil.copyfile(selected.parent / 'build-inputs.json', copied.parent / 'build-inputs.json')
        mf.require(async_inputs.bindings(copied) == before,
                   'copied retimed tension project differs from the fitted inputs')
        cwd = copied / PROJECT
        with (output / 'quartus-sta.log').open('wb') as stream:
            try:
                process = subprocess.run(command, cwd=cwd, stdout=stream, stderr=subprocess.STDOUT,
                                         timeout=TIMEOUT_SECONDS, check=False)
            except subprocess.TimeoutExpired as error:
                raise mf.BuildError(f'retimed tension TimeQuest timed out; see {output}') from error
    mf.require(async_inputs.bindings(selected) == before and async_inputs.sha(tool) == tool_sha
               and async_inputs.sha(target_file) == target_sha
               and all(async_inputs.sha(HERE.parents[2] / name) == digest for name, digest in sources.items()),
               'retimed tension fitted inputs, target list or review tool changed during TimeQuest')
    summary = check(target_file, paths)
    if process.returncode != 0:
        summary['pass'] = False
        summary['issues'].append({'reason': 'TimeQuest failed', 'exit_code': process.returncode})
    (output / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')
    mf.require(summary['pass'], 'retimed tension proof failed; see ' + str(output))
    receipt = {'policy': POLICY, 'bindings': before, 'sources_sha256': sources,
               'tool': {'path': str(tool), 'sha256': tool_sha}, 'command': command, 'cwd': str(cwd),
               'reports_sha256': {name: async_inputs.sha(output / name)
                                  for name in ('targets.txt', 'paths.tsv', 'summary.json')}}
    (output / 'receipt.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')
    return output


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    target_args = commands.add_parser('targets')
    target_args.add_argument('drc', type=Path)
    target_args.add_argument('output', type=Path)
    check_args = commands.add_parser('check')
    check_args.add_argument('targets', type=Path)
    check_args.add_argument('paths', type=Path)
    args = parser.parse_args(argv)
    if args.command == 'targets':
        args.output.write_text(''.join(name + '\n' for name in targets(args.drc.read_text())))
        return 0
    summary = check(args.targets, args.paths)
    print(json.dumps(summary, indent=2))
    return int(not summary['pass'])


if __name__ == '__main__':
    sys.exit(main())
