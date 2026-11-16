"""Source-bound EL1 mapping observations, without board or admission claims."""
import copy
import re
import unittest
from unittest.mock import patch

from test_nano_hps_context import context
import test_nano_native_trace as fixtures
from test_nano_native_trace import BUILD, OWNER, SEED, trace


class CPUMappingContractTests(unittest.TestCase):
    def setUp(self):
        self.base=fixtures.NativeComputerTraceTests();self.base.setUp()
        self.control={'cpu_base':0x20010400,'span':256}
        self.mapping=context.cpu_mapping(self.control,self.base.configuration,self.base.memory)

    def test_layout_is_derived_from_exact_separate_source_resources(self):
        self.assertEqual(self.mapping,{
            'format':2,'scope':'pinned-tamago-el1-resources-excluded-v2',
            'ram_base':0x84000000,'ram_bytes':256<<20,'control_base':0x20010400,'control_bytes':256,
            'dma_base':0x94800000,'dma_bytes':8<<20,'reader_base':0x95000000,'reader_bytes':2<<20,'firmware_base':0x50000,'firmware_bytes':0x10000})
        without=context.cpu_mapping(self.control,self.base.configuration,None)
        self.assertEqual((without['reader_base'],without['reader_bytes']),(0,0))
        for index,key,value in ((0,'cpu_base',0x20010300),(0,'span',128),(0,'span',True),
                                (1,'base',0x94000000),(1,'size',4<<20),
                                (2,'arena_base',0x95001000),(2,'arena_bytes',8192)):
            inputs=copy.deepcopy([self.control,self.base.configuration,self.base.memory]);inputs[index][key]=value
            with self.subTest(index=index,key=key),self.assertRaises(ValueError):context.cpu_mapping(*inputs)
        for inputs in ((None,self.base.configuration,None),(self.control,{},None),
                       (self.control,self.base.configuration,{})):
            with self.subTest(inputs=inputs),self.assertRaises(ValueError):context.cpu_mapping(*inputs)

    def test_new_load_receipts_require_the_complete_source_derived_mapping(self):
        record={'program':'fpgaload','hps_control':self.control,'cpu_mapping':self.mapping,
                'cpu_mapping_sha256':context.digest(self.mapping)}
        with patch.object(context,'checked_configuration',return_value=self.base.configuration) as configuration, \
                patch.object(context,'checked_reader_memory',return_value=self.base.memory) as memory:
            self.assertEqual(context.checked_cpu_mapping(record),self.mapping)
            configuration.assert_called_once_with(record,'program');memory.assert_called_once_with(record,'program')
            entry=dict(record,native_program='fpgaload');entry.pop('program')
            self.assertEqual(context.checked_cpu_mapping(entry,'native_program'),self.mapping)
            for key in ('cpu_mapping','cpu_mapping_sha256'):
                changed=dict(record);changed.pop(key)
                with self.subTest(missing=key),self.assertRaises(ValueError):context.checked_cpu_mapping(changed)
            for key,value in (('format',True),('ram_base',0x80000000),('ram_bytes',128<<20),
                              ('control_bytes',128),('dma_base',0x94000000),('reader_bytes',8192),('firmware_base',0x60000),('firmware_bytes',0),('format',1)):
                changed=dict(record,cpu_mapping=dict(self.mapping,**{key:value}))
                with self.subTest(key=key),self.assertRaises(ValueError):context.checked_cpu_mapping(changed)
            with self.assertRaises(ValueError):context.checked_cpu_mapping(dict(record,cpu_mapping_sha256='0'*64))
            for dependency in ('checked_configuration','checked_reader_memory'):
                with patch.object(context,dependency,side_effect=ValueError('unverified source')),self.assertRaises(ValueError):
                    context.checked_cpu_mapping(record)
        for program in ('probe','bootcontext','fpgastatus','hpsio'):
            self.assertIsNone(context.checked_cpu_mapping({'program':program}))
            with self.subTest(program=program),self.assertRaises(ValueError):
                context.checked_cpu_mapping(dict(record,program=program))


