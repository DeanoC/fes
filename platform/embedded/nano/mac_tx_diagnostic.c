/* SPDX-License-Identifier: BSD-3-Clause */
#include "mac_tx_diagnostic.h"
#ifdef FES_CBL_BIDIRECTIONAL
#include "mac_rx_diagnostic.h"
#endif

#define MAC 0x10810000U
#define SMMU 0x16000000U
#define RST 0x10d11024U
#define PHYSEL 0x10d12044U
#define STREAM 0x10d1219cU
#define SID 0x10d121c8U
#define MAC_RESETS 0x101U
/* MTL_TxQ0_Debug: pending completion, queue nonempty, write-active, read state. */
#define MTL_TX_ACTIVITY 0x3eU
#define S2_ROOT (FES_TX_TABLES + 0x4000U)
#define S2_MID (FES_TX_TABLES + 0x5000U)
#define S2_LEAF (FES_TX_TABLES + 0x6000U)
#define PAGE 4096U
#define NS_ABORT (1U << 20)
#define MAX_US 40000000U
#define PHY_WAIT_US 25000000U
#define PHY_SAMPLE_US 100000U
#define TX_COMPLETION_US 2000000U
#define CLEANUP_RESERVE_US 8000000U
#define OWN (1U << 31)
#define FIRST_LAST (3U << 28)
#define CONTEXT (1U << 30)
#define DESCRIPTOR_ERROR (1U << 27)
#define FAULTS ((1U << 9) | (1U << 12) | (1U << 13))
#define SID1_STE (FES_TX_TABLES + 64U)
#define DESC_FLAGS ((3ULL << 53) | (1U << 10) | (2U << 8) | (5U << 2) | 3U)

