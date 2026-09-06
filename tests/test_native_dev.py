"""Development caches must survive app edits but reject base changes."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import build
import native_dev


class NativeDevTest(unittest.TestCase):
    def test_base_key_tracks_recipes_not_application_revision(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            subprocess.run(['git', 'init', '-q', str(root)], check=True)
            files = {
                'buildroot/configs/native': 'base',
                'containers/target-image/Dockerfile': 'compiler',
                'build/target-image.sources.lock.toml': 'sources',
                'build/target-image-container-packages.sha256': 'packages',
                'scripts/build-target-image.sh': 'recipe',
                'Makefile': 'make',
                'cmd/mister-agent/main.go': 'agent',
                'build/native-runtime.inputs.lock.toml': '[mister_runtime]\ncommit="old"\nrepository="repo"\n',
            }
            for name, data in files.items():
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(data)
            subprocess.run(['git', '-C', str(root), 'add', '.'], check=True)
            before = native_dev.base_key(root)
            for name in ('cmd/mister-agent/main.go', 'build/native-runtime.inputs.lock.toml'):
                (root / name).write_text('[mister_runtime]\ncommit="new"\nrepository="repo"\n' if name.endswith('.toml') else 'changed')
            self.assertEqual(before, native_dev.base_key(root))
            for name in ('buildroot/configs/native', 'containers/target-image/Dockerfile',
                         'build/target-image.sources.lock.toml',
                         'build/target-image-container-packages.sha256',
                         'scripts/build-target-image.sh', 'Makefile'):
                with self.subTest(name=name):
                    original = (root / name).read_bytes()
                    (root / name).write_bytes(b'changed base')
                    self.assertNotEqual(before, native_dev.base_key(root))
                    (root / name).write_bytes(original)
            path = root / 'buildroot/new-patch'
            path.write_text('new')
            subprocess.run(['git', '-C', str(root), 'add', str(path)], check=True)
            self.assertNotEqual(before, native_dev.base_key(root))

    def test_seed_requires_matching_sources_and_intact_cold_evidence(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp)
            info = {'sources': {'FogCast': 'abc'}, 'profile': {'version': '1'}, 'go': 'go1'}
            (output / 'inputs.json').write_text(json.dumps(info))
            (output / 'linux.img').write_bytes(b'cold')
            sha = build.digest(output / 'linux.img')
            (output / 'reproducibility.txt').write_text(
                f'run_1_sha256={sha}\nrun_2_sha256={sha}\n')
            build.write_receipt(output, 'image', hashlib.sha256(json.dumps(info, sort_keys=True).encode()).hexdigest(),
                                ['linux.img', 'reproducibility.txt'])
            self.assertEqual(native_dev.seed_digest(output, info), sha)
            changed = dict(info, sources={'FogCast': 'different'})
            self.assertIsNone(native_dev.seed_digest(output, changed))
            (output / 'inputs.json').write_text(json.dumps(changed))
            self.assertIsNone(native_dev.seed_digest(output, changed),
                              'a host-only update must not relabel an old image seed')
            (output / 'inputs.json').write_text(json.dumps(info))
            (output / 'linux.img').write_bytes(b'changed')
            self.assertIsNone(native_dev.seed_digest(output, info))

    def test_unchanged_development_output_skips_build_tools(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            output = root / 'out/native-integration-dev/development'
            output.mkdir(parents=True)
            (output / 'linux.img').write_bytes(b'dev')
            build.write_receipt(output, 'development', 'same-inputs', ['linux.img'])
            with patch.object(native_dev, 'run', side_effect=AssertionError('unexpected rebuild')):
                native_dev.build_development(root, None, None, 'native-integration-dev',
                    {'bundle_interface': 'selection'}, {}, 'same-inputs', {}, [], None)

    def test_development_receipt_cannot_satisfy_release_reuse(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp)
            (output / 'linux.img').write_bytes(b'dev')
            build.write_receipt(output, 'development', 'same-inputs', ['linux.img'])
            self.assertFalse(build.reusable(output, 'image', 'same-inputs'))


if __name__ == '__main__':
    unittest.main()
