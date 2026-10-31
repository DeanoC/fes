/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_cold.h"
#include "nano_bridge_sip.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>

struct hw { uint32_t r[11]; unsigned fault, writes, resets, waits, idle_reads; };
static int read_reg(void *p,unsigned reg,uint32_t *v)
{
    struct hw *h=p;assert(reg<11);
    if (reg==FES_COLD_SD_STATE) {
        /* A controller held in reset must never be read by BL31 callbacks. */
        assert(!(h->r[FES_COLD_PER0]&0x80));h->idle_reads++;
        if (h->fault==1) h->r[reg]=3;
    }
    if (h->fault==4 && reg==FES_COLD_SD_MODE)return -1;
    *v=h->r[reg];return 0;
}
static int write_reg(void *p,unsigned reg,uint32_t v)
{
    struct hw *h=p;h->writes++;
    if (reg==FES_COLD_PER0) {
        assert(h->idle_reads>=2 && !(h->r[FES_COLD_SD_STATE]&FES_NANO_COLD_SD_BUSY) && !(h->r[FES_COLD_SD_CLOCK]&4));
        h->resets++;
        assert(v==(h->resets==1 ? FES_NANO_MASTER_FUNCTION:FES_NANO_MASTER_RESET));
        if (h->fault==5)return -1;
        if (!(h->fault==6 && h->resets==2))h->r[reg]|=v;
        if (h->fault==7)h->r[FES_COLD_CPUS]&=~2U;
    } else if (!(h->fault==3 && reg==FES_COLD_SD_CLOCK))h->r[reg]=v;
    return 0;
}
static void barrier(void *p) { (void)p; }
static void wait_poll(void *p) { ((struct hw *)p)->waits++; }
static struct fes_nano_cold_io ops(struct hw *h)
{ return (struct fes_nano_cold_io){h,read_reg,write_reg,barrier,wait_poll}; }
static void init(struct hw *h,unsigned core)
{
    memset(h,0,sizeof(*h));h->r[FES_COLD_CAUSE]=1;h->r[FES_COLD_BOOT_KIND]=4;h->r[FES_COLD_PER0]=FES_NANO_COLD_UNUSED;
    h->r[FES_COLD_BRG]=15;h->r[FES_COLD_CPUS]=15U^(1U<<core);h->r[FES_COLD_SD_CLOCK]=7;
}
static int per_read(void *p,uint32_t *v) { struct fes_nano_cold *s=p;return s->io.read(s->io.context,FES_COLD_PER0,v); }
static int per_assert(void *p,uint32_t bits) { struct fes_nano_cold *s=p;return s->io.write(s->io.context,FES_COLD_PER0,bits); }
static int br_read(void *p,uint32_t *v) { struct fes_nano_cold *s=p;return s->io.read(s->io.context,FES_COLD_BRG,v); }
static int disabled(void *p,uint32_t m,unsigned f) { (void)p;(void)m;(void)f;return -1; }
static const uint8_t chain[32]={0x42,0x17};
int main(void)
{
    unsigned cases=0;
    const uint32_t allowed_causes[]={0,1,4,5};
    for (unsigned n=0;n<4;n++) for (unsigned core=0;core<=2;core+=2) {
        struct hw h;init(&h,core);h.r[FES_COLD_CAUSE]=allowed_causes[n];
        struct fes_nano_cold spl={0},bl31={0};struct fes_nano_cold_record r;
        assert(!fes_nano_cold_begin(&spl,ops(&h),chain,17,7,core));
        assert(!fes_nano_cold_finish(&spl,1,&r));
        assert(!fes_nano_cold_consume(&bl31,&r,ops(&h),chain,17));cases++;
    }
    for (unsigned bit=0;bit<32;bit++) if (bit!=0 && bit!=2) {
        struct hw h;init(&h,0);h.r[FES_COLD_CAUSE]=4U|(1U<<bit);struct fes_nano_cold s={0};
        assert(fes_nano_cold_begin(&s,ops(&h),chain,17,7,0)<0 && !h.writes);cases++;
    }
    /* Exact first TRWU8CBL tuple stays rejected even after correcting POR.
     * Its CPU status does not match either qualified primary inventory. */
    for (unsigned core=0;core<=2;core+=2) {
        struct hw h;init(&h,core);h.r[FES_COLD_CAUSE]=4;h.r[FES_COLD_PER0]=0xff7fbfff;
        h.r[FES_COLD_BRG]=0x4f;h.r[FES_COLD_CPUS]=0xf;struct fes_nano_cold s={0};
        assert(fes_nano_cold_begin(&s,ops(&h),chain,17,7,core)<0 && !h.writes);cases++;
    }
    for (unsigned kind=0;kind<8;kind++) {
        struct hw h;init(&h,0);h.r[FES_COLD_BOOT_KIND]=kind;h.r[FES_COLD_CAUSE]=0;
        struct fes_nano_cold s={0};
        int expected=kind==0 || kind==2 || kind==3 || kind==4;
        assert((fes_nano_cold_begin(&s,ops(&h),chain,17,7,0)==0)==expected && !h.writes);
        cases++;
    }
    {
        struct hw h;init(&h,0);struct fes_nano_cold s={0};struct fes_nano_cold_record record;
        assert(!fes_nano_cold_begin(&s,ops(&h),chain,17,7,0));
        h.r[FES_COLD_BOOT_KIND]=2; /* another allowed kind is still a changed boot scope */
        assert(fes_nano_cold_finish(&s,1,&record)<0 && !h.writes);cases++;
    }
    for (unsigned core=0;core<=2;core+=2) {
        struct hw h;init(&h,core);struct fes_nano_cold spl={0},bl31={0};struct fes_nano_cold_record record;
        assert(!fes_nano_cold_begin(&spl,ops(&h),chain,17,7,core));
        assert(!fes_nano_cold_finish(&spl,1,&record));assert(h.resets==2 && h.idle_reads>=2);
        assert(!fes_nano_cold_consume(&bl31,&record,ops(&h),chain,17));
        record.boot.owner++;assert(fes_nano_cold_owns(&bl31,17,7)==1); /* private copy */
        unsigned reads=h.idle_reads,writes=h.writes;
        struct fes_nano_boot boot={0};struct fes_nano_bridges b={0};
        struct fes_nano_boot_policy policy={.qualifications=15,.history=1,.fabric=1,.producers=FES_NANO_PRODUCERS_ALL};
        memcpy(policy.chain,chain,32);
        struct fes_nano_master_ops m={&bl31,fes_nano_cold_owns,fes_nano_cold_quiesce,fes_nano_cold_quiescent,per_read,per_assert,barrier,barrier};
        struct fes_nano_bridge_ops raw={&bl31,0,br_read,0,0,disabled,disabled};
        assert(!fes_nano_boot_accept(&boot,&b,&bl31.record.boot,&policy,m,raw));
        uint64_t reply[4];fes_nano_bridge_sip(&b,0,0,0,0,reply);
        assert(reply[0]==0 && (uint32_t)reply[1]==0xdf && reply[2]==17 && reply[3]==1);
        uint32_t quiet=0;assert(!fes_nano_cold_quiescent(&bl31,&quiet)&&quiet==FES_NANO_PRODUCERS_ALL);
        assert(h.idle_reads==reads && h.writes==writes);
        assert(fes_nano_cold_owns(&bl31,18,7)==0);
        assert(!fes_nano_cold_consume(&(struct fes_nano_cold){0},&spl.record,ops(&h),chain,17));
        h.r[FES_COLD_BRG]=0;assert(fes_nano_cold_quiesce(&bl31,FES_NANO_PRODUCERS_ALL)<0);
        h.r[FES_COLD_BRG]=15;assert(fes_nano_cold_owns(&bl31,17,7)!=1); /* no replay */
        cases++;
    }
    for (unsigned fault=0;fault<7;fault++) {
        struct hw h;init(&h,0);struct fes_nano_cold s={0};
        switch(fault) {
        case 0:h.r[FES_COLD_BOOT_KIND]=1;break;
        case 1:h.r[FES_COLD_CAUSE]=2;break;
        case 2:h.r[FES_COLD_PER0]&=~1U;break;
        case 3:h.r[FES_COLD_BRG]=0;break;
        case 4:h.r[FES_COLD_CPUS]=12;break;
        case 5:h.r[FES_COLD_RELEASE]=2;break;
        case 6:h.r[FES_COLD_CAUSE]=0x100;break;
        }
        assert(fes_nano_cold_begin(&s,ops(&h),chain,17,7,0)<0 && !h.writes);
        init(&h,0);assert(fes_nano_cold_begin(&s,ops(&h),chain,17,7,0)<0);cases++;
    }
    for (unsigned fault=1;fault<=8;fault++) {
        struct hw h;init(&h,0);struct fes_nano_cold s={0};struct fes_nano_cold_record r;
        memset(&r,0xa5,sizeof(r));struct fes_nano_cold_record before=r;
        assert(!fes_nano_cold_begin(&s,ops(&h),chain,17,7,0));h.fault=fault;
        if(fault==2)h.r[FES_COLD_SD_MODE]=1;
        assert(fes_nano_cold_finish(&s,fault==8?0:1,&r)<0 && !memcmp(&r,&before,sizeof(r)));
        if(fault<=4 || fault==8)assert(!h.resets);
        if(fault==1)assert(h.waits==1000);
        unsigned writes=h.writes;assert(fes_nano_cold_finish(&s,1,&r)<0 && writes==h.writes);cases++;
    }
    struct hw h;init(&h,0);struct fes_nano_cold s={0};struct fes_nano_cold_record r;
    assert(!fes_nano_cold_begin(&s,ops(&h),chain,17,7,0)&&!fes_nano_cold_finish(&s,1,&r));
    for(unsigned mutation=0;mutation<9;mutation++) {
        struct fes_nano_cold_record bad=r;struct fes_nano_cold target={0};
        switch(mutation) {
        case 0:bad.checksum^=1;break;case 1:bad.boot.chain[0]^=1;break;
        case 2:bad.boot.reserved=1;break;case 3:bad.flags=0;break;
        case 4:bad.sd_after=1;break;case 5:bad.last.per0&=~1U;break;
        case 6:bad.first.bridge=0;break;case 7:bad.boot.history=2;break;case 8:bad.size++;break;
        }
        if(mutation)bad.checksum=fes_nano_cold_checksum(&bad);
        assert(fes_nano_cold_consume(&target,&bad,ops(&h),chain,17)<0);cases++;
    }
    printf("PASS %u actual cold/PIO ownership cases; register model only\n",cases);
}
