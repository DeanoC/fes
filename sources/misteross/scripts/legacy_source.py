"""Truthful repository-relative paths for commit-bound (format 1) producers.

The wire record and exporter remain unchanged: format 1 always names paths
relative to the real Git root and requires that entire checkout to be clean.
Compiler working directories and unsealed oracle inputs remain module-relative.
"""
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from scripts.source_repository import canonical_repository
import re
import subprocess


def _git(root, *args):
    result = subprocess.run(['git', '-C', str(root), *args], capture_output=True, text=True)
    if result.returncode:
        raise ValueError('cannot inspect legacy source: ' + result.stderr.strip())
    return result.stdout.strip()


def _unlinked(path):
    if any(part.is_symlink() for part in (path, *path.parents)):
        raise ValueError('legacy source must not traverse symlinks')


@dataclass(frozen=True)
class SourceContext:
    module: Path
    root: Path
    prefix: str

    def qualify(self, relative):
        path = PurePosixPath(relative)
        if (not relative or path.is_absolute() or path.as_posix() != relative or
                any(part in ('', '.', '..') for part in relative.split('/')) or '\\' in relative):
            raise ValueError('legacy source path must be canonical and relative')
        return self.prefix + relative

    def require_clean(self, pinned_inputs):
        revision = _git(self.root, 'rev-parse', 'HEAD')
        if re.fullmatch(r'[0-9a-f]{40}', revision) is None:
            raise ValueError('source HEAD is not a full lowercase Git commit')
        if _git(self.root, 'status', '--porcelain', '--untracked-files=all'):
            raise ValueError('source checkout must be clean before build and export')
        origins = _git(self.root, 'remote', 'get-url', '--all', 'origin').splitlines()
        if len(origins) != 1:
            raise ValueError('source checkout must have exactly one origin URL')
        for relative in pinned_inputs:
            qualified = self.qualify(relative)
            path = self.root / qualified
            _unlinked(path)
            if not path.is_file():
                raise ValueError('pinned build input must be a regular file: ' + qualified)
            _git(self.root, 'ls-files', '--error-unmatch', '--', qualified)
        return canonical_repository(origins[0]), revision


def context(module):
    module = Path(module).absolute()
    _unlinked(module)
    module = module.resolve()
    root = Path(_git(module, 'rev-parse', '--show-toplevel')).resolve()
    if module == root:
        # A standalone checkout is valid; a nested Git checkout masquerading as
        # the imported module is not. Separate out/dev worktrees remain valid.
        if module.name == 'misteross' and module.parent.name == 'sources':
            result = subprocess.run(['git', '-C', str(module.parent), 'rev-parse', '--show-toplevel'],
                                    capture_output=True, text=True)
            if result.returncode == 0 and Path(result.stdout.strip()).resolve() == module.parent.parent:
                raise ValueError('legacy source must be a tracked module, not an embedded checkout')
        return SourceContext(module, root, '')
    if module != root / 'sources/misteross':
        raise ValueError('legacy source supports only the exact sources/misteross module')
    entry = _git(root, 'ls-tree', 'HEAD', '--', 'sources/misteross')
    if not entry.startswith('040000 tree '):
        raise ValueError('legacy source must be a tracked module tree')
    return SourceContext(module, root, 'sources/misteross/')


def require_clean_source(module, pinned_inputs):
    return context(module).require_clean(pinned_inputs)
