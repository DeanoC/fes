#!/usr/bin/env python3
"""Bind unchanged factory analysis to this phase's fixtures and source checkout."""
import hashlib
import json
from pathlib import Path
import sys

BASE = Path(__file__).resolve().parent
SOURCE_RELATIVE = Path('sources/misteross/cores/fes-coleco/diagnostic')


def verify(path, identity):
    data = path.read_bytes()
    if hashlib.sha256(data).hexdigest() != identity['sha256'] or len(data) != identity['size']:
        raise ValueError(f'Phase input identity changed: {path}')


def main():
    binding = json.loads((BASE / 'phase-preparation.json').read_bytes())
    root = next(path for path in BASE.parents if (path / '.git').exists() and (path / SOURCE_RELATIVE).is_dir())
    for item in binding['copied_files']:
        verify(BASE / item['phase_path'], item)
    for item in binding['fixture_files']:
        verify(BASE.parent / 'fixture' / item['name'], item)
    for item in binding['source_bindings']:
        verify(root / item['path'], item)
    verify(BASE / 'fixture-input.json', binding['derived_fixture_input'])
    verify(Path(__file__), binding['phase_launcher'])
    if any(arg == '--fixture' or arg.startswith('--fixture=') for arg in sys.argv[1:]):
        raise ValueError('This phase launcher fixes --fixture to its bound fixture-input.json')

    import analyze_factory
    import fixture_oracle
    import native_oracle
    fixture_oracle.ROOT = native_oracle.ROOT = root
    fixture_oracle.SOURCE = root / SOURCE_RELATIVE
    native_oracle.SOURCE = root / SOURCE_RELATIVE / 'generate.py'
    sys.argv += ['--fixture', str(BASE / 'fixture-input.json')]
    return analyze_factory.main()


if __name__ == '__main__':
    sys.exit(main())
