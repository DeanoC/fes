/* SPDX-License-Identifier: BSD-3-Clause */
#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include "nano_hps_io.h"
#include "generated/fes_embedded_bridges.h"

struct model { unsigned reads, owns, lost, bad_read, bad_value, lose_after; };
static struct fes_nano_hps_io_register registers[FES_NANO_HPS_IO_COUNT];
static const uint8_t digest[32]={1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,
                               17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32};
static int owns(void *p,uint64_t owner,uint64_t generation)
{
    struct model *m=p;m->owns++;
    return !m->lost && owner==11 && generation==7;
}
static int read_reg(void *p,uint32_t offset,uint32_t *value)
{
    struct model *m=p;unsigned index=m->reads++%FES_NANO_HPS_IO_COUNT;
    assert(offset==registers[index].offset && fes_nano_hps_io_offset(offset));
    if (m->reads==m->bad_read) return -17;
    *value=registers[index].value^(m->reads==m->bad_value);
    if (m->reads==m->lose_after) m->lost=1;
    return 0;
}
static void barrier(void *p) { (void)p; }
static void setup(struct fes_nano_hps_io *s,struct model *m)
{
    *s=(struct fes_nano_hps_io){0};*m=(struct model){0};unsigned n=0;
    for (unsigned offset=0x1000;offset<=0x14bc;offset+=4) if (fes_nano_hps_io_offset(offset)) {
        assert(n<FES_NANO_HPS_IO_COUNT);
        registers[n++]=(struct fes_nano_hps_io_register){offset,0,0xffffffffU};
    }
    assert(n==FES_NANO_HPS_IO_COUNT);
    assert(!fes_nano_hps_io_init(s,(struct fes_nano_hps_io_ops){m,owns,read_reg,barrier,0},registers,n,digest));
}
static void call(struct fes_nano_hps_io *s,unsigned part,unsigned busy,uint64_t r[4])
{
    fes_nano_hps_io_sip(s,FES_EMBEDDED_BRIDGE_HPS_IO_PART0+part,11,7,busy,r);
}
int main(void)
{
    struct fes_nano_hps_io s;struct model m;uint64_t r[4];
    setup(&s,&m);call(&s,0,0,r);
    assert(!r[0] && r[1]==((uint64_t)FES_EMBEDDED_BRIDGE_HPS_IO_TAG<<32));
    assert(r[2]==0x0102030405060708ULL && r[3]==0x090a0b0c0d0e0f10ULL && m.reads==328 && s.pending);
    call(&s,1,0,r);assert(!r[0] && !s.pending && m.reads==328 && r[2]==0x1112131415161718ULL && r[3]==0x191a1b1c1d1e1f20ULL);
    call(&s,0,0,r);assert(!r[0] && m.reads==656);call(&s,1,0,r);assert(!r[0]);
    /* Busy sends no read and cannot consume a pending successful part0. */
    setup(&s,&m);call(&s,0,1,r);assert(r[0]==1 && !m.reads && !s.pending);
    call(&s,0,0,r);call(&s,1,1,r);assert(r[0]==1 && s.pending && m.reads==328);
    call(&s,1,0,r);assert(!r[0] && !s.pending);
    /* A premature/repeated part or changed scope cannot become a receipt. */
    setup(&s,&m);call(&s,1,0,r);assert(r[0] && s.failed && !m.reads);
    setup(&s,&m);call(&s,0,0,r);call(&s,0,0,r);assert(r[0] && s.failed && m.reads==328);
    setup(&s,&m);call(&s,0,0,r);fes_nano_hps_io_sip(&s,FES_EMBEDDED_BRIDGE_HPS_IO_PART1,11,8,0,r);assert(r[0] && s.failed && !r[2] && !r[3]);
    setup(&s,&m);call(&s,0,0,r);m.lost=1;call(&s,1,0,r);assert(r[0] && s.failed);
    /* Every first/second-pass read position is bounded and checked. */
    for (unsigned index=1;index<=328;index++) for (unsigned failure=0;failure<3;failure++) {
        setup(&s,&m);
        if (!failure) m.bad_read=index;
        else if (failure==1) m.bad_value=index;
        else m.lose_after=index;
        call(&s,0,0,r);assert(r[0] && s.failed && m.reads==index && !r[2] && !r[3]);
        int error=s.first_error;call(&s,0,0,r);call(&s,1,0,r);
        assert(r[0] && m.reads==index && s.first_error==error);
        if (!failure) assert(error==-17);
    }
    /* Bad/omitted/duplicated or masked source rows reject before any I/O. */
    for (unsigned kind=0;kind<4;kind++) {
        setup(&s,&m);s=(struct fes_nano_hps_io){0};
        if (!kind) registers[0].offset=0x14c0;
        else if (kind==1) registers[1].offset=registers[0].offset;
        else if (kind==2) registers[0].mask=15;
        unsigned count=kind==3 ? 163 : 164;
        assert(fes_nano_hps_io_init(&s,(struct fes_nano_hps_io_ops){&m,owns,read_reg,barrier,0},registers,count,digest));
        assert(s.failed && !m.reads && !m.owns);
    }
    assert(!fes_nano_hps_io_offset(0x14c0) && !fes_nano_hps_io_offset(0x14c4));
    assert(!fes_nano_hps_io_offset(0x1324) && !fes_nano_hps_io_offset(0x134c) && !fes_nano_hps_io_offset(0x1358));
    fes_nano_hps_io_sip(0,FES_EMBEDDED_BRIDGE_HPS_IO_PART0,11,7,0,r);assert(r[0]==FES_EMBEDDED_BRIDGE_NOT_READY);
    puts("PASS bounded 164-register HPS I/O observation, scoped digest and 984 read/ownership failure positions");
    return 0;
}
