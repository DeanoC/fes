#!/usr/bin/env python3
"""Compare the Quartus input and setting tables retained by two PC/XT fits."""

import argparse
from collections import Counter
from hashlib import sha256
from pathlib import Path
import re


REPORTS = Path('signoff/combined/hps/project/output_files')
OUTPUT_DIRS = {'output_files', 'qdb', 'tmp-clearbox', 'dni', '.qsys_edit', 'software'}
SANDBOX_PID = re.compile(rb'(dni/sandboxes/powerboat_)\d+(_0/)|(tmp-clearbox/golden_top/)\d+(/)')
TABLES = (
    ('golden_top.syn.rpt', b'General Register Statistics for Partition "root_partition"'),
    ('golden_top.syn.rpt', b'Synthesis Resource Usage Summary for Partition "root_partition"'),
    ('golden_top.fit.rpt', b'Fitter Settings'),
    ('golden_top.fit.rpt', b'Parallel Compilation'),
    ('golden_top.fit.plan.rpt', b'Fitter Device Options'),
)
STAGE_TABLES = (
    ('plan', b'Input Pins'),
    ('plan', b'Output Pins'),
    ('plan', b'PLL Usage Summary'),
    ('plan', b'Fixed Point DSP Register Packing Summary'),
    ('plan', b'RAM Register Packing Summary'),
    ('place', b'Fitter Resource Usage Summary'),
    ('place', b'Fitter Resource Utilization by Entity'),
    ('place', b'Fitter Partition Statistics'),
    ('retime', b'Retiming Limit Summary'),
    ('route', b'Routing Usage Summary'),
)
DATABASES = (
    'synthesized/1/sdc.cdb',
    'synthesized/1/names.hdb',
    'synthesized/1/netlist.atom.cdb',
    'partitioned/1/sdc.cdb',
    'partitioned/1/names.hdb',
    'partitioned/1/sgate_netlist.sgate.cdb',
)
RETAINED_GENERATED_INPUTS = ('qdb/**/cpt_proxy/*.v', 'qdb/**/cpt_proxy/*.sv', 'qdb/**/mifs/*.mif')


def table(report, name):
    lines = report.read_bytes().splitlines(keepends=True)
    title = next(i for i, line in enumerate(lines) if line.startswith(b'; ' + name + b' '))
    borders = [i for i, line in enumerate(lines[title + 1:], title + 1) if line.startswith(b'+-')]
    return b''.join(lines[borders[1] + 1:borders[2]])


def report_rows(table_bytes):
    return [tuple(cell.strip() for cell in line.split(b';')[1:-1]) for line in table_bytes.splitlines()]


def without_sandbox_pid(row):
    return tuple(SANDBOX_PID.sub(lambda match: (match[1] or match[3]) + b'<PID>' + (match[2] or match[4]), cell)
                 for cell in row)


def clearbox_inventory(project):
    inventory = {}
    for file in project.glob('tmp-clearbox/golden_top/*/*.tdf'):
        if not file.parent.name.isdecimal() or file.name in inventory:
            raise ValueError(f'Unexpected clearbox source path {file}')
        inventory[file.name] = sha256(file.read_bytes()).hexdigest()
    return inventory


