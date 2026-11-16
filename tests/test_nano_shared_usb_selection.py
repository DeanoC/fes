"""A composed native USB image needs explicit matching scope and linker proof."""
import copy
import importlib.util
from pathlib import Path
import unittest
ROOT=Path(__file__).resolve().parents[1]
def module(name,path):
    s=importlib.util.spec_from_file_location(name,ROOT/path)
    m=importlib.util.module_from_spec(s);s.loader.exec_module(m);return m
selection=module('shared_usb_selection','platform/embedded/nano/usb_selection.py')
entry=module('shared_usb_entry','platform/embedded/entry/build.py')
class SharedUSBSelectionTests(unittest.TestCase):
    def record(self,protocol='gamepad'):
        return {'program':'fpgaload','usb_control':True,'usb_shared':True,'usb_input':protocol,
                'tenfoot_serial':True,'selected_programs':True,'computer_reload':True,'computer_storage_return':True,
                'command':['go','build','-ldflags','-X main.expectedUSBControl=owned-shared-usb-pio-v1 -X main.expectedUSBInput='+protocol,'./cmd/fpgaload']}
    def test_explicit_shared_scope_and_exact_native_link_match(self):
        for protocol in ('boot-keyboard','gamepad'):
            r=self.record(protocol)
            self.assertEqual(selection.mode(r),(True,True))
            self.assertTrue(entry.usb_control_option(r))
            for key in ('usb_control','usb_shared','tenfoot_serial','selected_programs','computer_reload','computer_storage_return'):
                for bad in (False,0,1,None):
                    with self.subTest(protocol=protocol,key=key,bad=bad),self.assertRaises(ValueError):
                        entry.usb_control_option(dict(r,**{key:bad}))
    def test_shared_mode_cannot_adopt_another_program_or_memory_scope(self):
        r=self.record()
        for patch in ({'program':'usbcheck'},{'program':'tenfoot'},{'usb_input':None},{'usb_input':'mouse'},
                      {'window_memory':True},{'window_read':True},{'smmu_observe':True},{'hps_ddr_read':{}}):
            with self.subTest(patch=patch),self.assertRaises(ValueError):selection.mode(dict(r,**patch))
        for flags in ('','-X main.expectedUSBControl=owned-usb-pio-v1 -X main.expectedUSBInput=gamepad',
                      '-X main.expectedUSBControl=owned-shared-usb-pio-v1',
                      '-X main.expectedUSBControl=owned-shared-usb-pio-v1 -X main.expectedUSBInput=boot-keyboard'):
            changed=copy.deepcopy(r);changed['command'][3]=flags
            with self.subTest(flags=flags),self.assertRaises(ValueError):entry.usb_control_option(changed)
        for marker in ('main.expectedUSBControl','main.expectedUSBInput'):
            changed=copy.deepcopy(r);changed['command'][3]+=' -X '+marker+'=gamepad'
            with self.subTest(marker=marker),self.assertRaises(ValueError):entry.usb_control_option(changed)
        changed=copy.deepcopy(r);changed['command'][-1]='./cmd/usbcheck'
        with self.assertRaises(ValueError):entry.usb_control_option(changed)
