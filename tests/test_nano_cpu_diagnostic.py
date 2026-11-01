"""CPU observation must park for either admission result before DDR/storage."""
import importlib.util
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
HERE=ROOT/'platform/embedded/nano'
spec=importlib.util.spec_from_file_location('nano_cpu_diagnostic',HERE/'cpu_diagnostic.py')
cpu=importlib.util.module_from_spec(spec);spec.loader.exec_module(cpu)

BOARD='''void board_init_f(unsigned dummy)
{
    int ret;
    struct udevice *dev;
    (void)dummy;
\tfes_nano_spl_early();
\tret = spl_early_init();
    if (ret) hang();
\tsocfpga_get_managers_addr();
\tsysmgr_pinmux_init();
\ttimer_init();
\tmbox_init();
\tmbox_hps_stage_notify(HPS_EXECUTION_STATE_FSBL);
\tret = uclass_get_device(UCLASS_CLK, 0, &dev);
    if (ret) hang();
\tpreloader_console_init();
\tfes_nano_spl_report();
    uclass_get_device(UCLASS_RAM,0,0);
    forbidden_storage_initialization();
}
'''

def source_fixture(base):
    soc=base/'arch/arm/mach-socfpga';soc.mkdir(parents=True)
    (soc/'fes_nano_spl.c').write_bytes((HERE/'spl_cold.c').read_bytes())
    (soc/'spl_agilex5.c').write_text(BOARD)
    (soc/'Makefile').write_text('obj-y += spl_agilex5.o\n')
    (base/'common/spl').mkdir(parents=True)
    (base/'common/spl/spl.c').write_text('\t\tjump_to_image = &spl_invoke_atf;\n')
    (base/'Makefile').write_text('dts/dt.dtb: dtbs_prepare u-boot\n')
    return soc

