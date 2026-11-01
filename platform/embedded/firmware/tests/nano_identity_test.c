/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_identity.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>

struct hw {
    uint32_t reset;
    unsigned owner,reads,enables,disables,contains,stops;
    unsigned fail_enable,fail_disable,fail_read,change_read,lose_data,lose_owner,busy_word;
    unsigned config_calls,change_config;
    int configured;
};
static const uint32_t build[4]={0x44556677,0x00112233,0xbbccddee,0x778899aa};
static int owns(void *p,uint64_t owner,uint64_t epoch)
{ struct hw *h=p;return h->owner && owner==17 && epoch==7; }
static int reset(void *p,uint32_t *v) { *v=((struct hw *)p)->reset;return 0; }
static int masters(void *p) { return (((struct hw *)p)->reset & FES_NANO_BRIDGES_DATA)==FES_NANO_BRIDGES_DATA; }
static int stop(void *p) { struct hw *h=p;h->stops++;return masters(p)==1?0:-1; }
static int enable(void *p,uint32_t mask,unsigned fabric)
{
    struct hw *h=p;assert(mask==FES_NANO_BRIDGES_CONTROL && fabric==FES_NANO_FABRIC_ABSENT);
    h->enables++;
    if (h->fail_enable==1) return -5;
    h->reset&=~mask;return h->fail_enable==2?-5:0;
}
static int disable(void *p,uint32_t mask,unsigned fabric)
{
    struct hw *h=p;assert(mask==FES_NANO_BRIDGES_CONTROL && fabric==FES_NANO_FABRIC_ABSENT);
    h->disables++;if (h->fail_disable) return -5;h->reset|=mask;return 0;
}
static int contain(void *p)
{
    struct hw *h=p;h->contains++;h->reset|=FES_NANO_BRIDGES_CONTROL;h->owner=0;
    return h->reset==FES_NANO_BRIDGES_ALL?0:-5;
}
static int configured(void *p)
{
    struct hw *h=p;h->config_calls++;assert(h->reset==FES_NANO_BRIDGES_ALL && !h->enables && !h->reads);
    if(h->change_config)h->owner=0;
    return h->configured;
}
static int read32(void *p,uint32_t offset,uint32_t *value)
{
    struct hw *h=p;assert(h->reset==FES_NANO_BRIDGES_DATA);
    h->reads++;
    if (h->fail_read==h->reads) return -5;
    switch(offset) {
    case FES_EMBEDDED_REG_MAGIC:*value=FES_EMBEDDED_MAGIC;break;
    case FES_EMBEDDED_REG_VERSION:*value=FES_EMBEDDED_VERSION;break;
    case FES_EMBEDDED_REG_ABI:*value=0x10203040;break;
    case FES_EMBEDDED_REG_BUILD0:case FES_EMBEDDED_REG_BUILD1:
    case FES_EMBEDDED_REG_BUILD2:case FES_EMBEDDED_REG_BUILD3:
        *value=build[(offset-FES_EMBEDDED_REG_BUILD0)/4];break;
    case FES_EMBEDDED_REG_GENERATION_LO:case FES_EMBEDDED_REG_GENERATION_HI:
    case FES_EMBEDDED_REG_ACK_GENERATION_LO:case FES_EMBEDDED_REG_ACK_GENERATION_HI:
    case FES_EMBEDDED_REG_ACK_COOKIE_LO:case FES_EMBEDDED_REG_ACK_COOKIE_HI:
    case FES_EMBEDDED_REG_STATUS:case FES_EMBEDDED_REG_PENDING:*value=h->busy_word;break;
    default:assert(0);return -5;
    }
    if (h->change_read==h->reads)*value^=1;
    if (h->lose_data && h->reads==15)h->reset&=~4U;
    if (h->lose_owner && h->reads==15)h->owner=0;
    return 0;
}
static void setup(struct hw *h,struct fes_nano_bridges *b,struct fes_nano_identity_probe *p)
{
    *h=(struct hw){.reset=15,.owner=1,.configured=1};*b=(struct fes_nano_bridges){0};*p=(struct fes_nano_identity_probe){0};
    struct fes_nano_bridge_ops ops={h,owns,reset,masters,stop,disable,enable};
    assert(!fes_nano_bridges_init(b,ops,17,7,FES_NANO_FABRIC_ABSENT,1));
    struct fes_nano_identity_ops io={.context=h,.configured=configured,.read32=read32,.contain=contain};
    assert(!fes_nano_identity_init(p,b,io,0x10203040,build,1));
}
int main(void)
{
    struct hw h;struct fes_nano_bridges b;struct fes_nano_identity_probe p;uint64_t r[4];unsigned cases=0;
    assert(!fes_nano_platform_identity_probe() && !fes_nano_platform_identity_setup());
    assert(fes_nano_platform_control_target(1)<0);
    fes_nano_identity_sip(0,17,1,0,r);assert(r[0]==5 && !r[2] && !r[3]);cases++;
    setup(&h,&b,&p);
    fes_nano_identity_sip(&p,18,1,0,r);assert(r[0]==2 && !p.attempted && !h.enables);
    fes_nano_identity_sip(&p,17,2,0,r);assert(r[0]==2 && !p.attempted && !h.enables);
    fes_nano_identity_sip(&p,17,1,1,r);assert(r[0]==1 && !p.attempted && !h.enables);
    b.device_lease=1;fes_nano_identity_sip(&p,17,1,0,r);assert(r[0]==1 && !p.attempted && !h.enables);b.device_lease=0;
    fes_nano_identity_sip(&p,17,1,0,r);
    assert(!r[0] && r[1]==((uint64_t)FES_EMBEDDED_BRIDGE_IDENTITY_TAG<<32|0x10203040U));
    assert(r[2]==0x778899aabbccddeeULL && r[3]==0x0011223344556677ULL);
    assert(h.enables==1 && h.disables==1 && h.stops==1 && !h.contains && h.reads==30 && h.reset==15);
    assert(!p.active && p.stage==FES_ID_COMPLETE && !p.failed && !p.first_error);
    assert(b.state.generation==3 && b.state.masters_contained && !b.state.never_enabled);
    fes_nano_identity_sip(&p,17,3,0,r);assert(r[0]==2 && !r[2] && !r[3] && h.enables==1);cases++;
    setup(&h,&b,&p);h.configured=0;
    fes_nano_identity_sip(&p,17,1,0,r);
    assert(r[0]==5 && !r[2] && !r[3] && !p.failed && p.attempted && !p.active);
    assert(h.config_calls==1 && !h.reads && !h.enables && !h.disables && !h.contains && !b.failed);
    assert(b.state.generation==1 && b.state.never_enabled && h.reset==15);
    h.configured=1;fes_nano_identity_sip(&p,17,1,0,r);assert(r[0]==2 && h.config_calls==1);cases++;
    for(unsigned fault=0;fault<4;fault++) {
        setup(&h,&b,&p);
        if(fault==0)h.configured=-7;
        if(fault==1)h.configured=2;
        if(fault==2)h.change_config=1;
        if(fault==3){h.configured=0;h.change_config=1;}
        fes_nano_identity_sip(&p,17,1,0,r);
        assert(r[0]==4 && !r[2] && !r[3] && p.failed && b.failed && !p.active);
        assert(!h.reads && !h.enables && !h.disables && h.contains==1 && p.stage==FES_ID_CONFIG);
        if(fault==0)assert(p.first_error==-7);
        cases++;
    }
    setup(&h,&b,&p);
    struct fes_nano_identity_ops incomplete=p.io;incomplete.configured=0;
    p=(struct fes_nano_identity_probe){0};
    assert(fes_nano_identity_init(&p,&b,incomplete,0x10203040,build,1)<0 && p.failed);
    assert(!h.config_calls && !h.reads && !h.enables && !h.disables && !h.contains);cases++;
    fes_nano_identity_configuration_sip(0,17,1,0,r);
    assert(r[0]==5 && r[1]==(uint64_t)FES_EMBEDDED_BRIDGE_CONFIGURATION_TAG<<32 && !r[2] && !r[3]);cases++;
    for(int ready=0;ready<=1;ready++) {
        setup(&h,&b,&p);h.configured=ready;
        fes_nano_identity_configuration_sip(&p,18,1,0,r);assert(r[0]==2 && !p.queried);
        fes_nano_identity_configuration_sip(&p,17,2,0,r);assert(r[0]==2 && !p.queried);
        fes_nano_identity_configuration_sip(&p,17,1,1,r);assert(r[0]==1 && !p.queried);
        b.device_lease=1;fes_nano_identity_configuration_sip(&p,17,1,0,r);
        assert(r[0]==1 && !p.queried && !h.config_calls);b.device_lease=0;
        fes_nano_identity_configuration_sip(&p,17,1,0,r);
        assert(!r[0] && r[1]==((uint64_t)FES_EMBEDDED_BRIDGE_CONFIGURATION_TAG<<32|(unsigned)ready) && r[2]==17 && r[3]==1);
        assert(p.queried && !p.attempted && !p.active && !p.failed && !b.failed && b.state.generation==1 && b.state.never_enabled);
        assert(h.config_calls==1 && !h.reads && !h.enables && !h.disables && !h.contains && h.reset==15);
        fes_nano_identity_configuration_sip(&p,17,1,0,r);assert(r[0]==2 && h.config_calls==1);
        h.configured=1;fes_nano_identity_sip(&p,17,1,0,r);
        assert(!r[0] && h.config_calls==2 && h.reads==30 && h.reset==15);cases++;
    }
    for(unsigned fault=0;fault<3;fault++) {
        setup(&h,&b,&p);
        if(fault==0)h.configured=-7;
        if(fault==1)h.configured=2;
        if(fault==2)h.change_config=1;
        fes_nano_identity_configuration_sip(&p,17,1,0,r);
        assert(r[0]==4 && !r[2] && !r[3] && p.failed && b.failed && !p.active);
        assert(!h.reads && !h.enables && !h.disables && h.contains==1 && h.config_calls==1);
        fes_nano_identity_configuration_sip(&p,17,1,0,r);assert(r[0]==4 && h.config_calls==1);cases++;
    }
    for (unsigned read=1;read<=30;read++) {
        setup(&h,&b,&p);h.fail_read=read;
        fes_nano_identity_sip(&p,17,1,0,r);
        assert(r[0]==4 && !r[2] && !r[3] && p.first_error==-5 && p.stage==FES_ID_READ);
        assert(h.reset==15 && h.enables==1 && h.disables==1 && h.contains==1 && h.reads==read);
        assert(p.failed && b.failed && !p.active);
        unsigned reads=h.reads;h.fail_read=0;fes_nano_identity_sip(&p,17,1,0,r);
        assert(r[0]==4 && reads==h.reads && h.enables==1);cases++;
    }
    for (unsigned fault=0;fault<11;fault++) {
        setup(&h,&b,&p);
        switch(fault) {
        case 0:h.fail_enable=1;break;case 1:h.fail_enable=2;break;
        case 2:h.fail_disable=1;break;case 3:h.change_read=1;break;
        case 4:h.change_read=2;break;case 5:h.change_read=3;break;
        case 6:h.change_read=4;break;case 7:h.change_read=19;break;
        case 8:h.busy_word=1;break;case 9:h.lose_data=1;break;case 10:h.lose_owner=1;break;
        }
        fes_nano_identity_sip(&p,17,1,0,r);
        assert(r[0]==4 && !r[2] && !r[3] && p.failed && b.failed && !p.active && h.contains==1);
        assert((h.reset&FES_NANO_BRIDGES_CONTROL)==FES_NANO_BRIDGES_CONTROL);
        assert((p.cleanup_error!=0)==(fault==9));
        if (fault>=3 && fault<=8)assert(p.first_error==-71);
        if (fault==2)assert(p.stage==FES_ID_CLOSE);
        cases++;
    }
    printf("PASS %u identity/control containment cases\n",cases);
}
