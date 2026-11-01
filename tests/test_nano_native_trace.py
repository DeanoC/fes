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
                         ('per0=ff7fbfff', 'per0=ff7f3fff'),  # unused SD ECC reset released
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


class NativeHPSContextTraceTests(unittest.TestCase):
    expected = {'sha256': 'a'*64, 'package_id': 'b'*64, 'build_id': 'c'*32}

    def capture(self):
        marker = (f"FES_NATIVE_HPS_CONTEXT PASS sha256={self.expected['sha256']} "
                  f"package_id={self.expected['package_id']} build_id={self.expected['build_id']} "
                  f'owner={OWNER:x} generation=1 programming=unavailable')
        return fixture().replace('FES_NATIVE_GO_PROBE PASS os=tamago board=de25-nano', marker)

    def decode(self, raw):
        return trace.decode_native_boot(raw, BUILD, SEED, hps_context=self.expected)

    def test_exact_source_context_completes_only_this_diagnostic(self):
        report = self.decode(self.capture())
        self.assertTrue(report['complete'], report)
        self.assertEqual(report['last_checkpoint'], 'hps_context')
        self.assertFalse(report['core_load_acceptance'])
        self.assertFalse(trace.decode_native_boot(self.capture(), BUILD, SEED)['complete'])
        self.assertFalse(self.decode(fixture())['complete'])
        # UART fragments cannot grant a successful context before its newline.
        self.assertFalse(self.decode(self.capture().rstrip('\r\n'))['complete'])

    def test_wrong_identity_owner_generation_and_extra_authority_reject(self):
        for old, new in (('sha256='+'a'*64, 'sha256='+'d'*64),
                         ('package_id='+'b'*64, 'package_id='+'d'*64),
                         ('build_id='+'c'*32, 'build_id='+'d'*32),
                         (f'owner={OWNER:x} generation=1 programming=', f'owner={OWNER+2:x} generation=1 programming='),
                         ('generation=1 programming=', 'generation=2 programming='),
                         ('programming=unavailable', 'programming=available')):
            with self.subTest(new=new):
                report = self.decode(self.capture().replace(old, new))
                self.assertFalse(report['complete'], report)
                self.assertTrue(report['errors'])
        self.assertFalse(self.decode(self.capture()+self.capture().splitlines()[-1]+'\n')['complete'])
        self.assertFalse(self.decode(self.capture()+'FES_NATIVE_HPS_CONTEXT FAIL\n')['complete'])

    def test_expected_context_is_independent_and_excludes_sd_profiles(self):
        for expected in ({}, dict(self.expected, sha256='0'*64), dict(self.expected, build_id='wrong'),
                         dict(self.expected, trusted=True), []):
            with self.subTest(expected=expected), self.assertRaises(ValueError):
                trace.decode_native_boot(b'', BUILD, SEED, hps_context=expected)
        with self.assertRaises(ValueError):
            trace.decode_native_boot(b'', BUILD, SEED, hps_context=self.expected,
                                     storage={'bytes': 1, 'sha256': 'a'*64})


