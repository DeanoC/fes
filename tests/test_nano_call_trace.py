"""SMC instruction/PSCI receipts retain uncertainty and reject broken calls."""
import hashlib
from pathlib import Path
import sys
import unittest
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
sys.path[:0]=[str(ROOT/'platform/embedded/nano'),str(ROOT/'platform/embedded/firmware')]
import call_trace
import nano_firmware_call as producer
from test_nano_native_trace import trace,fixture,firmware_progress,atf_progress,BUILD,SEED
from prepare_uboot import atf_descriptor


def context(call):return dict(boot_seen=True,loader_phase=f'cpu{call}-before' if call<4 else 'owned-after',late_seen=call>=4,live_seen=False,prior_valid=True)
def marker(p):return 'FES_NANO_CALL phase='+p


class AffinityCallTraceTests(unittest.TestCase):
    def test_eight_complete_calls_and_each_partial_state_remain_bounded(self):
        value=call_trace.state()
        for call in range(8):
            for step,phase in enumerate(call_trace.PHASES):
                self.assertIsNone(call_trace.observe(value,marker(phase),call*6+step,**context(call)))
                self.assertEqual(value['initial_complete'],len(value['records'])>=24)
                self.assertEqual(value['complete'],len(value['records'])==48)
                self.assertEqual(value['instruction_returns_observed'],len(value['records'])//6)
        self.assertTrue(value['instruction_may_have_started'] and value['cache_may_have_started'])
        self.assertIsNotNone(call_trace.observe(value,marker('smc-after'),49,**context(7)))
        self.assertFalse(value['valid'] or value['complete'] or value['initial_complete'])
        self.assertEqual(len(value['records']),48)

    def test_wrong_scope_bad_order_and_failure_never_heal(self):
        for altered in (dict(boot_seen=False),dict(loader_phase='cpu1-before'),dict(late_seen=True),dict(live_seen=True),dict(prior_valid=False)):
            value=call_trace.state();args=context(0);args.update(altered)
            self.assertIsNotNone(call_trace.observe(value,marker('smc-before'),1,**args))
            self.assertFalse(value['valid'])
        for line in (marker('cache-before'),marker('smc-before')+' extra=1','FES_NANO_CALL FAIL phase=smc-before',marker('foreign')):
            value=call_trace.state();self.assertIsNotNone(call_trace.observe(value,line,1,**context(0)))
            for n in range(100):call_trace.observe(value,marker('smc-before'),n+2,**context(0))
            self.assertEqual(value['records'],[]);self.assertFalse(value['valid'] or value['complete'])

    def test_complete_boot_requires_all_initial_and_late_calls(self):
        expected=atf_descriptor('1'*64,90173);anchor='NOTICE:  FES_NANO_EL3_CAPTURE phase=before'
        descriptor=('FES_NANO_SPL ATF target=80000000 bl32=0 bl33=80200000 fdt=0 params=bff00008 cold=21768 cookie=464e433200000002 el=3 mpidr=81000000 sctlr=1000 scr=501 daif=3c0 bytes=90173 sha256='+'1'*64+'\r\n')
        raw=firmware_progress(fixture().replace(anchor,''.join(atf_progress())+descriptor+anchor))
        decode=lambda data:trace.decode_native_boot(data,BUILD,SEED,atf=expected,firmware_ram_trace=True)
        good=decode(raw);self.assertTrue(good['complete'],good['errors']);self.assertTrue(good['affinity_calls']['complete'])
        for phase in call_trace.PHASES:
            line=marker(phase)+'\r\n'
            for change in (raw.replace(line,''),raw.replace(line,line*2,1),raw.replace(line,marker('foreign')+'\r\n',1),line+raw.replace(line,'',1)):
                result=decode(change);self.assertFalse(result['complete']);self.assertTrue(result['errors'])
            partial=decode(raw[:raw.index(line)]+line[:-2])
            self.assertFalse(partial['complete'] or partial['affinity_calls']['complete'])
            self.assertFalse(partial['core_load_acceptance'])
        before=marker('cache-before')+'\r\n';partial=decode(raw[:raw.index(before)]+before)
        self.assertTrue(partial['affinity_calls']['cache_may_have_started'])
        self.assertFalse(partial['affinity_calls']['complete'])

    def test_source_pins_contexts_and_original_operations_are_preserved(self):
        with self.assertRaises(ValueError):producer.psci_source('changed')
        with self.assertRaises(ValueError):producer.fwcall_source('changed')
        psci='''int psci_affinity_info(u_register_t target_affinity,
                               unsigned int lowest_affinity_level)
{
\tunsigned int target_idx;
\ttarget_idx = 0;
\tflush_cpu_data_by_index(target_idx,
\t\t\t\tpsci_svc_cpu_data.aff_info_state);
\treturn psci_get_aff_info_state_by_idx(target_idx);
}
'''
        with patch.object(producer,'PSCI_SHA',hashlib.sha256(psci.encode()).hexdigest()):
            result=producer.psci_source(psci)
            self.assertIn('flush_cpu_data_by_index(target_idx,',result)
            self.assertEqual(result.count('fes_nano_ram_call_trace('),4)
        missing=psci.replace('flush_cpu_data_by_index','foreign_flush')
        with patch.object(producer,'PSCI_SHA',hashlib.sha256(missing.encode()).hexdigest()):
            with self.assertRaises(ValueError):producer.psci_source(missing)
        source='''void smc_call(struct pt_regs *args)
{
\tasm volatile(
\t\t"smc\t#0\\n"
\t\t: "+m" (args->regs[0]) : :
\t\t  "x16", "x17");
}
'''
        with patch.object(producer,'FWCALL_SHA',hashlib.sha256(source.encode()).hexdigest()):
            result=producer.fwcall_source(source)
            self.assertEqual(result.count('"smc\t#0\\n"'),1)
            self.assertIn('"x19", "cc", "memory"',result)
            self.assertIn('cbz x19',result)
        asm=producer.assembly()
        self.assertNotIn('bl ',asm);self.assertNotIn('sp,',asm)
        for phase in producer.C_PHASES:self.assertIn(marker(phase),asm)
        self.assertNotIn('msr ',asm);self.assertNotIn('dc ',asm)


if __name__=='__main__':unittest.main()
