"""Canonical paths and provenance for the tracked FES FPGA module.

Functional builds use the module context; explicit oracle records additionally
require the whole repository clean and qualify paths against its Git root.
"""
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from scripts.source_repository import canonical_repository
import re
import subprocess


def _git(root, *args):
    result = subprocess.run(['git', '-C', str(root), *args], capture_output=True, text=True)
    if result.returncode:
        raise ValueError('cannot inspect source: ' + result.stderr.strip())
    return result.stdout.strip()


def _unlinked(path):
    if any(part.is_symlink() for part in (path, *path.parents)):
        raise ValueError('source must not traverse symlinks')


@dataclass(frozen=True)
class SourceContext:
    module: Path
    root: Path
    prefix: str

    def qualify(self, relative):
        path = PurePosixPath(relative)
        if (not relative or path.is_absolute() or path.as_posix() != relative or
                any(part in ('', '.', '..') for part in relative.split('/')) or '\\' in relative):
            raise ValueError('source path must be canonical and relative')
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
    if module != root / 'sources/misteross':
        raise ValueError('source supports only the exact sources/misteross module')
    entry = _git(root, 'ls-tree', 'HEAD', '--', 'sources/misteross')
    if not entry.startswith('040000 tree '):
        raise ValueError('source must be a tracked module tree')
    return SourceContext(module, root, 'sources/misteross/')


def require_clean_source(module, pinned_inputs):
    return context(module).require_clean(pinned_inputs)
