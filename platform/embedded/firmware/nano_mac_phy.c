/* SPDX-License-Identifier: BSD-3-Clause */
/* DE25-Nano MAC0/KSZ9031. Register operations, training and FLP restoration
 * follow the CBL two-round SPL at 4d39e006427eaff785d09a69ff9ff19faf9a3343.
 * This leaf never publishes a stream, starts an engine or accesses a descriptor. */
#include "nano_mac_phy.h"
#ifndef FES_NANO_MAC_PHY_TEST
#include "nano_mac_phy_platform.inc"
#else
/* Host fixture supplies the same four primitives. */
extern uint32_t phy_read(uint32_t);
extern void phy_write(uint32_t, uint32_t);
extern uint64_t phy_us(void);
extern int phy_clock_valid(void);
#endif

_Static_assert(sizeof(struct fes_nano_mac_phy) == 28, "retained CBL PHY snapshot");

#define MAC 0x10810000U
#define CLOCK 0x10d10000U
#define RESET 0x10d11024U
#define MODE 0x10d12044U
#define OSC_HZ 0x10d12204U
#define RESET_MASK 0x101U
#define MDIO_BUSY (1U << 22)
#define TRAIN_US 25000000U
#define SAMPLE_US 100000U

struct operation {
    const struct fes_nano_mac_phy_guards *guards;
    uint64_t start, last;
    uint32_t budget;
    unsigned retiring, mac_live;
};

static int owner(const struct fes_nano_mac_phy_guards *g)
{
    return g && g->owned && g->drained && g->owned(g->context) == 1;
}

/* Only the fitted external reference and PLL paths are admitted. Equations and
 * divider masks follow Terasic's Agilex5 clk_get_{l4_sp,emac}_clk_hz. */
static uint64_t pll(unsigned global, unsigned mult, unsigned output, unsigned m, unsigned c)
{
    uint32_t cfg = phy_read(CLOCK + global);
    uint32_t ref_div = (cfg >> 8) & 15U;
    uint32_t out_div = phy_read(CLOCK + output) & 0x7ffU;
    uint32_t multiplier = phy_read(CLOCK + mult) & 0x3ffU;
    if (((cfg >> 16) & 3U) || ref_div != 1 || multiplier != m || out_div != c) return 0;
    return (uint64_t)(25000000U / ref_div) * multiplier / out_div;
}

static int clocks(void)
{
    if ((phy_read(CLOCK + 4) & 0x01030301U) != 0x00030300U ||
        (phy_read(CLOCK + 0x24) & 14U) != 14U || (phy_read(CLOCK + 0x30) & 2U) ||
        (phy_read(CLOCK + 0x7c) & 1U) != 1 || (phy_read(CLOCK + 0x94) & (1U << 26)) ||
        ((phy_read(CLOCK + 0x88) | phy_read(CLOCK + 0xf4)) & 1U) ||
        phy_read(OSC_HZ) != 25000000U || ((phy_read(CLOCK + 0x40) >> 16) & 7U)) return 0;
    uint32_t emac = phy_read(CLOCK + 0xd4);
    unsigned source = (emac >> 16) & 7U;
    unsigned div = (phy_read(CLOCK + 0x44) >> 6) & 3U;
    if (source != 1 || (emac & 0x7ffU) != 1 || div != 2) return 0;
    uint64_t reference = pll(0x9c, 0xc0, 0xbc, 120, 6);
    uint64_t l4 = pll(0x48, 0x6c, 0x68, 128, 8) >> div;
    return l4 == 100000000U && reference / (1U + (emac & 0x7ffU)) == 250000000U;
}

static struct operation begin(const struct fes_nano_mac_phy_guards *g, uint32_t budget, unsigned retiring)
{
    uint64_t now = phy_us();
    return (struct operation){g, now, now, budget, retiring, 0};
}

