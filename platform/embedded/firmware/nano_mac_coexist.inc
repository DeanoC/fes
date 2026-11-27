/* SPDX-License-Identifier: BSD-3-Clause */
/* Called under the existing SiP lock at MENU DDR release and before its
 * close. MAC0 stays in reset. Its Non-secure view never maps SDM SID 10. */
#define MAC_CMD UINT32_C(0x94018000)
#define MAC_EVT UINT32_C(0x94019000)
#define MAC_SMMU UINT32_C(0x16000000)
extern unsigned char __FES_MAC_QUEUES_START__[],__FES_MAC_QUEUES_END__[];
static struct {
    uint64_t generation;
    uint32_t displayed;
    uint8_t active, completed;
} mac_window;

struct mac_transaction {
    uint64_t start,last;
    uint32_t table,cmd,evt,control,cursor,commands;
    enum { WINDOW_OPEN, WINDOW_CLOSE, WINDOW_RUNNING } phase;
};

#if defined(FES_NANO_MAC_MENU_SUPPLIER) && FES_NANO_MAC_MENU_SUPPLIER
#include "nano_mac_channel.h"
#include "nano_mac_phy.h"
enum menu_grant_phase { MENU_NO_MAC, MENU_MAPPING, MENU_PHY, MENU_READY, MENU_CONFIRMED, MENU_STOPPING, MENU_REVOKED };
static struct {
    struct fes_nano_mac_phy phy;
    fes_channel channel;
    uint64_t generation, menu_generation;
    uint16_t vmid;
    uint8_t phase, withdrawn;
} menu_mac __attribute__((section(".data.fes_menu_mac")));
static int mac_controller_expected(void);
#else
static int mac_controller_expected(void)
{
    return !fes_nano_owned_mac.attempted && !fes_nano_owned_mac.initialized && !fes_nano_owned_mac.failed;
}
#endif

#if defined(FES_NANO_MAC_C64_SCOPE_SUPPLIER) && FES_NANO_MAC_C64_SCOPE_SUPPLIER
#include "nano_mac_c64.inc"
#endif
static int mac_window_role(unsigned phase,uint64_t gen,const struct fes_nano_bridges *b)
{
#if defined(FES_NANO_MAC_C64_SCOPE_SUPPLIER) && FES_NANO_MAC_C64_SCOPE_SUPPLIER
    if(configuration_program.role==FES_NANO_PROGRAM_ROLE_SPECTRUM48)
        return phase==WINDOW_RUNNING && gen==configuration.generation && c64_platform();
#endif
    return configuration_program.role==FES_NANO_PROGRAM_ROLE_MENU &&
        configuration_program.control_abi==FES_NANO_PROGRAM_MENU_ABI && identity_probe.abi==FES_NANO_PROGRAM_MENU_ABI &&
        identity_probe.bridges==b && identity_probe.active && !identity_probe.failed && !identity_probe.first_error &&
        !identity_probe.cleanup_error && identity_probe.reader==FES_NANO_READER_WINDOW &&
        identity_probe.stage==(phase==WINDOW_CLOSE ? FES_ID_CLOSE:phase==WINDOW_RUNNING ? FES_ID_DDR_READ:FES_ID_DDR_RELEASE) && provider.control_expected==1 &&
        provider.ddr_expected==(phase!=WINDOW_OPEN) && !window_prepared && window_issued==1 && !fabric_stop.attempted && !fabric_stop.failed &&
        gen<=UINT64_MAX-2 && b->state.generation==gen+(phase!=WINDOW_OPEN ? 2U:1U) && b->state.reset==(phase!=WINDOW_OPEN ? 5U:13U);
}

