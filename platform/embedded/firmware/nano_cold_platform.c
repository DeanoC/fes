/* SPDX-License-Identifier: BSD-3-Clause */
#include <arch_helpers.h>
#include <drivers/delay_timer.h>
#include <lib/mmio.h>
#include <string.h>
#include "nano_bridge_platform.h"
#include "nano_cold.h"
#include "nano_smmu.h"
#include "nano_fabric_stop.h"
#include "nano_ddr_firewall.h"
#include "nano_ddr_read.h"
#include "nano_hps_io.h"
#include "nano_program.h"
#include "generated/fes_de25_ddr_probe.h"
#include "fes_nano_cold_profile.h"
#define FES_COLD_READ32(a) mmio_read_32(a)
#define FES_COLD_READ16(a) mmio_read_16(a)
#define FES_COLD_WRITE32(a,v) mmio_write_32(a,v)
#define FES_COLD_WRITE16(a,v) mmio_write_16(a,v)
#define FES_COLD_BARRIER() dsbsy()
#define FES_COLD_WAIT() udelay(10)
#include "nano_cold_mmio.inc"
#if (defined(FES_NANO_CONTROL_PROBE) && FES_NANO_CONTROL_PROBE) || (defined(FES_NANO_SMMU_BOOT_OBSERVE) && FES_NANO_SMMU_BOOT_OBSERVE) || (defined(FES_NANO_HPS_IO_OBSERVE) && FES_NANO_HPS_IO_OBSERVE)
#include <common/debug.h>
#endif
#if defined(FES_NANO_CONTROL_PROBE) && FES_NANO_CONTROL_PROBE
#include <socfpga_mailbox.h>
#endif
#if defined(FES_NANO_CONTROLLED_SD) && FES_NANO_CONTROLLED_SD
#include <common/debug.h>
#include "nano_sd_platform.h"
#endif

static struct fes_nano_cold_record captured;
static struct fes_nano_cold provider;
#if defined(FES_NANO_COLD_CONFIG) && FES_NANO_COLD_CONFIG
static struct fes_nano_program configuration_program;
#endif
static unsigned capture_attempted, capture_valid;
#if defined(FES_NANO_HPS_IO_OBSERVE) && FES_NANO_HPS_IO_OBSERVE
static struct fes_nano_hps_io hps_io;
static const struct fes_nano_hps_io_register hps_io_registers[FES_NANO_HPS_IO_COUNT]=FES_NANO_HPS_IO_REGISTERS;
static const uint8_t hps_io_digest[32]=FES_NANO_HPS_IO_SHA;
static int hps_io_owns(void *context,uint64_t owner,uint64_t generation)
{
    struct fes_nano_cold *s=context;
    struct fes_nano_bridge_snapshot observed={0};
    return GET_EL(read_current_el())==MODE_EL3 && !(read_sctlr_el3()&5U) &&
        fes_nano_cold_cpu_release_locked()==1 && captured.core==MPIDR_AFFLVL1_VAL(read_mpidr()) &&
        !s->control_expected && !s->ddr_expected && !fes_nano_owned_bridges.device_lease &&
        fes_nano_cold_owns(s,s->record.boot.owner,s->record.boot.epoch)==1 &&
        fes_nano_bridges_observe(&fes_nano_owned_bridges,&observed)==0 &&
        observed.owner==owner && observed.generation==generation && observed.never_enabled &&
        observed.reset==FES_NANO_BRIDGES_ALL && observed.masters_contained;
}
static int hps_io_read(void *context,uint32_t offset,uint32_t *value)
{
    (void)context;
    if (!fes_nano_hps_io_offset(offset)) return -1;
    *value=mmio_read_32(FES_NANO_HPS_IO_BASE+offset);
    return 0;
}
static void hps_io_barrier(void *context) { (void)context;dsbsy(); }
static void hps_io_trace(void *context,unsigned pass,unsigned index,uint32_t offset,uint32_t expected,uint32_t observed)
{
    (void)context;
    NOTICE("FES_NANO_HPS_IO_ROW pass=%u index=%u offset=%x expected=%x observed=%x\n",pass,index,offset,expected,observed);
}
void fes_nano_platform_hps_io_sip(uint64_t operation,uint64_t owner,uint64_t generation,
                                unsigned busy,uint64_t result[4])
{
    if (!hps_io.initialized && !hps_io.failed) {
        struct fes_nano_hps_io_ops io={&provider,hps_io_owns,hps_io_read,hps_io_barrier,hps_io_trace};
        (void)fes_nano_hps_io_init(&hps_io,io,hps_io_registers,FES_NANO_HPS_IO_COUNT,hps_io_digest);
    }
    NOTICE("FES_NANO_HPS_IO_BEGIN part=%u owner=%llx generation=%llu count=%u busy=%u\n",
        operation==FES_EMBEDDED_BRIDGE_HPS_IO_PART1 ? 1U : 0U,(unsigned long long)owner,
        (unsigned long long)generation,FES_NANO_HPS_IO_COUNT,busy);
    fes_nano_hps_io_sip(&hps_io,operation,owner,generation,busy,result);
    NOTICE("FES_NANO_HPS_IO_END part=%u status=%llu reads=%u error=%d owner=%llx generation=%llu data=held\n",
        operation==FES_EMBEDDED_BRIDGE_HPS_IO_PART1 ? 1U : 0U,(unsigned long long)result[0],
        hps_io.reads,hps_io.first_error,(unsigned long long)owner,(unsigned long long)generation);
}
#endif
#ifdef FES_NANO_READER_CPU_BASE
static int cold_master_owns(void *,uint64_t,uint64_t);
static int cold_master_quiesce(void *,uint32_t);
static int cold_master_quiescent(void *,uint32_t *);
#endif

