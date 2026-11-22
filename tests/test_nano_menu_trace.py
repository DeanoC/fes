"""Synthetic UART coverage, including corrupt/partial menu and secure return.

These fixtures grant no physical scanout or runtime acceptance.
"""
import copy
import hashlib
import json
from pathlib import Path
import unittest

import test_nano_native_trace as fixtures
import test_nano_cpu_mapping as mapping_fixtures

trace, BUILD, SEED, OWNER = fixtures.trace, fixtures.BUILD, fixtures.SEED, fixtures.OWNER
ROOT = Path(__file__).resolve().parents[1]


class MenuTraceTests(unittest.TestCase):
    def setUp(self):
        base = fixtures.NativeContainedConfigurationTraceTests()
        self.identity = dict(base.identity, control_abi=0x10203043)
        self.configuration = base.configuration
        self.memory = json.loads((ROOT/'platform/embedded/tamago/nativefabric/testdata/hps-menu-memory.json').read_bytes())
        self.memory.update(package_id=self.identity['package_id'], build_id=self.identity['build_id'])
        self.mapping = dict(format=2, scope='pinned-tamago-el1-resources-excluded-v2', ram_base=0x84000000, ram_bytes=256<<20,
                            control_base=0x20010400, control_bytes=256, dma_base=0x94800000, dma_bytes=8<<20,
                            reader_base=0x95000000, reader_bytes=8<<20, firmware_base=0x50000, firmware_bytes=0x10000)
        self.contract = dict(format=1, scope='sealed-cold-menu-read-v1', package_id=self.identity['package_id'],
                             build_id=self.identity['build_id'], reader_memory_sha256=trace.context_digest(self.memory),
                             control_map_sha256=self.identity['map_sha256'], adapter_source_sha256='a'*64,
                             adapter_generated_sha256='b'*64, arcache=2, arprot=3, aruser=224, read_bytes=3686400,
                             open_mask=8, close_mask=10)
        pinned = (f'FES_NATIVE_FPGA_MENU_MEMORY PINNED cpu_base=95000000 bytes=8388608 slots=2 slot_bytes=4194304 '
                  f'map_sha256={trace.context_digest(self.memory)} initial_sha256={hashlib.sha256(bytes(8<<20)).hexdigest()} '
                  f'owner={OWNER:x} generation=4 fpga_access=held lifetime=until-reboot\n')
        prefix = base.stored_capture().split('NOTICE:  FES_NANO_IDENTITY phase=open')[0]
        prefix = prefix.replace('FES_NATIVE_HPS_CONTEXT', pinned+'FES_NATIVE_HPS_CONTEXT', 1)
        marker = mapping_fixtures.CPUMappingTraceTests.marker(self, bytes=(256+8+8)*(1<<20)+256+0x10000)
        prefix = prefix.replace('FES_NATIVE_HPS_CONTEXT', marker+'FES_NATIVE_HPS_CONTEXT', 1)
        self.prefix = prefix + self.gate(4, 15, permit=False)
        self.elapsed = 0
        # The real firmware queries readiness and the initial route inside
        # OpenMenuSession, after its native progress marker and before control.
        split = self.prefix.index('FES_NANO_CONFIG_QUERY')
        readiness, self.prefix = self.prefix[split:], self.prefix[:split]
        self.suffix = self.step(1,'open_begin') + readiness + self.scope('control',5,13)
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

    def gate(self,generation,reset,permit=True):
        result=('NOTICE:  FES_NANO_SMMU status=0 reads=26 idr0=080f7e3f idr1=0e739d18 aidr=1 s_idr1=80000018 '
                'cr0=0 ack=0 gbpa=0 agbpa=0 s_cr0=0 s_ack=0 s_init=0 s_gbpa=0 s_agbpa=0 '
                'identified=1 settled=1 bypass_controls_clear=1 access=unqualified\n')
        result+=(f'NOTICE:  FES_NANO_DDR_FIREWALL status=0 owner={OWNER:x} generation={generation} reset={reset:x} '
                 'mpfe_reset=0 reads=66 enable=1 base=95000000 bytes=8388608 access=unqualified\n')
        for i in range(8):
            result+=f'NOTICE:  FES_NANO_DDR_REGION index={i} base={"80000000" if not i else "0"} baseext=0 limit={"bfffffff" if not i else "ffff"} limitext=0\n'
        result+='NOTICE:  FES_NANO_DDR_FIREWALL_END layout_known=1 window_covered=1 access=unqualified\n'
        if permit:
            result+=(f'NOTICE:  FES_NANO_WINDOW_GATE status=0 owner={OWNER:x} generation={generation} reset={reset:x} '
                     'bytes=8388608 permit=owned-read\n')
        return result

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

    def test_readiness_is_inside_open_and_pre_io_firewall_failure_stays_rejected(self):
        start = self.suffix.index('FES_NANO_CONFIG_QUERY')
        end = self.suffix.index('NOTICE:  FES_NANO_WINDOW_SCOPE phase=control')
        readiness = self.suffix[start:end]
        # Do not accept the earlier synthetic fixture's misplaced ready prefix.
        misplaced = self.prefix+readiness+self.suffix[:start]+self.suffix[end:]
        self.assertFalse(self.decode(misplaced)['complete'])
        failed = self.raw.replace('FES_NANO_DDR_FIREWALL status=0','FES_NANO_DDR_FIREWALL status=-22',1)
        failed = failed.replace('reads=66 enable=1','reads=0 enable=0',1)
        result = self.decode(failed)
        self.assertFalse(result['complete'])
        self.assertIn('window use-point firewall observation differs',result['errors'])
        values = next(r['values'] for r in result['menu_present']['records'] if r['stage']=='ddr_firewall')
        self.assertEqual((values['status'],values['reads'],values['bytes']),(-22,0,8<<20))

    def test_complete_maps_and_separate_explicit_selection_are_required(self):
        for key,value in self.contract.items():
            changed = copy.deepcopy(self.contract); changed[key] = True if type(value) is int else 'changed'
            with self.subTest(field=key),self.assertRaises(ValueError):
                self.decode(self.raw,menu_read=changed)
        for key,value in [('from_storage',False),('cpu_mapping',None),('ddr_read',{}),('computer',{}),
                          ('computer_storage_return',0),('computer_activity',0),('computer_reload',True),
                          ('reader_memory',dict(self.memory,bytes=4<<20,arena_bytes=4<<20)),
                          ('fpga_identity',dict(self.identity,control_abi=0x10203041))]:
            with self.subTest(mode=key),self.assertRaises(ValueError): self.decode(self.raw,**{key:value})
        with self.assertRaises(ValueError): self.decode(self.raw,menu_read=None)


if __name__=='__main__': unittest.main()
