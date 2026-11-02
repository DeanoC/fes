"""Host fixtures for one coherent SPL/EL3/EL1 probe, never board evidence."""
import importlib.util
import json
import re
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('nano_native_trace', ROOT / 'platform/embedded/nano/native_trace.py')
trace = importlib.util.module_from_spec(spec)
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
try:spec.loader.exec_module(trace)
finally:sys.path.pop(0)
BUILD = '0123456789abcdef'
SEED = 0x13579bdf00000001
EPOCH = 0x101
OWNER = (SEED ^ EPOCH) | 1


def atf_progress():
    return ('FES_NANO_SPL BARRIER phase=before\r\n',
            'FES_NANO_SPL BARRIER phase=after\r\n',
            'FES_NANO_SPL ATF_STEP phase=entry\r\n',
            'FES_NANO_SPL ATF_STEP phase=hash-before\r\n',
            'FES_NANO_SPL ATF_STEP phase=hash-after\r\n')


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
        'NOTICE:  FES_NANO_EL3_CAPTURE phase=before pointer=21768 cookie=464e433200000002',
        'NOTICE:  FES_NANO_EL3_CAPTURE phase=after pointer=21768 cookie=464e433200000002',
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

    def test_loaded_bl31_observation_uses_independent_descriptor_and_preserves_partial_progress(self):
        sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
        try:from prepare_uboot import atf_descriptor,checked_atf_descriptor
        finally:sys.path.pop(0)
        expected=atf_descriptor('1'*64,90173)
        marker=('FES_NANO_SPL ATF target=80000000 bl32=0 bl33=80200000 fdt=0 params=bff00008 '
                'cold=21768 cookie=464e433200000002 el=3 mpidr=81000000 sctlr=1000 scr=501 daif=3c0 '
                'bytes=90173 sha256='+'1'*64+'\r\n')
        before='NOTICE:  FES_NANO_EL3_CAPTURE phase=before'
        raw=fixture().replace(before,''.join(atf_progress())+marker+before)
        observed=trace.decode_native_boot(raw,BUILD,SEED,atf=expected)
        self.assertTrue(observed['complete'],observed)
        self.assertFalse(observed['core_load_acceptance'])
        self.assertEqual(observed['atf_observation']['sctlr'],0x1000)
        prefix=raw.split(marker)[0]+marker
        partial=trace.decode_native_boot(prefix,BUILD,SEED,atf=expected)
        self.assertEqual(partial['last_checkpoint'],'spl_atf')
        self.assertEqual(partial['errors'],[])
        self.assertFalse(partial['loader_ready']);self.assertFalse(partial['complete'])
        truncated=trace.decode_native_boot(prefix.rstrip('\r\n'),BUILD,SEED,atf=expected)
        self.assertEqual(truncated['last_checkpoint'],'spl_atf_hash_after')
        self.assertFalse(trace.decode_native_boot(raw,BUILD,SEED)['complete'])
        self.assertFalse(trace.decode_native_boot(fixture(),BUILD,SEED,atf=expected)['complete'])
        self.assertFalse(trace.decode_native_boot(raw.replace(marker,marker+marker),BUILD,SEED,atf=expected)['complete'])
        for old,new in (('target=80000000','target=80000004'),('bl32=0','bl32=1'),
                        ('bl33=80200000','bl33=180200000'),('fdt=0','fdt=1'),
                        ('params=bff00008','params=bfeffff8'),('params=bff00008','params=bff00009'),
                        ('params=bff00008','params=bfffffc8'),('cold=21768','cold=21769'),
                        ('cold=21768','cold=40000'),('cookie=464e433200000002','cookie=464e433200000003'),
                        ('el=3','el=2'),('mpidr=81000000','mpidr=81000200'),
                        ('sctlr=1000','sctlr=1001'),('sctlr=1000','sctlr=1004'),
                        ('daif=3c0','daif=3c1'),('bytes=90173','bytes=90174'),
                        ('sha256='+'1'*64,'sha256='+'2'*64)):
            with self.subTest(old=old,new=new):
                self.assertFalse(trace.decode_native_boot(raw.replace(old,new),BUILD,SEED,atf=expected)['loader_ready'])
        changed=raw.replace('pointer=21768','pointer=21770')
        self.assertIn('secure capture differs from final SPL arguments',
                      trace.decode_native_boot(changed,BUILD,SEED,atf=expected)['errors'])
        for key,value in expected.items():
            if type(value) is int:
                with self.subTest(key=key),self.assertRaises(ValueError):
                    checked_atf_descriptor(dict(expected,**{key:bool(value)}))
        for bad in (None,{},dict(expected,bytes=0),dict(expected,sha256='0'*64),
                    dict(expected,target=0x80000004),dict(expected,extra=1),
                    dict(expected,format=1,scope='loaded-bl31-handoff-v1')):
            with self.subTest(bad=bad),self.assertRaises(ValueError):checked_atf_descriptor(bad)

    def test_barrier_and_guard_progress_requires_every_ordered_current_checkpoint(self):
        sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
        try:from prepare_uboot import atf_descriptor
        finally:sys.path.pop(0)
        expected=atf_descriptor('1'*64,90173)
        self.assertEqual((expected['format'],expected['scope']),(2,'loaded-bl31-handoff-v2'))
        anchor='NOTICE:  FES_NANO_EL3_CAPTURE phase=before'
        marker=('FES_NANO_SPL ATF target=80000000 bl32=0 bl33=80200000 fdt=0 params=bff00008 '
                'cold=21768 cookie=464e433200000002 el=3 mpidr=81000000 sctlr=1000 scr=501 daif=3c0 '
                'bytes=90173 sha256='+'1'*64+'\r\n')
        progress=atf_progress()
        raw=fixture().replace(anchor,''.join(progress)+marker+anchor)
        decode=lambda data:trace.decode_native_boot(data,BUILD,SEED,atf=expected)
        self.assertTrue(decode(raw)['complete'])
        names=('spl_barrier_before','spl_barrier_after','spl_atf_entry','spl_atf_hash_before','spl_atf_hash_after')
        for line,name in zip(progress,names):
            prefix=raw[:raw.index(line)]
            for end in range(len(line)):
                with self.subTest(stage=name,fragment=end):
                    result=decode(prefix+line[:end])
                    self.assertEqual(result['errors'],[])
                    self.assertFalse(result['complete']);self.assertFalse(result['loader_ready'])
                    self.assertFalse(result['core_load_acceptance'])
                    self.assertNotEqual(result['last_checkpoint'],name)
            self.assertEqual(decode(prefix+line)['last_checkpoint'],name)
            for changed in (raw.replace(line,''),raw.replace(line,line+line),line+raw,
                            raw.replace(line,line.replace('phase=','phase=unknown-'))):
                result=decode(changed)
                self.assertTrue(result['errors']);self.assertFalse(result['loader_ready'])
                self.assertFalse(result['complete']);self.assertFalse(result['core_load_acceptance'])
        for index in range(len(progress)-1):
            a,b=progress[index:index+2]
            self.assertFalse(decode(raw.replace(a+b,b+a))['loader_ready'])
        self.assertFalse(decode(raw.replace(''.join(progress),''))['complete'])
        self.assertFalse(trace.decode_native_boot(raw,BUILD,SEED)['complete'])

    def test_early_capture_pairs_locate_progress_without_granting_boot_authority(self):
        raw=fixture();before='NOTICE:  FES_NANO_EL3_CAPTURE phase=before pointer=21768 cookie=464e433200000002\r\n'
        after=before.replace('phase=before','phase=after')
        self.assertIn(before,raw);self.assertIn(after,raw)
        for marker,checkpoint in ((before,'el3_capture_before'),(after,'el3_capture_after')):
            prefix=raw.split(marker)[0]+marker
            observed=self.decode(prefix)
            self.assertEqual(observed['last_checkpoint'],checkpoint)
            self.assertFalse(observed['loader_ready']);self.assertFalse(observed['complete'])
            self.assertFalse(observed['core_load_acceptance'])
        for old,new in (('pointer=21768','pointer=21769'),('pointer=21768','pointer=40000'),
                        ('pointer=21768','pointer=fff'),('cookie=464e433200000002','cookie=464e433200000003')):
            self.assertFalse(self.decode(raw.replace(old,new))['loader_ready'])
        self.assertFalse(self.decode(raw.replace(after,after.replace('pointer=21768','pointer=21770')))['complete'])
        archived=raw.replace(before,'').replace(after,'')
        self.assertFalse(self.decode(archived)['complete'])
        self.assertTrue(trace.decode_native_boot(archived,BUILD,SEED,capture_trace=False)['complete'])
        self.assertFalse(trace.decode_native_boot(raw,BUILD,SEED,capture_trace=False)['complete'])
        for invalid in (None,0,1,'yes'):
            with self.assertRaises(ValueError):trace.decode_native_boot(raw,BUILD,SEED,capture_trace=invalid)

    def test_both_primary_types_and_masked_bridge_readback(self):
        for core in (0, 2):
            for bridges in (0xf, 0x4f):
                with self.subTest(core=core, bridges=bridges):
                    result = self.decode(fixture(core, bridges))
                    self.assertTrue(result['complete'], result)
                    self.assertTrue(result['loader_ready'])
                    self.assertEqual(tuple(row['stage'] for row in result['records']), trace.STAGES)
                    self.assertFalse(result['core_load_acceptance'])
        values={'status':0,'reads':26,'idr0':'8000000f','idr1':'10','aidr':'1','s_idr1':'80000000',
                'cr0':'0','ack':'0','gbpa':'0','agbpa':'0','s_cr0':'0','s_ack':'0','s_init':'0',
                's_gbpa':'0','s_agbpa':'0','identified':1,'settled':1,'bypass_controls_clear':1}
        template='NOTICE:  FES_NANO_SMMU '+' '.join(key+'={'+key+'}' for key in values)+' access=unqualified\r\n'
        for change in ({},{'gbpa':'100000','bypass_controls_clear':0},
                       {'cr0':'1','ack':'1','bypass_controls_clear':0},
                       {'s_idr1':'0','reads':16},
                       {'s_gbpa':'80000000','settled':0,'bypass_controls_clear':0}):
            row=template.format(**dict(values,**change))
            raw=fixture().replace('NOTICE:  FES_NANO_OWNED_BOOT',row+'NOTICE:  FES_NANO_OWNED_BOOT')
            result=trace.decode_native_boot(raw,BUILD,SEED,smmu_observe=True)
            self.assertTrue(result['complete'],result)
            self.assertFalse(result['smmu_observation']['access_qualified'])
            self.assertFalse(result['core_load_acceptance'])
            self.assertFalse(self.decode(raw)['complete'])
        for change in ({'status':-11},{'reads':25},{'gbpa':'100000'}, {'cr0':'1','ack':'1'},
                       {'s_idr1':'0','reads':16,'s_cr0':'1'},{'identified':0},
                       {'s_gbpa':'80000000','settled':1}):
            row=template.format(**dict(values,**change))
            raw=fixture().replace('NOTICE:  FES_NANO_OWNED_BOOT',row+'NOTICE:  FES_NANO_OWNED_BOOT')
            self.assertFalse(trace.decode_native_boot(raw,BUILD,SEED,smmu_observe=True)['complete'])
        row=template.format(**values)
        for raw in (fixture(),fixture()+row,
                    fixture().replace('NOTICE:  FES_NANO_OWNED_BOOT',row+row+'NOTICE:  FES_NANO_OWNED_BOOT')):
            self.assertFalse(trace.decode_native_boot(raw,BUILD,SEED,smmu_observe=True)['complete'])
        for option in (None,0,1,'yes'):
            with self.assertRaises(ValueError):trace.decode_native_boot(fixture(),BUILD,SEED,smmu_observe=option)

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


