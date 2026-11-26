#!/usr/bin/env python3
"""Run the shared x87 unit and TestFloat suites with the exact DE25 DX wrapper."""
import argparse
from hashlib import file_digest
import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[3] / 'sources/misteross'
CHECKS = {
    'fast-pipeline-fwait-retired-flush': 20,
    'fast-pipeline-unretired-squash': 1,
    'fast-pipeline-f1-reset': 19,
    'fast-pipeline-machine-reset': 19,
    'fast-pipeline-fwait-exception': 1,
}


def x87_derivation(root, path):
    from boards.de25nano.pc386.i486dx import derive

    upstream = root / derive.X87_UPSTREAM
    derived, record = derive.derive_x87(upstream.read_bytes())
    if path.name != upstream.name or path.read_bytes() != derived:
        raise ValueError('the simulated x87 differs from the DE25 build derivation')
    return {'upstream': derive.X87_UPSTREAM, **record}


def sha(path):
    with path.open('rb') as stream:
        return file_digest(stream, 'sha256').hexdigest()


def lockstep_witness(output):
    from boards.de25nano.pc386.i486dx import derive

    builds = ROOT / 'build/sim'
    l3 = builds / 'fes-i486dx-lockstep'
    upstream = ROOT / derive.X87_UPSTREAM
    wrapper = l3 / 'derived' / upstream.name
    derivation = x87_derivation(ROOT, wrapper)
    sources = {
        'l3-dx': ('source', l3 / 'rtl/Vfes_i386_cpu__ver.d'),
        'l4-dx': ('i486dx_source', builds / 'fes-i486dx-i486-lockstep/rtl-I4861/Vfes_i386_cpu__ver.d'),
    }
    dependencies = []
    for suite, (key, path) in sources.items():
        report = json.loads((output / f'{suite}.json').read_text())[key]
        if report['files'].get(wrapper.relative_to(ROOT.parents[1]).as_posix()) != derivation['derived_sha256']:
            raise ValueError(f'{suite} lacks the derived x87 source witness')
        candidates = set(path.read_text().split()).intersection(map(str, (upstream, wrapper)))
        if candidates != {str(wrapper)}:
            raise ValueError(f'{suite} did not compile the derived x87: {path.name}')
        dependencies.append(path)
    return {'derivation': derivation,
            'files_sha256': {path.relative_to(ROOT).as_posix(): sha(path) for path in [wrapper, *dependencies]}}


def test_unit(unit, options, stage):
    from boards.de25nano.pc386.i486dx import derive

    cpu = derive.sim_sources(stage, ROOT)
    upstream = ROOT / derive.X87_UPSTREAM
    wrapper = stage / upstream.name
    if [path for path in cpu if path.name == upstream.name] != [wrapper]:
        raise ValueError('the simulated DX CPU did not select the derived x87')
    record = x87_derivation(ROOT, wrapper)
    original = unit.SIM / 'x87_unit_tb.cpp'
    fragment = Path(__file__).with_name('sim_x87_pipeline.inc')
    case_anchor = '    std::printf("SUMMARY %d %d\\n", passed, failed);'
    template = original.read_text()
    if template.count(case_anchor) != 1:
        raise ValueError('the x87 bench has no unique end-of-suite insertion point')
    (stage / original.name).write_text(template.replace(case_anchor, fragment.read_text() + case_anchor))
    unit.RTL_SOURCES = tuple(wrapper if name == upstream.name else name for name in unit.RTL_SOURCES)
    unit.SIM = stage
    shared_sources = unit.tested_sources
    unit.tested_sources = lambda: sorted({*shared_sources(), original, upstream, fragment,
                                          Path(__file__).resolve(), Path(derive.__file__).resolve()})

    report_path = unit.OUTPUT / 'report.json'
    report_path.unlink(missing_ok=True)
    status = unit.main(options)
    report = json.loads(report_path.read_text())
    obj = unit.OUTPUT / 'rtl'
    dependency = obj / 'Vfes_i386_x87__ver.d'
    bench_dependency = obj / 'x87_unit_tb.d'
    dependencies = set(dependency.read_text().split())
    if (str(wrapper) not in dependencies or str(upstream) in dependencies
            or str(stage / original.name) not in bench_dependency.read_text().split()):
        raise ValueError('the x87 unit build did not compile the derived wrapper and focused bench')
    latency = report['latency_clocks']['fast-fmul-wait.pc64']
    if status == 0 and latency != 19:
        raise ValueError(f'the derived x87 bench completed FMUL in {latency} clocks, expected 19')
    files = (wrapper, dependency, bench_dependency, obj / 'Vfes_i386_x87', stage / original.name)
    report['fast_pipeline'] = {
        'derivation': record,
        'files_sha256': {path.relative_to(ROOT).as_posix(): sha(path) for path in files},
        'focused_checks_passed': CHECKS if status == 0 else {},
        'fmul_fwait_clocks': latency,
    }
    report_path.write_text(json.dumps(report, indent=2) + '\n')
    return status, report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('suite', choices=('configured', 'lockstep', 'unit', 'testfloat'))
    parser.add_argument('--cache-root', type=Path)
    parser.add_argument('--jobs', type=int, default=2)
    parser.add_argument('--testfloat-gen', type=Path)
    parser.add_argument('--lockstep-reports', type=Path)
    args = parser.parse_args()
    sys.path.insert(0, str(ROOT))
    from boards.de25nano.pc386 import build

    requested = build.record_parameters(ROOT, '486dx').get('x87_fast_pipelined') is True
    if args.suite == 'configured':
        print('derived' if requested else 'shared')
        return 0
    if args.suite == 'lockstep':
        if not requested or args.lockstep_reports is None:
            parser.error('lockstep needs an x87-pipelined candidate and its reports directory')
        print(json.dumps(lockstep_witness(args.lockstep_reports), indent=2))
        return 0
    if not requested or not args.cache_root:
        parser.error('this driver needs an x87-pipelined candidate and the locked toolchain cache')
    from scripts import test_fes_i386_x87_testfloat as testfloat
    from scripts import test_fes_i386_x87_unit as unit

    options = ['--cache-root', str(args.cache_root.resolve()), '--jobs', str(args.jobs)]
    stage = unit.OUTPUT / 'derived'
    if args.suite == 'unit':
        return test_unit(unit, options, stage)[0]
    if args.testfloat_gen is None:
        parser.error('testfloat requires the pinned --testfloat-gen')
    generator = args.testfloat_gen.resolve(strict=True)
    testfloat.OUTPUT.mkdir(parents=True, exist_ok=True)
    manifest = testfloat.OUTPUT / 'manifest.txt'
    generated = testfloat.generate(generator, manifest)
    status, unit_report = test_unit(unit, [*options, '--testfloat-manifest', str(manifest)], stage)
    if status == 0 and unit_report.get('testfloat') != {'vectors': generated['vectors'],
                                                      'configs': len(generated['configs'])}:
        raise ValueError('the unit report does not match the generated TestFloat manifest')
    report = {'testfloat_gen': str(generator), 'testfloat_gen_sha256': testfloat.sha256(generator),
              'testfloat_level': 1, 'generator_seed': 1, **generated, 'netlist': False,
              'unit_returncode': status, 'unit_report': unit_report}
    (testfloat.OUTPUT / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    return status


if __name__ == '__main__':
    raise SystemExit(main())
