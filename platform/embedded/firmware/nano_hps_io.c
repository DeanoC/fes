/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_hps_io.h"
#include "generated/fes_embedded_bridges.h"

static int fail(struct fes_nano_hps_io *s, int error)
{
    if (!s->failed) s->first_error=error<0 ? error : -5;
    s->failed=1;
    return s->first_error;
}
int fes_nano_hps_io_offset(uint32_t v)
{
    if (v&3U) return 0;
    if ((v>=0x1000 && v<=0x109c) || (v>=0x1100 && v<=0x111c) ||
        (v>=0x1130 && v<=0x119c) || (v>=0x1200 && v<=0x124c) ||
        (v>=0x1400 && v<=0x14bc)) return 1;
    return v>=0x1300 && v<=0x1354 && v!=0x1324 && v!=0x134c;
}
int fes_nano_hps_io_init(struct fes_nano_hps_io *s,struct fes_nano_hps_io_ops io,
                         const struct fes_nano_hps_io_register *r,unsigned count,const uint8_t digest[32])
{
    if (!s || s->initialized || s->failed) return -1;
    unsigned nonzero=0;
    if (digest) for (unsigned i=0;i<32;i++) nonzero|=digest[i];
    if (!r || count!=FES_NANO_HPS_IO_COUNT || !nonzero || !io.owns || !io.read || !io.barrier) return fail(s,-22);
    unsigned selected=0;
    for (uint32_t offset=0x1000;offset<=0x14bc;offset+=4) {
        if (!fes_nano_hps_io_offset(offset)) continue;
        if (selected>=count || r[selected].offset!=offset || r[selected].mask!=0xffffffffU) return fail(s,-22);
        if ((offset<0x1130 && r[selected].value>15) ||
            (offset>=0x1300 && offset<=0x1354 && r[selected].value>1)) return fail(s,-22);
        selected++;
    }
    if (selected!=count) return fail(s,-22);
    s->io=io;s->registers=r;
    for (unsigned i=0;i<32;i++) s->digest[i]=digest[i];
    s->initialized=1;
    return 0;
}
static int scope(struct fes_nano_hps_io *s)
{
    return !s->failed && s->io.owns(s->io.context,s->owner,s->generation)==1 ? 0 : fail(s,-1);
}
static int sample(struct fes_nano_hps_io *s)
{
    s->reads=0;
    for (unsigned pass=0;pass<2;pass++) {
        for (unsigned i=0;i<FES_NANO_HPS_IO_COUNT;i++) {
            const struct fes_nano_hps_io_register *r=&s->registers[i];uint32_t value=0;
            if (scope(s)) return -1;
            s->io.barrier(s->io.context);
            int status=s->io.read(s->io.context,r->offset,&value);
            s->io.barrier(s->io.context);
            if (status) return fail(s,status);
            s->reads++;
            if (s->io.trace) s->io.trace(s->io.context,pass,i,r->offset,r->value,value);
            if (scope(s)) return -1;
            if (value!=r->value || (pass && value!=s->first[i])) return fail(s,-5);
            if (!pass) s->first[i]=value;
        }
    }
    return scope(s);
}
void fes_nano_hps_io_sip(struct fes_nano_hps_io *s,uint64_t operation,
                        uint64_t owner,uint64_t generation,unsigned busy,uint64_t result[4])
{
    result[0]=2;result[1]=(uint64_t)FES_EMBEDDED_BRIDGE_HPS_IO_TAG<<32;result[2]=result[3]=0;
    if (operation!=FES_EMBEDDED_BRIDGE_HPS_IO_PART0 && operation!=FES_EMBEDDED_BRIDGE_HPS_IO_PART1) return;
    unsigned part=(unsigned)(operation-FES_EMBEDDED_BRIDGE_HPS_IO_PART0);result[1]|=part;
    if (!s || !s->initialized) {result[0]=FES_EMBEDDED_BRIDGE_NOT_READY;return;}
    if (busy) {result[0]=1;return;}
    if (s->failed) {result[0]=4;return;}
    if (!owner || !generation) return;
    if (!part) {
        if (s->pending) {fail(s,-1);result[0]=4;return;}
        s->owner=owner;s->generation=generation;
        if (sample(s)) {result[0]=4;return;}
        s->pending=1;
    } else if (!s->pending || owner!=s->owner || generation!=s->generation) {
        fail(s,-1);result[0]=4;return;
    }
    if (scope(s)) {result[0]=4;return;}
    for (unsigned word=0;word<2;word++) for (unsigned byte=0;byte<8;byte++)
        result[2+word]=(result[2+word]<<8)|s->digest[part*16+word*8+byte];
    if (part) s->pending=0;
    result[0]=0;
}
