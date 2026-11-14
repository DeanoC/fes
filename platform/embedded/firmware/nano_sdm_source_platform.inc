/* SPDX-License-Identifier: BSD-3-Clause */
/* Included after the actual cold provider/configuration globals. Test macros
 * replace only raw I/O/CPU instructions and linker-address observations. */
#include "probe_setup.h"
#include "input_transfer_service.h"
#include "nano_mmu_platform.h"
#if !defined(FES_NANO_SDM_SOURCE_LEASE) || FES_NANO_SDM_SOURCE_LEASE != 1
#error Explicit_SDM_source_lease_profile_required
#endif
extern unsigned char __FES_SMMU_PROBE_START__[],__FES_SMMU_PROBE_END__[];
extern unsigned char __FES_SMMU_STREAMS__[],__FES_SMMU_SDM_ROOT__[],__FES_SMMU_CMDQ__[],__FES_SMMU_EVTQ__[];
extern const uint64_t fes_nano_mmu_root[],fes_nano_mmu_ddr[];
#ifndef FES_PROBE_SYMBOL
#define FES_PROBE_SYMBOL(name) ((uintptr_t)(name))
#endif
#ifndef FES_PROBE_TABLE
#define FES_PROBE_TABLE(name,index) ((name)[index])
#endif
#ifndef FES_PROBE_READ32
#define FES_PROBE_READ32(address) mmio_read_32(address)
#define FES_PROBE_WRITE32(address,value) mmio_write_32(address,value)
#define FES_PROBE_LOAD64(address) (*(volatile const uint64_t *)(uintptr_t)(address))
#define FES_PROBE_STORE64(address,value) (*(volatile uint64_t *)(uintptr_t)(address)=(value))
#define FES_PROBE_COUNTER() read_cntpct_el0()
#define FES_PROBE_FREQUENCY() read_cntfrq_el0()
#define FES_PROBE_BARRIER() dsbsy()
#define FES_PROBE_CPU() (GET_EL(read_current_el())==MODE_EL3 && FES_COLD_CPU_MODE() && \
    fes_nano_cold_cpu_release_locked()==1 && captured.core==MPIDR_AFFLVL1_VAL(read_mpidr()))
#define FES_PROBE_WAIT() wfi()
#endif
static unsigned sdm_source_slot;
static struct nano_probe_setup sdm_arm_probes[2];
#define sdm_arm_probe sdm_arm_probes[sdm_source_slot]
static struct { unsigned admitted,previous_slot; uint64_t previous_generation,generation,returned; } sdm_rearm;
static int sdm_source_rearm_begin(void);
static int sdm_source_rearm_returned(uint64_t,uint64_t);
static int sdm_source_secure_scope(void);
static const struct nano_retire *sdm_source_previous_retirement(void);
static void sdm_source_rearm_prepared(void);
static int sdm_source_rearm_scope(void *,const struct nano_retire *,const struct nano_sdm_input_binding *);
static unsigned sdm_arm_entered,sdm_probe_scope_reason,sdm_input_phase;
static unsigned sdm_source_failed;
static unsigned sdm_source_released;
static int sdm_release_clock(void);
static uint32_t sdm_source_bad_offset,sdm_source_bad_value;
static unsigned sdm_source_clock_started;
static uint64_t sdm_source_clock_start,sdm_source_clock_last;
static int sdm_source_time(void *,uint64_t *);
#ifndef FES_PROBE_USB_BINDING_VALID
#define FES_PROBE_USB_BINDING_VALID() fes_nano_usb_master_service_valid(&usb_master,&fes_nano_owned_bridges,&usb_platform)
#endif
#ifndef FES_PROBE_SD_IO_BINDING_VALID
#define FES_PROBE_SD_IO_BINDING_VALID() (sd_platform.cold==&provider && !sd_platform.io.context && \
 sd_platform.io.read==sd_read && sd_platform.io.write==sd_write && \
 sd_platform.io.barrier==sd_barrier && sd_platform.io.wait==sd_wait)
