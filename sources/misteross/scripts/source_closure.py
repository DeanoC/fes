#!/usr/bin/env python3
"""Audited functional source roots for first-party core producers."""
import argparse
import ast
import importlib
import json
import re
import subprocess
from pathlib import Path
import sys
import os

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

ROOT = Path(__file__).resolve().parents[1]
MANIFEST = Path(__file__).with_name('source_closures.json')
EXCLUDED_PARTS = {'sim', 'testbench', 'tests'}


def covered(path, roots):
    return any(path == root or path.startswith(root + '/') for root in roots)


def load_manifest(path=MANIFEST):
    # The chosen root list itself enters the record. Read this control file
    # outside the producer read log to avoid a dependency on other entries.
    from scripts.compiler_read_audit import read_execution_data
    data = json.loads(read_execution_data(path))
    if not isinstance(data, dict) or any(not re.fullmatch(r'build_[A-Za-z0-9_]+', name)
        or not isinstance(roots, list) or roots != sorted(set(roots))
        or any(not isinstance(root, str) or root.startswith('/') or '..' in Path(root).parts
               for root in roots) for name, roots in data.items()):
        raise ValueError('invalid source closure manifest')
    return data


def tracked(root=ROOT):
    data = subprocess.check_output(['git', '-C', str(root), 'ls-files', '-z'])
    return {p.decode() for p in data.split(b'\0') if p}


def imports(module_name, root=ROOT):
    """Follow literal imports of scripts modules without executing a producer."""
    pending, result = [module_name.removeprefix('scripts.')], set()
    while pending:
        name = pending.pop()
        relative = f'scripts/{name}.py'
        if relative in result or not (root / relative).is_file():
            continue
        result.add(relative)
        tree = ast.parse((root / relative).read_text(), filename=relative)
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                for alias in node.names:
                    if alias.name.startswith('scripts.'):
                        pending.append(alias.name.split('.')[1])
            elif isinstance(node, ast.ImportFrom):
                if node.module == 'scripts':
                    pending.extend(alias.name for alias in node.names)
                elif node.module and node.module.startswith('scripts.'):
                    pending.append(node.module.split('.')[1])
                elif node.level == 1 and node.module:
                    pending.append(node.module.split('.')[0])
    return result


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
        from scripts.toolchain_cache import RECIPE_FILES
        result.update(RECIPE_FILES)
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


def minimize(reads, listed, files):
    reads, listed = set(reads), set(listed)
    if any(EXCLUDED_PARTS.intersection(Path(p).parts) for p in reads | listed):
        raise ValueError('recorded simulation/testbench read requires explicit investigation')
    roots = set(reads)
    for directory in sorted(listed, key=lambda p: (-p.count('/'), p)):
        if directory and directory in files:
            roots.add(directory)
        elif directory:
            roots.add(directory)
    # Collapse only when every tracked member was actually read or its parent
    # was listed. A directory containing sim/tests is never a legal root.
    directories = {str(Path(p).parent) for p in roots}
    directories |= {str(parent) for p in roots for parent in Path(p).parents if str(parent) != '.'}
    for directory in sorted(directories, key=lambda p: (-p.count('/'), p)):
        if EXCLUDED_PARTS.intersection(Path(directory).parts):
            continue
        members = {p for p in files if p.startswith(directory + '/')}
        if members and all(covered(p, reads | listed) for p in members):
            roots = {p for p in roots if not (p == directory or p.startswith(directory + '/'))}
            roots.add(directory)
    return sorted(roots)


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
