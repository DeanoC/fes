/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_configuration.h"
#include "nano_program.h"

__attribute__((weak)) struct fes_nano_configuration *fes_nano_platform_configuration(void) { return 0; }
__attribute__((weak)) int fes_nano_platform_configuration_setup(void) { return 0; }
__attribute__((weak)) int fes_nano_platform_configuration_diagnostic_ready(void) { return 0; }

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
static int held(struct fes_nano_configuration *s, struct fes_nano_bridge_snapshot *b)
{
    return s && s->initialized && !s->failed && s->bridges && !s->bridges->device_lease &&
        !fes_nano_bridges_observe(s->bridges,b) && b->owner==s->owner &&
        b->generation && b->never_enabled==(s->rearmed ? 0U : 1U) &&
        b->reset==FES_NANO_BRIDGES_ALL && b->masters_contained;
}
static int scope(struct fes_nano_configuration *s)
{
    struct fes_nano_bridge_snapshot b;
    return held(s,&b) && b.generation==s->generation &&
        s->chunk && s->completed<=s->bytes && s->pending<=s->bytes-s->completed &&
        s->pending<=(uint64_t)s->chunk*FES_NANO_CONFIG_BUFFERS &&
        (s->completed==s->bytes || s->completed%s->chunk==0) &&
        (s->completed+s->pending==s->bytes || s->pending%s->chunk==0);
}
static int check(struct fes_nano_configuration *s, unsigned whole)
{
    if(!scope(s))return 0;
    if(whole)return s->io.check(s->io.context,s->base,s->bytes,1)==0 && scope(s);
    for(uint32_t offset=0;offset<s->pending;) {
        uint32_t bytes=s->pending-offset;
        if(bytes>s->chunk)bytes=s->chunk;
        if(s->io.check(s->io.context,s->base+s->completed+offset,bytes,0)!=0 || !scope(s))return 0;
        offset+=bytes;
    }
    return 1;
}
static int bind(struct fes_nano_configuration *s)
{
    if(!s->io.bind)return 1;
    uint32_t bytes=0;
    if(s->io.bind(s->io.context,s->owner,s->generation,&bytes) ||
       !bytes || (bytes&3U) || bytes>s->maximum)return 0;
    s->bytes=bytes;
    return scope(s);
}
int fes_nano_configuration_init(struct fes_nano_configuration *s, struct fes_nano_bridges *b,
                                 struct fes_nano_configuration_ops io, uint64_t base,
                                 uint32_t bytes, uint32_t chunk, unsigned qualified)
{
    if(!s || s->initialized)return -1;
    s->initialized=1;s->stage=FES_CONFIG_INIT;s->bridges=b;s->io=io;
    if(qualified!=1 || !b || !io.check || !io.complete || base<0x82000000ULL || (base&63) ||
       !bytes || bytes>(8U<<20) || (bytes&3) || base>0xc0000000ULL-bytes ||
       !chunk || chunk>0xffff0U || (chunk&63))return fail(s,-22),-1;
    s->base=base;s->bytes=s->maximum=bytes;s->chunk=chunk;
    s->owner=b->state.owner;s->generation=b->state.generation;
    if(s->generation!=1 || !scope(s))return fail(s,-1),-1;
    return 0;
}
unsigned fes_nano_configuration_busy(const struct fes_nano_configuration *s)
{ return s && s->prepared && !s->failed && !s->finished; }
int fes_nano_configuration_retire(struct fes_nano_configuration *s, uint64_t owner,
                                    uint64_t generation, unsigned configuring)
{
    if(!s || !s->initialized || !s->bridges || !s->prepared || !s->started || s->finished ||
       s->failed || s->issued || !s->pending || !configuring || !owner ||
       owner!=s->owner || generation!=s->generation ||
       s->bridges->device_lease || !scope(s))return -1;
    fail(s,-125);
    return 0;
}
void fes_nano_configuration_prepare(struct fes_nano_configuration *s, uint64_t owner,
                                     uint64_t generation, unsigned configuring, uint64_t result[4])
{
    result[0]=2;result[1]=(uint64_t)FES_EMBEDDED_BRIDGE_PREPARATION_TAG<<32;result[2]=result[3]=0;
    if(configuring || (s && s->bridges && s->bridges->device_lease)){result[0]=1;return;}
    if(!s || !s->initialized){result[0]=FES_EMBEDDED_BRIDGE_NOT_READY;return;}
    if(s->failed){result[0]=4;return;}
    if(!owner || owner!=s->owner || !generation || s->attempted)return;
    struct fes_nano_bridge_snapshot current;
    if(!held(s,&current)){result[0]=fail(s,-1);return;}
    if(generation!=current.generation || generation<s->generation)return;
    /* Only secure closed-device transitions can advance a never-enabled boot.
     * Bind once to their current generation before digest checks or START;
     * no generation change is allowed after this preparation. */
    s->generation=generation;
    s->attempted=1;
    if(!bind(s) || !check(s,1)){result[0]=fail(s,-1);return;}
    s->prepared=1;trace(s,FES_CONFIG_ARM);
    result[0]=0;result[1]|=s->bytes|(s->io.bind ? FES_NANO_PROGRAM_BOUND_REPLY : 0U);result[2]=s->owner;result[3]=s->generation;
}
void fes_nano_configuration_rearm(struct fes_nano_configuration *s, uint64_t owner,
                                   uint64_t generation, unsigned configuring, uint64_t result[4])
{
    result[0]=2;result[1]=(uint64_t)FES_EMBEDDED_BRIDGE_REARM_TAG<<32;result[2]=result[3]=0;
    if(configuring || (s && s->bridges && s->bridges->device_lease)){result[0]=1;return;}
    if(!s || !s->initialized){result[0]=FES_EMBEDDED_BRIDGE_NOT_READY;return;}
    if(s->failed){result[0]=4;return;}
    if(!owner || owner!=s->owner || !generation || generation>UINT64_MAX-2 || generation<=s->generation ||
       !s->attempted || !s->prepared || !s->started || !s->finished || s->issued ||
       s->pending || s->completed!=s->bytes || !s->bytes)return;
    struct fes_nano_bridge_snapshot current;
    if(fes_nano_bridges_observe(s->bridges,&current)){result[0]=fail(s,-1);return;}
    if(current.owner!=owner || current.generation!=generation || current.never_enabled ||
       current.reset!=FES_NANO_BRIDGES_ALL || !current.masters_contained)return;
    if(fes_nano_bridges_configuration_guard(s->bridges)){result[0]=fail(s,-1);return;}
    /* Consume this new generation before checking memory. Failure keeps all
     * bytes and the original error owned; no stale completion can rearm it. */
    s->generation=generation;s->rearmed=1;
    s->completed=s->pending=s->issued=0;s->started=s->finished=0;
    if(!bind(s) || !check(s,1)){result[0]=fail(s,-1);return;}
    s->prepared=1;trace(s,FES_CONFIG_ARM);
    result[0]=0;result[1]|=s->bytes|(s->io.bind ? FES_NANO_PROGRAM_BOUND_REPLY : 0U);result[2]=s->owner;result[3]=s->generation;
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
            uint32_t amount=s->bytes-s->completed-s->pending;
            if(amount>s->chunk)amount=s->chunk;
            if(s->pending>=(uint64_t)s->chunk*FES_NANO_CONFIG_BUFFERS || !amount ||
               address!=s->base+s->completed+s->pending || bytes!=amount)return fail(s,-22);
            s->pending+=amount;
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
        if(r[0] || !r[1])goto failed;
        uint32_t bytes=0;
        for(unsigned i=1;i<4;i++) {
            if(!r[i]) {
                for(unsigned j=i+1;j<4;j++)if(r[j])goto failed;
                break;
            }
            if(bytes>=s->pending || r[i]!=s->base+s->completed+bytes-FES_NANO_CONFIG_DRAM)goto failed;
            uint32_t amount=s->pending-bytes;
            if(amount>s->chunk)amount=s->chunk;
            bytes+=amount;
        }
        s->completed+=bytes;s->pending-=bytes;
    } else if(fid==FES_NANO_CONFIG_ISDONE) {
        if(r[1] || r[2] || r[3] || !check(s,1))goto failed;
        if(r[0]==1)return;
        if(r[0])goto failed;
        int error=s->io.complete(s->io.context,s->owner,s->generation,s->rearmed);
        if(error){fail(s,error);goto failed;}
        if(!scope(s))goto failed;
        /* The preceding core's stop cannot authorize this newly configured
         * fabric. Its own opening and verified return must publish a new one. */
        s->bridges->closure_valid=0;
        s->finished=1;trace(s,FES_CONFIG_COMPLETE);
    } else goto failed;
    return;
failed:
    fail(s,-5);r[0]=4;r[1]=r[2]=r[3]=0;
}
