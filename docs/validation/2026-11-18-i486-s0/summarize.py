#!/usr/bin/env python3
"""Write this record's reports and its summary from the runs' own outputs.

  identity  the level-3 oracle's seal identity at two revisions, and whether
            any file pc386's build record hashes changed between them (rule 1)
  level3    the level-3 suites' reports from two checkouts, compared (rule 2)
  suite     the level-4 runner's report: self-check and RTL stops (rules 3, 4)
  boot      the level-4 FreeDOS boot's report (rule 5)
  census    the level-4 census rows, and their comparison with the plan's
  cachechk  CACHECHK at both levels, with the faulting step of each trace (rule 6)
  summary   summary.md from the files the others wrote

Each command reads files the runs wrote and writes one file beside this
script. summary.md has no figure that is not in one of them.
"""
from __future__ import annotations

import argparse
import collections
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[2]
MISTEROSS = REPO / 'sources/misteross'
sys.path.insert(0, str(MISTEROSS))


def write(name: str, value) -> None:
    (HERE / name).write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')


def read(name: str):
    path = HERE / name
    return json.loads(path.read_text()) if path.is_file() else None


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def git(*arguments: str, cwd: Path = REPO) -> str:
    return subprocess.check_output(['git', '-C', str(cwd), *arguments], text=True).strip()


# What each kind of run reads, as path prefixes under the repository, and the
# files under them it does not read. A run made at one commit stands for a
# later one when no input of it changed between the two (inputs_changed).
SOURCES = ('sources/misteross/cores/', 'sources/misteross/scripts/', 'sources/misteross/boards/', 'config/')
SUITE_ONLY = ('sources/misteross/scripts/fes_i486_cases.py', 'sources/misteross/scripts/test_fes_i486_lockstep.py',
              'sources/misteross/cores/fes-common/sim/i386/i486_tb.cpp',
              'sources/misteross/cores/fes-common/sim/i386/i486-expected-stops.txt')
NOT_READ = {
    # The level-3 lockstep, test386 and vectors read nothing of level 4.
    'level3': (*SUITE_ONLY, 'sources/misteross/scripts/fes_i486_oracle.py',
               'sources/misteross/cores/fes-pc386/sim/census.h', 'sources/misteross/cores/fes-pc386/sim/machine_tb.cpp',
               'sources/misteross/scripts/sim_fes_pc386.py'),
    # The machine bench at either level: everything but the directed suite.
    'machine': (*SUITE_ONLY, 'sources/misteross/cores/fes-common/sim/i386/lockstep_tb.cpp'),
    'suite': (),
}


def inputs_changed(kind: str, commit: str, head: str = 'HEAD') -> list[str]:
    changed = git('diff', '--name-only', commit, head).splitlines()
    return [path for path in changed if path.startswith(SOURCES) and path not in NOT_READ[kind]
            and not path.endswith('README.md')]


# ---- rule 1 ----------------------------------------------------------------

def identity_at(revision: str) -> dict:
    """oracle_identity()'s fields from the files as they are at a revision."""
    from scripts import fes_i386_oracle as oracle

    def show(path: str) -> bytes:
        return subprocess.check_output(['git', '-C', str(REPO), 'show', f'{revision}:sources/misteross/{path}'])
    provisioner = show('scripts/fes_i386_oracle.py')
    pin = re.search(rb"BOCHS_REVISION = '([0-9a-f]{40})'", provisioner)
    sim = 'cores/fes-common/sim/i386/'
    return {
        'commit': subprocess.check_output(['git', '-C', str(REPO), 'rev-parse', revision], text=True).strip(),
        'revision': pin.group(1).decode(),
        'patch_sha256': sha256(show(oracle.BOCHS_PATCH_RELATIVE)),
        'provisioner_sha256': sha256(provisioner),
        'instrument_sha256': sha256(show(sim + 'bochs/instrument.h')),
        'adapter_sha256': oracle.named_sources_sha256(oracle.ORACLE_SOURCES,
                                                      [show(sim + name) for name in oracle.ORACLE_SOURCES]),
        'suite_sha256': oracle.named_sources_sha256(oracle.SUITE_SOURCES, [show(name) for name in oracle.SUITE_SOURCES]),
        'files': {name: sha256(show(name)) for name in sorted(
            {'scripts/fes_i386_oracle.py', oracle.BOCHS_PATCH_RELATIVE, *oracle.SUITE_SOURCES,
             *(sim + name for name in oracle.ORACLE_SOURCES)})},
    }


