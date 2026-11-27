/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_mac_phy.h"
#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#define MAC 0x10810000U
#define CLOCK 0x10d10000U
#define PER0 0x10d11024U
#define MODE 0x10d12044U
#define SCRATCH 0x10d12204U
#define RESTORE 0xff7fbfffU

/* Addresses not observed on the production path fail the test, including any
 * DMA/STE/tail/PHY write other than the fixed reversible sequence. */
static struct fixture {
    uint64_t us, first_link, last_link;
    uint32_t step, cl[64], reset, mode, addr, data, c22, osc;
    uint16_t reg[32], mmd[32], hi, lo;
    unsigned owns, idle, timer_ok, backward, stuck_mdio, running, wrong_mac;
    unsigned writes, phy_writes, timing_writes, restarts, samples, saturations;
    unsigned withdraw_at, remove_clock_at, revoke_drain_at, omit_swr, freeze_restart;
} f;

uint64_t phy_us(void)
{
    if (f.backward && f.us > 50000) return 0;
    return f.us += f.step;
}
int phy_clock_valid(void) { return (int)f.timer_ok; }
static int owned(void *unused) { (void)unused; return f.owns; }
static int drained(void *unused) { (void)unused; return f.idle; }
static const struct fes_nano_mac_phy_guards guards = {0, owned, drained};

uint32_t phy_read(uint32_t a)
{
    if (a >= CLOCK && a < CLOCK + 256) return f.cl[(a-CLOCK)/4];
    if (a == SCRATCH) return f.osc;
    if (a == PER0) return f.reset;
    if (a == MODE) return f.mode;
    assert(!(f.reset & 0x101));
    if (a == MAC || a == MAC+4) return 0;
    if (a == MAC+0x3000) return f.omit_swr;
    if (a == MAC+0x110) return f.wrong_mac ? 0 : 0x107631;
    if (a == MAC+0x11c) return 0xcafd5b3f;
    if (a == MAC+0x120) return 0x49ef7a27;
    if (a == MAC+0x124) return 0x221c71c7;
    if (a == MAC+0x204) return f.stuck_mdio ? 1U<<22 : f.data;
    if (a == MAC+0x220) return f.c22;
    if (a == MAC+0x200) return f.addr;
    for (unsigned i=0; i<8; i++)
        if (a == MAC+0x3104+i*0x80 || a == MAC+0x3108+i*0x80)
            return i == 7 && f.running;
    assert(0 && "unexpected MMIO read"); return 0;
}

