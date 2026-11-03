"""Actual SPL/BL31 cold record and SD-stop implementation with injected MMIO."""
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
ROOT=Path(__file__).resolve().parents[1]
FW=ROOT/'platform/embedded/firmware'

def cold_console_fixture():
    return r'''
static uint32_t cold_console_read(uintptr_t address) { assert(address==0x10c02114UL);return 0x60; }
static void cold_console_write(uintptr_t address,uint32_t value) {
    assert(address==0x10c02100UL && value<=255);putchar((unsigned char)value);
}
#define FES_SPL_CONSOLE_READ(a) cold_console_read(a)
#define FES_SPL_CONSOLE_WRITE(a,v) cold_console_write(a,v)
#define FES_SPL_CONSOLE_PARK() hang()
''' + (ROOT/'platform/embedded/nano/spl_console.inc').read_text()

class NanoColdTests(unittest.TestCase):
    def test_actual_contained_console_bounds_io_formatting_and_failure_without_callbacks(self):
        fragment=(ROOT/'platform/embedded/nano/spl_console.inc').read_text()
        prefix=r'''
#include <assert.h>
#include <setjmp.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
static unsigned mode,reads,writes,parked;
static char wire[1024];
static jmp_buf stopped;
static uint32_t read_uart(uintptr_t address) {
    assert(address==0x10c02114UL);reads++;
    if(mode==2 || (mode==5 && reads<=3) || (mode==10 && writes==2))return 0;
    if(mode==3)return 0xffffffffU;
    return mode==4 ? 0x20U : 0x60U;
}
static void write_uart(uintptr_t address,uint32_t value) {
    assert(address==0x10c02100UL && value<=255 && writes<sizeof(wire));wire[writes++]=(char)value;
}
static void park(void) {parked++;longjmp(stopped,1);}
static int format_buffer(char *p,size_t size,const char *format,va_list args) {
    if(mode==9)return -1;
    return vsnprintf(p,size,format,args);
}
#define vsnprintf format_buffer
#define FES_SPL_CONSOLE_READ(a) read_uart(a)
#define FES_SPL_CONSOLE_WRITE(a,v) write_uart(a,v)
#define FES_SPL_CONSOLE_PARK() park()
'''
        main=r'''
int main(int argc,char **argv) {
    assert(argc==2);mode=strtoul(argv[1],0,0);char large[385];memset(large,'A',384);large[384]=0;
    if(!setjmp(stopped)) {
        switch(mode) {
        case 0:fes_nano_spl_cold_printf("%s:%llx:%u\n","C",0x123456789abcdef0ULL,17U);break;
        case 1:fes_nano_spl_cold_puts("ok\n");break;
        case 6:fes_nano_spl_cold_printf("%s",large);break;
        case 7:fes_nano_spl_cold_puts(large);break;
        case 8:fes_nano_spl_cold_puts(NULL);break;
        case 10:fes_nano_spl_cold_puts("abcd");break;
        case 11:fes_nano_spl_cold_puts("\200\n");break;
        case 12:fes_nano_spl_cold_puts("");break;
        case 13:fes_nano_spl_cold_printf("%s","");break;
        case 14:fes_nano_spl_cold_printf(NULL);break;
        default:fes_nano_spl_cold_printf("%u",7U);break;
        }
        assert(mode==0 || mode==1 || mode==5 || mode==11 || mode==12 || mode==13);
        assert(!parked);
    } else assert(mode==2 || mode==3 || mode==4 || (mode>=6 && mode<=10) || mode==14);
    assert(parked<=1);
    if(mode==0)assert(!strcmp(wire,"C:123456789abcdef0:17\r\n"));
    if(mode==1)assert(!strcmp(wire,"ok\r\n"));
    if(mode==2)assert(!writes && reads==100000);
    if(mode==3)assert(!writes && reads==1);
    if(mode==4)assert(writes==1 && reads==100001);
    if(mode==5)assert(writes==1 && reads==5);
    if(mode>=6 && mode<=9)assert(!writes && !reads);
    if(mode==10)assert(writes==2 && reads==100002);
    if(mode==11)assert(writes==3 && (unsigned char)wire[0]==0x80 && wire[1]=='\r' && wire[2]=='\n');
    if(mode==12 || mode==13 || mode==14)assert(!writes && !reads);
    puts("CONTAINED_CONSOLE_PASS");return 0;
}
'''
        with tempfile.TemporaryDirectory() as temp:
            source=Path(temp)/'console.c';binary=Path(temp)/'console'
            source.write_text(prefix+fragment+main)
            r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',str(source),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(r.returncode,0,r.stderr)
            for mode in range(15):
                with self.subTest(mode=mode):
                    r=subprocess.run([str(binary),str(mode)],capture_output=True,text=True)
                    self.assertEqual(r.returncode,0,r.stderr)
                    self.assertIn('CONTAINED_CONSOLE_PASS',r.stdout)
    def test_spl_atf_caller_preserves_order_and_full_width_arguments(self):
        sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
        try:import prepare_uboot
        finally:sys.path.pop(0)
        fixture=r'''
#include <assert.h>
#include <setjmp.h>
#include <stdint.h>
#define __noreturn __attribute__((noreturn))
#define CONFIG_IS_ENABLED(name) 0
#define SPSR_EXCEPTION_MASK 0x3c0
struct bl31_params { unsigned dummy; };
static struct bl31_params parameters;
static unsigned stage;
static uintptr_t a32,a33,afdt,target_address;
static jmp_buf stopped;
static void raw_write_daif(unsigned v) { assert(stage++==0 && v==0x3c0); }
static void dcache_disable(void) { assert(stage++==1); }
static struct bl31_params *bl2_plat_get_bl31_params(uintptr_t b32,uintptr_t b33,uintptr_t fdt) {
    assert(stage==0 && b32==a32 && b33==a33 && fdt==afdt);return &parameters;
}
#define bl2_plat_get_bl31_params_v2 bl2_plat_get_bl31_params
typedef void __noreturn (*atf_entry_t)(struct bl31_params *params, void *plat_params);
static void __noreturn bl31_entry(uintptr_t bl31_entry,uintptr_t bl32_entry,uintptr_t bl33_entry,uintptr_t fdt_addr)
{
    atf_entry_t atf_entry=(atf_entry_t)bl31_entry;
    void *bl31_params=bl2_plat_get_bl31_params(bl32_entry,bl33_entry,fdt_addr);
    raw_write_daif(SPSR_EXCEPTION_MASK);
    if (!CONFIG_IS_ENABLED(SYS_DCACHE_OFF)) dcache_disable();
	atf_entry(bl31_params, (void *)fdt_addr);
}
'''
        generated=prepare_uboot.atf_source(fixture).replace('#include <nano_cold.h>\n','')
        suffix=r'''
#define COOKIE 0x464e433200000002UL
const void *fes_nano_spl_handoff(void) { assert(stage++==2);return (const void *)0x1000021768UL; }
void fes_nano_spl_atf_trace(uintptr_t target,uintptr_t b32,uintptr_t b33,uintptr_t fdt,uintptr_t p,uintptr_t cold) {
    assert(stage++==3 && target==target_address && b32==a32 && b33==a33 && fdt==afdt);
    assert(p==(uintptr_t)&parameters && cold==0x1000021768UL);
}
static void __noreturn destination(struct bl31_params *p,void *fdt,const void *cold,unsigned long cookie) {
    assert(stage++==4 && p==&parameters && fdt==(void *)afdt && cold==(void *)0x1000021768UL && cookie==COOKIE);
    longjmp(stopped,1);
}
int main(void) {
    a32=0x1111111122222222UL;a33=0x3333333344444444UL;afdt=0x5555555566666666UL;
    target_address=(uintptr_t)destination;
    if(!setjmp(stopped))bl31_entry(target_address,a32,a33,afdt);
    assert(stage==5);return 0;
}
'''
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp);source=base/'caller.c';binary=base/'caller'
            source.write_text('#define FES_NANO_COLD_COOKIE 0x464e433200000002UL\n'+generated+suffix)
            r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',str(source),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(r.returncode,0,r.stderr)
            r=subprocess.run([str(binary)],capture_output=True,text=True)
            self.assertEqual(r.returncode,0,r.stderr)
        for bad in (fixture+fixture,fixture.replace('atf_entry(bl31_params','other_entry(bl31_params')):
            with self.assertRaises(ValueError):prepare_uboot.atf_source(bad)

    def test_actual_loaded_atf_check_rejects_arguments_state_and_every_corrupt_byte(self):
        # The SHA API substitute checks the complete fixed fixture. Algorithm
        # selection and linked calls are checked separately in the vendor build.
        source=(ROOT/'platform/embedded/nano/spl_cold.c').read_text()
        fragment=source[source.index('void fes_nano_spl_atf_trace('):source.index('void fes_nano_spl_early(')]
        for register in ('sctlr','scr','daif'):
            instruction='asm volatile("mrs %0, '+register+'_el3" : "=r"('+register+'));' if register!='daif' else 'asm volatile("mrs %0, daif" : "=r"(daif));'
            self.assertEqual(fragment.count(instruction),1)
            fragment=fragment.replace(instruction,register+'=sample_'+register+';samples++;')
        prefix=r'''
#define _GNU_SOURCE
#include <assert.h>
#include <setjmp.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include "nano_cold.h"
#define CONFIG_SPL_BSS_START_ADDR 0xbff00000UL
#define CONFIG_SPL_BSS_MAX_SIZE 0x100000UL
#define FES_NANO_ATF_SIZE atf_size
#define FES_NANO_ATF_SHA {0x5a}
static unsigned long atf_size=16;
static struct fes_nano_cold source;
static struct fes_nano_cold_record fes_nano_spl_record;
static unsigned mode,samples,hash_calls;
static uint64_t sample_sctlr=0x1000,sample_scr=0x501,sample_daif=0x3c0;
static jmp_buf stopped;
static const unsigned char original[16]="loaded-atf-test";
typedef struct { unsigned step; } sha256_context;
#define puts(text) fputs((text),stdout)
static unsigned current_el(void) { return mode==1 ? 2 : 3; }
static uint64_t read_mpidr(void) { return mode==15 ? 0x81000200 : 0x81000000; }
static void sha256_starts(sha256_context *s) { assert(samples==3);if(mode==36)longjmp(stopped,1);s->step=1; }
static void sha256_update(sha256_context *s,const uint8_t *p,uint32_t n) {
    assert(s->step==1 && p==(void *)0x80000000UL && n==16);if(mode==37)longjmp(stopped,1);s->step=2;hash_calls++;
}
static void sha256_finish(sha256_context *s,uint8_t *hash) {
    assert(s->step==2);if(mode==38)longjmp(stopped,1);s->step=3;memset(hash,0,32);
    hash[0]=memcmp((void *)0x80000000UL,original,16)?0xa5:0x5a;
}
static void hang(void) { longjmp(stopped,1); }
'''
        main=r'''
int main(int argc,char **argv) {
    assert(argc==2);mode=strtoul(argv[1],0,0);
    assert(mmap((void *)0x80000000UL,4096,PROT_READ|PROT_WRITE,MAP_PRIVATE|MAP_ANONYMOUS|MAP_FIXED_NOREPLACE,-1,0)==(void *)0x80000000UL);
    memcpy((void *)0x80000000UL,original,16);source.finish_stage=FES_COLD_FINISH_COMPLETE;
    uintptr_t target=0x80000000UL,b32=0,b33=0x80200000UL,fdt=0,p=0xbff00008UL,cold=(uintptr_t)&fes_nano_spl_record;
    switch(mode) {
    case 2:source.failed=1;break;case 3:source.finish_stage=0;break;
    case 4:target+=4;break;case 5:b32=1;break;case 6:b33+=4;break;case 7:fdt=1;break;
    case 8:p++;break;case 9:p=0xbfeffff8UL;break;case 10:p=0xc0000000UL-56;break;case 11:cold++;break;
    case 12:sample_sctlr|=1;break;case 13:sample_sctlr|=4;break;case 14:sample_daif=0;break;
    case 16:atf_size=0;break;case 17:atf_size=(2UL<<20)+1;break;case 18:b33+=1UL<<32;break;
    }
    if(mode>=20 && mode<36)((uint8_t *)0x80000000UL)[mode-20]^=1;
    if(!setjmp(stopped)) {
        fes_nano_spl_atf_trace(target,b32,b33,fdt,p,cold);
        assert(!mode && samples==3 && hash_calls==1);
    } else {
        assert(mode && hash_calls==((mode>=20 && mode<36) || mode==38));
        assert(samples==((mode>=12 && mode<=15) || mode>=20 ? 3U : 0U));
    }
    puts("ATF_CHECK_TEST_PASS\n");return 0;
}
'''
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp);test=base/'loaded.c';binary=base/'loaded';test.write_text(prefix+cold_console_fixture()+fragment+main)
            r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all','-I'+str(FW),str(test),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(r.returncode,0,r.stderr)
            for mode in list(range(19))+list(range(20,39)):
                with self.subTest(mode=mode):
                    r=subprocess.run([str(binary),str(mode)],capture_output=True,text=True)
                    self.assertEqual(r.returncode,0,r.stderr)
                    self.assertIn('ATF_CHECK_TEST_PASS',r.stdout)
                    self.assertEqual('FES_NANO_SPL ATF target=' in r.stdout,mode==0)
                    self.assertEqual('FES_NANO_SPL REJECT ' in r.stdout,0<mode<36)
                    entry='FES_NANO_SPL ATF_STEP phase=entry\n'
                    before='FES_NANO_SPL ATF_STEP phase=hash-before\n'
                    after='FES_NANO_SPL ATF_STEP phase=hash-after\n'
                    self.assertEqual(r.stdout.count(entry),1)
                    hashed=mode==0 or mode>=20
                    self.assertEqual(r.stdout.count(before),int(hashed))
                    self.assertEqual(r.stdout.count(after),int(hashed and mode<36))
                    if hashed:self.assertLess(r.stdout.index(entry),r.stdout.index(before))
                    if hashed and mode<36:self.assertLess(r.stdout.index(before),r.stdout.index(after))

    def test_secure_capture_trace_preserves_vendor_order_and_all_arguments(self):
        sys.path.insert(0,str(FW))
        try:import prepare_nano_owned
        finally:sys.path.remove(str(FW))
        fixture='''void bl31_early_platform_setup2(u_register_t arg0, u_register_t arg1,
                                u_register_t arg2, u_register_t arg3)
{
\tstatic console_t console;
\tmmio_write_64(PLAT_SEC_ENTRY, PLAT_SEC_WARM_ENTRY);
\tconsole_16550_register(PLAT_INTEL_UART_BASE, PLAT_UART_CLOCK, PLAT_BAUDRATE, &console);
\tconsole_set_scope(&console,
\t\t(CONSOLE_FLAG_BOOT | CONSOLE_FLAG_RUNTIME | CONSOLE_FLAG_CRASH));
\tsetup_smmu_stream_id();
\tcheck_previous_arguments(arg0, arg1);
}
'''
        generated=prepare_nano_owned.cold_setup_source(fixture)
        console='\tconsole_set_scope(&console,\n\t\t(CONSOLE_FLAG_BOOT | CONSOLE_FLAG_RUNTIME | CONSOLE_FLAG_CRASH));\n'
        before,tail=generated.split(console)
        stream=tail.index('\tsetup_smmu_stream_id();')
        restored=(before+console+tail[stream:]).replace('extern void fes_nano_cold_capture(uintptr_t, uint64_t);\n','')
        self.assertEqual(restored,fixture)
        for bad in (fixture.replace('CONSOLE_FLAG_CRASH','CONSOLE_FLAG_OTHER'),
                    fixture.replace('bl31_early_platform_setup2','other_setup'),fixture+fixture,
                    fixture.replace('\tsetup_smmu_stream_id();','\tfes_nano_cold_capture(arg2, arg3);')):
            with self.assertRaisesRegex(ValueError,'cold early capture context'):
                prepare_nano_owned.cold_setup_source(bad)
        prefix=r'''
#include <assert.h>
#include <stdarg.h>
#include <stdint.h>
#include <string.h>
typedef uint64_t u_register_t;
typedef unsigned console_t;
#define PLAT_SEC_ENTRY 1
#define PLAT_SEC_WARM_ENTRY 2
#define PLAT_INTEL_UART_BASE 3
#define PLAT_UART_CLOCK 4
#define PLAT_BAUDRATE 5
#define CONSOLE_FLAG_BOOT 1
#define CONSOLE_FLAG_RUNTIME 2
#define CONSOLE_FLAG_CRASH 4
static unsigned stage;
static uint64_t pointer,cookie;
void mmio_write_64(unsigned a,unsigned b) {assert(!stage++ && a==1 && b==2);}
void console_16550_register(unsigned a,unsigned b,unsigned c,console_t *p) {assert(stage++==1 && a==3 && b==4 && c==5 && p);}
void console_set_scope(console_t *p,unsigned flags) {assert(stage++==2 && p && flags==7);}
void trace_notice(const char *format,...) {
    assert(stage==(strstr(format,"phase=before")?3U:5U));stage++;
    va_list args;va_start(args,format);
    assert(va_arg(args,unsigned long long)==pointer && va_arg(args,unsigned long long)==cookie);va_end(args);
}
#define NOTICE(...) trace_notice(__VA_ARGS__)
void fes_nano_cold_capture(uintptr_t p,uint64_t c) {assert(stage++==4 && p==pointer && c==cookie);}
void setup_smmu_stream_id(void) {assert(stage++==6);}
void check_previous_arguments(uint64_t a,uint64_t b) {assert(stage++==7 && a==17 && b==19);}
'''
        main=r'''
int main(void) {
    const uint64_t pointers[]={0x21768,0x1000,0,0x100000021768ULL};
    for(unsigned i=0;i<4;i++) {
        stage=0;pointer=pointers[i];cookie=0x464e433200000002ULL^(i==2);
        bl31_early_platform_setup2(17,19,pointer,cookie);assert(stage==8);
    }
    return 0;
}
'''
        with tempfile.TemporaryDirectory() as temp:
            source=Path(temp)/'setup.c';binary=Path(temp)/'setup';source.write_text(prefix+generated+main)
            result=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',str(source),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            result=subprocess.run([str(binary)],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)

    def test_produced_psci_denies_secondary_release_without_retiring_owner(self):
        sys.path.insert(0,str(FW))
        try:
            import prepare_nano_owned
        finally:
            sys.path.remove(str(FW))
        # The old handler has observable vector/power/reset side effects. Run
        # both transformed modes, so a mere added invalidation cannot pass as
        # prevention of secondary release. The real vendor TU also cross-builds.
        fixture=r'''
#include <assert.h>
#include <stdint.h>
typedef uint64_t u_register_t;
#define PSCI_E_DENIED (-3)
#define PSCI_E_INTERN_FAIL (-6)
static unsigned invalidations,writes,releases;
void fes_nano_bridge_platform_invalidate(void) { invalidations++; }
void mmio_write_32(uintptr_t a,uint32_t v) { (void)a;(void)v;writes++; }
void bl31_plat_set_secondary_cpu_entrypoint(unsigned core) { (void)core;releases++; }
int socfpga_pwr_domain_on(u_register_t mpidr)
{
    mmio_write_32(0x10d14050,(uint32_t)mpidr);
    bl31_plat_set_secondary_cpu_entrypoint((unsigned)mpidr);
    return 0;
}
void socfpga_pwr_domain_off(void) {}
void socfpga_pwr_domain_suspend(void) {}
void socfpga_pwr_domain_on_finish(void) {}
void socfpga_pwr_domain_suspend_finish(void) {}
void socfpga_system_off(void) {}
void socfpga_system_reset(void) {}
int socfpga_system_reset2(void) { return 0; }
'''
        frontend=r'''
static unsigned bookkeeping;
int psci_cpu_on_start(u_register_t target,void *ep) {
    (void)ep;bookkeeping++;return socfpga_pwr_domain_on(target);
}
int psci_cpu_on(u_register_t target_cpu,uintptr_t entrypoint,u_register_t context_id)
{
    (void)entrypoint;(void)context_id;
    return psci_cpu_on_start(target_cpu,0);
}
'''
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp)
            for cold in (False,True):
                source=base/('cold.c' if cold else 'owned.c');binary=source.with_suffix('')
                main=r'''
int main(void) {
    const uint64_t ids[]={0,0x100,0x200,0x300,0x81000000,0xffffffffffffffffULL};
    for(unsigned i=0;i<sizeof(ids)/sizeof(ids[0]);i++) {
'''+('''
        assert(fes_nano_cold_cpu_release_locked()==1);
        assert(psci_cpu_on(ids[i],0x84000000,17)==PSCI_E_DENIED);
        assert(socfpga_pwr_domain_on(ids[i])==PSCI_E_INTERN_FAIL);
        assert(!bookkeeping && !invalidations && !writes && !releases);
''' if cold else '''
        assert(!psci_cpu_on(ids[i],0x84000000,17));
        assert(bookkeeping==i+1 && invalidations==i+1 && writes==i+1 && releases==i+1);
''')+r'''
    }
    invalidations=0;
    socfpga_pwr_domain_off();socfpga_pwr_domain_suspend();
    socfpga_pwr_domain_on_finish();socfpga_pwr_domain_suspend_finish();
    socfpga_system_off();socfpga_system_reset();socfpga_system_reset2();
    assert(invalidations==7);
    return 0;
}
'''
                source.write_text(prepare_nano_owned.psci_source(fixture,cold_serial=cold)+
                                  (prepare_nano_owned.psci_frontend_source(frontend) if cold else frontend)+main)
                r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',
                                  '-I'+str(FW),str(source),'-o',str(binary)],capture_output=True,text=True)
                self.assertEqual(r.returncode,0,r.stderr)
                r=subprocess.run([str(binary)],capture_output=True,text=True)
                self.assertEqual(r.returncode,0,r.stderr)
        with self.assertRaisesRegex(ValueError,'owned PSCI context changed'):
            prepare_nano_owned.psci_source(fixture.replace('socfpga_system_reset2','unexpected_reset'),cold_serial=True)
        with self.assertRaisesRegex(ValueError,'missing TF-A function psci_cpu_on'):
            prepare_nano_owned.psci_frontend_source(frontend.replace('psci_cpu_on(', 'unexpected_cpu_on('))

    def test_cold_producer_consumer_and_failures(self):
        with tempfile.TemporaryDirectory() as temp:
            binary=Path(temp)/'cold'
            r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all','-I'+str(FW),*(str(FW/name) for name in ('nano_cold.c','nano_boot.c','nano_bridges.c','nano_bridge_sip.c','tests/nano_cold_test.c')),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(r.returncode,0,r.stderr)
            r=subprocess.run([str(binary)],capture_output=True,text=True)
            self.assertEqual(r.returncode,0,r.stderr)
            self.assertRegex(r.stdout,r'^PASS [0-9]+ actual cold/PIO ownership cases')
            binary=Path(temp)/'smmu'
            r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',
                              '-I'+str(FW),str(FW/'nano_smmu.c'),str(FW/'tests/nano_smmu_test.c'),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(r.returncode,0,r.stderr)
            r=subprocess.run([str(binary)],capture_output=True,text=True)
            self.assertEqual(r.returncode,0,r.stderr)
            self.assertIn('PASS 269 read-only SMMU cases',r.stdout)
            print(r.stdout.strip(),flush=True)
            binary=Path(temp)/'firewall'
            r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',
                              '-I'+str(FW),str(FW/'nano_ddr_firewall.c'),str(FW/'tests/nano_ddr_firewall_test.c'),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(r.returncode,0,r.stderr)
            r=subprocess.run([str(binary)],capture_output=True,text=True)
            self.assertEqual(r.returncode,0,r.stderr)
            self.assertIn('read-only DDR firewall cases',r.stdout)
            print(r.stdout.strip(),flush=True)
        r=subprocess.run([sys.executable,str(FW/'verify_fabric_stop.py')],cwd=ROOT,capture_output=True,text=True,timeout=180)
        self.assertEqual(r.returncode,0,r.stderr+r.stdout)
        print(r.stdout.strip(),flush=True)
    def test_actual_early_capture_provider_and_owned_wire(self):
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp);inc=base/'include';(inc/'drivers').mkdir(parents=True);(inc/'lib').mkdir()
            (inc/'arch_helpers.h').write_text('#include <stdint.h>\n#define MODE_EL3 3\n#define GET_EL(v) (((v)>>2)&3)\n#define MPIDR_AFFLVL1_VAL(v) (((v)>>8)&255)\nuint64_t read_current_el(void);\nuint64_t read_mpidr(void);\nuint64_t read_sctlr_el3(void);\nvoid dsbsy(void);\n')
            (inc/'drivers/delay_timer.h').write_text('void udelay(unsigned);\n')
            (inc/'lib/mmio.h').write_text('#include <stdint.h>\nuint32_t mmio_read_32(uintptr_t);\nuint16_t mmio_read_16(uintptr_t);\nvoid mmio_write_32(uintptr_t,uint32_t);\nvoid mmio_write_16(uintptr_t,uint16_t);\n')
            (inc/'common').mkdir()
            (inc/'common/debug.h').write_text('#include <stdio.h>\n#define NOTICE(...) printf(__VA_ARGS__)\n')
            (inc/'socfpga_mailbox.h').write_text('#include <stdint.h>\n#define MBOX_JOB_ID 1U\n#define MBOX_CONFIG_STATUS 4U\n#define CMD_CASUAL 0U\n#define PIN_STATUS_NSTATUS (1U<<31)\n#define SOFTFUNC_STATUS_CONF_DONE 1U\n#define SOFTFUNC_STATUS_INIT_DONE 2U\n#define SOFTFUNC_STATUS_SEU_ERROR 8U\nint mailbox_send_cmd(uint32_t,uint32_t,uint32_t*,unsigned,uint32_t,uint32_t*,unsigned*);\n')
            header='#define FES_NANO_COLD_CHAIN {0x42,0x17}\n#define FES_NANO_COLD_OWNER_SEED 17ULL\n'
            command=['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',
                     '-I'+str(inc),'-I'+str(FW),*(str(FW/name) for name in ('nano_cold_platform.c','nano_cold.c','nano_boot.c','nano_bridges.c','nano_bridge_sip.c','nano_sd.c','nano_sd_platform.c','nano_identity.c','nano_configuration.c','nano_program.c','nano_smmu.c','nano_fabric_stop.c','nano_probe_stop.c','nano_ddr_firewall.c','nano_ddr_read.c','nano_hps_io.c','tests/nano_cold_platform_test.c'))]
            for sd,context,probe,ddr,smmu_boot,active,io_probe in ((0,0,0,0,0,0,0),(0,1,0,0,0,0,0),(1,0,0,0,0,0,0),(1,1,0,0,0,0,0),(0,0,1,0,0,0,0),(0,1,1,0,0,0,0),(0,1,1,1,0,0,0),(0,0,0,0,1,0,0),(0,1,1,1,0,1,0),(0,1,0,0,0,0,1)):
                values=','.join(str(i) for i in range(32)) if context else '0'
                extra=('#define FES_NANO_CONTROL_PROBE '+str(probe)+'\n'
                       '#define FES_NANO_SMMU_BOOT_OBSERVE '+str(smmu_boot)+'\n'
                       '#define FES_NANO_CONTROL_BASE 0x20010400UL\n'
                       '#define FES_NANO_CONTROL_ABI '+('0x10203041U' if ddr else '0x10203040U')+'\n'
                       '#define FES_NANO_CONTROL_BUILD {0x44556677,0x00112233,0xbbccddee,0x778899aa}\n'
                       '#define FES_NANO_CONTROL_MAP_SHA {1}\n')
                if ddr:extra+=('#define FES_NANO_READER_CPU_BASE 0x95000000ULL\n'
                               '#define FES_NANO_READER_BUS_BASE 0x95000000ULL\n'
                               '#define FES_NANO_READER_BYTES 8192U\n'
                               '#define FES_NANO_READER_ARENA_BYTES 2097152U\n'
                               '#define FES_NANO_PROBE_CPU_BASE 0x20010100UL\n'
                               '#define FES_NANO_PROBE_SPAN 128U\n')
                if active:extra+=('#define FES_NANO_DDR_READ_DIAGNOSTIC 1\n'
                                  '#define FES_NANO_DDR_READ_ATTRS_VERIFIED 1\n')
                if io_probe:
                    import json
                    rows=json.loads((ROOT/'platform/embedded/tamago/nativefabric/testdata/hps-io.json').read_bytes())['registers']
                    extra+='#define FES_NANO_HPS_IO_OBSERVE 1\n#define FES_NANO_HPS_IO_SHA {1,2,3}\n'
                    extra+='#define FES_NANO_HPS_IO_REGISTERS {'+','.join('{'+str(r['offset'])+','+str(r['value'])+',0xffffffffU}' for r in rows)+'}\n'
                (inc/'fes_nano_cold_profile.h').write_text(header+'#define FES_NANO_HPS_CONTEXT_SHA {'+values+'}\n'+extra)
                binary=base/('platform-'+str(sd)+'-'+str(context)+'-'+str(probe)+'-'+str(ddr)+'-'+str(smmu_boot)+'-'+str(active)+'-'+str(io_probe))
                r=subprocess.run(command+['-DFES_NANO_CONTROLLED_SD='+str(sd),*(['-DFES_TEST_HPS_CONTEXT'] if context else []),'-o',str(binary)],capture_output=True,text=True)
                self.assertEqual(r.returncode,0,r.stderr)
                mutations=list(range(43 if probe and context else 25 if sd else 22))+ (list(range(82,96)) if ddr or smmu_boot else [])+(list(range(96,124)) if ddr else [])
                if active:mutations=list(range(124,138))
                if io_probe:mutations=list(range(138,154))
                for mutation in mutations:
                    with self.subTest(sd=sd,context=context,probe=probe,ddr=ddr,smmu_boot=smmu_boot,mutation=mutation):
                        r=subprocess.run([str(binary),str(mutation)],capture_output=True,text=True)
                        self.assertEqual(r.returncode,0,r.stderr+r.stdout[-6000:])
                        if io_probe and mutation==138:
                            from test_nano_hps_io import io_trace,trace,BUILD,SEED
                            raw,expected,mapping=io_trace(r.stdout)
                            observed=trace.decode_native_boot(raw,BUILD,SEED,hps_context=expected,hps_io=mapping)
                            self.assertTrue(observed['complete'],observed['errors'])
                        if not active and (ddr or smmu_boot) and mutation>=82:
                            self.assertIn('access=unqualified',r.stdout)
                            if mutation<92:self.assertIn('bypass_controls_clear='+str(int(mutation==82)),r.stdout)
                            elif mutation==92:self.assertIn('status=-11',r.stdout)
                        if ddr and 96<=mutation<112:
                            self.assertIn('FES_NANO_MASTER_STOP status=',r.stdout)
                        if ddr and mutation>=112:
                            self.assertIn('FES_NANO_DDR_FIREWALL status=',r.stdout)
                        if active and mutation==124:
                            self.assertIn('FES_NANO_DDR_RESULT status=0 bytes=32',r.stdout)
                            self.assertIn('complete=1 data=active',r.stdout)
                            from test_nano_native_trace import NativeContainedConfigurationTraceTests
                            NativeContainedConfigurationTraceTests().check_actual_ddr_platform_trace(r.stdout)

    def test_actual_spl_handoff_reports_saved_failure_before_reject(self):
        source=(ROOT/'platform/embedded/nano/spl_cold.c').read_text()
        fragment=source[source.index('static void fes_nano_spl_sample_report('):]
        counter='asm volatile("mrs %0, cntpct_el0" : "=r"(epoch));'
        barrier='asm volatile("dsb sy" ::: "memory");'
        self.assertEqual(fragment.count(counter),1)
        self.assertEqual(fragment.count(barrier),1)
        fragment=fragment.replace(counter,'epoch=9;').replace(barrier,'barriers++;if(mode==5)longjmp(stopped,1);')
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp);test=base/'handoff.c';binary=base/'handoff'
            test.write_text(r'''
#include <assert.h>
#include <setjmp.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "nano_cold.h"
#define FES_NANO_COLD_ENTRY_SHA {0x5a}
#define FES_NANO_COLD_ENTRY_SIZE 16
#define FES_NANO_COLD_OWNER_SEED 17ULL
static struct fes_nano_cold source;
static struct fes_nano_cold_record fes_nano_spl_record;
static unsigned fit_verified=1,mode,calls,barriers;
static jmp_buf stopped;
#define puts(text) fputs((text),stdout) /* U-Boot puts does not append a newline. */
static unsigned current_el(void) { return mode==1 ? 2 : 3; }
static void sha256_csum_wd(const unsigned char *p,unsigned long n,uint8_t *hash,unsigned chunk) {
    assert(p==(void *)0x96080000UL && n==16 && chunk==65536);
    memset(hash,0,32);hash[0]=mode==3 ? 0xa5 : 0x5a;
}
static int finish(struct fes_nano_cold *s,unsigned entry,struct fes_nano_cold_record *r) {
    assert(s==&source && entry==1 && r==&fes_nano_spl_record);calls++;
    s->finish_stage=mode==4 ? FES_COLD_FINISH_IDLE_BEFORE : FES_COLD_FINISH_COMPLETE;
    s->before.per0=0xfeedbeef;s->record.sd_before=mode==4 ? 0x101 : 0;
    s->sd_clock_before=7;
    if(mode==4){s->failed=1;return -1;}
    *r=s->record;return 0;
}
#define fes_nano_cold_finish finish
static void hang(void) { longjmp(stopped,1); }
''' + cold_console_fixture() + fragment + r'''
int main(int argc,char **argv) {
    assert(argc==2);mode=strtoul(argv[1],0,0);if(mode==2)fit_verified=0;
    source.record.first.cause=4;source.record.first.power[2]=0x12345678;
    if(!setjmp(stopped)) {
        const void *r=fes_nano_spl_handoff();
        assert(!mode && r==&fes_nano_spl_record && barriers==1);
    }else assert(mode && barriers==(mode==5));
    assert(calls==(mode==0 || mode==4 || mode==5));
    assert(source.record.boot.epoch==9 && source.record.boot.owner==25);
    puts("HANDOFF_REPORT_TEST_PASS");return 0;
}
''')
            r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',
                              '-I'+str(FW),str(test),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(r.returncode,0,r.stderr)
            for mode in range(6):
                with self.subTest(mode=mode):
                    r=subprocess.run([str(binary),str(mode)],capture_output=True,text=True)
                    self.assertEqual(r.returncode,0,r.stderr)
                    self.assertIn('HANDOFF_REPORT_TEST_PASS',r.stdout)
                    if mode in (0,5):
                        self.assertIn('FES_NANO_SPL HANDOFF ',r.stdout)
                        self.assertNotIn('NANO_COLD_',r.stdout)
                        self.assertEqual(r.stdout.count('FES_NANO_SPL BARRIER phase=before\n'),1)
                        self.assertEqual(r.stdout.count('FES_NANO_SPL BARRIER phase=after\n'),int(mode==0))
                        self.assertLess(r.stdout.index('FES_NANO_SPL HANDOFF '),r.stdout.index('FES_NANO_SPL BARRIER phase=before'))
                        if not mode:self.assertLess(r.stdout.index('phase=before'),r.stdout.index('phase=after'))
                    else:
                        lines=r.stdout.splitlines()
                        reject=next(i for i,line in enumerate(lines) if line.startswith('FES_NANO_SPL REJECT '))
                        self.assertEqual(reject,5)
                        self.assertIn('phase=first cause=00000004',lines[0])
                        self.assertIn('power=00000000,00000000,12345678,00000000',lines[0])
                        self.assertIn('phase=before',lines[1]);self.assertIn('phase=last',lines[2])
                        self.assertTrue(lines[3].startswith('NANO_COLD_SD '))
                        self.assertTrue(lines[4].startswith('NANO_COLD_ENTRY actual='))
                        self.assertIn('cold_stage='+str(10 if mode==4 else 0),lines[reject])
                        self.assertIn('el='+str(2 if mode==1 else 3),lines[reject])
                        self.assertIn('fit='+str(int(mode!=2)),lines[reject])
                        self.assertIn('entry='+str(int(mode!=3)),lines[reject])
                        self.assertNotIn('FES_NANO_SPL HANDOFF ',r.stdout)
                        self.assertNotIn('FES_NANO_SPL BARRIER ',r.stdout)

    def test_actual_spl_seal_rejects_before_parse_and_reads_verified_ram(self):
        """Exercise the shipped SPL functions with FAT and SHA API substitutes.

        The hash substitute checks every byte of a known fixture. This tests the
        caller's verification/order and retained buffer, not vendor SHA itself.
        """
        source=(ROOT/'platform/embedded/nano/spl_cold.c').read_text()
        fragment=source[source.index('int fes_nano_spl_fit_check('):source.index('void fes_nano_spl_atf_trace(')]
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp);test=base/'fit.c';binary=base/'fit'
            test.write_text(r'''
#define _GNU_SOURCE
#include <assert.h>
#include <limits.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
typedef int64_t loff_t;
#define FES_NANO_FIT_PATH "FES/UPLOAD.BIN"
#define FES_NANO_SEALED_FIT_SIZE sealed_size
#define FES_NANO_SEALED_FIT_SHA {0x5a}
static unsigned fit_attempted,fit_verified,mode,size_calls,read_calls,hash_calls;
static unsigned long sealed_size=16;
static unsigned char original[16]="sealed-fit-test",file[16];
static int fat_size(const char *name,loff_t *size) {
    assert(!strcmp(name,FES_NANO_FIT_PATH));size_calls++;
    *size=mode==3?-1:mode==4?17:mode==5?15:16;
    return mode==2?-1:0;
}
static int file_fat_read(const char *name,void *buf,unsigned long size) {
    assert(!strcmp(name,FES_NANO_FIT_PATH) && size==16);read_calls++;
    memcpy(buf,file,size);
    return mode==6?15:mode==7?-1:16;
}
static void sha256_csum_wd(const unsigned char *buf,unsigned long size,uint8_t *hash,unsigned chunk) {
    assert(buf==(void *)0x94000000UL && size==16 && chunk==65536);hash_calls++;
    memset(hash,0,32);hash[0]=memcmp(buf,original,16)?0xa5:0x5a;
}
''' + fragment + r'''
int main(int argc,char **argv) {
    assert(argc==2);mode=strtoul(argv[1],0,0);memcpy(file,original,16);
    assert(mmap((void *)0x94000000UL,4096,PROT_READ|PROT_WRITE,MAP_PRIVATE|MAP_ANONYMOUS|MAP_FIXED_NOREPLACE,-1,0)==(void *)0x94000000UL);
    unsigned char out[16];memset(out,0xcc,sizeof(out));
    assert(!fes_nano_spl_fit_read(out,0,16) && out[0]==0xcc);
    if(mode==8)file[7]^=1;
    if(mode==9)sealed_size=0;
    if(mode==10)sealed_size=(8UL<<20)+1;
    int result=fes_nano_spl_fit_check(mode==1?"u-boot.itb":FES_NANO_FIT_PATH);
    if(mode) {
        assert(result<0 && !fit_verified && !fes_nano_spl_fit_read(out,0,16));
        unsigned reads=read_calls;mode=0;sealed_size=16;memcpy(file,original,16);
        assert(fes_nano_spl_fit_check(FES_NANO_FIT_PATH)<0 && read_calls==reads);
    } else {
        assert(!result && fit_verified && size_calls==1 && read_calls==1 && hash_calls==1);
        memset(file,0xa5,16); /* the parser must not re-read a changed FAT file */
        assert(fes_nano_spl_fit_read(out,0,16)==16 && !memcmp(out,original,16));
        assert(fes_nano_spl_fit_read(out,7,9)==9 && !memcmp(out,original+7,9));
        assert(!fes_nano_spl_fit_read(out,16,1));
        assert(!fes_nano_spl_fit_read(out,17,0));
        assert(!fes_nano_spl_fit_read(out,ULONG_MAX,2));
        assert(!fes_nano_spl_fit_read(out,1,ULONG_MAX));
        assert(fes_nano_spl_fit_check(FES_NANO_FIT_PATH)<0 && read_calls==1);
    }
    return 0;
}
''')
            r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',str(test),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(r.returncode,0,r.stderr)
            for mutation in range(11):
                with self.subTest(mutation=mutation):
                    r=subprocess.run([str(binary),str(mutation)],capture_output=True,text=True)
                    self.assertEqual(r.returncode,0,r.stderr)

if __name__=='__main__':unittest.main()