MUST_BE_EQUAL = ('revision', 'patch_sha256', 'provisioner_sha256', 'instrument_sha256')
# Deano, 18 November 2026: these two may move, through these two files only.
MAY_MOVE = ('adapter_sha256', 'suite_sha256')
MAY_CHANGE = ('cores/fes-common/sim/i386/bochs_oracle.cpp', 'cores/fes-common/sim/i386/lockstep_tb.cpp')


def identity(args) -> int:
    base, head = identity_at(args.base), identity_at(args.head)
    fields = {name: {'base': base[name], 'head': head[name], 'equal': base[name] == head[name]}
              for name in (*MUST_BE_EQUAL, *MAY_MOVE)}
    files = {name: {'base': base['files'][name], 'head': head['files'][name],
                    'equal': base['files'][name] == head['files'][name]} for name in base['files']}
    # Every file pc386's build record hashes (build_pc386_product.create_build_record):
    # the producer's pinned inputs and its import closure.
    from scripts import build_fes_pc386_quartus as producer
    from scripts import source_closure
    record_paths = sorted(set(producer.PINNED_INPUTS) | source_closure.imports('build_fes_pc386_quartus', MISTEROSS)
                          | {'scripts/source_closure.py', 'scripts/bootstrap.sh', 'scripts/toolchain_cache.py'})
    changed = set(git('diff', '--name-only', base['commit'], head['commit']).splitlines())
    moved = sorted(path for path in record_paths if 'sources/misteross/' + path in changed)
    passed = (all(fields[name]['equal'] for name in MUST_BE_EQUAL)
              and all(row['equal'] for name, row in files.items() if name not in MAY_CHANGE)
              and set(moved) <= set(MAY_CHANGE))
    write('identity.json', {'base': base['commit'], 'head': head['commit'], 'fields': fields, 'files': files,
                            'must_be_equal': list(MUST_BE_EQUAL), 'may_move': list(MAY_MOVE), 'may_change': list(MAY_CHANGE),
                            'pc386_record_paths': len(record_paths), 'pc386_record_paths_changed': moved,
                            'files_changed': sorted(changed), 'pass': passed})
    print(f'pc386 build record: {len(moved)} of {len(record_paths)} hashed files changed')
    for name, row in fields.items():
        print(f"{name}: {'equal' if row['equal'] else 'MOVED'}")
    return 0 if passed else 1


# ---- rule 2 ----------------------------------------------------------------

# Keys whose values are not behaviour: the tree's revision and source list,
# the oracle and suite identity digests rule 1 reports, binaries built from
# them, and paths.
IGNORED_KEYS = ('source', 'adapter_sha256', 'suite_sha256', 'testbench_sha256', 'linked_sha256', 'report', 'path',
                'directory', 'seconds_wall', 'wall_seconds', 'elapsed')


def differences(base, head, where: str, ignored: set[str], out: list[str]) -> None:
    if isinstance(base, dict) and isinstance(head, dict):
        for key in sorted(set(base) | set(head)):
            at = f'{where}.{key}' if where else str(key)
            if key in IGNORED_KEYS:
                ignored.add(at)
            elif key not in base or key not in head:
                out.append(f'{at}: only in the {"head" if key in head else "base"}')
            else:
                differences(base[key], head[key], at, ignored, out)
    elif isinstance(base, list) and isinstance(head, list) and len(base) == len(head):
        for n, (a, b) in enumerate(zip(base, head)):
            differences(a, b, f'{where}[{n}]', ignored, out)
    elif base != head:
        out.append(f'{where}: base {str(base)[:120]!r}, head {str(head)[:120]!r}')


