"""Terminal recovery is explicit and bound into both matched boot producers."""
import copy
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from test_nano_sdm_rearm_profile import mode, warm_profile


class RecoveryProfileTests(unittest.TestCase):
    def test_recovery_is_separate_from_default_repeated_loading(self):
        p=warm_profile()
        self.assertEqual(mode.checked(p)['format'],23)
        self.assertFalse(mode.checked(p)['automatic_reset'])
        self.assertIn('#define FES_NANO_SDM_COLD_RECOVERY 0\n',mode.header(p))
        p['sdm_input_transfer']=mode.selected(p,True,source_rearm=True,source_recovery=True)
        self.assertEqual(mode.checked(p),mode.RECOVERY_CONTRACT)
        self.assertTrue(mode.checked(p)['automatic_reset'])
        self.assertIn('#define FES_NANO_SDM_COLD_RECOVERY 1\n',mode.header(p))
        self.assertTrue(mode.header(p).startswith('#define FES_NANO_SDM_SOURCE_RELEASE 1\n'))
        self.assertFalse(mode.checked(p)['terminal_recovery']['reset_completion'])

    def test_every_selected_recovery_field_is_required(self):
        p=warm_profile()
        p['sdm_input_transfer']=mode.selected(p,True,source_rearm=True,source_recovery=True)
        for key in mode.RECOVERY_CONTRACT:
            changed=copy.deepcopy(p);changed['sdm_input_transfer'][key]='foreign'
            with self.subTest(key=key), self.assertRaises(ValueError):mode.checked(changed)
        for key in mode.RECOVERY_CONTRACT['terminal_recovery']:
            changed=copy.deepcopy(p);changed['sdm_input_transfer']['terminal_recovery'][key]='foreign'
            with self.subTest(terminal=key), self.assertRaises(ValueError):mode.header(changed)
        for value in (None,0,1,'true',{}):
            with self.subTest(value=value), self.assertRaises(ValueError):
                mode.selected(p,True,source_rearm=True,source_recovery=value)
        with self.assertRaises(ValueError):mode.selected(p,True,source_recovery=True)

    def test_both_builders_reject_unbound_recovery_before_output(self):
        for name in ('build_terasic.py','build_terasic_uboot.py'):
            with tempfile.TemporaryDirectory() as temp:
                output=Path(temp)/'candidate'
                command=[sys.executable,str(mode.HERE/name),'--reference-repo',temp,
                         '--cross-prefix',temp+'/missing-','--output',str(output),
                         '--nano-sdm-source-recovery']
                if name=='build_terasic_uboot.py':command+=['--bl31-build',temp]
                result=subprocess.run(command,capture_output=True,text=True,timeout=20)
                self.assertNotEqual(result.returncode,0)
                self.assertIn('terminal cold recovery requires explicit source rearm',result.stderr)
                self.assertFalse(output.exists())


if __name__=='__main__':unittest.main()
