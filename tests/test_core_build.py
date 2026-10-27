"""The selected profile controls the complete installed FPGA core set."""
from contextlib import ExitStack, contextmanager
import fcntl
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
from environment import build_environment, owned_package_environment_names


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


def add_video_generation(root, packages, built, label):
    package = packages[0]
    package_id = package['inputs']['selection']['package_id']
    directory = root / ('video-' + label)
    shell = directory / package_id
    shell.mkdir(parents=True)
    parts = []
    for profile in ('direct', 'scanlines'):
        data = (label + '-' + profile).encode()
        identity = hashlib.sha256(data).hexdigest()
        relative = package_id + '/' + identity + '.tar'
        (directory / relative).write_bytes(data)
        parts.append(dict(profile=profile, part_id=identity, archive_path=relative,
                          archive_sha256=identity, archive_size=len(data)))
    index = dict(version=1, packages=[dict(package_id=package_id, parts=parts)])
    encoded = (json.dumps(index, sort_keys=True, separators=(',', ':')) + '\n').encode()
    (directory / 'index.json').write_bytes(encoded)
    build._seal_package_tree(directory)
    package['video_parts'] = dict(directory=directory, index_path=directory / 'index.json',
                                 inputs=dict(index=index, index_sha256=hashlib.sha256(encoded).hexdigest()))
    package['inputs']['video_parts'] = package['video_parts']['inputs']
    build.prepare_video_selection(packages, root / ('merged-' + label))
    external = root / ('external-' + label + '.json')
    external.write_bytes(encoded)
    built['fes-core-video-parts.json'] = external
    return directory


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
    def test_video_generation_replacement_failure_restores_both_artifact_trees(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            output = root / 'output'
            output.mkdir()
            old, old_built = make_package_generation(root, 'old-video', ('a' * 64,))
            add_video_generation(root, old, old_built, 'old')
            names = build.publish_package_outputs(old, old_built, output)
            before = {name: (output / name).read_bytes() for name in names}
            new, new_built = make_package_generation(root, 'new-video', ('b' * 64,))
            add_video_generation(root, new, new_built, 'new')
            original = Path.replace
            failed = False

            def fail_video_selection(path, destination):
                nonlocal failed
                result = original(path, destination)
                if Path(destination) == output / 'fes-core-video-parts.json' and not failed:
                    failed = True
                    raise OSError('injected video selection replacement failure')
                return result

            with patch.object(Path, 'replace', fail_video_selection), self.assertRaisesRegex(OSError, 'injected'):
                build.publish_package_outputs(new, new_built, output)
            self.assertEqual(build.verify_package_outputs(output, old), names)
            self.assertEqual({name: (output / name).read_bytes() for name in names}, before)
            build.publish_package_outputs(new, new_built, output)
            self.assertEqual(build.verify_package_outputs(output, new), build.package_output_names(new[0]) + build.video_output_names(new))

    def test_video_files_and_selection_are_closed_and_removed_with_the_package_set(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            output = root / 'output'
            output.mkdir()
            packages, built = make_package_generation(root, 'video', ('a' * 64,))
            add_video_generation(root, packages, built, 'one')
            build.publish_package_outputs(packages, built, output)
            self.assertIn('FES_VIDEO_PARTS_DIR=', ' '.join(build.package_arguments(packages)))
            archive = next((output / 'core-video-parts' / ('a' * 64)).glob('*.tar'))
            archive.chmod(0o644)
            archive.write_bytes(b'changed')
            archive.chmod(0o444)
            with self.assertRaisesRegex(ValueError, 'digest|size'):
                build.verify_package_outputs(output, packages)
            # A verified fresh publication repairs the incomplete old generation.
            build.publish_package_outputs(packages, built, output)
            build.publish_package_state(None, None, output)
            self.assertFalse((output / 'core-video-parts').exists())
            self.assertFalse((output / 'fes-core-video-parts.json').exists())

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
            ['fes.menu'])
        self.assertEqual(build.selected_packages(repository_profile, 'native-integration-dev'),
                         ('fes.menu',))

    def test_selection_overrides_do_not_leak_from_shell(self):
        with patch.dict('os.environ', {'NATIVE_RUNTIME_SYSTEMS': 'pong',
                                      'FES_PONG_PACKAGE_DIR': '/untrusted-package',
                                      'FES_PONG_PACKAGE_SELECTION': '/untrusted-selection',
                                      'FES_ZX81_PACKAGE_DIR': '/untrusted-zx81-package',
                                      'FES_ZX81_PACKAGE_SELECTION': '/untrusted-zx81-selection',
                                      'FES_COLECO_PACKAGE_DIR': '/untrusted-coleco-package',
                                      'FES_COLECO_PACKAGE_SELECTION': '/untrusted-coleco-selection',
                                      'FES_SMS_PACKAGE_DIR': '/untrusted-sms-package',
                                      'FES_SMS_PACKAGE_SELECTION': '/untrusted-sms-selection',
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
        self.assertFalse('FES_SMS_PACKAGE_DIR' in env)
        self.assertFalse('FES_SMS_PACKAGE_SELECTION' in env)
        self.assertFalse('FES_PACKAGE_IDS' in env)
        self.assertFalse('FES_TOOLCHAIN_CACHE_ROOT' in env)

    def test_build_environment_drops_ambient_optional_package_inputs(self):
        ambient = {
            'FES_UNAVAILABLE_CORES': '/tmp/previous-unavailable-cores.json',
            'FES_ATARI_ST_PACKAGE_DIR': '/tmp/previous-package',
            'FES_ATARI_ST_PACKAGE_SELECTION': '/tmp/previous-selection',
            'FES_RAMTEST_PACKAGE_DIR': '/tmp/previous-ramtest',
            'FES_RAMTEST_PACKAGE_SELECTION': '/tmp/previous-ramtest-selection',
            'CC': 'clang',
        }
        for name in owned_package_environment_names():
            ambient.setdefault(name, '/tmp/ambient-' + name)
        with patch.dict('os.environ', ambient, clear=False):
            env = build_environment()
        for name in owned_package_environment_names():
            self.assertNotIn(name, env)
        self.assertEqual(env['CC'], 'clang')

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

    def test_missing_video_shell_requires_an_explicit_rebuild(self):
        from types import SimpleNamespace
        from factory_video_parts import MissingVideoShell
        calls = []

        def fake_resolve(source, packages_revision, selection_path, force=False, env=None, recipe=None):
            calls.append(force)
            return {'directory': Path('/pkg'), 'inputs': {'selection': {'package_id': 'fes.coleco'}}}

        def missing_shell(*args, **kwargs):
            raise MissingVideoShell('resolved video package has no frozen companion')

        recipe = SimpleNamespace(video_profiles=('direct',))
        revisions = {'misteross': 'a' * 40, 'mister-packages': 'b' * 40}
        with patch.object(build, 'source_checkout', return_value=Path('/work/misteross')), \
             patch.object(build.core_bundle, 'resolve_core_package', side_effect=fake_resolve), \
             patch('factory_video_parts.resolve_video_parts', side_effect=missing_shell):
            with self.assertRaises(MissingVideoShell) as caught:
                build.resolve_selected_package(
                    revisions, Path('/out/fes-coleco.package-selection.toml'), {}, recipe=recipe)
        self.assertEqual(calls, [False])
        self.assertIn('explicit rebuild', str(caught.exception))
        with patch.object(build, 'source_checkout', return_value=Path('/work/misteross')), \
             patch.object(build.core_bundle, 'resolve_core_package', side_effect=fake_resolve), \
             patch('factory_video_parts.resolve_video_parts', side_effect=missing_shell):
            with self.assertRaises(MissingVideoShell) as forced:
                build.resolve_selected_package(
                    revisions, Path('/out/fes-coleco.package-selection.toml'), {}, force=True, recipe=recipe)
        self.assertEqual(calls, [False, True])
        self.assertNotIn('explicit rebuild', str(forced.exception))

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
        self.assertEqual(result, (({'recipe': 'fes.zx81'}, {'recipe': 'fes.coleco'}), ()))
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
        sms_only = ({
            'directory': Path('/packages/sms'),
            'selection_path': Path('/records/fes-sms.package-selection.toml'),
            'inputs': {'selection': {'core_id': 'fes.sms', 'package_id': 'd' * 64},
                       'selection_sha256': 'a' * 64, 'manifest_sha256': 'b' * 64,
                       'core_rbf_sha256': 'c' * 64},
        },)
        self.assertEqual(build.package_arguments(sms_only), [
            'FES_PACKAGE_IDS=fes.sms',
            'FES_SMS_PACKAGE_DIR=/packages/sms',
            'FES_SMS_PACKAGE_SELECTION=/records/fes-sms.package-selection.toml',
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

    def test_external_native_policy_is_part_of_image_recipe_identity(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            policy = root / 'image/build/native-inputs.toml'
            policy.parent.mkdir(parents=True)
            policy.write_text('[idle_rbf]\nsha256="old"\n')
            files = build.image_recipe_files(root)
            self.assertIn(policy, files)
            license = root / 'image/licenses/fes.ramtest/COPYING'
            license.parent.mkdir(parents=True)
            license.write_text('license')
            self.assertIn(license, build.image_recipe_files(root))
            before = build.digest(policy)
            policy.write_text('[idle_rbf]\nsha256="new"\n')
            self.assertNotEqual(before, build.digest(policy))

    def test_clean_package_only_image_fetch_passes_complete_package_tuple(self):
        self.check_clean_fetch(False)

    def test_artifact_assembly_never_invokes_component_producers(self):
        self.check_clean_fetch(True)

    def test_artifact_assembly_and_verification_publish_a_receipted_image(self):
        """Exercise the complete parent path while replacing the expensive image child."""
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            image_root = root / 'image'
            (root / 'profiles').mkdir()
            (root / 'profiles/native-integration-dev.toml').write_text(
                "host_os = 'linux'\nhost_arch = 'amd64'\nversion = 'test'\n"
                "native_image_mode = 'package-only'\n"
                "[[fpga_packages]]\ncore_id = 'fes.menu'\n")
            (image_root / 'build').mkdir(parents=True)
            shutil.copyfile(build.IMAGE / 'build/native-inputs.toml',
                            image_root / 'build/native-inputs.toml')
            shutil.copyfile(build.IMAGE / 'build/target-image.sources.lock.toml',
                            image_root / 'build/target-image.sources.lock.toml')

            identity = 'a' * 64
            artifact_root = root / 'artifacts'
            package_dir = artifact_root / 'core-packages' / identity
            package_dir.mkdir(parents=True)
            (package_dir / 'manifest.toml').write_bytes(b'menu manifest')
            (package_dir / 'core.rbf').write_bytes(b'menu firmware')
            selection_path = artifact_root / 'fes-menu.package-selection.toml'
            selection_path.parent.mkdir(parents=True, exist_ok=True)
            selection_path.write_text(f'core_id = "fes.menu"\npackage_id = "{identity}"\n')

            digest = lambda path: hashlib.sha256(Path(path).read_bytes()).hexdigest()
            inputs = {'selection': {'core_id': 'fes.menu', 'package_id': identity},
                      'selection_sha256': digest(selection_path),
                      'manifest_sha256': digest(package_dir / 'manifest.toml'),
                      'core_rbf_sha256': digest(package_dir / 'core.rbf')}
            package = {'directory': package_dir, 'selection_path': selection_path, 'inputs': inputs}
            artifact_manifest = {
                'format': 1,
                'provenance': {'fogcast': '1' * 40, 'runtime': '2' * 40},
                'binaries': {name: '3' * 64 for name in (
                    'mister-agent', 'mister-runtime', 'fogcast-kit', 'fogcast-tenfoot',
                    'fes-kit-data', 'fes-kit-software')},
                'packages': [inputs],
            }

            revisions = {'FogCast': '4' * 40, 'libmister-runtime': '5' * 40,
                         'mister-packages': '6' * 40, 'misteross': '7' * 40}
            calls = []
            child_output = image_root / 'build/output/target-image/native-dev'
            image_bytes = b'assembled root filesystem fixture'
            image_sha = hashlib.sha256(image_bytes).hexdigest()

            class FakeDiagnostics:
                @contextmanager
                def measure(self, *args):
                    yield
                def cache(self, *args):
                    pass

            @contextmanager
            def no_diagnostics(*args, **kwargs):
                yield None, FakeDiagnostics()

            def fake_checkout(name, revision, *args):
                checkout = root / name
                (checkout / 'build').mkdir(parents=True, exist_ok=True)
                lock = checkout / 'build/native-runtime.inputs.lock.toml'
                if not lock.exists():
                    lock.write_text('initial lock')
                return checkout

            def fake_git(path, *args):
                if args[:1] == ('show',):
                    return 'module github.com/DeanoC/FogCast\n'
                if args[:1] == ('rev-parse',):
                    return '8' * 40
                if args[:1] == ('status',):
                    return ''
                return 'fixture'

            def fake_stage(diagnostics, stage, args, **kwargs):
                args = [str(arg) for arg in args]
                calls.append((stage, args))
                if 'target-image-native' in args:
                    child_output.mkdir(parents=True, exist_ok=True)
                    (child_output / 'linux.img').write_bytes(image_bytes)
                    (child_output / 'reproducibility.txt').write_text(
                        'image_passes=2\nrun_1_sha256=' + image_sha + '\nrun_2_sha256=' +
                        image_sha + '\ntoolchain_key=test-toolchain\ntoolchain_sha256=' +
                        '9' * 64 + '\n')
                    (child_output / 'manifest.tsv').write_text('fixture manifest\n')
                    (child_output / 'library-report.tsv').write_text('fixture libraries\n')
                    (child_output / 'fes-menu.package-selection.toml').write_bytes(
                        selection_path.read_bytes())
                if 'target-image-native-qemu-smoke' in args:
                    (child_output / 'qemu-smoke.log').write_text('fixture QEMU smoke\n')

            def fake_run(*args, **kwargs):
                return subprocess.CompletedProcess(args[0] if args else [], 0)

            output = root / 'out/native-integration-dev'
            with ExitStack() as stack:
                for context in (
                    patch.object(build, 'ROOT', root),
                    patch.object(build, 'IMAGE', image_root),
                    patch.object(build.sys, 'argv', ['build.py']),
                    patch.object(build, 'validate', return_value=revisions),
                    patch.object(build, 'selected_packages', return_value=('fes.menu',)),
                    patch.object(build, 'source_checkout', side_effect=fake_checkout),
                    patch.object(build, 'git', side_effect=fake_git),
                    patch.object(build.subprocess, 'check_output', return_value='go version go1.26.5 linux/amd64\n'),
                    patch.object(build.subprocess, 'run', side_effect=fake_run),
                    patch.object(build.platform, 'system', return_value='Linux'),
                    patch.object(build.platform, 'machine', return_value='x86_64'),
                    patch.object(build.platform, 'build_bluetooth'),
                    patch.object(build, 'build_environment', return_value={}),
                    patch.object(build.shutil, 'which', return_value='/usr/bin/tool'),
                    patch.object(build, 'locked_diagnostics', no_diagnostics),
                    patch.object(build, 'run', return_value=None),
                    patch.object(build, 'run_stage', side_effect=fake_stage),
                    patch.object(build, 'build_fingerprint', return_value=(
                        'source-key', {'sources': revisions, 'profile': {'version': 'test'},
                                       'recipe': {'build': 'fixture'}, 'image_recipe': {'os': 'fixture'},
                                       'image_toolchain_key': 'test-toolchain'})),
                    patch.object(build, 'host_fingerprint', return_value=('host-key', {})),
                    patch('image_artifacts.load', return_value=(artifact_manifest, (package,))),
                    patch('image_artifacts.stage', return_value=(artifact_manifest, (package,))),
                    patch.object(build, 'toolchain_key', return_value='test-toolchain'),
                    patch.object(build, 'validate_image_passes', return_value=2),
                ):
                    stack.enter_context(context)
                with patch.object(build.sys, 'argv', [
                        'build.py', 'image', '--profile', 'native-integration-dev',
                        '--artifacts', str(artifact_root)]):
                    build.main()

                (output / 'fogcast').write_bytes(b'host CLI')
                (output / 'fogcast-api').write_bytes(b'host API')
                build.write_receipt(output, 'host', 'host-key',
                                    ['fogcast', 'fogcast-api'], os_name='linux', arch='amd64')
                with patch.object(build.sys, 'argv', [
                        'build.py', 'verify', '--profile', 'native-integration-dev',
                        '--artifacts', str(artifact_root)]):
                    build.main()

            self.assertTrue((output / 'image.json').is_file())
            self.assertTrue((output / 'verification.json').is_file())
            self.assertEqual(digest(output / 'linux.img'), image_sha)
            self.assertEqual(build.recorded_image_fingerprint(
                json.loads((output / 'inputs.json').read_text())),
                json.loads((output / 'image.json').read_text())['inputs'])
            commands = [args for _, args in calls]
            self.assertTrue(any('target-image-native' in args for args in commands))
            self.assertTrue(any('target-image-native-verify' in args for args in commands))
            self.assertTrue(any('target-image-native-qemu-smoke' in args for args in commands))
            self.assertFalse(any('build-agent' in args or 'build-fogcast-kit' in args
                                 for args in commands))

    def check_clean_fetch(self, artifacts):
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
        artifact_manifest = {'provenance': {'runtime': '5' * 40, 'fogcast': '6' * 40},
                             'binaries': {'mister-runtime': 'a' * 64}}

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
            image = root / 'image'
            (image / 'build').mkdir(parents=True)
            shutil.copyfile(build.IMAGE / 'build/native-inputs.toml',
                            image / 'build/native-inputs.toml')
            (image / 'build/target-image.sources.lock.toml').write_text(
                '[container]\nimage = "docker.io/library/debian:12.11-slim"\n'
                'platform = "linux/amd64"\n' +
                'digest = "sha256:' + 'a' * 64 + '"\n')
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

            bluetooth_calls = []

            def fake_bluetooth(*args, **kwargs):
                if not artifacts:
                    raise AssertionError('bluetooth agent is built with kit software')
                bluetooth_calls.append(args)

            def fake_run(args, **kwargs):
                if artifacts:
                    self.assertNotIn('build-agent', args)
                    self.assertNotIn('build-fogcast-kit', args)
                if (len(args) >= 3 and
                        Path(args[0]).name == 'target-image-container.sh' and
                        args[1:3] == ['fetch', '/work/scripts/fetch-target-image-sources.sh']):
                    with (image / 'build/cache/.kit-sdk-sources.lock').open('a') as competing_lock:
                        with self.assertRaises(BlockingIOError):
                            fcntl.flock(competing_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    captured['env'] = kwargs['env']
                    captured['assembly_lock'] = real_tomllib_loads(
                        (fogcast / 'build/native-runtime.inputs.lock.toml').read_text())
                    raise StopAfterFetch
                return subprocess.CompletedProcess(args, 0)

            revisions = {
                'FogCast': '1' * 40,
                'libmister-runtime': '2' * 40,
                'mister-packages': '3' * 40,
                'misteross': '4' * 40,
            }
            with ExitStack() as stack:
                for context in (
                    patch.dict(os.environ, {
                        'FES_PACKAGE_IDS': 'ambient',
                        'FES_PONG_PACKAGE_DIR': '/ambient/pong',
                        'FES_PONG_PACKAGE_SELECTION': '/ambient/pong-selection',
                        'FES_ZX81_PACKAGE_DIR': '/ambient/zx81',
                        'FES_ZX81_PACKAGE_SELECTION': '/ambient/zx81-selection',
                        'FES_COLECO_PACKAGE_DIR': '/ambient/coleco',
                        'FES_COLECO_PACKAGE_SELECTION': '/ambient/coleco-selection',
                        'FES_SMS_PACKAGE_DIR': '/ambient/sms',
                        'FES_SMS_PACKAGE_SELECTION': '/ambient/sms-selection',
                    }),
                    patch.object(build.sys, 'argv',
                                 ['build.py', 'image' if artifacts else 'build', '--profile', 'native-integration-dev'] +
                                 (['--artifacts', '/explicit-artifacts'] if artifacts else [])),
                    patch.object(build, 'IMAGE', image),
                    patch('image_artifacts.load', return_value=(artifact_manifest, packages)),
                    patch('image_artifacts.stage', return_value=(artifact_manifest, packages)),
                    patch.object(build, 'validate', return_value=revisions),
                    patch.object(build, 'source_checkout', side_effect=fake_checkout),
                    patch.object(build, 'git', side_effect=lambda root, *args:
                                 "[mister_runtime]\nmount_path = '/runtime-source'\n"
                                 if args[-1] == 'HEAD:build/native-runtime.inputs.lock.toml'
                                 else 'module test\n'),
                    patch.object(build.tomllib, 'loads', side_effect=fake_loads),
                    patch.object(build.subprocess, 'check_output',
                                 side_effect=lambda args, **kwargs:
                                 b'lock' if args[0] == 'git'
                                 else 'go version go1.26.5 linux/amd64\n'),
                    patch.object(build.platform, 'system', return_value='Linux'),
                    patch.object(build.platform, 'machine', return_value='x86_64'),
                    patch.object(build.platform, 'build_kit_software',
                                 side_effect=AssertionError('must not compile') if artifacts else None),
                    patch.object(build.platform, 'build_bluetooth', side_effect=fake_bluetooth),
                    patch.object(build, 'write_receipt'),
                    patch.object(build, 'publish_host_inputs'),
                    patch.object(build.shutil, 'which', return_value='/usr/bin/tool'),
                    patch.object(build.subprocess, 'run',
                                 return_value=subprocess.CompletedProcess([], 1)),
                    patch.object(build, 'run', side_effect=fake_run),
                    patch.object(build, 'locked_diagnostics', no_diagnostics),
                    patch.object(build, 'resolve_packages_for_action',
                                 return_value=(packages, ()),
                                 side_effect=AssertionError('must not resolve producers') if artifacts else None),
                    patch.object(build, 'build_fingerprint',
                                 return_value=('base-fingerprint', {'sources': {}})),
                    patch.object(build, 'host_fingerprint',
                                 return_value=('host-fingerprint', {})),
                    patch.object(build, 'image_fingerprint',
                                 return_value=('image-fingerprint', {})),
                    patch.object(build, 'reuse_status',
                                 return_value=(False, 'test miss')),
                ):
                    stack.enter_context(context)
                with self.assertRaises(StopAfterFetch):
                    build.main()

        self.assertEqual(len(bluetooth_calls), 1 if artifacts else 0)
        self.assertEqual(captured['assembly_lock']['mister_runtime']['commit'], ('5' if artifacts else '2') * 40)
        self.assertEqual(captured['assembly_lock']['idle_rbf'],
                         tomllib.loads((build.IMAGE / 'build/native-inputs.toml').read_text())['idle_rbf'])
        self.assertEqual(captured['assembly_lock']['splash_rbf'],
                         tomllib.loads((build.IMAGE / 'build/native-inputs.toml').read_text())['splash_rbf'])
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
        self.assertNotIn('FES_SMS_PACKAGE_DIR', captured['env'])
        self.assertNotIn('FES_SMS_PACKAGE_SELECTION', captured['env'])

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
