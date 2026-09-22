"""Clean tracked FES module fixtures for producer provenance tests."""
from pathlib import Path
import shutil
import subprocess
import tempfile

EXECUTION = {"gpu_device": 0, "version": 1, "environment": {"LANG": "C"}}


def init_source(root):
    repo = root.parent.parent if root.parts[-2:] == ('sources', 'misteross') else root
    (repo / '.gitignore').write_text('build/\n__pycache__/\n')
    for args in [('init', '-q'), ('config', 'user.name', 'Fixture'),
                 ('config', 'user.email', 'fixture@example.invalid'),
                 ('remote', 'add', 'origin', 'https://example.invalid/fes'),
                 ('add', '.'), ('commit', '-qm', 'source')]:
        subprocess.run(['git', '-C', str(repo), *args], check=True, capture_output=True)


def clean_module(source):
    temporary = tempfile.TemporaryDirectory()
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
