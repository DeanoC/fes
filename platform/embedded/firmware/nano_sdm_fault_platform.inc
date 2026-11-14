/* SPDX-License-Identifier: BSD-3-Clause */
/* Included after the actual cold provider/configuration globals. Test macros
 * replace only raw I/O/CPU instructions and linker-address observations. */
#include "probe_setup.h"
#include "fault64_service.h"
#include "nano_mmu_platform.h"
#if !defined(FES_NANO_SDM_FAULT64) || FES_NANO_SDM_FAULT64 != 1
#error Explicit_SDM_fault64_supplier_profile_required
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
static unsigned sdm_arm_entered,sdm_probe_scope_reason,sdm_fault_phase,sdm_fault_mailbox_attempted;
static uint32_t sdm_fault_bad_offset,sdm_fault_bad_value;
static struct nano_fault64 sdm_fault_gate;
static char sdm_fault_wire[NANO_EVT_WIRE_MAX+1];
#ifndef FES_NANO_FAULT64_BUILD
#error Explicit_fault64_build_identity_required
#endif
#ifndef FES_PROBE_USB_BINDING_VALID
#define FES_PROBE_USB_BINDING_VALID() fes_nano_usb_master_service_valid(&usb_master,&fes_nano_owned_bridges,&usb_platform)
#endif
static int sdm_probe_reject(unsigned reason)
{ sdm_probe_scope_reason=reason;return 0; }
static int sdm_probe_geometry(void)
{
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
       configuration.completed || !configuration.bytes || !configuration.chunk || (configuration.chunk&63U) ||
       configuration.bytes>configuration.maximum ||
       !((configuration.issued==FES_NANO_CONFIG_START && configuration.stage==FES_CONFIG_START && !configuration.pending) ||
         (sdm_fault_phase==2 && ((configuration.issued==FES_NANO_CONFIG_WRITE && configuration.stage==FES_CONFIG_WRITE) ||
          (configuration.issued==FES_NANO_CONFIG_COMPLETED && configuration.stage==FES_CONFIG_POLL)) &&
          configuration.pending && configuration.pending<=configuration.bytes)) ||
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
        FES_PROBE_READ32(0x10d12280)==(sdm_fault_phase==2 ? 0x80000000U:0U) &&
        FES_PROBE_READ32(0x10d12284)==(sdm_fault_phase==2 ? 0x80000000U:0U);
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
    switch(offset) {
    case 0x20:case 0x28:case 0x80:case 0x84:case 0x88:case 0x90:case 0x94:case 0x98:case 0xa0:case 0xa4:break;
    default:return -1;
    }
    FES_PROBE_WRITE32(UINT64_C(0x16000000)+offset,value);return 0;
}
static int sdm_probe_range(uint32_t offset,uint32_t bytes)
{
    return !(offset&7) && !(bytes&7) && bytes &&
        ((offset<0x5000 && bytes<=0x5000-offset) ||
         (offset>=NANO_PROBE_CMDQ && offset<NANO_PROBE_CMDQ+128 && bytes<=NANO_PROBE_CMDQ+128-offset) ||
         (offset>=NANO_PROBE_EVTQ && offset<NANO_PROBE_EVTQ+256 && bytes<=NANO_PROBE_EVTQ+256-offset));
}
static int sdm_probe_clear(void *context,uint32_t offset,uint32_t bytes)
{
    if(context!=&sdm_arm_probe || !((offset==0 && bytes==0x5000) ||
        (offset==NANO_PROBE_CMDQ && bytes==128) || (offset==NANO_PROBE_EVTQ && bytes==256)))return -1;
    for (uint32_t n=0;n<bytes;n+=8) FES_PROBE_STORE64(NANO_PROBE_BASE+offset+n,0);
    return 0;
}
static int sdm_probe_store(void *context,uint32_t offset,uint64_t value)
{
    if(context!=&sdm_arm_probe || !sdm_probe_range(offset,8) || offset>=NANO_PROBE_EVTQ)return -1;
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
       offset>=0x5000 || !sdm_probe_range(offset,words*8) || words*8>0x5000-offset ||
       !sdm_arm_probe.preparing || sdm_arm_probe.prepared || sdm_arm_probe.arming || sdm_arm_probe.armed || sdm_arm_probe.failed) return -1;
    for(unsigned n=0;n<words;n++)FES_PROBE_STORE64(NANO_PROBE_BASE+offset+n*8,values[n]);
    return 0;
}
static int sdm_probe_load_block(void *context,uint32_t offset,uint64_t *values,unsigned words)
{
    if(context!=&sdm_arm_probe || !values || !words || words>NANO_PROBE_MEMORY_WORDS ||
       !sdm_probe_range(offset,words*8) || !sdm_arm_probe.preparing || sdm_arm_probe.prepared ||
       sdm_arm_probe.arming || sdm_arm_probe.armed || sdm_arm_probe.failed) return -1;
    for(unsigned n=0;n<words;n++)values[n]=FES_PROBE_LOAD64(NANO_PROBE_BASE+offset+n*8);
    return 0;
}
static void sdm_probe_barrier(void *context) {(void)context;FES_PROBE_BARRIER();}
static int sdm_fault_owns(void *,uint64_t,uint64_t,uint64_t);
static int sdm_fault_clock(void *,uint64_t *);
static int sdm_fault_event_owns(void *,const struct nano_evt_binding *);
static int sdm_fault_event_read(void *,uint32_t,uint32_t *);
static int sdm_fault_event_copy(void *,unsigned,uint8_t [32]);
static void sdm_fault_event_barrier(void *);
static int sdm_fault_wait_read(void *,uint32_t *);
static int sdm_fault_wait_io_valid(const struct nano_evt_wait_io *io)
{
    return io->context==&sdm_fault_gate && io->owns==sdm_fault_event_owns &&
        io->read_producer==sdm_fault_wait_read && io->barrier==sdm_fault_event_barrier;
}
static int sdm_fault_event_io_valid(const struct nano_evt_io *io)
{
    return io->context==&sdm_fault_gate && io->owns==sdm_fault_event_owns &&
        io->read32==sdm_fault_event_read && io->copy_entry==sdm_fault_event_copy &&
        io->barrier==sdm_fault_event_barrier;
}
/* Recheck cheap immutable callback identities after scope's hardware
 * reads too; callbacks cannot change unnoticed inside an ownership proof. */