static int mac_window_owner(unsigned phase)
{
    const struct nano_retire *r=&source_retirement;
    const struct fes_nano_bridges *b=&fes_nano_owned_bridges;
    const uint64_t gen=r->binding.generation;
    return FES_PROBE_CPU() && FES_PROBE_FREQUENCY()==400000000 && fes_nano_mmu_platform_scope() &&
        sdm_probe_geometry() && FES_PROBE_SYMBOL(__FES_MAC_TABLE_START__)==NANO_MAC_TABLE_BASE &&
        FES_PROBE_SYMBOL(__FES_MAC_TABLE_END__)==NANO_MAC_TABLE_BASE+NANO_MAC_TABLE_BYTES &&
        FES_PROBE_SYMBOL(__FES_MAC_QUEUES_START__)==MAC_CMD && FES_PROBE_SYMBOL(__FES_MAC_QUEUES_END__)==MAC_EVT+4096 &&
        mac_handoff.frozen && !mac_handoff.failed && mac_handoff.slot==r &&
        mac_handoff.generation==gen && mac_handoff.owner==r->binding.owner &&
        mac_handoff.epoch==r->binding.epoch && mac_handoff.cursor==r->queue_target &&
        r->phase==RET_COMPLETE && r->continued && r->retired && !r->failed && !r->first_error &&
        !r->commands.failed && r->command_receipt.commands_completed && nano_sdm_input_same(&sdm_arm_probe.input,&r->binding) &&
        sdm_input_source(&sdm_arm_probe,&sdm_arm_probe.input)==1 && !sdm_source_failed && !sdm_source_released &&
        !configuration.failed && configuration.finished && !configuration.pending && !configuration.issued &&
        configuration.completed==configuration.bytes && configuration.bridges==b &&
        configuration.owner==r->binding.owner && configuration.generation==gen && mac_window_role(phase,gen,b) &&
        !b->failed && !b->device_lease && b->state.owner==r->binding.owner && b->epoch==r->binding.epoch &&
        !sd_platform.active && !sd_platform.prepared && !sd_platform.failed &&
        mac_controller_expected() &&
        fes_nano_cold_owns(&provider,r->binding.owner,r->binding.epoch)==1;
}

static uint64_t mac_window_word(unsigned offset)
{
#if defined(FES_NANO_MAC_MENU_SUPPLIER) && FES_NANO_MAC_MENU_SUPPLIER
    if(offset>=64 && offset<128 && menu_mac.vmid){
        if(offset==64 && menu_mac.withdrawn)return 0;
        return nano_stage2_ste_word((offset-64)/8,menu_mac.vmid,NANO_MAC_TABLE_BASE+0x4000);
    }
#endif
    return mac_table_word(offset);
}
static int mac_window_resets(void)
{
#if defined(FES_NANO_MAC_MENU_SUPPLIER) && FES_NANO_MAC_MENU_SUPPLIER
    uint32_t released;
    return fes_nano_menu_mac_reset_mask(&provider,&released)==1 &&
        mac_handoff_reg(0x10d11024,0x101,0x101U & ~released);
#else
    return mac_handoff_reg(0x10d11024,0x101,0x101);
#endif
}

static uint32_t mac_reg(uint32_t offset){return FES_PROBE_READ32(MAC_SMMU+offset);}
static int mac_window_scope(struct mac_transaction *t)
{
    if(!mac_window_owner(t->phase) || !sdm_source_secure_scope())return 0;
    uint64_t now=FES_PROBE_COUNTER()/400U;
    if(now<t->last || now-t->start>=NANO_RETIRE_US)return 0;
    t->last=now;
    FES_PROBE_BARRIER();
    return mac_window_resets() && mac_handoff_reg(0x10d12184,UINT32_MAX,3) &&
        mac_handoff_reg(0x10d121b0,UINT32_MAX,0xa000a) && mac_handoff_reg(0x10d1219c,UINT32_MAX,3) &&
        mac_handoff_reg(0x10d121c8,UINT32_MAX,0x10001) && mac_reg(0)==0x080f7e3f && mac_reg(4)==0x0e739d18 &&
        mac_reg(0x14)==0x00400075 && mac_reg(0x28)==0x820 && mac_reg(0x2c)==4 &&
        mac_reg(0x44)==0x100000 && !mac_reg(0x48) && !mac_reg(0x50) && !mac_reg(0x54) &&
        !mac_reg(0x60) && !mac_reg(0x64) && !mac_reg(0x84) && mac_reg(0x88)==8 &&
        !mac_reg(0x94) && !mac_reg(0xa4) && !mac_reg(0x100a8) && !mac_reg(0x100ac);
}
static int mac_window_fixed(struct mac_transaction *t)
{
    return mac_window_scope(t) && mac_reg(0x80)==t->table && mac_reg(0x90)==(t->cmd|3U) &&
        mac_reg(0xa0)==(t->evt|3U) && mac_reg(0x20)==t->control;
}
static int mac_window_stable(struct mac_transaction *t)
{
    return mac_window_fixed(t) && mac_reg(0x24)==t->control &&
        mac_reg(0x98)==t->cursor && mac_reg(0x9c)==t->cursor;
}
/* Both fixed bases fit in the verified zero high half. Never write a queue
 * consumer or an event cursor. Reject a controller that moved at a base switch. */
