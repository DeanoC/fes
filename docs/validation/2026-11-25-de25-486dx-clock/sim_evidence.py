#!/usr/bin/env python3
"""Summarize completed simulation gates and verify retained evidence by digest.

ROOT holds wt, wt-games, lockstep and games for one candidate. The receipt
hashes completed artifacts only, so later gates may finish without changing
the evidence for an earlier gate.
"""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

from sim_games import sha


def source_revision(checkout):
    def git(*args):
        return subprocess.check_output(['git', '-C', checkout, *args], text=True).strip()

    if git('status', '--porcelain', '--untracked-files=no'):
        raise ValueError(f'{checkout} has local source changes')
    return git('rev-parse', 'HEAD')


def lockstep_pair(root, level, revision):
    original = Path(__file__).resolve().parent.parent / '2026-11-24-i486dx-s1'
    paths = [root / 'lockstep' / f'{level}-{arm}.json' for arm in ('base', 'dx')]
    if not all(path.is_file() for path in paths):
        return {'status': 'pending'}, []
    base, dx = (json.loads(path.read_text()) for path in paths)
    dx_source = dx['source'] if level == 'l3' else dx['i486dx_source']
    if dx_source['fes_revision'] != revision or (level == 'l3' and base['source']['fes_revision'] != revision):
        raise ValueError(f'{level} report revision differs from the run source')
    lines = subprocess.check_output(['python3', original / 'g2_compare.py', *paths], text=True).splitlines()
    verdict = {'status': lines[-1].lower(), 'comparison': lines[:2]}
    for arm, report in (('base', base), ('dx', dx)):
        if level == 'l3':
            verdict[arm] = {'corpus_tests': report['corpus']['totals']['passed'],
                            'random_programs': report['random']['totals']['passed'],
                            'x87_cases': report['x87_cases']['summary']['passed'],
                            'x87_built_forms': report['x87_built_forms']['output']}
        else:
            verdict[arm] = {'directed_agree': report['lockstep']['486sx']['agree'],
                            'random_programs': sum(result['totals']['passed']
                                                   for result in report['random']['sets'].values())}
    return verdict, paths


def x87_suites(root, revision):
    paths = {name: root / 'lockstep' / f'{name}.json' for name in ('x87unit', 'trans', 'testfloat')}
    paths = {name: path for name, path in paths.items() if path.is_file()}
    results = {name: json.loads(path.read_text()) for name, path in paths.items()}
    testfloat = results.get('testfloat')
    reports = [testfloat['unit_report'] if name == 'testfloat' else result
               for name, result in results.items()]
    if any(report['source']['fes_revision'] != revision for report in reports):
        raise ValueError('x87 report revision differs from the run source')
    failed = any(report['failed'] for report in reports) or (testfloat and testfloat['unit_returncode'] != 0)
    if failed:
        status = 'fail'
    else:
        status = 'pass' if len(reports) == 3 else 'pending'
    evidence = {'status': status}
    for field, name in (('unit_passed', 'x87unit'), ('trans_passed', 'trans')):
        if name in results:
            evidence[field] = results[name]['passed']
    if testfloat:
        evidence.update(testfloat_vectors=testfloat['vectors'], testfloat_configs=len(testfloat['configs']),
                        testfloat_gen_sha256=testfloat['testfloat_gen_sha256'])
    return evidence, list(paths.values())


def checked_presenter(games, inputs):
    presenter = inputs.get('presenter')
    if presenter is None:
        return None
    path = games / presenter['derived']
    if (not path.resolve().is_relative_to(games.resolve()) or path.is_symlink()
            or not path.is_file() or sha(path) != presenter['derived_sha256']):
        raise ValueError('derived presenter is missing or differs from its recorded digest')
    return path


def checked_x87(root, inputs, lockstep_reports):
    pipeline = inputs.get('x87_fast_pipeline')
    if pipeline is None:
        return {}
    files = {}

    def check(base, name, expected):
        path = base / name
        if (not path.resolve().is_relative_to(base) or path.is_symlink() or not path.is_file()
                or sha(path) != expected):
            raise ValueError(f'x87 replay input is missing or differs from its recorded digest: {path}')
        files[path.relative_to(root).as_posix()] = expected
        return path

    games = root / 'games'
    derivation = pipeline['derivation']
    upstream = check(root / 'wt-games/sources/misteross', derivation['upstream'], derivation['source_sha256'])
    derived = check(games, pipeline['derived'], derivation['derived_sha256'])
    for arm, selected in (('base', upstream), ('dx', derived)):
        role = pipeline['processor_dependencies'][arm]
        deps = check(games, role['dependency'], role['dependency_sha256'])
        archive = check(games, role['archive'], role['archive_sha256'])
        link_log = check(games, role['link_log'], role['link_log_sha256'])
        if (set(deps.read_text().split()).intersection(map(str, (upstream, derived))) != {str(selected)}
                or sha(selected) != role['compiled_sha256']):
            raise ValueError(f'{arm} CPU did not compile its recorded x87 wrapper')
        if not any(str(archive) in line.split() and line.endswith('-o Vpc386_machine')
                   for line in link_log.read_text().splitlines()):
            raise ValueError(f'{arm} game model did not link the recorded CPU archive')

    lockstep = root / 'wt/sources/misteross'
    if 'fast-pipeline' in lockstep_reports:
        report = json.loads((root / 'lockstep/fast-pipeline.json').read_text())
        if report['derivation'] != derivation:
            raise ValueError('DX lockstep used a different x87 derivation from the game arm')
        for path, digest in report['files_sha256'].items():
            check(lockstep, path, digest)
    elif 'x87unit' in lockstep_reports:
        raise ValueError('the retained lockstep reports lack the compiled x87 witness')
    for name in ('x87unit', 'testfloat'):
        if name not in lockstep_reports:
            continue
        report = json.loads((root / 'lockstep' / f'{name}.json').read_text())
        if name == 'testfloat':
            report = report['unit_report']
        actual = report.get('fast_pipeline')
        if actual is None or actual['derivation'] != derivation:
            raise ValueError(f'{name} lacks the exact DE25 DX x87 derivation')
        for path, digest in actual['files_sha256'].items():
            check(lockstep, path, digest)
        if not report['failed'] and (not actual['focused_checks_passed'] or actual['fmul_fwait_clocks'] != 19):
            raise ValueError(f'{name} lacks the seven-stage x87 protocol and latency proof')
    return files


