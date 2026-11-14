"""Private cold BSS clean requires complete ordered exit-maintenance evidence."""
import copy
from pathlib import Path
import sys
import unittest

ROOT=Path(__file__).resolve().parents[1]
sys.path[:0]=[str(ROOT/'platform/embedded/nano'),str(ROOT/'platform/embedded/firmware')]
import runtime_startup as startup
import nano_firmware_ram as ram
from test_nano_native_trace import trace,fixture,firmware_progress,atf_progress,BUILD,SEED


def marker(phase):return 'FES_NANO_RAM EXIT_CACHE phase='+phase


def context():return dict(startup_valid=True,runtime_phase='bss-clean-before',exception=False,uboot_seen=False)


class ExitCacheTests(unittest.TestCase):
    def test_partial_is_uncertain_and_completion_has_no_permission(self):
        state=startup.exit_state()
        self.assertIsNone(startup.observe_exit(state,marker('clean-before'),1,**context()))
        self.assertTrue(state['maintenance_may_have_started']);self.assertFalse(state['complete'])
        self.assertIsNone(startup.observe_exit(state,marker('clean-after'),2,**context()))
        self.assertTrue(state['complete'] and state['valid'])
        self.assertEqual(set(state),{'records','valid','complete','maintenance_may_have_started'})

    def test_rejects_failed_order_scope_and_flood_without_healing(self):
        for line in (marker('clean-after'),marker('clean-before')+' extra=1','FES_NANO_RAM EXIT_CACHE FAIL phase=admit'):
            state=startup.exit_state();self.assertIsNotNone(startup.observe_exit(state,line,1,**context()))
            for n in range(100):startup.observe_exit(state,marker('clean-before'),n+2,**context())
            self.assertFalse(state['valid'] or state['complete']);self.assertEqual(state['records'],[])
        for changes in (dict(startup_valid=False),dict(exception=True),dict(uboot_seen=True),dict(runtime_phase='main-after')):
            args=context();args.update(changes);state=startup.exit_state()
            self.assertIsNotNone(startup.observe_exit(state,marker('clean-before'),1,**args))
            self.assertFalse(state['complete'] or state['valid'])
        state=startup.exit_state()
        for n,phase in enumerate(startup.EXIT_PHASES):startup.observe_exit(state,marker(phase),n,**context())
        self.assertIsNotNone(startup.observe_exit(state,marker('clean-after'),3,**context()))
        self.assertFalse(state['complete']);self.assertEqual(len(state['records']),2)

    def test_old_profile_and_layout_changes_are_rejected(self):
        from test_nano_firmware_selection import profile
        p=profile();p['firmware_ram']=dict(ram.CONTRACT)
        self.assertEqual(ram.checked(p)['exit_cache_policy'],'owned-cold-dccsw-before-exit-v1')
        for change in (dict(format=4,scope='matched-spl-el3-ocram-nobits-v4',bss_end=0x5ddc0),
                       dict(bss_base=0x58040),dict(bss_limit=0x60040),dict(exit_cache_policy=None)):
            altered=copy.deepcopy(p);altered['firmware_ram'].update(change)
            with self.assertRaises(ValueError):ram.checked(altered)

    def test_boot_rejects_missing_failed_or_misplaced_clean_receipts(self):
        from prepare_uboot import atf_descriptor
        expected=atf_descriptor('1'*64,90173);anchor='NOTICE:  FES_NANO_EL3_CAPTURE phase=before'
        atf=('FES_NANO_SPL ATF target=80000000 bl32=0 bl33=80200000 fdt=0 params=bff00008 cold=21768 cookie=464e433200000002 el=3 mpidr=81000000 sctlr=1000 scr=501 daif=3c0 bytes=90173 sha256='+'1'*64+'\r\n')
        raw=firmware_progress(fixture().replace(anchor,''.join(atf_progress())+atf+anchor))
        decode=lambda value:trace.decode_native_boot(value,BUILD,SEED,atf=expected,firmware_ram_trace=True)
        self.assertTrue(decode(raw)['complete'])
        for phase in startup.EXIT_PHASES:
            row=marker(phase)+'\r\n'
            for altered in (raw.replace(row,''),raw.replace(row,row+row),row+raw.replace(row,''),raw.replace(row,'FES_NANO_RAM EXIT_CACHE FAIL phase=admit\r\n')):
                self.assertFalse(decode(altered)['loader_ready'] or decode(altered)['complete'])
        before=marker('clean-before')+'\r\n'
        fault='FES_NANO_RAM EXCEPTION '+' '.join(k+'=0000000000000000' for k in ('vector','esr','elr','far','spsr','r0','r1','r2','r3'))+'\r\n'
        bad=decode(raw.replace(before,before+fault))
        self.assertFalse(bad['loader_ready'] or bad['complete'])
        self.assertFalse(bad['firmware_ram_trace']['exit_cache']['complete'])


if __name__=='__main__':unittest.main()
