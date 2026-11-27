/* SPDX-License-Identifier: BSD-3-Clause */
/* EL3 observation of the production CBL XGMAC2 driver. No MMIO or DMA writes.
 * Terminal masks use the two independent channel/AXI acknowledgements from the
 * CBL BIDI05 trial, 4d39e006427eaff785d09a69ff9ff19faf9a3343. */
#include <string.h>
#include "nano_mac_channel.h"
#include "input_tables.h"

#define PHYS ((uint32_t)NANO_MAC_POOL_BASE)
#define MAC 0x10810000U
#define RESET 0x10d11024U
#define RESETS 0x101U
#define ARENA (5U * 4096U)
#define RING_BYTES (32U * 16U)
#define RX_BASE (PHYS + RING_BYTES)
#define TX_CONTROL (4U << 16)
#define RX_CONTROL (TX_CONTROL | (1600U << 1))
#define TX_CONFIG (3U << 29)
#define RX_CONFIG 6U
#define STOPPED 0x102U
#define FAULT 0x3200U
#define OWN (1U << 31)
#define IOC (1U << 30)
#define FIRST_LAST (3U << 28)
#define CHECK_US 500000U
#define DWELL_US 10000U

#ifdef FES_NANO_MAC_CHANNEL_TEST
/* Use the host's standard SHA-384 for register/arena model tests. */
#define OPENSSL_API_COMPAT 0x10100000L
#include <openssl/sha.h>
typedef SHA512_CTX digest_context;
#define digest_init SHA384_Init
#define digest_update SHA384_Update
#define digest_finish(ctx, out) SHA384_Final(out, ctx)
extern uint32_t channel_mmio(uint32_t);
extern uint32_t channel_word(uint32_t);
extern uint64_t channel_time(void);
extern int channel_frequency(void);
extern void channel_barrier(void);
#else
#include <arch_helpers.h>
#include <lib/mmio.h>
#include "sha.h"
typedef sha512_context digest_context;
#define digest_init sha384_init
#define digest_update sha384_update
#define digest_finish sha384_finish
static uint32_t channel_mmio(uint32_t address) { return mmio_read_32(address); }
static uint32_t channel_word(uint32_t offset)
{ return *(volatile const uint32_t *)(uintptr_t)(PHYS + offset); }
static uint64_t channel_time(void) { return read_cntpct_el0() / 400U; }
static int channel_frequency(void) { return read_cntfrq_el0() == 400000000U; }
static void channel_barrier(void) { dsbsy(); }
#endif

