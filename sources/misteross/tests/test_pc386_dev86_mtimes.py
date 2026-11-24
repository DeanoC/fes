"""build_pc386_firmware.settle_mtimes keeps make from rerunning dev86's gperf step."""

import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

from scripts import build_pc386_firmware as firmware  # noqa: E402

# The shape of dev86's cpp/Makefile rule: a shipped header made from a .tok file
# by a tool the build host need not have.
MAKEFILE = 'token2.h: token2.tok\n\tmissing-gperf --output-file $@ $<\n'


@unittest.skipUnless(shutil.which('git') and shutil.which('make'), 'needs git and make')
class SettleMtimesTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.tree = Path(self.directory.name)
        (self.tree / 'Makefile').write_text(MAKEFILE)
        (self.tree / 'token2.tok').write_text('keywords\n')
        (self.tree / 'token2.h').write_text('/* generated */\n')
        env = dict(os.environ, GIT_AUTHOR_NAME='t', GIT_AUTHOR_EMAIL='t@example.invalid',
                   GIT_COMMITTER_NAME='t', GIT_COMMITTER_EMAIL='t@example.invalid',
                   GIT_COMMITTER_DATE='2020-01-01T00:00:00Z')
        for args in (('init', '-q'), ('add', '.'), ('commit', '-q', '-m', 'tree')):
            subprocess.run(['git', '-C', str(self.tree), *args], check=True, env=env)
        # The checkout order that breaks the build: the .tok file lands a tick later.
        os.utime(self.tree / 'token2.h', (1_700_000_000, 1_700_000_000))
        os.utime(self.tree / 'token2.tok', (1_700_000_001, 1_700_000_001))

    def tearDown(self):
        self.directory.cleanup()

    def make_is_up_to_date(self):
        result = subprocess.run(['make', '-q', '-C', str(self.tree), 'token2.h'],
                                capture_output=True)
        return result.returncode == 0

    def test_a_newer_tok_file_makes_make_want_the_generator(self):
        self.assertFalse(self.make_is_up_to_date())

    def test_settled_tree_needs_no_generator(self):
        firmware.settle_mtimes(self.tree)
        self.assertTrue(self.make_is_up_to_date())
        when = 1577836800  # the commit time, 2020-01-01T00:00:00Z
        for name in ('Makefile', 'token2.tok', 'token2.h'):
            self.assertEqual((self.tree / name).stat().st_mtime, when)


if __name__ == '__main__':
    unittest.main()
