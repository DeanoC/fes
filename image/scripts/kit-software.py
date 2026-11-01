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
import tomllib

BINARIES = ('mister-agent', 'mister-runtime', 'fogcast-kit', 'fogcast-tenfoot')
INTERFACES = {'runtime-protocol': '2', 'maintenance': 'fes-kit-maintenance-v1',
              'software-admission': 'fes-kit-software-v1'}
MENU_SELECTION = 'usr/share/mister-runtime/selections/fes-menu.package.toml'
INTERFACE_KEY = re.compile(r'[a-zA-Z0-9][a-zA-Z0-9._+-]{0,127}')
VERSION = re.compile(r'(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})')
TARGET_ABIS = {
    'de10-nano': ('armv7', 'ELF32', 'ARM', 'armv7-linux-gnueabihf'),
    'de25-nano': ('aarch64', 'ELF64', 'AArch64', 'aarch64-linux-gnu'),
}


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


def elf(path, board='de10-nano'):
    if board not in TARGET_ABIS:
        raise ValueError('unsupported software base board: ' + str(board))
    _, elf_class, machine, _ = TARGET_ABIS[board]
    header = run('readelf', '-h', str(path))
    if (not re.search(r'Class:\s+' + elf_class + r'\s*$', header, re.M)
            or not re.search(r'Machine:\s+' + machine + r'\s*$', header, re.M)):
        raise ValueError(f'not {machine} {elf_class}: {path}')
    if (not re.search(r'Data:.*little endian', header)
            or (board == 'de10-nano' and 'Version5 EABI' not in header)):
        raise ValueError(f'unsupported {machine} ABI: {path}')
    dynamic = run('readelf', '-d', str(path))
    # Buildroot sanitizes some libraries by retaining the dynamic tag with
    # an empty value. Empty tags add no search path; nonempty paths remain
    # forbidden so the checked base closure matches loader resolution.
    search_paths = re.findall(r'\((?:RPATH|RUNPATH)\).*?\[(.*?)\]', dynamic)
    if any(search_paths):
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