def summarize(root):
    games = root / 'games'
    inputs = json.loads((games / 'inputs.json').read_text())
    revision = inputs['revision']
    if any(source_revision(root / name) != revision for name in ('wt', 'wt-games')):
        raise ValueError('the game and lockstep worktrees must match the candidate revision')
    files = [games / name for name in ('inputs.json', 'harness.py', 'machine_tb.cpp', 'audio_link.h')]
    if presenter := checked_presenter(games, inputs):
        files.append(presenter)
    result = {key: inputs[key] for key in ('revision', 'model', 'c0', 'clock_hz', 'verilator')}
    result['class'] = 'host RTL simulation'
    result['games'] = {'artifact_sha256': inputs['binaries'], 'stimulus_sha256': inputs['fixtures']}
    for gate, run in (('l3', lockstep_pair(root, 'l3', revision)),
                      ('l4', lockstep_pair(root, 'l4', revision)),
                      ('x87', x87_suites(root, revision))):
        result[gate], artifacts = run
        files.extend(artifacts)
    pipeline_report = root / 'lockstep/fast-pipeline.json'
    if inputs.get('x87_fast_pipeline') and pipeline_report.is_file():
        files.append(pipeline_report)
    analysis = games / 'analysis.json'
    if analysis.is_file():
        data = analysis.read_bytes()
        report = json.loads(data)
        snapshot = games / f'analysis-{hashlib.sha256(data).hexdigest()}.json'
        if not snapshot.exists():
            snapshot.write_bytes(data)
        files.append(snapshot)
        result['games']['g4_cpi_pass'] = report['g4_pass']
        result['games']['cpi_ratio'] = {key: run['cpi_ratio'] for key, run in report['cpi'].items()}
        quake = report['quake_execution']
        result['games']['quake_execution'] = quake['pass'] if quake else None
        result['games']['frame_gate'] = 'check retained frames'
        for directory in sorted(games.glob('*-*')):
            if (directory / 'result.json').is_file():
                files.extend(path for name in ('command.json', 'result.json', 'run.log', 'cpu.prof')
                             if (path := directory / name).is_file())
                files.extend(sorted((directory / 'frames').glob('*.ppm')))
    else:
        result['games']['status'] = 'pending'
    demonstration = games / 'dx-quake/demo-milestone/summary.json'
    if demonstration.is_file():
        demo = json.loads(demonstration.read_text())
        if demo['revision'] != revision:
            raise ValueError('the Quake milestone differs from the run source')
        for name, expected in demo.pop('files_sha256').items():
            path = (root / name).resolve()
            if not path.is_relative_to(demonstration.parent) or sha(path) != expected:
                raise ValueError(f'Quake milestone file differs from its digest: {name}')
            files.append(path)
        files.append(demonstration)
        result['games']['quake_demo_milestone'] = demo
    reports = {path.stem for path in files if path.parent == root / 'lockstep'}
    result['files_sha256'] = {path.relative_to(root).as_posix(): sha(path) for path in files}
    result['files_sha256'].update(checked_x87(root, inputs, reports))
    return result


def verify(root, receipt):
    revision = receipt['revision']
    if any(source_revision(root / name) != revision for name in ('wt', 'wt-games')):
        raise ValueError('retained worktrees differ from the candidate revision')
    for name, expected in receipt['files_sha256'].items():
        path = (root / name).resolve()
        if not path.is_relative_to(root) or not path.is_file() or sha(path) != expected:
            raise ValueError(f'evidence missing or changed: {name}')
    games = root / 'games'
    inputs = json.loads((games / 'inputs.json').read_text())
    if presenter := checked_presenter(games, inputs):
        name = presenter.relative_to(root).as_posix()
        if receipt['files_sha256'].get(name) != inputs['presenter']['derived_sha256']:
            raise ValueError('receipt does not bind the derived presenter digest')
    reports = {Path(name).stem for name in receipt['files_sha256'] if Path(name).parent == Path('lockstep')}
    if any(receipt['files_sha256'].get(name) != digest
           for name, digest in checked_x87(root, inputs, reports).items()):
        raise ValueError('receipt does not bind the compiled x87 evidence')
    for arm, expected in receipt['games']['artifact_sha256'].items():
        if sha(games / 'build' / arm / 'obj/Vpc386_machine') != expected:
            raise ValueError(f'compiled game harness changed: {arm}')
    print(f'Verified {len(receipt["files_sha256"])} artifacts for {revision}')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('root', type=Path)
    parser.add_argument('--verify', type=Path, help='check the artifacts named by a published receipt')
    args = parser.parse_args()
    root = args.root.resolve()
    if args.verify:
        verify(root, json.loads(args.verify.read_text()))
    else:
        print(json.dumps(summarize(root), indent=2))


if __name__ == '__main__':
    main()