enum failure { START = 1, CONFIRM, SCOPE, DRAIN, ACK, RETAIN };
struct observation {
    const fes_channel_guards *guards;
    uint64_t generation, start, last;
    unsigned revoked;
};
static struct observation begin(const fes_channel_guards *g, uint64_t generation, unsigned revoked)
{
    uint64_t now = channel_time();
    return (struct observation){g, generation, now, now, revoked};
}
static int guard(struct observation *o)
{
    uint64_t first = channel_time();
    if (first < o->last || first - o->start >= CHECK_US || !channel_frequency() ||
        !o->generation || !o->guards || !o->guards->owned ||
        o->guards->owned(o->guards->context, o->generation, o->revoked) != 1) return -1;
    channel_barrier();
    o->last = channel_time();
    return o->last < first || o->last - o->start >= CHECK_US ? -1 : 0;
}
static int fail(fes_channel *c, enum failure reason)
{
    if (c) {
        if (!c->failed_stage) c->failed_stage = reason;
        c->phase = FES_CHANNEL_FAILED;
    }
    return -1;
}
static uint32_t reg(unsigned offset) { return channel_mmio(MAC + offset); }
static int released(struct observation *o)
{
    return !guard(o) && !(channel_mmio(RESET) & RESETS);
}
static int other_channels_stopped(void)
{
    for (unsigned n = 1; n < 8; n++)
        if ((reg(0x3104 + n*0x80) | reg(0x3108 + n*0x80)) & 1U) return 0;
    return 1;
}
static uint64_t station(void)
{
    uint32_t high = reg(0x300), low = reg(0x304);
    /* Address zero can report its permanent Address Enable bit. No mask,
     * alternate source match or assigned global/multicast address is admitted. */
    if ((high & 0x7fff0000U) || (low & 3) != 2) return 0;
    return ((uint64_t)(high & 0xffffU) << 32) | low;
}
static int filter(void)
{
    /* The matched XGMAC advertises 31 additional perfect-match addresses.
     * The driver programs only address zero. */
    if (((reg(0x11c) >> 18) & 31U) != 31U) return 0;
    for (unsigned i = 1; i <= 31; i++)
        if (reg(0x300 + 8*i) & (1U << 31)) return 0;
    /* Fitted 64-bit MAC hash filter. The standard driver's CRC32 bit for
     * 01:00:5e:00:00:fb is 48. Both initial unicast/broadcast and the one
     * mDNS hash are needed across the first and subsequent confirmations. */
    uint32_t value = reg(8);
    if (!value) return 1;
    return value == 4U && !reg(0x10) && reg(0x14) == (1U << 16);
}
static int tail(uint32_t value, uint32_t base)
{
    return value >= base && value < base + RING_BYTES && !(value & 15U);
}
static int layout(uint64_t mac)
{
    uint32_t tx = reg(0x3124), irq = reg(0x3138);
    return station() == mac && filter() && other_channels_stopped() && !reg(0xb4) &&
           (!irq || irq == 0x8040) &&
           !reg(0x3000) && reg(0x3004) == 2 && !reg(0x3100) &&
           !reg(0x3110) && reg(0x3114) == PHYS && !reg(0x3118) && reg(0x311c) == RX_BASE &&
           reg(0x3130) == 31 && reg(0x3134) == 31 && (!tx || tail(tx, PHYS)) &&
           tail(reg(0x312c), RX_BASE) && !reg(0x1030) && reg(0xa0) == 2 &&
           reg(0xa4) == (1U << 15) && !reg(0xa8) &&
           reg(0x1100) == ((127U << 16) | 10U) && reg(0x1140) == ((63U << 16) | 32U);
}

/* Never dereference a device-supplied address. On OWN->CPU, RDES0..2 have
 * hardware writeback meanings. In read format the programmed slot and flags
 * must be exact. One bounded reread handles hardware returning OWN mid-sample.
 * The CPUs that could repost are stopped in this SMC; DMA cannot set OWN. */
