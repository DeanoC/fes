/* SPDX-License-Identifier: BSD-3-Clause */
/* Called under the owned SiP lock, after actual SDM retirement and immediately
 * before its existing warm rearm. No state here enables a device or writes a
 * stream entry, page table, queue or register. */
static struct {
    /* The retired and setup slot retain separate complete immutable source
     * bindings. Pin their secure address and owner across the slot switch. */
    const struct nano_retire *slot;
    uint64_t owner, epoch, generation;
    uint32_t cursor;
    uint16_t leaf_access;
    uint8_t frozen, failed;
} mac_handoff;
_Static_assert(sizeof(mac_handoff)<=40, "retained MAC handoff OCRAM budget");

static int mac_handoff_reject(void)
{
    mac_handoff.failed = 1;
    return -1;
}

static int mac_handoff_reg(uint32_t address, uint32_t mask, uint32_t expected)
{
    return (FES_PROBE_READ32(address) & mask) == expected;
}

static int mac_handoff_view(const struct nano_retire *r)
{
    static const struct { uint32_t offset, value; } registers[] = {
        {0,0x080f7e3f}, {4,0x0e739d18}, {0x14,0x00400075}, {0x20,0}, {0x24,0}, {0x28,0x820}, {0x2c,4}, {0x44,0x100000}, {0x48,0},
        {0x50,0}, {0x54,0}, {0x60,0}, {0x64,0}, {0x80,0x94000000}, {0x84,0},
        {0x88,8}, {0x90,0x94008003}, {0x94,0}, {0xa0,0x94009003}, {0xa4,0},
        {0x100a8,0}, {0x100ac,0}
    };
    static const unsigned streams[] = {1,10};
    uint64_t expected[8];
    if (r != &source_retirement || !r->attempted || !r->continuation_attempted ||
        !r->retired || !r->continued || r->failed || r->first_error ||
        r->phase != RET_COMPLETE || r->queue_target > 15 ||
        r->command_receipt.owner != r->binding.owner || r->command_receipt.epoch != r->binding.epoch ||
        r->command_receipt.generation != r->binding.generation ||
        r->command_receipt.baseline != r->queue_baseline || r->command_receipt.commands_completed != 1 ||
        r->command_receipt.interconnect_drained || r->command_receipt.DMA_access_granted ||
        r->command_receipt.consumer != r->queue_target ||
        r->command_receipt.producer != r->queue_target ||
        !nano_sdm_input_same(&r->binding,&sdm_arm_probe.input))
        return 0;
    for (unsigned pass=0; pass<2; pass++) {
        FES_PROBE_BARRIER();
        if (!FES_PROBE_CPU() || FES_PROBE_FREQUENCY()!=400000000U || !sdm_source_secure_scope() ||
            !mac_handoff_reg(0x10d11024,0x101,0x101) ||
            !mac_handoff_reg(0x10d12184,UINT32_MAX,3) ||
            !mac_handoff_reg(0x10d121b0,UINT32_MAX,0x000a000a) ||
            !mac_handoff_reg(0x10d1219c,UINT32_MAX,3) ||
            !mac_handoff_reg(0x10d121c8,UINT32_MAX,0x00010001))
            return 0;
        for (unsigned i=0;i<sizeof(registers)/sizeof(registers[0]);i++)
            if (!mac_handoff_reg(0x16000000+registers[i].offset,UINT32_MAX,registers[i].value))
                return 0;
        if (!mac_handoff_reg(0x16000098,UINT32_MAX,r->queue_target) ||
            !mac_handoff_reg(0x1600009c,UINT32_MAX,r->queue_target))
            return 0;
        /* Only the known secure reservation is dereferenced. The actual table
         * base and linear layout have both been read back above. */
        for (unsigned i=0;i<2;i++) {
            unsigned offset=streams[i]*64;
            if (nano_sdm_input_words(&r->binding,offset,expected,8))
                return 0;
            for (unsigned j=0;j<8;j++)
                if (FES_PROBE_LOAD64(NANO_SDM_INPUT_ARENA+offset+8*j) != expected[j])
                    return 0;
        }
        FES_PROBE_BARRIER();
        if (!FES_PROBE_CPU() || FES_PROBE_FREQUENCY()!=400000000U)
            return 0;
    }
    return 1;
}

/* All five page addresses, memory attributes and execute permissions are
 * fixed. The ten S2AP bits uniquely encode the five unpublished leaves.
 * Keep only this reversible representation in the limited secure OCRAM. */
static uint16_t mac_handoff_leaf_access(void)
{
    uint16_t access=0;
    for(unsigned n=0;n<5;n++){
        uint64_t address=NANO_MAC_POOL_BASE+4096U*n;
        uint64_t leaf=nano_mac_stage2_leaf(address);
        if((leaf & ~UINT64_C(0xc0)) != (address | UINT64_C(0x0060000000000617)))
            return 0;
        access|=((leaf>>6)&3U)<<(2*n);
    }
    return access;
}

__attribute__((noinline)) static int mac_handoff_freeze(const struct nano_retire *r)
{
    if (mac_handoff.failed || mac_handoff.frozen || !mac_handoff_view(r) ||
        (mac_handoff.slot &&
         (mac_handoff.slot == r || mac_handoff.generation != r->binding.generation ||
          r->binding.owner != mac_handoff.owner || r->binding.epoch != mac_handoff.epoch)))
        return mac_handoff_reject();
    mac_handoff.slot=r;
    mac_handoff.owner=r->binding.owner;
    mac_handoff.epoch=r->binding.epoch;
    mac_handoff.generation=r->binding.generation;
    mac_handoff.cursor=r->queue_target;
    mac_handoff.leaf_access=mac_handoff_leaf_access();
    if(mac_handoff.leaf_access != (3U | (1U<<2) | (1U<<4) | (2U<<6) | (2U<<8)))
        return mac_handoff_reject();
    mac_handoff.frozen=1;
    return 0;
}

__attribute__((noinline)) static int mac_handoff_before_rearm(const struct nano_retire *r,uint64_t next)
{
    if (mac_handoff.failed || !mac_handoff.frozen ||
        !r || mac_handoff.slot != r || r->binding.generation != mac_handoff.generation ||
        r->binding.owner != mac_handoff.owner || r->binding.epoch != mac_handoff.epoch ||
        r->queue_target != mac_handoff.cursor || next <= r->binding.generation ||
        !mac_handoff_view(r))
        return mac_handoff_reject();
    if (mac_handoff.leaf_access!=mac_handoff_leaf_access())
        return mac_handoff_reject();
    mac_handoff.frozen=0;
    mac_handoff.generation=next;
    return 0;
}