#endif
static int sdm_probe_reject(unsigned reason)
{ sdm_probe_scope_reason=reason;return 0; }
static int sdm_probe_geometry(void)
{
    for(unsigned n=0;n<4;n++)if(FES_PROBE_TABLE(fes_nano_mmu_ddr,164+n)!=
        (UINT64_C(0x0040000094800669)+(uint64_t)n*0x200000))return 0;

    return FES_PROBE_SYMBOL(__FES_SMMU_PROBE_START__)==NANO_PROBE_BASE &&
        FES_PROBE_SYMBOL(__FES_SMMU_STREAMS__)==NANO_PROBE_BASE &&
        FES_PROBE_SYMBOL(__FES_SMMU_PROBE_END__)==NANO_PROBE_BASE+NANO_PROBE_BYTES &&
        FES_PROBE_SYMBOL(__FES_SMMU_SDM_ROOT__)==NANO_PROBE_BASE+NANO_PROBE_ROOT &&
        FES_PROBE_SYMBOL(__FES_SMMU_CMDQ__)==NANO_PROBE_BASE+NANO_PROBE_CMDQ &&
        FES_PROBE_SYMBOL(__FES_SMMU_EVTQ__)==NANO_PROBE_BASE+NANO_PROBE_EVTQ &&
        FES_NANO_PROBE_HEAP_FIRST==UINT64_C(0x84000000) &&
        FES_NANO_PROBE_HEAP_LAST==NANO_PROBE_BASE &&
        FES_NANO_CONFIG_BASE==UINT64_C(0x94800000) &&
        FES_NANO_CONFIG_ARENA_SIZE==(8U<<20) &&
        FES_NANO_READER_CPU_BASE==UINT64_C(0x95000000) &&
        FES_PROBE_TABLE(fes_nano_mmu_root,2)==(FES_PROBE_SYMBOL(fes_nano_mmu_ddr)|3U) &&
        FES_PROBE_TABLE(fes_nano_mmu_ddr,160)==UINT64_C(0x0040000094000669);
}
static int sdm_release_returned(const struct nano_probe_binding *binding)
{
#if defined(FES_NANO_SDM_SOURCE_RELEASE) && FES_NANO_SDM_SOURCE_RELEASE == 1
 struct fes_nano_sd *s=&fes_nano_owned_sd;uint64_t returned=0;
 struct fes_nano_sd_ops expected=fes_nano_sd_platform_ops(&sd_platform);
 unsigned menu=configuration_program.role==FES_NANO_PROGRAM_ROLE_MENU;
 const struct fes_nano_fabric_progress *last=&fabric_stop.last_observed;
 unsigned session=menu ? fes_nano_platform_window_return(&returned)==1 :
  configuration_program.role==FES_NANO_PROGRAM_ROLE_COMPUTER &&
  identity_probe.initialized && identity_probe.attempted && !identity_probe.active && !identity_probe.failed &&
  !identity_probe.first_error && !identity_probe.cleanup_error && identity_probe.stage==FES_ID_COMPLETE &&
  !identity_probe.reader && identity_probe.abi==FES_NANO_PROGRAM_COMPUTER_ABI &&
  identity_probe.bridges==&fes_nano_owned_bridges && identity_probe.io.context==&provider && fabric_stop.qualified==1 &&
  fabric_stop.attempted==1 && fabric_stop.done==1 && !fabric_stop.failed && !fabric_stop.first_error &&
  fabric_stop.probe.done==1 && !fabric_stop.probe.failed && last->generation &&
  last->generation==last->request_generation && last->generation==last->ack_generation &&
  last->cookie && last->cookie==last->ack_cookie && last->status==FES_EMBEDDED_STATUS_DRAINED && !last->pending &&
  fes_nano_bridges_held_return(&fes_nano_owned_bridges,&returned)==1;
 return binding && !sdm_source_released && FES_PROBE_SD_IO_BINDING_VALID() && configuration.source_release_attempted==1 &&
  !configuration.source_released && configuration.source_release_source==binding->generation &&
  configuration.source_release_generation==fes_nano_owned_bridges.state.generation &&
  session && binding->generation<=UINT64_MAX-(menu ? 3U:2U) &&
  returned==binding->generation+(menu ? 3U:2U) && fabric_generation!=UINT64_MAX &&
  returned==fabric_generation+1 && returned<=UINT64_MAX-3 &&
  configuration.source_release_generation==returned+3 && s->initialized && !s->failed &&
  !s->active && !s->prepared && s->bridges==&fes_nano_owned_bridges &&
  s->owner==binding->owner && s->epoch==binding->epoch && s->grant_generation==returned &&
  s->return_generation==returned && s->stopped_generation==configuration.source_release_generation &&
  s->ops.context==expected.context && s->ops.scope==expected.scope && s->ops.resume==expected.resume &&
  s->ops.prepare==expected.prepare && s->ops.stop==expected.stop && s->ops.returned==window_sd_return &&
  !fes_nano_sd_returned(s,binding->owner,returned,configuration.source_release_generation);
#else
 (void)binding;return 0;
#endif
}
static int sdm_probe_owns_mode(void *context,const struct nano_probe_binding *binding,unsigned completed)
{
    struct fes_nano_bridges *b=&fes_nano_owned_bridges;
    sdm_probe_scope_reason=0;
    if(context!=&sdm_arm_probe || !binding || completed>3 || sdm_source_released)return sdm_probe_reject(1);
    if(!FES_PROBE_CPU() || FES_PROBE_FREQUENCY()!=400000000U)return sdm_probe_reject(2);
    if(!fes_nano_mmu_platform_scope())return sdm_probe_reject(3);
    if(sdm_rearm.admitted && sdm_source_secure_scope()!=1)return sdm_probe_reject(37);
    if(!sdm_probe_geometry())return sdm_probe_reject(4);
    if(!provider.valid || provider.failed ||
       binding->owner!=provider.record.boot.owner || binding->epoch!=provider.record.boot.epoch ||
       provider.control_expected!=(completed==2) || provider.ddr_expected)return sdm_probe_reject(5);
    if(!b->initialized || b->failed || b->device_lease || b->epoch!=binding->epoch ||
       b->state.owner!=binding->owner ||
       b->state.generation!=(completed==3 ? configuration.source_release_generation:binding->generation+(completed==2)) ||
       b->state.reset!=(completed==2 ? 13U:15U) || b->state.masters_contained!=1 || b->state.never_enabled!=(completed<2 && !sdm_rearm.admitted))
        return sdm_probe_reject(6);
    if(!provider.devices_attempted || !provider.devices_bound ||
       !sd_platform.initialized || sd_platform.failed || sd_platform.active || sd_platform.prepared)
        return sdm_probe_reject(7);
    if(!usb_platform.initialized || usb_platform.failed ||
       usb_platform.active>1 || usb_platform.confirmed>1 || usb_platform.active!=usb_platform.confirmed ||
       (usb_platform.active && usb_platform.generation!=2) ||
       !usb_composition.initialized || usb_composition.failed || !FES_PROBE_USB_BINDING_VALID())
        return sdm_probe_reject(8);
    if(fes_nano_owned_mac.attempted || fes_nano_owned_mac.initialized || fes_nano_owned_mac.failed)
        return sdm_probe_reject(7);
    if(!configuration.initialized || configuration.failed || !configuration.prepared ||
       !configuration.started || configuration.finished!=(completed!=0) || configuration.rearmed!=sdm_rearm.admitted ||
       configuration.bridges!=b ||
       configuration.owner!=binding->owner || configuration.generation!=binding->generation ||
       !configuration.bytes || !configuration.chunk || (configuration.chunk&63U) ||
       configuration.bytes>configuration.maximum || configuration.completed>configuration.bytes ||
       configuration.pending>configuration.bytes-configuration.completed ||
       configuration.pending>(uint64_t)configuration.chunk*FES_NANO_CONFIG_BUFFERS ||
       (configuration.completed!=configuration.bytes && configuration.completed%configuration.chunk) ||
       (configuration.completed+configuration.pending!=configuration.bytes && configuration.pending%configuration.chunk) ||
       !((completed && !configuration.issued && configuration.stage==FES_CONFIG_COMPLETE &&
          !configuration.pending && configuration.completed==configuration.bytes) ||
         (!completed && ((configuration.issued==FES_NANO_CONFIG_START && configuration.stage==FES_CONFIG_START &&
          !configuration.completed && !configuration.pending) ||
         (sdm_input_phase==2 && (
          ((configuration.issued==FES_NANO_CONFIG_WRITE && configuration.stage==FES_CONFIG_WRITE) ||
           (configuration.issued==FES_NANO_CONFIG_COMPLETED && configuration.stage==FES_CONFIG_POLL)) ?
            configuration.pending!=0 :
          (configuration.issued==FES_NANO_CONFIG_ISDONE && configuration.stage==FES_CONFIG_STATUS &&
           !configuration.pending && configuration.completed==configuration.bytes)))))) ||
       configuration.base!=FES_NANO_CONFIG_BASE)return sdm_probe_reject(9);
    if(!configuration_program.initialized || !configuration_program.bound || configuration_program.failed ||
       configuration_program.owner!=binding->owner || configuration_program.generation!=binding->generation ||
       configuration_program.bytes!=configuration.bytes || configuration_program.chunk!=configuration.chunk ||
       configuration_program.mixed_roles!=1 ||
       !((configuration_program.role==FES_NANO_PROGRAM_ROLE_MENU &&
          configuration_program.control_abi==FES_NANO_PROGRAM_MENU_ABI && !configuration_program.rom_bytes) ||
         (sdm_rearm.admitted && configuration_program.role==FES_NANO_PROGRAM_ROLE_COMPUTER &&
          configuration_program.control_abi==FES_NANO_PROGRAM_COMPUTER_ABI &&
          (configuration_program.rom_bytes==4096 || configuration_program.rom_bytes==8192))))return sdm_probe_reject(10);
    if(completed==3){
       if(fabric_configuration_generation!=binding->generation || !sdm_release_returned(binding))
           return sdm_probe_reject(34);
    }else if(!completed && sdm_rearm.admitted){
       if(fabric_configuration_generation!=sdm_rearm.previous_generation ||
          !fabric_generation || fabric_generation==UINT64_MAX ||
          fabric_generation+1!=sdm_rearm.returned ||
          !fabric_stop.qualified || !fabric_stop.attempted || !fabric_stop.done || fabric_stop.failed ||
          fabric_stop.first_error || !fabric_stop.probe.done || fabric_stop.probe.failed ||
          !identity_probe.initialized || !identity_probe.attempted || identity_probe.active ||
          identity_probe.failed || identity_probe.first_error || identity_probe.cleanup_error ||
          identity_probe.stage!=FES_ID_COMPLETE || identity_probe.bridges!=b ||
          !sdm_source_rearm_returned(binding->owner,binding->generation))return sdm_probe_reject(36);
    }else if(fabric_configuration_generation!=(completed ? binding->generation:0) ||
       fabric_generation!=(completed==2 ? binding->generation+1:0) ||
       fabric_stop.attempted || fabric_stop.failed ||
       (completed && (fabric_stop.qualified!=1 || fabric_stop.done || fabric_stop.first_error)))
        return sdm_probe_reject(11);
    if(completed==2 && (!identity_probe.initialized || !identity_probe.attempted || identity_probe.active!=1 ||
       identity_probe.failed || identity_probe.first_error || identity_probe.cleanup_error ||
       identity_probe.bridges!=b || identity_probe.reader!=FES_NANO_READER_WINDOW ||
       identity_probe.abi!=0x10203043U || identity_probe.io.context!=&provider ||
       !((identity_probe.stage==FES_ID_DDR_PREPARE && !window_prepared && !window_issued) ||
         (identity_probe.stage==FES_ID_DDR_RELEASE && !window_prepared && window_issued==1))))
        return sdm_probe_reject(33);
    /* Cold ownership calls the pinned shared-device containment callback for
     * active USB: DMA/IRQ disabled and AHB master idle under stable clocks,
     * CPU/reset/owner and generation. Ready flags alone never qualify it.
     * That callback performs read-only checks; it never stops or reclaims USB. */
    if(fes_nano_cold_owns(&provider,binding->owner,binding->epoch)!=1)return sdm_probe_reject(12);
    FES_PROBE_BARRIER();
    unsigned route=FES_PROBE_READ32(0x10d12184)==3 && FES_PROBE_READ32(0x10d121b0)==0xa000a &&
        FES_PROBE_READ32(0x10d1225c)==0 &&
        FES_PROBE_READ32(0x10d12280)==(sdm_input_phase==2 || sdm_rearm.admitted ? 0x80000000U:0U) &&
        FES_PROBE_READ32(0x10d12284)==(sdm_input_phase==2 || sdm_rearm.admitted ? 0x80000000U:0U);
    FES_PROBE_BARRIER();
    if(!route)return sdm_probe_reject(13);
    if(fes_nano_cold_owns(&provider,binding->owner,binding->epoch)!=1)return sdm_probe_reject(12);
    return FES_PROBE_CPU() && fes_nano_mmu_platform_scope() ? 1 : sdm_probe_reject(14);
}
static int sdm_probe_owns(void *context,const struct nano_probe_binding *binding)
{return sdm_probe_owns_mode(context,binding,0);}
static int sdm_probe_time(void *context,uint64_t *value)
{
    if (context!=&sdm_arm_probe || !value || FES_PROBE_FREQUENCY()!=400000000U) return -1;
    *value=FES_PROBE_COUNTER()/400U;return 0;
}
static int sdm_probe_read(void *context,uint32_t offset,uint32_t *value)
{
    if(context!=&sdm_arm_probe || !value)return -1;
    switch(offset) {
    case 0:case 4:case 0x14:case 0x20:case 0x24:case 0x28:case 0x2c:
    case 0x44:case 0x48:case 0x50:case 0x54:case 0x60:case 0x64:
    case 0x80:case 0x84:case 0x88:case 0x90:case 0x94:case 0x98:case 0x9c:
    case 0xa0:case 0xa4:case 0x100a8:case 0x100ac:break;
    default:return -1;
    }
    *value=FES_PROBE_READ32(UINT64_C(0x16000000)+offset);return 0;
}
static int sdm_probe_write(void *context,uint32_t offset,uint32_t value)
{
    if(context!=&sdm_arm_probe || sdm_arm_probe.failed)return -1;
    if(sdm_arm_probe.warm && offset!=0x20 && offset!=0x98)return -1;
    uint32_t expected=0;
    switch(offset) {
    case 0x20:if(value!=8 && value!=12 && value!=13)return -1;expected=value;break;
    case 0x28:expected=0x820;break;
    case 0x80:expected=0x94000000;break;
    case 0x84:case 0x94:case 0xa4:break;
    case 0x88:expected=8;break;
    case 0x90:expected=0x94008003;break;
    case 0x98:expected=sdm_arm_probe.queue_target;break;
    case 0xa0:expected=0x94009003;break;
    default:return -1;
    }
    if(value!=expected)return -1;
    FES_PROBE_WRITE32(UINT64_C(0x16000000)+offset,value);return 0;
}
static int sdm_probe_range(uint32_t offset,uint32_t bytes)
{
    return !(offset&7) && !(bytes&7) && bytes &&
        (nano_sdm_input_table_range(offset,bytes) ||
         (offset>=NANO_PROBE_CMDQ && offset<NANO_PROBE_CMDQ+128 && bytes<=NANO_PROBE_CMDQ+128-offset) ||
         (offset>=NANO_PROBE_EVTQ && offset<NANO_PROBE_EVTQ+256 && bytes<=NANO_PROBE_EVTQ+256-offset));
}
static int sdm_probe_clear(void *context,uint32_t offset,uint32_t bytes)
{
    if(context!=&sdm_arm_probe || sdm_arm_probe.warm || sdm_arm_probe.writes || !sdm_arm_probe.preparing || sdm_arm_probe.prepared ||
        sdm_arm_probe.arming || sdm_arm_probe.armed || sdm_arm_probe.failed || !((offset==0 && bytes==0x8000) || (offset==0xa000 && bytes==0x2000) ||
        (offset==NANO_PROBE_CMDQ && bytes==128) || (offset==NANO_PROBE_EVTQ && bytes==256)))return -1;
    for (uint32_t n=0;n<bytes;n+=8) FES_PROBE_STORE64(NANO_PROBE_BASE+offset+n,0);
    return 0;
}
static int sdm_probe_store(void *context,uint32_t offset,uint64_t value)
{
    if(context!=&sdm_arm_probe || (offset&7U) || offset<NANO_PROBE_CMDQ || offset>=NANO_PROBE_CMDQ+128 ||
        !sdm_arm_probe.preparing || sdm_arm_probe.prepared || sdm_arm_probe.arming || sdm_arm_probe.armed || sdm_arm_probe.failed ||
        !sdm_arm_probe.command.initialized || !sdm_arm_probe.command.submitted || sdm_arm_probe.command.publish_attempted ||
        !sdm_arm_probe.command.slots_attempted || sdm_arm_probe.command.slots_attempted>6 ||
        (offset-NANO_PROBE_CMDQ)/16!=((sdm_arm_probe.queue_baseline+sdm_arm_probe.command.slots_attempted-1)&7U))return -1;
    FES_PROBE_STORE64(NANO_PROBE_BASE+offset,value);return 0;
}
static int sdm_probe_load(void *context,uint32_t offset,uint64_t *value)
{
    if(context!=&sdm_arm_probe || !value || !sdm_probe_range(offset,8))return -1;
    *value=FES_PROBE_LOAD64(NANO_PROBE_BASE+offset);return 0;
}
/* Bounded unpublished private-memory callbacks, under the same secure lock.
 * The portable caller validates the entire resulting table before publication.
 * MMIO operations and active queue writes keep their existing individual guards. */