static int mac_window_base(struct mac_transaction *t,uint32_t reg,uint32_t *base,uint32_t value)
{
    if(t->control || !mac_window_stable(t))return 0;
    FES_PROBE_WRITE32(MAC_SMMU+reg,value|(reg==0x80 ? 0U:3U));
    FES_PROBE_BARRIER();*base=value;
    return mac_window_stable(t);
}
static int mac_window_enable(struct mac_transaction *t,uint32_t next)
{
    if(!mac_window_stable(t))return 0;
    uint32_t prior=t->control;
    FES_PROBE_WRITE32(MAC_SMMU+0x20,next);
    FES_PROBE_BARRIER();t->control=next;
    for(unsigned n=0;n<4096;n++){
        if(!mac_window_fixed(t) || mac_reg(0x98)!=t->cursor || mac_reg(0x9c)!=t->cursor)return 0;
        uint32_t ack=mac_reg(0x24);
        if(ack==next)return mac_window_stable(t);
        if(ack!=prior)return 0;
    }
    return 0;
}
static int mac_window_pair(struct mac_transaction *t,uint64_t op,uint64_t arg)
{
    if(t->cmd!=MAC_CMD || (t->control!=8 && t->control!=13) || !mac_window_stable(t))return 0;
    uint32_t prior=t->cursor,next=(prior+2)&15U;
    const uint32_t p=MAC_CMD+(prior&7U)*16U,s=MAC_CMD+((prior+1)&7U)*16U;
    const uint32_t addresses[]={p,p+8,s,s+8};
    const uint64_t words[]={op,arg,UINT64_C(0x0fc00046),0};
    for(unsigned i=0;i<4;i++){
        if(!mac_window_stable(t))return 0;
        FES_PROBE_STORE64(addresses[i],words[i]);
    }
    FES_PROBE_BARRIER();
    for(unsigned i=0;i<4;i++)
        if(!mac_window_stable(t) || FES_PROBE_LOAD64(addresses[i])!=words[i])return 0;
    if(!mac_window_stable(t))return 0;
    FES_PROBE_WRITE32(MAC_SMMU+0x98,next);FES_PROBE_BARRIER();
    uint32_t seen=0;
    for(unsigned n=0;n<4096;n++){
        if(!mac_window_fixed(t) || mac_reg(0x24)!=t->control || mac_reg(0x98)!=next)return 0;
        uint32_t c=mac_reg(0x9c),progress=(c-prior)&15U;
        if(c>15 || progress<seen || progress>2)return 0;
        if(progress==2){
            t->cursor=next;t->commands+=2;
            return mac_window_stable(t);
        }
        seen=progress;
    }
    return 0;
}
static int mac_window_invalidate(struct mac_transaction *t)
{
    return mac_window_pair(t,4,31) && mac_window_pair(t,0x20,0) && mac_window_pair(t,0x30,0);
}
/* Verify all stream entries and original SDM words under the window owner.
 * Limit each bank of loads with a fresh guard and counter. */