class NativeFPGAIdentityTraceTests(unittest.TestCase):
    expected=dict(NativeHPSContextTraceTests.expected,control_abi=0x10203040,cpu_base=0x20010400,map_sha256='d'*64)

    def capture(self):
        raw=NativeHPSContextTraceTests().capture()
        for phase,generation,reset in (('open',1,15),('read',2,13),('close',2,13),('closed',3,15)):
            raw+=f'NOTICE:  FES_NANO_IDENTITY phase={phase} owner={OWNER:x} generation={generation} reset={reset:x} base=20010400\r\n'
        return raw+('FES_NATIVE_FPGA_IDENTITY PASS package_id='+'b'*64+' build_id='+'c'*32+
                    ' abi=10203040 base=20010400 map_sha256='+'d'*64+
                    f' owner={OWNER:x} generation=3 data_held=1 programming=unavailable\r\n')

    def decode(self,raw):return trace.decode_native_boot(raw,BUILD,SEED,fpga_identity=self.expected)

    def test_complete_cpu_identity_read_and_reclosed_control_remain_diagnostic(self):
        r=self.decode(self.capture());self.assertTrue(r['complete'],r)
        self.assertEqual(r['last_checkpoint'],'fpga_identity');self.assertFalse(r['core_load_acceptance'])
        self.assertFalse(self.decode(self.capture().rstrip('\r\n'))['complete'])
        self.assertFalse(trace.decode_native_boot(self.capture(),BUILD,SEED,hps_context=NativeHPSContextTraceTests.expected)['complete'])

    def test_each_missing_phase_and_wrong_transition_or_identity_reject(self):
        raw=self.capture();lines=raw.splitlines(keepends=True)
        for row in self.decode(raw)['records']:
            i=row['line']-1
            self.assertFalse(self.decode(''.join(lines[:i]+lines[i+1:]))['complete'],row)
        for old,new in (('reset=d','reset=9'),('phase=read','phase=open'),('generation=2 reset=d','generation=1 reset=d'),
                        ('generation=3 reset=f','generation=2 reset=f'),('base=20010400','base=ff210400'),
                        ('abi=10203040','abi=10203041'),('map_sha256='+'d'*64,'map_sha256='+'e'*64),
                        ('generation=3 data_held=1','generation=1 data_held=1'),('data_held=1','data_held=0'),
                        ('programming=unavailable','programming=available')):
            with self.subTest(new=new):
                r=self.decode(raw.replace(old,new));self.assertFalse(r['complete'],r);self.assertTrue(r['errors'])
        self.assertFalse(self.decode(raw+'NOTICE: FES_NANO_IDENTITY_FAILURE stage=3 error=-5\n')['complete'])

    def test_independent_expected_map_and_profile_are_required(self):
        for data in ({},dict(self.expected,cpu_base=0xff210400),dict(self.expected,map_sha256='0'*64),
                     dict(self.expected,control_abi=True)):
            with self.subTest(data=data),self.assertRaises(ValueError):trace.decode_native_boot(b'',BUILD,SEED,fpga_identity=data)
        with self.assertRaises(ValueError):
            trace.decode_native_boot(b'',BUILD,SEED,fpga_identity=self.expected,hps_context=NativeHPSContextTraceTests.expected)


class NativeFPGAConfigurationTraceTests(unittest.TestCase):
    expected=NativeFPGAIdentityTraceTests.expected

    def capture(self,unready=False):
        status=('FES_NANO_CONFIG_QUERY command=4 words=6\r\n'
                'FES_NANO_CONFIG_STATUS status=0 words=6 state=0 version=10203 pins=80000000 soft='+
                ('1' if unready else '3')+' error_location=0 error_details=0 ready='+str(int(not unready))+'\r\n')
        if unready:
            return NativeHPSContextTraceTests().capture()+status+f'FES_NATIVE_FPGA_UNREADY CLOSED owner={OWNER:x} generation=1 programming=unavailable\r\n'
        raw=NativeFPGAIdentityTraceTests().capture()
        raw=raw.replace('NOTICE:  FES_NANO_IDENTITY phase=open',status+'NOTICE:  FES_NANO_IDENTITY phase=open')
        offsets=(0,4,8,12,16,20,24,28,32,64,68,72,76,56,60)
        values=(0x46455345,0x10000,0x10203040,0xcccccccc,0xcccccccc,0xcccccccc,0xcccccccc,0,0,0,0,0,0,0,0)
        access=''
        for ordinal in range(1,31):
            index=(ordinal-1)%15
            for phase,value in (('before',0),('after',values[index])):
                access+=f'FES_NANO_IDENTITY_ACCESS phase={phase} ordinal={ordinal} offset={offsets[index]:x} value={value:x}\r\n'
        return raw.replace('NOTICE:  FES_NANO_IDENTITY phase=close',access+'NOTICE:  FES_NANO_IDENTITY phase=close',1)

    def decode(self,raw,unready=False):
        return trace.decode_native_boot(raw,BUILD,SEED,fpga_identity=self.expected,
                                       fpga_configuration=True,fpga_unready=unready)

    def test_complete_configuration_and_each_register_return_remain_diagnostic(self):
        r=self.decode(self.capture());self.assertTrue(r['complete'],r)
        self.assertEqual(sum(row['stage'].startswith('identity_access_') for row in r['records']),60)
        self.assertFalse(r['core_load_acceptance'])
        self.assertFalse(trace.decode_native_boot(self.capture(),BUILD,SEED,fpga_identity=self.expected)['complete'])
        self.assertFalse(self.decode(NativeFPGAIdentityTraceTests().capture())['complete'])

    def test_unready_requires_the_complete_sdm_response_and_original_closed_owner(self):
        raw=self.capture(True);r=self.decode(raw,True)
        self.assertTrue(r['complete'],r);self.assertEqual(r['last_checkpoint'],'fpga_unready')
        self.assertFalse(any(row['stage'].startswith('identity_') for row in r['records']))
        self.assertFalse(r['core_load_acceptance'])
        for old,new in (('status=0 words=6','status=4 words=6'),('words=6 state=','words=5 state='),
                        ('ready=0','ready=1'),('soft=1','soft=3'),('generation=1 programming=','generation=3 programming='),
                        (f'CLOSED owner={OWNER:x}',f'CLOSED owner={OWNER+2:x}'),('error_details=0','error_details=100000000')):
            with self.subTest(new=new):self.assertFalse(self.decode(raw.replace(old,new),True)['complete'])
        for line in ('FES_NANO_CONFIG_QUERY','FES_NANO_CONFIG_STATUS','FES_NATIVE_FPGA_UNREADY'):
            partial=''.join(row for row in raw.splitlines(keepends=True) if line not in row)
            self.assertFalse(self.decode(partial,True)['complete'])
        self.assertFalse(self.decode(raw)['complete'])
        self.assertFalse(self.decode(raw.rstrip('\r\n'),True)['complete'])
        self.assertFalse(self.decode(raw+'panic: late failure\r\n',True)['complete'])

    def test_missing_or_changed_accesses_and_unsupported_profiles_reject(self):
        raw=self.capture();lines=raw.splitlines(keepends=True)
        for row in self.decode(raw)['records']:
            if row['stage'].startswith(('fpga_','identity_access_')):
                index=row['line']-1
                self.assertFalse(self.decode(''.join(lines[:index]+lines[index+1:]))['complete'],row)
        for old,new in (('ordinal=1 offset=0','ordinal=2 offset=0'),('offset=0 value=0','offset=4 value=0'),
                        ('value=46455345','value=ffffffff'),('soft=3','soft=1'),('ready=1','ready=0')):
            with self.subTest(new=new):self.assertFalse(self.decode(raw.replace(old,new))['complete'])
        for extra in ({'fpga_configuration':True},{'fpga_unready':True},{'fpga_configuration':1},
                      {'fpga_identity':self.expected,'fpga_unready':True}):
            with self.subTest(extra=extra),self.assertRaises(ValueError):trace.decode_native_boot(b'',BUILD,SEED,**extra)

    def test_status_only_observation_never_requires_or_accepts_a_control_open(self):
        for ready in (0,1):
            raw=self.capture(True).replace('FES_NATIVE_FPGA_UNREADY CLOSED',f'FES_NATIVE_FPGA_STATUS CLOSED ready={ready}')
            if ready:raw=raw.replace('soft=1','soft=3').replace('ready=0','ready=1')
            def decode(data):return trace.decode_native_boot(data,BUILD,SEED,fpga_identity=self.expected,
                                                           fpga_configuration=True,fpga_status_only=True)
            r=decode(raw);self.assertTrue(r['complete'],r)
            self.assertEqual(r['last_checkpoint'],'fpga_status_closed');self.assertFalse(r['core_load_acceptance'])
            self.assertFalse(any(row['stage'].startswith('identity_') for row in r['records']))
            for old,new in (('generation=1 programming=','generation=3 programming='),
                            (f'CLOSED ready={ready}',f'CLOSED ready={1-ready}')):
                self.assertFalse(decode(raw.replace(old,new))['complete'])
            extra=f'NOTICE:  FES_NANO_IDENTITY phase=open owner={OWNER:x} generation=1 reset=f base=20010400\r\n'
            self.assertFalse(decode(raw+extra)['complete'])
            self.assertFalse(decode(raw.rstrip('\r\n'))['complete'])
        for extra in ({'fpga_status_only':True},{'fpga_configuration':True,'fpga_status_only':True},
                      {'fpga_identity':self.expected,'fpga_configuration':True,'fpga_status_only':True,'fpga_unready':True}):
            with self.assertRaises(ValueError):trace.decode_native_boot(b'',BUILD,SEED,**extra)


