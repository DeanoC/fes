"""RAM Tester notices bind to the sealed artifact, including cached source reuse."""
import hashlib
import importlib.util
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('ramtest_notices', ROOT / 'image/scripts/ramtest-notices.py')
notices = importlib.util.module_from_spec(spec)
spec.loader.exec_module(notices)


class RamtestNoticesTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.target = Path(self.temp.name)
        self.package = self.target / ('a' * 64)
        self.package.mkdir()
        payload = b'RAM Tester fixture'
        (self.package / 'core.rbf').write_bytes(payload)
        (self.package / 'manifest.toml').write_text(
            '[core]\nid = "fes.ramtest"\n[payload]\nsha256 = "' + hashlib.sha256(payload).hexdigest()
            + '"\n[build]\nrepository = "https://github.com/DeanoC/fes.git"\nrevision = "' + '1' * 40 + '"\n')
        self.destination = self.target / 'usr/share/mister-runtime/core-notices/fes.ramtest' / self.package.name
        self.addCleanup(self.make_writable)

    def make_writable(self):
        if self.destination.is_dir():
            self.destination.chmod(0o755)

    def test_install_binds_original_artifact_revision_and_preserves_package(self):
        notices.process('install', self.target, self.package)
        text = (self.destination / 'SOURCE.md').read_text()
        self.assertIn('Exact producing commit: ' + '1' * 40, text)
        self.assertIn('https://github.com/DeanoC/fes/archive/' + '1' * 40 + '.tar.gz', text)
        self.assertEqual((self.destination / 'COPYING').read_bytes(), notices.LICENSE.read_bytes())
        self.assertEqual({p.name for p in self.package.iterdir()}, {'manifest.toml', 'core.rbf'})
        notices.process('verify', self.target, self.package)
        notices.process('install', self.target, self.package)  # Repeated image finalization.

    def test_missing_and_changed_notices_fail_verification(self):
        notices.process('install', self.target, self.package)
        self.destination.chmod(0o755)
        (self.destination / 'SOURCE.md').unlink()
        with self.assertRaises(ValueError):
            notices.process('verify', self.target, self.package)
        notices.process('install', self.target, self.package)
        path = self.destination / 'COPYING'
        path.chmod(0o644)
        path.write_text('changed')
        with self.assertRaises(ValueError):
            notices.process('verify', self.target, self.package)

    def test_deselection_removes_old_notices(self):
        notices.process('install', self.target, self.package)
        with self.assertRaises(ValueError):
            notices.process('verify', self.target)
        notices.process('install', self.target)
        notices.process('verify', self.target)
        self.assertFalse(self.destination.exists())

    def test_changed_payload_or_wrong_source_rejected(self):
        (self.package / 'core.rbf').write_bytes(b'changed')
        with self.assertRaises(ValueError):
            notices.process('install', self.target, self.package)
        path = self.package / 'manifest.toml'
        path.write_text(path.read_text().replace('https://github.com/DeanoC/fes.git', 'https://github.com/DeanoC/misteross.git'))
        with self.assertRaises(ValueError):
            notices.process('install', self.target, self.package)
