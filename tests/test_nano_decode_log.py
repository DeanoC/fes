"""Contained decode logging lifetime and failure receipts; never boot grants."""
from pathlib import Path
import sys
import unittest

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
import fault_status as status
from test_nano_native_trace import trace,fixture,firmware_progress,atf_progress,BUILD,SEED


def row(phase):return 'FES_NANO_RAM DECODE phase='+phase


def observe(phases, *, fatal_at=None, inventory=True, vendor_override=None, guard=True):
    state=status.decode_state();errors=[]
    for index,phase in enumerate(phases):
        fatal=fatal_at is not None and index>=fatal_at
        vendor=None if phase=='idle' else 'nobits-cache-before' if phase in ('restore-before','restored') else 'rw-cache-after'
        if vendor_override is not None:vendor=vendor_override
        error=status.observe_decode(state,row(phase),index+1,guard_complete=guard,vendor_phase=vendor,exception=fatal,fault_inventory_complete=inventory)
        if error:errors.append(error)
    return state,errors


class DecodeLogTests(unittest.TestCase):
    def test_complete_cycle_requires_restoration_and_preserves_write_history(self):
        state,errors=observe(status.DECODE_PHASES)
        self.assertEqual(errors,[])
        self.assertTrue(state['valid'] and state['complete'] and state['restored'])
        self.assertTrue(state['write_attempted'] and state['restore_attempted'])
        self.assertFalse(state['failed'])

    def test_each_partial_boundary_keeps_write_and_restore_uncertainty(self):
        for count in range(8):
            state,errors=observe(status.DECODE_PHASES[:count])
            self.assertEqual(errors,[])
            self.assertEqual(state['write_attempted'],count>=4)
            self.assertEqual(state['restore_attempted'],count>=6)
            self.assertEqual(state['restored'],count>=7)
            self.assertEqual(state['complete'],count>=7)

    def test_fatal_cleanup_accepts_interrupted_enable_only_after_inventory(self):
        phases=status.DECODE_PHASES[:4]+('restore-before','restored')
        state,errors=observe(phases,fatal_at=4)
        self.assertEqual(errors,[])
        self.assertTrue(state['restored'])
        state,errors=observe(phases,fatal_at=4,inventory=False)
        self.assertTrue(errors);self.assertFalse(state['restored'])
        state,errors=observe(phases)
        self.assertTrue(errors);self.assertFalse(state['restored'])

    def test_inactive_and_interrupted_cleanup_are_explicit_terminal_outcomes(self):
        state,errors=observe(('idle','inactive'),fatal_at=1)
        self.assertEqual(errors,[])
        self.assertFalse(state['write_attempted'] or state['restored'] or state['complete'])
        for phases in (status.DECODE_PHASES[:4]+('restore-interrupted',),status.DECODE_PHASES[:6]+('restore-interrupted',)):
            state,errors=observe(phases,fatal_at=len(phases)-1)
            self.assertEqual(errors,[])
            self.assertTrue(state['restore_attempted'] and state['failed'])
            self.assertFalse(state['restored'] or state['complete'])
            state,errors=observe(phases+('restored',),fatal_at=len(phases)-1)
            self.assertTrue(errors);self.assertFalse(state['restored'])
        state,errors=observe(status.DECODE_PHASES[:4]+('inactive',),fatal_at=4)
        self.assertTrue(errors)

    def test_wrong_order_scope_missing_guard_and_extra_data_fail(self):
        cases=[status.DECODE_PHASES[1:],status.DECODE_PHASES[:1]*2,
               status.DECODE_PHASES[:3]+status.DECODE_PHASES[4:],
               status.DECODE_PHASES+('idle',),('unknown',)]
        for phases in cases:
            state,errors=observe(phases)
            self.assertTrue(errors);self.assertFalse(state['complete'])
        for context in (dict(guard=False),dict(vendor_override='setup-before')):
            state,errors=observe(status.DECODE_PHASES,**context)
            self.assertTrue(errors);self.assertFalse(state['complete'])
        state=status.decode_state()
        self.assertIsNotNone(status.observe_decode(state,row('idle')+' extra=1',1,guard_complete=True,vendor_phase=None,exception=False,fault_inventory_complete=False))

    def test_failures_never_become_success_and_flood_is_bounded(self):
        for prefix,phase in (((), 'identity'),(('idle',),'admit'),(status.DECODE_PHASES[:4],'enable'),(status.DECODE_PHASES[:6],'restore')):
            state,errors=observe(prefix)
            error=status.observe_decode(state,'FES_NANO_RAM DECODE FAIL phase='+phase,9,guard_complete=True,vendor_phase='rw-cache-after',exception=False,fault_inventory_complete=False)
            self.assertIsNotNone(error);self.assertTrue(state['failed'])
            self.assertFalse(state['complete'])
            for n in range(100):
                status.observe_decode(state,row('restored'),10+n,guard_complete=True,vendor_phase='nobits-cache-before',exception=False,fault_inventory_complete=False)
            self.assertFalse(state['complete']);self.assertLessEqual(len(state['records']),10)

    def test_actual_boot_decoder_requires_logging_cycle_before_vendor_can_finish(self):
        from prepare_uboot import atf_descriptor
        expected=atf_descriptor('1'*64,90173)
        marker='NOTICE:  FES_NANO_EL3_CAPTURE phase=before'
        descriptor=('FES_NANO_SPL ATF target=80000000 bl32=0 bl33=80200000 fdt=0 params=bff00008 cold=21768 cookie=464e433200000002 el=3 mpidr=81000000 sctlr=1000 scr=501 daif=3c0 bytes=90173 sha256='+'1'*64+'\r\n')
        raw=firmware_progress(fixture().replace(marker,''.join(atf_progress())+descriptor+marker))
        decode=lambda value:trace.decode_native_boot(value,BUILD,SEED,atf=expected,firmware_ram_trace=True)
        good=decode(raw)
        self.assertTrue(good['complete'],good['errors'])
        self.assertTrue(good['firmware_ram_trace']['decode_log']['restored'])
        for phase in status.DECODE_PHASES:
            marker=row(phase)+'\r\n'
            for bad in (raw.replace(marker,''),raw.replace(marker,marker+marker)):
                report=decode(bad)
                self.assertFalse(report['loader_ready'])

    def test_fault_cleanup_retains_restoration_without_accepting_boot(self):
        from test_nano_fault_status import lines as inventory_lines
        from test_nano_fault_ras import lines as ras_lines
        from prepare_uboot import atf_descriptor
        expected=atf_descriptor('1'*64,90173)
        anchor='NOTICE:  FES_NANO_EL3_CAPTURE phase=before'
        descriptor=('FES_NANO_SPL ATF target=80000000 bl32=0 bl33=80200000 fdt=0 params=bff00008 cold=21768 cookie=464e433200000002 el=3 mpidr=81000000 sctlr=1000 scr=501 daif=3c0 bytes=90173 sha256='+'1'*64+'\r\n')
        raw=firmware_progress(fixture().replace(anchor,''.join(atf_progress())+descriptor+anchor))
        prefix=raw.split('FES_NANO_RAM VENDOR phase=nobits-cache-before',1)[0]
        fault='FES_NANO_RAM EXCEPTION '+' '.join(name+'=0000000000000000' for name in
            ('vector','esr','elr','far','spsr','r0','r1','r2','r3'))
        for complete in (True,False):
            inventory=inventory_lines() if complete else inventory_lines()[:-1]
            stream=prefix+'\r\n'.join([fault]+inventory+ras_lines()+
                [row('restore-before'),row('restored')])+'\r\n'
            report=trace.decode_native_boot(stream,BUILD,SEED,atf=expected,firmware_ram_trace=True)
            self.assertFalse(report['complete'] or report['loader_ready'])
            self.assertFalse(report['firmware_ram_trace']['valid'])
            self.assertEqual(report['firmware_ram_trace']['decode_log']['restored'],complete)

    def test_new_error_without_exception_rejects_after_verified_restoration(self):
        state,errors=observe(status.DECODE_PHASES)
        self.assertEqual(errors,[])
        error=status.observe_decode(state,'FES_NANO_RAM DECODE FAIL phase=status',8,
            guard_complete=True,vendor_phase='nobits-cache-before',exception=False,
            fault_inventory_complete=False)
        self.assertIsNotNone(error)
        self.assertTrue(state['restored'] and state['write_attempted'] and state['restore_attempted'])
        self.assertFalse(state['valid'] or state['complete'])
        self.assertTrue(state['failed'])


if __name__=='__main__':unittest.main()
