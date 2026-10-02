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
        # Buildroot cleans the copied tree without changing notice file modes.
        for path in self.destination.iterdir():
            self.assertEqual(path.stat().st_mode & 0o777, 0o444)
            path.unlink()
        self.destination.rmdir()

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

    def test_equivalent_clone_urls_produce_the_same_notice(self):
        path = self.package / 'manifest.toml'
        original = path.read_text()
        expected = notices.source_notice(self.package)
        for repository in (
                'https://github.com/DeanoC/fes',
                'https://github.com/DeanoC/fes.git/',
                'git@github.com:DeanoC/fes',
                'git@github.com:DeanoC/fes.git',
                'ssh://git@github.com/DeanoC/fes',
                'ssh://git@github.com/DeanoC/fes.git',
                'https://GITHUB.COM/deanoc/FES'):
            with self.subTest(repository=repository):
                path.write_text(original.replace(notices.REPOSITORY, repository))
                self.assertEqual(notices.source_notice(self.package), expected)
                notices.process('install', self.target, self.package)
                notices.process('verify', self.target, self.package)

    def test_wrong_source_rejected(self):
        path = self.package / 'manifest.toml'
        original = path.read_text()
        for repository in (
                'https://github.com/DeanoC/misteross.git',
                'https://github.com/another/fes.git',
                'https://example.com/DeanoC/fes.git',
                'https://github.com/DeanoC/fes.git/extra',
                'https://github.com/DeanoC/fes.git?other',
                'git@github.com:DeanoC/fes-other'):
            with self.subTest(repository=repository):
                path.write_text(original.replace(notices.REPOSITORY, repository))
                with self.assertRaises(ValueError):
                    notices.process('install', self.target, self.package)

    def test_changed_payload_rejected(self):
        (self.package / 'core.rbf').write_bytes(b'changed')
        with self.assertRaises(ValueError):
            notices.process('install', self.target, self.package)
