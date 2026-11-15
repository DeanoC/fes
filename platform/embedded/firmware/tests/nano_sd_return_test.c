/* SPDX-License-Identifier: BSD-3-Clause */
/* Reuse the actual cold-SD fixtures and run the real bridge/SD services. */
#define main fes_cold_sd_test_main
#include "nano_sd_test.c"
#undef main

static int warm_stop(void *p)
{
    struct hardware *h=p;
    assert(!h->active && h->reset==FES_NANO_BRIDGES_DATA);
    return 0;
}
static int warm_enable(void *p,uint32_t mask,unsigned fabric)
{
    struct hardware *h=p;
    assert(!h->active && mask==FES_NANO_BRIDGES_CONTROL && fabric==FES_NANO_FABRIC_ABSENT);
    h->reset &= ~mask;
    return 0;
}
static int warm_disable(void *p,uint32_t mask,unsigned fabric)
{
    struct hardware *h=p;
    assert(!h->active && mask==FES_NANO_BRIDGES_CONTROL && fabric==FES_NANO_FABRIC_ABSENT);
    h->reset |= mask;
    return 0;
}
static void returned(struct test *t)
{
    uint64_t r[4];struct fes_nano_bridge_snapshot opened;
    struct fes_nano_bridge_closure closed;
    setup(t);assert(!initialize(t,FES_NANO_SD_QUALIFIED));
    call(t,1,r);reply(r,0,0x33,2);call(t,2,r);reply(r,0,0x37,3);call(t,3,r);reply(r,0,0x39,4);
    t->bridge.ops.stop_producers=warm_stop;
    t->bridge.ops.enable=warm_enable;t->bridge.ops.disable=warm_disable;
    assert(!fes_nano_bridges_open(&t->bridge,17,4,FES_NANO_BRIDGES_CONTROL,&opened));
    assert(!t->bridge.closure_valid && opened.generation==5);
    assert(!fes_nano_bridges_close(&t->bridge,17,5,0,1,&closed));
    assert(t->bridge.closure_valid && closed.after.generation==6);
}
int main(void)
{
    assert(!fes_cold_sd_test_main());
    struct test t;uint64_t r[4],generation=0;
    returned(&t);
    assert(fes_nano_bridges_held_return(&t.bridge,&generation)==1 && generation==6);
    for(unsigned cycle=0;cycle<3;cycle++) {
        uint64_t base=t.bridge.state.generation;
        call(&t,0,r);reply(r,0,0x39,base);
        call(&t,1,r);reply(r,0,0x33,base+1);
        call(&t,2,r);reply(r,0,0x37,base+2);
        call(&t,3,r);reply(r,0,0x39,base+3);
        assert(!fes_nano_sd_returned(&t.sd,17,6,base+3));
        assert(fes_nano_sd_returned(&t.sd,18,6,base+3));
        assert(fes_nano_sd_returned(&t.sd,17,4,base+3));
        assert(fes_nano_sd_returned(&t.sd,17,6,base+2));
    }
    for(unsigned fault=0;fault<12;fault++) {
        returned(&t);
        switch(fault) {
        case 0:t.bridge.closure_valid=0;break;
        case 1:t.bridge.closure.after.owner++;break;
        case 2:t.bridge.closure.after.generation++;break;
        case 3:t.bridge.closure.after.reset=13;break;
        case 4:t.bridge.closure.after.never_enabled=1;break;
        case 5:t.bridge.closure.producers_quiescent=0;break;
        case 6:t.bridge.closure.data_remained_contained=0;break;
        case 7:t.bridge.closure.fabric_acknowledged=1;break;
        case 8:t.bridge.closure.hardware_drained=4;break;
        case 9:t.bridge.failed=1;break;
        case 10:t.bridge.state.generation++;break;
        case 11:t.bridge.device_lease=1;break;
        }
        struct hardware before=t.h;
        call(&t,0,r);reply(r,4,0,0);
        assert(t.sd.failed && t.bridge.failed && !memcmp(&before,&t.h,sizeof(before)));
        for(unsigned op=0;op<4;op++){call(&t,op,r);reply(r,4,0,0);}
        assert(!memcmp(&before,&t.h,sizeof(before)));
    }
    returned(&t);
    struct fes_nano_bridge_snapshot opened;
    assert(!fes_nano_bridges_open(&t.bridge,17,6,2,&opened));
    assert(!t.bridge.closure_valid);
    unsigned releases=t.h.resumes;
    call(&t,1,r);reply(r,4,0,0);assert(t.h.resumes==releases);
    returned(&t);call(&t,1,r);reply(r,0,0x33,7);
    t.bridge.state.generation=10;
    struct hardware before=t.h;
    call(&t,0,r);reply(r,4,0,0);assert(t.sd.active && t.bridge.device_lease);
    assert(!memcmp(&before,&t.h,sizeof(before)));
    puts("PASS verified held-data return, three SD cycles and stale/forged/uncertain return rejection");
    return 0;
}
