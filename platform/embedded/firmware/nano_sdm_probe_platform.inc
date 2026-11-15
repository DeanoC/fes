/* SPDX-License-Identifier: BSD-3-Clause */
/* Included after the actual cold provider/configuration globals. Test macros
 * replace only raw I/O/CPU instructions and linker-address observations. */
#include "probe_setup.h"
#include "nano_sdm_probe.h"
#include "nano_mmu_platform.h"
#if !defined(FES_NANO_SDM_ARM_PROBE) || FES_NANO_SDM_ARM_PROBE != 1
#error Explicit_SDM_arm_and_park_profile_required
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
static unsigned sdm_arm_entered,sdm_probe_scope_reason;
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
       configuration.issued!=FES_NANO_CONFIG_START || configuration.bridges!=b ||
       configuration.owner!=binding->owner || configuration.generation!=binding->generation ||
       configuration.pending || configuration.completed || !configuration.bytes ||
       configuration.base!=FES_NANO_CONFIG_BASE)return sdm_probe_reject(9);
    if(!configuration_program.initialized || !configuration_program.bound || configuration_program.failed ||
       configuration_program.owner!=binding->owner || configuration_program.generation!=binding->generation ||
       configuration_program.bytes!=configuration.bytes)return sdm_probe_reject(10);
    if(fabric_configuration_generation || fabric_generation || fabric_stop.attempted || fabric_stop.failed)
        return sdm_probe_reject(11);
    /* Cold ownership calls the pinned shared-device containment callback for
     * active USB: DMA/IRQ disabled and AHB master idle under stable clocks,
     * CPU/reset/owner and generation. Ready flags alone never qualify it.
     * That callback performs read-only checks; it never stops or reclaims USB. */
    if(fes_nano_cold_owns(&provider,binding->owner,binding->epoch)!=1)return sdm_probe_reject(12);
    FES_PROBE_BARRIER();
    unsigned route=FES_PROBE_READ32(0x10d12184)==3 && FES_PROBE_READ32(0x10d121b0)==0xa000a &&
        FES_PROBE_READ32(0x10d1225c)==0 && FES_PROBE_READ32(0x10d12280)==0 && FES_PROBE_READ32(0x10d12284)==0;
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
__attribute__((noreturn)) static void sdm_probe_park(int status,unsigned sampled,uint32_t control,uint32_t ack)
{
    NOTICE("FES_NANO_SDM_ARM phase=result status=%d owner=%llx epoch=%llx generation=%llu prepared=%u armed=%u failed=%u first_error=%d kind=%u offset=%x value=%llx callback=%d scope_reason=%u reads=%u writes=%u loads=%u sampled=%u cr0=%x ack=%x submitted=0 access=unqualified\n",
        status,(unsigned long long)sdm_arm_probe.binding.owner,(unsigned long long)sdm_arm_probe.binding.epoch,
        (unsigned long long)sdm_arm_probe.binding.generation,sdm_arm_probe.prepared,sdm_arm_probe.armed,
        sdm_arm_probe.failed,sdm_arm_probe.first_error,sdm_arm_probe.last_kind,sdm_arm_probe.last_offset,
        (unsigned long long)sdm_arm_probe.last_value,sdm_arm_probe.last_callback_status,sdm_probe_scope_reason,
        sdm_arm_probe.reads,sdm_arm_probe.writes,sdm_arm_probe.loads,sampled,control,ack);
    NOTICE("FES_NANO_SDM_ARM phase=park submitted=0 retained=1\n");
    for (;;) FES_PROBE_WAIT();
}
void fes_nano_sdm_probe_arm_park(uint32_t bypass)
{
    if (sdm_arm_entered++) sdm_probe_park(-16,0,0,0);
    struct nano_probe_binding binding={provider.record.boot.owner,provider.record.boot.epoch,configuration.generation};
    struct nano_probe_io io={&sdm_arm_probe,sdm_probe_owns,sdm_probe_time,sdm_probe_read,sdm_probe_write,
        sdm_probe_clear,sdm_probe_store,sdm_probe_load,sdm_probe_store_block,sdm_probe_load_block,sdm_probe_barrier};
    int status=nano_probe_init(&sdm_arm_probe,binding,io);
    NOTICE("FES_NANO_SDM_ARM phase=begin owner=%llx epoch=%llx generation=%llu bypass=%x submitted=0\n",
        (unsigned long long)binding.owner,(unsigned long long)binding.epoch,(unsigned long long)binding.generation,bypass);
    struct nano_probe_receipt receipt;
    if (!status && bypass) status=-22;
    if (!status) status=nano_probe_prepare(&sdm_arm_probe,&receipt);
    if (!status) status=nano_probe_arm(&sdm_arm_probe,&receipt);
    uint32_t control=0,ack=0;unsigned sampled=0;
    if(!status && sdm_probe_owns(&sdm_arm_probe,&binding)==1) {
        FES_PROBE_BARRIER();control=FES_PROBE_READ32(0x16000020);ack=FES_PROBE_READ32(0x16000024);FES_PROBE_BARRIER();
        sampled=sdm_probe_owns(&sdm_arm_probe,&binding)==1;
        if(!sampled || control!=13 || ack!=13)status=-5;
    } else if(!status) status=-13;
    sdm_probe_park(status,sampled,control,ack);
}