static int sdm_probe_store_block(void *context,uint32_t offset,const uint64_t *values,unsigned words)
{
    if(context!=&sdm_arm_probe || !values || !words || words>NANO_PROBE_MEMORY_WORDS ||
       !nano_sdm_input_table_range(offset,words*8) || sdm_arm_probe.writes ||
       !sdm_arm_probe.preparing || sdm_arm_probe.prepared || sdm_arm_probe.arming || sdm_arm_probe.armed || sdm_arm_probe.failed) return -1;
    for(unsigned n=0;n<words;n++)FES_PROBE_STORE64(NANO_PROBE_BASE+offset+n*8,values[n]);
    return 0;
}
static int sdm_probe_load_block(void *context,uint32_t offset,uint64_t *values,unsigned words)
{
    if(context!=&sdm_arm_probe || !values || !words || words>NANO_PROBE_MEMORY_WORDS ||
       !sdm_probe_range(offset,words*8) || sdm_arm_probe.writes || !sdm_arm_probe.preparing || sdm_arm_probe.prepared ||
       sdm_arm_probe.arming || sdm_arm_probe.armed || sdm_arm_probe.failed) return -1;
    for(unsigned n=0;n<words;n++)values[n]=FES_PROBE_LOAD64(NANO_PROBE_BASE+offset+n*8);
    return 0;
}
static void sdm_probe_barrier(void *context) {(void)context;FES_PROBE_BARRIER();}
static int sdm_input_owns(void *,const struct nano_probe_binding *);
static int sdm_input_source(void *,const struct nano_sdm_input_binding *);
static int sdm_input_callbacks(void)
{
 const struct nano_probe_io *p=&sdm_arm_probe.io;
 return p->context==&sdm_arm_probe && p->owns==sdm_input_owns && p->time_us==sdm_source_time &&
  p->read32==sdm_probe_read && p->write32==sdm_probe_write && p->clear==sdm_probe_clear &&
  p->store64==sdm_probe_store && p->load64==sdm_probe_load && p->store_block==sdm_probe_store_block &&
  p->load_block==sdm_probe_load_block && p->barrier==sdm_probe_barrier &&
  sdm_arm_probe.input_selected==1 && sdm_arm_probe.input_scope==sdm_input_source;
}
static int sdm_input_owns(void *context,const struct nano_probe_binding *binding)
{
 if(context!=&sdm_arm_probe || !sdm_input_callbacks())return sdm_probe_reject(19);
 if(sdm_probe_owns(context,binding)!=1)return 0;
 return sdm_input_callbacks() ? 1:sdm_probe_reject(19);
}
static int sdm_input_source(void *context,const struct nano_sdm_input_binding *b)
{
 if(context!=&sdm_arm_probe || b!=&sdm_arm_probe.input || !sdm_input_callbacks() || nano_sdm_input_validate(b) ||
    b->owner!=provider.record.boot.owner || b->epoch!=provider.record.boot.epoch ||
    b->generation!=configuration.generation || b->owner!=configuration_program.owner ||
    b->generation!=configuration_program.generation || b->bytes!=configuration.bytes ||
    b->bytes!=configuration_program.bytes || b->chunk!=configuration.chunk || b->chunk!=configuration_program.chunk ||
    memcmp(b->digest384,configuration_program.header+FES_NANO_PROGRAM_SHA384,48))return sdm_probe_reject(20);
 return 1;
}
/* A source lease is scoped to one cold configuration. No buffer/map retirement
 * or post-success/failure return-to-bypass operation is supplied here. */
