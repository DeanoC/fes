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
from scripts.quartus_tools import held_quartus_lease  # noqa: E402

RULE = 'TMC-20602'
REVIEW = 'retimed-machine-control-tension'
POLICY = 'de25-pc386-retimed-tension-v2'
PROJECT = Path('combined/hps/project')
DRC = PROJECT / 'output_files/golden_top.tq.drc.signoff.rpt'
FIT_VERSION = PROJECT.parent / 'quartus-version.txt'
TCL = HERE / 'retimed_tension.tcl'
STRUCTURE_TCL = HERE / 'retimed_structure.tcl'
REPORTS = ('targets.txt', 'paths.tsv', 'structure.tsv', 'unconstrained.rpt', 'summary.json')
TIMEOUT_SECONDS = 900
CORNERS = {'Slow fix4 100C Model', 'Slow fix4 0C Model', 'Slow fix4a 0C Model', 'Fast fix4 0C Model'}
CHECKS = {('ownership', ''), ('setup', 'from'), ('setup', 'to'), ('hold', 'from'),
          ('hold', 'to'), ('recovery', 'to'), ('removal', 'to')}
COLUMNS = ['corner', 'node', 'check', 'direction', 'exact_matches', 'owner_clocks', 'async_edges',
           'sync_stage', 'from_clock', 'to_clock', 'slack_ns', 'from', 'to', 'status']
STRUCTURE_COLUMNS = '''corner node direction exact_matches owner_clocks sync_stage is_latch async_inputs async_outputs
clock_outputs keepers machine foreign unclocked port non_register unresolved foreign_examples unclocked_examples
port_examples non_register_examples unresolved_examples setup_inter_clock setup_inter_clock_example setup_false_path
setup_false_path_example hold_inter_clock hold_inter_clock_example hold_false_path hold_false_path_example
async_output_ids async_output_details async_output_own_q async_output_other async_output_unknown status'''.split()
STRUCTURE_ZEROS = ('sync_stage', 'is_latch', 'async_inputs', 'clock_outputs', 'foreign', 'unclocked', 'port',
                   'non_register', 'unresolved', 'setup_inter_clock', 'setup_false_path', 'hold_inter_clock',
                   'hold_false_path', 'async_output_other', 'async_output_unknown')
UCP_PROPERTIES = ('Illegal Clocks', 'Unconstrained Clocks', 'Unconstrained Input Ports',
                  'Paths from Unconstrained Input Ports (Pairs-Only)', 'Unconstrained Output Ports',
                  'Paths to Unconstrained Output Ports (Pairs-Only)')
UCP_TRUNCATED = 'Table was truncated at '


def _target_rows(drc, original_basis=None):
    summary = re.search(r'^; TMC-20602 - [^;]+; Low\s*;\s*(\d+)\s*;', drc, re.M)
    if summary is None or int(summary[1]) == 0:
        return (), ()
    if original_basis is None:
        original_basis = mf.bases().CPU_CONTROL_TENSION
    header, rows = mf.table(drc, RULE)
    mf.require(len(rows) == int(summary[1]), RULE + ' detail rows differ from its violation count')
    for row in rows:
        mf.require(len(row) == len(header) and not row[-1], RULE + ' row changed shape or is waived')
    return header, tuple(row for row in rows
                         if not all(re.fullmatch(pattern, row[column]) for column, pattern in original_basis['match'].items()))


def targets(drc, original_basis=None):
    """List all TMC subjects outside the original basis, including unknown rows in a refused fit."""
    _, rows = _target_rows(drc, original_basis)
    return tuple(row[0] for row in rows)


def reviewed_targets(drc, control_basis, retimed_basis):
    """Require each TMC row to match one basis before choosing retimed subjects."""
    entries = mf.review(drc, '', (control_basis, retimed_basis))
    if not entries:
        return ()
    [entry] = entries
    header, rows = _target_rows(drc, control_basis)
    expected = next(group['rows'] for group in entry['groups'] if group['name'] == REVIEW)
    mf.require(len(rows) == expected, 'retimed tension target count differs from the reviewed basis')
    for row in rows:
        name = re.fullmatch(retimed_basis['match'][0], row[0])
        mf.require(name is not None, 'retimed tension subject differs from the reviewed basis')
        if name['x'] is not None:
            mf.require(header[1:2] == ['Register Location'], RULE + ' register location column changed')
            location = re.fullmatch(r'\(\s*([0-9]+)\s*,\s*([0-9]+)\s*\)', row[1])
            mf.require(location is not None and location.groups() == (name['x'], name['y']),
                       RULE + ' register location does not match the fitted name: ' + row[0])
    return tuple(row[0] for row in rows)


