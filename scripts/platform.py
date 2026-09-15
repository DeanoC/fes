#!/usr/bin/env python3
"""Build the FES platform boot selector against the selected FogCast appliance module.

This module is imported as ``platform`` when ``scripts/`` is on ``sys.path``.
Re-export the standard-library ``platform`` API so uuid and other stdlib
importers keep working.
"""
import hashlib
import importlib.util
import os
from pathlib import Path
import re
import subprocess
import tempfile


def _stdlib_platform():
    # ``scripts/`` is first on sys.path when build scripts run directly. Load
    # the stdlib implementation by filename so this module can safely be
    # imported as either ``scripts.platform`` or the top-level ``platform``.
    standard_path = Path(os.__file__).resolve().with_name('platform.py')
    spec = importlib.util.spec_from_file_location('_fes_stdlib_platform', standard_path)
    if spec is None or spec.loader is None:
        raise ImportError(f'cannot load standard-library platform from {standard_path}')
    loaded = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(loaded)
    return loaded


_STDLIB_PLATFORM = _stdlib_platform()


def __getattr__(name):
    return getattr(_STDLIB_PLATFORM, name)


def __dir__():
    return sorted(set(globals()) | set(dir(_STDLIB_PLATFORM)))


__all__ = [name for name in dir(_STDLIB_PLATFORM) if not name.startswith('_')]

try:
    from . import appliance as _appliance
except ImportError:
    _appliance = None


def _appliance_module():
    if _appliance is not None:
        return _appliance
    import appliance
    return appliance


def _digest(path):
    try:
        from .media_inputs import digest
    except ImportError:
        from media_inputs import digest
    return digest(path)

PLATFORM_MODULE = 'github.com/DeanoC/fes/platform'
APPLIANCE_MODULE = 'github.com/DeanoC/FogCast/appliance'
PACKAGE = './cmd/fes-boot'
BUILD_FLAGS = ('-trimpath', '-buildvcs=false', '-ldflags=-s -w -buildid=')
BUILD_ENV = {
    'CGO_ENABLED': '0',
    'GOARCH': 'arm',
    'GOARM': '7',
    'GOOS': 'linux',
    'GOPROXY': 'off',
    'GOSUMDB': 'off',
    'GOTOOLCHAIN': 'local',
}
RECORD_FIELDS = {
    'appliance_module', 'appliance_module_sha256', 'build_env', 'build_flags',
    'fogcast_revision', 'go_sha256', 'go_version', 'package', 'platform_module', 'platform_sha256',
}
SKIP_TREE_NAMES = frozenset({'go.work', 'go.work.sum'})


def tree_identity(root):
    root = Path(root)
    if not root.is_dir() or root.is_symlink():
        raise ValueError('source tree must be a directory: ' + str(root))
    hasher = hashlib.sha256()
    for path in sorted(root.rglob('*')):
        if path.is_symlink() or not path.is_file() or path.name in SKIP_TREE_NAMES:
            continue
        relative = path.relative_to(root).as_posix()
        data = path.read_bytes()
        hasher.update(relative.encode() + b'\0' + len(data).to_bytes(8, 'big') + data)
    return hasher.hexdigest()


def git_revision(repo):
    revision = subprocess.check_output(['git', '-C', str(repo), 'rev-parse', 'HEAD'], text=True).strip()
    if not re.fullmatch('[0-9a-f]{40}', revision):
        raise ValueError('selected FogCast revision is not a full commit')
    return revision


def git_tree_identity(repo, revision, relative):
    listed = subprocess.check_output(
        ['git', '-C', str(repo), 'ls-tree', '-r', '--name-only', revision, '--', relative], text=True
    )
    names = [name for name in listed.splitlines() if name and Path(name).name not in SKIP_TREE_NAMES]
    if not names:
        raise ValueError('bootstrap assembly revision lacks ' + relative + ' source')
    prefix = relative.rstrip('/') + '/'
    hasher = hashlib.sha256()
    for name in sorted(names):
        data = subprocess.check_output(['git', '-C', str(repo), 'show', f'{revision}:{name}'], stderr=subprocess.DEVNULL)
        key = name[len(prefix):] if name.startswith(prefix) else name
        hasher.update(key.encode() + b'\0' + len(data).to_bytes(8, 'big') + data)
    return hasher.hexdigest()


def selected_appliance(fogcast):
    fogcast = Path(fogcast)
    nested = fogcast / 'appliance'
    if (nested / 'go.mod').is_file() and not nested.is_symlink():
        return fogcast, nested
    go_mod = fogcast / 'go.mod'
    if go_mod.is_file() and any(
            line.split(maxsplit=1)[1].strip() == APPLIANCE_MODULE
            for line in go_mod.read_text().splitlines()
            if line.startswith('module ') and len(line.split(maxsplit=1)) == 2):
        parent = fogcast.parent
        return parent, fogcast
    raise ValueError('selected FogCast appliance module is missing')


def go_floor(platform_dir):
    for line in (Path(platform_dir) / 'go.mod').read_text().splitlines():
        if line.startswith('go '):
            return line.split()[1]
    raise ValueError('platform go.mod lacks a Go version')


