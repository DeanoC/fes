"""The selected profile controls the complete installed FPGA core set."""
from contextlib import contextmanager
import json
import os
from pathlib import Path
import hashlib
import shutil
import sys
import tempfile
import tomllib
import unittest
from unittest.mock import patch
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import build
from environment import build_environment


def make_package_generation(root, label, package_ids):
    packages = []
    built_selections = {}
    for index, (core_id, selection_name, package_id) in enumerate(zip(
            ('fes.pong', 'fes.zx81'),
            ('fes-pong.package-selection.toml', 'fes-zx81.package-selection.toml'),
            package_ids)):
        source = root / f'{label}-{core_id}'
        source.mkdir()
        (source / 'manifest.toml').write_bytes(f'{label}-manifest-{index}'.encode())
        (source / 'core.rbf').write_bytes(f'{label}-payload-{index}'.encode())
        selection = root / f'{label}-{selection_name}'
        selection.write_bytes(f'format = 2\nlabel = "{label}"\n'.encode())
        built = root / 'built' / label / selection_name
        built.parent.mkdir(parents=True, exist_ok=True)
        built.write_bytes(selection.read_bytes())
        packages.append({
            'directory': source,
            'selection_path': selection,
            'inputs': {
                'selection': {'package_id': package_id, 'core_id': core_id},
                'selection_sha256': hashlib.sha256(selection.read_bytes()).hexdigest(),
                'manifest_sha256': hashlib.sha256((source / 'manifest.toml').read_bytes()).hexdigest(),
                'core_rbf_sha256': hashlib.sha256((source / 'core.rbf').read_bytes()).hexdigest(),
            },
        })
        built_selections[selection_name] = built
    return tuple(packages), built_selections


def package_file_snapshot(output):
    output = Path(output)
    files = {}
    root = output / 'core-packages'
    if root.is_dir() and not root.is_symlink():
        files.update({path.relative_to(output).as_posix(): path.read_bytes()
                      for path in root.rglob('*') if path.is_file()})
    files.update({path.name: path.read_bytes()
                  for path in output.glob('*.package-selection.toml') if path.is_file()})
    return files


def write_complete_backup_fixture(output, package_id):
    backup = output / '.package-generation.previous'
    backup.mkdir()
    shutil.copytree(output / 'core-packages', backup / 'core-packages', symlinks=True)
    for path in output.glob('*.package-selection.toml'):
        shutil.copy2(path, backup / path.name)
    (backup / 'core-packages' / package_id).chmod(0o755)
    nested = backup / 'core-packages' / package_id / 'nested'
    nested.mkdir()
    (nested / 'link').symlink_to('/tmp/package-backup-outside')
    nested.chmod(0o555)
    (backup / 'core-packages' / package_id).chmod(0o555)
    (backup / 'core-packages').chmod(0o555)
    entries = []
    for relative in (
            'fes-pong.package-selection.toml',
            f'core-packages/{package_id}/manifest.toml',
            f'core-packages/{package_id}/core.rbf'):
        path = backup / relative
        entries.append({'path': relative,
                        'sha256': hashlib.sha256(path.read_bytes()).hexdigest()})
        path.chmod(0o444)
    marker = backup / '.package-generation.complete'
    marker.write_text(json.dumps({
        'format': 1,
        'directories': ['core-packages', f'core-packages/{package_id}'],
        'files': entries,
    }, sort_keys=True) + '\n')
    marker.chmod(0o444)
    backup.chmod(0o555)
    return backup


