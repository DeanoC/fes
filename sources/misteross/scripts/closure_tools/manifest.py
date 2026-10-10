#!/usr/bin/env python3
"""Maintain the audited source-closure manifest (static, derive, check, record).

Kept out of the top-level scripts/ modules on purpose: it loads producers by
computed name, and no producer may depend on it. A top-level script importing
this package is reported as an unresolved import by the HIL planner.
"""
import argparse
import importlib
import json
import os
from pathlib import Path
import subprocess
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from scripts.source_closure import (EXCLUDED_PARTS, ROOT, TOOLCHAIN_RECIPE_FILES, covered, imports,
                                    load_manifest, minimize, tracked)


def producer_inputs(name):
    module = importlib.import_module('scripts.' + name)
    if name == 'build_fes_ramtest':
        inputs = module.inputs_for(100)
    elif name == 'build_fes_coleco_socket_v2':
        inputs = module.video_profile(native_video=True)[2]
    elif hasattr(module, 'PINNED_INPUTS'):
        inputs = module.PINNED_INPUTS
    else:
        inputs = module.INPUTS
    return set(inputs)


def required(name, root=ROOT):
    module = importlib.import_module('scripts.' + name)
    inputs = producer_inputs(name)
    for attr in ('RECIPE', 'ABI_DEFINITION', 'CONTRACT', 'TOOLCHAIN_LOCK',
                 'MENU_TOOLCHAIN_LOCK', 'SOCKET_TOOLCHAIN_LOCK', 'SMS_TOOLCHAIN_LOCK',
                 'SG1000_TOOLCHAIN_LOCK', 'COLECO_TOOLCHAIN_LOCK'):
        value = getattr(module, attr, None)
        if isinstance(value, str) and (root / value).is_file():
            inputs.add(value)
    result = inputs | imports(name, root)
    if 'scripts/toolchain_cache.py' in result:
        # Tool authentication hashes the toolchain recipe into the cache key.
        result.update(TOOLCHAIN_RECIPE_FILES)
    return result


def static(name, root=ROOT):
    paths = required(name, root)
    # Core-local top-level files and functional subtrees are conservative, while
    # shared RTL is limited to files named by the producer's explicit inputs.
    core_dirs = {('/'.join(p.split('/')[:2])) for p in paths if p.startswith('cores/')}
    files = tracked(root)
    for core in core_dirs - {'cores/fes-common'}:
        paths.update(p for p in files if p.startswith(core + '/') and
                     not EXCLUDED_PARTS.intersection(Path(p).parts) and
                     Path(p).suffix.lower() not in ('.md', '.markdown'))
    return minimize(paths, set(), files)


def derive(name, records, root=ROOT):
    reads, listed = set(), set()
    for record in records:
        for line in Path(record).read_text().splitlines():
            event = json.loads(line)
            (listed if event['event'] == 'list' else reads).add(event['path'])
    reads.update(required(name, root))
    return minimize(reads, listed, tracked(root))


def check(root=ROOT, manifest=None):
    manifest = load_manifest() if manifest is None else manifest
    files = tracked(root)
    for name, roots in manifest.items():
        for entry in required(name, root):
            if not covered(entry, roots):
                raise ValueError(f'{name}: missing required source {entry}')
        for entry in roots:
            if EXCLUDED_PARTS.intersection(Path(entry).parts):
                raise ValueError(f'{name}: forbidden simulation/test root {entry}')
            if not any(covered(file, [entry]) for file in files) or not (root / entry).exists():
                raise ValueError(f'{name}: missing or untracked root {entry}')
    return len(manifest)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('static', 'derive', 'check', 'record'))
    parser.add_argument('--producer')
    parser.add_argument('--record', action='append', default=[])
    parser.add_argument('--record-only', action='store_true')
    args, producer_args = parser.parse_known_args()
    if args.action != 'record' and producer_args:
        parser.error('unexpected producer arguments')
    if args.action == 'check':
        print(f'{check()} source closures valid')
    elif args.action == 'record':
        if not args.producer or len(args.record) != 1:
            parser.error('record requires --producer and one --record FILE')
        script = ROOT / 'scripts' / (args.producer + '.py')
        if not script.is_file():
            parser.error('unknown producer script')
        destination = Path(args.record[0]).resolve()
        if not destination.parent.is_dir():
            parser.error('record directory does not exist')
        environment = dict(os.environ, FES_SOURCE_READ_RECORD=str(destination))
        if args.record_only:
            environment['FES_SOURCE_CLOSURE_RECORD_ONLY'] = '1'
        arguments = producer_args[1:] if producer_args[:1] == ['--'] else producer_args
        subprocess.run([sys.executable, str(script), '--root', str(ROOT),
                        '--identity-version', '2', *arguments],
                       cwd=ROOT, env=environment, check=True)
    else:
        if not args.producer:
            parser.error('--producer required')
        print(json.dumps({args.producer: static(args.producer) if args.action == 'static'
                          else derive(args.producer, args.record)}, indent=2))


if __name__ == '__main__':
    main()
