"""DE25 Spectrum, Atari ST and pc386 producer identity, manifest and firmware binding.

The temporary Git tree and payload below exercise source and manifest
boundaries; they are deliberately unqualified and never stand in for a
compiled package.
"""
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import tomllib
import unittest
from unittest import mock

from boards.de25nano.package.core_package import PackageError, encode_manifest, read_package, validate_rom_map
from boards.de25nano.package.export_core_package import (
    build_identity, export_package, verify_record_source_at_revision)
from boards.de25nano.producers import build_atari_st_package as atari_st
from boards.de25nano.producers import build_machine_package as shared
from boards.de25nano.producers import build_pc386_package as pc386
from boards.de25nano.producers import build_spectrum_package as spectrum

ROOT = Path(__file__).resolve().parents[1]
REPOSITORY = 'https://github.com/DeanoC/fes'
SHELL = {'fes.video.fixed-720p60', 'fes.keyboard.hid', 'fes.gamepad.ports', 'fes.audio.pcm-s16-stereo-48k',
         'fes.video.session-display', 'fes.fabric.video.display-plane', 'fes.fabric.de25-scanout',
         'fes.fabric.firmware-upload'}
CASES = (
    (spectrum, '48', 16384, 'fes.spectrum', 'spectrum-firmware', {'fes.media.spectrum-tape'}),
    (spectrum, '128', 32768, 'fes.spectrum128', 'spectrum128-firmware', {'fes.media.spectrum-tape'}),
    (atari_st, '520st', 196608, 'fes.atari-st', 'atari-st-firmware', {'fes.mouse.relative', 'fes.media.atari-st-floppy',
                                 'fes.media.atari-st-floppy-write', 'fes.media.atari-st-floppy-geometry'}),
)


class MachineProducerTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory(prefix='de25-machine-producer-test-')
        cls.addClassCleanup(cls.temp.cleanup)
        cls.root = Path(cls.temp.name)
        # The real captured closure of both machines, laid out as a source module.
        cls.captured = {}
        for producer in (spectrum, atari_st, pc386):
            spec = producer.MACHINE.spec(ROOT)
            cls.captured[producer] = spec.board.capture(spec)
        data = {}
        for captured in cls.captured.values():
            data.update({Path(name).relative_to('sources/misteross').as_posix(): value
                         for name, value in captured.items()})
        for name in (spectrum.RECIPE, atari_st.RECIPE, pc386.RECIPE, shared.SHARED, shared.ABI, 'boards/de25nano/build.py',
                     'boards/de25nano/quartus.lock', 'boards/de25nano/package/core_package.py',
                     'boards/de25nano/package/export_core_package.py', *shared.REVIEW_INPUTS):
            data.setdefault(name, (ROOT / name).read_bytes())
        for root in shared.SOURCE_ROOTS:
            if not Path(root).suffix and not any(name.startswith(root + '/') for name in data):
                data[root + '/fixture.v'] = b'// source closure fixture\n'
        # The repository ignores bytecode caches; a record loads its builders from this tree.
        data['.gitignore'] = b'/build/\n__pycache__/\n'
        for name, value in data.items():
            path = cls.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(value)
        for command in (['git', 'init', '-q'], ['git', 'remote', 'add', 'origin', REPOSITORY], ['git', 'add', '.'],
                        ['git', '-c', 'user.name=Test', '-c', 'user.email=test@example.invalid',
                         'commit', '-qm', 'fixture']):
            subprocess.run(command, cwd=cls.root, check=True)
        cls.revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=cls.root, text=True).strip()
        cls.tools = {name: 'explicit fixture ' + name for name in ('quartus', 'qsys', 'ipgenerate', 'pfg')}
        cls.execution = {'toolchain': 'quartus', 'fixture': True}

    def record(self, producer, model, tools=None):
        return producer.create_build_record(self.root, REPOSITORY, self.revision,
                                            tools or self.tools, self.execution, self.captured[producer],
                                            model=model)

    def test_record_binds_machine_model_memory_and_firmware_delivery(self):
        identities = set()
        for producer, model, size, core_id, rom_id, _ in CASES:
            with self.subTest(core=producer.MACHINE.core_id, model=model):
                record = self.record(producer, model)
                parameters = verify_record_source_at_revision(self.root, record)['parameters']
                self.assertEqual(parameters['core_id'], core_id)
                self.assertEqual(parameters['rom_id'], rom_id)
                self.assertEqual((parameters['model'], parameters['firmware_bytes']), (model, size))
                self.assertEqual(parameters['firmware_delivery'], 'gp-firmware-upload-v1')
                self.assertEqual(parameters['guest_memory'], 'board-sdram')
                self.assertEqual(parameters['programming_profile'], 'de25-linux-computer-v1')
                self.assertEqual(parameters['machine_clock_hz'], 52222222)
                self.assertFalse(parameters['expansion_socket'])
                self.assertEqual(json.loads(record)['recipe'], producer.RECIPE)
                identities.add(build_identity(record))
                other = self.record(producer, model, dict(self.tools, quartus='changed actual compiler'))
                self.assertEqual(build_identity(record), build_identity(other))
        self.assertEqual(len(identities), len(CASES))
        with self.assertRaisesRegex(shared.BuildError, 'no model'):
            self.record(atari_st, '1040st')
        changed = dict(self.captured[spectrum])
        changed['sources/misteross/cores/fes-spectrum/rtl/spectrum_ula.sv'] = b'changed'
        with self.assertRaisesRegex(shared.BuildError, 'differs from canonical closure'):
            spectrum.create_build_record(self.root, REPOSITORY, self.revision,
                                         self.tools, self.execution, changed)

    def test_manifest_is_the_runtime_contract_and_the_map_patches_nothing(self):
        payload = self.root / 'core.rbf'
        payload.write_bytes(b'unqualified payload fixture')
        self.addCleanup(payload.unlink)
        for producer, model, size, core_id, rom_id, media in CASES:
            with self.subTest(core=producer.MACHINE.core_id, model=model):
                record = self.record(producer, model)
                manifest = tomllib.loads(producer.manifest(record, payload).decode())
                self.assertEqual(manifest['format'], 2)
                self.assertEqual(manifest['core']['id'], core_id)
                self.assertEqual(manifest['target'], {'platform': 'de25_nano', 'device': shared.DEVICE,
                                                      'programming_profile': 'de25-linux-computer-v1'})
                self.assertEqual(manifest['abi'], {'id': 'fes.computer', 'major': 1, 'minor': 0})
                self.assertEqual({i['id'] for i in manifest['interfaces']}, SHELL | media)
                self.assertTrue(all(i['required'] for i in manifest['interfaces']))
                versions = {i['id']: (i['major'], i['minor']) for i in manifest['interfaces']}
                self.assertEqual(versions['fes.fabric.de25-scanout'], (1, 3))
                self.assertEqual(versions['fes.fabric.firmware-upload'], (1, 0))
                if producer is spectrum:
                    self.assertEqual(versions['fes.media.spectrum-tape'], (2, 0))
                self.assertEqual(manifest['build']['id'], build_identity(record))
                self.assertEqual(producer.MACHINE.rom_id_for(model), rom_id)
                mapping = json.loads(producer.MACHINE.rom_map(record, payload))
                self.assertEqual(mapping, {'format': 1, 'device': shared.DEVICE,
                                           'encoding': 'gp-firmware-upload-v1',
                                           'base_sha256': manifest['payload']['sha256'], 'source_size': size})

    def test_refuses_before_any_build(self):
        for producer in (spectrum, atari_st):
            with mock.patch.object(producer.MACHINE, 'spec', side_effect=AssertionError('builder ran')):
                with self.assertRaisesRegex(shared.BuildError, 'requires --vendor-archive'):
                    producer.build(quartus_root='', vendor_archive=None)
                with self.assertRaisesRegex(shared.BuildError, 'compile-only cannot select'):
                    producer.build(quartus_root='', vendor_archive=Path('unopened.zip'), compile_only=True,
                                   package_output=Path('packages'))

    def test_export_seals_a_format3_upload_package_and_the_reader_rejects_other_rom_shapes(self):
        work = self.root / 'build/export'
        work.mkdir(parents=True)
        self.addCleanup(shutil.rmtree, self.root / 'build')
        store = Path(tempfile.mkdtemp(prefix='de25-machine-store-'))
        def discard():
            # A published package is sealed read-only.
            subprocess.run(['chmod', '-R', 'u+w', str(store)], check=True)
            shutil.rmtree(store)
        self.addCleanup(discard)
        record = self.record(atari_st, '520st')
        payload = work / 'core.rbf'
        payload.write_bytes(b'unqualified payload fixture')
        (work / 'build-inputs.json').write_bytes(record)
        manifest = atari_st.manifest(record, payload)
        mapping = work / 'rom-map.json'
        mapping.write_bytes(atari_st.MACHINE.rom_map(record, payload))
        directory = export_package(manifest, payload, store, rom_map=mapping,
                                   rom_id=atari_st.MACHINE.rom_id, rom_role='firmware')
        package = read_package(directory)
        self.assertEqual(package.fields['format'], 3)
        self.assertEqual(package.fields['rom'], {'id': 'atari-st-firmware', 'role': 'firmware', 'source_size': 196608,
            'file': 'rom-map.json', 'size': len(mapping.read_bytes()),
            'sha256': package.fields['rom']['sha256']})
        self.assertEqual(package.rom_map_bytes, mapping.read_bytes())
        self.assertEqual(package, read_package(directory.with_suffix('.fcore')))
        fields = package.fields
        good = json.loads(mapping.read_bytes())

        def refused(**changes):
            data = json.dumps({**good, **changes}, separators=(',', ':')).encode()
            bound = {**fields, 'rom': {**fields['rom'], 'size': len(data),
                                       'sha256': hashlib.sha256(data).hexdigest()}}
            with self.assertRaises(PackageError):
                validate_rom_map(data, bound)
        refused(encoding='m10k-1024x10-v1')
        refused(encoding='cpu-csr-v1')
        refused(blocks=[])
        refused(source_size=16384)
        refused(device='5CSEBA6U23I7')
        refused(base_sha256='0' * 64)
        # Without the upload interface, or as a cartridge, a DE25 ROM package is refused.
        for change in (lambda f: f['interfaces'].remove(next(
                           i for i in f['interfaces'] if i['id'] == 'fes.fabric.firmware-upload')),
                       lambda f: f['rom'].update(role='cartridge'),
                       lambda f: next(i for i in f['interfaces']
                                      if i['id'] == 'fes.fabric.firmware-upload').update(required=False)):
            bad = json.loads(json.dumps(fields))
            change(bad)
            with self.assertRaises(PackageError):
                encode_manifest(bad)

    def test_pc386_record_and_manifest_name_the_window_and_the_linked_firmware(self):
        from scripts import pc386_quartus as sealer
        tools = {**self.tools, 'yosys': 'explicit fixture yosys'}
        with self.assertRaisesRegex(shared.BuildError, 'controlled Quartus execution inputs'):
            self.record(pc386, '386')  # the record names the front end's Yosys
        record = self.record(pc386, '386', tools)
        parameters = verify_record_source_at_revision(self.root, record)['parameters']
        self.assertEqual(parameters['core_id'], 'fes.pc386')
        self.assertNotIn('rom_id', parameters)
        self.assertNotIn('firmware_delivery', parameters)
        self.assertEqual((parameters['f2sdram_master_profile'], parameters['guest_memory'],
                          parameters['window_contract']),
                         ('scanout+window', 'hps-window', 'fes.fabric.de25-hps-window 1.0'))
        self.assertEqual(parameters['machine_clock_hz'], 61437908)
        spec = pc386.MACHINE.spec(ROOT)
        self.assertEqual(json.loads(parameters['guest_rates']), spec.guest_rates())
        self.assertEqual(json.loads(parameters['firmware']),
                         {**sealer.firmware_digests(ROOT), 'rom_hex': sealer.pinned_rom_digest(ROOT)})
        self.assertIs(parameters['x87_present'], False)
        self.assertEqual(json.loads(record)['tools']['yosys'], 'explicit fixture yosys')
        work = self.root / 'build/pc386'
        work.mkdir(parents=True)
        self.addCleanup(shutil.rmtree, self.root / 'build')
        payload = work / 'core.rbf'
        payload.write_bytes(b'unqualified payload fixture')
        (work / 'build-inputs.json').write_bytes(record)
        manifest = tomllib.loads(pc386.manifest(record, payload).decode())
        self.assertEqual(manifest['format'], 2)
        self.assertEqual(manifest['abi'], {'id': 'fes.computer', 'major': 1, 'minor': 1})
        versions = {i['id']: (i['major'], i['minor']) for i in manifest['interfaces']}
        self.assertEqual(versions, {**{name: (1, 0) for name in sealer.INTERFACES},
                                    'fes.fabric.de25-hps-window': (1, 0), 'fes.fabric.de25-scanout': (1, 3)})
        self.assertTrue(all(i['required'] for i in manifest['interfaces']))
        # Format 2: no ROM map, and no ROM metadata.
        store = Path(tempfile.mkdtemp(prefix='de25-pc386-store-'))
        def discard():
            subprocess.run(['chmod', '-R', 'u+w', str(store)], check=True)
            shutil.rmtree(store)
        self.addCleanup(discard)
        package = read_package(export_package(pc386.manifest(record, payload), payload, store))
        self.assertEqual(package.fields['format'], 2)
        self.assertNotIn('rom', package.fields)
        self.assertIsNone(package.rom_map_bytes)


if __name__ == '__main__':
    unittest.main()
