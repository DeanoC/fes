/* SPDX-License-Identifier: BSD-3-Clause */
#include <arch_helpers.h>
#include <drivers/delay_timer.h>
#include <lib/mmio.h>
#include <string.h>
#include "nano_bridge_platform.h"
#include "nano_cold.h"
#include "fes_nano_cold_profile.h"
#define FES_COLD_READ32(a) mmio_read_32(a)
#define FES_COLD_READ16(a) mmio_read_16(a)
#define FES_COLD_WRITE32(a,v) mmio_write_32(a,v)
#define FES_COLD_WRITE16(a,v) mmio_write_16(a,v)
#define FES_COLD_BARRIER() dsbsy()
#define FES_COLD_WAIT() udelay(10)
#include "nano_cold_mmio.inc"
#if defined(FES_NANO_CONTROL_PROBE) && FES_NANO_CONTROL_PROBE
#include <common/debug.h>
#include <socfpga_mailbox.h>
#endif
#if defined(FES_NANO_CONTROLLED_SD) && FES_NANO_CONTROLLED_SD
#include <common/debug.h>
#include "nano_sd_platform.h"
#endif

static struct fes_nano_cold_record captured;
static struct fes_nano_cold provider;
static unsigned capture_attempted, capture_valid;

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
    *record=provider.record.boot;
    *policy=(struct fes_nano_boot_policy){.qualifications=FES_NANO_BOOT_QUALIFIED,
        .history=FES_NANO_BOOT_NEVER_ENABLED,.fabric=FES_NANO_FABRIC_ABSENT,.producers=FES_NANO_PRODUCERS_ALL};
    memcpy(policy->chain,chain,32);
    *masters=(struct fes_nano_master_ops){.context=&provider,.owns=fes_nano_cold_owns,
        .quiesce=fes_nano_cold_quiesce,.quiescent=fes_nano_cold_quiescent};
    return 1;
}

