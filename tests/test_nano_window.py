"""Execute the owned window dispatcher; this is no hardware or admission proof."""
from pathlib import Path
import base64
import importlib.util
import json
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
FW=ROOT/'platform/embedded/firmware'
FIXTURES=ROOT/'platform/embedded/tamago/nativefabric/testdata'

def module(name,path):
    spec=importlib.util.spec_from_file_location(name,path)
    value=importlib.util.module_from_spec(spec);spec.loader.exec_module(value)
    return value

context=module('window_hps_context',ROOT/'platform/embedded/nano/hps_context.py')
entry=module('window_entry',ROOT/'platform/embedded/entry/build.py')
cold=module('window_cold',ROOT/'platform/embedded/nano/cold_profile.py')

class WindowContractTests(unittest.TestCase):
    def setUp(self):
        self.source=json.loads((FIXTURES/'hps-context.json').read_bytes())
        self.source.update(programming_profile='de25-embedded-hps-first-window-diagnostic-v1',
                           control_abi=0x10203042,package_id='d'*64,build_id='e'*32)
        self.memory=json.loads((FIXTURES/'hps-window-memory.json').read_bytes())
        self.control=json.loads((FIXTURES/'hps-control.json').read_bytes())
        self.control.update(package_id='d'*64,build_id='e'*32,
            fabric_stop={'scope':'sealed-window-master-stop-v1','probe_cpu_base':0x20010100,
                         'probe_span':128,'maintenance':'held-reset-from-configuration'})
        self.access=json.loads((FIXTURES/'hps-window-read.json').read_bytes())

    def test_full_window_contract_matches_go_and_rejects_every_changed_field(self):
        self.assertEqual(context.validate_window_read(self.source,self.memory,context.digest(self.control),self.access),self.access)
        for key,value in self.access.items():
            changed=dict(self.access,**{key:True if type(value) is int else 'changed'})
            with self.subTest(key=key),self.assertRaises(ValueError):
                context.validate_window_read(self.source,self.memory,context.digest(self.control),changed)
        for changed in (dict(self.access,extra=1),dict(self.access,adapter_source_sha256='0'*64),
                        dict(self.access,read_bytes=32),dict(self.access,scope='sealed-cold-ddr-read-v1')):
            with self.assertRaises(ValueError):context.validate_window_read(self.source,self.memory,context.digest(self.control),changed)
        with self.assertRaises(ValueError):
            context.validate_window_read(self.source,dict(self.memory,arena_bytes=2<<20),context.digest(self.control),self.access)

    def test_receipt_and_exact_link_assignment_require_explicit_selection(self):
        record={'program':'fpgaload','window_memory':True,'window_read':True,'hps_project':'sealed',
                'hps_window_read':self.access,'hps_window_read_sha256':context.digest(self.access)}
        with patch.object(context,'checked_reader_memory',return_value=self.memory),patch.object(context,'window_read',return_value=self.access):
            self.assertEqual(context.checked_window_read(record),self.access)
            for key,value in (('window_read',False),('window_read',1),('window_memory',False),('program','bootcontext'),
                              ('hps_window_read_sha256','0'*64),('hps_ddr_read',{}),('hps_computer',{})):
                with self.subTest(key=key),self.assertRaises(ValueError):context.checked_window_read(dict(record,**{key:value}))
        self.assertIsNone(context.checked_window_read({}))
        assignment='main.expectedWindowReadMap='+base64.b64encode(context.encode(self.access)).decode()
        def receipt(flags,selected=True):return {'window_read':selected,'window_memory':True,'command':['compiler','build','-ldflags',flags]}
        self.assertTrue(entry.window_read_option(receipt('-X '+assignment),self.access))
        self.assertFalse(entry.window_read_option(receipt('-s -w',False),None))
        for flags in ('','-X '+assignment+' -X '+assignment,assignment,'-X '+assignment+'0','-X=main.expectedWindowReadMap=invalid'):
            with self.subTest(flags=flags),self.assertRaises(ValueError):entry.window_read_option(receipt(flags),self.access)
        with self.assertRaises(ValueError):entry.window_read_option(receipt('-X '+assignment,False),None)

    def test_presentation_requires_one_exact_link_selection_and_complete_window(self):
        def receipt(flags='-X main.expectedWindowPresent=1'):
            return dict(window_present=True,window_read=True,window_memory=True,
                        command=['compiler','build','-ldflags',flags])
        self.assertTrue(entry.window_present_option(receipt()))
        unselected={'command':['compiler','build','-ldflags','-s -w']}
        self.assertFalse(entry.window_present_option(unselected))
        for changes in ({'window_present':1},{'window_present':None},{'window_read':False},
                        {'window_read':1},{'window_memory':False},{'window_memory':1},
                        {'command':[]},{'command':['compiler','build','-ldflags']}):
            with self.subTest(changes=changes),self.assertRaises(ValueError):
                entry.window_present_option(dict(receipt(),**changes))
        for flags in ('','main.expectedWindowPresent=1','-X main.expectedWindowPresent=0',
                      '-X=main.expectedWindowPresent=1',
                      '-X main.expectedWindowPresent=1 -X main.expectedWindowPresent=1'):
            with self.subTest(flags=flags),self.assertRaises(ValueError):
                entry.window_present_option(receipt(flags))
        with self.assertRaises(ValueError):
            entry.window_present_option(dict(receipt(),window_present=False))

    def test_storage_return_requires_explicit_exact_link_assignment(self):
        def receipt(flags='-X main.expectedWindowStorageReturn=1'):
            return dict(window_present=True,window_read=True,window_memory=True,window_storage_return=True,
                        command=['compiler','build','-ldflags',flags])
        self.assertTrue(entry.window_storage_return_option(receipt()))
        self.assertFalse(entry.window_storage_return_option({'command':['compiler','build','-ldflags','-s -w']}))
        for key,value in [('window_storage_return',1),('window_storage_return',None),('window_present',False),
                          ('window_present',1),('window_read',False),('window_memory',False),('command',[])]:
            with self.subTest(key=key),self.assertRaises(ValueError):
                entry.window_storage_return_option(dict(receipt(),**{key:value}))
        for flags in ('','main.expectedWindowStorageReturn=1','-X=main.expectedWindowStorageReturn=1',
                      '-X main.expectedWindowStorageReturn=0',
                      '-X main.expectedWindowStorageReturn=1 -X main.expectedWindowStorageReturn=1'):
            with self.subTest(flags=flags),self.assertRaises(ValueError):entry.window_storage_return_option(receipt(flags))
        with self.assertRaises(ValueError):entry.window_storage_return_option(dict(receipt(),window_storage_return=False))

    def test_reload_requires_explicit_complete_SD_selection_and_exact_link(self):
        def receipt(flags='-X main.expectedWindowStorageReturn=1 -X main.expectedWindowReload=1'):
            return dict(window_reload=True,window_storage_return=True,window_present=True,window_read=True,window_memory=True,
                        command=['compiler','build','-ldflags',flags])
        self.assertTrue(entry.window_reload_option(receipt()))
        self.assertTrue(entry.window_storage_return_option(receipt()))
        self.assertFalse(entry.window_reload_option({'command':['compiler','build','-ldflags','-s -w']}))
        for key,value in [('window_reload',1),('window_reload',None),('window_storage_return',False),
                          ('window_storage_return',1),('window_present',False),('window_read',False),('window_memory',False),('command',[])]:
            with self.subTest(key=key),self.assertRaises(ValueError):entry.window_reload_option(dict(receipt(),**{key:value}))
        for flags in ('','main.expectedWindowReload=1','-X=main.expectedWindowReload=1','-X main.expectedWindowReload=0',
                      '-X main.expectedWindowReload=1 -X main.expectedWindowReload=1'):
            with self.subTest(flags=flags),self.assertRaises(ValueError):entry.window_reload_option(receipt(flags))
        with self.assertRaises(ValueError):entry.window_reload_option(dict(receipt(),window_reload=False))

    def test_firmware_header_binds_window_mode_full_memory_and_access_identity(self):
        configuration={'base':0x94800000,'size':8<<20,'core_size':128,'chunk_bytes':65536,
                       'core_sha384':'d'*96,'chunk_sha384':['d'*96]}
        profile={'sd_pio':True,'chain_sha256':'a'*64,'owner_seed':1,'entry_image_sha256':'b'*64,'entry_image_size':128,
                 'window_memory':True,'hps_context':self.source,'hps_context_sha256':context.digest(self.source),
                 'hps_control':self.control,'hps_control_sha256':context.digest(self.control),'hps_configuration':configuration,
                 'hps_reader_memory':self.memory,'hps_reader_memory_sha256':context.digest(self.memory),
                 'window_read':True,'hps_window_read':self.access,'hps_window_read_sha256':context.digest(self.access)}
        profile['cpu_mapping']=context.cpu_mapping(self.control,configuration,self.memory)
        profile['cpu_mapping_sha256']=context.digest(profile['cpu_mapping'])
        header=cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(profile)} )
        for line in ('#define FES_NANO_WINDOW_READ_DIAGNOSTIC 1','#define FES_NANO_WINDOW_READ_ATTRS_VERIFIED 1',
                     '#define FES_NANO_READER_BYTES 4194304U','#define FES_NANO_WINDOW_FRAME_BYTES 3686400U',
                     '#define FES_NANO_WINDOW_PRESENT_DIAGNOSTIC 0'):
            self.assertIn(line,header)
        self.assertIn('#define FES_NANO_WINDOW_PRESENT_DIAGNOSTIC 1',cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(profile,window_present=True))} ))
        self.assertIn('#define FES_NANO_WINDOW_STORAGE_RETURN 0',header)
        self.assertIn('#define FES_NANO_WINDOW_STORAGE_RETURN 1',cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(profile,window_present=True,window_storage_return=True))} ))
        self.assertIn('#define FES_NANO_WINDOW_STORAGE_RETURN 1',cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(profile,window_present=True,window_storage_return=True,window_reload=True))} ))
        self.assertIn('#define FES_NANO_WINDOW_STORAGE_RETURN 1',cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(profile,window_present=True,window_storage_return=True,window_service=True))} ))
        for changes in ({'window_service':True},{'window_service':1},{'window_service':None},
                        {'window_present':True,'window_storage_return':True,'window_reload':True,'window_service':True}):
            with self.subTest(changes=changes),self.assertRaises(ValueError):cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(profile,**changes))} )
        for changes in ({'window_reload':True},{'window_reload':1},{'window_reload':None}):
            with self.subTest(changes=changes),self.assertRaises(ValueError):cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(profile,**changes))} )
        for changes in ({'window_storage_return':True},{'window_present':True,'window_storage_return':1},
                        {'window_present':True,'window_storage_return':None}):
            with self.subTest(changes=changes),self.assertRaises(ValueError):cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(profile,**changes))} )
        for key,value in (('window_memory',False),('window_read',False),('window_read',1),('hps_window_read_sha256','0'*64),
                          ('hps_ddr_read',{}),('hps_computer',{}),('window_present',1),('window_present',None)):
            with self.subTest(key=key),self.assertRaises(ValueError):cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(profile,**{key:value}))} )
        with self.assertRaises(ValueError):
            cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(profile,window_present=True,window_read=False,hps_window_read=None))} )

