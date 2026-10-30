"""Quartus package gates must preserve timing failures and artifact identity."""
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
from scripts import sim_fes_pcxt as generator
from scripts.export_core_package import build_identity, encode_build_record, _validate_build_record


class PCXTQuartusPackageTests(unittest.TestCase):
    def summary(self):
        return {'status': 'diagnostic-captured', 'seeds': [1], 'video_mailbox_contract': True,
                'elaboration': {'hps_primitive_instances': {
                    'hdmi_i2c': 'cyclonev_hps_interface_peripheral_i2c',
                    'fes_computer_runtime_hps_gp': 'cyclonev_hps_interface_mpu_general_purpose'}},
                'profile': {'HALF_RATE_BOARD': 0, 'ENABLE_FES_COMPUTER': 1,
                            'ENABLE_PCXT_MEDIA': 1, 'HARDWARE_DOS': 0}, 'runs': [{
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
        summary['profile']['ENABLE_PCXT_MEDIA'] = 0
        producer.require_timing(summary, [1], dos_profile=True)
        with self.assertRaises(producer.BuildError):
            producer.require_timing(summary, [1])

        summary['profile']['HARDWARE_DOS'] = 0
        with self.assertRaises(producer.BuildError):
            producer.require_timing(summary, [1], dos_profile=True)
        summary['profile']['HARDWARE_DOS'] = 1
        summary['profile']['ENABLE_PCXT_MEDIA'] = 1
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
        self.assertEqual(parameters['freedos_source_urls'], '; '.join(
            source['url'] for source in metadata['floppy']['sources']))

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
        self.assertFalse(parameters['enable_pcxt_media'])
        with mock.patch.object(producer, 'require_clean_source', return_value=('repo', 'a' * 40)) as clean:
            producer._require_clean_source(producer.ROOT, identity_version=2, **options)
            clean.assert_called_once()
        with self.assertRaisesRegex(producer.BuildError, 'core ID'):
            producer.create_build_record(*arguments, execution=execution,
                                         core_id='fes.pcxt', dos_profile=True)

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

    def test_package_elaboration_embeds_the_exact_build_id(self):
        program = producer.board.elaboration(producer.ROOT, producer.ROOT / 'build',
                                             producer.ROOT / 'unused-library',
                                             package_profile=True, build_id='1234567890abcdef'*2)
        self.assertIn('-set HALF_RATE_BOARD 0', program)
        self.assertIn('-set ENABLE_FES_COMPUTER 1', program)
        self.assertIn('-set ENABLE_PCXT_MEDIA 1', program)
        self.assertIn('-set PHASED_HDMI_CLOCK 1', program)
        dos_program = producer.board.elaboration(producer.ROOT, producer.ROOT / 'build',
                                                 producer.ROOT / 'unused-library',
                                                 package_profile=True,
                                                 build_id='1234567890abcdef'*2,
                                                 dos_profile=True)
        self.assertIn('-set HARDWARE_DOS 1', dos_program)
        self.assertIn('-set ENABLE_PCXT_MEDIA 0', dos_program)
        self.assertIn("-set BUILD_ID 128'h" + '1234567890abcdef'*2, program)
        with self.assertRaisesRegex(ValueError, 'BUILD_ID'):
            producer.board.elaboration(producer.ROOT, producer.ROOT / 'build',
                                       producer.ROOT / 'unused-library', package_profile=True)

    def test_package_elaboration_with_real_profile_inputs(self):
        candidate = os.environ.get('FES_PCXT_YOSYS') or os.environ.get('YOSYS') or 'yosys'
        yosys_name = shutil.which(candidate)
        if not yosys_name:
            self.skipTest('Yosys is required for real package elaboration')
        yosys = Path(yosys_name).resolve()
        library = yosys.parent.parent / 'share/yosys/intel_alm/common/megafunction_bb.v'
        if not library.is_file():
            self.skipTest('Yosys Intel megafunction library is required')
        for dos_profile in (False, True):
            with self.subTest(dos_profile=dos_profile), tempfile.TemporaryDirectory(prefix='pcxt-profile-elab-') as directory:
                work = Path(directory)
                inputs = producer.board.prepare_elaboration_inputs(producer.ROOT, work, generator,
                                                                    dos_profile=dos_profile)
                disk = work / 'pcxt_freedos_disk.hex'
                self.assertEqual(disk.exists(), dos_profile)
                if dos_profile:
                    self.assertEqual(inputs['freedos_image_sha256'], producer.dos_assets(producer.ROOT)[0]['floppy']['sha256'])
                    for bank in range(8):
                        self.assertEqual((work / f'pcxt_diagnostic.bank{bank}.hex').read_bytes(),
                                         (work / f'pcxt_glabios.bank{bank}.hex').read_bytes())
                else:
                    self.assertEqual(inputs, {})
                program = producer.board.elaboration(producer.ROOT, work, library, package_profile=True,
                    build_id='1234567890abcdef' * 2, dos_profile=dos_profile)
                self.assertEqual('pcxt_disk_boot.sv' in program, dos_profile)
                self.assertEqual('pcxt_xt_keyboard_bat.sv' in program, dos_profile)
                script = work / 'elaborate.ys'
                script.write_text(program)
                result = subprocess.run([str(yosys), '-s', str(script)], cwd=work,
                                        capture_output=True, text=True, timeout=300)
                self.assertEqual(result.returncode, 0, result.stdout[-4000:] + result.stderr[-4000:])
                self.assertTrue((work / 'elaborated.json').is_file())
                self.assertTrue((work / 'elaborated.v').is_file())
                producer.board.validate_renaming(work, package_profile=True)
                before = json.loads((work / 'before-names.json').read_text())['modules']['top']
                after = json.loads((work / 'elaborated.json').read_text())['modules']['top']
                producer.board.validate_mailbox(before, after, half_rate_board=False)

    def test_manifest_declares_the_actual_media_enabled_identity(self):
        record = producer.create_build_record(producer.ROOT, 'https://github.com/DeanoC/fes.git',
            'a'*40, {'quartus': 'pinned', 'yosys': 'pinned'}, execution={'toolchain': 'quartus'})
        with tempfile.TemporaryDirectory(prefix='pcxt-manifest-') as directory:
            payload = Path(directory) / 'core.rbf'
            payload.write_bytes(b'test diagnostic')
            manifest = tomllib.loads(producer.manifest(record, payload).decode())
        self.assertEqual(manifest['abi'], {'id': 'fes.computer', 'major': 1, 'minor': 1})
        interfaces = {i['id']: i for i in manifest['interfaces']}
        self.assertEqual(interfaces['fes.media.pcxt-floppy'],
                         {'id': 'fes.media.pcxt-floppy', 'major': 1, 'minor': 0, 'required': True})
        self.assertTrue(interfaces['fes.video.fixed-720p60']['required'])

    def test_dos_manifest_keeps_legacy_identity_and_no_runtime_floppy(self):
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
            wrong_metadata = {'floppy': dict(metadata['floppy'], sha256='0' * 64)}
            with self.assertRaisesRegex(producer.BuildError, 'manifest media'):
                producer.manifest(record, payload, dos_profile=True, metadata=wrong_metadata)
        self.assertEqual(manifest['core']['id'], 'fes.pcxt-dos')
        self.assertEqual(manifest['abi'], {'id': 'fes.computer', 'major': 1, 'minor': 0})
        self.assertEqual([i['id'] for i in manifest['interfaces']], ['fes.video.fixed-720p60'])


if __name__ == '__main__':
    unittest.main()
