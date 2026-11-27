/* SPDX-License-Identifier: BSD-3-Clause */
/* Sealed C64/MENU PIO firmware, under the existing owned SiP dispatcher lock.
 * Only the running authenticated MENU exposes the API. No NS addresses,
 * translation parameters, completion flags, PHY IDs or source role are taken
 * from a nonsecure call. */
static int menu_scope(void *,const struct fes_nano_mac_binding *,unsigned,unsigned);
static int menu_acquire(void *,const struct fes_nano_mac_binding *);
static int menu_confirm(void *,const struct fes_nano_mac_binding *);
static int menu_stop(void *,const struct fes_nano_mac_binding *);

static int mac_controller_expected(void)
{
    const struct fes_nano_mac *m=&fes_nano_owned_mac;
    return !m->failed && (!m->attempted ? (!m->initialized && !menu_mac.generation) :
        m->ops.context==&menu_mac && m->ops.scope==menu_scope && m->ops.acquire==menu_acquire &&
        m->ops.confirm==menu_confirm && m->ops.stop==menu_stop && m->binding.mac==0 &&
        m->binding.phy==1 && m->binding.phy_id==0x00221622U &&
        m->binding.dma_start==NANO_MAC_POOL_BASE && m->binding.dma_size==(2U<<20) &&
        m->binding.owner==provider.record.boot.owner && m->binding.epoch==provider.record.boot.epoch);
}
/* Called inside the cold/USB master sampler: never call that sampler here.
 * The physical pair can change during the exclusive PHY prepare/retire call.
 * No sibling SMC can enter those transitions. Outside them a partial or
 * unexpectedly released reset is never admitted. */
