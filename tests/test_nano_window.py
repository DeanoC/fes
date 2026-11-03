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
        header=cold.header(profile)
        for line in ('#define FES_NANO_WINDOW_READ_DIAGNOSTIC 1','#define FES_NANO_WINDOW_READ_ATTRS_VERIFIED 1',
                     '#define FES_NANO_READER_BYTES 4194304U','#define FES_NANO_WINDOW_FRAME_BYTES 3686400U'):
            self.assertIn(line,header)
        for key,value in (('window_memory',False),('window_read',False),('window_read',1),('hps_window_read_sha256','0'*64),
                          ('hps_ddr_read',{}),('hps_computer',{})):
            with self.subTest(key=key),self.assertRaises(ValueError):cold.header(dict(profile,**{key:value}))

class WindowFirmwareTests(unittest.TestCase):
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
