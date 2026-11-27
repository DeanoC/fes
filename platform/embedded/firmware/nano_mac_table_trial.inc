/* SPDX-License-Identifier: BSD-3-Clause */
/* A short EL3 transaction while all masters are contained and both SMMU
 * interfaces are disabled. No device, queue, translation or reset is enabled.
 * The original SDM arena and its retained command cursor remain untouched. */
#define NANO_MAC_TABLE_BASE UINT64_C(0x94010000)
#define NANO_MAC_TABLE_BYTES 0x7000U
#define NANO_MAC_TABLE_BATCH 256U
extern unsigned char __FES_MAC_TABLE_START__[],__FES_MAC_TABLE_END__[];
static uint64_t mac_table_word(uint32_t offset)
{
    const uint32_t l1=0x4000, l2=0x5000, l3=0x6000;
    if (offset<l1) {
        if (offset/64U!=1) return offset%64U==0 ? 1 : 0;
        return nano_stage2_ste_word((offset%64U)/8U,2U,NANO_MAC_TABLE_BASE+l1);
    }
    if (offset==l1+((NANO_MAC_POOL_BASE>>30)&511U)*8U)
        return (NANO_MAC_TABLE_BASE+l2)|3U;
    if (offset==l2+((NANO_MAC_POOL_BASE>>21)&511U)*8U)
        return (NANO_MAC_TABLE_BASE+l3)|3U;
    if (offset>=l3 && offset<NANO_MAC_TABLE_BYTES)
        return nano_mac_stage2_leaf(NANO_MAC_POOL_BASE+((offset-l3)/8U)*4096U);
    return 0;
}

struct mac_table_clock {uint64_t first,last;};

static int mac_table_scope(struct mac_table_clock *clock,uint32_t base)
{
    const struct nano_retire *r=&source_retirement;
    if (!mac_handoff.frozen || mac_handoff.failed || mac_handoff.slot!=r ||
        mac_handoff.generation!=r->binding.generation || mac_handoff.owner!=r->binding.owner ||
        mac_handoff.epoch!=r->binding.epoch || mac_handoff.cursor!=r->queue_target ||
        !FES_PROBE_CPU() || FES_PROBE_FREQUENCY()!=400000000U ||
        FES_PROBE_SYMBOL(__FES_MAC_TABLE_START__)!=NANO_MAC_TABLE_BASE ||
        FES_PROBE_SYMBOL(__FES_MAC_TABLE_END__)!=NANO_MAC_TABLE_BASE+NANO_MAC_TABLE_BYTES ||
        retire_owned(&source_retirement,&r->binding)!=1 || !sdm_source_secure_scope())
        return 0;
    /* The new linker reservation and the existing 64 KiB SDM reservation
     * share one already authenticated 2 MiB noncacheable EL3 block mapping. */
    FES_PROBE_BARRIER();
    if (!mac_handoff_reg(0x10d11024,0x101,0x101) ||
        !mac_handoff_reg(0x10d1219c,UINT32_MAX,3) || !mac_handoff_reg(0x10d121c8,UINT32_MAX,0x10001) ||
        !mac_handoff_reg(0x16000020,UINT32_MAX,0) || !mac_handoff_reg(0x16000024,UINT32_MAX,0) ||
        !mac_handoff_reg(0x16000044,UINT32_MAX,0x100000) || !mac_handoff_reg(0x16000048,UINT32_MAX,0) ||
        !mac_handoff_reg(0x16000080,UINT32_MAX,base) || !mac_handoff_reg(0x16000084,UINT32_MAX,0) ||
        !mac_handoff_reg(0x16000088,UINT32_MAX,8) ||
        !mac_handoff_reg(0x16000098,UINT32_MAX,r->queue_target) ||
        !mac_handoff_reg(0x1600009c,UINT32_MAX,r->queue_target) ||
        !mac_handoff_reg(0x16000060,UINT32_MAX,0) || !mac_handoff_reg(0x16000064,UINT32_MAX,0) ||
        !mac_handoff_reg(0x160100a8,UINT32_MAX,0) || !mac_handoff_reg(0x160100ac,UINT32_MAX,0))
        return 0;
    FES_PROBE_BARRIER();
    uint64_t now=FES_PROBE_COUNTER()/400U;
    if(now<clock->last || now-clock->first>=NANO_RETIRE_US)return 0;
    clock->last=now;
    return 1;
}

