/* SPDX-License-Identifier: BSD-3-Clause */
/* Included by the strong cold provider, after its configuration objects and
 * pinned SHA implementation. This is opt-in until a matched producer exports
 * the fixed ROM aperture and held execution conduit. */
#if !defined(FES_NANO_COLD_COMPUTER) || !FES_NANO_COLD_COMPUTER || !defined(FES_NANO_ROM_CPU_BASE)
#error ROM_requires_the_matched_computer_and_fixed_aperture
#endif
#include "nano_rom.h"
static struct fes_nano_rom computer_rom;
/* Cached observations only. Reporting happens after the existing containment
 * callback and must never query an uncertain endpoint or ownership scope. */
static struct {
    const char *scope;
    uint64_t tick, owner_started, owner_ticks, owner_max;
    uint32_t hz, reads, writes, read_offset, read_value, write_offset, owner_checks, fast_checks;
    unsigned pending, reported, phase, owner_pending, owner_invalid;
    int result;
} computer_rom_diagnostic;
#if defined(FES_NANO_SHARED_USB) && FES_NANO_SHARED_USB
/* A private, non-yielding protocol step under the existing secure lock. */
static struct {
    uint64_t owner,generation,usb_generation;
    uint32_t next,offset,phase,polls; /* phase: status, received, written, four words, done */
    unsigned active,failed;
} computer_rom_step;
#endif

static int computer_rom_scope_failure(const char *scope)
{ computer_rom_diagnostic.scope=scope;return 0; }

