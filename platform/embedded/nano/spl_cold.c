/* SPDX-License-Identifier: GPL-2.0-or-later */
#include <asm/io.h>
#include <asm/system.h>
#include <hang.h>
#include <fat.h>
#include <linux/delay.h>
#include <stdio.h>
#include <string.h>
#include <u-boot/sha256.h>
#include "nano_cold.h"
#include "fes_nano_cold_profile.h"
#include "fes_nano_fit_seal.h"
#define FES_COLD_READ32(a) readl((void *)(a))
#define FES_COLD_READ16(a) readw((void *)(a))
#define FES_COLD_WRITE32(a,v) writel((v),(void *)(a))
#define FES_COLD_WRITE16(a,v) writew((v),(void *)(a))
#define FES_COLD_BARRIER() asm volatile("dsb sy" ::: "memory")
#define FES_COLD_WAIT() udelay(10)
#include "nano_cold_mmio.inc"

/* Explicit .data: SPL BSS is high DDR and unavailable at the first sample.
 * The linker-checked record remains inside loaded SPL OCRAM, not an NS buffer. */
static struct fes_nano_cold source __attribute__((section(".data"),aligned(8))) = {0};
struct fes_nano_cold_record fes_nano_spl_record __attribute__((section(".data"),aligned(8))) = {.magic=FES_NANO_COLD_MAGIC};
static unsigned fit_attempted __attribute__((section(".data"))) = 0;
static unsigned fit_verified __attribute__((section(".data"))) = 0;

int fes_nano_spl_fit_check(const char *filename)
{
    static const uint8_t expected[32]=FES_NANO_SEALED_FIT_SHA;
    uint8_t hash[32];loff_t size;
    if (fit_attempted++ || strcmp(filename,FES_NANO_FIT_PATH) ||
        !FES_NANO_SEALED_FIT_SIZE || FES_NANO_SEALED_FIT_SIZE>(8UL<<20) ||
        fat_size(filename,&size) || size<0 || (unsigned long)size!=FES_NANO_SEALED_FIT_SIZE ||
        file_fat_read(filename,(void *)0x94000000UL,FES_NANO_SEALED_FIT_SIZE)!=(int)FES_NANO_SEALED_FIT_SIZE)
        return -1;
    sha256_csum_wd((const unsigned char *)0x94000000UL,FES_NANO_SEALED_FIT_SIZE,hash,65536);
    if (memcmp(hash,expected,32)) return -1;
    fit_verified=1;
    puts("FES_NANO_SPL FIT verified before parsing\n");
    return 0;
}
unsigned long fes_nano_spl_fit_read(void *buffer, unsigned long offset, unsigned long size)
{
    if (!fit_verified || offset>FES_NANO_SEALED_FIT_SIZE || size>FES_NANO_SEALED_FIT_SIZE-offset) return 0;
    memcpy(buffer,(const void *)(0x94000000UL+offset),size);
    return size;
}

void fes_nano_spl_early(void)
{
    static const uint8_t chain[32]=FES_NANO_COLD_CHAIN;
    unsigned core=(read_mpidr() >> 8)&0xff;
    if (current_el()!=3) hang();
    (void)fes_nano_cold_begin(&source,fes_cold_hw_ops(),chain,FES_NANO_COLD_OWNER_SEED,1,core);
}
void fes_nano_spl_report(void)
{
    uint32_t cause=0,cpus=0,release=0;
    unsigned long long mpidr=read_mpidr();
    (void)fes_cold_hw_read(NULL,FES_COLD_CAUSE,&cause);
    (void)fes_cold_hw_read(NULL,FES_COLD_CPUS,&cpus);
    (void)fes_cold_hw_read(NULL,FES_COLD_RELEASE,&release);
    printf("FES_NANO_SPL BEGIN cause=%08x per0=%08x bridge=%08x cpus=%08x release=%08x kind=%u valid=%u\n",
           source.record.first.cause,source.record.first.per0,source.record.first.bridge,
           source.record.first.cpus,source.record.first.release,source.record.first.boot_kind,!source.failed);
    /* A later diagnostic never replaces a failed first-sample proof. */
    printf("FES_NANO_SPL SCOPE mpidr=%llx core=%u cause_now=%08x cpus_now=%08x release_now=%08x reset_timeouts=%08x\n",
           mpidr,(unsigned)((mpidr>>8)&0xff),cause,cpus,release,
           FES_COLD_READ32(FES_NANO_COLD_RSTMGR+0x8));
    if (source.failed) hang();
}
static void fes_nano_spl_sample_report(const char *phase, const struct fes_nano_cold_sample *v)
{
    /* Saved observations only: do not retry reads or touch an SD block that
     * may already have been reset by the failed policy. A sample-stage failure
     * can leave the corresponding saved sample incomplete. */
    printf("NANO_COLD_SAMPLE phase=%s cause=%08x per0=%08x bridge=%08x cpus=%08x release=%08x kind=%u power=%08x,%08x,%08x,%08x request=%08x status=%08x config=%08x\n",
           phase,v->cause,v->per0,v->bridge,v->cpus,v->release,v->boot_kind,
           v->power[0],v->power[1],v->power[2],v->power[3],v->pch_request,v->pch_status,v->boot_config);
}
const void *fes_nano_spl_handoff(void)
{
    static const uint8_t expected[32]=FES_NANO_COLD_ENTRY_SHA;
    uint8_t hash[32];uint64_t epoch;
    sha256_csum_wd((const unsigned char *)0x96080000UL,FES_NANO_COLD_ENTRY_SIZE,hash,65536);
    asm volatile("mrs %0, cntpct_el0" : "=r"(epoch));
    source.record.boot.epoch=epoch|1U;
    source.record.boot.owner=(FES_NANO_COLD_OWNER_SEED ^ source.record.boot.epoch)|1U;
    unsigned el=current_el(), entry_verified=!memcmp(hash,expected,32);
    if (el!=3 || !fit_verified || !entry_verified || fes_nano_cold_finish(&source,1,&fes_nano_spl_record)) {
        fes_nano_spl_sample_report("first",&source.record.first);
        fes_nano_spl_sample_report("before",&source.before);
        fes_nano_spl_sample_report("last",&source.record.last);
        printf("NANO_COLD_SD mode=%08x before=%08x after=%08x clock_before=%08x clock_after=%08x reset_readback=%08x\n",
               source.record.sd_mode,source.record.sd_before,source.record.sd_after,
               source.sd_clock_before,source.record.sd_clock,source.reset_readback);
        puts("NANO_COLD_ENTRY actual=");
        for (unsigned i=0;i<32;i++) printf("%02x",hash[i]);
        puts(" expected=");
        for (unsigned i=0;i<32;i++) printf("%02x",expected[i]);
        puts("\n");
        printf("FES_NANO_SPL REJECT handoff el=%u fit=%u entry=%u cold_stage=%u\n",
               el,fit_verified,entry_verified,source.finish_stage);
        hang();
    }
    printf("FES_NANO_SPL HANDOFF owner=%llx epoch=%llx per0=%08x bridge=%08x sd=%08x\n",
           (unsigned long long)source.record.boot.owner,(unsigned long long)source.record.boot.epoch,
           source.record.last.per0,source.record.last.bridge,source.record.sd_after);
    asm volatile("dsb sy" ::: "memory");
    return &fes_nano_spl_record;
}
