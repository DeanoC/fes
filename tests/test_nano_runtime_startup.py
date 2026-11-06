"""Cold runtime progress rejects incomplete, misplaced and damaged evidence."""
import copy
import hashlib
from pathlib import Path
import sys
import unittest
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
sys.path[:0]=[str(ROOT/'platform/embedded/nano'),str(ROOT/'platform/embedded/firmware')]
import runtime_startup as startup
import nano_firmware_runtime as producer
from test_nano_native_trace import trace,fixture,firmware_progress,atf_progress,BUILD,SEED


def marker(phase):return 'FES_NANO_RAM RUNTIME phase='+phase


def context(phase):
    after= startup.RUNTIME_PHASES.index(phase)>=3
    return dict(startup_valid=True,vendor_complete=True,cache_complete=True,
                logging_restored=True,exception=False,provider_seen=after,
                sd_seen=after,uboot_seen=False)


class ColdRuntimeStartupTests(unittest.TestCase):
    def test_partial_progress_and_final_receipt_are_observation_only(self):
        progress=startup.state()
        for n,phase in enumerate(startup.RUNTIME_PHASES):
            self.assertIsNone(startup.observe(progress,marker(phase),n,**context(phase)))
            self.assertEqual(progress['complete'],n==len(startup.RUNTIME_PHASES)-1)
        self.assertEqual(tuple(row['phase'] for row in progress['records']),startup.RUNTIME_PHASES)
        self.assertEqual(set(progress),{'records','complete','valid'})

    def test_failed_order_scope_and_flood_cannot_heal(self):
        variants=[dict(startup_valid=False),dict(vendor_complete=False),dict(cache_complete=False),
                  dict(logging_restored=False),dict(exception=True),dict(uboot_seen=True),
                  dict(provider_seen=True),dict(sd_seen=True)]
        for change in variants:
            progress=startup.state();args=context('main-before');args.update(change)
            self.assertIsNotNone(startup.observe(progress,marker('main-before'),1,**args))
            for n in range(100):startup.observe(progress,marker('main-before'),n+2,**context('main-before'))
            self.assertFalse(progress['valid'] or progress['complete']);self.assertEqual(progress['records'],[])
        for text in (marker('main-after'),marker('main-before')+' extra=1','FES_NANO_RAM RUNTIME FAIL phase=main-before'):
            progress=startup.state();self.assertIsNotNone(startup.observe(progress,text,1,**context('main-before')))
            self.assertFalse(progress['valid'] or progress['complete'])
        progress=startup.state()
        for n,phase in enumerate(startup.RUNTIME_PHASES):startup.observe(progress,marker(phase),n,**context(phase))
        self.assertIsNotNone(startup.observe(progress,marker('exit-before'),100,**context('exit-before')))
        self.assertFalse(progress['complete']);self.assertEqual(len(progress['records']),len(startup.RUNTIME_PHASES))

    def test_owned_return_requires_the_platform_sd_observation(self):
        progress=startup.state()
        for n,phase in enumerate(startup.RUNTIME_PHASES[:3]):startup.observe(progress,marker(phase),n,**context(phase))
        args=context('owned-after');args['sd_seen']=False
        self.assertIsNotNone(startup.observe(progress,marker('owned-after'),4,**args))
        self.assertFalse(progress['valid'] or progress['complete'])

    def test_complete_boot_requires_every_runtime_marker(self):
        from prepare_uboot import atf_descriptor
        expected=atf_descriptor('1'*64,90173)
        anchor='NOTICE:  FES_NANO_EL3_CAPTURE phase=before'
        descriptor=('FES_NANO_SPL ATF target=80000000 bl32=0 bl33=80200000 fdt=0 params=bff00008 cold=21768 cookie=464e433200000002 el=3 mpidr=81000000 sctlr=1000 scr=501 daif=3c0 bytes=90173 sha256='+'1'*64+'\r\n')
        raw=firmware_progress(fixture().replace(anchor,''.join(atf_progress())+descriptor+anchor))
        decode=lambda value:trace.decode_native_boot(value,BUILD,SEED,atf=expected,firmware_ram_trace=True)
        good=decode(raw);self.assertTrue(good['complete'],good['errors'])
        self.assertTrue(good['firmware_ram_trace']['runtime_startup']['complete'])
        for phase in startup.RUNTIME_PHASES:
            line=marker(phase)+'\r\n'
            for damaged in (raw.replace(line,''),raw.replace(line,line+line)):
                report=decode(damaged);self.assertFalse(report['loader_ready'] or report['complete'],phase)
        lines=''.join(marker(p)+'\r\n' for p in startup.RUNTIME_PHASES)
        missing=raw
        for p in startup.RUNTIME_PHASES:missing=missing.replace(marker(p)+'\r\n','')
        self.assertFalse(decode(missing+lines)['loader_ready'])
        # A real exception remains fatal even after all progress returned.
        fault='FES_NANO_RAM EXCEPTION '+' '.join(k+'=0000000000000000' for k in
            ('vector','esr','elr','far','spsr','r0','r1','r2','r3'))+'\r\n'
        self.assertFalse(decode(raw.replace(marker('exit-before'),fault+marker('exit-before')))['loader_ready'])

    def test_entry_transform_preserves_original_calls_arguments_and_warm_bytes(self):
        cold='func bl31_entrypoint\n\tbl\tbl31_main\n\tmov x0, x23\n\tbl\tclean_dcache_range\n\tmov x1, x24\n\tbl\tclean_dcache_range\n\tb\tel3_exit\nendfunc bl31_entrypoint\n'
        warm='func bl31_warm_entrypoint\n\tbl\tclean_dcache_range\nendfunc bl31_warm_entrypoint\n'
        result=producer.entry_source(cold+warm)
        self.assertIn(warm,result)
        self.assertIn('\tmov x0, x23\n',result);self.assertIn('\tmov x1, x24\n',result)
        for old in ('\tbl\tbl31_main\n','\tbl\tclean_dcache_range\n','\tb\tel3_exit\n'):
            self.assertEqual(result.count(old),(cold+warm).count(old))
        for bad in (cold.replace('\tbl\tbl31_main\n',''),cold.replace('\tb\tel3_exit\n',''),cold.replace('\tbl\tclean_dcache_range\n','',1)):
            with self.assertRaises(ValueError):producer.entry_source(bad+warm)

    def test_c_transform_rejects_unpinned_or_missing_call_context(self):
        with self.assertRaisesRegex(ValueError,'unreviewed'):producer.c_source('void bl31_main(void) {}\n',platform=False)
        body='void bl31_main(void)\n{\n'+''.join('\t'+call+'();\n' for call in
            ('bl31_platform_setup','bl31_lib_init','runtime_svc_init','bl31_prepare_next_image_entry','bl31_plat_runtime_setup','console_flush'))+'\tconsole_switch_state(CONSOLE_FLAG_RUNTIME);\n}\n'
        with patch.object(producer,'MAIN_SHA',hashlib.sha256(body.encode()).hexdigest()):
            result=producer.c_source(body,platform=False)
            self.assertIn(body.splitlines()[0],result)
            self.assertEqual(result.count('console_flush();'),1)
            self.assertIn(producer.c_mark('services-before')+'\truntime_svc_init();\n'+producer.c_mark('services-after'),result)
        missing=body.replace('\truntime_svc_init();\n','')
        with patch.object(producer,'MAIN_SHA',hashlib.sha256(missing.encode()).hexdigest()):
            with self.assertRaisesRegex(ValueError,'call context'):producer.c_source(missing,platform=False)


if __name__=='__main__':unittest.main()