#if defined(FES_NANO_READER_CPU_BASE) || (defined(FES_NANO_SMMU_BOOT_OBSERVE) && FES_NANO_SMMU_BOOT_OBSERVE)
static int smmu_owns(void *context)
{
    struct fes_nano_cold *s=context;
    unsigned control_scope=!s->control_expected;
#if defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
    struct fes_nano_identity_probe *p=fes_nano_platform_identity_probe();
    control_scope |= p && p->active && p->reader==2 && p->bridges==&fes_nano_owned_bridges &&
        (p->stage==FES_ID_DDR_PREPARE || p->stage==FES_ID_DDR_RELEASE) &&
        s->control_expected==1 && !s->ddr_expected && fes_nano_owned_bridges.state.reset==13;
#endif
    return GET_EL(read_current_el())==MODE_EL3 && !(read_sctlr_el3()&5U) &&
        fes_nano_cold_cpu_release_locked()==1 && control_scope &&
        fes_nano_cold_owns(s,s->record.boot.owner,s->record.boot.epoch)==1;
}
static int smmu_read(void *context,uint32_t offset,uint32_t *value)
{
    if (!fes_nano_smmu_offset(offset) || smmu_owns(context)!=1) return -1;
    dsbsy();*value=mmio_read_32(FES_NANO_SMMU_BASE+offset);dsbsy();
    return smmu_owns(context)==1 ? 0 : -1;
}
static int smmu_report(struct fes_nano_cold *s,struct fes_nano_smmu_observation *out)
{
    struct fes_nano_smmu_probe probe={.io={s,smmu_owns,smmu_read}};
    struct fes_nano_smmu_observation observation={0};
    int status=fes_nano_smmu_observe(&probe,&observation);
    const struct fes_nano_smmu_snapshot *v=&observation.registers;
    NOTICE("FES_NANO_SMMU status=%d reads=%u idr0=%x idr1=%x aidr=%x s_idr1=%x cr0=%x ack=%x gbpa=%x agbpa=%x s_cr0=%x s_ack=%x s_init=%x s_gbpa=%x s_agbpa=%x identified=%u settled=%u bypass_controls_clear=%u access=unqualified\n",
        status,probe.reads,v->idr0,v->idr1,v->aidr,v->s_idr1,v->cr0,v->cr0ack,v->gbpa,v->agbpa,
        v->s_cr0,v->s_cr0ack,v->s_init,v->s_gbpa,v->s_agbpa,observation.identified,
        observation.controls_settled,observation.bypass_controls_clear);
    if (!status && out) *out=observation;
    return status;
}
#endif

/* Runs in early EL3 setup, before BL33. No pointer from a later SMC is used. */
void fes_nano_cold_capture(uintptr_t pointer, uint64_t cookie)
{
    if (capture_attempted++) return;
    if (cookie!=FES_NANO_COLD_COOKIE || pointer&7U || pointer<0x1000 ||
        pointer>0x40000-sizeof(captured) || GET_EL(read_current_el())!=MODE_EL3) return;
    memcpy(&captured,(const void *)pointer,sizeof(captured));
    capture_valid=captured.checksum==fes_nano_cold_checksum(&captured);
}
int fes_nano_platform_boot_record(struct fes_nano_boot_record *record,
                    struct fes_nano_boot_policy *policy, struct fes_nano_master_ops *masters)
{
    static const uint8_t chain[32]=FES_NANO_COLD_CHAIN;
    uint64_t owner=(FES_NANO_COLD_OWNER_SEED ^ captured.boot.epoch)|1U;
    if (!capture_valid || fes_nano_cold_cpu_release_locked()!=1 ||
        captured.core!=MPIDR_AFFLVL1_VAL(read_mpidr()) ||
        fes_nano_cold_consume(&provider,&captured,fes_cold_hw_ops(),chain,owner)) return -1;
#if defined(FES_NANO_SMMU_BOOT_OBSERVE) && FES_NANO_SMMU_BOOT_OBSERVE
    if (smmu_report(&provider,0)) return -1;
#endif
    *record=provider.record.boot;
    *policy=(struct fes_nano_boot_policy){.qualifications=FES_NANO_BOOT_QUALIFIED,
        .history=FES_NANO_BOOT_NEVER_ENABLED,.fabric=FES_NANO_FABRIC_ABSENT,.producers=FES_NANO_PRODUCERS_ALL};
    memcpy(policy->chain,chain,32);
    *masters=(struct fes_nano_master_ops){.context=&provider,.owns=fes_nano_cold_owns,
        .quiesce=fes_nano_cold_quiesce,.quiescent=fes_nano_cold_quiescent};
#ifdef FES_NANO_READER_CPU_BASE
    masters->owns=cold_master_owns;
    masters->quiesce=cold_master_quiesce;
    masters->quiescent=cold_master_quiescent;
#endif
    return 1;
}

#if defined(FES_NANO_CONTROL_PROBE) && FES_NANO_CONTROL_PROBE
static struct fes_nano_identity_probe identity_probe;
static unsigned identity_reported;
struct fes_nano_identity_probe *fes_nano_platform_identity_probe(void) { return &identity_probe; }
#ifdef FES_NANO_READER_CPU_BASE
_Static_assert(FES_NANO_CONTROL_ABI==0x10203041U && FES_NANO_READER_CPU_BASE==0x95000000ULL &&
    FES_NANO_READER_BUS_BASE==0x95000000ULL && FES_NANO_READER_BYTES==8192U &&
    FES_NANO_READER_ARENA_BYTES==(2U<<20) && FES_NANO_PROBE_CPU_BASE==0x20010100UL &&
    FES_NANO_PROBE_SPAN==FES_DE25_DDR_PROBE_SPAN,"source-bound cold DDR reader profile");
_Static_assert(FES_NANO_READER_BUS_BASE==FES_NANO_DDR_FIREWALL_WINDOW &&
    FES_NANO_READER_BYTES==FES_NANO_DDR_FIREWALL_BYTES,"fixed observed DDR firewall window");