class NativeContainedConfigurationTraceTests(unittest.TestCase):
    identity=NativeFPGAIdentityTraceTests.expected
    configuration={'format':1,'scope':'sealed-cold-configuration-v1','package_id':identity['package_id'],
                   'build_id':identity['build_id'],'core_sha256':'e'*64,'core_sha384':'f'*96,
                   'core_size':65664,'base':0x94800000,'size':8<<20,'dram_base':0x80000000,
                   'chunk_bytes':65536,'chunk_sha384':['1'*96,'2'*96]}

    def capture(self):
        raw=NativeFPGAConfigurationTraceTests().capture().replace('programming=unavailable','runtime_programming=unavailable')
        def row(phase,completed,pending):
            return f'NOTICE:  FES_NANO_CORE_TRANSFER phase={phase} owner={OWNER:x} generation=1 completed={completed} pending={pending} total=65664 base=94800000\r\n'
        transfer=(row('arm',0,0)+row('start',0,0)+row('write',0,65536)+row('poll',0,65536)+
                  row('poll',0,65536)+row('write',65536,128)+row('poll',65536,128)+
                  row('status',65664,0)+row('status',65664,0)+row('complete',65664,0))
        transfer+=f'FES_NATIVE_FPGA_PROGRAMMED PASS package_id={self.identity["package_id"]} core_sha256={"e"*64} bytes=65664 owner={OWNER:x} generation=1 data_held=1 activation=unavailable\r\n'
        return raw.replace('FES_NANO_CONFIG_QUERY',transfer+'FES_NANO_CONFIG_QUERY',1)

    def decode(self,raw):
        return trace.decode_native_configuration(raw,BUILD,SEED,fpga_identity=self.identity,configuration=self.configuration)

    def stored_capture(self):
        raw=self.capture()
        provider=f'NOTICE:  FES_NANO_OWNED_BOOT provider=1 result=0 reset=0000004f\r\n'
        self.assertIn(provider,raw)
        raw=raw.replace(provider,provider+'NOTICE:  FES_NANO_SD_PLATFORM setup=1 ready=1\r\n')
        lines=raw.splitlines(keepends=True)
        for i,line in enumerate(lines):
            if 'FES_NATIVE_HPS_CONTEXT' in line or 'FES_NANO_CORE_TRANSFER' in line or 'FES_NATIVE_FPGA_PROGRAMMED' in line:
                lines[i]=line.replace('generation=1','generation=4')
            elif 'FES_NANO_IDENTITY phase=' in line:
                for old,new in ((3,6),(2,5),(1,4)):
                    if f'generation={old} ' in line:lines[i]=line.replace(f'generation={old} ',f'generation={new} ');break
            elif 'FES_NATIVE_FPGA_IDENTITY' in line:
                lines[i]=line.replace('generation=3','generation=6')
        raw=''.join(lines)
        marker='FES_NATIVE_HPS_CONTEXT'
        contained=f'FES_NANO_LOAD_PACKAGE CONTAINED package_id={self.identity["package_id"]} owner={OWNER:x} generation=4 source=sd storage_active=0\r\n'
        return raw.replace(marker,contained+marker,1)

    def window_capture(self):
        raw=self.capture();start=raw.index('NOTICE:  FES_NANO_CORE_TRANSFER phase=arm')
        end=raw.index('FES_NATIVE_FPGA_PROGRAMMED')
        def row(phase,completed,pending):
            return f'NOTICE:  FES_NANO_CORE_TRANSFER phase={phase} owner={OWNER:x} generation=1 completed={completed} pending={pending} total=65664 base=94800000\r\n'
        rows=(row('arm',0,0)+row('start',0,0)+row('write',0,65536)+row('write',0,65664)+
              row('poll',0,65664)+row('status',65664,0)+row('complete',65664,0))
        return raw[:start]+rows+raw[end:]

    def test_two_buffer_batch_capture(self):
        report=self.decode(self.window_capture())
        self.assertTrue(report['complete'],report)
        self.assertEqual(report['configuration_transfer']['completed_bytes'],65664)

    def test_four_buffer_window_requires_bounded_real_completion_progress(self):
        raw=self.capture();first=raw.index('NOTICE:  FES_NANO_CORE_TRANSFER phase=arm');end=raw.index('FES_NATIVE_FPGA_PROGRAMMED')
        config=dict(self.configuration,core_size=262272,chunk_sha384=['1'*96]*5)
        def row(phase,completed,pending):
            return f'NOTICE:  FES_NANO_CORE_TRANSFER phase={phase} owner={OWNER:x} generation=1 completed={completed} pending={pending} total=262272 base=94800000\r\n'
        def decode(data):return trace.decode_native_configuration(data,BUILD,SEED,fpga_identity=self.identity,configuration=config)
        rows=(row('arm',0,0)+row('start',0,0)+row('write',0,65536)+row('write',0,131072)+
              row('write',0,196608)+row('write',0,262144)+row('poll',0,262144)+
              row('write',196608,65664)+row('poll',196608,65664)+row('status',262272,0)+row('complete',262272,0))
        full=raw[:first]+rows+raw[end:].replace('bytes=65664','bytes=262272')
        report=decode(full);self.assertTrue(report['complete'],report)
        self.assertFalse(report['core_load_acceptance'])
        self.assertEqual(report['configuration_transfer']['completed_bytes'],262272)
        for old,new in ((row('write',0,262144),row('write',0,262144)+row('write',0,262272)),
                        (row('write',196608,65664),row('write',262144,128)),
                        (row('write',0,131072),row('write',0,131068)),
                        (row('poll',0,262144),''),
                        (row('poll',0,262144),row('poll',65536,196608))):
            with self.subTest(new=new):self.assertFalse(decode(full.replace(old,new))['complete'])

    def test_complete_transfer_then_live_identity_remains_contained_diagnostic(self):
        raw=self.capture();result=self.decode(raw)
        self.assertTrue(result['complete'],result)
        self.assertTrue(result['configuration_transfer']['complete'])
        self.assertEqual(result['configuration_transfer']['completed_bytes'],65664)
        self.assertFalse(result['core_load_acceptance'])
        stored=self.stored_capture()
        decode=lambda raw:trace.decode_native_configuration(raw,BUILD,SEED,fpga_identity=self.identity,
                                                           configuration=self.configuration,from_storage=True)
        stored_result=decode(stored)
        self.assertTrue(stored_result['complete'],stored_result)
        self.assertFalse(stored_result['core_load_acceptance'])
        self.assertFalse(self.decode(stored)['complete'])
        self.assertFalse(decode(self.capture())['complete'])
        for old,new in (('source=sd storage_active=0','source=sd storage_active=1'),
                        ('generation=4 source=sd','generation=3 source=sd'),
                        ('generation=4 runtime_programming=','generation=1 runtime_programming='),
                        ('generation=6 data_held=','generation=3 data_held='),
                        ('FES_NANO_LOAD_PACKAGE CONTAINED package_id='+self.identity['package_id'],
                         'FES_NANO_LOAD_PACKAGE CONTAINED package_id='+'f'*64),
                        ('FES_NANO_SD_PLATFORM setup=1 ready=1','FES_NANO_SD_PLATFORM setup=0 ready=0')):
            with self.subTest(stored_mutation=new):self.assertFalse(decode(stored.replace(old,new))['complete'])
        for line in stored.splitlines(keepends=True):
            if 'FES_NANO_LOAD_PACKAGE' in line or 'FES_NANO_SD_PLATFORM' in line:
                self.assertFalse(decode(stored.replace(line,''))['complete'])
                self.assertFalse(decode(stored.replace(line,line+line))['complete'])
                self.assertFalse(decode(stored.replace(line,'')+line)['complete'])
                self.assertFalse(decode(stored.split(line)[0]+line.rstrip('\r\n'))['complete'])
        self.assertEqual(result['last_checkpoint'],'fpga_identity')
        self.assertEqual(sum(row['stage'].startswith('configuration_') for row in result['records']),11)
        self.assertFalse(trace.decode_native_boot(raw,BUILD,SEED,fpga_identity=self.identity,fpga_configuration=True)['complete'])
        ordinary=NativeFPGAConfigurationTraceTests().capture()
        self.assertFalse(self.decode(ordinary)['complete'])
        self.check_reader_trace()
        self.check_ddr_trace()

    def check_actual_ddr_platform_trace(self,actual,*,atf=None):
        """Actual strong C provider/MMIO trace joined to a host boot fixture.

        The ordinary boot and Go final markers are synthetic. This verifies
        decoder integration, never physical memory or raw bridge hardware.
        """
        build='778899aabbccddee0011223344556677'
        identity=dict(self.identity,control_abi=0x10203041,build_id=build)
        memory=json.loads((ROOT/'platform/embedded/tamago/nativefabric/testdata/hps-reader-memory.json').read_text())
        memory.update(package_id=identity['package_id'],build_id=build)
        configuration=dict(self.configuration,build_id=build)
        contract={'format':1,'scope':'sealed-cold-ddr-read-v1','package_id':identity['package_id'],'build_id':build,
                  'reader_memory_sha256':trace.context_digest(memory),'control_map_sha256':identity['map_sha256'],
                  'adapter_source_sha256':'a'*64,'adapter_generated_sha256':'b'*64,
                  'arcache':2,'arprot':3,'aruser':224,'read_bytes':32,'open_mask':8,'close_mask':10}
        prefix=self.stored_capture().split('FES_NANO_CONFIG_QUERY',1)[0].replace('c'*32,build)
        pinned=(f'FES_NATIVE_FPGA_READER_MEMORY PINNED cpu_base=95000000 bus_base=95000000 bytes=8192 arena_bytes=2097152 '
                f'map_sha256={trace.context_digest(memory)} pattern_sha256={memory["pattern_sha256"]} '
                f'owner={OWNER:x} generation=4 fpga_access=unqualified\n')
        prefix=prefix.replace('FES_NATIVE_HPS_CONTEXT',pinned+'FES_NATIVE_HPS_CONTEXT',1)
        actual=actual[actual.index('FES_NANO_CONFIG_QUERY'):].replace('owner=17 ',f'owner={OWNER:x} ')
        raw=prefix+actual
        raw+=(f'FES_NATIVE_FPGA_IDENTITY PASS package_id={identity["package_id"]} build_id={build} abi=10203041 '
              f'base=20010400 map_sha256={identity["map_sha256"]} owner={OWNER:x} generation=7 data_held=1 runtime_programming=unavailable\n'
              f'FES_NATIVE_FPGA_DDR_READER PASS owner={OWNER:x} generation=7 reader_generation=1 cookie=7 bytes=32 data_closed=1 runtime_programming=unavailable\n')
        sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
        try:
            from prepare_uboot import atf_descriptor,checked_atf_descriptor
            atf=checked_atf_descriptor(atf if atf is not None else atf_descriptor('e'*64,90173))
            anchor='NOTICE:  FES_NANO_EL3_CAPTURE phase=before'
            marker=('FES_NANO_SPL ATF target=80000000 bl32=0 bl33=80200000 fdt=0 params=bff00008 '
                    'cold=21768 cookie=464e433200000002 el=3 mpidr=81000000 sctlr=1000 scr=501 daif=3c0 '
                    f'bytes={atf["bytes"]} sha256={atf["sha256"]}\n')
            raw=raw.replace(anchor,''.join(atf_progress())+marker+anchor,1)
            def decode(data):return trace.decode_native_configuration(data,BUILD,SEED,fpga_identity=identity,
                configuration=configuration,from_storage=True,reader_memory=memory,ddr_read=contract,atf=atf)
            result=decode(raw)
            self.assertTrue(result['complete'],result['errors'])
            self.assertTrue(result['ddr_read']['complete'])
            self.assertFalse(result['core_load_acceptance'])
            self.assertFalse(result['ddr_read']['data_remained_contained'])
            self.assertEqual(result['ddr_read']['writes'],9)
            self.assertFalse(trace.decode_native_configuration(raw,BUILD,SEED,fpga_identity=identity,
                configuration=configuration,from_storage=True,reader_memory=memory,atf=atf)['complete'])
            for old,new in (('pattern_words=2048','pattern_words=2047'),('hardware_drained=8','hardware_drained=0'),
                            ('producers_quiescent=1','producers_quiescent=0'),('fabric_acknowledged=0','fabric_acknowledged=1'),
                            ('offset=84 value=95000000','offset=84 value=95001000'),('offset=48 value=7','offset=48 value=6'),
                            ('bytes=32 checksum=','bytes=16 checksum='),('complete=1 data=active','complete=1 data=held'),
                            ('phase=closed owner='+f'{OWNER:x} generation=7','phase=closed owner='+f'{OWNER:x} generation=6'),
                            ('bypass_controls_clear=1','bypass_controls_clear=0')):
                self.assertIn(old,raw)
                with self.subTest(active_change=new):self.assertFalse(decode(raw.replace(old,new))['complete'])
            lines=raw.splitlines(keepends=True)
            important=[i for i,line in enumerate(lines) if 'FES_NANO_DDR_CONTROL' in line or 'FES_NANO_DDR_GATE' in line or
                'FES_NANO_DDR_READ phase=' in line or 'FES_NANO_DDR_CLOSURE' in line or 'FES_NATIVE_FPGA_DDR_READER' in line or
                'ordinal=30 phase=' in line or 'phase=after ordinal=30 ' in line or
                'FES_NANO_SPL BARRIER' in line or 'FES_NANO_SPL ATF_STEP' in line or 'FES_NANO_SPL ATF target=' in line]
            for i in important:
                with self.subTest(active_line=i):
                    self.assertFalse(decode(''.join(lines[:i]+lines[i+1:]))['complete'])
                    self.assertFalse(decode(''.join(lines[:i]+[lines[i],lines[i]]+lines[i+1:]))['complete'])
                    self.assertFalse(decode(''.join(lines[:i]))['complete'])
            self.assertFalse(decode(raw.rstrip())['complete'])
            self.assertFalse(decode(raw+'partial tail')['complete'])
            self.assertFalse(decode(raw+'panic: late failure\n')['complete'])
            self.assertFalse(decode(raw.replace('reads=0 writes=0 polls=0','reads='+'9'*5000+' writes=0 polls=0'))['complete'])
        finally:sys.path.pop(0)

    def check_ddr_trace(self):
        """The DDR source remains blocked after pinned-memory/idle-stop proof."""
        memory=json.loads((ROOT/'platform/embedded/tamago/nativefabric/testdata/hps-reader-memory.json').read_text())
        memory.update(package_id=self.identity['package_id'],build_id=self.identity['build_id'])
        identity=dict(self.identity,control_abi=0x10203041)
        raw=self.stored_capture().replace('10203040','10203041')
        pinned=(f'FES_NATIVE_FPGA_READER_MEMORY PINNED cpu_base=95000000 bus_base=95000000 bytes=8192 arena_bytes=2097152 '
                f'map_sha256={trace.context_digest(memory)} pattern_sha256={memory["pattern_sha256"]} '
                f'owner={OWNER:x} generation=4 fpga_access=unqualified\r\n')
        smmu=('NOTICE:  FES_NANO_SMMU status=0 reads=26 idr0=080f7e3f idr1=0e739d18 aidr=1 s_idr1=80000018 '
              'cr0=0 ack=0 gbpa=0 agbpa=0 s_cr0=0 s_ack=0 s_init=0 s_gbpa=0 s_agbpa=0 '
              'identified=1 settled=1 bypass_controls_clear=1 access=unqualified\r\n')
        firewall=(f'NOTICE:  FES_NANO_DDR_FIREWALL status=0 owner={OWNER:x} generation=4 reset=f mpfe_reset=0 reads=66 enable=1 base=95000000 bytes=8192 access=unqualified\r\n')
        for i in range(8):
            firewall+=f'NOTICE:  FES_NANO_DDR_REGION index={i} base={"80000000" if not i else "0"} baseext=0 limit={"bfffffff" if not i else "ffff"} limitext=0\r\n'
        firewall+='NOTICE:  FES_NANO_DDR_FIREWALL_END layout_known=1 window_covered=1 access=unqualified\r\n'
        stop=(f'NOTICE:  FES_NANO_MASTER_STOP status=0 owner={OWNER:x} generation=5 reset=d '
              'reader_reads=88 reader_polls=1 disable=0 probe_polls=2 cancel=0 complete=1 data=held\r\n')
        blocked=(f'FES_NATIVE_FPGA_DDR_READER BLOCKED owner={OWNER:x} generation=6 data_held=1 '
                 'translation=unqualified drain=unqualified runtime_programming=unavailable\r\n')
        raw=raw.replace('FES_NATIVE_HPS_CONTEXT',pinned+'FES_NATIVE_HPS_CONTEXT',1)
        raw=raw.replace('NOTICE:  FES_NANO_IDENTITY phase=open',smmu+firewall+'NOTICE:  FES_NANO_IDENTITY phase=open',1)
        raw=raw.replace('NOTICE:  FES_NANO_IDENTITY phase=closed',stop+'NOTICE:  FES_NANO_IDENTITY phase=closed',1)+blocked
        def decode(data,**changes):
            options={'fpga_identity':identity,'configuration':self.configuration,'from_storage':True,'reader_memory':memory}
            options.update(changes)
            return trace.decode_native_configuration(data,BUILD,SEED,**options)
        result=decode(raw)
        self.assertTrue(result['complete'],result)
        self.assertEqual(result['last_checkpoint'],'ddr_blocked')
        self.assertFalse(result['core_load_acceptance'])
        self.assertFalse(result['reader_memory_observation']['fpga_access_qualified'])
        self.assertFalse(result['master_stop_observation']['physical_ddr_drain_qualified'])
        self.assertTrue(result['smmu_observation']['bypass_controls_clear'])
        self.assertFalse(result['smmu_observation']['access_qualified'])
        self.assertTrue(result['ddr_firewall_observation']['window_covered'])
        self.assertFalse(result['ddr_firewall_observation']['access_qualified'])
        # A truthful enabled-translation observation is still a valid contained
        # diagnostic. It must not turn into an access qualification.
        enabled=raw.replace('cr0=0 ack=0','cr0=1 ack=1',1).replace('bypass_controls_clear=1','bypass_controls_clear=0')
        self.assertTrue(decode(enabled)['complete'])
        closed=raw.replace('enable=1 base=','enable=0 base=',1).replace('window_covered=1','window_covered=0')
        self.assertTrue(decode(closed)['complete'])
        self.assertFalse(decode(closed)['ddr_firewall_observation']['window_covered'])
        for row in (pinned,smmu,stop,blocked,*firewall.splitlines(keepends=True)):
            for changed in (raw.replace(row,''),raw.replace(row,row+row),raw.replace(row,'')+row,
                            raw.replace(row,row.replace('=',':',1))):
                # Moving the last record to its existing position is unchanged.
                if changed==raw:continue
                self.assertFalse(decode(changed)['complete'],row)
        for old,new in (('bytes=8192 arena_bytes=','bytes=4096 arena_bytes='),
                        ('bus_base=95000000','bus_base=95001000'),('arena_bytes=2097152','arena_bytes=8192'),
                        ('map_sha256='+trace.context_digest(memory),'map_sha256='+'f'*64),
                        ('pattern_sha256='+memory['pattern_sha256'],'pattern_sha256='+'f'*64),
                        ('generation=4 fpga_access=','generation=3 fpga_access='),
                        ('fpga_access=unqualified','fpga_access=qualified'),('reader_reads=88','reader_reads=44'),
                        ('reader_polls=1','reader_polls=0'),('probe_polls=2','probe_polls=1'),('disable=0','disable=1'),
                        ('cancel=0','cancel=1'),('complete=1 data=held','complete=0 data=held'),
                        ('generation=5 reset=d reader_reads=','generation=4 reset=d reader_reads='),
                        ('reset=d reader_reads=','reset=5 reader_reads='),('status=0 owner=','status=-7 owner='),
                        ('generation=6 data_held=1 translation=','generation=5 data_held=1 translation='),
                        ('translation=unqualified','translation=qualified')):
            with self.subTest(ddr_change=new):self.assertFalse(decode(raw.replace(old,new))['complete'])
        for old,new in (('reads=66 enable=','reads=33 enable='),('enable=1 base=','enable=0 base='),
                        ('index=0 base=80000000','index=0 base=96000000'),('index=0 base=80000000','index=0 base=80000001'),
                        ('index=1 base=0 baseext=0','index=1 base=0 baseext=100'),
                        ('window_covered=1','window_covered=0'),('layout_known=1','layout_known=0'),
                        ('mpfe_reset=0','mpfe_reset=1'),
                        ('generation=4 reset=f mpfe_reset=','generation=5 reset=f mpfe_reset=')):
            with self.subTest(firewall_change=new):self.assertFalse(decode(raw.replace(old,new))['complete'])
        self.assertFalse(decode(raw.rstrip('\r\n'))['complete'])
        self.assertFalse(decode(raw+'panic: late failure\r\n')['complete'])
        for expected in ({},dict(memory,format=True),dict(memory,cpu_attributes='cached'),
                         dict(memory,fpga_addressing='physical-identity-qualified'),dict(memory,bytes=4096),
                         dict(memory,pattern_sha256='f'*64)):
            with self.subTest(ddr_expected=expected),self.assertRaises(ValueError):decode(raw,reader_memory=expected)
        for changes in ({'reader_memory':None},{'fpga_identity':self.identity},{'from_storage':False}):
            with self.subTest(ddr_mode=changes),self.assertRaises(ValueError):decode(raw,**changes)

    def reader_capture(self,stored=True,waits=0):
        raw=self.stored_capture() if stored else self.capture()
        source_generation=4 if stored else 1
        lines=[];ordinal=31
        def read(offset,value):
            nonlocal ordinal
            for phase,v in (('before',0),('after',value)):
                lines.append(f'NOTICE:  FES_NANO_IDENTITY_ACCESS phase={phase} ordinal={ordinal} offset={offset:x} value={v:x}\r\n')
            ordinal+=1
        def write(offset,value):
            for phase in ('before','after'):
                lines.append(f'NOTICE:  FES_NANO_READER_WRITE phase={phase} offset={offset:x} value={value:x}\r\n')
        def state(status,pending,ack):
            for offset,value in zip((0x38,0x3c,0x1c,0x20,0x40,0x44,0x48,0x4c),(status,pending,1,0,int(ack),0,7 if ack else 0,0)):
                read(offset,value)
        def stage(value):
            lines.append(f'NOTICE:  FES_NANO_READER_CYCLE phase={value} reader_generation=1 cookie=7 owner={OWNER:x} generation={source_generation+1} reset=d\r\n')
        write(0x24,1);write(0x28,0);write(0x34,1);state(1,0,False);stage(9)
        write(0x84,0x1000);write(0x88,1);write(0x80,0x0c);state(1,0x10000,False)
        read(0x8c,4);read(0x90,0x1000);stage(10)
        write(0x2c,7);write(0x30,0);write(0x34,2);state(2,0x10000,False)
        read(0x8c,0x24);read(0x90,0x1000);stage(11)
        write(0x80,5);state(2,1,False);write(0x80,7);state(2,1,False)
        read(0x8c,0x10068);stage(12);write(0x80,7)
        for _ in range(waits):state(2,0,False)
        state(4,0,True);stage(13)
        offsets=(0,4,8,12,16,20,24,28,32,64,68,72,76,56,60)
        values=(0x46455345,0x10000,0x10203040,0xcccccccc,0xcccccccc,0xcccccccc,0xcccccccc,1,0,1,0,7,0,4,0)
        for _ in range(2):
            for offset,value in zip(offsets,values):read(offset,value)
        raw=raw.replace('NOTICE:  FES_NANO_IDENTITY phase=close',''.join(lines)+'NOTICE:  FES_NANO_IDENTITY phase=close',1)
        return raw+f'FES_NATIVE_FPGA_READER PASS owner={OWNER:x} generation={source_generation+2} reader_generation=1 cookie=7 data_held=1 runtime_programming=unavailable\r\n'

    def check_reader_trace(self):
        def decode(raw,stored=True):
            return trace.decode_native_reader_configuration(raw,BUILD,SEED,fpga_identity=self.identity,
                                                           configuration=self.configuration,from_storage=stored)
        for stored in (False,True):
            raw=self.reader_capture(stored);result=decode(raw,stored)
            self.assertTrue(result['complete'],result)
            self.assertTrue(result['configuration_transfer']['complete'])
            self.assertEqual(result['last_checkpoint'],'fpga_reader')
            self.assertEqual(result['reader_cycle']['reads'],83)
            self.assertEqual(result['reader_cycle']['writes'],12)
            self.assertFalse(result['reader_cycle']['ddr_drain'])
            self.assertFalse(result['core_load_acceptance'])
        raw=self.reader_capture();result=decode(raw);lines=raw.splitlines(keepends=True)
        self.assertFalse(decode(self.stored_capture())['complete'])
        self.assertFalse(trace.decode_native_configuration(raw,BUILD,SEED,fpga_identity=self.identity,
                                                          configuration=self.configuration,from_storage=True)['complete'])
        for row in result['records']:
            if not row['stage'].startswith(('reader_','fpga_reader')):continue
            i=row['line']-1
            with self.subTest(reader_record=row['stage']):
                self.assertFalse(decode(''.join(lines[:i]+lines[i+1:]))['complete'])
                self.assertFalse(decode(''.join(lines[:i]+[lines[i]]+lines[i:]))['complete'])
                prefix=decode(''.join(lines[:i])+lines[i].rstrip('\r\n'))
                self.assertFalse(prefix['complete']);self.assertFalse(prefix['errors'],prefix)
        mutations=(('phase=9 reader_generation=1','phase=10 reader_generation=1'),
                   ('reader_generation=1 cookie=7','reader_generation=2 cookie=7'),
                   (f'cookie=7 owner={OWNER:x}',f'cookie=7 owner={OWNER+2:x}'),
                   ('ordinal=31 offset=38 value=1','ordinal=31 offset=38 value=4'),
                   ('ordinal=34 offset=20 value=0','ordinal=34 offset=20 value=1'),
                   ('phase=after offset=24 value=1','phase=after offset=24 value=2'),
                   ('phase=after offset=80 value=c','phase=after offset=80 value=d'),
                   ('ordinal=47 offset=8c value=4','ordinal=47 offset=8c value=0'),
                   ('generation=5 reset=d','generation=4 reset=d'),
                   ('ordinal=84 offset=0 value=46455345','ordinal=84 offset=0 value=46455344'),
                   ('READER PASS owner='+format(OWNER,'x')+' generation=6','READER PASS owner='+format(OWNER,'x')+' generation=3'),
                   ('reader_generation=1 cookie=7 data_held=1','reader_generation=1 cookie=7 data_held=0'))
        for old,new in mutations:
            self.assertIn(old,raw)
            with self.subTest(reader_mutation=new):self.assertFalse(decode(raw.replace(old,new))['complete'])
        self.assertFalse(decode(raw+'FES_NATIVE_FPGA_READER malformed\n')['complete'])
        self.assertTrue(decode(self.reader_capture(waits=127))['complete'])
        self.assertFalse(decode(self.reader_capture(waits=128))['complete'])

    def test_omissions_duplicates_reordering_and_partial_transfer_never_complete(self):
        raw=self.capture();result=self.decode(raw);lines=raw.splitlines(keepends=True)
        rows=[row for row in result['records'] if row['stage'].startswith('configuration_')]
        for row in rows:
            index=row['line']-1
            with self.subTest(stage=row['stage'],line=index):
                optional=(row['stage'] in ('configuration_poll','configuration_status') and
                          any(other is not row and other['stage']==row['stage'] and other['values']==row['values'] for other in rows))
                self.assertEqual(self.decode(''.join(lines[:index]+lines[index+1:]))['complete'],optional)
                if row['stage'] not in ('configuration_poll','configuration_status'):
                    self.assertFalse(self.decode(''.join(lines[:index]+[lines[index]]+lines[index:]))['complete'])
                self.assertFalse(self.decode(''.join(lines[:index])+lines[index].rstrip('\r\n'))['complete'])
        first=rows[0]['line']-1;last=rows[-1]['line']-1
        moved=list(lines);moved[first],moved[last]=moved[last],moved[first]
        self.assertFalse(self.decode(''.join(moved))['complete'])
        partial=self.decode(raw.split('FES_NANO_CORE_TRANSFER phase=write')[0])
        self.assertFalse(partial['complete']);self.assertFalse(partial['errors'])

    def test_changed_owner_extent_memory_identity_and_readiness_reject(self):
        raw=self.capture()
        mutations=(('phase=arm','phase=FAIL'),('total=65664','total=65660'),('base=94800000','base=94000000'),
                   ('completed=65536 pending=128','completed=65532 pending=128'),
                   ('completed=0 pending=65536','completed=0 pending=128'),
                   ('phase=start','phase=write'),('generation=1 completed=','generation=2 completed='),
                   (f'phase=arm owner={OWNER:x}',f'phase=arm owner={OWNER+2:x}'),
                   ('bytes=65664 owner=','bytes=65660 owner='),('core_sha256='+'e'*64,'core_sha256='+'d'*64),
                   ('soft=3','soft=1'),('ready=1','ready=0'))
        for old,new in mutations:
            with self.subTest(new=new):self.assertFalse(self.decode(raw.replace(old,new))['complete'])
        with self.assertRaises(ValueError):
            trace.decode_native_configuration(b'',BUILD,SEED,fpga_identity=self.identity,configuration=dict(self.configuration,base=0x94000000))
        for chunk in (0,-64,63,0xffff0,0x100000):
            with self.subTest(chunk=chunk), self.assertRaises(ValueError):
                trace.decode_native_configuration(b'',BUILD,SEED,fpga_identity=self.identity,
                                                  configuration=dict(self.configuration,chunk_bytes=chunk))

        # The same graph must observe two large compiled buffers before identity.
        chunk=0xfffc0;total=2027520
        config=dict(self.configuration,core_size=total,chunk_bytes=chunk)
        raw=self.capture();first=raw.index('NOTICE:  FES_NANO_CORE_TRANSFER phase=arm')
        last=raw.index('FES_NATIVE_FPGA_PROGRAMMED')
        def row(phase,completed,pending):
            return f'NOTICE:  FES_NANO_CORE_TRANSFER phase={phase} owner={OWNER:x} generation=1 completed={completed} pending={pending} total={total} base=94800000\r\n'
        rows=(row('arm',0,0)+row('start',0,0)+row('write',0,chunk)+row('write',0,total)+
              row('poll',0,total)+row('poll',chunk,total-chunk)+row('status',total,0)+row('complete',total,0))
        raw=raw[:first]+rows+raw[last:].replace('bytes=65664',f'bytes={total}')
        result=trace.decode_native_configuration(raw,BUILD,SEED,fpga_identity=self.identity,configuration=config)
        self.assertTrue(result['complete'],result)
        self.assertFalse(result['core_load_acceptance'])
        for old,new in ((f'pending={chunk}',f'pending={chunk-64}'),
                        (f'completed={chunk}',f'completed={chunk+64}')):
            self.assertFalse(trace.decode_native_configuration(raw.replace(old,new),BUILD,SEED,
                             fpga_identity=self.identity,configuration=config)['complete'])

    def test_counter_and_digest_costs_are_observations_with_strict_accounting(self):
        lines=self.capture().splitlines(keepends=True);count=0
        for i,line in enumerate(lines):
            if 'FES_NANO_CORE_TRANSFER' in line:
                count+=1
                lines[i]=line.rstrip('\r\n')+f' counter={0x100000+count*0x1000:x} hash_ticks={count*0x100:x}\r\n'
        raw=''.join(lines);result=self.decode(raw)
        self.assertTrue(result['complete'],result)
        self.assertFalse(result['core_load_acceptance'])
        timing=result['configuration_timing']
        self.assertEqual(timing,{'counter_frequency':400000000,'first_counter':0x101000,
                                'last_counter':0x100000+count*0x1000,'hash_ticks':count*0x100,
                                'interval_ticks':(count-1)*0x1000,'interval_hash_ticks':(count-1)*0x100})
        self.assertNotIn('configuration_timing',self.decode(self.capture()))
        mutations=(('counter=102000','counter=100000'),('hash_ticks=200','hash_ticks=ff'),
                   ('hash_ticks=200','hash_ticks=200000'),(' counter=102000 hash_ticks=200',''),
                   (' hash_ticks=200',''),('counter=102000','counter=0'))
        for old,new in mutations:
            with self.subTest(new=new):
                report=self.decode(raw.replace(old,new))
                self.assertFalse(report['complete']);self.assertTrue(report['errors'])
        partial=raw[:raw.index('FES_NANO_CORE_TRANSFER phase=write')]
        report=self.decode(partial)
        self.assertFalse(report['complete']);self.assertFalse(report['errors'])
        self.assertIn('configuration_timing',report)
        self.assertFalse(self.decode(raw+'panic: late FPGA firmware return\r\n')['complete'])

    def test_engine_limits_are_scoped_observations_and_never_hide_failures(self):
        lines=self.capture().splitlines(keepends=True)
        first=next(i for i,line in enumerate(lines) if 'phase=write ' in line)
        engine=f'NOTICE:  FES_NANO_CORE_ENGINE credits=4 block_bytes=65536 next_job=0 owner={OWNER:x} generation=1\r\n'
        raw=''.join(lines[:first]+[engine]+lines[first:])
        report=self.decode(raw)
        self.assertTrue(report['complete'],report)
        observed=[r for r in report['records'] if r['stage']=='configuration_engine']
        self.assertEqual(len(observed),1)
        self.assertEqual(observed[0]['values'],{'credits':4,'block_bytes':65536,'next_job':0,'owner':OWNER,'generation':1})
        for old,new in (('credits=4','credits=0'),('credits=4','credits=2147483648'),
                        ('block_bytes=65536','block_bytes=0'),('next_job=0','next_job=14'),
                        (f'owner={OWNER:x}',f'owner={OWNER+2:x}'),('generation=1','generation=2')):
            with self.subTest(new=new):
                changed=raw.replace(engine,engine.replace(old,new))
                self.assertFalse(self.decode(changed)['complete'])
        self.assertFalse(self.decode(raw.replace(engine,engine+engine))['complete'])
        self.assertFalse(self.decode(engine+self.capture())['complete'])
        failure='NOTICE:  FES_NANO_CORE_TRANSPORT_FAILURE phase=2 status=-17 completed=12 submitted=13 next_job=13 credits=3\r\n'
        self.assertFalse(self.decode(raw+failure)['complete'])
        partial=self.decode(''.join(lines[:first])+engine)
        self.assertFalse(partial['complete']);self.assertFalse(partial['errors'])

    def test_mailbox_observations_do_not_advance_transfer_authority(self):
        lines=self.capture().splitlines(keepends=True)
        start=next(i for i,line in enumerate(lines) if 'phase=start ' in line)
        write=next(i for i,line in enumerate(lines) if 'phase=write ' in line)
        poll=next(i for i,line in enumerate(lines) if 'phase=poll ' in line)
        engine=f'NOTICE:  FES_NANO_CORE_ENGINE credits=4 block_bytes=1048560 next_job=0 owner={OWNER:x} generation=1\r\n'
        def observation(phase,completed,pending):
            return (f'NOTICE:  FES_NANO_CORE_MAILBOX phase={phase} owner={OWNER:x} generation=1 '
                    f'completed={completed} pending={pending} job=1 cin=4 cout=4 rin=0 rout=0 bell=0 status=0 '
                    'state=0 next=0 slot=-1 cmd_used=0 resp_used=0\r\n')
        prepared=observation(1,0,0);queued=observation(2,0,65536);acked=observation(3,65536,0)
        raw=''.join(lines[:start+1]+[engine,prepared]+lines[start+1:write+1]+[queued]+
                    lines[write+1:poll+1]+[acked]+lines[poll+1:])
        report=self.decode(raw);self.assertTrue(report['complete'],report)
        self.assertEqual(sum(r['stage']=='configuration_mailbox' for r in report['records']),3)
        for old,new in (('generation=1','generation=2'),(f'owner={OWNER:x}',f'owner={OWNER+2:x}'),
                        ('job=1','job=14'),('completed=65536','completed=65664'),('pending=0','pending=1'),
                        ('state=0','state=4'),('next=0','next=4'),('slot=-1','slot=32'),
                        ('cmd_used=0','cmd_used=33'),('resp_used=0','resp_used=33')):
            with self.subTest(new=new):
                self.assertFalse(self.decode(raw.replace(acked,acked.replace(old,new)))['complete'])
        self.assertFalse(self.decode(raw.replace(prepared,prepared+prepared))['complete'])
        self.assertFalse(self.decode(raw.replace(engine,''))['complete'])
        self.assertFalse(self.decode(raw.replace(queued,acked))['complete'])
        # Preserve unknown raw register values for diagnosis; do not interpret
        # them as a transfer acknowledgement or as permission to open bridges.
        unusual=self.decode(raw.replace('cin=4','cin=ffffffff').replace('rin=0','rin=ffffffff'))
        self.assertTrue(unusual['complete'],unusual)
        partial=''.join(lines[:start+1])+engine+prepared+''.join(lines[start+1:write+1])+queued+''.join(lines[write+1:poll+1])+acked
        report=self.decode(partial);self.assertFalse(report['complete']);self.assertFalse(report['errors'])
        self.assertEqual(report['configuration_transfer']['completed_bytes'],0)
        self.assertFalse(report['core_load_acceptance'])
        self.assertFalse(self.decode(raw+'panic: late firmware failure\r\n')['complete'])


