"""Execute the produced SiP entry guard and actual shared MAC dispatcher."""
import ast
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
FW=ROOT/'platform/embedded/firmware'


def entry_guard():
    tree=ast.parse((FW/'prepare_nano_owned.py').read_text())
    service=next(n for n in tree.body if isinstance(n,ast.FunctionDef) and n.name=='service_source')
    return next(ast.literal_eval(n.value) for n in ast.walk(service) if isinstance(n,ast.Assign)
                and any(isinstance(t,ast.Name) and t.id=='guard' for t in n.targets))


class NanoMACDispatchTests(unittest.TestCase):
    def test_actual_boot_setup_binds_exact_original_owner_and_epoch(self):
        import importlib.util
        spec=importlib.util.spec_from_file_location('fes_mac_setup_verify',FW/'verify.py')
        verify=importlib.util.module_from_spec(spec)
        sys.path.insert(0,str(FW))
        try:
            spec.loader.exec_module(verify)
        finally:
            sys.path.pop(0)
        with tempfile.TemporaryDirectory(prefix='fes-mac-setup-') as temp:
            base=Path(temp)
            (base/'mac_setup_body.inc').write_text(verify.function(
                (FW/'nano_bridge_platform.inc').read_text(),'fes_nano_bridge_platform_setup'))
            r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined',
                '-fno-sanitize-recover=all','-I'+str(FW),'-I'+temp,str(FW/'nano_mac.c'),
                str(FW/'tests/nano_mac_setup_test.c'),'-o',str(base/'setup')],
                capture_output=True,text=True,timeout=30)
            self.assertEqual(r.returncode,0,r.stderr)
            for mode in range(17):
                with self.subTest(mode=mode):
                    r=subprocess.run([str(base/'setup'),str(mode)],capture_output=True,text=True,timeout=10)
                    self.assertEqual(r.returncode,0,r.stdout+r.stderr)
                    self.assertIn('PASS',r.stdout)

    def test_actual_entry_dispatch_ownership_and_partner_gates(self):
        with tempfile.TemporaryDirectory(prefix='fes-mac-dispatch-') as temp:
            base=Path(temp);(base/'mac_entry_guard.inc').write_text(entry_guard())
            sources=('nano_cold.c','nano_boot.c','nano_bridges.c','nano_bridge_sip.c','nano_sd.c',
                'nano_sd_platform.c','nano_usb_platform.c','nano_usb_service.c','nano_usb_composition.c',
                'nano_identity.c','nano_configuration.c','nano_mac.c','tests/nano_mac_dispatch_test.c')
            cmd=['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined',
                '-fno-sanitize-recover=all','-I'+str(FW),'-I'+temp,*[str(FW/p) for p in sources],'-o',str(base/'dispatch')]
            r=subprocess.run(cmd,capture_output=True,text=True,timeout=30)
            self.assertEqual(r.returncode,0,r.stderr)
            for mode in range(10):
                with self.subTest(mode=mode):
                    r=subprocess.run([str(base/'dispatch'),str(mode)],capture_output=True,text=True,timeout=15)
                    self.assertEqual(r.returncode,0,r.stdout+r.stderr)
                    self.assertIn('PASS',r.stdout)

if __name__=='__main__':unittest.main()