struct trial {
    const struct fes_tx_io *io;
    struct fes_tx_result *out;
    unsigned prod, expected_ste, vmid;
#ifdef FES_CBL_BIDIRECTIONAL
    unsigned inherited;
#endif
};
static uint64_t table_word(uint32_t a, unsigned vmid);
static uint32_t read_reg(struct trial *t, uint32_t a);
static int queues_clean(struct trial *t);
static int domain_current(struct trial *t)
{
    const uint32_t walk[] = {SID1_STE + 8,
                             SID1_STE + 16,
                             SID1_STE + 24,
                             S2_ROOT + 8 * ((FES_TX_IOVA >> 30) & 511),
                             S2_MID + 8 * ((FES_TX_IOVA >> 21) & 511),
                             S2_LEAF + 8 * ((FES_TX_IOVA >> 12) & 511),
                             S2_LEAF + 8 * ((FES_TX_BUFFER >> 12) & 511),
#ifdef FES_CBL_BIDIRECTIONAL
                             S2_LEAF + 8 * ((FES_RX_DATA_IOVA >> 12) & 511),
#endif
    };
    if (read_reg(t, STREAM) != 3 || read_reg(t, SID) != 0x10001 || read_reg(t, SMMU + 0x20) != 13 ||
        read_reg(t, SMMU + 0x24) != 13 || read_reg(t, SMMU + 0x28) != 0x820 ||
        read_reg(t, SMMU + 0x44) != NS_ABORT || read_reg(t, SMMU + 0x88) != 8 || queues_clean(t) ||
        t->io->load(t->io->ctx, SMMU + 0x80) != FES_TX_TABLES ||
        t->io->load(t->io->ctx, SMMU + 0x90) != (FES_TX_CMDQ | 3U) ||
        t->io->load(t->io->ctx, SMMU + 0xa0) != (FES_TX_EVTQ | 3U) ||
        t->io->load(t->io->ctx, SID1_STE) != t->expected_ste)
        return 0;
    for (unsigned i = 0; i < sizeof(walk) / sizeof(walk[0]); i++) {
        if (t->io->load(t->io->ctx, walk[i]) != table_word(walk[i], t->vmid))
            return 0;
    }
    return 1;
}
static int guard(struct trial *t)
{
    if (t->out->scope_lost)
        return -1;
    uint64_t now = t->io->time_us(t->io->ctx);
    if (now < t->out->last_us || now - t->out->started_us > MAX_US || t->io->owns(t->io->ctx) != 1)
        goto lost;
    if (!t->out->mac_release_attempted) {
        uint32_t reset = t->io->read(t->io->ctx, RST);
        if ((reset & MAC_RESETS) != MAC_RESETS) {
            t->out->last_address = RST;
            t->out->last_value = reset;
            goto lost;
        }
    }
    unsigned check_domain = t->out->mapped;
#ifdef FES_CBL_BIDIRECTIONAL
    check_domain |= t->inherited;
#endif
    if (check_domain && !domain_current(t))
        goto lost;
    t->out->last_us = now;
    return 0;
lost:
    t->out->scope_lost = 1;
    return -1;
}
static uint32_t read_reg(struct trial *t, uint32_t a)
{
    t->out->last_address = a;
    t->out->last_value = t->io->read(t->io->ctx, a);
    return t->out->last_value;
}
static int write_reg(struct trial *t, uint32_t a, uint32_t v)
{
    if (guard(t))
        return -1;
    t->io->sync_memory(t->io->ctx);
    t->out->write_attempted = 1;
    t->io->write(t->io->ctx, a, v);
    t->io->sync_memory(t->io->ctx);
    return 0;
}
static int set_reg(struct trial *t, uint32_t a, uint32_t v)
{
    return write_reg(t, a, v) || read_reg(t, a) != v;
}
static int poll(struct trial *t, uint32_t a, uint32_t mask, uint32_t wanted)
{
    /* Separate poll limit also terminates a frozen-but-monotonic counter. */
    for (unsigned n = 0; n < 1000000; n++) {
        if (guard(t))
            return -1;
        if ((read_reg(t, a) & mask) == wanted)
            return 0;
    }
    return -1;
}
static int store(struct trial *t, uint32_t a, uint64_t v)
{
    t->io->store(t->io->ctx, a, v);
    t->io->sync_memory(t->io->ctx);
    return t->io->load(t->io->ctx, a) != v;
}
static int queues_clean(struct trial *t)
{
    return read_reg(t, SMMU + 0x60) || read_reg(t, SMMU + 0x64) || read_reg(t, SMMU + 0x100a8) ||
           read_reg(t, SMMU + 0x100ac);
}
static uint64_t table_word(uint32_t a, unsigned vmid)
{
    if (a < S2_ROOT) {
        if (a == SID1_STE)
            return 13; /* V=1, CFG=stage 2 only */
        if (a == SID1_STE + 8)
            return 1ULL << 44;
        if (a == SID1_STE + 16)
            return vmid | (0x2060ULL << 32) | (1ULL << 51) | (1ULL << 54) | (1ULL << 58);
        if (a == SID1_STE + 24)
            return S2_ROOT;
        return (a - FES_TX_TABLES) % 64 == 0 ? 1 : 0; /* valid abort STE */
    }
    if (a == S2_ROOT + 8 * ((FES_TX_IOVA >> 30) & 511))
        return S2_MID | 3U;
    if (a == S2_MID + 8 * ((FES_TX_IOVA >> 21) & 511))
        return S2_LEAF | 3U;
    if (a == S2_LEAF + 8 * ((FES_TX_IOVA >> 12) & 511))
        return FES_TX_DESC | DESC_FLAGS | (3U << 6);
    if (a == S2_LEAF + 8 * ((FES_TX_BUFFER >> 12) & 511))
        return FES_TX_DATA | DESC_FLAGS | (1U << 6);
#ifdef FES_CBL_BIDIRECTIONAL
    if (a == S2_LEAF + 8 * ((FES_RX_DATA_IOVA >> 12) & 511))
        return FES_RX_DATA | DESC_FLAGS | (2U << 6); /* device-write only */
#endif
    return 0;
}
static int init_tables(struct trial *t)
{
    for (uint32_t a = FES_TX_TABLES; a < FES_TX_TABLES + 0xa000; a += 8) {
        if (((a & 255) == 0 && guard(t)) || store(t, a, table_word(a, t->vmid)))
            return -1;
    }
    return 0;
}
static int init_packets(struct trial *t, const uint8_t frame[60])
{
#ifdef FES_CBL_BIDIRECTIONAL
    const uint32_t end = FES_RX_DATA_IOVA + PAGE;
#else
    const uint32_t end = FES_TX_BUFFER + PAGE;
#endif
    for (uint32_t a = FES_TX_DESC; a < end; a += 8) {
        uint64_t word = 0;
        if (a >= FES_TX_DATA && a < FES_TX_DATA + 60) {
            unsigned offset = a - FES_TX_DATA;
            for (unsigned j = 0; j < 8 && offset + j < 60; j++)
                word |= (uint64_t)frame[offset + j] << (8 * j);
        }
        if (((a & 255) == 0 && guard(t)) || store(t, a, word))
            return -1;
    }
    return 0;
}
/* Single owner, empty queue before each publication. CMD_SYNC CS_NONE
 * completes when the hardware consumer has passed this command. */