class MenuMemoryContractTests(unittest.TestCase):
    def test_two_slot_contract_matches_native_and_rejects_mutations(self):
        source=json.loads((FIXTURES/'hps-context.json').read_bytes())
        source.update(programming_profile='de25-embedded-hps-first-menu-prototype-v1',control_abi=0x10203043,
                      package_id='d'*64,build_id='e'*32)
        parameters=dict(reader_memory_backend='hps-ddr-menu',reader_memory_base=0x95000000,reader_memory_bytes=8<<20)
        memory=context.reader_memory_contract(source,parameters)
        self.assertEqual(context.encode(memory),(FIXTURES/'hps-menu-memory.json').read_bytes())
        self.assertEqual(context.validate_reader_memory(source,memory),memory)
        access=json.loads((FIXTURES/'hps-menu-read.json').read_bytes())
        control=json.loads((FIXTURES/'hps-control.json').read_bytes())
        control.update(package_id='d'*64,build_id='e'*32,fabric_stop=dict(scope='sealed-menu-master-stop-v1',
            probe_cpu_base=0x20010100,probe_span=128,maintenance='held-reset-from-configuration'))
        self.assertEqual(context.validate_menu_read(source,memory,context.digest(control),access),access)
        for key,value in memory.items():
            with self.subTest(key=key),self.assertRaises(ValueError):
                context.validate_reader_memory(source,dict(memory,**{key:True if type(value) is int else 'changed'}))
        for key,value in access.items():
            with self.subTest(key=key),self.assertRaises(ValueError):
                context.validate_menu_read(source,memory,context.digest(control),dict(access,**{key:True if type(value) is int else 'changed'}))
        for bad in (dict(parameters,reader_memory_bytes=4<<20),dict(parameters,reader_memory_base=0x95001000),
                    dict(parameters,reader_memory_backend='hps-ddr-window'),dict(parameters,reader_memory_bytes=True)):
            with self.assertRaises(ValueError):context.reader_memory_contract(source,bad)
        old=dict(source,programming_profile='de25-embedded-hps-first-window-diagnostic-v1',control_abi=0x10203042)
        with self.assertRaises(ValueError):context.validate_reader_memory(old,memory)
        with self.assertRaises(ValueError):context.validate_window_read(source,memory,context.digest(control),access)