def strip_paths(value, roots: tuple[str, ...]):
    """A report with each checkout's own path replaced, so paths compare."""
    if isinstance(value, dict):
        return {key: strip_paths(item, roots) for key, item in value.items()}
    if isinstance(value, list):
        return [strip_paths(item, roots) for item in value]
    if isinstance(value, str):
        for root in roots:
            value = value.replace(root, '<checkout>')
    return value


def lockstep_counts(report: dict) -> dict:
    """Every suite's runs, passes and failures, and how many trace hashes it has."""
    out = {}
    for name, section in sorted(report.items()):
        if not isinstance(section, dict):
            continue
        row = {}
        if isinstance(section.get('summary'), dict):
            row = {key: section['summary'][key] for key in ('runs', 'passed', 'failed')}
            row['trace_hash'] = section['summary']['trace_hash']
        elif isinstance(section.get('totals'), dict):
            totals = section['totals']
            row = {'runs': totals.get('runs', totals.get('tests')), 'passed': totals['passed'],
                   'failed': totals.get('runs', totals.get('tests')) - totals['passed']}
            row['trace_hashes'] = len(section.get('trace_hashes', {}))
            row['trace_hashes_sha256'] = sha256(json.dumps(section.get('trace_hashes', {}), sort_keys=True).encode())
        elif name == 'unbuilt':
            row = {'runs': section['encodings'], 'failed': len(section['failures'])}
        if row:
            out[name] = row
    return out


def test386_facts(log: Path, build: Path) -> dict:
    """What test386's runner printed, without paths, and its outputs' digests."""
    lines = [re.sub(r' in /\S+', '', line) for line in log.read_text().splitlines()
             if not line.startswith(('Fetching', 'Building', 'Assembling', 'Corpus', 'Local corpus'))
             and '/build/' not in re.sub(r' in /\S+', '', line)]
    return {'lines': lines, 'dump_sha256': sha256((build / 'fes-i386-test386/test386-dump.bin').read_bytes()),
            'output_sha256': sha256((build / 'fes-i386-test386/test386-output.txt').read_bytes())}


def level3(args) -> int:
    out = {'base_checkout': str(args.base), 'head_checkout': str(args.head), 'ignored_keys': list(IGNORED_KEYS),
           'runs': {}}
    roots = (str(args.base), str(args.head))
    passed = True
    for name, relative in (('lockstep', 'fes-i386-lockstep/report.json'), ('vectors', 'fes-i386-vectors/report.json'),
                           ('boot', 'fes-pc386/boot/report.json')):
        pair = [strip_paths(json.loads((root / 'sources/misteross/build/sim' / relative).read_text()), roots)
                for root in (args.base, args.head)]
        ignored: set[str] = set()
        found: list[str] = []
        differences(pair[0], pair[1], '', ignored, found)
        row = {'report': relative, 'differences': found, 'ignored': sorted(ignored), 'equal': not found}
        if name == 'lockstep':
            row['arguments'] = pair[1]['run']['arguments']
            row['default_suite'] = pair[1]['run']['default_suite']
            row['suites'] = {'base': lockstep_counts(pair[0]), 'head': lockstep_counts(pair[1])}
            row['revisions'] = [pair[n]['source']['fes_revision'] for n in (0, 1)]
            row['tracked_changes'] = [pair[n]['source']['tracked_changes'] for n in (0, 1)]
        if name == 'vectors':
            row['totals'] = {side: {key: pair[n].get(key) for key in ('tests', 'passed', 'failed')}
                             for n, side in enumerate(('base', 'head'))}
        if name == 'boot':
            row['end'] = {side: pair[n].get('end') for n, side in enumerate(('base', 'head'))}
            row['replay'] = {side: (pair[n].get('replay') or {}).get('summary') for n, side in enumerate(('base', 'head'))}
        out['runs'][name] = row
        passed = passed and not found
    facts = [test386_facts(args.logs / f'{tag}-test386.log', root / 'sources/misteross/build/sim')
             for tag, root in (('base', args.base), ('head', args.head))]
    out['runs']['test386'] = {'base': facts[0], 'head': facts[1], 'equal': facts[0] == facts[1]}
    out['exit_status'] = {tag: {path.name[len(tag) + 1:-5]: path.read_text().strip()
                                for path in sorted(args.logs.glob(f'{tag}-*.done')) if '-all-' not in path.name}
                          for tag in ('base', 'head')}
    passed = passed and facts[0] == facts[1]
    out['commits'] = {'base': git('rev-parse', 'HEAD', cwd=args.base), 'head': git('rev-parse', 'HEAD', cwd=args.head)}
    out['pass'] = passed
    write('level3-compare.json', out)
    for name, row in out['runs'].items():
        print(f"{name}: {'equal' if row['equal'] else 'DIFFERENT'}")
    return 0 if passed else 1


