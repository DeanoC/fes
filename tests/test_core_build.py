"""The selected profile controls the complete installed FPGA core set."""
from contextlib import contextmanager
import json
import os
from pathlib import Path
import hashlib
import shutil
import sys
import subprocess
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
    def test_native_image_mode_is_package_only(self):
        root = Path(__file__).resolve().parents[1]
        integration = tomllib.loads(
            (root / 'profiles/native-integration-dev.toml').read_text())

        self.assertEqual(build.native_image_mode(integration), 'package-only')
        self.assertNotIn('fpga_cores', integration)
        self.assertNotIn('fpga_core', integration)
        self.assertNotIn('quartus_version', integration)
        with self.assertRaisesRegex(ValueError, 'package-only'):
            build.native_image_mode({'native_image_mode': 'legacy'})
        readme = (root / 'README.md').read_text()
        development = (root / 'docs/development.md').read_text()
        packages = (root / 'docs/core-packages.md').read_text()
        self.assertIn('HIP/nextpnr', readme)
        self.assertIn('no legacy bundle', readme)
        self.assertIn('package-only', development)
        self.assertIn('image route is package-only', packages)
        self.assertNotIn('alongside the four existing format-1 catalog cores', packages)

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
            ('native-integration-dev', {'fpga_packages': [{'core_id': 'pong'}]}),
            ('native-integration-dev', {'fpga_packages': [{'core_id': 'fes.pong'}, {'core_id': 'fes.pong'}]}),
            ('native-integration-dev', {'fpga_packages': {'core_id': 'fes.pong'}}),
        ):
            with self.subTest(profile=profile_name, value=profile), self.assertRaises(ValueError):
                build.selected_packages(profile, profile_name)
        repository_profile = tomllib.loads((Path(__file__).resolve().parents[1] /
                                             'profiles/native-integration-dev.toml').read_text())
        self.assertEqual(repository_profile['native_image_mode'], 'package-only')
        self.assertEqual(
            [entry['core_id'] for entry in repository_profile['fpga_packages']],
            ['fes.pong', 'fes.zx81', 'fes.coleco'])
        self.assertEqual(build.selected_packages(repository_profile, 'native-integration-dev'),
                         ('fes.pong', 'fes.zx81', 'fes.coleco'))

    def test_selection_overrides_do_not_leak_from_shell(self):
        with patch.dict('os.environ', {'NATIVE_RUNTIME_SYSTEMS': 'pong',
                                      'FES_PONG_PACKAGE_DIR': '/untrusted-package',
                                      'FES_PONG_PACKAGE_SELECTION': '/untrusted-selection',
                                      'FES_ZX81_PACKAGE_DIR': '/untrusted-zx81-package',
                                      'FES_ZX81_PACKAGE_SELECTION': '/untrusted-zx81-selection',
                                      'FES_COLECO_PACKAGE_DIR': '/untrusted-coleco-package',
                                      'FES_COLECO_PACKAGE_SELECTION': '/untrusted-coleco-selection',
                                      'FES_PACKAGE_IDS': 'fes.pong,fes.zx81',
                                      'FES_TOOLCHAIN_CACHE_ROOT': '/ambient-toolchains'}):
            env = build_environment()
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

    def test_clean_package_only_image_fetch_passes_complete_package_tuple(self):
        packages = tuple({
            'directory': Path('/packages') / core_id,
            'selection_path': Path('/records') / (core_id.replace('.', '-') + '.package-selection.toml'),
            'inputs': {'selection': {'core_id': core_id, 'package_id': package_id}},
        } for core_id, package_id in (
            ('fes.pong', 'a' * 64),
            ('fes.zx81', 'b' * 64),
            ('fes.coleco', 'c' * 64),
        ))
        captured = {}

        class StopAfterFetch(Exception):
            pass

        class FakeDiagnostics:
            @contextmanager
            def measure(self, *args):
                yield

            def cache(self, *args):
                pass

        @contextmanager
        def no_diagnostics(*args, **kwargs):
            yield None, FakeDiagnostics()

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fogcast = root / 'FogCast'
            (fogcast / 'build').mkdir(parents=True)
            (fogcast / 'build/native-runtime.inputs.lock.toml').write_text('lock')
            real_tomllib_loads = tomllib.loads

            def fake_checkout(name, revision, *args):
                checkout = fogcast if name == 'FogCast' else root / name
                checkout.mkdir(parents=True, exist_ok=True)
                return checkout

            def fake_loads(data):
                parsed = real_tomllib_loads(data)
                if 'native_image_mode' in data and 'fpga_packages' in data:
                    parsed.pop('check_packages', None)
                return parsed

            def fake_run(args, **kwargs):
                if (len(args) >= 3 and
                        Path(args[0]).name == 'target-image-container.sh' and
                        args[1:3] == ['fetch', '/work/scripts/fetch-target-image-sources.sh']):
                    captured['env'] = kwargs['env']
                    raise StopAfterFetch
                return subprocess.CompletedProcess(args, 0)

            revisions = {
                'FogCast': '1' * 40,
                'libmister-runtime': '2' * 40,
                'mister-packages': '3' * 40,
                'misteross': '4' * 40,
            }
            with patch.dict(os.environ, {
                    'FES_PACKAGE_IDS': 'ambient',
                    'FES_PONG_PACKAGE_DIR': '/ambient/pong',
                    'FES_PONG_PACKAGE_SELECTION': '/ambient/pong-selection',
                    'FES_ZX81_PACKAGE_DIR': '/ambient/zx81',
                    'FES_ZX81_PACKAGE_SELECTION': '/ambient/zx81-selection',
                    'FES_COLECO_PACKAGE_DIR': '/ambient/coleco',
                    'FES_COLECO_PACKAGE_SELECTION': '/ambient/coleco-selection',
                }), \
                    patch.object(build.sys, 'argv',
                                 ['build.py', 'image', '--profile', 'native-integration-dev']), \
                    patch.object(build, 'validate', return_value=revisions), \
                    patch.object(build, 'source_checkout', side_effect=fake_checkout), \
                    patch.object(build, 'git', return_value='module test\n'), \
                    patch.object(build.tomllib, 'loads', side_effect=fake_loads), \
                    patch.object(build.subprocess, 'check_output',
                                 side_effect=lambda args, **kwargs:
                                 b'lock' if args[0] == 'git'
                                 else 'go version go1.26.5 linux/amd64\n'), \
                    patch.object(build.platform, 'system', return_value='Linux'), \
                    patch.object(build.platform, 'machine', return_value='x86_64'), \
                    patch.object(build.shutil, 'which', return_value='/usr/bin/tool'), \
                    patch.object(build.subprocess, 'run',
                                 return_value=subprocess.CompletedProcess([], 1)), \
                    patch.object(build, 'run', side_effect=fake_run), \
                    patch.object(build, 'locked_diagnostics', no_diagnostics), \
                    patch.object(build, 'resolve_packages_for_action',
                                 return_value=packages), \
                    patch.object(build, 'build_fingerprint',
                                 return_value=('base-fingerprint', {'sources': {}})), \
                    patch.object(build, 'host_fingerprint',
                                 return_value=('host-fingerprint', {})), \
                    patch.object(build, 'image_fingerprint',
                                 return_value=('image-fingerprint', {})), \
                    patch.object(build, 'reuse_status',
                                 return_value=(False, 'test miss')):
                with self.assertRaises(StopAfterFetch):
                    build.main()

        self.assertEqual(captured['env']['FES_PACKAGE_IDS'],
                         'fes.pong,fes.zx81,fes.coleco')
        for core_id in ('fes.pong', 'fes.zx81', 'fes.coleco'):
            prefix = core_id.upper().replace('.', '_')
            self.assertEqual(captured['env'][prefix + '_PACKAGE_DIR'],
                             str(Path('/packages') / core_id))
            self.assertEqual(
                captured['env'][prefix + '_PACKAGE_SELECTION'],
                str(Path('/records') /
                    (core_id.replace('.', '-') + '.package-selection.toml')))
        self.assertNotEqual(captured['env']['FES_PACKAGE_IDS'], 'ambient')
        self.assertNotEqual(captured['env']['FES_PONG_PACKAGE_DIR'], '/ambient/pong')

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

    def test_completed_backup_cleanup_failure_handoffs_before_deletion(self):
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

            original_replace = Path.replace
            selection_replacements = 0

            def fail_after_first_selection(source, target):
                nonlocal selection_replacements
                result = original_replace(source, target)
                target = Path(target)
                if (target.parent == output and
                        target.name.endswith('.package-selection.toml')):
                    selection_replacements += 1
                    if selection_replacements == 1:
                        raise RuntimeError('injected publish failure')
                return result

            original_remove = build._remove_sealed_tree
            cleanup_path = None

            def fail_during_completed_backup_cleanup(path):
                nonlocal cleanup_path
                path = Path(path)
                if (cleanup_path is None and path.parent == output and
                        (path.name == '.package-generation.previous' or
                         path.name.startswith('.package-generation.previous.'))):
                    cleanup_path = path
                    payload = path / 'core-packages' / ('a' * 64) / 'core.rbf'
                    payload.parent.chmod(0o755)
                    payload.unlink()
                    raise OSError('injected completed backup cleanup failure')
                return original_remove(path)

            with patch.object(Path, 'replace', new=fail_after_first_selection), \
                    patch.object(build, '_remove_sealed_tree',
                                  new=fail_during_completed_backup_cleanup), \
                    self.assertRaisesRegex(RuntimeError, 'injected publish failure'):
                build.publish_package_outputs(new_packages, new_built, output)

            self.assertEqual(package_file_snapshot(output), old_files)
            self.assertIsNotNone(cleanup_path)
            self.assertFalse(
                (output / '.package-generation.previous').exists(),
                'completed backup cleanup left a malformed canonical backup')

            build.publish_package_outputs(new_packages, new_built, output)
            self.assertEqual(build.verify_package_outputs(output, new_packages), [
                'fes-pong.package-selection.toml',
                'core-packages/' + 'c' * 64 + '/manifest.toml',
                'core-packages/' + 'c' * 64 + '/core.rbf',
                'fes-zx81.package-selection.toml',
                'core-packages/' + 'd' * 64 + '/manifest.toml',
                'core-packages/' + 'd' * 64 + '/core.rbf',
            ])

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
            with self.assertRaisesRegex(ValueError, 'stale top-level'):
                build.verify_package_only_outputs(output, package)
            build.remove_stale_parent_outputs(output)
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
