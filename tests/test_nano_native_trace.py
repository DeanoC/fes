"""Host fixtures for one coherent SPL/EL3/EL1 probe, never board evidence."""
import importlib.util
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('nano_native_trace', ROOT / 'platform/embedded/nano/native_trace.py')
trace = importlib.util.module_from_spec(spec)
spec.loader.exec_module(trace)
BUILD = '0123456789abcdef'
SEED = 0x13579bdf00000001
EPOCH = 0x101
OWNER = (SEED ^ EPOCH) | 1


def fixture(core=0, bridge=0x4f):
    """Match the actual producer printf formats, including TF-A NOTICE prefix."""
    cpus = ','.join('0' if i == core else '1' for i in range(4))
    return '\r\n'.join((
        f'U-Boot SPL 2025.01-fes-nano-{BUILD} (source-bound)',
        f'FES_NANO_SPL BEGIN cause=00000004 per0=ff7fbfff bridge={bridge:08x} cpus=0000000f release=00000000 kind=4 valid=1',
        f'FES_NANO_SPL SCOPE mpidr={0x81000000 | core << 8:x} core={core} cause_now=00000004 cpus_now=0000000f release_now=00000000 reset_timeouts=00000000',
        'SDRAM calibration complete',
        'Trying to boot from MMC1',
        'FES_NANO_SPL FIT verified before parsing',
        f'FES_NANO_SPL HANDOFF owner={OWNER:x} epoch={EPOCH:x} per0=ff21bfbf bridge={bridge:08x} sd=00000000',
        f'NOTICE:  FES_NANO_OWNED_BOOT provider=1 result=0 reset={bridge:08x}',
        f'U-Boot 2025.01-fes-nano-{BUILD} (source-bound)',
        'DRAM:  1 GiB',
        'SOCFPGA_AGILEX5 # fesboot',
        f'FES_NANO_BOOT id={BUILD}',
        'FES_NANO_MEMORY ram_top=c0000000 sp=bfd00000 malloc=bfa00000..bfc00000 reloc=bfe00000 fdt=bfc80000',
        f'FES_NANO_OWNED phase=before-entry status=0 flags=46424f31000000df owner={OWNER:x} generation=1',
        f'FES_NANO_OWNED phase=after-remove status=0 flags=46424f31000000df owner={OWNER:x} generation=1',
        f'FES_NANO_HANDOFF affinity={core << 8:x} counter=400000000 cpus={cpus} per0=ff21bfbf bridges={bridge:08x}',
        'FES_NANO_HANDOFF PASS serial-only',
        'FES_ENTRY EL2 CLEAN',
        'FES_ENTRY EL1 HANDOFF',
        'PASS volatile memory filesystem (not SD/FAT32)',
        'FES_NATIVE_GO_PROBE PASS os=tamago board=de25-nano',
        ''))


