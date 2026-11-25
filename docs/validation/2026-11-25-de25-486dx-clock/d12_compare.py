#!/usr/bin/env python3
"""D12 for matching BUILD_IDs. Run `select` before `compare` on the fit outputs."""

import argparse
import hashlib
import importlib
import json
from pathlib import Path
import sys
import tomllib


PACKAGES = {
    'menu': ('menu', None),
    'zx81': ('zx81', None),
    'spectrum48': ('spectrum', '48'),
    'spectrum128': ('spectrum', '128'),
    'st': ('atari_st', None),
    'c64': ('c64', 'pal'),
    'pcxt': ('pcxt', 'xt'),
}
PROJECT = Path('signoff/combined/hps/project')
EXTENSIONS = {'.qsf', '.qpf', '.sdc', '.v', '.sv', '.vh', '.svh', '.tcl', '.ip', '.qsys', '.hex', '.mif', '.json', '.vhd'}
REPORT_DIRS = {'output_files', 'qdb', 'tmp-clearbox', 'dni', '.qsys_edit', 'software'}


def read_json(path):
    return json.loads(path.read_bytes())


def select(module_root, output):
    sys.path.insert(0, str(module_root))
    output.mkdir(parents=True, exist_ok=True)
    for package, (producer, model) in PACKAGES.items():
        module = importlib.import_module(f'boards.de25nano.producers.build_{producer}_package')
        machine = getattr(module, 'MACHINE', None)
        tools = {'quartus', 'qsys', 'ipgenerate', 'pfg'}
        record_options = {}
        if machine is None:
            repository, revision = module._require_clean_source(module_root)
            create_record = module.create_build_record
        else:
            repository, revision = machine.require_clean_source(module_root)
            tools = machine.tool_set(machine.spec(module_root))
            create_record = machine.create_build_record
            record_options['model'] = model
        record = create_record(
            module_root, repository, revision, dict.fromkeys(tools, 'selection-only'),
            {'toolchain': 'quartus', 'selection_only': True}, **record_options,
        )
        (output / f'{package}.json').write_bytes(record)


def functional_record(record):
    return {
        **{key: value for key, value in record.items() if key not in ('revision', 'identities')},
        'identities': {key: value for key, value in record['identities'].items() if key != 'record'},
    }


def compare_records(a, b):
    a, b = functional_record(a), functional_record(b)
    if a != b:
        differing = sorted(key for key in a.keys() | b.keys() if a.get(key) != b.get(key))
        raise ValueError(f'build records differ in {differing}')
    return a['identities']['design'], a['identities']['toolchain']


def compare_selection(a, b):
    for package in PACKAGES:
        name = f'{package}.json'
        one, two = read_json(a / name), read_json(b / name)
        design, toolchain = compare_records(one, two)
        print(f'{package}: {len(one["source_inputs"])} identical captured inputs, '
              f'design {design}, toolchain selection {toolchain}')


def project_inputs(product):
    project = product / PROJECT
    result = {}
    for path in project.rglob('*'):
        relative = path.relative_to(project)
        if path.suffix in EXTENSIONS and not REPORT_DIRS.intersection(relative.parts) and path.is_file():
            data = path.read_bytes().replace(str(product).encode(), b'<build-output>')
            result[relative] = data.replace(str(product.parents[2]).encode(), b'<source-module>')
    if not result:
        raise ValueError(f'no project inputs in {project}')
    return result


def exported_package(store, record):
    # Discover exports by archive. The exporter may write both primary and
    # record-keyed copies of the build-input receipt beside one archive.
    exported = [archive.with_suffix('') for archive in store.glob('*.fcore')
                if read_json(archive.with_suffix('.build-inputs.json')) == record]
    if len(exported) != 1:
        raise ValueError('expected one exported package for the candidate build record')
    return exported[0]


def compare_products(a, b):
    record_a = read_json(a / 'build-inputs.json')
    record_b = read_json(b / 'build-inputs.json')
    design, toolchain = compare_records(record_a, record_b)
    summary_a = read_json(a / 'build-summary.json')
    summary_b = read_json(b / 'build-summary.json')
    if summary_a['revision'] != record_a['revision'] or summary_b['revision'] != record_b['revision']:
        raise ValueError('build summary revision does not match its record')
    if summary_a['build_id'] != design[:32] or summary_b['build_id'] != design[:32]:
        raise ValueError('build summary does not carry the common design BUILD_ID')
    if summary_b['package_timing_admission'] is not True:
        raise ValueError('candidate did not pass ordinary package timing admission')
    inputs_a, inputs_b = project_inputs(a), project_inputs(b)
    differing = sorted(path.as_posix() for path in inputs_a.keys() | inputs_b.keys()
                       if inputs_a.get(path) != inputs_b.get(path))
    if differing:
        raise ValueError(f'project inputs differ in {differing}')
    rbf_a, rbf_b = (a / 'core.rbf').read_bytes(), (b / 'core.rbf').read_bytes()
    digest = hashlib.sha256(rbf_a).hexdigest()
    if rbf_a != rbf_b or summary_a['rbf_sha256'] != digest or summary_b['rbf_sha256'] != digest:
        raise ValueError(f'RBF differs: main {digest}, candidate {hashlib.sha256(rbf_b).hexdigest()}')
    # A producer can write its local manifest before the exporter refuses it.
    # Require the published package's build record and payload instead.
    package = exported_package(b.parents[1] / 'packages', record_b)
    if (package / 'core.rbf').read_bytes() != rbf_b:
        raise ValueError('exported package payload differs')
    manifest = tomllib.loads((package / 'manifest.toml').read_text())
    if (manifest['payload']['sha256'] != digest or manifest['build']['id'] != design[:32]
            or manifest['build']['revision'] != record_b['revision']):
        raise ValueError('candidate manifest differs from the admitted payload or build record')
    print(f'  main {record_a["revision"]}, candidate {record_b["revision"]}')
    print(f'  same BUILD_ID {design[:32]} and {len(inputs_a)} project inputs after path normalization')
    print(f'  same toolchain identity {toolchain}')
    print(f'  cmp-equal RBF {digest}, candidate admitted and sealed as {package.name}')


def compare(root_a, root_b, packages):
    failures = []
    for package in packages:
        print(package)
        try:
            compare_products(root_a / package, root_b / package)
        except (OSError, ValueError, KeyError) as error:
            print(f'  FAIL {error}')
            failures.append(package)
    if failures:
        raise SystemExit(f'D12 incomplete for {", ".join(failures)}')
    print(f'PASS D12 for {", ".join(packages)}')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('select', 'compare-selection', 'compare'))
    parser.add_argument('left', type=Path)
    parser.add_argument('right', type=Path)
    parser.add_argument('--package', choices=PACKAGES, help='compare one fitted package on its build host')
    args = parser.parse_args()
    a, b = args.left.resolve(), args.right.resolve()
    if args.action == 'compare':
        compare(a, b, [args.package] if args.package else PACKAGES)
    elif args.package:
        parser.error('--package requires compare')
    elif args.action == 'select':
        select(a, b)
    else:
        compare_selection(a, b)


if __name__ == '__main__':
    main()