static int sdm_source_stop(unsigned reason)
{
 if(!sdm_source_failed){sdm_source_failed=1;if(!sdm_probe_scope_reason)sdm_probe_reject(reason);}
 return 0;
}
static int sdm_source_time(void *context,uint64_t *output)
{
 uint64_t now=0;
 if(sdm_source_failed || !sdm_source_clock_started || !output ||
    sdm_probe_time(context,&now) || now<sdm_source_clock_last ||
    now-sdm_source_clock_start>=NANO_SDM_TRANSFER_US)return sdm_source_stop(31),-13;
 sdm_source_clock_last=now;*output=now;return 0;
}
static int sdm_source_queues(void)
{
 if(sdm_source_failed || !sdm_arm_probe.armed || sdm_arm_probe.failed ||
    !sdm_arm_probe.attempted || !sdm_arm_probe.initialized || !sdm_arm_probe.prepared ||
    sdm_arm_probe.preparing!=1 || sdm_arm_probe.arming!=1 ||
    !sdm_input_callbacks() || sdm_arm_probe.command_receipt.producer!=sdm_arm_probe.queue_target ||
    sdm_arm_probe.command_receipt.consumer!=sdm_arm_probe.queue_target || !sdm_arm_probe.command_receipt.commands_completed)
  return sdm_source_stop(21);
 const uint32_t registers[][2]={
  {0,0x080f7e3f},{4,0x0e739d18},{0x14,0x00400075},{0x20,13},{0x24,13},{0x28,0x820},{0x2c,4},
  {0x44,sdm_arm_probe.abort},{0x48,0},{0x50,0},{0x54,0},{0x60,0},{0x64,0},
  {0x80,0x94000000},{0x84,0},{0x88,8},{0x90,0x94008003},{0x94,0},{0x98,sdm_arm_probe.queue_target},{0x9c,sdm_arm_probe.queue_target},
  {0xa0,0x94009003},{0xa4,0},{0x100a8,0},{0x100ac,0}
 };
 for(unsigned n=0;n<sizeof(registers)/sizeof(registers[0]);n++){
  FES_PROBE_BARRIER();uint32_t actual=FES_PROBE_READ32(UINT64_C(0x16000000)+registers[n][0]);FES_PROBE_BARRIER();
  if(actual!=registers[n][1]){
   sdm_source_bad_offset=registers[n][0];sdm_source_bad_value=actual;return sdm_source_stop(22);
  }
 }
 /* Secure CPU/configuration and other-writer ownership is also checked on
  * both sides of each bounded block by source_validate below. No command,
  * table, error acknowledgement or reset writes occur in this callback. */
 return 1;
}
static int sdm_source_owns(void *context,const struct nano_sdm_input_binding *binding)
{
 if(sdm_source_failed)return 0;
 if(context!=&sdm_arm_probe || !binding || (sdm_input_phase!=1 && sdm_input_phase!=2) ||
    !nano_sdm_input_same(binding,&sdm_arm_probe.input))return sdm_source_stop(23);
 if(sdm_input_owns(&sdm_arm_probe,&sdm_arm_probe.binding)!=1 ||
    sdm_input_source(&sdm_arm_probe,&sdm_arm_probe.input)!=1 || !sdm_source_queues())return sdm_source_stop(24);
 if(sdm_input_owns(&sdm_arm_probe,&sdm_arm_probe.binding)!=1 ||
    sdm_input_source(&sdm_arm_probe,&sdm_arm_probe.input)!=1 || !sdm_input_callbacks())return sdm_source_stop(24);
 return 1;
}
int fes_nano_sdm_source_validate(void)
{
 if(sdm_source_owns(&sdm_arm_probe,&sdm_arm_probe.input)!=1)return -13;
 uint64_t expected[NANO_PROBE_MEMORY_WORDS];
 for(uint32_t at=0;at<0xc000;at+=NANO_PROBE_MEMORY_WORDS*8){
  if(!nano_sdm_input_table_range(at,NANO_PROBE_MEMORY_WORDS*8))continue;
  if(sdm_source_owns(&sdm_arm_probe,&sdm_arm_probe.input)!=1 ||
     nano_sdm_input_words(&sdm_arm_probe.input,at,expected,NANO_PROBE_MEMORY_WORDS))return sdm_source_stop(29),-13;
  for(unsigned n=0;n<NANO_PROBE_MEMORY_WORDS;n++){
   FES_PROBE_BARRIER();uint64_t actual=FES_PROBE_LOAD64(NANO_PROBE_BASE+at+n*8);FES_PROBE_BARRIER();
   if(actual!=expected[n]){sdm_source_bad_offset=at+n*8;sdm_source_bad_value=(uint32_t)actual;return sdm_source_stop(30),-13;}
  }
  if(sdm_source_owns(&sdm_arm_probe,&sdm_arm_probe.input)!=1)return -13;
 }
 return sdm_source_owns(&sdm_arm_probe,&sdm_arm_probe.input)==1 ? 0:-13;
}
/* Observe only already-retained failure state. No MMIO, counter sample,
 * ownership callback or retry occurs here, including after a lost scope.
 * stage: 1=initialization, 2=preparation, 3=arming. Queue failure remains
 * distinct from the enclosing probe error, which may have mapped it to -5. */
