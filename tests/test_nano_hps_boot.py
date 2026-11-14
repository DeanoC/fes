"""File-only composition tests; synthetic converter output cannot admit hardware."""
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import types
import unittest
from unittest.mock import Mock, patch

import test_nano_sof_hps as sof_fixture

ROOT = Path(__file__).resolve().parents[1]
HERE = ROOT/'platform/embedded/nano'
sys.path.insert(0, str(HERE))
spec = importlib.util.spec_from_file_location('nano_hps_boot', HERE/'hps_boot.py')
boot = importlib.util.module_from_spec(spec)
spec.loader.exec_module(boot)


class HpsBootTests(unittest.TestCase):
    def run_candidate(self, base, mode='pass', order='HPS FIRST', sealed=True, context=None, control=None, rom=None, chain=None, entry_mode=None):
        project = base/'project'
        hps = project/'board/hps'
        (hps/'project/output_files').mkdir(parents=True)
        original = sof_fixture.rows(b'vendor SPL')
        (hps/'project/output_files/golden_top.sof').write_bytes(sof_fixture.pack([row for row in original if row[0] != 62]))
        (hps/'diagnostic-with-vendor-spl.sof').write_bytes(sof_fixture.pack(original))
        for name in ('seal-result.json', 'build-inputs.json'):
            (hps/name).write_text('{}')
        (hps/'hps-first.hps.rbf').write_bytes(b'vendor phase one')
        payload = b'synthetic core bytes; not programmable'
        (hps/'hps-first.core.rbf').write_bytes(payload)
        uboot, entry = base/'uboot', base/'entry'
        (uboot/'build/spl').mkdir(parents=True)
        entry.mkdir()
        fit, spl, image = uboot/'build/u-boot.itb', uboot/'build/spl/u-boot-spl-dtb.hex', entry/'entry.img'
        native_spl = b'native SPL'
        fit.write_bytes(b'synthetic native FIT')
        spl.write_bytes(sof_fixture.hex_file(native_spl))
        image.write_bytes(b'synthetic entry')
        payload_elf=entry/'payload.elf';payload_elf.write_bytes(b'synthetic native ELF')
        source_files={name:base/(name+'.c') for name in ('driver','entry-producer','native-producer')}
        for name,path in source_files.items():path.write_text(name+' source\n')
        profile = base/'profile.json'
        for path in (profile, uboot/'result.json', entry/'result.json', uboot/'build/.config'):
            path.write_text('{}')
        pfg = base/'tools/bin/quartus_pfg'
        pfg.parent.mkdir(parents=True)
        binary = base/'tools/linux64/quartus_pfg'
        binary.parent.mkdir()
        binary.write_bytes(b'fake converter binary identity')
        rows = sof_fixture.rows(b'wrong SPL' if mode == 'spl' else native_spl)
        if mode == 'sections':
            rows[1] = (58, b'changed FPGA configuration')
        converted = base/'converted.sof'
        converted.write_bytes(sof_fixture.pack(rows))
        calls = base/'calls.jsonl'
        io_hash = 'a'*64
        pfg.write_text('#!'+sys.executable+'\n'+f'''
from pathlib import Path
import json,sys
args=sys.argv[1:]; mode={mode!r}
with Path({str(calls)!r}).open('a') as f:f.write(json.dumps(args)+'\\n')
if args[0]=='-c':
    if mode=='convert':raise SystemExit(7)
    target=Path(args[2])
    if 'hps=on' in args:
        target.with_suffix('.hps.rbf').write_bytes(b'native phase one')
        target.with_suffix('.core.rbf').write_bytes(b'changed' if mode=='core' else {payload!r})
    else:target.write_bytes(Path({str(converted)!r}).read_bytes())
elif args[0]=='-i':
    path=Path(args[1]); device='OTHER' if mode=='device' else 'A5EB013BB23BE4SCS'
    print('Device name: '+device)
    if path.suffix=='.sof':print('HPS/FPGA configuration order: '+('HPS_AFTER_INIT_DONE' if mode=='order' else 'HPS_FIRST'))
    else:print('HPS IO hash: '+('b'*64 if mode=='hash' else {io_hash!r}))
    changes={dict(input=str(fit),payload=str(payload_elf),profile=str(profile),config=str(uboot/'build/.config'),entry_receipt=str(entry/'result.json'),uboot_receipt=str(uboot/'result.json'),**{k:str(v) for k,v in source_files.items()})!r}
    if mode in changes and path.name.endswith('.core.rbf'):Path(changes[mode]).write_bytes(b'changed input')
else:raise SystemExit(9)
''')
        pfg.chmod(0o755)
        pair = {'package_id': 'b'*64, 'build_id': 'c'*32, 'hps_io_hash': io_hash,
                'pfg_identity': 'wrapper-sha256:'+boot.sha(pfg)+'; binary-sha256:'+boot.sha(binary),
                'phase1_sha256': boot.sha(hps/'hps-first.hps.rbf')}
        (hps/'hps-first-pair.json').write_text(json.dumps(pair))
        inputs = {'embedded_control': {'build_inputs_sha256': 'd'*64 if sealed else None},
                  'computer': {'hps_initialization': order}}
        verifier = types.SimpleNamespace(inputs_for=Mock(return_value=(inputs, {}, None)))
        real_module = boot.producer_module
        native = {'native_handoff': {'build_id': '1234567890abcdef','producer_sources':{str(source_files['native-producer']):boot.sha(source_files['native-producer'])}}, 'bl31_sha256': 'e'*64,
                  'fit_sha256':boot.sha(fit),'spl_sha256':boot.sha(spl),'config_sha256':boot.sha(uboot/'build/.config'),
                  'producer_sha256':boot.sha(HERE.parent/'firmware/build_terasic_uboot.py')}
        loaded_entry={'native_build_sources':{str(source_files['driver']):boot.sha(source_files['driver'])},
                      'sources':{str(source_files['entry-producer']):boot.sha(source_files['entry-producer'])},
                      'payload_sha256':boot.sha(payload_elf),'entry_image_sha256':boot.sha(image),'firmware_profile_sha256':boot.sha(profile)}
        if entry_mode is not None:loaded_entry.update(entry_mode)
        def checked(*args):
            if mode=='pre-source':source_files['driver'].write_text('changed before snapshot\n')
            if mode=='conflicting-source':loaded_entry['sources'][str(source_files['driver'])]='f'*64
            return fit,spl,image,native,loaded_entry,profile
        if context is not None:
            native['native_handoff']['cold_boot_profile'] = {'hps_context': context}
        if control is not None:
            native['native_handoff'].setdefault('cold_boot_profile', {})['hps_control'] = control
        if rom is not None:
            native['native_handoff'].setdefault('cold_boot_profile', {}).update(hps_rom=rom,hps_rom_sha256=boot.hps_context.digest(rom))
        if chain is not None:
            native['native_handoff'].setdefault('cold_boot_profile', {}).update(chain)
        output = base/'output'
        with patch.object(boot, 'producer_module', side_effect=lambda name, filename:
                          verifier if filename == 'verify.py' else real_module(name, filename)), \
                patch.object(boot, 'checked_native', side_effect=checked) as checker:
            result=boot.prepare(project, uboot, entry, pfg, output)
            checker.assert_called_once_with(uboot,entry)
            return result

    def test_combined_source_requires_the_full_union_before_conversion(self):
        import copy
        import test_nano_combined_program as combined
        fixture=combined.CombinedProgramTests();fixture.setUp()
        profile=fixture.profile
        for changed in (False,True):
            value=copy.deepcopy(profile)
            if changed:
                value['cpu_mapping']['reader_bytes']=2<<20
                value['cpu_mapping_sha256']=boot.hps_context.digest(value['cpu_mapping'])
            with self.subTest(changed=changed),tempfile.TemporaryDirectory() as folder, \
                 patch.object(boot.hps_context,'derive',return_value=profile['hps_context']), \
                 patch.object(boot.hps_context,'control',return_value=profile['hps_control']), \
                 patch.object(boot.hps_context,'configuration',return_value=profile['hps_configuration']), \
                 patch.object(boot.hps_context,'reader_memory',return_value=profile['hps_reader_memory']), \
                 patch.object(boot.hps_context,'rom',return_value=profile['hps_rom']):
                base=Path(folder)
                if changed:
                    with self.assertRaisesRegex(ValueError,'CPU mapping'):self.run_candidate(base,chain=value)
                    self.assertFalse((base/'calls.jsonl').exists())
                    self.assertFalse((base/'output').exists())
                else:
                    result=self.run_candidate(base,chain=value)
                    self.assertTrue(result['paired_programs'])
                    self.assertEqual(result['cpu_mapping']['reader_bytes'],8<<20)
                    self.assertEqual(result['hps_menu_program_sha256'],profile['hps_menu_program_sha256'])
                    self.assertFalse(result['physical_programming_admission'])

    def test_native_spl_is_decoded_and_core_unchanged_without_hardware_permission(self):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            result = self.run_candidate(base)
            self.assertEqual(result['status'], 'prepared')
            self.assertEqual(result['embedded_spl_identity']['spl_bytes'], len(b'native SPL'))
            self.assertTrue(result['core_payload_unchanged'])
            self.assertEqual(result['native_inputs_rechecked'],12)
            self.assertNotEqual(result['phase1_sha256'], result['original_phase1_sha256'])
            self.assertFalse(result['trusted_boot_handoff'])
            self.assertFalse(result['physical_programming_admission'])
            self.assertEqual(result['hardware_acceptance'], 'not-run')
            self.assertEqual(result['reference_combined_sof_sha256'], boot.sha(base/'project/board/hps/diagnostic-with-vendor-spl.sof'))
            with self.assertRaisesRegex(ValueError, 'one HPS section'):
                sof_fixture.sof.sections((base/'project/board/hps/project/output_files/golden_top.sof').read_bytes())
            calls = [json.loads(row) for row in (base/'calls.jsonl').read_text().splitlines()]
            self.assertEqual([row[0] for row in calls], ['-c', '-i', '-c', '-i', '-i'])
            self.assertTrue(all(row['returncode'] == 0 for row in result['commands']))

    def test_wrong_spl_config_core_hash_or_changed_input_cannot_publish_receipt(self):
        for mode in ('spl', 'sections', 'order', 'device', 'hash', 'core', 'input', 'convert',
                     'driver','entry-producer','native-producer','payload','profile','config','entry_receipt','uboot_receipt'):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as temp:
                base = Path(temp)
                with self.assertRaises((ValueError, boot.subprocess.CalledProcessError)):
                    self.run_candidate(base, mode)
                self.assertFalse((base/'output/result.json').exists())
                calls = (base/'calls.jsonl').read_text().splitlines()
                self.assertLessEqual(len(calls), 5)
                self.assertTrue((base/'output/pfg-combine.log').is_file())

    def test_changed_or_conflicting_sources_cannot_enter_conversion(self):
        for mode in ('pre-source','conflicting-source'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as temp:
                base=Path(temp)
                with self.assertRaisesRegex(ValueError,'native (composition input changed|input digest)'):
                    self.run_candidate(base,mode)
                self.assertFalse((base/'calls.jsonl').exists())
                self.assertFalse((base/'output').exists())

    def test_unsealed_or_wrong_boot_order_stops_before_conversion(self):
        for sealed, order in ((False, 'HPS FIRST'), (True, 'AFTER INIT_DONE')):
            with self.subTest(sealed=sealed, order=order), tempfile.TemporaryDirectory() as temp:
                base = Path(temp)
                with self.assertRaisesRegex(ValueError, 'sealed HPS-first'):
                    self.run_candidate(base, sealed=sealed, order=order)
                self.assertFalse((base/'calls.jsonl').exists())
                self.assertFalse((base/'output').exists())

    def test_source_bound_native_chain_requires_the_same_sealed_project(self):
        context = json.loads((ROOT/'platform/embedded/tamago/nativefabric/testdata/hps-context.json').read_bytes())
        for changed in (False, True):
            with self.subTest(changed=changed), tempfile.TemporaryDirectory() as temp:
                base = Path(temp)
                actual = dict(context, package_id='f'*64) if changed else context
                with patch.object(boot.hps_context, 'derive', return_value=actual):
                    if changed:
                        with self.assertRaisesRegex(ValueError, 'sealed FPGA project'):
                            self.run_candidate(base, context=context)
                        self.assertFalse((base/'calls.jsonl').exists())
                        self.assertFalse((base/'output').exists())
                    else:
                        result = self.run_candidate(base, context=context)
                        self.assertTrue(result['source_context_bound'])
                        self.assertEqual(result['hps_context_sha256'], boot.hps_context.digest(context))
                        self.assertFalse(result['trusted_boot_handoff'])
                        self.assertFalse(result['physical_programming_admission'])

    def test_rom_resource_requires_the_same_sealed_profile_before_conversion(self):
        self.check_rom_resource_profile({'programming_profile':boot.hps_context.ROM_PROFILE})

    def test_contained_usb_probe_agrees_with_entry_and_never_inherits_rom_controls(self):
        context={'programming_profile':boot.hps_context.COMPUTER_PROFILE,
                 'abi':'fes.computer','abi_major':1,'abi_minor':0,
                 'control_abi':0x10203041,'core_id':'fes.zx81'}
        entry={'native_program':'usbcheck','usb_control':True,'usb_input':'gamepad'}
        chain={'usb_probe':True,'usb_pio':True,'usb_input':'gamepad','sd_pio':False}
        cases=[({},{}),({'usb_probe':False},{}),({'usb_probe':1},{}),
               ({'selected_programs':True},{}),({'hps_rom_sha256':'a'*64},{}),
               ({},{'native_program':'fpgaload'}),({},{'hps_rom':{'claimed':'access'}})]
        for index,(cold_change,entry_change) in enumerate(cases):
            with self.subTest(index=index),tempfile.TemporaryDirectory() as folder, \
                 patch.object(boot.hps_context,'derive',return_value=context), \
                 patch.object(boot.hps_context,'rom',side_effect=AssertionError('ROM control derived')):
                base=Path(folder)
                if index==0:
                    result=self.run_candidate(base,context=context,chain=dict(chain,**cold_change),entry_mode=dict(entry,**entry_change))
                    self.assertTrue(result['usb_probe'])
                    self.assertFalse(result['held_rom'])
                    self.assertFalse(result['control_probe'])
                    self.assertFalse(result['physical_programming_admission'])
                else:
                    with self.assertRaises(ValueError):
                        self.run_candidate(base,context=context,chain=dict(chain,**cold_change),entry_mode=dict(entry,**entry_change))
                    self.assertFalse((base/'calls.jsonl').exists())
                    self.assertFalse((base/'output').exists())

    def test_native_computer_resources_require_the_same_sealed_profile_before_conversion(self):
        self.check_rom_resource_profile({'programming_profile':boot.hps_context.COMPUTER_PROFILE,
            'abi':'fes.computer','abi_major':1,'abi_minor':0,'control_abi':0x10203041,'core_id':'fes.zx81'})

    def check_rom_resource_profile(self, context):
        rom={'complete':'source-derived ROM fixture'}
        for mode in ('pass','missing','changed','ordinary'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as temp:
                base=Path(temp)
                source=context if mode!='ordinary' else {'programming_profile':'ordinary'}
                selected=None if mode=='missing' else dict(rom,complete='changed') if mode=='changed' else rom
                with patch.object(boot.hps_context,'derive',return_value=source), \
                        patch.object(boot.hps_context,'rom',return_value=rom):
                    if mode=='pass':
                        result=self.run_candidate(base,context=source,rom=selected)
                        self.assertTrue(result['held_rom'])
                        self.assertEqual(result['hps_rom_sha256'],boot.hps_context.digest(rom))
                    else:
                        with self.assertRaisesRegex(ValueError,'ROM resources'):
                            self.run_candidate(base,context=source,rom=selected)
                        self.assertFalse((base/'calls.jsonl').exists())
                        self.assertFalse((base/'output').exists())

    def test_control_probe_requires_the_same_compiled_address_map_before_conversion(self):
        fixture = ROOT/'platform/embedded/tamago/nativefabric/testdata'
        context = json.loads((fixture/'hps-context.json').read_bytes())
        control = json.loads((fixture/'hps-control.json').read_bytes())
        for changed in (False, True):
            with self.subTest(changed=changed), tempfile.TemporaryDirectory() as temp:
                base = Path(temp)
                actual = dict(control, cpu_base=0xff200000) if changed else control
                with patch.object(boot.hps_context, 'derive', return_value=context), \
                        patch.object(boot.hps_context, 'control', return_value=actual):
                    if changed:
                        with self.assertRaisesRegex(ValueError, 'control map'):
                            self.run_candidate(base, context=context, control=control)
                        self.assertFalse((base/'calls.jsonl').exists())
                        self.assertFalse((base/'output').exists())
                    else:
                        result = self.run_candidate(base, context=context, control=control)
                        self.assertTrue(result['control_probe'])
                        self.assertEqual(result['hps_control_sha256'], boot.hps_context.digest(control))
                        self.assertFalse(result['trusted_boot_handoff'])
                        self.assertFalse(result['physical_programming_admission'])


if __name__ == '__main__':
    unittest.main()
