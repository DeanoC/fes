#!/usr/bin/env python3
"""Install/verify RAM Tester's adjacent license and exact-artifact source notice."""
import argparse
import hashlib
from pathlib import Path
import re
import shutil
import tomllib

IMAGE = Path(__file__).resolve().parents[1]
LICENSE = IMAGE / 'licenses/fes.ramtest/COPYING'
REPOSITORY = 'https://github.com/DeanoC/fes.git'


def is_fes_repository(repository):
    return re.fullmatch(
        r'(?:https://(?i:github\.com)/|git@(?i:github\.com):|ssh://git@(?i:github\.com)/)'
        r'(?i:DeanoC/fes)(?:\.git)?/?', repository) is not None


def source_notice(package):
    manifest = tomllib.loads((package / 'manifest.toml').read_text())
    revision = manifest['build']['revision']
    if (manifest['core']['id'] != 'fes.ramtest'
            or not is_fes_repository(manifest['build']['repository'])
            or re.fullmatch(r'[0-9a-f]{40}', revision) is None
            or re.fullmatch(r'[0-9a-f]{64}', package.name) is None):
        raise ValueError('RAM Tester notice requires an exact FES source identity')
    payload_sha = hashlib.sha256((package / 'core.rbf').read_bytes()).hexdigest()
    if payload_sha != manifest['payload']['sha256']:
        raise ValueError('RAM Tester notice payload differs from manifest')
    repository = REPOSITORY.removesuffix('.git')
    return f'''# RAM Tester corresponding source

Core: fes.ramtest
License: GPL-2.0-or-later (full GPL version 2 text: COPYING)
Package: {package.name}
RBF SHA-256: {payload_sha}
Source repository: {repository}
Exact producing commit: {revision}
Source download: {repository}/archive/{revision}.tar.gz

Obtain the complete corresponding source by downloading that archive, or by
cloning the repository and checking out the exact producing commit above.
The archive includes the RTL, shared definitions, build scripts and tool locks.
From sources/misteross, run make toolchain-fes-ramtest followed by
make build-fes-ramtest-100 for the shipped OSS 100 MHz variant.
See cores/fes-ramtest/README.md, scripts/build_fes_ramtest.py and
toolchains/ramtest.lock at the producing commit for the build instructions.

This notice identifies the source of the sealed RBF, including when an image
built from a later FES commit reuses that package. The RBF is installed at
../../../core-packages/{package.name}/core.rbf relative to this directory.
'''.encode()


def process(action, target, package=None):
    root = target / 'usr/share/mister-runtime/core-notices/fes.ramtest'
    expected = None if package is None else {
        'COPYING': LICENSE.read_bytes(), 'SOURCE.md': source_notice(package)}
    if action == 'install':
        if root.is_symlink():
            raise ValueError('RAM Tester notices root must not be a symlink')
        if root.exists():
            for directory in (root, *root.rglob('*')):
                if directory.is_symlink():
                    raise ValueError('RAM Tester notices must not contain symlinks')
                if directory.is_dir():
                    directory.chmod(0o755)
            shutil.rmtree(root)
        if expected is not None:
            destination = root / package.name
            destination.mkdir(parents=True)
            for name, data in expected.items():
                path = destination / name
                path.write_bytes(data)
                path.chmod(0o444)
            # Buildroot removes its copied root after filesystem creation.
            # Keep unlink permission on this adjacent metadata directory;
            # the closed package and both notice files remain read-only.
            destination.chmod(0o755)
    if expected is None:
        if root.exists() or root.is_symlink():
            raise ValueError('unselected RAM Tester has installed notices')
        return
    if root.is_symlink() or not root.is_dir() or {p.name for p in root.iterdir()} != {package.name}:
        raise ValueError('RAM Tester notices do not match selected package')
    destination = root / package.name
    if destination.is_symlink() or not destination.is_dir() or destination.stat().st_mode & 0o777 != 0o755 or {p.name for p in destination.iterdir()} != set(expected):
        raise ValueError('RAM Tester notices members differ')
    for name, data in expected.items():
        path = destination / name
        if path.is_symlink() or not path.is_file() or path.read_bytes() != data or path.stat().st_mode & 0o777 != 0o444:
            raise ValueError('RAM Tester notice missing, changed or unsealed: ' + name)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('install', 'verify'))
    parser.add_argument('target', type=Path)
    parser.add_argument('package', nargs='?', type=Path)
    args = parser.parse_args()
    process(args.action, args.target, args.package)
