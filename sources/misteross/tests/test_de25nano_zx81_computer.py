"""Combined endpoint selection containment; no vendor archive or physical device."""
import importlib.util
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

BOARD = Path(__file__).resolve().parents[1] / 'boards/de25nano/link-proof'


def builder():
    spec = importlib.util.spec_from_file_location('combined_zx81_recipe_test', BOARD/'build.py')
    b = importlib.util.module_from_spec(spec); spec.loader.exec_module(b)
    b.PROFILE = b.load_profile(BOARD/'zx81-computer/profile.py')
    return b


class CombinedComputerTests(unittest.TestCase):
    def test_selected_source_closure_and_profile_drift(self):
        b=builder();frozen=b.capture()
        required=('zx81-hps/fes_de25_computer.v','zx81-hps/video.v','link-proof/zx81-ram/personas.v','link-proof/zx81-ram/timing.tcl','link-proof/zx81-computer/profile.py')
        for name in required:self.assertIn('sources/misteross/boards/de25nano/'+name,frozen['files'])
        self.assertNotEqual(b.PROFILE.identity,b.INTERFACE['name'])
        b.PROFILE.selected_source_bytes+=b'\n'
        with self.assertRaisesRegex(b.BuildError,'profile source changed'):b.capture()

    def test_identity_binding_updates_both_endpoints_and_source_receipt(self):
        b=builder()
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp); project=root/'project';mailbox=project/'fes_mailbox';mailbox.mkdir(parents=True)
            (mailbox/'video.v').write_bytes(b'actual selected RTL')
            (project/'sys_pll.ip').write_bytes(b'captured PLL')
            (project/'golden_top.v').write_bytes(b'captured top')
            (project/'integrate-mailbox.tcl').write_bytes(b'captured integration')
            receipt=root/'inputs.json';receipt.write_text(json.dumps({'computer':{'build_id':'old'}}))
            b.PROFILE.bind_identity(project,'0123456789abcdef'*2)
            result=json.loads(receipt.read_text())['computer']
            self.assertEqual(result['build_id'],'0123456789abcdef'*2)
            self.assertTrue(result['external_ram'])
            self.assertEqual(result['source_sha256'],{'video.v':b.sha(b'actual selected RTL')})
            self.assertEqual(set(b.PROFILE.extra_project_inputs(project)),{project/'sys_pll.ip',project/'integrate-mailbox.tcl',mailbox/'video.v'})

    def test_zero_build_identity_rejected_before_preparation(self):
        with tempfile.TemporaryDirectory() as tmp:
            output=Path(tmp)/'proof'
            result=subprocess.run([sys.executable,str(BOARD/'zx81-computer/proof.py'),'--native-only','--output',str(output),'--build-id','0'*32],capture_output=True,text=True,timeout=30)
            self.assertNotEqual(result.returncode,0)
            self.assertFalse(output.exists())

    @unittest.skipUnless(shutil.which('verilator') and shutil.which('c++'),'requires Verilator/C++')
    def test_actual_both_clients_and_combined_persona_fault_containment(self):
        with tempfile.TemporaryDirectory(prefix='fes-zx81-pr-combined-') as tmp:
            command=[sys.executable,str(BOARD/'zx81-computer/proof.py'),'--native-only','--allow-working-copy','--output',str(Path(tmp)/'proof')]
            for scenario in ('wrong-build','wrong-persona','program-failure','ambiguous-write','missing-response','late-reset'):
                command+=['--scenario',scenario]
            result=subprocess.run(command,capture_output=True,text=True,timeout=240)
            self.assertEqual(result.returncode,0,result.stdout+result.stderr)
            self.assertIn('"cases": 6',result.stdout)
