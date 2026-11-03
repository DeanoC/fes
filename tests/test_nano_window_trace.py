"""Host-only full-frame trace fixtures; none establish physical acceptance."""
import copy
import json
from pathlib import Path
import unittest

import test_nano_native_trace as fixtures
import test_nano_cpu_mapping as mapping_fixtures

trace,BUILD,SEED,OWNER=fixtures.trace,fixtures.BUILD,fixtures.SEED,fixtures.OWNER

ROOT=Path(__file__).resolve().parents[1]


class WindowTraceTests(unittest.TestCase):
    def setUp(self):
        base=fixtures.NativeContainedConfigurationTraceTests()
        self.identity=dict(base.identity,control_abi=0x10203042)
        self.configuration=base.configuration
        self.memory=json.loads((ROOT/'platform/embedded/tamago/nativefabric/testdata/hps-window-memory.json').read_bytes())
        self.memory.update(package_id=self.identity['package_id'],build_id=self.identity['build_id'])
        self.mapping=dict(format=1,scope='pinned-tamago-el1-resources-v1',ram_base=0x84000000,ram_bytes=256<<20,
            control_base=0x20010400,control_bytes=256,dma_base=0x94800000,dma_bytes=8<<20,
            reader_base=0x95000000,reader_bytes=4<<20)
        self.contract=dict(format=1,scope='sealed-cold-window-read-v1',package_id=self.identity['package_id'],
            build_id=self.identity['build_id'],reader_memory_sha256=trace.context_digest(self.memory),
            control_map_sha256=self.identity['map_sha256'],adapter_source_sha256='a'*64,
            adapter_generated_sha256='b'*64,arcache=2,arprot=3,aruser=224,read_bytes=3686400,open_mask=8,close_mask=10)
        self.prefix=base.stored_capture().split('NOTICE:  FES_NANO_IDENTITY phase=open')[0]
        pinned=(f'FES_NATIVE_FPGA_WINDOW_MEMORY PINNED cpu_base=95000000 bus_base=95000000 bytes=4194304 '
                f'frame_bytes=3686400 row_bytes=5120 stride=5120 rows=720 map_sha256={trace.context_digest(self.memory)} '
                f'frame_sha256={"a"*64} crc32=12345678 owner={OWNER:x} generation=4 '
                'fpga_access=held lifetime=until-reboot renderer=tenfoot\n')
        self.prefix=self.prefix.replace('FES_NATIVE_HPS_CONTEXT',pinned+'FES_NATIVE_HPS_CONTEXT',1)
        marker=mapping_fixtures.CPUMappingTraceTests.marker(self,bytes=(256+8+4)*(1<<20)+256)
        self.prefix=self.prefix.replace('FES_NATIVE_HPS_CONTEXT',marker+'FES_NATIVE_HPS_CONTEXT',1)
        self.prefix+=self.gate(4,15,False)
        self.suffix=f'NOTICE:  FES_NANO_WINDOW_SCOPE phase=control owner={OWNER:x} generation=5 reset=d\n'
        self.suffix+=self.gate(5,13)+self.gate(5,13)
        for phase in ('data','close'):
            self.suffix+=f'NOTICE:  FES_NANO_WINDOW_SCOPE phase={phase} owner={OWNER:x} generation=6 reset=5\n'
        self.suffix+=(f'NOTICE:  FES_NANO_MASTER_STOP status=0 owner={OWNER:x} generation=6 reset=5 '
                      'reader_reads=132 reader_polls=2 disable=1 probe_polls=2 cancel=0 complete=1 data=active\n')
        self.suffix+=(f'NOTICE:  FES_NANO_WINDOW_CLOSURE owner={OWNER:x} generation=7 reset=f hardware_drained=8 '
                      'data_remained_contained=0 producers_quiescent=1 fabric_acknowledged=0\n')
        self.suffix+=(f'FES_NATIVE_FPGA_WINDOW_READ PASS package_id={self.identity["package_id"]} '
            f'build_id={self.identity["build_id"]} owner={OWNER:x} source_generation=4 closed_generation=7 '
            f'reader_generation=1 cookie=1 bytes=3686400 crc32=12345678 frame_sha256={"a"*64} '
            f'access_map_sha256={trace.context_digest(self.contract)} data_closed=1 memory_retained=1 runtime_admission=unavailable\n')
        self.raw=self.prefix+self.suffix

    def gate(self,generation,reset,permit=True):
        result=('NOTICE:  FES_NANO_SMMU status=0 reads=26 idr0=080f7e3f idr1=0e739d18 aidr=1 s_idr1=80000018 '
                'cr0=0 ack=0 gbpa=0 agbpa=0 s_cr0=0 s_ack=0 s_init=0 s_gbpa=0 s_agbpa=0 '
                'identified=1 settled=1 bypass_controls_clear=1 access=unqualified\n')
        result+=(f'NOTICE:  FES_NANO_DDR_FIREWALL status=0 owner={OWNER:x} generation={generation} reset={reset:x} '
                 'mpfe_reset=0 reads=66 enable=1 base=95000000 bytes=4194304 access=unqualified\n')
        for i in range(8):
            result+=f'NOTICE:  FES_NANO_DDR_REGION index={i} base={"80000000" if not i else "0"} baseext=0 limit={"bfffffff" if not i else "ffff"} limitext=0\n'
        result+='NOTICE:  FES_NANO_DDR_FIREWALL_END layout_known=1 window_covered=1 access=unqualified\n'
        if permit:
            result+=(f'NOTICE:  FES_NANO_WINDOW_GATE status=0 owner={OWNER:x} generation={generation} reset={reset:x} '
                     'bytes=4194304 permit=owned-read\n')
        return result

    def decode(self,raw,**changes):
        args=dict(fpga_identity=self.identity,configuration=self.configuration,from_storage=True,
                  reader_memory=self.memory,window_read=self.contract,cpu_mapping=self.mapping)
        args.update(changes)
        return trace.decode_native_configuration(raw,BUILD,SEED,**args)

    def test_full_frame_and_return_are_a_distinct_selected_observation(self):
        result=self.decode(self.raw)
        self.assertTrue(result['complete'],result)
        self.assertFalse(result['errors'])
        self.assertEqual(result['last_checkpoint'],'window_read_closed')
        self.assertEqual(result['window_read']['receipt']['closed_generation'],7)
        self.assertFalse(result['core_load_acceptance'])
        self.assertFalse(result['window_read']['memory_released'])
        with self.assertRaises(ValueError):self.decode(self.raw,window_read=None)

    def test_required_events_cannot_be_removed_or_repeated_and_prefixes_stay_incomplete(self):
        lines=self.raw.splitlines(keepends=True)
        for i,line in enumerate(lines):
            if not any(tag in line for tag in ('FES_NANO_','FES_NATIVE_','FES_ENTRY','U-Boot ','Trying to boot from ','DRAM:','SOCFPGA_AGILEX5 #')):continue
            if 'FES_NANO_CORE_TRANSFER phase=poll ' in line or 'FES_NANO_CORE_TRANSFER phase=status ' in line:continue
            with self.subTest(omitted=i):
                result=self.decode(''.join(lines[:i]+lines[i+1:]))
                self.assertFalse(result['complete'])
            if any(key in line for key in ('WINDOW_','MASTER_STOP','DDR_REGION','SMMU')):
                with self.subTest(duplicate=i):
                    self.assertFalse(self.decode(''.join(lines[:i]+[line]+lines[i:]))['complete'])
        for i in range(len(lines)):
            self.assertFalse(self.decode(''.join(lines[:i]))['complete'])
        self.assertFalse(self.decode(self.raw[:-1])['complete'])
        self.assertFalse(self.decode(self.raw+'panic: late error\n')['complete'])

    def test_gate_memory_frame_ownership_and_drain_damage_is_rejected(self):
        replacements=[
            ('bytes=4194304','bytes=8192'),('row_bytes=5120','row_bytes=2560'),
            ('generation=5 reset=d','generation=6 reset=d'),('generation=6 reset=5','generation=6 reset=d'),
            ('generation=4 fpga_access=held','generation=3 fpga_access=held'),
            ('cr0=0 ack=0','cr0=1 ack=1'),('idr0=080f7e3f','idr0=080f7e3e'),
            ('limit=bfffffff','limit=95001fff'),('mpfe_reset=0','mpfe_reset=1'),
            ('permit=owned-read','permit=denied'),('reader_reads=132','reader_reads=88'),
            ('reader_polls=2','reader_polls=129'),('disable=1','disable=0'),
            ('complete=1 data=active','complete=0 data=active'),('closed_generation=7','closed_generation=6'),
            ('reader_generation=1','reader_generation=2'),('cookie=1','cookie=0'),
            ('bytes=3686400 crc32=12345678','bytes=32 crc32=12345678'),
            ('access_map_sha256='+trace.context_digest(self.contract),'access_map_sha256='+'0'*64),
            ('data_closed=1','data_closed=0'),('memory_retained=1','memory_retained=0'),
            ('runtime_admission=unavailable','runtime_admission=available')]
        replacements += [('hardware_drained=8','hardware_drained=c'),('data_remained_contained=0','data_remained_contained=1'),
                         ('producers_quiescent=1','producers_quiescent=0'),('fabric_acknowledged=0','fabric_acknowledged=1')]
        for old,new in replacements:
            self.assertIn(old,self.raw)
            with self.subTest(field=old):
                self.assertFalse(self.decode(self.raw.replace(old,new))['complete'])
        self.assertFalse(self.decode(self.raw.replace('crc32=12345678','crc32=12345679',1))['complete'])
        self.assertFalse(self.decode(self.raw.replace('frame_sha256='+'a'*64,'frame_sha256='+'b'*64,1))['complete'])
        self.assertFalse(self.decode(self.raw.replace(f'owner={OWNER:x} source_generation=4','owner=1 source_generation=4'))['complete'])

    def test_maps_and_modes_cannot_inherit_the_fixed_reader_recipe(self):
        for key,value in self.contract.items():
            changed=copy.deepcopy(self.contract);changed[key]=True if type(value) is int else 'changed'
            with self.subTest(key=key),self.assertRaises(ValueError):self.decode(self.raw,window_read=changed)
        for key,value in [('from_storage',False),('cpu_mapping',None),('ddr_read',{}),('computer',{}),('computer_reload',True),('computer_activity',True)]:
            with self.subTest(key=key),self.assertRaises(ValueError):self.decode(self.raw,**{key:value})
        for changed in [dict(self.memory,arena_bytes=2<<20),dict(self.memory,bytes=8192)]:
            with self.assertRaises(ValueError):self.decode(self.raw,reader_memory=changed)
