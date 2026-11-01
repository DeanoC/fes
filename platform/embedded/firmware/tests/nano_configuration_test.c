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
static int stop(void *p) { (void)p;assert(0);return -1; }
static int driver(void *p,uint32_t mask,unsigned fabric)
{ (void)p;(void)mask;(void)fabric;assert(0);return -1; }
static int check(void *p,uint64_t address,uint32_t bytes,unsigned whole)
{
    struct fixture *f=p;f->checks++;
    if(whole)assert(address==0x94800000ULL && bytes==128);
    else assert(address==0x94800000ULL+f->configuration.completed && bytes==64);
    if(f->checks==f->change_check)f->valid=0;
    return f->checks==f->fail_check ? -7 : 0;
}
static void trace(void *p,unsigned stage,uint32_t completed,uint32_t pending)
{
    struct fixture *f=p;assert(completed<=128 && pending<=64);
    if(stage==FES_CONFIG_FAILURE)f->failures++;
}
static void init(struct fixture *f)
{
    memset(f,0,sizeof(*f));f->reset=15;f->masters=f->valid=1;
    struct fes_nano_bridge_ops b={f,owns,reset,masters,stop,driver,driver};
    assert(!fes_nano_bridges_init(&f->bridges,b,17,9,FES_NANO_FABRIC_ABSENT,1));
    struct fes_nano_configuration_ops io={f,check,trace};
    assert(!fes_nano_configuration_init(&f->configuration,&f->bridges,io,0x94800000,128,64,1));
}
static void arm(struct fixture *f)
{
    uint64_t r[4];fes_nano_configuration_prepare(&f->configuration,17,1,0,r);
    assert(r[0]==0 && r[1]==((uint64_t)FES_EMBEDDED_BRIDGE_PREPARATION_TAG<<32|128) && r[2]==17 && r[3]==1);
    assert(fes_nano_configuration_busy(&f->configuration));
}
static void reply(struct fixture *f,uint32_t fid,uint64_t address,uint64_t bytes,unsigned configuring,
                    uint64_t a,uint64_t b,uint64_t c,uint64_t d)
{
    assert(!fes_nano_configuration_before(&f->configuration,fid,address,bytes,configuring));
    uint64_t r[]={a,b,c,d};f->engine_calls++;
    fes_nano_configuration_after(&f->configuration,fid,r);
    assert(!f->configuration.failed && r[0]==a && r[1]==b && r[2]==c && r[3]==d);
    assert(f->reset==15 && f->bridges.state.generation==1 && f->bridges.state.never_enabled);
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
    fes_nano_configuration_prepare(&f->configuration,17,1,0,r);
    assert(r[0]!=0 && !r[2] && !r[3] && f->checks==checks && f->engine_calls==engine);
    assert(f->reset==observed_reset); /* Never invent reclose after an external change. */
}
int main(void)
{
    unsigned cases=0;struct fixture f;uint64_t r[4];
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
    init(&f);arm(&f);unsigned checks=f.checks;
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
    printf("PASS %u contained configuration ownership and failure cases\n",cases);
    return 0;
}