static uint16_t reg_read(unsigned r)
{
    uint16_t sel = f.reg[13];
    unsigned dev = sel & 31;
    if (r == 14) {
        if (!(sel & 0xc000)) return f.mmd[dev];
        assert((sel & 0xc000) == 0x4000 && dev == 0);
        assert(f.mmd[dev] == 3 || f.mmd[dev] == 4);
        return f.mmd[dev] == 3 ? f.lo : f.hi;
    }
    if (r == 10 && f.samples < f.saturations) return 0x3cff;
    if (r == 31) {
        f.samples++;
        if (!f.first_link) f.first_link = f.us;
        f.last_link = f.us;
    }
    return f.reg[r];
}
static void reg_write(unsigned r, uint16_t value)
{
    f.phy_writes++;
    uint16_t sel=f.reg[13];
    if (r == 13) f.reg[13] = value;
    else if (r == 14 && !(sel & 0xc000)) f.mmd[sel&31] = value;
    else if (r == 14) {
        assert(sel == 0x4000 && (f.mmd[0] == 3 || f.mmd[0] == 4));
        assert(f.idle && !f.running);
        if (f.mmd[0] == 3) f.lo = value;
        else f.hi = value;
        f.timing_writes++;
    } else {
        assert(r==0 && value==(f.reg[0] | 0x200));
        assert(f.idle && !f.running);
        f.restarts++;
        if (f.freeze_restart) f.reg[0] = value;
    }
}
void phy_write(uint32_t a, uint32_t v)
{
    assert(f.owns && f.timer_ok && f.osc == 25000000);
    f.writes++;
    if (a == MODE) f.mode = v;
    else if (a == PER0) {
        assert(((f.reset ^ v) & ~0x101U) == 0);
        f.reset = v;
    } else {
        assert(!(f.reset & 0x101));
        switch (a-MAC) {
        case 0x3000: assert(v == 1 && !f.running); break;
        case 0x220: assert(v==2); f.c22=v; break;
        case 0x200: assert((v>>16)==1 && (uint16_t)v<32); f.addr=v; break;
        case 0x204:
            assert((v & 0xfffc0000U)==0x440000);
            if (((v>>16)&3)==3) f.data=reg_read((uint16_t)f.addr);
            else { assert(((v>>16)&3)==1); reg_write((uint16_t)f.addr, (uint16_t)v); f.data=0; }
            break;
        default: assert(0 && "unqualified write");
        }
    }
    if (f.writes==f.withdraw_at) f.owns=0;
    if (f.writes==f.remove_clock_at) f.osc=24000000;
    if (f.writes==f.revoke_drain_at) f.idle=0;
}
static struct fes_nano_mac_phy reset_fixture(void)
{
    memset(&f,0,sizeof(f));
    f.owns=f.idle=f.timer_ok=1;
    f.step=5; f.osc=25000000; f.reset=RESTORE; f.mode=3;
    /* Fitted C64+MENU agilex_hps.xml. Raw gate values and PHY/CAPS responses
     * are from the retained CBL admission-05 and two completed BIDI05 rounds. */
    f.cl[4/4]=0x02030300; f.cl[0x24/4]=0xf5e; f.cl[0x44/4]=0x28240098;
    f.cl[0x7c/4]=0xfffffff; f.cl[0x48/4]=0x100; f.cl[0x6c/4]=128;
    f.cl[0x68/4]=8; f.cl[0x9c/4]=0x100; f.cl[0xc0/4]=120;
    f.cl[0xbc/4]=6; f.cl[0xd4/4]=0x10001;
    f.reg[0]=0x1140; f.reg[1]=0x796d; f.reg[2]=0x22; f.reg[3]=0x1622;
    f.reg[4]=0x1e1; f.reg[5]=0xcde1; f.reg[9]=0x300; f.reg[10]=0x3c00;
    f.reg[19]=0x0806; f.reg[31]=0x0348;
    f.reg[13]=0xc003; f.mmd[3]=0xbeef; f.mmd[0]=0x5a5a;
    f.hi=3; f.lo=0x4000;
    return (struct fes_nano_mac_phy){0};
}
static void verify_cycles(void)
{
    struct fes_nano_mac_phy p=reset_fixture();
    for (unsigned i=0;i<2;i++) {
        f.saturations=f.samples+2;
        assert(fes_nano_mac_phy_prepare(&p,&guards)==0);
        assert(p.ready && f.mode==1 && f.reset==(RESTORE&~0x101U));
        assert(f.hi==6 && f.lo==0x1a80 && f.last_link-f.first_link>=100000);
        unsigned n=f.writes;
        assert(fes_nano_mac_phy_prepare(&p,&guards)!=0);
        assert(fes_nano_mac_phy_active(&p,&guards)==0 && f.writes==n);
        f.idle=0;
        assert(fes_nano_mac_phy_retire(&p,&guards)!=0 && f.writes==n);
        f.idle=1; f.running=1;
        assert(fes_nano_mac_phy_retire(&p,&guards)!=0 && f.writes==n);
        f.running=0;
        assert(fes_nano_mac_phy_retire(&p,&guards)==0 && !p.begun);
        assert(f.reset==RESTORE && f.mode==3 && f.hi==3 && f.lo==0x4000);
        assert(f.mmd[0]==0x5a5a && f.mmd[3]==0xbeef && f.reg[13]==0xc003);
        assert(f.restarts==2*(1+i));
    }
}
static void verify_rejections(void)
{
    struct fes_nano_mac_phy p;
    for (unsigned which=0;which<10;which++) {
        p=reset_fixture();
        switch (which) {
        case 0:f.owns=2;break;
        case 1:f.timer_ok=0;break;
        case 2:f.osc=24000000;break;
        case 3:f.cl[0xd4/4]=1;break;
        case 4:f.cl[0xbc/4]=0;break;
        case 5:f.cl[0x48/4]=0;break;
        case 6:f.cl[0x44/4]=0;break;
        case 7:f.cl[0x7c/4]=0;break;
        case 8:f.reset &= ~1U;break;
        case 9:f.mode=0x80000003;break;
        }
        assert(fes_nano_mac_phy_prepare(&p,&guards)!=0 && !f.writes);
    }
    p=reset_fixture(); f.reg[3]=0x1621;
    assert(fes_nano_mac_phy_prepare(&p,&guards)!=0 && !f.phy_writes);
    assert(fes_nano_mac_phy_retire(&p,&guards)==0 && f.reset==RESTORE);
    p=reset_fixture(); f.hi=5;
    assert(fes_nano_mac_phy_prepare(&p,&guards)!=0 && !f.timing_writes);
    assert(fes_nano_mac_phy_retire(&p,&guards)==0 && f.hi==5 && f.reg[13]==0xc003);
    p=reset_fixture(); f.hi=6; f.lo=0x1a80;
    assert(fes_nano_mac_phy_prepare(&p,&guards)==0 && !f.timing_writes);
    assert(fes_nano_mac_phy_retire(&p,&guards)==0 && f.restarts==1 && !f.timing_writes);
    p=reset_fixture(); f.reg[19]=2; f.step=25;
    assert(fes_nano_mac_phy_prepare(&p,&guards)!=0 && f.us>=25000000 && f.us<30000000);
    assert(fes_nano_mac_phy_retire(&p,&guards)==0);
    p=reset_fixture(); f.freeze_restart=1;
    assert(fes_nano_mac_phy_prepare(&p,&guards)!=0 && p.timing_changed && f.us>=500000);
    assert(fes_nano_mac_phy_retire(&p,&guards)!=0 && f.restarts==1);
    p=reset_fixture(); f.stuck_mdio=1;
    assert(fes_nano_mac_phy_prepare(&p,&guards)!=0 && !f.phy_writes && f.us>=100000 && f.us<110000);
    p=reset_fixture(); f.step=0;
    assert(fes_nano_mac_phy_prepare(&p,&guards)!=0 && !p.ready);
    p=reset_fixture(); f.backward=1;
    assert(fes_nano_mac_phy_prepare(&p,&guards)!=0 && !p.ready);
    p=reset_fixture(); f.withdraw_at=120;
    assert(fes_nano_mac_phy_prepare(&p,&guards)!=0 && f.writes==120);
    p=reset_fixture(); f.remove_clock_at=120;
    assert(fes_nano_mac_phy_prepare(&p,&guards)!=0 && f.writes==120);
    p=reset_fixture();
    assert(fes_nano_mac_phy_prepare(&p,&guards)==0);
    unsigned n=f.writes;
    f.reg[9]^=1;
    assert(fes_nano_mac_phy_retire(&p,&guards)!=0 && p.begun && f.timing_writes==2);
    f.reg[9]^=1; f.revoke_drain_at=f.writes+3;
    assert(fes_nano_mac_phy_retire(&p,&guards)!=0 && f.writes==f.revoke_drain_at && f.writes>n);
}
/* Interrupt ownership after every distinct preparation write. A separately
 * re-established trusted scope can restore each recorded partial state. */
static void verify_partial(void)
{
    struct fes_nano_mac_phy p=reset_fixture();
    assert(fes_nano_mac_phy_prepare(&p,&guards)==0);
    unsigned all=f.writes;
    for (unsigned pos=1;pos<=all;pos++) {
        p=reset_fixture();
        f.withdraw_at=pos;
        assert(fes_nano_mac_phy_prepare(&p,&guards)!=0 && f.writes==pos);
        f.owns=1;
        assert(fes_nano_mac_phy_retire(&p,&guards)==0);
        assert(!p.begun && f.reset==RESTORE && f.mode==3);
        assert(f.hi==3 && f.lo==0x4000 && f.reg[13]==0xc003);
        assert(f.mmd[3]==0xbeef && f.mmd[0]==0x5a5a);
    }
}
int main(void)
{
    verify_cycles();
    verify_rejections();
    verify_partial();
    puts("PASS matched CBL MAC/PHY training, deadlines, ownership and restoration");
    return 0;
}