static int sdm_fault_bindings_valid(void)
{
    return sdm_arm_probe.io.context==&sdm_arm_probe && sdm_arm_probe.io.owns==sdm_probe_owns &&
        sdm_arm_probe.io.time_us==sdm_probe_time && sdm_arm_probe.io.read32==sdm_probe_read &&
        sdm_arm_probe.io.write32==sdm_probe_write && sdm_arm_probe.io.clear==sdm_probe_clear &&
        sdm_arm_probe.io.store64==sdm_probe_store && sdm_arm_probe.io.load64==sdm_probe_load &&
        sdm_arm_probe.io.store_block==sdm_probe_store_block && sdm_arm_probe.io.load_block==sdm_probe_load_block &&
        sdm_arm_probe.io.barrier==sdm_probe_barrier &&
        (!sdm_fault_gate.initialized ||
         (sdm_fault_gate.io.context==&sdm_fault_gate && sdm_fault_gate.io.owns==sdm_fault_owns &&
          sdm_fault_gate.io.time==sdm_fault_clock && sdm_fault_event_io_valid(&sdm_fault_gate.baseline.io) &&
          sdm_fault_event_io_valid(&sdm_fault_gate.events.io) && sdm_fault_wait_io_valid(&sdm_fault_gate.waiter.source) &&
          sdm_fault_gate.waiter.clock.context==&sdm_fault_gate && sdm_fault_gate.waiter.clock.time==sdm_fault_clock));
}
static int sdm_fault_live_queues(void)
{
    if(!sdm_arm_probe.initialized || !sdm_arm_probe.prepared || !sdm_arm_probe.armed || sdm_arm_probe.failed ||
       sdm_arm_probe.io.context!=&sdm_arm_probe || sdm_arm_probe.io.owns!=sdm_probe_owns ||
       sdm_arm_probe.io.time_us!=sdm_probe_time || sdm_arm_probe.io.read32!=sdm_probe_read ||
       sdm_arm_probe.io.write32!=sdm_probe_write || sdm_arm_probe.io.clear!=sdm_probe_clear ||
       sdm_arm_probe.io.store64!=sdm_probe_store || sdm_arm_probe.io.load64!=sdm_probe_load ||
       sdm_arm_probe.io.store_block!=sdm_probe_store_block ||
       sdm_arm_probe.io.load_block!=sdm_probe_load_block || sdm_arm_probe.io.barrier!=sdm_probe_barrier || sdm_arm_probe.command_receipt.producer!=6 ||
       sdm_arm_probe.command_receipt.consumer!=6 || !sdm_arm_probe.command_receipt.commands_completed)
        return sdm_probe_reject(15);
    static const uint32_t words[][2]={
        {0x20,13},{0x24,13},{0x28,0x820},{0x2c,4},{0x44,0},{0x48,0},{0x50,0},{0x54,0},{0x60,0},{0x64,0},
        {0x80,0x94000000},{0x84,0},{0x88,8},{0x90,0x94008003},{0x94,0},{0x98,6},{0x9c,6},
        {0xa0,0x94009003},{0xa4,0},{0x100ac,0}};
    for(unsigned n=0;n<sizeof(words)/sizeof(words[0]);n++){
        FES_PROBE_BARRIER();uint32_t actual=FES_PROBE_READ32(UINT64_C(0x16000000)+words[n][0]);FES_PROBE_BARRIER();
        if(actual!=words[n][1]){sdm_fault_bad_offset=words[n][0];sdm_fault_bad_value=actual;return sdm_probe_reject(16);}
    }
    return sdm_fault_bindings_valid() ? 1:sdm_probe_reject(15);
}
static int sdm_fault_owns(void *context,uint64_t owner,uint64_t epoch,uint64_t generation)
{
    if(context!=&sdm_fault_gate || sdm_fault_phase!=2 || owner!=sdm_arm_probe.binding.owner ||
       epoch!=sdm_arm_probe.binding.epoch || generation!=sdm_arm_probe.binding.generation ||
       sdm_fault_gate.io.context!=&sdm_fault_gate || sdm_fault_gate.io.owns!=sdm_fault_owns ||
       sdm_fault_gate.io.time!=sdm_fault_clock)return sdm_probe_reject(17);
    if(sdm_fault_gate.initialized &&
       (!sdm_fault_event_io_valid(&sdm_fault_gate.baseline.io) ||
        !sdm_fault_event_io_valid(&sdm_fault_gate.events.io) ||
        !sdm_fault_wait_io_valid(&sdm_fault_gate.waiter.source) ||
        sdm_fault_gate.waiter.clock.context!=&sdm_fault_gate || sdm_fault_gate.waiter.clock.time!=sdm_fault_clock))
        return sdm_probe_reject(17);
    if(sdm_probe_owns(&sdm_arm_probe,&sdm_arm_probe.binding)!=1 || !sdm_fault_live_queues())return 0;
    if(sdm_probe_owns(&sdm_arm_probe,&sdm_arm_probe.binding)!=1)return 0;
    return sdm_fault_bindings_valid() ? 1:sdm_probe_reject(17);
}
/* The capture/wait engines own the fresh before/after boundary. A clock
 * callback has no peripheral side effect; do not recursively re-prove scope. */
