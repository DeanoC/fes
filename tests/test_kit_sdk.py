import io
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import kit_software as kit


class DE25SdkWorkflowTests(unittest.TestCase):
    def setUp(self):
        import kit_sdk
        self.sdk = kit_sdk
        self.temp = tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.image = self.root / 'image'; self.image.mkdir()
        (self.image / 'aarch64-sdk.tar.gz').write_bytes(b'unqualified SDK cache fixture')
        self.selected = {'key': 'managed-' + 'a'*64, 'archive_sha256': kit.sha(self.image / 'aarch64-sdk.tar.gz'),
                         'image_receipt_sha256': 'b'*64, 'producer_revision': 'c'*40,
                         'builder_image': 'sha256:' + 'd'*64, 'profile': 'managed-base', 'archive_root': 'sdk',
                         'base_rootfs_sha256': 'e'*64, 'kernel_image_sha256': 'f'*64}
        self.cache = kit.load(ROOT / 'image/scripts/toolchain_cache.py', 'sdk_workflow_cache_fixture')

    def test_cache_reuse_corruption_repair_and_raw_archive_identity(self):
        with patch.dict(os.environ, {'FES_TARGET_IMAGE_SHARED_CACHE': str(self.root / 'cache')}), \
                patch.object(self.sdk, 'de25_sdk', return_value=self.selected) as authenticate, \
                patch.object(self.sdk, 'load', return_value=self.cache):
            selected = self.sdk.ensure_de25(ROOT, self.image)
            directory = Path(selected['directory'])
            self.assertEqual((directory / 'host.tar').read_bytes(), (self.image / 'aarch64-sdk.tar.gz').read_bytes())
            self.assertEqual(self.cache.validated_sha(directory, selected['key']), self.selected['archive_sha256'])
            with patch.object(self.cache, 'import_archive', wraps=self.cache.import_archive) as imported:
                again = self.sdk.ensure_de25(ROOT, self.image)
                self.assertEqual(again['key'], selected['key']); imported.assert_not_called()
                (directory / 'host.tar').write_bytes(b'corrupt cached archive')
                repaired = self.sdk.ensure_de25(ROOT, self.image)
                imported.assert_called_once()
                self.assertEqual(repaired['archive_sha256'], self.selected['archive_sha256'])
            self.assertEqual(authenticate.call_count, 3)

    def test_same_archive_reuses_cache_across_authenticated_producers_and_keeps_provenance(self):
        with patch.dict(os.environ, {'FES_TARGET_IMAGE_SHARED_CACHE': str(self.root / 'cache')}), \
                patch.object(self.sdk, 'de25_sdk', return_value=self.selected) as authenticate, \
                patch.object(self.sdk, 'load', return_value=self.cache):
            first = self.sdk.ensure_de25(ROOT, self.image)
            changed = dict(self.selected, producer_revision='d'*40, image_receipt_sha256='e'*64, profile='managed-factory')
            authenticate.return_value = changed
            second = self.sdk.ensure_de25(ROOT, self.image)
            self.assertEqual(first['key'], second['key'])
            self.assertEqual(second['source'], changed)
            self.assertNotEqual(first['key'], self.cache.key(ROOT / 'image'))
            (self.image / 'aarch64-sdk.tar.gz').write_bytes(b'a different authenticated SDK fixture')
            authenticate.return_value = dict(changed, archive_sha256=kit.sha(self.image / 'aarch64-sdk.tar.gz'))
            third = self.sdk.ensure_de25(ROOT, self.image)
            self.assertNotEqual(second['key'], third['key'])

    def test_bad_source_never_selects_a_cache_or_falls_back(self):
        with patch.object(self.sdk, 'de25_sdk', side_effect=ValueError('source not authenticated')), \
                patch.object(self.sdk, 'load') as load:
            with self.assertRaisesRegex(ValueError, 'not authenticated'):
                self.sdk.ensure_de25(ROOT, self.image)
            load.assert_not_called()

    def test_cli_requires_independent_de25_and_image(self):
        for args in ([], ['--de25-image', str(self.image)], ['--board', 'de25-nano']):
            with self.subTest(args=args), patch.object(sys, 'argv', ['kit_sdk.py', *args]), \
                    patch.object(self.sdk, 'ensure_de25') as ensure, patch('sys.stderr', io.StringIO()):
                with self.assertRaises(SystemExit) as result:
                    self.sdk.main()
                self.assertEqual(result.exception.code, 1); ensure.assert_not_called()

    def test_make_routes_de25_without_starting_the_de10_builder_and_preserves_overrides(self):
        command = ['make', '--dry-run', 'kit-sdk', 'BOARD=de25-nano', 'DE25_IMAGE=/verified-image']
        result = subprocess.run(command, cwd=ROOT, capture_output=True, text=True, check=True)
        self.assertIn('scripts/kit_sdk.py --board "de25-nano" --de25-image "/verified-image"', result.stdout)
        self.assertNotIn('--ensure-kit-sdk', result.stdout)
        native = subprocess.run(['make', '--dry-run', 'kit-sdk'], cwd=ROOT, capture_output=True, text=True, check=True)
        self.assertIn('build-target-image.sh --ensure-kit-sdk', native.stdout)
        for args in (['BOARD=unknown'], ['BOARD=de25-nano'], ['DE25_IMAGE=/wrong-board']):
            rejected = subprocess.run(['make', '--dry-run', 'kit-sdk', *args], cwd=ROOT, capture_output=True, text=True)
            self.assertNotEqual(rejected.returncode, 0)
            self.assertNotIn('--ensure-kit-sdk', rejected.stdout)
        common = ['make', '--dry-run', 'kit-software', 'BOARD=de25-nano', 'DE25_IMAGE=/verified-image',
                  'KIT_SOFTWARE_BASE=/base', 'KIT_SOFTWARE_BASE_SHA256=' + 'a'*64, 'KIT_SOFTWARE_VERSION=fixture']
        software = subprocess.run(common, cwd=ROOT, capture_output=True, text=True, check=True)
        self.assertIn('--board "de25-nano" --de25-image "/verified-image"', software.stdout)
        override = subprocess.run([*common, 'KIT_SOFTWARE_BOARD=de10-nano', 'KIT_SOFTWARE_DE25_IMAGE='],
                                  cwd=ROOT, capture_output=True, text=True, check=True)
        self.assertIn('--board "de10-nano"', override.stdout)
        self.assertNotIn('--de25-image', override.stdout)


if __name__ == '__main__':
    unittest.main()