static int descriptors(struct observation *o, int initial)
{
    unsigned rx_owned = 0, tx_owned = 0;
    for (unsigned i = 0; i < 64; i++) {
        if (!(i % 8) && !released(o)) return -1;
        uint32_t off = i * 16;
        uint32_t flags = channel_word(off + 12);
        channel_barrier();
        uint32_t address = channel_word(off), high = channel_word(off + 4);
        uint32_t size = channel_word(off + 8);
        channel_barrier();
        uint32_t end = channel_word(off + 12);
        if (end != flags && (!(flags & OWN) || (end & OWN))) return -1;
        if (initial && (i < 32 || i >= 36)) {
            if (address || high || size || flags || end) return -1;
        } else if (end & OWN) {
            if (i < 32) {
                if (tx_owned++ || address != PHYS + 4096 + (i % 4)*1600 || high || size < 14 || size > 1514 ||
                    end != (OWN | FIRST_LAST | size)) return -1;
            } else {
                unsigned bit = 1U << (i % 4);
                if ((rx_owned & bit) || address != PHYS + 3*4096 + (i % 4)*1600 ||
                    high || size || end != (OWN | IOC)) return -1;
                rx_owned |= bit;
            }
        } else if (initial && (end & (IOC | FIRST_LAST)) != FIRST_LAST) {
            return -1;
        }
    }
    return 0;
}
static int engines(unsigned on)
{
    return reg(0) == (TX_CONFIG | on) && reg(4) == (RX_CONFIG | on) &&
           reg(0x3104) == (TX_CONTROL | on) && reg(0x3108) == (RX_CONTROL | on);
}
static int idle_registers(fes_channel *c)
{
    c->status = reg(0x3160);
    c->dma_debug = reg(0x3020);
    c->channel_debug = reg(0x3164);
    c->mac_debug = reg(0x114);
    c->tx_debug = reg(0x1108);
    c->rx_debug = reg(0x1148);
    return (c->status & (STOPPED | FAULT)) == STOPPED && !(c->dma_debug & 0xff03) &&
           !(c->channel_debug & 0x7fff0000) && !(c->mac_debug & 7) &&
           !(c->tx_debug & 0x3e) && !(c->rx_debug & 0x3fff0037);
}
static int active_registers(uint64_t mac)
{
    return layout(mac) && engines(1) && !(reg(0x3160) & (STOPPED | FAULT));
}
/* The Go driver may yield while closing in this order. Such a reservation
 * stays active until the independent drain and SMMU revocation both complete.
 * A stopped engine cannot be restarted within the same confirmed generation. */
static int progress(fes_channel *c)
{
    if (!layout(c->station)) return -1;
    uint32_t tx = reg(0x3104), tx_mac = reg(0), rx_mac = reg(4), rx = reg(0x3108);
    if ((tx & ~1U) != TX_CONTROL || (tx_mac & ~1U) != TX_CONFIG ||
        (rx_mac & ~1U) != RX_CONFIG || (rx & ~1U) != RX_CONTROL) return -1;
    unsigned starts = ((tx & 1U) << 3) | ((tx_mac & 1U) << 2) | ((rx_mac & 1U) << 1) | (rx & 1U);
    uint32_t status = reg(0x3160) & (FAULT | STOPPED);
    unsigned step;
    switch (starts) {
    case 15: if (status) return -1; step = 0; break;
    case 7: if (status & ~2U) return -1; step = 1; break;
    case 3: if (status != 2) return -1; step = 2; break;
    case 1: if (status != 2) return -1; step = 3; break;
    case 0: if (status != 2 && status != STOPPED) return -1;
            step = status == STOPPED ? 5 : 4; break;
    default: return -1;
    }
    if (step < c->stop_progress) return -1;
    c->stop_progress = step;
    return 0;
}
static int stopped_registers(fes_channel *c)
{
    return layout(c->station) && engines(0) && !reg(0x3138) && idle_registers(c);
}

/* Hash actual ordered 32-bit loads from all five fixed uncached pages, under
 * a fresh scope and the independent deadline at every 256-byte batch. */