__attribute__((noinline)) static void sdm_source_setup_failure(unsigned stage,int status)
{
 (void)stage;(void)status;
 NOTICE("FES_NANO_SOURCE_SETUP build=%s stage=%u status=%d owner=%llx epoch=%llx generation=%llu warm=%u attempted=%u initialized=%u preparing=%u prepared=%u arming=%u armed=%u failed=%u first_error=%d timed=%u start_us=%llu last_us=%llu reads=%u writes=%u clears=%u stores=%u loads=%u ack_polls=%u kind=%u offset=%x value=%llx callback=%d scope_reason=%u retained=1 grant=0\n",
  FES_NANO_INPUT_BUILD,stage,status,
  (unsigned long long)sdm_arm_probe.binding.owner,(unsigned long long)sdm_arm_probe.binding.epoch,
  (unsigned long long)sdm_arm_probe.binding.generation,sdm_arm_probe.warm,
  sdm_arm_probe.attempted,sdm_arm_probe.initialized,sdm_arm_probe.preparing,sdm_arm_probe.prepared,
  sdm_arm_probe.arming,sdm_arm_probe.armed,sdm_arm_probe.failed,sdm_arm_probe.first_error,
  sdm_arm_probe.timed,(unsigned long long)sdm_arm_probe.start,(unsigned long long)sdm_arm_probe.last,
  sdm_arm_probe.reads,sdm_arm_probe.writes,sdm_arm_probe.clears,sdm_arm_probe.stores,
  sdm_arm_probe.loads,sdm_arm_probe.ack_polls,sdm_arm_probe.last_kind,sdm_arm_probe.last_offset,
  (unsigned long long)sdm_arm_probe.last_value,sdm_arm_probe.last_callback_status,sdm_probe_scope_reason);
 NOTICE("FES_NANO_SOURCE_SETUP_QUEUE build=%s attempted=%u initialized=%u submitted=%u failed=%u error=%d stage=%u policy=%u baseline=%u target=%u timed=%u start_us=%llu last_us=%llu observed_us=%llu samples=%u slots=%u published=%u polls=%u reads=%u completion_seen=%u completion_us=%llu retained=1 grant=0\n",
  FES_NANO_INPUT_BUILD,sdm_arm_probe.command.attempted,sdm_arm_probe.command.initialized,
  sdm_arm_probe.command.submitted,sdm_arm_probe.command.failed,sdm_arm_probe.command.first_error,
  sdm_arm_probe.command.stage,sdm_arm_probe.command.validation_policy,
  sdm_arm_probe.queue_baseline,sdm_arm_probe.queue_target,sdm_arm_probe.command.timed,
  (unsigned long long)sdm_arm_probe.command.start,(unsigned long long)sdm_arm_probe.command.last,
  (unsigned long long)sdm_arm_probe.command.observed,sdm_arm_probe.command.clock_samples,
  sdm_arm_probe.command.slots_attempted,sdm_arm_probe.command.publish_attempted,
  sdm_arm_probe.command.polls,sdm_arm_probe.command.reads,sdm_arm_probe.command.completion_seen,
  (unsigned long long)sdm_arm_probe.command.completion_us);
}
int fes_nano_sdm_source_prepare(struct nano_sdm_input_binding *output,
 struct nano_sdm_transfer_io *transfer_io,uint64_t *trusted_start_us)
{
 if(!output || !transfer_io || !trusted_start_us || sdm_source_failed || sdm_arm_entered==UINT32_MAX)
  return sdm_source_stop(25),-13;
 unsigned warm=sdm_arm_entered!=0;
 if(warm ? sdm_source_rearm_begin()!=1 : sdm_input_phase!=0)return sdm_source_stop(25),-13;
 sdm_arm_entered++;
 struct nano_probe_binding b={provider.record.boot.owner,provider.record.boot.epoch,configuration.generation};
 struct nano_sdm_input_binding input={.owner=b.owner,.epoch=b.epoch,.generation=b.generation,
  .bytes=configuration.bytes,.chunk=configuration.chunk,.idr0=0x080f7e3f,.idr1=0x0e739d18,.idr5=0x00400075};
 memcpy(input.digest384,configuration_program.header+FES_NANO_PROGRAM_SHA384,48);
 struct nano_probe_io io={&sdm_arm_probe,sdm_input_owns,sdm_source_time,sdm_probe_read,sdm_probe_write,
  sdm_probe_clear,sdm_probe_store,sdm_probe_load,sdm_probe_store_block,sdm_probe_load_block,sdm_probe_barrier};
 /* The actual cold check uses the pinned SHA-384 implementation and proves
  * complete source bytes under the held CPU/master/configuration ownership.
  * Issued START also requires the outer before-START full hash to have passed. */
 if(sdm_probe_owns(&sdm_arm_probe,&b)!=1 ||
    sdm_probe_time(&sdm_arm_probe,&sdm_source_clock_start))return sdm_source_stop(26),-13;
 sdm_source_clock_last=sdm_source_clock_start;sdm_source_clock_started=1;
 if(sdm_probe_owns(&sdm_arm_probe,&b)!=1 ||
    configuration_check(&provider,FES_NANO_CONFIG_BASE,configuration.bytes,1))return sdm_source_stop(26),-13;
 uint64_t after_hash=0;
 if(sdm_source_time(&sdm_arm_probe,&after_hash))return -13;
 unsigned setup_stage=1;
 int status=warm ? nano_probe_init_rearmed_input(&sdm_arm_probe,b,io,&input,sdm_input_source,
   sdm_source_previous_retirement(),sdm_source_rearm_scope) :
   nano_probe_init_input(&sdm_arm_probe,b,io,&input,sdm_input_source);
 struct nano_probe_receipt receipt;
 if(!status){setup_stage=2;status=nano_probe_prepare(&sdm_arm_probe,&receipt);}
 if(!status){setup_stage=3;status=nano_probe_arm(&sdm_arm_probe,&receipt);}
 if(status){sdm_source_setup_failure(setup_stage,status);return sdm_source_stop(27),status;}
 if(warm)sdm_source_rearm_prepared();
 sdm_input_phase=1;
 if(sdm_source_owns(&sdm_arm_probe,&sdm_arm_probe.input)!=1)return -13;
 if(warm){
  char digest[97];static const char hex[]="0123456789abcdef";
  for(unsigned n=0;n<48;n++){digest[n*2]=hex[input.digest384[n]>>4];digest[n*2+1]=hex[input.digest384[n]&15U];}digest[96]=0;
  NOTICE("FES_NANO_SOURCE_REARM status=0 owner=%llx epoch=%llx previous=%llu generation=%llu baseline=%u producer=%u abort=%x bytes=%u sha384=%s start_us=%llu\n",
   (unsigned long long)b.owner,(unsigned long long)b.epoch,(unsigned long long)sdm_rearm.previous_generation,
   (unsigned long long)b.generation,sdm_arm_probe.queue_baseline,sdm_arm_probe.queue_target,sdm_arm_probe.abort,
   input.bytes,digest,(unsigned long long)sdm_source_clock_start);
 }
 *output=sdm_arm_probe.input;
 *transfer_io=(struct nano_sdm_transfer_io){&sdm_arm_probe,sdm_source_owns,sdm_source_time};
 *trusted_start_us=sdm_source_clock_start;
 return 0;
}
int fes_nano_sdm_source_start_ready(void)
{
 if(sdm_source_failed || sdm_input_phase!=1 || configuration.issued!=FES_NANO_CONFIG_START ||
    configuration.stage!=FES_CONFIG_START || configuration.completed || configuration.pending)
  return sdm_source_stop(28),-13;
 /* The original remapper ran between prepare and this hook. Its observed
  * post-START values are mandatory, not interchangeable with pre-START. */
 sdm_input_phase=2;
 return fes_nano_sdm_source_validate();
}

#include "nano_sdm_input_finish.inc"

#include "nano_sdm_control_platform.inc"
#include "nano_sdm_reader_platform.inc"
#include "nano_sdm_release_platform.inc"

#include "nano_sdm_rearm_platform.inc"