class CPUMappingTraceTests(unittest.TestCase):
    def setUp(self):
        self.base=fixtures.NativeComputerTraceTests();self.base.setUp()
        self.mapping=context.cpu_mapping({'cpu_base':0x20010400,'span':256},self.base.configuration,self.base.memory)

    def marker(self,**changes):
        values=dict(el=1,sctlr=0xc5183d,tcr=0x200003519,root=0x84004000,mair=0x44ff00,mmfr0=0x1122,
                    mpidr=0x81000000,reads=3840,translations=5648,bytes=(256+8+2)*(1<<20)+256+0x10000,
                    descriptor_sha256='a'*64,owner=OWNER,generation=4)
        values.update({key:value for key,value in self.mapping.items() if key not in ('format','scope')})
        values.update(changes)
        decimal={'el','ram_bytes','control_bytes','dma_bytes','reader_bytes','firmware_bytes','reads','translations','bytes','generation'}
        fields=('el','sctlr','tcr','root','mair','mmfr0','mpidr','ram_base','ram_bytes','control_base','control_bytes',
                'dma_base','dma_bytes','reader_base','reader_bytes','firmware_base','firmware_bytes','reads','translations','bytes','descriptor_sha256','owner','generation')
        return ('FES_NATIVE_CPU_MAPPING PASS '+' '.join(key+'='+
                (values[key] if key=='descriptor_sha256' else str(values[key]) if key in decimal else format(values[key],'x'))
                for key in fields)+' data_held=1 runtime_admission=unavailable\r\n')

    def capture(self,marker=None):
        return self.base.capture().replace('FES_NATIVE_HPS_CONTEXT',
            (self.marker() if marker is None else marker)+'FES_NATIVE_HPS_CONTEXT',1)

    def decode(self,raw):return self.base.decode(raw,cpu_mapping=self.mapping)

    def test_full_observation_is_required_before_the_first_transfer(self):
        report=self.decode(self.capture())
        self.assertTrue(report['complete'],report)
        value=report['cpu_mapping_observation']
        self.assertEqual(value['scope'],'EL1-resource-mapping-and-firmware-exclusion-only')
        self.assertEqual(value['bytes'],(256+8+2)*(1<<20)+256+0x10000)
        self.assertFalse(value['runtime_admission']);self.assertFalse(report['core_load_acceptance'])
        self.assertFalse(self.decode(self.base.capture())['complete'])
        self.assertFalse(self.base.decode(self.capture())['complete'])
        for marker in (self.marker()*2,self.marker().replace(' PASS ',' READY '),
                       self.marker().replace('data_held=1','data_held=0'),
                       self.marker().replace('runtime_admission=unavailable','runtime_admission=ready')):
            with self.subTest(marker=marker):self.assertFalse(self.decode(self.capture(marker))['complete'])
        raw=self.capture();marker=self.marker()
        # A later mapping record cannot retroactively guard the transfer.
        moved=raw.replace(marker,'').replace('FES_NATIVE_COMPUTER_CONTROL',marker+'FES_NATIVE_COMPUTER_CONTROL')
        self.assertFalse(self.decode(moved)['complete'])
        prefix=raw[:raw.index(marker)]
        for end in (0,1,len(marker)-2,len(marker)-1):
            with self.subTest(end=end):self.assertFalse(self.decode(prefix+marker[:end])['complete'])

    def test_changed_cpu_translation_resources_or_owner_reject(self):
        changes=(('el',2),('sctlr',0xc5183c),('sctlr',0xc5183d|1<<19),('sctlr',0xc5183d|1<<25),
                 ('tcr',0x20000351a),('root',0x84005000),('mair',0xff00),('mmfr0',0x1121),
                 ('mmfr0',0x20001122),('mpidr',0x81000100),('ram_base',0x80000000),('ram_bytes',128<<20),
                 ('control_base',0x20010300),('control_bytes',128),('dma_base',0x94000000),('dma_bytes',4<<20),
                 ('reader_base',0),('reader_bytes',8192),('firmware_base',0x4f000),('firmware_bytes',0),('reads',0),('reads',16385),('translations',0),
                 ('translations',16385),('bytes',0),('descriptor_sha256','0'*64),('owner',OWNER+2),('generation',1))
        for key,value in changes:
            with self.subTest(key=key,value=value):
                report=self.decode(self.capture(self.marker(**{key:value})))
                self.assertFalse(report['complete']);self.assertTrue(report['errors'])
        for key,value in (('format',True),('reader_base',0),('control_bytes',128)):
            with self.subTest(contract=key),self.assertRaises(ValueError):
                self.base.decode(self.capture(),cpu_mapping=dict(self.mapping,**{key:value}))

    def test_missing_exclusion_or_old_resource_contract_cannot_qualify(self):
        for removed in (' firmware_base=50000', ' firmware_bytes=65536',
                        ' firmware_base=50000 firmware_bytes=65536'):
            with self.subTest(removed=removed):
                self.assertFalse(self.decode(self.capture(self.marker().replace(removed, '')))['complete'])
        old = dict(self.mapping, format=1, scope='pinned-tamago-el1-resources-v1')
        old.pop('firmware_base');old.pop('firmware_bytes')
        with self.assertRaises(ValueError):
            self.base.decode(self.capture(), cpu_mapping=old)

    def test_two_loads_keep_one_mapping_observation_and_fresh_cycle_evidence(self):
        reload=fixtures.NativeComputerReloadTraceTests();reload.setUp()
        raw=reload.capture().replace('FES_NATIVE_FPGA_CYCLE BEGIN cycle=1',self.marker()+'FES_NATIVE_FPGA_CYCLE BEGIN cycle=1',1)
        options=dict(fpga_identity=self.base.identity,configuration=self.base.configuration,
                     reader_memory=self.base.memory,computer=self.base.computer,cpu_mapping=self.mapping)
        report=trace.decode_native_computer_reload(raw,BUILD,SEED,**options)
        self.assertTrue(report['complete'],report)
        self.assertIn('cpu_mapping_observation',report['cycles'][0]['report'])
        self.assertNotIn('cpu_mapping_observation',report['cycles'][1]['report'])
        self.assertTrue(trace.decode_native_configuration(raw,BUILD,SEED,**options,from_storage=True,
            computer_activity=True,computer_reload=True)['complete'])
        for changed in (raw.replace(self.marker(),''),raw.replace(self.marker(),self.marker()*2),
                        raw.replace('FES_NATIVE_FPGA_CYCLE BEGIN cycle=2',self.marker()+'FES_NATIVE_FPGA_CYCLE BEGIN cycle=2')):
            with self.subTest(length=len(changed)):
                self.assertFalse(trace.decode_native_computer_reload(changed,BUILD,SEED,**options)['complete'])


