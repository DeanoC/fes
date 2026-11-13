"""Explicit service selection must never imply ordinary admission or bounded HIL."""
from pathlib import Path
import importlib.util
import unittest

ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('window_service_entry',ROOT/'platform/embedded/entry/build.py')
entry=importlib.util.module_from_spec(spec);spec.loader.exec_module(entry)

class WindowServiceSelectionTests(unittest.TestCase):
    def receipt(self,flags='-X main.expectedWindowService=1'):
        return dict(window_service=True,window_storage_return=True,window_present=True,window_read=True,window_memory=True,
                    window_reload=False,command=['compiler','build','-ldflags',flags])

    def test_complete_selection_and_exact_link_assignment(self):
        self.assertTrue(entry.window_service_option(self.receipt()))
        self.assertFalse(entry.window_service_option(dict(command=['compiler','build','-ldflags','-s -w'])))
        for key,value in [('window_service',1),('window_service',None),('window_storage_return',False),
                          ('window_storage_return',1),('window_present',False),('window_read',False),
                          ('window_memory',False),('window_reload',True),('window_reload',1),('command',[])]:
            with self.subTest(key=key,value=value),self.assertRaises(ValueError):
                entry.window_service_option(dict(self.receipt(),**{key:value}))
        for flags in ['', 'main.expectedWindowService=1','-X=main.expectedWindowService=1',
                      '-X main.expectedWindowService=0','-X main.expectedWindowService=1 -X main.expectedWindowService=1']:
            with self.subTest(flags=flags),self.assertRaises(ValueError):entry.window_service_option(self.receipt(flags))
        with self.assertRaises(ValueError):entry.window_service_option(dict(self.receipt(),window_service=False))

if __name__=='__main__':unittest.main()
