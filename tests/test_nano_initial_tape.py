"""Source-bound secure tape selection through native, entry and cold producers."""
import copy
from pathlib import Path
import unittest
import test_nano_rom_context as rom
import test_nano_combined_program as combined

hps = rom.hps
cold = rom.cold


class InitialTapeSelectionTests(unittest.TestCase):
    def record(self):
        fixture = combined.CombinedProgramTests(); fixture.setUp()
        profile = fixture.profile
        profile.update(program='fpgaload', initial_tape=True, selected_programs=True,
            computer_reload=True, computer_storage_return=True, tenfoot_serial=True,
            usb_control=True, usb_shared=True, usb_input='gamepad',
            command=['go', 'build', '-ldflags', '-s -w -X main.expectedInitialTape=1'])
        profile['initial_tape_memory']=copy.deepcopy(hps.initial_tape.MEMORY)
        profile['initial_tape_memory_sha256']=hps.initial_tape.memory_digest(profile['initial_tape_memory'])
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
            'window_storage_return','window_reload','menu_present','ddr_read','smmu_observe')]
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
        self.assertIn('#define FES_NANO_TAPE_BASE 0x95800000ULL\n',cold.initial_tape_header(r))
        r['firmware_ram']['base']+=4096
        with self.assertRaises(ValueError):cold.initial_tape_header(r)
        self.assertEqual(cold.initial_tape_header({}),'#define FES_NANO_INITIAL_TAPE 0\n')

    def test_selection_helper_is_in_every_producer_source_inventory(self):
        helper=Path(__file__).resolve().parents[1]/'platform/embedded/nano/initial_tape.py'
        self.assertIn(helper,hps.inputs())
        self.assertIn(helper,cold.sources())
        self.assertTrue(all(isinstance(p,Path) for p in hps.inputs()))

    def test_independent_memory_contract_cannot_be_omitted_mixed_or_unselected(self):
        record=self.record()
        for name in ('initial_tape_memory','initial_tape_memory_sha256'):
            changed=copy.deepcopy(record);del changed[name]
            with self.subTest(missing=name),self.assertRaises(ValueError):
                hps.initial_tape.checked(changed)
        for key,value in record['initial_tape_memory'].items():
            changed=copy.deepcopy(record)
            changed['initial_tape_memory'][key]=None
            changed['initial_tape_memory_sha256']=hps.initial_tape.memory_digest(changed['initial_tape_memory'])
            with self.subTest(field=key),self.assertRaises(ValueError):
                hps.initial_tape.checked(changed)
        for changed in (dict(record,initial_tape=False),dict(record,initial_tape_memory_sha256='0'*64)):
            with self.assertRaises(ValueError):hps.initial_tape.checked(changed)
        for mapping in (None,{},dict(record['cpu_mapping'],reader_bytes=2<<20),
                        dict(record['cpu_mapping'],reader_base=0x95800000)):
            with self.subTest(mapping=mapping),self.assertRaises(ValueError):
                hps.initial_tape.checked(dict(record,cpu_mapping=mapping))
        # The former borrowed-configuration profile has no replacement-memory
        # proof. It must not be reinterpreted by a new producer.
        del record['initial_tape_memory'];del record['initial_tape_memory_sha256']
        with self.assertRaises(ValueError):cold.initial_tape_header(record)