static uint32_t firewall_bridge;
static int firewall_owns(void *context)
{
    if (smmu_owns(context)!=1) return 0;
    /* RC28 BRGMODRST.MPFE: never sample its CSR while MPFE is reset. */
    dsbsy();firewall_bridge=mmio_read_32(FES_NANO_COLD_RSTMGR+0x2c);dsbsy();
    return !(firewall_bridge&0x40U) && smmu_owns(context)==1;
}
static int firewall_read(void *context,uint32_t offset,uint32_t *value)
{
    if (!fes_nano_ddr_firewall_offset(offset) || firewall_owns(context)!=1) return -1;
    dsbsy();*value=mmio_read_32(FES_NANO_DDR_FIREWALL_BASE+offset);dsbsy();
    return firewall_owns(context)==1 ? 0 : -1;
}
static int firewall_report(struct fes_nano_cold *s,struct fes_nano_ddr_firewall_observation *out)
{
    struct fes_nano_ddr_firewall_probe probe={.io={s,firewall_owns,firewall_read}};
    struct fes_nano_ddr_firewall_observation observation={0};
    firewall_bridge=0;
    int status=fes_nano_ddr_firewall_observe(&probe,&observation);
    NOTICE("FES_NANO_DDR_FIREWALL status=%d owner=%llx generation=%llu reset=%x mpfe_reset=%u reads=%u enable=%x base=%llx bytes=%u access=unqualified\n",
        status,(unsigned long long)fes_nano_owned_bridges.state.owner,
        (unsigned long long)fes_nano_owned_bridges.state.generation,fes_nano_owned_bridges.state.reset,
        !!(firewall_bridge&0x40U),probe.reads,observation.registers.enable,(unsigned long long)FES_NANO_DDR_FIREWALL_WINDOW,FES_NANO_DDR_FIREWALL_BYTES);
    for (unsigned i=0;i<8;i++) {
        const struct fes_nano_ddr_region *r=&observation.registers.region[i];
        NOTICE("FES_NANO_DDR_REGION index=%u base=%x baseext=%x limit=%x limitext=%x\n",i,r->base,r->baseext,r->limit,r->limitext);
    }
    NOTICE("FES_NANO_DDR_FIREWALL_END layout_known=%u window_covered=%u access=unqualified\n",observation.layout_known,observation.window_covered);
    if (!status && out) *out=observation;
    return status;
}
static struct fes_nano_fabric_stop fabric_stop;
static uint64_t fabric_generation, fabric_reported_generation;
#if defined(FES_NANO_COLD_CONFIG) && FES_NANO_COLD_CONFIG
static uint64_t fabric_configuration_generation;
#endif
static int cold_master_owns(void *context,uint64_t owner,uint64_t epoch)
{
    return GET_EL(read_current_el())==MODE_EL3 && !(read_sctlr_el3()&5U) &&
        fes_nano_cold_cpu_release_locked()==1 && captured.core==MPIDR_AFFLVL1_VAL(read_mpidr()) &&
        fes_nano_cold_owns(context,owner,epoch)==1;
}
static int fabric_owns(void *context)
{
    struct fes_nano_cold *s=context;
    return s->control_expected && identity_probe.active && !identity_probe.first_error &&
        identity_probe.bridges==&fes_nano_owned_bridges &&
        (identity_probe.stage==FES_ID_CLOSE ||
         (identity_probe.reader==2 && (identity_probe.stage==FES_ID_DDR_PREPARE || identity_probe.stage==FES_ID_DDR_RELEASE))) &&
        fes_nano_owned_bridges.state.generation==fabric_generation &&
        fes_nano_owned_bridges.state.reset==(s->ddr_expected ? 5U : FES_NANO_BRIDGES_DATA) &&
        cold_master_owns(s,s->record.boot.owner,s->record.boot.epoch)==1;
}
static int fabric_read(void *context,uint32_t offset,uint32_t *value)
{
    if ((offset>0x4c && offset!=0xb0 && offset!=0xb4 && offset!=0xb8) ||
        offset==FES_EMBEDDED_REG_COMMAND || offset&3U || fabric_owns(context)!=1) return -1;
    dsbsy();*value=mmio_read_32(FES_NANO_CONTROL_BASE+offset);dsbsy();
    return fabric_owns(context)==1 ? 0 : -1;
}
static int fabric_write(void *context,uint32_t offset,uint32_t value)
{
    if ((offset!=FES_EMBEDDED_REG_REQUEST_COOKIE_LO && offset!=FES_EMBEDDED_REG_REQUEST_COOKIE_HI &&
         !(offset==FES_EMBEDDED_REG_COMMAND && value==FES_EMBEDDED_COMMAND_DISABLE)) ||
        identity_probe.stage!=FES_ID_CLOSE || fabric_owns(context)!=1) return -1;
    dsbsy();mmio_write_32(FES_NANO_CONTROL_BASE+offset,value);dsbsy();
    return fabric_owns(context)==1 ? 0 : -1;
}
static int probe_read(void *context,uint32_t offset,uint32_t *value)
{
    switch(offset) {
    case FES_DE25_DDR_PROBE_ID_OFFSET:case FES_DE25_DDR_PROBE_VERSION_OFFSET:
    case FES_DE25_DDR_PROBE_STATUS_OFFSET:case FES_DE25_DDR_PROBE_ACCEPTED_BYTES_OFFSET:
    case FES_DE25_DDR_PROBE_COMPLETED_BYTES_OFFSET:case FES_DE25_DDR_PROBE_ERROR_OFFSET:
    case FES_DE25_DDR_PROBE_ACTIVE_SEQUENCE_OFFSET:case FES_DE25_DDR_PROBE_ACTIVE_GENERATION_LO_OFFSET:
    case FES_DE25_DDR_PROBE_ACTIVE_GENERATION_HI_OFFSET:break;
    default:return -1;
    }
    if (fabric_owns(context)!=1) return -1;
    dsbsy();*value=mmio_read_32(FES_NANO_PROBE_CPU_BASE+offset);dsbsy();
    return fabric_owns(context)==1 ? 0 : -1;
}
static int probe_write(void *context,uint32_t offset,uint32_t value)
{
    if (offset!=FES_DE25_DDR_PROBE_COMMAND_OFFSET || value!=FES_DE25_DDR_PROBE_COMMAND_CANCEL ||
        identity_probe.stage!=FES_ID_CLOSE || fabric_owns(context)!=1) return -1;
    dsbsy();mmio_write_32(FES_NANO_PROBE_CPU_BASE+offset,value);dsbsy();
    return fabric_owns(context)==1 ? 0 : -1;
}
static void fabric_wait(void *context) { (void)context;udelay(10); }
static int master_failure(struct fes_nano_cold *s,int error)
{
    s->failed=1;s->valid=0;
    return error<0 ? error : -1;
}
static int cold_master_quiesce(void *context,uint32_t producers)
{
    struct fes_nano_cold *s=context;
    if (producers!=FES_NANO_PRODUCERS_ALL || identity_probe.first_error ||
        cold_master_owns(s,s->record.boot.owner,s->record.boot.epoch)!=1 || !s->control_expected)
        return master_failure(s,-1);
    if (!fabric_stop.attempted) fabric_generation=fes_nano_owned_bridges.state.generation;
    int error=fes_nano_fabric_stop(&fabric_stop);
    if (fabric_reported_generation!=fabric_generation) {
        fabric_reported_generation=fabric_generation;
        NOTICE("FES_NANO_MASTER_STOP status=%d owner=%llx generation=%llu reset=%x reader_reads=%u reader_polls=%u disable=%u probe_polls=%u cancel=%u complete=%u data=%s\n",
        error,(unsigned long long)fes_nano_owned_bridges.state.owner,
        (unsigned long long)fes_nano_owned_bridges.state.generation,fes_nano_owned_bridges.state.reset,
        fabric_stop.reads,fabric_stop.polls,fabric_stop.disable_issued,fabric_stop.probe.polls,
        fabric_stop.probe.cancel_issued,fabric_stop.done,s->ddr_expected ? "active" : "held");
    }
    return error ? master_failure(s,error) : 0;
}
static int cold_master_quiescent(void *context,uint32_t *producers)
{
    struct fes_nano_cold *s=context;
    if (!producers || cold_master_owns(s,s->record.boot.owner,s->record.boot.epoch)!=1 || fabric_stop.failed)
        return master_failure(s,-1);
    /* Before the live identity match, only independent cold HPS containment is
     * known. boot.contained permits that bitmap while every data path is held.
     * After control closes, retain the receipt without reading a reset aperture. */
    if (fabric_stop.done && s->control_expected && fes_nano_fabric_stopped(&fabric_stop))
        return master_failure(s,fabric_stop.first_error);
    *producers=FES_NANO_PRODUCERS_HPS | (fabric_stop.done ? FES_NANO_PRODUCERS_FPGA : 0U);
    return 0;
}

