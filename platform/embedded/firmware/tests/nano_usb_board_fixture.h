/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_TEST_NANO_USB_BOARD_FIXTURE_H
#define FES_TEST_NANO_USB_BOARD_FIXTURE_H
#ifndef USB_BOARD_REGS
#define USB_BOARD_REGS(t) ((t)->base.h.regs)
#endif
#include "nano_usb_board.h"
#include "usb_board_policy.h"
static const struct fes_nano_hps_io_register compiled_pins[FES_NANO_HPS_IO_COUNT]=FES_NANO_USB_BOARD_PINS;
static const uint8_t compiled_digest[32]=FES_NANO_USB_BOARD_DIGEST;
struct board_test {
    struct test base;
    struct fes_nano_usb_board board;
    uint32_t clocks[64],pins[0x200];
    uint8_t phy[64];
    uint32_t viewport;
    unsigned pending,requests,waits,accesses,fail_at,fault;
};
static int board_read(void *context,uintptr_t address,uint32_t *value)
{
    struct board_test *t=context;
    if (++t->accesses==t->fail_at) return -99;
    if (address>=0x10d10000UL && address<0x10d10100UL) {*value=t->clocks[(address-0x10d10000UL)/4];return 0;}
    if (address>=FES_NANO_HPS_IO_BASE+0x1000 && address<=FES_NANO_HPS_IO_BASE+0x14bc) {
        *value=t->pins[(address-FES_NANO_HPS_IO_BASE-0x1000)/4];return 0;
    }
    if (address==FES_NANO_USB_BASE+0xe00) {*value=0;return 0;}
    if (address==FES_NANO_USB_BASE+0x34) {
        assert(!(t->base.h.per0&FES_NANO_USB_RESET));
        if (t->pending && t->fault!=1) {
            if (--t->pending==0) {
                unsigned reg=(t->viewport>>16)&63;
                t->viewport=(1U<<27)|(reg<<16)|t->phy[reg];
                if (t->fault==2) t->viewport^=1U<<16;
                if (t->fault==3) t->viewport|=1U<<31;
            }
        }
        *value=t->viewport;return 0;
    }
    return read32(&t->base.h,address,value);
}
static int board_write(void *context,uintptr_t address,uint32_t value)
{
    struct board_test *t=context;
    if (++t->accesses==t->fail_at) return -99;
    if (address==FES_NANO_USB_BASE+0x34) {
        assert(!(t->base.h.per0&FES_NANO_USB_RESET));
        assert((value&~(0x3fU<<16|1U<<25))==0); /* PHY reads only. */
        assert(!t->pending);
        t->requests++;t->viewport=value|(1U<<26);t->pending=2;return 0;
    }
    return write32(&t->base.h,address,value);
}
static void board_wait(void *context) {((struct board_test *)context)->waits++;}
static void setup(struct board_test *t)
{
    memset(t,0,sizeof(*t));boot(&t->base);
    t->clocks[4/4]=0x02030300;t->clocks[0x44/4]=1U<<4;
    t->clocks[0x48/4]=t->clocks[0x9c/4]=1U<<8;
    t->clocks[0x6c/4]=t->clocks[0xc0/4]=64;
    t->clocks[0x68/4]=t->clocks[0xb4/4]=0x08000004;
    t->clocks[0x7c/4]=0; /* USB readiness must not depend on SDMMC clock gating. */
    for (unsigned i=0;i<FES_NANO_HPS_IO_COUNT;i++)
        t->pins[(compiled_pins[i].offset-0x1000)/4]=compiled_pins[i].value;
    t->phy[0]=0x24;t->phy[1]=4;t->phy[2]=7;t->phy[3]=0;
    t->phy[4]=0x40;t->phy[7]=0;t->phy[0xa]=0x66;t->phy[0x13]=6;
}
static void host_started(struct board_test *t)
{
    /* Observations left by the real host initialization; this model does not
     * enumerate a USB device or establish electrical acceptance. */
    USB_BOARD_REGS(t)[12/4]=1U<<29|1U<<4;
    USB_BOARD_REGS(t)[20/4]=1;
    USB_BOARD_REGS(t)[0x440/4]=1U<<12;
}

#endif
