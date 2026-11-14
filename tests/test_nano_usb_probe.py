"""USB inspection on a held-ROM shell must expose no program resources."""
import copy
import importlib.util
import json
from pathlib import Path
import unittest
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
NANO=ROOT/'platform/embedded/nano'


def module(name,path):
    spec=importlib.util.spec_from_file_location(name,path)
    result=importlib.util.module_from_spec(spec);spec.loader.exec_module(result)
    return result


hps=module('usb_probe_context',NANO/'hps_context.py')
cold=module('usb_probe_cold',NANO/'cold_profile.py')


class ContainedUSBProbeTests(unittest.TestCase):
    def setUp(self):
        data=ROOT/'platform/embedded/tamago/nativefabric/testdata'
        self.context=json.loads((data/'hps-context.json').read_bytes())
        self.context.update(programming_profile=hps.COMPUTER_PROFILE,core_id='fes.zx81',
                            abi='fes.computer',abi_major=1,abi_minor=0,control_abi=0x10203041)
        self.record=dict(program='usbcheck',usb_control=True,usb_input='gamepad',
                         hps_context=self.context,hps_context_sha256=hps.digest(self.context))

    def test_inspection_does_not_derive_or_grant_computer_or_rom_access(self):
        for program_key in ('program','native_program'):
            record=dict(self.record);record[program_key]=record.pop('program')
            with patch.object(hps,'rom',side_effect=AssertionError('ROM access derived')), \
                 patch.object(hps,'checked_computer',side_effect=AssertionError('computer access derived')):
                self.assertIsNone(hps.checked_rom(record,program_key=program_key))
        for protocol in ('boot-keyboard','gamepad'):
            self.assertTrue(hps.usb_selection.contained_probe(dict(self.record,usb_input=protocol)))

    def test_every_program_resource_and_digest_rejects_even_without_its_peer(self):
        resources=('hps_control','hps_configuration','hps_reader_memory','hps_ddr_read',
                   'hps_window_read','hps_menu_read','hps_computer','hps_rom',
                   'hps_program_platform','hps_menu_program','cpu_mapping','rom_selection')
        for resource in resources:
            for field in (resource,resource+'_sha256') if resource!='rom_selection' else (resource,):
                with self.subTest(field=field),self.assertRaises(ValueError):
                    hps.checked_rom(dict(self.record,**{field:{'claimed':'resource'} if field==resource else 'a'*64}))

    def test_mixed_modes_and_non_boolean_flags_cannot_inherit_probe_scope(self):
        flags=('usb_shared','sd_pio','selected_programs','computer_reload','computer_storage_return',
               'tenfoot_serial','initial_tape','window_memory','window_read','window_present',
               'window_storage_return','window_reload','window_service','menu_present','smmu_observe')
        for flag in flags:
            for value in (True,0,None,'false'):
                with self.subTest(flag=flag,value=value),self.assertRaises(ValueError):
                    hps.checked_rom(dict(self.record,**{flag:value}))
        for patch in ({'usb_control':False},{'usb_control':1},{'usb_input':None},{'usb_input':'mouse'}):
            with self.subTest(patch=patch),self.assertRaises(ValueError):hps.checked_rom(dict(self.record,**patch))

    def test_other_programs_still_require_the_complete_rom_selection(self):
        for program in ('fpgaload','hpsio','probe','fpgastatus'):
            record=dict(self.record,program=program)
            self.assertFalse(hps.usb_selection.contained_probe(record))
            with self.subTest(program=program),self.assertRaises(ValueError):hps.checked_rom(record)

    def profile(self):
        data=ROOT/'platform/embedded/tamago/nativefabric/testdata'
        io=json.loads((data/'hps-io.json').read_bytes())
        return dict(sd_pio=False,usb_pio=True,usb_probe=True,usb_input='gamepad',
                    chain_sha256='1'*64,owner_seed=17,entry_image_sha256='2'*64,entry_image_size=128,
                    hps_context=self.context,hps_context_sha256=hps.digest(self.context),
                    hps_io=io,hps_io_sha256=hps.digest(io))

    def test_explicit_probe_header_enables_only_contained_usb_and_pin_observation(self):
        p=self.profile();text=cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(p)} )
        self.assertIn('#define FES_NANO_CONTROLLED_USB 1',text)
        self.assertIn('#define FES_NANO_HPS_IO_OBSERVE 1',text)
        for define in ('SHARED_USB','CONTROL_PROBE','COLD_CONFIG','COLD_COMPUTER','COLD_ROM',
                       'COMPUTER_RELOAD','COMPUTER_STORAGE_RETURN','INITIAL_TAPE'):
            self.assertIn('#define FES_NANO_'+define+' 0',text)
        for patch in ({'usb_probe':False},{'usb_probe':1},{'usb_probe':None},{'usb_pio':False},
                      {'sd_pio':True},{'hps_io':None},{'selected_programs':True},
                      {'hps_rom_sha256':'a'*64},{'hps_control':{}},{'cpu_mapping':{}}):
            with self.subTest(patch=patch),self.assertRaises(ValueError):cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(p,**patch))} )

    def test_pin_map_provenance_remains_required(self):
        for field,value in (('hps_io_hash','a'*64),('handoff_sha256','0'*64),('base',0x10b00000)):
            bad=copy.deepcopy(self.profile());bad['hps_io'][field]=value
            bad['hps_io_sha256']=hps.digest(bad['hps_io'])
            with self.subTest(field=field),self.assertRaises(ValueError):cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(bad)} )


if __name__=='__main__':unittest.main()