#if defined(FES_NANO_SHARED_USB) && FES_NANO_SHARED_USB
static int computer_rom_step_guard(uint64_t owner,uint64_t generation)
{
    computer_rom_diagnostic.fast_checks++;
    struct fes_nano_bridges *b=&fes_nano_owned_bridges;
    if(computer_rom_step.active!=1 || computer_rom_step.failed || computer_rom_step.owner!=owner ||
       computer_rom_step.generation!=generation || !computer_rom.initialized ||
       !computer_rom.busy || !computer_rom.used || computer_rom.failed || computer_rom.complete ||
       computer_rom.owner!=owner || computer_rom.generation!=generation ||
       computer_rom.bytes!=configuration_program.rom_bytes || computer_rom_step.phase>7 ||
       (computer_rom_step.offset&15U) || computer_rom_step.offset!=computer_rom_step.next ||
       computer_rom_step.offset>=computer_rom.bytes || computer_rom.bytes-computer_rom_step.offset<16 ||
       usb_platform.active!=1 || usb_platform.confirmed!=1 ||
       usb_platform.generation!=computer_rom_step.usb_generation ||
       b->state.owner!=owner || b->state.generation!=generation ||
       b->state.reset!=FES_NANO_BRIDGES_DATA || !b->state.masters_contained)
        return computer_rom_scope_failure("step");
    struct fes_nano_usb_shared_ops policy=fes_nano_usb_master_service_ops(&usb_master);
    if(!policy.valid || policy.valid(policy.context,b,&usb_platform)!=1)
        return computer_rom_scope_failure("step");
    /* No NS execution or other HPS CPU can run inside this bounded step.
     * Complete cold/USB/clock proofs frame it. Independently guard every
     * endpoint access against CPU/reset/bridge drift within that frame. */
    dsbsy();
    uint32_t reset=mmio_read_32(FES_NANO_COLD_RSTMGR+0x2c);
    uint32_t per0=mmio_read_32(FES_NANO_COLD_RSTMGR+0x24);
    dsbsy();
    if((reset&FES_NANO_BRIDGES_ALL)!=FES_NANO_BRIDGES_DATA ||
       (per0&FES_NANO_MASTER_RESET)!=(FES_NANO_MASTER_RESET&~FES_NANO_COLD_USB_RESET) ||
       !cold_master_cpu_scope())return computer_rom_scope_failure("step");
    return 1;
}
#endif
static int computer_rom_owned(void *context,uint64_t owner,uint64_t generation)
{
    struct fes_nano_bridge_snapshot s;
    struct fes_nano_configuration *c=&configuration;
    struct fes_nano_identity_probe *p=&identity_probe;
    if(computer_rom_diagnostic.phase==2) {
        computer_rom_diagnostic.owner_checks++;
        computer_rom_diagnostic.owner_started=computer_rom_diagnostic.tick;
        computer_rom_diagnostic.owner_pending=1;
    }
    if(context!=&provider || !cold_master_cpu_scope())return computer_rom_scope_failure("cpu");
    computer_rom_diagnostic.hz=read_cntfrq_el0();
    if(computer_rom_diagnostic.hz!=FES_EMBEDDED_ROM_TICK_HZ)return computer_rom_scope_failure("timer");
    if(provider.control_expected!=1 || provider.ddr_expected)
        return computer_rom_scope_failure("cold");
#if defined(FES_NANO_SHARED_USB) && FES_NANO_SHARED_USB
    /* Only the accepted private shared-boot binding may supply the complete
     * live cold/USB/master proof through bridges_observe below. Do not repeat
     * that same full proof immediately here, or accept a substituted observer.
     * Every invocation still takes its own fresh observation; nothing is
     * cached across ROM accesses or across their before/after brackets. */
    if(!provider.valid || provider.failed || provider.record.boot.owner!=owner ||
       provider.record.boot.epoch!=fes_nano_owned_bridges.epoch ||
       !fes_nano_owned_bridges.observe_state ||
       !fes_nano_boot_usb_pio_matches(&fes_nano_owned_bridges,fes_nano_usb_master_ops(&usb_master)))
        return computer_rom_scope_failure("cold");
#else
    if(fes_nano_cold_owns(&provider,provider.record.boot.owner,provider.record.boot.epoch)!=1)
        return computer_rom_scope_failure("cold");
#endif
    if(!(c->initialized && c->finished && !c->failed && !c->pending && !c->issued &&
        c->completed==c->bytes && c->bytes && c->owner==owner && c->generation<UINT64_MAX &&
        c->generation+1==generation && c->bridges==&fes_nano_owned_bridges))
        return computer_rom_scope_failure("configuration");
    if(!(p->initialized && p->active && p->attempted && !p->failed && !p->first_error &&
        !p->cleanup_error && p->stage==FES_ID_OPEN && p->bridges==c->bridges))
        return computer_rom_scope_failure("identity");
    if(!(configuration_program.bound && !configuration_program.failed &&
        configuration_program.owner==owner && configuration_program.generation==c->generation &&
        configuration_program.rom_bytes && !c->bridges->device_lease))
        return computer_rom_scope_failure("program");
#if defined(FES_NANO_SHARED_USB) && FES_NANO_SHARED_USB
    if(computer_rom_step.active)return computer_rom_step_guard(owner,generation);
#endif
    if(fes_nano_bridges_observe(c->bridges,&s))return computer_rom_scope_failure("bridges");
    if(!(s.owner==owner && s.generation==generation &&
         s.reset==FES_NANO_BRIDGES_DATA && s.masters_contained))return computer_rom_scope_failure("snapshot");
    return 1;
}
#if defined(FES_NANO_SHARED_USB) && FES_NANO_SHARED_USB
static int computer_rom_step_begin(void *context,uint64_t owner,uint64_t generation,uint32_t offset)
{
    if(computer_rom_step.active || computer_rom_step.failed) {
        computer_rom_step.active=0;computer_rom_step.failed=1;return -1;
    }
    if(context!=&provider ||
       !computer_rom.initialized || !computer_rom.busy || !computer_rom.used ||
       computer_rom.failed || computer_rom.complete || computer_rom.owner!=owner ||
       computer_rom.generation!=generation || computer_rom.bytes!=configuration_program.rom_bytes ||
       (offset&15U) || offset!=computer_rom_step.next || offset>=computer_rom.bytes ||
       computer_rom.bytes-offset<16 || computer_rom_owned(context,owner,generation)!=1) {
        computer_rom_step.failed=1;return -1;
    }
    computer_rom_step.owner=owner;computer_rom_step.generation=generation;
    computer_rom_step.usb_generation=usb_platform.generation;
    computer_rom_step.offset=offset;computer_rom_step.phase=computer_rom_step.polls=0;
    computer_rom_step.active=1;
    return 0;
}
static int computer_rom_step_end(void *context,uint64_t owner,uint64_t generation,uint32_t offset,unsigned success)
{
    unsigned active=computer_rom_step.active;
    computer_rom_step.active=0; /* Consume before checking or reading anything. */
    if(!success) {computer_rom_step.failed=1;return 0;} /* Abort: no I/O. */
    if(success!=1 || active!=1 || computer_rom_step.failed || context!=&provider ||
       computer_rom_step.owner!=owner || computer_rom_step.generation!=generation ||
       computer_rom_step.offset!=offset || computer_rom_step.phase!=7 ||
       !computer_rom_step.polls || computer_rom_step.polls>FES_EMBEDDED_ROM_MAX_POLLS ||
       !computer_rom.busy || !computer_rom.used || computer_rom.failed || computer_rom.complete ||
       computer_rom.owner!=owner || computer_rom.generation!=generation ||
       computer_rom.bytes!=configuration_program.rom_bytes || (offset&15U) ||
       offset!=computer_rom_step.next || offset>=computer_rom.bytes || computer_rom.bytes-offset<16 ||
       usb_platform.generation!=computer_rom_step.usb_generation ||
       computer_rom_owned(context,owner,generation)!=1 ||
       usb_platform.generation!=computer_rom_step.usb_generation) {
        computer_rom_step.failed=1;return -1;
    }
    computer_rom_step.next=offset+16;
    return 0;
}
#endif
static int computer_rom_read(void *context,uint32_t offset,uint32_t *value)
{
    if(context!=&provider || (offset&3U) || offset>=FES_EMBEDDED_ROM_SPAN)return -1;
#if defined(FES_NANO_SHARED_USB) && FES_NANO_SHARED_USB
    if(computer_rom_step.failed)return -1;
    if(computer_rom_step.active) {
        unsigned phase=computer_rom_step.phase;
        if(computer_rom_step.failed ||
           !((phase==0 && offset==FES_EMBEDDED_ROM_STATUS_OFFSET &&
              computer_rom_step.polls++<FES_EMBEDDED_ROM_MAX_POLLS) ||
             (phase==1 && offset==FES_EMBEDDED_ROM_RECEIVED_OFFSET) ||
             (phase==2 && offset==FES_EMBEDDED_ROM_WRITTEN_OFFSET))) {
            computer_rom_step.failed=1;return -1;
        }
    }
#endif
    computer_rom_diagnostic.reads++;
    *value=mmio_read_32(FES_NANO_ROM_CPU_BASE+offset);
    computer_rom_diagnostic.read_offset=offset;computer_rom_diagnostic.read_value=*value;
#if defined(FES_NANO_SHARED_USB) && FES_NANO_SHARED_USB
    if(computer_rom_step.failed)return -1;
    if(computer_rom_step.active) {
        if(computer_rom_step.phase==0) {
            if(*value==(FES_EMBEDDED_ROM_HELD|FES_EMBEDDED_ROM_ACTIVE|FES_EMBEDDED_ROM_READY))
                computer_rom_step.phase=1;
        } else {
            if(*value!=computer_rom_step.offset) {computer_rom_step.failed=1;return -1;}
            computer_rom_step.phase++;
        }
    }
#endif
    return 0;
}
static int computer_rom_write(void *context,uint32_t offset,uint32_t value)
{
    if(context!=&provider || (offset!=FES_EMBEDDED_ROM_EXPECTED_CRC_OFFSET &&
       offset!=FES_EMBEDDED_ROM_COMMAND_OFFSET &&
       (offset<FES_EMBEDDED_ROM_DATA_OFFSET || offset>=FES_EMBEDDED_ROM_DATA_OFFSET+16)))return -1;
#if defined(FES_NANO_SHARED_USB) && FES_NANO_SHARED_USB
    if(computer_rom_step.failed)return -1;
    if(computer_rom_step.active) {
        unsigned phase=computer_rom_step.phase;
        if(computer_rom_step.failed || phase<3 || phase>6 ||
           offset!=FES_EMBEDDED_ROM_DATA_OFFSET+4*(phase-3)) {
            computer_rom_step.failed=1;return -1;
        }
        computer_rom_step.phase++;
    }
#endif
    computer_rom_diagnostic.writes++;computer_rom_diagnostic.write_offset=offset;
    mmio_write_32(FES_NANO_ROM_CPU_BASE+offset,value);return 0;
}
static uint64_t computer_rom_now(void *context)
{
    (void)context;uint64_t tick=read_cntpct_el0();
    /* The driver's existing timer samples already bracket owns(). Account
     * only completed brackets; never add a read to resolve a failed guard. */
    if(computer_rom_diagnostic.owner_pending) {
        computer_rom_diagnostic.owner_pending=0;
        uint64_t began=computer_rom_diagnostic.owner_started;
        if(tick<began || tick-began>UINT64_MAX-computer_rom_diagnostic.owner_ticks)
            computer_rom_diagnostic.owner_invalid=1;
        else {
            uint64_t elapsed=tick-began;
            computer_rom_diagnostic.owner_ticks+=elapsed;
            if(elapsed>computer_rom_diagnostic.owner_max)computer_rom_diagnostic.owner_max=elapsed;
        }
    }
    computer_rom_diagnostic.tick=tick;return tick;
}
static void computer_rom_barrier(void *context) { (void)context;dsbsy(); }
static int computer_rom_verify(void *context,const uint8_t *image,size_t bytes)
{
    uint8_t observed[48];sha512_context hash;
    if(context!=&configuration_program || bytes!=configuration_program.rom_bytes)return -1;
    sha384_init(&hash);sha384_update(&hash,image,(uint32_t)bytes);sha384_finish(&hash,observed);
    return memcmp(observed,configuration_program.rom_header+FES_NANO_PROGRAM_ROM_SHA384,48) ? -1 : 0;
}
static int configuration_rom_descriptor(void *context,uint8_t result[FES_NANO_PROGRAM_ROM_HEADER_BYTES])
{
    if(context!=&provider || !fes_nano_platform_configuration_diagnostic_ready())return -1;
    dsbsy();
    memcpy(result,(const void *)(uintptr_t)(FES_NANO_CONFIG_BASE+FES_NANO_CONFIG_ARENA_SIZE-
        FES_NANO_PROGRAM_METADATA_BYTES+FES_NANO_PROGRAM_HEADER_BYTES),FES_NANO_PROGRAM_ROM_HEADER_BYTES);
    dsbsy();return fes_nano_platform_configuration_diagnostic_ready() ? 0 : -1;
}
static int computer_rom_prepare(uint64_t owner,uint64_t generation)
{
    if((computer_rom.initialized && (!computer_rom.complete || computer_rom.failed ||
        computer_rom.owner!=owner || computer_rom.generation>=generation)) ||
       generation==UINT64_MAX || !configuration_program.bound || configuration_program.failed ||
       configuration_program.owner!=owner || configuration_program.generation!=generation ||
       !configuration_program.rom_bytes || (FES_NANO_ROM_CPU_BASE&127U))return -1;
    uint32_t words[4];
    for(unsigned i=0;i<4;i++)words[i]=identity_probe.build[3-i];
    struct fes_nano_rom_ops io={&provider,computer_rom_owned,computer_rom_read,computer_rom_write,
                               computer_rom_now,computer_rom_barrier};
    /* The preceding activation completed and this newer configuration has
     * replaced the image. Construct its fresh object in secure static memory;
     * never put an 8 KiB image on the small EL3 stack or reset a failed load. */
    memset(&computer_rom,0,sizeof(computer_rom));
    memset(&computer_rom_diagnostic,0,sizeof(computer_rom_diagnostic));
#if defined(FES_NANO_SHARED_USB) && FES_NANO_SHARED_USB
    memset(&computer_rom_step,0,sizeof(computer_rom_step));
#endif
    return fes_nano_rom_init(&computer_rom,io,owner,generation+1,words);
}
int fes_nano_platform_computer_rom(struct fes_nano_configuration *c,uint64_t owner,uint64_t generation)
{
    computer_rom_diagnostic.phase=1;
    if(c!=&configuration || computer_rom_owned(&provider,owner,generation)!=1) {
        computer_rom_diagnostic.pending=1;computer_rom_diagnostic.result=-1;return -1;
    }
    computer_rom_diagnostic.phase=2;
#if defined(FES_NANO_SHARED_USB) && FES_NANO_SHARED_USB
    const struct fes_nano_rom_steps steps={&provider,computer_rom_step_begin,computer_rom_step_end};
    int result=fes_nano_rom_load_steps_checked(&computer_rom,
        (const uint8_t *)(uintptr_t)(FES_NANO_CONFIG_BASE+configuration_program.rom_offset),
        configuration_program.rom_bytes,configuration_program.rom_crc,
        computer_rom_verify,&configuration_program,&steps);
#else
    int result=fes_nano_rom_load_checked(&computer_rom,
        (const uint8_t *)(uintptr_t)(FES_NANO_CONFIG_BASE+configuration_program.rom_offset),
        configuration_program.rom_bytes,configuration_program.rom_crc,
        computer_rom_verify,&configuration_program);
#endif
    if(result || !computer_rom.complete) {
        computer_rom_diagnostic.pending=1;computer_rom_diagnostic.result=result;return -1;
    }
    return 0;
}
void fes_nano_platform_computer_rom_report(void)
{
    if(!computer_rom_diagnostic.pending || computer_rom_diagnostic.reported)return;
    computer_rom_diagnostic.reported=1;
    NOTICE("FES_NANO_ROM_FAILURE phase=%u scope=%s status=%d error=%d used=%u complete=%u bytes=%u reads=%u writes=%u read_offset=%x read_value=%x write_offset=%x began=%llx tick=%llx hz=%u owner_checks=%u owner_ticks=%llx owner_max=%llx owner_invalid=%u owner_pending=%u fast_checks=%u\n",
        computer_rom_diagnostic.phase,computer_rom_diagnostic.scope ? computer_rom_diagnostic.scope : "none",
        computer_rom_diagnostic.result,computer_rom.first_error,computer_rom.used,computer_rom.complete,
        computer_rom.bytes,computer_rom_diagnostic.reads,computer_rom_diagnostic.writes,
        computer_rom_diagnostic.read_offset,computer_rom_diagnostic.read_value,computer_rom_diagnostic.write_offset,
        (unsigned long long)computer_rom.began,(unsigned long long)computer_rom_diagnostic.tick,computer_rom_diagnostic.hz,
        computer_rom_diagnostic.owner_checks,(unsigned long long)computer_rom_diagnostic.owner_ticks,
        (unsigned long long)computer_rom_diagnostic.owner_max,computer_rom_diagnostic.owner_invalid,
        computer_rom_diagnostic.owner_pending,computer_rom_diagnostic.fast_checks);
}
