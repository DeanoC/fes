#!/usr/bin/env python3
"""Report design, toolchain and record identities, including update advisories."""
import argparse
import hashlib
import json
from pathlib import Path
import sys
import subprocess

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.export_core_package import (
    MAX_BUILD_RECORD_SIZE, PackageExportError, _decode_build_record, build_identity,
)


from scripts.input_identity import with_identities, projections as input_projections, toolchain_selection


def projections(record):
    fields = _decode_build_record(record)
    if fields['format'] != 2:
        raise PackageExportError('identity report requires a format-2 source-closed record')
    classified = fields if 'identities' in fields else with_identities(fields)
    design, toolchain = input_projections(classified)
    return fields, design, toolchain


def report(record):
    fields, design, toolchain = projections(record)
    ids = (fields if 'identities' in fields else with_identities(fields))['identities']
    return {
        'format': 1,
        'build_id': build_identity(record),
        'design_inputs_sha256': ids['design'],
        'toolchain_inputs_sha256': ids['toolchain'],
        'record_sha256': hashlib.sha256(record).hexdigest(),
        'repository': fields['repository'],
        'revision': fields['revision'],
        'tools': fields['tools'],
        'selection_only': fields['parameters'].get('selection_only', False),
        'pins': json.loads(fields['parameters'].get('toolchain_pins', '{}')),
        'toolchain_locks': {path: value for path, value in toolchain['source_inputs'].items()
                            if Path(path).name in ('toolchain.lock', 'quartus.lock')},
        'execution_parameters': toolchain['parameters'],
        'design_classification': 'design-v1',
        'cache_migration_required': 'identities' not in fields,
    }


def compare(built, current):
    before, after = report(built), report(current)
    left, right = projections(built)[0], projections(current)[0]
    design_changed = before['design_inputs_sha256'] != after['design_inputs_sha256']
    if after['selection_only']:
        # Selection does not authenticate the currently installed binaries or
        # execution environment. Compare only recorded source/pin choices.
        toolchain_changed = toolchain_selection(left) != toolchain_selection(right)
    else:
        toolchain_changed = before['toolchain_inputs_sha256'] != after['toolchain_inputs_sha256']
    return {
        'built_with': before,
        'candidate': after,
        'design_inputs_changed': design_changed,
        'toolchain_inputs_changed': toolchain_changed,
        'toolchain_change_only': toolchain_changed and not design_changed,
        'execution_comparison_available': not after['selection_only'],
        'changed_source_inputs': sorted(path for path in left['source_inputs'].keys() | right['source_inputs'].keys()
                                       if left['source_inputs'].get(path) != right['source_inputs'].get(path)),
        # Advisory classification does not confer cache reuse or hardware acceptance.
        'current_build_id_matches': before['build_id'] == after['build_id'],
    }


def _read(path):
    with path.open('rb') as stream:
        return stream.read(MAX_BUILD_RECORD_SIZE + 1)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('record', type=Path, help='original build-inputs.json')
    parser.add_argument('--current', type=Path, help='candidate canonical record from the selected producer')
    parser.add_argument('--source', type=Path, help='clean committed misteross source; reads current pins without a compiler')
    parser.add_argument('--core', help='core ID for --source, for example fes.zx81')
    parser.add_argument('--board', default='de10-nano', choices=('de10-nano', 'de25-nano'))
    args = parser.parse_args(argv)
    try:
        original = _read(args.record)
        if args.source:
            if not args.core or args.current:
                raise ValueError('--source requires --core and cannot be combined with --current')
            program = '''
import sys
from scripts.bundle import canonical_package_record, recipe_for
sys.stdout.buffer.write(canonical_package_record(sys.argv[1],
    recipe=recipe_for(sys.argv[2], board=sys.argv[3]), selection_only=True))
'''
            probe = subprocess.run([sys.executable, '-c', program, str(args.source.resolve()), args.core, args.board],
                cwd=Path(__file__).resolve().parents[3], capture_output=True)
            if probe.returncode:
                raise ValueError('cannot derive selected design inputs: ' + probe.stderr.decode('utf-8', errors='replace').strip())
            result = compare(original, probe.stdout)
        else:
            if args.core:
                raise ValueError('--core requires --source')
            result = compare(original, _read(args.current)) if args.current else report(original)
    except (OSError, ValueError) as exc:
        parser.exit(1, f'identity report: {exc}\n')
    print(json.dumps(result, ensure_ascii=False, sort_keys=True, indent=2))


if __name__ == '__main__':
    main()
