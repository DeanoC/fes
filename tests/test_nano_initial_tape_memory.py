"""Independent staging geometry must leave all existing allocations intact."""
import copy
import json
from pathlib import Path
import unittest
import test_nano_initial_tape as fixture
import test_nano_combined_program as combined

ROOT = Path(__file__).resolve().parents[1]
tape = fixture.hps.initial_tape


class InitialTapeMemoryTests(unittest.TestCase):
    def inputs(self):
        case = combined.CombinedProgramTests(); case.setUp()
        mapping = case.profile["cpu_mapping"]
        reserved = json.loads((ROOT/'platform/embedded/nano/sd-profile.json').read_text())['reserved']
        return mapping, reserved, dict(base=0x96080000, bytes=8 << 20)

    def test_separate_mapping_keeps_complete_source_and_both_menu_slots(self):
        mapping, reserved, entry = self.inputs()
        before = copy.deepcopy((mapping, reserved, entry))
        result = tape.staging_layout(mapping, reserved, entry)
        self.assertEqual((result['base'], result['bytes'], result['payload_bytes']), (0x95800000, 2 << 20, 16384))
        self.assertEqual(result['metadata_base'] + result['metadata_bytes'], 0x95a00000)
        self.assertEqual((mapping, reserved, entry), before)
        self.assertFalse(result['configuration_reused'] or result['physical_authority'])
        self.assertLessEqual(result['base'] + result['bytes'], entry['base'])

    def test_old_partial_or_changed_mapping_cannot_select_staging(self):
        mapping, reserved, entry = self.inputs()
        for key in mapping:
            for bad_value in (None, True, 'foreign'):
                bad = copy.deepcopy(mapping); bad[key] = bad_value
                with self.subTest(key=key, value=bad_value), self.assertRaises(ValueError):
                    tape.staging_layout(bad, reserved, entry)
        for key in mapping:
            bad = copy.deepcopy(mapping); del bad[key]
            with self.subTest(missing=key), self.assertRaises(ValueError):
                tape.staging_layout(bad, reserved, entry)

    def test_boot_reservations_cannot_be_dropped_or_claim_tape_bytes(self):
        mapping, reserved, entry = self.inputs()
        for n in range(len(reserved)):
            with self.subTest(omitted=n), self.assertRaises(ValueError):
                tape.staging_layout(mapping, reserved[:n]+reserved[n+1:], entry)
        for start, size in ((0x95800000, 1), (0x957fffff, 2), (0x959fffff, 2), (0x95800000, 2 << 20)):
            with self.subTest(start=start), self.assertRaises(ValueError):
                tape.staging_layout(mapping, reserved+[dict(start=start,size=size)], entry)

    def test_invalid_reservation_and_entry_cannot_supply_proof(self):
        mapping, reserved, entry = self.inputs()
        for row in ({},dict(start=True,size=4096),dict(start=0,size=0),dict(start=(1<<64)-1,size=2),dict(start=-1,size=1)):
            with self.subTest(row=row), self.assertRaises(ValueError):
                tape.staging_layout(mapping,reserved+[row],entry)
        for candidate in ({},dict(base=0x95800000,bytes=4096),dict(base=0x96080000,bytes=True),dict(base=0x96080000,bytes=0),dict(base=0x96080000,bytes=(16<<20)+1)):
            with self.subTest(entry=candidate), self.assertRaises(ValueError):
                tape.staging_layout(mapping,reserved,candidate)
