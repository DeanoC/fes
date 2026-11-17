"""Initial loader progress and actual bounded C UART/snapshot fault cases."""
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
import loader_progress as progress
from native_trace import decode_native_boot
from test_nano_native_trace import firmware_progress,fixture,atf_progress,BUILD,SEED
from prepare_uboot import atf_descriptor


def marker(phase):return 'FES_NANO_LOADER phase='+phase


def arguments(phase):
    return dict(boot_seen=True,memory_seen=phase!='memory-before',
                owned_seen=phase=='owned-after',later_handoff_seen=False,prior_valid=True)


class LoaderProgressTests(unittest.TestCase):
    def test_ordered_partial_receipts_and_permanent_rejection(self):
        value=progress.state()
        for n,phase in enumerate(progress.PHASES):
            self.assertIsNone(progress.observe(value,marker(phase),n,**arguments(phase)))
            self.assertEqual(value['complete'],n==len(progress.PHASES)-1)
        self.assertTrue(value['firmware_call_may_have_started'])
        for line in (marker('owned-after'),marker('memory-before')+' extra=1',
                     'FES_NANO_LOADER FAIL phase=memory-before',marker('foreign')):
            value=progress.state()
            self.assertIsNotNone(progress.observe(value,line,1,**arguments('memory-before')))
            for n in range(100):progress.observe(value,marker('memory-before'),n,**arguments('memory-before'))
            self.assertFalse(value['valid'] or value['complete'])
            self.assertEqual(value['records'],[])
        for name in ('boot_seen','memory_seen','owned_seen','later_handoff_seen','prior_valid'):
            value=progress.state();args=arguments('memory-before');args[name]=not args[name]
            self.assertIsNotNone(progress.observe(value,marker('memory-before'),1,**args),name)

    def test_complete_boot_rejects_missing_reordered_fragmented_and_flooded_receipts(self):
        expected=atf_descriptor('1'*64,90173)
        anchor='NOTICE:  FES_NANO_EL3_CAPTURE phase=before'
        descriptor=('FES_NANO_SPL ATF target=80000000 bl32=0 bl33=80200000 fdt=0 params=bff00008 '
                    'cold=21768 cookie=464e433200000002 el=3 mpidr=81000000 sctlr=1000 scr=501 daif=3c0 '
                    'bytes=90173 sha256='+'1'*64+'\r\n')
        raw=firmware_progress(fixture().replace(anchor,''.join(atf_progress())+descriptor+anchor))
        def decode(text):return decode_native_boot(text,BUILD,SEED,atf=expected,firmware_ram_trace=True)
        good=decode(raw)
        self.assertTrue(good['loader_progress']['complete'],good['errors'])
        self.assertTrue(good['complete'],good['errors'])
        for phase in progress.PHASES:
            line=marker(phase)+'\r\n'
            for damaged in (raw.replace(line,''),raw.replace(line,line*2),
                            raw.replace(line,line.replace('phase=','unknown=')),
                            raw.replace(line,''.join(marker(p)+'\r\n' for p in reversed(progress.PHASES)))):
                report=decode(damaged)
                self.assertFalse(report['complete'],phase)
                self.assertTrue(report['errors'],phase)
            # Complete progress is still insufficient before real ownership/entry.
            prefix=raw[:raw.index(line)]+line[:-2]
            report=decode(prefix)
            self.assertEqual(len(report['loader_progress']['records']),progress.PHASES.index(phase))
            self.assertFalse(report['complete'])
            self.assertFalse(report['core_load_acceptance'])
        for phase in ('cpu0-before','cpu3-before','owned-before'):
            line=marker(phase)+'\r\n';prefix=raw[:raw.index(line)]+line
            report=decode(prefix)
            self.assertTrue(report['loader_progress']['firmware_call_may_have_started'])
            self.assertFalse(report['loader_progress']['complete'])
        missing=re.sub(r'FES_NANO_LOADER[^\r\n]*\r\n','',raw)
        self.assertFalse(decode(missing)['complete'])
        flood=raw.replace(marker('memory-before')+'\r\n',(marker('memory-before')+'\r\n')*300)
        report=decode(flood)
        self.assertFalse(report['complete'])
        self.assertLessEqual(len(report['loader_progress']['records']),len(progress.PHASES))

    def test_actual_C_uart_bounds_snapshot_call_order_and_late_path(self):
        text=(ROOT/'platform/embedded/nano/uboot.c').read_text()
        def function(name):
            start=text.index('static void '+name+'(');brace=text.index('{',start);depth=1;end=brace+1
            while depth:
                depth+=(text[end]=='{')-(text[end]=='}');end+=1
            return text[start:end]
        leaf='\n'.join(function(name) for name in ('loader_uart_wait','loader_progress','snapshot'))
        self.assertEqual(leaf.count('asm volatile("wfe")'),2)
        self.assertEqual(leaf.count('asm volatile("mrs %0, cntfrq_el0" : "=r"(s->counter_hz));'),1)
        # Only architecture/park effects are modeled; production C bodies and
        # real polling/message limits are unchanged.
        leaf=leaf.replace('asm volatile("wfe")','longjmp(parked, 1)')
        leaf=leaf.replace('asm volatile("mrs %0, cntfrq_el0" : "=r"(s->counter_hz));','s->counter_hz = 400000000;')
        prelude=r'''
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <setjmp.h>
#include "handoff.h"
typedef unsigned long long u64;
#define UART1 0x10c02100UL
#define RSTMGR 0x10d11000UL
#define PSCI_AFFINITY_INFO_64 0xc4000004U
#define LOADER_PROGRESS(phase) loader_progress("FES_NANO_LOADER phase=" phase "\r\n")
static jmp_buf parked;
static unsigned reads, writes, smcs, fault;
static unsigned read_mpidr(void) { return 0x81000000; }
static unsigned readl(unsigned long p) {
    if (p == UART1 + 0x14) {
        reads++;
        if (fault == 1 || (fault == 3 && writes)) return 0;
        if (fault == 2) return ~0U;
        return 0x60;
    }
    return (unsigned)p;
}
static void writel(unsigned v, unsigned long p) {
    if (p != UART1) abort();
    writes++; putchar(v);
}
static int invoke_smc(unsigned id, u64 *args, int n, void *out, int count) {
    if (id != PSCI_AFFINITY_INFO_64 || n != 2 || out || count ||
        args[0] != ((u64)smcs << 8) || args[1] || smcs >= 4) abort();
    smcs++;
    if (fault == 4) longjmp(parked, 1);
    return smcs == 1 ? 0 : 1;
}
'''
        main=r'''
int main(int argc, char **argv) {
    struct fes_nano_state s;
    char unterminated[128]; memset(unterminated, 'A', sizeof(unterminated));
    fault = atoi(argv[1]);
    int park = setjmp(parked);
    if (!park) {
        if (fault == 5) loader_progress(unterminated);
        else if (fault == 6) snapshot(&s, 0);
        else if (fault == 0 || fault == 4) snapshot(&s, 1);
        else loader_progress("X");
    }
    fprintf(stderr, "%u %u %u %u\n", park, reads, writes, smcs);
    if (!park && (fault == 0 || fault == 6)) {
        if (s.affinity || s.counter_hz != 400000000 || s.cpu_state[0] ||
            s.cpu_state[1] != 1 || s.cpu_state[2] != 1 || s.cpu_state[3] != 1 ||
            s.per0_reset != RSTMGR+0x24 || s.per1_reset != RSTMGR+0x28 ||
            s.bridge_reset != RSTMGR+0x2c || s.uart_lcr != UART1+0x0c || s.uart_lsr != 0x60) abort();
        for (unsigned i=0;i<5;i++) if (s.watchdog_control[i] != 0x10d00200UL+i*0x100) abort();
    }
    return 0;
}
'''
        with tempfile.TemporaryDirectory() as directory:
            source=Path(directory)/'case.c';binary=Path(directory)/'case'
            source.write_text(prelude+leaf+main)
            subprocess.run(['cc','-std=gnu11','-O2','-Wall','-Werror','-I',str(ROOT/'platform/embedded/nano'),str(source),'-o',str(binary)],check=True,capture_output=True)
            results={}
            for case in range(7):
                run=subprocess.run([str(binary),str(case)],check=True,capture_output=True,text=True,timeout=5)
                results[case]=(run.stdout,tuple(map(int,run.stderr.split())))
        self.assertEqual([line.removeprefix('FES_NANO_LOADER phase=') for line in results[0][0].splitlines()],list(progress.PHASES[3:19]))
        self.assertEqual(results[0][1][0],0);self.assertEqual(results[0][1][3],4)
        self.assertEqual(results[1],('',(1,100000,0,0)))
        self.assertEqual(results[2],('',(1,1,0,0)))
        self.assertEqual(results[3],('X',(1,100001,1,0)))
        self.assertTrue(results[4][0].endswith(marker('cpu0-before')+'\n'))
        self.assertEqual(results[4][1][0],1);self.assertEqual(results[4][1][3],1)
        self.assertEqual(results[5][0],'A'*128);self.assertEqual(results[5][1],(1,128,128,0))
        self.assertEqual(results[6],('',(0,1,0,4)))


if __name__=='__main__':unittest.main()
