#!/usr/bin/env python3
"""Assemble the factory userspace bundle against the actual base ELF ABI.

The compatibility ID describes exported system interfaces, not an OS revision.
All dynamic dependencies and required versioned symbols must resolve in that
base. This deliberately does not package system libraries inside updates.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

BINARIES = ('mister-agent', 'mister-runtime', 'fogcast-kit', 'fogcast-tenfoot')
INTERFACES = {'runtime-protocol': '2', 'maintenance': 'fes-kit-maintenance-v1',
              'software-admission': 'fes-kit-software-v1'}


def run(*args):
    return subprocess.check_output(args, text=True, env={**os.environ, 'LC_ALL': 'C'})


def resolve(root, path):
    """Resolve target symlinks within the target, including absolute links."""
    root = root.resolve()
    parts = list(Path(path).parts)
    if parts and parts[0] == '/':
        parts.pop(0)
    result = root
    count = 0
    while parts:
        part = parts.pop(0)
        if part == '..':
            result = result.parent
        elif part != '.':
            result = result / part
        if not result.is_relative_to(root):
            raise ValueError('dependency escapes target')
        if result.is_symlink():
            count += 1
            if count > 40:
                raise ValueError('dependency symlink loop')
            link = Path(os.readlink(result))
            result = root if link.is_absolute() else result.parent
            parts = list(link.parts[1:] if link.is_absolute() else link.parts) + parts
    if not result.is_file():
        raise ValueError(f'missing dependency {path}')
    return result


def elf(path):
    header = run('readelf', '-h', str(path))
    if not re.search(r'Class:\s+ELF32', header) or not re.search(r'Machine:\s+ARM\s*$', header, re.M):
        raise ValueError(f'not ARM ELF32: {path}')
    if not re.search(r'Data:.*little endian', header) or 'Version5 EABI' not in header:
        raise ValueError(f'unsupported ARM ABI: {path}')
    dynamic = run('readelf', '-d', str(path))
    if re.search(r'\((?:RPATH|RUNPATH)\)', dynamic):
        raise ValueError(f'embedded dynamic search path: {path}')
    needed = re.findall(r'\(NEEDED\).*?\[(.*?)\]', dynamic)
    sonames = re.findall(r'\(SONAME\).*?\[(.*?)\]', dynamic)
    program = run('readelf', '-l', str(path))
    interpreter = re.findall(r'Requesting program interpreter:\s*([^\]]+)\]', program)
    defined, required = set(), set()
    for line in run('readelf', '--dyn-syms', '--wide', str(path)).splitlines():
        fields = line.split()
        if len(fields) < 8 or not fields[0].endswith(':'):
            continue
        _, _, _, _, binding, _, index, name = fields[:8]
        if binding not in ('GLOBAL', 'WEAK'):
            continue
        if index == 'UND':
            if binding == 'GLOBAL':
                required.add(name)
        else:
            defined.add(name.replace('@@', '@'))
            if '@@' in name:
                defined.add(name.split('@@')[0])
    return {'needed': needed, 'soname': sonames, 'interpreter': interpreter,
            'defined': sorted(defined), 'required': sorted(required)}


def base_contract(target, executables):
    libraries = {}
    for directory in ('lib', 'usr/lib'):
        for p in sorted((target / directory).glob('*.so*')):
            actual = resolve(target, p.relative_to(target))
            libraries[p.name] = actual
    cache = {}
    def inspect(p):
        if p not in cache:
            cache[p] = elf(p)
        return cache[p]
    # Pin the whole available base-library interface set, not only the current
    # bundle's dependency subset: future bundles may use another existing lib.
    inventory = []
    for name, p in sorted(libraries.items()):
        info = inspect(p)
        inventory.append({'name': name, 'soname': info['soname'], 'symbols': info['defined']})
    for executable in executables:
        pending, closure = [executable], set()
        while pending:
            p = pending.pop()
            if p in closure:
                continue
            closure.add(p)
            info = inspect(p)
            for interp in info['interpreter']:
                pending.append(resolve(target, interp))
            for needed in info['needed']:
                if '/' in needed or needed not in libraries:
                    raise ValueError(f'unresolved system library {needed}: {p}')
                pending.append(libraries[needed])
        provided = set().union(*(set(inspect(p)['defined']) for p in closure))
        for p in closure:
            missing = set(inspect(p)['required']) - provided
            if missing:
                raise ValueError(f'unresolved base symbols in {p}: {sorted(missing)}')
    abi = {'format': 1, 'architecture': 'armv7-linux-gnueabihf', 'libraries': inventory}
    digest = hashlib.sha256(json.dumps(abi, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
    return {'board': 'de10-nano', 'architecture': 'armv7',
            'base_contract': 'fes-kit-base-v1-' + digest, 'interfaces': INTERFACES}


def assemble(target, fogcast, runtime_revision):
    target = target.resolve()
    inputs = [target / 'usr/sbin' / name for name in BINARIES]
    for p in inputs:
        if p.is_symlink() or not p.is_file():
            raise ValueError(f'fresh regular executable required: {p}')
    contract = base_contract(target, inputs)
    # Build-time helper identity is derived from the same selected FogCast
    # sources as its agent, and is recorded by the Makefile beside the helper.
    revision = (fogcast / 'bin/kit-software.revision').read_text().strip()
    if not re.fullmatch(r'[a-f0-9]{40}', revision) or not re.fullmatch(r'[a-f0-9]{40}', runtime_revision):
        raise ValueError('software provenance requires full source revisions')
    destination = target / 'usr/share/fes/software'
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.software-', dir=destination.parent) as temporary:
        temporary = Path(temporary)
        payload = temporary / 'payload'
        (payload / 'bin').mkdir(parents=True)
        for p in inputs:
            shutil.copyfile(p, payload / 'bin' / p.name)
            (payload / 'bin' / p.name).chmod(0o555)
        assets = target / 'usr/share/fogcast'
        if assets.is_symlink():
            raise ValueError('fresh factory assets must not be a symlink')
        if assets.exists():
            shutil.copytree(assets, payload / 'share/fogcast', symlinks=True)
        metadata = {**contract, 'format': 1, 'version': 'factory-' + revision[:12],
                    'provenance': {'fogcast': revision, 'runtime': runtime_revision}}
        meta = temporary / 'metadata.json'
        meta.write_text(json.dumps(metadata))
        result = json.loads(run(str(fogcast / 'bin/fes-kit-bundle-linux-amd64'), 'pack',
                                '--manifest', str(meta), '--root', str(payload),
                                '--output', str(temporary / 'bundle.tar')))
        manifest = result['manifest']
        for entry in manifest['files']:
            (payload / entry['path']).chmod(entry['mode'])
        (payload / 'manifest.json').write_text(json.dumps(manifest, separators=(',', ':')))
        (payload / 'manifest.json').chmod(0o444)
        if destination.is_symlink():
            raise ValueError('unsafe factory software destination')
        if destination.exists():
            shutil.rmtree(destination)
        os.rename(payload, destination)
    base = target / 'etc/fes/software-base.json'
    base.parent.mkdir(parents=True, exist_ok=True)
    base.write_text(json.dumps(contract, sort_keys=True, separators=(',', ':')) + '\n')
    for name in BINARIES:
        p = target / 'usr/sbin' / name
        p.unlink()
        p.symlink_to('/run/fogcast/software/current/bin/' + name)
    if assets.exists():
        shutil.rmtree(assets)
        assets.symlink_to('/run/fogcast/software/current/share/fogcast')


def pack_update(base_root, payload, output, version, fogcast, runtime_revision):
    """Package against a retained base without writing to that base or payload."""
    base_root, payload = base_root.resolve(), payload.resolve()
    tool = str(fogcast / 'bin/fes-kit-bundle-linux-amd64')
    base_path = base_root / 'etc/fes/software-base.json'
    # Authenticate the retained factory payload and its recorded contract before
    # treating that base as an authority for a newly built software bundle.
    run(tool, 'verify-directory', '--root', str(base_root / 'usr/share/fes/software'),
        '--base', str(base_path))
    expected = json.loads(base_path.read_text())
    executables = [payload / 'bin' / name for name in BINARIES]
    for p in executables:
        if p.is_symlink() or not p.is_file():
            raise ValueError(f'regular payload executable required: {p}')
    actual = base_contract(base_root, executables)
    if expected != actual:
        raise ValueError('retained base ELF contract differs from recorded compatibility')
    revision = (fogcast / 'bin/kit-software.revision').read_text().strip()
    if not re.fullmatch(r'[a-f0-9]{40}', revision) or not re.fullmatch(r'[a-f0-9]{40}', runtime_revision):
        raise ValueError('software provenance requires full source revisions')
    metadata = {**expected, 'format': 1, 'version': version,
                'provenance': {'fogcast': revision, 'runtime': runtime_revision}}
    with tempfile.TemporaryDirectory(prefix='fes-kit-software-') as temporary:
        meta = Path(temporary) / 'metadata.json'
        meta.write_text(json.dumps(metadata))
        return json.loads(run(tool, 'pack', '--manifest', str(meta), '--root', str(payload),
                              '--output', str(output)))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument('--target', type=Path, help='assemble this Buildroot factory root')
    mode.add_argument('--base-root', type=Path, help='retained extracted base for a software-only update')
    parser.add_argument('--payload', type=Path, help='new bin/ and share/ tree (update only)')
    parser.add_argument('--output', type=Path, help='software archive output (update only)')
    parser.add_argument('--version', help='software version label (update only)')
    parser.add_argument('--fogcast', type=Path, required=True)
    parser.add_argument('--runtime-revision', required=True)
    args = parser.parse_args()
    if args.target:
        if args.payload or args.output or args.version:
            parser.error('--payload, --output and --version require --base-root')
        assemble(args.target, args.fogcast, args.runtime_revision)
    else:
        if not args.payload or not args.output or not args.version:
            parser.error('--base-root requires --payload, --output and --version')
        result = pack_update(args.base_root, args.payload, args.output, args.version,
                             args.fogcast, args.runtime_revision)
        print(json.dumps(result, sort_keys=True))


if __name__ == '__main__':
    main()