static int mac_window_tables(struct mac_transaction *t)
{
    uint64_t words[NANO_MAC_TABLE_BATCH/8];
    for(unsigned n=0;n<NANO_SDM_INPUT_ARENA_BYTES;n+=NANO_MAC_TABLE_BATCH){
        if(!mac_window_stable(t))return 0;
        if(n<NANO_MAC_TABLE_BYTES)for(unsigned i=0;i<NANO_MAC_TABLE_BATCH;i+=8)
            if(FES_PROBE_LOAD64(NANO_MAC_TABLE_BASE+n+i)!=mac_window_word(n+i))return 0;
        if(!nano_sdm_input_table_range(n,NANO_MAC_TABLE_BATCH))continue;
        if(nano_sdm_input_words(&source_retirement.binding,n,words,NANO_MAC_TABLE_BATCH/8))return 0;
        for(unsigned i=0;i<NANO_MAC_TABLE_BATCH/8;i++)
            if(FES_PROBE_LOAD64(NANO_SDM_INPUT_ARENA+n+8*i)!=words[i])return 0;
    }
    return mac_window_stable(t);
}
static int mac_window_frame(struct mac_transaction *t,uint32_t *sequence)
{
    /* Do not refresh STATUS at close: the real presenter latches its final
     * generation/sequence atomically on STATUS before disabling the endpoint.
     * Generic reader disable cannot modify these FMS1 snapshot registers. */
    uint32_t previous=0;
    for(unsigned pass=0;pass<2;pass++){
        if(!mac_window_stable(t))return 0;
        if(FES_PROBE_READ32(FES_NANO_CONTROL_BASE+0xc4)!=0x31534d46 ||
           FES_PROBE_READ32(FES_NANO_CONTROL_BASE+0xc8)!=1 ||
           FES_PROBE_READ32(FES_NANO_CONTROL_BASE+0xb0)!=0x95000000 ||
           FES_PROBE_READ32(FES_NANO_CONTROL_BASE+0xb4)!=(8U<<20))return 0;
        if(t->phase!=WINDOW_CLOSE){
            uint32_t flags=FES_PROBE_READ32(FES_NANO_CONTROL_BASE+0x94);
            if(t->phase==WINDOW_RUNNING && (!(flags&2U) || (flags&~6U)))return 0;
        }
        FES_PROBE_BARRIER();
        uint32_t gen=FES_PROBE_READ32(FES_NANO_CONTROL_BASE+0x98),hi=FES_PROBE_READ32(FES_NANO_CONTROL_BASE+0x9c);
        uint32_t accepted=FES_PROBE_READ32(FES_NANO_CONTROL_BASE+0xa0),shown=FES_PROBE_READ32(FES_NANO_CONTROL_BASE+0xa4);
        if(FES_PROBE_READ32(FES_NANO_CONTROL_BASE+0xa8) ||
           FES_PROBE_READ32(FES_NANO_CONTROL_BASE+0xac)!=(shown ? (shown-1U)&1U:0U) ||
           hi || gen!=(t->phase!=WINDOW_OPEN) || accepted!=shown || (!!shown)!=(t->phase!=WINDOW_OPEN) ||
           (pass && shown!=previous) || !mac_window_stable(t))return 0;
        previous=shown;
    }
    *sequence=previous;return 1;
}
static int mac_window_failed(unsigned step)
{
    NOTICE("FES_NANO_MAC_COEXIST_STOP step=%u generation=%llu restoration_verified=0 grant=0\n",
        step,(unsigned long long)source_retirement.binding.generation);
    return mac_handoff_reject();
}
__attribute__((noinline)) static int mac_window_open(void)
{
    struct mac_transaction t={.table=NANO_SDM_INPUT_ARENA,.cmd=0x94008000,.evt=0x94009000,.cursor=mac_handoff.cursor};
    t.start=t.last=FES_PROBE_COUNTER()/400U;
    uint32_t seq;
    if(mac_window.active || mac_window.generation>=mac_handoff.generation ||
       !mac_handoff_view(&source_retirement) || !mac_window_tables(&t) || !mac_window_frame(&t,&seq))
        return mac_window_failed(1);
    mac_window.generation=mac_handoff.generation;mac_window.completed=0;mac_window.displayed=0;
    if(!mac_window_base(&t,0x80,&t.table,NANO_MAC_TABLE_BASE) ||
       !mac_window_base(&t,0x90,&t.cmd,MAC_CMD) || !mac_window_base(&t,0xa0,&t.evt,MAC_EVT) ||
       !mac_window_enable(&t,8) || !mac_window_invalidate(&t) || !mac_window_enable(&t,12) ||
       !mac_window_enable(&t,13) || !mac_window_tables(&t))return mac_window_failed(2);
    mac_window.completed=t.commands;mac_window.active=1;
    NOTICE("FES_NANO_MAC_COEXIST_OPEN generation=%llu baseline=%u cursor=%u commands=%u cr0=13 secure_cr0=0 mac_reset=1 grant=0\n",
        (unsigned long long)mac_window.generation,mac_handoff.cursor,t.cursor,t.commands);
    return 0;
}
/* Observed only on the existing zero-argument MAC QUERY, after local frame
 * presentation. No acquisition or device release follows from this receipt. */
