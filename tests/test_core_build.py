"""The selected profile controls the complete installed FPGA core set."""
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import build
from environment import build_environment


class CoreBuildTest(unittest.TestCase):
    def test_historical_default_and_explicit_set(self):
        self.assertEqual(build.selected_cores({'fpga_core': 'megadrive'}), ('megadrive',))
        self.assertEqual(build.selected_cores({'fpga_cores': ['megadrive', 'pong', 'snes', 'nes']}),
                         ('megadrive', 'pong', 'snes', 'nes'))
        for cores in ([], ['pong'], ['megadrive', 'pong'], ['megadrive', 'snes', 'pong'], ['megadrive', 'pong', 'pong'], ['megadrive', '../snes']):
            with self.subTest(cores=cores), self.assertRaises(ValueError):
                build.selected_cores({'fpga_cores': cores})

    def test_bundle_arguments_require_exact_selected_set(self):
        cores = ('megadrive', 'pong', 'snes', 'nes')
        bundles = {core: Path('/bundles') / core for core in cores}
        args = build.bundle_arguments(cores, bundles)
        self.assertIn('NATIVE_RUNTIME_SYSTEMS=megadrive pong snes nes', args)
        self.assertIn('PONG_RBF_BUNDLE=/bundles/pong', args)
        self.assertIn('SNES_RBF_BUNDLE=/bundles/snes', args)
        self.assertIn('NES_RBF_BUNDLE=/bundles/nes', args)
        for bad in ({'megadrive': bundles['megadrive']}, dict(bundles, zx81=Path('/x'))):
            with self.assertRaises(ValueError):
                build.bundle_arguments(cores, bad)

    def test_selection_overrides_do_not_leak_from_shell(self):
        with patch.dict('os.environ', {'PONG_RBF_BUNDLE': '/untrusted',
                                      'SNES_RBF_BUNDLE': '/wrong',
                                      'NES_RBF_BUNDLE': '/also-wrong',
                                      'NATIVE_RUNTIME_SYSTEMS': 'pong'}):
            env = build_environment()
        self.assertFalse('PONG_RBF_BUNDLE' in env)
        self.assertFalse('SNES_RBF_BUNDLE' in env)
        self.assertFalse('NES_RBF_BUNDLE' in env)
        self.assertFalse('NATIVE_RUNTIME_SYSTEMS' in env)

    def test_cached_bundle_is_checked_with_selected_source_and_recipe(self):
        with tempfile.TemporaryDirectory() as tmp:
            source = Path(tmp)
            (source / 'scripts').mkdir()
            (source / 'scripts/build_pong.py').write_text('recipe')
            bundle = source / 'build/bundles/pong/identity'
            bundle.mkdir(parents=True)
            (bundle / 'pong-rbf.toml').write_text('manifest')
            with patch.object(build, 'source_checkout', return_value=source), \
                 patch.object(build.core_bundle, 'load') as load, \
                 patch.object(build, 'run', side_effect=AssertionError('unexpected rebuild')):
                self.assertEqual(build.build_bundle({'misteross': 'a' * 40}, {}, system='pong'), bundle)
                load.assert_called_once_with(bundle, build.digest(source / 'scripts/build_pong.py'),
                                             system='pong', expected_revision='a' * 40)
