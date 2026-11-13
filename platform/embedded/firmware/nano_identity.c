/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_identity.h"
#include <string.h>

__attribute__((weak)) struct fes_nano_identity_probe *fes_nano_platform_identity_probe(void) { return 0; }
__attribute__((weak)) int fes_nano_platform_identity_setup(void) { return 0; }
__attribute__((weak)) int fes_nano_platform_control_target(unsigned open) { (void)open; return -1; }
__attribute__((weak)) int fes_nano_platform_ddr_target(unsigned open) { (void)open; return -1; }
__attribute__((weak)) int fes_nano_platform_window_prepare(void) { return -1; }
__attribute__((weak)) void fes_nano_platform_window_report(unsigned phase) { (void)phase; }
__attribute__((weak)) int fes_nano_platform_window_return(uint64_t *generation) { (void)generation; return 0; }
__attribute__((weak)) int fes_nano_platform_window_rearm(uint64_t owner,uint64_t generation)
{ (void)owner;(void)generation;return 0; }
__attribute__((weak)) void fes_nano_platform_identity_report(void) {}

static void trace(struct fes_nano_identity_probe *p, unsigned stage)
{ if (p->io.trace) p->io.trace(p->io.context,stage); }

static int fail(struct fes_nano_identity_probe *p, int error)
{
    if (!p->first_error) p->first_error=error<0 ? error : -5;
    p->failed=1;
    return p->first_error;
}
static int closed(struct fes_nano_bridge_snapshot *s)
{ return s->reset==FES_NANO_BRIDGES_ALL && s->masters_contained; }
static void recover(struct fes_nano_identity_probe *p, uint64_t result[4])
{
    trace(p,FES_ID_CONTAIN);
    p->cleanup_error=p->io.contain(p->io.context);
    p->active=0;p->failed=1;p->bridges->failed=1;result[0]=4;
}
int fes_nano_identity_init(struct fes_nano_identity_probe *p, struct fes_nano_bridges *b,
                             struct fes_nano_identity_ops io, uint32_t abi,
                             const uint32_t build[4], unsigned qualified)
{
    struct fes_nano_bridge_snapshot s;
    if (!p || p->initialized) return -1;
    p->initialized=1;p->stage=FES_ID_INIT;
    unsigned nonzero=0;
    if (build) for (unsigned i=0;i<4;i++) nonzero|=build[i];
    if (qualified!=1 || !b || !io.configured || !io.read32 || !io.contain || !abi || !nonzero ||
        (!!io.ddr_prepare!=!!io.ddr_cycle) ||
        fes_nano_bridges_observe(b,&s) || !closed(&s) || !s.never_enabled || s.generation!=1)
        return fail(p,-1);
    p->bridges=b;p->io=io;p->abi=abi;memcpy(p->build,build,sizeof(p->build));
    return 0;
}
static int control_owned(struct fes_nano_identity_probe *p, const struct fes_nano_bridge_snapshot *expected)
{
    struct fes_nano_bridge_snapshot actual;
    unsigned ddr=p->reader==2 && expected->reset==5 && !expected->masters_contained;
    return (!ddr && (expected->reset!=FES_NANO_BRIDGES_DATA || !expected->masters_contained)) ||
           fes_nano_bridges_observe(p->bridges,&actual) || actual.owner!=expected->owner ||
           actual.generation!=expected->generation || actual.reset!=expected->reset ||
           actual.masters_contained!=expected->masters_contained || actual.never_enabled ? fail(p,-1) : 0;
}
static int identity_read(struct fes_nano_identity_probe *p, const struct fes_nano_bridge_snapshot *control,
                           uint32_t values[15])
{
    static const uint32_t offsets[]={FES_EMBEDDED_REG_MAGIC,FES_EMBEDDED_REG_VERSION,FES_EMBEDDED_REG_ABI,
        FES_EMBEDDED_REG_BUILD0,FES_EMBEDDED_REG_BUILD1,FES_EMBEDDED_REG_BUILD2,FES_EMBEDDED_REG_BUILD3,
        FES_EMBEDDED_REG_GENERATION_LO,FES_EMBEDDED_REG_GENERATION_HI,
        FES_EMBEDDED_REG_ACK_GENERATION_LO,FES_EMBEDDED_REG_ACK_GENERATION_HI,
        FES_EMBEDDED_REG_ACK_COOKIE_LO,FES_EMBEDDED_REG_ACK_COOKIE_HI,
        FES_EMBEDDED_REG_STATUS,FES_EMBEDDED_REG_PENDING};
    if (control_owned(p,control)) return -1;
    for (unsigned i=0;i<15;i++) {
        p->reads++;
        if (p->io.read32(p->io.context,offsets[i],&values[i])) return fail(p,-5);
    }
    return control_owned(p,control);
}

