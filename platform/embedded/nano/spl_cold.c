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
#include "fes_nano_atf_seal.h"
#include "nano_firmware_ram.h"
#if defined(FES_NANO_COLD_ROM) && FES_NANO_COLD_ROM && defined(FES_NANO_SHARED_USB) && FES_NANO_SHARED_USB
#include <asm/global_data.h>
#include <malloc.h>
DECLARE_GLOBAL_DATA_PTR;
extern char _image_binary_end[];
/* This check describes only the pinned SPL allocation scheme. A different
 * configuration needs a new source/lifetime audit, not a wider live bound. */
_Static_assert(CONFIG_SPL_TEXT_BASE==0 && CONFIG_SPL_MAX_SIZE<=0x40000,
               "firmware RAM exclusion requires the pinned SPL image range");
#if !defined(CONFIG_SPL_SYS_DCACHE_OFF) || !CONFIG_SPL_SYS_DCACHE_OFF
#error "selected cold firmware requires the uncached SPL handoff"
#endif
_Static_assert(CONFIG_SPL_STACK==0x71000 && CONFIG_SPL_SYS_MALLOC_F_LEN==0x2000,
               "firmware RAM exclusion requires the pinned early allocations");
_Static_assert(CONFIG_SPL_BSS_START_ADDR==0xbff00000 && CONFIG_SPL_BSS_MAX_SIZE==0x100000 &&
               CONFIG_SPL_CUSTOM_SYS_MALLOC_ADDR==0xbfa00000 && CONFIG_SPL_SYS_MALLOC_SIZE==0x500000,
               "firmware RAM exclusion requires the pinned DDR allocations");
#ifdef CONFIG_SPL_STACK_R
#error "firmware RAM exclusion has no relocated SPL stack contract"
#endif
#endif
#define FES_SPL_CONSOLE_READ(a) readl((void *)(a))
#define FES_SPL_CONSOLE_WRITE(a,v) writel((v),(void *)(a))
#define FES_SPL_CONSOLE_PARK() asm volatile("wfe" ::: "memory")
#include "fes_nano_spl_console.inc"
_Static_assert(FES_NANO_FW_RAM_UART_DATA==FES_SPL_CONSOLE_THR &&
               FES_NANO_FW_RAM_UART_STATUS==FES_SPL_CONSOLE_LSR &&
               FES_NANO_FW_RAM_UART_BYTES==FES_SPL_CONSOLE_BYTES &&
               FES_NANO_FW_RAM_UART_POLLS==FES_SPL_CONSOLE_POLLS,
               "early EL3 trace must use the existing bounded SPL UART sink");
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

#if defined(FES_NANO_COLD_ROM) && FES_NANO_COLD_ROM && defined(FES_NANO_SHARED_USB) && FES_NANO_SHARED_USB
static unsigned ram_guard_attempted __attribute__((section(".data"))) = 0;
/* Diagnostic observations only. Retain the one consumed snapshot; reporting a
 * rejection must never resample it, retry the guard or manufacture a grant. */
static struct fes_nano_spl_ram_view ram_guard_view __attribute__((section(".data"),aligned(8))) = {0};
static int ram_guard_snapshot_result __attribute__((section(".data"))) = -1;
static int ram_guard_result __attribute__((section(".data"))) = -1;
static int fes_nano_spl_ram_snapshot(uintptr_t params,uintptr_t cold,struct fes_nano_spl_ram_view *v)
{
    uint64_t sp,sctlr,daif;
    if(current_el()!=3 || source.failed || source.finish_stage!=FES_COLD_FINISH_COMPLETE ||
       cold!=(uintptr_t)&fes_nano_spl_record) return -1;
    asm volatile("mov %0, sp" : "=r"(sp));
    asm volatile("mrs %0, sctlr_el3" : "=r"(sctlr));
    asm volatile("mrs %0, daif" : "=r"(daif));
    if((sctlr&5U) || daif!=0x3c0U || ((read_mpidr()>>8)&0xff)!=fes_nano_spl_record.core ||
       mem_malloc_end<mem_malloc_start) return -1;
    *v=(struct fes_nano_spl_ram_view){sp,(uintptr_t)gd,sizeof(*gd),gd->malloc_base,
        CONFIG_SPL_SYS_MALLOC_F_LEN,(uintptr_t)_image_binary_end,cold,sizeof(fes_nano_spl_record),
        params,mem_malloc_start,mem_malloc_end-mem_malloc_start};
    return 0;
}
/* Called after the final trace and before the no-return ATF branch. Consume
 * before observing; a failed check cannot be repaired and replayed. This
 * excludes SPL's live allocations only, not later native or SDM users. */
