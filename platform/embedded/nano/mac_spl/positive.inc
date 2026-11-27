/* SPDX-License-Identifier: GPL-2.0-or-later */
/* Runs only in the CBL standalone positive SPL. No sd, qspi, USB or BL31 path
 * returns from this build of board_init_f. CPU uses physical uncached accesses. */
#include <asm/global_data.h>
#include "mac_tx_diagnostic.h"
#include "fes_cpu_diag_config.h"
#include "fes_mac_positive_frame.h"
DECLARE_GLOBAL_DATA_PTR;

_Static_assert(CONFIG_SPL_TEXT_BASE==0 && CONFIG_SPL_MAX_SIZE<=0x40000 &&
               CONFIG_SPL_STACK==0x71000 && CONFIG_SPL_SYS_MALLOC_F_LEN==0x2000 &&
               CONFIG_SPL_BSS_START_ADDR==0xbff00000 && CONFIG_SPL_BSS_MAX_SIZE==0x100000 &&
               CONFIG_SPL_CUSTOM_SYS_MALLOC_ADDR==0xbfa00000 && CONFIG_SPL_SYS_MALLOC_SIZE==0x500000,
               "diagnostic DDR scratch must be disjoint from all SPL allocations");
#if !defined(CONFIG_SPL_SYS_DCACHE_OFF) || !CONFIG_SPL_SYS_DCACHE_OFF || defined(CONFIG_SPL_STACK_R)
#error Unqualified_SPL_cache_or_stack_configuration
#endif
extern int fes_positive_cpu_scope(struct fes_nano_cold *, struct fes_nano_cold_sample *);
extern void fes_nano_positive_clock_rates(unsigned *, unsigned *);
extern int fes_nano_positive_sdm_status(void);
static unsigned positive_admitted __attribute__((section(".data"))) = 0;
static unsigned reset_baseline __attribute__((section(".data"))) = 0;
static unsigned bridge_baseline __attribute__((section(".data"))) = 0;
static unsigned clocks_baseline[9] __attribute__((section(".data"))) = {0};
static const unsigned clock_addresses[]={0x10d10004,0x10d10024,0x10d10030,0x10d10040,0x10d10044,
                                        0x10d1007c,0x10d10088,0x10d10094,0x10d100f4};
static unsigned positive_rd(void *ctx,unsigned a){(void)ctx;return readl((void *)(unsigned long)a);}
static void positive_wr(void *ctx,unsigned a,unsigned v){(void)ctx;writel(v,(void *)(unsigned long)a);}
static uint64_t positive_ld(void *ctx,unsigned a){(void)ctx;return *(volatile uint64_t *)(unsigned long)a;}
static void positive_st(void *ctx,unsigned a,uint64_t v){(void)ctx;*(volatile uint64_t *)(unsigned long)a=v;}
static void positive_barrier(void *ctx){(void)ctx;asm volatile("dsb sy" ::: "memory");}
static uint64_t positive_time(void *ctx)
{
    uint64_t ticks,frequency;(void)ctx;
    asm volatile("mrs %0,cntpct_el0" : "=r"(ticks));
    asm volatile("mrs %0,cntfrq_el0" : "=r"(frequency));
    return frequency==400000000 ? ticks/400 : ~(uint64_t)0;
}
/* At the owning EL3 these are the local PSTATE masks and physical SError
 * pending status. Reading ISR does not acknowledge the interrupt. */
static unsigned positive_daif(void)
{
    unsigned long value;
    asm volatile("mrs %0, daif" : "=r"(value));
    return value & 0x3c0;
}
static unsigned positive_serror(void)
{
    unsigned long value;
    asm volatile("mrs %0, isr_el1" : "=r"(value));
    return value & 0x100;
}
static int positive_owns(void *ctx)
{
    uint64_t sctlr;(void)ctx;
    asm volatile("mrs %0,sctlr_el3" : "=r"(sctlr));
    if(current_el()!=3 || (sctlr&5) || positive_daif()!=0x3c0 || positive_serror() ||
       positive_time(NULL)==~(uint64_t)0 || read_mpidr()!=0x81000000 ||
       source.failed || !positive_admitted || !(source.record.first.per0&1) ||
       (positive_rd(NULL,0x10d11024)&~0x101U)!=(reset_baseline&~0x101U) ||
       positive_rd(NULL,0x10d1102c)!=bridge_baseline ||
       positive_rd(NULL,0x10d11000)!=source.record.first.cause ||
       positive_rd(NULL,0x10d11008) || positive_rd(NULL,0x16008020) ||
       positive_rd(NULL,0x16008024)) return 0;
    struct fes_nano_cold_sample now;
    if (!fes_positive_cpu_scope(&source, &now)) return 0;
    for(unsigned i=0;i<sizeof(clock_addresses)/sizeof(clock_addresses[0]);i++)
        if(positive_rd(NULL,clock_addresses[i])!=clocks_baseline[i])return 0;
    return 1;
}
/* This path admits register observations only. It has no dependency on the
 * positive-only DDR, SDM, PHY clock, counter, or exception-mask suppliers. */
