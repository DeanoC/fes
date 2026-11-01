"""Actual SPL/BL31 cold record and SD-stop implementation with injected MMIO."""
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
ROOT=Path(__file__).resolve().parents[1]
FW=ROOT/'platform/embedded/firmware'
class NanoColdTests(unittest.TestCase):
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
    def test_actual_early_capture_provider_and_owned_wire(self):
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp);inc=base/'include';(inc/'drivers').mkdir(parents=True);(inc/'lib').mkdir()
            (inc/'arch_helpers.h').write_text('#include <stdint.h>\n#define MODE_EL3 3\n#define GET_EL(v) (((v)>>2)&3)\n#define MPIDR_AFFLVL1_VAL(v) (((v)>>8)&255)\nuint64_t read_current_el(void);\nuint64_t read_mpidr(void);\nvoid dsbsy(void);\n')
            (inc/'drivers/delay_timer.h').write_text('void udelay(unsigned);\n')
            (inc/'lib/mmio.h').write_text('#include <stdint.h>\nuint32_t mmio_read_32(uintptr_t);\nuint16_t mmio_read_16(uintptr_t);\nvoid mmio_write_32(uintptr_t,uint32_t);\nvoid mmio_write_16(uintptr_t,uint16_t);\n')
            (inc/'fes_nano_cold_profile.h').write_text('#define FES_NANO_COLD_CHAIN {0x42,0x17}\n#define FES_NANO_COLD_OWNER_SEED 17ULL\n')
            binary=base/'platform'
            command=['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',
                     '-I'+str(inc),'-I'+str(FW),*(str(FW/name) for name in ('nano_cold_platform.c','nano_cold.c','nano_boot.c','nano_bridges.c','nano_bridge_sip.c','nano_sd.c','tests/nano_cold_platform_test.c')),'-o',str(binary)]
            r=subprocess.run(command,capture_output=True,text=True);self.assertEqual(r.returncode,0,r.stderr)
            for mutation in range(22):
                with self.subTest(mutation=mutation):
                    r=subprocess.run([str(binary),str(mutation)],capture_output=True,text=True)
                    self.assertEqual(r.returncode,0,r.stderr)

    def test_actual_spl_handoff_reports_saved_failure_before_reject(self):
        source=(ROOT/'platform/embedded/nano/spl_cold.c').read_text()
        fragment=source[source.index('static void fes_nano_spl_sample_report('):]
        counter='asm volatile("mrs %0, cntpct_el0" : "=r"(epoch));'
        barrier='asm volatile("dsb sy" ::: "memory");'
        self.assertEqual(fragment.count(counter),1)
        self.assertEqual(fragment.count(barrier),1)
        fragment=fragment.replace(counter,'epoch=9;').replace(barrier,'barriers++;')
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
''' + fragment + r'''
int main(int argc,char **argv) {
    assert(argc==2);mode=strtoul(argv[1],0,0);if(mode==2)fit_verified=0;
    source.record.first.cause=4;source.record.first.power[2]=0x12345678;
    if(!setjmp(stopped)) {
        const void *r=fes_nano_spl_handoff();
        assert(!mode && r==&fes_nano_spl_record && barriers==1);
    }else assert(mode && !barriers);
    assert(calls==(mode==0 || mode==4));
    assert(source.record.boot.epoch==9 && source.record.boot.owner==25);
    puts("HANDOFF_REPORT_TEST_PASS");return 0;
}
''')
            r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',
                              '-I'+str(FW),str(test),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(r.returncode,0,r.stderr)
            for mode in range(5):
                with self.subTest(mode=mode):
                    r=subprocess.run([str(binary),str(mode)],capture_output=True,text=True)
                    self.assertEqual(r.returncode,0,r.stderr)
                    self.assertIn('HANDOFF_REPORT_TEST_PASS',r.stdout)
                    if not mode:
                        self.assertIn('FES_NANO_SPL HANDOFF ',r.stdout)
                        self.assertNotIn('NANO_COLD_',r.stdout)
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

    def test_actual_spl_seal_rejects_before_parse_and_reads_verified_ram(self):
        """Exercise the shipped SPL functions with FAT and SHA API substitutes.

        The hash substitute checks every byte of a known fixture. This tests the
        caller's verification/order and retained buffer, not vendor SHA itself.
        """
        source=(ROOT/'platform/embedded/nano/spl_cold.c').read_text()
        fragment=source[source.index('int fes_nano_spl_fit_check('):source.index('void fes_nano_spl_early(')]
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
