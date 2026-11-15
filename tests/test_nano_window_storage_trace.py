"""Require the complete frame/firmware return before fresh SD handback evidence."""
import unittest
import importlib.util
from pathlib import Path
import sys

ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location("nano_window_storage_trace",ROOT/"platform/embedded/nano/window_storage_trace.py")
trace=importlib.util.module_from_spec(spec)
sys.path.insert(0,str(ROOT/"platform/embedded/nano"))
try:spec.loader.exec_module(trace)
finally:sys.path.pop(0)
import test_nano_window_present as fixture


class WindowStorageTraceTests(unittest.TestCase):
    def setUp(self):
        f=fixture.WindowPresentationTraceTests();f.setUp();self.frames=f
        self.begin=f'FES_NATIVE_FPGA_WINDOW_STORAGE_RETURN BEGIN owner={fixture.OWNER:x} closed_generation=7\n'
        self.receipt=(f'FES_NATIVE_FPGA_WINDOW_STORAGE_RETURN PASS package_id={f.base.identity["package_id"]} '
                      f'owner={fixture.OWNER:x} closed_generation=7 resumed_generation=9 contained_generation=10 '
                      'source=sd fresh_selection=1 data_closed=1 memory_retained=1 runtime_admission=unavailable\n')
        self.raw=f.raw+self.begin+self.receipt

    def decode(self,data,**changes):
        f=self.frames.base
        options=dict(fpga_identity=f.identity,configuration=f.configuration,from_storage=True,
                     reader_memory=f.memory,window_read=f.contract,cpu_mapping=f.mapping,window_present=True)
        options.update(changes)
        return trace.decode(data,fixture.BUILD,fixture.SEED,**options)

    def test_complete_return_and_fresh_containment_are_both_required(self):
        r=self.decode(self.raw)
        self.assertTrue(r['complete'],r['errors']);self.assertEqual(r['last_checkpoint'],'window_storage_contained')
        self.assertTrue(r['window_read']['complete']);self.assertTrue(r['window_storage_return']['complete'])
        self.assertFalse(r['core_load_acceptance']);self.assertFalse(r['window_storage_return']['memory_released'])
        self.assertFalse(self.decode(self.frames.raw)['complete'])
        pending=self.decode(self.frames.raw+self.begin)
        self.assertEqual(pending['errors'],[]);self.assertFalse(pending['complete'])
        self.assertEqual(pending['last_checkpoint'],'window_storage_begin')
        for option in ('window_present','from_storage'):
            for value in (False,1,None,'yes'):
                with self.subTest(option=option,value=value),self.assertRaises(ValueError):self.decode(self.raw,**{option:value})

    def test_changed_owner_package_and_generations_reject(self):
        changes=[('owner='+format(fixture.OWNER,'x'),'owner='+format(fixture.OWNER+1,'x')),
                 ('closed_generation=7','closed_generation=6'),('resumed_generation=9','resumed_generation=8'),
                 ('contained_generation=10','contained_generation=9'),('resumed_generation=9','resumed_generation=18446744073709551616'),
                 (self.frames.base.identity['package_id'],'0'*64),('fresh_selection=1','fresh_selection=0'),
                 ('source=sd','source=cached'),('data_closed=1','data_closed=0'),('memory_retained=1','memory_retained=0'),
                 ('runtime_admission=unavailable','runtime_admission=available')]
        for before,after in changes:
            with self.subTest(before=before):
                self.assertFalse(self.decode(self.frames.raw+self.begin+self.receipt.replace(before,after))['complete'])
        self.assertFalse(self.decode(self.frames.raw+self.begin.replace('closed_generation=7','closed_generation=8')+self.receipt)['complete'])

    def test_missing_reordered_replayed_or_partial_evidence_rejects(self):
        for suffix in (self.receipt,self.receipt+self.begin,self.begin+self.begin+self.receipt,
                       self.begin+self.receipt+self.receipt,self.begin+self.receipt+'unexpected event\n',
                       self.begin+self.receipt[:-1]):
            with self.subTest(suffix=suffix):self.assertFalse(self.decode(self.frames.raw+suffix)['complete'])
        for line in self.frames.frames:
            self.assertFalse(self.decode(self.raw.replace(line,'',1))['complete'])
        for marker in ('FES_NANO_MASTER_STOP ','FES_NANO_WINDOW_CLOSURE ','FES_NATIVE_FPGA_WINDOW_PRESENT PASS '):
            changed=''.join(line for line in self.raw.splitlines(keepends=True) if marker not in line)
            self.assertFalse(self.decode(changed)['complete'])
        self.assertFalse(self.decode(self.begin+self.frames.raw+self.receipt)['complete'])
        for end in range(0,len(self.raw),511):self.assertFalse(self.decode(self.raw[:end])['complete'])
        self.assertFalse(self.decode(self.raw+'x'*(1<<20))['complete'])


if __name__=='__main__':unittest.main()
