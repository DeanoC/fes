/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_configuration.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>

struct fixture {
    struct fes_nano_bridges bridges;
    struct fes_nano_configuration configuration;
    unsigned reset, masters, valid, checks, fail_check, change_check, failures;
    unsigned engine_calls;
};
static int owns(void *p,uint64_t owner,uint64_t epoch)
{ struct fixture *f=p;return f->valid && owner==17 && epoch==9; }
static int reset(void *p,uint32_t *value) { *value=((struct fixture *)p)->reset;return 0; }
static int masters(void *p) { return ((struct fixture *)p)->masters; }
static int stop(void *p) { ((struct fixture *)p)->masters=1;return 0; }
static int enable(void *p,uint32_t mask,unsigned fabric)
{
    struct fixture *f=p;assert(mask==2 && fabric==FES_NANO_FABRIC_ABSENT && f->reset==15);
    f->reset&=~mask;return 0; /* Control-only access keeps producers stopped. */
}
static int disable(void *p,uint32_t mask,unsigned fabric)
{
    struct fixture *f=p;assert(mask==2 && fabric==FES_NANO_FABRIC_ABSENT && f->reset==13);
    f->reset|=mask;return 0;
}
static int check(void *p,uint64_t address,uint32_t bytes,unsigned whole)
{
    struct fixture *f=p;f->checks++;
    if(whole)assert(address==0x94800000ULL && bytes==f->configuration.bytes);
    else {
        uint64_t offset=address-0x94800000ULL;
        unsigned amount=f->configuration.bytes-(unsigned)offset;
        if(amount>64)amount=64;
        assert(offset>=f->configuration.completed && offset<f->configuration.completed+f->configuration.pending &&
               !(offset%64) && bytes==amount);
    }
    if(f->checks==f->change_check)f->valid=0;
    return f->checks==f->fail_check ? -7 : 0;
}
static void trace(void *p,unsigned stage,uint32_t completed,uint32_t pending)
{
    struct fixture *f=p;assert(completed<=f->configuration.bytes && pending<=256);
    if(stage==FES_CONFIG_FAILURE)f->failures++;
}
static void init_bytes(struct fixture *f,unsigned bytes)
{
    memset(f,0,sizeof(*f));f->reset=15;f->masters=f->valid=1;
    struct fes_nano_bridge_ops b={f,owns,reset,masters,stop,disable,enable};
    assert(!fes_nano_bridges_init(&f->bridges,b,17,9,FES_NANO_FABRIC_ABSENT,1));
    struct fes_nano_configuration_ops io={f,check,trace};
    assert(!fes_nano_configuration_init(&f->configuration,&f->bridges,io,0x94800000,bytes,64,1));
}
static void init(struct fixture *f) { init_bytes(f,128); }
static void arm(struct fixture *f)
{
    uint64_t r[4];fes_nano_configuration_prepare(&f->configuration,17,f->bridges.state.generation,0,r);
    assert(r[0]==0 && r[1]==((uint64_t)FES_EMBEDDED_BRIDGE_PREPARATION_TAG<<32|f->configuration.bytes) && r[2]==17 && r[3]==f->bridges.state.generation);
    assert(fes_nano_configuration_busy(&f->configuration));
}
static void reply(struct fixture *f,uint32_t fid,uint64_t address,uint64_t bytes,unsigned configuring,
                    uint64_t a,uint64_t b,uint64_t c,uint64_t d)
{
    assert(!fes_nano_configuration_before(&f->configuration,fid,address,bytes,configuring));
    uint64_t r[]={a,b,c,d};f->engine_calls++;
    fes_nano_configuration_after(&f->configuration,fid,r);
    assert(!f->configuration.failed && r[0]==a && r[1]==b && r[2]==c && r[3]==d);
    assert(f->reset==15 && f->bridges.state.generation==f->configuration.generation &&
           f->bridges.state.never_enabled==(f->configuration.rearmed ? 0U : 1U));
}
static void started(struct fixture *f)
{ arm(f);reply(f,FES_NANO_CONFIG_START,0,0,0,0,0,0,0); }
static void pending(struct fixture *f)
{ started(f);reply(f,FES_NANO_CONFIG_WRITE,0x94800000,64,1,1,0,0,0); }
static void all_bytes(struct fixture *f)
{
    pending(f);
    reply(f,FES_NANO_CONFIG_COMPLETED,0,0,1,1,0,0,0);
    assert(f->configuration.pending==64 && !f->configuration.completed);
    reply(f,FES_NANO_CONFIG_COMPLETED,0,0,1,0,0x14800000,0,0);
    reply(f,FES_NANO_CONFIG_WRITE,0x94800040,64,1,0,0,0,0);
    reply(f,FES_NANO_CONFIG_COMPLETED,0,0,1,0,0x14800040,0,0);
    assert(f->configuration.completed==128 && !f->configuration.pending);
}
static void retired(struct fixture *f)
{
    assert(f->configuration.failed && f->bridges.failed && f->configuration.first_error<0 && f->failures);
    unsigned checks=f->checks,engine=f->engine_calls,observed_reset=f->reset;uint64_t r[4];
    assert(fes_nano_configuration_before(&f->configuration,FES_NANO_CONFIG_START,0,0,0)==4);
    fes_nano_configuration_prepare(&f->configuration,17,f->bridges.state.generation,0,r);
    assert(r[0]!=0 && !r[2] && !r[3] && f->checks==checks && f->engine_calls==engine);
    assert(f->reset==observed_reset); /* Never invent reclose after an external change. */
}
static void closed_after_configuration(struct fixture *f)
{
    init(f);all_bytes(f);reply(f,FES_NANO_CONFIG_ISDONE,0,0,1,0,0,0,0);
    struct fes_nano_bridge_snapshot open;
    struct fes_nano_bridge_closure close;
    assert(!fes_nano_bridges_open(&f->bridges,17,f->bridges.state.generation,2,&open));
    assert(!fes_nano_bridges_close(&f->bridges,17,open.generation,0,1,&close));
    assert(close.producers_quiescent && close.data_remained_contained && close.after.reset==15 &&
           !close.after.never_enabled && close.after.generation==3);
}
static unsigned reconfiguration_cases(void)
{
    struct fixture f;uint64_t r[4];unsigned cases=0;
    closed_after_configuration(&f);
    for(unsigned cycle=0;cycle<3;cycle++) {
        uint64_t generation=f.bridges.state.generation;
        fes_nano_configuration_rearm(&f.configuration,17,generation,0,r);
        assert(!r[0] && r[1]==((uint64_t)FES_EMBEDDED_BRIDGE_REARM_TAG<<32|128) &&
               r[2]==17 && r[3]==generation && f.configuration.rearmed && !f.configuration.finished);
        unsigned checks=f.checks,engine=f.engine_calls;
        fes_nano_configuration_rearm(&f.configuration,17,generation,0,r);
        assert(r[0]==2 && f.checks==checks && f.engine_calls==engine); /* no arm replay */
        reply(&f,FES_NANO_CONFIG_START,0,0,0,0,0,0,0);
        reply(&f,FES_NANO_CONFIG_WRITE,0x94800000,64,1,0,0,0,0);
        reply(&f,FES_NANO_CONFIG_COMPLETED,0,0,1,0,0x14800000,0,0);
        reply(&f,FES_NANO_CONFIG_WRITE,0x94800040,64,1,0,0,0,0);
        reply(&f,FES_NANO_CONFIG_COMPLETED,0,0,1,0,0x14800040,0,0);
        reply(&f,FES_NANO_CONFIG_ISDONE,0,0,1,0,0,0,0);
        assert(f.configuration.finished && !fes_nano_configuration_busy(&f.configuration));
        fes_nano_configuration_rearm(&f.configuration,17,generation,0,r);
        assert(r[0]==2); /* completion alone is not a new close */
        struct fes_nano_bridge_snapshot open;struct fes_nano_bridge_closure close;
        assert(!fes_nano_bridges_open(&f.bridges,17,generation,2,&open));
        assert(!fes_nano_bridges_close(&f.bridges,17,open.generation,0,1,&close));cases++;
    }
    for(unsigned mode=0;mode<22;mode++) {
        closed_after_configuration(&f);
        uint64_t owner=17,generation=f.bridges.state.generation;
        if(mode==0)owner++;
        if(mode==1)generation=f.configuration.generation;
        if(mode==2)generation++;
        if(mode==4)f.bridges.device_lease=1;
        if(mode==5)f.configuration.finished=0;
        if(mode==6)f.configuration.started=0;
        if(mode==7)f.configuration.prepared=0;
        if(mode==8)f.configuration.attempted=0;
        if(mode==9)f.configuration.pending=64;
        if(mode==10)f.configuration.issued=FES_NANO_CONFIG_WRITE;
        if(mode==11)f.configuration.completed=64;
        if(mode==12)f.configuration.bytes=0;
        if(mode==13)f.configuration.failed=1;
        if(mode==14)f.valid=0;
        if(mode==15)f.reset=13;
        if(mode==16)f.masters=0;
        if(mode==17)f.bridges.state.never_enabled=1;
        if(mode==18)f.bridges.state.generation++;
        if(mode==19)f.configuration.generation=generation;
        if(mode==20)generation=UINT64_MAX-1;
        if(mode==21)f.configuration.chunk=0;
        struct fes_nano_configuration before;memcpy(&before,&f.configuration,sizeof(before));
        unsigned checks=f.checks,engine=f.engine_calls;
        fes_nano_configuration_rearm(&f.configuration,owner,generation,mode==3,r);
        unsigned expected=(mode==3 || mode==4) ? 1U :
                          ((mode>=13 && mode<=16) || mode==21) ? 4U : 2U;
        assert(r[0]==expected && !r[2] && !r[3] && f.engine_calls==engine);
        if(expected!=4)assert(!memcmp(&before,&f.configuration,sizeof(before)) && f.checks==checks);
        else assert(f.configuration.failed);
        cases++;
    }
    for(unsigned mode=0;mode<2;mode++) {
        closed_after_configuration(&f);
        if(mode)f.change_check=f.checks+1;else f.fail_check=f.checks+1;
        fes_nano_configuration_rearm(&f.configuration,17,f.bridges.state.generation,0,r);
        assert(r[0]==4 && !r[2] && !r[3]);retired(&f);cases++;
    }
    return cases;
}
int main(void)
{
    unsigned cases=reconfiguration_cases();struct fixture f;uint64_t r[4];
    init(&f);pending(&f);
    unsigned checks=f.checks,engine=f.engine_calls;
    assert(!fes_nano_configuration_retire(&f.configuration,17,1,1));
    assert(f.configuration.pending==64 && !f.configuration.completed &&
           f.checks==checks && f.engine_calls==engine && !fes_nano_configuration_busy(&f.configuration));
    assert(fes_nano_configuration_retire(&f.configuration,17,1,1)<0);retired(&f);cases++;
    for(unsigned mode=0;mode<12;mode++) {
        init(&f);pending(&f);
        if(mode==2)f.configuration.prepared=0;
        if(mode==3)f.configuration.started=0;
        if(mode==4)f.configuration.finished=1;
        if(mode==5)f.configuration.failed=1;
        if(mode==6)f.configuration.issued=FES_NANO_CONFIG_WRITE;
        if(mode==7)f.bridges.device_lease=1;
        if(mode==8)f.configuration.pending=0;
        if(mode==9)f.reset=13;
        if(mode==10)f.masters=0;
        checks=f.checks;engine=f.engine_calls;
        assert(fes_nano_configuration_retire(&f.configuration,mode==0?18:17,mode==1?2:1,mode!=11)<0);
        assert(f.checks==checks && f.engine_calls==engine);cases++;
    }
    init_bytes(&f,260);started(&f);
    for(unsigned i=0;i<4;i++)reply(&f,FES_NANO_CONFIG_WRITE,0x94800000+i*64,64,1,i==3,0,0,0);
    assert(f.configuration.pending==256 && !f.configuration.completed);
    reply(&f,FES_NANO_CONFIG_COMPLETED,0,0,1,1,0,0,0);
    reply(&f,FES_NANO_CONFIG_COMPLETED,0,0,1,0,0x14800000,0x14800040,0x14800080);
    assert(f.configuration.completed==192 && f.configuration.pending==64);
    reply(&f,FES_NANO_CONFIG_WRITE,0x94800100,4,1,0,0,0,0);
    reply(&f,FES_NANO_CONFIG_COMPLETED,0,0,1,0,0x148000c0,0x14800100,0);
    assert(f.configuration.completed==260 && !f.configuration.pending);
    reply(&f,FES_NANO_CONFIG_ISDONE,0,0,1,0,0,0,0);cases++;
    init_bytes(&f,260);started(&f);
    for(unsigned i=0;i<4;i++)reply(&f,FES_NANO_CONFIG_WRITE,0x94800000+i*64,64,1,i==3,0,0,0);
    assert(fes_nano_configuration_before(&f.configuration,FES_NANO_CONFIG_WRITE,0x94800100,4,1)==4);
    assert(f.configuration.pending==256 && !f.configuration.completed);retired(&f);cases++;
    for(unsigned mode=0;mode<10;mode++) {
        init_bytes(&f,260);started(&f);
        for(unsigned i=0;i<4;i++)reply(&f,FES_NANO_CONFIG_WRITE,0x94800000+i*64,64,1,i==3,0,0,0);
        assert(!fes_nano_configuration_before(&f.configuration,FES_NANO_CONFIG_COMPLETED,0,0,1));
        uint64_t value[]={0,0x14800000,0x14800040,0x14800080};
        if(mode==0)value[2]=value[1];
        if(mode==1)value[1]+=64;
        if(mode==2)value[2]=0;
        if(mode==3)value[3]=0x14800100;
        if(mode==4)value[0]=1;
        if(mode==5)value[0]=4;
        if(mode>=6)f.fail_check=f.checks+1+(mode-6);
        fes_nano_configuration_after(&f.configuration,FES_NANO_CONFIG_COMPLETED,value);
        assert(value[0]==4 && !value[1] && !value[2] && !value[3]);
        assert(f.configuration.pending==256 && !f.configuration.completed);retired(&f);cases++;
    }
    init(&f);all_bytes(&f);
    reply(&f,FES_NANO_CONFIG_ISDONE,0,0,1,1,0,0,0);
    reply(&f,FES_NANO_CONFIG_ISDONE,0,0,1,0,0,0,0);
    assert(f.configuration.finished && !fes_nano_configuration_busy(&f.configuration));cases++;
    assert(fes_nano_configuration_before(&f.configuration,FES_NANO_CONFIG_START,0,0,0)==4);retired(&f);cases++;
    fes_nano_configuration_prepare(0,17,1,0,r);assert(r[0]==5 && !r[2] && !r[3]);cases++;
    for(unsigned mode=0;mode<7;mode++) {
        init(&f);
        if(mode==2)f.bridges.device_lease=1;
        if(mode==4)f.fail_check=1;
        if(mode==5)f.change_check=1;
        if(mode==6)f.reset=13;
        fes_nano_configuration_prepare(&f.configuration,mode==0?19:17,mode==1?2:1,mode==3,r);
        assert(r[0]==(mode<2?2:mode<4?1:4) && !r[2] && !r[3]);
        if(mode>=4)retired(&f);
        else assert(!f.configuration.attempted && !f.configuration.failed && !f.checks);
        cases++;
    }
    init(&f);arm(&f);checks=f.checks;
    fes_nano_configuration_prepare(&f.configuration,17,1,0,r);
    assert(r[0]==2 && !r[2] && !r[3] && f.checks==checks);cases++;
    init(&f);assert(fes_nano_configuration_before(&f.configuration,FES_NANO_CONFIG_START,0,0,0)==2 && !f.configuration.failed);cases++;
    for(unsigned mode=0;mode<8;mode++) {
        init(&f);started(&f);
        uint64_t address=0x94800000,bytes=64;
        if(mode==0)address+=64;
        if(mode==1)bytes=128;
        if(mode==2)bytes=0;
        if(mode==3)f.fail_check=f.checks+1;
        if(mode==4)f.change_check=f.checks+1;
        if(mode==5)f.bridges.state.generation++;
        if(mode==6)f.masters=0;
        if(mode==7)f.bridges.device_lease=1;
        assert(fes_nano_configuration_before(&f.configuration,FES_NANO_CONFIG_WRITE,address,bytes,1)==4);
        retired(&f);cases++;
    }
    for(unsigned mode=0;mode<7;mode++) {
        init(&f);pending(&f);
        assert(!fes_nano_configuration_before(&f.configuration,FES_NANO_CONFIG_COMPLETED,0,0,1));
        uint64_t value[4]={0,0x14800000,0,0};
        if(mode==0)value[1]=0;
        if(mode==1)value[1]=0x94800000;
        if(mode==2)value[2]=value[1];
        if(mode==3)value[0]=1;
        if(mode==4)value[0]=4;
        if(mode==5)f.fail_check=f.checks+1;
        if(mode==6)f.change_check=f.checks+1;
        fes_nano_configuration_after(&f.configuration,FES_NANO_CONFIG_COMPLETED,value);
        assert(value[0]==4 && !value[1] && !value[2] && !value[3] && f.configuration.pending==64 && !f.configuration.completed);
        retired(&f);cases++;
    }
    for(unsigned mode=0;mode<5;mode++) {
        init(&f);all_bytes(&f);
        if(mode==0)f.fail_check=f.checks+1;
        if(mode==0)assert(fes_nano_configuration_before(&f.configuration,FES_NANO_CONFIG_ISDONE,0,0,1)==4);
        else {
            assert(!fes_nano_configuration_before(&f.configuration,FES_NANO_CONFIG_ISDONE,0,0,1));
            uint64_t value[4]={0};
            if(mode==1)value[0]=4;
            if(mode==2)value[1]=1;
            if(mode==3)f.fail_check=f.checks+1;
            if(mode==4)f.change_check=f.checks+1;
            fes_nano_configuration_after(&f.configuration,FES_NANO_CONFIG_ISDONE,value);
            assert(value[0]==4 && !value[1] && !value[2] && !value[3]);
        }
        assert(!f.configuration.finished);retired(&f);cases++;
    }
    for(unsigned mode=0;mode<8;mode++) {
        init(&f);arm(&f);
        uint32_t fid=FES_NANO_CONFIG_START;uint64_t address=0,bytes=0;unsigned configuring=0;
        if(mode==0)address=1;
        if(mode==1)bytes=1;
        if(mode==2)configuring=1;
        if(mode==3)fid=FES_NANO_CONFIG_WRITE;
        if(mode==4)fid=FES_NANO_CONFIG_COMPLETED;
        if(mode==5)fid=FES_NANO_CONFIG_ISDONE;
        if(mode==6)f.fail_check=f.checks+1;
        if(mode==7)f.valid=0;
        assert(fes_nano_configuration_before(&f.configuration,fid,address,bytes,configuring)==4);
        retired(&f);cases++;
    }
    for(unsigned mode=0;mode<7;mode++) {
        init(&f);memset(&f.configuration,0,sizeof(f.configuration));
        struct fes_nano_configuration_ops io={&f,check,trace};
        uint64_t base=0x94800000;uint32_t bytes=128,chunk=64;unsigned qualified=1;
        if(mode==0)base++;
        if(mode==1)base=UINT64_MAX-63;
        if(mode==2)bytes=0;
        if(mode==3)bytes=130;
        if(mode==4)chunk=63;
        if(mode==5)qualified=0;
        if(mode==6)io.check=0;
        assert(fes_nano_configuration_init(&f.configuration,&f.bridges,io,base,bytes,chunk,qualified)<0);
        retired(&f);cases++;
    }
    /* Device containment may advance the secure generation before first load.
     * Old requests cannot bind; the accepted generation is then immutable. */
    for(unsigned mode=0;mode<4;mode++) {
        init(&f);f.bridges.state.generation=4;
        fes_nano_configuration_prepare(&f.configuration,17,1,0,r);
        assert(r[0]==2 && !f.configuration.attempted && f.configuration.generation==1);
        if(mode==0) {
            all_bytes(&f);reply(&f,FES_NANO_CONFIG_ISDONE,0,0,1,0,0,0,0);
            assert(f.configuration.finished && f.configuration.generation==4);
        } else {
            pending(&f);assert(f.configuration.generation==4);
            if(mode==1) {
                assert(fes_nano_configuration_retire(&f.configuration,17,1,1));
                assert(!fes_nano_configuration_retire(&f.configuration,17,4,1));
                assert(f.configuration.pending==64 && f.configuration.failed && f.reset==15);
            } else {
                if(mode==2)f.bridges.state.generation++;
                if(mode==3)f.bridges.device_lease=1;
                if(mode==2)assert(fes_nano_configuration_before(&f.configuration,FES_NANO_CONFIG_COMPLETED,0,0,1)==4);
                else assert(fes_nano_configuration_retire(&f.configuration,17,4,1));
                assert(f.configuration.pending==64 && !f.configuration.completed && f.reset==15);
            }
        }
        cases++;
    }
    printf("PASS %u contained configuration ownership and failure cases\n",cases);
    return 0;
}
