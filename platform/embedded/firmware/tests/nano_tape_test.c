/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_tape.h"
#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

struct model {
    struct fes_nano_tape engine;
    uint16_t identity[16];
    uint8_t image[FES_DE25_COMPUTER_ZX81_TAPE_MAX_BYTES],received[FES_DE25_COMPUTER_ZX81_TAPE_MAX_BYTES];
    uint64_t tick;
    uint32_t gpo,gpi,fetches,size,expected_crc,chunk_offset,chunk_size,data_bytes,state;
    unsigned mode,active,phase,offset,begins,ends,aborts,reads,writes,owns,images,commits;
};
static uint32_t crc(const uint8_t *p,unsigned count)
{
    uint32_t value=UINT32_MAX;
    for (unsigned i=0;i<count;i++) {
        value^=p[i];
        for (unsigned bit=0;bit<8;bit++) value=(value>>1)^((value&1U) ? UINT32_C(0xedb88320) : 0);
    }
    return ~value;
}
static int owns(void *context,uint64_t owner,uint64_t generation)
{
    struct model *m=context;m->owns++;
    assert(m->active && owner==17 && generation==9);
    return !(m->mode==1 && m->writes>=48);
}
static int begin(void *context,uint64_t owner,uint64_t generation,unsigned phase,uint32_t offset)
{
    struct model *m=context;assert(!m->active && owner==17 && generation==9);
    if (m->mode==13 && phase==FES_TAPE_CHUNK) return -1;
    assert((phase==FES_TAPE_PREPARE && !offset && !m->begins) ||
           (phase==FES_TAPE_CHUNK && offset==m->data_bytes) ||
           (phase==FES_TAPE_FINISH && offset==m->size));
    m->begins++;m->active=1;m->phase=phase;m->offset=offset;
    if (m->mode==25) m->tick=1000001;
    return 0;
}
static int end(void *context,uint64_t owner,uint64_t generation,unsigned phase,uint32_t offset,unsigned success)
{
    struct model *m=context;
    assert(m->active && owner==17 && generation==9 && m->phase==phase && m->offset==offset);
    m->active=0;
    if (!success) {m->aborts++;return 0;}
    m->ends++;
    if (m->mode==24 && phase==FES_TAPE_FINISH) m->tick=1000001;
    return m->mode==12 && phase==FES_TAPE_CHUNK ? -1 : 0;
}
static int read_reg(void *context,uint32_t offset,uint32_t *v)
{
    struct model *m=context;m->reads++;
    assert(m->active);
    if (m->mode==2 && m->writes>=48) return -1;
    switch (offset) {
    case FES_DE25_COMPUTER_ID_OFFSET:*v=FES_DE25_COMPUTER_IDENTITY;break;
    case FES_DE25_COMPUTER_VERSION_OFFSET:*v=FES_DE25_COMPUTER_VERSION;break;
    case FES_DE25_COMPUTER_STATUS_OFFSET:
        *v=FES_DE25_COMPUTER_STATUS_READY;
        if (!!(m->gpo & FES_COMPUTER_REQUEST_MASK)!=!!(m->gpi & FES_COMPUTER_ACK_MASK)) *v|=FES_DE25_COMPUTER_STATUS_BUSY;
        if (m->mode==16 && m->writes>=48) *v^=FES_DE25_COMPUTER_STATUS_BUSY;
        break;
    case FES_DE25_COMPUTER_GPO_OFFSET:*v=m->gpo;
        if (m->mode==18 && m->writes>=48) *v^=1U<<16;
        break;
    case FES_DE25_COMPUTER_GPI_OFFSET:*v=m->gpi;
        if (m->mode==20 && m->writes>=48) *v|=1U<<16;
        break;
    case FES_DE25_COMPUTER_FAULT_OFFSET:*v=m->mode==21 && m->writes>=48;break;
    case FES_DE25_COMPUTER_FETCH_COUNT_OFFSET:*v=m->fetches;break;
    case FES_DE25_COMPUTER_FETCH_ADDRESS_OFFSET:*v=m->mode==23 ? 65536 : 0x4000;break;
    default:assert(0);
    }
    return 0;
}
static int write_reg(void *context,uint32_t offset,uint32_t word)
{
    struct model *m=context;assert(m->active && offset==FES_DE25_COMPUTER_GPO_OFFSET);
    assert((word^m->gpo)&FES_COMPUTER_REQUEST_MASK);
    unsigned opcode=(word>>24)&127U,index=(word>>16)&255U;
    uint16_t argument=(uint16_t)word,response=0;
    assert(opcode!=FES_COMPUTER_OPCODE_EXECUTION); /* Never Hold/Run or reset. */
    m->writes++;
    if (m->mode==3 && m->writes>=48) return 0; /* Lost write, no replay. */
    m->gpo=word;
    if (opcode==FES_COMPUTER_OPCODE_IDENTITY) {
        assert(index<16);response=m->identity[index];
        if (m->mode==7 && index==8) response^=1;
        if (m->mode==19 && m->commits && index==15) response^=1;
    } else if (opcode==FES_COMPUTER_OPCODE_MEDIA_INFO) {
        const uint16_t info[]={1,0,16375,0,512,(uint16_t)m->state};
        assert(index<6);response=info[index];
        if (m->mode==8 && index==4) response++;
    } else if (opcode==FES_COMPUTER_OPCODE_MEDIA_BEGIN) {
        assert(index<4);
        if (index==0) assert(argument==m->size);
        if (index==1) assert(argument==0);
        if (index==2) m->expected_crc=argument;
        if (index==3) {m->expected_crc|=(uint32_t)argument<<16;m->state=FES_COMPUTER_MEDIA_STATE_LOADING;}
    } else if (opcode==FES_COMPUTER_OPCODE_MEDIA_CHUNK) {
        assert(index<3);
        if (index==0) m->chunk_offset=argument;
        if (index==1) m->chunk_offset|=(uint32_t)argument<<16;
        if (index==2) {m->chunk_size=argument;assert(m->chunk_offset==m->data_bytes && argument && argument<=512);}
    } else if (opcode==FES_COMPUTER_OPCODE_MEDIA_DATA) {
        unsigned pos=m->chunk_offset+2*index;
        assert(pos==m->data_bytes && 2*index<m->chunk_size && pos<m->size);
        m->received[pos]=(uint8_t)argument;m->data_bytes++;
        if (2*index+1<m->chunk_size) {m->received[pos+1]=(uint8_t)(argument>>8);m->data_bytes++;}
        else assert(!(argument>>8));
        if (m->mode==11) m->fetches++;
    } else if (opcode==FES_COMPUTER_OPCODE_MEDIA_COMMIT) {
        assert(index==0 && !argument && m->data_bytes==m->size && m->expected_crc==crc(m->received,m->size));
        m->commits++;m->state=FES_COMPUTER_MEDIA_STATE_READY;
    } else assert(0);
    if (m->mode==5 && m->writes>=48) return 0; /* Lost ACK. */
    m->gpi=FES_COMPUTER_SIGNATURE|((word & FES_COMPUTER_REQUEST_MASK) ? FES_COMPUTER_ACK_MASK : 0)|response;
    if (m->mode==6 && m->writes>=48) m->gpi|=FES_COMPUTER_ERROR_MASK;
    if (m->mode==26 && opcode==FES_COMPUTER_OPCODE_MEDIA_DATA) m->gpi|=1;
    return m->mode==4 && m->writes>=48 ? -1 : 0; /* Issued, returned error. */
}
static int image(void *context,uint32_t offset,uint8_t *out,uint32_t count)
{
    struct model *m=context;m->images++;
    assert(m->active && offset<=m->size && count<=m->size-offset && count<=512);
    memcpy(out,m->image+offset,count);
    if (m->mode==10 && m->phase==FES_TAPE_CHUNK) out[0]^=1;
    if (m->mode==17 && m->phase==FES_TAPE_CHUNK) return -1;
    return 0;
}
static uint64_t now(void *context)
{
    struct model *m=context;
    if (m->mode==14 && m->writes>=48) return 1000001;
    if (m->mode==15 && m->writes>=48) return 0;
    return ++m->tick;
}
static void barrier(void *context) {(void)context;}
static void rejected_arguments(struct model *m,struct fes_nano_tape_ops ops,const uint16_t words[16])
{
    for (unsigned variant=0;variant<14;variant++) {
        struct fes_nano_tape test={0};struct fes_nano_tape_ops missing=ops;
        uint16_t invalid[16];memcpy(invalid,words,sizeof(invalid));
        switch (variant) {
        case 0:missing.owns=0;break;
        case 1:missing.begin=0;break;
        case 2:missing.end=0;break;
        case 3:missing.read32=0;break;
        case 4:missing.write32=0;break;
        case 5:missing.image=0;break;
        case 6:missing.now=0;break;
        case 7:missing.barrier=0;break;
        case 8:invalid[0]^=1;break;
        case 9:invalid[6]=1;break; /* no ABI discovery or downgrade */
        case 10:memset(invalid+8,0,16);break;
        default:break;
        }
        assert(fes_nano_tape_init(&test,missing,variant==11 ? 0 : 17,9,invalid,variant==12 ? 0 : variant==13 ? 1000000001U : 1000000));
        assert(test.failed && !test.used && !m->owns && !m->reads && !m->writes && !m->begins);
        assert(fes_nano_tape_init(&test,ops,17,9,words,1000000));
    }
    struct fes_nano_tape_receipt receipt;
    assert(fes_nano_tape_load(&m->engine,17,9,m->gpo,0,0,&receipt));
    assert(fes_nano_tape_load(&m->engine,17,9,m->gpo,16376,0,&receipt));
    assert(fes_nano_tape_load(&m->engine,18,9,m->gpo,m->size,0,&receipt));
    assert(fes_nano_tape_load(&m->engine,17,10,m->gpo,m->size,0,&receipt));
    assert(fes_nano_tape_load(&m->engine,17,9,m->gpo,m->size,0,0));
    m->engine.busy=1;
    assert(fes_nano_tape_load(&m->engine,17,9,m->gpo,m->size,0,&receipt)==FES_TAPE_BUSY);
    m->engine.busy=0;
    assert(!m->engine.failed && !m->engine.used && !m->owns && !m->reads && !m->writes && !m->begins);
}
int main(int argc,char **argv)
{
    assert(argc==3);
    struct model m={0};m.mode=(unsigned)strtoul(argv[1],0,0);m.size=(unsigned)strtoul(argv[2],0,0);
    assert(m.mode<=28 && m.size && m.size<=sizeof(m.image));
    for (unsigned i=0;i<m.size;i++) m.image[i]=(uint8_t)(i*37+11);
    uint16_t words[]={0x4546,0x3153,1,0,4,1,0,0x803,0x2301,0x6745,0xab89,0xefcd,0x3210,0x7654,0xba98,0xfedc};
    memcpy(m.identity,words,sizeof(words));m.gpo=0x82000000;m.gpi=FES_COMPUTER_SIGNATURE|FES_COMPUTER_ACK_MASK;
    if (m.mode==27) m.gpi|=FES_COMPUTER_ERROR_MASK;
    if (m.mode==28) m.gpi|=1;
    m.fetches=0x1234;m.state=FES_COMPUTER_MEDIA_STATE_EMPTY;
    struct fes_nano_tape_ops ops={&m,owns,begin,end,read_reg,write_reg,image,now,barrier};
    assert(!fes_nano_tape_init(&m.engine,ops,17,9,words,1000000));
    if (!m.mode && m.size==1) rejected_arguments(&m,ops,words);
    struct fes_nano_tape_receipt receipt,untouched;memset(&receipt,0xa5,sizeof(receipt));untouched=receipt;
    uint32_t expected=crc(m.image,m.size);
    if (m.mode==9) expected^=1;
    uint32_t initial=m.gpo;
    if (m.mode==22) initial|=1; /* Run can never enter a tape transaction. */
    int error=fes_nano_tape_load(&m.engine,17,9,initial,m.size,expected,&receipt);
    assert(!m.active && !m.engine.busy);
    if (!m.mode) {
        assert(!error && m.engine.complete && !m.engine.failed && !m.aborts && m.commits==1);
        assert(receipt.owner==17 && receipt.generation==9 && receipt.bytes==m.size && receipt.crc==expected &&
            receipt.gpo==m.gpo && receipt.fetches==0x1234 && receipt.address==0x4000);
        assert(m.data_bytes==m.size && !memcmp(m.image,m.received,m.size));
        assert(m.begins==2+(m.size+511)/512 && m.ends==m.begins);
        assert(m.owns==2*(m.reads+m.writes+m.images)+2*m.begins);
    } else {
        assert(error && !m.engine.complete && !memcmp(&receipt,&untouched,sizeof(receipt)));
        if (m.mode==22) assert(!m.reads && !m.writes && !m.begins && !m.engine.used);
        else assert(m.engine.failed && m.engine.used && m.aborts<=1);
        if (m.mode==5) assert(m.engine.polls==64 && m.engine.first_error==FES_TAPE_LIMIT);
        if (m.mode==9 || m.mode==10) assert(m.engine.first_error==FES_TAPE_INTEGRITY && !m.commits);
        if (m.mode==3 || m.mode==4) assert(m.writes==48);
        if (m.mode==27 || m.mode==28) assert(!m.writes && !m.images);
    }
    unsigned accesses=m.reads+m.writes+m.owns+m.images+m.begins+m.ends+m.aborts;
    assert(fes_nano_tape_load(&m.engine,18,9,initial,m.size,expected,&receipt));
    if (m.mode!=22) assert(fes_nano_tape_load(&m.engine,17,9,initial,m.size,expected,&receipt));
    assert(accesses==m.reads+m.writes+m.owns+m.images+m.begins+m.ends+m.aborts);
    printf("PASS secure tape mode=%u bytes=%u writes=%u reads=%u frames=%u crc=%08x result=%d\n",m.mode,m.size,m.writes,m.reads,m.begins,expected,error);
    return 0;
}
