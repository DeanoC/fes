/* SPDX-License-Identifier: BSD-3-Clause */
/* Strong fixed-source supplier, included after configuration and held-ROM
 * objects. No request selects these callbacks, addresses, identity or timer. */
#if !defined(FES_NANO_COLD_CONFIG) || !FES_NANO_COLD_CONFIG || \
    !defined(FES_NANO_COLD_COMPUTER) || !FES_NANO_COLD_COMPUTER || \
    !defined(FES_NANO_COLD_ROM) || !FES_NANO_COLD_ROM || \
    !defined(FES_NANO_SHARED_USB) || !FES_NANO_SHARED_USB || \
    !defined(FES_NANO_CONTROLLED_SD) || !FES_NANO_CONTROLLED_SD
#error Initial_tape_requires_the_matched_shared_USB_computer_ROM_and_SD_path
#endif
#include "nano_tape_service.h"
_Static_assert(FES_NANO_CONFIG_BASE==0x94800000ULL && FES_NANO_CONFIG_ARENA_SIZE==(8U<<20) &&
    FES_NANO_CONTROL_ABI==FES_NANO_PROGRAM_COMPUTER_ABI && FES_NANO_ROM_CPU_BASE==0x20010500UL,
    "initial tape fixed matched memory and computer control");
/* The sealed cold computer contract accepts only this internal-RAM GP1.0
 * aperture/capability combination. It is not inferred from an NS descriptor. */
#define FES_NANO_TAPE_CPU_BASE 0x20010300UL
#define FES_NANO_TAPE_CAPABILITIES 0x803U
static struct fes_nano_tape_service computer_tape;
static unsigned computer_tape_reported;
/* Diagnostic samples only: never read by admission, ownership or deadlines.
 * Hot callbacks sample once per64 invocations; frame/hash callbacks sample all.
 * Record after the operation, print only after load and its failure containment.
 * These extra timer reads consume the original deadline, never extend it. */
enum tape_cost_kind { TAPE_COST_GUARD,TAPE_COST_OWNS,TAPE_COST_READ,TAPE_COST_WRITE,
    TAPE_COST_BARRIER,TAPE_COST_IMAGE,TAPE_COST_HASH,TAPE_COST_COUNT };
static struct { uint64_t ticks,max;unsigned calls,samples,clock_invalid; } tape_cost[TAPE_COST_COUNT];
enum tape_owns_part { TAPE_OWNS_BEFORE,TAPE_OWNS_USB,TAPE_OWNS_REGISTERS,TAPE_OWNS_AFTER,TAPE_OWNS_PARTS };
/* Sample only the same outer owns invocations selected above. Partial/failed
 * paths can have fewer samples; these counters never supply an owns result. */
static struct { uint64_t ticks,max;unsigned samples,clock_invalid; } tape_owns_cost[TAPE_OWNS_PARTS];
static void tape_owns_cost_end(unsigned part,uint64_t began)
{
    uint64_t now=read_cntpct_el0();
    tape_owns_cost[part].samples++;
    if(now<began) {tape_owns_cost[part].clock_invalid=1;return;}
    uint64_t ticks=now-began;
    tape_owns_cost[part].ticks+=ticks;
    if(ticks>tape_owns_cost[part].max)tape_owns_cost[part].max=ticks;
}
static unsigned tape_cost_mask(unsigned kind)
{return kind>=TAPE_COST_OWNS && kind<=TAPE_COST_BARRIER ? 63U : 0U;}
static uint64_t tape_cost_begin(unsigned kind)
{
    unsigned call=++tape_cost[kind].calls;
    return ((call-1)&tape_cost_mask(kind)) ? 0 : read_cntpct_el0();
}
static void tape_cost_end(unsigned kind,uint64_t began)
{
    if((tape_cost[kind].calls-1)&tape_cost_mask(kind))return;
    uint64_t now=read_cntpct_el0();
    tape_cost[kind].samples++;
    if(now<began) {tape_cost[kind].clock_invalid=1;return;}
    uint64_t ticks=now-began;
    tape_cost[kind].ticks+=ticks;
    if(ticks>tape_cost[kind].max)tape_cost[kind].max=ticks;
}
static struct {
    uint64_t owner,generation,usb_generation;
    uint32_t phase,offset,next;
    unsigned active,failed,prepared,finished;
} computer_tape_frame;

/* These hot private checks stay live on every access. Inline them so their
 * nested calls do not repeatedly spill the same owner/context to uncached RAM. */
