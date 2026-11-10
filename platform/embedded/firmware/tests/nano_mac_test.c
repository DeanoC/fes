/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_mac_fixture.h"
int main(void)
{
    struct fes_nano_mac s={0};struct mac_hardware h={0};uint64_t r[4];
    mac_call(&s,0,r);mac_empty_reply(r,FES_EMBEDDED_MAC_NOT_READY);
    assert(fes_nano_mac_gate(&s)==0 && fes_nano_mac_gate(0)==0);
    for (unsigned bit=1;bit<32;bit<<=1) {
        memset(&s,0,sizeof(s));
        assert(fes_nano_mac_init(&s,mac_binding(),mac_ops(&h),31U^bit));
        assert(s.failed && !s.reservation && h.scopes==0);
    }
    for (unsigned missing=0;missing<4;missing++) {
        memset(&s,0,sizeof(s));struct fes_nano_mac_ops ops=mac_ops(&h);
        if(missing==0)ops.scope=0;
        if(missing==1)ops.acquire=0;
        if(missing==2)ops.confirm=0;
        if(missing==3)ops.stop=0;
        assert(fes_nano_mac_init(&s,mac_binding(),ops,31));assert(!h.scopes);
    }
    for (unsigned bad=0;bad<8;bad++) {
        memset(&s,0,sizeof(s));struct fes_nano_mac_binding b=mac_binding();
        if(bad==0)b.owner=0;
        if(bad==1)b.epoch=0;
        if(bad==2)b.dma_start++;
        if(bad==3)b.dma_size*=2;
        if(bad==4)b.mac=3;
        if(bad==5)b.phy=32;
        if(bad==6)b.phy_id=0;
        if(bad==7)b.phy_id=UINT32_MAX;
        assert(fes_nano_mac_init(&s,b,mac_ops(&h),31));assert(!h.scopes);
    }
    mac_setup(&s,&h);unsigned scopes=h.scopes;
    fes_nano_mac_sip(&s,1,99,0,0,r);mac_empty_reply(r,2);
    fes_nano_mac_sip(&s,1,17,9,0,r);mac_empty_reply(r,2);
    fes_nano_mac_sip(&s,0,17,0,0,r);mac_empty_reply(r,2);
    fes_nano_mac_sip(&s,4,0,0,0,r);mac_empty_reply(r,2);
    fes_nano_mac_sip(&s,1,17,0,1,r);mac_empty_reply(r,1);
    assert(h.scopes==scopes && !h.acquires && !s.failed);
    assert(fes_nano_mac_init(&s,mac_binding(),mac_ops(&h),31));
    for (unsigned cycle=0;cycle<3;cycle++) {
        mac_call(&s,1,r);assert(!r[0] && r[2]==17 && r[3]==cycle*3+1 && s.reservation);
        assert(fes_nano_mac_gate(&s)==1);
        scopes=h.scopes;mac_call(&s,1,r);mac_empty_reply(r,2);assert(h.scopes==scopes);
        mac_call(&s,2,r);assert(!r[0] && r[3]==cycle*3+2 && s.confirmed);
        scopes=h.scopes;mac_call(&s,2,r);mac_empty_reply(r,2);assert(h.scopes==scopes);
        mac_call(&s,3,r);assert(!r[0] && r[3]==cycle*3+3 && !s.reservation);
        unsigned stops=h.stops;
        mac_call(&s,3,r);assert(!r[0] && r[3]==cycle*3+3 && h.stops==stops);
        assert(fes_nano_mac_gate(&s)==0);
        assert(h.bridge_generation==43 && h.usb_generation==5 && h.sd_generation==42);
    }
    for (unsigned op=1;op<=3;op++) {
        for (unsigned kind=0;kind<4;kind++) {
            mac_setup(&s,&h);
            if(op>1)mac_call(&s,1,r);
            if(op>2)mac_call(&s,2,r);
            uint64_t generation=s.generation;
            h.after=0;h.fault=kind==0 ? 1U : kind==1 ? 1U<<op : kind==2 ? 16U : 64U;
            mac_call(&s,op,r);mac_empty_reply(r,4);assert(s.failed && s.generation==generation);
            if(kind==1 || kind==2 || op>1)assert(s.reservation);
            unsigned a=h.acquires,c=h.confirms,d=h.stops,observations=h.scopes;
            mac_call(&s,0,r);mac_empty_reply(r,4);mac_call(&s,3,r);mac_empty_reply(r,4);
            assert(fes_nano_mac_gate(&s)<0 && a==h.acquires && c==h.confirms &&
                d==h.stops && observations==h.scopes);
        }
    }
    mac_setup(&s,&h);mac_call(&s,1,r);mac_call(&s,2,r);h.fault=32;
    mac_call(&s,3,r);mac_empty_reply(r,4);assert(s.reservation && s.active && s.confirmed && h.pending);
    mac_setup(&s,&h);s.generation=UINT64_MAX-2;mac_call(&s,1,r);mac_empty_reply(r,4);
    assert(s.failed && !h.acquires);
    mac_setup(&s,&h);s.generation=UINT64_MAX-3;mac_call(&s,1,r);mac_call(&s,2,r);mac_call(&s,3,r);
    assert(!r[0] && s.generation==UINT64_MAX && !s.active);
    puts("PASS MAC private generation, ownership, drain and failure retention");
}