def check(targets_path, paths_path, *, structure_path=None, unconstrained_path=None):
    """Verify the retained checks, including structure and unconstrained paths for live admission."""
    mf.require((structure_path is None) == (unconstrained_path is None),
               'retimed tension requires both structure and unconstrained-path reports')
    reports = (targets_path, paths_path, structure_path, unconstrained_path)
    mf.require(all(Path(path).is_file() and not Path(path).is_symlink() for path in reports if path is not None),
               'missing/linked retimed tension proof report')
    subjects = Path(targets_path).read_text().splitlines()
    mf.require(subjects and len(subjects) == len(set(subjects)) and
               all(re.fullmatch(mf.S + r'[^|\s]+', name) for name in subjects),
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
    summary = {'pass': expected == seen and not issues, 'expected_checks': len(expected), 'observed_checks': len(seen),
               'missing_checks': sorted(expected - seen), 'extra_checks': sorted(seen - expected),
               'minimum_slacks': {name: {'count': len(slacks), 'worst': min(slacks)} for name, slacks in minima.items()},
               'issues': issues}
    if structure_path is not None:
        summary['structure'] = check_structure(subjects, structure_path)
        summary['unconstrained'] = check_ucp(subjects, unconstrained_path)
        summary['pass'] = summary['pass'] and summary['structure']['pass'] and summary['unconstrained']['pass']
    return summary


def own_q_only(row):
    """Check the raw TimeQuest edge as well as its computed count."""
    own_q = row['async_output_own_q']
    if row['async_outputs'] != own_q:
        return False
    ids, details = row['async_output_ids'], row['async_output_details']
    if own_q == '0':
        return not ids and not details
    if own_q != '1':
        return False
    arc = re.fullmatch(r'\{(\S+) asynchronous 0 \S+ reg (\S+) \S+ pin (\S+)\}', details)
    source, pin = row['node'], row['node'] + '|q'
    # Tcl braces bus-bit names when serializing them as list elements.
    return (arc is not None and arc[1] == ids and arc[2] in (source, '{' + source + '}')
            and arc[3] in (pin, '{' + pin + '}'))


def check_structure(subjects, path):
    expected = {(corner, node, direction) for corner in CORNERS for node in subjects for direction in ('from', 'to')}
    seen = set()
    issues = []
    with Path(path).open() as stream:
        reader = csv.DictReader(stream, delimiter='\t')
        mf.require(reader.fieldnames == STRUCTURE_COLUMNS, 'retimed tension structure columns changed')
        for row in reader:
            mf.require(set(row) == set(STRUCTURE_COLUMNS) and None not in row.values(),
                       'retimed tension structure row changed shape')
            key = row['corner'], row['node'], row['direction']
            if key in seen:
                issues.append(dict(reason='duplicate structure row', row=row))
            seen.add(key)
            if key in expected and (row['status'] != 'clear' or row['exact_matches'] != '1'
                                    or row['owner_clocks'] != mf.MACHINE or not own_q_only(row)
                                    or not re.fullmatch(r'[1-9][0-9]*', row['keepers'])
                                    or row['machine'] != row['keepers']
                                    or any(row[name] != '0' for name in STRUCTURE_ZEROS)
                                    or any(row[name] for name in STRUCTURE_COLUMNS if name.endswith('_example')
                                           or name.endswith('_examples'))):
                issues.append(dict(reason='unproved same-clock keeper topology', row=row))
    return {'pass': expected == seen and not issues, 'expected_checks': len(expected), 'observed_checks': len(seen),
            'missing_checks': sorted(expected - seen), 'extra_checks': sorted(seen - expected), 'issues': issues}


def check_ucp(subjects, path):
    """Verify the full report, including the detail row count of every nonzero summary category."""
    text = Path(path).read_text()
    mf.require(UCP_TRUNCATED not in text, 'retimed tension unconstrained-path report was truncated')
    lines = text.strip().splitlines()
    border = re.compile(r'\+[+-]+\+')
    mf.require(lines and border.fullmatch(lines[-1]) and all(
        not line or border.fullmatch(line) or (line.startswith(';') and line.endswith(';')) for line in lines),
        'retimed tension unconstrained-path report is incomplete or malformed')
    row_lines = [index for index, line in enumerate(lines) if line.startswith(';')]
    rows = [tuple(cell.strip() for cell in lines[index].split(';')[1:-1]) for index in row_lines]
    closed = {index for index, line in enumerate(row_lines) if border.fullmatch(lines[line + 1])}
    framed = {index for index in closed if row_lines[index] and border.fullmatch(lines[row_lines[index] - 1])}
    summary_end = 3 + len(UCP_PROPERTIES)
    mf.require(rows[:3] == [('Unconstrained Paths',), ('Unconstrained Paths Summary',), ('Property', 'Setup', 'Hold')]
               and rows[summary_end:summary_end + 2] == [('Clock Status Summary',), ('Target', 'Clock', 'Type', 'Status')],
               'retimed tension unconstrained-path report lacks its summary and clock status')
    mf.require({0, 1, 2, summary_end, summary_end + 1} <= framed and summary_end - 1 in closed,
               'retimed tension unconstrained-path report has incomplete summary tables')
    summary = rows[3:summary_end]
    mf.require(all(len(row) == 3 and row[0] == name and all(re.fullmatch(r'[0-9]+', value) for value in row[1:])
                   for name, row in zip(UCP_PROPERTIES, summary)), 'retimed tension unconstrained summary changed')
    counts = {name: (int(setup), int(hold)) for name, setup, hold in summary}
    mf.require(all(counts[name] == (0, 0) for name in UCP_PROPERTIES[:2]),
               'retimed tension unconstrained-path report has illegal or unconstrained clocks')
    index = summary_end + 2
    clocks = []
    while index < len(rows) and len(rows[index]) == 4:
        clocks.append(rows[index])
        index += 1
    mf.require(clocks and index - 1 in closed and len({row[:2] for row in clocks}) == len(clocks)
               and all(row[0] and row[1] and row[2] in ('Base', 'Generated') and row[3] == 'Constrained' for row in clocks)
               and sum(row[1] == mf.MACHINE for row in clocks) == 1,
               'retimed tension unconstrained-path clock status is incomplete')
    for phase, side in enumerate(('Setup', 'Hold')):
        detail = [(name, counts[name][phase]) for name in UCP_PROPERTIES[2:] if counts[name][phase]]
        if rows[index:index + 1] == [(side + ' Analysis',)]:
            mf.require(index in framed, 'retimed tension unconstrained-path report has an incomplete ' + side + ' section')
            index += 1
        else:
            mf.require(not detail, 'retimed tension unconstrained-path report lacks ' + side + ' details')
        for name, count in detail:
            block = rows[index:index + 2 + count]
            heading = ('From', 'To') if name.startswith('Paths ') else (name.split()[1] + ' Port', 'Comment')
            mf.require(len(block) == 2 + count and block[0] == (name,) and block[1][:2] == heading
                       and {index, index + 1} <= framed and index + 1 + count in closed
                       and len(set(block[2:])) == count and all(len(row) == len(block[1]) for row in block[2:]),
                       'retimed tension unconstrained-path detail count or columns changed for ' + name)
            index += len(block)
    mf.require(index == len(rows), 'unexpected detail in retimed tension unconstrained-path report')
    subject_set = set(subjects)
    tokens = {token for row in rows for cell in row for token in cell.split()}
    matches = sorted({name for token in tokens for name in (token, token.rpartition('|')[0]) if name in subject_set})
    return {'pass': not matches, 'summary_counts': counts, 'matching_subjects': matches}


def collect(selected, quartus_sta, subjects):
    """Bind a successful trace to the compiled database and exact package candidate."""
    mf.require(held_quartus_lease('pro') is not None, 'retimed tension TimeQuest requires a Quartus Pro lease')
    selected = Path(selected).resolve()
    tool = Path(quartus_sta).resolve(strict=True)
    before = async_inputs.bindings(selected)
    tool_sha = async_inputs.sha(tool)
    fit_version_path = selected / FIT_VERSION
    mf.require(fit_version_path.is_file() and not fit_version_path.is_symlink(),
               'retimed tension fit version is missing or linked')
    version = fit_version_path.read_text().strip()
    mf.require(re.fullmatch(r'Version \S+ Build [^\r\n]+ Pro Edition', version),
               'retimed tension fit lacks an exact Quartus Pro version')
    version_sha = async_inputs.sha(fit_version_path)
    sources = {path.relative_to(HERE.parents[2]).as_posix(): async_inputs.sha(path)
               for path in (Path(__file__).resolve(), TCL, STRUCTURE_TCL)}
    record = json.loads((selected.parent / 'build-inputs.json').read_bytes())
    mf.require(record['parameters'].get('model') == '486dx', 'retimed tension proof requires a recorded DX fit')
    mf.require(all(record['source_inputs'].get(name) == digest for name, digest in sources.items()),
               'retimed tension review tools differ from the captured build inputs')
    subjects = tuple(subjects)
    basis = mf.bases()
    current_targets = reviewed_targets((selected / DRC).read_text(), basis.CPU_CONTROL_TENSION,
                                       basis.RETIMED_CONTROL_TENSION)
    mf.require(subjects and subjects == current_targets,
               'retimed tension target list differs from the fitted DRC')
    output = selected / 'retimed-tension'
    output.mkdir(exist_ok=False)
    target_file = output / 'targets.txt'
    target_file.write_text(''.join(name + '\n' for name in subjects))
    target_sha = async_inputs.sha(target_file)
    paths = output / 'paths.tsv'
    structure = output / 'structure.tsv'
    unconstrained = output / 'unconstrained.rpt'
    log = output / 'quartus-sta.log'
    command = [str(tool), '-t', str(TCL), 'golden_top', str(target_file), str(paths), str(structure), str(unconstrained)]
    # TimeQuest writes private report caches into its open project. Copy the
    # compiled input tree so the package's original QDB remains intact.
    with tempfile.TemporaryDirectory(prefix='fitted-project-', dir=output) as work:
        copied = Path(work) / 'signoff'
        shutil.copytree(selected / 'combined', copied / 'combined')
        shutil.copyfile(selected.parent / 'build-inputs.json', copied.parent / 'build-inputs.json')
        mf.require(async_inputs.bindings(copied) == before and async_inputs.sha(copied / FIT_VERSION) == version_sha,
                   'copied retimed tension project differs from the fitted inputs')
        cwd = copied / PROJECT
        with log.open('wb') as stream:
            try:
                process = subprocess.run(command, cwd=cwd, stdout=stream, stderr=subprocess.STDOUT,
                                         timeout=TIMEOUT_SECONDS, check=False)
            except subprocess.TimeoutExpired as error:
                raise mf.BuildError(f'retimed tension TimeQuest timed out; see {output}') from error
    mf.require(async_inputs.bindings(selected) == before and async_inputs.sha(tool) == tool_sha
               and async_inputs.sha(fit_version_path) == version_sha
               and async_inputs.sha(target_file) == target_sha
               and all(async_inputs.sha(HERE.parents[2] / name) == digest for name, digest in sources.items()),
               'retimed tension fitted inputs, target list or review tool changed during TimeQuest')
    log_text = log.read_text(errors='replace')
    reported_versions = re.findall(r'^ *Info: (Version [^\r\n]+)', log_text, re.M)
    mf.require(reported_versions == [version], 'retimed tension TimeQuest version differs from the fitted project')
    mf.require(UCP_TRUNCATED not in log_text, 'retimed tension unconstrained-path report was truncated')
    summary = check(target_file, paths, structure_path=structure, unconstrained_path=unconstrained)
    if process.returncode != 0:
        summary['pass'] = False
        summary['issues'].append({'reason': 'TimeQuest failed', 'exit_code': process.returncode})
    (output / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')
    mf.require(summary['pass'], 'retimed tension proof failed; see ' + str(output))
    receipt = {'policy': POLICY, 'bindings': before, 'sources_sha256': sources,
               'tool': {'path': str(tool), 'sha256': tool_sha, 'version': version, 'fit_version_sha256': version_sha,
                        'log_sha256': async_inputs.sha(log)},
               'command': command, 'cwd': str(cwd), 'reports_sha256': {name: async_inputs.sha(output / name) for name in REPORTS}}
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
    check_args.add_argument('--structure', type=Path)
    check_args.add_argument('--unconstrained', type=Path)
    args = parser.parse_args(argv)
    if args.command == 'targets':
        args.output.write_text(''.join(name + '\n' for name in targets(args.drc.read_text())))
        return 0
    summary = check(args.targets, args.paths, structure_path=args.structure, unconstrained_path=args.unconstrained)
    print(json.dumps(summary, indent=2))
    return int(not summary['pass'])


if __name__ == '__main__':
    sys.exit(main())
