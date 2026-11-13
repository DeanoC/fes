"""Exercise the RAM tester's nested source guard with a changing root."""
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class NestedSourceGuardTests(unittest.TestCase):
    def test_ramtest_record_after_source_root_timestamp_changes(self):
        # Use a private indexed copy: changing a shared checkout's directory
        # timestamp would perturb other builds and unrelated tests.
        with tempfile.TemporaryDirectory() as temporary:
            repo = Path(temporary) / 'repository'
            root = repo / 'sources/misteross'
            root.mkdir(parents=True)
            tracked = subprocess.check_output([
                'git', 'ls-files', '-z', '--', 'scripts', 'cores/fes-ramtest',
                'cores/fes-pong', 'cores/fes-common', 'cores/*/toolchain.lock', 'boards/de10nano',
            ], cwd=ROOT).split(b'\0')
            for name in filter(None, tracked):
                relative = os.fsdecode(name)
                target = root / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(ROOT / relative, target)
            (repo / 'config').mkdir()
            shutil.copy2(ROOT.parents[1] / 'config/core-recipes.toml', repo / 'config')
            subprocess.run(['git', 'init', '-q', str(repo)], check=True)
            subprocess.run(['git', 'add', '-f', '.'], cwd=repo, check=True)
            (root / 'outside-source.txt').write_text('excluded input')
            child = r'''
import os
from pathlib import Path
import sys

root = Path(sys.argv[1]).resolve()
sys.path.insert(0, str(root))
os.chdir(root)
from scripts import build_fes_ramtest as ram
from scripts.compiler_read_audit import ReadAuditError, python_source_guard, _settle_import_root
from scripts.functional_execution import AuditedRoots

def must_reject(action):
    try:
        action()
    except ReadAuditError:
        return
    raise AssertionError('outside-closure source access was not rejected')

def deny_outside_access():
    must_reject(lambda: (root / 'outside-source.txt').read_text())
    must_reject(lambda: os.listdir(root))
    must_reject(lambda: os.scandir(root))

args = dict(root=root, repository='https://example.invalid/fes', revision='a' * 40,
            identities={'yosys': 'test', 'nextpnr-mistral': 'test'},
            memory_mhz=100, execution={'gpu_device': 'lease'})
_settle_import_root(root)
with python_source_guard(root, AuditedRoots(['scripts'])):
    before = ram.create_build_record(**args)
    deny_outside_access()
    previous_mtime = root.stat().st_mtime
    sentinel = root / '.timestamp-change'
    sentinel.mkdir()
    sentinel.rmdir()
    os.utime(root, (previous_mtime + 2, previous_mtime + 2))
    assert root.stat().st_mtime != previous_mtime
    after = ram.create_build_record(**args)
    assert before == after
    deny_outside_access()
    with python_source_guard(root, AuditedRoots(['scripts'])):
        deny_outside_access()
'''
            env = dict(os.environ, PYTHONDONTWRITEBYTECODE='1')
            for key in ('FES_SOURCE_CLOSURE_AUDIT', 'FES_SOURCE_CLOSURE_RECORD_ONLY',
                        'FES_SOURCE_CLOSURE_BROAD', 'FES_SOURCE_READ_RECORD', 'PYTHONPATH'):
                env.pop(key, None)
            result = subprocess.run([sys.executable, '-c', child, str(root)], cwd=root,
                                    env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == '__main__':
    unittest.main()