class NativeComputerTraceTests(unittest.TestCase):
    """The source-bound program reports commands; CPU execution is unobserved."""
    identity=dict(NativeContainedConfigurationTraceTests.identity,control_abi=0x10203041)
    configuration=NativeContainedConfigurationTraceTests.configuration

    def setUp(self):
        data=ROOT/'platform/embedded/tamago/nativefabric/testdata'
        self.memory=json.loads((data/'hps-reader-memory.json').read_text())
        self.computer=json.loads((data/'hps-computer.json').read_text())
        for mapping in (self.memory,self.computer):
            mapping.update(package_id=self.identity['package_id'],build_id=self.identity['build_id'])
        self.computer['control_map_sha256']=self.identity['map_sha256']

    def decode(self,raw,**changes):
        options=dict(fpga_identity=self.identity,configuration=self.configuration,from_storage=True,
                     reader_memory=self.memory,computer=self.computer)
        options.update(changes)
        return trace.decode_native_configuration(raw,BUILD,SEED,**options)

    def capture(self):
        raw=NativeContainedConfigurationTraceTests().stored_capture()
        # The native computer client reads its own GP port. It never emits the
        # separate EL3 endpoint identity probe's reads or transition markers.
        raw=raw[:raw.index('NOTICE:  FES_NANO_IDENTITY phase=open')]
        pinned=(f'FES_NATIVE_FPGA_READER_MEMORY PINNED cpu_base=95000000 bus_base=95000000 bytes=8192 arena_bytes=2097152 '
                f'map_sha256={trace.context_digest(self.memory)} pattern_sha256={self.memory["pattern_sha256"]} '
                f'owner={OWNER:x} generation=4 fpga_access=unqualified\r\n')
        raw=raw.replace('FES_NATIVE_HPS_CONTEXT',pinned+'FES_NATIVE_HPS_CONTEXT',1)
        raw+=('NOTICE:  FES_NANO_SMMU status=0 reads=26 idr0=080f7e3f idr1=0e739d18 aidr=1 s_idr1=80000018 '
              'cr0=0 ack=0 gbpa=0 agbpa=0 s_cr0=0 s_ack=0 s_init=0 s_gbpa=0 s_agbpa=0 '
              'identified=1 settled=1 bypass_controls_clear=1 access=unqualified\r\n')
        raw+=f'NOTICE:  FES_NANO_DDR_FIREWALL status=0 owner={OWNER:x} generation=4 reset=f mpfe_reset=0 reads=66 enable=1 base=95000000 bytes=8192 access=unqualified\r\n'
        for i in range(8):
            raw+=f'NOTICE:  FES_NANO_DDR_REGION index={i} base={"80000000" if not i else "0"} baseext=0 limit={"bfffffff" if not i else "ffff"} limitext=0\r\n'
        raw+='NOTICE:  FES_NANO_DDR_FIREWALL_END layout_known=1 window_covered=1 access=unqualified\r\n'
        raw+=(f'NOTICE:  FES_NANO_MASTER_STOP status=0 owner={OWNER:x} generation=5 reset=d '
              'reader_reads=88 reader_polls=1 disable=0 probe_polls=2 cancel=0 complete=1 data=held\r\n')
        raw+=(f'FES_NATIVE_COMPUTER_CONTROL PASS package_id={self.computer["package_id"]} build_id={self.computer["build_id"]} '
              f'base=20010300 map_sha256={trace.context_digest(self.computer)} owner={OWNER:x} generation=6 '
              'held_ack=1 data_held=1 execution_observed=0 runtime_admission=unavailable\r\n')
        return raw

    def test_control_cycle_requires_contained_configuration_and_owned_master_stop(self):
        report=self.decode(self.capture())
        self.assertTrue(report['complete'],report)
        self.assertEqual(report['last_checkpoint'],'computer_control')
        self.assertTrue(report['configuration_transfer']['complete'])
        self.assertEqual(report['configuration_transfer']['completed_bytes'],65664)
        self.assertFalse(report['core_load_acceptance'])
        observed=report['computer_control_observation']
        self.assertTrue(observed['command_cycle_reported'])
        self.assertFalse(observed['execution_observed'])
        self.assertFalse(observed['runtime_admission'])
        self.assertFalse(report['master_stop_observation']['physical_ddr_drain_qualified'])
        self.assertFalse(any(row['stage'].startswith('identity_') for row in report['records']))

    def test_missing_replayed_reordered_or_malformed_scope_observations_never_pass(self):
        raw=self.capture();lines=raw.splitlines(keepends=True)
        important=('reader_memory','hps_context','fpga_query','fpga_status','smmu','ddr_firewall',
                   'ddr_firewall_end','master_stop','computer_control')
        report=self.decode(raw)
        for row in report['records']:
            if row['stage'] not in important and not row['stage'].startswith(('ddr_region_','configuration_')):continue
            # Identical bounded status/poll samples can legitimately repeat.
            if row['stage'] in ('configuration_poll','configuration_status'):continue
            i=row['line']-1
            for changed in (''.join(lines[:i]+lines[i+1:]),
                            ''.join(lines[:i]+[lines[i],lines[i]]+lines[i+1:]),
                            ''.join(lines[:i]+lines[i+1:]+[lines[i]]),
                            raw.replace(lines[i],lines[i].replace('=',':',1))):
                if changed==raw:continue
                with self.subTest(stage=row['stage'],changed=changed[-100:]):
                    self.assertFalse(self.decode(changed)['complete'])

    def test_scope_marker_cannot_claim_another_identity_generation_or_execution(self):
        raw=self.capture();prefix,marker=raw.rsplit('FES_NATIVE_COMPUTER_CONTROL',1)
        for old,new in (('package_id='+self.computer['package_id'],'package_id='+'a'*64),
                        ('build_id='+self.computer['build_id'],'build_id='+'a'*32),
                        ('base=20010300','base=20010400'),
                        ('map_sha256='+trace.context_digest(self.computer),'map_sha256='+'f'*64),
                        (f'owner={OWNER:x}',f'owner={OWNER^2:x}'),('generation=6','generation=5'),
                        ('held_ack=1','held_ack=0'),('data_held=1','data_held=0'),
                        ('execution_observed=0','execution_observed=1'),
                        ('runtime_admission=unavailable','runtime_admission=available'),('PASS','FAIL')):
            self.assertIn(old,marker)
            with self.subTest(new=new):
                self.assertFalse(self.decode(prefix+'FES_NATIVE_COMPUTER_CONTROL'+marker.replace(old,new))['complete'])
        for old,new in (('generation=5 reset=d reader_reads=','generation=4 reset=d reader_reads='),
                        ('complete=1 data=held','complete=0 data=held'),('reset=d reader_reads=','reset=5 reader_reads='),
                        ('reader_reads=88','reader_reads=44'),('disable=0','disable=1'),('cancel=0','cancel=1')):
            with self.subTest(master_stop=new):self.assertFalse(self.decode(raw.replace(old,new))['complete'])

    def test_computer_selection_is_explicit_source_bound_and_excludes_active_ddr(self):
        raw=self.capture()
        self.assertFalse(self.decode(raw,computer=None)['complete'])
        for changes in ({'from_storage':False},{'reader_memory':None},{'ddr_read':{}},
                        {'computer':{}},{'computer':False},{'computer':dict(self.computer,extra=1)}):
            with self.subTest(selection=changes),self.assertRaises(ValueError):self.decode(raw,**changes)
        for key,value in (('format',True),('cpu_base',0x20010400),('span',True),('span',64),
                          ('abi_minor',1),('capabilities',0x1803),('hps_master',True),
                          ('scope','runtime'),('package_id','a'*64),('build_id','a'*32),
                          ('control_map_sha256','f'*64),('handoff_sha256','0'*64),
                          ('endpoint_sha256','bad'),('wrapper_sha256','0'*64)):
            with self.subTest(key=key),self.assertRaises(ValueError):
                self.decode(raw,computer=dict(self.computer,**{key:value}))

    def test_fragmented_output_and_late_failures_do_not_complete(self):
        raw=self.capture()
        for i in range(0,len(raw),101):
            with self.subTest(prefix=i):self.assertFalse(self.decode(raw[:i])['complete'])
        partial=self.decode(raw[:raw.index('FES_NATIVE_COMPUTER_CONTROL')])
        self.assertEqual(partial['errors'],[])
        self.assertEqual(partial['last_checkpoint'],'master_stop')
        self.assertTrue(partial['configuration_transfer']['complete'])
        for changed in (raw.rstrip(),raw+'partial',raw+'panic: late failure\n',
                        raw+'FES_NATIVE_COMPUTER_CONTROL broken\n',raw+'x'*(trace.MAX_BYTES+1)):
            self.assertFalse(self.decode(changed)['complete'])
        self.assertTrue(self.decode(raw.encode())['complete'])

    def test_computer_cycle_preserves_the_checked_loaded_bl31_gate(self):
        sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
        try:from prepare_uboot import atf_descriptor
        finally:sys.path.pop(0)
        expected=atf_descriptor('1'*64,90173)
        marker=('FES_NANO_SPL ATF target=80000000 bl32=0 bl33=80200000 fdt=0 params=bff00008 '
                'cold=21768 cookie=464e433200000002 el=3 mpidr=81000000 sctlr=1000 scr=501 daif=3c0 '
                'bytes=90173 sha256='+'1'*64+'\r\n')
        before='NOTICE:  FES_NANO_EL3_CAPTURE phase=before'
        raw=self.capture().replace(before,''.join(atf_progress())+marker+before)
        self.assertTrue(self.decode(raw,atf=expected)['complete'])
        for changed in (raw.replace(marker,''),raw.replace('sha256='+'1'*64,'sha256='+'2'*64),
                        raw.replace(atf_progress()[0],'')):
            self.assertFalse(self.decode(changed,atf=expected)['complete'])


