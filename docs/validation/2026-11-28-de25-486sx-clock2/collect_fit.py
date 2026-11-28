#!/usr/bin/env python3
"""Summarize the four registered 486SX builds and their retained LOADED route reports (#2264's collector, SX)."""

import argparse
import hashlib
import json
import math
import re
import socket
import sys
from fractions import Fraction
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / '2026-11-24-de25-486sx-fmax'))
from rung_summary import summary

sys.path.insert(0, str(Path(__file__).resolve().parent))
from cdc_compare import crossing_rows  # noqa: E402

CORNERS = {
    '4_slow_fix4_100c': 'Slow fix4 100C Model',
    '4_slow_fix4_0c': 'Slow fix4 0C Model',
    '4_slow_fix4a_0c': 'Slow fix4a 0C Model',
    'MIN_fast_fix4_0c': 'Fast fix4 0C Model',
}
VCO_HZ = Fraction(50_000_000 * 188, 3)
CORNER_LINE = re.compile(r'CORNER \{([^}]+)\} (setup|hold|recovery|removal|mpw) (\d+) (-?\d+\.\d+)')
RETIMED_REVIEW = 'boards/de25nano/pc386/retimed_tension.py'
RETIMED_STRUCTURE = 'boards/de25nano/pc386/retimed_structure.tcl'
RETIMED_FIT_ARTIFACTS = {
    'inputs.json', 'generation.json', 'linux-boot.core.rbf', 'project/golden_top.qsf',
    'project/output_files/golden_top.tq.drc.signoff.rpt', 'project/output_files/golden_top.sta.summary',
}
PRESENTER_SOURCE = 'cores/fes-pc386/rtl/pc386_presenter.sv'
MODEL = '486sx'
# method.md's control: main at its base, the 486SX at C0 = 34, seed 1, through the registered producer.
CONTROL_REVISION = '17cbddbec2f7559e37467a975943828c766310f7'
CONTROL_CLOCK_HZ = 92156863
CONTROL_DRC = 'signoff/combined/hps/project/output_files/golden_top.tq.drc.signoff.rpt'


def control_identity(control):
    """The control fit's identity: its build record must be main's 486SX at C0 = 34, seed 1."""
    build = json.loads((control / 'build-inputs.json').read_bytes())
    parameters = build['parameters']
    if (build['revision'], parameters['model'], parameters['machine_clock_hz'], parameters['seed']) != (
            CONTROL_REVISION, MODEL, CONTROL_CLOCK_HZ, 1):
        raise ValueError('the CDC control is not main\'s 486SX C0 = 34 seed 1 fit')
    return {'build_inputs_sha256': file_sha256(control / 'build-inputs.json'),
            'drc_sha256': file_sha256(control / CONTROL_DRC)}
REVIEWED_PRESENTER_SHA256 = '7437e4b680112d6d8e5fb7ecf5a0a65f92ba937148b9bfbdfe1550703bdf8167'
REVIEWED_REARM_CYCLES = 8