def write_workspace(directory, platform_dir, appliance_dir):
    directory = Path(directory)
    directory.mkdir(parents=True, exist_ok=True)
    platform_dir = Path(platform_dir).resolve()
    appliance_dir = Path(appliance_dir).resolve()
    workspace = directory / 'go.work'
    workspace.write_text(
        f'go {go_floor(platform_dir)}\n\n'
        'use (\n'
        f'\t{platform_dir.as_posix()}\n'
        f'\t{appliance_dir.as_posix()}\n'
        ')\n\n'
        f'replace {APPLIANCE_MODULE} v0.0.0 => {appliance_dir.as_posix()}\n'
    )
    return workspace


def platform_build_record(platform_sha256, appliance_module_sha256, fogcast_revision, go_version, go_sha256):
    return {
        'appliance_module': APPLIANCE_MODULE,
        'appliance_module_sha256': appliance_module_sha256,
        'build_env': dict(BUILD_ENV),
        'build_flags': list(BUILD_FLAGS),
        'fogcast_revision': fogcast_revision,
        'go_sha256': go_sha256,
        'go_version': go_version,
        'package': PACKAGE,
        'platform_module': PLATFORM_MODULE,
        'platform_sha256': platform_sha256,
    }


def _reject_paths(value):
    if isinstance(value, dict):
        for item in value.values():
            _reject_paths(item)
        return
    if isinstance(value, (list, tuple)):
        for item in value:
            _reject_paths(item)
        return
    if isinstance(value, str) and (value.startswith('/') or '/home/' in value or '\\' in value):
        raise ValueError('platform build record may not contain filesystem paths')


def validate_platform_build(record):
    if not isinstance(record, dict) or set(record) != RECORD_FIELDS:
        raise ValueError('platform build record has missing or unknown fields')
    if record['platform_module'] != PLATFORM_MODULE or record['appliance_module'] != APPLIANCE_MODULE:
        raise ValueError('platform build record module path is unsupported')
    if record['package'] != PACKAGE or record['build_flags'] != list(BUILD_FLAGS) or record['build_env'] != BUILD_ENV:
        raise ValueError('platform build flags or environment differ from the locked recipe')
    if not isinstance(record['go_version'], str) or not record['go_version'].startswith('go'):
        raise ValueError('invalid Go toolchain version')
    if not re.fullmatch('[0-9a-f]{40}', record['fogcast_revision']):
        raise ValueError('invalid platform FogCast revision')
    for name in ('appliance_module_sha256', 'go_sha256', 'platform_sha256'):
        if not isinstance(record[name], str) or not re.fullmatch('[0-9a-f]{64}', record[name]):
            raise ValueError('invalid platform digest: ' + name)
    _reject_paths(record)
    return record


def verify_identities(fes_root, fogcast, record):
    record = validate_platform_build(record)
    platform_dir = Path(fes_root) / 'platform'
    if not (platform_dir / 'go.mod').is_file():
        raise ValueError('FES platform module is missing')
    _, appliance_dir = selected_appliance(fogcast)
    if tree_identity(platform_dir) != record['platform_sha256']:
        raise ValueError('bootstrap platform source identity differs from selected FES platform')
    if tree_identity(appliance_dir) != record['appliance_module_sha256']:
        raise ValueError('bootstrap appliance-module identity differs from selected FogCast module')
    return record


def verify_retained(fes_root, fogcast, record, assembly_revision=None):
    record = verify_identities(fes_root, fogcast, record)
    fogcast_root, _ = selected_appliance(fogcast)
    if git_revision(fogcast_root) != record['fogcast_revision']:
        raise ValueError('bootstrap FogCast revision differs from selected checkout')
    if assembly_revision is not None:
        if git_tree_identity(fes_root, assembly_revision, 'platform') != record['platform_sha256']:
            raise ValueError('bootstrap assembly revision platform source differs')
    return record


def build_static_arm(root, fogcast, output, env):
    """Compile static ARM fes-boot from FES platform plus the selected appliance module.

    A temporary Go workspace is created only for this invocation. Absolute
    checkout paths stay in that workspace and are not returned in the record.
    """
    root = Path(root)
    fogcast_root, appliance_dir = selected_appliance(fogcast)
    platform_dir = root / 'platform'
    if not (platform_dir / 'go.mod').is_file() or platform_dir.is_symlink():
        raise ValueError('FES platform module is missing')
    output = Path(output)
    output.parent.mkdir(parents=True, exist_ok=True)
    revision = git_revision(fogcast_root)
    platform_sha = tree_identity(platform_dir)
    appliance_sha = tree_identity(appliance_dir)
    probe_env = dict(env)
    probe_env['GOPROXY'] = 'off'
    probe_env['GOWORK'] = 'off'
    with tempfile.TemporaryDirectory(prefix='fes-platform-work-') as temporary:
        workspace = write_workspace(temporary, platform_dir, appliance_dir)
        goroot = subprocess.check_output(['go', 'env', 'GOROOT'], cwd=platform_dir, env=probe_env, text=True).strip()
        go_bin = Path(goroot) / 'bin' / 'go'
        go_version = subprocess.check_output([str(go_bin), 'env', 'GOVERSION'], cwd=platform_dir, env=probe_env, text=True).strip()
        go_sha = _digest(go_bin)
        build_env = dict(env)
        build_env.update(BUILD_ENV)
        build_env['GOWORK'] = str(workspace)
        command = [str(go_bin), 'build', *BUILD_FLAGS, '-o', str(output), PACKAGE]
        subprocess.run(command, cwd=platform_dir, env=build_env, check=True)
    binary_sha = _appliance_module().validate_static_arm(output)
    return binary_sha, validate_platform_build(
        platform_build_record(platform_sha, appliance_sha, revision, go_version, go_sha)
    )