static inline __attribute__((always_inline)) int computer_tape_state(void *context,uint64_t owner,uint64_t generation)
{
    struct fes_nano_bridges *b=&fes_nano_owned_bridges;
    struct fes_nano_configuration *c=&configuration;
    struct fes_nano_identity_probe *p=&identity_probe;
#if defined(FES_NANO_PROGRAM_ROLES) && FES_NANO_PROGRAM_ROLES
    if(active_program_role!=FES_NANO_PROGRAM_ROLE_COMPUTER)return 0;
#endif
    return context==&provider && cold_master_cpu_scope() &&
        read_cntfrq_el0()==FES_EMBEDDED_ROM_TICK_HZ && provider.valid && !provider.failed &&
        provider.record.boot.owner==owner && provider.record.boot.epoch==b->epoch &&
        provider.control_expected==1 && !provider.ddr_expected &&
        b->initialized && !b->failed && !b->device_lease && b->state.owner==owner &&
        b->state.generation==generation && b->state.reset==FES_NANO_BRIDGES_DATA && b->state.masters_contained &&
        c->initialized && c->attempted && c->prepared && c->started && c->finished && !c->failed &&
        !c->pending && !c->issued && c->bytes && c->completed==c->bytes && c->bridges==b &&
        c->owner==owner && c->generation<UINT64_MAX && c->generation+1==generation &&
        configuration_program.bound && !configuration_program.failed && configuration_program.owner==owner &&
        configuration_program.generation==c->generation && configuration_program.bytes==c->bytes &&
        configuration_program.rom_bytes && p->initialized && p->attempted && p->active &&
        !p->failed && !p->first_error && !p->cleanup_error && p->bridges==b && p->stage==FES_ID_READ &&
        p->abi==FES_NANO_PROGRAM_COMPUTER_ABI && !p->reader &&
        computer_rom.initialized && computer_rom.used && computer_rom.complete && !computer_rom.busy &&
        !computer_rom.failed && computer_rom.owner==owner && computer_rom.generation==generation &&
        computer_rom.bytes==configuration_program.rom_bytes &&
        usb_platform.active==1 && usb_platform.confirmed==1 && !usb_platform.failed &&
        !sd_platform.active && !sd_platform.failed && !fes_nano_owned_sd.active && !fes_nano_owned_sd.failed;
}
static int computer_tape_frame_guard(void *context,uint64_t owner,uint64_t generation)
{
    struct fes_nano_bridge_snapshot s;
    if(computer_tape_frame.active || computer_tape_frame.failed ||
       !computer_tape_state(context,owner,generation) ||
       !fes_nano_usb_master_binding_matches(&usb_master,&fes_nano_owned_bridges) ||
       fes_nano_bridges_observe(&fes_nano_owned_bridges,&s))return -1;
    return computer_tape_state(context,owner,generation) && s.owner==owner && s.generation==generation &&
        s.reset==FES_NANO_BRIDGES_DATA && s.masters_contained ? 0 : -1;
}
static int computer_tape_guard(void *context,uint64_t owner,uint64_t generation)
{
    uint64_t began=tape_cost_begin(TAPE_COST_GUARD);
    int status=computer_tape_frame_guard(context,owner,generation);
    tape_cost_end(TAPE_COST_GUARD,began);return status;
}
static inline __attribute__((always_inline)) int computer_tape_sample_state(void *context,uint64_t owner,uint64_t generation,unsigned part,unsigned sampled)
{
    if(!sampled)return computer_tape_state(context,owner,generation);
    uint64_t began=read_cntpct_el0();
    int status=computer_tape_state(context,owner,generation);
    tape_owns_cost_end(part,began);return status;
}
static int computer_tape_sample_usb(unsigned sampled)
{
    uint64_t began=sampled ? read_cntpct_el0() : 0;
    int status=fes_nano_usb_master_service_valid(&usb_master,&fes_nano_owned_bridges,&usb_platform);
    if(sampled)tape_owns_cost_end(TAPE_OWNS_USB,began);
    return status;
}
static int computer_tape_sample_registers(unsigned sampled)
{
    uint64_t began=sampled ? read_cntpct_el0() : 0;
    dsbsy();
    uint32_t reset=mmio_read_32(FES_NANO_COLD_RSTMGR+0x2c);
    uint32_t per0=mmio_read_32(FES_NANO_COLD_RSTMGR+0x24);
    dsbsy();
    int status=(reset&FES_NANO_BRIDGES_ALL)==FES_NANO_BRIDGES_DATA &&
        (per0&FES_NANO_MASTER_RESET)==(FES_NANO_MASTER_RESET&~FES_NANO_COLD_USB_RESET);
    if(sampled)tape_owns_cost_end(TAPE_OWNS_REGISTERS,began);
    return status;
}
static int computer_tape_owned(void *context,uint64_t owner,uint64_t generation,unsigned sampled)
{
    struct fes_nano_tape *e=&computer_tape.engine;
    if(!computer_tape_frame.active || computer_tape_frame.failed || !computer_tape.used || computer_tape.failed ||
       !e->busy || !e->used || e->failed || e->complete || e->owner!=owner || e->generation!=generation ||
       computer_tape_frame.owner!=owner || computer_tape_frame.generation!=generation ||
       computer_tape_frame.phase!=e->phase || computer_tape_frame.offset!=e->frame_offset ||
       usb_platform.generation!=computer_tape_frame.usb_generation ||
       !computer_tape_sample_state(context,owner,generation,TAPE_OWNS_BEFORE,sampled) ||
       computer_tape_sample_usb(sampled)!=1)return 0;
    return computer_tape_sample_registers(sampled) &&
        computer_tape_sample_state(context,owner,generation,TAPE_OWNS_AFTER,sampled);
}
static int computer_tape_owns(void *context,uint64_t owner,uint64_t generation)
{
    uint64_t began=tape_cost_begin(TAPE_COST_OWNS);
    unsigned sampled=((tape_cost[TAPE_COST_OWNS].calls-1)&tape_cost_mask(TAPE_COST_OWNS))==0;
    int status=computer_tape_owned(context,owner,generation,sampled);
    tape_cost_end(TAPE_COST_OWNS,began);return status;
}
static int computer_tape_begin(void *context,uint64_t owner,uint64_t generation,unsigned phase,uint32_t offset)
{
    struct fes_nano_tape *e=&computer_tape.engine;
    if(computer_tape_frame.active || computer_tape_frame.failed || !computer_tape.used ||
       !e->busy || !e->used || e->failed || e->complete ||
       e->owner!=owner || e->generation!=generation ||
       !((phase==FES_TAPE_PREPARE && !offset && !computer_tape_frame.prepared) ||
         (phase==FES_TAPE_CHUNK && computer_tape_frame.prepared && offset==computer_tape_frame.next && offset<e->bytes) ||
         (phase==FES_TAPE_FINISH && computer_tape_frame.prepared && !computer_tape_frame.finished &&
          offset==computer_tape_frame.next && offset==e->bytes)) ||
       computer_tape_guard(context,owner,generation)) {
        computer_tape_frame.failed=1;return -1;
    }
    computer_tape_frame.owner=owner;computer_tape_frame.generation=generation;
    computer_tape_frame.usb_generation=usb_platform.generation;
    computer_tape_frame.phase=phase;computer_tape_frame.offset=offset;
    computer_tape_frame.active=1;return 0;
}
static int computer_tape_end(void *context,uint64_t owner,uint64_t generation,unsigned phase,uint32_t offset,unsigned success)
{
    unsigned active=computer_tape_frame.active;
    computer_tape_frame.active=0; /* Consume even on a failed final proof. */
    if(!success) {computer_tape_frame.failed=1;return 0;} /* Abort without I/O. */
    if(success!=1 || active!=1 || computer_tape_frame.failed ||
       computer_tape_frame.owner!=owner || computer_tape_frame.generation!=generation ||
       computer_tape_frame.phase!=phase || computer_tape_frame.offset!=offset ||
       usb_platform.generation!=computer_tape_frame.usb_generation ||
       computer_tape_guard(context,owner,generation) ||
       usb_platform.generation!=computer_tape_frame.usb_generation) {
        computer_tape_frame.failed=1;return -1;
    }
    if(phase==FES_TAPE_PREPARE)computer_tape_frame.prepared=1;
    else if(phase==FES_TAPE_CHUNK) {
        uint32_t count=computer_tape.engine.bytes-offset;
        if(count>FES_COMPUTER_MEDIA_CHUNK_MAX_BYTES)count=FES_COMPUTER_MEDIA_CHUNK_MAX_BYTES;
        computer_tape_frame.next=offset+count;
    } else computer_tape_frame.finished=1;
    return 0;
}
static int computer_tape_read_register(void *context,uint32_t offset,uint32_t *value)
{
    if(context!=&provider || !value || (offset&3U) || offset>=FES_DE25_COMPUTER_SPAN)return -1;
    *value=mmio_read_32(FES_NANO_TAPE_CPU_BASE+offset);return 0;
}
static int computer_tape_read(void *context,uint32_t offset,uint32_t *value)
{
    uint64_t began=tape_cost_begin(TAPE_COST_READ);
    int status=computer_tape_read_register(context,offset,value);
    tape_cost_end(TAPE_COST_READ,began);return status;
}
static int computer_tape_write_register(void *context,uint32_t offset,uint32_t value)
{
    if(context!=&provider || offset!=FES_DE25_COMPUTER_GPO_OFFSET)return -1;
    mmio_write_32(FES_NANO_TAPE_CPU_BASE+offset,value);return 0;
}
static int computer_tape_write(void *context,uint32_t offset,uint32_t value)
{
    uint64_t began=tape_cost_begin(TAPE_COST_WRITE);
    int status=computer_tape_write_register(context,offset,value);
    tape_cost_end(TAPE_COST_WRITE,began);return status;
}
static int computer_tape_copy_image(void *context,uint32_t offset,uint8_t *out,uint32_t bytes)
{
    if(context!=&provider || !out || !bytes || bytes>FES_COMPUTER_MEDIA_CHUNK_MAX_BYTES ||
       offset>computer_tape.engine.bytes || bytes>computer_tape.engine.bytes-offset ||
       computer_tape.engine.bytes>FES_DE25_COMPUTER_ZX81_TAPE_MAX_BYTES)return -1;
    dsbsy();memcpy(out,(const void *)(uintptr_t)(FES_NANO_CONFIG_BASE+offset),bytes);dsbsy();return 0;
}
static int computer_tape_image(void *context,uint32_t offset,uint8_t *out,uint32_t bytes)
{
    uint64_t began=tape_cost_begin(TAPE_COST_IMAGE);
    int status=computer_tape_copy_image(context,offset,out,bytes);
    tape_cost_end(TAPE_COST_IMAGE,began);return status;
}
static uint64_t computer_tape_now(void *context) {(void)context;return read_cntpct_el0();}
static void computer_tape_barrier(void *context)
{
    (void)context;
    uint64_t began=tape_cost_begin(TAPE_COST_BARRIER);
    dsbsy();tape_cost_end(TAPE_COST_BARRIER,began);
}
static int computer_tape_header(void *context,uint8_t header[128])
{
    if(context!=&provider)return -1;
    dsbsy();memcpy(header,(const void *)(uintptr_t)(FES_NANO_CONFIG_BASE+FES_NANO_CONFIG_ARENA_SIZE-
        FES_NANO_PROGRAM_METADATA_BYTES),128);dsbsy();return 0;
}
static int computer_tape_verify_image(void *context,uint32_t bytes,const uint8_t digest[48])
{
    uint8_t observed[48];sha512_context hash;
    if(context!=&provider || !bytes || bytes>FES_DE25_COMPUTER_ZX81_TAPE_MAX_BYTES)return -1;
    dsbsy();sha384_init(&hash);sha384_update(&hash,(const void *)(uintptr_t)FES_NANO_CONFIG_BASE,bytes);
    sha384_finish(&hash,observed);dsbsy();return memcmp(observed,digest,48) ? -1 : 0;
}
static int computer_tape_verify(void *context,uint32_t bytes,const uint8_t digest[48])
{
    uint64_t began=tape_cost_begin(TAPE_COST_HASH);
    int status=computer_tape_verify_image(context,bytes,digest);
    tape_cost_end(TAPE_COST_HASH,began);return status;
}
static int computer_tape_prepare(uint64_t owner,uint64_t generation,unsigned computer)
{
    if(!owner || !generation || generation==UINT64_MAX || computer_tape_frame.active || computer_tape_frame.failed ||
       (computer_tape.initialized && (computer_tape.failed || computer_tape.engine.busy ||
        (computer_tape.used && !computer_tape.complete) || computer_tape.engine.owner!=owner ||
        computer_tape.engine.generation>=generation+1)))return -1;
    memset(&computer_tape,0,sizeof(computer_tape));memset(&computer_tape_frame,0,sizeof(computer_tape_frame));
    computer_tape_reported=0;memset(tape_cost,0,sizeof(tape_cost));
    memset(tape_owns_cost,0,sizeof(tape_owns_cost));
    if(!computer)return 0; /* A no-tape or menu activation still returns normally. */
    if(!configuration_program.bound || configuration_program.failed || configuration_program.owner!=owner ||
       configuration_program.generation!=generation || !configuration_program.rom_bytes)return -1;
    uint16_t identity[16]={FES_COMPUTER_IDENTITY_MAGIC0,FES_COMPUTER_IDENTITY_MAGIC1,
        FES_COMPUTER_TRANSPORT_MAJOR,FES_COMPUTER_TRANSPORT_MINOR,FES_COMPUTER_ABI_TAG,FES_COMPUTER_ABI_MAJOR,0,
        FES_NANO_TAPE_CAPABILITIES};
    const uint8_t *build=configuration_program.header+FES_NANO_PROGRAM_BUILD;
    for(unsigned i=0;i<8;i++)identity[8+i]=build[2*i]|(uint16_t)build[2*i+1]<<8;
    struct fes_nano_tape_ops io={&provider,computer_tape_owns,computer_tape_begin,computer_tape_end,
        computer_tape_read,computer_tape_write,computer_tape_image,computer_tape_now,computer_tape_barrier};
    struct fes_nano_tape_service_ops service={&provider,computer_tape_guard,computer_tape_header,computer_tape_verify};
    return fes_nano_tape_service_init(&computer_tape,service,io,owner,generation+1,identity,FES_EMBEDDED_ROM_TICK_HZ);
}
/* Called by the actual phase1 computer policy, under the existing SiP lock.
 * A selected uncertain transfer returns -1 so that policy retires/contains.
 * Foreign requests and replay do no supplier I/O and cannot reset the object. */
