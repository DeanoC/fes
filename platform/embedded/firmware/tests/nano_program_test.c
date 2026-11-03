/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_program.h"
#include "nano_configuration.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>

struct fixture {
    struct fes_nano_program program;
    struct fes_nano_configuration configuration;
    struct fes_nano_bridges bridges;
    uint8_t wire[FES_NANO_PROGRAM_HEADER_BYTES],context[32];
    unsigned valid,reset,reads,guards,read_failure,guard_failure,changing;
    unsigned checks,completions;
};
static void put32(uint8_t *p,uint32_t v)
{ for(unsigned i=0;i<4;i++)p[i]=(uint8_t)(v>>(8*i)); }
static void put64(uint8_t *p,uint64_t v)
{ for(unsigned i=0;i<8;i++)p[i]=(uint8_t)(v>>(8*i)); }
static int owns(void *p,uint64_t owner,uint64_t epoch)
{ return ((struct fixture *)p)->valid && owner==17 && epoch==9; }
static int reset(void *p,uint32_t *v) { *v=((struct fixture *)p)->reset;return 0; }
static int masters(void *p) { return ((struct fixture *)p)->valid; }
static int stop(void *p) { (void)p;return 0; }
static int disable(void *p,uint32_t mask,unsigned fabric)
{ assert(mask==2 && fabric==FES_NANO_FABRIC_ABSENT);((struct fixture *)p)->reset|=mask;return 0; }
static int enable(void *p,uint32_t mask,unsigned fabric)
{ assert(mask==2 && fabric==FES_NANO_FABRIC_ABSENT);((struct fixture *)p)->reset&=~mask;return 0; }
static int guard(void *p,uint64_t owner,uint64_t generation)
{
    struct fixture *f=p;f->guards++;
    return !f->valid || f->reset!=15 || owner!=17 || generation!=f->bridges.state.generation ||
           f->guards==f->guard_failure ? -1 : 0;
}
static int read_header(void *p,uint8_t result[FES_NANO_PROGRAM_HEADER_BYTES])
{
    struct fixture *f=p;f->reads++;
    if(f->reads==f->read_failure)return -1;
    memcpy(result,f->wire,sizeof(f->wire));
    if(f->changing && f->reads==2)result[120]^=1;
    return 0;
}
static void header(struct fixture *f,uint32_t bytes,unsigned seed)
{
    uint8_t *h=f->wire;memset(h,0,sizeof(f->wire));
    put32(h,FES_NANO_PROGRAM_MAGIC);put32(h+4,1);put32(h+8,sizeof(f->wire));
    put32(h+12,bytes);put32(h+16,64);put32(h+20,(bytes-1)/64+1);
    put64(h+24,17);put64(h+32,f->bridges.state.generation);
    memcpy(h+40,f->context,32);memset(h+72,seed,32);
    for(unsigned i=0;i<16;i++)h[104+i]=(uint8_t)(seed+i);
    memset(h+120,seed+1,32);memset(h+152,seed+2,48);
    for(unsigned i=0;i<(bytes-1)/64+1;i++)memset(h+200+48*i,seed+3+i,48);
}
static int bind(void *p,uint64_t owner,uint64_t generation,uint32_t *bytes)
{
    struct fixture *f=p;
    if(fes_nano_program_capture(&f->program,owner,generation))return -1;
    *bytes=f->program.bytes;return 0;
}
static int check(void *p,uint64_t address,uint32_t bytes,unsigned whole)
{
    struct fixture *f=p;f->checks++;
    assert(address>=0x94800000 && address-0x94800000<512);
    return fes_nano_program_expected(&f->program,(uint32_t)(address-0x94800000),bytes,whole) ? 0 : -1;
}
static int complete(void *p,uint64_t owner,uint64_t generation,unsigned rearmed)
{
    struct fixture *f=p;
    assert(owner==17 && generation==f->program.generation && rearmed==!!f->completions);
    f->completions++;return 0;
}
static void init(struct fixture *f)
{
    memset(f,0,sizeof(*f));f->valid=1;f->reset=15;f->context[0]=0x42;
    struct fes_nano_bridge_ops bridge={f,owns,reset,masters,stop,disable,enable};
    assert(!fes_nano_bridges_init(&f->bridges,bridge,17,9,FES_NANO_FABRIC_ABSENT,1));
    struct fes_nano_program_ops program={f,guard,read_header};
    assert(!fes_nano_program_init(&f->program,program,f->context,512,64,1));
    struct fes_nano_configuration_ops configuration={.context=f,.check=check,.complete=complete,.bind=bind};
    assert(!fes_nano_configuration_init(&f->configuration,&f->bridges,configuration,0x94800000,512,64,1));
    header(f,128,1);
}
static void transfer(struct fixture *f,unsigned rearm)
{
    struct fes_nano_configuration *c=&f->configuration;uint64_t r[4];
    if(rearm)fes_nano_configuration_rearm(c,17,f->bridges.state.generation,0,r);
    else fes_nano_configuration_prepare(c,17,f->bridges.state.generation,0,r);
    assert(!r[0] && (uint32_t)r[1]==(FES_NANO_PROGRAM_BOUND_REPLY|f->program.bytes) && c->bytes==f->program.bytes);
    unsigned reads=f->reads;
    memset(f->wire,0xff,sizeof(f->wire)); /* The secure copy is the authority. */
    assert(!fes_nano_configuration_before(c,FES_NANO_CONFIG_START,0,0,0));
    memset(r,0,sizeof(r));fes_nano_configuration_after(c,FES_NANO_CONFIG_START,r);assert(!r[0]);
    for(uint32_t offset=0;offset<c->bytes;) {
        uint32_t bytes=c->bytes-offset;if(bytes>64)bytes=64;
        assert(!fes_nano_configuration_before(c,FES_NANO_CONFIG_WRITE,0x94800000+offset,bytes,1));
        memset(r,0,sizeof(r));fes_nano_configuration_after(c,FES_NANO_CONFIG_WRITE,r);assert(!r[0]);
        assert(!fes_nano_configuration_before(c,FES_NANO_CONFIG_COMPLETED,0,0,1));
        r[0]=0;r[1]=0x14800000+offset;r[2]=r[3]=0;
        fes_nano_configuration_after(c,FES_NANO_CONFIG_COMPLETED,r);assert(!r[0]);offset+=bytes;
    }
    assert(!fes_nano_configuration_before(c,FES_NANO_CONFIG_ISDONE,0,0,1));
    memset(r,0,sizeof(r));fes_nano_configuration_after(c,FES_NANO_CONFIG_ISDONE,r);
    assert(!r[0] && c->finished && !c->failed && !c->pending && c->completed==c->bytes && f->reads==reads);
    struct fes_nano_bridge_snapshot open;struct fes_nano_bridge_closure closed;
    assert(!fes_nano_bridges_open(&f->bridges,17,f->bridges.state.generation,2,&open));
    assert(!fes_nano_bridges_close(&f->bridges,17,open.generation,0,1,&closed));
}
int main(int argc,char **argv)
{
    if(argc==2) {
        struct fixture f;init(&f);FILE *in=fopen(argv[1],"rb");assert(in);
        f.program.chunk=0xfffc0; /* The paired native fixture's qualified block size. */
        f.configuration.chunk=0xfffc0;
        assert(fread(f.wire,1,sizeof(f.wire),in)==sizeof(f.wire) && fgetc(in)==EOF);fclose(in);
        /* The paired Go fixture uses this independent context and ownership. */
        uint64_t reply[4];fes_nano_configuration_prepare(&f.configuration,17,1,0,reply);
        assert(!reply[0] && (uint32_t)reply[1]==(FES_NANO_PROGRAM_BOUND_REPLY|f.program.bytes));
        printf("%u %u %u ",f.program.bytes,f.program.chunks,(unsigned)(reply[1]>>31)&1U);
        uint32_t words[4];assert(!fes_nano_program_build(&f.program,words));
        for(unsigned i=0;i<4;i++)printf("%08x ",words[i]);
        for(unsigned i=0;i<32;i++)printf("%02x",f.program.header[72+i]);
        putchar('\n');return 0;
    }
    unsigned cases=0;
    for(unsigned mode=0;mode<32;mode++) {
        struct fixture f;init(&f);
        uint32_t offset=mode<7 ? mode*4 : 0;
        if(mode<7)f.wire[offset]^=1;
        if(mode==7)put64(f.wire+24,18);
        if(mode==8)put64(f.wire+32,2);
        if(mode==9)f.wire[40]^=1;
        if(mode==10)memset(f.wire+72,0,32);
        if(mode==11)memset(f.wire+104,0,16);
        if(mode==12)memset(f.wire+120,0,32);
        if(mode==13)memset(f.wire+152,0,48);
        if(mode==14)memset(f.wire+200,0,48);
        if(mode==15)f.wire[632]=1;
        if(mode==16)f.wire[296]=1; /* Unused third digest. */
        if(mode==17)put32(f.wire+12,516);
        if(mode==18)put32(f.wire+12,0);
        if(mode==19)put32(f.wire+16,0);
        if(mode==20)put32(f.wire+20,10);
        if(mode==21)f.changing=1;
        if(mode==22)f.read_failure=1;
        if(mode==23)f.read_failure=2;
        if(mode>=24 && mode<28)f.guard_failure=mode-23;
        if(mode==28)f.valid=0;
        if(mode==29)f.reset=13;
        if(mode==30)put32(f.wire+12,127);
        if(mode==31)put32(f.wire+20,0);
        uint64_t r[4];fes_nano_configuration_prepare(&f.configuration,17,1,0,r);
        assert(r[0] && f.configuration.failed && f.bridges.failed && !f.program.bound);
        unsigned reads=f.reads;header(&f,128,1);
        fes_nano_configuration_prepare(&f.configuration,17,1,0,r);
        assert(r[0] && f.reads==reads && fes_nano_configuration_before(&f.configuration,FES_NANO_CONFIG_START,0,0,0));
        cases++;
    }
    struct fixture f;init(&f);transfer(&f,0);cases++;
    const unsigned sizes[]={192,64,160};
    for(unsigned i=0;i<3;i++) {
        header(&f,sizes[i],i+7);transfer(&f,1);
        assert(f.configuration.bytes==sizes[i] && f.completions==i+2);
        assert(!fes_nano_program_expected(&f.program,1,64,0));
        assert(!fes_nano_program_expected(&f.program,0,sizes[i]-4,1));
        unsigned reads=f.reads;uint64_t r[4];
        fes_nano_configuration_rearm(&f.configuration,17,f.configuration.generation,0,r);
        assert(r[0] && f.reads==reads);cases++;
    }
    printf("PASS %u per-load descriptor and actual configuration/bridge engine cases; injected digest checks, no hardware\n",cases);
    return 0;
}