class NativeTraceTests(unittest.TestCase):
    def decode(self, data):
        return trace.decode_native_boot(data, BUILD, SEED)

    def test_both_primary_types_and_masked_bridge_readback(self):
        for core in (0, 2):
            for bridges in (0xf, 0x4f):
                with self.subTest(core=core, bridges=bridges):
                    result = self.decode(fixture(core, bridges))
                    self.assertTrue(result['complete'], result)
                    self.assertTrue(result['loader_ready'])
                    self.assertEqual(tuple(row['stage'] for row in result['records']), trace.STAGES)
                    self.assertFalse(result['core_load_acceptance'])

    def test_each_fragment_boundary_preserves_partial_progress(self):
        raw = fixture().encode()
        for end in range(len(raw)):
            result = self.decode(raw[:end])
            self.assertFalse(result['complete'], end)
            self.assertEqual(result['errors'], [], (end, result))
        self.assertTrue(self.decode(raw)['complete'])
        self.assertFalse(self.decode(b'FES_NATIVE_GO_PROBE PASS os=tamago board=de25-nano')['complete'])
        self.assertFalse(self.decode(raw + b'FES_ENTRY REJECT')['complete'])
        self.assertTrue(self.decode(raw + b'FES_ENTRY REJECT\r\n')['errors'])

    def test_loader_gate_requires_one_sealed_chain_and_prompt(self):
        raw = fixture().split('SOCFPGA_AGILEX5 # ')[0]
        self.assertFalse(self.decode(raw)['loader_ready'])
        ready = self.decode(raw + 'SOCFPGA_AGILEX5 # ')
        self.assertTrue(ready['loader_ready'], ready)
        self.assertFalse(ready['complete'])
        for old, new in (('provider=1', 'provider=0'), ('valid=1', 'valid=0'),
                         (f'owner={OWNER:x} epoch=101', f'owner={OWNER + 2:x} epoch=101'),
                         ('reset=0000004f', 'reset=0000004b'), ('MMC1', 'MMC2')):
            with self.subTest(new=new):
                self.assertFalse(self.decode((raw + 'SOCFPGA_AGILEX5 # ').replace(old, new))['loader_ready'])

    def test_same_wrong_owner_in_both_snapshots_cannot_replace_spl_owner(self):
        raw = fixture().replace(f'owner={OWNER:x} generation', f'owner={OWNER + 2:x} generation')
        result = self.decode(raw)
        self.assertFalse(result['complete'])
        self.assertTrue(any('before-entry owner' in error for error in result['errors']))
        self.assertTrue(any('after-remove owner' in error for error in result['errors']))
        wrong_everywhere = fixture().replace(f'owner={OWNER:x}', f'owner={OWNER + 2:x}')
        self.assertIn('SPL owner/epoch differs from matched chain', self.decode(wrong_everywhere)['errors'])

    def test_changed_scope_and_failed_live_ownership_reject(self):
        changes = (
            ('epoch=101', 'epoch=0'), ('epoch=101', 'epoch=103'),
            ('valid=1', 'valid=0'), ('kind=4', 'kind=1'),
            ('cause=00000004', 'cause=00000006'), ('release_now=00000000', 'release_now=00000002'),
            ('cpus_now=0000000f', 'cpus_now=0000000d'), ('reset_timeouts=00000000', 'reset_timeouts=00000001'),
            ('sd=00000000', 'sd=00000001'), ('per0=ff21bfbf', 'per0=ff21bf3f'),
            ('flags=46424f31000000df', 'flags=00000001000000df'), ('generation=1', 'generation=2'),
            ('status=0 flags=', 'status=5 flags='), ('counter=400000000', 'counter=62500000'),
            ('cpus=0,1,1,1', 'cpus=0,0,1,1'), ('affinity=0 ', 'affinity=200 '),
            (f'FES_NANO_BOOT id={BUILD}', 'FES_NANO_BOOT id=fedcba9876543210'),
        )
        for old, new in changes:
            with self.subTest(new=new):
                result = self.decode(fixture().replace(old, new))
                self.assertFalse(result['complete'], result)
                self.assertTrue(result['errors'], result)

    def test_missing_duplicate_reordered_or_foreign_phases_cannot_pass(self):
        lines = fixture().splitlines(keepends=True)
        for row in self.decode(fixture())['records']:
            index = row['line'] - 1
            with self.subTest(stage=row['stage']):
                self.assertFalse(self.decode(''.join(lines[:index] + lines[index + 1:]))['complete'])
                self.assertFalse(self.decode(''.join(lines[:index] + [lines[index]] + lines[index:]))['complete'])
        self.assertFalse(self.decode(fixture() + fixture())['complete'])
        raw = fixture().replace('FES_ENTRY EL2 CLEAN\r\nFES_ENTRY EL1 HANDOFF',
                                'FES_ENTRY EL1 HANDOFF\r\nFES_ENTRY EL2 CLEAN')
        self.assertFalse(self.decode(raw)['complete'])
        self.assertFalse(self.decode('U-Boot SPL 2025.01-factory\r\n' + fixture())['complete'])

    def test_rejection_and_malformed_markers_remain_failures_even_after_pass(self):
        for line in ('FES_ENTRY EXCEPTION', 'FES_NANO_HANDOFF REJECT live state',
                     'FES_NATIVE_GO_PROBE FAIL', 'FES_NANO_OWNED phase=unknown status=0',
                     'panic: runtime fault', 'FES_NANO_SPL HANDOFF owner=nothex'):
            with self.subTest(line=line):
                result = self.decode(fixture() + line + '\r\n')
                self.assertFalse(result['complete'])
                self.assertTrue(result['errors'])

    def test_argument_and_capture_bounds(self):
        for seed in (0, 2, -1, 1 << 64, True):
            with self.assertRaises(ValueError):trace.decode_native_boot(b'', BUILD, seed)
        with self.assertRaises(ValueError):trace.decode_native_boot(b'', 'wrong', SEED)
        with self.assertRaises(TypeError):self.decode(100)
        self.assertFalse(self.decode(b'x' * (trace.MAX_BYTES + 1))['complete'])
        self.assertTrue(self.decode('\u00e9' * trace.MAX_BYTES)['errors'])

    def test_snapshot_flags_match_generated_wire_oracle(self):
        values = json.loads((ROOT / 'sources/mister-packages/testdata/oracles/fes-embedded-bridges.json').read_text())['constants']
        expected = values['FesEmbeddedBridgeMask'] | (1 << values['FesEmbeddedBridgeFabricShift'])
        expected |= values['FesEmbeddedBridgeNeverEnabled'] | values['FesEmbeddedBridgeMastersContained']
        self.assertEqual(trace.OWNED_FLAGS, (values['FesEmbeddedBridgeTag'] << 32) | expected)


if __name__ == '__main__':
    unittest.main()