int fes_nano_mac_window_progress(void)
{
    if(mac_handoff.failed)return -1;
    if(!mac_window.active || identity_probe.stage!=FES_ID_DDR_READ)return 0;
    struct mac_transaction t={.table=NANO_MAC_TABLE_BASE,.cmd=MAC_CMD,.evt=MAC_EVT,.control=13,
        .cursor=(mac_handoff.cursor+6U)&15U,.commands=6,.phase=WINDOW_RUNNING};
    t.start=t.last=FES_PROBE_COUNTER()/400U;
    if(mac_window.completed!=6 || mac_window.generation!=mac_handoff.generation || !mac_window_stable(&t))return mac_window_failed(5);
    /* STATUS atomically snapshots hardware counters. Pending work is ordinary
     * before the first completely displayed local frame. An observed fault or
     * foreign generation never becomes a carry-over proof. */
    uint32_t flags=FES_PROBE_READ32(FES_NANO_CONTROL_BASE+0x94);
    FES_PROBE_BARRIER();
    if(!mac_window_stable(&t) || (flags&~63U) || (flags&0x30))return mac_window_failed(5);
    if(!(flags&2))return 0;
    if(flags&(1U|8U))return mac_window_failed(5);
    uint32_t generation=FES_PROBE_READ32(FES_NANO_CONTROL_BASE+0x98);
    uint32_t high=FES_PROBE_READ32(FES_NANO_CONTROL_BASE+0x9c);
    uint32_t accepted=FES_PROBE_READ32(FES_NANO_CONTROL_BASE+0xa0);
    uint32_t seen=FES_PROBE_READ32(FES_NANO_CONTROL_BASE+0xa4);
    if(!mac_window_stable(&t) || generation!=1 || high || seen>accepted || seen<mac_window.displayed)
        return mac_window_failed(5);
    if(!seen || seen!=accepted)return 0;
    if(FES_PROBE_READ32(FES_NANO_CONTROL_BASE+0xa8) ||
       FES_PROBE_READ32(FES_NANO_CONTROL_BASE+0xac)!=((seen-1U)&1U))return mac_window_failed(5);
    if(!mac_window_tables(&t))return mac_window_failed(5);
    if(!mac_window.displayed)NOTICE("FES_NANO_MAC_COEXIST_FRAME generation=%llu frame=%u cursor=%u cr0=13 secure_cr0=0 mac_reset=1 grant=0\n",
        (unsigned long long)mac_window.generation,seen,t.cursor);
    mac_window.displayed=seen;
    return 1;
}
__attribute__((noinline)) static int mac_window_close(void)
{
    struct mac_transaction t={.table=NANO_MAC_TABLE_BASE,.cmd=MAC_CMD,.evt=MAC_EVT,
        .cursor=(mac_handoff.cursor+6U)&15U,.control=13,.commands=6,.phase=WINDOW_CLOSE};
    t.start=t.last=FES_PROBE_COUNTER()/400U;
    uint32_t seq;
#if defined(FES_NANO_MAC_MENU_SUPPLIER) && FES_NANO_MAC_MENU_SUPPLIER
    if(!mac_window.active && mac_window.completed==16 && menu_mac.phase==MENU_NO_MAC &&
       mac_window.generation==mac_handoff.generation && mac_window.displayed){
        t=(struct mac_transaction){.table=NANO_SDM_INPUT_ARENA,.cmd=0x94008000,.evt=0x94009000,
            .cursor=mac_handoff.cursor,.phase=WINDOW_CLOSE,.start=t.start,.last=t.last};
        if(!mac_handoff_view(&source_retirement) || !mac_window_tables(&t) || !mac_window_frame(&t,&seq) ||
           seq<mac_window.displayed)return mac_window_failed(4);
        NOTICE("FES_NANO_MAC_MENU_CLOSE generation=%llu frame=%u restored=1 grant=0\n",
            (unsigned long long)mac_window.generation,seq);
        return 0;
    }
#endif
    if(!mac_window.active || mac_window.completed!=6 || mac_window.generation!=mac_handoff.generation ||
       !mac_window_tables(&t) || !mac_window_frame(&t,&seq) || seq<mac_window.displayed)return mac_window_failed(3);
    if(!mac_window_enable(&t,12) || !mac_window_enable(&t,8) ||
       !mac_window_pair(&t,UINT64_C(0x100000003),1) || !mac_window_pair(&t,UINT64_C(0x200000028),0) ||
       !mac_window_enable(&t,0) || !mac_window_base(&t,0x80,&t.table,NANO_SDM_INPUT_ARENA) ||
       !mac_window_enable(&t,8) || !mac_window_invalidate(&t) || !mac_window_enable(&t,0) ||
       t.commands!=16 || t.cursor!=mac_handoff.cursor || !mac_window_base(&t,0x90,&t.cmd,0x94008000) ||
       !mac_window_base(&t,0xa0,&t.evt,0x94009000) || !mac_window_tables(&t) || !mac_handoff_view(&source_retirement))
        return mac_window_failed(4);
    mac_window.active=0;mac_window.completed=t.commands;
    NOTICE("FES_NANO_MAC_COEXIST_CLOSE generation=%llu frame=%u cursor=%u commands=%u restored=1 mac_reset=1 grant=0\n",
        (unsigned long long)mac_window.generation,seq,t.cursor,t.commands);
    return 0;
}