static int guard(struct operation *o)
{
    uint64_t now = phy_us();
    if (now < o->last || now - o->start >= o->budget || !phy_clock_valid() ||
        !owner(o->guards) || !clocks()) return -1;
    if (o->mac_live && ((phy_read(RESET) & RESET_MASK) || (phy_read(MODE) & 3U) != 1)) return -1;
    if (o->retiring && o->guards->drained(o->guards->context) != 1) return -1;
    o->last = phy_us();
    return o->last < now || o->last - o->start >= o->budget ? -1 : 0;
}

static int put(struct operation *o, uint32_t addr, uint32_t value)
{
    if (guard(o)) return -1;
    phy_write(addr, value);
    return 0;
}

static int set(struct operation *o, uint32_t addr, uint32_t value)
{
    if (put(o, addr, value)) return -1;
    /* After asserting reset only the reset manager, outside that domain, is readable. */
    if (addr == RESET && (value & RESET_MASK) == RESET_MASK) o->mac_live = 0;
    return guard(o) || phy_read(addr) != value;
}

static int poll_clear(struct operation *o, uint32_t addr, uint32_t mask)
{
    uint64_t first = phy_us();
    for (unsigned n = 0; n < 100000; n++) {
        if (guard(o) || o->last < first || o->last - first >= 100000U) return -1;
        if (!(phy_read(addr) & mask)) return 0;
    }
    return -1;
}

static int mac_identity(void)
{
    uint32_t f0 = phy_read(MAC + 0x11c);
    const uint32_t fixed = (1U << 31) | (1U << 5) | 3U;
    const uint32_t fifo = (3U << 14) | (31U << 6) | 31U;
    const uint32_t queues = (15U << 18) | (15U << 12) | (15U << 6) | 15U;
    return phy_read(MAC + 0x110) == 0x107631 && (phy_read(MODE) & 3U) == 1 &&
           (f0 & fixed) == fixed && ((f0 >> 23) & 3U) == 1 &&
           (phy_read(MAC + 0x120) & fifo) == ((1U << 14) | (8U << 6) | 7U) &&
           (phy_read(MAC + 0x124) & queues) == ((7U << 18) | (7U << 12) | (7U << 6) | 7U);
}

static int stopped(void)
{
    if ((phy_read(MAC) | phy_read(MAC + 4) | phy_read(MAC + 0x3000)) & 1U) return 0;
    for (unsigned i = 0; i < 8; i++)
        if ((phy_read(MAC + 0x3104 + i * 0x80) | phy_read(MAC + 0x3108 + i * 0x80)) & 1U)
            return 0;
    return 1;
}

static int access_mdio(struct operation *o, unsigned reg, unsigned opcode, uint16_t data, uint16_t *out)
{
    if ((phy_read(RESET) & RESET_MASK) || poll_clear(o, MAC + 0x204, MDIO_BUSY) ||
        set(o, MAC + 0x220, 2) || set(o, MAC + 0x200, (1U << 16) | reg) ||
        put(o, MAC + 0x204, MDIO_BUSY | (1U << 18) | (opcode << 16) | data) ||
        poll_clear(o, MAC + 0x204, MDIO_BUSY)) return -1;
    if (out) *out = (uint16_t)phy_read(MAC + 0x204);
    return 0;
}

