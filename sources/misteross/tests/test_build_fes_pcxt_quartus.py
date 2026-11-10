"""Quartus package gates must preserve timing failures and artifact identity."""
import copy
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import tomllib
import unittest
from unittest import mock
from scripts import build_fes_pcxt_quartus as producer
from scripts.quartus_tools import QuartusLock, load_quartus_lock
from scripts.export_core_package import build_identity, encode_build_record, _validate_build_record


class PCXTQuartusPackageTests(unittest.TestCase):
    def summary(self):
        return {'status': 'diagnostic-captured', 'seeds': [1], 'video_mailbox_contract': True,
                'elaboration': {'hps_primitive_instances': {
                    'hdmi_i2c': 'cyclonev_hps_interface_peripheral_i2c',
                    'fes_computer_runtime_hps_gp': 'cyclonev_hps_interface_mpu_general_purpose'}},
                'profile': {'HALF_RATE_BOARD': 0, 'ENABLE_FES_COMPUTER': 1,
                            'ENABLE_PCXT_MEDIA': 1, 'ENABLE_PCXT_FLOPPY_WRITE': 0, 'HARDWARE_DOS': 0}, 'runs': [{
                    'seed': 1, 'capture_complete': True, 'reported_endpoint_checks_pass': True,
                    'minimum_pulse_width': {str(c): {'failures': 0} for c in range(4)},
                    'mailbox_interstage': [{'slack_ns': '0.123'} for _ in range(24)],
                    'no_clock_registers': [],
                    'timing_coverage_issues': {k: 0 for k in
                        ('no_clock', 'loops', 'latches', 'multiple_clock', 'generated_clock',
                         'partial_input_delay', 'partial_output_delay', 'io_min_max_delay_consistency',
                         'reference_pin', 'pll_cross_check', 'uncertainty')}}]}

    def test_all_timing_classes_and_profile_must_pass(self):
        producer.require_timing(self.summary(), [1])
        mutations = [lambda s: s['runs'][0].update(reported_endpoint_checks_pass=False),
                     lambda s: s['runs'][0]['minimum_pulse_width']['0'].update(failures=1),
                     lambda s: s['runs'][0]['mailbox_interstage'][0].update(slack_ns='-0.000'),
                     lambda s: s['runs'][0]['timing_coverage_issues'].update(generated_clock=1),
                     lambda s: s['profile'].update(HALF_RATE_BOARD=1),
                     lambda s: s['profile'].update(ENABLE_PCXT_MEDIA=0),
                     lambda s: s['profile'].pop('ENABLE_PCXT_MEDIA'),
                     lambda s: s['profile'].update(ENABLE_PCXT_FLOPPY_WRITE=1),
                     lambda s: s['profile'].pop('ENABLE_PCXT_FLOPPY_WRITE'),
                     lambda s: s['profile'].update(HARDWARE_DOS=1),
                     lambda s: s['profile'].pop('HARDWARE_DOS'),
                     lambda s: s.update(video_mailbox_contract=False),
                     lambda s: s['runs'].clear()]
        for mutate in mutations:
            summary = self.summary()
            mutate(summary)
            with self.subTest(mutation=mutate), self.assertRaises(producer.BuildError):
                producer.require_timing(summary, [1])

    def test_dos_profile_is_explicit_in_the_timing_record(self):
        summary = self.summary()
        summary['profile']['HARDWARE_DOS'] = 1
        summary['profile']['ENABLE_PCXT_MEDIA'] = 1
        summary['profile']['ENABLE_PCXT_FLOPPY_WRITE'] = 1
        summary['audio_binding'] = {'chains': [{}, {}, {}, {}]}
        summary['audio_proof_log_sha256'] = 'a' * 64
        summary['runs'][0].update(audio_clock='audio', audio_interstage=[{}] * 32, audio_payload=[{}])
        producer.require_timing(summary, [1], dos_profile=True)
        for key in ('audio_clock', 'audio_interstage', 'audio_payload'):
            broken = copy.deepcopy(summary)
            broken['runs'][0].pop(key)
            with self.assertRaises(producer.BuildError):
                producer.require_timing(broken, [1], dos_profile=True)
        broken = copy.deepcopy(summary); broken.pop('audio_proof_log_sha256')
        with self.assertRaises(producer.BuildError):
            producer.require_timing(broken, [1], dos_profile=True)
        broken = copy.deepcopy(summary); broken.pop('audio_binding')
        with self.assertRaises(producer.BuildError):
            producer.require_timing(broken, [1], dos_profile=True)
        summary['profile']['ENABLE_PCXT_FLOPPY_WRITE'] = 0
        with self.assertRaises(producer.BuildError):
            producer.require_timing(summary, [1], dos_profile=True)
        summary['profile']['ENABLE_PCXT_FLOPPY_WRITE'] = 1
        with self.assertRaises(producer.BuildError):
            producer.require_timing(summary, [1])

        summary['profile']['HARDWARE_DOS'] = 0
        with self.assertRaises(producer.BuildError):
            producer.require_timing(summary, [1], dos_profile=True)
        summary['profile']['HARDWARE_DOS'] = 1
        summary['profile']['ENABLE_PCXT_MEDIA'] = 0
        with self.assertRaises(producer.BuildError):
            producer.require_timing(summary, [1], dos_profile=True)

    def test_dos_assets_match_redistribution_provenance_locks(self):
        metadata, font = producer.dos_assets(producer.ROOT)
        self.assertEqual(metadata['floppy']['sha256'],
                         '140a2d0a507f7452790e73416dd4dab2c66a89e09a71e26b2ca8f95d1f3c5e5c')
        self.assertEqual([source['license'] for source in metadata['floppy']['sources']],
                         ['GPL-2.0-or-later', 'GPL-2.0'])
        self.assertEqual(len(font), 4096)
        record = producer.create_build_record(
            producer.ROOT, 'https://github.com/DeanoC/fes.git', 'a' * 40,
            {'quartus': 'pinned', 'yosys': 'pinned'},
            execution={'toolchain': 'quartus', 'files': {}},
            core_id='fes.pcxt-dos', dos_profile=True)
        parameters = json.loads(record)['parameters']
        self.assertNotIn('freedos_image_sha256', parameters)
        self.assertEqual(parameters['glabios_rom_sha256'], metadata['bios']['sha256'])

    def test_dos_resolver_and_producer_use_the_same_record(self):
        registry = tomllib.loads((producer.ROOT.parents[1] / 'config/core-recipes.toml').read_text())
        recipe = next(r for r in registry['recipes'] if r['core_id'] == 'fes.pcxt-dos')
        options = recipe['producer_options']
        self.assertEqual(options, {'dos_profile': True})
        arguments = (producer.ROOT, 'https://github.com/DeanoC/fes.git', 'a' * 40,
                     {'quartus': 'pinned', 'yosys': 'pinned'})
        execution = {'toolchain': 'quartus', 'files': {}}
        resolver = producer.create_build_record(*arguments, execution=execution, **options)
        builder = producer.create_build_record(*arguments, execution=execution,
                                                core_id='fes.pcxt-dos', dos_profile=True)
        self.assertEqual(resolver, builder)
        parameters = json.loads(resolver)['parameters']
        self.assertEqual(parameters['core_id'], 'fes.pcxt-dos')
        self.assertTrue(parameters['hardware_dos'])
        self.assertTrue(parameters['enable_pcxt_media'])
        self.assertTrue(parameters['enable_pcxt_floppy_write'])
        with mock.patch.object(producer, 'require_clean_source', return_value=('repo', 'a' * 40)) as clean:
            producer._require_clean_source(producer.ROOT, identity_version=2, **options)
            clean.assert_called_once()
        with self.assertRaisesRegex(producer.BuildError, 'core ID'):
            producer.create_build_record(*arguments, execution=execution,
                                         core_id='fes.pcxt', dos_profile=True)

    def authenticate_with_fakes(self, directory, *, recipe_version=None, lock=None):
        yosys = Path(directory) / 'yosys'
        yosys.write_text('locked yosys')
        quartus_sh = Path(directory) / 'quartus_sh'
        quartus_sh.write_text('locked quartus')
        lock = lock or load_quartus_lock(producer.ROOT, producer.LOCK)
        line = 'Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition'
        recipe = {'quartus': {'version': recipe_version or line}}
        hashes = {yosys.resolve(): lock.yosys_sha256 or 'y' * 64, quartus_sh.resolve(): 'q' * 64}
        with mock.patch.dict(os.environ, {'QUARTUS_ROOTDIR': '/quartus', 'FES_PCXT_YOSYS': str(yosys)}), \
                mock.patch.object(producer, 'load_quartus_lock', return_value=lock), \
                mock.patch.object(producer, 'locate_quartus', return_value=(Path('/quartus'), quartus_sh, line)) as locate, \
                mock.patch.object(producer, 'selected_recipe', return_value=recipe), \
                mock.patch.object(producer, 'sha', side_effect=lambda p: hashes[Path(p).resolve()]), \
                mock.patch.object(producer.subprocess, 'check_output',
                                  return_value=f'Yosys 0.47 (git sha1 {(lock.yosys_commit or "0" * 40)[:9]}, clang)\n'):
            tools = producer.authenticate_tools(producer.ROOT)
        locate.assert_called_once_with('/quartus', producer.ROOT, lock)
        return tools, quartus_sh, yosys

    def test_locked_tool_identities_and_labels_match_the_lite_baseline(self):
        lock = load_quartus_lock(producer.ROOT, producer.LOCK)
        self.assertEqual((lock.release, lock.series, lock.qsf_version, lock.compiler, lock.product),
                         ('17.0.2', '17.0', '17.0.2 Lite Edition', 'quartus-17.0.2', 'Quartus Prime Lite 17.0.2'))
        self.assertEqual(lock.version, 'Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition')
        self.assertEqual(lock.yosys_commit, '3a5accbd78c526ab7bae32640044a2e062a3cfc9')
        with tempfile.TemporaryDirectory() as directory:
            tools, quartus_sh, yosys = self.authenticate_with_fakes(directory)
        self.assertEqual(tools['quartus'], producer.Tool(
            quartus_sh.resolve(), 'Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition sha256:' + 'q' * 64))
        self.assertEqual(tools['yosys'], producer.Tool(
            yosys.resolve(), 'Yosys 0.47 (git sha1 3a5accbd7, clang) sha256:' + lock.yosys_sha256))
        record = producer.create_build_record(
            producer.ROOT, 'https://github.com/DeanoC/fes.git', 'a' * 40,
            {name: tool.identity for name, tool in tools.items()},
            execution={'toolchain': 'quartus', 'files': {}})
        self.assertNotIn('quartus_lock', json.loads(record))
        self.assertEqual(json.loads(record)['tools']['quartus'],
                         'Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition sha256:' + 'q' * 64)

    def test_recipe_version_and_yosys_pin_must_match_the_lock(self):
        with tempfile.TemporaryDirectory() as directory, \
                self.assertRaisesRegex(producer.BuildError, 'lock/recipe'):
            self.authenticate_with_fakes(directory, recipe_version='Version 17.1.0 Build 590 SJ Lite Edition')
        lock = load_quartus_lock(producer.ROOT, producer.LOCK)
        unpinned = QuartusLock(lock.path, lock.edition, lock.version, lock.device_family, lock.quartus_sh_sha256)
        with tempfile.TemporaryDirectory() as directory, \
                self.assertRaisesRegex(producer.BuildError, r'\[yosys\]'):
            self.authenticate_with_fakes(directory, lock=unpinned)

    def test_quartus_lock_is_a_pinned_clean_source_input(self):
        with mock.patch.object(producer, 'require_clean_source', return_value=('repo', 'a' * 40)) as clean:
            producer._require_clean_source(producer.ROOT)
        self.assertIn('cores/fes-pcxt/quartus.lock', clean.call_args.kwargs['pinned_inputs'])

    def test_invalid_internal_clocks_and_unknown_unclocked_registers_reject(self):
        for key in ('reference_pin', 'pll_cross_check', 'uncertainty', 'partial_input_delay',
                    'partial_output_delay', 'io_min_max_delay_consistency'):
            summary = self.summary()
            summary['runs'][0]['timing_coverage_issues'][key] = 1
            with self.subTest(issue=key), self.assertRaises(producer.BuildError):
                producer.require_timing(summary, [1])
        summary = self.summary()
        run = summary['runs'][0]
        run['timing_coverage_issues']['no_clock'] = 1
        run['no_clock_registers'] = ['hdmi_i2c~FF_1']
        producer.require_timing(summary, [1])
        for names in ([], ['sdram_input[0]'], ['hdmi_i2c_fabric_register']):
            run['no_clock_registers'] = names
            with self.subTest(names=names), self.assertRaises(producer.BuildError):
                producer.require_timing(summary, [1])
        run['no_clock_registers'] = ['hdmi_i2c~FF_1']
        summary['elaboration']['hps_primitive_instances']['hdmi_i2c'] = '$dff'
        with self.assertRaises(producer.BuildError):
            producer.require_timing(summary, [1])

    def test_actual_interpreter_support_and_host_library_bytes_change_identity(self):
        with tempfile.TemporaryDirectory(prefix='pcxt-runtime-') as directory:
            base = Path(directory)
            paths = {'quartus': base / 'quartus/bin/quartus_sh', 'yosys': base / 'yosys/bin/yosys'}
            for path in paths.values():
                path.parent.mkdir(parents=True)
                path.write_bytes(b'tool')
            paths['yosys'].with_name('yosys-abc').write_bytes(b'abc')
            native = base / 'quartus/linux64'
            native.mkdir()
            for name in ('sh', 'map', 'fit', 'asm', 'sta'):
                (native / ('quartus_' + name)).write_bytes(b'native')
            actual, shadow, library = (base / n for n in ('actual-python', 'path-helper', 'host-lib.so'))
            actual.write_bytes(b'\x7fELF python A')
            shadow.write_bytes(b'PATH helper')
            library.write_bytes(b'library A')
            stdlib = base / 'stdlib'
            stdlib.mkdir()
            support = stdlib / 'json.py'
            support.write_bytes(b'support A')
            env = {'PATH': str(base), 'HOME': '/private'}
            result = subprocess.CompletedProcess([], 0, 'libhost => ' + str(library) + ' (0x123)\n', '')
            with mock.patch.object(producer.sys, 'executable', str(actual)), \
                    mock.patch.object(producer.sysconfig, 'get_path', return_value=str(stdlib)), \
                    mock.patch.object(producer.shutil, 'which', return_value=str(shadow)), \
                    mock.patch.object(producer.subprocess, 'run', return_value=result):
                first = producer.execution_inputs(paths, env)
                actual.write_bytes(b'\x7fELF python B')
                second = producer.execution_inputs(paths, env)
                self.assertNotEqual(first, second)
                self.assertEqual(second['helpers']['python']['path'], str(actual))
                library.write_bytes(b'library B')
                third = producer.execution_inputs(paths, env)
                self.assertNotEqual(second['runtime_libraries'], third['runtime_libraries'])
                support.write_bytes(b'support B')
                self.assertNotEqual(third['python'], producer.execution_inputs(paths, env)['python'])
                result.stdout = 'libhost => not found\n'
                with self.assertRaisesRegex(producer.BuildError, 'runtime libraries'):
                    producer.execution_inputs(paths, env)

    def test_interrupted_compile_retries_in_a_new_preserved_attempt(self):
        record = producer.create_build_record(producer.ROOT, 'https://github.com/DeanoC/fes.git',
            'a'*40, {'quartus': 'pinned', 'yosys': 'pinned'}, execution={'toolchain': 'quartus'})
        with tempfile.TemporaryDirectory(prefix='pcxt-retry-') as directory:
            root = Path(directory)
            tools = {n: producer.Tool(root / n, 'pinned') for n in ('quartus', 'yosys')}
            def interrupted(command, **kwargs):
                output = Path(command[command.index('--output') + 1]).parent
                (output / 'interrupted.log').write_text('retained failure')
                raise subprocess.CalledProcessError(1, command)
            with mock.patch.object(producer, '_require_clean_source', return_value=('repo', 'a'*40)), \
                    mock.patch.object(producer, 'authenticate_tools', return_value=tools), \
                    mock.patch.object(producer, 'execution_inputs', return_value={'toolchain': 'quartus'}), \
                    mock.patch.object(producer, 'create_build_record', return_value=record), \
                    mock.patch.object(producer, 'selected_recipe', return_value={'quartus': {'seeds': [1], 'threads': 2}}), \
                    mock.patch.object(producer.subprocess, 'run', side_effect=interrupted) as compile:
                for _ in range(2):
                    with self.assertRaises(subprocess.CalledProcessError):
                        producer.build(root)
                self.assertEqual(compile.call_count, 2)
            attempts = list((root / 'build/fes-pcxt-quartus' / build_identity(record)).glob('attempt-*'))
            self.assertEqual(len(attempts), 2)
            for attempt in attempts:
                self.assertEqual((attempt / 'build-inputs.json').read_bytes(), record)
                self.assertEqual((attempt / 'interrupted.log').read_text(), 'retained failure')

    def test_quartus_has_a_distinct_functional_identity_without_gpu_inputs(self):
        execution = {'toolchain': 'quartus', 'files': {}}
        record = producer.create_build_record(producer.ROOT, 'https://github.com/DeanoC/fes.git',
            'a'*40, {'quartus': 'pinned quartus', 'yosys': 'pinned yosys'}, execution=execution)
        fields = json.loads(record)
        self.assertEqual(fields['format'], 2)
        self.assertEqual(fields['toolchain'], 'quartus')
        self.assertNotIn('gpu_device', fields['parameters'])
        self.assertTrue(fields['parameters']['enable_pcxt_media'])
        self.assertFalse(fields['parameters']['enable_pcxt_floppy_write'])
        before = build_identity(record)
        fields.pop('toolchain')
        self.assertNotEqual(build_identity(encode_build_record(fields)), before)
        fields['toolchain'] = 'oss'
        with self.assertRaisesRegex(ValueError, 'toolchain'):
            _validate_build_record(fields)

    def test_package_uses_common_clock_pad_constraints_not_board50_packing(self):
        qsf = producer.board.project_qsf(producer.ROOT, 1, 2, package_profile=True)
        self.assertNotIn('FAST_INPUT_REGISTER ON -to SDRAM_DQ', qsf)
        self.assertIn('set_location_assignment', qsf)
        experimental = producer.board.project_qsf(producer.ROOT, 1, 2)
        self.assertIn('FAST_INPUT_REGISTER ON -to SDRAM_DQ', experimental)

    def test_mailbox_contract_identifies_only_its_synchronizer_stages(self):
        qsf = producer.board.project_qsf(
            producer.ROOT, 1, 2, package_profile=True, video_mailbox_contract=True)
        expected = {
            f'set_instance_assignment -name SYNCHRONIZER_IDENTIFICATION FORCED '
            f'-to system_video_adapter_{role}_{stage}'
            for role in ('ack', 'sent', 'hold') for stage in ('meta', 'sync')}
        assignments = {line for line in qsf.splitlines()
                       if 'SYNCHRONIZER_IDENTIFICATION' in line}
        self.assertEqual(assignments, expected)
        self.assertNotIn('ALLOW_REGISTER_DUPLICATION OFF', qsf)

        without_contract = producer.board.project_qsf(producer.ROOT, 1, 2, package_profile=True)
        self.assertNotIn('SYNCHRONIZER_IDENTIFICATION', without_contract)

    def test_package_elaboration_embeds_the_exact_build_id(self):
        program = producer.board.elaboration(producer.ROOT, producer.ROOT / 'build',
                                             producer.ROOT / 'unused-library',
                                             package_profile=True, build_id='1234567890abcdef'*2)
        self.assertIn('-set HALF_RATE_BOARD 0', program)
        self.assertIn('-set ENABLE_FES_COMPUTER 1', program)
        self.assertIn('-set ENABLE_PCXT_MEDIA 1', program)
        self.assertIn('-set ENABLE_PCXT_FLOPPY_WRITE 0', program)
        self.assertIn('-set PHASED_HDMI_CLOCK 1', program)
        dos_program = producer.board.elaboration(producer.ROOT, producer.ROOT / 'build',
                                                 producer.ROOT / 'unused-library',
                                                 package_profile=True,
                                                 build_id='1234567890abcdef'*2,
                                                 dos_profile=True)
        self.assertIn('-set HARDWARE_DOS 1', dos_program)
        self.assertIn('-set ENABLE_PCXT_MEDIA 1', dos_program)
        self.assertIn('-set ENABLE_PCXT_FLOPPY_WRITE 1', dos_program)
        self.assertIn("-set BUILD_ID 128'h" + '1234567890abcdef'*2, program)
        with self.assertRaisesRegex(ValueError, 'BUILD_ID'):
            producer.board.elaboration(producer.ROOT, producer.ROOT / 'build',
                                       producer.ROOT / 'unused-library', package_profile=True)

    def test_manifest_declares_the_actual_media_enabled_identity(self):
        record = producer.create_build_record(producer.ROOT, 'https://github.com/DeanoC/fes.git',
            'a'*40, {'quartus': 'pinned', 'yosys': 'pinned'}, execution={'toolchain': 'quartus'})
        with tempfile.TemporaryDirectory(prefix='pcxt-manifest-') as directory:
            payload = Path(directory) / 'core.rbf'
            payload.write_bytes(b'test diagnostic')
            manifest = tomllib.loads(producer.manifest(record, payload).decode())
        self.assertEqual(manifest['abi'], {'id': 'fes.computer', 'major': 1, 'minor': 1})
        interfaces = {i['id']: i for i in manifest['interfaces']}
        self.assertTrue(interfaces['fes.media.pcxt-floppy']['required'])
        self.assertNotIn('fes.media.pcxt-floppy-write', interfaces)
        self.assertTrue(interfaces['fes.video.fixed-720p60']['required'])

    def test_dos_manifest_declares_runtime_floppy(self):
        record = producer.create_build_record(producer.ROOT, 'https://github.com/DeanoC/fes.git',
            'a'*40, {'quartus': 'pinned', 'yosys': 'pinned'},
            execution={'toolchain': 'quartus'}, dos_profile=True)
        metadata = producer.dos_assets(producer.ROOT)[0]
        with tempfile.TemporaryDirectory(prefix='pcxt-dos-manifest-') as directory:
            payload = Path(directory) / 'core.rbf'
            payload.write_bytes(b'test DOS')
            manifest = tomllib.loads(producer.manifest(record, payload, dos_profile=True, metadata=metadata).decode())
            with self.assertRaisesRegex(producer.BuildError, 'manifest profile'):
                producer.manifest(record, payload)
        self.assertEqual(manifest['core']['id'], 'fes.pcxt-dos')
        self.assertEqual(manifest['abi'], {'id': 'fes.computer', 'major': 1, 'minor': 1})
        self.assertIn('image is optional', manifest['core']['description'])
        self.assertIn('empty drive', manifest['core']['description'])
        self.assertEqual([i['id'] for i in manifest['interfaces']],
                         ['fes.video.fixed-720p60', 'fes.media.pcxt-floppy', 'fes.media.pcxt-floppy-write', 'fes.keyboard.hid',
                          'fes.audio.pcm-s16-stereo-48k'])
        self.assertTrue(manifest['interfaces'][1]['required'])
        self.assertTrue(manifest['interfaces'][2]['required'])
        fields = json.loads(record)
        fields['parameters']['enable_pcxt_floppy_write'] = False
        with tempfile.TemporaryDirectory(prefix='pcxt-manifest-negative-') as directory:
            payload = Path(directory) / 'core.rbf'
            payload.write_bytes(b'test DOS')
            with self.assertRaisesRegex(producer.BuildError, 'manifest profile'):
                producer.manifest(json.dumps(fields).encode(), payload, dos_profile=True, metadata=metadata)
        self.assertIn('runtime floppy image is optional', producer.build_limits(True)[1])
        self.assertNotIn('required', producer.build_limits(True)[1])

    def test_yosys_bus_hold_rewrite_runs_after_deferred_hierarchy(self):
        program = producer.board.elaboration(producer.ROOT, producer.ROOT / 'build',
                                             producer.ROOT / 'unused-library',
                                             package_profile=True,
                                             build_id='1234567890abcdef'*2)
        hierarchy = program.index('hierarchy -check -top top;')
        bus_hold = program.index('setparam -set enable_bus_hold "FALSE" t:altiobuf_bidir;')
        self.assertLess(hierarchy, bus_hold)

    def test_yosys_elaborates_diagnostic_and_dos_package_profiles(self):
        toolchain = tomllib.loads((producer.ROOT / 'cores/fes-pcxt/toolchain.lock').read_text())
        expected_sha = load_quartus_lock(producer.ROOT, producer.LOCK).yosys_sha256
        expected_commit = toolchain['tool']['yosys']['commit']
        configured = os.environ.get('FES_PCXT_YOSYS')
        candidates = [Path(configured)] if configured else []
        if not candidates:
            toolchain_root = os.environ.get('FES_TOOLCHAIN_ROOT')
            if toolchain_root:
                candidates.append(Path(toolchain_root) / 'bin/yosys')
            candidates.append(producer.ROOT / 'build/toolchain/fes-pcxt/bin/yosys')
            cache_root = os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or os.environ.get('CACHE_ROOT')
            if cache_root:
                candidates.extend(sorted(Path(cache_root).glob('slots/*/install/bin/yosys')))
            system_yosys = shutil.which('yosys')
            if system_yosys:
                candidates.append(Path(system_yosys))

        yosys = None
        for candidate in candidates:
            if candidate.is_file() and producer.sha(candidate) == expected_sha:
                version = subprocess.check_output([str(candidate), '-V'], text=True).strip()
                if 'git sha1 ' + expected_commit[:9] in version:
                    yosys = candidate.resolve()
                    break
        if yosys is None:
            if configured:
                self.fail('FES_PCXT_YOSYS does not match the locked PC/XT Yosys')
            self.skipTest('locked PC/XT Yosys is not installed; set FES_PCXT_YOSYS to run this check')

        library = yosys.parent.parent / 'share/yosys/intel_alm/common/megafunction_bb.v'
        self.assertTrue(library.is_file(), f'missing locked Yosys library: {library}')
        for dos_profile in (False, True):
            profile = 'dos' if dos_profile else 'diagnostic'
            with self.subTest(profile=profile), tempfile.TemporaryDirectory(
                    prefix='pcxt-yosys-elaboration-') as directory:
                work = Path(directory)
                dos_inputs = producer.board.prepare_elaboration_inputs(
                    producer.ROOT, work, dos_profile=dos_profile)
                if dos_profile:
                    self.assertTrue(dos_inputs)
                    self.assertFalse(list(work.glob('pcxt_diagnostic.bank*.hex')))
                    self.assertTrue((work / 'pcxt_glabios.bank0.hex').is_file())
                    self.assertFalse((work / 'pcxt_freedos_disk.hex').exists())
                else:
                    self.assertFalse(dos_inputs)
                    self.assertTrue((work / 'pcxt_diagnostic.bank0.hex').is_file())
                    self.assertFalse((work / 'pcxt_freedos_disk.hex').exists())

                program = producer.board.elaboration(
                    producer.ROOT, work, library, package_profile=True,
                    build_id='1234567890abcdef' * 2, dos_profile=dos_profile)
                self.assertNotIn('pcxt_disk_boot.sv', program)
                self.assertEqual('pcxt_xt_keyboard_bat.sv' in program, dos_profile)
                self.assertEqual('pcxt_hid_keyboard.sv' in program, dos_profile)
                self.assertEqual('c:dos_font.glyphs' in program, dos_profile)
                (work / 'elaborate.ys').write_text(program)
                result = subprocess.run([str(yosys), '-s', 'elaborate.ys'], cwd=work,
                                        capture_output=True, text=True, timeout=600, check=False)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                producer.board.validate_renaming(work, package_profile=True, dos_profile=dos_profile)
                if dos_profile:
                    # A valid port set and primitive count do not prove the audio clock.
                    before_path, after_path = work / 'before-names.json', work / 'elaborated.json'
                    originals = [path.read_text() for path in (before_path, after_path)]
                    for path, original in zip((before_path, after_path), originals):
                        data = json.loads(original)
                        plls = [cell for cell in data['modules']['top']['cells'].values()
                                if cell['type'] == 'altera_pll' and
                                cell['parameters'].get('output_clock_frequency0') == '12.288 MHz']
                        self.assertEqual(len(plls), 1)
                        plls[0]['parameters']['output_clock_frequency0'] = '12.000 MHz'
                        path.write_text(json.dumps(data))
                    with self.assertRaisesRegex(ValueError, 'clock request changed'):
                        producer.board.validate_renaming(work, package_profile=True, dos_profile=True)
                    for path, original in zip((before_path, after_path), originals):
                        path.write_text(original)
                before = json.loads((work / 'before-names.json').read_text())['modules']['top']
                after = json.loads((work / 'elaborated.json').read_text())['modules']['top']
                producer.board.validate_mailbox(before, after, half_rate_board=False)
                if dos_profile:
                    binding = producer.board.validate_audio(before, after)
                    self.assertEqual(len(binding['chains']), 4)
                    # An extra interstage consumer invalidates the binding.
                    head = binding['chains'][0]['head']
                    broken = copy.deepcopy(after)
                    bit = broken['netnames'][head]['bits']
                    broken['cells']['extra_audio_fanout'] = {
                        'type': '$not', 'port_directions': {'A': 'input', 'Y': 'output'},
                        'connections': {'A': bit, 'Y': [999999]}}
                    with self.assertRaisesRegex(ValueError, 'interstage'):
                        producer.board.validate_audio(before, broken)
                    broken = copy.deepcopy(after)
                    pcm = broken['netnames']['speaker_output_audio_output_stage_pcm']['bits']
                    pcm_ff = next(c for c in broken['cells'].values() if c['connections'].get('Q') == pcm)
                    pcm_ff['connections']['D'][0] = '0'
                    with self.assertRaisesRegex(ValueError, 'payload'):
                        producer.board.validate_audio(before, broken)
                    broken = copy.deepcopy(after)
                    hold = broken['netnames']['speaker_output_audio_hold']['bits']
                    hold_gate = next(c for c in broken['cells'].values() if c['connections'].get('Y') == hold)
                    hold_gate['connections']['B'] = ['0']
                    with self.assertRaisesRegex(ValueError, 'hold sources'):
                        producer.board.validate_audio(before, broken)
                elaborated_verilog = (work / 'elaborated.v').read_text()
                bus_hold_parameters = elaborated_verilog.count('.enable_bus_hold("FALSE")')
                self.assertEqual(bus_hold_parameters, 16,
                                 f'{profile} Quartus elaboration has {bus_hold_parameters}/16 '
                                 'altiobuf_bidir cells with enable_bus_hold="FALSE"')


if __name__ == '__main__':
    unittest.main()
