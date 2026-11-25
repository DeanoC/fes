#!/usr/bin/env python3
"""D12 for matching BUILD_IDs. Run `select` before `compare` on the fit outputs."""

import argparse
import hashlib
import importlib
import json
from pathlib import Path
import re
import sys
import tarfile
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
QUARTUS_QSF = Path('golden_top.qsf')
SOPC_TIMESTAMP = re.compile(rb'(?m)^([ \t]*<!-- )\d{4}\.\d{2}\.\d{2}\.\d{2}:\d{2}:\d{2}( -->[ \t]*\r?)$')
RTL_EXTENSIONS = {'.v', '.sv', '.vh', '.svh', '.vhd'}
EXTENSIONS = RTL_EXTENSIONS | {'.qsf', '.qpf', '.sdc', '.tcl', '.ip', '.qsys', '.hex', '.mem', '.mif', '.json'}
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


def project_inputs(product, extensions=EXTENSIONS):
    project = product / PROJECT
    result = {}
    for path in project.rglob('*'):
        relative = path.relative_to(project)
        if path.suffix in extensions and not REPORT_DIRS.intersection(relative.parts) and path.is_file():
            result[relative] = path.read_bytes()
    if not result:
        raise ValueError(f'no project inputs in {project}')
    return result


def file_inventory(inputs, extensions):
    files = {path.as_posix(): hashlib.sha256(data).hexdigest() for path, data in inputs.items()
             if path.suffix in extensions}
    rows = ''.join(f'{files[path]}  {path}\n' for path in sorted(files))
    return files, hashlib.sha256(rows.encode()).hexdigest()


def same_native_inputs(inputs_a, inputs_b):
    qsf_a, qsf_b = inputs_a[QUARTUS_QSF], inputs_b[QUARTUS_QSF]
    print(f'  raw {QUARTUS_QSF} SHA-256 main {hashlib.sha256(qsf_a).hexdigest()}')
    print(f'  raw {QUARTUS_QSF} SHA-256 candidate {hashlib.sha256(qsf_b).hexdigest()}')
    rtl_a, digest_a = file_inventory(inputs_a, RTL_EXTENSIONS)
    rtl_b, digest_b = file_inventory(inputs_b, RTL_EXTENSIONS)
    if not rtl_a or not rtl_b:
        raise ValueError('no compiler project RTL files')
    print(f'  raw RTL inventory SHA-256 main {digest_a} ({len(rtl_a)} files)')
    print(f'  raw RTL inventory SHA-256 candidate {digest_b} ({len(rtl_b)} files)')
    if qsf_a != qsf_b:
        raise ValueError(f'raw compiler QSF {QUARTUS_QSF} differs')
    differing = sorted(path for path in rtl_a.keys() | rtl_b.keys()
                       if inputs_a.get(Path(path)) != inputs_b.get(Path(path)))
    if differing:
        raise ValueError(f'raw project RTL differs in {differing}')
    print('  compiler QSF and all staged and generated RTL are cmp-equal')


def report_generated_metadata(a, b):
    extensions = {'.qip', '.sopcinfo'}
    inputs_a, inputs_b = project_inputs(a, extensions), project_inputs(b, extensions)
    for extension in sorted(extensions):
        files_a, digest_a = file_inventory(inputs_a, {extension})
        files_b, digest_b = file_inventory(inputs_b, {extension})
        differences = sorted(path for path in files_a.keys() | files_b.keys()
                             if files_a.get(path) != files_b.get(path))
        print(f'  raw {extension} inventory SHA-256 main {digest_a} ({len(files_a)} files)')
        print(f'  raw {extension} inventory SHA-256 candidate {digest_b} ({len(files_b)} files)')
        if extension == '.sopcinfo':
            other = []
            for name in differences:
                one, two = inputs_a.get(Path(name)), inputs_b.get(Path(name))
                if (one is None or two is None or
                        SOPC_TIMESTAMP.sub(rb'\1<timestamp>\2', one) != SOPC_TIMESTAMP.sub(rb'\1<timestamp>\2', two)):
                    other.append(name)
            print(f'  {len(differences) - len(other)} raw .sopcinfo differences confined to generation timestamps')
            print(f'  other raw .sopcinfo differences {other}')
        else:
            print(f'  raw {extension} differences {differences}')


def normalise_paths(data, product):
    data = data.replace(str(product).encode(), b'<build-output>')
    return data.replace(str(product.parents[2]).encode(), b'<source-module>')


def exported_package(store, record):
    # Discover exports by archive. The exporter may write both primary and
    # record-keyed copies of the build-input receipt beside one archive.
    exported = [archive.with_suffix('') for archive in store.glob('*.fcore')
                if read_json(archive.with_suffix('.build-inputs.json')) == record]
    if len(exported) != 1:
        raise ValueError('expected one exported package for the candidate build record')
    return exported[0]