# ---- rules 3 and 4 ---------------------------------------------------------

# method.md's prediction, written before the RTL ran: these agree, every
# other case stops at its probe.
PREDICTED_AGREE = ('cmpxchg-a6', 'xadd-reg-lock', 'mov-tr6')


def reset_inputs(commit: str) -> dict:
    """The reset values pc386_system.sv drives into the CPU, read from the file at a commit."""
    text = git('show', f'{commit}:sources/misteross/cores/fes-pc386/rtl/pc386_system.sv')
    found = {name: re.search(r'\.' + name + r'\s*\(([^)]*(?:\([^)]*\))?[^)]*)\)', text) for name in ('init_gpr', 'init_dr6', 'init_cr0')}
    return {name: ' '.join(match.group(1).split()) if match else None for name, match in found.items()}


def suite(args) -> int:
    report = json.loads((args.checkout / 'sources/misteross/build/sim/fes-i486-lockstep/report.json').read_text())
    commit = git('rev-parse', 'HEAD', cwd=args.checkout)
    if git('status', '--porcelain', '--untracked-files=no', cwd=args.checkout):
        raise SystemExit('the checkout has tracked changes')
    write('oracle-selfcheck.json', {'commit': commit, 'oracle': report['oracle'], 'cases_sha256': report['cases_sha256'],
                                    **report['self_check']})
    rows = report['lockstep']['cases']
    for row in rows:
        row['predicted'] = 'agree' if row['label'] in PREDICTED_AGREE else 'stop'
        row['as_predicted'] = (row['rtl'] == 'agree') == (row['predicted'] == 'agree')
    write('expected-stops.json', {'commit': commit, 'machine_reset_inputs': reset_inputs(commit),
                                  'testbench_sha256': report['testbench_sha256'],
                                  'oracle': report['oracle'], 'verilator': report['verilator'],
                                  'cases_sha256': report['cases_sha256'], 'runs_equal': report['lockstep']['runs_equal'],
                                  'cases': rows, 'failures': report['failures']})
    print(f"{len(report['self_check']['cases'])} cases, {len(report['failures'])} failures")
    return 1 if report['failures'] else 0


# ---- rule 5 ----------------------------------------------------------------

def boot(args) -> int:
    report = json.loads((args.checkout / 'sources/misteross/build/sim/fes-pc386-level4/boot/report.json').read_text())
    replay = report.get('replay') or {}
    out = {'commit': git('rev-parse', 'HEAD', cwd=args.checkout),
           'cpu_level': report.get('cpu_level'), 'end': report.get('end'), 'exit': report.get('exit'),
           'failures': report.get('failures'), 'replay_summary': replay.get('summary'),
           'replay_mismatches': len(replay.get('mismatches', [])), 'replay_failures': replay.get('failures'),
           'frames_compared': (replay.get('counts') or {}).get('frames-compared'),
           'frames_matched': (replay.get('counts') or {}).get('frames-matched')}
    out['pass'] = bool(out['cpu_level'] == 4 and out['exit'] == 0 and not out['failures']
                       and (out['end'] or {}).get('reason') == 'prompt' and replay.get('exit') == 0
                       and not out['replay_mismatches'] and not out['replay_failures']
                       and 'replay_mismatches=0' in (out['replay_summary'] or ''))
    write('boot.json', out)
    print(out)
    return 0 if out['pass'] else 1


