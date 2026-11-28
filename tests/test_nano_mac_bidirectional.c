/* SPDX-License-Identifier: BSD-3-Clause */
/* The exact TX-only fixture supplies MMIO, table walks and command acks. */
#define main tx_single_fixture_main
#include "test_nano_mac_positive.c"
#undef main
#include "mac_rx_diagnostic.h"

struct endpoint {
    struct fixture f;
    unsigned grants, received, mode, post_stop_reads;
};
static uint32_t endpoint_read(void *ctx, uint32_t address)
{
    struct endpoint *e=ctx;
    if (e->mode==10 && address==M+0x3020 && (*reg(&e->f,M+0x3160) & (1U<<8)) &&
        ++e->post_stop_reads==3)
        return 1; /* Both RX idle samples passed, shared AXI no longer idle. */
    return read32(&e->f,address);
}
static void grant(void *ctx,unsigned vmid,unsigned cmd,unsigned ste)
{
    struct endpoint *e=ctx;
    assert(++e->grants==vmid && ste==13 && cmd==(vmid==1 ? 6U : 2U));
    assert((get(ctx,FES_TX_TABLES+64+16)&0xffff)==vmid);
    assert((*reg(&e->f,R)&0x101)==0x101);
}
static void caps(void *ctx,const struct fes_tx_caps *c)
{
    assert(c->version==0x107631 && c->interface==1 && c->feature0==0xcafd5b3f);
    (void)ctx;
}
static void phy(void *ctx,unsigned sample,const struct fes_tx_phy_sample *s)
{
    (void)ctx;(void)sample;assert(s->resolved==0x0348 && s->pcs==0x0806);
}
static void channel(void *ctx,uint32_t address,uint32_t value)
{
    struct endpoint *e=ctx;
    struct fixture *f=&e->f;
    unsigned old=*reg(f,address);
    /* Original fixture's CFGI_STE asserts invalid/abort because TX has no
     * regrant. VMID2's make publishes a translated STE before invalidation. */
    if (address==S+0x98 && get(f,FES_TX_TABLES+64)==13 &&
        get(f,FES_TX_CMDQ+16*(*reg(f,S+0x9c)&7))==(3ULL|(1ULL<<32))) {
        assert((get(f,FES_TX_TABLES+64+16)&0xffff)==2);
        *reg(f,S+0x98)=*reg(f,S+0x9c)=value;
        return;
    }
    /* Original TX-only tail observes disabled RX, as that profile requires. */
    unsigned cfg=*reg(f,M+4);
    if (address==M+0x3124) *reg(f,M+4)=0;
    write32(f,address,value);
    if (address==M+0x3124) *reg(f,M+4)=cfg;
    if (address==M+0x3108) {
        if (value&1) *reg(f,M+0x3020)=e->mode==8 ? 0 : 0x300;
        else if (old&1) {
            *reg(f,M+0x3020)=0;
            *reg(f,M+0x3160)|=1U<<8;
        }
    }
    /* Fail the MAC RX enable readback after the DMA channel has started. */
    if (e->mode==7 && address==M+4 && (value&1)) *reg(f,address)=value&~1U;
    if (address!=M+0x3124) return;
    assert((cfg&1) && (*reg(f,M+0x3108)&1));
    assert(*reg(f,M+0x311c)==FES_RX_RING_IOVA);
    uint32_t desc=translate(f,FES_RX_RING_IOVA,1);
    assert(desc==FES_RX_DESC && get(f,desc)==FES_RX_DATA_IOVA+FES_RX_OFFSET);
    const uint32_t index=(FES_RX_DATA_IOVA>>12)&511;
    uint64_t leaf=get(f,FES_TX_TABLES+0x6000+8*index);
    assert(((leaf>>6)&3)==2 && ((uint32_t)leaf&~4095U)==FES_RX_DATA);
    assert(!get(f,FES_RX_DATA_IOVA) && !get(f,FES_RX_DATA_IOVA+FES_RX_OFFSET));
    uint8_t reply[60];
    memcpy(reply,f->wire,60);
    memcpy(reply,f->wire+6,6);memcpy(reply+6,f->wire,6);reply[22]='R';
    if (e->mode==1) reply[28]^=1;
    memcpy(ram(f,FES_RX_DATA+FES_RX_OFFSET),reply,60);
    put(f,desc+8,(uint64_t)0x3000003c<<32);
    e->received++;
    if (e->mode==9) *reg(f,M+0x3164)=1U<<28; /* RX descriptor write not retired. */
    if (e->mode==8) *reg(f,M+0x3160)|=0x84; /* BIDI04 RBU, TBU and generic debug zero. */
}
static void run(unsigned mode)
{
    struct endpoint e={0};
    e.mode=mode;
    struct fixture *f=&e.f;
    initial_phy(f);
    f->reset=0xff7fbfff; f->ctrl=0x30;
    *reg(f,S)=0x080f7e3f; *reg(f,S+4)=0x0e739d18; *reg(f,S+0x14)=0x400075;
    *reg(f,S+0x2c)=4;
    *reg(f,M+0x110)=0x107631; *reg(f,M+0x11c)=0xcafd5b3f;
    *reg(f,M+0x120)=0x49ef7a27; *reg(f,M+0x124)=0x221c71c7;
    struct fes_tx_io io={&e,endpoint_read,channel,get,put,barrier,now,owns,observe,caps,phy,grant};
    uint8_t frame[60]={2,0,0,0,0,0x45,2,0xfe,0x25,8,0xcb,1,0x88,0xb5};
    memcpy(frame+14,"FES-CBL-TX-v1",14);memset(frame+28,0x71,16);
    struct fes_rx_result r1={0},r2={0};
    struct fes_tx_result t1={.rx=&r1}, t2={.rx=&r2,.previous=&t1};
    int rc=fes_tx_once(&io,frame,&t1);
    if (mode==10) {
        assert(rc && t1.process_stopped && r1.stopped && r1.stop.phase==6);
        assert(!r1.stop.shared && !t1.retained && !t1.phy.restored && t1.translation_revoked);
        assert(fes_tx_terminal(&io,&t1,rc)==FES_TX_COLD_RECOVERY);
        assert(fes_tx_once(&io,frame,&t2) && e.grants==1);
        return;
    }
    if (mode==9) {
        assert(rc && t1.process_stopped && r1.matched && !r1.stopped && r1.stop.phase==3);
        assert(!r1.stop.shared && !t1.phy.restored && !t1.retained && t1.translation_revoked);
        assert(fes_tx_terminal(&io,&t1,rc)==FES_TX_COLD_RECOVERY);
        assert(fes_tx_once(&io,frame,&t2) && e.grants==1);
        return;
    }
    if (mode==7) {
        assert(rc && r1.dma_possible && !r1.armed && !r1.stopped);
        assert(!t1.dma_possible && !t1.dispatched && !f->transmits);
        assert(t1.phy.applied && !t1.phy.restored && f->restarts==1 && f->timing_writes==2);
        assert(t1.translation_revoked && t1.mac_in_reset && get(f,FES_TX_TABLES+64)==1);
        assert(fes_tx_terminal(&io,&t1,rc)==FES_TX_COLD_RECOVERY);
        assert(fes_tx_once(&io,frame,&t2) && e.grants==1);
        return;
    }
    if (mode==1) {
        assert(rc && fes_tx_terminal(&io,&t1,rc)==FES_TX_FAILED_DRAINED);
        assert(r1.failure==FES_RX_WRONG_FRAME && !r1.matched && r1.stopped && t1.retained);
        assert(fes_tx_once(&io,frame,&t2) && e.grants==1);
        return;
    }
    assert(!rc && fes_tx_terminal(&io,&t1,rc)==FES_TX_CLOSED);
    assert(t1.vmid==1 && t1.command==12 && t1.retained && t1.dwell_us>=10000);
    if(mode!=2) frame[28]^=1;
    if (mode==3) put(f,FES_RX_DATA+FES_RX_OFFSET,0);
    if (mode==4) put(f,FES_TX_TABLES+7*64,13);
    if (mode==5) *reg(f,S+0x9c)=10;
    if (mode==6) t1.dwell_us=0;
    rc=fes_tx_once(&io,frame,&t2);
    if(mode>=2 && mode<=7) {
        assert(rc && e.grants==1 && !t1.consumed && !t2.arm_attempted);
        return;
    }
    assert(!rc && fes_tx_terminal(&io,&t2,rc)==FES_TX_CLOSED);
    assert(t1.consumed && t2.vmid==2 && t2.command==8 && t2.retained && t2.dwell_us>=10000);
    assert(e.grants==2 && e.received==2 && f->transmits==2 && r1.bytes[28]!=r2.bytes[28]);
    struct fes_rx_result r3={0};
    struct fes_tx_result t3={.rx=&r3,.previous=&t1};
    assert(fes_tx_once(&io,frame,&t3) && e.grants==2);
}
int main(void)
{
    for (unsigned mode=0;mode<11;mode++) run(mode);
    puts("two nonce-bound TX/RX grants, abort dwell, stale regrant, and contained failure verified");
}
