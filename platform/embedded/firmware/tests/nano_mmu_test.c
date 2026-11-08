/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_mmu.h"
#include "nano_mmu_memory.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>

struct fixture {
    struct fes_nano_mmu state;
    struct fes_nano_mmu_policy policy;
    struct fes_nano_mmu_registers registers;
    unsigned events, fail_event, writes, barriers, admits, drop_write, reenter;
    unsigned order[4];
    volatile uint64_t line[8] __attribute__((aligned(64)));
    unsigned flushes, corrupt_flush;
};
static struct fes_nano_mmu_ops ops(struct fixture *);
static struct fixture canonical(void)
{
    return (struct fixture){.policy={0x8003c000},.registers={
        .el=12,.midr=0x412fd050,.mpidr=0x81000000,.mmfr0=2,.daif=0x3c0,
        .scr=0x530,.sctlr=0x30c5183a,.mair=0x1234,.tcr=0x5678,.root=0x9abc,
        .sole_cpu=1}};
}
static int event(struct fixture *f)
{ return ++f->events==f->fail_event; }
static int observe(void *p,struct fes_nano_mmu_registers *r)
{ struct fixture *f=p;if(event(f))return -1;*r=f->registers;return 0; }
static int admit(void *p)
{ struct fixture *f=p;++f->admits;return event(f) ? 0 : 1; }
static int write_reg(void *p,unsigned reg,uint64_t value)
{
    struct fixture *f=p;assert(f->writes<4);f->order[f->writes++]=reg;
    if(event(f))return -1;
    if(f->reenter) {
        f->reenter=0;
        assert(fes_nano_mmu_enable(&f->state,&f->policy,ops(f))<0);
    }
    if(f->writes==f->drop_write)return 0;
    switch(reg) {
    case FES_MMU_MAIR:assert(!(f->registers.sctlr&1U));f->registers.mair=value;break;
    case FES_MMU_TCR:f->registers.tcr=value;break;
    case FES_MMU_ROOT:f->registers.root=value;break;
    case FES_MMU_SCTLR:
        assert(f->barriers==2 && f->admits==2 && f->registers.mair==FES_NANO_MMU_MAIR &&
               f->registers.tcr==FES_NANO_MMU_TCR && f->registers.root==f->policy.root);
        f->registers.sctlr=value;break;
    default:assert(0);
    }
    return 0;
}
static int barrier(void *p,unsigned phase)
{
    struct fixture *f=p;assert(phase==f->barriers++);
    assert(!(f->registers.sctlr&5U));
    return event(f) ? -1 : 0;
}
static int flush_line(void *p)
{
    struct fixture *f=p;
    if(++f->flushes==f->corrupt_flush)f->line[0]^=1;
    return 0;
}
static int memory(void *p)
{
    struct fixture *f=p;if(event(f))return -1;
    return fes_nano_mmu_memory_probe(f->line,f,flush_line);
}
static struct fes_nano_mmu_ops ops(struct fixture *f)
{ return (struct fes_nano_mmu_ops){f,observe,admit,write_reg,barrier,memory}; }
static void failed_stays_failed(struct fixture *f)
{
    assert(f->state.stage==FES_MMU_FAILED && f->state.first_error);
    unsigned events=f->events,writes=f->writes;
    int error=f->state.first_error;
    assert(!fes_nano_mmu_scope(&f->state,&f->policy,ops(f)));
    assert(fes_nano_mmu_enable(&f->state,&f->policy,ops(f))<0);
    assert(f->events==events && f->writes==writes && f->state.first_error==error);
}
int main(void)
{
    struct fixture f=canonical();
    assert(fes_nano_mmu_scope(&f.state,&f.policy,ops(&f))==1 && !f.writes);
    f=canonical();
    assert(!fes_nano_mmu_enable(&f.state,&f.policy,ops(&f)));
    assert(f.state.stage==FES_MMU_ACTIVE && f.state.attempted && f.state.writes==4);
    assert(f.state.memory_verified && f.flushes==2);
    for(unsigned i=0;i<4;i++)assert(f.order[i]==i);
    assert(f.registers.sctlr==(0x30c5183aULL|FES_NANO_MMU_SET));
    unsigned events=f.events;
    f.registers.scr|=1;f.registers.daif&=~0x100ULL;
    assert(fes_nano_mmu_scope(&f.state,&f.policy,ops(&f))==1);
    assert(fes_nano_mmu_enable(&f.state,&f.policy,ops(&f))<0);
    failed_stays_failed(&f);
    for(unsigned n=1;n<=events;n++) {
        f=canonical();f.fail_event=n;
        assert(fes_nano_mmu_enable(&f.state,&f.policy,ops(&f))<0);
        assert(f.events==n && f.writes<=4);
        failed_stays_failed(&f);
    }
    for(unsigned n=1;n<=4;n++) {
        f=canonical();f.drop_write=n;
        assert(fes_nano_mmu_enable(&f.state,&f.policy,ops(&f))<0);
        assert(f.writes==n);failed_stays_failed(&f);
    }
    for(unsigned mode=0;mode<16;mode++) {
        f=canonical();
        switch(mode) {
        case 0:f.registers.el=8;break;
        case 1:f.registers.midr++;break;
        case 2:f.registers.mpidr^=0x200;break;
        case 3:f.registers.mmfr0|=1ULL<<28;break;
        case 4:f.registers.mmfr0=15;break;
        case 5:f.registers.sole_cpu=0;break;
        case 6:f.registers.daif&=~0x200ULL;break;
        case 7:f.registers.daif&=~0x80ULL;break;
        case 8:f.registers.daif&=~0x40ULL;break;
        case 9:f.registers.scr|=1;break;
        case 10:f.registers.sctlr|=1;break;
        case 11:f.registers.sctlr|=4;break;
        case 12:f.registers.sctlr&=~0x1000ULL;break;
        case 13:f.registers.sctlr|=1ULL<<19;break;
        case 14:f.registers.sctlr|=1ULL<<24;break;
        case 15:f.registers.sctlr|=1ULL<<25;break;
        }
        assert(fes_nano_mmu_enable(&f.state,&f.policy,ops(&f))<0 && !f.writes);
        failed_stays_failed(&f);
    }
    for(unsigned mode=0;mode<13;mode++) {
        f=canonical();assert(!fes_nano_mmu_enable(&f.state,&f.policy,ops(&f)));
        struct fes_nano_mmu_registers saved=f.registers;
        switch(mode) {
        case 0:f.registers.sctlr^=1;break;
        case 1:f.registers.sctlr^=4;break;
        case 2:f.registers.sctlr^=2;break; /* Even preserved alignment settings. */
        case 3:f.registers.mair^=0x400;break;
        case 4:f.registers.tcr^=0x100;break;
        case 5:f.registers.root^=4096;break;
        case 6:f.registers.mpidr^=0x200;break;
        case 7:f.registers.el=4;break;
        case 8:f.registers.mmfr0^=0x100;break;
        case 9:f.registers.sole_cpu=0;break;
        case 10:f.registers.daif=0;break;
        case 11:f.policy.root+=4096;break;
        case 12:f.state.memory_verified=0;break;
        }
        assert(!fes_nano_mmu_scope(&f.state,&f.policy,ops(&f)));
        f.registers=saved;failed_stays_failed(&f);
    }
    for(unsigned mode=0;mode<4;mode++) {
        f=canonical();
        f.policy.root=(uint64_t[]){0x80000000,0x80001001,0x801fb000,0xfffffffffffff000ULL}[mode];
        assert(fes_nano_mmu_enable(&f.state,&f.policy,ops(&f))<0 && !f.events);
        failed_stays_failed(&f);
    }
    for(unsigned mode=0;mode<5;mode++) {
        f=canonical();struct fes_nano_mmu_ops io=ops(&f);
        switch(mode) {case 0:io.observe=0;break;case 1:io.admit=0;break;
                      case 2:io.write=0;break;case 3:io.barrier=0;break;case 4:io.memory=0;break;}
        assert(fes_nano_mmu_enable(&f.state,&f.policy,io)<0 && !f.events);
        failed_stays_failed(&f);
    }
    f=canonical();f.reenter=1;
    assert(fes_nano_mmu_enable(&f.state,&f.policy,ops(&f))<0 && f.writes==1);
    failed_stays_failed(&f);
    f=canonical();f.registers.scr|=1;
    assert(!fes_nano_mmu_scope(&f.state,&f.policy,ops(&f)) && !f.writes);
    f.registers.scr&=~1ULL;failed_stays_failed(&f);
    for(unsigned n=1;n<=2;n++) {
        f=canonical();f.corrupt_flush=n;
        assert(fes_nano_mmu_enable(&f.state,&f.policy,ops(&f))<0 && f.writes==4 && !f.state.memory_verified);
        failed_stays_failed(&f);
    }
    printf("PASS one-attempt MMU enable: %u callback failures, dropped writes, CPU/attribute faults, NS-before-enable and replay\n",events);
}