class StorageReturnTraceTests(unittest.TestCase):
    """Fresh SD ownership must join the two independently verified core cycles."""
    def setUp(self):
        self.mapping_fixture=CPUMappingTraceTests();self.mapping_fixture.setUp()
        self.reload=fixtures.NativeComputerReloadTraceTests();self.reload.setUp()
        base=self.mapping_fixture.base
        self.options=dict(fpga_identity=base.identity,configuration=base.configuration,
            reader_memory=base.memory,computer=base.computer,cpu_mapping=self.mapping_fixture.mapping)
        self.package_id=base.configuration['package_id']

    def markers(self):
        return (
            f'FES_NATIVE_STORAGE_RETURN BEGIN owner={OWNER:x} generation=6 package_id={self.package_id} data_held=1\r\n'
            f'FES_NATIVE_STORAGE_RETURN ACTIVE owner={OWNER:x} generation=8 return_generation=6 writes=disabled\r\n'
            f'FES_NATIVE_STORAGE_RETURN CONTAINED owner={OWNER:x} generation=9 return_generation=6 package_id={self.package_id} source=sd writes=disabled\r\n')

    def capture(self):
        raw=self.reload.capture().replace('FES_NATIVE_FPGA_CYCLE BEGIN cycle=1',
            self.mapping_fixture.marker()+'FES_NATIVE_FPGA_CYCLE BEGIN cycle=1',1)
        start=raw.index('FES_NATIVE_FPGA_CYCLE BEGIN cycle=2')
        # The new SD grant, prepare and stop consume three additional generations.
        second=re.sub(r'\bgeneration=([678])\b',lambda m:'generation='+str(int(m[1])+3),raw[start:])
        return raw[:start]+self.markers()+second

    def decode(self,raw,**changes):
        return trace.decode_native_computer_reload(raw,BUILD,SEED,
            **(dict(self.options,computer_storage_return=True)|changes))

    def test_owned_return_reselection_and_reconfiguration_have_exact_lineage(self):
        report=self.decode(self.capture())
        self.assertTrue(report['complete'],report)
        observation=report['storage_return_observation']
        self.assertTrue(observation['complete']);self.assertFalse(observation['runtime_admission'])
        self.assertEqual([r['values']['generation'] for r in observation['records']],[6,8,9])
        self.assertEqual([c['report']['fabric_activation']['generation'] for c in report['cycles']],[4,9])
        self.assertEqual([c['report']['master_stop_observation']['generation'] for c in report['cycles']],[5,10])
        self.assertEqual([c['report']['computer_control_observation']['generation'] for c in report['cycles']],[6,11])
        self.assertFalse(report['core_load_acceptance'])
        self.assertTrue(trace.decode_native_configuration(self.capture(),BUILD,SEED,**self.options,
            from_storage=True,computer_activity=True,computer_reload=True,computer_storage_return=True)['complete'])

    def test_partial_captures_cannot_complete_or_invent_failure(self):
        raw=self.capture();offset=0
        for line in raw.splitlines(keepends=True)[:-1]:
            offset+=len(line)
            with self.subTest(offset=offset):
                report=self.decode(raw[:offset])
                self.assertFalse(report['complete']);self.assertEqual(report['errors'],[])
        start=raw.index('FES_NATIVE_STORAGE_RETURN')
        end=start+len(self.markers())
        for position in range(start,end):
            if raw[position:position+1]!='\n':continue
            with self.subTest(fragment=position):self.assertFalse(self.decode(raw[:position])['complete'])
        self.assertFalse(self.decode(raw.rstrip())['complete'])

    def test_each_missing_duplicate_or_reordered_storage_stage_rejects(self):
        raw=self.capture();markers=self.markers().splitlines(keepends=True)
        for line in markers:
            for replacement in ('',line+line):
                with self.subTest(line=line,replacement=replacement):
                    self.assertFalse(self.decode(raw.replace(line,replacement))['complete'])
        for order in ((1,0,2),(0,2,1),(2,1,0)):
            with self.subTest(order=order):
                self.assertFalse(self.decode(raw.replace(self.markers(),''.join(markers[i] for i in order)))['complete'])

    def test_storage_markers_cannot_escape_the_verified_return_interval(self):
        raw=self.capture();stripped=raw.replace(self.markers(),'')
        for position in (0,stripped.index('FES_NATIVE_FPGA_CYCLE END cycle=1'),
                         stripped.index('FES_NATIVE_FPGA_CYCLE END cycle=2'),len(stripped)):
            with self.subTest(position=position):
                self.assertFalse(self.decode(stripped[:position]+self.markers()+stripped[position:])['complete'])

    def test_changed_storage_owner_receipt_package_or_scope_rejects(self):
        raw=self.capture();markers=self.markers()
        changes=((f'owner={OWNER:x}',f'owner={OWNER+2:x}'),('generation=6 package_id','generation=5 package_id'),
                 ('generation=8 return_generation=6','generation=7 return_generation=6'),
                 ('generation=9 return_generation=6','generation=9 return_generation=8'),
                 ('generation=9 return_generation=6','generation=8 return_generation=6'),
                 (self.package_id,'0'*64),('data_held=1','data_held=0'),('writes=disabled','writes=enabled'),
                 ('source=sd','source=ram'),(' ACTIVE ',' READY '))
        for old,new in changes:
            self.assertIn(old,markers)
            with self.subTest(change=new):
                self.assertFalse(self.decode(raw.replace(markers,markers.replace(old,new)))['complete'])
        old_mode=self.reload.capture().replace('FES_NATIVE_FPGA_CYCLE BEGIN cycle=1',
            self.mapping_fixture.marker()+'FES_NATIVE_FPGA_CYCLE BEGIN cycle=1',1)
        self.assertFalse(self.decode(old_mode)['complete'])

    def test_explicit_build_selection_and_cpu_mapping_are_required(self):
        self.assertFalse(self.decode(self.capture(),computer_storage_return=False)['complete'])
        with self.assertRaises(ValueError):self.decode(self.capture(),cpu_mapping=None)
        for invalid in (None,0,1,'true'):
            with self.subTest(invalid=invalid),self.assertRaises(ValueError):
                self.decode(self.capture(),computer_storage_return=invalid)
        with self.assertRaises(ValueError):
            trace.decode_native_configuration(self.capture(),BUILD,SEED,**self.options,
                from_storage=True,computer_activity=True,computer_storage_return=True)


if __name__=='__main__':unittest.main()
