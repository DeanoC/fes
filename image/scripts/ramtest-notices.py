#!/usr/bin/env python3
"""Install/verify RAM Tester's adjacent license and exact-artifact source notice."""
import argparse
import hashlib
from pathlib import Path
import re
import shutil
import tomllib

IMAGE = Path(__file__).resolve().parents[1]
REPO = IMAGE.parent
LICENSE = IMAGE / 'licenses/fes.ramtest/COPYING'
RAMTEST_LOCK = REPO / 'sources/misteross/cores/fes-ramtest/toolchain.lock'
REPOSITORY = 'https://github.com/DeanoC/fes.git'


def reject_target_symlinks(target, path):
    """Do not read or modify metadata through a target-tree directory alias."""
    current = target
    for name in ('', *path.relative_to(target).parts):
        if name:
            current = current / name
        if current.is_symlink():
            raise ValueError('RAM Tester target paths must not contain symlinks')


def is_fes_repository(repository):
    return re.fullmatch(
        r'(?:https://(?i:github\.com)/|git@(?i:github\.com):|ssh://git@(?i:github\.com)/)'
        r'(?i:DeanoC/fes)(?:\.git)?/?', repository) is not None


def ramtest_nextpnr_pin():
    """Nextpnr commit in RAM Tester's per-core lock, for the shipped notice."""
    lock = tomllib.loads(RAMTEST_LOCK.read_text())
    commit = lock['tool']['nextpnr']['commit']
    if re.fullmatch(r'[0-9a-f]{40}', commit) is None:
        raise ValueError('RAM Tester nextpnr pin must be a 40-digit commit')
    return commit


def package_identity(package):
    if package.is_symlink() or not package.is_dir():
        raise ValueError('RAM Tester package must be a regular directory')
    for name in ('manifest.toml', 'core.rbf'):
        path = package / name
        if path.is_symlink() or not path.is_file():
            raise ValueError('RAM Tester package inputs must be regular non-symlink files')
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
    return {'id': package.name, 'payload_sha': payload_sha, 'revision': revision}


def _format_source_notice(identity, tool_lock_details):
    repository = REPOSITORY.removesuffix('.git')
    return f'''# RAM Tester corresponding source

Core: fes.ramtest
License: GPL-2.0-or-later (full GPL version 2 text: COPYING)
Package: {identity['id']}
RBF SHA-256: {identity['payload_sha']}
Source repository: {repository}
Exact producing commit: {identity['revision']}
Source download: {repository}/archive/{identity['revision']}.tar.gz

Obtain the complete corresponding source by downloading that archive, or by
cloning the repository and checking out the exact producing commit above.
The archive includes the RTL, shared definitions, build scripts and tool locks.
From sources/misteross, run make toolchain-fes-ramtest followed by
make build-fes-ramtest-100 for the shipped OSS 100 MHz variant.
See cores/fes-ramtest/README.md and scripts/build_fes_ramtest.py at the
producing commit for the build instructions.{tool_lock_details}

This notice identifies the source of the sealed RBF, including when an image
built from a later FES commit reuses that package. The RBF is installed at
../../../core-packages/{identity['id']}/core.rbf relative to this directory.
'''.encode()


def _current_tool_lock_details(pin):
    return f''' The tool lock is the one named by
the fes.ramtest lock_path in config/core-recipes.toml at that commit. Current
packages use sources/misteross/cores/fes-ramtest/toolchain.lock
(nextpnr pin {pin}). Packages from the shared-lock period used
sources/misteross/toolchain.lock. Earlier packages used
sources/misteross/toolchains/ramtest.lock.'''


def source_notice(package):
    identity = package_identity(package)
    return _format_source_notice(identity, _current_tool_lock_details(ramtest_nextpnr_pin()))


def retained_source_notice_matches(package, source):
    """Match only the byte-exact notice generated from the current template."""
    return source == source_notice(package)


def process(action, target, package=None):
    root = target / 'usr/share/mister-runtime/core-notices/fes.ramtest'
    reject_target_symlinks(target, root)
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


def _retained_notice_context(target):
    root = target / 'usr/share/mister-runtime/core-notices/fes.ramtest'
    reject_target_symlinks(target, root)
    if not root.exists():
        return None
    if not root.is_dir():
        raise ValueError('RAM Tester notices root must be a directory')
    members = list(root.iterdir())
    if len(members) != 1 or re.fullmatch(r'[0-9a-f]{64}', members[0].name) is None:
        raise ValueError('RAM Tester retained notice must identify one exact package')
    package = target / 'usr/share/mister-runtime/core-packages' / members[0].name
    reject_target_symlinks(target, package)
    reject_target_symlinks(target, target / 'usr/share/mister-runtime/selections/fes-ramtest.package.toml')
    destination = members[0]
    if (destination.is_symlink() or not destination.is_dir()
            or destination.stat().st_mode & 0o777 != 0o755
            or {p.name for p in destination.iterdir()} != {'COPYING', 'SOURCE.md'}):
        raise ValueError('RAM Tester notices members differ')
    license_path = destination / 'COPYING'
    source_path = destination / 'SOURCE.md'
    for path in (license_path, source_path):
        if path.is_symlink() or not path.is_file() or path.stat().st_mode & 0o777 != 0o444:
            raise ValueError('RAM Tester notice missing, changed or unsealed: ' + path.name)
    if license_path.read_bytes() != LICENSE.read_bytes():
        raise ValueError('RAM Tester notice missing, changed or unsealed: COPYING')
    return members[0], package, source_path


def verify_retained(target):
    """Admit only a current-format notice for the package retained by a warm build."""
    retained = _retained_notice_context(target)
    if retained is None:
        return None
    package_id, package, source_path = retained
    if not retained_source_notice_matches(package, source_path.read_bytes()):
        raise ValueError('RAM Tester retained notice is stale, changed or unsealed: SOURCE.md')
    return package_id / 'SOURCE.md'


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('install', 'verify', 'verify-retained'))
    parser.add_argument('target', type=Path)
    parser.add_argument('package', nargs='?', type=Path)
    args = parser.parse_args()
    if args.action == 'verify-retained':
        if args.package is not None:
            parser.error(args.action + ' derives the package from the target tree')
        notice = verify_retained(args.target)
        if notice is not None:
            print(notice)
    else:
        process(args.action, args.target, args.package)