class CPUOnlyTests(unittest.TestCase):
    def test_actual_report_always_parks_and_later_device_init_is_removed(self):
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp);soc=source_fixture(base)
            report=soc/'fes_nano_spl.c'
            board=soc/'spl_agilex5.c'
            cpu.contain_source(base)
            source=base/'test.c';binary=base/'test'
            source.write_text(r'''
#include <assert.h>
#include <setjmp.h>
#include <stdint.h>
#include <stdlib.h>
#include "nano_cold.h"
#include "cpu_trace.h"
#include <stdio.h>
static struct fes_nano_cold source;
static jmp_buf stop;
static unsigned consoles,reads,parks,core,mode,clock_ready,serials,stages[32],stage_count;
struct udevice { unsigned unused; };
enum { UCLASS_CLK=1, HPS_EXECUTION_STATE_FSBL=0 };
void fes_nano_cpu_trace(unsigned stage,unsigned value){
    (void)value;assert(stage_count<32);stages[stage_count++]=stage;
}
void fes_nano_cpu_trace_serial(void){assert(clock_ready);serials++;}
static void fes_nano_spl_early(void){assert(stage_count==1 && stages[0]==FES_CPU_C_ENTRY);}
static int spl_early_init(void){return mode==1 ? -5 : 0;}
static void socfpga_get_managers_addr(void){}
static void sysmgr_pinmux_init(void){}
static void timer_init(void){}
static void mbox_init(void){}
static void mbox_hps_stage_notify(unsigned value){assert(value==HPS_EXECUTION_STATE_FSBL);}
static int uclass_get_device(unsigned cl,unsigned n,struct udevice **out){
    (void)out;assert(cl==UCLASS_CLK && !n);clock_ready=mode!=2;return clock_ready ? 0 : -7;
}
static uint64_t read_mpidr(void){return 0x80000000ULL|((uint64_t)core<<8);}
static int fes_cold_hw_read(void *p,unsigned r,uint32_t *out){
    (void)p;reads++;
    if(r==FES_COLD_CAUSE)*out=4;
    else if(r==FES_COLD_CPUS)*out=15;
    else {assert(r==FES_COLD_RELEASE);*out=0;}
    return 0;
}
static uint32_t mmio(uintptr_t a){assert(a==0x10d11008);return 0;}
#define FES_COLD_READ32(a) mmio(a)
static void preloader_console_init(void){consoles++;}
static void fes_nano_cpu_diag_park(void){parks++;fes_nano_cpu_trace(FES_CPU_PARK,0);longjmp(stop,1);}
void hang(void){assert(mode);longjmp(stop,2);}
''' + cpu.function(report.read_text(),'fes_nano_spl_report') +
                cpu.function(board.read_text(),'board_init_f') + r'''
int main(int argc,char **argv){
    assert(argc==4);source.failed=atoi(argv[1]);core=atoi(argv[2]);mode=atoi(argv[3]);
    source.record.first=(struct fes_nano_cold_sample){4,0xff7fbfff,0x4f,15,0,4};
    if(!setjmp(stop)){board_init_f(0);assert(!"report returned");}
    if(!mode){
        assert(parks==1 && consoles==1 && reads==3 && serials==2 && stage_count==18);
        for(unsigned i=0;i<18;i++)assert(stages[i]==i+1);
    }else{
        assert(!parks && !consoles && !reads && !serials);
        assert(stages[stage_count-1]==(mode==1 ? FES_CPU_DM_ERROR : FES_CPU_CLOCK_ERROR));
    }
    assert(source.failed==(unsigned)atoi(argv[1]));
    puts("CPU_ONLY_PARK_PASS");return 0;
}
''')
            r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',
                              '-I'+str(ROOT/'platform/embedded/firmware'),'-I'+str(HERE),str(source),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(r.returncode,0,r.stderr)
            for failed in (0,1):
                for core in (0,2):
                    for mode in (0,1,2):
                        with self.subTest(failed=failed,core=core,mode=mode):
                            r=subprocess.run([str(binary),str(failed),str(core),str(mode)],capture_output=True,text=True)
                            self.assertEqual(r.returncode,0,r.stderr)
                            self.assertIn('CPU_ONLY_PARK_PASS',r.stdout)
                            if not mode:
                                self.assertIn('valid='+str(1-failed),r.stdout)
                                self.assertIn('core='+str(core),r.stdout)

    def test_changed_report_or_ddr_boundary_rejects(self):
        for changed in ('report','board','checkpoint','late'):
            with tempfile.TemporaryDirectory() as temp:
                base=Path(temp);soc=source_fixture(base)
                if changed=='report':(soc/'fes_nano_spl.c').write_text('')
                if changed=='board':(soc/'spl_agilex5.c').write_text('void board_init_f(void) {\n\tfes_nano_spl_report();\n}\n')
                if changed=='checkpoint':(soc/'spl_agilex5.c').write_text(BOARD.replace('mbox_init();','unknown();'))
                if changed=='late':(base/'common/spl/spl.c').write_text('changed entry')
                with self.assertRaises(ValueError):cpu.contain_source(base)

    def test_trace_uses_ocram_and_guards_and_bounds_uart(self):
        with tempfile.TemporaryDirectory() as temp:
            source=Path(temp)/'trace.c';binary=Path(temp)/'trace'
            source.write_text(r'''
#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include "cpu_trace.h"
static unsigned reads,writes,reset,lcr=3,ready=1;
static char output[8192];
static uint32_t read32(uintptr_t a){
    reads++;
    if(a==FES_CPU_TRACE_RSTMGR_PER1)return reset;
    if(a==FES_CPU_TRACE_UART1+0x0c)return lcr;
    assert(a==FES_CPU_TRACE_UART1+0x14);return ready ? 0x20 : 0;
}
static void write32(uintptr_t a,uint32_t value){
    assert(a==FES_CPU_TRACE_UART1 && writes<sizeof(output)-1);output[writes++]=(char)value;
}
#define FES_CPU_TRACE_HOST_TEST 1
#define FES_CPU_DIAG_ID "0123456789abcdef"
#define FES_CPU_TRACE_READ(a) read32(a)
#define FES_CPU_TRACE_WRITE(a,v) write32(a,v)
#define FES_CPU_TRACE_MPIDR() 0x80000200ULL
#define FES_CPU_TRACE_EL() 3U
#define FES_CPU_TRACE_BARRIER() ((void)0)
#include "cpu_trace.c"
int main(int argc,char **argv){
    assert(argc==2);unsigned mode=(unsigned)(argv[1][0]-'0');
    fes_nano_cpu_trace(FES_CPU_C_ENTRY,0);
    fes_nano_cpu_trace(FES_CPU_SAMPLE_DONE,0);
    assert(reads==0 && writes==0 && fes_nano_cpu_trace_record.count==2);
    assert(fes_nano_cpu_trace_record.mpidr==0x80000200ULL && fes_nano_cpu_trace_record.el==3);
    if(mode==1)reset=1U<<17;
    if(mode==2)lcr=0x83;
    if(mode==3)ready=0;
    fes_nano_cpu_trace_serial();
    if(mode==1 || mode==2){
        assert(!writes && reads==(mode==1 ? 1U : 2U));
        assert(fes_nano_cpu_trace_record.uart_skipped==1);
    }else if(mode==3){
        assert(!writes && reads==FES_CPU_TRACE_POLL_LIMIT+2);
        assert(fes_nano_cpu_trace_record.uart_failed && !fes_nano_cpu_trace_record.uart_active);
        unsigned before=reads;fes_nano_cpu_trace_serial();assert(reads==before);
    }else{
        assert(strstr(output,"id=0123456789abcdef stage=01 value=00000000 mpidr=0000000080000200 el=3\r\n"));
        assert(strstr(output,"stage=02 value=00000000"));
        fes_nano_cpu_trace(FES_CPU_DM_ERROR,(unsigned)-5);
        assert(strstr(output,"stage=e0 value=fffffffb"));
        assert(fes_nano_cpu_trace_record.emitted==3);
    }
    for(unsigned i=0;i<40;i++)fes_nano_cpu_trace(0x20+i,i);
    assert(fes_nano_cpu_trace_record.count==32 && fes_nano_cpu_trace_record.overflow);
    puts("TRACE_PASS");return 0;
}
''')
            r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined',
                              '-fno-sanitize-recover=all','-I'+str(HERE),str(source),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(r.returncode,0,r.stderr)
            for mode in range(4):
                with self.subTest(mode=mode):
                    r=subprocess.run([str(binary),str(mode)],capture_output=True,text=True)
                    self.assertEqual(r.returncode,0,r.stderr)
                    self.assertIn('TRACE_PASS',r.stdout)

    def test_partial_and_mismatched_traces_never_imply_admission(self):
        identity='0123456789abcdef'
        def line(stage,seen=identity,mpidr='0000000080000200'):
            return f'FES_NANO_CPU_TRACE id={seen} stage={stage:02x} value=00000000 mpidr={mpidr} el=3\r\n'
        full=''.join(line(i) for i in range(1,19))
        r=cpu.decode_trace(full,identity)
        self.assertTrue(r['complete']);self.assertEqual(r['last_checkpoint']['name'],'park')
        cases=('', ''.join(line(i) for i in range(1,14)),full[:-8],full+line(1,'fedcba9876543210'),
               line(2)+line(1),''.join(line(i) for i in range(1,18))+line(18,mpidr='0000000080000000'))
        for data in cases:
            with self.subTest(data=data[-100:]):
                r=cpu.decode_trace(data,identity)
                self.assertFalse(r['complete']);self.assertFalse(r['native_admission'])
        partial=cpu.decode_trace(''.join(line(i) for i in range(1,14)),identity)
        self.assertTrue(partial['sequence_valid']);self.assertEqual(partial['last_checkpoint']['name'],'clock-begin')
        error=cpu.decode_trace(''.join(line(i) for i in range(1,4))+line(0xe0),identity)
        self.assertTrue(error['sequence_valid']);self.assertEqual(error['last_checkpoint']['name'],'dm-error')
        self.assertFalse(error['complete']);self.assertFalse(error['native_admission'])

if __name__=='__main__':unittest.main()