int fes_nano_spl_firmware_ram_guard(uintptr_t params,uintptr_t cold)
{
    if(ram_guard_attempted) return -1;
    ram_guard_attempted=1;
    ram_guard_snapshot_result=fes_nano_spl_ram_snapshot(params,cold,&ram_guard_view);
    ram_guard_result=ram_guard_snapshot_result || !fes_nano_spl_ram_excluded(&ram_guard_view) ? -1 : 0;
    return ram_guard_result;
}
void fes_nano_spl_firmware_ram_rejected(void) __attribute__((noreturn));
void fes_nano_spl_firmware_ram_rejected(void)
{
    const struct fes_nano_spl_ram_view *v=&ram_guard_view;
    fes_nano_spl_cold_printf("FES_NANO_SPL REJECT firmware_ram attempted=%u snapshot=%d result=%d\n",
                           ram_guard_attempted,ram_guard_snapshot_result,ram_guard_result);
    fes_nano_spl_cold_printf("FES_NANO_SPL RAM_VIEW sp=%llx gd=%llx gd_bytes=%llx early=%llx early_bytes=%llx image=%llx\n",
        (unsigned long long)v->sp,(unsigned long long)v->gd_base,(unsigned long long)v->gd_bytes,
        (unsigned long long)v->early_base,(unsigned long long)v->early_bytes,(unsigned long long)v->image_bytes);
    fes_nano_spl_cold_printf("FES_NANO_SPL RAM_VIEW cold=%llx cold_bytes=%llx params=%llx heap=%llx heap_bytes=%llx\n",
        (unsigned long long)v->cold_base,(unsigned long long)v->cold_bytes,(unsigned long long)v->params_base,
        (unsigned long long)v->heap_base,(unsigned long long)v->heap_bytes);
    fes_nano_spl_cold_stop();
}
#endif

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

/* Final observation after cold containment and immediately before the branch.
 * Hash fixed loaded BL31 bytes directly: no watchdog or device callback is
 * allowed after SD/master containment. Never derive a read range from args. */