static int get_phy(struct operation *o, unsigned reg, uint16_t *out)
{
    return access_mdio(o, reg, 3, 0, out);
}
static int put_phy(struct operation *o, unsigned reg, uint16_t value)
{
    if (reg != 0 && reg != 13 && reg != 14) return -1;
    return access_mdio(o, reg, 1, value, 0);
}
static int set_phy(struct operation *o, unsigned reg, uint16_t value)
{
    uint16_t seen;
    return put_phy(o, reg, value) || get_phy(o, reg, &seen) || seen != value;
}
static int select_word(struct operation *o, unsigned reg)
{
    return set_phy(o, 13, 0) || set_phy(o, 14, reg) || set_phy(o, 13, 0x4000);
}
static int read_word(struct operation *o, unsigned reg, uint16_t *out)
{
    return select_word(o, reg) || get_phy(o, 14, out);
}
static int write_pair(struct operation *o, uint16_t hi, uint16_t lo)
{
    return select_word(o, 4) || set_phy(o, 14, hi) || select_word(o, 3) || set_phy(o, 14, lo);
}
static int restart(struct operation *o, uint16_t original)
{
    uint16_t seen;
    if (get_phy(o, 0, &seen) || seen != original || put_phy(o, 0, original | 0x200)) return -1;
    uint64_t first = phy_us();
    for (unsigned n = 0; n < 10000; n++) {
        if (get_phy(o, 0, &seen) || o->last < first || o->last - first > 500000U) return -1;
        if (seen == original) return 0;
        if (seen != (original | 0x200)) return -1;
    }
    return -1;
}

static int fixed_phy(struct operation *o, uint16_t *bmcr, uint16_t *adv, uint16_t *gig)
{
    uint16_t id1, id2;
    return get_phy(o, 2, &id1) || get_phy(o, 3, &id2) || id1 != 0x22 || id2 != 0x1622 ||
           get_phy(o, 0, bmcr) || get_phy(o, 4, adv) || get_phy(o, 9, gig) ||
           (*bmcr & 0xde00) != 0x1000 || (*adv & 31) != 1 || (*gig & 0xe200) != 0x200;
}
static int same_phy(struct operation *o, const struct fes_nano_mac_phy *p)
{
    uint16_t bmcr, adv, gig;
    return fixed_phy(o, &bmcr, &adv, &gig) || bmcr != p->bmcr || adv != p->adv || gig != p->gig_adv;
}

static int prepare_flp(struct operation *o, struct fes_nano_mac_phy *p)
{
    uint16_t hi, lo;
    if (fixed_phy(o, &p->bmcr, &p->adv, &p->gig_adv) ||
        get_phy(o, 13, &p->selector) || (p->selector & 0x3fe0)) return -1;
    p->portal = 1;
    if (set_phy(o, 13, p->selector & 31) || get_phy(o, 14, &p->pointer) ||
        set_phy(o, 13, 0) || get_phy(o, 14, &p->dev0_pointer)) return -1;
    p->pointers = 1;
    if (read_word(o, 4, &p->high) || read_word(o, 3, &p->low)) return -1;
    p->snapshot = 1;
    if (p->high == 3 && p->low == 0x4000) {
        p->timing_changed = 1;
        if (write_pair(o, 6, 0x1a80)) return -1;
    } else if (p->high != 6 || p->low != 0x1a80) return -1;
    return read_word(o, 4, &hi) || read_word(o, 3, &lo) || hi != 6 || lo != 0x1a80 ||
           restart(o, p->bmcr);
}

static int train(struct operation *o, const struct fes_nano_mac_phy *p)
{
    uint64_t start = phy_us();
    int previous = 0;
    for (unsigned n = 0; n <= TRAIN_US / SAMPLE_US; n++) {
        uint64_t sample = phy_us();
        uint16_t latch, current, partner, stat, pcs, resolved;
        if (same_phy(o, p) || get_phy(o, 1, &latch) || get_phy(o, 1, &current) ||
            get_phy(o, 5, &partner) || get_phy(o, 10, &stat) ||
            get_phy(o, 19, &pcs) || get_phy(o, 31, &resolved) || guard(o) ||
            o->last < start || o->last - start >= TRAIN_US) return -1;
        int ready = (current & 0x34) == 0x24 && (partner & 0x201f) == 1 &&
                    (stat & 0xb800) == 0x3800 && (stat & 0xff) != 0xff &&
                    (pcs & 4) && (resolved & 0x78) == 0x48;
        if (ready && previous && (latch & 0x34) == 0x24) return 0;
        previous = ready;
        for (unsigned spin = 0; ; spin++) {
            uint64_t now = phy_us();
            if (now < sample || spin >= 10000000 || (!(spin & 255) && guard(o))) return -1;
            if (now - sample >= SAMPLE_US) break;
        }
    }
    return -1;
}