static int reader_write(struct fes_nano_identity_probe *p, const struct fes_nano_bridge_snapshot *control,
                           uint32_t offset, uint32_t value)
{
    if (control_owned(p,control) || p->io.write32(p->io.context,offset,value) ||
        control_owned(p,control)) return fail(p,-5);
    return 0;
}
static int reader_read(struct fes_nano_identity_probe *p, const struct fes_nano_bridge_snapshot *control,
                          uint32_t offset, uint32_t *value)
{
    if (control_owned(p,control)) return -1;
    p->reads++;
    if (p->io.read32(p->io.context,offset,value) || control_owned(p,control)) return fail(p,-5);
    return 0;
}
/* Only read polling can repeat. Commands and injected response beats are
 * issued once. A held AXI address and the first of two beats must not grant an
 * acknowledgement. This responder is on chip, not a DDR containment proof. */
static int reader_state(struct fes_nano_identity_probe *p, const struct fes_nano_bridge_snapshot *control,
                           uint32_t status, uint32_t pending, unsigned acknowledged)
{
    for (unsigned poll=0;poll<128;poll++) {
        uint32_t values[8];
        static const uint32_t offsets[]={FES_EMBEDDED_REG_STATUS,FES_EMBEDDED_REG_PENDING,
            FES_EMBEDDED_REG_GENERATION_LO,FES_EMBEDDED_REG_GENERATION_HI,
            FES_EMBEDDED_REG_ACK_GENERATION_LO,FES_EMBEDDED_REG_ACK_GENERATION_HI,
            FES_EMBEDDED_REG_ACK_COOKIE_LO,FES_EMBEDDED_REG_ACK_COOKIE_HI};
        for (unsigned i=0;i<8;i++) if (reader_read(p,control,offsets[i],&values[i])) return -1;
        if (values[0]&FES_EMBEDDED_STATUS_FAULT || values[2]!=1 || values[3] ||
            values[5] || values[7]) return fail(p,-71);
        if (!acknowledged && (values[4] || values[6] || (values[0]&FES_EMBEDDED_STATUS_DRAINED)))
            return fail(p,-71);
        if (values[0]==status && values[1]==pending && values[4]==acknowledged &&
            values[6]==(acknowledged ? 7U : 0U)) return 0;
    }
    return fail(p,-110);
}
static int reader_cycle(struct fes_nano_identity_probe *p, const struct fes_nano_bridge_snapshot *control)
{
    uint32_t status,address;
#define WRITE(offset,value) do { if(reader_write(p,control,offset,value))return -1; } while(0)
    WRITE(FES_EMBEDDED_REG_REQUEST_GENERATION_LO,1);
    WRITE(FES_EMBEDDED_REG_REQUEST_GENERATION_HI,0);
    WRITE(FES_EMBEDDED_REG_COMMAND,FES_EMBEDDED_COMMAND_ENABLE);
    if(reader_state(p,control,FES_EMBEDDED_STATUS_ENABLED,0,0))return -1;
    p->stage=FES_ID_READER_ENABLED;trace(p,p->stage);
    WRITE(0x84,0x1000);WRITE(0x88,1);WRITE(0x80,0x0c);
    if(reader_state(p,control,FES_EMBEDDED_STATUS_ENABLED,0x10000,0) ||
       reader_read(p,control,0x8c,&status) || reader_read(p,control,0x90,&address))return -1;
    if(!(status&4) || address!=0x1000)return fail(p,-71);
    p->stage=FES_ID_READER_HELD;trace(p,p->stage);
    WRITE(FES_EMBEDDED_REG_REQUEST_COOKIE_LO,7);
    WRITE(FES_EMBEDDED_REG_REQUEST_COOKIE_HI,0);
    WRITE(FES_EMBEDDED_REG_COMMAND,FES_EMBEDDED_COMMAND_DISABLE);
    if(reader_state(p,control,FES_EMBEDDED_STATUS_DRAINING,0x10000,0) ||
       reader_read(p,control,0x8c,&status) || reader_read(p,control,0x90,&address))return -1;
    if(!(status&4) || address!=0x1000)return fail(p,-71);
    p->stage=FES_ID_READER_DISABLED;trace(p,p->stage);
    WRITE(0x80,0x05);
    if(reader_state(p,control,FES_EMBEDDED_STATUS_DRAINING,1,0))return -1;
    WRITE(0x80,0x07);
    if(reader_state(p,control,FES_EMBEDDED_STATUS_DRAINING,1,0) || reader_read(p,control,0x8c,&status))return -1;
    if((status>>16)!=1)return fail(p,-71);
    p->stage=FES_ID_READER_PARTIAL;trace(p,p->stage);
    WRITE(0x80,0x07);
    if(reader_state(p,control,FES_EMBEDDED_STATUS_DRAINED,0,1))return -1;
    p->stage=FES_ID_READER_DRAINED;trace(p,p->stage);
#undef WRITE
    return 0;
}
void fes_nano_identity_configuration_sip(struct fes_nano_identity_probe *p, uint64_t owner,
                            uint64_t generation, unsigned configuring, uint64_t result[4])
{
    struct fes_nano_bridge_snapshot s;
    result[0]=2;result[1]=(uint64_t)FES_EMBEDDED_BRIDGE_CONFIGURATION_TAG<<32;result[2]=result[3]=0;
    if(configuring || (p && p->bridges && p->bridges->device_lease)){result[0]=1;return;}
    if(!p || !p->initialized){result[0]=FES_EMBEDDED_BRIDGE_NOT_READY;return;}
    if(p->failed){result[0]=4;return;}
    if(!owner || !generation || p->queried || p->bridges->state.owner!=owner ||
       p->bridges->state.generation!=generation)return;
    if(fes_nano_bridges_observe(p->bridges,&s) || !closed(&s) ||
       s.generation!=generation || !s.never_enabled){result[0]=4;return;}
    p->queried=1;p->stage=FES_ID_CONFIG;
    int ready=p->io.configured(p->io.context);
    if(ready<0 || ready>1){fail(p,ready);recover(p,result);return;}
    if(fes_nano_bridges_observe(p->bridges,&s) || !closed(&s) ||
       s.owner!=owner || s.generation!=generation || !s.never_enabled){
        fail(p,-1);recover(p,result);return;
    }
    result[0]=0;result[1]|=(unsigned)ready;result[2]=owner;result[3]=generation;
}
static void identity_sip(struct fes_nano_identity_probe *p, uint64_t owner,
                            uint64_t generation, unsigned configuring, unsigned reader, uint64_t result[4])
{
    struct fes_nano_bridge_snapshot before,control,current;
    struct fes_nano_bridge_closure after;
    uint32_t first[15],second[15];
    result[0]=2;result[1]=(uint64_t)(reader ? FES_EMBEDDED_BRIDGE_READER_TAG : FES_EMBEDDED_BRIDGE_IDENTITY_TAG)<<32;result[2]=result[3]=0;
    if (configuring || (p && p->bridges && p->bridges->device_lease)) { result[0]=1;return; }
    if (!p || !p->initialized) { result[0]=FES_EMBEDDED_BRIDGE_NOT_READY;return; }
    if (p->failed) { result[0]=4;return; }
    /* This recipe controls the fixed on-chip responder. A DDR personality
     * needs its own qualified address/bridge/drain path before any control I/O. */
    unsigned ddr=reader && p->abi==0x10203041U && p->io.ddr_prepare && p->io.ddr_cycle;
    if (reader && ((!ddr && p->abi!=0x10203040U) || !p->io.write32)) { result[0]=FES_EMBEDDED_BRIDGE_NOT_READY;return; }
    if (!owner || !generation || generation>UINT64_MAX-(ddr ? 3U : 2U) || p->attempted || p->bridges->state.owner!=owner ||
        p->bridges->state.generation!=generation) return;
    if (fes_nano_bridges_observe(p->bridges,&before) || !closed(&before) ||
        before.generation!=generation || !before.never_enabled) { result[0]=4;return; }
    p->attempted=1;p->reader=ddr ? 2U : reader;p->stage=FES_ID_CONFIG;
    int ready=p->io.configured(p->io.context);
    if (ready<0 || ready>1) { fail(p,ready);goto recover; }
    if (fes_nano_bridges_observe(p->bridges,&current) || !closed(&current) ||
        current.owner!=owner || current.generation!=generation || !current.never_enabled) {
        fail(p,-1);goto recover;
    }
    if (!ready) { result[0]=FES_EMBEDDED_BRIDGE_NOT_READY;return; }
    p->active=1;p->stage=FES_ID_OPEN;
    trace(p,FES_ID_OPEN);
    if (fes_nano_bridges_open(p->bridges,owner,generation,FES_NANO_BRIDGES_CONTROL,&control)) {
        fail(p,-1);goto recover;
    }
    p->stage=FES_ID_READ;
    trace(p,FES_ID_READ);
    if (identity_read(p,&control,first) || identity_read(p,&control,second)) goto close;
    p->stage=FES_ID_MATCH;
    if (memcmp(first,second,sizeof(first)) || first[0]!=FES_EMBEDDED_MAGIC ||
        first[1]!=FES_EMBEDDED_VERSION || first[2]!=p->abi || memcmp(first+3,p->build,sizeof(p->build)))
        fail(p,-71);
    for (unsigned i=7;i<15;i++) if (first[i]) fail(p,-71);
    if (!p->first_error && ddr) {
        p->stage=FES_ID_DDR_PREPARE;trace(p,p->stage);
        int status=p->io.ddr_prepare(p->io.context);
        if (status) { fail(p,status);goto close; }
        p->stage=FES_ID_DDR_RELEASE;trace(p,p->stage);
        status=fes_nano_bridges_open(p->bridges,owner,control.generation,8,&control);
        if (status) {
            fail(p,status);goto recover;
        }
        p->stage=FES_ID_DDR_READ;trace(p,p->stage);
        status=p->io.ddr_cycle(p->io.context);
        if (status) { fail(p,status);goto close; }
        p->stage=FES_ID_DDR_VERIFY;trace(p,p->stage);
    }
    if (!p->first_error && reader) {
        if(!ddr && reader_cycle(p,&control))goto close;
        if(identity_read(p,&control,first) || identity_read(p,&control,second))goto close;
        static const uint32_t finished[8]={1,0,1,0,7,0,FES_EMBEDDED_STATUS_DRAINED,0};
        if(memcmp(first,second,sizeof(first)) || first[0]!=FES_EMBEDDED_MAGIC ||
           first[1]!=FES_EMBEDDED_VERSION || first[2]!=p->abi || memcmp(first+3,p->build,sizeof(p->build)) ||
           memcmp(first+7,finished,sizeof(finished)))fail(p,-71);
    }
close:
    trace(p,FES_ID_CLOSE);
    if (!p->first_error) p->stage=FES_ID_CLOSE;
    unsigned data_active=control.reset==5 && ddr;
    int close_status=fes_nano_bridges_close(p->bridges,owner,control.generation,0,!data_active,&after);
    if (close_status) fail(p,close_status);
    else if (after.after.owner!=owner || after.after.generation!=generation+(data_active ? 3U : 2U) || !closed(&after.after) ||
        after.data_remained_contained==data_active || !after.producers_quiescent ||
        after.fabric_acknowledged || after.hardware_drained!=(data_active ? 8U : 0U)) fail(p,-1);
    if (p->first_error) goto recover;
    p->closure=after;
    p->active=0;p->stage=FES_ID_COMPLETE;
    trace(p,FES_ID_COMPLETE);
    result[0]=0;result[1]|=p->abi;
    result[2]=((uint64_t)first[6]<<32)|first[5];
    result[3]=((uint64_t)first[4]<<32)|first[3];
    return;
recover:
    /* Even a partially failed enable can have opened control. Close that path
     * independently, preserve the first error, and retire this boot's proof. */
    recover(p,result);
}

void fes_nano_identity_sip(struct fes_nano_identity_probe *p, uint64_t owner,
                            uint64_t generation, unsigned configuring, uint64_t result[4])
{ identity_sip(p,owner,generation,configuring,0,result); }
void fes_nano_reader_sip(struct fes_nano_identity_probe *p, uint64_t owner,
                            uint64_t generation, unsigned configuring, uint64_t result[4])
{ identity_sip(p,owner,generation,configuring,1,result); }