class NativeSDTraceTests(unittest.TestCase):
    expected={'bytes':1098,'sha256':'a'*64}
    def capture(self):
        raw=fixture().replace(f'U-Boot 2025.01-fes-nano-{BUILD}',
              f'NOTICE:  FES_NANO_SD_PLATFORM setup=1 ready=1\r\nU-Boot 2025.01-fes-nano-{BUILD}')
        raw=raw[:raw.index('PASS volatile memory filesystem')]
        return raw+'\r\n'.join((
            'FES_STORAGE_PROBE board=de25-nano EL=1 counter=400000000Hz writes=disabled',
            f'FES_NANO_SD READ bytes=1098 sha256={"a"*64} cid={"b"*30} sectors=7774208 owner={OWNER:016x} generation=3',
            f'FES_NANO_SD CONTAINED owner={OWNER:016x} generation=4',
            f'FES_NANO_SD RESUMED bytes=1098 sha256={"a"*64} owner={OWNER:016x} generation=6',
            f'FES_NANO_SD PASS read-only owner={OWNER:016x} generation=7',''))
    def decode(self,raw):return trace.decode_native_boot(raw,BUILD,SEED,storage=self.expected)
    def test_full_read_contain_resume_and_final_containment(self):
        report=self.decode(self.capture())
        self.assertTrue(report['complete'],report)
        self.assertFalse(report['core_load_acceptance'])
        self.assertEqual(report['last_checkpoint'],'sd_pass')
        prefix=self.capture().split('SOCFPGA_AGILEX5 # ')[0]+'SOCFPGA_AGILEX5 # '
        self.assertTrue(self.decode(prefix)['loader_ready'])
        self.assertFalse(self.decode(prefix)['complete'])
    def test_every_fragment_remains_partial_without_false_errors(self):
        raw=self.capture().encode()
        for end in range(len(raw)):
            report=self.decode(raw[:end])
            self.assertFalse(report['complete'],end)
            self.assertEqual(report['errors'],[],(end,report))
    def test_bad_hash_owner_generation_setup_and_each_missing_or_duplicate_stage(self):
        raw=self.capture()
        for old,new in (('setup=1 ready=1','setup=0 ready=0'),('bytes=1098','bytes=1099'),
                        ('sha256='+'a'*64,'sha256='+'0'*64),('generation=7','generation=6'),
                        ('generation=6','generation=5'),('generation=4','generation=3'),
                        ('generation=3','generation=2'),('cid='+'b'*30,'cid='+'0'*30),
                        ('cid='+'b'*30,'cid='+'b'*32),('cid='+'b'*30,'cid='+'b'*28),
                        (f'owner={OWNER:016x} generation',f'owner={OWNER+2:016x} generation')):
            with self.subTest(new=new):
                report=self.decode(raw.replace(old,new))
                self.assertFalse(report['complete']);self.assertTrue(report['errors'])
        lines=raw.splitlines(keepends=True)
        for row in self.decode(raw)['records']:
            i=row['line']-1
            with self.subTest(stage=row['stage']):
                self.assertFalse(self.decode(''.join(lines[:i]+lines[i+1:]))['complete'])
                self.assertFalse(self.decode(''.join(lines[:i]+[lines[i]]+lines[i:]))['complete'])
        self.assertTrue(self.decode(raw+'NOTICE:  FES_NANO_SD_FAILURE stage=12\r\n')['errors'])
    def test_checked_expected_file_is_required_and_serial_cannot_grant_sd(self):
        for value in ({},{'bytes':True,'sha256':'a'*64},{'bytes':1098,'sha256':'wrong'}):
            with self.assertRaises(ValueError):trace.decode_native_boot('',BUILD,SEED,storage=value)
        self.assertFalse(trace.decode_native_boot(self.capture(),BUILD,SEED)['complete'])
        disabled=fixture().replace(f'U-Boot 2025.01-fes-nano-{BUILD}',
                 f'NOTICE:  FES_NANO_SD_PLATFORM setup=0 ready=0\r\nU-Boot 2025.01-fes-nano-{BUILD}')
        self.assertTrue(trace.decode_native_boot(disabled,BUILD,SEED)['complete'])

    def test_retained_physical_trace_has_exact_r2_cid_and_complete_containment(self):
        import hashlib
        folder=ROOT/'tests/fixtures/nano'
        expected=json.loads((folder/'sd-native-complete.json').read_text())
        raw=(ROOT/expected['console_path']).read_bytes()
        self.assertEqual(hashlib.sha256(raw).hexdigest(),expected['console_sha256'])
        report=trace.decode_native_boot(raw,expected['build_id'],expected['owner_seed'],storage=expected['storage'])
        self.assertTrue(report['complete'],report)
        read=next(row['values'] for row in report['records'] if row['stage']=='read')
        self.assertEqual(len(read['cid']),30)
        self.assertEqual(read['sectors'],expected['card_sectors'])
        self.assertEqual(report['last_checkpoint'],'sd_pass')