NAMES = {'0F08': 'invd', '0F09': 'wbinvd', '0FA2': 'cpuid', '0FB0': 'cmpxchg', '0FB1': 'cmpxchg',
         '0FC0': 'xadd', '0FC1': 'xadd', '0F01.7': 'invlpg', '0FA6': 'cmpxchg-a-step', '0FA7': 'cmpxchg-a-step',
         '63': 'arpl', '0FFF': 'undefined-0f-ff', **{f'0F{n:02X}': 'bswap' for n in range(0xc8, 0xd0)}}
# The plan's map of 486 stems; ARPL and 0F FF name exception 6's instruction only.
I486_STEMS = {stem: name for stem, name in NAMES.items() if name not in ('arpl', 'undefined-0f-ff')}
FLAG_BY = {'669D': 'popfd', '9D': 'popf', 'CF': 'iret', '66CF': 'iretd', 'EA': 'jmp-far', 'CD': 'int',
           '0F22.0': 'mov-cr0'}
COMPARED = ('steps', 'end', 'i486', 'eflags', 'cr0_486_bits', 'exceptions')


def census_row(work: Path, title: list[str]) -> dict:
    """One run's counts, as the plan's census.jsonl has them. Counts only."""
    tid, drive, entry, limit = title
    row = {'id': tid, 'drive': drive, 'boot_entry': int(entry), 'limit_s': float(limit), 'oracle': 'bochs-level4',
           'x87_fitted': True}
    report, census = work / 'report.json', work / 'census.txt'
    if report.is_file():
        data = json.loads(report.read_text())
        end = data.get('end') or {}
        game = data.get('game') or {}
        row.update(title=game.get('title'), drive_sha256=(game.get('drive_sha256') or '')[:8], end=end.get('reason'),
                   seconds=end.get('seconds'), steps=end.get('steps'), cpu_level=data.get('cpu_level'))
    if not census.is_file():
        row['census'] = None
        return row
    i486, flags, cr0, exc = (collections.Counter() for _ in range(4))
    modes = collections.defaultdict(collections.Counter)
    total = 0
    for line in census.read_text().splitlines():
        parts = line.split()
        if len(parts) < 2 or not parts[1].isdigit():
            continue
        key, count = parts[0], int(parts[1])
        if key.startswith(('ac:', 'id:')):
            stem = key[3:].split(':')[0]
            flags[key[:2]] += count
            flags[key[:2] + '-by:' + FLAG_BY.get(stem, stem)] += count
        elif key.startswith('cr0.486:'):
            _, bits, stem = key.split(':')[:3]
            cr0[bits + '-by:' + FLAG_BY.get(stem, stem)] += count
        elif key.startswith('exc:'):
            _, vector, stem = key.split(':', 2)
            exc[vector] += count
            if vector == '6':
                exc['6-at:' + NAMES.get(stem.split(':')[0], stem.split(':')[0])] += count
        elif re.fullmatch(r'[0-9A-F]{2,4}(\.[0-9A-Fa-f]+)?(:pm[0-3]|:v86)?', key):
            stem, _, mode = key.partition(':')
            total += count
            if stem in I486_STEMS:
                i486[I486_STEMS[stem]] += count
                modes[I486_STEMS[stem]][mode or 'real'] += count
    row.update(retired_stem_total=total, i486=dict(i486), i486_by_mode={k: dict(v) for k, v in modes.items()},
               eflags=dict(flags), cr0_486_bits=dict(cr0), exceptions=dict(exc))
    return row