static int positive_observe_unchanged(void *ctx)
{
    uint64_t sctlr;
    struct fes_nano_cold_sample now;
    if (current_el()!=3 || read_mpidr()!=0x81000000) return 0;
    asm volatile("mrs %0,sctlr_el3" : "=r"(sctlr));
    if ((sctlr&1) || positive_serror() || !fes_positive_cpu_scope(&source,&now)) return 0;
    return source.record.first.cause==4 && source.record.first.per0==0xff7fbfff &&
           source.record.first.bridge==0x4f && now.cause==4 && now.per0==0xff7fbfff &&
           (now.bridge&15)==15 && !positive_rd(ctx,0x10d11008) &&
           (positive_rd(ctx,0x10d10004)&0x01030301)==0x00030300 &&
           (positive_rd(ctx,0x10d10024)&14)==14 && !(positive_rd(ctx,0x10d10030)&2) &&
           ((positive_rd(ctx,0x10d10040)>>16)&7)<=1;
}
static void positive_report_caps(void *ctx, const struct fes_tx_caps *c)
{
    (void)ctx;
    printf("FES_CBL_TX_CAPS id=%s version=%08x feature0=%08x feature1=%08x feature2=%08x interface=%08x\n",
           FES_CPU_DIAG_ID,c->version,c->feature0,c->feature1,c->feature2,c->interface);
}
static int positive_matches(const char *predicate, unsigned long long observed, unsigned long long required)
{
    if (observed==required) return 1;
    printf("FES_CBL_TX_GATE id=%s predicate=%s observed=%016llx required=%016llx\n",
           FES_CPU_DIAG_ID,predicate,observed,required);
    return 0;
}
static int positive_startup(void)
{
    uint64_t sctlr,frequency;
    unsigned l4sp=0,emac=0;
    if(!positive_matches("exception-level",current_el(),3) ||
       !positive_matches("boot-mpidr",read_mpidr(),0x81000000)) return 0;
    asm volatile("mrs %0,sctlr_el3" : "=r"(sctlr));
    asm volatile("mrs %0,cntfrq_el0" : "=r"(frequency));
    if(!positive_matches("mmu-cache",sctlr&5,0) ||
       !positive_matches("counter-hz",frequency,400000000)) return 0;
    reset_baseline=positive_rd(NULL,0x10d11024);
    bridge_baseline=positive_rd(NULL,0x10d1102c);
    for(unsigned i=0;i<sizeof(clock_addresses)/sizeof(clock_addresses[0]);i++)
        clocks_baseline[i]=positive_rd(NULL,clock_addresses[i]);
    const unsigned select=1U<<((clocks_baseline[7]>>26)&1);
    if (!positive_matches("initial-proof",source.failed,0) ||
        !positive_matches("first-cause",source.record.first.cause,4) ||
        !positive_matches("first-reset",source.record.first.per0,0xff7fbfff) ||
        !positive_matches("first-bridge",source.record.first.bridge,0x4f) ||
        !positive_matches("held-reset",reset_baseline&0xff7fbfff,0xff7fbfff) ||
        !positive_matches("held-bridge",bridge_baseline&15,15) ||
        !positive_matches("board-data",gd->bd!=NULL,1) ||
        !positive_matches("bank-base",gd->bd->bi_dram[0].start,0x80000000) ||
        !positive_matches("ram-size",gd->ram_size,0x40000000) ||
        !positive_matches("clock-state",clocks_baseline[0]&0x01030301,0x00030300) ||
        !positive_matches("bus-clock-gates",clocks_baseline[1]&14,14) ||
        !positive_matches("emac-source-b",(clocks_baseline[7]>>26)&1,0) ||
        !positive_matches("emac-clock-gate",clocks_baseline[5]&1,1) ||
        !positive_matches("noc-source-high",(clocks_baseline[3]>>17)&3,0) ||
        !positive_matches("noc-bypass",clocks_baseline[2]&2,0) ||
        !positive_matches("emac-bypass-reset",(clocks_baseline[6]|clocks_baseline[8])&select,0) ||
        !positive_matches("current-cause",positive_rd(NULL,0x10d11000),source.record.first.cause) ||
        !positive_matches("reset-timeouts",positive_rd(NULL,0x10d11008),0) ||
        !positive_matches("secure-smmu-control",positive_rd(NULL,0x16008020),0) ||
        !positive_matches("secure-smmu-ack",positive_rd(NULL,0x16008024),0) ||
        !positive_matches("sdm-config-status",(unsigned)fes_nano_positive_sdm_status(),0)) return 0;
    fes_nano_positive_clock_rates(&l4sp,&emac);
    if (!positive_matches("l4sp-hz",l4sp,100000000) ||
        !positive_matches("emac-reference-hz",emac,FES_TX_EMAC_REF_HZ)) return 0;
    /* Vendor SPL can arrive with A clear. The primary is already proven
     * exclusive, storage and other firmware never resume in this image. */
    unsigned before=positive_daif();
    if (!positive_matches("entry-dif",before&0x2c0,0x2c0) ||
        !positive_matches("serror-before",positive_serror(),0)) return 0;
    asm volatile("dsb sy; msr daifset, #4; isb" ::: "memory");
    unsigned after=positive_daif(), pending=positive_serror();
    if (!positive_matches("cpu-masks",after,0x3c0) ||
        !positive_matches("serror-after",pending,0)) return 0;
    printf("FES_CBL_TX_SETUP id=%s daif_before=%03x daif_after=%03x serror=%03x l4sp_hz=%u emaca_hz=%u\n",
           FES_CPU_DIAG_ID,before,after,pending,l4sp,emac);
    return 1;
}
__attribute__((__noinline__)) void fes_nano_positive_tx(void)
{
    struct fes_tx_result result={0};
    const struct fes_tx_io io={NULL,positive_rd,positive_wr,positive_ld,positive_st,positive_barrier,
                               positive_time,positive_owns,positive_observe_unchanged,positive_report_caps};
    int rc=-1;
    enum fes_tx_disposition closed;
    static const char *const names[]={"cold-recovery","closed","unchanged","revoked-undispatched"};
    printf("FES_CBL_MAC_TX BEGIN id=%s\n",FES_CPU_DIAG_ID);
    struct fes_nano_cold_sample now;
    if (!positive_matches("initial-cpu-scope",fes_positive_cpu_scope(&source,&now),1)) goto done;
    const struct fes_nano_cold_sample *p = &source.record.first;
    printf("FES_CBL_MAC_TX POWER phase=first cpu0=%08x cpu1=%08x cpu2=%08x cpu3=%08x req=%08x ack=%08x boot=%08x\n",
           p->power[0],p->power[1],p->power[2],p->power[3],p->pch_request,p->pch_status,p->boot_config);
    printf("FES_CBL_MAC_TX POWER phase=live cpu0=%08x cpu1=%08x cpu2=%08x cpu3=%08x req=%08x ack=%08x boot=%08x\n",
           now.power[0],now.power[1],now.power[2],now.power[3],now.pch_request,now.pch_status,now.boot_config);
    if (!positive_startup()) goto done;
    positive_admitted=1;
    if (!positive_matches("stable-dma-owner",positive_owns(NULL),1)) goto done;
    printf("FES_CBL_TX_GATE id=%s predicate=accepted observed=0000000000000001 required=0000000000000001\n",FES_CPU_DIAG_ID);
    rc=fes_tx_once(&io,fes_positive_frame,&result);
done:
    closed=fes_tx_terminal(&io,&result,rc);
    if(!rc && closed!=FES_TX_CLOSED)rc=-1;
    printf("FES_CBL_MAC_TX END id=%s rc=%d stage=%u arm=%u mapped=%u mac=%u possible=%u dispatch=%u tps=%u revoked=%u reset=%u disposition=%s writes=%u terminal_per0=%08x terminal_route=%08x terminal_sid=%08x terminal_cr0=%08x terminal_ack=%08x terminal_ste=%08x reg=%08x value=%08x desc=%08x dma=%08x fault_reg=%08x fault_value=%08x phy=%04x\n",
           FES_CPU_DIAG_ID,rc,result.stage,result.arm_attempted,result.mapped,result.mac_release_attempted,
           result.dma_possible,result.dispatched,result.process_stopped,
           result.translation_revoked,result.mac_in_reset,names[closed],result.write_attempted,result.terminal_per0,
           result.terminal_route,result.terminal_sid,result.terminal_cr0,result.terminal_ack,result.terminal_ste,
           result.last_address,result.last_value,result.descriptor_status,result.channel_status,
           result.fault_address,result.fault_value,result.phy_status);
    fes_nano_cpu_diag_park();
}
