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

class CPUOnlyTests(unittest.TestCase):
    def test_actual_report_always_parks_and_later_device_init_is_removed(self):
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp);soc=base/'arch/arm/mach-socfpga';soc.mkdir(parents=True)
            report=soc/'fes_nano_spl.c';report.write_bytes((HERE/'spl_cold.c').read_bytes())
            board=soc/'spl_agilex5.c'
            (base/'Makefile').write_text('dts/dt.dtb: dtbs_prepare u-boot\n')
            board.write_text('''void board_init_f(unsigned dummy)
{
    (void)dummy;
    preloader_console_init();
\tfes_nano_spl_report();
    uclass_get_device(UCLASS_RAM,0,0);
    forbidden_storage_initialization();
}
''')
            cpu.contain_source(base)
            source=base/'test.c';binary=base/'test'
            source.write_text(r'''
#include <assert.h>
#include <setjmp.h>
#include <stdint.h>
#include <stdlib.h>
#include "nano_cold.h"
#include <stdio.h>
static struct fes_nano_cold source;
static jmp_buf stop;
static unsigned consoles,reads,parks,core;
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
static void fes_nano_cpu_diag_park(void){parks++;longjmp(stop,1);}
void hang(void){abort();}
''' + cpu.function(report.read_text(),'fes_nano_spl_report') +
                cpu.function(board.read_text(),'board_init_f') + r'''
int main(int argc,char **argv){
    assert(argc==3);source.failed=atoi(argv[1]);core=atoi(argv[2]);
    source.record.first=(struct fes_nano_cold_sample){4,0xff7fbfff,0x4f,15,0,4};
    if(!setjmp(stop)){board_init_f(0);assert(!"report returned");}
    assert(parks==1 && consoles==1 && reads==3);
    assert(source.failed==(unsigned)atoi(argv[1]));
    puts("CPU_ONLY_PARK_PASS");return 0;
}
''')
            r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',
                              '-I'+str(ROOT/'platform/embedded/firmware'),str(source),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(r.returncode,0,r.stderr)
            for failed in (0,1):
                for core in (0,2):
                    with self.subTest(failed=failed,core=core):
                        r=subprocess.run([str(binary),str(failed),str(core)],capture_output=True,text=True)
                        self.assertEqual(r.returncode,0,r.stderr)
                        self.assertIn('CPU_ONLY_PARK_PASS',r.stdout)
                        self.assertIn('valid='+str(1-failed),r.stdout)
                        self.assertIn('core='+str(core),r.stdout)

    def test_changed_report_or_ddr_boundary_rejects(self):
        for changed in ('report','board'):
            with tempfile.TemporaryDirectory() as temp:
                base=Path(temp);soc=base/'arch/arm/mach-socfpga';soc.mkdir(parents=True)
                (soc/'fes_nano_spl.c').write_text('' if changed=='report' else (HERE/'spl_cold.c').read_text())
                (soc/'spl_agilex5.c').write_text('void board_init_f(void) {\n\tfes_nano_spl_report();\n}\n')
                with self.assertRaises(ValueError):cpu.contain_source(base)

if __name__=='__main__':unittest.main()