class NativeComputerActivityTraceTests(unittest.TestCase):
    """Synthetic UART integration; real fetch signals are tested by the RTL model."""
    def setUp(self):
        self.base=NativeComputerTraceTests()
        self.base.setUp()

    def decode(self,raw,**changes):
        options={'computer_activity':True};options.update(changes)
        return self.base.decode(raw,**options)

    def capture(self,counts=(17,17,117,121,121,221,225,225)):
        raw=self.base.capture().replace('execution_observed=0','execution_observed=1')
        addresses=(0x10,0x10,0x36,0x42,0x42,0x56,0x60,0x60)
        samples=''.join(f'FES_NATIVE_COMPUTER_ACTIVITY phase={phase} fetches={count} address={address:x}\r\n'
                        for phase,count,address in zip(trace.ACTIVITY_PHASES,counts,addresses))
        return raw.replace('NOTICE:  FES_NANO_MASTER_STOP',samples+'NOTICE:  FES_NANO_MASTER_STOP')

    def test_two_runs_and_three_held_intervals_require_actual_fetch_progress(self):
        report=self.decode(self.capture())
        self.assertTrue(report['complete'],report)
        self.assertTrue(report['computer_activity']['complete'])
        self.assertEqual(report['computer_activity']['kind'],'z80-instruction-fetch-bus')
        self.assertEqual(len(report['computer_activity']['samples']),8)
        self.assertTrue(report['computer_control_observation']['execution_observed'])
        self.assertFalse(report['computer_control_observation']['runtime_admission'])
        self.assertFalse(report['core_load_acceptance'])
        wrapped=self.capture((0xfffffff1,0xfffffff1,17,21,21,117,130,130))
        self.assertTrue(self.decode(wrapped)['complete'])
        self.assertFalse(self.decode(self.base.capture())['complete'])
        self.assertFalse(self.decode(self.capture(),computer_activity=False)['complete'])

    def test_missing_replayed_reordered_or_malformed_samples_never_qualify_execution(self):
        raw=self.capture();lines=raw.splitlines(keepends=True)
        for index,line in enumerate(lines):
            if not line.startswith('FES_NATIVE_COMPUTER_ACTIVITY'):continue
            for changed in (''.join(lines[:index]+lines[index+1:]),
                            ''.join(lines[:index]+[line,line]+lines[index+1:]),
                            ''.join(lines[:index]+lines[index+1:]+[line]),
                            raw.replace(line,line.replace('=',':',1))):
                report=self.decode(changed)
                self.assertFalse(report['complete'])
                self.assertFalse(report['computer_activity']['complete'])
                self.assertFalse(report.get('computer_control_observation',{}).get('execution_observed',False))

    def test_stalled_runs_held_activity_resets_and_invalid_widths_reject(self):
        for counts in ((17,17,17,21,21,31,35,35),(17,18,117,121,121,221,225,225),
                       (17,17,117,0,0,100,104,104),(17,17,117,121,121,121,125,125),
                       (17,17,117,121,121,221,225,226),(17,17,0x80000011,0x80000021,0x80000021,0x80000031,0x80000035,0x80000035),
                       (17,17,0x100000011,0x100000021,0x100000021,0x100000031,0x100000035,0x100000035)):
            with self.subTest(counts=counts):self.assertFalse(self.decode(self.capture(counts))['complete'])
        raw=self.capture()
        for old,new in (('address=42','address=10000'),('address=42','address=43'),
                        ('phase=initial-held','phase=held'),('fetches=17','fetches=-1'),
                        ('runtime_admission=unavailable','runtime_admission=available'),
                        ('execution_observed=1','execution_observed=0')):
            self.assertIn(old,raw)
            changed=raw.replace(old,new,1)
            with self.subTest(new=new):self.assertFalse(self.decode(changed)['complete'])

    def test_partial_or_failed_configuration_cannot_leave_an_execution_claim(self):
        raw=self.capture()
        for changed in (raw[:raw.index('FES_NATIVE_COMPUTER_CONTROL')],raw.rstrip(),
                        raw.replace('bytes=65664 owner=','bytes=65660 owner='),
                        raw+'panic: late failure\n',raw.replace('complete=1 data=held','complete=0 data=held')):
            report=self.decode(changed)
            self.assertFalse(report['complete'])
            self.assertFalse(report['computer_activity']['complete'])
            self.assertFalse(report.get('computer_control_observation',{}).get('execution_observed',False))
        for changes in ({'computer':None},{'computer_activity':1},{'computer_activity':None},{'ddr_read':{}}):
            with self.subTest(changes=changes),self.assertRaises(ValueError):self.decode(raw,**changes)