static int mac_table_walk(struct mac_table_clock *clock,uint32_t base,unsigned write)
{
    for(uint32_t first=0;first<NANO_MAC_TABLE_BYTES;first+=NANO_MAC_TABLE_BATCH){
        if(!mac_table_scope(clock,base))return 0;
        for(uint32_t n=first;n<first+NANO_MAC_TABLE_BATCH;n+=8){
            uint64_t address=NANO_MAC_TABLE_BASE+n,value=mac_table_word(n);
            if(write)FES_PROBE_STORE64(address,value);
            else if(FES_PROBE_LOAD64(address)!=value)return 0;
        }
        FES_PROBE_BARRIER();
    }
    return mac_table_scope(clock,base);
}

/* The full canonical SDM tree is independently rederived from the actual
 * retained payload digest, size and stream binding before and after the switch.
 * The queue pages use live cursor/status checks and are never written here. */
static int mac_table_original(struct mac_table_clock *clock)
{
    uint64_t words[NANO_MAC_TABLE_BATCH/8];
    for(uint32_t offset=0;offset<NANO_SDM_INPUT_ARENA_BYTES;offset+=NANO_MAC_TABLE_BATCH){
        if(!nano_sdm_input_table_range(offset,NANO_MAC_TABLE_BATCH))continue;
        if(!mac_table_scope(clock,NANO_SDM_INPUT_ARENA) ||
           nano_sdm_input_words(&source_retirement.binding,offset,words,NANO_MAC_TABLE_BATCH/8))return 0;
        for(unsigned n=0;n<NANO_MAC_TABLE_BATCH/8;n++)
            if(FES_PROBE_LOAD64(NANO_SDM_INPUT_ARENA+offset+8*n)!=words[n])return 0;
    }
    return mac_table_scope(clock,NANO_SDM_INPUT_ARENA);
}

static int mac_table_stop(unsigned stage)
{
    NOTICE("FES_NANO_MAC_TABLE_STOP generation=%llu stage=%u restoration_verified=0\n",
           (unsigned long long)source_retirement.binding.generation,stage);
    return mac_handoff_reject();
}

__attribute__((noinline)) static int mac_table_switch_trial(void)
{
    uint64_t start=FES_PROBE_COUNTER()/400U;
    struct mac_table_clock clock={start,start};
    if(!mac_handoff_view(&source_retirement) || !mac_table_original(&clock) ||
       !mac_table_walk(&clock,NANO_SDM_INPUT_ARENA,1) ||
       !mac_table_walk(&clock,NANO_SDM_INPUT_ARENA,0))
        return mac_table_stop(1);
    /* Same 32-bit base-low write as the original SDM publisher. The high half
     * is zero for both pointers. CPU and masters stay held across the writes. */
    FES_PROBE_WRITE32(0x16000080,NANO_MAC_TABLE_BASE);
    FES_PROBE_BARRIER();
    if(!mac_table_walk(&clock,NANO_MAC_TABLE_BASE,0))
        return mac_table_stop(2);
    FES_PROBE_WRITE32(0x16000080,NANO_SDM_INPUT_ARENA);
    FES_PROBE_BARRIER();
    if(!mac_table_original(&clock) || !mac_handoff_view(&source_retirement))
        return mac_table_stop(3);
    NOTICE("FES_NANO_MAC_TABLE generation=%llu cursor=%u mac_table=%x sid=1 vmid=2 pages=5 sdm_sid10=abort restored=1 translation=0 dma=0\n",
        (unsigned long long)source_retirement.binding.generation,mac_handoff.cursor,(unsigned)NANO_MAC_TABLE_BASE);
    return 0;
}