static struct fes_nano_fabric_stop new_fabric_stop(unsigned qualified)
{
    struct fes_nano_fabric_stop out={
        .io={&provider,fabric_owns,fabric_read,fabric_write,fabric_wait},
        .probe={.io={&provider,fabric_owns,probe_read,probe_write,fabric_wait}},
        .abi=FES_NANO_CONTROL_ABI,.build=FES_NANO_CONTROL_BUILD,
        .bus_base=FES_NANO_READER_BUS_BASE,.bytes=FES_NANO_READER_BYTES,.cookie=7,.qualified=qualified};
#if defined(FES_NANO_COLD_CONFIG) && FES_NANO_COLD_CONFIG
    if(configuration_program.initialized && fes_nano_program_build(&configuration_program,out.build))out.qualified=0;
#endif
    return out;
}

#if defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
_Static_assert(FES_NANO_DDR_READ_ATTRS_VERIFIED==1,"sealed DDR adapter attributes required");
static struct fes_nano_ddr_read ddr_reader;
static unsigned ddr_prepared,ddr_issued;
static uint64_t ddr_generation;
static int ddr_gate(struct fes_nano_cold *s)
{
    struct fes_nano_smmu_observation translation={0};
    struct fes_nano_ddr_firewall_observation firewall={0};
    unsigned words=0;int status=-1;
    if (smmu_owns(s)!=1) goto out;
    if (smmu_report(s,&translation) || firewall_report(s,&firewall)) { status=-5;goto out; }
    if (!translation.bypass_controls_clear || !translation.secure_implemented ||
        translation.registers.idr0!=0x080f7e3f || translation.registers.idr1!=0x0e739d18 ||
        translation.registers.aidr!=1 || translation.registers.s_idr1!=0x80000018 ||
        !firewall.layout_known || !firewall.window_covered) goto out;
    /* The pinned arena belongs to this single CPU until reboot. Check its
     * complete fixed pattern in place; no pointer/digest comes from NS and
     * neither caches nor translation controls are changed to obtain access. */
    for (unsigned i=0;i<FES_NANO_READER_BYTES/4;i++) {
        if (firewall_owns(s)!=1) goto out;
        dsbsy();uint32_t value=mmio_read_32(FES_NANO_READER_CPU_BASE+4*i);dsbsy();words++;
        if (value!=fes_nano_ddr_read_pattern(i) || firewall_owns(s)!=1) goto out;
    }
    status=0;
out:
    NOTICE("FES_NANO_DDR_GATE status=%d owner=%llx generation=%llu reset=%x pattern_words=%u permit=bounded-read\n",
        status,(unsigned long long)fes_nano_owned_bridges.state.owner,
        (unsigned long long)fes_nano_owned_bridges.state.generation,fes_nano_owned_bridges.state.reset,words);
    return status;
}
static int ddr_prepare(void *context)
{
    struct fes_nano_cold *s=context;
    if (ddr_prepared || ddr_issued || fabric_stop.attempted || identity_probe.reader!=2 ||
        identity_probe.stage!=FES_ID_DDR_PREPARE || smmu_owns(s)!=1) return -1;
    fabric_generation=fes_nano_owned_bridges.state.generation;
    struct fes_nano_fabric_stop idle=fabric_stop;
    /* Preparation's MMIO callbacks reject all writes. An unexpectedly active
     * master cannot be stopped and silently turned into a fresh start. */
    if (fes_nano_fabric_stop(&idle) || idle.disable_issued || idle.probe.cancel_issued || ddr_gate(s)) return -1;
    ddr_prepared=1;
    return 0;
}
int fes_nano_platform_ddr_target(unsigned open)
{
    if (!identity_probe.active || identity_probe.reader!=2 ||
        identity_probe.bridges!=&fes_nano_owned_bridges || open>1) return -1;
    if (open) {
        if (!ddr_prepared || ddr_issued || identity_probe.stage!=FES_ID_DDR_RELEASE) return -1;
        ddr_prepared=0;ddr_issued=1;
        /* Fresh use-point check immediately precedes the exact DDR-only raw
         * enable. An earlier observation or preparation receipt cannot grant it. */
        if (ddr_gate(&provider)) return -1;
        ddr_generation=fes_nano_owned_bridges.state.generation+1;
        return fes_nano_cold_ddr_target(&provider,provider.record.boot.owner,provider.record.boot.epoch);
    }
    if (!ddr_issued || !ddr_reader.done || ddr_reader.failed || identity_probe.stage!=FES_ID_CLOSE ||
        !fabric_stop.done || fabric_stop.failed) return -1;
    return fes_nano_cold_ddr_close_target(&provider,provider.record.boot.owner,provider.record.boot.epoch);
}
static int ddr_owns(void *context)
{
    struct fes_nano_cold *s=context;
    return ddr_issued && identity_probe.active && identity_probe.reader==2 && !identity_probe.first_error &&
        identity_probe.stage==FES_ID_DDR_READ && s->control_expected==1 && s->ddr_expected==1 &&
        fes_nano_owned_bridges.state.generation==ddr_generation && fes_nano_owned_bridges.state.reset==5 &&
        cold_master_owns(s,s->record.boot.owner,s->record.boot.epoch)==1;
}
static int ddr_read_word(void *context,uint32_t offset,uint32_t *value)
{
    if (ddr_owns(context)!=1 || offset&3U ||
        (offset>0x4c && offset!=0x94 && offset!=0x98 && offset!=0xa0 && offset!=0xa4 &&
         offset!=0xa8 && offset!=0xac && offset!=0xb0 && offset!=0xb4 && offset!=0xb8) ||
        offset==FES_EMBEDDED_REG_COMMAND) return -1;
    NOTICE("FES_NANO_DDR_ACCESS phase=before ordinal=%u offset=%x value=0\n",ddr_reader.reads,offset);
    dsbsy();*value=mmio_read_32(FES_NANO_CONTROL_BASE+offset);dsbsy();
    NOTICE("FES_NANO_DDR_ACCESS phase=after ordinal=%u offset=%x value=%x\n",ddr_reader.reads,offset,*value);
    return ddr_owns(context)==1 ? 0 : -1;
}
static int ddr_write_word(void *context,uint32_t offset,uint32_t value)
{
    unsigned phase=ddr_reader.phase;
    unsigned valid=(phase==FES_DDR_READ_ENABLE &&
        ((offset==FES_EMBEDDED_REG_REQUEST_GENERATION_LO && value==1) ||
         (offset==FES_EMBEDDED_REG_REQUEST_GENERATION_HI && !value) ||
         (offset==FES_EMBEDDED_REG_COMMAND && value==FES_EMBEDDED_COMMAND_ENABLE))) ||
        (phase==FES_DDR_READ_OFFER && ((offset==0x84 && value==FES_NANO_READER_BUS_BASE) ||
         (offset==0x88 && value==1) || (offset==0x80 && value==8))) ||
        (phase==FES_DDR_READ_DISABLE &&
         ((offset==FES_EMBEDDED_REG_REQUEST_COOKIE_LO && value==7) ||
          (offset==FES_EMBEDDED_REG_REQUEST_COOKIE_HI && !value) ||
          (offset==FES_EMBEDDED_REG_COMMAND && value==FES_EMBEDDED_COMMAND_DISABLE)));
    if (!valid || ddr_owns(context)!=1) return -1;
    NOTICE("FES_NANO_DDR_WRITE phase=before offset=%x value=%x\n",offset,value);
    dsbsy();mmio_write_32(FES_NANO_CONTROL_BASE+offset,value);dsbsy();
    NOTICE("FES_NANO_DDR_WRITE phase=after offset=%x value=%x\n",offset,value);
    return ddr_owns(context)==1 ? 0 : -1;
}
static void ddr_trace(void *context,unsigned phase)
{
    (void)context;
    NOTICE("FES_NANO_DDR_READ phase=%u owner=%llx generation=%llu reset=%x reads=%u writes=%u polls=%u\n",
        phase,(unsigned long long)fes_nano_owned_bridges.state.owner,
        (unsigned long long)fes_nano_owned_bridges.state.generation,fes_nano_owned_bridges.state.reset,
        ddr_reader.reads,ddr_reader.writes,ddr_reader.polls);
}
static int ddr_cycle(void *context)
{
    if (ddr_owns(context)!=1 || ddr_reader.attempted) return -1;
    ddr_reader=(struct fes_nano_ddr_read){.io={context,ddr_owns,ddr_read_word,ddr_write_word,fabric_wait,ddr_trace},
        .abi=FES_NANO_CONTROL_ABI,.build=FES_NANO_CONTROL_BUILD,.bus_base=FES_NANO_READER_BUS_BASE,
        .window_bytes=FES_NANO_READER_BYTES,.qualified=1};
    struct fes_nano_ddr_read_observation observed={0};
    int status=fes_nano_ddr_read(&ddr_reader,&observed);
    NOTICE("FES_NANO_DDR_RESULT status=%d bytes=%u checksum=%x data0=%x data1=%x data2=%x data3=%x generation=%llu cookie=%llu\n",
        status,observed.returned_bytes,observed.checksum,observed.last_data[0],observed.last_data[1],observed.last_data[2],observed.last_data[3],
        (unsigned long long)observed.generation,(unsigned long long)observed.cookie);
    return status;
}
#endif
#endif
static int identity_configured(void *context)
{
    struct fes_nano_cold *s=context;
    uint32_t response[6]={0};unsigned words=6;
    if (s->control_expected || fes_nano_cold_owns(s,s->record.boot.owner,s->record.boot.epoch)!=1) return -1;
    /* CONFIG_STATUS only. The legacy ISDONE frontend can mutate bridge state.
     * Keep the response on the secure stack and require its complete shape. */
    NOTICE("FES_NANO_CONFIG_QUERY command=4 words=6\n");
    int status=mailbox_send_cmd(MBOX_JOB_ID,MBOX_CONFIG_STATUS,0,0,CMD_CASUAL,response,&words);
    unsigned valid=status==0 && words==6;
    unsigned ready=valid && response[0]==0 && (response[2]&PIN_STATUS_NSTATUS) &&
        (response[3]&(SOFTFUNC_STATUS_CONF_DONE|SOFTFUNC_STATUS_INIT_DONE))==
            (SOFTFUNC_STATUS_CONF_DONE|SOFTFUNC_STATUS_INIT_DONE) &&
        !(response[3]&SOFTFUNC_STATUS_SEU_ERROR);
    NOTICE("FES_NANO_CONFIG_STATUS status=%d words=%u state=%x version=%x pins=%x soft=%x error_location=%x error_details=%x ready=%u\n",
           status,words,response[0],response[1],response[2],response[3],response[4],response[5],ready);
#ifdef FES_NANO_READER_CPU_BASE
    if (valid && (smmu_report(s,0) || firewall_report(s,0))) return -5;
#endif
    return valid ? (int)ready : -5;
}
void fes_nano_platform_identity_report(void)
{
    if (!identity_probe.failed || identity_reported++) return;
    NOTICE("FES_NANO_IDENTITY_FAILURE stage=%u error=%d cleanup=%d reads=%u base=%lx\n",
           identity_probe.stage,identity_probe.first_error,identity_probe.cleanup_error,
           identity_probe.reads,(unsigned long)FES_NANO_CONTROL_BASE);
}
static void identity_trace(void *context, unsigned stage)
{
    (void)context;
    if (stage==FES_ID_COMPLETE && identity_probe.reader==2) {
        const struct fes_nano_bridge_closure *c=&identity_probe.closure;
        NOTICE("FES_NANO_DDR_CLOSURE owner=%llx generation=%llu reset=%x hardware_drained=%x data_remained_contained=%u producers_quiescent=%u fabric_acknowledged=%u\n",
            (unsigned long long)c->after.owner,(unsigned long long)c->after.generation,c->after.reset,
            c->hardware_drained,c->data_remained_contained,c->producers_quiescent,c->fabric_acknowledged);
    }
    if (stage>=FES_ID_DDR_PREPARE && stage<=FES_ID_DDR_VERIFY) {
        NOTICE("FES_NANO_DDR_CONTROL phase=%u owner=%llx generation=%llu reset=%x\n",stage,
               (unsigned long long)fes_nano_owned_bridges.state.owner,
               (unsigned long long)fes_nano_owned_bridges.state.generation,fes_nano_owned_bridges.state.reset);
        return;
    }
    if (stage>=FES_ID_READER_ENABLED && stage<=FES_ID_READER_DRAINED) {
        NOTICE("FES_NANO_READER_CYCLE phase=%u reader_generation=1 cookie=7 owner=%llx generation=%llu reset=%x\n",stage,
               (unsigned long long)fes_nano_owned_bridges.state.owner,
               (unsigned long long)fes_nano_owned_bridges.state.generation,
               fes_nano_owned_bridges.state.reset);
        return;
    }
    const char *name=stage==FES_ID_OPEN?"open":stage==FES_ID_READ?"read":
                     stage==FES_ID_CLOSE?"close":stage==FES_ID_COMPLETE?"closed":"contain";
    NOTICE("FES_NANO_IDENTITY phase=%s owner=%llx generation=%llu reset=%x base=%lx\n",name,
           (unsigned long long)fes_nano_owned_bridges.state.owner,
           (unsigned long long)fes_nano_owned_bridges.state.generation,
           fes_nano_owned_bridges.state.reset,(unsigned long)FES_NANO_CONTROL_BASE);
}
int fes_nano_platform_control_target(unsigned open)
{
    if (!identity_probe.active || identity_probe.bridges!=&fes_nano_owned_bridges) return -1;
#ifdef FES_NANO_READER_CPU_BASE
    if (!open && (!fabric_stop.done || fabric_stop.failed)) return -1;
#endif
    return fes_nano_cold_control_target(&provider,fes_nano_owned_bridges.state.owner,
                                          fes_nano_owned_bridges.epoch,open);
}
static int identity_read(void *context, uint32_t offset, uint32_t *value)
{
    struct fes_nano_cold *s=context;
    /* The inspected pin has no EL3 MMU setup. This diagnostic requires both
     * translation and data cache disabled. A changed context needs separate
     * qualification; never turn translation off as a fallback. */
    if (!identity_probe.active || !s->control_expected || (read_sctlr_el3()&5U) ||
        (offset>FES_EMBEDDED_REG_ACK_COOKIE_HI &&
         (!identity_probe.reader || (offset!=0x8c && offset!=0x90))) || offset&3U ||
        fes_nano_cold_owns(s,s->record.boot.owner,s->record.boot.epoch)!=1) return -1;
    dsbsy();
    NOTICE("FES_NANO_IDENTITY_ACCESS phase=before ordinal=%u offset=%x value=0\n",identity_probe.reads,offset);
    *value=mmio_read_32(FES_NANO_CONTROL_BASE+offset);
    NOTICE("FES_NANO_IDENTITY_ACCESS phase=after ordinal=%u offset=%x value=%x\n",identity_probe.reads,offset,*value);
    dsbsy();
    return (read_sctlr_el3()&5U) || fes_nano_cold_owns(s,s->record.boot.owner,s->record.boot.epoch)!=1 ? -1 : 0;
}
static int identity_write(void *context, uint32_t offset, uint32_t value)
{
    struct fes_nano_cold *s=context;
    if (!identity_probe.reader || !identity_probe.active || !s->control_expected ||
        (read_sctlr_el3()&5U) ||
        fes_nano_cold_owns(s,s->record.boot.owner,s->record.boot.epoch)!=1) return -1;
    switch(offset) {
    case FES_EMBEDDED_REG_REQUEST_GENERATION_LO:case FES_EMBEDDED_REG_REQUEST_GENERATION_HI:
    case FES_EMBEDDED_REG_REQUEST_COOKIE_LO:case FES_EMBEDDED_REG_REQUEST_COOKIE_HI:
    case FES_EMBEDDED_REG_COMMAND:case 0x80:case 0x84:case 0x88:break;
    default:return -1;
    }
    dsbsy();
    NOTICE("FES_NANO_READER_WRITE phase=before offset=%x value=%x\n",offset,value);
    mmio_write_32(FES_NANO_CONTROL_BASE+offset,value);dsbsy();
    NOTICE("FES_NANO_READER_WRITE phase=after offset=%x value=%x\n",offset,value);
    return (read_sctlr_el3()&5U) || fes_nano_cold_owns(s,s->record.boot.owner,s->record.boot.epoch)!=1 ? -1 : 0;
}
static int identity_contain(void *context)
{
    struct fes_nano_cold *s=context;
    uintptr_t address=FES_NANO_COLD_RSTMGR+0x2c;
    uint32_t before=mmio_read_32(address);
    /* Failure recovery asserts only control reset. An unexpected data path
     * opening remains a failure; this callback supplies no active data drain. */
    mmio_write_32(address,before|FES_NANO_BRIDGES_CONTROL);dsbsy();
    uint32_t after=mmio_read_32(address);dsbsy();
    s->failed=1;s->valid=0;
    return (before&FES_NANO_BRIDGES_DATA)==FES_NANO_BRIDGES_DATA &&
           (after&FES_NANO_BRIDGES_ALL)==FES_NANO_BRIDGES_ALL ? 0 : -1;
}
int fes_nano_platform_identity_setup(void)
{
    static const uint8_t context[32]=FES_NANO_HPS_CONTEXT_SHA;
    static const uint8_t map[32]=FES_NANO_CONTROL_MAP_SHA;
    static const uint32_t build[4]=FES_NANO_CONTROL_BUILD;
    unsigned context_nonzero=0,map_nonzero=0;
    for(unsigned i=0;i<32;i++) { context_nonzero|=context[i];map_nonzero|=map[i]; }
    uint64_t sctlr=read_sctlr_el3();
    unsigned qualified=context_nonzero && map_nonzero && !(sctlr&5U) &&
        FES_NANO_CONTROL_BASE>=0x20000000UL && FES_NANO_CONTROL_BASE<=0x3fffff00UL &&
        !(FES_NANO_CONTROL_BASE&3U);
    struct fes_nano_identity_ops io={.context=&provider,.configured=identity_configured,.read32=identity_read,.contain=identity_contain,.trace=identity_trace,.write32=identity_write};
#if defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
    io.ddr_prepare=ddr_prepare;io.ddr_cycle=ddr_cycle;
#endif
    int result=fes_nano_identity_init(&identity_probe,&fes_nano_owned_bridges,io,
                                      FES_NANO_CONTROL_ABI,build,qualified);
#ifdef FES_NANO_READER_CPU_BASE
    if (!result) fabric_stop=new_fabric_stop(qualified);
#endif
    if (result) NOTICE("FES_NANO_IDENTITY_FAILURE stage=%u error=%d sctlr=%llx base=%lx\n",
                      identity_probe.stage,identity_probe.first_error,(unsigned long long)sctlr,
                      (unsigned long)FES_NANO_CONTROL_BASE);
    return result ? -1 : 1;
}
#endif