int fes_nano_menu_mac_reset_mask(const struct fes_nano_cold *s,uint32_t *released)
{
    if(s!=&provider || !released)return 0;
    *released=0;
    if(menu_mac.phase<MENU_PHY || menu_mac.phase>MENU_STOPPING)return 1;
    if(!mac_controller_expected() || !fes_nano_owned_mac.active || !fes_nano_owned_mac.reservation ||
       !mac_handoff.frozen || mac_handoff.failed || !mac_window.active ||
       mac_window.generation!=menu_mac.menu_generation || !mac_window.displayed ||
       !menu_mac.generation || !FES_PROBE_CPU() || FES_PROBE_FREQUENCY()!=400000000U ||
       !s->valid || s->failed || !s->control_expected || !s->ddr_expected ||
       configuration_program.role!=FES_NANO_PROGRAM_ROLE_MENU || configuration.generation!=mac_window.generation ||
       mac_reg(0x20)!=13 || mac_reg(0x24)!=13 || mac_reg(0x80)!=NANO_MAC_TABLE_BASE ||
       mac_reg(0x84) || mac_reg(0x88)!=8 || mac_reg(0x60) || mac_reg(0x64) ||
       FES_PROBE_LOAD64(NANO_MAC_TABLE_BASE+64)!=13 ||
       !mac_handoff_reg(0x10d1219c,UINT32_MAX,3) || !mac_handoff_reg(0x10d121c8,UINT32_MAX,0x10001))return 0;
    uint32_t reset=FES_PROBE_READ32(0x10d11024)&0x101U;
    if(reset && reset!=0x101)return 0;
    if(!reset && !menu_mac.phy.released)return 0;
    if(reset && (menu_mac.phase==MENU_READY || menu_mac.phase==MENU_CONFIRMED))return 0;
    *released=reset ? 0U:0x101U;
    return 1;
}
int fes_nano_menu_mac_source_safe(void)
{
    return mac_controller_expected() && menu_mac.phase==MENU_NO_MAC && !mac_window.active &&
        !fes_nano_owned_mac.active && !fes_nano_owned_mac.reservation && !fes_nano_owned_mac.confirmed &&
        !menu_mac.phy.begun && !menu_mac.phy.released && !menu_mac.vmid && !menu_mac.withdrawn &&
        (!menu_mac.generation || menu_mac.channel.phase==FES_CHANNEL_RETAINED) &&
        mac_handoff_reg(0x10d11024,0x101,0x101);
}
static struct mac_transaction menu_view(unsigned live)
{
    uint64_t now=FES_PROBE_COUNTER()/400U;
    return (struct mac_transaction){.table=live ? NANO_MAC_TABLE_BASE:NANO_SDM_INPUT_ARENA,
        .cmd=live ? MAC_CMD:0x94008000,.evt=live ? MAC_EVT:0x94009000,.control=live ? 13:0,
        .cursor=(mac_handoff.cursor+(live ? 6U:0))&15U,.commands=live ? 6:0,.phase=WINDOW_RUNNING,
        .start=now,.last=now};
}
static int menu_guard(void *p,uint64_t generation,unsigned revoked)
{
    if(p!=&menu_mac || !generation || generation!=menu_mac.generation || !mac_window.displayed ||
       menu_mac.menu_generation!=mac_window.generation || revoked>1)return 0;
    struct mac_transaction t=menu_view(!revoked);
    if(!mac_window_stable(&t))return 0;
    if(revoked)return menu_mac.phase==MENU_REVOKED && !mac_window.active && mac_window.completed==16 &&
        mac_handoff_view(&source_retirement);
    if(!mac_window.active || mac_window.completed!=6 || !menu_mac.vmid || menu_mac.withdrawn ||
       FES_PROBE_TABLE(fes_nano_mmu_ddr,(NANO_MAC_POOL_BASE-0x80000000U)>>21)!=
          (NANO_MAC_POOL_BASE|UINT64_C(0x0040000000000669)))return 0;
    /* Recheck all eight STE words, the three-level walk and the adjacent
     * unmapped leaf, starting only from the authenticated table base. */
    for(unsigned j=0;j<8;j++)if(FES_PROBE_LOAD64(NANO_MAC_TABLE_BASE+64+8*j)!=mac_window_word(64+8*j) ||
        FES_PROBE_LOAD64(NANO_MAC_TABLE_BASE+640+8*j)!=mac_window_word(640+8*j))return 0;
    unsigned parents[]={0x4000+((NANO_MAC_POOL_BASE>>30)&511U)*8,0x5000+((NANO_MAC_POOL_BASE>>21)&511U)*8};
    for(unsigned i=0;i<2;i++)if(FES_PROBE_LOAD64(NANO_MAC_TABLE_BASE+parents[i])!=mac_window_word(parents[i]))return 0;
    for(unsigned i=0;i<6;i++)if(FES_PROBE_LOAD64(NANO_MAC_TABLE_BASE+0x6000+i*8)!=mac_window_word(0x6000+i*8))return 0;
    return mac_window_stable(&t);
}
static int menu_phy_owned(void *p){return menu_guard(p,menu_mac.generation,0);}
static const fes_channel_guards channel_guards={&menu_mac,menu_guard};
static int menu_phy_drained(void *p)
{
    if(p!=&menu_mac || menu_mac.phase!=MENU_STOPPING)return 0;
    if((FES_PROBE_READ32(0x10d11024)&0x101)==0x101){
        /* Channel cannot be sampled through asserted reset. Its preceding
         * acknowledged drain is pinned, and the positively revoked arena is
         * independently verified below. */
        return menu_mac.channel.phase==FES_CHANNEL_DRAINED && menu_mac.channel.generation==menu_mac.generation;
    }
    return !fes_nano_mac_channel_drained(&menu_mac.channel,&channel_guards,menu_mac.generation);
}
static const struct fes_nano_mac_phy_guards phy_guards={&menu_mac,menu_phy_owned,menu_phy_drained};

/* Real consumed invalidations. Do not write any original SDM storage or
 * advance its software cursor. Six + four + six private commands advance
 * the eight-slot hardware ring through one complete wrap-bit cycle. */