def census(args) -> int:
    titles = [line.rstrip('\n').split('\t') for line in args.titles.read_text().splitlines() if line.strip()]
    plan = {}
    text = subprocess.check_output(['git', '-C', str(REPO), 'show', f'{args.plan}:docs/validation/2026-11-17-i486-plan/census.jsonl'],
                                   text=True)
    for line in text.splitlines():
        row = json.loads(line)
        plan[row['id']] = row
    rows, compared = [], []
    for title in titles:
        work = args.runs / f'game-{title[0]}-x87'
        done = args.logs / f'{title[0]}-l4.done'
        if not done.is_file():
            compared.append({'id': title[0], 'run': False})
            continue
        row = census_row(work, title[:4])
        rows.append(row)
        want = plan.get(title[0])
        different = {} if want is None else {key: {'plan': want.get(key), 'slice0': row.get(key)}
                                             for key in COMPARED if want.get(key) != row.get(key)}
        compared.append({'id': title[0], 'run': True, 'in_plan': want is not None, 'equal': want is not None and not different,
                         'differences': different})
    (HERE / 'census.jsonl').write_text(''.join(json.dumps(row, sort_keys=True) + '\n' for row in rows))
    not_run = sorted(set(plan) - {row['id'] for row in rows})
    out = {'commit': git('rev-parse', args.commit), 'host': args.host,
           'exit_status': {path.name[:-len('-l4.done')]: path.read_text().strip() for path in sorted(args.logs.glob('*-l4.done'))},
           'plan_revision': subprocess.check_output(['git', '-C', str(REPO), 'rev-parse', args.plan], text=True).strip(),
           'plan_rows': len(plan), 'rows_run': len(rows), 'rows_equal': sum(1 for row in compared if row.get('equal')),
           'compared_keys': list(COMPARED), 'not_run': not_run, 'rows': compared,
           'pass': len(rows) == len(plan) and all(row.get('equal') for row in compared)}
    write('census-compare.json', out)
    print(f"{out['rows_run']} of {out['plan_rows']} rows run, {out['rows_equal']} equal")
    return 0 if out['pass'] else 1


# ---- rule 6 ----------------------------------------------------------------

def trace_fault(path: Path) -> dict:
    """The faulting step of a scratch DE trace (see the summary's notes): numbers only."""
    rows = [line.split() for line in path.read_text().splitlines() if line.startswith('DE ')]
    last = rows[-1]
    fields = dict(word.split('=') for word in last[3:])
    code = bytes.fromhex(last[2])
    # F7 /6 with a register operand is DIV r16; the register number is ModRM's low three bits.
    register = ('ax', 'cx', 'dx', 'bx', 'sp', 'bp', 'si', 'di')[code[1] & 7]
    is_div16 = code[0] == 0xf7 and code[1] >> 3 == 0b11110
    values = {name: int(fields['e' + name], 16) & 0xffff for name in ('ax', 'cx', 'dx', 'bx', 'si', 'di')}
    return {'steps_in_trace': len(rows), 'site': last[1], 'seconds': float(fields['t']),
            'instruction': f'DIV {register.upper()}' if is_div16 else 'not a 16-bit register DIV',
            'dividend_dx_ax': f"{values['dx']:04x}:{values['ax']:04x}",
            'divisor': f'{values[register]:04x}' if register in values else None,
            'divide_by_zero': is_div16 and values.get(register) == 0}


