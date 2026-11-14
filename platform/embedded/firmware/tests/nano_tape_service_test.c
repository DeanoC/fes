/* SPDX-License-Identifier: BSD-3-Clause */
/* Reuse the injected GP endpoint model; execute the real service and engine. */
#define main engine_fixture_main
#include "nano_tape_test.c"
#undef main
#include "nano_tape_service.h"

struct fixture {
    struct model tape; /* The existing endpoint callbacks use this first member. */
    struct fes_nano_tape_service service;
    uint8_t header[128],digest[48];
    uint64_t previous;
    unsigned mode,guards,headers,verifies,regressed;
};
static void put32(uint8_t *p,uint32_t n)
{for (unsigned i=0;i<4;i++) p[i]=(uint8_t)(n>>(8*i));}
static void put64(uint8_t *p,uint64_t n)
{put32(p,(uint32_t)n);put32(p+4,(uint32_t)(n>>32));}
static int service_guard(void *context,uint64_t owner,uint64_t generation)
{
    struct fixture *f=context;f->guards++;
    assert(owner==17 && generation==9 && !f->tape.active);
    if (f->mode==101 || (f->mode==102 && f->guards==3) ||
        (f->mode==103 && f->service.engine.complete)) return -1;
    if (f->mode==104 && f->guards==2) f->tape.tick=f->service.step_began+1000000;
    if (f->mode==105 && f->guards==2) f->tape.tick=0;
    if (f->mode==106 && f->service.engine.complete) f->tape.tick=f->service.step_began+1000000;
    return 0;
}
static int read_header(void *context,uint8_t header[128])
{
    struct fixture *f=context;f->headers++;
    assert(!f->tape.active);
    if (f->mode==107) return -1;
    memcpy(header,f->header,128);
    if (f->mode==108 && f->headers==2) header[63]^=1;
    return 0;
}
static int verify_source(void *context,uint32_t count,const uint8_t digest[48])
{
    struct fixture *f=context;f->verifies++;
    assert(!f->tape.active);
    if (count!=f->tape.size) return -1;
    if (f->mode==109) return -1;
    if (f->mode==114 && f->verifies==2) return -1;
    /* The cryptographic supplier is injected here; engine CRC uses real bytes. */
    if (memcmp(digest,f->digest,48)) return -1;
    if (f->mode==110) f->tape.tick=f->service.step_began+1000000;
    if (f->mode==111) f->tape.image[0]^=1;
    if (f->mode==112) f->tape.tick=700000; /* prologue consumes original budget */
    if (f->mode==115 && f->verifies==2) f->tape.tick=f->service.step_began+1000000;
    return 0;
}
static uint64_t service_now(void *context)
{
    struct fixture *f=context;
    if (f->mode==116 && f->service.engine.complete) return f->service.engine.last-1;
    if (f->mode==113 && f->service.engine.used && !f->regressed) {
        f->regressed=1;f->tape.tick=f->previous-1;return f->tape.tick;
    }
    if (f->mode==112 && f->service.engine.used) f->tape.tick+=1000;
    f->previous=now(context);return f->previous;
}
static unsigned accesses(const struct fixture *f)
{
    const struct model *m=&f->tape;
    return f->guards+f->headers+f->verifies+m->reads+m->writes+m->owns+m->images+m->begins+m->ends+m->aborts;
}
static void read_file(const char *path,uint8_t *data,unsigned size)
{
    FILE *file=fopen(path,"rb");assert(file);
    assert(fread(data,1,size,file)==size && fgetc(file)==EOF);assert(!fclose(file));
}
int main(int argc,char **argv)
{
    assert(argc==3 ||argc==6 ||argc==7);
    struct fixture f={0};f.mode=(unsigned)strtoul(argv[1],0,10);
    struct model *m=&f.tape;m->size=(uint32_t)strtoul(argv[2],0,10);
    assert(m->size && m->size<=sizeof(m->image));
    m->mode=f.mode<100 ? f.mode : 0;
    for (unsigned i=0;i<m->size;i++) m->image[i]=(uint8_t)(i*37+11);
    uint16_t words[]={0x4546,0x3153,1,0,4,1,0,0x803,0x2301,0x6745,0xab89,0xefcd,0x3210,0x7654,0xba98,0xfedc};
    memcpy(m->identity,words,sizeof(words));
    m->gpo=0x82000000;m->gpi=FES_COMPUTER_SIGNATURE|FES_COMPUTER_ACK_MASK;
    m->fetches=0x1234;m->state=FES_COMPUTER_MEDIA_STATE_EMPTY;
    memset(f.digest,0x5a,48);
    put32(f.header,FES_EMBEDDED_TAPE_MAGIC);put32(f.header+4,FES_EMBEDDED_TAPE_VERSION);
    put32(f.header+8,128);put32(f.header+12,m->size);put64(f.header+16,17);put64(f.header+24,9);
    put32(f.header+32,m->gpo);put32(f.header+36,crc(m->image,m->size));
    for (unsigned i=0;i<8;i++) {f.header[40+2*i]=(uint8_t)words[8+i];f.header[41+2*i]=(uint8_t)(words[8+i]>>8);}
    memcpy(f.header+56,f.digest,48);
    if (argc>=6) {read_file(argv[3],f.header,128);read_file(argv[4],m->image,m->size);read_file(argv[5],f.digest,48);}
    /* Exact header rejection, including every reserved/build byte. */
    if (f.mode>=120 &&f.mode<248) f.header[f.mode-120]^=1;
    struct fes_nano_tape_ops tape={&f,owns,begin,end,read_reg,write_reg,image,service_now,barrier};
    struct fes_nano_tape_service_ops io={&f,service_guard,read_header,verify_source};
    assert(!fes_nano_tape_service_init(&f.service,io,tape,17,9,words,1000000));
    uint64_t result[4];
    if (argc==7) {
        unsigned long long operation,owner,generation;
        while (scanf("%llu %llu %llu",&operation,&owner,&generation)==3) {
            fes_nano_tape_service_call(&f.service,operation,owner,generation,result);
            printf("REPLY %llu %llu %llu %llu\n",(unsigned long long)result[0],
                (unsigned long long)result[1],(unsigned long long)result[2],(unsigned long long)result[3]);
            fflush(stdout);
        }
        return 0;
    }
    fes_nano_tape_service_call(&f.service,1,17,9,result);
    assert(result[0]==FES_EMBEDDED_TAPE_REJECTED && !accesses(&f) && !f.service.used);
    fes_nano_tape_service_call(&f.service,0,18,9,result);
    assert(result[0]==FES_EMBEDDED_TAPE_REJECTED && !accesses(&f) && !f.service.used);
    fes_nano_tape_service_call(&f.service,0,17,10,result);
    assert(result[0]==FES_EMBEDDED_TAPE_REJECTED && !accesses(&f) && !f.service.used);
    fes_nano_tape_service_call(&f.service,0,17,9,result);
    if (result[0]==FES_EMBEDDED_TAPE_PENDING && f.mode==31) {
        unsigned before=accesses(&f);
        fes_nano_tape_service_call(&f.service,FES_EMBEDDED_TAPE_ABORT|(result[3]<<FES_EMBEDDED_TAPE_SEQUENCE_SHIFT),17,9,result);
        assert(result[0]==FES_EMBEDDED_TAPE_CANCELLED && before==accesses(&f));
    }
    if (f.mode==30) f.tape.tick+=1000000; /* outside a step, inside total budget */
    if (f.mode==32) f.header[31]^=1;
    if (f.mode==33) f.tape.tick=f.service.began+f.service.budget;
    while (result[0]==FES_EMBEDDED_TAPE_PENDING) {
        assert(!m->active && f.service.engine.busy && !f.service.engine.complete && result[2]==9);
        unsigned before=accesses(&f);
        uint64_t next=result[3],progress=result[1];
        fes_nano_tape_service_call(&f.service,FES_EMBEDDED_TAPE_STEP|((next-1)<<FES_EMBEDDED_TAPE_SEQUENCE_SHIFT),17,9,result);
        assert(result[0]==FES_EMBEDDED_TAPE_REJECTED && before==accesses(&f));
        fes_nano_tape_service_call(&f.service,FES_EMBEDDED_TAPE_STEP|(next<<FES_EMBEDDED_TAPE_SEQUENCE_SHIFT),17,9,result);
        if (result[0]==FES_EMBEDDED_TAPE_PENDING) assert(result[1]>progress && result[3]==next+1);
    }
    assert(!m->active &&f.service.used);
    if (!f.mode ||f.mode==29 ||f.mode==30) {
        assert(!result[0] &&f.service.complete && !f.service.failed &&m->commits==1);
        assert(result[1]==((uint64_t)FES_EMBEDDED_TAPE_TAG<<32|m->size) &&result[2]==9);
        assert((uint32_t)result[3]==m->gpo &&result[3]>>32==crc(m->image,m->size));
        assert(m->data_bytes==m->size && !memcmp(m->image,m->received,m->size));
    } else {
        assert(result[0] &&f.service.failed && !f.service.complete);
        assert(result[1]==(uint64_t)FES_EMBEDDED_TAPE_TAG<<32 && !result[2] && !result[3]);
        if (f.mode==104 ||f.mode==105 ||f.mode==106 ||f.mode==110 ||f.mode==112 ||f.mode==113 ||f.mode==115 ||f.mode==116)
            assert(result[0]==FES_EMBEDDED_TAPE_LIMIT);
        if ((f.mode>=120 && (f.mode<156 ||f.mode>159)) ||f.mode==101 ||f.mode==102 ||f.mode==104 ||f.mode==105 ||f.mode==107 ||f.mode==108 ||f.mode==109 ||f.mode==110 ||f.mode==113)
            assert(!m->writes && !m->reads && !m->begins);
        if (f.mode>=156 &&f.mode<=159) assert(!m->commits && !m->data_bytes && m->state==FES_COMPUTER_MEDIA_STATE_EMPTY);
    }
    printf("PASS tape service mode=%u status=%llu tagbytes=%llu generation=%llu gpocrc=%llu writes=%u reads=%u\n",
        f.mode,(unsigned long long)result[0],(unsigned long long)result[1],(unsigned long long)result[2],
        (unsigned long long)result[3],m->writes,m->reads);
    unsigned before=accesses(&f);
    fes_nano_tape_service_call(&f.service,0,17,9,result);
    assert(result[0]==FES_EMBEDDED_TAPE_REJECTED && !result[2] && !result[3] &&before==accesses(&f));
    return 0;
}
