"""Synthetic UART coverage, including corrupt/partial menu and secure return.

These fixtures grant no physical scanout or runtime acceptance.
"""
import copy
import hashlib
import json
from pathlib import Path
import unittest

import test_nano_window_trace as window

trace, BUILD, SEED, OWNER = window.trace, window.BUILD, window.SEED, window.OWNER
ROOT = Path(__file__).resolve().parents[1]


class MenuTraceTests(unittest.TestCase):
    def setUp(self):
        self.window = window.WindowTraceTests(); self.window.setUp()
        self.identity = dict(self.window.identity,control_abi=0x10203043)
        self.configuration = self.window.configuration
        self.memory = json.loads((ROOT/'platform/embedded/tamago/nativefabric/testdata/hps-menu-memory.json').read_bytes())
        self.memory.update(package_id=self.identity['package_id'],build_id=self.identity['build_id'])
        self.mapping = dict(self.window.mapping,reader_bytes=8<<20)
        self.contract = dict(self.window.contract,scope='sealed-cold-menu-read-v1',reader_memory_sha256=trace.context_digest(self.memory))
        pinned = (f'FES_NATIVE_FPGA_MENU_MEMORY PINNED cpu_base=95000000 bytes=8388608 slots=2 slot_bytes=4194304 '
                  f'map_sha256={trace.context_digest(self.memory)} initial_sha256={hashlib.sha256(bytes(8<<20)).hexdigest()} '
                  f'owner={OWNER:x} generation=4 fpga_access=held lifetime=until-reboot\n')
        self.prefix = ''.join(pinned if line.startswith('FES_NATIVE_FPGA_WINDOW_MEMORY') else line
                              for line in self.window.prefix.splitlines(keepends=True))
        self.prefix = self.prefix.replace('bytes=4194304 access=unqualified','bytes=8388608 access=unqualified')
        self.prefix = self.prefix.replace('reader_bytes=4194304','reader_bytes=8388608')
        self.prefix = self.prefix.replace('bytes='+str((256+8+4)*(1<<20)+256+0x10000),
                                         'bytes='+str((256+8+8)*(1<<20)+256+0x10000))
        self.elapsed = 0
        self.suffix = self.step(1,'open_begin') + self.scope('control',5,13)
        self.suffix += self.gate(5,13)*2 + self.scope('data',6,5) + self.step(1,'open_end')
        for index in range(1,4):
            self.suffix += self.step(index,'render_begin')+self.step(index,'render_end')+self.step(index,'present_begin')
            self.suffix += (f'FES_NATIVE_FPGA_MENU_PRESENT FRAME index={index} package_id={self.identity["package_id"]} '
                            f'build_id={self.identity["build_id"]} reader_generation=1 sequence={index} submitted={index} '
                            f'slot={(index-1)%2} underflows=0 memory_retained=1\n')
            self.suffix += self.step(index,'present_end')
        self.suffix += self.step(4,'return_begin') + self.scope('close',6,5)
        self.suffix += (f'NOTICE:  FES_NANO_MASTER_STOP status=0 owner={OWNER:x} generation=6 reset=5 '
                        'reader_reads=88 reader_polls=1 disable=0 probe_polls=2 cancel=0 complete=1 data=active\n')
        self.suffix += (f'NOTICE:  FES_NANO_WINDOW_CLOSURE owner={OWNER:x} generation=7 reset=f hardware_drained=8 '
                        'data_remained_contained=0 producers_quiescent=1 fabric_acknowledged=0\n')
        self.suffix += self.step(4,'return_end')
        self.suffix += (f'FES_NATIVE_FPGA_MENU_PRESENT PASS package_id={self.identity["package_id"]} '
                        f'build_id={self.identity["build_id"]} owner={OWNER:x} source_generation=4 closed_generation=7 '
                        f'frames=3 last_sequence=3 last_slot=0 access_map_sha256={trace.context_digest(self.contract)} '
                        'data_closed=1 memory_retained=1 runtime_admission=unavailable\n')
        self.raw = self.prefix+self.suffix

    def step(self,index,phase):
        self.elapsed += 1
        return f'FES_NATIVE_FPGA_MENU_PRESENT STEP index={index} phase={phase} elapsed_us={self.elapsed}\n'

    def scope(self,phase,generation,reset):
        return f'NOTICE:  FES_NANO_WINDOW_SCOPE phase={phase} owner={OWNER:x} generation={generation} reset={reset:x}\n'

    def gate(self,generation,reset):
        return self.window.gate(generation,reset).replace('bytes=4194304','bytes=8388608')

    def decode(self,raw,**changes):
        args = dict(fpga_identity=self.identity,configuration=self.configuration,from_storage=True,
                    reader_memory=self.memory,menu_read=self.contract,cpu_mapping=self.mapping)
        args.update(changes)
        return trace.decode_native_configuration(raw,BUILD,SEED,**args)

    def test_three_slot_acknowledgements_and_independent_secure_return(self):
        result = self.decode(self.raw)
        self.assertTrue(result['complete'],result['errors'])
        self.assertEqual(result['last_checkpoint'],'menu_present_closed')
        self.assertEqual([f['slot'] for f in result['menu_present']['frames']],[0,1,0])
        self.assertFalse(result['core_load_acceptance'])
        for key in ('memory_released','hdmi_qualified','runtime_admission'):
            self.assertFalse(result['menu_present'][key])

    def test_partial_failure_and_duplicate_events_cannot_complete(self):
        lines = self.suffix.splitlines(keepends=True)
        for i,line in enumerate(lines):
            if not line.strip(): continue
            with self.subTest(omitted=i):
                self.assertFalse(self.decode(self.prefix+''.join(lines[:i]+lines[i+1:]))['complete'])
            with self.subTest(replayed=i):
                self.assertFalse(self.decode(self.prefix+''.join(lines[:i]+[line]+lines[i:]))['complete'])
        for i in range(len(lines)):
            with self.subTest(prefix=i):
                self.assertFalse(self.decode(self.prefix+''.join(lines[:i]))['complete'])
        for raw in (self.raw[:-1],self.raw+'panic: late failure\n',
                    self.prefix+'FES_NATIVE_FPGA_MENU_PRESENT STEP index=1',
                    self.raw.replace('FES_NATIVE_FPGA_MENU_PRESENT FRAME index=2',
                        'FES_NATIVE_FPGA_MENU_PRESENT FAILURE operation=frame stage=poll-limit polls=64 flags=2 copied=3686400 attempted=true memory_retained=1\nFES_NATIVE_FPGA_MENU_PRESENT FRAME index=2')):
            self.assertFalse(self.decode(raw)['complete'])

    def test_bad_publication_owner_frame_gate_and_closure_fail(self):
        replacements = [('bytes=8388608','bytes=4194304'),('slots=2','slots=1'),('slot_bytes=4194304','slot_bytes=2097152'),
            ('initial_sha256='+hashlib.sha256(bytes(8<<20)).hexdigest(),'initial_sha256='+'a'*64),
            ('generation=4 fpga_access','generation=3 fpga_access'),('generation=5 reset=d','generation=6 reset=d'),
            ('cr0=0 ack=0','cr0=1 ack=1'),('limit=bfffffff','limit=95001fff'),('permit=owned-read','permit=denied'),
            ('sequence=2 submitted=2','sequence=1 submitted=2'),('reader_generation=1','reader_generation=2'),
            ('slot=1 underflows=0','slot=0 underflows=0'),('underflows=0','underflows=1'),
            ('reader_reads=88','reader_reads=44'),('reader_polls=1','reader_polls=2'),('disable=0','disable=1'),
            ('complete=1 data=active','complete=0 data=active'),('hardware_drained=8','hardware_drained=c'),
            ('producers_quiescent=1','producers_quiescent=0'),('fabric_acknowledged=0','fabric_acknowledged=1'),
            ('closed_generation=7','closed_generation=6'),('frames=3 last_sequence=3','frames=2 last_sequence=3'),
            ('access_map_sha256='+trace.context_digest(self.contract),'access_map_sha256='+'0'*64),
            ('data_closed=1','data_closed=0'),('memory_retained=1','memory_retained=0'),
            ('runtime_admission=unavailable','runtime_admission=available'),('elapsed_us=1\n','elapsed_us=240000001\n')]
        for old,new in replacements:
            self.assertIn(old,self.raw)
            with self.subTest(field=old):
                self.assertFalse(self.decode(self.raw.replace(old,new))['complete'])
        self.assertFalse(self.decode(self.raw.replace(f'owner={OWNER:x}', 'owner=1',1))['complete'])

    def test_missing_or_bad_boot_and_configuration_prefix_cannot_grant_menu(self):
        for raw in (self.suffix,self.raw.replace('FES_NANO_HANDOFF PASS serial-only',''),
                    self.raw.replace('phase=complete','phase=incomplete')):
            self.assertNotEqual(raw,self.raw)
            self.assertFalse(self.decode(raw)['complete'])
        for raw in (self.raw.encode(),bytearray(self.raw.encode()),memoryview(self.raw.encode())):
            self.assertTrue(self.decode(raw)['complete'])
        self.assertFalse(self.decode('x'*(1+(1<<20)))['complete'])

    def test_complete_maps_and_separate_explicit_selection_are_required(self):
        for key,value in self.contract.items():
            changed = copy.deepcopy(self.contract); changed[key] = True if type(value) is int else 'changed'
            with self.subTest(field=key),self.assertRaises(ValueError):
                self.decode(self.raw,menu_read=changed)
        for key,value in [('from_storage',False),('cpu_mapping',None),('ddr_read',{}),('computer',{}),
                          ('window_read',self.window.contract),('window_present',True),('computer_reload',True),
                          ('reader_memory',self.window.memory),('fpga_identity',self.window.identity)]:
            with self.subTest(mode=key),self.assertRaises(ValueError): self.decode(self.raw,**{key:value})
        with self.assertRaises(ValueError): self.decode(self.raw,menu_read=None)


if __name__=='__main__': unittest.main()