static int menu_restore_domain(void)
{
    struct mac_transaction t=menu_view(1);
    if(!mac_window.active || mac_window.completed!=6 || !mac_window_tables(&t) ||
       !mac_handoff_reg(0x10d11024,0x101,0x101))return 0;
    uint16_t vmid=menu_mac.vmid ? menu_mac.vmid:2;
    menu_mac.vmid=vmid;
    FES_PROBE_STORE64(NANO_MAC_TABLE_BASE+64,0);FES_PROBE_BARRIER();menu_mac.withdrawn=1;
    if(FES_PROBE_LOAD64(NANO_MAC_TABLE_BASE+64)!=0 || !mac_window_tables(&t) ||
       !mac_window_pair(&t,UINT64_C(0x100000003),1) || !mac_window_pair(&t,((uint64_t)vmid<<32)|0x28U,0) ||
       !mac_window_enable(&t,12) || !mac_window_enable(&t,8) || !mac_window_enable(&t,0) ||
       !mac_window_base(&t,0x80,&t.table,NANO_SDM_INPUT_ARENA) || !mac_window_enable(&t,8) ||
       !mac_window_invalidate(&t) || !mac_window_enable(&t,0) || t.commands!=16 || t.cursor!=mac_handoff.cursor ||
       !mac_window_base(&t,0x90,&t.cmd,0x94008000) || !mac_window_base(&t,0xa0,&t.evt,0x94009000) ||
       !mac_window_tables(&t) || !mac_handoff_view(&source_retirement))return 0;
    /* The private table is now disconnected and all old walks invalidated.
     * Restore its original five-page template for the next authenticated role. */
    menu_mac.vmid=0;menu_mac.withdrawn=0;
    for(unsigned i=0;i<8;i++){
        if(!mac_window_stable(&t))return 0;
        FES_PROBE_STORE64(NANO_MAC_TABLE_BASE+64+8*i,mac_table_word(64+8*i));
    }
    FES_PROBE_BARRIER();
    if(!mac_window_tables(&t))return 0;
    mac_window.completed=16;mac_window.active=0;
    return 1;
}
static int menu_open_domain(void)
{
    struct mac_transaction t=menu_view(0);
    if(mac_window.active || mac_window.completed!=16 || !mac_window.displayed ||
       !mac_window_tables(&t) || !mac_handoff_view(&source_retirement) || !menu_mac.generation ||
       menu_mac.generation>UINT16_MAX-2U)return 0;
    menu_mac.vmid=(uint16_t)(2U+menu_mac.generation);
    for(unsigned i=0;i<8;i++){
        if(!mac_window_stable(&t))return 0;
        FES_PROBE_STORE64(NANO_MAC_TABLE_BASE+64+8*i,mac_window_word(64+8*i));
    }
    FES_PROBE_BARRIER();
    if(!mac_window_tables(&t) || !mac_window_base(&t,0x80,&t.table,NANO_MAC_TABLE_BASE) ||
       !mac_window_base(&t,0x90,&t.cmd,MAC_CMD) || !mac_window_base(&t,0xa0,&t.evt,MAC_EVT) ||
       !mac_window_enable(&t,8) || !mac_window_invalidate(&t) ||
       !mac_window_enable(&t,12) || !mac_window_enable(&t,13) || !mac_window_tables(&t))return 0;
    mac_window.active=1;mac_window.completed=6;
    return 1;
}
static int menu_binding(void *p,const struct fes_nano_mac_binding *b)
{
    return p==&menu_mac && b==&fes_nano_owned_mac.binding && mac_controller_expected() &&
        FES_PROBE_CPU() && FES_PROBE_FREQUENCY()==400000000 && !mac_handoff.failed && !sdm_source_failed;
}
static int menu_scope(void *p,const struct fes_nano_mac_binding *b,unsigned active,unsigned confirmed)
{
    if(!menu_binding(p,b))return -1;
    if(!active)return !confirmed && menu_mac.phase==MENU_NO_MAC && !menu_mac.phy.begun &&
        (!menu_mac.generation || menu_mac.channel.phase==FES_CHANNEL_RETAINED) &&
        mac_handoff_reg(0x10d11024,0x101,0x101) ? 0:-1;
    if((!confirmed && menu_mac.phase!=MENU_READY) || (confirmed && menu_mac.phase!=MENU_CONFIRMED) ||
       !menu_guard(p,menu_mac.generation,0) || fes_nano_mac_phy_active(&menu_mac.phy,&phy_guards))return -1;
    return confirmed ? fes_nano_mac_channel_scope(&menu_mac.channel,&channel_guards,menu_mac.generation):0;
}
static int menu_acquire(void *p,const struct fes_nano_mac_binding *b)
{
    if(!menu_binding(p,b) || menu_mac.phase!=MENU_NO_MAC || !mac_window.displayed ||
       mac_window.generation!=mac_handoff.generation || !mac_window_owner(WINDOW_RUNNING) ||
       menu_mac.generation>=UINT16_MAX-2U)return -1;
    if(mac_window.active && (fes_nano_mac_window_progress()!=1 || !menu_restore_domain()))return -1;
    /* Every resume needs its current frame independently of a previous MENU. */
    struct mac_transaction t=menu_view(0);uint32_t seq;
    if(!mac_window_frame(&t,&seq) || seq<mac_window.displayed)return -1;
    menu_mac.generation++;menu_mac.menu_generation=mac_window.generation;menu_mac.phase=MENU_MAPPING;
    if(!menu_open_domain())return -1;
    menu_mac.phase=MENU_PHY;
    if(fes_nano_mac_phy_prepare(&menu_mac.phy,&phy_guards) ||
       fes_nano_mac_channel_prepare(&menu_mac.channel,&channel_guards,menu_mac.generation))return -1;
    menu_mac.phase=MENU_READY;
    NOTICE("FES_NANO_MENU_MAC_ACQUIRE menu=%llu grant=%llu vmid=%u cursor=%u frame=%u\n",
        (unsigned long long)menu_mac.menu_generation,(unsigned long long)menu_mac.generation,
        menu_mac.vmid,(mac_handoff.cursor+6U)&15U,seq);
    return 0;
}
static int menu_confirm(void *p,const struct fes_nano_mac_binding *b)
{
    if(!menu_binding(p,b) || menu_mac.phase!=MENU_READY ||
       fes_nano_mac_channel_confirm(&menu_mac.channel,&channel_guards,menu_mac.generation))return -1;
    menu_mac.phase=MENU_CONFIRMED;
    NOTICE("FES_NANO_MENU_MAC_CONFIRM menu=%llu grant=%llu vmid=%u\n",
        (unsigned long long)menu_mac.menu_generation,(unsigned long long)menu_mac.generation,menu_mac.vmid);
    return 0;
}
static int menu_stop(void *p,const struct fes_nano_mac_binding *b)
{
    if(!menu_binding(p,b) || menu_mac.phase!=MENU_CONFIRMED)return -1;
    menu_mac.phase=MENU_STOPPING;
    if(fes_nano_mac_channel_drain(&menu_mac.channel,&channel_guards,menu_mac.generation) ||
       fes_nano_mac_phy_retire(&menu_mac.phy,&phy_guards))return -1;
    menu_mac.phase=MENU_REVOKED;
    if(!menu_restore_domain() || fes_nano_mac_channel_retained(&menu_mac.channel,&channel_guards,menu_mac.generation))
        return -1;
    menu_mac.phase=MENU_NO_MAC;
    NOTICE("FES_NANO_MENU_MAC_STOP menu=%llu grant=%llu status=%x dwell_us=%llu cursor=%u restored=1\n",
        (unsigned long long)menu_mac.menu_generation,(unsigned long long)menu_mac.generation,
        menu_mac.channel.status,(unsigned long long)menu_mac.channel.dwell_us,mac_handoff.cursor);
    return 0;
}
int fes_nano_menu_mac_admit(void)
{
    if(!mac_controller_expected() || mac_handoff.failed || menu_mac.phase==MENU_REVOKED)return -1;
    if(!mac_handoff.frozen || configuration_program.role!=FES_NANO_PROGRAM_ROLE_MENU ||
       identity_probe.stage!=FES_ID_DDR_READ || !mac_window.generation ||
       mac_window.generation!=mac_handoff.generation)return 0;
    if(mac_window.active && menu_mac.phase==MENU_NO_MAC){
        int progress=fes_nano_mac_window_progress();
        if(progress!=1)return progress;
    }
    if(!mac_window.displayed)return 0;
    if(fes_nano_owned_mac.initialized)return 1;
    if(fes_nano_owned_mac.attempted || menu_mac.phase!=MENU_NO_MAC || !mac_window.active ||
       !mac_handoff_reg(0x10d11024,0x101,0x101) || !mac_window_owner(WINDOW_RUNNING))return -1;
    struct fes_nano_mac_binding b={.owner=mac_handoff.owner,.epoch=mac_handoff.epoch,.mac=0,.phy=1,
        .phy_id=0x00221622,.dma_start=NANO_MAC_POOL_BASE,.dma_size=2U<<20};
    struct fes_nano_mac_ops ops={&menu_mac,menu_scope,menu_acquire,menu_confirm,menu_stop};
    /* The matched supplier provides all five independent operations. Actual
     * PHY, channel confirmation and terminal drain are checked when performed.
     * The observed FMS1/translation proof is required before the first offer. */
    if(fes_nano_mac_init(&fes_nano_owned_mac,b,ops,FES_NANO_MAC_BOOT_SCOPE|
       FES_NANO_MAC_DMA_WINDOW|FES_NANO_MAC_CLOCK_PHY|FES_NANO_MAC_INDEPENDENT_DRAIN|FES_NANO_MAC_COEXISTENCE))return -1;
    return 1;
}
