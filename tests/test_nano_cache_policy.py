"""Selected cold set/way maintenance receipts and profile rejection."""
import copy
from pathlib import Path
import sys
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
sys.path[:0]=[str(ROOT/'platform/embedded/nano'),str(ROOT/'platform/embedded/firmware')]
import fault_status as status
import nano_firmware_ram as ram
from test_nano_native_trace import trace,fixture,firmware_progress,atf_progress,BUILD,SEED


def row(phase):return 'FES_NANO_RAM CACHE phase='+phase


def context():
    decode=status.decode_state()
    decode.update(records=[dict(phase='enabled',line=1)],write_attempted=True)
    return dict(guard_complete=True,vendor_phase='nobits-cache-before',decode=decode,exception=False)


class ColdCachePolicyTests(unittest.TestCase):
    def test_partial_and_complete_receipts_do_not_grant_ownership(self):
        state=status.cache_state()
        self.assertFalse(state['maintenance_may_have_started'] or state['complete'])
        self.assertIsNone(status.observe_cache(state,row('setway-before'),2,**context()))
        self.assertTrue(state['maintenance_may_have_started']);self.assertFalse(state['complete'])
        self.assertIsNone(status.observe_cache(state,row('setway-after'),3,**context()))
        self.assertTrue(state['valid'] and state['complete'])

    def test_failed_duplicate_reordered_and_flooded_receipts_stay_invalid(self):
        for phases in (('setway-after',),('setway-before','setway-before'),('setway-before','unknown'),
                       ('setway-before','setway-after','setway-after')):
            state=status.cache_state();errors=[]
            for n,phase in enumerate(phases):
                errors.append(status.observe_cache(state,row(phase),n+1,**context()))
            self.assertTrue(any(errors));self.assertFalse(state['valid'] or state['complete'])
            for n in range(100):status.observe_cache(state,row('setway-after'),n+10,**context())
            self.assertLessEqual(len(state['records']),2);self.assertFalse(state['complete'])
        for line in ('FES_NANO_RAM CACHE FAIL phase=admit',row('setway-before')+' extra=1'):
            state=status.cache_state();self.assertIsNotNone(status.observe_cache(state,line,1,**context()))
            self.assertFalse(state['valid'] or state['complete'])

    def test_only_selected_live_vendor_window_accepts_receipts(self):
        cases=[dict(guard_complete=False),dict(exception=True),dict(vendor_phase='rw-cache-after')]
        for key,value in (('valid',False),('records',[]),('restore_attempted',True)):
            decode=context()['decode'];decode[key]=value;cases.append(dict(decode=decode))
        for changes in cases:
            args=context();args.update(changes);state=status.cache_state()
            self.assertIsNotNone(status.observe_cache(state,row('setway-before'),1,**args))
            self.assertFalse(state['valid'] or state['complete'])

    def test_old_profile_and_unreviewed_cache_source_are_rejected(self):
        from test_nano_firmware_selection import profile
        selected=profile();selected.update(firmware_ram=dict(ram.CONTRACT),chain_sha256='42'*32)
        self.assertEqual(ram.checked(selected)['cache_policy'],'owned-cold-all-setway-before-nobits-v1')
        old=copy.deepcopy(selected);old['firmware_ram'].update(format=1,scope='matched-spl-el3-ocram-nobits-v1')
        old['firmware_ram'].pop('cache_policy')
        with self.assertRaises(ValueError):ram.checked(old)
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);cache=root/ram.CACHE;cache.parent.mkdir(parents=True);cache.write_text('changed helper')
            with self.assertRaisesRegex(ValueError,'unreviewed cold set/way'):ram.install(root,selected)

    def test_boot_requires_complete_maintenance_and_exception_stays_fatal(self):
        from prepare_uboot import atf_descriptor
        expected=atf_descriptor('1'*64,90173)
        anchor='NOTICE:  FES_NANO_EL3_CAPTURE phase=before'
        descriptor=('FES_NANO_SPL ATF target=80000000 bl32=0 bl33=80200000 fdt=0 params=bff00008 cold=21768 cookie=464e433200000002 el=3 mpidr=81000000 sctlr=1000 scr=501 daif=3c0 bytes=90173 sha256='+'1'*64+'\r\n')
        raw=firmware_progress(fixture().replace(anchor,''.join(atf_progress())+descriptor+anchor))
        decode=lambda data:trace.decode_native_boot(data,BUILD,SEED,atf=expected,firmware_ram_trace=True)
        good=decode(raw);self.assertTrue(good['complete'],good['errors'])
        self.assertTrue(good['firmware_ram_trace']['cache_maintenance']['complete'])
        for phase in status.CACHE_PHASES:
            marker=row(phase)+'\r\n'
            for damaged in (raw.replace(marker,''),raw.replace(marker,marker+marker)):
                self.assertFalse(decode(damaged)['loader_ready'])
        fault='FES_NANO_RAM EXCEPTION '+' '.join(name+'=0000000000000000' for name in
            ('vector','esr','elr','far','spsr','r0','r1','r2','r3'))+'\r\n'
        bad=decode(raw.replace(row('setway-before')+'\r\n',row('setway-before')+'\r\n'+fault))
        self.assertFalse(bad['complete'] or bad['loader_ready'])
        self.assertFalse(bad['firmware_ram_trace']['cache_maintenance']['complete'])


if __name__=='__main__':unittest.main()
