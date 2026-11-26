"""DE25 Spectrum, Atari ST, C64, PC/XT and pc386 producer identity, manifest and firmware binding.

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
from boards.de25nano.producers import build_c64_package as c64
from boards.de25nano.producers import build_machine_package as shared
from boards.de25nano.producers import build_pcxt_package as pcxt
from boards.de25nano.producers import build_pc386_package as pc386
from boards.de25nano.producers import build_spectrum_package as spectrum
from boards.de25nano.spectrum import native_hps

ROOT = Path(__file__).resolve().parents[1]
REPOSITORY = 'https://github.com/DeanoC/fes'
SHELL = {'fes.video.fixed-720p60', 'fes.keyboard.hid', 'fes.gamepad.ports', 'fes.audio.pcm-s16-stereo-48k',
         'fes.video.session-display', 'fes.fabric.video.display-plane', 'fes.fabric.de25-scanout',
         'fes.fabric.firmware-upload'}
CASES = (
    (spectrum, '48', 16384, 'fes.spectrum', 'spectrum-firmware', {'fes.media.spectrum-tape'}),
    (spectrum, '128', 32768, 'fes.spectrum128', 'spectrum128-firmware', {'fes.media.spectrum-tape'}),
    (spectrum, 'plus3', 65536, 'fes.spectrum-plus3', 'spectrum-plus3-firmware',
     {'fes.media.spectrum-tape', 'fes.media.spectrum-disk', 'fes.media.spectrum-disk-plus3'}),
    (spectrum, 'pentagon', 49152, 'fes.pentagon', 'pentagon-firmware',
     {'fes.media.spectrum-tape', 'fes.media.spectrum-disk', 'fes.media.spectrum-disk-trdos'}),
    (atari_st, '520st', 196608, 'fes.atari-st', 'atari-st-firmware', {'fes.mouse.relative', 'fes.media.atari-st-floppy',
                                 'fes.media.atari-st-floppy-write', 'fes.media.atari-st-floppy-geometry'}),
    (c64, 'pal', 36864, 'fes.c64', 'c64-firmware', {'fes.media.c64-disk'}),
    (pcxt, 'xt', 69632, 'fes.pcxt-dos', 'pcxt-firmware', {'fes.media.pcxt-floppy', 'fes.media.pcxt-floppy-write'}),
)
PRODUCERS = (spectrum, atari_st, c64, pcxt, pc386)
# The shared machine PLL's clock, or the machine's own; and the fes.computer
# minor its endpoint answers.
MACHINE_CLOCK = {spectrum: 52222222, atari_st: 52222222, c64: 52222222, pcxt: 100000000}
ABI_MINOR = {spectrum: 0, atari_st: 0, c64: 0, pcxt: 1}


class MachineProducerTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory(prefix='de25-machine-producer-test-')
        cls.addClassCleanup(cls.temp.cleanup)
        cls.root = Path(cls.temp.name)
        # The real captured closure of both machines, laid out as a source module.
        cls.captured = {}
        for producer in PRODUCERS:
            spec = producer.MACHINE.spec(ROOT)
            cls.captured[producer] = spec.board.capture(spec)
        c64_spec = c64.MACHINE.spec(ROOT)
        cls.native_c64_captured = c64_spec.board.capture(c64_spec, native=True)
        data = {}
        for captured in (*cls.captured.values(), cls.native_c64_captured):
            data.update({Path(name).relative_to('sources/misteross').as_posix(): value
                         for name, value in captured.items()})
        for name in (spectrum.RECIPE, atari_st.RECIPE, c64.RECIPE, pcxt.RECIPE, pc386.RECIPE, shared.SHARED, shared.ABI, 'boards/de25nano/build.py',
                     'boards/de25nano/quartus.lock', 'boards/de25nano/package/core_package.py',
                     'boards/de25nano/package/export_core_package.py', shared.NATIVE_SPECTRUM_COMPOSER, *shared.REVIEW_INPUTS):
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
                self.assertEqual(parameters['threads'], 8)
                self.assertEqual(parameters['core_id'], core_id)
                self.assertEqual(parameters['rom_id'], rom_id)
                self.assertEqual((parameters['model'], parameters['firmware_bytes']), (model, size))
                self.assertEqual(parameters['firmware_delivery'], 'gp-firmware-upload-v1')
                self.assertEqual(parameters['guest_memory'], 'fabric-bram' if producer is c64 else 'board-sdram')
                self.assertEqual(parameters['programming_profile'], 'de25-linux-computer-v1')
                self.assertEqual(parameters['machine_clock_hz'], MACHINE_CLOCK[producer])
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
                self.assertEqual(manifest['abi'], {'id': 'fes.computer', 'major': 1, 'minor': ABI_MINOR[producer]})
                self.assertEqual({i['id'] for i in manifest['interfaces']}, SHELL | media)
                # Only a disk controller is optional, and it is declared beside its disk unit.
                self.assertEqual({i['id'] for i in manifest['interfaces'] if not i['required']},
                                 media & {'fes.media.spectrum-disk-plus3', 'fes.media.spectrum-disk-trdos'})
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

    def test_native_c64_seals_its_own_identity_and_shared_aperture(self):
        build = self.root / 'build'
        build.mkdir(exist_ok=True)
        payload = build / 'nativepal.rbf'
        payload.write_bytes(b'unqualified native C64 payload fixture')
        record = c64.create_build_record(self.root, REPOSITORY, self.revision,
                                         self.tools, self.execution, self.native_c64_captured,
                                         model='nativepal')
        with self.assertRaisesRegex(shared.BuildError, "captured upload-shell sources"):
            self.record(c64, 'nativepal')
        fields = verify_record_source_at_revision(self.root, record)
        self.assertIn(shared.NATIVE_SPECTRUM_COMPOSER, fields['source_inputs'])
        self.assertIn('boards/de25nano/spectrum/derive.py', fields['source_inputs'])
        self.assertFalse(fields['parameters']['linux_dma'])
        self.assertEqual(fields['parameters']['guest_memory'], 'fabric-bram')
        self.assertNotEqual(build_identity(record), build_identity(self.record(c64, 'pal')))
        manifest = tomllib.loads(c64.manifest(record, payload).decode())
        self.assertEqual(manifest['target']['programming_profile'], 'de25-native-c64-v1')
        self.assertIn(dict(id='fes.media.c64-disk', major=1, minor=0, required=True), manifest['interfaces'])
        spec = c64.MACHINE.spec(self.root)
        maps = json.loads(spec.native_controls(manifest['build']['id'], manifest['payload']['sha256']))
        self.assertEqual(maps['firmware_bytes'], 36864)
        self.assertEqual(maps['computer']['peripheral_offset'], 0x10300)
        self.assertEqual(maps['display_reader']['grant_base'], 0x95000000)
        self.assertEqual(json.loads(c64.MACHINE.rom_map(record, payload))['source_size'], 36864)

    def test_native48_uses_its_own_profile_identity_and_control_maps(self):
        build = self.root / 'build'
        build.mkdir(exist_ok=True)
        payload = build / 'native48.rbf'
        payload.write_bytes(b'unqualified native payload fixture')
        self.addCleanup(payload.unlink)
        record = self.record(spectrum, 'native48')
        fields = verify_record_source_at_revision(self.root, record)
        self.assertIn(shared.NATIVE_SPECTRUM_COMPOSER, fields['source_inputs'])
        self.assertEqual(fields['parameters']['f2sdram_master_profile'], 'native-scanout-and-probe')
        self.assertEqual(fields['parameters']['fabric_initiator_policy'], 'native-maintenance-absent-v1')
        self.assertFalse(fields['parameters']['linux_dma'])
        linux = json.loads(self.record(spectrum, '48'))
        self.assertNotIn(shared.NATIVE_SPECTRUM_COMPOSER, linux['source_inputs'])
        self.assertTrue(linux['parameters']['linux_dma'])
        manifest = tomllib.loads(spectrum.manifest(record, payload).decode())
        self.assertEqual(manifest['target']['programming_profile'], 'de25-native-spectrum48-v1')
        self.assertNotEqual(build_identity(record), build_identity(self.record(spectrum, '48')))
        self.assertEqual(json.loads(spectrum.MACHINE.rom_map(record, payload))['source_size'], 16384)
        spec = spectrum.MACHINE.spec(ROOT)
        maps = json.loads(spec.native_controls(manifest['build']['id'], manifest['payload']['sha256']))
        self.assertEqual(maps['build_id'], manifest['build']['id'])
        self.assertEqual(maps['payload_sha256'], manifest['payload']['sha256'])
        self.assertEqual(maps['computer'], {'peripheral_offset': 0x10300, 'span': 32,
                                            'fetch_count_offset': 0x18, 'fetch_address_offset': 0x1c})
        self.assertEqual(maps['display_reader'], {'peripheral_offset': 0x10200, 'span': 128,
                                                  'stop_command': 2, 'status_offset': 8, 'drained_mask': 8,
                                           'retired_mask': 0x80000000, 'grant_base': 0x95000000, 'grant_bytes': 8 << 20})
        with tempfile.TemporaryDirectory(dir=build) as name:
            work = Path(name)
            (work / 'build-inputs.json').write_bytes(record)
            candidate = work / 'core.rbf'
            candidate.write_bytes(payload.read_bytes())
            rom_map = work / 'rom-map.json'
            rom_map.write_bytes(spectrum.MACHINE.rom_map(record, candidate))
            store = work / 'packages'
            try:
                exported = export_package(spectrum.manifest(record, candidate), candidate, store,
                    rom_map=rom_map, rom_id=spectrum.MACHINE.rom_id_for('native48'), rom_role='firmware')
                package = read_package(exported.with_suffix('.fcore'))
                self.assertEqual(package.fields['format'], 3)
                self.assertEqual(package.fields['target'], manifest['target'])
                self.assertEqual(package.fields['rom']['source_size'], maps['firmware_bytes'])
                self.assertEqual(package.fields['payload']['sha256'], maps['payload_sha256'])
            finally:
                if store.exists():
                    subprocess.run(['chmod', '-R', 'u+w', str(store)], check=True)

    def test_native_phase_export_rechecks_the_generated_handoff(self):
        with tempfile.TemporaryDirectory() as name:
            output = Path(name)
            directory = output / 'combined/hps'
            directory.mkdir(parents=True)
            hps, scanout, tool = mock.Mock(), mock.Mock(), Path('/fixture/quartus_sh')
            before, changed = {'endpoint': 'selected'}, {'endpoint': 'changed'}
            for values, export_count in (([before, changed], 0), ([before, before, changed], 1)):
                hps.reset_mock()
                with mock.patch.object(native_hps, 'verify_handoff', side_effect=values):
                    with self.assertRaisesRegex(ValueError, 'handoff changed'):
                        native_hps.compile_package_candidate(output, hps, scanout, tool)
                self.assertEqual(hps.linux_configuration_artifacts.call_count, export_count)
                self.assertFalse((directory / 'generation.json').exists())

    def test_refuses_before_any_build(self):
        for producer in PRODUCERS:
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
            self.record(pc386, '486sx')  # the record names the front end's Yosys
        record = self.record(pc386, '486sx', tools)
        parameters = verify_record_source_at_revision(self.root, record)['parameters']
        self.assertEqual(parameters['core_id'], 'fes.pc386')
        self.assertNotIn('rom_id', parameters)
        self.assertNotIn('firmware_delivery', parameters)
        self.assertEqual((parameters['f2sdram_master_profile'], parameters['guest_memory'],
                          parameters['window_contract']),
                         ('scanout+window', 'hps-window', 'fes.fabric.de25-hps-window 1.0'))
        # The 486SX's own machine clock (pc386/build.py MODEL_CLOCKS): C0 = 34 on the 3,133.333 MHz VCO.
        self.assertEqual(parameters['machine_clock_hz'], 92156863)
        spec = pc386.MACHINE.spec(ROOT)
        self.assertEqual(json.loads(parameters['guest_rates'])['AUDIO_D'], 1920)
        self.assertEqual(json.loads(parameters['guest_rates']), spec.guest_rates())
        self.assertEqual(json.loads(parameters['firmware']),
                         {**sealer.firmware_digests(ROOT), 'rom_hex': sealer.pinned_rom_digest(ROOT)})
        self.assertIs(parameters['x87_present'], False)
        # The DE25 machine is the 486SX identity (pc386/build.py I486), set in the record.
        self.assertEqual((parameters['model'], parameters['i486']), ('486sx', 1))
        self.assertEqual(json.loads(record)['tools']['yosys'], 'explicit fixture yosys')
        # The reviewed signoff bases and the handshake sources they cite are in the record.
        from boards.de25nano.scanout import machine_findings
        table = machine_findings.bases()
        self.assertEqual(set(machine_findings.sources(json.loads(record), table)), {*table.SOURCES, table.TABLE})
        self.assertEqual(parameters['signoff_findings_review_policy'], machine_findings.POLICY)
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

    def test_pc386_models_carry_their_own_machine_clocks(self):
        """Two pc386 models with different clocks, from one spec and one producer: each record, each
        board clock spec, PLL comment, parent digest and set of guest rates is its own model's."""
        import copy

        class TwoClocks(type(pc386.MACHINE)):
            # A second model at the 61.437908 MHz clock (C0 = 51), standing in for #2194's 486dx, with
            # the 486SX's IP and another parent digest: the record and clock spec are what this test
            # checks, not a fit.
            def spec(self, root):
                module = super().spec(root)
                module.MODELS = {**module.MODELS, 'clocktwin': None}
                module.CLOCKS.MODEL_CLOCKS = {**module.CLOCKS.MODEL_CLOCKS, 'clocktwin': {
                    **module.CLOCKS.MODEL_CLOCKS['486sx'], 'c0': 51, 'parent_sha256': 'f' * 64}}
                return module

        machine = copy.copy(pc386.MACHINE)
        machine.__class__ = TwoClocks
        machine.descriptions = {**pc386.MACHINE.descriptions, 'clocktwin': 'fixture'}
        tools = {**self.tools, 'yosys': 'explicit fixture yosys'}
        expected = {'486sx': (92156863, '92.156863', 1920, '/34 '), 'clocktwin': (61437908, '61.437908', 1280, '/51 ')}
        spec = machine.spec(ROOT)
        board = spec.board
        self.assertTrue(board.per_model_clocks(spec))
        identities = {}
        for model in ('clocktwin', '486sx', 'clocktwin'):
            with self.subTest(model=model):
                hz, mhz, audio, divider = expected[model]
                record = machine.create_build_record(self.root, REPOSITORY, self.revision, tools, self.execution,
                                                     self.captured[pc386], model=model)
                parameters = json.loads(record)['parameters']
                self.assertEqual((parameters['model'], parameters['machine_clock_hz']), (model, hz))
                rates = json.loads(parameters['guest_rates'])
                self.assertEqual((rates['AUDIO_N'], rates['AUDIO_D']), (1, audio))
                self.assertEqual(spec.guest_rates(model), rates)
                self.assertEqual(board.clocks(spec, model)[0], hz)
                self.assertEqual(board.clocks(spec, model)[2]['gui_output_clock_frequency0'], mhz)
                self.assertIn(divider, board.top_after(spec, model))
                self.assertEqual(spec.model_clock(model)['parent_sha256'],
                                 spec.CLOCKS.MODEL_CLOCKS[model]['parent_sha256'])
                identities.setdefault(model, set()).add(build_identity(record))
        # One identity per model, and the two models' identities differ.
        self.assertEqual({model: len(ids) for model, ids in identities.items()}, {'486sx': 1, 'clocktwin': 1})
        self.assertNotEqual(identities['486sx'], identities['clocktwin'])
        # No model named is the first model; an unknown model has no clock.
        self.assertEqual(board.clocks(spec)[0], 92156863)
        with self.assertRaisesRegex(ValueError, 'no model'):
            spec.model_clock('486dx2')
        # A machine-wide clock spec beside model_clock is refused, not silently preferred.
        spec.MACHINE_HZ = 1
        with self.assertRaisesRegex(ValueError, 'both a per-model and a machine-wide'):
            board.clocks(spec)

    def test_pc386_486dx_record_names_its_cpu_x87_and_clock(self):
        """#2194's 486dx: its own record, at 61.437908 MHz (C0 51), with the i486DX CPU and the 387."""
        tools = {**self.tools, 'yosys': 'explicit fixture yosys'}
        records = {model: json.loads(pc386.create_build_record(self.root, REPOSITORY, self.revision, tools, self.execution,
                                                               self.captured[pc386], model=model))['parameters']
                   for model in ('486sx', '486dx')}
        dx = records['486dx']
        self.assertEqual((dx['model'], dx['machine_clock_hz'], dx['cpu'], dx['x87_present'], dx['i486']),
                         ('486dx', 61437908, 'fes_i486dx_cpu', True, 1))
        self.assertEqual(json.loads(dx['guest_rates'])['AUDIO_D'], 1280)
        sx = records['486sx']
        self.assertEqual((sx['model'], sx['x87_present'], 'cpu' in sx), ('486sx', False, False))

    def test_fitter_seed_reaches_the_record_and_the_qsf_and_seed_one_changes_nothing(self):
        """--seed: the record carries it and the QSF's one SEED assignment takes it. Seed 1, the
        default, leaves the record and the QSF byte-identical to a build that names no seed."""
        tools = {**self.tools, 'yosys': 'explicit fixture yosys'}
        record = lambda **kw: pc386.create_build_record(self.root, REPOSITORY, self.revision, tools, self.execution,
                                                       self.captured[pc386], model='486sx', **kw)
        unset, one, three = record(), record(seed=1), record(seed=3)
        self.assertEqual(unset, one)
        self.assertEqual(json.loads(unset)['parameters']['seed'], 1)
        self.assertEqual(json.loads(three)['parameters']['seed'], 3)
        self.assertNotEqual(build_identity(unset), build_identity(three))
        for bad in (0, -1, True, '2', 2 ** 31):
            with self.subTest(seed=bad), self.assertRaises(shared.BuildError):
                record(seed=bad)
        spec = pc386.MACHINE.spec(ROOT)
        qsf = 'set_global_assignment -name FAMILY "Agilex 5"\nset_global_assignment -name SEED 1\nx\n'
        self.assertIs(spec.board.seeded_qsf(qsf, 1), qsf)
        self.assertEqual(spec.board.seeded_qsf(qsf, 4), qsf.replace('SEED 1\n', 'SEED 4\n'))
        with self.assertRaises(ValueError):
            spec.board.seeded_qsf(qsf.replace('SEED 1', 'SEED 2'), 3)  # not the scanout's seed-1 line
        with self.assertRaises(ValueError):
            spec.board.seeded_qsf(qsf, 0)
        # The CLI default is seed 1.
        import inspect
        self.assertEqual(inspect.signature(shared.Machine.build).parameters['seed'].default, 1)

if __name__ == '__main__':
    unittest.main()