def verify_package(product, summary, record, payload, digest):
    if summary['package_timing_admission'] is not True:
        raise ValueError(f'{record["revision"]} did not pass ordinary package timing admission')
    # A producer can write its local manifest before the exporter refuses it.
    # Require the published package's build record and payload instead.
    package = exported_package(product.parents[1] / 'packages', record)
    manifest_bytes = (package / 'manifest.toml').read_bytes()
    archive_path = package.with_suffix('.fcore')
    with tarfile.open(archive_path, 'r:') as archive:
        for name, expected in (('core.rbf', payload), ('manifest.toml', manifest_bytes)):
            member = archive.getmember(name)
            if not member.isfile() or archive.extractfile(member).read() != expected:
                raise ValueError(f'{archive_path} {name} differs from its admitted export')
    if (package / 'core.rbf').read_bytes() != payload:
        raise ValueError(f'{package} payload differs')
    manifest = tomllib.loads(manifest_bytes.decode())
    if (manifest['payload']['sha256'] != digest or manifest['build']['id'] != summary['build_id']
            or manifest['build']['revision'] != record['revision']):
        raise ValueError(f'{package} manifest differs from the admitted payload or build record')
    return package.name


def compare_products(a, b, raw_project, both_admitted):
    record_a = read_json(a / 'build-inputs.json')
    record_b = read_json(b / 'build-inputs.json')
    design, toolchain = compare_records(record_a, record_b)
    execution_a, execution_b = ((product / 'execution.json').read_bytes() for product in (a, b))
    if execution_a != execution_b:
        raise ValueError('raw execution records differ')
    summary_a = read_json(a / 'build-summary.json')
    summary_b = read_json(b / 'build-summary.json')
    if summary_a['revision'] != record_a['revision'] or summary_b['revision'] != record_b['revision']:
        raise ValueError('build summary revision does not match its record')
    if summary_a['build_id'] != design[:32] or summary_b['build_id'] != design[:32]:
        raise ValueError('build summary does not carry the common design BUILD_ID')
    inputs_a, inputs_b = project_inputs(a), project_inputs(b)
    same_native_inputs(inputs_a, inputs_b)
    if raw_project:
        report_generated_metadata(a, b)
    different_paths = inputs_a.keys() ^ inputs_b.keys()
    same_paths = inputs_a.keys() & inputs_b.keys()
    raw_differences = {path for path in same_paths if inputs_a[path] != inputs_b[path]}
    differences = raw_differences if raw_project else {
        path for path in raw_differences if normalise_paths(inputs_a[path], a) != normalise_paths(inputs_b[path], b)
    }
    differing = sorted(path.as_posix() for path in different_paths | differences)
    if differing:
        raise ValueError(f'project inputs differ in {differing}')
    rbf_a, rbf_b = (a / 'core.rbf').read_bytes(), (b / 'core.rbf').read_bytes()
    digest_a, digest_b = hashlib.sha256(rbf_a).hexdigest(), hashlib.sha256(rbf_b).hexdigest()
    if summary_a['rbf_sha256'] != digest_a or summary_b['rbf_sha256'] != digest_b:
        raise ValueError('RBF digest does not match its build summary')
    package_b = verify_package(b, summary_b, record_b, rbf_b, digest_b)
    print(f'  main {record_a["revision"]}, candidate {record_b["revision"]}')
    if raw_project:
        print(f'  same BUILD_ID {design[:32]} and {len(inputs_a)} raw cmp-equal project inputs')
    else:
        print(f'  same BUILD_ID {design[:32]} and {len(inputs_a)} project inputs after path normalization')
        print(f'  project inputs with path differences only {sorted(path.as_posix() for path in raw_differences)}')
    print(f'  same toolchain identity {toolchain}')
    print(f'  cmp-equal execution record SHA-256 {hashlib.sha256(execution_a).hexdigest()}')
    if both_admitted:
        package_a = verify_package(a, summary_a, record_a, rbf_a, digest_a)
        print(f'  main admitted and sealed as {package_a}')
    print(f'  candidate admitted and sealed as {package_b}')
    if rbf_a != rbf_b:
        raise ValueError(f'RBF differs: main {digest_a}, candidate {digest_b}')
    print(f'  cmp-equal RBF {digest_a}')


def compare(root_a, root_b, packages, raw_project, both_admitted):
    failures = []
    for package in packages:
        print(package)
        try:
            compare_products(root_a / package, root_b / package, raw_project, both_admitted)
        except (OSError, ValueError, KeyError, tarfile.TarError) as error:
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
    parser.add_argument('--raw-project', action='store_true', help='require raw equality for all project input files')
    parser.add_argument('--both-admitted', action='store_true', help='validate the ordinary package export on both sides')
    args = parser.parse_args()
    a, b = args.left.resolve(), args.right.resolve()
    if args.action == 'compare':
        compare(a, b, [args.package] if args.package else PACKAGES, args.raw_project, args.both_admitted)
    elif args.package or args.raw_project or args.both_admitted:
        parser.error('--package, --raw-project and --both-admitted require compare')
    elif args.action == 'select':
        select(a, b)
    else:
        compare_selection(a, b)


if __name__ == '__main__':
    main()