def file_sha256(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            digest.update(block)
    return digest.hexdigest()


def corner_slacks(path):
    expected = {(corner, analysis) for corner in CORNERS.values()
                for analysis in ('setup', 'hold', 'recovery', 'removal', 'mpw')}
    slacks = {}
    for line in path.read_text().splitlines():
        if not line.startswith('CORNER '):
            continue
        match = CORNER_LINE.fullmatch(line)
        if match is None:
            raise ValueError(f'invalid corner report row: {line}')
        corner, analysis, count, slack = match.groups()
        if (corner, analysis) in slacks or not int(count):
            raise ValueError(f'duplicate corner or no paths: {line}')
        slacks[corner, analysis] = float(slack)
    if slacks.keys() != expected:
        raise ValueError('incomplete per-analysis corner coverage')
    return {corner: {analysis: slack for (name, analysis), slack in slacks.items() if name == corner}
            for corner in CORNERS.values()}


def loaded_routes(path):
    routes = {}
    for line in path.read_text().splitlines():
        corner, delay, skew = line.split('\t')
        delay = float(delay.removeprefix('data_delay='))
        skew = float(skew.removeprefix('skew='))
        if corner not in CORNERS or corner in routes or not all(map(math.isfinite, (delay, skew))) or delay < 0:
            raise ValueError(f'invalid LOADED route at {corner}')
        routes[corner] = {'data_ns': delay, 'skew_ns': skew, 'total_ns': delay + abs(skew)}
    if routes.keys() != CORNERS.keys():
        raise ValueError('incomplete LOADED corner coverage')
    return routes


def loaded_rearm_cycles(output, build, fit):
    parameters = build['parameters']
    cycles = parameters['loaded_rearm_cycles']
    if type(cycles) is not int or cycles != REVIEWED_REARM_CYCLES or parameters['model'] != MODEL:
        raise ValueError('unreviewed LOADED re-arm interval')
    prepared = json.loads((output / 'signoff/combined/hps/inputs.json').read_text())
    machine = prepared['standard_zx81']
    if (prepared['fes_revision'] != build['revision'] or machine['model'] != MODEL
            or machine['build_id'] != build['identities']['design'][:32]
            or machine['derivations']['pc386_system.v']['netlist_sha256'] != fit['netlist_sha256']):
        raise ValueError('LOADED re-arm evidence differs from the fitted SX build')
    presenter = machine['derivations']['pc386_presenter.sv']
    if (presenter['loaded_rearm_cycles'] != cycles
            or presenter['source_sha256'] != build['source_inputs'][PRESENTER_SOURCE]
            or presenter['derived_sha256'] != REVIEWED_PRESENTER_SHA256):
        raise ValueError('fitted LOADED re-arm derivation differs from the digitally reviewed presenter')
    return cycles


def retimed_evidence(output, logs, build, build_digest, retimed_rows):
    if RETIMED_REVIEW in build['source_inputs']:
        proof_directory = output / 'signoff/retimed-tension'
        receipt_path = proof_directory / 'receipt.json'
        receipt = json.loads(receipt_path.read_text())
        structural_review = RETIMED_STRUCTURE in build['source_inputs']
        policy = 'de25-pc386-retimed-tension-v2' if structural_review else 'de25-pc386-retimed-tension-v1'
        if receipt['policy'] != policy:
            raise ValueError('retimed endpoint receipt used a different review policy')
        if receipt['bindings']['build_record_sha256'] != build_digest:
            raise ValueError('retimed endpoint receipt selected a different build record')
        review_sources = (RETIMED_REVIEW, RETIMED_REVIEW.replace('.py', '.tcl'))
        reports = ('targets.txt', 'paths.tsv', 'summary.json')
        if structural_review:
            review_sources += (RETIMED_STRUCTURE,)
            reports += ('structure.tsv', 'unconstrained.rpt')
        if receipt['sources_sha256'] != {name: build['source_inputs'][name] for name in review_sources}:
            raise ValueError('retimed endpoint receipt used different captured review tools')
        if receipt['reports_sha256'].keys() != set(reports):
            raise ValueError('retimed endpoint receipt has an unexpected report set')
        for name in reports:
            digest = file_sha256(proof_directory / name)
            if receipt['reports_sha256'][name] != digest:
                raise ValueError(f'retimed endpoint receipt digest differs for {name}')
        hps = (output / 'signoff/combined/hps').resolve()
        if structural_review:
            tool = receipt['tool']
            version_path = hps / 'quartus-version.txt'
            if (tool['fit_version_sha256'] != file_sha256(version_path)
                    or tool['version'] != version_path.read_text().strip()
                    or tool['log_sha256'] != file_sha256(proof_directory / 'quartus-sta.log')):
                raise ValueError('retimed endpoint receipt selected a different fit version or TimeQuest log')
        artifacts = receipt['bindings']['artifacts']
        if artifacts.keys() != RETIMED_FIT_ARTIFACTS:
            raise ValueError('retimed endpoint receipt has an unexpected fitted artifact set')
        for name, expected in artifacts.items():
            path = (hps / name).resolve()
            if not path.is_relative_to(hps) or file_sha256(path) != expected:
                raise ValueError(f'retimed endpoint receipt differs from fitted artifact {name}')
        database = hps / 'project/qdb'
        entries = list(database.rglob('*'))
        if database.is_symlink() or any(path.is_symlink() for path in entries):
            raise ValueError('retimed endpoint receipt refers to a linked fitted database')
        compiled = {path.relative_to(database).as_posix(): file_sha256(path) for path in entries if path.is_file()}
        if not compiled or compiled != receipt['bindings']['compiled_database']:
            raise ValueError('retimed endpoint receipt selected a different or incomplete fitted database')
        receipt_digest = file_sha256(receipt_path)
        targets = proof_directory / 'targets.txt'
        proof = json.loads((proof_directory / 'summary.json').read_text())
        if structural_review and any(proof[name]['pass'] is not True for name in ('structure', 'unconstrained')):
            raise ValueError('retimed endpoint receipt lacks passing structural and unconstrained-path checks')
    else:
        # Preserve the independent evidence recorded before production proved
        # these endpoints as part of the same build.
        receipt_digest = None
        targets = logs / 'tension-reviewed.txt'
        proof = json.loads((logs / 'tension-summary.json').read_text())
        if (logs / 'tension-proof.exit').read_text().strip() != '0':
            raise ValueError('the retained standalone retimed endpoint check failed')
    count = len(targets.read_text().splitlines())
    if count != retimed_rows or proof['pass'] is not True:
        raise ValueError('retimed endpoint review lacks passing fitted clock and corner evidence')
    return {'rows': count, 'minimum_slacks': proof['minimum_slacks'], 'receipt_sha256': receipt_digest}


def seed_result(output, logs, seed, divider, control):
    record = {'seed': seed, 'c0': divider, 'host': socket.gethostname(), 'passes': False, 'failures': []}
    failures = record['failures']
    try:
        exit_code = int((logs / 'producer.exit').read_text())
        record['producer_exit'] = exit_code
        build_inputs = (output / 'build-inputs.json').read_bytes()
        build = json.loads(build_inputs)
        record['revision'] = build['revision']
        record['build_inputs_sha256'] = hashlib.sha256(build_inputs).hexdigest()
        if (logs / 'revision').read_text().strip() != record['revision']:
            failures.append('producer source revision differs from the run record')
        fit = summary(output / 'signoff/combined/hps/project')
        record['fit'] = fit
        clock = VCO_HZ / divider
        parameters = build['parameters']
        rearm_cycles = loaded_rearm_cycles(output, build, fit)
        pixel_time = Fraction(3 * 1_000_000_000, parameters['pixel_clock_hz'])
        loaded_budget = min(3.9, float(rearm_cycles * 1_000_000_000 / clock - pixel_time - 10))
        record.update(loaded_budget_ns=loaded_budget, loaded_rearm_cycles=rearm_cycles)
        if (parameters['model'], parameters['seed'], parameters['machine_clock_hz'], fit['seed']) != (
            MODEL, seed, round(clock), seed
        ):
            failures.append('wrong model, clock or seed')
        pll = fit['pll']
        if (pll['m'], pll['n'], [counter for _, counter in pll['outputs']]) != ('188', '3', [str(divider), '255']):
            failures.append('wrong fitted PLL counters')
        if not fit['passes']:
            failures.append('negative timing slack or fitter area over budget')
        timing_path = output / 'signoff-timing.json'
        timing = json.loads(timing_path.read_text()) if timing_path.is_file() else {}
        record['unreviewed_failed_rules'] = timing.get('unreviewed_failed_rules')
        if not timing.get('package_timing_admission') or exit_code != 0:
            failures.append('registered producer did not admit the package')
        else:
            record['package'] = json.loads((output / 'build-summary.json').read_text())
        retimed_rows = sum(group['rows'] for finding in timing.get('reviewed_findings', ())
                           if finding['id'] == 'TMC-20602' for group in finding.get('groups', ())
                           if group['name'] == 'retimed-machine-control-tension')
        if retimed_rows:
            record['retimed_tension_proof'] = retimed_evidence(
                output, logs, build, record['build_inputs_sha256'], retimed_rows)
        slacks = corner_slacks(logs / 'corners.log')
        record['corner_slacks_ns'] = slacks
        if any(slack < 0 for analyses in slacks.values() for slack in analyses.values()):
            failures.append('negative per-corner slack')
        if (logs / 'report.exit').read_text().strip() != '0':
            failures.append('LOADED route report failed')
        routes = loaded_routes(logs / 'loaded.tsv')
        record['loaded_routes'] = routes
        if any(route['total_ns'] > loaded_budget for route in routes.values()):
            failures.append('LOADED route exceeds the re-arm budget')
        # method.md F5: no CDC-* or RDC-* row the control fit lacks.
        record['cdc_control'] = control_identity(control)
        base = crossing_rows(control / 'signoff/combined/hps/project')
        mine = crossing_rows(output / 'signoff/combined/hps/project')
        new = {rule: sorted(rows - base.get(rule, set())) for rule, rows in mine.items()}
        new = {rule: rows for rule, rows in new.items() if rows}
        record['cdc_new_rows'] = new
        if new:
            failures.append('CDC or RDC findings the control lacks')
    except (OSError, ValueError, KeyError, IndexError, AttributeError) as error:
        failures.append(f'incomplete evidence: {error}')
    record['passes'] = not failures
    return record


def rung_result(divider, seeds):
    revisions = {seed.get('revision') for seed in seeds}
    # method.md F6: all four seeds compiled one raw machine netlist.
    netlists = {seed.get('fit', {}).get('netlist_sha256') for seed in seeds}
    return {'c0': divider, 'seeds': seeds, 'netlists': sorted(map(str, netlists)), 'passes': (
        len(seeds) == 4 and {seed.get('seed') for seed in seeds} == {1, 2, 3, 4}
        and all(seed.get('c0') == divider and seed.get('passes') is True for seed in seeds)
        and all('cdc_new_rows' in seed for seed in seeds)
        and len({json.dumps(seed.get('cdc_control'), sort_keys=True) for seed in seeds}) == 1
        and seeds[0].get('cdc_control') is not None
        and len(revisions) == 1 and None not in revisions
        and len(netlists) == 1 and None not in netlists
    )}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('divider', type=int)
    parser.add_argument('build_directory', type=Path, nargs='?')
    parser.add_argument('log_directory', type=Path, nargs='?')
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument('--seed', type=int, choices=range(1, 5), help='emit one seed measurement from the builder')
    modes.add_argument('--seed-reports', type=Path, nargs=4, help='combine four builder measurements')
    parser.add_argument('--control', type=Path, help="the control fit's producer output directory (method.md F5)")
    args = parser.parse_args()
    if args.seed_reports:
        if args.build_directory or args.log_directory:
            parser.error('seed reports already contain the builder measurements')
        result = rung_result(args.divider, [json.loads(path.read_text()) for path in args.seed_reports])
    else:
        if not args.build_directory or not args.log_directory or not args.control:
            parser.error('expected build and log directories and --control')
        seeds = [seed_result(args.build_directory / f's{seed}', args.log_directory / f's{seed}', seed, args.divider,
                             args.control)
                 for seed in ((args.seed,) if args.seed else range(1, 5))]
        result = seeds[0] if args.seed else rung_result(args.divider, seeds)
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if result['passes'] else 1


if __name__ == '__main__':
    sys.exit(main())
