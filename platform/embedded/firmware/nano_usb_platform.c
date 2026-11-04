/* SPDX-License-Identifier: BSD-3-Clause */
/* USB0 base/reset: pinned Terasic DE25 U-Boot DTS and altr,rst-mgr-agx5.h.
 * DWC2 PIO/ULPI/channel/flush policy follows the same register protocol as the
 * native host. PHY/VBUS/clock qualification remains an injected trusted policy.
 * Only the selected USB-only cold image installs it; no combined SD/USB use. */
#include "nano_usb_platform.h"
#include <stdint.h>

enum { GAHB=8, GUSB=12, RESET=16, INTMASK=24, HWID=0x40, CFG2=0x48,
       CFG3=0x4c, PORT=0x440, CHANNEL=0x500, CHANNEL_INT=0x508,
       CHANNEL_MASK=0x50c };
#define ENABLE (1U<<31)
#define DISABLE (1U<<30)
#define PER0 (FES_NANO_COLD_RSTMGR+0x24)

static int fail(struct fes_nano_usb_platform *p, int error)
{
    p->failed=1;
    if (!p->first_error) p->first_error=error ? error : -5;
    if (p->cold) { p->cold->failed=1; p->cold->valid=0; }
    return p->first_error;
}
static int read_reg(struct fes_nano_usb_platform *p, uintptr_t address, uint32_t *v)
{
    if (p->failed) return -1;
    p->last_address=address;
    int error=p->io.read(p->io.context,address,v);
    if (error) return fail(p,error);
    p->last_value=*v;
    return 0;
}
static int write_reg(struct fes_nano_usb_platform *p, uintptr_t address, uint32_t v)
{
    if (p->failed) return -1;
    p->last_address=address;
    int error=p->io.write(p->io.context,address,v);
    if (error) return fail(p,error);
    p->io.barrier(p->io.context);
    return 0;
}
static int poll(struct fes_nano_usb_platform *p, unsigned offset, uint32_t mask, uint32_t want)
{
    uint32_t v;
    for (unsigned i=0;i<FES_NANO_COLD_POLLS;i++) {
        if (read_reg(p,FES_NANO_USB_BASE+offset,&v)) return -1;
        if ((v&mask)==want) return 0;
        p->io.wait(p->io.context);
    }
    return fail(p,-60);
}
static int admit(struct fes_nano_usb_platform *p, uint64_t owner, uint64_t generation)
{
    /* A stale or foreign request performs no platform reads/writes. */
    return !p || !p->initialized || p->failed || p->owner!=owner ||
           p->generation!=generation ? -1 : 0;
}
static int scope(struct fes_nano_usb_platform *p)
{
    if (p->io.prepare(p->io.context)!=1 ||
        fes_nano_cold_usb_scope(p->cold,p->owner,p->epoch,p->active)) return fail(p,-5);
    return 0;
}
static int pio_owner(struct fes_nano_usb_platform *p)
{
    uint32_t ahb, mask;
    if (read_reg(p,FES_NANO_USB_BASE+GAHB,&ahb) ||
        read_reg(p,FES_NANO_USB_BASE+INTMASK,&mask)) return -1;
    /* Do not silently disable a foreign DMA/IRQ owner or accept changed PHY. */
    if ((ahb&0x21U) || mask) return fail(p,-5);
    return 0;
}
static int pio(struct fes_nano_usb_platform *p)
{
    uint32_t phy;
    if (pio_owner(p) || read_reg(p,FES_NANO_USB_BASE+GUSB,&phy)) return -1;
    if ((phy&0xd8U)!=0x10U) return fail(p,-5);
    return 0;
}
int fes_nano_usb_platform_init(struct fes_nano_usb_platform *p, struct fes_nano_cold *cold,
                              struct fes_nano_usb_io io, unsigned qualifications)
{
    if (!p || p->attempted) return -1;
    p->attempted=1;
    if (!cold || !cold->valid || cold->failed || qualifications!=FES_NANO_USB_QUALIFIED ||
        !io.read || !io.write || !io.barrier || !io.wait || !io.ready || !io.prepare) return fail(p,-22);
    p->cold=cold;p->io=io;
    p->owner=cold->record.boot.owner;p->epoch=cold->record.boot.epoch;
    if (scope(p)) return -1;
    p->initialized=1;
    return 0;
}
int fes_nano_usb_platform_observe(struct fes_nano_usb_platform *p, uint64_t owner,
                                 uint64_t generation)
{
    if (admit(p,owner,generation)) return -1;
    if (scope(p)) return -1;
    if (p->active && pio(p)) return -1;
    if (p->confirmed && p->io.ready(p->io.context)!=1) return fail(p,-5);
    return scope(p);
}
int fes_nano_usb_platform_acquire(struct fes_nano_usb_platform *p, uint64_t owner,
                                 uint64_t generation)
{
    uint32_t per0,id,cfg,depth,phy;
    if (admit(p,owner,generation) || p->active) return -1;
    if (p->generation==UINT64_MAX) return fail(p,-75);
    if (scope(p) || read_reg(p,PER0,&per0)) return -1;
    /* Reserve before the first mutation. Partial release never publishes a
     * closed scope, and this lease cannot be retried after uncertainty. */
    p->active=1;
    if (write_reg(p,PER0,per0&~FES_NANO_USB_RESET) || scope(p) ||
        read_reg(p,FES_NANO_USB_BASE+HWID,&id) ||
        read_reg(p,FES_NANO_USB_BASE+CFG2,&cfg) ||
        read_reg(p,FES_NANO_USB_BASE+CFG3,&depth)) return -1;
    unsigned mode=cfg&7U, architecture=(cfg>>3)&3U;
    if (id<0x4f542000U || id>=0x4f544000U || mode==3 || mode==4 || mode==7 ||
        architecture==1 || architecture==3 || !(cfg&(1U<<19)) || depth>>16<256) return fail(p,-22);
    p->channels=((cfg>>14)&15U)+1;
    if (pio_owner(p)) return -1;
    for (unsigned i=0;i<p->channels;i++) {
        uint32_t v;
        if (read_reg(p,FES_NANO_USB_BASE+CHANNEL+32*i,&v)) return -1;
        if (v&ENABLE) return fail(p,-5);
    }
    /* Only ULPI single-data-rate selection is established here. Host startup
     * and live PHY/VBUS confirmation are separate; no readiness is published. */
    if (read_reg(p,FES_NANO_USB_BASE+GUSB,&phy) ||
        write_reg(p,FES_NANO_USB_BASE+GUSB,(phy&~0xd8U)|0x10U) || pio(p) ||
        poll(p,RESET,ENABLE,ENABLE) || scope(p)) return -1;
    p->generation++;
    return 0;
}
int fes_nano_usb_platform_confirm(struct fes_nano_usb_platform *p, uint64_t owner,
                                 uint64_t generation)
{
    if (admit(p,owner,generation) || !p->active || p->confirmed) return -1;
    if (p->generation==UINT64_MAX) return fail(p,-75);
    if (scope(p) || pio(p)) return -1;
    if (p->io.ready(p->io.context)!=1) return fail(p,-5);
    if (scope(p) || pio(p)) return -1;
    p->confirmed=1;p->generation++;
    return 0;
}
int fes_nano_usb_platform_stop(struct fes_nano_usb_platform *p, uint64_t owner,
                              uint64_t generation)
{
    uint32_t v,per0;
    if (admit(p,owner,generation)) return -1;
    if (!p->active) return scope(p); /* Already held: no controller accesses. */
    if (p->generation==UINT64_MAX) return fail(p,-75);
    if (scope(p) || pio(p)) return -1;
    for (unsigned i=0;i<p->channels;i++) {
        unsigned offset=CHANNEL+32*i;
        if (read_reg(p,FES_NANO_USB_BASE+offset,&v)) return -1;
        if (v&ENABLE) {
            if (write_reg(p,FES_NANO_USB_BASE+offset,v|ENABLE|DISABLE) ||
                poll(p,offset,ENABLE,0)) return -1;
        }
        if (write_reg(p,FES_NANO_USB_BASE+CHANNEL_MASK+32*i,0) ||
            write_reg(p,FES_NANO_USB_BASE+CHANNEL_INT+32*i,0x7ffU)) return -1;
    }
    if (read_reg(p,FES_NANO_USB_BASE+PORT,&v) ||
        write_reg(p,FES_NANO_USB_BASE+PORT,v&~(0x2eU|1U<<12|1U<<8)) ||
        read_reg(p,FES_NANO_USB_BASE+PORT,&v)) return -1;
    if (v&(1U<<12|1U<<8)) return fail(p,-5);
    if (poll(p,RESET,ENABLE,ENABLE) ||
        write_reg(p,FES_NANO_USB_BASE+RESET,0x10U<<6|1U<<5|1U<<4) ||
        poll(p,RESET,1U<<5|1U<<4,0) || poll(p,RESET,ENABLE,ENABLE) ||
        pio(p) || scope(p) || read_reg(p,PER0,&per0) ||
        write_reg(p,PER0,per0|FES_NANO_USB_RESET)) return -1;
    /* Do not publish inactive until independent reset/boot readback succeeds. */
    if (fes_nano_cold_usb_scope(p->cold,p->owner,p->epoch,0)) return fail(p,-5);
    p->active=0;p->confirmed=0;p->generation++;
    return 0;
}