def base_contract(target, executables, board='de10-nano'):
    if board not in TARGET_ABIS:
        raise ValueError('unsupported software base board: ' + str(board))
    architecture, _, _, abi_name = TARGET_ABIS[board]
    identity = target / 'etc/fes-board'
    if board == 'de25-nano' or identity.exists() or identity.is_symlink():
        if identity.is_symlink() or not identity.is_file() or identity.read_bytes() != (board + '\n').encode():
            raise ValueError('software base requires its exact FES board identity')
    libraries = {}
    for directory in ('lib', 'usr/lib'):
        for p in sorted((target / directory).glob('*.so*')):
            actual = resolve(target, p.relative_to(target))
            # Linker scripts and debugger helpers also match *.so*. Only ELF
            # objects expose loader interfaces. A DT_NEEDED reference to an
            # excluded file still fails the unresolved-library check below.
            with actual.open('rb') as stream:
                if stream.read(4) != b'\x7fELF':
                    continue
            libraries[p.name] = actual
    cache = {}
    def inspect(p):
        if p not in cache:
            cache[p] = elf(p, board)
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
    abi = {'format': 1, 'architecture': abi_name, 'libraries': inventory}
    digest = hashlib.sha256(json.dumps(abi, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
    return {'board': board, 'architecture': architecture,
            'base_contract': 'fes-kit-base-v1-' + digest, 'interfaces': INTERFACES}


def menu_requirements(target, board='de10-nano'):
    """Interfaces the image's sealed menu package declares, as menu.<id> keys."""
    selection = tomllib.loads((target / MENU_SELECTION).read_text())
    package = selection.get('package_id')
    if not isinstance(package, str) or not re.fullmatch('[0-9a-f]{64}', package):
        raise ValueError('invalid menu package selection')
    return menu_package_requirements(
        target / 'usr/share/mister-runtime/core-packages' / package / 'manifest.toml', board)


def menu_package_requirements(manifest, board=None):
    """Interfaces a menu package manifest declares, as menu.<id> keys.

    The runtime counts every declared interface when it admits the menu, so
    optional declarations are required of the software too. Bootstrap artifact
    selection (scripts/image_artifacts.py) applies the same check.
    """
    fields = tomllib.loads(manifest.read_text())
    if board is not None:
        if board not in TARGET_ABIS or fields.get('target', {}).get('platform') != board.replace('-', '_'):
            raise ValueError('menu package target differs from the selected software board')
    interfaces = fields.get('interfaces')
    if not isinstance(interfaces, list) or not interfaces:
        raise ValueError('menu package declares no interfaces')
    requires = {}
    for interface in interfaces:
        if not isinstance(interface, dict):
            raise ValueError(f'invalid menu package interface: {interface!r}')
        name, major, minor = interface.get('id'), interface.get('major'), interface.get('minor')
        name = name if isinstance(name, str) and INTERFACE_KEY.fullmatch(name) else ''
        version = f'{major}.{minor}' if type(major) is int and type(minor) is int else ''
        key = 'menu.' + name
        if not name or not INTERFACE_KEY.fullmatch(key) or not VERSION.fullmatch(version) or key in requires:
            raise ValueError(f'invalid menu package interface: {interface!r}')
        requires[key] = version
    return requires


def runtime_menu_interfaces(runtime):
    """Menu interfaces a runtime admits, from its .fes_menu_interfaces section."""
    dump = run('readelf', '--string-dump=.fes_menu_interfaces', str(runtime))
    provides = {}
    for record in re.findall(r'^\s*\[\s*[0-9a-f]+\]\s+(\S.*)$', dump, re.M):
        name, _, version = record.partition('=')
        key = 'menu.' + name
        if (not INTERFACE_KEY.fullmatch(name) or not INTERFACE_KEY.fullmatch(key)
                or not VERSION.fullmatch(version) or key in provides):
            raise ValueError(f'invalid menu interface record in {runtime}: {record!r}')
        provides[key] = version
    if not provides:
        raise ValueError(f'runtime declares no menu interfaces: {runtime}')
    return provides


def unmet(provides, requires):
    """Required interfaces not provided at the same major and at least that minor."""
    missing = []
    for key, version in sorted(requires.items()):
        have, need = VERSION.fullmatch(provides.get(key, '')), VERSION.fullmatch(version)
        if not have or not need or have[1] != need[1] or int(have[2]) < int(need[2]):
            missing.append(f'{key} {version}')
    return missing


def assemble(target, fogcast, runtime_revision, window_driver=None, board='de10-nano'):
    if board not in TARGET_ABIS:
        raise ValueError('unsupported factory software board: ' + str(board))
    if board == 'de10-nano' and window_driver is None:
        raise ValueError('DE10 factory software requires its fes-window driver')
    if board == 'de25-nano' and window_driver is not None:
        raise ValueError('DE25 uses its built-in DMA owner; a fes-window module is invalid')
    target = target.resolve()
    inputs = [target / 'usr/sbin' / name for name in BINARIES]
    for p in inputs:
        if p.is_symlink() or not p.is_file():
            raise ValueError(f'fresh regular executable required: {p}')
    # The image records what its sealed menu needs; its own runtime must admit it.
    requires = menu_requirements(target, board)
    contract = base_contract(target, inputs, board)
    provides = runtime_menu_interfaces(target / 'usr/sbin/mister-runtime')
    if missing := unmet(provides, requires):
        raise ValueError('factory runtime does not accept the menu package interfaces: ' + ', '.join(missing))
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
        if board == 'de10-nano':
            # DE10 loads the fes-window driver its release carries.
            window_driver = Path(window_driver)
            if sorted(p.name for p in window_driver.iterdir()) != ['fes_window.ko', 'version'] or \
                    not re.fullmatch(r'[0-9a-f]{16}', (window_driver / 'version').read_text().strip()):
                raise ValueError('fes-window driver input must be fes_window.ko and its version')
            (payload / 'share/fes-window').mkdir(parents=True)
            for name in ('fes_window.ko', 'version'):
                shutil.copyfile(window_driver / name, payload / 'share/fes-window' / name)
        metadata = {**contract, 'interfaces': {**contract['interfaces'], **provides},
                    'format': 1, 'version': 'factory-' + revision[:12],
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
    base.write_text(json.dumps({**contract, 'requires': requires}, sort_keys=True, separators=(',', ':')) + '\n')
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
    requires = expected.pop('requires', {})
    executables = [payload / 'bin' / name for name in BINARIES]
    for p in executables:
        if p.is_symlink() or not p.is_file():
            raise ValueError(f'regular payload executable required: {p}')
    actual = base_contract(base_root, executables, expected.get('board'))
    if expected != actual:
        raise ValueError('retained base ELF contract differs from recorded compatibility')
    # The bundle lists the menu interfaces its runtime admits. A base image
    # holds back a bundle that does not cover its own menu, so refuse it here.
    provides = runtime_menu_interfaces(payload / 'bin/mister-runtime')
    if missing := unmet(provides, requires):
        raise ValueError('new runtime does not accept the base image menu interfaces: ' + ', '.join(missing))
    revision = (fogcast / 'bin/kit-software.revision').read_text().strip()
    if not re.fullmatch(r'[a-f0-9]{40}', revision) or not re.fullmatch(r'[a-f0-9]{40}', runtime_revision):
        raise ValueError('software provenance requires full source revisions')
    metadata = {**expected, 'interfaces': {**expected['interfaces'], **provides}, 'format': 1,
                'version': version, 'provenance': {'fogcast': revision, 'runtime': runtime_revision}}
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
    parser.add_argument('--board', choices=tuple(TARGET_ABIS), default='de10-nano',
                        help='explicit factory target; updates use the authenticated base')
    parser.add_argument('--window-driver', type=Path, help='built fes-window directory (DE10 factory only)')
    args = parser.parse_args()
    if args.target:
        if args.payload or args.output or args.version:
            parser.error('--payload, --output and --version require --base-root')
        assemble(args.target, args.fogcast, args.runtime_revision, args.window_driver, args.board)
    else:
        if not args.payload or not args.output or not args.version:
            parser.error('--base-root requires --payload, --output and --version')
        result = pack_update(args.base_root, args.payload, args.output, args.version,
                             args.fogcast, args.runtime_revision)
        print(json.dumps(result, sort_keys=True))


if __name__ == '__main__':
    main()