def cachechk(args) -> int:
    out = {'commit': git('rev-parse', args.commit), 'host': args.host, 'levels': {}}
    for level in (3, 4):
        report = json.loads((args.directory / f'report-l{level}.json').read_text())
        screen = report.get('screen', [])
        clock = next((re.search(r'([0-9.]+) MHz', row).group(1) for row in screen if 'MHz' in row), None)
        out['levels'][str(level)] = {
            'end': report.get('end'), 'divide_error_printed': any('Divide error' in row for row in screen),
            'clock_mhz_printed': clock, 'trace': trace_fault(args.directory / f'de-l{level}.txt')}
    three, four = out['levels']['3'], out['levels']['4']
    same_site = three['trace']['site'] == four['trace']['site']
    both = three['divide_error_printed'] and four['divide_error_printed']
    out['same_fault_site'] = same_site
    explained = both and same_site and three['trace']['divide_by_zero'] and four['trace']['divide_by_zero']
    out['result'] = 'explained' if explained else 'still open'
    out['one_line'] = ('It is not a level-4 behaviour: the level-3 oracle prints it too, at the same instruction, '
                       'a division by zero in the program\'s own arithmetic after it measures an impossible clock.'
                       if explained else 'The two levels did not show the same fault; see below.')
    findings = []
    for level, row in (('3', three), ('4', four)):
        trace = row['trace']
        findings.append(
            f"Level {level}: the run ends at `{row['end']['reason']}` after {row['end']['seconds']} simulated seconds and "
            f"{row['end']['steps']:,} steps; \"Divide error\" on the screen: {'yes' if row['divide_error_printed'] else 'no'}; "
            f"the clock it prints: {row['clock_mhz_printed']} MHz. Exception 0 is raised at `{trace['site']}` at "
            f"{trace['seconds']} s by `{trace['instruction']}` with DX:AX {trace['dividend_dx_ax']} and divisor "
            f"{trace['divisor']}: {'a division by zero' if trace['divide_by_zero'] else 'not a division by zero'}.")
    findings.append(f"The fault site is the same at both levels: {'yes' if same_site else 'no'}.")
    findings.append('What was measured: the control, the faulting instruction and its operands at each level, from '
                    'a scratch trace of the last 3,000 oracle steps before the first exception 0. The divisor is '
                    'the low word of a 32-bit value the program\'s own scaling routine had just shifted left; the '
                    'trace window (about 0.1 ms) does not reach back to the timing loop that produced the value.')
    findings.append('What that supports: the oracle has no CPU speed (every instruction takes the same machine '
                    'time), so the program\'s timing gives a clock no CPU has, at either level, and its arithmetic '
                    'then divides by zero. The committed kit runs of CACHECHK on the RTL, which has a speed, print '
                    'its table. What was not measured: which timing loop, and why the two levels print different '
                    'clocks.')
    out['findings'] = findings
    write('cachechk.json', out)
    print(out['result'], same_site)
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    commands = parser.add_subparsers(dest='command', required=True)
    sub = commands.add_parser('identity')
    sub.add_argument('--base', required=True)
    sub.add_argument('--head', default='HEAD')
    sub = commands.add_parser('level3')
    for name in ('--base', '--head', '--logs'):
        sub.add_argument(name, type=Path, required=True)
    for name in ('suite', 'boot'):
        sub = commands.add_parser(name)
        sub.add_argument('--checkout', type=Path, required=True, help='the clean checkout the run was made in')
    sub = commands.add_parser('census')
    for name in ('--runs', '--logs', '--titles'):
        sub.add_argument(name, type=Path, required=True)
    sub.add_argument('--commit', required=True, help='the commit the sweep ran at')
    sub.add_argument('--host', required=True)
    sub.add_argument('--plan', default='origin/deano/i486-plan-50e04f')
    sub = commands.add_parser('cachechk')
    sub.add_argument('--directory', type=Path, required=True, help='report-l3.json, report-l4.json, de-l3.txt, de-l4.txt')
    sub.add_argument('--commit', required=True)
    sub.add_argument('--host', required=True)
    commands.add_parser('summary')
    args = parser.parse_args()
    if args.command == 'summary':
        from summary_text import render
        (HERE / 'summary.md').write_text(render(read, inputs_changed, git('rev-parse', 'HEAD')))
        return 0
    return {'identity': identity, 'level3': level3, 'suite': suite, 'boot': boot, 'census': census,
            'cachechk': cachechk}[args.command](args)


if __name__ == '__main__':
    sys.exit(main())
