/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_configuration.h"

__attribute__((weak)) struct fes_nano_configuration *fes_nano_platform_configuration(void) { return 0; }
__attribute__((weak)) int fes_nano_platform_configuration_setup(void) { return 0; }

static void trace(struct fes_nano_configuration *s, unsigned stage)
{
    s->stage=stage;
    if(s->io.trace)s->io.trace(s->io.context,stage,s->completed,s->pending);
}
static int fail(struct fes_nano_configuration *s, int error)
{
    if(s) {
        if(!s->first_error)s->first_error=error<0 ? error : -5;
        s->failed=1;
        if(s->bridges)s->bridges->failed=1;
        trace(s,FES_CONFIG_FAILURE);
    }
    return 4;
}
static int scope(struct fes_nano_configuration *s)
{
    struct fes_nano_bridge_snapshot b;
    return s && s->initialized && !s->failed && s->bridges &&
        !fes_nano_bridges_observe(s->bridges,&b) && b.owner==s->owner &&
        b.generation==s->generation && b.generation==1 && b.never_enabled &&
        b.reset==FES_NANO_BRIDGES_ALL && b.masters_contained &&
        s->completed<=s->bytes && s->pending<=s->bytes-s->completed;
}
static int check(struct fes_nano_configuration *s, unsigned whole)
{
    uint64_t address=s->base+(whole ? 0 : s->completed);
    uint32_t bytes=whole ? s->bytes : s->pending;
    return scope(s) && s->io.check(s->io.context,address,bytes,whole)==0 && scope(s);
}
int fes_nano_configuration_init(struct fes_nano_configuration *s, struct fes_nano_bridges *b,
                                 struct fes_nano_configuration_ops io, uint64_t base,
                                 uint32_t bytes, uint32_t chunk, unsigned qualified)
{
    if(!s || s->initialized)return -1;
    s->initialized=1;s->stage=FES_CONFIG_INIT;s->bridges=b;s->io=io;
    if(qualified!=1 || !b || !io.check || base<0x82000000ULL || (base&63) ||
       !bytes || bytes>(8U<<20) || (bytes&3) || base>0xc0000000ULL-bytes ||
       !chunk || chunk>0xffff0U || (chunk&63))return fail(s,-22),-1;
    s->base=base;s->bytes=bytes;s->chunk=chunk;
    s->owner=b->state.owner;s->generation=b->state.generation;
    if(!scope(s))return fail(s,-1),-1;
    return 0;
}
unsigned fes_nano_configuration_busy(const struct fes_nano_configuration *s)
{ return s && s->prepared && !s->failed && !s->finished; }
void fes_nano_configuration_prepare(struct fes_nano_configuration *s, uint64_t owner,
                                     uint64_t generation, unsigned configuring, uint64_t result[4])
{
    result[0]=2;result[1]=(uint64_t)FES_EMBEDDED_BRIDGE_PREPARATION_TAG<<32;result[2]=result[3]=0;
    if(configuring || (s && s->bridges && s->bridges->device_lease)){result[0]=1;return;}
    if(!s || !s->initialized){result[0]=FES_EMBEDDED_BRIDGE_NOT_READY;return;}
    if(s->failed){result[0]=4;return;}
    if(!owner || owner!=s->owner || generation!=s->generation || generation!=1 || s->attempted)return;
    s->attempted=1;
    if(!check(s,1)){result[0]=fail(s,-1);return;}
    s->prepared=1;trace(s,FES_CONFIG_ARM);
    result[0]=0;result[1]|=s->bytes;result[2]=s->owner;result[3]=s->generation;
}
int fes_nano_configuration_before(struct fes_nano_configuration *s, uint32_t fid,
                                    uint64_t address, uint64_t bytes, unsigned configuring)
{
    if(!s || !s->initialized)return 2;
    if(s->failed)return 4;
    if(!s->prepared)return 2;
    if(s->finished || s->issued || !scope(s))return fail(s,-1);
    if(fid==FES_NANO_CONFIG_START) {
        if(address || bytes || configuring || s->started || s->completed || s->pending || !check(s,1))return fail(s,-1);
        s->started=1;trace(s,FES_CONFIG_START);
    } else {
        if(!s->started || !configuring)return fail(s,-1);
        if(fid==FES_NANO_CONFIG_WRITE) {
            uint32_t amount=s->bytes-s->completed;
            if(amount>s->chunk)amount=s->chunk;
            if(s->pending || !amount || address!=s->base+s->completed || bytes!=amount)return fail(s,-22);
            s->pending=amount;
            if(!check(s,0))return fail(s,-1);
            trace(s,FES_CONFIG_WRITE);
        } else if(fid==FES_NANO_CONFIG_COMPLETED) {
            if(address || bytes || !s->pending || !check(s,0))return fail(s,-1);
            trace(s,FES_CONFIG_POLL);
        } else if(fid==FES_NANO_CONFIG_ISDONE) {
            if(address || bytes || s->pending || s->completed!=s->bytes || !check(s,1))return fail(s,-1);
            trace(s,FES_CONFIG_STATUS);
        } else return fail(s,-22);
    }
    s->issued=fid;
    return 0;
}
void fes_nano_configuration_after(struct fes_nano_configuration *s, uint32_t fid, uint64_t r[4])
{
    if(!s || s->failed || s->issued!=fid || !scope(s))goto failed;
    s->issued=0;
    if(fid==FES_NANO_CONFIG_START) {
        if(r[0] || r[1] || r[2] || r[3] || !check(s,1))goto failed;
    } else if(fid==FES_NANO_CONFIG_WRITE) {
        if((r[0]!=0 && r[0]!=1) || r[1] || r[2] || r[3] || !check(s,0))goto failed;
    } else if(fid==FES_NANO_CONFIG_COMPLETED) {
        if(!check(s,0))goto failed;
        if(r[0]==1 && !r[1] && !r[2] && !r[3])return;
        if(r[0] || r[1]!=s->base+s->completed-FES_NANO_CONFIG_DRAM || r[2] || r[3])goto failed;
        s->completed+=s->pending;s->pending=0;
    } else if(fid==FES_NANO_CONFIG_ISDONE) {
        if(r[1] || r[2] || r[3] || !check(s,1))goto failed;
        if(r[0]==1)return;
        if(r[0])goto failed;
        s->finished=1;trace(s,FES_CONFIG_COMPLETE);
    } else goto failed;
    return;
failed:
    fail(s,-5);r[0]=4;r[1]=r[2]=r[3]=0;
}