static int arena(struct observation *o, uint8_t digest[48])
{
    digest_context ctx;
    uint32_t words[64];
    uint64_t start = o->last;
    digest_init(&ctx);
    for (unsigned off = 0; off < ARENA; off += sizeof(words)) {
        if (guard(o)) return -1;
        for (unsigned i = 0; i < 64; i++) words[i] = channel_word(off + 4*i);
        channel_barrier();
        digest_update(&ctx, (const unsigned char *)words, sizeof(words));
    }
    digest_finish(&ctx, digest);
    return guard(o) || o->last <= start;
}
int fes_nano_mac_channel_prepare(fes_channel *c, const fes_channel_guards *g, uint64_t generation)
{
    struct observation o = begin(g, generation, 0);
    if (!c || (c->phase != FES_CHANNEL_NEW && c->phase != FES_CHANNEL_RETAINED) ||
        generation <= c->generation || !released(&o) || !other_channels_stopped() ||
        (reg(0) | reg(4) | reg(0x3104) | reg(0x3108) | reg(0x3000)) & 1U ||
        reg(0x3160) & (STOPPED | FAULT) || !released(&o)) return fail(c, START);
    c->generation = generation;
    c->stop_progress = 0;
    c->phase = FES_CHANNEL_PREPARED;
    return 0;
}
int fes_nano_mac_channel_confirm(fes_channel *c, const fes_channel_guards *g, uint64_t generation)
{
    struct observation o = begin(g, generation, 0);
    if (!c || c->phase != FES_CHANNEL_PREPARED || c->generation != generation || !released(&o))
        return fail(c, CONFIRM);
    uint64_t mac = station();
    if (!mac || (c->station && c->station != mac) || !active_registers(mac) ||
        reg(0x3124) || reg(0x312c) != RX_BASE + 4*16 || descriptors(&o, 1) ||
        !released(&o) || !active_registers(mac)) return fail(c, CONFIRM);
    c->station = mac;
    c->phase = FES_CHANNEL_ACTIVE;
    return 0;
}
int fes_nano_mac_channel_scope(fes_channel *c, const fes_channel_guards *g, uint64_t generation)
{
    struct observation o = begin(g, generation, 0);
    if (!c || c->phase != FES_CHANNEL_ACTIVE || c->generation != generation || !released(&o) ||
        progress(c) || descriptors(&o, 0) || !released(&o) || progress(c)) return fail(c, SCOPE);
    return 0;
}
int fes_nano_mac_channel_drain(fes_channel *c, const fes_channel_guards *g, uint64_t generation)
{
    uint8_t verified[48];
    struct observation o = begin(g, generation, 0);
    if (!c || c->phase != FES_CHANNEL_ACTIVE || c->generation != generation ||
        !released(&o) || !stopped_registers(c) || descriptors(&o, 0) ||
        arena(&o, c->arena_sha384) || !released(&o) || !stopped_registers(c) ||
        arena(&o, verified) || memcmp(verified, c->arena_sha384, 48) ||
        !released(&o) || !stopped_registers(c)) return fail(c, DRAIN);
    c->phase = FES_CHANNEL_DRAINED;
    return 0;
}
int fes_nano_mac_channel_drained(fes_channel *c, const fes_channel_guards *g, uint64_t generation)
{
    struct observation o = begin(g, generation, 0);
    if (!c || c->phase != FES_CHANNEL_DRAINED || c->generation != generation || guard(&o))
        return fail(c, ACK);
    unsigned rst = channel_mmio(RESET) & RESETS;
    /* After independently observing drain and asserting both resets, MAC CSR
     * accesses no longer apply. The live guard still verifies the same grant. */
    if (rst != RESETS && (rst || !stopped_registers(c))) return fail(c, ACK);
    return guard(&o) ? fail(c, ACK) : 0;
}
static int retained(fes_channel *c, struct observation *o)
{
    uint8_t seen[48];
    return guard(o) || (channel_mmio(RESET) & RESETS) != RESETS ||
           arena(o, seen) || memcmp(c->arena_sha384, seen, sizeof(seen)) ||
           (channel_mmio(RESET) & RESETS) != RESETS;
}
int fes_nano_mac_channel_retained(fes_channel *c, const fes_channel_guards *g, uint64_t generation)
{
    struct observation o = begin(g, generation, 1);
    if (!c || c->phase != FES_CHANNEL_DRAINED || c->generation != generation || retained(c, &o))
        return fail(c, RETAIN);
    uint64_t start = o.last;
    /* Compare the complete reservation continuously through the dwell. A
     * second independent cap also terminates a counter stuck at one value. */
    for (unsigned n = 0; n < 2048; n++) {
        if (retained(c, &o)) return fail(c, RETAIN);
        if (o.last - start >= DWELL_US) {
            c->dwell_us = o.last - start;
            c->phase = FES_CHANNEL_RETAINED;
            return 0;
        }
    }
    return fail(c, RETAIN);
}
