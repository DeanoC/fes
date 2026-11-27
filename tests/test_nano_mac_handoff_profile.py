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
