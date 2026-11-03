"""Source-context identity and omission failures, without programming hardware."""
import importlib.util
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch
import xml.etree.ElementTree as ET
import zipfile

ROOT = Path(__file__).resolve().parents[1]
NANO = ROOT/'platform/embedded/nano'
spec = importlib.util.spec_from_file_location('test_hps_context', NANO/'hps_context.py')
context = importlib.util.module_from_spec(spec)
spec.loader.exec_module(context)
spec = importlib.util.spec_from_file_location('test_hps_cold', NANO/'cold_profile.py')
cold = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cold)
GOLDEN = ROOT/'platform/embedded/tamago/nativefabric/testdata/hps-context.json'


class HPSContextTests(unittest.TestCase):
    def setUp(self):
        self.descriptor = json.loads(GOLDEN.read_bytes())
        self.record = {'program': 'bootcontext', 'hps_project': 'sealed-project',
                       'hps_context': self.descriptor,
                       'hps_context_sha256': context.digest(self.descriptor)}

    def test_configuration_mapping_binds_actual_core_bytes_and_separate_memory(self):
        chunk=0xfffc0;raw=(bytes(range(256))*((chunk+128+255)//256))[:chunk+128]
        descriptor=dict(self.descriptor,core_sha256=hashlib.sha256(raw).hexdigest(),core_size=len(raw))
        with tempfile.TemporaryDirectory() as directory:
            project=Path(directory)
            payload=project/'board/hps/packages'/descriptor['package_id']/'core.rbf'
            payload.parent.mkdir(parents=True);payload.write_bytes(raw)
            with patch.object(context,'_derive',return_value=descriptor):
                mapping=context._configuration(project)
                self.assertEqual(mapping['core_size'],len(raw))
                self.assertEqual(mapping['core_sha256'],hashlib.sha256(raw).hexdigest())
                self.assertEqual(mapping['core_sha384'],hashlib.sha384(raw).hexdigest())
                self.assertEqual(mapping['chunk_bytes'],chunk)
                self.assertEqual(mapping['chunk_sha384'],[hashlib.sha384(raw[:chunk]).hexdigest(),hashlib.sha384(raw[chunk:]).hexdigest()])
                self.assertEqual((mapping['base'],mapping['base']+mapping['size']),(0x94800000,0x95000000))
                payload.write_bytes(raw[:-4]+b'bad!')
                with self.assertRaisesRegex(ValueError,'sealed core'):
                    context._configuration(project)
            archive=project/'package.fcore';archive.write_bytes(b'canonical archive fixture')
            receipt={'path':str(archive),'size':archive.stat().st_size,'sha256':hashlib.sha256(archive.read_bytes()).hexdigest()}
            with patch.object(context,'configuration',return_value=mapping),patch.object(context,'_worker',return_value=receipt):
                self.assertEqual(context.package_archive(project,mapping),archive.read_bytes())
                archive.write_bytes(b'changed after validation')
                with self.assertRaisesRegex(ValueError,'changed after verification'):
                    context.package_archive(project,mapping)
                with self.assertRaisesRegex(ValueError,'map differs'):
                    context.package_archive(project,dict(mapping,base=0))
            payload.unlink();payload.symlink_to(project/'untrusted.rbf')
            with patch.object(context,'_derive',return_value=descriptor):
                with self.assertRaisesRegex(ValueError,'symlink'):
                    context._configuration(project)
            ddr=dict(descriptor,programming_profile='de25-embedded-hps-first-ddr-diagnostic-v1',
                     control_abi=0x10203041,package_id='d'*64,build_id='e'*32)
            params={'reader_memory_backend':'hps-ddr','reader_memory_base':0x95000000,'reader_memory_bytes':8192}
            build=project/'board/hps/build-inputs.json'
            build.write_text(json.dumps({'parameters':params}))
            with patch.object(context,'_derive',return_value=ddr):
                memory=context._reader_memory(project)
                self.assertEqual(context.encode(memory),(ROOT/'platform/embedded/tamago/nativefabric/testdata/hps-reader-memory.json').read_bytes())
                self.assertEqual((memory['cpu_base'],memory['bus_base'],memory['bytes']), (0x95000000,0x95000000,8192))
                self.assertEqual((memory['arena_base'],memory['arena_bytes']),(0x95000000,2<<20))
                for key,value in (('reader_memory_backend','onchip'),('reader_memory_base',0x95001000),
                                  ('reader_memory_base',0x94000000),('reader_memory_bytes',4096),('reader_memory_bytes',True)):
                    build.write_text(json.dumps({'parameters':dict(params,**{key:value})}))
                    with self.subTest(key=key,value=value),self.assertRaisesRegex(ValueError,'fixed sealed'):
                        context._reader_memory(project)
                build.write_text(json.dumps({'parameters':params}))
                for key,value in (('control_abi',0x10203040),('programming_profile','de25-embedded-ddr-diagnostic-v1')):
                    with patch.object(context,'_derive',return_value=dict(ddr,**{key:value})),self.assertRaises(ValueError):
                        context._reader_memory(project)
            with patch.object(context,'_derive',return_value=self.descriptor):
                self.assertIsNone(context._reader_memory(project))

    def test_window_memory_requires_its_explicit_geometry_identity_and_stop_contract(self):
        descriptor=dict(self.descriptor,programming_profile='de25-embedded-hps-first-window-diagnostic-v1',
                        control_abi=0x10203042,package_id='d'*64,build_id='e'*32)
        params={'reader_memory_backend':'hps-ddr-window','reader_memory_base':0x95000000,'reader_memory_bytes':4<<20}
        memory=context.reader_memory_contract(descriptor,params)
        fixture=ROOT/'platform/embedded/tamago/nativefabric/testdata/hps-window-memory.json'
        self.assertEqual(context.encode(memory),fixture.read_bytes())
        self.assertEqual(context.validate_reader_memory(descriptor,memory),memory)
        for key,value in (('reader_memory_backend','hps-ddr'),('reader_memory_base',0x95001000),
                          ('reader_memory_bytes',8192),('reader_memory_bytes',True)):
            with self.subTest(parameter=key),self.assertRaises(ValueError):
                context.reader_memory_contract(descriptor,dict(params,**{key:value}))
        configuration={'base':0x94800000,'size':8<<20,'core_size':128,'chunk_bytes':65536,
                       'core_sha384':'d'*96,'chunk_sha384':['d'*96]}
        control={'cpu_base':0x20010400,'span':256,'fabric_stop':{'scope':'sealed-window-master-stop-v1',
                 'probe_cpu_base':0x20010100,'probe_span':128,'maintenance':'held-reset-from-configuration'}}
        profile={'sd_pio':True,'chain_sha256':'a'*64,'owner_seed':1,'entry_image_sha256':'b'*64,'entry_image_size':128,
                 'window_memory':True,'hps_context':descriptor,'hps_context_sha256':context.digest(descriptor),
                 'hps_control':control,'hps_control_sha256':'c'*64,'hps_configuration':configuration,
                 'hps_reader_memory':memory,'hps_reader_memory_sha256':context.digest(memory)}
        profile['cpu_mapping']=context.cpu_mapping(control,configuration,memory)
        profile['cpu_mapping_sha256']=context.digest(profile['cpu_mapping'])
        self.assertEqual(profile['cpu_mapping']['reader_bytes'],4<<20)
        header=cold.header(profile)
        for line in ('#define FES_NANO_WINDOW_MEMORY 1','#define FES_NANO_READER_BYTES 4194304U',
                     '#define FES_NANO_READER_ARENA_BYTES 4194304U','#define FES_NANO_WINDOW_FRAME_BYTES 3686400U',
                     '#define FES_NANO_WINDOW_ROW_BYTES 5120U','#define FES_NANO_WINDOW_ROWS 720U',
                     '#define FES_NANO_DDR_READ_DIAGNOSTIC 0'):
            self.assertIn(line,header)
        self.assertNotIn('FES_NANO_READER_PATTERN_SHA',header)
        for key,value in memory.items():
            changed=dict(memory,**{key:True if type(value) is int else 'changed'})
            with self.subTest(memory_field=key),self.assertRaises(ValueError):
                cold.header(dict(profile,hps_reader_memory=changed,hps_reader_memory_sha256=context.digest(changed)))
        for key,value in (('window_memory',False),('window_memory',1),('hps_reader_memory',None),
                          ('hps_ddr_read',{}),('hps_computer',{}),
                          ('hps_control',dict(control,fabric_stop=dict(control['fabric_stop'],scope='sealed-ddr-master-stop-v1')))):
            with self.subTest(header_field=key),self.assertRaises(ValueError):cold.header(dict(profile,**{key:value}))

    def test_window_entry_mode_requires_the_exact_single_linker_assignment(self):
        spec=importlib.util.spec_from_file_location('window_native_entry',NANO.parent/'entry/build.py')
        entry=importlib.util.module_from_spec(spec);spec.loader.exec_module(entry)
        memory={'scope':'sealed-cold-window-reader-memory-v1'}
        def receipt(flags,selected=True):return {'window_memory':selected,'command':['compiler','build','-ldflags',flags]}
        self.assertTrue(entry.window_memory_option(receipt('-T 0x84000000 -X main.expectedWindowMemory=1'),memory))
        self.assertFalse(entry.window_memory_option(receipt('-T 0x84000000',False),None))
        for flags in ('-X main.expectedWindowMemory=10','-X main.expectedWindowMemory=1 -X main.expectedWindowMemory=1',
                      'main.expectedWindowMemory=1','-X=main.expectedWindowMemory=1','-X "main.expectedWindowMemory=1', ''):
            with self.subTest(flags=flags),self.assertRaises(ValueError):entry.window_memory_option(receipt(flags),memory)
        for changed in (receipt('-X main.expectedWindowMemory=1',False),receipt('-X main.expectedWindowMemory=1',1),
                        {'window_memory':True,'command':[]},{'window_memory':True,'command':['-ldflags',1]}):
            with self.subTest(changed=changed),self.assertRaises(ValueError):entry.window_memory_option(changed,memory)
        with self.assertRaises(ValueError):entry.window_memory_option(receipt('-X main.expectedWindowMemory=1'),None)

    def test_configuration_receipt_requires_its_dedicated_source_and_map(self):
        mapping={'format':1,'base':0x94800000,'size':8<<20,'core_size':128,
                 'chunk_bytes':65536,'core_sha384':hashlib.sha384(bytes(128)).hexdigest(),
                 'chunk_sha384':[hashlib.sha384(bytes(128)).hexdigest()]}
        record=dict(self.record,program='fpgaload',hps_configuration=mapping,
                    hps_configuration_sha256=context.digest(mapping))
        with patch.object(context,'checked_control',return_value={'qualified-source':True}),patch.object(context,'configuration',return_value=mapping):
            self.assertEqual(context.checked_configuration(record),mapping)
            for key in ('hps_configuration','hps_configuration_sha256'):
                missing=dict(record);missing.pop(key)
                with self.subTest(key=key),self.assertRaises(ValueError):context.checked_configuration(missing)
            for changed in (dict(record,program='fpgastatus'),
                            dict(record,hps_configuration_sha256='0'*64),
                            dict(record,hps_configuration=dict(mapping,base=0x94000000))):
                with self.assertRaises(ValueError):context.checked_configuration(changed)
        self.assertIsNone(context.checked_configuration({'program':'fpgastatus'}))
        with self.assertRaises(ValueError):context.checked_configuration({'program':'fpgaload'})
        memory=json.loads((ROOT/'platform/embedded/tamago/nativefabric/testdata/hps-reader-memory.json').read_bytes())
        record.update(hps_reader_memory=memory,hps_reader_memory_sha256=context.digest(memory))
        with patch.object(context,'checked_configuration',return_value=mapping),patch.object(context,'reader_memory',return_value=memory):
            self.assertEqual(context.checked_reader_memory(record),memory)
            entry=dict(record,native_program=record['program']);entry.pop('program')
            self.assertEqual(context.checked_reader_memory(entry,'native_program'),memory)
            for key in ('hps_reader_memory','hps_reader_memory_sha256'):
                changed=dict(record);changed.pop(key)
                with self.subTest(key=key),self.assertRaises(ValueError):context.checked_reader_memory(changed)
            for changed in (dict(record,program='bootcontext'),dict(record,hps_reader_memory_sha256='f'*64),
                            dict(record,hps_reader_memory=dict(memory,bus_base=0x15000000))):
                with self.subTest(changed=changed),self.assertRaises(ValueError):context.checked_reader_memory(changed)
        with patch.object(context,'checked_configuration',return_value=mapping),patch.object(context,'reader_memory',return_value=None):
            self.assertIsNone(context.checked_reader_memory(dict(self.record,program='fpgaload')))
            with self.assertRaises(ValueError):context.checked_reader_memory(record)
        self.assertIsNone(context.checked_reader_memory({'program':'fpgastatus'}))
        ddr=dict(self.descriptor,programming_profile='de25-embedded-hps-first-ddr-diagnostic-v1',control_abi=0x10203041,
                 package_id=memory['package_id'],build_id=memory['build_id'])
        header_profile={'sd_pio':True,'chain_sha256':'a'*64,'owner_seed':1,'entry_image_sha256':'b'*64,'entry_image_size':128,
                        'hps_context':ddr,'hps_context_sha256':context.digest(ddr),
                        'hps_control':{'cpu_base':0x20010400,'span':256,'fabric_stop':{'scope':'sealed-ddr-master-stop-v1',
                            'probe_cpu_base':0x20010100,'probe_span':128,'maintenance':'held-reset-from-configuration'}},'hps_control_sha256':'c'*64,
                        'hps_configuration':dict(mapping,core_sha384='d'*96),
                        'hps_reader_memory':memory,'hps_reader_memory_sha256':context.digest(memory)}
        header_profile['cpu_mapping']=context.cpu_mapping(header_profile['hps_control'],header_profile['hps_configuration'],memory)
        header_profile['cpu_mapping_sha256']=context.digest(header_profile['cpu_mapping'])
        header=cold.header(header_profile)
        for key,value in (('cpu_mapping',None),('cpu_mapping_sha256','0'*64),
                          ('cpu_mapping',dict(header_profile['cpu_mapping'],format=True)),
                          ('cpu_mapping',dict(header_profile['cpu_mapping'],dma_base=0x94000000))):
            with self.subTest(cpu_contract=key,value=value),self.assertRaises(ValueError):
                cold.header(dict(header_profile,**{key:value}))
        self.assertIn('#define FES_NANO_READER_CPU_BASE 0x95000000ULL',header)
        self.assertIn('#define FES_NANO_READER_BUS_BASE 0x95000000ULL',header)
        self.assertIn('#define FES_NANO_READER_ARENA_BYTES 2097152U',header)
        self.assertIn('#define FES_NANO_PROBE_CPU_BASE 0x20010100UL',header)
        self.assertIn('#define FES_NANO_PROBE_SPAN 128U',header)
        self.assertIn('#define FES_NANO_DDR_READ_DIAGNOSTIC 0',header)
        active={'format':1,'scope':'sealed-cold-ddr-read-v1','package_id':ddr['package_id'],'build_id':ddr['build_id'],
                'reader_memory_sha256':context.digest(memory),'control_map_sha256':'c'*64,
                'adapter_source_sha256':'a'*64,'adapter_generated_sha256':'b'*64,
                'arcache':2,'arprot':3,'aruser':224,'read_bytes':32,'open_mask':8,'close_mask':10}
        active_profile=dict(header_profile,hps_ddr_read=active,hps_ddr_read_sha256=context.digest(active))
        self.assertIn('#define FES_NANO_DDR_READ_DIAGNOSTIC 1',cold.header(active_profile))
        self.assertIn('#define FES_NANO_DDR_READ_ATTRS_VERIFIED 1',cold.header(active_profile))
        for key,value in (('arcache',15),('arprot',2),('aruser',0),('read_bytes',8192),('open_mask',13),
                          ('close_mask',2),('format',True),('adapter_source_sha256','0'*64),('reader_memory_sha256','f'*64)):
            changed=dict(active,**{key:value})
            with self.subTest(key=key),self.assertRaises(ValueError):
                cold.header(dict(active_profile,hps_ddr_read=changed,hps_ddr_read_sha256=context.digest(changed)))
        for key,value in (('sd_pio',False),('hps_control',None),('hps_configuration',None),
                          ('hps_context',self.descriptor),('hps_reader_memory_sha256','0'*64)):
            with self.subTest(key=key),self.assertRaises(ValueError):cold.header(dict(header_profile,**{key:value}))
        for stop in (None,{},dict(header_profile['hps_control']['fabric_stop'],probe_cpu_base=0x20010400),
                     dict(header_profile['hps_control']['fabric_stop'],maintenance='reset-unobserved')):
            with self.subTest(stop=stop),self.assertRaisesRegex(ValueError,'complete master stop map'):
                cold.header(dict(header_profile,hps_control=dict(header_profile['hps_control'],fabric_stop=stop)))
        with tempfile.TemporaryDirectory() as temp:
            entry=Path(temp);image=entry/'entry.img';image.write_bytes(bytes(64))
            receipt={'status':'built','target':'de25-nano','entry_image_address':0x96080000,
                     'ram_start':0x84000000,'ram_end':0x94000000,'uart_base':0x10c02100,
                     'entry_image_sha256':cold.sha(image),'entry_image_size':64,'native_program':'fpgaload',
                     'firmware_profile_sha256':cold.sha(NANO/'sd-profile.json'),'sources':{},
                     'native_build_sources':{'platform/embedded/tamago/build.py':cold.sha(NANO.parent/'tamago/build.py')}}
            config_range=[mapping['base'],mapping['base']+mapping['size']]
            memory_range=[memory['arena_base'],memory['arena_base']+memory['arena_bytes']]
            with patch.object(cold.hps_context,'checked',return_value=ddr), \
                    patch.object(cold.hps_context,'checked_control',return_value={'compiled':True}), \
                    patch.object(cold.hps_context,'checked_configuration',return_value=mapping), \
                    patch.object(cold.hps_context,'checked_reader_memory',return_value=memory), \
                    patch.object(cold.hps_context,'checked_cpu_mapping',return_value=header_profile['cpu_mapping']):
                for exclusion in (None,[memory['cpu_base'],memory['cpu_base']+memory['bytes']],memory_range):
                    receipt['staging_exclusions']=[config_range]+([exclusion] if exclusion else [])
                    (entry/'result.json').write_text(json.dumps(receipt))
                    if exclusion==memory_range:self.assertEqual(cold.entry(entry)[0],image)
                    else:
                        with self.assertRaisesRegex(ValueError,'reader arena'):cold.entry(entry)

    def test_active_ddr_requires_complete_generated_vendor_adapter_and_receipt(self):
        with tempfile.TemporaryDirectory() as temp:
            project=Path(temp);hps=project/'board/hps';source=hps/'project'
            tcl=source/'custom_ip/f2sdram_adapter/f2sdram_adapter_hw.tcl';tcl.parent.mkdir(parents=True)
            generated=source/'hps_subsys/ip/hps_subsys/f2sdram_adapter/f2sdram_adapter_10/synth/f2sdram_adapter_f2sdram_adapter_10_fixture.v'
            generated.parent.mkdir(parents=True)
            template="\nmodule ${output_name};\nassign man_arcache = 4'h2;\nassign man_arprot = 3'b011;\nassign man_aruser = 8'hE0;\nendmodule"
            raw=('set verilog_code {'+template+'\n}\n\nadd_fileset_file fixture\n').encode()
            tcl.write_bytes(raw)
            expected=template.replace('${output_name}',generated.stem)+'\n';generated.write_text(expected)
            archive=project/'board/vendor.zip'
            with zipfile.ZipFile(archive,'w') as z:
                z.writestr('Demonstration/SoC_FPGA/GHRD/custom_ip/f2sdram_adapter/f2sdram_adapter_hw.tcl',raw)
            vendor=hashlib.sha256(archive.read_bytes()).hexdigest()
            proof=context._ddr_adapter(project,vendor)
            self.assertEqual((proof['arcache'],proof['arprot'],proof['aruser']),(2,3,224))
            for changed in (expected.replace("4'h2","4'hf"),expected+'assign man_aruser = 0;\n',expected.replace('endmodule','wire changed;\nendmodule')):
                generated.write_text(changed)
                with self.assertRaisesRegex(ValueError,'complete vendor template'):context._ddr_adapter(project,vendor)
            generated.write_text(expected);tcl.write_bytes(raw+b'# edited\n')
            with self.assertRaisesRegex(ValueError,'source differs'):context._ddr_adapter(project,vendor)
            tcl.write_bytes(raw)
            with self.assertRaisesRegex(ValueError,'archive differs'):context._ddr_adapter(project,'0'*64)
            generated.rename(generated.with_suffix('.saved'));generated.symlink_to(generated.with_suffix('.saved'))
            with self.assertRaisesRegex(ValueError,'private bounded'):context._ddr_adapter(project,vendor)
        active={'format':1,'scope':'sealed-cold-ddr-read-v1'}
        record={'program':'fpgaload','hps_project':'sealed','hps_ddr_read':active,'hps_ddr_read_sha256':context.digest(active)}
        with patch.object(context,'checked_reader_memory',return_value={'pinned':True}),patch.object(context,'ddr_read',return_value=active):
            self.assertEqual(context.checked_ddr_read(record),active)
            for changed in (dict(record,program='fpgastatus'),dict(record,hps_ddr_read=None),
                            dict(record,hps_ddr_read_sha256='f'*64),dict(record,hps_ddr_read={'format':2})):
                with self.assertRaises(ValueError):context.checked_ddr_read(changed)
        self.assertIsNone(context.checked_ddr_read({'program':'fpgaload'}))

    def test_canonical_descriptor_matches_both_consumers(self):
        self.assertEqual(context.encode(self.descriptor), GOLDEN.read_bytes())
        self.assertEqual(context.digest(self.descriptor),
                         '94da3557f175eae05144d70937c3afceda20c6c9f9ca9d25155535da3e0814fc')
        with patch.object(context, 'derive', return_value=self.descriptor) as derive:
            self.assertEqual(context.checked(self.record), self.descriptor)
            derive.assert_called_once_with('sealed-project')
        header = cold.header({'sd_pio': False, 'chain_sha256': 'a'*64,
                              'owner_seed': 1, 'entry_image_sha256': 'b'*64,
                              'entry_image_size': 128,
                              'hps_context_sha256': self.record['hps_context_sha256']})
        digest = self.record['hps_context_sha256']
        self.assertIn('#define FES_NANO_HPS_CONTEXT_SHA {'+
                      ','.join('0x'+digest[i:i+2] for i in range(0, 64, 2))+'}', header)
        boot={'sd_pio':False,'smmu_observe':True,'chain_sha256':'a'*64,'owner_seed':1,
              'entry_image_sha256':'b'*64,'entry_image_size':128}
        self.assertIn('#define FES_NANO_SMMU_BOOT_OBSERVE 1',cold.header(boot))
        for changed in (dict(boot,sd_pio=True),dict(boot,hps_context=self.descriptor),
                        dict(boot,smmu_observe=1),dict(boot,smmu_observe='yes')):
            with self.assertRaises(ValueError):cold.header(changed)
        # Isolate only the already-verified project inputs. Exercise the actual
        # descriptor builder so a DDR package cannot regain the on-chip label.
        (ROOT/'out').mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=ROOT/'out') as temp:
            project=Path(temp);hps=project/'board/hps';hps.mkdir(parents=True)
            (hps/'hps-first-pair.json').write_text(json.dumps(self.descriptor))
            (hps/'build-inputs.json').write_text(json.dumps({'revision':self.descriptor['overlay_revision']}))
            for profile,abi in (('de25-embedded-hps-first-diagnostic-v1',0x10203040),
                                ('de25-embedded-hps-first-ddr-diagnostic-v1',0x10203041)):
                source={'embedded_control':dict(build_inputs_sha256='a'*64,control_abi=abi,
                                               peripheral_offset=0x10400,span=256),
                        'board_fes_revision':self.descriptor['board_revision']}
                fields={'abi':{'id':self.descriptor['abi'],'major':1,'minor':0},
                        'target':{'programming_profile':profile},'core':{'id':self.descriptor['core_id']}}
                select=Mock(return_value=profile)
                seal=SimpleNamespace(PLATFORM='de25_nano',DEVICE=self.descriptor['device'],
                                     boot_order=lambda _: 'HPS FIRST',profile_for=select,
                                     read_package=lambda _: SimpleNamespace(fields=fields))
                verifier=SimpleNamespace(inputs_for=lambda _: (source,None,None),sha=lambda _:self.descriptor['raw_sof_sha256'])
                with patch.dict(sys.modules,seal=seal),patch.object(sys,'path',sys.path[:]), \
                        patch.object(context.importlib.util,'spec_from_file_location',return_value=SimpleNamespace(loader=SimpleNamespace(exec_module=lambda _:None))), \
                        patch.object(context.importlib.util,'module_from_spec',return_value=verifier):
                    result=context._derive(project)
                    self.assertEqual((result['programming_profile'],result['control_abi']),(profile,abi))
                    select.assert_called_once_with(source)
                    fields['target']['programming_profile']='wrong-backend'
                    with self.assertRaisesRegex(ValueError,'profile differs'):context._derive(project)

    def test_native_receipt_key_and_source_or_digest_changes_reject(self):
        entry = dict(self.record)
        entry['native_program'] = entry.pop('program')
        with patch.object(context, 'derive', return_value=self.descriptor):
            self.assertEqual(context.checked(entry, program_key='native_program'), self.descriptor)
            for key, value in (('hps_context_sha256', 'f'*64),
                               ('hps_context', dict(self.descriptor, package_id='f'*64)),
                               ('program', 'probe')):
                with self.subTest(key=key), self.assertRaises(ValueError):
                    context.checked(dict(self.record, **{key: value}))
            changed = dict(self.descriptor, hps_io_hash='f'*64)
            with patch.object(context, 'derive', return_value=changed):
                with self.assertRaises(ValueError):
                    context.checked(self.record)

    def test_partial_or_omitted_context_cannot_claim_bootcontext(self):
        for key in ('hps_context', 'hps_context_sha256', 'hps_project'):
            partial = dict(self.record)
            del partial[key]
            with self.subTest(key=key), self.assertRaises(ValueError):
                context.checked(partial)
        with self.assertRaises(ValueError):
            context.checked({'program': 'bootcontext'})
        with patch.object(context, 'derive', side_effect=AssertionError('unexpected project access')):
            self.assertIsNone(context.checked({'program': 'probe'}))

    def test_cli_rejects_wrong_board_program_and_missing_project_before_output(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)/'not-created'
            common = [sys.executable, str(ROOT/'platform/embedded/tamago/run.py'),
                      '--compiler', '/bin/false', '--output', str(output)]
            for args in (['bootcontext'], ['fpgacontext'], ['fpgastatus'],['probe','--window-memory'],
                         ['probe','--board','de25-nano','--profile',str(NANO/'probe-profile.json'),'--window-memory'],
                         ['fpgaload','--board','de25-nano','--profile',str(NANO/'sd-profile.json'),'--window-memory','--ddr-read'],
                         ['fpgaload','--board','de25-nano','--profile',str(NANO/'sd-profile.json'),'--window-memory','--computer-control'],
                         ['tenfoot','--board','de25-nano','--profile',str(NANO/'smmu-profile.json')],
                         ['bootcontext', '--board', 'de25-nano', '--profile', str(NANO/'probe-profile.json')],
                         ['fpgacontext', '--board', 'de25-nano', '--profile', str(NANO/'probe-profile.json')],
                         ['fpgastatus', '--board', 'de25-nano', '--profile', str(NANO/'probe-profile.json')],
                         ['fpgacontext', '--board', 'de25-nano', '--profile', str(NANO/'sd-profile.json'),
                          '--hps-first-project', 'absent'],
                         ['probe', '--board', 'de25-nano', '--profile', str(NANO/'probe-profile.json'),
                          '--hps-first-project', 'absent']):
                with self.subTest(args=args):
                    result = subprocess.run(common+args, capture_output=True, text=True, timeout=10)
                    self.assertEqual(result.returncode, 2, result.stderr)
                    self.assertFalse(output.exists())

    def test_invalid_project_cannot_change_a_callers_firmware_imports(self):
        # The FPGA verifier's sys.path edits previously caused the following
        # firmware import to resolve the wrong verify.py during a real build.
        source = '''
import sys
import tempfile
from pathlib import Path
sys.path.insert(0, 'platform/embedded/nano')
import hps_context
before = sys.path[:]
(Path('out')).mkdir(exist_ok=True)
with tempfile.TemporaryDirectory(dir='out') as broken:
    try:hps_context.derive(broken)
    except (ValueError, OSError):pass
    else:raise AssertionError('invalid project accepted')
assert before == sys.path
sys.path.insert(0, 'platform/embedded/firmware')
import prepare_nano_owned
assert callable(prepare_nano_owned.function)
'''
        subprocess.run([sys.executable, '-c', source], cwd=ROOT,
                       env=dict(os.environ, PYTHONPATH=str(ROOT/'out/nano-bridge-diag-test-python')),
                       check=True, capture_output=True, timeout=15)

    def test_control_route_decoder_requires_compiler_hashes_exact_route_and_no_overlap(self):
        # These two connections are copied from the actual compiler handoff.
        # Only the decoder is under test; the project seal is explicitly mocked.
        fixture=ROOT/'tests/fixtures/nano/control-route.xml'
        for fault in ('pass','file','compiler','bridge','offset','overlap','duplicate','span','width'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as temp:
                project=Path(temp);hps=project/'board/hps';(hps/'project/qsys_top').mkdir(parents=True)
                xml=ET.fromstring(fixture.read_bytes())
                lw=next(c for c in xml if c.get('start')=='subsys_hps_agilex_hps.lwhps2fpga')
                endpoint=next(c for c in xml if c.get('start')=='subsys_periph_pb_cpu_0.m0')
                def parameter(c,name):return next(p.find('value') for p in c.findall('parameter') if p.get('name')==name)
                address_map=ET.fromstring(parameter(lw,'addressMapSysInfo').text)
                slot=next(s for s in address_map if s.get('name')=='subsys_periph.fes_embedded.avs')
                if fault=='bridge':parameter(lw,'baseAddress').text='0x100000'
                if fault=='offset':parameter(endpoint,'baseAddress').text='0x10500'
                if fault=='overlap':ET.SubElement(address_map,'slave',name='overlap',start='0x10440',end='0x10480')
                if fault=='duplicate':xml.append(ET.fromstring(ET.tostring(lw)))
                if fault=='span':slot.set('end','0x10600')
                if fault=='width':slot.set('datawidth','64')
                parameter(lw,'addressMapSysInfo').text=ET.tostring(address_map,encoding='unicode')
                raw=ET.tostring(xml);digest=hashlib.sha256(raw).hexdigest()
                (hps/'project/qsys_top/qsys_top.sopcinfo').write_bytes(raw+(b' ' if fault=='file' else b''))
                (hps/'generation.json').write_text(json.dumps({'top_system_handoff':'qsys_top/qsys_top.sopcinfo','sha256':digest}))
                (hps/'build.json').write_text(json.dumps({'generated_handoff_and_sdc_sha256':{
                    'qsys_top/qsys_top.sopcinfo':'f'*64 if fault=='compiler' else digest}}))
                with patch.object(context,'_derive',return_value=self.descriptor):
                    if fault=='pass':
                        mapping=context._control(project)
                        self.assertEqual(mapping['cpu_base'],0x20010400)
                        self.assertEqual(mapping['span'],256)
                        self.assertEqual(mapping['handoff_sha256'],digest)
                    else:
                        with self.assertRaises(ValueError):context._control(project)
        for fault in ('pass','inventory','probe-route','probe-base','probe-span','probe-width'):
            with self.subTest(ddr=fault),tempfile.TemporaryDirectory() as temp:
                project=Path(temp);hps=project/'board/hps';(hps/'project/qsys_top').mkdir(parents=True)
                xml=ET.fromstring(fixture.read_bytes())
                route=ET.fromstring(ET.tostring(next(c for c in xml if c.get('start')=='subsys_periph_pb_cpu_0.m0')))
                route.set('end','subsys_periph_fes_ddr_probe.avs')
                parameter(route,'baseAddress').text='0x10200' if fault=='probe-route' else '0x10100'
                xml.append(route)
                lw=next(c for c in xml if c.get('start')=='subsys_hps_agilex_hps.lwhps2fpga')
                address_map=ET.fromstring(parameter(lw,'addressMapSysInfo').text)
                probe=next(s for s in address_map if s.get('name')=='subsys_periph.fes_ddr_probe.avs')
                if fault=='probe-base':probe.set('start','0x10104')
                if fault=='probe-span':probe.set('end','0x10184')
                if fault=='probe-width':probe.set('datawidth','64')
                parameter(lw,'addressMapSysInfo').text=ET.tostring(address_map,encoding='unicode')
                raw=ET.tostring(xml);digest=hashlib.sha256(raw).hexdigest()
                (hps/'project/qsys_top/qsys_top.sopcinfo').write_bytes(raw)
                (hps/'generation.json').write_text(json.dumps({'top_system_handoff':'qsys_top/qsys_top.sopcinfo','sha256':digest}))
                (hps/'build.json').write_text(json.dumps({'generated_handoff_and_sdc_sha256':{'qsys_top/qsys_top.sopcinfo':digest}}))
                ddr=dict(self.descriptor,programming_profile='de25-embedded-hps-first-ddr-diagnostic-v1',control_abi=0x10203041)
                with patch.object(context,'_derive',return_value=ddr),patch.object(context,'_fabric_stop_inventory',
                        side_effect=ValueError('inventory differs') if fault=='inventory' else None) as inventory:
                    if fault=='pass':
                        m=context._control(project)
                        self.assertEqual(m['fabric_stop'],{'scope':'sealed-ddr-master-stop-v1','probe_cpu_base':0x20010100,
                                                         'probe_span':128,'maintenance':'held-reset-from-configuration'})
                    else:
                        with self.assertRaises(ValueError):context._control(project)
                    inventory.assert_called_once_with(project)

    def test_control_map_cannot_be_added_omitted_or_changed_outside_dedicated_image(self):
        mapping=json.loads((ROOT/'platform/embedded/tamago/nativefabric/testdata/hps-control.json').read_bytes())
        record=dict(self.record,program='fpgacontext',hps_control=mapping,hps_control_sha256=context.digest(mapping))
        with patch.object(context,'derive',return_value=self.descriptor),patch.object(context,'control',return_value=mapping):
            self.assertEqual(context.checked_control(record),mapping)
            self.assertEqual(context.checked_control(dict(record,program='fpgastatus')),mapping)
            for changed in (dict(record,program='bootcontext'),dict(record,hps_control=None),
                            dict(record,hps_control_sha256='f'*64),
                            dict(record,hps_control=dict(mapping,cpu_base=0xff210400))):
                with self.subTest(changed=changed),self.assertRaises(ValueError):context.checked_control(changed)
            with self.assertRaises(ValueError):context.checked_control(dict(self.record,program='fpgacontext'))
            with self.assertRaises(ValueError):context.checked({'program':'fpgacontext'})
            self.assertIsNone(context.checked_control(self.record))
        header=cold.header({'sd_pio':False,'chain_sha256':'a'*64,'owner_seed':1,
                            'entry_image_sha256':'b'*64,'entry_image_size':128,
                            'hps_context':self.descriptor,'hps_context_sha256':context.digest(self.descriptor),
                            'hps_control':mapping,'hps_control_sha256':context.digest(mapping)})
        self.assertIn('#define FES_NANO_CONTROL_PROBE 1',header)
        self.assertIn('#define FES_NANO_CONTROL_BASE 0x20010400UL',header)
        self.assertIn('#define FES_NANO_CONTROL_BUILD {0x0ea033e5U,0xbddf4c77U,0x1d14a2bbU,0xe25bb65aU}',header)


if __name__ == '__main__':
    unittest.main()
