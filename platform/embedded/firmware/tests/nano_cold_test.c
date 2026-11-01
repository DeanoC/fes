/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_cold.h"
#include "nano_bridge_sip.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>

struct hw {
    uint32_t r[FES_COLD_REGISTER_COUNT];
    unsigned fault, writes, resets, waits, idle_reads, fail_read, change_reg, fail_write;
    uint32_t change_bit;
};
static int read_reg(void *p,unsigned reg,uint32_t *v)
{
    struct hw *h=p;assert(reg<FES_COLD_REGISTER_COUNT);
    if (h->fail_read==reg+1)return -1;
    if (h->fault==9 && h->resets==2 && reg==FES_COLD_CAUSE)return -1;
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
    if (h->fail_write==reg+1)return -1;
    if (reg==FES_COLD_PER0) {
        assert(h->idle_reads>=2 && !(h->r[FES_COLD_SD_STATE]&FES_NANO_COLD_SD_BUSY) && !(h->r[FES_COLD_SD_CLOCK]&4));
        h->resets++;
        assert(v==(h->resets==1 ? FES_NANO_MASTER_FUNCTION:FES_NANO_MASTER_RESET));
        if (h->fault==5)return -1;
        if (h->fault==6 && h->resets==2)h->r[reg]&=~FES_NANO_COLD_SD_ECC_RESET;
        else h->r[reg]|=v;
        if (h->fault==7)h->r[FES_COLD_CPUS]&=~2U;
        if (h->resets==2 && h->change_reg)h->r[h->change_reg-1]^=h->change_bit;
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
    h->r[FES_COLD_CPU0_POWER+core]=0x90000021;
    h->r[FES_COLD_PCH_REQUEST]=h->r[FES_COLD_PCH_STATUS]=0x20U|(1U<<(core+1));
    h->r[FES_COLD_BOOT_CONFIG]=0x0070021eU|(core<<10);
}
static int per_read(void *p,uint32_t *v) { struct fes_nano_cold *s=p;return s->io.read(s->io.context,FES_COLD_PER0,v); }
static int per_assert(void *p,uint32_t bits) { struct fes_nano_cold *s=p;return s->io.write(s->io.context,FES_COLD_PER0,bits); }
static int br_read(void *p,uint32_t *v) { struct fes_nano_cold *s=p;return s->io.read(s->io.context,FES_COLD_BRG,v); }
static int disabled(void *p,uint32_t m,unsigned f) { (void)p;(void)m;(void)f;return -1; }
static const uint8_t chain[32]={0x42,0x17};
/* A spare can change during SD drain or after a valid seal. Each stage must
 * reject independently; a restored register cannot resurrect its owner. */
static unsigned parking_failures(unsigned core,unsigned reg,uint32_t bit)
{
    for (unsigned stage=0;stage<5;stage++) {
        struct hw h;init(&h,core);
        struct fes_nano_cold spl={0},bl31={0};struct fes_nano_cold_record r;
        memset(&r,0xa5,sizeof(r));struct fes_nano_cold_record untouched=r;
        if (!stage) {
            h.r[reg]^=bit;
            assert(fes_nano_cold_begin(&spl,ops(&h),chain,17,7,core)<0 && !h.writes);
            continue;
        }
        assert(!fes_nano_cold_begin(&spl,ops(&h),chain,17,7,core));
        if (stage==1 || stage==2) {
            if(stage==1)h.r[reg]^=bit;
            else {h.change_reg=reg+1;h.change_bit=bit;}
            assert(fes_nano_cold_finish(&spl,1,&r)<0 && !memcmp(&r,&untouched,sizeof(r)));
            assert(stage==1 ? !h.writes : h.resets==2);
            h.r[reg]^=bit;
            unsigned writes=h.writes;
            assert(fes_nano_cold_finish(&spl,1,&r)<0 && h.writes==writes);
        } else {
            assert(!fes_nano_cold_finish(&spl,1,&r));
            if(stage==3)assert(!fes_nano_cold_consume(&bl31,&r,ops(&h),chain,17));
            unsigned writes=h.writes;h.r[reg]^=bit;
            if(stage==3)assert(fes_nano_cold_owns(&bl31,17,7)!=1);
            else assert(fes_nano_cold_consume(&bl31,&r,ops(&h),chain,17)<0);
            h.r[reg]^=bit;
            assert(fes_nano_cold_owns(&bl31,17,7)!=1 && h.writes==writes);
            assert(fes_nano_cold_consume(&bl31,&r,ops(&h),chain,17)<0);
        }
    }
    return 5;
}
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
    /* The old six-word tuple alone still cannot prove initial parking. */
    for (unsigned core=0;core<=2;core+=2) {
        struct hw h;init(&h,core);h.r[FES_COLD_CAUSE]=4;h.r[FES_COLD_PER0]=0xff7fbfff;
        h.r[FES_COLD_BRG]=0x4f;h.r[FES_COLD_CPUS]=0xf;struct fes_nano_cold s={0};
        h.r[FES_COLD_PCH_REQUEST]=h.r[FES_COLD_PCH_STATUS]=h.r[FES_COLD_BOOT_CONFIG]=0;
        assert(fes_nano_cold_begin(&s,ops(&h),chain,17,7,core)<0 && !h.writes);cases++;
    }
    /* Actual enriched EL3 inventory, independent of the primary reset bit.
     * Non-CPU PCH bits and the running primary's power word are observations,
     * not invented CPU-off encodings or a single magic tuple. */
    for (unsigned core=0;core<=2;core+=2) for(unsigned bit=0;bit<2;bit++) {
        struct hw h;init(&h,core);h.r[FES_COLD_CAUSE]=4;h.r[FES_COLD_PER0]=0xff7fbfff;
        h.r[FES_COLD_BRG]=0x4f;h.r[FES_COLD_CPUS]|=1U<<core;
        struct fes_nano_cold spl={0},bl31={0};struct fes_nano_cold_record r;
        assert(!fes_nano_cold_begin(&spl,ops(&h),chain,17,7,core));
        /* Actual v13 pre-drain value: SD controller/PHY released, ECC held.
         * Keep this literal independent of the policy's reset mask. */
        h.r[FES_COLD_PER0]=0xff7fbf3f;
        h.r[FES_COLD_BRG]=15; /* observed vendor setup clears an unrelated bit */
        assert(h.r[FES_COLD_PER0]&FES_NANO_COLD_SD_ECC_RESET);
        if(bit) {
            h.r[FES_COLD_CPUS]^=1U<<core;h.r[FES_COLD_CPU0_POWER+core]^=0x81234567U;
            h.r[FES_COLD_PCH_REQUEST]^=0x21U;h.r[FES_COLD_PCH_STATUS]^=0x21U;
        }
        assert(!fes_nano_cold_finish(&spl,1,&r));
        assert(spl.finish_stage==FES_COLD_FINISH_COMPLETE && h.idle_reads>=2 && h.resets==2);
        assert(!fes_nano_cold_consume(&bl31,&r,ops(&h),chain,17));cases++;
    }
    for (unsigned core=0;core<=2;core+=2) {
        cases+=parking_failures(core,FES_COLD_PER0,FES_NANO_COLD_SD_ECC_RESET);
        for(unsigned spare=0;spare<4;spare++) if(spare!=core) {
            for(unsigned bit=0;bit<32;bit++)cases+=parking_failures(core,FES_COLD_CPU0_POWER+spare,1U<<bit);
            cases+=parking_failures(core,FES_COLD_CPUS,1U<<spare);
        }
        for(unsigned bit=0;bit<4;bit++) {
            cases+=parking_failures(core,FES_COLD_PCH_REQUEST,1U<<(bit+1));
            cases+=parking_failures(core,FES_COLD_PCH_STATUS,1U<<(bit+1));
            cases+=parking_failures(core,FES_COLD_RELEASE,1U<<bit);
        }
        cases+=parking_failures(core,FES_COLD_BOOT_CONFIG,1U<<10);
        cases+=parking_failures(core,FES_COLD_BOOT_CONFIG,1U<<11);
    }
    for(unsigned reg=FES_COLD_CPU0_POWER;reg<=FES_COLD_BOOT_CONFIG;reg++) for(unsigned stage=0;stage<4;stage++) {
        struct hw h;init(&h,0);struct fes_nano_cold s={0};struct fes_nano_cold_record r;
        if(stage)assert(!fes_nano_cold_begin(&s,ops(&h),chain,17,7,0));
        if(stage>=2)assert(!fes_nano_cold_finish(&s,1,&r));
        h.fail_read=reg+1;
        if(!stage)assert(fes_nano_cold_begin(&s,ops(&h),chain,17,7,0)<0 && !h.writes);
        else if(stage==1)assert(fes_nano_cold_finish(&s,1,&r)<0 && !h.writes);
        else if(stage==2)assert(fes_nano_cold_owns(&s,17,7)!=1);
        else assert(fes_nano_cold_consume(&(struct fes_nano_cold){0},&r,ops(&h),chain,17)<0);
        cases++;
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
        assert(spl.finish_stage==FES_COLD_FINISH_COMPLETE && spl.sd_clock_before==7 &&
               !(spl.record.sd_clock&4) && (spl.reset_readback&FES_NANO_MASTER_RESET)==FES_NANO_MASTER_RESET);
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
        static const unsigned stages[]={0,FES_COLD_FINISH_IDLE_BEFORE,FES_COLD_FINISH_DMA,
            FES_COLD_FINISH_CLOCK_CHECK,FES_COLD_FINISH_MODE_READ,FES_COLD_FINISH_FUNCTION_RESET,
            FES_COLD_FINISH_ALL_RESET,FES_COLD_FINISH_STABLE_LAST,FES_COLD_FINISH_ENTRY};
        assert(s.finish_stage==stages[fault]);
        if(fault<=4 || fault==8)assert(!h.resets);
        if(fault==1)assert(h.waits==1000);
        unsigned writes=h.writes;h.fault=0;h.r[FES_COLD_SD_MODE]=0;
        assert(fes_nano_cold_finish(&s,1,&r)<0 && writes==h.writes && s.finish_stage==stages[fault]);cases++;
    }
    /* Stage reporting must neither probe a stopped device again nor replace
     * the first failed stage after its injected fault is removed. */
    for(unsigned fault=0;fault<9;fault++) {
        struct hw h;init(&h,0);struct fes_nano_cold s={0};struct fes_nano_cold_record r;
        memset(&r,0xa5,sizeof(r));struct fes_nano_cold_record untouched=r;
        assert(!fes_nano_cold_begin(&s,ops(&h),chain,17,7,0));
        static const unsigned stages[]={FES_COLD_FINISH_SAMPLE,FES_COLD_FINISH_INITIAL,
            FES_COLD_FINISH_STABLE,FES_COLD_FINISH_SD_RESET,FES_COLD_FINISH_INT,
            FES_COLD_FINISH_SIGNAL,FES_COLD_FINISH_CLOCK_READ,FES_COLD_FINISH_CLOCK_STOP,
            FES_COLD_FINISH_SAMPLE_LAST};
        switch(fault) {
        case 0:h.fail_read=FES_COLD_PCH_STATUS+1;break;
        case 1:h.r[FES_COLD_PCH_STATUS]|=4;break;
        case 2:h.r[FES_COLD_BOOT_KIND]=2;break;
        case 3:h.r[FES_COLD_PER0]|=FES_NANO_COLD_SD_RESET;break;
        case 4:h.fail_write=FES_COLD_SD_INT+1;break;
        case 5:h.fail_write=FES_COLD_SD_SIGNAL+1;break;
        case 6:h.fail_read=FES_COLD_SD_CLOCK+1;break;
        case 7:h.fail_write=FES_COLD_SD_CLOCK+1;break;
        case 8:h.fault=9;break;
        }
        /* The last-sample read fails only after both reset assertions. */
        assert(fes_nano_cold_finish(&s,1,&r)<0 && s.finish_stage==stages[fault]);
        assert(!memcmp(&r,&untouched,sizeof(r)));
        if(fault<4)assert(!h.writes);
        else if(fault<8)assert(!h.resets);
        unsigned writes=h.writes,reads=h.idle_reads;
        h.fail_read=h.fail_write=h.fault=0;
        assert(fes_nano_cold_finish(&s,1,&r)<0 && writes==h.writes && reads==h.idle_reads &&
               s.finish_stage==stages[fault]);cases++;
    }
    struct hw h;init(&h,0);struct fes_nano_cold s={0};struct fes_nano_cold_record r;
    assert(!fes_nano_cold_begin(&s,ops(&h),chain,17,7,0)&&!fes_nano_cold_finish(&s,1,&r));
    for(unsigned mutation=0;mutation<17;mutation++) {
        struct fes_nano_cold_record bad=r;struct fes_nano_cold target={0};
        switch(mutation) {
        case 0:bad.checksum^=1;break;case 1:bad.boot.chain[0]^=1;break;
        case 2:bad.boot.reserved=1;break;case 3:bad.flags=0;break;
        case 4:bad.sd_after=1;break;case 5:bad.last.per0&=~1U;break;
        case 6:bad.first.bridge=0;break;case 7:bad.boot.history=2;break;case 8:bad.size++;break;
        case 9:bad.version=1;break;case 10:bad.magic=0x464e4331;break;
        case 11:bad.first.power[1]=0x80000021;break;case 12:bad.last.power[3]=0x90000021;break;
        case 13:bad.last.pch_request|=4;break;case 14:bad.last.pch_status|=4;break;
        case 15:bad.first.boot_config^=0x800;break;case 16:bad.boot.epoch=0;break;
        }
        if(mutation)bad.checksum=fes_nano_cold_checksum(&bad);
        assert(fes_nano_cold_consume(&target,&bad,ops(&h),chain,17)<0);cases++;
    }
    printf("PASS %u actual cold/PIO ownership cases; register model only\n",cases);
}