class CoreBuildTest(unittest.TestCase):
    def test_native_image_mode_is_explicit_and_historical_profiles_are_isolated(self):
        root = Path(__file__).resolve().parents[1]
        integration = tomllib.loads(
            (root / 'profiles/native-integration-dev.toml').read_text())
        historical = tomllib.loads((root / 'profiles/native-dev.toml').read_text())
        source_historical = tomllib.loads(
            (root / 'profiles/native-source-dev.toml').read_text())

        self.assertEqual(build.native_image_mode(integration), 'package-only')
        self.assertNotIn('fpga_cores', integration)
        self.assertNotIn('fpga_core', integration)
        self.assertNotIn('quartus_version', integration)
        self.assertEqual(build.native_image_mode(historical), 'format1')
        self.assertEqual(build.native_image_mode(source_historical), 'format1')
        readme = (root / 'README.md').read_text()
        development = (root / 'docs/development.md').read_text()
        packages = (root / 'docs/core-packages.md').read_text()
        self.assertIn('HIP/nextpnr', readme)
        self.assertIn('Format-1 catalog cores are not part of the', readme)
        self.assertIn('historical format-1 source builds', development)
        self.assertIn('not built or installed by this FES production path', packages)
        self.assertNotIn('alongside the four existing format-1 catalog cores', packages)

    def test_package_only_profile_never_dispatches_format1_bundles(self):
        profile = {'native_image_mode': 'package-only'}
        with patch.object(build, 'build_bundles',
                          side_effect=AssertionError('format-1 bundle dispatch')):
            self.assertEqual(
                build.build_bundles_for_profile(profile, {}, {}, ()), {})

    def test_legacy_source_image_environment_preserves_selected_native_mode(self):
        environment = build.legacy_source_image_environment(
            {'BASE': '1'}, Path('/runtime'), 'format1')
        self.assertEqual(environment, {
            'BASE': '1',
            'LIBMISTER_RUNTIME_DIR': '/runtime',
            'NATIVE_RUNTIME_MODE': 'format1',
        })

    def test_historical_default_and_explicit_set(self):
        self.assertEqual(build.selected_cores({'fpga_core': 'megadrive'}), ('megadrive',))
        self.assertEqual(build.selected_cores({'fpga_cores': ['megadrive', 'pong', 'snes', 'nes']}),
                         ('megadrive', 'pong', 'snes', 'nes'))
        for cores in ([], ['pong'], ['megadrive', 'pong'], ['megadrive', 'snes', 'pong'], ['megadrive', 'pong', 'pong'], ['megadrive', '../snes']):
            with self.subTest(cores=cores), self.assertRaises(ValueError):
                build.selected_cores({'fpga_cores': cores})

    def test_only_integration_profile_selects_the_ordered_supported_package_set(self):
        selection = {'fpga_packages': [
            {'core_id': 'fes.pong'},
            {'core_id': 'fes.zx81'},
            {'core_id': 'fes.coleco'},
        ]}
        self.assertEqual(build.selected_packages(selection, 'native-integration-dev'),
                         ('fes.pong', 'fes.zx81', 'fes.coleco'))
        self.assertEqual(build.selected_packages(
            {'fpga_packages': [{'core_id': 'fes.zx81'}]}, 'native-integration-dev'),
                         ('fes.zx81',))
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
                                      'FES_ZX81_PACKAGE_DIR': '/untrusted-zx81-package',
                                      'FES_ZX81_PACKAGE_SELECTION': '/untrusted-zx81-selection',
                                      'FES_COLECO_PACKAGE_DIR': '/untrusted-coleco-package',
                                      'FES_COLECO_PACKAGE_SELECTION': '/untrusted-coleco-selection',
                                      'FES_PACKAGE_IDS': 'fes.pong,fes.zx81',
                                      'FES_TOOLCHAIN_CACHE_ROOT': '/ambient-toolchains'}):
            env = build_environment()
        self.assertFalse('PONG_RBF_BUNDLE' in env)
        self.assertFalse('SNES_RBF_BUNDLE' in env)
        self.assertFalse('NES_RBF_BUNDLE' in env)
        self.assertFalse('NATIVE_RUNTIME_SYSTEMS' in env)
        self.assertFalse('FES_PONG_PACKAGE_DIR' in env)
        self.assertFalse('FES_PONG_PACKAGE_SELECTION' in env)
        self.assertFalse('FES_ZX81_PACKAGE_DIR' in env)
        self.assertFalse('FES_ZX81_PACKAGE_SELECTION' in env)
        self.assertFalse('FES_COLECO_PACKAGE_DIR' in env)
        self.assertFalse('FES_COLECO_PACKAGE_SELECTION' in env)
        self.assertFalse('FES_PACKAGE_IDS' in env)
        self.assertFalse('FES_TOOLCHAIN_CACHE_ROOT' in env)

    def test_generic_build_environment_keeps_local_compiler_overrides(self):
        with patch.dict('os.environ', {'LD_LIBRARY_PATH': '/opt/rocm/lib', 'CC': 'clang',
                                       'PYTHON': 'python3'}):
            env = build_environment()
        self.assertEqual(env['LD_LIBRARY_PATH'], '/opt/rocm/lib')
        self.assertEqual(env['CC'], 'clang')
        self.assertEqual(env['PYTHON'], 'python3')
        self.assertFalse('FES_TOOLCHAIN_CACHE_ROOT' in env)

    def test_parent_package_resolution_forwards_scrubbed_environment(self):
        captured = {}

        def fake_resolve(source, packages_revision, selection_path, force=False, env=None, recipe=None):
            captured['source'] = source
            captured['packages'] = packages_revision
            captured['selection'] = selection_path
            captured['force'] = force
            captured['env'] = env
            return {'directory': Path('/pkg')}

        with patch.dict('os.environ', {'MAKEFLAGS': 's', 'MFLAGS': '-j2',
                                       'FES_TOOLCHAIN_CACHE_ROOT': '/ambient-toolchains'}):
            env = build_environment()
            env['KEEP'] = '1'
            with patch.object(build, 'source_checkout', return_value=Path('/work/misteross')) as checkout, \
                 patch.object(build.core_bundle, 'resolve_core_package', side_effect=fake_resolve):
                result = build.resolve_selected_package(
                    {'misteross': 'a' * 40, 'mister-packages': 'b' * 40},
                    Path('/out/fes-pong.package-selection.toml'), env)
        checkout.assert_called_once_with('misteross', 'a' * 40)
        self.assertEqual(captured['source'], Path('/work/misteross'))
        self.assertEqual(captured['packages'], 'b' * 40)
        self.assertEqual(captured['selection'], Path('/out/fes-pong.package-selection.toml'))
        self.assertFalse(captured['force'])
        self.assertIs(captured['env'], env)
        self.assertNotIn('MAKEFLAGS', captured['env'])
        self.assertNotIn('MFLAGS', captured['env'])
        self.assertNotIn('FES_TOOLCHAIN_CACHE_ROOT', captured['env'])
        self.assertEqual(captured['env']['KEEP'], '1')
        self.assertEqual(result['directory'], Path('/pkg'))
        self.assertNotIn('MAKEFLAGS', env)
        self.assertNotIn('FES_TOOLCHAIN_CACHE_ROOT', env)

    def test_rebuild_forces_selected_package_production(self):
        captured = {}

        def fake_resolve(revisions, selection_path, env, force=False, recipe=None):
            captured.update(revisions=revisions, selection=selection_path,
                            env=env, force=force, recipe=recipe)
            return {'directory': Path('/pkg')}

        recipe = build.recipe_for('fes.pong')
        with patch.object(build, 'resolve_selected_package', side_effect=fake_resolve):
            result = build.resolve_package_for_action(
                {'misteross': 'a' * 40, 'mister-packages': 'b' * 40},
                Path('/out'), {'KEEP': '1'}, 'rebuild', recipe)
        self.assertEqual(result['directory'], Path('/pkg'))
        self.assertEqual(captured['selection'], Path('/out/' + recipe.selection_filename))
        self.assertTrue(captured['force'])
        self.assertIs(captured['recipe'], recipe)

    def test_parent_resolves_each_selected_recipe_in_profile_order(self):
        resolved = []

        def fake_resolve(revisions, output, env, action, recipe):
            resolved.append((output, action, recipe.core_id))
            return {'recipe': recipe.core_id}

        with patch.object(build, 'resolve_package_for_action', side_effect=fake_resolve):
            result = build.resolve_packages_for_action(
                {'misteross': 'a' * 40, 'mister-packages': 'b' * 40},
                Path('/out'), {'KEEP': '1'}, 'build', ('fes.zx81', 'fes.coleco'))
        self.assertEqual(result, ({'recipe': 'fes.zx81'}, {'recipe': 'fes.coleco'}))
        self.assertEqual(resolved, [
            (Path('/out'), 'build', 'fes.zx81'),
            (Path('/out'), 'build', 'fes.coleco'),
        ])

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
            'FES_PACKAGE_IDS=fes.pong',
            'FES_PONG_PACKAGE_DIR=/packages/identity',
            'FES_PONG_PACKAGE_SELECTION=/records/fes-pong.package-selection.toml'])
        first, first_info = build.image_fingerprint('base', {'sources': {}}, package)
        changed = dict(package, inputs=dict(package['inputs'], selection_sha256='0' * 64))
        second, _ = build.image_fingerprint('base', {'sources': {}}, changed)
        self.assertNotEqual(first, second)
        self.assertEqual(first_info['fpga_packages'], [package['inputs']])
        self.assertEqual(first_info['image_base_fingerprint'], 'base')
        self.assertEqual(first_info['image_fingerprint'], first)

    def test_package_set_arguments_and_fingerprint_preserve_ordered_inputs(self):
        packages = (
            {
                'directory': Path('/packages/pong'),
                'selection_path': Path('/records/fes-pong.package-selection.toml'),
                'inputs': {'selection': {'core_id': 'fes.pong', 'package_id': 'a' * 64},
                           'selection_sha256': '1' * 64, 'manifest_sha256': '2' * 64,
                           'core_rbf_sha256': '3' * 64},
            },
            {
                'directory': Path('/packages/zx81'),
                'selection_path': Path('/records/fes-zx81.package-selection.toml'),
                'inputs': {'selection': {'core_id': 'fes.zx81', 'package_id': 'b' * 64},
                           'selection_sha256': '4' * 64, 'manifest_sha256': '5' * 64,
                           'core_rbf_sha256': '6' * 64},
            },
            {
                'directory': Path('/packages/coleco'),
                'selection_path': Path('/records/fes-coleco.package-selection.toml'),
                'inputs': {'selection': {'core_id': 'fes.coleco', 'package_id': 'c' * 64},
                           'selection_sha256': '7' * 64, 'manifest_sha256': '8' * 64,
                           'core_rbf_sha256': '9' * 64},
            },
        )
        self.assertEqual(build.package_arguments(packages), [
            'FES_PACKAGE_IDS=fes.pong,fes.zx81,fes.coleco',
            'FES_PONG_PACKAGE_DIR=/packages/pong',
            'FES_PONG_PACKAGE_SELECTION=/records/fes-pong.package-selection.toml',
            'FES_ZX81_PACKAGE_DIR=/packages/zx81',
            'FES_ZX81_PACKAGE_SELECTION=/records/fes-zx81.package-selection.toml',
            'FES_COLECO_PACKAGE_DIR=/packages/coleco',
            'FES_COLECO_PACKAGE_SELECTION=/records/fes-coleco.package-selection.toml',
        ])
        first, first_info = build.image_fingerprint('base', {'sources': {}}, packages)
        reversed_fingerprint, _ = build.image_fingerprint(
            'base', {'sources': {}}, tuple(reversed(packages)))
        changed = tuple(dict(package, inputs=dict(package['inputs'], selection_sha256='0' * 64))
                        if index == 1 else package
                        for index, package in enumerate(packages))
        changed_fingerprint, _ = build.image_fingerprint('base', {'sources': {}}, changed)
        self.assertNotEqual(first, reversed_fingerprint)
        self.assertNotEqual(first, changed_fingerprint)
        self.assertEqual(first_info['fpga_packages'],
                         [package['inputs'] for package in packages])

    def test_multi_package_publication_is_complete_and_rejects_extra_selection_or_identity(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            output = root / 'output'
            output.mkdir()
            packages = []
            built_selections = {}
            for index, (core_id, selection_name, package_id) in enumerate((
                    ('fes.pong', 'fes-pong.package-selection.toml', 'a' * 64),
                    ('fes.zx81', 'fes-zx81.package-selection.toml', 'b' * 64))):
                source = root / core_id
                source.mkdir()
                (source / 'manifest.toml').write_bytes(f'manifest-{index}'.encode())
                (source / 'core.rbf').write_bytes(f'payload-{index}'.encode())
                selection = root / selection_name
                selection.write_bytes(f'format = 2\ncore = "{core_id}"\n'.encode())
                built = root / 'built' / selection_name
                built.parent.mkdir(exist_ok=True)
                built.write_bytes(selection.read_bytes())
                packages.append({
                    'directory': source,
                    'selection_path': selection,
                    'inputs': {
                        'selection': {'package_id': package_id, 'core_id': core_id},
                        'selection_sha256': build.digest(selection),
                        'manifest_sha256': build.digest(source / 'manifest.toml'),
                        'core_rbf_sha256': build.digest(source / 'core.rbf'),
                    },
                })
                built_selections[selection_name] = built
            packages = tuple(packages)

            names = build.publish_package_outputs(packages, built_selections, output)
            self.assertEqual(names, [
                'fes-pong.package-selection.toml',
                'core-packages/' + 'a' * 64 + '/manifest.toml',
                'core-packages/' + 'a' * 64 + '/core.rbf',
                'fes-zx81.package-selection.toml',
                'core-packages/' + 'b' * 64 + '/manifest.toml',
                'core-packages/' + 'b' * 64 + '/core.rbf',
            ])
            self.assertEqual(build.verify_package_outputs(output, packages), names)

            (output / 'fes-coleco.package-selection.toml').write_bytes(b'extra')
            with self.assertRaisesRegex(ValueError, 'closed set'):
                build.verify_package_outputs(output, packages)
            (output / 'fes-coleco.package-selection.toml').unlink()
            extra = output / 'core-packages' / ('c' * 64)
            (output / 'core-packages').chmod(0o755)
            extra.mkdir()
            (output / 'core-packages').chmod(0o555)
            with self.assertRaisesRegex(ValueError, 'changed|differs'):
                build.verify_package_outputs(output, packages)
            (output / 'core-packages').chmod(0o755)
            extra.rmdir()
            (output / 'core-packages').chmod(0o555)
            with self.assertRaisesRegex(ValueError, 'duplicate'):
                build.verify_package_outputs(output, (packages[0], packages[0]))

    def test_package_publication_rolls_back_after_a_selection_replacement_failure(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            output = root / 'output'
            output.mkdir()

            def make_generation(label, package_ids):
                packages = []
                built_selections = {}
                for index, (core_id, selection_name, package_id) in enumerate(zip(
                        ('fes.pong', 'fes.zx81'),
                        ('fes-pong.package-selection.toml', 'fes-zx81.package-selection.toml'),
                        package_ids)):
                    source = root / f'{label}-{core_id}'
                    source.mkdir()
                    (source / 'manifest.toml').write_bytes(f'{label}-manifest-{index}'.encode())
                    (source / 'core.rbf').write_bytes(f'{label}-payload-{index}'.encode())
                    selection = root / f'{label}-{selection_name}'
                    selection.write_bytes(f'format = 2\nlabel = "{label}"\n'.encode())
                    built = root / 'built' / label / selection_name
                    built.parent.mkdir(parents=True, exist_ok=True)
                    built.write_bytes(selection.read_bytes())
                    packages.append({
                        'directory': source,
                        'selection_path': selection,
                        'inputs': {
                            'selection': {'package_id': package_id, 'core_id': core_id},
                            'selection_sha256': build.digest(selection),
                            'manifest_sha256': build.digest(source / 'manifest.toml'),
                            'core_rbf_sha256': build.digest(source / 'core.rbf'),
                        },
                    })
                    built_selections[selection_name] = built
                return tuple(packages), built_selections

            old_packages, old_built = make_generation('old', ('a' * 64, 'b' * 64))
            build.publish_package_outputs(old_packages, old_built, output)
            old_root = {
                path.relative_to(output).as_posix(): path.read_bytes()
                for path in (output / 'core-packages').rglob('*') if path.is_file()
            }
            old_selections = {
                name: (output / name).read_bytes()
                for name in ('fes-pong.package-selection.toml',
                             'fes-zx81.package-selection.toml')
            }
            new_packages, new_built = make_generation('new', ('c' * 64, 'd' * 64))
            original_replace = Path.replace
            replacements = 0

            def fail_after_first_selection(self, target):
                nonlocal replacements
                result = original_replace(self, target)
                target = Path(target)
                if (target.parent == output and
                        target.name.endswith('.package-selection.toml')):
                    replacements += 1
                    if replacements == 1:
                        raise RuntimeError('injected after first selection replacement')
                return result

            with patch.object(Path, 'replace', new=fail_after_first_selection), \
                 self.assertRaisesRegex(RuntimeError, 'injected'):
                build.publish_package_outputs(new_packages, new_built, output)

            self.assertEqual({
                path.relative_to(output).as_posix(): path.read_bytes()
                for path in (output / 'core-packages').rglob('*') if path.is_file()
            }, old_root)
            self.assertEqual({
                name: (output / name).read_bytes() for name in old_selections
            }, old_selections)
            self.assertFalse((output / '.core-packages.new').exists())
            self.assertFalse((output / '.package-selections.new').exists())
            self.assertFalse((output / '.package-generation.new').exists())
            self.assertFalse((output / '.package-generation.previous').exists())

    def test_malformed_sealed_package_backup_preserves_the_live_generation(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            output = root / 'output'
            output.mkdir()
            old_packages, old_built = make_package_generation(root, 'old', ('a' * 64,))
            build.publish_package_outputs(old_packages, old_built, output)
            before = package_file_snapshot(output)
            write_complete_backup_fixture(output, 'a' * 64)
            new_packages, new_built = make_package_generation(root, 'new', ('b' * 64,))

            with self.assertRaisesRegex(ValueError, 'backup|generation|symlink|closed'):
                build.publish_package_outputs(new_packages, new_built, output)

            self.assertEqual(package_file_snapshot(output), before)
            self.assertTrue((output / '.package-generation.previous').is_dir())

    def test_selection_only_package_backup_is_rejected_without_touching_live_generation(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            output = root / 'output'
            output.mkdir()
            old_packages, old_built = make_package_generation(root, 'old', ('a' * 64,))
            build.publish_package_outputs(old_packages, old_built, output)
            before = package_file_snapshot(output)

            backup = output / '.package-generation.previous'
            backup.mkdir()
            selection = backup / 'fes-pong.package-selection.toml'
            selection.write_bytes((output / selection.name).read_bytes())
            selection.chmod(0o444)
            marker = backup / '.package-generation.complete'
            marker.write_text(json.dumps({
                'format': 1,
                'directories': [],
                'files': [{'path': selection.name,
                           'sha256': build.digest(selection)}],
            }, sort_keys=True) + '\n')
            marker.chmod(0o444)
            backup.chmod(0o555)

            new_packages, new_built = make_package_generation(root, 'new', ('b' * 64,))
            with self.assertRaisesRegex(ValueError, 'closed|generation|package'):
                build.publish_package_outputs(new_packages, new_built, output)

            self.assertEqual(package_file_snapshot(output), before)
            self.assertTrue(backup.is_dir())
            self.assertTrue(marker.is_file())

    def test_unmarked_partial_backup_is_discarded_after_validating_current_generation(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            output = root / 'output'
            output.mkdir()
            old_packages, old_built = make_package_generation(
                root, 'old', ('a' * 64, 'b' * 64))
            build.publish_package_outputs(old_packages, old_built, output)

            backup = output / '.package-generation.previous'
            partial = backup / 'core-packages' / ('a' * 64)
            partial.mkdir(parents=True)
            (partial / 'manifest.toml').write_bytes(b'partial')
            partial.chmod(0o555)
            (backup / 'core-packages').chmod(0o555)
            backup.chmod(0o555)

            new_packages, new_built = make_package_generation(
                root, 'new', ('c' * 64, 'd' * 64))
            build.publish_package_outputs(new_packages, new_built, output)

            self.assertEqual(build.verify_package_outputs(output, new_packages), [
                'fes-pong.package-selection.toml',
                'core-packages/' + 'c' * 64 + '/manifest.toml',
                'core-packages/' + 'c' * 64 + '/core.rbf',
                'fes-zx81.package-selection.toml',
                'core-packages/' + 'd' * 64 + '/manifest.toml',
                'core-packages/' + 'd' * 64 + '/core.rbf',
            ])
            self.assertFalse(backup.exists())

    def test_package_publication_fsyncs_payloads_and_replacement_boundaries(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            output = root / 'output'
            output.mkdir()
            old_packages, old_built = make_package_generation(
                root, 'old', ('a' * 64, 'b' * 64))
            build.publish_package_outputs(old_packages, old_built, output)
            new_packages, new_built = make_package_generation(
                root, 'new', ('c' * 64, 'd' * 64))
            fsynced = []

            def record_fsync(descriptor):
                try:
                    fsynced.append(Path(os.readlink(f'/proc/self/fd/{descriptor}')))
                except OSError:
                    pass

            with patch.object(build.os, 'fsync', side_effect=record_fsync):
                build.publish_package_outputs(new_packages, new_built, output)

            expected = {
                output / '.package-generation.previous' / 'fes-pong.package-selection.toml',
                output / '.package-generation.previous' / 'fes-zx81.package-selection.toml',
                output / '.package-generation.previous' / 'core-packages',
                output / '.package-generation.previous' / 'core-packages' / ('a' * 64),
                output / '.package-generation.previous' / 'core-packages' / ('b' * 64),
                output / '.package-generation.previous' / 'core-packages' / ('a' * 64) / 'manifest.toml',
                output / '.package-generation.previous' / 'core-packages' / ('a' * 64) / 'core.rbf',
                output / '.package-generation.previous' / 'core-packages' / ('b' * 64) / 'manifest.toml',
                output / '.package-generation.previous' / 'core-packages' / ('b' * 64) / 'core.rbf',
                output / 'fes-pong.package-selection.toml',
                output / 'fes-zx81.package-selection.toml',
                output / 'core-packages',
                output / 'core-packages' / ('c' * 64),
                output / 'core-packages' / ('d' * 64),
                output / 'core-packages' / ('c' * 64) / 'manifest.toml',
                output / 'core-packages' / ('c' * 64) / 'core.rbf',
                output / 'core-packages' / ('d' * 64) / 'manifest.toml',
                output / 'core-packages' / ('d' * 64) / 'core.rbf',
                output,
            }
            self.assertTrue(expected.issubset(set(fsynced)),
                            f'missing fsync paths: {sorted(expected - set(fsynced))}')

    def test_rollback_failure_preserves_backup_for_the_next_normal_publish(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            output = root / 'output'
            output.mkdir()
            old_packages, old_built = make_package_generation(
                root, 'old', ('a' * 64, 'b' * 64))
            build.publish_package_outputs(old_packages, old_built, output)
            old_files = package_file_snapshot(output)
            new_packages, new_built = make_package_generation(
                root, 'new', ('c' * 64, 'd' * 64))
            backup = output / '.package-generation.previous'
            original_replace = Path.replace
            phase = 'publish'

            def fail_during_rollback(source, target):
                nonlocal phase
                source = Path(source)
                target = Path(target)
                if (phase == 'publish' and target.parent == output and
                        target.name == 'fes-pong.package-selection.toml'):
                    result = original_replace(source, target)
                    phase = 'rollback'
                    raise RuntimeError('injected publish failure')
                if (phase == 'rollback' and target.parent == output and
                        target.name == 'fes-zx81.package-selection.toml'):
                    raise RuntimeError('injected rollback failure')
                return original_replace(source, target)

            with patch.object(Path, 'replace', new=fail_during_rollback), \
                    self.assertRaisesRegex(RuntimeError, 'injected publish failure'):
                build.publish_package_outputs(new_packages, new_built, output)

            self.assertTrue(backup.is_dir())
            self.assertEqual(package_file_snapshot(backup), old_files)

            build.publish_package_outputs(new_packages, new_built, output)
            self.assertEqual(build.verify_package_outputs(output, new_packages), [
                'fes-pong.package-selection.toml',
                'core-packages/' + 'c' * 64 + '/manifest.toml',
                'core-packages/' + 'c' * 64 + '/core.rbf',
                'fes-zx81.package-selection.toml',
                'core-packages/' + 'd' * 64 + '/manifest.toml',
                'core-packages/' + 'd' * 64 + '/core.rbf',
            ])
            self.assertFalse(backup.exists())
            self.assertFalse((output / '.package-generation.restore').exists())

    def test_package_publication_cleans_sealed_interrupted_staging(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            output = root / 'output'
            output.mkdir()
            source = root / 'source'
            source.mkdir()
            (source / 'manifest.toml').write_bytes(b'manifest')
            (source / 'core.rbf').write_bytes(b'payload')
            selection = root / 'selection.toml'
            selection.write_bytes(b'format = 2\n')
            built = root / 'built.toml'
            built.write_bytes(selection.read_bytes())
            identity = 'a' * 64
            package = {
                'directory': source,
                'selection_path': selection,
                'inputs': {
                    'selection': {'package_id': identity, 'core_id': 'fes.pong'},
                    'selection_sha256': build.digest(selection),
                    'manifest_sha256': build.digest(source / 'manifest.toml'),
                    'core_rbf_sha256': build.digest(source / 'core.rbf'),
                },
            }
            old_stage_root = output / '.core-packages.new' / identity
            old_stage_root.mkdir(parents=True)
            (old_stage_root / 'core.rbf').write_bytes(b'interrupted')
            (output / '.core-packages.new' / identity / 'core.rbf').chmod(0o444)
            (output / '.core-packages.new' / identity).chmod(0o555)
            (output / '.core-packages.new').chmod(0o555)
            old_stage_selections = output / '.package-selections.new'
            old_stage_selections.mkdir()
            (old_stage_selections / 'stale.package-selection.toml').write_bytes(b'interrupted')
            (old_stage_selections / 'stale.package-selection.toml').chmod(0o444)
            old_stage_selections.chmod(0o555)

            build.publish_package_outputs(package, built, output)

            self.assertEqual(build.verify_package_outputs(output, package), [
                'fes-pong.package-selection.toml',
                f'core-packages/{identity}/manifest.toml',
                f'core-packages/{identity}/core.rbf'])
            self.assertFalse((output / '.core-packages.new').exists())
            self.assertFalse((output / '.package-selections.new').exists())
            self.assertFalse((output / '.package-generation.new').exists())
            self.assertFalse((output / '.package-generation.previous').exists())

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
            for stale_name in ('fes-zx81.package-selection.toml',
                               'fes-coleco.package-selection.toml'):
                stale_selection = output / stale_name
                stale_selection.write_bytes(b'stale selection')
                stale_selection.chmod(0o444)
            stale = output / 'core-packages/stale'
            stale.mkdir(parents=True)
            (stale / 'old').write_bytes(b'old')
            package = {
                'directory': source,
                'selection_path': selection,
                'inputs': {
                    'selection': {'package_id': identity, 'core_id': 'fes.pong'},
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
            self.assertFalse((output / 'fes-zx81.package-selection.toml').exists())
            self.assertFalse((output / 'fes-coleco.package-selection.toml').exists())
            build.verify_package_outputs(output, package)
            (output / 'megadrive.rbf').write_bytes(b'legacy')
            with self.assertRaisesRegex(ValueError, 'legacy format-1'):
                build.verify_package_only_outputs(output, package)
            build.remove_format1_parent_outputs(output)
            build.verify_package_only_outputs(output, package)
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
                'selection': {'package_id': 'a' * 64, 'core_id': 'fes.pong'},
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
