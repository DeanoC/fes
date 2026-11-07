"""Native menu selection through real receipt/entry/cold-header validators."""
import base64
import copy
import json
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

from test_nano_window import context, cold, entry, FIXTURES, ROOT


class MenuBootTests(unittest.TestCase):
    def setUp(self):
        self.source=json.loads((FIXTURES/'hps-context.json').read_bytes())
        self.source.update(programming_profile=context.MENU_PROFILE, control_abi=0x10203043,
                           package_id='d'*64, build_id='e'*32)
        self.memory=json.loads((FIXTURES/'hps-menu-memory.json').read_bytes())
        self.control=json.loads((FIXTURES/'hps-control.json').read_bytes())
        self.control.update(package_id='d'*64,build_id='e'*32,fabric_stop=dict(
            scope='sealed-menu-master-stop-v1',probe_cpu_base=0x20010100,probe_span=128,
            maintenance='held-reset-from-configuration'))
        self.access=json.loads((FIXTURES/'hps-menu-read.json').read_bytes())
        self.receipt=dict(program='fpgaload',menu_present=True,hps_project='sealed',
            hps_context=self.source,hps_control_sha256=context.digest(self.control),
            hps_menu_read=self.access,hps_menu_read_sha256=context.digest(self.access))

    def checked(self, value, **kwargs):
        with patch.object(context,'checked_reader_memory',return_value=self.memory),\
             patch.object(context,'menu_read',return_value=self.access):
            return context.checked_menu_read(value,**kwargs)

    def test_complete_selection_and_every_changed_map_rejected(self):
        self.assertEqual(self.checked(self.receipt),self.access)
        self.assertEqual(self.checked(dict(self.receipt,native_program='fpgaload',program=None),
                                      program_key='native_program'),self.access)
        for key,value in self.access.items():
            changed=copy.deepcopy(self.receipt)
            changed['hps_menu_read'][key]=True if type(value) is int else 'changed'
            with self.subTest(key=key),self.assertRaises(ValueError):self.checked(changed)
        for change in ({'menu_present':False},{'menu_present':1},{'program':'probe'},
                {'hps_menu_read':None},{'hps_menu_read_sha256':'0'*64},{'hps_control_sha256':'0'*64},
                {'hps_context':dict(self.source,programming_profile='de25-embedded-hps-first-window-diagnostic-v1')}):
            with self.subTest(change=change),self.assertRaises(ValueError):self.checked(dict(self.receipt,**change))
        with patch.object(context,'checked_reader_memory',return_value={'scope':'sealed-cold-window-reader-memory-v1'}):
            with self.assertRaises(ValueError):context.checked_menu_read(self.receipt)
        self.assertIsNone(context.checked_menu_read({}))
        with self.assertRaises(ValueError):context.checked_menu_read(dict(program='fpgaload',hps_context=self.source))

    def test_other_boot_modes_cannot_mix_with_menu(self):
        for flag in ('ddr_read','window_memory','window_read','window_present','window_storage_return',
                     'window_reload','window_service','computer_control','computer_reload',
                     'computer_storage_return','selected_programs','tenfoot_serial','usb_control','usb_shared'):
            for value in (True,1,None):
                with self.subTest(flag=flag,value=value),self.assertRaises(ValueError):
                    context.menu_selection(dict(self.receipt,**{flag:value}))
        for key in ('hps_ddr_read','hps_window_read','hps_computer','hps_rom','hps_program_platform',
                    'usb_input','shared_usb_input','firmware_rom','rom_host_go'):
            with self.subTest(key=key),self.assertRaises(ValueError):
                context.menu_selection(dict(self.receipt,**{key:'supplied'}))

    def test_entry_requires_exact_single_assignments_and_complete_memory(self):
        flag='-X main.expectedMenuPresent=1'
        mapping='-X main.expectedMenuReadMap='+base64.b64encode(context.encode(self.access)).decode()
        def record(flags):return dict(self.receipt,command=['compiler','build','-ldflags',flags])
        self.assertTrue(entry.menu_present_option(record(flag+' '+mapping),self.memory,self.access))
        self.assertFalse(entry.menu_present_option(dict(command=['compiler','build','-ldflags','-s -w']),None,None))
        for flags in ('',flag,mapping,flag+' '+mapping+' '+flag,flag+' '+mapping+' '+mapping,
                      flag.replace('-X ','-X=')+' '+mapping,flag+' '+mapping+'0',
                      flag.replace('=1','=0')+' '+mapping):
            with self.subTest(flags=flags),self.assertRaises(ValueError):
                entry.menu_present_option(record(flags),self.memory,self.access)
        for memory,access in ((None,self.access),(self.memory,None),({'scope':'sealed-cold-window-reader-memory-v1'},self.access)):
            with self.assertRaises(ValueError):entry.menu_present_option(record(flag+' '+mapping),memory,access)
        with self.assertRaises(ValueError):entry.menu_present_option(dict(record(flag+' '+mapping),menu_present=False),None,None)

    def profile(self):
        configuration=dict(base=0x94800000,size=8<<20,core_size=128,chunk_bytes=65536,
                           core_sha384='d'*96,chunk_sha384=['d'*96])
        profile=dict(sd_pio=True,chain_sha256='a'*64,owner_seed=1,entry_image_sha256='b'*64,entry_image_size=128,
            menu_present=True,hps_context=self.source,hps_context_sha256=context.digest(self.source),
            hps_control=self.control,hps_control_sha256=context.digest(self.control),hps_configuration=configuration,
            hps_reader_memory=self.memory,hps_reader_memory_sha256=context.digest(self.memory),
            hps_menu_read=self.access,hps_menu_read_sha256=context.digest(self.access))
        profile['cpu_mapping']=context.cpu_mapping(self.control,configuration,self.memory)
        profile['cpu_mapping_sha256']=context.digest(profile['cpu_mapping'])
        return profile

    def test_cold_header_selects_actual_menu_firmware_abi_and_complete_arena(self):
        profile=self.profile();header=cold.header(profile)
        for line in ('#define FES_NANO_MENU_MEMORY 1','#define FES_NANO_MENU_SLOTS 2U',
                     '#define FES_NANO_MENU_SLOT_BYTES 4194304U','#define FES_NANO_CONTROL_ABI 0x10203043U',
                     '#define FES_NANO_READER_BYTES 8388608U','#define FES_NANO_READER_ARENA_BYTES 8388608U',
                     '#define FES_NANO_WINDOW_MEMORY 1','#define FES_NANO_WINDOW_READ_DIAGNOSTIC 1',
                     '#define FES_NANO_WINDOW_READ_ATTRS_VERIFIED 1','#define FES_NANO_WINDOW_STORAGE_RETURN 0'):
            self.assertIn(line,header)
        for change in ({'menu_present':False},{'menu_present':1},{'window_memory':True},{'window_read':True},
                {'hps_menu_read':None},{'hps_menu_read_sha256':'0'*64},{'sd_pio':False},
                {'hps_control':dict(self.control,fabric_stop=dict(self.control['fabric_stop'],scope='sealed-window-master-stop-v1'))}):
            with self.subTest(change=change),self.assertRaises(ValueError):cold.header(dict(profile,**change))
        for key in self.memory:
            damaged=copy.deepcopy(profile)
            damaged['hps_reader_memory'][key]=True if type(self.memory[key]) is int else 'changed'
            damaged['hps_reader_memory_sha256']=context.digest(damaged['hps_reader_memory'])
            with self.subTest(key=key),self.assertRaises(ValueError):cold.header(damaged)

    def test_cli_rejects_wrong_board_and_mixed_modes_before_output(self):
        output=ROOT/'out/test-menu-rejected-should-not-exist'
        self.assertFalse(output.exists())
        for options in (['probe','--menu-present'],
                ['fpgaload','--board','de25-nano','--profile',str(ROOT/'platform/embedded/nano/sd-profile.json'),
                 '--menu-present','--window-memory']):
            run=subprocess.run(['python3','platform/embedded/tamago/run.py',*options,'--compiler','/absent',
                '--output',str(output)],cwd=ROOT,capture_output=True,text=True,timeout=15)
            self.assertNotEqual(run.returncode,0,run.stdout+run.stderr)
            self.assertFalse(output.exists())

    def test_menu_ocram_requires_complete_descriptors_and_unchanged_exclusion(self):
        ram=cold.firmware_memory
        p=self.profile()
        contract=ram.selected(p,True)
        self.assertEqual(contract,ram.CONTRACT)
        self.assertEqual((contract['base'],contract['limit']),(0x50000,0x60000))
        self.assertEqual(ram.checked(dict(p,firmware_ram=contract)),contract)
        for change in ({'menu_present':False},{'usb_pio':True},{'usb_shared':True},{'sd_pio':False},
                {'hps_reader_memory_sha256':'0'*64},{'hps_menu_read_sha256':'0'*64},
                {'hps_control_sha256':'0'*64},{'cpu_mapping_sha256':'0'*64},
                {'hps_reader_memory':dict(self.memory,arena_bytes=4<<20)},
                {'cpu_mapping':dict(p['cpu_mapping'],firmware_base=0x60000)},
                {'cpu_mapping':dict(p['cpu_mapping'],reader_bytes=4<<20)},
                {'hps_context':dict(self.source,control_abi=0x10203042)}):
            with self.subTest(change=change),self.assertRaises(ValueError):ram.selected(dict(p,**change),True)
        with self.assertRaises(ValueError):ram.checked(dict(p,firmware_ram=dict(contract,limit=0x61000)))


if __name__=='__main__':unittest.main()