static int command_sync(struct trial *t, uint64_t op, uint64_t argument)
{
    unsigned p = t->prod;
    if (guard(t) || queues_clean(t) || read_reg(t, SMMU + 0x98) != p ||
        read_reg(t, SMMU + 0x9c) != p)
        return -1;
    uint32_t cmd = FES_TX_CMDQ + (p % 8) * 16, sync = FES_TX_CMDQ + ((p + 1) % 8) * 16;
    if (store(t, cmd, op) || store(t, cmd + 8, argument) || store(t, sync, 0x0fc00046) ||
        store(t, sync + 8, 0))
        return -1;
    p = (p + 2) % 16;
    if (set_reg(t, SMMU + 0x98, p) || poll(t, SMMU + 0x9c, ~0U, p) || queues_clean(t))
        return -1;
    t->prod = p;
#ifdef FES_CBL_BIDIRECTIONAL
    t->out->command = p;
#endif
    return 0;
}
static int region64(struct trial *t, uint32_t offset, uint32_t base)
{
    /* Aligned 64-bit SMMU register write while its interface is disabled. */
    if (guard(t))
        return -1;
    return store(t, SMMU + offset, base);
}
static int arm(struct trial *t)
{
    if (set_reg(t, SMMU + 0x28, 0x820) || region64(t, 0x80, FES_TX_TABLES) ||
        set_reg(t, SMMU + 0x88, 8) || region64(t, 0x90, FES_TX_CMDQ | 3U) ||
        region64(t, 0xa0, FES_TX_EVTQ | 3U))
        return -1;
    if (set_reg(t, SMMU + 0x20, 12) || poll(t, SMMU + 0x24, ~0U, 12) || command_sync(t, 4, 31) ||
        command_sync(t, 0x20, 0) || command_sync(t, 0x30, 0) || set_reg(t, SMMU + 0x20, 13) ||
        poll(t, SMMU + 0x24, ~0U, 13) || queues_clean(t))
        return -1;
    return 0;
}
static int mdio_transfer(struct trial *t, uint32_t reg, unsigned opcode, uint16_t data, uint16_t *v)
{
    /* MDIO Clause 22, PHY 1, 100 MHz L4_SP CSR clock, divider 62. */
    if (poll(t, MAC + 0x204, 1U << 22, 0) || set_reg(t, MAC + 0x220, 2) ||
        set_reg(t, MAC + 0x200, 1U << 16 | reg) ||
        write_reg(t, MAC + 0x204, 1U << 22 | 1U << 18 | opcode << 16 | data) ||
        poll(t, MAC + 0x204, 1U << 22, 0))
        return -1;
    if (v) *v = (uint16_t)read_reg(t, MAC + 0x204);
    return 0;
}
static int mdio(struct trial *t, uint32_t reg, uint16_t *v)
{
    return mdio_transfer(t, reg, 3, 0, v);
}
static int sample_phy(struct trial *t, struct fes_tx_phy_sample *p)
{
    p->time_us = t->io->time_us(t->io->ctx);
    return mdio(t,2,&p->id1) || mdio(t,3,&p->id2) || mdio(t,0,&p->bmcr) ||
           mdio(t,1,&p->bmsr_latch) || mdio(t,1,&p->bmsr) ||
           mdio(t,4,&p->adv) || mdio(t,5,&p->partner) ||
           mdio(t,9,&p->gig_adv) || mdio(t,10,&p->gig_partner) ||
           mdio(t,19,&p->pcs) || mdio(t,31,&p->resolved);
}
#include "mac_phy_flp.inc"
static int link_ready(const struct fes_tx_phy_sample *p)
{
    /* Standard current link, AN, IEEE 802.3 selectors and both gigabit
     * receiver statuses. Linux micrel rejects a saturated idle-error count.
     * Microchip 13.2 reports the current 1000BASE-T PMA/PCS link. The independent
     * 100BASE-TX bit 13.1 can also be high; 1F[6:3] reports the selected speed/duplex.
     * 1F.0 is a separate vendor diagnostic, reported verbatim on both reads. */
    return p->id1 == 0x22 && p->id2 == 0x1622 && (p->bmcr & 0xde00) == 0x1000 &&
           (p->bmsr & 0x34) == 0x24 && (p->adv & 31) == 1 &&
           (p->partner & 0x201f) == 1 && (p->gig_adv & 0xe200) == 0x200 &&
           (p->gig_partner & 0xb800) == 0x3800 && (p->gig_partner & 0xff) != 0xff &&
           (p->pcs & (1U << 2)) && (p->resolved & 0x78) == 0x48;
}
static int verify_link(struct trial *t)
{
    struct fes_tx_phy_sample previous={0}, current={0};
    const uint64_t start=t->io->time_us(t->io->ctx);
    for (unsigned attempt=0; attempt<=PHY_WAIT_US/PHY_SAMPLE_US; attempt++) {
        if (guard(t) || sample_phy(t,&current)) return -1;
        if (!(attempt % 50)) t->io->report_phy(t->io->ctx, 3 + attempt / 50, &current);
        t->out->phy_status=current.resolved;
        int ready=link_ready(&current);
        /* The first BMSR read can be latched low; a transition since the
         * preceding qualifying sample breaks that pair. Subsequent pairs can qualify. */
        int stable=attempt && link_ready(&previous) && ready &&
                   (current.bmsr_latch & 0x34)==0x24;
        /* The idle-error count is RO/SC. Training can saturate it
         * before the partner receiver is ready. Such a sample breaks the
         * qualifying pair; a later pair still needs every link check. */
        const struct fes_phy_profile *phy = &t->out->phy;
        int changed = current.id1 != 0x22 || current.id2 != 0x1622 ||
                      current.bmcr != phy->bmcr || current.adv != phy->adv ||
                      current.gig_adv != phy->gig_adv;
        int expired = current.time_us-start >= PHY_WAIT_US ||
                      current.time_us-t->out->started_us >= MAX_US-TX_COMPLETION_US-CLEANUP_RESERVE_US;
        if (stable || changed || expired || attempt==PHY_WAIT_US/PHY_SAMPLE_US) {
            if (attempt) t->io->report_phy(t->io->ctx,0,&previous);
            t->io->report_phy(t->io->ctx,attempt!=0,&current);
            return stable && !changed && !expired ? 0 : -1;
        }
        previous=current;
        /* The suite also bounds a stopped counter; reserve the rest of the
         * independent domain lease for stop, revoke and readback. */
        unsigned poll_count=0;
        while (t->io->time_us(t->io->ctx)-previous.time_us < PHY_SAMPLE_US) {
            if (++poll_count>=10000000 || (!(poll_count&255) && guard(t))) return -1;
        }
    }
    return -1;
}
/* Only qualify fields that determine this small AXI DMA, MTL and MDIO path.
 * PHYSEL is a live sample of phy_intf_sel_i, unlike the capability bits. */
