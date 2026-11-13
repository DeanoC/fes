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
static struct nano_probe_setup sdm_arm_probe;
static unsigned sdm_arm_entered,sdm_probe_scope_reason,sdm_input_phase;
static unsigned sdm_source_failed;
static uint32_t sdm_source_bad_offset,sdm_source_bad_value;
static unsigned sdm_source_clock_started;
static uint64_t sdm_source_clock_start,sdm_source_clock_last;
static int sdm_source_time(void *,uint64_t *);
#ifndef FES_PROBE_USB_BINDING_VALID
#define FES_PROBE_USB_BINDING_VALID() fes_nano_usb_master_service_valid(&usb_master,&fes_nano_owned_bridges,&usb_platform)
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
static int sdm_probe_owns(void *context,const struct nano_probe_binding *binding)
{
    struct fes_nano_bridges *b=&fes_nano_owned_bridges;
    sdm_probe_scope_reason=0;
    if(context!=&sdm_arm_probe || !binding)return sdm_probe_reject(1);
    if(!FES_PROBE_CPU() || FES_PROBE_FREQUENCY()!=400000000U)return sdm_probe_reject(2);
    if(!fes_nano_mmu_platform_scope())return sdm_probe_reject(3);
    if(!sdm_probe_geometry())return sdm_probe_reject(4);
    if(!provider.valid || provider.failed ||
       binding->owner!=provider.record.boot.owner || binding->epoch!=provider.record.boot.epoch ||
       provider.control_expected || provider.ddr_expected)return sdm_probe_reject(5);
    if(!b->initialized || b->failed || b->device_lease || b->epoch!=binding->epoch ||
       b->state.owner!=binding->owner || b->state.generation!=binding->generation ||
       b->state.reset!=15 || b->state.masters_contained!=1 || b->state.never_enabled!=1)
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
       !configuration.started || configuration.finished || configuration.rearmed ||
       configuration.bridges!=b ||
       configuration.owner!=binding->owner || configuration.generation!=binding->generation ||
       !configuration.bytes || !configuration.chunk || (configuration.chunk&63U) ||
       configuration.bytes>configuration.maximum || configuration.completed>configuration.bytes ||
       configuration.pending>configuration.bytes-configuration.completed ||
       configuration.pending>(uint64_t)configuration.chunk*FES_NANO_CONFIG_BUFFERS ||
       (configuration.completed!=configuration.bytes && configuration.completed%configuration.chunk) ||
       (configuration.completed+configuration.pending!=configuration.bytes && configuration.pending%configuration.chunk) ||
       !((configuration.issued==FES_NANO_CONFIG_START && configuration.stage==FES_CONFIG_START &&
          !configuration.completed && !configuration.pending) ||
         (sdm_input_phase==2 && (
          ((configuration.issued==FES_NANO_CONFIG_WRITE && configuration.stage==FES_CONFIG_WRITE) ||
           (configuration.issued==FES_NANO_CONFIG_COMPLETED && configuration.stage==FES_CONFIG_POLL)) ?
            configuration.pending!=0 :
          (configuration.issued==FES_NANO_CONFIG_ISDONE && configuration.stage==FES_CONFIG_STATUS &&
           !configuration.pending && configuration.completed==configuration.bytes)))) ||
       configuration.base!=FES_NANO_CONFIG_BASE)return sdm_probe_reject(9);
    if(!configuration_program.initialized || !configuration_program.bound || configuration_program.failed ||
       configuration_program.owner!=binding->owner || configuration_program.generation!=binding->generation ||
       configuration_program.bytes!=configuration.bytes || configuration_program.chunk!=configuration.chunk ||
       configuration_program.mixed_roles!=1 || configuration_program.role!=FES_NANO_PROGRAM_ROLE_MENU ||
       configuration_program.control_abi!=FES_NANO_PROGRAM_MENU_ABI)return sdm_probe_reject(10);
    if(fabric_configuration_generation || fabric_generation || fabric_stop.attempted || fabric_stop.failed)
        return sdm_probe_reject(11);
    /* Cold ownership calls the pinned shared-device containment callback for
     * active USB: DMA/IRQ disabled and AHB master idle under stable clocks,
     * CPU/reset/owner and generation. Ready flags alone never qualify it.
     * That callback performs read-only checks; it never stops or reclaims USB. */
    if(fes_nano_cold_owns(&provider,binding->owner,binding->epoch)!=1)return sdm_probe_reject(12);
    FES_PROBE_BARRIER();
    unsigned route=FES_PROBE_READ32(0x10d12184)==3 && FES_PROBE_READ32(0x10d121b0)==0xa000a &&
        FES_PROBE_READ32(0x10d1225c)==0 &&
        FES_PROBE_READ32(0x10d12280)==(sdm_input_phase==2 ? 0x80000000U:0U) &&
        FES_PROBE_READ32(0x10d12284)==(sdm_input_phase==2 ? 0x80000000U:0U);
    FES_PROBE_BARRIER();
    if(!route)return sdm_probe_reject(13);
    if(fes_nano_cold_owns(&provider,binding->owner,binding->epoch)!=1)return sdm_probe_reject(12);
    return FES_PROBE_CPU() && fes_nano_mmu_platform_scope() ? 1 : sdm_probe_reject(14);
}
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
    if(context!=&sdm_arm_probe)return -1;
    uint32_t expected=0;
    switch(offset) {
    case 0x20:if(value!=8 && value!=12 && value!=13)return -1;expected=value;break;
    case 0x28:expected=0x820;break;
    case 0x80:expected=0x94000000;break;
    case 0x84:case 0x94:case 0xa4:break;
    case 0x88:expected=8;break;
    case 0x90:expected=0x94008003;break;
    case 0x98:expected=6;break;
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
    if(context!=&sdm_arm_probe || sdm_arm_probe.writes || !sdm_arm_probe.preparing || sdm_arm_probe.prepared ||
        sdm_arm_probe.arming || sdm_arm_probe.armed || sdm_arm_probe.failed || !((offset==0 && bytes==0x8000) || (offset==0xa000 && bytes==0x2000) ||
        (offset==NANO_PROBE_CMDQ && bytes==128) || (offset==NANO_PROBE_EVTQ && bytes==256)))return -1;
    for (uint32_t n=0;n<bytes;n+=8) FES_PROBE_STORE64(NANO_PROBE_BASE+offset+n,0);
    return 0;
}
static int sdm_probe_store(void *context,uint32_t offset,uint64_t value)
{
    if(context!=&sdm_arm_probe || (offset&7U) || offset<NANO_PROBE_CMDQ || offset>=NANO_PROBE_CMDQ+96 ||
        !sdm_arm_probe.preparing || sdm_arm_probe.prepared || sdm_arm_probe.arming || sdm_arm_probe.armed || sdm_arm_probe.failed ||
        !sdm_arm_probe.command.initialized || !sdm_arm_probe.command.submitted || sdm_arm_probe.command.publish_attempted)return -1;
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
    !sdm_input_callbacks() || sdm_arm_probe.command_receipt.producer!=6 ||
    sdm_arm_probe.command_receipt.consumer!=6 || !sdm_arm_probe.command_receipt.commands_completed)
  return sdm_source_stop(21);
 static const uint32_t registers[][2]={
  {0,0x080f7e3f},{4,0x0e739d18},{0x14,0x00400075},{0x20,13},{0x24,13},{0x28,0x820},{0x2c,4},
  {0x44,0},{0x48,0},{0x50,0},{0x54,0},{0x60,0},{0x64,0},
  {0x80,0x94000000},{0x84,0},{0x88,8},{0x90,0x94008003},{0x94,0},{0x98,6},{0x9c,6},
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
int fes_nano_sdm_source_prepare(struct nano_sdm_input_binding *output,
 struct nano_sdm_transfer_io *transfer_io,uint64_t *trusted_start_us)
{
 if(!output || !transfer_io || !trusted_start_us || sdm_arm_entered++ || sdm_input_phase || sdm_source_failed)
  return sdm_source_stop(25),-13;
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
 int status=nano_probe_init_input(&sdm_arm_probe,b,io,&input,sdm_input_source);struct nano_probe_receipt receipt;
 if(!status)status=nano_probe_prepare(&sdm_arm_probe,&receipt);
 if(!status)status=nano_probe_arm(&sdm_arm_probe,&receipt);
 if(status)return sdm_source_stop(27),status;
 sdm_input_phase=1;
 if(sdm_source_owns(&sdm_arm_probe,&sdm_arm_probe.input)!=1)return -13;
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