#if defined(FES_NANO_COLD_CONFIG) && FES_NANO_COLD_CONFIG
#if !defined(FES_NANO_CONTROL_PROBE) || !FES_NANO_CONTROL_PROBE
#error "Cold configuration requires the source-bound control profile"
#endif
#include "sha.h"
static struct fes_nano_configuration configuration;
/* Diagnostic time only; these counters never grant configuration authority. */
static uint64_t configuration_hash_ticks;
struct fes_nano_configuration *fes_nano_platform_configuration(void) { return &configuration; }
int fes_nano_platform_configuration_diagnostic_ready(void)
{
    return !(read_sctlr_el3()&5U) && !provider.control_expected &&
           fes_nano_cold_owns(&provider,provider.record.boot.owner,provider.record.boot.epoch)==1;
}
static int configuration_guard(void *context,uint64_t owner,uint64_t generation)
{
    struct fes_nano_bridge_snapshot current;
    return context!=&provider || !fes_nano_platform_configuration_diagnostic_ready() ||
        GET_EL(read_current_el())!=MODE_EL3 || fes_nano_cold_cpu_release_locked()!=1 ||
        captured.core!=MPIDR_AFFLVL1_VAL(read_mpidr()) || fes_nano_owned_bridges.device_lease ||
        fes_nano_bridges_observe(&fes_nano_owned_bridges,&current) ||
        current.owner!=owner || current.generation!=generation ||
        current.reset!=FES_NANO_BRIDGES_ALL || !current.masters_contained ? -1 : 0;
}
static int configuration_descriptor(void *context,uint8_t result[FES_NANO_PROGRAM_HEADER_BYTES])
{
    if(context!=&provider || !fes_nano_platform_configuration_diagnostic_ready())return -1;
    dsbsy();
    memcpy(result,(const void *)(uintptr_t)(FES_NANO_CONFIG_BASE+FES_NANO_CONFIG_ARENA_SIZE-
                                          FES_NANO_PROGRAM_METADATA_BYTES),FES_NANO_PROGRAM_HEADER_BYTES);
    dsbsy();
    return fes_nano_platform_configuration_diagnostic_ready() ? 0 : -1;
}
static int configuration_bind(void *context,uint64_t owner,uint64_t generation,uint32_t *bytes)
{
    if(context!=&provider || !bytes || fes_nano_program_capture(&configuration_program,owner,generation))return -1;
    *bytes=configuration_program.bytes;
    return 0;
}
static int configuration_check(void *context, uint64_t address, uint32_t bytes, unsigned whole)
{
    struct fes_nano_cold *s=context;
    uint8_t observed[48];const uint8_t *expected;
    if(whole>1 || s->control_expected || (read_sctlr_el3()&5U) ||
       fes_nano_cold_owns(s,s->record.boot.owner,s->record.boot.epoch)!=1 ||
       configuration_guard(context,configuration.owner,configuration.generation) ||
       address<FES_NANO_CONFIG_BASE || address-FES_NANO_CONFIG_BASE>UINT32_MAX)return -1;
    expected=fes_nano_program_expected(&configuration_program,(uint32_t)(address-FES_NANO_CONFIG_BASE),bytes,whole);
    if(!expected)return -1;
    dsbsy();
    /* Use the already pinned vendor's pure SHA-384 primitives. Its convenience
     * sha384_start wrapper also touches the watchdog and is not used here. */
    uint64_t before=read_cntpct_el0();
    sha512_context hash;
    sha384_init(&hash);sha384_update(&hash,(const uint8_t *)(uintptr_t)address,bytes);sha384_finish(&hash,observed);
    dsbsy();
    configuration_hash_ticks+=read_cntpct_el0()-before;
    return configuration_guard(context,configuration.owner,configuration.generation) ||
           memcmp(expected,observed,48) ? -1 : 0;
}
#ifdef FES_NANO_READER_CPU_BASE
static int configuration_return_generation(uint64_t owner,uint64_t generation)
{
    uint64_t returned;
    if(fabric_generation==UINT64_MAX ||
       !fes_nano_bridges_held_return(&fes_nano_owned_bridges,&returned) ||
       returned!=fabric_generation+1)return 0;
    if(generation==returned)return 1;
#if defined(FES_NANO_CONTROLLED_SD) && FES_NANO_CONTROLLED_SD
    return fes_nano_sd_returned(&fes_nano_owned_sd,owner,returned,generation)==0;
#else
    (void)owner;return 0;
#endif
}
#endif
static int configuration_complete(void *context, uint64_t owner, uint64_t generation, unsigned rearmed)
{
    struct fes_nano_bridge_snapshot current;
    if(context!=&provider || configuration.bridges!=&fes_nano_owned_bridges ||
       !configuration.initialized || !configuration.prepared || !configuration.started ||
       configuration.finished || configuration.failed || configuration.issued || configuration.pending ||
       configuration.completed!=configuration.bytes || !configuration.bytes ||
       configuration.owner!=owner || configuration.generation!=generation || configuration.rearmed!=rearmed ||
       !fes_nano_platform_configuration_diagnostic_ready() ||
       fes_nano_bridges_observe(&fes_nano_owned_bridges,&current) || current.owner!=owner ||
       current.generation!=generation || current.reset!=FES_NANO_BRIDGES_ALL || !current.masters_contained ||
       current.never_enabled!=(rearmed ? 0U : 1U))return -1;
#ifdef FES_NANO_READER_CPU_BASE
    if(!identity_probe.initialized || identity_probe.active || identity_probe.failed ||
       identity_probe.first_error || identity_probe.cleanup_error || fabric_stop.qualified!=1 ||
       fabric_stop.failed || fabric_stop.first_error || fabric_stop.probe.failed)return -1;
#if defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
    if(rearmed)return -1; /* Active-DDR runtime state has no repeated-load contract. */
#endif
    if(rearmed ? (!fabric_configuration_generation || generation<=fabric_configuration_generation ||
                  !fabric_stop.attempted || !fabric_stop.done || !fabric_stop.probe.done ||
                  !configuration_return_generation(owner,generation) ||
                  identity_probe.stage!=FES_ID_COMPLETE) :
                 (fabric_configuration_generation || fabric_stop.attempted || fabric_stop.done))return -1;
    uint64_t previous=fabric_configuration_generation;
    /* A successful new configuration ends the preceding owned activation.
     * Construct a fresh stop from the compiled inventory; never reset a failed
     * activation or accept its cached reader/probe receipts for the new image. */
    fabric_stop=new_fabric_stop(1);
    if(fabric_stop.qualified!=1)return -1;
    fabric_configuration_generation=generation;fabric_generation=0;
    NOTICE("FES_NANO_FABRIC_ACTIVATION owner=%llx generation=%llu previous=%llu rearm=%u complete=1 data_held=1\n",
           (unsigned long long)owner,(unsigned long long)generation,(unsigned long long)previous,rearmed);
#endif
    if(fes_nano_program_build(&configuration_program,identity_probe.build))return -1;
    return 0;
}
static void configuration_trace(void *context, unsigned stage, uint32_t completed, uint32_t pending)
{
    (void)context;
    static const char *names[]={"init","arm","start","write","poll","status","complete","FAIL"};
    const char *name=stage<sizeof(names)/sizeof(names[0]) ? names[stage] : "FAIL";
    NOTICE("FES_NANO_CORE_TRANSFER phase=%s owner=%llx generation=%llu completed=%u pending=%u total=%u base=%llx counter=%llx hash_ticks=%llx\n",
           name,(unsigned long long)fes_nano_owned_bridges.state.owner,
           (unsigned long long)fes_nano_owned_bridges.state.generation,
           completed,pending,configuration.bytes,(unsigned long long)FES_NANO_CONFIG_BASE,
           (unsigned long long)read_cntpct_el0(),(unsigned long long)configuration_hash_ticks);
}
int fes_nano_platform_configuration_setup(void)
{
    static const uint8_t context[32]=FES_NANO_HPS_CONTEXT_SHA;
    unsigned qualified=!(read_sctlr_el3()&5U) &&
        FES_NANO_CONFIG_BASE==0x94800000ULL && FES_NANO_CONFIG_ARENA_SIZE==(8U<<20) &&
        FES_NANO_CONFIG_CHUNK>0 && FES_NANO_CONFIG_CHUNK<=0xffff0U && !(FES_NANO_CONFIG_CHUNK&63U);
    struct fes_nano_program_ops program={&provider,configuration_guard,configuration_descriptor};
    if(fes_nano_program_init(&configuration_program,program,context,FES_NANO_PROGRAM_MAX_BYTES,
                             FES_NANO_CONFIG_CHUNK,qualified))return -1;
    struct fes_nano_configuration_ops io={.context=&provider,.check=configuration_check,.trace=configuration_trace,.complete=configuration_complete,.bind=configuration_bind};
    return fes_nano_configuration_init(&configuration,&fes_nano_owned_bridges,io,FES_NANO_CONFIG_BASE,
                                       FES_NANO_PROGRAM_MAX_BYTES,FES_NANO_CONFIG_CHUNK,qualified) ? -1 : 1;
}
#endif