class NativePackageTraceTests(unittest.TestCase):
    expected={'package_id':'c'*64,'payload_sha256':'d'*64,'payload_bytes':12,'build_id':'e'*32}

    def capture(self):
        raw=fixture().replace(f'U-Boot 2025.01-fes-nano-{BUILD}',
              f'NOTICE:  FES_NANO_SD_PLATFORM setup=1 ready=1\r\nU-Boot 2025.01-fes-nano-{BUILD}')
        raw=raw[:raw.index('PASS volatile memory filesystem')]
        identity=f'package_id={"c"*64} payload_sha256={"d"*64}'
        return raw+'\r\n'.join((
            'FES_SD_PACKAGE_PROBE board=de25-nano EL=1 counter=400000000Hz writes=disabled activation=unavailable',
            f'FES_NANO_PACKAGE RETAINED {identity} payload_bytes=12 build_id={"e"*32} owner={OWNER:016x} generation=3',
            f'FES_NANO_PACKAGE CONTAINED {identity} owner={OWNER:016x} generation=4',
            f'FES_NANO_PACKAGE RESUMED {identity} owner={OWNER:016x} generation=6',
            f'FES_NANO_PACKAGE PASS read-only {identity} payload_bytes=12 owner={OWNER:016x} generation=7',''))

    def decode(self,raw):return trace.decode_native_boot(raw,BUILD,SEED,package=self.expected)

    def test_retained_package_cycle_is_observation_without_activation_acceptance(self):
        report=self.decode(self.capture())
        self.assertTrue(report['complete'],report)
        self.assertFalse(report['core_load_acceptance'])
        self.assertEqual(report['last_checkpoint'],'package_pass')
        prefix=self.capture().split('SOCFPGA_AGILEX5 # ')[0]+'SOCFPGA_AGILEX5 # '
        self.assertTrue(self.decode(prefix)['loader_ready'])
        self.assertFalse(self.decode(prefix)['complete'])

    def test_fragments_cannot_complete_or_grant_storage_authority(self):
        raw=self.capture().encode()
        for end in range(len(raw)):
            report=self.decode(raw[:end])
            self.assertFalse(report['complete'],end)
            self.assertEqual(report['errors'],[],(end,report))
        self.assertFalse(trace.decode_native_boot(raw,BUILD,SEED)['complete'])
        self.assertFalse(trace.decode_native_boot(raw,BUILD,SEED,storage=NativeSDTraceTests.expected)['complete'])

    def test_identity_owner_generation_disabled_setup_and_failure_reject(self):
        raw=self.capture()
        for old,new in (('package_id='+'c'*64,'package_id='+'a'*64),
                        ('payload_sha256='+'d'*64,'payload_sha256='+'0'*64),
                        ('payload_bytes=12','payload_bytes=13'),('build_id='+'e'*32,'build_id='+'f'*32),
                        ('setup=1 ready=1','setup=0 ready=0'),('generation=3','generation=2'),
                        ('generation=4','generation=3'),('generation=6','generation=5'),
                        ('generation=7','generation=6'),('activation=unavailable','activation=available'),
                        (f'owner={OWNER:016x} generation',f'owner={OWNER+2:016x} generation')):
            with self.subTest(new=new):
                report=self.decode(raw.replace(old,new))
                self.assertFalse(report['complete']);self.assertTrue(report['errors'])
        for line in ('panic: package changed','FES_NANO_PACKAGE FAIL','FES_NANO_PACKAGE PASS'):
            with self.subTest(line=line):
                self.assertTrue(self.decode(raw+line+'\r\n')['errors'])

    def test_missing_duplicate_and_reordered_stages_reject(self):
        raw=self.capture();lines=raw.splitlines(keepends=True)
        for row in self.decode(raw)['records']:
            i=row['line']-1
            with self.subTest(stage=row['stage']):
                self.assertFalse(self.decode(''.join(lines[:i]+lines[i+1:]))['complete'])
                self.assertFalse(self.decode(''.join(lines[:i]+[lines[i]]+lines[i:]))['complete'])
        a=next(i for i,line in enumerate(lines) if 'FES_NANO_PACKAGE CONTAINED' in line)
        b=next(i for i,line in enumerate(lines) if 'FES_NANO_PACKAGE RESUMED' in line)
        lines[a],lines[b]=lines[b],lines[a]
        self.assertFalse(self.decode(''.join(lines))['complete'])

    def test_expected_package_must_be_selected_independently(self):
        invalid=[{},dict(self.expected,extra=True)]
        for key,value in (('package_id','A'*64),('package_id',None),('payload_sha256','d'*63),
                          ('payload_bytes',True),('payload_bytes',0),('payload_bytes',(8<<20)+1),
                          ('build_id','e'*16)):
            invalid.append(dict(self.expected,**{key:value}))
        for value in invalid:
            with self.subTest(value=value),self.assertRaises(ValueError):
                trace.decode_native_boot('',BUILD,SEED,package=value)
        with self.assertRaises(ValueError):
            trace.decode_native_boot('',BUILD,SEED,package=self.expected,storage=NativeSDTraceTests.expected)

    def test_retained_physical_package_trace_keeps_exact_identity_and_containment(self):
        import hashlib
        expected=json.loads((ROOT/'tests/fixtures/nano/packages-native-complete.json').read_text())
        raw=(ROOT/expected['console_path']).read_bytes()
        self.assertEqual(hashlib.sha256(raw).hexdigest(),expected['console_sha256'])
        report=trace.decode_native_boot(raw,expected['build_id'],expected['owner_seed'],package=expected['package'])
        self.assertTrue(report['complete'],report)
        self.assertEqual(report['last_checkpoint'],'package_pass')
        self.assertFalse(report['core_load_acceptance'])


if __name__ == '__main__':
    unittest.main()
