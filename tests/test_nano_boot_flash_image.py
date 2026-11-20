"""The flash packager can publish only committed producer bytes."""
import hashlib
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'platform/embedded/nano'))
from boot_flash_image import clean_head, committed_sources


class FlashProducerSourceTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.path = self.root/'producer.py'
        self.git('init', '-q')
        self.path.write_bytes(b'# pinned producer\n')
        self.git('add', self.path.name)
        self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid',
                 'commit', '-qm', 'fixture')

    def git(self, *args):
        subprocess.run(['git', *args], cwd=self.root, check=True, capture_output=True)

    def test_pins_committed_bytes_and_rejects_dirty_sources(self):
        head = clean_head(self.root)
        expected = hashlib.sha256(self.path.read_bytes()).hexdigest()
        self.assertEqual(committed_sources(self.root, head, (self.path,)), {self.path: expected})

        self.path.write_bytes(b'# edited producer\n')
        with self.assertRaisesRegex(ValueError, 'clean committed checkout'):
            clean_head(self.root)

    def test_git_status_alone_cannot_hide_changed_producer_bytes(self):
        head = clean_head(self.root)
        self.git('update-index', '--assume-unchanged', self.path.name)
        self.path.write_bytes(b'# hidden edit\n')
        self.assertEqual(clean_head(self.root), head)
        with self.assertRaisesRegex(ValueError, 'source differs from its commit'):
            committed_sources(self.root, head, (self.path,))


if __name__ == '__main__':
    unittest.main()
