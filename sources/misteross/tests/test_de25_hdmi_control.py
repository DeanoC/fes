"""Exercise the real captured wrapper derivation and control bus model."""
import copy
import importlib.util
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
import uuid
import zipfile

ROOT=Path(__file__).resolve().parents[3]
HERE=ROOT/'sources/misteross/prototypes/de25_board_control'
spec=importlib.util.spec_from_file_location('de25_hdmi_control',HERE/'hdmi_control.py')
hdmi=importlib.util.module_from_spec(spec);spec.loader.exec_module(hdmi)


class HDMIControlTests(unittest.TestCase):
    def test_wrapper_derivation_requires_the_reviewed_source(self):
        source=subprocess.check_output(['git','show','79d616e85e19d65f0d645b67a85ffe7e142dcdfc:'+hdmi.BOARD_SOURCE],cwd=ROOT)
        self.assertIn(b'.HDMI_TX_INT(hdmi_irq_sync[1])',hdmi.board(source))
        with self.assertRaises(ValueError):hdmi.board(source+b'changed\n')

    def test_captured_wrapper_and_ownership_edits_are_checked(self):
        archive=os.environ.get('FES_DE25_VENDOR_ARCHIVE')
        if not archive:self.skipTest('set FES_DE25_VENDOR_ARCHIVE to the pinned resource archive')
        source=subprocess.check_output(['git','show','79d616e85e19d65f0d645b67a85ffe7e142dcdfc:'+hdmi.BOARD_SOURCE],cwd=ROOT)
        with tempfile.TemporaryDirectory() as temporary:
            project=Path(temporary);p=project/'fes_mailbox/board.v';p.parent.mkdir();p.write_bytes(source)
            inputs={'computer':{'source_sha256':{}}};sources={hdmi.BOARD_SOURCE:source}
            with zipfile.ZipFile(archive) as z:
                raw=z.read('Demonstration/FPGA/HDMI_ASx4/HDMI_I2C/I2C_HDMI_Config.v')
            config=p.parent/'I2C_HDMI_Config.v';config.write_bytes(raw)
            hdmi.install(project,inputs,sources);hdmi.verify(project,inputs,sources)
            for changed in (source,p.read_bytes().replace(b'CLOCK0_50 or',b'pixel_clk or'),p.read_bytes().replace(b'.HDMI_TX_INT(hdmi_irq_sync[1])',b'.HDMI_TX_INT(HDMI_TX_INT)')):
                original=p.read_bytes();p.write_bytes(changed)
                with self.assertRaises(ValueError):hdmi.verify(project,inputs,sources)
                p.write_bytes(original)
            modified=copy.deepcopy(inputs);modified['computer']['source_sha256']['board.v']='0'*64
            with self.assertRaises(ValueError):hdmi.verify(project,modified,sources)
            with self.assertRaises(ValueError):hdmi.board(source+b'changed\n')
            with self.assertRaises(ValueError):hdmi.install(project,inputs,sources)
            for name in ('I2C_HDMI_Config.v','I2C_HDMI_Config.v.original'):
                path=p.parent/name;data=path.read_bytes();path.write_bytes(data+b'changed\n')
                with self.assertRaises(ValueError):hdmi.verify(project,inputs,sources)
                path.write_bytes(data)

    def test_actual_vendor_bus_and_interrupt_model(self):
        project=os.environ.get('FES_NANO_HDMI_PROJECT')
        if not project or not shutil.which('verilator'):
            self.skipTest('set FES_NANO_HDMI_PROJECT to a prepared physical project; Verilator required')
        subprocess.run(['python3',str(HERE/'verify_hdmi.py'),'--project',project,'--output',str(ROOT/'out'/('de25-hdmi-control-'+uuid.uuid4().hex))],cwd=ROOT,check=True,timeout=180)


if __name__=='__main__':unittest.main()