static int restore_flp(struct operation *o, const struct fes_nano_mac_phy *p)
{
    uint16_t hi, lo, ptr;
    if (!p->portal) return 0;
    if (same_phy(o, p)) return -1;
    if (p->timing_changed && (read_word(o, 4, &hi) || read_word(o, 3, &lo) ||
        (hi != 6 && hi != p->high) || (lo != 0x1a80 && lo != p->low) ||
        write_pair(o, p->high, p->low) || restart(o, p->bmcr))) return -1;
    if (p->snapshot && (read_word(o, 4, &hi) || read_word(o, 3, &lo) ||
                         hi != p->high || lo != p->low)) return -1;
    if (p->pointers && (set_phy(o, 13, 0) || set_phy(o, 14, p->dev0_pointer) ||
                        set_phy(o, 13, p->selector & 31) || get_phy(o, 14, &ptr) ||
                        ptr != p->pointer)) return -1;
    return set_phy(o, 13, p->selector) || same_phy(o, p);
}

int fes_nano_mac_phy_prepare(struct fes_nano_mac_phy *p, const struct fes_nano_mac_phy_guards *g)
{
    if (!p || p->begun || !owner(g) || !phy_clock_valid()) return -1;
    struct operation op = begin(g, 30000000, 0);
    if (guard(&op)) return -1;
    uint32_t per0 = phy_read(RESET), mode = phy_read(MODE);
    if ((per0 & RESET_MASK) != RESET_MASK || (mode & 0xc0000000U)) return -1;
    p->begun = 1;
    p->original_mode = mode;
    if (set(&op, MODE, (mode & ~3U) | 1)) return -1;
    p->released = 1;
    if (set(&op, RESET, per0 & ~RESET_MASK)) return -1;
    op.mac_live = 1;
    if (guard(&op) || !mac_identity() || !stopped() ||
        put(&op, MAC + 0x3000, 1) || poll_clear(&op, MAC + 0x3000, 1) ||
        prepare_flp(&op, p) || train(&op, p) || guard(&op) || !stopped()) return -1;
    p->ready = 1;
    return 0;
}

int fes_nano_mac_phy_active(const struct fes_nano_mac_phy *p, const struct fes_nano_mac_phy_guards *g)
{
    if (!p || !p->ready || !owner(g) || !phy_clock_valid() || !clocks() ||
        (phy_read(RESET) & RESET_MASK) || phy_read(MODE) != ((p->original_mode & ~3U) | 1))
        return -1;
    return mac_identity() && owner(g) ? 0 : -1;
}

int fes_nano_mac_phy_retire(struct fes_nano_mac_phy *p, const struct fes_nano_mac_phy_guards *g)
{
    if (!p || !p->begun) return -1;
    p->ready = 0;
    if (!owner(g) || !phy_clock_valid()) return -1;
    struct operation op = begin(g, 5000000, 1);
    if (guard(&op)) return -1;
    uint32_t per0 = phy_read(RESET), mode = phy_read(MODE);
    if (mode != ((p->original_mode & ~3U) | 1) && mode != p->original_mode) return -1;
    if (p->released && (per0 & RESET_MASK) == 0) {
        op.mac_live = 1;
        if (guard(&op) || !mac_identity() || !stopped() || restore_flp(&op, p) ||
            guard(&op) || !stopped() || set(&op, RESET, phy_read(RESET) | RESET_MASK)) return -1;
    } else if ((per0 & RESET_MASK) != RESET_MASK || p->portal) return -1;
    if (set(&op, MODE, p->original_mode) || guard(&op) ||
        (phy_read(RESET) & RESET_MASK) != RESET_MASK || phy_read(MODE) != p->original_mode)
        return -1;
    *p = (struct fes_nano_mac_phy){0};
    return 0;
}
