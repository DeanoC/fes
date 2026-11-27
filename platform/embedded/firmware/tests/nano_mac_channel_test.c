/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_mac_channel.h"
#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#ifdef FES_NANO_MAC_JOINED_TEST
#include "nano_mac_joined_fixture.h"
#endif

#define MAC 0x10810000U
#define PHYS 0x95c00000U
#define RX (PHYS+512)
#define RESETS 0x101U
#define OWN (1U<<31)
#define IOC (1U<<30)

static struct model {
    uint32_t regs[0x3500/4], pool[5*4096/4], reset, step;
    uint64_t now, generation, change_after;
    unsigned owner, revoked, timer_ok, guard_count, lose_after, reads, flip_address, rollover;
} f;
#define REG(x) f.regs[(x)/4]
#ifndef FES_NANO_MAC_JOINED_TEST
static int owner(void *ctx, uint64_t generation,unsigned revoked)
{
    assert(ctx == &f);
    f.guard_count++;
    return generation==f.generation && revoked==f.revoked &&
           (!f.lose_after || f.guard_count < f.lose_after) ? (int)f.owner : 0;
}
static const fes_channel_guards guard={&f,owner};
#endif
uint64_t channel_time(void)
{
    f.now+=f.step;
    if (f.rollover && f.now>2000) return 0;
    if (f.change_after && f.now>=f.change_after) {
        f.pool[f.flip_address]^=0x40;
        f.change_after=0;
    }
    return f.now;
}
int channel_frequency(void) {return f.timer_ok;}
void channel_barrier(void) {}
uint32_t channel_mmio(uint32_t a)
{
    if(a==0x10d11024) return f.reset;
    assert(a>=MAC && a<MAC+sizeof(f.regs) && !(f.reset & RESETS) && !((a-MAC)%4));
    return REG(a-MAC);
}
uint32_t channel_word(uint32_t off)
{
    assert(off<sizeof(f.pool) && !(off%4));
    f.reads++;
    return f.pool[off/4];
}
static void clear(void)
{
    memset(&f,0,sizeof(f));
    f.generation=11; f.timer_ok=f.owner=1; f.step=1;
}
static void setup(void)
{
    memset(f.regs,0,sizeof(f.regs)); memset(f.pool,0,sizeof(f.pool));
    REG(0x11c)=0xcafd5b3f;
    REG(0x300)=0x2e49; REG(0x304)=0x0511bb02;
    REG(0x3004)=2; REG(0x3114)=PHYS; REG(0x311c)=RX; REG(0x312c)=RX+64;
    REG(0x3130)=REG(0x3134)=31; REG(0xa0)=2; REG(0xa4)=1U<<15;
    REG(0x1100)=(127U<<16)|10; REG(0x1140)=(63U<<16)|32;
    REG(0)=0x60000001; REG(4)=7; REG(0x3104)=0x40001; REG(0x3108)=0x40c81;
    for(unsigned i=0;i<4;i++){
        f.pool[(32+i)*4]=PHYS+3*4096+1600*i;
        f.pool[(32+i)*4+3]=OWN|IOC;
    }
}
#ifndef FES_NANO_MAC_JOINED_TEST
static void active(fes_channel *c)
{
    assert(fes_nano_mac_channel_prepare(c,&guard,f.generation)==0);
    setup();
    assert(fes_nano_mac_channel_confirm(c,&guard,f.generation)==0);
}
#endif
static void stopped(void)
{
    REG(0)&=~1; REG(4)&=~1; REG(0x3104)&=~1; REG(0x3108)&=~1;
    /* Both real BIDI05 terminal captures reported the two process-stop bits
     * in 0x186, all independent channel, transaction and FIFO probes zero. */
    REG(0x3160)=0x186; REG(0x3138)=0;
}
#ifdef FES_NANO_MAC_JOINED_TEST
void fes_mac_fixture_channel_boot(void) { clear(); }
void fes_mac_fixture_core_reset(void)
{
    memset(f.regs, 0, sizeof(f.regs));
    REG(0x110)=0x107631; REG(0x11c)=0xcafd5b3f;
    REG(0x120)=0x49ef7a27; REG(0x124)=0x221c71c7;
}
void fes_mac_fixture_set_reset(uint32_t value)
{
    f.reset=value;
    if ((value & RESETS)==RESETS) fes_mac_fixture_core_reset();
}
unsigned fes_mac_fixture_engines_live(void)
{
    return ((REG(0)|REG(4)|REG(0x3104)|REG(0x3108)) & 1U) != 0;
}
void fes_mac_fixture_driver_start(void)
{
    assert(!(f.reset & RESETS) && !fes_mac_fixture_engines_live());
    setup();
    REG(0x110)=0x107631; REG(0x120)=0x49ef7a27; REG(0x124)=0x221c71c7;
}
void fes_mac_fixture_driver_stop(void)
{
    assert(!(f.reset & RESETS) && fes_mac_fixture_engines_live());
    stopped();
}
void fes_mac_fixture_fault(enum fes_mac_fixture_fault fault)
{
    switch(fault){
    case FES_FIXTURE_RX_PENDING:REG(0x1148)|=1U<<16;break;
    case FES_FIXTURE_DMA_FAULT:REG(0x3160)|=1U<<12;break;
    case FES_FIXTURE_CHANGE_RX_DATA:f.pool[3*1024+23]^=0x40;break;
    }
}
#else
static void healthy(void)
{
    clear(); fes_channel c={0};
    for(unsigned k=0;k<2;k++){
        active(&c);
        assert(fes_nano_mac_channel_scope(&c,&guard,f.generation)==0);
        /* Ordinary MDNS filter and a descriptor returned before a scope call.
         * In writeback format the address word no longer denotes a buffer. */
        REG(8)=4; REG(0x14)=1U<<16;
        f.pool[32*4]=0xcafef00d; f.pool[32*4+3]=0x3007003c;
        f.pool[36*4]=PHYS+3*4096; f.pool[36*4+3]=OWN|IOC;
        REG(0x312c)=RX+80; REG(0x3138)=0x8040;
        f.pool[4]=PHYS+4096+1600; f.pool[6]=60; f.pool[7]=OWN|0x3000003c;
        REG(0x3124)=PHYS+32;
        assert(fes_nano_mac_channel_scope(&c,&guard,f.generation)==0);
        f.pool[7]=0x3000003c; REG(0x3104)&=~1;
        assert(fes_nano_mac_channel_scope(&c,&guard,f.generation)==0);
        REG(0x3160)|=2; REG(0)&=~1;
        assert(fes_nano_mac_channel_scope(&c,&guard,f.generation)==0);
        REG(4)&=~1;
        assert(fes_nano_mac_channel_scope(&c,&guard,f.generation)==0);
        REG(0x3108)&=~1;
        assert(fes_nano_mac_channel_scope(&c,&guard,f.generation)==0);
        stopped();
        assert(fes_nano_mac_channel_scope(&c,&guard,f.generation)==0);
        assert(fes_nano_mac_channel_drain(&c,&guard,f.generation)==0);
        assert(fes_nano_mac_channel_drained(&c,&guard,f.generation)==0);
        f.reset=RESETS;
        assert(fes_nano_mac_channel_drained(&c,&guard,f.generation)==0);
        f.revoked=1;
        assert(fes_nano_mac_channel_retained(&c,&guard,f.generation)==0);
        assert(c.dwell_us>=10000 && c.phase==FES_CHANNEL_RETAINED && c.status==0x186 && !c.failed_stage);
        f.generation++; f.revoked=0;f.reset=0;memset(f.regs,0,sizeof(f.regs));
    }
}
static void bad_confirm(void)
{
    for(unsigned n=0;n<22;n++){
        clear();fes_channel c={0};
        assert(fes_nano_mac_channel_prepare(&c,&guard,11)==0); setup();
        switch(n){
        case 0:REG(0x3114)+=4096;break;
        case 1:REG(0x311c)+=16;break;
        case 2:REG(0x3110)=1;break;
        case 3:REG(0x3118)=1;break;
        case 4:REG(0x3134)=15;break;
        case 5:REG(0x312c)+=16;break;
        case 6:f.pool[32*4]+=4096;break;
        case 7:f.pool[33*4+1]=1;break;
        case 8:f.pool[33*4+2]=1;break;
        case 9:f.pool[33*4+3]=0;break;
        case 10:f.pool[36*4]=4;break;
        case 11:REG(0x304)|=1;break;
        case 12:REG(0x308)=1U<<31;break;
        case 13:REG(8)=1;break;
        case 14:REG(8)=4;REG(0x14)=1;break;
        case 15:REG(0x3108+7*0x80)=1;break;
        case 16:REG(0x3160)=0x100;break;
        case 17:f.timer_ok=0;break;
        case 18:f.owner=2;break;
        case 19:f.lose_after=f.guard_count+3;break;
        case 20:REG(0x1140)=0;break;
        case 21:REG(0x3100)=1;break;
        }
        assert(fes_nano_mac_channel_confirm(&c,&guard,11)!=0 && c.phase==FES_CHANNEL_FAILED);
    }
}
static void faults(void)
{
    clear();fes_channel backwards={0};active(&backwards);
    REG(0x3104)&=~1; assert(fes_nano_mac_channel_scope(&backwards,&guard,11)==0);
    REG(0x3104)|=1; assert(fes_nano_mac_channel_scope(&backwards,&guard,11)!=0);
    for(unsigned n=0;n<15;n++){
        clear();fes_channel c={0};active(&c);stopped();
        switch(n){
        case 0:REG(0x3160)&=~2;break;
        case 1:REG(0x3160)&=~0x100;break;
        case 2:REG(0x3160)|=1U<<12;break;
        case 3:REG(0x1108)=1U<<5;break;
        case 4:REG(0x1148)=1U<<16;break;
        case 5:REG(0x3020)=1;break;
        case 6:REG(0x3164)=1U<<19;break;
        case 7:REG(0x114)=1;break;
        case 8:REG(0x3104)|=1;break;
        case 9:REG(0x3138)=0x8040;break;
        case 10:REG(0x304)^=0x10;break;
        case 11:f.pool[36*4]=PHYS+3*4096; f.pool[36*4+3]=OWN|IOC;break;
        case 12:f.step=20000;break;
        case 13:f.rollover=1;f.now=1900;break;
        case 14:f.revoked=1;break;
        }
        assert(fes_nano_mac_channel_drain(&c,&guard,11)!=0 && c.phase==FES_CHANNEL_FAILED);
        unsigned reason=c.failed_stage;
        assert(fes_nano_mac_channel_prepare(&c,&guard,12)!=0 && c.failed_stage==reason);
    }
    for(unsigned n=0;n<9;n++){
        clear();fes_channel c={0};active(&c);stopped();
        assert(fes_nano_mac_channel_drain(&c,&guard,11)==0);
        f.reset=RESETS; f.revoked=1;
        if(n<5) {f.flip_address=n*1024+73; f.change_after=f.now+3000;}
        if(n==5) f.revoked=0;
        if(n==6) f.reset=1;
        if(n==7) f.step=0;
        if(n==8) f.lose_after=f.guard_count+400;
        assert(fes_nano_mac_channel_retained(&c,&guard,11)!=0 && c.phase==FES_CHANNEL_FAILED);
    }
}
int main(void)
{
    healthy();bad_confirm();faults();
    puts("PASS production CBL MAC descriptors, independent drain and revoked retention");
    return 0;
}
#endif
