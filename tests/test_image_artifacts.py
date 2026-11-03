import contextlib
import hashlib
import json
from pathlib import Path
import sys
import tempfile
import os
import shutil
import subprocess
import struct
import unittest
from types import SimpleNamespace
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import image_artifacts as artifacts
import build


MENU_INTERFACES = (('fes.video.fixed-720p60', 1, 0), ('fes.memory.hps-ddr', 1, 0),
                   ('fes.video.menu-display', 1, 0))
DE25_MENU_INTERFACES = (('fes.video.fixed-720p60', 1, 0), ('fes.video.menu-display', 1, 0),
                        ('fes.fabric.de25-scanout', 1, 1))
needs_readelf = unittest.skipUnless(shutil.which('readelf'), 'needs readelf from binutils')


def static_arm_elf(architecture='armv7'):
    data = bytearray(256)
    if architecture == 'aarch64':
        data[:16] = b'\x7fELF\x02\x01\x01' + bytes(9)
        struct.pack_into('<HHIQQQIHHHHHH', data, 16, 2, 183, 1, 0x400080, 64, 0,
                         0, 64, 56, 1, 0, 0, 0)
        struct.pack_into('<IIQQQQQQ', data, 64, 1, 5, 0, 0x400000, 0x400000,
                         len(data), len(data), 4096)
        return data
    data[:16] = b'\x7fELF\x01\x01\x01' + bytes(9)
    struct.pack_into('<HHIIIIIHHHHHH', data, 16, 2, 40, 1, 0x10054, 52, 0,
                     0x05000000, 52, 32, 1, 0, 0, 0)
    struct.pack_into('<IIIIIIII', data, 52, 1, 0, 0x10000, 0x10000,
                     len(data), len(data), 5, 4096)
    return data


def runtime_elf(interfaces, architecture='armv7'):
    """ARM runtime whose .fes_menu_interfaces section lists interfaces; None omits it."""
    names = [b'', b'.shstrtab'] + ([] if interfaces is None else [b'.fes_menu_interfaces'])
    strings = b'\0'.join(names) + b'\0'
    records = b''.join(f'{name}={major}.{minor}\0'.encode() for name, major, minor in interfaces or ())
    if architecture == 'aarch64':
        data = bytearray(64) + strings + (b'' if interfaces is None else records + b'\0')
        sections = [bytes(64), struct.pack('<IIQQQQIIQQ', 1, 3, 0, 0, 64, len(strings), 0, 0, 1, 0)]
        if interfaces is not None:
            sections.append(struct.pack('<IIQQQQIIQQ', len(b'\0.shstrtab\0'), 1, 2, 0, 64 + len(strings),
                                        len(records) + 1, 0, 0, 1, 0))
        data[:16] = b'\x7fELF\x02\x01\x01' + bytes(9)
        struct.pack_into('<HHIQQQIHHHHHH', data, 16, 2, 183, 1, 0, 0, len(data),
                         0, 64, 0, 0, 64, len(sections), 1)
        return bytes(data) + b''.join(sections)
    data = bytearray(52) + strings + (b'' if interfaces is None else records + b'\0')
    sections = [bytes(40), struct.pack('<10I', 1, 3, 0, 0, 52, len(strings), 0, 0, 1, 0)]
    if interfaces is not None:
        sections.append(struct.pack('<10I', len(b'\0.shstrtab\0'), 1, 2, 0, 52 + len(strings),
                                    len(records) + 1, 0, 0, 1, 0))
    data[:16] = b'\x7fELF\x01\x01\x01' + bytes(9)
    struct.pack_into('<HHIIIIIHHHHHH', data, 16, 2, 40, 1, 0, 0, len(data),
                     0x05000000, 52, 0, 0, 40, len(sections), 1)
    return bytes(data) + b''.join(sections)


def menu_manifest(interfaces, board='de10-nano'):
    return ('[target]\nplatform = "' + board.replace('-', '_') + '"\n\n' +
            ''.join(f'[[interfaces]]\nid = "{name}"\nmajor = {major}\nminor = {minor}\nrequired = true\n\n'
                    for name, major, minor in interfaces))


def inspect_only(run=subprocess.run):
    """Allow readelf inspection; any other subprocess would build something."""
    def checked(args, *rest, **kwargs):
        if list(args)[:1] != ['readelf']:
            raise AssertionError('must not compile')
        return run(args, *rest, **kwargs)
    return checked