class WindowFirmwareTests(unittest.TestCase):
    def test_menu_dispatcher_requires_its_own_compiled_abi(self):
        with tempfile.TemporaryDirectory() as temporary:
            binary=Path(temporary)/'menu'
            run=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined',
                '-fno-sanitize-recover=all','-DFES_TEST_MENU_SCOPE=1','-I'+str(FW),str(FW/'nano_bridges.c'),str(FW/'nano_bridge_sip.c'),
                str(FW/'nano_ddr_firewall.c'),str(FW/'tests/nano_window_control_test.c'),'-o',str(binary)],capture_output=True,text=True,timeout=30)
            self.assertEqual(run.returncode,0,run.stderr)
            for mutation in range(23):
                with self.subTest(mutation=mutation):
                    result=subprocess.run([str(binary),str(mutation)],capture_output=True,text=True,timeout=5)
                    self.assertEqual(result.returncode,0,result.stderr+result.stdout)
                    self.assertIn('PASS ',result.stdout)

    def test_actual_dispatcher_uses_owned_control_ddr_and_complete_return(self):
        with tempfile.TemporaryDirectory() as temporary:
            binary=Path(temporary)/'window'
            run=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined',
                '-fno-sanitize-recover=all','-I'+str(FW),str(FW/'nano_bridges.c'),str(FW/'nano_bridge_sip.c'),
                str(FW/'tests/nano_window_control_test.c'),'-o',str(binary)],capture_output=True,text=True,timeout=30)
            self.assertEqual(run.returncode,0,run.stderr)
            for mutation in range(22):
                with self.subTest(mutation=mutation):
                    result=subprocess.run([str(binary),str(mutation)],capture_output=True,text=True,timeout=5)
                    self.assertEqual(result.returncode,0,result.stderr+result.stdout)
                    self.assertIn('PASS ',result.stdout)