static int sdm_fault_clock(void *context,uint64_t *value)
{
    if(context!=&sdm_fault_gate || !value || FES_PROBE_FREQUENCY()!=400000000U)return -13;
    *value=FES_PROBE_COUNTER()/400U;return 0;
}
static int sdm_fault_event_owns(void *context,const struct nano_evt_binding *b)
{
    if(!b || b->queue!=NANO_PROBE_BASE+NANO_PROBE_EVTQ || b->consumer)return sdm_probe_reject(18);
    return sdm_fault_owns(context,b->owner,b->epoch,b->generation);
}
/* The full snapshot caller (event_capture.rd) prove the complete
 * current scope immediately before and after this one whitelisted read. */
static int sdm_fault_event_read(void *context,uint32_t offset,uint32_t *value)
{
    switch(offset){case 0x60:case 0x64:case 0x20:case 0x24:case 0xa0:case 0xa4:case 0x100a8:case 0x100ac:break;
    default:return -22;}
    if(context!=&sdm_fault_gate || !value || sdm_fault_phase!=2)return -13;
    FES_PROBE_BARRIER();*value=FES_PROBE_READ32(UINT64_C(0x16000000)+offset);FES_PROBE_BARRIER();return 0;
}
/* The qualified waiter checks complete static/error/owner scope on both
 * sides. Only this fixed NS producer address is exposed by its narrow API. */
