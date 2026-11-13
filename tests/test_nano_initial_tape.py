"""Source-bound secure tape selection through native, entry and cold producers."""
import copy
from pathlib import Path
import unittest
import test_nano_rom_context as rom

hps = rom.hps
cold = rom.cold


class InitialTapeSelectionTests(unittest.TestCase):
    def record(self):
        fixture = rom.NativeComputerContextTests(); fixture.setUp()
        profile = fixture.profile()
        profile.update(program='fpgaload', initial_tape=True, selected_programs=True,
            computer_reload=True, computer_storage_return=True, tenfoot_serial=True,
            usb_control=True, usb_shared=True, usb_input='gamepad',
            command=['go', 'build', '-ldflags', '-s -w -X main.expectedInitialTape=1'])
        return profile

    def test_valid_selection_and_explicit_link_assignment(self):
        record = self.record()
        for protocol in ('boot-keyboard', 'gamepad'):
            with self.subTest(protocol=protocol):
                record['usb_input'] = protocol
                self.assertTrue(hps.initial_tape.checked(record))
                self.assertTrue(hps.initial_tape.linked(record))
        self.assertFalse(hps.initial_tape.checked({}))
        self.assertFalse(hps.initial_tape.linked({'command':['go','build','-ldflags','-s -w']}))

    def test_incomplete_or_mixed_selection_rejects(self):
        changes = [('initial_tape', 1), ('initial_tape', None), ('program','tenfoot'),
            ('usb_input','mouse'), ('hps_context',None), ('hps_computer',None), ('hps_rom',None),
            ('hps_configuration',None)]
        changes += [(k, False) for k in ('selected_programs','computer_reload','computer_storage_return',
                                       'tenfoot_serial','usb_control','usb_shared')]
        changes += [(k, True) for k in ('window_memory','window_read','window_present',
            'window_storage_return','window_reload','window_service','menu_present','ddr_read','smmu_observe')]
        for key,value in changes:
            with self.subTest(key=key,value=value), self.assertRaises(ValueError):
                hps.initial_tape.checked(dict(self.record(), **{key:value}))
        for field, key, value in [('hps_context','programming_profile',hps.ROM_PROFILE),
                ('hps_computer','cpu_base',0), ('hps_computer','capabilities',0x3),
                ('hps_computer','abi_minor',1), ('hps_rom','bytes',16384),
                ('hps_rom','cpu_base',0), ('hps_configuration','size',4<<20),
                ('hps_configuration','base',0x95000000)]:
            r=copy.deepcopy(self.record());r[field][key]=value
            with self.subTest(field=field,key=key), self.assertRaises(ValueError):
                hps.initial_tape.checked(r)

    def test_link_cannot_add_drop_duplicate_or_change_service_selection(self):
        for flags in ('', '-X main.expectedInitialTape=0', 'main.expectedInitialTape=1',
                '-X main.expectedInitialTape=1 -X main.expectedInitialTape=1',
                '-X othermain.expectedInitialTape=1'):
            r=self.record();r['command']=['go','build','-ldflags',flags]
            with self.subTest(flags=flags), self.assertRaises(ValueError):
                hps.initial_tape.linked(r)
        r=self.record();r['initial_tape']=False
        with self.assertRaises(ValueError):hps.initial_tape.linked(r)

    def test_cold_flag_requires_matching_complete_secure_memory_contract(self):
        r=self.record();r['usb_pio']=True
        with self.assertRaises(ValueError):cold.initial_tape_header(r)
        r['firmware_ram']=copy.deepcopy(cold.firmware_memory.CONTRACT)
        self.assertEqual(cold.initial_tape_header(r),'#define FES_NANO_INITIAL_TAPE 1\n')
        r['firmware_ram']['base']+=4096
        with self.assertRaises(ValueError):cold.initial_tape_header(r)
        self.assertEqual(cold.initial_tape_header({}),'#define FES_NANO_INITIAL_TAPE 0\n')

    def test_selection_helper_is_in_every_producer_source_inventory(self):
        helper=Path(__file__).resolve().parents[1]/'platform/embedded/nano/initial_tape.py'
        self.assertIn(helper,hps.inputs())
        self.assertIn(helper,cold.sources())
        self.assertTrue(all(isinstance(p,Path) for p in hps.inputs()))