class ArtifactFixture(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / 'bin').mkdir()
        for name in artifacts.BINARIES:
            binary = (static_arm_elf() if name in ('fes-kit-data', 'fes-kit-software')
                      else runtime_elf(MENU_INTERFACES) if name == 'mister-runtime'
                      else b'\x7fELF\x01\x01' + b'\0' * 12 + b'\x28\0' + name.encode())
            (self.root / 'bin' / name).write_bytes(binary)
        identity = 'a' * 64
        core = self.root / 'core-packages' / identity
        core.mkdir(parents=True)
        (core / 'manifest.toml').write_text(menu_manifest(MENU_INTERFACES))
        (core / 'core.rbf').write_bytes(b'firmware')
        selection = self.root / 'fes-menu.package-selection.toml'
        selection.write_text('fixture selection')
        selection.chmod(0o444)
        self.manifest = {'format': 1, 'provenance': {'fogcast': '1' * 40, 'runtime': '2' * 40},
            'binaries': {n: artifacts.sha(self.root / 'bin' / n) for n in artifacts.BINARIES},
            'packages': [{'selection': {'core_id': 'fes.menu', 'package_id': identity},
                'selection_sha256': artifacts.sha(selection),
                'manifest_sha256': artifacts.sha(core / 'manifest.toml'),
                'core_rbf_sha256': artifacts.sha(core / 'core.rbf')}]}
        self.write_manifest()

    def write_manifest(self):
        (self.root / 'artifacts.json').write_text(json.dumps(self.manifest))

    def replace_runtime(self, binary):
        (self.root / 'bin/mister-runtime').write_bytes(binary)
        self.manifest['binaries']['mister-runtime'] = artifacts.sha(self.root / 'bin/mister-runtime')
        self.write_manifest()

    def select_de25(self):
        for name in artifacts.BINARIES:
            data = runtime_elf(DE25_MENU_INTERFACES, 'aarch64') if name == 'mister-runtime' else static_arm_elf('aarch64')
            (self.root / 'bin' / name).write_bytes(data)
        core = self.root / 'core-packages' / ('a' * 64)
        (core / 'manifest.toml').write_text(menu_manifest(DE25_MENU_INTERFACES, 'de25-nano'))
        self.manifest.update(board='de25-nano', architecture='aarch64')
        self.manifest['binaries'] = {name: artifacts.sha(self.root / 'bin' / name) for name in artifacts.BINARIES}
        self.manifest['packages'][0]['manifest_sha256'] = artifacts.sha(core / 'manifest.toml')
        self.write_manifest()

