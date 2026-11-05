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
    uint64_t tick;
    uint32_t hz, reads, writes, read_offset, read_value, write_offset;
    unsigned pending, reported, phase;
    int result;
} computer_rom_diagnostic;

static int computer_rom_scope_failure(const char *scope)
{ computer_rom_diagnostic.scope=scope;return 0; }

static int computer_rom_owned(void *context,uint64_t owner,uint64_t generation)
{
    struct fes_nano_bridge_snapshot s;
    struct fes_nano_configuration *c=&configuration;
    struct fes_nano_identity_probe *p=&identity_probe;
    if(context!=&provider || !cold_master_cpu_scope())return computer_rom_scope_failure("cpu");
    computer_rom_diagnostic.hz=read_cntfrq_el0();
    if(computer_rom_diagnostic.hz!=FES_EMBEDDED_ROM_TICK_HZ)return computer_rom_scope_failure("timer");
    if(provider.control_expected!=1 || provider.ddr_expected ||
       fes_nano_cold_owns(&provider,provider.record.boot.owner,provider.record.boot.epoch)!=1)
        return computer_rom_scope_failure("cold");
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
    if(fes_nano_bridges_observe(c->bridges,&s))return computer_rom_scope_failure("bridges");
    if(!(s.owner==owner && s.generation==generation &&
         s.reset==FES_NANO_BRIDGES_DATA && s.masters_contained))return computer_rom_scope_failure("snapshot");
    return 1;
}
static int computer_rom_read(void *context,uint32_t offset,uint32_t *value)
{
    if(context!=&provider || (offset&3U) || offset>=FES_EMBEDDED_ROM_SPAN)return -1;
    computer_rom_diagnostic.reads++;
    *value=mmio_read_32(FES_NANO_ROM_CPU_BASE+offset);
    computer_rom_diagnostic.read_offset=offset;computer_rom_diagnostic.read_value=*value;return 0;
}
static int computer_rom_write(void *context,uint32_t offset,uint32_t value)
{
    if(context!=&provider || (offset!=FES_EMBEDDED_ROM_EXPECTED_CRC_OFFSET &&
       offset!=FES_EMBEDDED_ROM_COMMAND_OFFSET &&
       (offset<FES_EMBEDDED_ROM_DATA_OFFSET || offset>=FES_EMBEDDED_ROM_DATA_OFFSET+16)))return -1;
    computer_rom_diagnostic.writes++;computer_rom_diagnostic.write_offset=offset;
    mmio_write_32(FES_NANO_ROM_CPU_BASE+offset,value);return 0;
}
static uint64_t computer_rom_now(void *context)
{ (void)context;computer_rom_diagnostic.tick=read_cntpct_el0();return computer_rom_diagnostic.tick; }
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
    return fes_nano_rom_init(&computer_rom,io,owner,generation+1,words);
}
int fes_nano_platform_computer_rom(struct fes_nano_configuration *c,uint64_t owner,uint64_t generation)
{
    computer_rom_diagnostic.phase=1;
    if(c!=&configuration || computer_rom_owned(&provider,owner,generation)!=1) {
        computer_rom_diagnostic.pending=1;computer_rom_diagnostic.result=-1;return -1;
    }
    computer_rom_diagnostic.phase=2;
    int result=fes_nano_rom_load_checked(&computer_rom,
        (const uint8_t *)(uintptr_t)(FES_NANO_CONFIG_BASE+configuration_program.rom_offset),
        configuration_program.rom_bytes,configuration_program.rom_crc,
        computer_rom_verify,&configuration_program);
    if(result || !computer_rom.complete) {
        computer_rom_diagnostic.pending=1;computer_rom_diagnostic.result=result;return -1;
    }
    return 0;
}
void fes_nano_platform_computer_rom_report(void)
{
    if(!computer_rom_diagnostic.pending || computer_rom_diagnostic.reported)return;
    computer_rom_diagnostic.reported=1;
    NOTICE("FES_NANO_ROM_FAILURE phase=%u scope=%s status=%d error=%d used=%u complete=%u bytes=%u reads=%u writes=%u read_offset=%x read_value=%x write_offset=%x began=%llx tick=%llx hz=%u\n",
        computer_rom_diagnostic.phase,computer_rom_diagnostic.scope ? computer_rom_diagnostic.scope : "none",
        computer_rom_diagnostic.result,computer_rom.first_error,computer_rom.used,computer_rom.complete,
        computer_rom.bytes,computer_rom_diagnostic.reads,computer_rom_diagnostic.writes,
        computer_rom_diagnostic.read_offset,computer_rom_diagnostic.read_value,computer_rom_diagnostic.write_offset,
        (unsigned long long)computer_rom.began,(unsigned long long)computer_rom_diagnostic.tick,computer_rom_diagnostic.hz);
}
