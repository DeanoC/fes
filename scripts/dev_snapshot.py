"""Freeze local module edits in a disposable diagnostic checkout, without staging them."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile

from inputs import development_snapshot

MARKER = 'config/development-snapshot.json'


def clean_git_environment():
    # A caller's alternate index or repository must never redirect clone writes.
    return {key: value for key, value in os.environ.items()
            if key not in ('GIT_INDEX_FILE', 'GIT_DIR', 'GIT_WORK_TREE',
                           'GIT_COMMON_DIR', 'GIT_OBJECT_DIRECTORY',
                           'GIT_ALTERNATE_OBJECT_DIRECTORIES')}


def git(root, *args, env=None):
    if env is None:
        env = clean_git_environment()
    return subprocess.check_output(['git', '-C', str(root), *args], env=env).decode().strip()


def create(root):
    root = Path(root).resolve()
    if git(root, 'rev-parse', '--show-toplevel') != str(root):
        raise ValueError('run from the FES repository root')
    if (root / MARKER).is_symlink() or (root / MARKER).exists() or development_snapshot(root) is not None:
        raise ValueError('create snapshots from the feature worktree, not another snapshot')
    if any(line.startswith('160000 ') for line in git(root, 'ls-files', '--stage').splitlines()):
        raise ValueError('development snapshots require tracked modules, not submodules')
    base = git(root, 'rev-parse', 'HEAD')
    origin = git(root, 'remote', 'get-url', 'origin')
    parent = root / 'out/development-snapshots'
    if parent.resolve() != parent:
        raise ValueError('snapshot output must not traverse symlinks')
    subprocess.run(['git', '-C', str(root), 'check-ignore', '-q', '--', 'out/development-snapshots/probe'], check=True, env=clean_git_environment())
    parent.mkdir(parents=True, exist_ok=True)
    # A separate index captures staged and unstaged bytes together. The user's
    # index, branches and working files are never reset, added to or committed.
    with tempfile.TemporaryDirectory(prefix='.index-', dir=parent) as index_dir:
        env = dict(clean_git_environment(), GIT_INDEX_FILE=str(Path(index_dir) / 'index'))
        git(root, 'read-tree', base, env=env)
        git(root, 'add', '-A', '--', '.', env=env)
        tree = git(root, 'write-tree', env=env)
        if any(line.startswith('160000 ') for line in git(root, 'ls-tree', '-r', tree).splitlines()):
            raise ValueError('snapshot contains an embedded repository; use tracked module files')
    destination = Path(tempfile.mkdtemp(prefix='snapshot-', dir=parent))
    destination.rmdir()
    subprocess.run(['git', 'clone', '--local', '--no-hardlinks', '--no-checkout', str(root), str(destination)], check=True, env=clean_git_environment())
    git(destination, 'remote', 'set-url', 'origin', origin)
    git(destination, 'read-tree', '--reset', '-u', tree)
    marker = destination / MARKER
    if marker.parent.is_symlink():
        raise ValueError('snapshot config directory must not be a symlink')
    marker.parent.mkdir(parents=True, exist_ok=True)
    info = {'format': 1, 'classification': 'development-only',
            'base_revision': base, 'captured_tree': tree}
    marker.write_text(json.dumps(info, sort_keys=True, indent=2) + '\n')
    git(destination, 'add', '--', MARKER)
    snapshot_tree = git(destination, 'write-tree')
    identity = dict(clean_git_environment(), GIT_AUTHOR_NAME='FES development snapshot',
                    GIT_AUTHOR_EMAIL='development-snapshot@example.invalid',
                    GIT_COMMITTER_NAME='FES development snapshot',
                    GIT_COMMITTER_EMAIL='development-snapshot@example.invalid')
    revision = git(destination, 'commit-tree', snapshot_tree, '-p', base,
                   '-m', 'Local development snapshot; not eligible for release', env=identity)
    git(destination, 'update-ref', 'refs/heads/development-snapshot', revision)
    git(destination, 'symbolic-ref', 'HEAD', 'refs/heads/development-snapshot')
    if git(destination, 'status', '--porcelain', '--untracked-files=all'):
        raise ValueError('snapshot changed during creation; inspect ' + str(destination))
    return dict(info, directory=str(destination), revision=revision,
                commands=['make host', 'make dev'],
                limitation='Diagnostic only. Cold build, verify, media and release reject this selection.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[1])
    args = parser.parse_args()
    try:
        print(json.dumps(create(args.root), indent=2))
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        parser.exit(2, str(error) + '\n')


if __name__ == '__main__':
    main()
