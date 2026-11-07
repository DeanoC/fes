"""One computer boot anchor, two sealed resource sets and one full exclusion."""
import copy
import base64
import unittest
from unittest.mock import patch

import test_nano_rom_context as rom
import test_nano_menu_boot as menu

hps=rom.hps


class CombinedProgramTests(unittest.TestCase):
    def setUp(self):
        c=rom.NativeComputerContextTests();c.setUp()
        m=menu.MenuBootTests();m.setUp()
        self.profile=c.profile()
        def platform(context, inventory):
            values={k:context[k] for k in ('platform','device','programming_profile','abi','abi_major',
                'abi_minor','control_abi','hps_io_hash','pfg_identity')}
            values.update(format=2,scope='nano-contained-program-platform-v2',
                hps_parameters_sha256='1'*64,hps_configuration_sha256='2'*64,master_inventory_sha256=inventory)
            return dict(format=1,scope='nano-program-platform-source-v1',boot_context_sha256=hps.digest(context),platform=values)
        self.platform=platform(c.context,'3'*64)
        self.companion=dict(format=1,scope='nano-compiled-menu-program-v1',context=m.source,
            platform=platform(m.source,'4'*64),control=m.control,configuration=m.profile()['hps_configuration'],
            memory=m.memory,access=m.access)
        self.profile.update(selected_programs=True,computer_reload=True,computer_storage_return=True,
            hps_program_platform=self.platform,hps_program_platform_sha256=hps.digest(self.platform),
            hps_menu_program=self.companion,hps_menu_program_sha256=hps.digest(self.companion))
        self.profile['cpu_mapping']=hps.cpu_mapping(c.control,self.profile['hps_configuration'],
            self.profile['hps_reader_memory'],menu_program=self.companion)
        self.profile['cpu_mapping_sha256']=hps.digest(self.profile['cpu_mapping'])

    def test_pair_keeps_anchor_and_separate_inventories_with_exact_union(self):
        p=self.profile;before=copy.deepcopy(p)
        self.assertEqual(hps.compiled_menu_program(dict(p,program='fpgaload')),self.companion)
        self.assertEqual(p['hps_reader_memory']['arena_bytes'],2<<20)
        self.assertEqual(p['cpu_mapping']['reader_bytes'],8<<20)
        header=rom.cold.header(p)
        for line in ('FES_NANO_PROGRAM_ROLES 1','FES_NANO_CONTROL_ABI 0x10203041U',
                     'FES_NANO_READER_BYTES 8192U','FES_NANO_READER_ARENA_BYTES 8388608U',
                     'FES_NANO_WINDOW_STORAGE_RETURN 1','FES_NANO_COMPUTER_STORAGE_RETURN 1',
                     'FES_NANO_MENU_MEMORY 1'):
            self.assertIn('#define '+line,header)
        self.assertEqual(self.profile,before)
        layout=rom.cold.firmware_memory.selected(p,True)
        self.assertEqual(layout['base'],0x50000)

    def test_every_shared_fingerprint_and_resource_change_rejects(self):
        for key in ('platform','device','hps_io_hash','pfg_identity','hps_parameters_sha256','hps_configuration_sha256'):
            value=copy.deepcopy(self.companion);value['platform']['platform'][key]='other'
            with self.subTest(key=key),self.assertRaises(ValueError):hps.validate_menu_program(self.profile['hps_context'],self.platform,value)
        for field,key,value in (('memory','arena_bytes',2<<20),('memory','slots',1),
                ('access','arcache',15),('access','control_map_sha256','0'*64),
                ('control','cpu_base',0x20010500),('control','fabric_stop',{}),
                ('configuration','base',0x95000000)):
            changed=copy.deepcopy(self.companion);changed[field][key]=value
            with self.subTest(field=field,key=key),self.assertRaises(ValueError):
                hps.validate_menu_program(self.profile['hps_context'],self.platform,changed)
        for update in ({'hps_menu_program_sha256':'0'*64},{'selected_programs':False},
                       {'computer_storage_return':False},{'menu_present':True},{'window_memory':True},
                       {'hps_program_platform_sha256':'0'*64}):
            with self.subTest(update=update),self.assertRaises(ValueError):rom.cold.header(dict(self.profile,**update))
        changed=copy.deepcopy(self.profile);changed['cpu_mapping']['reader_bytes']=2<<20
        changed['cpu_mapping_sha256']=hps.digest(changed['cpu_mapping'])
        with self.assertRaises(ValueError):rom.cold.header(changed)
        with self.assertRaises(ValueError):rom.cold.firmware_memory.selected(changed,True)

    def test_fresh_companion_source_is_required_at_receipt_admission(self):
        p=dict(self.profile,program='fpgaload',hps_project='computer',hps_menu_project='menu')
        with patch.object(hps,'checked',return_value=p['hps_context']), \
             patch.object(hps,'checked_program_platform',return_value=self.platform), \
             patch.object(hps,'menu_program',return_value=self.companion):
            self.assertEqual(hps.checked_menu_program(p),self.companion)
            for field in ('context','control','configuration','memory','access','platform'):
                changed=copy.deepcopy(p);changed['hps_menu_program'][field]['format']=99
                changed['hps_menu_program_sha256']=hps.digest(changed['hps_menu_program'])
                with self.subTest(field=field),self.assertRaises(ValueError):hps.checked_menu_program(changed)
        self.assertIsNone(hps.checked_menu_program({}))
        with self.assertRaises(ValueError):hps.checked_menu_program(dict(p,hps_menu_project=None))

    def test_entry_requires_one_exact_companion_link_assignment(self):
        assignment='main.expectedCompiledMenuProgram='+base64.b64encode(hps.encode(self.companion)).decode()
        receipt={'command':['go','build','-ldflags','-s -w -X '+assignment]}
        self.assertEqual(menu.entry.compiled_menu_option(receipt,self.companion),self.companion)
        for flags in ('-s -w','-X '+assignment+' -X '+assignment,'-X main.expectedCompiledMenuProgram=other',
                      '-X'+assignment,assignment):
            with self.subTest(flags=flags[:60]),self.assertRaises(ValueError):
                menu.entry.compiled_menu_option({'command':['go','build','-ldflags',flags]},self.companion)
        with self.assertRaises(ValueError):menu.entry.compiled_menu_option(receipt,None)
        self.assertIsNone(menu.entry.compiled_menu_option({'command':['go','build','-ldflags','-s -w']},None))


if __name__=='__main__':unittest.main()