#if defined(FES_NANO_CONTROL_PROBE) && FES_NANO_CONTROL_PROBE
static struct fes_nano_identity_probe identity_probe;
static unsigned identity_reported;
struct fes_nano_identity_probe *fes_nano_platform_identity_probe(void) { return &identity_probe; }
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
        offset>FES_EMBEDDED_REG_ACK_COOKIE_HI || offset&3U ||
        fes_nano_cold_owns(s,s->record.boot.owner,s->record.boot.epoch)!=1) return -1;
    dsbsy();
    NOTICE("FES_NANO_IDENTITY_ACCESS phase=before ordinal=%u offset=%x value=0\n",identity_probe.reads,offset);
    *value=mmio_read_32(FES_NANO_CONTROL_BASE+offset);
    NOTICE("FES_NANO_IDENTITY_ACCESS phase=after ordinal=%u offset=%x value=%x\n",identity_probe.reads,offset,*value);
    dsbsy();
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
    struct fes_nano_identity_ops io={.context=&provider,.configured=identity_configured,.read32=identity_read,.contain=identity_contain,.trace=identity_trace};
    int result=fes_nano_identity_init(&identity_probe,&fes_nano_owned_bridges,io,
                                      FES_NANO_CONTROL_ABI,build,qualified);
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
static const uint8_t configuration_hash[48]=FES_NANO_CONFIG_SHA384;
static const uint8_t configuration_chunks[][48]=FES_NANO_CONFIG_CHUNK_DIGESTS;
struct fes_nano_configuration *fes_nano_platform_configuration(void) { return &configuration; }
int fes_nano_platform_configuration_diagnostic_ready(void)
{
    return !(read_sctlr_el3()&5U) && !provider.control_expected &&
           fes_nano_cold_owns(&provider,provider.record.boot.owner,provider.record.boot.epoch)==1;
}
static int configuration_check(void *context, uint64_t address, uint32_t bytes, unsigned whole)
{
    struct fes_nano_cold *s=context;
    uint8_t observed[48];const uint8_t *expected;
    if(whole>1 || s->control_expected || (read_sctlr_el3()&5U) ||
       fes_nano_cold_owns(s,s->record.boot.owner,s->record.boot.epoch)!=1 ||
       address<FES_NANO_CONFIG_BASE || address>FES_NANO_CONFIG_BASE+FES_NANO_CONFIG_BYTES ||
       !bytes || bytes>FES_NANO_CONFIG_BYTES-(address-FES_NANO_CONFIG_BASE))return -1;
    if(whole) {
        if(address!=FES_NANO_CONFIG_BASE || bytes!=FES_NANO_CONFIG_BYTES)return -1;
        expected=configuration_hash;
    } else {
        uint64_t offset=address-FES_NANO_CONFIG_BASE;
        unsigned index=(unsigned)(offset/FES_NANO_CONFIG_CHUNK);
        uint32_t amount=FES_NANO_CONFIG_BYTES-(uint32_t)offset;
        if(amount>FES_NANO_CONFIG_CHUNK)amount=FES_NANO_CONFIG_CHUNK;
        if(offset%FES_NANO_CONFIG_CHUNK || index>=sizeof(configuration_chunks)/48 || bytes!=amount)return -1;
        expected=configuration_chunks[index];
    }
    dsbsy();
    /* Use the already pinned vendor's pure SHA-384 primitives. Its convenience
     * sha384_start wrapper also touches the watchdog and is not used here. */
    uint64_t before=read_cntpct_el0();
    sha512_context hash;
    sha384_init(&hash);sha384_update(&hash,(const uint8_t *)(uintptr_t)address,bytes);sha384_finish(&hash,observed);
    dsbsy();
    configuration_hash_ticks+=read_cntpct_el0()-before;
    return (read_sctlr_el3()&5U) || fes_nano_cold_owns(s,s->record.boot.owner,s->record.boot.epoch)!=1 ||
           memcmp(expected,observed,48) ? -1 : 0;
}
static void configuration_trace(void *context, unsigned stage, uint32_t completed, uint32_t pending)
{
    (void)context;
    static const char *names[]={"init","arm","start","write","poll","status","complete","FAIL"};
    const char *name=stage<sizeof(names)/sizeof(names[0]) ? names[stage] : "FAIL";
    NOTICE("FES_NANO_CORE_TRANSFER phase=%s owner=%llx generation=%llu completed=%u pending=%u total=%u base=%llx counter=%llx hash_ticks=%llx\n",
           name,(unsigned long long)fes_nano_owned_bridges.state.owner,
           (unsigned long long)fes_nano_owned_bridges.state.generation,
           completed,pending,FES_NANO_CONFIG_BYTES,(unsigned long long)FES_NANO_CONFIG_BASE,
           (unsigned long long)read_cntpct_el0(),(unsigned long long)configuration_hash_ticks);
}
int fes_nano_platform_configuration_setup(void)
{
    unsigned nonzero=0;
    for(unsigned i=0;i<48;i++)nonzero|=configuration_hash[i];
    unsigned qualified=nonzero && !(read_sctlr_el3()&5U) &&
        FES_NANO_CONFIG_BASE==0x94800000ULL && FES_NANO_CONFIG_ARENA_SIZE==(8U<<20) &&
        FES_NANO_CONFIG_BYTES<=FES_NANO_CONFIG_ARENA_SIZE &&
        FES_NANO_CONFIG_CHUNK>0 && FES_NANO_CONFIG_CHUNK<=0xffff0U && !(FES_NANO_CONFIG_CHUNK&63U) &&
        sizeof(configuration_chunks)/48==(FES_NANO_CONFIG_BYTES+FES_NANO_CONFIG_CHUNK-1)/FES_NANO_CONFIG_CHUNK;
    struct fes_nano_configuration_ops io={.context=&provider,.check=configuration_check,.trace=configuration_trace};
    return fes_nano_configuration_init(&configuration,&fes_nano_owned_bridges,io,FES_NANO_CONFIG_BASE,
                                       FES_NANO_CONFIG_BYTES,FES_NANO_CONFIG_CHUNK,qualified) ? -1 : 1;
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
