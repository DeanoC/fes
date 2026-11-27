"""Opt-in EL3 boundary checks for the PIO SDM rearm chain."""
import copy
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import test_nano_sdm_input_profile as fixture
from test_nano_sdm_rearm_profile import warm_profile

mode=fixture.mode


class MacHandoffProfileTests(unittest.TestCase):
    def test_exact_contract_and_profile(self):
        profile=warm_profile()
        self.assertNotIn('FES_NANO_MAC_HANDOFF',mode.header(profile))
        contract=mode.mac_handoff_contract(profile)
        self.assertIs(contract['mac_grant'],False)
        self.assertIs(contract['translation_published'],False)
        self.assertEqual(contract['page_access'],['RW','R','R','W','W'])
        profile['mac_handoff']=contract
        self.assertIn('#define FES_NANO_MAC_HANDOFF 1\n',mode.header(profile))
        for changed in (dict(profile,sd_dma=True),dict(profile,sd_pio=False),dict(profile,usb_shared=False),
                        dict(profile,sdm_input_transfer=None),dict(profile,sdm_input_transfer=mode.CONTRACT),
                        dict(profile,computer_storage_return=False),dict(profile,mac_handoff=None)):
            with self.assertRaises(ValueError):
                mode.header(changed)
        for key in contract:
            changed=copy.deepcopy(profile)
            changed['mac_handoff'][key]=None
            with self.subTest(key=key), self.assertRaises(ValueError):
                mode.header(changed)

    def test_source_export_binds_private_handoff_include(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)
            args=fixture.InputProfileTests().fixture(path)
            profile=warm_profile()
            profile['mac_handoff']=mode.mac_handoff_contract(profile)
            (path/mode.HEADER).write_text(mode.header(profile)+'#define FES_NANO_SDM_FAULT64 0\n#define FES_NANO_SDM_ARM_PROBE 0\n')
            binding=mode.install(path,profile,*args)
            mode.verify_installed(path,binding,profile)
            with self.assertRaises(ValueError):
                mode.verify_installed(path,binding,warm_profile())
            with (path/mode.HEADER).open('a') as header:
                header.write('#define FES_NANO_MAC_HANDOFF 1\n')
            with self.assertRaises(ValueError):
                mode.verify_handoff_header(path,profile)
            (path/mode.HEADER).write_text(mode.header(profile)+'#define FES_NANO_SDM_FAULT64 0\n#define FES_NANO_SDM_ARM_PROBE 0\n')
            source='plat/intel/soc/common/include/nano_mac_handoff.inc'
            self.assertEqual(binding['sources'][source],mode.sha(mode.HERE/'nano_mac_handoff.inc'))
            (path/source).write_text('')
            with self.assertRaises(ValueError):
                mode.verify_installed(path,binding,profile)

    def test_inactive_table_trial_requires_matched_staging_and_plain_handoff(self):
        profile=warm_profile()
        with self.assertRaises(ValueError):
            mode.mac_table_trial_contract(profile,0x96080000)
        profile['mac_handoff']=mode.mac_handoff_contract(profile)
        self.assertNotIn('MAC_TABLE_TRIAL',mode.header(profile))
        with self.assertRaises(ValueError):
            mode.mac_table_trial_contract(profile,0x94010000)
        profile['mac_table_trial']=mode.mac_table_trial_contract(profile,0x96080000)
        self.assertIn('#define FES_NANO_MAC_TABLE_TRIAL 1',mode.header(profile))
        self.assertIs(profile['mac_table_trial']['translation_enabled'],False)
        self.assertEqual(profile['mac_table_trial']['queue_commands'],0)
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)
            args=fixture.InputProfileTests().fixture(path)
            (path/mode.HEADER).write_text(mode.header(profile)+'#define FES_NANO_SDM_FAULT64 0\n#define FES_NANO_SDM_ARM_PROBE 0\n')
            binding=mode.install(path,profile,*args)
            mode.verify_installed(path,binding,profile)
            self.assertIn('plat/intel/soc/common/include/nano_mac_table_trial.inc',binding['sources'])
            self.assertIn('__FES_MAC_TABLE_START__ = .; . += 0x7000', (path/mode.LD).read_text())
        for field in profile['mac_table_trial']:
            changed=copy.deepcopy(profile)
            changed['mac_table_trial'][field]=None
            with self.subTest(field=field), self.assertRaises(ValueError):
                mode.header(changed)

    def test_secure_menu_coexistence_is_separate_and_cannot_offer_mac(self):
        profile=warm_profile()
        profile['mac_handoff']=mode.mac_handoff_contract(profile)
        with self.assertRaises(ValueError):
            mode.mac_coexistence_contract(profile)
        profile['mac_table_trial']=mode.mac_table_trial_contract(profile,0x96080000)
        self.assertNotIn('FES_NANO_MAC_COEXISTENCE_TRIAL',mode.header(profile))
        contract=mode.mac_coexistence_contract(profile)
        profile['mac_coexistence_trial']=contract
        self.assertTrue(contract['mac_reset_held'])
        self.assertFalse(contract['mac_grant'])
        self.assertEqual(contract['commands'],16)
        self.assertIn('#define FES_NANO_MAC_COEXISTENCE_TRIAL 1',mode.header(profile))
        self.assertIn('nano_mac_coexist.inc',mode.installed_headers(profile))
        for field in contract:
            broken=copy.deepcopy(profile)
            broken['mac_coexistence_trial'][field]=None
            with self.subTest(field=field), self.assertRaises(ValueError):
                mode.header(broken)

    def test_menu_supplier_requires_exact_paired_client(self):
        profile=warm_profile()
        profile['mac_handoff']=mode.mac_handoff_contract(profile)
        profile['mac_table_trial']=mode.mac_table_trial_contract(profile,0x96080000)
        profile['mac_coexistence_trial']=mode.mac_coexistence_contract(profile)
        with self.assertRaises(ValueError):
            mode.mac_menu_contract(profile)
        profile['launcher_ethernet']=mode.ethernet.contract()
        profile['mac_menu_supplier']=mode.mac_menu_contract(profile)
        self.assertFalse(profile['mac_menu_supplier']['hardware_accepted'])
        self.assertIn('#define FES_NANO_MAC_MENU_SUPPLIER 1',mode.header(profile))
        for key in profile['launcher_ethernet']:
            with self.subTest(key=key), self.assertRaises(ValueError):
                mode.header(dict(profile,launcher_ethernet=dict(profile['launcher_ethernet'],**{key:None})))

    def test_both_builders_require_explicit_source_rearm(self):
        for builder in ('build_terasic.py','build_terasic_uboot.py'):
            with tempfile.TemporaryDirectory() as directory:
                output=Path(directory)/'candidate'
                cmd=[sys.executable,str(mode.HERE/builder),'--reference-repo',directory,
                     '--cross-prefix',directory+'/no-tool-', '--output',str(output),'--nano-mac-handoff']
                if builder=='build_terasic_uboot.py':
                    cmd+=['--bl31-build',directory]
                result=subprocess.run(cmd,capture_output=True,text=True,timeout=20)
                self.assertNotEqual(result.returncode,0)
                self.assertIn('MAC handoff requires explicit source rearm',result.stderr)
                self.assertFalse(output.exists())
                cmd[cmd.index('--nano-mac-handoff')]='--nano-mac-table-trial'
                result=subprocess.run(cmd,capture_output=True,text=True,timeout=20)
                self.assertNotEqual(result.returncode,0)
                self.assertIn('table trial requires explicit MAC handoff',result.stderr)
                self.assertFalse(output.exists())