static int check_capabilities(struct trial *t)
{
    struct fes_tx_caps caps;
    caps.version = read_reg(t, MAC + 0x110);
    caps.feature0 = read_reg(t, MAC + 0x11c);
    caps.feature1 = read_reg(t, MAC + 0x120);
    caps.feature2 = read_reg(t, MAC + 0x124);
    caps.interface = read_reg(t, PHYSEL);
    t->io->report_caps(t->io->ctx, &caps);

    const uint32_t mdio_gigabit_axi = (1U << 31) | (1U << 5) | 3U;
    /* One 32 KiB TX FIFO, 16 KiB RX FIFO and 40-bit AXI address path.
     * All DMA addresses programmed by this diagnostic fit below 4 GiB. */
    const uint32_t fifo_address = (3U << 14) | (31U << 6) | 31U;
    /* The fitted device has eight TX/RX DMA channels and eight TX/RX
     * queues. The admission scan below covers exactly this register window. */
    const uint32_t queue_counts = (15U << 18) | (15U << 12) | (15U << 6) | 15U;
    if (caps.version != 0x107631) {
        t->out->last_address = MAC + 0x110;
        t->out->last_value = caps.version;
    } else if ((caps.interface & 3U) != FES_TX_RGMII_SELECT) {
        t->out->last_address = PHYSEL;
        t->out->last_value = caps.interface;
    } else if ((caps.feature0 & mdio_gigabit_axi) != mdio_gigabit_axi ||
               ((caps.feature0 >> 23) & 3U) != (caps.interface & 3U)) {
        t->out->last_address = MAC + 0x11c;
        t->out->last_value = caps.feature0;
    } else if ((caps.feature1 & fifo_address) != ((1U << 14) | (8U << 6) | 7U)) {
        t->out->last_address = MAC + 0x120;
        t->out->last_value = caps.feature1;
    } else if ((caps.feature2 & queue_counts) != ((7U << 18) | (7U << 12) | (7U << 6) | 7U)) {
        t->out->last_address = MAC + 0x124;
        t->out->last_value = caps.feature2;
    } else {
        return 0;
    }
    return -1;
}
static int start_mac(struct trial *t)
{
    uint32_t per0 = read_reg(t, RST), mode = read_reg(t, PHYSEL);
    if ((per0 & MAC_RESETS) != MAC_RESETS || mode & 0xc0000000U ||
        set_reg(t, PHYSEL, (mode & ~3U) | FES_TX_RGMII_SELECT))
        return -1;
    t->out->mac_release_attempted = 1;
    if (set_reg(t, RST, per0 & ~MAC_RESETS))
        return -1;
    if (guard(t) || check_capabilities(t) ||
        (read_reg(t, MAC) | read_reg(t, MAC + 4) | read_reg(t, MAC + 0x3000)) & 1)
        return -1;
    for (unsigned i = 0; i < 8; i++)
        if ((read_reg(t, MAC + 0x3104 + i * 0x80) | read_reg(t, MAC + 0x3108 + i * 0x80)) & 1)
            return -1;
    return write_reg(t, MAC + 0x3000, 1) || poll(t, MAC + 0x3000, 1, 0);
}
static int setup_tx(struct trial *t)
{
    if (set_reg(t, MAC + 0x1100, 127U << 16 | 2U << 2 | 2) || set_reg(t, MAC + 0x3004, 2) ||
        set_reg(t, MAC + 0x3100, 0) || set_reg(t, MAC + 0x3104, 4U << 16) ||
        set_reg(t, MAC + 0x3110, 0) || set_reg(t, MAC + 0x3114, FES_TX_IOVA) ||
        set_reg(t, MAC + 0x3130, 31) || set_reg(t, MAC + 0x3138, 0) ||
        set_reg(t, MAC + 0x300, 0x800001cb) || set_reg(t, MAC + 0x304, 0x0825fe02) ||
        set_reg(t, MAC, FES_TX_GIGABIT_SPEED) || write_reg(t, MAC + 0x3160, 0xffff) ||
        read_reg(t, MAC + 0x3160) & (FAULTS | 2))
        return -1;
    return 0;
}
static int send_frame(struct trial *t)
{
    if (guard(t) || store(t, FES_TX_DESC, FES_TX_BUFFER) ||
        store(t, FES_TX_DESC + 8, (uint64_t)(FIRST_LAST | OWN | 60) << 32 | 60))
        return -1;
    /* Reserve time for an independent channel stop, translation revocation
     * and reset even if the descriptor consumes its full completion window. */
    if (guard(t) || t->out->last_us - t->out->started_us >= MAX_US - TX_COMPLETION_US - CLEANUP_RESERVE_US)
        return -1;
    t->out->dma_possible = 1;
    if (set_reg(t, MAC + 0x3104, 4U << 16 | 1) || set_reg(t, MAC, FES_TX_GIGABIT_SPEED | 1))
        return -1;
    if (guard(t))
        return -1;
    t->out->dispatched = 1;
    if (write_reg(t, MAC + 0x3124, FES_TX_IOVA + 16))
        return -1;
    uint64_t completion_start=t->io->time_us(t->io->ctx);
    for (unsigned i = 0; i < 1000000; i++) {
        if (guard(t) || queues_clean(t))
            return -1;
        if (t->out->last_us-completion_start >= TX_COMPLETION_US) {
            t->out->last_address = FES_TX_DESC + 12;
            t->out->last_value = t->out->descriptor_status;
            return -1;
        }
        t->out->channel_status = read_reg(t, MAC + 0x3160);
        if (t->out->channel_status & FAULTS)
            return -1;
        t->io->sync_memory(t->io->ctx);
        t->out->descriptor_status = (uint32_t)(t->io->load(t->io->ctx, FES_TX_DESC + 8) >> 32);
        if (!(t->out->descriptor_status & OWN)) {
            t->out->last_address = FES_TX_DESC + 12;
            t->out->last_value = t->out->descriptor_status;
            return (t->out->descriptor_status & (FIRST_LAST | CONTEXT | DESCRIPTOR_ERROR)) != FIRST_LAST;
        }
    }
    return -1;
}
static int sample_tx_drain(struct trial *t, unsigned dma_mask)
{
    for (unsigned j=0; j<2; j++) {
        if (guard(t)) return -1;
        t->io->sync_memory(t->io->ctx);
        t->out->drain_dma=read_reg(t,MAC+0x3020);
        t->out->drain_mtl=read_reg(t,MAC+0x1108);
        t->out->drain_status=read_reg(t,MAC+0x3160);
        if ((t->out->drain_dma & dma_mask) || (t->out->drain_mtl & MTL_TX_ACTIVITY) ||
            (t->out->drain_status & (FAULTS | 2)) != 2) return -1;
    }
    return 0;
}
static int stop_mac(struct trial *t, unsigned dma_mask)
{
    /* Only TX channel 0 ran. Require a fresh process-stopped acknowledgement. */
    if (read_reg(t, MAC + 0x3160) & (FAULTS | 2) || set_reg(t, MAC + 0x3104, 4U << 16) ||
        poll(t, MAC + 0x3160, FAULTS | 2, 2))
        return -1;
    for (unsigned i = 0; i < 100000; i++) {
        if (guard(t)) return -1;
        if (!(read_reg(t,MAC+0x1108) & MTL_TX_ACTIVITY)) {
            if (set_reg(t,MAC,FES_TX_GIGABIT_SPEED)) return -1;
            if (sample_tx_drain(t,dma_mask)) return -1;
            t->out->process_stopped=1;
            return 0;
        }
    }
    return -1;
}
static int revoke(struct trial *t)
{
    /* Break before make. CFGI_STE targets SID1, and TLBI_S12_VMALL targets
     * its dedicated VMID1. Wait for both before publishing valid-abort. */
    if (t->out->revoke_started || guard(t) || read_reg(t, STREAM) != 3 ||
        read_reg(t, SID) != 0x10001 || read_reg(t, SMMU + 0x20) != 13 ||
        read_reg(t, SMMU + 0x24) != 13 || read_reg(t, SMMU + 0x80) != FES_TX_TABLES ||
        read_reg(t, SMMU + 0x84) || t->io->load(t->io->ctx, SID1_STE) != 13)
        return -1;
    t->out->revoke_started = 1;
    if (store(t, SID1_STE, 0))
        return -1;
    t->expected_ste = 0;
    if (command_sync(t, 3U | (1ULL << 32), 1) || command_sync(t, 0x28U | ((uint64_t)t->vmid << 32), 0) ||
        guard(t) || store(t, SID1_STE, 1))
        return -1;
    t->expected_ste = 1;
    if (command_sync(t, 3U | (1ULL << 32), 1) || queues_clean(t) ||
        read_reg(t, SMMU + 0x24) != 13 || t->io->load(t->io->ctx, SID1_STE) != 1)
        return -1;
    t->out->translation_revoked = 1;
    if (set_reg(t, RST, read_reg(t, RST) | MAC_RESETS))
        return -1;
    t->out->mac_in_reset = 1;
    return 0;
}
#ifdef FES_CBL_BIDIRECTIONAL
#include "mac_bidirectional.inc"
#endif
int fes_tx_once(const struct fes_tx_io *io, const uint8_t frame[60], struct fes_tx_result *out)
{
    /* The private build verifies the complete frame SHA, including the witness's unicast address.
     */
    static const uint8_t header[22] = {2,   0xfe, 0x25, 8,   0xcb, 1,   0x88, 0xb5, 'F', 'E', 'S',
                                       '-', 'C',  'B',  'L', '-',  'T', 'X',  '-',  'v', '1', 0};
    if (!io || !frame || !out || out->stage != FES_TX_INITIAL || out->dispatched || !io->owns ||
        !io->report_phy || !io->report_caps || !io->read || !io->write || !io->store || !io->load || !io->time_us || !io->sync_memory)
        return -1;
    struct trial t = {.io=io, .out=out, .expected_ste=13, .vmid=1};
    unsigned nonce = 0, nonzero = 0, different = 0;
    for (unsigned i = 0; i < 6; i++) {
        nonzero |= frame[i];
        different |= frame[i] ^ header[i];
    }
    if ((frame[0] & 1) || !nonzero || !different)
        return -1;
    for (unsigned i = 0; i < 60; i++) {
        if (i >= 6 && i < 28 && frame[i] != header[i - 6])
            return -1;
        if (i >= 28 && i < 44)
            nonce |= frame[i];
        if (i >= 44 && frame[i])
            return -1;
    }
    out->started_us = out->last_us = io->time_us(io->ctx);
    out->stage = FES_TX_TABLE_BUILD;
#ifdef FES_CBL_BIDIRECTIONAL
    if (!out->rx || !io->report_grant || !nonce) return -1;
    out->vmid=out->previous ? 2 : 1;
    for (unsigned i=0; i<16; i++) out->nonce[i]=frame[28+i];
    if (out->previous) {
        if (regrant(&t,frame)) return -1;
        goto granted;
    }
#endif
    if (!nonce || guard(&t) || (read_reg(&t, RST) & MAC_RESETS) != MAC_RESETS ||
        read_reg(&t, SMMU) != 0x080f7e3f || read_reg(&t, SMMU + 4) != 0x0e739d18 ||
        read_reg(&t, SMMU + 0x14) != 0x400075 || read_reg(&t, SMMU + 0x20) ||
        read_reg(&t, SMMU + 0x24) || read_reg(&t, SMMU + 0x8020) || read_reg(&t, SMMU + 0x8024) ||
        read_reg(&t, SMMU + 0x28) || read_reg(&t, SMMU + 0x2c) != 4 || queues_clean(&t) ||
        read_reg(&t, SMMU + 0x98) || read_reg(&t, SMMU + 0x9c))
        return -1;
    /* ATF has not run. Select known SID and security at both read/write ports
     * while TSN remains reset. Close global bypass before publishing tables. */
    uint32_t routing = read_reg(&t, STREAM), ids = read_reg(&t, SID),
             gbpa = read_reg(&t, SMMU + 0x44);
    if (!((routing == 0x30 && ids == 0) || (routing == 3 && ids == 0x10001)) ||
        (gbpa != 0 && gbpa != NS_ABORT) || read_reg(&t, SMMU + 0x48))
        return -1;
    if (set_reg(&t, SID, 0x10001) || set_reg(&t, STREAM, 3) ||
        write_reg(&t, SMMU + 0x44, (1U << 31) | NS_ABORT) || poll(&t, SMMU + 0x44, ~0U, NS_ABORT) ||
        init_tables(&t) || init_packets(&t, frame))
        return -1;
    out->stage = FES_TX_ARM;
    out->arm_attempted = 1;
    if (arm(&t))
        return -1;
#ifdef FES_CBL_BIDIRECTIONAL
granted:
#endif
    out->mapped = 1;
#ifdef FES_CBL_BIDIRECTIONAL
    if (guard(&t) || read_reg(&t,SMMU+0x98)!=t.prod || read_reg(&t,SMMU+0x9c)!=t.prod) return -1;
    io->report_grant(io->ctx,t.vmid,t.prod,t.expected_ste);
#endif
    out->stage = FES_TX_RELEASE_MAC;
    if (start_mac(&t))
        goto failed;
    out->stage = FES_TX_PHY;
    if (prepare_flp(&t) || verify_link(&t))
        goto failed;
    out->stage = FES_TX_DISPATCH;
    if (setup_tx(&t)) goto failed;
#ifdef FES_CBL_BIDIRECTIONAL
    if (bidi_transfer(&t,frame)) goto failed;
#else
    if (send_frame(&t)) goto failed;
#endif
    out->stage = FES_TX_STOP;
    if (!out->process_stopped && stop_mac(&t,0xff03))
        goto failed;
    out->stage = FES_TX_REVOKE;
    int restored = restore_flp(&t);
    if (revoke(&t) || restored) return -1;
    out->stage = FES_TX_PARKED;
    return 0;
failed:
    if (!out->fault_address) {
        out->fault_address = out->last_address;
        out->fault_value = out->last_value;
    }
    /* Revoke only while the original controlled domain is still proved present. */
    if (!guard(&t) && !queues_clean(&t)) {
        /* Only a completion failure after the tail doorbell can take the
         * normal stop path. A failed stop is never retried or called drained. */
#ifndef FES_CBL_BIDIRECTIONAL
        if (out->stage == FES_TX_DISPATCH && out->dma_possible && out->dispatched)
            (void)stop_mac(&t,0xff03);
#endif
        int dma_idle = !out->dma_possible || out->process_stopped;
#ifdef FES_CBL_BIDIRECTIONAL
        /* RX starts before TX. An uncertain RX enable can leave the
         * external link in use even when no TX descriptor was submitted. */
        dma_idle = dma_idle && (!out->rx->dma_possible || out->rx->stopped);
#endif
        if (dma_idle) (void)restore_flp(&t);
        (void)revoke(&t);
    }
    return -1;
}

