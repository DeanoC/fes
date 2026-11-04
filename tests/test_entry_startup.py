"""CPU progress never substitutes for native ownership or physical admission."""
import importlib.util
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('entry_startup', ROOT / 'platform/embedded/entry/startup.py')
startup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(startup)


def fixture():
    lines = []
    for index, phase in enumerate(startup.PHASES):
        state = 0 if index in (0, 4) else 0x1000 if index < 5 else 0x1005
        lines.append(f'FES_BOOT_STEP phase={phase} ticks={400000000 * (index + 1):016x} '
                     f'hz={400000000:016x} el={2 if index < 5 else 1} sctlr={state:016x}\r\n')
    return lines


class StartupTests(unittest.TestCase):
    def test_complete_counter_sequence_preserves_evidence_boundary(self):
        report = startup.decode(''.join(fixture()))
        self.assertTrue(report['complete'], report)
        self.assertEqual(report['errors'], [])
        self.assertEqual(report['timings_us']['decode_end'], 2000000)
        self.assertEqual(report['timings_us']['retain_end'], 13000000)
        self.assertFalse(report['core_load_acceptance'])

    def test_loader_only_and_every_partial_checkpoint(self):
        lines = fixture()
        for count in range(len(lines)):
            with self.subTest(count=count):
                report = startup.decode(''.join(lines[:count]))
                self.assertEqual(report['errors'], [])
                self.assertFalse(report['complete'])
                self.assertEqual(report['last_checkpoint'], startup.PHASES[count - 1] if count else None)
        self.assertTrue(startup.decode(''.join(lines[:5]), through='handoff')['complete'])
        self.assertEqual(startup.decode(''.join(lines)[:-1])['last_checkpoint'], 'retain_begin')

    def test_bad_cache_exception_level_and_counter_cannot_establish_timings(self):
        lines = fixture()
        for index, old, new in (
                (0, 'sctlr=0000000000000000', 'sctlr=0000000000001000'),
                (1, 'sctlr=0000000000001000', 'sctlr=0000000000001004'),
                (4, 'sctlr=0000000000000000', 'sctlr=0000000000001000'),
                (5, 'sctlr=0000000000001005', 'sctlr=0000000000001004'),
                (5, 'el=1', 'el=2'),
                (2, f'hz={400000000:016x}', f'hz={1:016x}'),
                (0, f'hz={400000000:016x}', f'hz={0:016x}'),
                (2, f'ticks={1200000000:016x}', f'ticks={1:016x}')):
            with self.subTest(index=index, value=new):
                changed = lines.copy()
                self.assertIn(old, changed[index])
                changed[index] = changed[index].replace(old, new)
                report = startup.decode(''.join(changed))
                self.assertFalse(report['complete'])
                self.assertTrue(report['errors'])
                self.assertEqual(report['timings_us'], {})

    def test_corrupt_duplicate_missing_and_reset_sequences(self):
        lines = fixture()
        for changed in (lines[1:], lines[:3] + lines[4:], lines + lines,
                        lines[:3] + [lines[2]] + lines[3:],
                        [lines[0].replace('loader_begin', 'foreign')] + lines[1:],
                        [lines[0].replace(' hz=', ' Hz=')] + lines[1:]):
            report = startup.decode(''.join(changed))
            self.assertFalse(report['complete'])
            self.assertTrue(report['errors'])

    def test_rejection_requires_an_entered_decoder_and_clean_parking_state(self):
        lines = fixture()
        rejected = ('FES_BOOT_STEP phase=rejected ticks=0000000090000000 '
                    'hz=0000000017d78400 el=2 sctlr=0000000000000000\n')
        for count in (2, 3):
            report = startup.decode(''.join(lines[:count]) + rejected, through='handoff')
            self.assertEqual(report['errors'], [])
            self.assertTrue(report['rejected'])
            self.assertFalse(report['complete'])
        for changed in (rejected, ''.join(lines[:1]) + rejected,
                        ''.join(lines[:2]) + rejected.replace('sctlr=0000000000000000', 'sctlr=0000000000001000'),
                        ''.join(lines[:2]) + rejected + lines[2]):
            self.assertTrue(startup.decode(changed)['errors'])

    def test_absent_oversized_and_foreign_output_supplies_no_admission(self):
        self.assertFalse(startup.decode('ordinary old console\n')['complete'])
        self.assertFalse(startup.decode('FES_NATIVE_SOMETHING PASS\n')['complete'])
        self.assertTrue(startup.decode(bytes(startup.MAX_BYTES + 1))['errors'])
        with self.assertRaises(ValueError):
            startup.decode('', through='unknown')


if __name__ == '__main__':
    unittest.main()