class NativeComputerReloadTraceTests(unittest.TestCase):
    """Two-load UART fixtures, with one boot and no reused hardware receipts."""
    def setUp(self):
        self.base=NativeComputerActivityTraceTests();self.base.setUp()

    def decode(self,raw):
        base=self.base.base
        return trace.decode_native_computer_reload(raw,BUILD,SEED,fpga_identity=base.identity,
            configuration=base.configuration,reader_memory=base.memory,computer=base.computer)

    def capture(self):
        raw=self.base.capture((0,0,117,121,121,221,225,225))
        prefix,body=raw.split('FES_NATIVE_HPS_CONTEXT',1);body='FES_NATIVE_HPS_CONTEXT'+body
        bodies=[]
        for cycle in (1,2):
            phase=body
            if cycle==2:
                phase=re.sub(r'\bgeneration=([456])\b',lambda m:'generation='+str(int(m[1])+2),phase)
            phase=re.sub(r'(phase=initial(?:-held)? fetches=0 address=)10',r'\g<1>0',phase)
            generation=4+(cycle-1)*2
            activation=(f'NOTICE:  FES_NANO_FABRIC_ACTIVATION owner={OWNER:x} generation={generation} '
                        f'previous={0 if cycle==1 else 4} rearm={cycle-1} complete=1 data_held=1\r\n')
            phase=phase.replace('NOTICE:  FES_NANO_CORE_TRANSFER phase=complete',activation+'NOTICE:  FES_NANO_CORE_TRANSFER phase=complete')
            lines=phase.splitlines(keepends=True);ordinal=0
            for index,line in enumerate(lines):
                if 'FES_NANO_CORE_TRANSFER' not in line:continue
                ordinal+=1
                lines[index]=line.rstrip('\r\n')+f' counter={cycle*0x1000000+ordinal*0x1000:x} hash_ticks={cycle*0x100000+ordinal*0x100:x}\r\n'
            phase=''.join(lines)
            bodies.append(f'FES_NATIVE_FPGA_CYCLE BEGIN cycle={cycle} count=2 owner={OWNER:x} generation={generation}\r\n'+phase+
                          f'FES_NATIVE_FPGA_CYCLE END cycle={cycle} count=2\r\n')
        return prefix+''.join(bodies)

    def test_two_transfers_and_returns_keep_one_boot_and_fresh_lineage(self):
        report=self.decode(self.capture())
        self.assertTrue(report['complete'],report)
        self.assertTrue(report['loader_ready']);self.assertTrue(report['execution_observed'])
        self.assertFalse(report['core_load_acceptance'])
        self.assertEqual([c['report']['fabric_activation']['generation'] for c in report['cycles']],[4,6])
        self.assertEqual([c['report']['master_stop_observation']['generation'] for c in report['cycles']],[5,7])
        self.assertEqual([c['report']['computer_control_observation']['generation'] for c in report['cycles']],[6,8])
        self.assertFalse(report['cycles'][1]['report']['loader_ready'])
        self.assertTrue(self.decode(self.capture().encode())['complete'])
        base=self.base.base
        options=dict(fpga_identity=base.identity,configuration=base.configuration,from_storage=True,
                     reader_memory=base.memory,computer=base.computer,computer_activity=True)
        self.assertTrue(trace.decode_native_configuration(self.capture(),BUILD,SEED,**options,computer_reload=True)['complete'])
        self.assertFalse(trace.decode_native_configuration(self.capture(),BUILD,SEED,**options)['complete'])
        self.assertFalse(trace.decode_native_configuration(self.base.capture(),BUILD,SEED,**options,computer_reload=True)['complete'])
        for invalid in (None,0,1,'true'):
            with self.subTest(selection=invalid),self.assertRaises(ValueError):
                trace.decode_native_configuration(self.capture(),BUILD,SEED,**options,computer_reload=invalid)

    def test_every_partial_line_keeps_reload_incomplete_without_replaying_boot(self):
        raw=self.capture();offset=0
        for line in raw.splitlines(keepends=True)[:-1]:
            offset+=len(line)
            with self.subTest(offset=offset):
                report=self.decode(raw[:offset])
                self.assertFalse(report['complete'])
                self.assertEqual(report['errors'],[])
        self.assertFalse(self.decode(raw.rstrip())['complete'])

    def test_missing_repeated_or_unframed_cycle_evidence_never_completes(self):
        raw=self.capture();lines=raw.splitlines(keepends=True)
        for i,line in enumerate(lines):
            if not any(marker in line for marker in ('FES_NATIVE_FPGA_CYCLE','FES_NANO_FABRIC_ACTIVATION','FES_NANO_MASTER_STOP','FES_NATIVE_COMPUTER_CONTROL','FES_NATIVE_HPS_CONTEXT')):continue
            for changed in (''.join(lines[:i]+lines[i+1:]),''.join(lines[:i]+[line,line]+lines[i+1:])):
                with self.subTest(line=i,changed=len(changed)):self.assertFalse(self.decode(changed)['complete'])
        for extra in ('panic: late failure\n','FES_NATIVE_FPGA_CYCLE BEGIN cycle=3 count=3 owner=1 generation=8\n',
                      'FES_NANO_MASTER_STOP malformed\n','U-Boot unexpected reset\n','partial'):
            with self.subTest(extra=extra):self.assertFalse(self.decode(raw+extra)['complete'])
        self.assertFalse(self.decode(raw+'x'*(trace.MAX_BYTES+1))['complete'])

    def test_second_cycle_rejects_old_owner_receipt_fetch_state_and_timing(self):
        raw=self.capture();index=raw.index('FES_NATIVE_FPGA_CYCLE BEGIN cycle=2')
        prefix,second=raw[:index],raw[index:]
        changes=(('generation=6 previous=4 rearm=1','generation=6 previous=0 rearm=1'),
                 ('generation=6 previous=4 rearm=1','generation=6 previous=4 rearm=0'),
                 ('generation=7 reset=d','generation=5 reset=d'),
                 ('generation=8 held_ack=1','generation=6 held_ack=1'),
                 ('reader_reads=88','reader_reads=0'),
                 ('phase=initial fetches=0','phase=initial fetches=225'),
                 ('phase=initial fetches=0 address=0','phase=initial fetches=0 address=10'),
                 (f'owner={OWNER:x}',f'owner={OWNER+2:x}'))
        for old,new in changes:
            self.assertIn(old,second)
            with self.subTest(change=new):self.assertFalse(self.decode(prefix+second.replace(old,new))['complete'])
        without_timing=re.sub(r' counter=[0-9a-f]+ hash_ticks=[0-9a-f]+','',second)
        self.assertFalse(self.decode(prefix+without_timing)['complete'])
        stale_clock=re.sub(r' counter=([0-9a-f]+)',lambda m:' counter='+format(int(m[1],16)-0x1000000,'x'),second)
        self.assertFalse(self.decode(prefix+stale_clock)['complete'])
        stale_hash=re.sub(r' hash_ticks=([0-9a-f]+)',lambda m:' hash_ticks='+format(int(m[1],16)-0x100000,'x'),second)
        self.assertFalse(self.decode(prefix+stale_hash)['complete'])


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
        # Retained physical capture predates secure capture instrumentation.
        report=trace.decode_native_boot(raw,expected['build_id'],expected['owner_seed'],storage=expected['storage'],capture_trace=False)
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
        # Retained physical capture predates secure capture instrumentation.
        report=trace.decode_native_boot(raw,expected['build_id'],expected['owner_seed'],package=expected['package'],capture_trace=False)
        self.assertTrue(report['complete'],report)
        self.assertEqual(report['last_checkpoint'],'package_pass')
        self.assertFalse(report['core_load_acceptance'])


if __name__ == '__main__':
    unittest.main()
