"""The native USB lease is selected by its real link command, never static readiness."""
import copy
import importlib.util
from pathlib import Path
import unittest
ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('usb_entry_build',ROOT/'platform/embedded/entry/build.py')
entry=importlib.util.module_from_spec(spec);spec.loader.exec_module(entry)

class USBSelectionTests(unittest.TestCase):
    def test_actual_usb_link_assignment_and_program_are_required(self):
        record={'program':'usbcheck','usb_control':True,'command':['go','build','-ldflags','-X main.expectedUSBControl=owned-usb-pio-v1','./cmd/usbcheck']}
        self.assertTrue(entry.usb_control_option(record))
        for flags in ('','-X main.expectedUSBControl=no','main.expectedUSBControl=owned-usb-pio-v1',
                      '-X main.expectedUSBControl=owned-usb-pio-v1 -X main.expectedUSBControl=owned-usb-pio-v1'):
            bad=copy.deepcopy(record);bad['command'][3]=flags
            with self.subTest(flags=flags),self.assertRaises(ValueError):entry.usb_control_option(bad)
        for patch in ({'program':'hpsio'},{'usb_control':False},{'usb_control':1},{'command':[]},
                      {'command':['go','build','-ldflags','-X main.expectedUSBControl=owned-usb-pio-v1','./cmd/hpsio']}):
            with self.subTest(patch=patch),self.assertRaises(ValueError):entry.usb_control_option(dict(record,**patch))
        self.assertFalse(entry.usb_control_option({'program':'fpgaload'}))
        with self.assertRaises(ValueError):entry.usb_control_option(dict(record,program='fpgaload',usb_control=False))
