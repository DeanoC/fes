/* SPDX-License-Identifier: BSD-3-Clause */
/* Nano-specific register/PHY policy: Terasic d0f8813 DE25 DTS/clock source;
 * reset and indirect PHY sequence also described by Altera MIT-0 drivers,
 * Copyright (C) 2026 Altera, pin 66bdcc347564c5698a8997169bce83a31647d618.
 * These are actual platform callbacks with injectable ordered MMIO for tests.
 * Their presence is not physical qualification. */
#include "nano_sd_platform.h"

enum { HRS_RESET=0, PHY_ADDRESS=0x10, PHY_DATA=0x14, MODE=0x18,
       PHY_STATUS=0x24, CLOCK_ADJUST=0x28, COMPENSATION=0x1c, OUTPUT_DELAY=0x40,
       COMMAND=0x20c, PRESENT=0x224, CLOCK=0x22c, STATUS=0x230,
       STATUS_ENABLE=0x234, SIGNAL_ENABLE=0x238 };
static const unsigned clock_offsets[FES_NANO_SD_CLOCK_REGS] = {
    0, 4, 0x30, 0x88, 0x40, 0x44, 0x48, 0x6c, 0x68, 0x9c, 0xc0, 0xb4, 0x7c, 0x18
};
/* Nano generated timing, not the generic SDK's different defaults. */
static const struct { unsigned address; uint32_t mask, value; } phy_policy[] = {
    {0x2004,0x00780000U,0x00780000U},
    {0x2008,0x91fc0040U,0x81a40040U},
    {0x200c,0x00f000ffU,0x00a000feU},
    {0x2010,0xffffffffU,0},
    {0x2080,0x000003f0U,0},
    {0x2000,0xbf000007U,0x28000001U}
};
static int fail(struct fes_nano_sd_platform *p, int error)
{
    p->failed=1;
    if (!p->first_error) p->first_error=error ? error : -5;
    if (p->cold) { p->cold->failed=1; p->cold->valid=0; }
    return p->first_error;
}
static int read_reg(struct fes_nano_sd_platform *p, uintptr_t address, uint32_t *value)
{
    if (p->failed) return -1;
    p->last_address=address;
    int error=p->io.read(p->io.context,address,value);
    if (error) return fail(p,error);
    p->last_value=*value;
    return 0;
}
static int check(struct fes_nano_sd_platform *p, uintptr_t address, uint32_t mask, uint32_t expected)
{
    uint32_t value;
    p->last_mask=mask; p->last_expected=expected;
    if (read_reg(p,address,&value)) return -1;
    return (value & mask)==expected ? 0 : fail(p,-5);
}
static int write_reg(struct fes_nano_sd_platform *p, uintptr_t address, uint32_t value)
{
    if (p->failed) return -1;
    p->last_address=address;
    int error=p->io.write(p->io.context,address,value);
    if (error) return fail(p,error);
    p->io.barrier(p->io.context);
    return 0;
}
static int update(struct fes_nano_sd_platform *p, uintptr_t address, uint32_t mask, uint32_t value)
{
    uint32_t old;
    if (read_reg(p,address,&old) || write_reg(p,address,(old & ~mask)|value)) return -1;
    return check(p,address,mask,value);
}
static int poll(struct fes_nano_sd_platform *p, uintptr_t address, uint32_t mask, uint32_t expected)
{
    uint32_t value;
    p->last_mask=mask; p->last_expected=expected;
    for (unsigned i=0;i<FES_NANO_COLD_POLLS;i++) {
        if (read_reg(p,address,&value)) return -1;
        if ((value & mask)==expected) return 0;
        p->io.wait(p->io.context);
    }
    return fail(p,-60);
}
static int clocks(struct fes_nano_sd_platform *p, unsigned capture)
{
    uint32_t v[FES_NANO_SD_CLOCK_REGS];
    p->stage=FES_SD_CLOCK_SCOPE;
    for (unsigned i=0;i<FES_NANO_SD_CLOCK_REGS;i++) {
        if (read_reg(p,FES_NANO_SD_CLKMGR+clock_offsets[i],&v[i])) return -1;
        p->observed[i]=v[i];
    }
    /* CS/R1 RC28: MAIN_TRANS/PERF_TRANS are completion bits, high when
     * power-state transitions finish. BUSY alone has the opposite polarity. */
    v[1] &= 0x01030301U;
    v[13] &= 12U; /* lost-lock indications; unrelated IRQs are independent */
    if ((v[0]&1U) || v[1]!=0x30300U || (v[2]&0xf6U) || (v[3]&0xefU) ||
        !(v[12]&32U) || v[13] || (v[5]&0x30000U)) return fail(p,-5);
    unsigned source=(v[4]>>16)&7U;
    if (source>1U) return fail(p,-22);
    uint32_t glob=v[source ? 9 : 6], m=v[source ? 10 : 7]&0x3ffU;
    uint32_t c=v[source ? 11 : 8], reference=(glob>>8)&15U;
    /* This matched Nano board uses a 25 MHz external oscillator. Internal or
     * FPGA-supplied references are outside the qualified source policy. */
    if (((glob>>16)&3U) || !reference || !m || !(c&0x7ffU) ||
        (c&0x18000000U)!=0x08000000U) return fail(p,-22);
    uint64_t numerator=25000000ULL*m;
    uint64_t denominator=(uint64_t)reference*(c&0x7ffU)*
        (1U<<((v[5]>>4)&3U))*(1U<<((v[5]>>16)&3U));
    if (numerator!=denominator*FES_NANO_SD_INPUT_HZ) return fail(p,-22);
    for (unsigned i=0;i<FES_NANO_SD_CLOCK_REGS;i++) {
        if (capture) p->clocks[i]=v[i];
        else if (p->clocks[i]!=v[i]) return fail(p,-5);
    }
    return 0;
}
static int route(struct fes_nano_sd_platform *p)
{
    p->stage=FES_SD_ROUTE_SCOPE;
    return check(p,FES_NANO_SD_SYSMGR+0xfc,0xffffffffU,1) ||
           check(p,FES_NANO_SD_SYSMGR+0x1354,0xffffffffU,0) ? -1 : 0;
}
static int scope(void *context, uint64_t owner, uint64_t epoch, unsigned active)
{
    struct fes_nano_sd_platform *p=context;
    if (!p || p->failed || !p->initialized) return -1;
    p->stage=FES_SD_BOOT_SCOPE;
    if (active>1 || p->active!=active || fes_nano_cold_sd_scope(p->cold,owner,epoch,active))
        return fail(p,-1);
    if (clocks(p,0) || route(p) ||
        check(p,FES_NANO_COLD_RSTMGR+0x24,FES_NANO_SD_PHY_RESET,0)) return -1;
    if (p->prepared && check(p,FES_NANO_SD_BASE+PHY_STATUS,0x1800fU,0x1800fU)) return -1;
    if (fes_nano_cold_sd_scope(p->cold,owner,epoch,active)) return fail(p,-1);
    return 0;
}
static int phy(struct fes_nano_sd_platform *p)
{
    p->stage=FES_SD_PHY_CONFIG;
    if (update(p,FES_NANO_SD_BASE+PHY_STATUS,1U,0)) return -1;
    for (unsigned i=0;i<5;i++) {
        uint32_t old;
        if (write_reg(p,FES_NANO_SD_BASE+PHY_ADDRESS,phy_policy[i].address) ||
            read_reg(p,FES_NANO_SD_BASE+PHY_DATA,&old) ||
            write_reg(p,FES_NANO_SD_BASE+PHY_DATA,(old & ~phy_policy[i].mask)|phy_policy[i].value) ||
            check(p,FES_NANO_SD_BASE+PHY_DATA,phy_policy[i].mask,phy_policy[i].value)) return -1;
    }
    if (update(p,FES_NANO_SD_BASE+PHY_STATUS,1U,1U)) return -1;
    p->stage=FES_SD_PHY_READY;
    if (poll(p,FES_NANO_SD_BASE+PHY_STATUS,2U,2U)) return -1;
    unsigned i=5; uint32_t old;
    if (write_reg(p,FES_NANO_SD_BASE+PHY_ADDRESS,phy_policy[i].address) ||
        read_reg(p,FES_NANO_SD_BASE+PHY_DATA,&old) ||
        write_reg(p,FES_NANO_SD_BASE+PHY_DATA,(old & ~phy_policy[i].mask)|phy_policy[i].value) ||
        check(p,FES_NANO_SD_BASE+PHY_DATA,phy_policy[i].mask,phy_policy[i].value) ||
        update(p,FES_NANO_SD_BASE+MODE,7U,0) ||
        update(p,FES_NANO_SD_BASE+PHY_STATUS,0x1800cU,0x1800cU) ||
        update(p,FES_NANO_SD_BASE+CLOCK_ADJUST,0xf0000U,0x30000U) ||
        update(p,FES_NANO_SD_BASE+OUTPUT_DELAY,0xffffffffU,0) ||
        update(p,FES_NANO_SD_BASE+COMPENSATION,0x1f001fU,0xa0000U)) return -1;
    return check(p,FES_NANO_SD_BASE+PHY_STATUS,0x1800fU,0x1800fU);
}
static int resume(void *context)
{
    struct fes_nano_sd_platform *p=context;
    if (!p || p->failed || p->active || scope(p,p->cold->record.boot.owner,p->cold->record.boot.epoch,0)) return -1;
    p->stage=FES_SD_RESET_ASSERT;
    if (update(p,FES_NANO_COLD_RSTMGR+0x24,0xc0U,0xc0U)) return -1;
    p->io.wait(p->io.context);
    p->stage=FES_SD_PHY_RELEASE;
    if (update(p,FES_NANO_COLD_RSTMGR+0x24,0xc0U,0x80U)) return -1;
    p->stage=FES_SD_CONTROLLER_RELEASE;
    if (update(p,FES_NANO_COLD_RSTMGR+0x24,0xc0U,0)) return -1;
    p->active=1; p->prepared=0;
    p->stage=FES_SD_HOST_RESET;
    uint32_t reset;
    if (read_reg(p,FES_NANO_SD_BASE+HRS_RESET,&reset) ||
        write_reg(p,FES_NANO_SD_BASE+HRS_RESET,reset|1U) ||
        poll(p,FES_NANO_SD_BASE+HRS_RESET,1U,0)) return -1;
    if (write_reg(p,FES_NANO_SD_BASE+SIGNAL_ENABLE,0) ||
        write_reg(p,FES_NANO_SD_BASE+STATUS_ENABLE,0) ||
        check(p,FES_NANO_SD_BASE+SIGNAL_ENABLE,0xffffffffU,0) ||
        check(p,FES_NANO_SD_BASE+STATUS_ENABLE,0xffffffffU,0) ||
        check(p,FES_NANO_SD_BASE+CLOCK,4U,0) ||
        check(p,FES_NANO_SD_BASE+COMMAND,1U,0) || phy(p)) return -1;
    if (scope(p,p->cold->record.boot.owner,p->cold->record.boot.epoch,1)) return -1;
    p->stage=FES_SD_COMPLETE;
    return 0;
}
static int prepare(void *context)
{
    struct fes_nano_sd_platform *p=context;
    if (!p || p->failed || !p->active || p->prepared ||
        scope(p,p->cold->record.boot.owner,p->cold->record.boot.epoch,1)) return -1;
    p->stage=FES_SD_PREPARE_CLOCK;
    if (check(p,FES_NANO_SD_BASE+CLOCK,0x070fffffU,FES_NANO_SD_IDENT_CLOCK) ||
        check(p,FES_NANO_SD_BASE+SIGNAL_ENABLE,0xffffffffU,0) ||
        check(p,FES_NANO_SD_BASE+COMMAND,1U,0) ||
        check(p,FES_NANO_SD_BASE+PRESENT,FES_NANO_COLD_SD_BUSY,0) || phy(p) ||
        check(p,FES_NANO_SD_BASE+CLOCK,0x070fffffU,FES_NANO_SD_IDENT_CLOCK)) return -1;
    p->prepared=1;
    if (scope(p,p->cold->record.boot.owner,p->cold->record.boot.epoch,1)) return -1;
    p->stage=FES_SD_COMPLETE;
    return 0;
}
static int stop(void *context)
{
    struct fes_nano_sd_platform *p=context;
    if (!p || p->failed || !p->active ||
        scope(p,p->cold->record.boot.owner,p->cold->record.boot.epoch,1)) return -1;
    p->stage=FES_SD_STOP_MODE;
    if (check(p,FES_NANO_SD_BASE+COMMAND,1U,0) ||
        check(p,FES_NANO_SD_BASE+SIGNAL_ENABLE,0xffffffffU,0) ||
        check(p,FES_NANO_SD_BASE+STATUS_ENABLE,0xffffffffU,0) ||
        check(p,FES_NANO_SD_BASE+STATUS,0xffff8000U,0) ||
        check(p,FES_NANO_SD_BASE+CLOCK,0x070fffffU,FES_NANO_SD_IDENT_CLOCK)) return -1;
    p->stage=FES_SD_STOP_IDLE;
    if (poll(p,FES_NANO_SD_BASE+PRESENT,FES_NANO_COLD_SD_BUSY,0)) return -1;
    p->stage=FES_SD_STOP_CLOCK;
    if (update(p,FES_NANO_SD_BASE+CLOCK,4U,0) ||
        poll(p,FES_NANO_SD_BASE+PRESENT,FES_NANO_COLD_SD_BUSY,0) ||
        check(p,FES_NANO_SD_BASE+SIGNAL_ENABLE,0xffffffffU,0) ||
        check(p,FES_NANO_SD_BASE+STATUS_ENABLE,0xffffffffU,0) ||
        check(p,FES_NANO_SD_BASE+STATUS,0xffff8000U,0) ||
        check(p,FES_NANO_SD_BASE+COMMAND,1U,0)) return -1;
    p->stage=FES_SD_STOP_RESET;
    if (update(p,FES_NANO_COLD_RSTMGR+0x24,FES_NANO_COLD_SD_RESET,FES_NANO_COLD_SD_RESET)) return -1;
    p->active=p->prepared=0;
    if (scope(p,p->cold->record.boot.owner,p->cold->record.boot.epoch,0)) return -1;
    p->stage=FES_SD_COMPLETE;
    return 0;
}
int fes_nano_sd_platform_init(struct fes_nano_sd_platform *p, struct fes_nano_cold *cold,
                              struct fes_nano_sd_io io, unsigned qualifications)
{
    if (!p || p->attempted) return -1;
    p->attempted=1; p->cold=cold;
    if (!cold || !io.read || !io.write || !io.barrier || !io.wait ||
        qualifications!=FES_NANO_SD_QUALIFIED) return fail(p,-22);
    p->io=io;
    p->stage=FES_SD_BOOT_SCOPE;
    if (fes_nano_cold_sd_scope(cold,cold->record.boot.owner,cold->record.boot.epoch,0)) return fail(p,-1);
    if (clocks(p,1) || route(p)) return -1;
    p->initialized=1;
    return scope(p,cold->record.boot.owner,cold->record.boot.epoch,0);
}
struct fes_nano_sd_ops fes_nano_sd_platform_ops(struct fes_nano_sd_platform *p)
{
    return (struct fes_nano_sd_ops){p,scope,resume,prepare,stop};
}