static int sdm_fault_wait_read(void *context,uint32_t *value)
{
    if(context!=&sdm_fault_gate || !value || sdm_fault_phase!=2)return -13;
    FES_PROBE_BARRIER();*value=FES_PROBE_READ32(UINT64_C(0x160100a8));FES_PROBE_BARRIER();return 0;
}
static int sdm_fault_event_copy(void *context,unsigned slot,uint8_t destination[32])
{
    if(slot>=NANO_EVT_ENTRIES || !destination || context!=&sdm_fault_gate)return -22;
    for(unsigned n=0;n<4;n++){
        if(sdm_fault_owns(context,sdm_arm_probe.binding.owner,sdm_arm_probe.binding.epoch,sdm_arm_probe.binding.generation)!=1)return -13;
        FES_PROBE_BARRIER();uint64_t word=FES_PROBE_LOAD64(NANO_PROBE_BASE+NANO_PROBE_EVTQ+slot*32+n*8);FES_PROBE_BARRIER();
        if(sdm_fault_owns(context,sdm_arm_probe.binding.owner,sdm_arm_probe.binding.epoch,sdm_arm_probe.binding.generation)!=1)return -13;
        for(unsigned i=0;i<8;i++)destination[n*8+i]=(uint8_t)(word>>(i*8));
    }
    return 0;
}
static void sdm_fault_event_barrier(void *context){(void)context;FES_PROBE_BARRIER();}
static void sdm_fault_emit_wire(unsigned before)
{
    size_t length=0;int wire;
    if(before){
        struct nano_evt_wire_context context={.build=FES_NANO_FAULT64_BUILD,
            .owner=sdm_arm_probe.binding.owner,.epoch=sdm_arm_probe.binding.epoch,
            .command_generation=sdm_arm_probe.binding.generation,.event_generation=sdm_arm_probe.binding.generation};
        wire=nano_evt_wire_format(&context,&sdm_arm_probe.command,&sdm_arm_probe.command_receipt,
            &sdm_fault_gate.baseline,&sdm_fault_gate.before_batch,sdm_fault_wire,NANO_EVT_WIRE_MAX,&length);
    }else wire=nano_fault64_format(&sdm_fault_gate,FES_NANO_FAULT64_BUILD,sdm_fault_wire,NANO_EVT_WIRE_MAX,&length);
    NOTICE("FES_NANO_FAULT64 phase=wire sample=%s status=%d bytes=%u\n",before ? "before":"after",wire,(unsigned)length);
    if(!wire && length<=NANO_EVT_WIRE_MAX){sdm_fault_wire[length]=0;NOTICE("%s",sdm_fault_wire);}
}
__attribute__((noreturn)) static void sdm_fault_park(int status)
{
    NOTICE("FES_NANO_FAULT64 phase=result status=%d owner=%llx epoch=%llx generation=%llu phase_id=%u scope_reason=%u bad_offset=%x bad_value=%x arm_writes=%u mailbox_attempted=%u mailbox_status=%d captured=%u retained=1 access=unqualified\n",
        status,(unsigned long long)sdm_arm_probe.binding.owner,(unsigned long long)sdm_arm_probe.binding.epoch,
        (unsigned long long)sdm_arm_probe.binding.generation,sdm_fault_phase,sdm_probe_scope_reason,
        sdm_fault_bad_offset,sdm_fault_bad_value,sdm_arm_probe.writes,sdm_fault_mailbox_attempted,
        sdm_fault_gate.mailbox_status,sdm_fault_gate.captured);
    NOTICE("FES_NANO_FAULT64 phase=wait status=%d elapsed_us=%llu polls=%u timed_out=%u poll_limit=%u publication_seen=%u event_status=%d events=%u\n",
        sdm_fault_gate.wait_status,(unsigned long long)sdm_fault_gate.waiter.elapsed,sdm_fault_gate.waiter.polls,
        sdm_fault_gate.waiter.timed_out,sdm_fault_gate.waiter.poll_limit,sdm_fault_gate.waiter.publication_seen,
        sdm_fault_gate.event_status,sdm_fault_gate.batch.count);
    if(sdm_fault_gate.baseline.captured && !sdm_fault_gate.baseline.failed)sdm_fault_emit_wire(1);
    if(sdm_fault_gate.captured)sdm_fault_emit_wire(0);
    NOTICE("FES_NANO_FAULT64 phase=park retained=1 resume=0\n");
    for(;;)FES_PROBE_WAIT();
}
void fes_nano_sdm_fault64_arm(uint32_t bypass)
{
    if(sdm_arm_entered++ || sdm_fault_phase || bypass)sdm_fault_park(-16);
    struct nano_probe_binding b={provider.record.boot.owner,provider.record.boot.epoch,configuration.generation};
    struct nano_probe_io io={&sdm_arm_probe,sdm_probe_owns,sdm_probe_time,sdm_probe_read,sdm_probe_write,
        sdm_probe_clear,sdm_probe_store,sdm_probe_load,sdm_probe_store_block,sdm_probe_load_block,sdm_probe_barrier};
    int status=nano_probe_init(&sdm_arm_probe,b,io);struct nano_probe_receipt receipt;
    if(!status)status=nano_probe_prepare(&sdm_arm_probe,&receipt);
    if(!status)status=nano_probe_arm(&sdm_arm_probe,&receipt);
    if(status || sdm_probe_owns(&sdm_arm_probe,&b)!=1 || !sdm_fault_live_queues())sdm_fault_park(status ? status:-13);
    sdm_fault_phase=1;
    NOTICE("FES_NANO_FAULT64 phase=armed owner=%llx epoch=%llx generation=%llu SDM_DATA=0 access=unqualified\n",
        (unsigned long long)b.owner,(unsigned long long)b.epoch,(unsigned long long)b.generation);
}
void fes_nano_sdm_fault64_start_result(int status)
{
    if(sdm_fault_phase!=1 || status)sdm_fault_park(status<0 ? status:-13);
    sdm_fault_phase=2;
    struct nano_fault64_io io={&sdm_fault_gate,sdm_fault_owns,sdm_fault_clock};
    struct nano_evt_io events={&sdm_fault_gate,sdm_fault_event_owns,sdm_fault_event_read,sdm_fault_event_copy,sdm_fault_event_barrier};
    struct nano_evt_wait_io publication={&sdm_fault_gate,sdm_fault_event_owns,sdm_fault_wait_read,sdm_fault_event_barrier};
    status=nano_fault64_init(&sdm_fault_gate,&sdm_arm_probe,&configuration,&configuration_program,io,events,publication);
    if(status)sdm_fault_park(status);
    NOTICE("FES_NANO_FAULT64 phase=start_ready SDM_DATA=0 access=unqualified\n");
}
uint32_t fes_nano_sdm_fault64_before(const struct nano_fault64_vendor *vendor)
{
    uint32_t selected=0;int status=nano_fault64_before(&sdm_fault_gate,vendor,&selected);
    if(status || selected!=64)sdm_fault_park(status ? status:-13);
    return selected;
}
void fes_nano_sdm_fault64_submit(unsigned observed)
{
    if(observed!=1)sdm_fault_park(-13);
    int status=nano_fault64_submit(&sdm_fault_gate);
    if(status)sdm_fault_park(status);
}
void fes_nano_sdm_fault64_after(int mailbox_status)
{
    sdm_fault_mailbox_attempted=1;
    int status=nano_fault64_after(&sdm_fault_gate,mailbox_status);
    /* Keep blocking output outside the DATA-to-observation interval. */
    NOTICE("FES_NANO_SDM_RESULT status=%d\n",mailbox_status);sdm_fault_park(status);
}
