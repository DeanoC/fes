"""Cold host prerequisite failures must never authorize media publication."""
import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import build


class MediaHostTests(unittest.TestCase):
    def setUp(self):
        self.assertTrue(hasattr(build, 'load_verified_host'), 'media requires a strict host receipt loader')
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.output = Path(self.temp.name)
        (self.output / 'fogcast').write_bytes(b'host cli')
        (self.output / 'fogcast-api').write_bytes(b'host api')
        build.write_receipt(self.output, 'host', 'cold-fingerprint', ['fogcast', 'fogcast-api'])

    def test_host_receipt_and_both_binary_hashes_are_bound(self):
        self.assertEqual(build.load_verified_host(self.output, 'cold-fingerprint'), {
            'host_receipt_sha256': build.digest(self.output / 'host.json'),
            'fogcast_sha256': build.digest(self.output / 'fogcast'),
            'fogcast_api_sha256': build.digest(self.output / 'fogcast-api'),
        })

    def test_missing_stale_and_malformed_host_receipts_rejected(self):
        path = self.output / 'host.json'
        original = path.read_bytes()
        values = [None, b'[]', b'null', b'garbage', b'{"inputs":"cold-fingerprint","files":[]}',
                  b'{"inputs":"stale","files":{}}', b'{"inputs":"cold-fingerprint","inputs":"cold-fingerprint","files":{}}']
        for value in values:
            with self.subTest(value=value):
                path.unlink(missing_ok=True)
                if value is not None:
                    path.write_bytes(value)
                with self.assertRaisesRegex(ValueError, 'host.*run make build'):
                    build.load_verified_host(self.output, 'cold-fingerprint')
        path.write_bytes(original)
        with self.assertRaises(ValueError):
            build.load_verified_host(self.output, 'new-fingerprint')

    def test_missing_changed_symlink_and_extra_host_files_rejected(self):
        for name in ('host.json', 'fogcast', 'fogcast-api'):
            path = self.output / name
            original = path.read_bytes()
            for mode in ('missing', 'changed', 'symlink'):
                with self.subTest(name=name, mode=mode):
                    path.unlink(missing_ok=True)
                    if mode == 'changed':
                        path.write_bytes(b'changed')
                    elif mode == 'symlink':
                        target = self.output / 'elsewhere'
                        target.write_bytes(original)
                        path.symlink_to(target)
                    with self.assertRaises(ValueError):
                        build.load_verified_host(self.output, 'cold-fingerprint')
                    path.unlink(missing_ok=True)
                    path.write_bytes(original)
        receipt = json.loads((self.output / 'host.json').read_text())
        for files in ({'fogcast': build.digest(self.output / 'fogcast')},
                      {**receipt['files'], '../escape': '0' * 64}):
            (self.output / 'host.json').write_text(json.dumps(dict(receipt, files=files)))
            with self.assertRaises(ValueError):
                build.load_verified_host(self.output, 'cold-fingerprint')