void fes_nano_spl_atf_trace(uintptr_t target, uintptr_t bl32, uintptr_t bl33,
                           uintptr_t fdt, uintptr_t params, uintptr_t cold)
{
    static const uint8_t expected[32]=FES_NANO_ATF_SHA;
    uint8_t hash[32];sha256_context state;
    uint64_t sctlr,scr,daif;
    unsigned el;
    fes_nano_spl_cold_puts("FES_NANO_SPL ATF_STEP phase=entry\n");
    el=current_el();
    if (el!=3 || source.failed || source.finish_stage!=FES_COLD_FINISH_COMPLETE ||
        target!=0x80000000UL || bl32 || bl33!=0x80200000UL || fdt ||
        params&7U || params<CONFIG_SPL_BSS_START_ADDR ||
        params>CONFIG_SPL_BSS_START_ADDR+CONFIG_SPL_BSS_MAX_SIZE-64UL ||
        cold!=(uintptr_t)&fes_nano_spl_record ||
        FES_NANO_ATF_SIZE<16UL || FES_NANO_ATF_SIZE>(2UL<<20)) {
        fes_nano_spl_cold_printf("FES_NANO_SPL REJECT atf arguments el=%u target=%llx bl32=%llx bl33=%llx fdt=%llx params=%llx cold=%llx failed=%u stage=%u\n",
               el,(unsigned long long)target,(unsigned long long)bl32,(unsigned long long)bl33,
               (unsigned long long)fdt,(unsigned long long)params,(unsigned long long)cold,
               source.failed,source.finish_stage);fes_nano_spl_cold_stop();
    }
    asm volatile("mrs %0, sctlr_el3" : "=r"(sctlr));
    asm volatile("mrs %0, scr_el3" : "=r"(scr));
    asm volatile("mrs %0, daif" : "=r"(daif));
    if (sctlr&5U || daif!=0x3c0U ||
        ((read_mpidr()>>8)&0xff)!=fes_nano_spl_record.core) {
        fes_nano_spl_cold_printf("FES_NANO_SPL REJECT atf CPU state sctlr=%llx scr=%llx daif=%llx mpidr=%llx core=%u\n",
               (unsigned long long)sctlr,(unsigned long long)scr,(unsigned long long)daif,
               (unsigned long long)read_mpidr(),fes_nano_spl_record.core);fes_nano_spl_cold_stop();
    }
    fes_nano_spl_cold_puts("FES_NANO_SPL ATF_STEP phase=hash-before\n");
    sha256_starts(&state);
    sha256_update(&state,(const uint8_t *)0x80000000UL,FES_NANO_ATF_SIZE);
    sha256_finish(&state,hash);
    fes_nano_spl_cold_puts("FES_NANO_SPL ATF_STEP phase=hash-after\n");
    if (memcmp(hash,expected,32)) {
        fes_nano_spl_cold_puts("FES_NANO_SPL REJECT loaded BL31 bytes actual=");
        for (unsigned i=0;i<32;i++) fes_nano_spl_cold_printf("%02x",hash[i]);
        fes_nano_spl_cold_puts(" expected=");
        for (unsigned i=0;i<32;i++) fes_nano_spl_cold_printf("%02x",expected[i]);
        fes_nano_spl_cold_puts("\n");fes_nano_spl_cold_stop();
    }
    fes_nano_spl_cold_printf("FES_NANO_SPL ATF target=%llx bl32=%llx bl33=%llx fdt=%llx params=%llx cold=%llx cookie=%llx el=%u mpidr=%llx sctlr=%llx scr=%llx daif=%llx bytes=%lu sha256=",
           (unsigned long long)target,(unsigned long long)bl32,(unsigned long long)bl33,
           (unsigned long long)fdt,(unsigned long long)params,(unsigned long long)cold,
           (unsigned long long)FES_NANO_COLD_COOKIE,el,(unsigned long long)read_mpidr(),
           (unsigned long long)sctlr,(unsigned long long)scr,(unsigned long long)daif,FES_NANO_ATF_SIZE);
    for (unsigned i=0;i<32;i++) fes_nano_spl_cold_printf("%02x",hash[i]);
    fes_nano_spl_cold_puts("\n");
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
    fes_nano_spl_cold_printf("NANO_COLD_SAMPLE phase=%s cause=%08x per0=%08x bridge=%08x cpus=%08x release=%08x kind=%u power=%08x,%08x,%08x,%08x request=%08x status=%08x config=%08x\n",
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
        fes_nano_spl_cold_printf("NANO_COLD_SD mode=%08x before=%08x after=%08x clock_before=%08x clock_after=%08x reset_readback=%08x\n",
               source.record.sd_mode,source.record.sd_before,source.record.sd_after,
               source.sd_clock_before,source.record.sd_clock,source.reset_readback);
        fes_nano_spl_cold_puts("NANO_COLD_ENTRY actual=");
        for (unsigned i=0;i<32;i++) fes_nano_spl_cold_printf("%02x",hash[i]);
        fes_nano_spl_cold_puts(" expected=");
        for (unsigned i=0;i<32;i++) fes_nano_spl_cold_printf("%02x",expected[i]);
        fes_nano_spl_cold_puts("\n");
        fes_nano_spl_cold_printf("FES_NANO_SPL REJECT handoff el=%u fit=%u entry=%u cold_stage=%u\n",
               el,fit_verified,entry_verified,source.finish_stage);
        fes_nano_spl_cold_stop();
    }
    fes_nano_spl_cold_printf("FES_NANO_SPL HANDOFF owner=%llx epoch=%llx per0=%08x bridge=%08x sd=%08x\n",
           (unsigned long long)source.record.boot.owner,(unsigned long long)source.record.boot.epoch,
           source.record.last.per0,source.record.last.bridge,source.record.sd_after);
    fes_nano_spl_cold_puts("FES_NANO_SPL BARRIER phase=before\n");
    asm volatile("dsb sy" ::: "memory");
    fes_nano_spl_cold_puts("FES_NANO_SPL BARRIER phase=after\n");
    return &fes_nano_spl_record;
}
