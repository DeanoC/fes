"""Serial Tenfoot selects an application, without granting an HDMI path."""
from pathlib import Path
import importlib.util
import unittest

ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('tenfoot_serial_entry',ROOT/'platform/embedded/entry/build.py')
entry=importlib.util.module_from_spec(spec);spec.loader.exec_module(entry)

class TenfootSerialSelectionTests(unittest.TestCase):
    def receipt(self,flags='-X main.expectedTenfootSerial=1'):
        return dict(tenfoot_serial=True,selected_programs=True,computer_reload=True,computer_storage_return=True,
                    hps_computer={},window_memory=False,command=['compiler','build','-tags=de25nano,de25tenfootserial','-ldflags',flags])

    def test_exact_selection_tags_and_link_command(self):
        self.assertTrue(entry.tenfoot_serial_option(self.receipt()))
        self.assertFalse(entry.tenfoot_serial_option(dict(command=['compiler','build','-tags=de25nano','-ldflags','-s -w'])))
        for key,value in [('tenfoot_serial',1),('tenfoot_serial',None),('selected_programs',False),
                          ('selected_programs',1),('computer_reload',False),('computer_storage_return',False),
                          ('hps_computer',None),('window_memory',True),('window_memory',1),('command',[])]:
            with self.subTest(key=key,value=value),self.assertRaises(ValueError):
                entry.tenfoot_serial_option(dict(self.receipt(),**{key:value}))
        for flags in ['', 'main.expectedTenfootSerial=1','-X=main.expectedTenfootSerial=1',
                      '-X main.expectedTenfootSerial=0','-X main.expectedTenfootSerial=1 -X main.expectedTenfootSerial=1']:
            with self.subTest(flags=flags),self.assertRaises(ValueError):entry.tenfoot_serial_option(self.receipt(flags))
        with self.assertRaises(ValueError):entry.tenfoot_serial_option(dict(self.receipt(),tenfoot_serial=False))
        bad=self.receipt();bad['command']=[s for s in bad['command'] if not s.startswith('-tags=')]
        with self.assertRaises(ValueError):entry.tenfoot_serial_option(bad)
        bad=self.receipt();bad['command'].append('-tags=other')
        with self.assertRaises(ValueError):entry.tenfoot_serial_option(bad)

if __name__=='__main__':unittest.main()
