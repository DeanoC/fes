/* SPDX-License-Identifier: BSD-3-Clause */
#define main fes_cold_sd_test_main
#include "nano_sd_test.c"
#undef main

static unsigned window_masters;
static int window_contained(void *p) { (void)p; return window_masters; }
static int window_stop(void *p)
{ struct hardware *h=p; assert(!h->active && h->reset==5); window_masters=1; return 0; }
static int window_enable(void *p,uint32_t mask,unsigned fabric)
{
    struct hardware *h=p;
    assert(!h->active && fabric==FES_NANO_FABRIC_ABSENT &&
           ((mask==2 && h->reset==15) || (mask==8 && h->reset==13)));
    h->reset &= ~mask; if(mask==8)window_masters=0; return 0;
}
static int window_disable(void *p,uint32_t mask,unsigned fabric)
{
    struct hardware *h=p;
    assert(!h->active && h->reset==5 && mask==10 && fabric==FES_NANO_FABRIC_ABSENT);
    h->reset |= mask; return 0;
}
static int window_return(void *p,const struct fes_nano_bridges *b,uint64_t *generation)
{ (void)p; return fes_nano_bridges_window_return(b,generation); }
static int wrong_generation(void *p,const struct fes_nano_bridges *b,uint64_t *generation)
{ (void)p; *generation=b->closure.after.generation-1; return 1; }
static void returned_window(struct test *t,unsigned opt_in)
{
    uint64_t r[4]; struct fes_nano_bridge_snapshot opened; struct fes_nano_bridge_closure closed;
    setup(t); assert(!initialize(t,FES_NANO_SD_QUALIFIED));
    call(t,1,r);reply(r,0,0x33,2);call(t,2,r);reply(r,0,0x37,3);call(t,3,r);reply(r,0,0x39,4);
    window_masters=1;t->bridge.ops.masters_contained=window_contained;
    t->bridge.ops.stop_producers=window_stop;
    t->bridge.ops.enable=window_enable;t->bridge.ops.disable=window_disable;
    assert(!fes_nano_bridges_open(&t->bridge,17,4,2,&opened));
    assert(!fes_nano_bridges_open(&t->bridge,17,5,8,&opened));
    assert(!fes_nano_bridges_close(&t->bridge,17,6,0,0,&closed));
    assert(closed.after.generation==7 && closed.hardware_drained==8 && !closed.data_remained_contained);
    t->h.prepares=t->h.stops=0;
    if(opt_in)t->sd.ops.returned=window_return;
}
int main(void)
{
    struct test t;uint64_t r[4],generation=0;unsigned cases=0;
    returned_window(&t,0);
    assert(!fes_nano_bridges_held_return(&t.bridge,&generation) && !generation);
    assert(fes_nano_bridges_window_return(&t.bridge,&generation) && generation==7);
    unsigned resumes=t.h.resumes;call(&t,1,r);reply(r,4,0,0);
    assert(t.h.resumes==resumes && t.sd.failed);cases++;
    returned_window(&t,1);
    for(unsigned cycle=0;cycle<3;cycle++) {
        uint64_t base=t.bridge.state.generation;
        call(&t,0,r);reply(r,0,0x39,base);
        call(&t,1,r);reply(r,0,0x33,base+1);
        call(&t,2,r);reply(r,0,0x37,base+2);
        call(&t,3,r);reply(r,0,0x39,base+3);
        assert(!fes_nano_sd_returned(&t.sd,17,7,base+3));cases++;
    }
    for(unsigned fault=0;fault<17;fault++) {
        returned_window(&t,1);
        switch(fault) {
        case 0:t.bridge.closure_valid=0;break;
        case 1:t.bridge.closure.after.owner++;break;
        case 2:t.bridge.closure.after.generation++;break;
        case 3:t.bridge.closure.after.reset=13;break;
        case 4:t.bridge.closure.after.never_enabled=1;break;
        case 5:t.bridge.closure.producers_quiescent=0;break;
        case 6:t.bridge.closure.data_remained_contained=1;break;
        case 7:t.bridge.closure.fabric_acknowledged=1;break;
        case 8:t.bridge.closure.hardware_drained=0;break;
        case 9:t.bridge.closure.hardware_drained=12;break;
        case 10:t.bridge.failed=1;break;
        case 11:t.bridge.state.generation++;break;
        case 12:t.bridge.device_lease=1;break;
        case 13:t.sd.ops.returned=wrong_generation;break;
        case 14:t.bridge.closure.after.masters_contained=0;break;
        case 15:t.bridge.closure.after.fabric=FES_NANO_FABRIC_PRESENT;break;
        case 16:t.h.fault=1;break; /* Actual live scope still rejects cached proof. */
        }
        unsigned before=t.h.resumes;
        call(&t,1,r);reply(r,4,0,0);
        assert(t.sd.failed && t.bridge.failed && t.h.resumes==before);
        call(&t,1,r);reply(r,4,0,0);assert(t.h.resumes==before);cases++;
    }
    for(unsigned fault=2;fault<=7;fault++) {
        if(fault==3)continue;
        returned_window(&t,1);t.h.fault=fault;
        call(&t,1,r);
        if(fault==2){reply(r,4,0,0);assert(t.sd.active && t.bridge.device_lease);}
        else if(fault==4 || fault==5){reply(r,0,0x33,8);call(&t,2,r);reply(r,4,0,0);}
        else{reply(r,0,0x33,8);call(&t,2,r);reply(r,0,0x37,9);call(&t,3,r);reply(r,4,0,0);}
        assert(t.sd.failed && t.bridge.failed && t.bridge.device_lease);
        resumes=t.h.resumes;call(&t,1,r);reply(r,4,0,0);assert(t.h.resumes==resumes);cases++;
    }
    printf("PASS %u explicit window SD-return provenance and failure cases; no hardware\n",cases);
    return 0;
}
