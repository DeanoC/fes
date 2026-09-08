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
    def test_verification_record_binds_qemu_log_digest(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp)
            (output / 'qemu-smoke.log').write_bytes(b'qemu passed\n')
            image_sha256 = hashlib.sha256(b'cold').hexdigest()
            record = build.verification_record(output, image_sha256, None)
            self.assertEqual(record['image_sha256'], image_sha256)
            self.assertEqual(record['qemu_log_sha256'],
                             hashlib.sha256(b'qemu passed\n').hexdigest())

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

    def test_four_core_build_receipts_cover_every_selected_artifact(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fogcast = root / 'FogCast'
            (fogcast / 'build').mkdir(parents=True)
            (fogcast / 'scripts').mkdir()
            (fogcast / 'scripts/build-target-image.sh').write_text('epoch=1234567890\n')
            (fogcast / 'build/target-image.sources.lock.toml').write_text(
                '[container]\nimage="base"\ndigest="sha256:abc"\nplatform="linux/amd64"\n')
            cores = ('megadrive', 'pong', 'snes', 'nes')
            bundles = {core: root / core for core in cores}
            built = fogcast / 'build/output/target-image/fes-development'
            built.mkdir(parents=True)
            for name in ['linux.img', 'manifest.tsv', 'library-report.tsv']:
                (built / name).write_text(name)
            for core, bundle in bundles.items():
                bundle.mkdir()
                (bundle / (core + '.rbf')).write_text(core)
                (bundle / (core + '-rbf.toml')).write_text('bundle-' + core)
                (built / (core + '.selection.toml')).write_text('selection-' + core)
            with patch.object(native_dev, 'base_key', return_value='base'), \
                 patch.object(native_dev, 'seed_base'), \
                 patch.object(native_dev, 'git', return_value='a' * 40), \
                 patch.object(native_dev.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0)), \
                 patch.object(native_dev, 'run') as run:
                native_dev.build_development(root, fogcast, root / 'runtime', 'native-integration-dev',
                    {'bundle_interface': 'selection', 'fpga_cores': list(cores)}, {}, 'candidate',
                    {'TARGET_IMAGE_CONTAINER_RUNTIME': 'docker'}, ['make'], bundles)
            for call in run.call_args_list:
                if str(call.args[0][0]).endswith('verify-target-image.sh'):
                    self.assertEqual(call.kwargs['env']['NATIVE_RUNTIME_SYSTEMS'], 'megadrive pong snes nes')
            output = root / 'out/native-integration-dev/development'
            self.assertTrue(build.reusable(output, 'development', 'candidate'))
            # A changed extra core, not only the original Mega Drive, invalidates reuse.
            (output / 'snes.rbf').write_text('changed')
            self.assertFalse(build.reusable(output, 'development', 'candidate'))

    def test_development_receipt_cannot_satisfy_release_reuse(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp)
            (output / 'linux.img').write_bytes(b'dev')
            build.write_receipt(output, 'development', 'same-inputs', ['linux.img'])
            self.assertFalse(build.reusable(output, 'image', 'same-inputs'))


if __name__ == '__main__':
    unittest.main()