#if defined(FES_NANO_CONTROLLED_SD) && FES_NANO_CONTROLLED_SD
static struct fes_nano_sd_platform sd_platform;
static unsigned sd_reported;
static int sd_read(void *context, uintptr_t address, uint32_t *value)
{ (void)context; dsbsy(); *value=mmio_read_32(address); dsbsy(); return 0; }
static int sd_write(void *context, uintptr_t address, uint32_t value)
{ (void)context; dsbsy(); mmio_write_32(address,value); dsbsy(); return 0; }
static void sd_barrier(void *context) { (void)context; dsbsy(); }
static void sd_wait(void *context) { (void)context; udelay(10); }
void fes_nano_platform_sd_report(void)
{
    if (sd_reported++ || (!sd_platform.failed && !fes_nano_owned_sd.failed)) return;
    NOTICE("FES_NANO_SD_FAILURE stage=%u error=%d address=%lx value=%08x mask=%08x expected=%08x\n",
           sd_platform.stage, sd_platform.first_error, (unsigned long)sd_platform.last_address,
           sd_platform.last_value, sd_platform.last_mask, sd_platform.last_expected);
    for (unsigned i=0; i<FES_NANO_SD_CLOCK_REGS; i++)
        NOTICE("FES_NANO_SD_CLOCK index=%u captured=%08x observed=%08x\n",
               i,sd_platform.clocks[i],sd_platform.observed[i]);
}
int fes_nano_platform_sd_setup(void)
{
    struct fes_nano_sd_io io={0,sd_read,sd_write,sd_barrier,sd_wait};
    int result=fes_nano_sd_platform_init(&sd_platform,&provider,io,FES_NANO_SD_QUALIFIED);
    if (!result) result=fes_nano_sd_init(&fes_nano_owned_sd,&fes_nano_owned_bridges,
                                       fes_nano_sd_platform_ops(&sd_platform),FES_NANO_SD_QUALIFIED);
    if (result) { fes_nano_platform_sd_report(); return -1; }
    return 1;
}
#endif
