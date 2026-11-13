"""Menu producer selection and actual captured board project wiring."""
import copy
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[3]
HERE=ROOT/'sources/misteross/prototypes/de25_board_control'
spec=importlib.util.spec_from_file_location('de25_menu_seal',HERE/'seal.py')
seal=importlib.util.module_from_spec(spec);spec.loader.exec_module(seal)
menu=seal.menu_transport

class MenuProducerTests(unittest.TestCase):
    def inputs(self):
        return dict(board_fes_revision=menu.BOARD_REVISION,computer={'hps_initialization':'HPS FIRST'},
            reader_memory=seal.reader_memory.selection('hps-ddr-menu',0x95000000,8<<20))

    def test_menu_requires_complete_explicit_layout_board_and_clock_contract(self):
        inputs=self.inputs()
        self.assertEqual(seal.reader_memory.control_abi(inputs['reader_memory']),0x10203043)
        self.assertEqual(seal.profile_for(inputs),seal.HPS_MENU_PROFILE)
        expected=menu.parameters(inputs)
        self.assertEqual((expected['menu_system_hz'],expected['menu_pixel_hz'],expected['menu_csr_timeout_cycles']),
                         (100000000,74250000,1048576))
        self.assertEqual((expected['menu_slots'],expected['menu_slot_bytes']),(2,4<<20))
        for base,size in ((0,8<<20),(0x95000000,4<<20),(0x95000000,True),(True,8<<20),(0x95200000,8<<20)):
            with self.subTest(base=base,size=size),self.assertRaises(ValueError):
                seal.reader_memory.selection('hps-ddr-menu',base,size)
        for changes in ({'board_fes_revision':'a'*40},{'computer':{'hps_initialization':'AFTER INIT_DONE'}},
                        {'rom_transport':{'enabled':True}},{'reader_memory':{'backend':'hps-ddr-menu','bus_base':0x95000000,'bytes':4<<20}}):
            with self.subTest(changes=changes),self.assertRaises(ValueError):menu.parameters(dict(inputs,**changes))
        for backend,base,size in (('onchip',0,0),('hps-ddr',0x95000000,8192),('hps-ddr-window',0x95000000,4<<20)):
            previous=dict(inputs,reader_memory=seal.reader_memory.selection(backend,base,size))
            self.assertFalse(menu.enabled(previous));self.assertEqual(menu.parameters(previous),{})
            self.assertEqual(menu.install(Path('/unused'),previous,{},'unchanged'),'unchanged')

    def test_actual_captured_project_binds_menu_and_rejects_changed_wiring(self):
        archive=os.environ.get('FES_DE25_VENDOR_ARCHIVE')
        if not archive:self.skipTest('set FES_DE25_VENDOR_ARCHIVE for real captured project preparation')
        with tempfile.TemporaryDirectory(prefix='test-menu-project-',dir=ROOT/'out') as folder:
            output=Path(folder)/'candidate'
            command=['python3',str(HERE/'build.py'),'project','--board-revision',menu.BOARD_REVISION,
                '--vendor-archive',archive,'--output',str(output),'--hps-first','--reader-memory','hps-ddr-menu',
                '--ddr-bus-base','0x95000000','--ddr-bytes','8388608','--allow-dirty']
            result=subprocess.run(command,cwd=ROOT,capture_output=True,text=True,timeout=120)
            self.assertEqual(result.returncode,0,result.stdout+result.stderr)
            inputs=json.loads((output/'board/hps/inputs.json').read_text());project=output/'board/hps/project'
            sources={name:(output/'board/functional-source'/name).read_bytes() for name in inputs['functional_snapshot_sha256']}
            self.assertEqual(inputs['embedded_control']['control_abi'],0x10203043)
            self.assertEqual(menu.verify(project,inputs,sources),menu.parameters(inputs))
            self.assertEqual((project/'fes_mailbox/fes_menu_reader.v').read_bytes(),menu.reader(sources[menu.VIDEO+'fes_menu_reader.v']))
            import hdmi_control
            hdmi_control.verify(project,inputs,sources)
            for path,before,after in (
                ('golden_top.v',b'.pixel_reset_n(menu_pixel_reset_n)',b'.pixel_reset_n(1\'b1)'),
                ('integrate-mailbox.tcl',b'fes_computer_pixel_clk.out_clk fes_embedded.pixel_clk',b'periph_clk.out_clk fes_embedded.pixel_clk'),
                ('integrate-mailbox.tcl',b'EXPORT_OF fes_embedded.video',b'EXPORT_OF fes_computer.video'),
                ('fes_mailbox/fes_de25_menu_cdc.sv',b'TIMEOUT_CYCLES = 1048576',b'TIMEOUT_CYCLES = 42'),
                ('fes_mailbox/fes_menu_reader.v',b'M20K',b'M10K'),
                ('fes_mailbox/clocks.sdc',b'10.000',b'20.000')):
                target=project/path;original=target.read_bytes();self.assertIn(before,original)
                target.write_bytes(original.replace(before,after))
                with self.subTest(path=path),self.assertRaises(ValueError):menu.verify(project,inputs,sources)
                target.write_bytes(original)
            changed=copy.deepcopy(inputs);changed['computer']['source_sha256']['clocks.sdc']='0'*64
            with self.assertRaises(ValueError):menu.verify(project,changed,sources)
            for bad in (sources[menu.VIDEO+'fes_menu_reader.v'].replace(b'M10K',b'M20K'),b'no FIFO'):
                with self.assertRaises(ValueError):menu.reader(bad)

if __name__=='__main__':unittest.main()
