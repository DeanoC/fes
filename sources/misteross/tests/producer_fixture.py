"""Clean tracked FES module fixtures for producer provenance tests."""
from pathlib import Path
import errno
import shutil
import subprocess
import tempfile
import time

EXECUTION = {"gpu_device": 0, "version": 1, "environment": {"LANG": "C"}}


def init_source(root):
    repo = root.parent.parent if root.parts[-2:] == ('sources', 'misteross') else root
    (repo / '.gitignore').write_text('build/\n__pycache__/\n')
    # Git 2.55 can fork `git maintenance` after commit. That rewrite of
    # .git/objects races fixture cleanup and leaves the directory non-empty.
    for args in [('init', '-q'), ('config', 'user.name', 'Fixture'),
                 ('config', 'user.email', 'fixture@example.invalid'),
                 ('config', 'maintenance.auto', 'false'),
                 ('config', 'gc.auto', '0'),
                 ('remote', 'add', 'origin', 'https://example.invalid/fes'),
                 ('add', '.'),
                 ('-c', 'maintenance.auto=false', '-c', 'gc.auto=0', 'commit', '-qm', 'source')]:
        subprocess.run(['git', '-C', str(repo), *args], check=True, capture_output=True)


class SourceTree:
    def __init__(self, path):
        self.name = path

    def cleanup(self):
        last = None
        for attempt in range(5):
            try:
                shutil.rmtree(self.name)
                return
            except OSError as error:
                last = error
                if error.errno != errno.ENOTEMPTY:
                    raise
                time.sleep(0.05 * (attempt + 1))
        raise last


def clean_module(source):
    temporary = SourceTree(tempfile.mkdtemp())
    root = Path(temporary.name) / 'sources/misteross'
    shutil.copytree(source, root, ignore=shutil.ignore_patterns('build', '__pycache__', '.git'))
    init_source(root)
    return temporary, root


class FakeInvocation:
    """Compiler identity seam; provenance and artifact checks remain real."""
    def __init__(self, *args):
        self.inputs = dict(EXECUTION)
        self.env = {'LANG': 'C'}
        self.gpu_device = 0
    def verify(self):
        pass
    def close(self):
        pass