def input_audit(a, b):
    digests = []
    for root in (a, b):
        project = (root / REPORTS).parent
        files = {}
        for path in project.rglob('*'):
            relative = path.relative_to(project)
            if not OUTPUT_DIRS.intersection(relative.parts) and path.is_file():
                files[relative] = sha256(path.read_bytes()).hexdigest()
        digests.append(files)
    first, second = digests
    differences = Counter(path.suffix for path in first.keys() | second.keys() if first.get(path) != second.get(path))
    print(f'Non-output project files examined {len(first)} and {len(second)}')
    print(f'Raw non-output differences by file extension {dict(sorted(differences.items()))}')
    mem_a, mem_b = ({path: digest for path, digest in files.items() if path.suffix == '.mem'} for files in digests)
    print(f'Raw .mem files {len(mem_a)} and {len(mem_b)}, identical: {mem_a == mem_b}')
    for extension in ('.ini', '.qws'):
        counts = [sum(path.suffix == extension for path in files) for files in digests]
        print(f'Project {extension} counts {counts}')
    execution = [(root / 'execution.json').read_bytes() for root in (a, b)]
    print(f'Execution record SHA-256 {sha256(execution[0]).hexdigest()} {sha256(execution[1]).hexdigest()}')
    return bool(mem_a) and mem_a == mem_b and execution[0] == execution[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('left', type=Path)
    parser.add_argument('right', type=Path)
    args = parser.parse_args()
    a, b = args.left / REPORTS, args.right / REPORTS
    same_inputs = input_audit(args.left, args.right)
    for name in ('golden_top.pin', 'golden_top.sof'):
        sha_a, sha_b = (sha256((root / name).read_bytes()).hexdigest() for root in (a, b))
        print(f'{name} raw SHA-256 {sha_a} {sha_b}')

    one, two = (table(root / 'golden_top.syn.rpt', b'Synthesis Source Files Read') for root in (a, b))
    rows_a, rows_b = report_rows(one), report_rows(two)
    same_order = list(map(without_sandbox_pid, rows_a)) == list(map(without_sandbox_pid, rows_b))
    same_checksums = [row[-1] for row in rows_a] == [row[-1] for row in rows_b]
    checksummed = [sum(bool(row[-1]) for row in rows) for rows in (rows_a, rows_b)]
    unchecked = [len(rows) - checked for rows, checked in zip((rows_a, rows_b), checksummed)]
    unchecked_types = Counter(row[1].decode() for row in rows_a if not row[-1])
    print(f'Raw ordered synthesis source table SHA-256 {sha256(one).hexdigest()} {sha256(two).hexdigest()}')
    print(f'Ordered source rows {len(rows_a)} and {len(rows_b)}')
    print(f'Rows with populated MD5s {checksummed[0]} and {checksummed[1]}, blank {unchecked[0]} and {unchecked[1]}')
    print(f'Rows with blank MD5 by left source type {dict(sorted(unchecked_types.items()))}')
    print(f'Quartus-reported file MD5s match in order: {same_checksums}')
    print(f'Report rows match after substituting generated sandbox PIDs: {same_order}')
    passed = same_inputs and same_order and same_checksums and all(checksummed)
    for pattern in RETAINED_GENERATED_INPUTS:
        files_a, files_b = ({file.relative_to(root.parent): sha256(file.read_bytes()).hexdigest()
                             for file in root.parent.glob(pattern)} for root in (a, b))
        for file in sorted(files_a.keys() | files_b.keys()):
            print(f'{file.name} raw SHA-256 {files_a.get(file, "missing")} {files_b.get(file, "missing")}')
        passed &= bool(files_a) and files_a == files_b
    clearbox_a, clearbox_b = (clearbox_inventory(root.parent) for root in (a, b))
    print(f'Retained clearbox .tdf files {len(clearbox_a)} and {len(clearbox_b)}, raw bytes match: {clearbox_a == clearbox_b}')
    for inventory in (clearbox_a, clearbox_b):
        contents = ''.join(f'{digest}  {path}\n' for path, digest in sorted(inventory.items()))
        print(f'Clearbox .tdf inventory SHA-256 {sha256(contents.encode()).hexdigest()}')
    passed &= bool(clearbox_a) and clearbox_a == clearbox_b
    for filename, name in TABLES:
        one, two = (table(root / filename, name) for root in (a, b))
        print(f'{name.decode()} raw SHA-256 {sha256(one).hexdigest()} {sha256(two).hexdigest()}')
        passed &= one == two and bool(one)
    for label, root in (('left', a), ('right', b)):
        report = root / 'golden_top.fit.rpt'
        panels = ((b'Fitter Settings', (b'Fitter Initial Placement Seed',)),
                  (b'Parallel Compilation', (b'Number detected on machine', b'Maximum allowed')))
        for panel, fields in panels:
            rows = {row[0]: row[1] for row in report_rows(table(report, panel))}
            for field in fields:
                print(f'{label} {field.decode()} {rows[field].decode()}')
    for stage, name in STAGE_TABLES:
        filename = f'golden_top.fit.{stage}.rpt'
        one, two = (table(root / filename, name) for root in (a, b))
        print(f'{stage} {name.decode()} raw SHA-256 {sha256(one).hexdigest()} {sha256(two).hexdigest()}')
    for name in DATABASES:
        files = [list(root.parent.glob(f'qdb/_compiler/golden_top/root_partition/*/{name}')) for root in (a, b)]
        if not all(len(matches) == 1 for matches in files):
            raise ValueError(f'Expected one compiler database for {name} in each build')
        sha_a, sha_b = (sha256(matches[0].read_bytes()).hexdigest() for matches in files)
        print(f'{name} raw SHA-256 {sha_a} {sha_b}')
    return 0 if passed else 1


if __name__ == '__main__':
    raise SystemExit(main())
