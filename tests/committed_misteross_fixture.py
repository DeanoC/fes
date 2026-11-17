"""Build a committed temporary source snapshot for historical-source tests.

The production verifier must compare records with Git, so a record made from
working bytes must name a commit containing those same bytes. This fixture
never changes the real checkout or its refs.
"""
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def make_snapshot(destination):
    repo = Path(destination) / 'repo'
    repo.mkdir(parents=True)
    names = subprocess.check_output(['git', '-C', str(ROOT), 'ls-files', '-z', '--',
                                     'sources/misteross', 'config/core-recipes.toml']).split(b'\0')
    for raw in filter(None, names):
        relative = raw.decode('utf-8')
        source = ROOT / relative
        if not source.is_file() or source.is_symlink():
            raise ValueError('snapshot source must be a tracked regular file: ' + relative)
        target = repo / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, target)
    (repo / '.gitignore').write_text('__pycache__/\nbuild/\n')
    def git(*args):
        subprocess.run(['git', '-C', str(repo), '-c', 'core.hooksPath=/dev/null', *args],
                       check=True, stdout=subprocess.DEVNULL)
    git('init', '-q')
    git('config', 'user.name', 'Committed source fixture')
    git('config', 'user.email', 'fixture@example.invalid')
    # Some tracked source files match component ignore rules (for example
    # the Atari ST JSON). The snapshot contains only paths from git ls-files.
    git('add', '-f', '.')
    git('commit', '-qm', 'snapshot test source')
    return repo / 'sources/misteross'