int fes_nano_platform_computer_tape(uint64_t operation,uint64_t owner,uint64_t generation,uint64_t result[4])
{
    unsigned used=computer_tape.used;
    fes_nano_tape_service_call(&computer_tape,operation,owner,generation,result);
    return !used && computer_tape.used && computer_tape.failed ? -1 : 0;
}
void fes_nano_platform_computer_tape_report(unsigned containment_attempted,int containment_status)
{
    if(!computer_tape.failed || computer_tape_reported)return;
    computer_tape_reported=1;
    const struct fes_nano_tape *e=&computer_tape.engine;
    if(computer_tape.failed)
        NOTICE("FES_NANO_TAPE_CONTAINMENT owner=%llx generation=%llu attempted=%u status=%d\n",
            (unsigned long long)e->owner,(unsigned long long)e->generation,containment_attempted,containment_status);
    static const char *const names[]={"guard","owns","read","write","barrier","image","hash"};
    for(unsigned i=0;i<TAPE_COST_COUNT;i++)
        NOTICE("FES_NANO_TAPE_COST owner=%llx generation=%llu kind=%s calls=%u samples=%u period=%u ticks=%llx max=%llx clock_invalid=%u complete=%u hz=%u\n",
            (unsigned long long)e->owner,(unsigned long long)e->generation,names[i],tape_cost[i].calls,
            tape_cost[i].samples,tape_cost_mask(i)+1,(unsigned long long)tape_cost[i].ticks,
            (unsigned long long)tape_cost[i].max,tape_cost[i].clock_invalid,computer_tape.complete,e->hz);
    static const char *const parts[]={"before","usb","registers","after"};
    for(unsigned i=0;i<TAPE_OWNS_PARTS;i++)
        NOTICE("FES_NANO_TAPE_OWNERSHIP_COST owner=%llx generation=%llu kind=%s samples=%u period=64 ticks=%llx max=%llx clock_invalid=%u complete=%u hz=%u parent_calls=%u parent_samples=%u\n",
            (unsigned long long)e->owner,(unsigned long long)e->generation,parts[i],tape_owns_cost[i].samples,
            (unsigned long long)tape_owns_cost[i].ticks,(unsigned long long)tape_owns_cost[i].max,
            tape_owns_cost[i].clock_invalid,computer_tape.complete,e->hz,
            tape_cost[TAPE_COST_OWNS].calls,tape_cost[TAPE_COST_OWNS].samples);
    /* Emit the terminal failure marker last: collectors stop at that marker. */
    NOTICE("FES_NANO_TAPE_FAILURE error=%d owner=%llx generation=%llu bytes=%u opcode=%u index=%u offset=%x reads=%u writes=%u frame_phase=%u next=%u frame_failed=%u complete=%u began=%llx tick=%llx hz=%u\n",
        computer_tape.first_error,(unsigned long long)e->owner,(unsigned long long)e->generation,e->bytes,
        e->opcode,e->index,e->offset,e->reads,e->writes,computer_tape_frame.phase,computer_tape_frame.next,
        computer_tape_frame.failed,computer_tape.complete,(unsigned long long)e->began,(unsigned long long)e->last,e->hz);
}