class ImageArtifactsTests(ArtifactFixture):
    def test_explicit_de25_reaches_all_package_recipe_lookups(self):
        self.select_de25()
        selection = self.manifest['packages'][0]['selection']
        selection.update(format=2, kind='core-package', payload_sha256=self.manifest['packages'][0]['core_rbf_sha256'],
                         misteross_revision='1' * 40, mister_packages_revision='2' * 40,
                         install_path='/usr/share/mister-runtime/core-packages/' + selection['package_id'])
        path = self.root / 'fes-menu.package-selection.toml'
        path.chmod(0o644)
        path.write_text('\n'.join(name + ' = ' + json.dumps(value) for name, value in selection.items()) + '\n')
        path.chmod(0o444)
        self.manifest['packages'][0]['selection_sha256'] = artifacts.sha(path)
        self.write_manifest()
        original = build.recipe_for
        def selected(core, *, board='de10-nano'):
            self.assertEqual(board, 'de25-nano')
            return original(core, board=board)
        with patch.object(build, 'recipe_for', side_effect=selected) as lookup:
            _, packages = artifacts.load(self.root, board='de25-nano')
            output = self.root / 'published'; output.mkdir()
            build.publish_package_state(packages, path, output, board='de25-nano')
            build.verify_package_outputs(output, packages, board='de25-nano')
        self.assertGreaterEqual(lookup.call_count, 3)

    @needs_readelf
    def test_explicit_de25_stage_uses_aarch64_and_preserves_source_binding(self):
        self.select_de25()
        destination = self.root / 'snapshot'
        destination.mkdir()
        manifest, packages = artifacts.stage(self.root, destination, board='de25-nano')
        self.assertEqual(manifest['board'], 'de25-nano')
        self.assertEqual(len(packages), 1)
        for name in artifacts.BINARIES:
            self.assertEqual(artifacts.sha(destination / 'bin' / (name + '-linux-aarch64')),
                             self.manifest['binaries'][name])
            self.assertFalse((destination / 'bin' / (name + '-linux-armv7')).exists())

    def test_foreign_board_artifacts_refused_before_staging_or_tools(self):
        self.select_de25()
        destination = self.root / 'missing-snapshot'
        with patch('subprocess.run', side_effect=AssertionError('must stop before inspection')):
            with self.assertRaisesRegex(ValueError, 'board or architecture'):
                artifacts.stage(self.root, destination)
        self.assertFalse(destination.exists())
        self.manifest.update(board='de10-nano', architecture='armv7')
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, 'board or architecture'):
            artifacts.load(self.root, board='de25-nano')

    @needs_readelf
    def test_de25_menu_for_other_board_is_refused_even_with_matching_hash(self):
        self.select_de25()
        core = self.root / 'core-packages' / ('a' * 64)
        (core / 'manifest.toml').write_text(menu_manifest(MENU_INTERFACES))
        self.manifest['packages'][0]['manifest_sha256'] = artifacts.sha(core / 'manifest.toml')
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, 'menu package target'):
            artifacts.load(self.root, board='de25-nano')

    @needs_readelf
    def test_de25_pack_requires_explicit_board_and_records_cpu(self):
        self.select_de25()
        selection = self.root / 'fes-menu.package-selection.toml'
        selection.chmod(0o644)
        selection.write_text('core_id = "fes.menu"\npackage_id = "' + 'a' * 64 + '"\n')
        selection.chmod(0o444)
        args = (self.root/'bin', self.root/'core-packages'/('a'*64), selection, '1'*40, '2'*40)
        with self.assertRaisesRegex(ValueError, 'ARM ELF'):
            artifacts.pack(*args, self.root/'wrong-default')
        self.assertFalse((self.root/'wrong-default').exists())
        manifest = artifacts.pack(*args, self.root/'packed', board='de25-nano')
        self.assertEqual((manifest['board'], manifest['architecture']), ('de25-nano', 'aarch64'))
        artifacts.load(self.root/'packed', board='de25-nano')

    @needs_readelf
    def test_stage_preserves_original_provenance_and_binary(self):
        destination = self.root / 'snapshot'
        destination.mkdir()
        artifacts.stage(self.root, destination)
        # Reuse of the private snapshot must replace its read-only binaries.
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

    def test_dynamic_bootstrap_helper_is_rejected_even_with_matching_hash(self):
        binary = self.root / 'bin/fes-kit-data'
        data = bytearray(binary.read_bytes())
        struct.pack_into('<I', data, 52, 3)  # PT_INTERP
        binary.write_bytes(data)
        self.manifest['binaries']['fes-kit-data'] = artifacts.sha(binary)
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, 'static ARM ELF: fes-kit-data'):
            artifacts.load(self.root)

    def test_missing_input_stops_before_tools_or_sources(self):
        with patch.object(sys, 'argv', ['build.py', 'image']), \
             patch.object(build, 'validate', side_effect=AssertionError('must stop first')):
            with self.assertRaisesRegex(ValueError, 'requires --artifacts'):
                build.main()

    def test_media_uses_artifact_fingerprint_and_runtime_lock(self):
        import media
        import tomllib
        repo = Path(__file__).resolve().parents[1]
        (self.root / 'profiles').mkdir()
        shutil.copyfile(repo / 'profiles/native-integration-dev.toml',
                        self.root / 'profiles/native-integration-dev.toml')
        (self.root / 'image/build').mkdir(parents=True)
        shutil.copyfile(repo / 'image/build/native-inputs.toml', self.root / 'image/build/native-inputs.toml')
        output = self.root / 'out/native-integration-dev'
        output.mkdir(parents=True)
        (output / 'inputs.json').write_text(json.dumps({'bootstrap_artifacts': self.manifest}))
        fogcast = self.root / 'FogCast'
        fogcast.mkdir()
        revisions = {'FogCast': '8' * 40, 'libmister-runtime': '9' * 40}
        with patch.object(build, 'validate', return_value=revisions), \
             patch.object(build, 'git', return_value='module fixture'), \
             patch.object(build, 'build_fingerprint', return_value=('base', {'recipe': 'fixture'})), \
             patch.object(build, 'host_fingerprint', return_value=('host', {})), \
             patch.object(build, 'source_checkout', return_value=fogcast), \
             patch.object(media.subprocess, 'check_output', return_value='go version fixture'):
            fingerprint, host, _, _ = media.select(self.root, 'native-integration-dev')
        self.assertEqual(fingerprint, build.bootstrap_fingerprint('base', {'recipe': 'fixture'}, self.manifest)[0])
        self.assertEqual(host, 'host')
        lock = tomllib.loads((fogcast / 'build/native-runtime.inputs.lock.toml').read_text())
        self.assertEqual(lock['mister_runtime']['commit'], self.manifest['provenance']['runtime'])

    @needs_readelf
    def test_pack_independent_binaries_without_source_or_image(self):
        selection = self.root / 'menu.toml'
        selection.write_text('core_id = "fes.menu"\npackage_id = "' + 'a' * 64 + '"\n')
        output = self.root / 'packed'
        with patch('subprocess.run', side_effect=inspect_only()):
            artifacts.pack(self.root / 'bin', self.root / 'core-packages' / ('a' * 64),
                           selection, '3' * 40, '4' * 40, output)
        manifest, _ = artifacts.load(output)
        self.assertEqual(manifest['binaries'], self.manifest['binaries'])
        self.assertEqual(manifest['provenance'], {'fogcast': '3' * 40, 'runtime': '4' * 40})
        self.assertEqual((output / 'fes-menu.package-selection.toml').stat().st_mode & 0o777, 0o444)

    @needs_readelf
    def test_runtime_without_menu_interface_records_is_refused_before_assembly(self):
        # An export from an image whose runtime predates these records stops
        # here, not after minutes of Buildroot in target-finalize (#1008).
        self.replace_runtime(runtime_elf(None))
        with self.assertRaisesRegex(ValueError, 'lacks valid menu interface records'):
            artifacts.load(self.root)
        with patch.object(sys, 'argv', ['build.py', 'image', '--artifacts', str(self.root)]), \
             patch.object(build, 'validate', side_effect=AssertionError('must stop first')):
            with self.assertRaisesRegex(ValueError, 'lacks valid menu interface records'):
                build.main()
        selection = self.root / 'menu.toml'
        selection.write_text('core_id = "fes.menu"\npackage_id = "' + 'a' * 64 + '"\n')
        output = self.root / 'packed'
        with self.assertRaisesRegex(ValueError, 'lacks valid menu interface records'):
            artifacts.pack(self.root / 'bin', self.root / 'core-packages' / ('a' * 64),
                           selection, '3' * 40, '4' * 40, output)
        self.assertFalse(output.exists())

    @needs_readelf
    def test_runtime_must_accept_every_menu_interface(self):
        manifest = self.root / 'core-packages' / ('a' * 64) / 'manifest.toml'
        for interfaces, missing in (
                (MENU_INTERFACES + (('fes.video.menu-blitter', 1, 0),), 'menu.fes.video.menu-blitter 1.0'),
                (MENU_INTERFACES[:2] + (('fes.video.menu-display', 1, 1),), 'menu.fes.video.menu-display 1.1')):
            with self.subTest(missing=missing):
                manifest.write_text(menu_manifest(interfaces))
                self.manifest['packages'][0]['manifest_sha256'] = artifacts.sha(manifest)
                self.write_manifest()
                with self.assertRaisesRegex(ValueError, 'does not accept its MENU interfaces: ' + missing):
                    artifacts.load(self.root)
        self.replace_runtime(runtime_elf(MENU_INTERFACES[:2] + (('fes.video.menu-display', 1, 2),)))
        artifacts.load(self.root)

    @needs_readelf
    def test_export_real_ext4_image(self):
        tools = os.defpath + ':/usr/sbin:/sbin'
        mke2fs = shutil.which('mke2fs', path=tools)
        debugfs = shutil.which('debugfs', path=tools)
        if not mke2fs or not debugfs:
            self.skipTest('e2fsprogs required for ext4 export integration')
        target = self.root / 'target'
        software = target / 'usr/share/fes/software'
        (software / 'bin').mkdir(parents=True)
        (target / 'usr/sbin').mkdir(parents=True)
        (software / 'manifest.json').write_text(json.dumps({'provenance': self.manifest['provenance']}))
        for name in artifacts.BINARIES:
            destination = target / 'usr/sbin' if name.startswith('fes-') else software / 'bin'
            shutil.copyfile(self.root / 'bin' / name, destination / name)
        subprocess.run([mke2fs, '-q', '-t', 'ext4', '-F', '-d', str(target),
                        str(self.root / 'linux.img'), '8M'], check=True,
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        _, packages = artifacts.load(self.root)
        fp, info = build.image_fingerprint('base', {}, packages)
        (self.root / 'inputs.json').write_text(json.dumps(info))
        (self.root / 'image.json').write_text(json.dumps({'fes_revision': '1' * 40,
            'inputs': fp, 'files': {name: artifacts.sha(self.root / name)
                                   for name in ('linux.img', 'inputs.json')}}))
        output = self.root / 'exported'
        with patch.dict(os.environ, {'PATH': tools}):
            artifacts.export(self.root, output)
        exported, _ = artifacts.load(output)
        self.assertEqual(exported['binaries'], self.manifest['binaries'])
        self.assertEqual(exported['packages'], self.manifest['packages'])
        self.assertEqual(exported['provenance'], self.manifest['provenance'])
        selection = output / 'fes-menu.package-selection.toml'
        self.assertEqual(selection.stat().st_mode & 0o777, 0o444)
        with patch.object(sys, 'argv', ['image_artifacts.py', 'check', str(output)]):
            artifacts.main()
        selection.chmod(0o644)
        with patch.object(sys, 'argv', ['image_artifacts.py', 'check', str(output)]):
            with self.assertRaisesRegex(ValueError, 'package selection is writable'):
                artifacts.main()
        with self.assertRaisesRegex(ValueError, 'already exists'):
            artifacts.export(self.root, output)

    @needs_readelf
    def test_artifact_identity_survives_receipt_roundtrip(self):
        manifest, packages = artifacts.load(self.root)
        info = {'bootstrap_artifacts': manifest}
        fp = hashlib.sha256(json.dumps(info, sort_keys=True).encode()).hexdigest()
        result, enriched = build.image_fingerprint(fp, info, packages)
        self.assertEqual(build.recorded_image_fingerprint(enriched), result)

    def test_artifact_cache_ignores_unconsumed_runtime_source_revision(self):
        revisions = {'FogCast': '1' * 40, 'libmister-runtime': '2' * 40,
                     'mister-packages': '3' * 40, 'misteross': '4' * 40}
        profile = {'version': '0.1.0', 'native_image_mode': 'package-only'}
        first_base, first_info = build.build_fingerprint(revisions, profile, 'go test')
        first, first_inputs = build.bootstrap_fingerprint(first_base, first_info, self.manifest)
        changed = dict(revisions, **{'libmister-runtime': '5' * 40})
        second_base, second_info = build.build_fingerprint(changed, profile, 'go test')
        second, second_inputs = build.bootstrap_fingerprint(second_base, second_info, self.manifest)
        self.assertEqual(first, second)
        self.assertEqual(first_inputs['sources'], {'FogCast': revisions['FogCast']})
        self.assertEqual(second_inputs['sources'], first_inputs['sources'])

        changed_artifacts = json.loads(json.dumps(self.manifest))
        changed_artifacts['binaries']['mister-runtime'] = 'f' * 64
        changed_key, _ = build.bootstrap_fingerprint(second_base, second_info, changed_artifacts)
        self.assertNotEqual(first, changed_key)


class DE25ImageCommandTests(unittest.TestCase):
    def test_board_profile_and_factory_arguments_fail_before_native_builder(self):
        cases = [(['image', '--profile', 'de25-nano'], 'independently selected'),
                 (['image', '--board', 'de25-nano', '--profile', 'native-integration-dev'], 'independently selected'),
                 (['image', '--kernel', '/fixture/kernel'], 'explicitly selected DE25')]
        cases += [([action, '--board', 'de25-nano'], 'source builds require --kernel')
                  for action in ('build', 'rebuild', 'dev')]
        with patch.object(build, 'validate', side_effect=AssertionError('native builder must not run')):
            for argv, reason in cases:
                with self.subTest(argv=argv), patch.object(sys, 'argv', ['build.py', *argv]):
                    with self.assertRaisesRegex(ValueError, reason):
                        build.main()

    def test_de25_host_and_doctor_use_shared_frontend(self):
        for action in ('host', 'doctor'):
            with self.subTest(action=action), patch.object(sys, 'argv', ['build.py', action, '--board', 'de25-nano']), \
                    patch.object(build, 'validate', side_effect=ValueError('shared source selection reached')) as select:
                with self.assertRaisesRegex(ValueError, 'shared source selection reached'):
                    build.main()
                self.assertEqual(select.call_args.args[1]['board'], 'de25-nano')

    def test_de25_source_commands_route_and_make_forwards_sdk_selection(self):
        import native_dev
        root = Path(__file__).resolve().parents[1]
        for action in ('build', 'rebuild', 'dev'):
            argv = ['build.py', action, '--board', 'de25-nano', '--kernel', '/kernel',
                    '--sdk', '/owner-sdk', '--de25-image', '/base']
            with self.subTest(action=action), patch.object(sys, 'argv', argv), \
                    patch.dict(os.environ, {'IMAGE_PASSES': '1'}), \
                    patch.object(native_dev, 'build_de25_development') as route:
                build.main()
                args = route.call_args.args[1]
                self.assertEqual((args.board, args.profile, args.de25_image),
                                 ('de25-nano', 'de25-nano', Path('/base')))
            dry = subprocess.run(['make', '--dry-run', action, 'BOARD=de25-nano',
                'DE25_IMAGE=/base', 'IMAGE_KERNEL=/kernel', 'IMAGE_SDK=/owner-sdk'],
                cwd=root, capture_output=True, text=True, check=True)
            for flag in ('--board "de25-nano"', '--de25-image "/base"', '--kernel "/kernel"', '--sdk "/owner-sdk"'):
                self.assertIn(flag, dry.stdout)

    def test_de25_entrypoint_uses_explicit_selection_and_refuses_invented_two_pass(self):
        from scripts import de25_appliance
        argv = ['build.py', 'image', '--board', 'de25-nano', '--kernel', '/kernel',
                '--sdk', '/sdk', '--artifacts', '/artifacts', '--output', '/new-factory']
        with patch.object(sys, 'argv', argv), patch.dict(os.environ, {'IMAGE_PASSES': '1'}), \
                patch.object(de25_appliance, 'image_action') as route, \
                patch.object(build, 'validate', side_effect=AssertionError('must not select DE10')):
            build.main()
        args = route.call_args.args[1]
        self.assertEqual((args.board, args.profile, args.output), ('de25-nano', 'de25-nano', Path('/new-factory')))
        with patch.object(sys, 'argv', argv), patch.dict(os.environ, {'IMAGE_PASSES': '2'}), \
                patch.object(de25_appliance, 'image_action') as route:
            with self.assertRaisesRegex(ValueError, 'one actual image-build pass'):
                build.main()
            route.assert_not_called()

    def test_make_forwards_board_and_recorded_factory_inputs(self):
        root = Path(__file__).resolve().parents[1]
        for command in ('image', 'verify'):
            result = subprocess.run(['make', '--dry-run', command, 'BOARD=de25-nano',
                                     'IMAGE_KERNEL=/kernel', 'IMAGE_SDK=/owner-sdk',
                                     'IMAGE_ARTIFACTS=/artifacts', 'IMAGE_OUTPUT=/factory'],
                                    cwd=root, capture_output=True, text=True, check=True)
            for flag in ('--board "de25-nano"', '--profile "de25-nano"', '--kernel "/kernel"',
                         '--sdk "/owner-sdk"', '--artifacts "/artifacts"', '--output "/factory"'):
                self.assertIn(flag, result.stdout)
        default = subprocess.run(['make', '--dry-run', 'image'], cwd=root, capture_output=True, text=True, check=True)
        self.assertIn('--board "de10-nano" --profile "native-integration-dev"', default.stdout)

    def test_image_routes_to_existing_factory_producer_and_shared_lease(self):
        from scripts import de25_appliance
        import media
        root = Path(__file__).resolve().parents[1]
        args = SimpleNamespace(action='image', output=None, kernel=Path('/kernel'), sdk=Path('/sdk'), artifacts=Path('/artifacts'))
        events = []

        def admit(directory, *, board):
            self.assertEqual((directory, board), (args.artifacts, 'de25-nano'))
            events.append('admit')

        @contextlib.contextmanager
        def held_lease():
            events.append('lease')
            yield
            events.append('release')

        with patch.object(artifacts, 'load', side_effect=admit), \
                patch.object(media, 'operation', side_effect=lambda *a, **kw: held_lease()) as lease, \
                patch.object(de25_appliance.subprocess, 'run', side_effect=lambda *a, **kw: events.append('produce')) as run:
            de25_appliance.image_action(root, args)
        # Admission precedes output/lease work and is rechecked under the lease.
        self.assertEqual(events, ['admit', 'lease', 'admit', 'produce', 'release'])
        lease.assert_called_once_with(root, 'de25-nano', board='de25-nano')
        command = run.call_args.args[0]
        self.assertEqual(command[1:3], [root / 'image/de25-nano/build.py', 'managed-factory'])
        self.assertEqual(command[command.index('--output') + 1], root / 'out/de25-nano/factory')
        for flag, path in (('--kernel', args.kernel), ('--sdk', args.sdk), ('--artifacts', args.artifacts)):
            self.assertEqual(command[command.index(flag) + 1], path)

    def test_missing_build_inputs_stop_before_tools_or_lease(self):
        from scripts import de25_appliance
        import media
        with patch.object(media, 'operation') as lease, patch.object(de25_appliance.subprocess, 'run') as run:
            for missing in ('kernel', 'sdk', 'artifacts'):
                args = SimpleNamespace(action='image', output=None, kernel=Path('/kernel'), sdk=Path('/sdk'), artifacts=Path('/artifacts'))
                setattr(args, missing, None)
                with self.subTest(missing=missing), self.assertRaisesRegex(ValueError, 'requires --kernel, --sdk and --artifacts'):
                    de25_appliance.image_action(Path(__file__).resolve().parents[1], args)
            lease.assert_not_called(); run.assert_not_called()

    def test_verify_always_checks_factory_and_compares_supplied_artifacts(self):
        from scripts import de25_appliance
        import media
        root = Path(__file__).resolve().parents[1]
        manifest = {'format': 1, 'board': 'de25-nano', 'architecture': 'aarch64',
                    'provenance': {}, 'binaries': {}, 'packages': []}
        selected = de25_appliance.Factory(Path('/factory'), {}, manifest, Path('/idle'))
        args = SimpleNamespace(action='verify', output=Path('/factory'), kernel=None, sdk=None, artifacts=Path('/artifacts'))
        with patch.object(media, 'operation', return_value=contextlib.nullcontext()), \
                patch.object(de25_appliance, 'verified_inputs', return_value=(None, None, None, None, None, selected)) as verify, \
                patch.object(artifacts, 'load', return_value=(dict(manifest, source_image_sha256='a'*64), ())) as load, \
                patch('builtins.print'):
            de25_appliance.image_action(root, args)
            verify.assert_called_once_with(root, 'de25-nano', Path('/factory'))
            load.return_value = (dict(manifest, binaries={'mister-runtime': 'b'*64}), ())
            with self.assertRaisesRegex(ValueError, 'differs from supplied artifact'):
                de25_appliance.image_action(root, args)

    def test_verify_refuses_changed_optional_kernel_and_compiler(self):
        from scripts import de25_appliance
        import media
        root = Path(__file__).resolve().parents[1]
        base, _, _, _ = de25_appliance.modules(root)
        with tempfile.TemporaryDirectory() as temporary:
            kernel = Path(temporary); files = {}
            for name in base.KERNEL_FILES:
                (kernel / name).write_bytes(('fixture ' + name).encode())
                files['kernel/' + name] = artifacts.sha(kernel / name)
            selected = de25_appliance.Factory(Path('/factory'), {'files': files}, {}, Path('/idle'))
            args = SimpleNamespace(action='verify', output=Path('/factory'), kernel=kernel, sdk=None, artifacts=None)
            with patch.object(media, 'operation', return_value=contextlib.nullcontext()), \
                    patch.object(de25_appliance, 'verified_inputs', return_value=(None, None, None, None, None, selected)), \
                    patch.object(base.owner, 'selected_kernel'), patch('builtins.print'):
                de25_appliance.image_action(root, args)
                (kernel / 'Image').write_bytes(b'changed kernel')
                with self.assertRaisesRegex(ValueError, 'differs from supplied kernel'):
                    de25_appliance.image_action(root, args)
                args.kernel = None; args.sdk = kernel / 'unqualified-sdk'; args.sdk.write_bytes(b'not the owner compiler')
                with self.assertRaisesRegex(ValueError, 'differs from supplied owner compiler'):
                    de25_appliance.image_action(root, args)


class DE25FactoryExportTests(ArtifactFixture):
    """Real ext4 extraction with explicit unqualified factory-authority fixtures."""
    def setUp(self):
        super().setUp()
        self.select_de25()
        selection = self.root / 'fes-menu.package-selection.toml'
        selection.chmod(0o644)
        selection.write_text('core_id = "fes.menu"\npackage_id = "' + 'a'*64 + '"\n')
        selection.chmod(0o444)
        self.manifest['packages'][0]['selection_sha256'] = artifacts.sha(selection)
        self.write_manifest()
        self.tools = os.defpath + ':/usr/sbin:/sbin'
        self.mke2fs = shutil.which('mke2fs', path=self.tools)
        if not self.mke2fs or not shutil.which('debugfs', path=self.tools) or not shutil.which('readelf', path=self.tools):
            self.skipTest('requires e2fsprogs and readelf')

    def factory(self, fault=None):
        from scripts import appliance, de25_appliance
        _, factory, _, _ = de25_appliance.modules(Path(__file__).resolve().parents[1])
        directory = self.root / ('factory-' + (fault or 'valid'))
        source = directory / 'artifacts'; source.mkdir(parents=True)
        for name in ('bin', 'core-packages'):
            shutil.copytree(self.root / name, source / name)
        for name in ('artifacts.json', 'fes-menu.package-selection.toml'):
            shutil.copy2(self.root / name, source / name)
        target = directory / 'target'; helpers = {}
        for name in artifacts.BINARIES:
            destination = target / ('usr/sbin' if name.startswith('fes-') else 'usr/share/fes/software/bin')
            destination.mkdir(parents=True, exist_ok=True)
            data = (self.root / 'bin' / name).read_bytes()
            if name.startswith('fes-'):
                data += b'factory-built helper'
                helpers[name] = hashlib.sha256(data).hexdigest()
            (destination / name).write_bytes(b'wrong executable' if fault == name else data)
        (directory / factory.INPUTS).write_text(json.dumps({'executables': helpers}))
        image = directory / 'rootfs.ext4'
        subprocess.run([self.mke2fs, '-q', '-t', 'ext4', '-F', '-d', str(target), str(image), '8M'],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30)
        receipt = {'classification': 'UNQUALIFIED UNIT FIXTURE', 'files': {
            'rootfs.ext4': artifacts.sha(image), factory.INPUTS: artifacts.sha(directory / factory.INPUTS)}}
        (directory / 'factory.json').write_text(json.dumps(receipt))
        p = appliance.ReleaseProvenance('a'*40, '1'*40, '2'*40, artifacts.sha(image), 'b'*64,
                                        artifacts.sha(directory / 'factory.json'), 'c'*64, 'd'*64, 1)
        selected = de25_appliance.Factory(directory, receipt, self.manifest,
                                         source / 'core-packages' / ('a'*64) / 'core.rbf')
        return directory, factory, selected, p

    def authority(self, directory, factory, selected, provenance):
        from scripts import de25_appliance
        import media
        stack = contextlib.ExitStack()
        stack.enter_context(patch.object(media, 'operation', return_value=contextlib.nullcontext()))
        stack.enter_context(patch.object(de25_appliance, 'verified_inputs',
                                        return_value=(directory / 'rootfs.ext4', None, provenance, None, None, selected)))
        stack.enter_context(patch.object(factory, 'verify_receipt', return_value=selected.receipt))
        stack.enter_context(patch.dict(os.environ, {'PATH': self.tools}))
        return stack

    def test_export_reads_actual_bundle_and_rebuilt_base_helpers_from_ext4(self):
        directory, factory, selected, provenance = self.factory()
        output = self.root / 'exported'
        with self.authority(directory, factory, selected, provenance):
            exported = artifacts.export(directory, output, board='de25-nano')
        self.assertEqual(exported['board'], 'de25-nano')
        self.assertEqual(exported['architecture'], 'aarch64')
        self.assertEqual(exported['provenance'], self.manifest['provenance'])
        self.assertEqual(exported['source_image_sha256'], provenance.rootfs_sha256)
        self.assertEqual(exported['source_factory_receipt_sha256'], provenance.image_receipt_sha256)
        for name in artifacts.BINARIES:
            expected = (directory / 'target' / ('usr/sbin' if name.startswith('fes-') else 'usr/share/fes/software/bin') / name).read_bytes()
            self.assertEqual((output / 'bin' / name).read_bytes(), expected)
            if name.startswith('fes-'):
                self.assertNotEqual(exported['binaries'][name], self.manifest['binaries'][name])
        with patch.dict(os.environ, {'PATH': self.tools}):
            artifacts.load(output, board='de25-nano')

    def test_export_refuses_wrong_ext4_bundle_or_base_helper_without_publication(self):
        for name in ('mister-runtime', 'fes-kit-software'):
            with self.subTest(name=name):
                directory, factory, selected, provenance = self.factory(name)
                output = self.root / ('rejected-' + name)
                with self.authority(directory, factory, selected, provenance), \
                        self.assertRaisesRegex(ValueError, 'exported executable differs: ' + name):
                    artifacts.export(directory, output, board='de25-nano')
                self.assertFalse(output.exists())

    def test_factory_receipt_change_during_extraction_prevents_publication(self):
        from scripts import de25_appliance
        directory, factory, selected, provenance = self.factory()
        output = self.root / 'rejected-race'; run = subprocess.run
        def change_after_dump(command, *args, **kwargs):
            result = run(command, *args, **kwargs)
            if command[0] == 'debugfs' and command[2].endswith('bin/fes-kit-software'):
                with (directory / 'factory.json').open('a') as stream:stream.write(' ')
            return result
        with self.authority(directory, factory, selected, provenance), \
                patch.object(de25_appliance.subprocess, 'run', side_effect=change_after_dump), \
                self.assertRaisesRegex(ValueError, 'factory changed during artifact export'):
            artifacts.export(directory, output, board='de25-nano')
        self.assertFalse(output.exists())

    def test_factory_overlap_and_real_intermediate_fail_before_extraction(self):
        from scripts import de25_appliance
        import media
        directory = self.root / 'intermediate'; directory.mkdir(); (directory / 'base.json').write_text('{}')
        with patch.object(de25_appliance, 'verified_inputs') as verify:
            with self.assertRaisesRegex(ValueError, 'overlaps'):
                artifacts.export(directory, directory / 'exported', board='de25-nano')
            verify.assert_not_called()
        with patch.object(de25_appliance.subprocess, 'run') as run, \
                patch.object(media, 'operation', return_value=contextlib.nullcontext()):
            with self.assertRaisesRegex(ValueError, 'actual managed-factory receipt'):
                artifacts.export(directory, self.root / 'rejected-intermediate', board='de25-nano')
            run.assert_not_called()
        self.assertFalse((self.root / 'rejected-intermediate').exists())


if __name__ == '__main__':
    unittest.main()
