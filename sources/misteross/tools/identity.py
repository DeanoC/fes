#!/usr/bin/env python3
"""Report separate input fingerprints without changing sealed build identities."""
import argparse
import hashlib
import json
from pathlib import Path
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.export_core_package import (
    MAX_BUILD_RECORD_SIZE, PackageExportError, _decode_build_record, build_identity,
)


# Everything not explicitly classified as execution remains a design input.
# In particular producer scripts can generate HDL, constraints and tool argv.
EXECUTION_PARAMETERS = frozenset({
    'execution_sha256', 'gpu_device', 'gpu_backend', 'toolchain_configuration',
})


def _digest(domain, fields):
    encoded = json.dumps(fields, ensure_ascii=False, sort_keys=True,
                         separators=(',', ':')).encode('utf-8')
    return hashlib.sha256(domain + b'\0' + encoded).hexdigest()


def projections(record):
    fields = _decode_build_record(record)
    if fields['format'] != 2:
        raise PackageExportError('identity report requires a format-2 source-closed record')
    design = {key: value for key, value in fields.items()
              if key not in {'repository', 'revision', 'source_path', 'source_roots',
                             'tools', 'toolchain', 'source_inputs', 'parameters'}}
    locks = {path: digest for path, digest in fields['source_inputs'].items()
             if Path(path).name == 'toolchain.lock'}
    design['source_inputs'] = {path: digest for path, digest in fields['source_inputs'].items()
                               if path not in locks}
    design['parameters'] = {key: value for key, value in fields['parameters'].items()
                            if key not in EXECUTION_PARAMETERS}
    toolchain = {
        'tools': fields['tools'],
        'locks': locks,
        'parameters': {key: value for key, value in fields['parameters'].items()
                       if key in EXECUTION_PARAMETERS},
    }
    if 'toolchain' in fields:
        toolchain['toolchain'] = fields['toolchain']
    return fields, design, toolchain


def report(record):
    fields, design, toolchain = projections(record)
    return {
        'format': 1,
        'build_id': build_identity(record),
        'design_inputs_sha256': _digest(b'fes-design-inputs-report-v1', design),
        'toolchain_inputs_sha256': _digest(b'fes-toolchain-inputs-report-v1', toolchain),
        'record_sha256': hashlib.sha256(record).hexdigest(),
        'repository': fields['repository'],
        'revision': fields['revision'],
        'tools': fields['tools'],
        'toolchain_locks': toolchain['locks'],
        'execution_parameters': toolchain['parameters'],
        'design_classification': 'conservative-source-closure',
    }


def compare(built, current):
    before, after = report(built), report(current)
    left, right = projections(built)[0], projections(current)[0]
    design_changed = before['design_inputs_sha256'] != after['design_inputs_sha256']
    toolchain_changed = before['toolchain_inputs_sha256'] != after['toolchain_inputs_sha256']
    return {
        'built_with': before,
        'candidate': after,
        'design_inputs_changed': design_changed,
        'toolchain_inputs_changed': toolchain_changed,
        'toolchain_change_only': toolchain_changed and not design_changed,
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
    args = parser.parse_args(argv)
    try:
        original = _read(args.record)
        result = compare(original, _read(args.current)) if args.current else report(original)
    except (OSError, ValueError) as exc:
        parser.exit(1, f'identity report: {exc}\n')
    print(json.dumps(result, ensure_ascii=False, sort_keys=True, indent=2))


if __name__ == '__main__':
    main()
