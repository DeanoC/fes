/* SPDX-License-Identifier: BSD-3-Clause */
/* Terasic DE25 d0f8813: USB bus clock is L4_MP, not the external ULPI clock.
 * Complete pin layout uses the existing sealed hps_io.py/nano_hps_io policy.
 * USB3320 register semantics: Microchip DS00001792F. GPVNDCTL uses the
 * Synopsys DWC OTG register layout (RegAddr[21:16], NewRegReq[25]).
 * This is a checked platform implementation, not physical qualification. */
#include "nano_usb_board.h"
#include <string.h>

#define CLK_BASE 0x10d10000UL
#define PER0 (FES_NANO_COLD_RSTMGR+0x24)
#define VIEW (FES_NANO_USB_BASE+0x34)
#define REQUEST (1U<<25)
#define BUSY (1U<<26)
#define DONE (1U<<27)
static const unsigned clock_offsets[FES_NANO_USB_CLOCK_COUNT]={
    0,4,0x30,0x88,0x40,0x44,0x48,0x6c,0x68,0x9c,0xc0,0xb4,0x18
};
static int fail(struct fes_nano_usb_board *b,int error)
{
    b->failed=1;
    if (!b->first_error) b->first_error=error ? error : -5;
    if (b->cold) {b->cold->failed=1;b->cold->valid=0;}
    return b->first_error;
}
static int read_reg(struct fes_nano_usb_board *b,uintptr_t address,uint32_t *value)
{
    if (b->failed) return -1;
    b->last_address=address;
    b->physical.barrier(b->physical.context);
    int error=b->physical.read(b->physical.context,address,value);
    b->physical.barrier(b->physical.context);
    if (error) return fail(b,error);
    b->last_value=*value;return 0;
}
static int scope(struct fes_nano_usb_board *b,unsigned *active)
{
    uint32_t per0;
    if (read_reg(b,PER0,&per0)) return -1;
    *active=(per0&FES_NANO_USB_RESET)==0;
    if (fes_nano_cold_usb_scope(b->cold,b->owner,b->epoch,*active)) return fail(b,-5);
    return 0;
}
static int clocks(struct fes_nano_usb_board *b,unsigned capture)
{
    uint32_t v[FES_NANO_USB_CLOCK_COUNT];
    uint32_t previous=b->last_value;
    /* Independent read-only policy registers need ordering at the sample's
     * boundaries, not a full barrier around each word. Every value is still
     * synchronously read afresh before any controller operation or grant.
     * Controller/PHY read_reg() ordering remains unchanged. */
    b->physical.barrier(b->physical.context);
    for (unsigned i=0;i<FES_NANO_USB_CLOCK_COUNT;i++) {
        uintptr_t address=CLK_BASE+clock_offsets[i];
        if (b->failed) {b->physical.barrier(b->physical.context);return -1;}
        int error=b->physical.read(b->physical.context,address,&v[i]);
        if (error) {
            b->physical.barrier(b->physical.context);
            b->last_address=address;b->last_value=previous;
            return fail(b,error);
        }
        previous=v[i];
    }
    b->physical.barrier(b->physical.context);
    b->last_address=CLK_BASE+clock_offsets[FES_NANO_USB_CLOCK_COUNT-1];
    b->last_value=previous;
    v[1]&=0x01030301U;v[12]&=12U;
    if ((v[0]&1U) || v[1]!=0x30300U || (v[2]&0xf6U) || (v[3]&0xefU) ||
        v[12]) return fail(b,-5);
    unsigned source=(v[4]>>16)&7U;
    if (source>1U) return fail(b,-22);
    uint32_t glob=v[source?9:6],m=v[source?10:7]&0x3ffU;
    uint32_t c=v[source?11:8],reference=(glob>>8)&15U;
    if (((glob>>16)&3U) || !reference || !m || !(c&0x7ffU) ||
        (c&0x18000000U)!=0x08000000U) return fail(b,-22);
    uint64_t numerator=25000000ULL*m;
    uint64_t denominator=(uint64_t)reference*(c&0x7ffU)*(1U<<((v[5]>>4)&3U));
    if (numerator!=denominator*FES_NANO_USB_BUS_HZ) return fail(b,-22);
    for (unsigned i=0;i<FES_NANO_USB_CLOCK_COUNT;i++) {
        if (capture) b->clocks[i]=v[i];
        else if (v[i]!=b->clocks[i]) return fail(b,-5);
    }
    return 0;
}
static int pins(struct fes_nano_usb_board *b)
{
    uint32_t previous=b->last_value;
    b->physical.barrier(b->physical.context);
    for (unsigned i=0;i<FES_NANO_HPS_IO_COUNT;i++) {
        uintptr_t address=FES_NANO_HPS_IO_BASE+b->pins[i].offset;
        uint32_t value;
        if (b->failed) {b->physical.barrier(b->physical.context);return -1;}
        int error=b->physical.read(b->physical.context,address,&value);
        if (error || value!=b->pins[i].value) {
            b->physical.barrier(b->physical.context);
            b->last_address=address;b->last_value=error ? previous : value;
            return fail(b,error ? error : -5);
        }
        previous=value;
    }
    b->physical.barrier(b->physical.context);
    b->last_address=FES_NANO_HPS_IO_BASE+b->pins[FES_NANO_HPS_IO_COUNT-1].offset;
    b->last_value=previous;
    return 0;
}
static int prepare(void *context)
{
    struct fes_nano_usb_board *b=context;unsigned active;
    if (!b || !b->initialized || b->failed) return 0;
    /* Every invocation checks the complete captured clock vector and compiled
     * pins against their immutable expected values. The caller brackets the
     * actual controller/PHY operation with fresh preparation; ready() also
     * prepares before and after its independent PHY observations. Repeating
     * the same scan immediately here adds no operation boundary or authority.
     * Initial capture/recheck in board_init remains separate. */
    if (scope(b,&active) || clocks(b,0) || pins(b) || scope(b,&active)) return 0;
    return 1;
}
static int prepare_master(void *context)
{
    struct fes_nano_usb_board *b=context;unsigned active;
    if (!b || !b->initialized || b->failed) return 0;
    /* A controller with DMA/IRQ disabled and an independently idle AHB
     * master cannot access HPS memory through its external USB pins. Keep
     * the complete fresh bus-clock and cold scope proof for meaningful CSR
     * access; pin/PHY qualification remains mandatory for device readiness. */
    if (scope(b,&active) || clocks(b,0) || scope(b,&active)) return 0;
    return 1;
}
static int ulpi(struct fes_nano_usb_board *b,unsigned address,uint8_t *value)
{
    uint32_t v;
    if (read_reg(b,VIEW,&v)) return -1;
    if (v&(REQUEST|BUSY|1U<<31)) return fail(b,-5);
    uint32_t command=REQUEST|(address<<16);
    b->last_address=VIEW;
    int error=b->physical.write(b->physical.context,VIEW,command);
    b->physical.barrier(b->physical.context);
    if (error) return fail(b,error);
    for (unsigned i=0;i<FES_NANO_COLD_POLLS;i++) {
        if (read_reg(b,VIEW,&v)) return -1;
        if (!(v&(REQUEST|BUSY)) && (v&DONE)) {
            if ((v&(0x3fU<<16|1U<<22|1U<<31))!=(address<<16)) return fail(b,-5);
            *value=(uint8_t)v;return 0;
        }
        b->physical.wait(b->physical.context);
    }
    return fail(b,-60);
}
static int core(struct fes_nano_usb_board *b)
{
    static const struct {unsigned offset;uint32_t mask,want;} policy[]={
        {8,0x21U,0},{12,0x600000d8U,0x20000010U},{20,1,1},{24,0xffffffffU,0},
        {0x400,1U<<23,0},{0x440,1U<<12|1U<<8|1U<<4,1U<<12},{0xe00,0x3fU,0}
    };
    for (unsigned i=0;i<sizeof(policy)/sizeof(policy[0]);i++) {
        uint32_t v;
        if (read_reg(b,FES_NANO_USB_BASE+policy[i].offset,&v)) return -1;
        if ((v&policy[i].mask)!=policy[i].want) return fail(b,-5);
    }
    return 0;
}
static int ready(void *context)
{
    struct fes_nano_usb_board *b=context;unsigned active;
    static const unsigned addresses[]={0,1,2,3,4,7,0x0a,0x13};
    if (!prepare(b) || scope(b,&active) || !active) return 0;
    for (unsigned pass=0;pass<2;pass++) {
        if (core(b)) return 0;
        uint8_t values[8];
        for (unsigned i=0;i<8;i++) if (ulpi(b,addresses[i],&values[i])) return 0;
        if (((unsigned)values[0]|(unsigned)values[1]<<8)!=FES_NANO_USB_PHY_VENDOR ||
            ((unsigned)values[2]|(unsigned)values[3]<<8)!=FES_NANO_USB_PHY_PRODUCT ||
            (values[4]&0x78U)!=0x40U || (values[5]&7U) ||
            (values[6]&0x9eU)!=6U || !(values[6]&0x60U) || (values[7]&14U)!=6U)
            {fail(b,-5);return 0;}
        if (!pass) memcpy(b->phy,values,sizeof(values));
        else if (memcmp(b->phy,values,7) || (b->phy[7]&14U)!=(values[7]&14U)) {fail(b,-5);return 0;}
        if (scope(b,&active) || !active || core(b)) return 0;
    }
    if (!prepare(b)) return 0;
    b->live_checks++;return 1;
}
int fes_nano_usb_board_init(struct fes_nano_usb_board *b,struct fes_nano_cold *cold,
                            struct fes_nano_usb_io io,
                            const struct fes_nano_hps_io_register *rows,unsigned count,
                            const uint8_t digest[32])
{
    if (!b || b->attempted) return -1;
    b->attempted=1;
    unsigned nonzero=0;
    if (digest) for (unsigned i=0;i<32;i++) nonzero|=digest[i];
    if (!cold || !cold->valid || cold->failed || !rows || count!=FES_NANO_HPS_IO_COUNT ||
        !nonzero || !io.read || !io.write || !io.barrier || !io.wait) return fail(b,-22);
    unsigned selected=0;
    for (uint32_t off=0x1000;off<=0x14bc;off+=4) {
        if (!fes_nano_hps_io_offset(off)) continue;
        if (selected>=count || rows[selected].offset!=off || rows[selected].mask!=0xffffffffU ||
            (off<0x1130 && rows[selected].value>15) ||
            (off>=0x1300 && off<=0x1354 && rows[selected].value>1)) return fail(b,-22);
        b->pins[selected]=rows[selected];selected++;
    }
    if (selected!=count) return fail(b,-22);
    b->physical=io;b->cold=cold;b->owner=cold->record.boot.owner;b->epoch=cold->record.boot.epoch;
    memcpy(b->policy_digest,digest,32);
    unsigned active;
    if (scope(b,&active) || active || clocks(b,1) || pins(b) || clocks(b,0) || pins(b) ||
        scope(b,&active) || active) return fail(b,-5);
    b->initialized=1;
    return 0;
}
static int physical_read(void *context,uintptr_t address,uint32_t *value)
{
    struct fes_nano_usb_board *b=context;
    if (!b || !b->initialized || b->failed) return -1;
    return b->physical.read(b->physical.context,address,value);
}
static int physical_write(void *context,uintptr_t address,uint32_t value)
{
    struct fes_nano_usb_board *b=context;
    if (!b || !b->initialized || b->failed) return -1;
    return b->physical.write(b->physical.context,address,value);
}
static void physical_barrier(void *context)
{struct fes_nano_usb_board *b=context;b->physical.barrier(b->physical.context);}
static void physical_wait(void *context)
{struct fes_nano_usb_board *b=context;b->physical.wait(b->physical.context);}
static int prepared_ready(void *context,struct fes_nano_cold *cold,uint64_t owner,uint64_t epoch)
{
    struct fes_nano_usb_board *b=context;
    /* Do not touch another boot's objects, even on a bad trusted binding. */
    if (!b || !b->initialized || b->failed || b->cold!=cold ||
        b->owner!=owner || b->epoch!=epoch) return 0;
    /* ready() already checks the full immutable clock/pin policy before and
     * after the controller/PHY observations. Repeat the independent cold
     * ownership check, not another adjacent policy scan with no intervening
     * controller operation. Both PHY passes and all PIO predicates remain. */
    if (!ready(b) || fes_nano_cold_usb_scope(cold,owner,epoch,1)) return 0;
    return 1;
}
struct fes_nano_usb_io fes_nano_usb_board_ops(struct fes_nano_usb_board *b)
{
    return (struct fes_nano_usb_io){b,physical_read,physical_write,physical_barrier,physical_wait,ready,prepare,prepared_ready,prepare_master};
}
