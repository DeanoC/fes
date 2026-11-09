"""Executable metadata is part of scan-tool admission, before a reservation."""
import hashlib
import importlib.util
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('nano_jtag_probe', ROOT/'platform/embedded/nano/jtag_probe.py')
probe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe)
RAW = b'host-only scan-tool fixture; never executed'
DIGEST = hashlib.sha256(RAW).hexdigest()


class ProbeTests(unittest.TestCase):
    def test_copying_bytes_alone_loses_required_execution_mode(self):
        with tempfile.TemporaryDirectory() as temp:
            source = Path(temp)/'qualified';source.write_bytes(RAW);source.chmod(0o775)
            copied = Path(temp)/'copied';shutil.copyfile(source, copied);copied.chmod(0o644)
            self.assertEqual(hashlib.sha256(copied.read_bytes()).hexdigest(), DIGEST)
            with self.assertRaises(ValueError):probe.checked(copied, DIGEST, 0o775)
            shutil.copymode(source, copied)
            self.assertEqual(probe.checked(copied, DIGEST, 0o775),
                             dict(path=str(copied), sha256=DIGEST, mode=0o775))

    def test_later_mode_byte_or_access_changes_reject(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp)/'probe';path.write_bytes(RAW);path.chmod(0o775)
            probe.checked(path, DIGEST, 0o775)
            path.chmod(0o755)
            with self.assertRaises(ValueError):probe.checked(path, DIGEST, 0o775)
            path.chmod(0o775);path.write_bytes(RAW+b' changed')
            with self.assertRaises(ValueError):probe.checked(path, DIGEST, 0o775)
            path.write_bytes(RAW)
            with patch.object(probe.os, 'access', return_value=False):
                with self.assertRaises(ValueError):probe.checked(path, DIGEST, 0o775)

    def test_symlink_directory_and_malformed_selection_reject(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp)/'probe';path.write_bytes(RAW);path.chmod(0o775)
            link = Path(temp)/'link';link.symlink_to(path)
            parent = Path(temp)/'parent';parent.symlink_to(temp, target_is_directory=True)
            for bad in (link, parent/'probe', Path(temp), Path('relative')):
                with self.subTest(path=bad),self.assertRaises(ValueError):probe.checked(bad, DIGEST, 0o775)
            for mode in (True, 0o644, 0o4775, 0, -1):
                with self.subTest(mode=mode),self.assertRaises(ValueError):probe.checked(path, DIGEST, mode)
            with self.assertRaises(ValueError):probe.checked(path, 'wrong digest', 0o775)

    def test_mode_change_while_hashing_cannot_return_a_valid_observation(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp)/'probe';path.write_bytes(RAW);path.chmod(0o775)
            original = hashlib.file_digest
            def changing(stream, algorithm):
                path.chmod(0o644)
                return original(stream, algorithm)
            with patch.object(probe.hashlib, 'file_digest', changing):
                with self.assertRaises(ValueError):probe.checked(path, DIGEST, 0o775)


if __name__ == '__main__':unittest.main()
