"""The selected profile controls the complete installed FPGA core set."""
from contextlib import contextmanager
from pathlib import Path
import hashlib
import sys
import tempfile
import tomllib
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

    def test_only_integration_profile_selects_the_closed_fes_pong_recipe(self):
        selection = {'fpga_packages': [{'core_id': 'fes.pong'}]}
        self.assertEqual(build.selected_packages(selection, 'native-integration-dev'),
                         ('fes.pong',))
        self.assertEqual(build.selected_packages({}, 'native-dev'), ())
        self.assertEqual(build.selected_packages({'fpga_packages': []}, 'native-integration-dev'), ())
        for profile_name, profile in (
            ('native-dev', selection),
            ('native-source-dev', selection),
            ('native-integration-dev', {'fpga_packages': [{'core_id': 'pong'}]}),
            ('native-integration-dev', {'fpga_packages': [{'core_id': 'fes.pong'}, {'core_id': 'fes.pong'}]}),
            ('native-integration-dev', {'fpga_packages': {'core_id': 'fes.pong'}}),
        ):
            with self.subTest(profile=profile_name, value=profile), self.assertRaises(ValueError):
                build.selected_packages(profile, profile_name)
        repository_profile = tomllib.loads((Path(__file__).resolve().parents[1] /
                                             'profiles/native-integration-dev.toml').read_text())
        self.assertEqual(build.selected_packages(repository_profile, 'native-integration-dev'),
                         ('fes.pong',))

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
                                      'NATIVE_RUNTIME_SYSTEMS': 'pong',
                                      'FES_PONG_PACKAGE_DIR': '/untrusted-package',
                                      'FES_PONG_PACKAGE_SELECTION': '/untrusted-selection',
                                      'FES_TOOLCHAIN_CACHE_ROOT': '/ambient-toolchains'}):
            env = build_environment()
        self.assertFalse('PONG_RBF_BUNDLE' in env)
        self.assertFalse('SNES_RBF_BUNDLE' in env)
        self.assertFalse('NES_RBF_BUNDLE' in env)
        self.assertFalse('NATIVE_RUNTIME_SYSTEMS' in env)
        self.assertFalse('FES_PONG_PACKAGE_DIR' in env)
        self.assertFalse('FES_PONG_PACKAGE_SELECTION' in env)
        self.assertFalse('FES_TOOLCHAIN_CACHE_ROOT' in env)

    def test_package_arguments_and_image_fingerprint_bind_exact_selection_bytes(self):
        package = {
            'directory': Path('/packages/identity'),
            'selection_path': Path('/records/fes-pong.package-selection.toml'),
            'inputs': {
                'selection': {'format': 2, 'kind': 'core-package', 'core_id': 'fes.pong',
                              'package_id': 'a' * 64, 'payload_sha256': 'b' * 64,
                              'misteross_revision': 'c' * 40,
                              'mister_packages_revision': 'd' * 40,
                              'install_path': '/usr/share/mister-runtime/core-packages/' + 'a' * 64},
                'selection_sha256': 'e' * 64,
                'manifest_sha256': 'f' * 64,
                'core_rbf_sha256': 'b' * 64,
            },
        }
        self.assertEqual(build.package_arguments(package), [
            'FES_PONG_PACKAGE_DIR=/packages/identity',
            'FES_PONG_PACKAGE_SELECTION=/records/fes-pong.package-selection.toml'])
        first, first_info = build.image_fingerprint('base', {'sources': {}}, package)
        changed = dict(package, inputs=dict(package['inputs'], selection_sha256='0' * 64))
        second, _ = build.image_fingerprint('base', {'sources': {}}, changed)
        self.assertNotEqual(first, second)
        self.assertEqual(first_info['fpga_packages'], [package['inputs']])
        self.assertEqual(first_info['image_base_fingerprint'], 'base')
        self.assertEqual(first_info['image_fingerprint'], first)

    def test_host_build_fingerprint_ignores_package_recipe_selection(self):
        revisions = {'FogCast': 'a' * 40}
        plain, plain_info = build.build_fingerprint(revisions, {'version': '1'}, 'go test')
        selected, selected_info = build.build_fingerprint(revisions, {
            'version': '1', 'fpga_packages': [{'core_id': 'fes.pong'}]}, 'go test')
        self.assertEqual(selected, plain)
        self.assertEqual(selected_info, plain_info)

    def test_published_package_outputs_are_closed_and_byte_exact(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'source'
            output = root / 'output'
            source.mkdir()
            output.mkdir()
            identity = 'a' * 64
            (source / 'manifest.toml').write_bytes(b'manifest')
            (source / 'core.rbf').write_bytes(b'payload')
            selection = root / 'selected.toml'
            selection.write_bytes(b'format = 2\n')
            built = root / 'built.toml'
            built.write_bytes(selection.read_bytes())
            stale = output / 'core-packages/stale'
            stale.mkdir(parents=True)
            (stale / 'old').write_bytes(b'old')
            package = {
                'directory': source,
                'selection_path': selection,
                'inputs': {
                    'selection': {'package_id': identity},
                    'selection_sha256': hashlib.sha256(selection.read_bytes()).hexdigest(),
                    'manifest_sha256': hashlib.sha256(b'manifest').hexdigest(),
                    'core_rbf_sha256': hashlib.sha256(b'payload').hexdigest(),
                },
            }
            names = build.publish_package_outputs(package, built, output)
            self.assertEqual(names, [
                'fes-pong.package-selection.toml',
                f'core-packages/{identity}/manifest.toml',
                f'core-packages/{identity}/core.rbf'])
            build.verify_package_outputs(output, package)
            self.assertEqual([path.name for path in (output / 'core-packages').iterdir()], [identity])
            payload = output / 'core-packages' / identity / 'core.rbf'
            payload.chmod(0o644)
            payload.write_bytes(b'changed')
            with self.assertRaisesRegex(ValueError, 'changed|differs'):
                build.verify_package_outputs(output, package)

            payload.write_bytes(b'payload')
            payload.chmod(0o444)
            package_directory = output / 'core-packages' / identity
            for name, create in (
                ('extra', lambda path: path.write_bytes(b'extra')),
                ('.hidden', lambda path: path.write_bytes(b'hidden')),
                ('link', lambda path: path.symlink_to(source / 'manifest.toml')),
                ('directory', lambda path: path.mkdir()),
            ):
                with self.subTest(stray=name):
                    package_directory.chmod(0o755)
                    stray = package_directory / name
                    create(stray)
                    package_directory.chmod(0o555)
                    with self.assertRaisesRegex(ValueError, 'changed|differs'):
                        build.verify_package_outputs(output, package)
                    package_directory.chmod(0o755)
                    if stray.is_symlink() or stray.is_file():
                        stray.unlink()
                    else:
                        stray.rmdir()
                    package_directory.chmod(0o555)

    def test_package_free_publication_removes_closed_previous_selection(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp)
            selection = output / 'fes-pong.package-selection.toml'
            selection.write_bytes(b'selection')
            selection.chmod(0o444)
            package = output / 'core-packages' / ('a' * 64)
            package.mkdir(parents=True)
            (package / 'manifest.toml').write_bytes(b'manifest')
            (package / 'core.rbf').write_bytes(b'payload')
            package.chmod(0o555)
            (output / 'core-packages').chmod(0o555)

            self.assertEqual(build.publish_package_state(None, None, output), [])
            self.assertFalse(selection.exists())
            self.assertFalse((output / 'core-packages').exists())
            self.assertEqual(build.verify_package_outputs(output, None), [])

    def test_package_free_publication_rejects_symlink_destinations_without_following(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            output = root / 'output'
            ambient = root / 'ambient'
            output.mkdir()
            ambient.mkdir()
            marker = ambient / 'keep'
            marker.write_bytes(b'unchanged')
            (output / 'core-packages').symlink_to(ambient, target_is_directory=True)
            with self.assertRaisesRegex(ValueError, 'non-symlink'):
                build.publish_package_state(None, None, output)
            self.assertEqual(marker.read_bytes(), b'unchanged')

    def test_package_publication_does_not_follow_existing_output_symlink(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            output = root / 'output'
            output.mkdir()
            ambient = root / 'ambient'
            ambient.mkdir()
            marker = ambient / 'keep'
            marker.write_bytes(b'unchanged')
            (output / 'core-packages').symlink_to(ambient, target_is_directory=True)
            source = root / 'source'
            source.mkdir()
            (source / 'manifest.toml').write_bytes(b'manifest')
            (source / 'core.rbf').write_bytes(b'payload')
            selection = root / 'selection.toml'
            selection.write_bytes(b'format = 2\n')
            package = {'directory': source, 'selection_path': selection, 'inputs': {
                'selection': {'package_id': 'a' * 64},
                'selection_sha256': build.digest(selection),
                'manifest_sha256': build.digest(source / 'manifest.toml'),
                'core_rbf_sha256': build.digest(source / 'core.rbf')}}
            with self.assertRaisesRegex(ValueError, 'non-symlink'):
                build.publish_package_outputs(package, selection, output)
            self.assertEqual(marker.read_bytes(), b'unchanged')

    def test_generic_fpga_bundle_subprocess_does_not_opt_into_shared_toolchain_cache(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'source'
            cache = root / 'cache'
            (source / 'scripts').mkdir(parents=True)
            (source / 'scripts/rebuild_core.py').write_text('recipe')
            produced = source / 'build/bundles/megadrive/identity'
            captured = []

            def fake_run(args, **kwargs):
                captured.append(kwargs.get('env'))
                produced.mkdir(parents=True, exist_ok=True)
                (produced / 'megadrive.rbf').write_bytes(b'rbf-bytes')
                (produced / 'megadrive-rbf.toml').write_bytes(b'manifest')

            with patch.dict('os.environ', {'FES_TOOLCHAIN_CACHE_ROOT': '/ambient-toolchains'}), \
                 patch.object(build, 'FPGA_BUNDLE_CACHE', cache), \
                 patch.object(build, 'source_checkout', return_value=source), \
                 patch.object(build.core_bundle, 'load', return_value={'sha256': 'a' * 64}), \
                 patch.object(build, 'run', side_effect=fake_run):
                env = build_environment()
                env['QUARTUS_ROOTDIR'] = '/quartus'
                build.build_bundle({'misteross': 'b' * 40}, env, system='megadrive')
            self.assertTrue(captured)
            for passed in captured:
                self.assertIsNotNone(passed)
                self.assertNotIn('FES_TOOLCHAIN_CACHE_ROOT', passed)

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

    def _write_format1_megadrive(self, directory, recipe_sha256, payload=b'rbf'):
        directory.mkdir(parents=True, exist_ok=True)
        rbf = directory / 'megadrive.rbf'
        manifest = directory / 'megadrive-rbf.toml'
        rbf.write_bytes(payload)
        manifest.write_text(
            'format = 1\n'
            'abi = "mister"\n'
            'system = "megadrive"\n'
            'artifact = "megadrive.rbf"\n'
            f'sha256 = "{hashlib.sha256(payload).hexdigest()}"\n'
            f'size = {len(payload)}\n'
            f'repository = "{build.core_bundle.REPOSITORY}"\n'
            f'revision = "{build.core_bundle.REVISION}"\n'
            'recipe = "scripts/rebuild_core.py"\n'
            f'recipe_sha256 = "{recipe_sha256}"\n'
            f'toolchain = "{build.core_bundle.TOOLCHAIN}"\n')
        rbf.chmod(0o444)
        manifest.chmod(0o444)
        directory.chmod(0o555)
        return directory

    def _sealed_bundle(self, directory, system, extra=()):
        directory.mkdir(parents=True, exist_ok=True)
        rbf = directory / f'{system}.rbf'
        manifest = directory / f'{system}-rbf.toml'
        rbf.write_bytes(b'rbf')
        manifest.write_bytes(b'manifest')
        rbf.chmod(0o444)
        manifest.chmod(0o444)
        for name in extra:
            path = directory / name
            path.write_bytes(b'unexpected')
            path.chmod(0o444)
        directory.chmod(0o555)
        return directory

    def test_stable_bundle_requires_the_closed_sealed_two_file_set(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp) / ('a' * 64)
            self._sealed_bundle(directory, 'megadrive')
            build._require_sealed_bundle(directory, 'megadrive')
            extra = directory / 'extra'
            directory.chmod(0o755)
            extra.write_bytes(b'unexpected')
            extra.chmod(0o444)
            directory.chmod(0o555)
            with self.assertRaisesRegex(ValueError, 'closed|unexpected'):
                build._require_sealed_bundle(directory, 'megadrive')

    def test_stable_bundle_rejects_a_writable_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp) / ('b' * 64)
            self._sealed_bundle(directory, 'megadrive')
            directory.chmod(0o755)
            (directory / 'megadrive.rbf').chmod(0o644)
            directory.chmod(0o555)
            with self.assertRaisesRegex(ValueError, 'write|sealed|writable'):
                build._require_sealed_bundle(directory, 'megadrive')

    def test_stable_bundle_rejects_a_missing_manifest(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp) / ('c' * 64)
            directory.mkdir()
            rbf = directory / 'megadrive.rbf'
            rbf.write_bytes(b'rbf')
            rbf.chmod(0o444)
            directory.chmod(0o555)
            with self.assertRaisesRegex(ValueError, 'manifest|closed'):
                build._require_sealed_bundle(directory, 'megadrive')

    def test_stable_bundle_directories_skip_symlink_roots(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            real = root / 'real'
            digest = self._sealed_bundle(real / 'megadrive' / ('d' * 64), 'megadrive')
            link = root / 'link'
            link.symlink_to(real, target_is_directory=True)
            self.assertTrue(link.is_symlink())
            self.assertEqual(build._bundle_directories(link, 'megadrive'), ())
            self.assertEqual(build._bundle_directories(root / 'missing', 'megadrive'), ())
            self.assertEqual(build._bundle_directories(real, 'megadrive'), (digest,))
            linked_bundle = root / 'bundle-link'
            linked_bundle.symlink_to(digest, target_is_directory=True)
            self.assertTrue(linked_bundle.is_symlink())
            with self.assertRaisesRegex(ValueError, 'symlink'):
                build._require_sealed_bundle(linked_bundle, 'megadrive')

    def test_stable_bundle_directories_skip_hidden_staging_names(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            digest = self._sealed_bundle(root / 'megadrive' / ('e' * 64), 'megadrive')
            self._sealed_bundle(root / 'megadrive' / '.new-staging', 'megadrive')
            self.assertEqual(build._bundle_directories(root, 'megadrive'), (digest,))

    def test_upstream_bundle_from_another_misteross_revision_is_reused(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'source'
            cache = root / 'cache' / 'megadrive' / ('a' * 64)
            (source / 'scripts').mkdir(parents=True)
            (source / 'scripts/rebuild_core.py').write_text('recipe')
            self._sealed_bundle(cache, 'megadrive')
            with patch.object(build, 'FPGA_BUNDLE_CACHE', root / 'cache'), \
                 patch.object(build, 'source_checkout', return_value=source), \
                 patch.object(build.core_bundle, 'load', return_value={'sha256': 'a' * 64}), \
                 patch.object(build, 'run', side_effect=AssertionError('unexpected FPGA build')):
                self.assertEqual(
                    build.build_bundle({'misteross': 'b' * 40}, {}, system='megadrive'), cache)

    def test_malformed_stable_entry_is_a_cache_miss(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'source'
            cache = root / 'cache' / 'megadrive' / ('a' * 64)
            (source / 'scripts').mkdir(parents=True)
            (source / 'scripts/rebuild_core.py').write_text('recipe')
            self._sealed_bundle(cache, 'megadrive', extra=('extra',))
            with patch.object(build, 'FPGA_BUNDLE_CACHE', root / 'cache'), \
                 patch.object(build, 'source_checkout', return_value=source), \
                 patch.object(build.core_bundle, 'load', return_value={'sha256': 'a' * 64}), \
                 patch.object(build, 'run', side_effect=AssertionError('unexpected FPGA build')):
                with self.assertRaisesRegex(ValueError, 'QUARTUS_ROOTDIR'):
                    build.build_bundle({'misteross': 'b' * 40}, {}, system='megadrive')

    def test_distinct_valid_stable_artifacts_are_ambiguous(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'source'
            first = root / 'cache' / 'megadrive' / ('a' * 64)
            second = root / 'cache' / 'megadrive' / ('b' * 64)
            (source / 'scripts').mkdir(parents=True)
            (source / 'scripts/rebuild_core.py').write_text('recipe')
            self._sealed_bundle(first, 'megadrive')
            self._sealed_bundle(second, 'megadrive')
            with patch.object(build, 'FPGA_BUNDLE_CACHE', root / 'cache'), \
                 patch.object(build, 'source_checkout', return_value=source), \
                 patch.object(build.core_bundle, 'load',
                              side_effect=lambda directory, *args, **kwargs: {
                                  'sha256': Path(directory).name}), \
                 patch.object(build, 'run', side_effect=AssertionError('unexpected FPGA build')):
                with self.assertRaises(ValueError) as raised:
                    build.build_bundle({'misteross': 'c' * 40}, {}, system='megadrive')
            message = str(raised.exception)
            self.assertIn('ambiguous', message)
            self.assertIn(str(first), message)
            self.assertIn(str(second), message)

    def test_selected_checkout_and_distinct_stable_artifact_are_ambiguous(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'source'
            selected = source / 'build/bundles/megadrive' / ('a' * 64)
            stable = root / 'cache' / 'megadrive' / ('b' * 64)
            (source / 'scripts').mkdir(parents=True)
            (source / 'scripts/rebuild_core.py').write_text('recipe')
            selected.mkdir(parents=True)
            (selected / 'megadrive.rbf').write_bytes(b'rbf-selected')
            (selected / 'megadrive-rbf.toml').write_bytes(b'manifest-selected')
            self._sealed_bundle(stable, 'megadrive')
            with patch.object(build, 'FPGA_BUNDLE_CACHE', root / 'cache'), \
                 patch.object(build, 'source_checkout', return_value=source), \
                 patch.object(build.core_bundle, 'load',
                              side_effect=lambda directory, *args, **kwargs: {
                                  'sha256': Path(directory).name}), \
                 patch.object(build, 'run', side_effect=AssertionError('unexpected FPGA build')):
                with self.assertRaises(ValueError) as raised:
                    build.build_bundle({'misteross': 'c' * 40}, {}, system='megadrive')
            message = str(raised.exception)
            self.assertIn('ambiguous', message)
            self.assertIn(str(selected), message)
            self.assertIn(str(stable), message)

    def test_sealed_stable_megadrive_misses_after_recipe_change(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'source'
            (source / 'scripts').mkdir(parents=True)
            recipe = source / 'scripts/rebuild_core.py'
            recipe.write_text('recipe-v1')
            cache = root / 'cache' / 'megadrive' / ('a' * 64)
            self._write_format1_megadrive(cache, build.digest(recipe))
            with patch.object(build, 'FPGA_BUNDLE_CACHE', root / 'cache'), \
                 patch.object(build, 'source_checkout', return_value=source), \
                 patch.object(build, 'run', side_effect=AssertionError('unexpected FPGA build')):
                self.assertEqual(
                    build.build_bundle({'misteross': 'b' * 40}, {}, system='megadrive'),
                    cache)
                recipe.write_text('recipe-v2')
                with self.assertRaisesRegex(ValueError, 'QUARTUS_ROOTDIR'):
                    build.build_bundle({'misteross': 'b' * 40}, {}, system='megadrive')

    def test_pong_stable_candidate_requires_selected_revision(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'source'
            cache = root / 'cache' / 'pong' / ('a' * 64)
            selected = 'b' * 40
            (source / 'scripts').mkdir(parents=True)
            (source / 'scripts/build_pong.py').write_text('recipe')
            self._sealed_bundle(cache, 'pong')

            def load(directory, recipe_sha256, *, system='megadrive', expected_revision=None):
                if system == 'pong' and expected_revision != selected:
                    raise ValueError('bundle expected revision differs from policy')
                return {'sha256': 'a' * 64}

            with patch.object(build, 'FPGA_BUNDLE_CACHE', root / 'cache'), \
                 patch.object(build, 'source_checkout', return_value=source), \
                 patch.object(build.core_bundle, 'load', side_effect=load), \
                 patch.object(build, 'run', side_effect=AssertionError('unexpected FPGA build')):
                self.assertEqual(
                    build.build_bundle({'misteross': selected}, {}, system='pong'), cache)
                build.core_bundle.load.assert_called_with(
                    cache, build.digest(source / 'scripts/build_pong.py'),
                    system='pong', expected_revision=selected)
                with self.assertRaisesRegex(ValueError, 'QUARTUS_ROOTDIR'):
                    build.build_bundle({'misteross': 'c' * 40}, {}, system='pong')

    def test_publish_bundle_cache_installs_a_sealed_digest_copy(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'source'
            source.mkdir()
            (source / 'megadrive.rbf').write_bytes(b'rbf-bytes')
            (source / 'megadrive-rbf.toml').write_bytes(b'manifest-bytes')
            cache = root / 'cache'
            with patch.object(build, 'FPGA_BUNDLE_CACHE', cache):
                published = build.publish_bundle_cache(source, 'megadrive')
            digest = build._closed_bundle_digest(source, 'megadrive')
            self.assertEqual(published, cache / 'megadrive' / digest)
            self.assertEqual((published / 'megadrive.rbf').read_bytes(), b'rbf-bytes')
            self.assertEqual((published / 'megadrive-rbf.toml').read_bytes(), b'manifest-bytes')
            self.assertFalse((published / 'megadrive.rbf').stat().st_mode & 0o222)
            self.assertFalse((published / 'megadrive-rbf.toml').stat().st_mode & 0o222)
            self.assertFalse(published.stat().st_mode & 0o222)
            with patch.object(build, 'FPGA_BUNDLE_CACHE', cache):
                self.assertEqual(build.publish_bundle_cache(source, 'megadrive'), published)
            self.assertNotEqual(digest, build.digest(source / 'megadrive.rbf'))
            published.chmod(0o755)
            cached_rbf = published / 'megadrive.rbf'
            cached_rbf.chmod(0o644)
            cached_rbf.write_bytes(b'different')
            cached_rbf.chmod(0o444)
            published.chmod(0o555)
            with patch.object(build, 'FPGA_BUNDLE_CACHE', cache), \
                 self.assertRaisesRegex(ValueError, 'differ|exists'):
                build.publish_bundle_cache(source, 'megadrive')
            self.assertEqual(cached_rbf.read_bytes(), b'different')

    def test_publish_keys_manifest_only_changes_to_a_new_closed_digest(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'source'
            source.mkdir()
            (source / 'megadrive.rbf').write_bytes(b'rbf-bytes')
            (source / 'megadrive-rbf.toml').write_bytes(b'manifest-v1')
            cache = root / 'cache'
            with patch.object(build, 'FPGA_BUNDLE_CACHE', cache):
                first = build.publish_bundle_cache(source, 'megadrive')
                (source / 'megadrive-rbf.toml').write_bytes(b'manifest-v2')
                second = build.publish_bundle_cache(source, 'megadrive')
            self.assertNotEqual(first, second)
            self.assertEqual(first, cache / 'megadrive' / build._closed_bundle_digest(
                first, 'megadrive'))
            self.assertEqual((first / 'megadrive-rbf.toml').read_bytes(), b'manifest-v1')
            self.assertEqual((second / 'megadrive-rbf.toml').read_bytes(), b'manifest-v2')
            self.assertEqual((first / 'megadrive.rbf').read_bytes(), b'rbf-bytes')
            self.assertEqual((second / 'megadrive.rbf').read_bytes(), b'rbf-bytes')

    def test_publish_bundle_cache_rejects_symlinks_without_following(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            ambient = root / 'ambient'
            ambient.mkdir()
            marker = ambient / 'keep'
            marker.write_bytes(b'unchanged')
            source = root / 'source'
            source.mkdir()
            (source / 'megadrive.rbf').write_bytes(b'rbf-bytes')
            (source / 'megadrive-rbf.toml').write_bytes(b'manifest-bytes')
            linked_source = root / 'linked-source'
            linked_source.symlink_to(source, target_is_directory=True)
            cache = root / 'cache'
            with patch.object(build, 'FPGA_BUNDLE_CACHE', cache), \
                 self.assertRaisesRegex(ValueError, 'symlink'):
                build.publish_bundle_cache(linked_source, 'megadrive')
            cache.mkdir()
            cache.joinpath('megadrive').symlink_to(ambient, target_is_directory=True)
            with patch.object(build, 'FPGA_BUNDLE_CACHE', cache), \
                 self.assertRaisesRegex(ValueError, 'symlink'):
                build.publish_bundle_cache(source, 'megadrive')
            self.assertEqual(marker.read_bytes(), b'unchanged')
            self.assertEqual([path.name for path in ambient.iterdir()], ['keep'])

    def test_built_bundle_is_published_and_checkout_path_is_returned(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'source'
            cache = root / 'cache'
            (source / 'scripts').mkdir(parents=True)
            (source / 'scripts/rebuild_core.py').write_text('recipe')
            produced = source / 'build/bundles/megadrive/identity'

            def fake_run(args, **kwargs):
                produced.mkdir(parents=True, exist_ok=True)
                (produced / 'megadrive.rbf').write_bytes(b'rbf-bytes')
                (produced / 'megadrive-rbf.toml').write_bytes(b'manifest')

            with patch.object(build, 'FPGA_BUNDLE_CACHE', cache), \
                 patch.object(build, 'source_checkout', return_value=source), \
                 patch.object(build.core_bundle, 'load', return_value={'sha256': 'a' * 64}), \
                 patch.object(build, 'run', side_effect=fake_run):
                result = build.build_bundle(
                    {'misteross': 'b' * 40}, {'QUARTUS_ROOTDIR': '/quartus'},
                    system='megadrive')
            self.assertEqual(result, produced)
            cached = cache / 'megadrive' / build._closed_bundle_digest(produced, 'megadrive')
            self.assertEqual((cached / 'megadrive.rbf').read_bytes(), b'rbf-bytes')
            self.assertFalse(cached.stat().st_mode & 0o222)

    def test_unsealed_cache_destination_does_not_fail_a_real_build(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'source'
            cache = root / 'cache'
            (source / 'scripts').mkdir(parents=True)
            (source / 'scripts/rebuild_core.py').write_text('recipe')
            produced = source / 'build/bundles/megadrive/identity'
            template = root / 'template'
            template.mkdir()
            (template / 'megadrive.rbf').write_bytes(b'rbf-bytes')
            (template / 'megadrive-rbf.toml').write_bytes(b'manifest')
            dest = cache / 'megadrive' / build._closed_bundle_digest(template, 'megadrive')
            dest.mkdir(parents=True)
            (dest / 'megadrive.rbf').write_bytes(b'rbf-bytes')
            (dest / 'megadrive-rbf.toml').write_bytes(b'manifest')

            class Recorder:
                def __init__(self):
                    self.events = []

                def cache(self, name, status, reason):
                    self.events.append((name, status, reason))

                @contextmanager
                def measure(self, name):
                    yield

            def fake_run(args, **kwargs):
                produced.mkdir(parents=True, exist_ok=True)
                (produced / 'megadrive.rbf').write_bytes(b'rbf-bytes')
                (produced / 'megadrive-rbf.toml').write_bytes(b'manifest')

            recorder = Recorder()
            with patch.object(build, 'FPGA_BUNDLE_CACHE', cache), \
                 patch.object(build, 'source_checkout', return_value=source), \
                 patch.object(build.core_bundle, 'load', return_value={'sha256': 'a' * 64}), \
                 patch.object(build, 'run', side_effect=fake_run):
                result = build.build_bundle(
                    {'misteross': 'b' * 40}, {'QUARTUS_ROOTDIR': '/quartus'},
                    system='megadrive', diagnostics=recorder)
            self.assertEqual(result, produced)
            reasons = [reason for name, status, reason in recorder.events
                       if name == 'fpga:megadrive' and status == 'miss']
            self.assertTrue(reasons)
            self.assertTrue(any(str(dest) in reason and 'stable publish skipped' in reason
                                for reason in reasons))

    def test_publish_oserror_does_not_fail_a_real_build(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'source'
            cache = root / 'cache'
            (source / 'scripts').mkdir(parents=True)
            (source / 'scripts/rebuild_core.py').write_text('recipe')
            produced = source / 'build/bundles/megadrive/identity'

            class Recorder:
                def __init__(self):
                    self.events = []

                def cache(self, name, status, reason):
                    self.events.append((name, status, reason))

                @contextmanager
                def measure(self, name):
                    yield

            def fake_run(args, **kwargs):
                produced.mkdir(parents=True, exist_ok=True)
                (produced / 'megadrive.rbf').write_bytes(b'rbf-bytes')
                (produced / 'megadrive-rbf.toml').write_bytes(b'manifest')

            recorder = Recorder()
            with patch.object(build, 'FPGA_BUNDLE_CACHE', cache), \
                 patch.object(build, 'source_checkout', return_value=source), \
                 patch.object(build.core_bundle, 'load', return_value={'sha256': 'a' * 64}), \
                 patch.object(build, 'run', side_effect=fake_run), \
                 patch.object(build.shutil, 'copy2', side_effect=OSError('copy failed')), \
                 patch.object(build, '_remove_tree', side_effect=OSError('cleanup failed')):
                result = build.build_bundle(
                    {'misteross': 'b' * 40}, {'QUARTUS_ROOTDIR': '/quartus'},
                    system='megadrive', diagnostics=recorder)
            self.assertEqual(result, produced)
            dest = cache / 'megadrive' / build._closed_bundle_digest(produced, 'megadrive')
            reasons = [reason for name, status, reason in recorder.events
                       if name == 'fpga:megadrive' and status == 'miss']
            self.assertTrue(any(str(dest) in reason and 'stable publish skipped' in reason
                                and 'copy failed' in reason for reason in reasons))
            self.assertFalse(any('cleanup failed' in reason for reason in reasons))

    def test_unreadable_stable_entry_is_skipped(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'source'
            readable = root / 'cache' / 'megadrive' / ('a' * 64)
            unreadable = root / 'cache' / 'megadrive' / ('b' * 64)
            (source / 'scripts').mkdir(parents=True)
            (source / 'scripts/rebuild_core.py').write_text('recipe')
            self._sealed_bundle(readable, 'megadrive')
            self._sealed_bundle(unreadable, 'megadrive')

            def require(directory, system):
                if Path(directory) == unreadable:
                    raise OSError('Permission denied')
                build._require_closed_bundle(directory, system, sealed=True)

            class Recorder:
                def __init__(self):
                    self.events = []

                def cache(self, name, status, reason):
                    self.events.append((name, status, reason))

                @contextmanager
                def measure(self, name):
                    yield

            recorder = Recorder()
            with patch.object(build, 'FPGA_BUNDLE_CACHE', root / 'cache'), \
                 patch.object(build, 'source_checkout', return_value=source), \
                 patch.object(build, '_require_sealed_bundle', side_effect=require), \
                 patch.object(build.core_bundle, 'load', return_value={'sha256': 'a' * 64}), \
                 patch.object(build, 'run', side_effect=AssertionError('unexpected FPGA build')):
                result = build.build_bundle(
                    {'misteross': 'c' * 40}, {}, system='megadrive', diagnostics=recorder)
            self.assertEqual(result, readable)
            reasons = [reason for name, status, reason in recorder.events
                       if name == 'fpga:megadrive' and status == 'miss']
            self.assertTrue(any(str(unreadable) in reason and 'stable candidate skipped' in reason
                                for reason in reasons))

    def test_unreadable_cache_root_metadata_is_a_miss(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'source'
            cache = root / 'cache'
            cache.mkdir()
            (cache / 'megadrive').mkdir()
            (source / 'scripts').mkdir(parents=True)
            (source / 'scripts/rebuild_core.py').write_text('recipe')
            original_lstat = build.Path.lstat

            def lstat(self):
                if self == cache:
                    raise OSError('Permission denied')
                return original_lstat(self)

            class Recorder:
                def __init__(self):
                    self.events = []

                def cache(self, name, status, reason):
                    self.events.append((name, status, reason))

                @contextmanager
                def measure(self, name):
                    yield

            recorder = Recorder()
            with patch.object(build.Path, 'lstat', lstat), \
                 patch.object(build, 'FPGA_BUNDLE_CACHE', cache), \
                 patch.object(build, 'source_checkout', return_value=source), \
                 patch.object(build, 'run', side_effect=AssertionError('unexpected FPGA build')):
                with self.assertRaisesRegex(ValueError, 'QUARTUS_ROOTDIR'):
                    build.build_bundle(
                        {'misteross': 'c' * 40}, {}, system='megadrive',
                        diagnostics=recorder)
            reasons = [reason for name, status, reason in recorder.events
                       if name == 'fpga:megadrive' and status == 'miss']
            self.assertTrue(any(str(cache) in reason for reason in reasons))

    def test_unreadable_cache_system_metadata_is_a_miss(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'source'
            cache = root / 'cache'
            system_root = cache / 'megadrive'
            system_root.mkdir(parents=True)
            (source / 'scripts').mkdir(parents=True)
            (source / 'scripts/rebuild_core.py').write_text('recipe')
            original_lstat = build.Path.lstat

            def lstat(self):
                if self == system_root:
                    raise OSError('Permission denied')
                return original_lstat(self)

            class Recorder:
                def __init__(self):
                    self.events = []

                def cache(self, name, status, reason):
                    self.events.append((name, status, reason))

                @contextmanager
                def measure(self, name):
                    yield

            recorder = Recorder()
            with patch.object(build.Path, 'lstat', lstat), \
                 patch.object(build, 'FPGA_BUNDLE_CACHE', cache), \
                 patch.object(build, 'source_checkout', return_value=source), \
                 patch.object(build, 'run', side_effect=AssertionError('unexpected FPGA build')):
                with self.assertRaisesRegex(ValueError, 'QUARTUS_ROOTDIR'):
                    build.build_bundle(
                        {'misteross': 'c' * 40}, {}, system='megadrive',
                        diagnostics=recorder)
            reasons = [reason for name, status, reason in recorder.events
                       if name == 'fpga:megadrive' and status == 'miss']
            self.assertTrue(any(str(system_root) in reason for reason in reasons))

    def test_unreadable_cache_system_enumeration_is_a_miss(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'source'
            cache = root / 'cache'
            system_root = cache / 'megadrive'
            system_root.mkdir(parents=True)
            (source / 'scripts').mkdir(parents=True)
            (source / 'scripts/rebuild_core.py').write_text('recipe')
            original_iterdir = build.Path.iterdir

            def iterdir(self):
                if self == system_root:
                    raise OSError('Permission denied')
                return original_iterdir(self)

            class Recorder:
                def __init__(self):
                    self.events = []

                def cache(self, name, status, reason):
                    self.events.append((name, status, reason))

                @contextmanager
                def measure(self, name):
                    yield

            recorder = Recorder()
            with patch.object(build.Path, 'iterdir', iterdir), \
                 patch.object(build, 'FPGA_BUNDLE_CACHE', cache), \
                 patch.object(build, 'source_checkout', return_value=source), \
                 patch.object(build, 'run', side_effect=AssertionError('unexpected FPGA build')):
                with self.assertRaisesRegex(ValueError, 'QUARTUS_ROOTDIR'):
                    build.build_bundle(
                        {'misteross': 'c' * 40}, {}, system='megadrive',
                        diagnostics=recorder)
            reasons = [reason for name, status, reason in recorder.events
                       if name == 'fpga:megadrive' and status == 'miss']
            self.assertTrue(any(str(system_root) in reason for reason in reasons))
