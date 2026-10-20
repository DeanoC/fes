import hashlib
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import image_artifacts as artifacts
import build


class ImageArtifactsTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / 'bin').mkdir()
        elf = b'\x7fELF\x01\x01' + b'\0' * 12 + b'\x28\0'
        for name in artifacts.BINARIES:
            (self.root / 'bin' / name).write_bytes(elf + name.encode())
        identity = 'a' * 64
        core = self.root / 'core-packages' / identity
        core.mkdir(parents=True)
        (core / 'manifest.toml').write_text('fixture')
        (core / 'core.rbf').write_bytes(b'firmware')
        selection = self.root / 'fes-menu.package-selection.toml'
        selection.write_text('fixture selection')
        self.manifest = {'format': 1, 'provenance': {'fogcast': '1' * 40, 'runtime': '2' * 40},
            'binaries': {n: artifacts.sha(self.root / 'bin' / n) for n in artifacts.BINARIES},
            'packages': [{'selection': {'core_id': 'fes.menu', 'package_id': identity},
                'selection_sha256': artifacts.sha(selection),
                'manifest_sha256': artifacts.sha(core / 'manifest.toml'),
                'core_rbf_sha256': artifacts.sha(core / 'core.rbf')}]}
        self.write_manifest()

    def write_manifest(self):
        (self.root / 'artifacts.json').write_text(json.dumps(self.manifest))

    def test_stage_preserves_original_provenance_and_binary(self):
        destination = self.root / 'snapshot'
        destination.mkdir()
        manifest, packages = artifacts.stage(self.root, destination)
        self.assertEqual(manifest, self.manifest)
        self.assertEqual(len(packages), 1)
        for name in artifacts.BINARIES:
            self.assertEqual(artifacts.sha(destination / 'bin' / (name + '-linux-armv7')),
                             self.manifest['binaries'][name])
        self.assertEqual((destination / 'bin/kit-software.revision').read_text().strip(), '1' * 40)

    def test_missing_binary_never_calls_a_producer(self):
        (self.root / 'bin/fogcast-tenfoot').unlink()
        with patch('subprocess.run', side_effect=AssertionError('must not compile')):
            with self.assertRaisesRegex(ValueError, 'artifact changed: fogcast-tenfoot'):
                artifacts.load(self.root)

    def test_changed_menu_is_rejected(self):
        (self.root / 'core-packages' / ('a' * 64) / 'core.rbf').write_bytes(b'changed')
        with self.assertRaisesRegex(ValueError, 'package changed'):
            artifacts.load(self.root)

    def test_symlink_binary_is_rejected(self):
        binary = self.root / 'bin/mister-runtime'
        binary.rename(self.root / 'runtime')
        binary.symlink_to(self.root / 'runtime')
        with self.assertRaisesRegex(ValueError, 'artifact changed'):
            artifacts.load(self.root)

    def test_wrong_architecture_rejected_even_with_matching_hash(self):
        binary = self.root / 'bin/mister-agent'
        binary.write_bytes(b'not an ARM executable')
        self.manifest['binaries']['mister-agent'] = artifacts.sha(binary)
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, 'ARM ELF'):
            artifacts.load(self.root)

    def test_missing_input_stops_before_tools_or_sources(self):
        with patch.object(sys, 'argv', ['build.py', 'image']), \
             patch.object(build, 'validate', side_effect=AssertionError('must stop first')):
            with self.assertRaisesRegex(ValueError, 'requires --artifacts'):
                build.main()

    def test_artifact_identity_survives_receipt_roundtrip(self):
        manifest, packages = artifacts.load(self.root)
        info = {'bootstrap_artifacts': manifest}
        fp = hashlib.sha256(json.dumps(info, sort_keys=True).encode()).hexdigest()
        result, enriched = build.image_fingerprint(fp, info, packages)
        self.assertEqual(build.recorded_image_fingerprint(enriched), result)


if __name__ == '__main__':
    unittest.main()