/* Separate terminal observation after all writes, while the standalone SPL
 * still owns its initialized DDR and held MAC. No access to MAC registers. */
enum fes_tx_disposition fes_tx_terminal(const struct fes_tx_io *io, struct fes_tx_result *r, int rc)
{
    if (!r->fault_address && rc) {
        r->fault_address = r->last_address;
        r->fault_value = r->last_value;
    }
    struct trial t = {.io=io, .out=r, .expected_ste=1, .vmid=1};
#ifdef FES_CBL_BIDIRECTIONAL
    if (r->vmid == 2) t.vmid=2;
    t.prod=r->command;
    if (r->rx && r->rx->dma_possible) {
        if (!r->rx->stopped || !r->translation_revoked || !r->mac_in_reset ||
            !r->process_stopped || retained_after_reset(&t)) return FES_TX_COLD_RECOVERY;
    }
#endif
    if (r->scope_lost || (r->phy.portal && !r->phy.restored))
        return FES_TX_COLD_RECOVERY;
    if (rc && r->stage <= FES_TX_TABLE_BUILD && !r->write_attempted && !r->arm_attempted &&
        !r->mapped && !r->mac_release_attempted && !r->dma_possible && !r->dispatched &&
        !r->process_stopped && !r->revoke_started && !r->translation_revoked && !r->mac_in_reset) {
        /* Repeat the physical observations under their own ownership predicate.
         * The clock/SDM/DDR profile needed for TX has no role in an idle read. */
        for (unsigned i = 0; i < 2; i++) {
            if (!io->observe_unchanged || io->observe_unchanged(io->ctx) != 1)
                return FES_TX_COLD_RECOVERY;
            io->sync_memory(io->ctx);
            if (read_reg(&t, SMMU) != 0x080f7e3f || read_reg(&t, SMMU + 4) != 0x0e739d18 ||
                read_reg(&t, SMMU + 0x14) != 0x400075 || read_reg(&t, SMMU + 0x8020) ||
                read_reg(&t, SMMU + 0x8024) || read_reg(&t, SMMU + 0x98) ||
                read_reg(&t, SMMU + 0x9c) || queues_clean(&t))
                return FES_TX_COLD_RECOVERY;
            r->terminal_route = read_reg(&t, STREAM);
            r->terminal_sid = read_reg(&t, SID);
            r->terminal_cr0 = read_reg(&t, SMMU + 0x20);
            r->terminal_ack = read_reg(&t, SMMU + 0x24);
            r->terminal_per0 = read_reg(&t, RST);
            if (r->terminal_route != 0x30 || r->terminal_sid || r->terminal_cr0 ||
                r->terminal_ack || r->terminal_per0 != 0xff7fbfff)
                return FES_TX_COLD_RECOVERY;
        }
        return FES_TX_UNCHANGED;
    }
    if (io->owns(io->ctx) != 1)
        return FES_TX_COLD_RECOVERY;
    io->sync_memory(io->ctx);
    r->terminal_route = read_reg(&t, STREAM);
    r->terminal_sid = read_reg(&t, SID);
    r->terminal_cr0 = read_reg(&t, SMMU + 0x20);
    r->terminal_ack = read_reg(&t, SMMU + 0x24);
    if (read_reg(&t, SMMU + 0x8020) || read_reg(&t, SMMU + 0x8024) || queues_clean(&t))
        return FES_TX_COLD_RECOVERY;
    if (r->mapped && r->translation_revoked && r->mac_in_reset) {
        if (!domain_current(&t))
            return FES_TX_COLD_RECOVERY;
        r->terminal_ste = (uint32_t)io->load(io->ctx, SID1_STE);
    }
    if (r->scope_lost || io->owns(io->ctx) != 1)
        return FES_TX_COLD_RECOVERY;
    r->terminal_per0 = read_reg(&t, RST);
    if (r->terminal_per0 != 0xff7fbfffU)
        return FES_TX_COLD_RECOVERY;
    if (!r->write_attempted || !r->arm_attempted || !r->mapped || !r->translation_revoked ||
        !r->mac_in_reset || r->terminal_ste != 1)
        return FES_TX_COLD_RECOVERY;
    if (rc && r->stage >= FES_TX_RELEASE_MAC && r->stage <= FES_TX_DISPATCH && !r->dma_possible &&
        !r->dispatched && !r->process_stopped)
        return FES_TX_REVOKED_UNDISPATCHED;
    if (rc && r->stage == FES_TX_DISPATCH && r->dma_possible && r->dispatched &&
        r->process_stopped && r->fault_address && !(r->channel_status & FAULTS))
        return FES_TX_FAILED_DRAINED;
    if (!rc && r->stage == FES_TX_PARKED && r->dispatched && r->process_stopped)
        return FES_TX_CLOSED;
    return FES_TX_COLD_RECOVERY;
}
