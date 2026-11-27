/* SPDX-License-Identifier: BSD-3-Clause */
#include <assert.h>
#include <string.h>
#include "mac_rx_diagnostic.h"

#define BASE 0x10810000U
#define OWN (1U << 31)
#define DONE ((3U << 28) | 60U)
#define RPS (1U << 8)

struct model {
    uint32_t regs[0x3200 / 4];
    uint64_t desc[FES_RX_ENTRIES * 2], data[512];
    unsigned cycles, cutoff, stores, revoked, published, ingress, skip_rps, sticky_debug;
    unsigned pending_sync;
};
static int guard(void *ctx)
{
    struct model *m = ctx;
    if (++m->cycles > m->cutoff)
        m->revoked = 1;
    return m->revoked ? -1 : 0;
}
static uint32_t read_reg(void *ctx, uint32_t a)
{
    struct model *m = ctx;
    assert(!m->revoked && a >= BASE && a < BASE + sizeof(m->regs));
    return m->regs[(a - BASE) / 4];
}
static void write_reg(void *ctx, uint32_t a, uint32_t v)
{
    struct model *m = ctx;
    assert(!m->revoked && !m->pending_sync && a >= BASE && a < BASE + sizeof(m->regs));
    unsigned offset = a - BASE;
    uint32_t prior = m->regs[offset / 4];
    m->regs[offset / 4] = v;
    if (offset == 0x3108 && (v & 1)) {
        assert(m->regs[0x3118 / 4] == 0 && m->regs[0x311c / 4] == FES_RX_RING_IOVA);
        assert(m->regs[0x3134 / 4] == 31 && v == ((4U << 16) | 0xc81U));
        assert(m->desc[0] == FES_RX_DATA_IOVA + FES_RX_OFFSET && m->desc[1] == (uint64_t)OWN << 32);
        for (unsigned i = 2; i < FES_RX_ENTRIES * 2; i++)
            assert(m->desc[i] == 0);
        m->regs[0x3020 / 4] = 3U << 8;
    }
    if (offset == 0x312c) {
        assert(!m->published && v == FES_RX_RING_IOVA + 16);
        m->published = 1;
    }
    if (offset == 4 && (v & 1)) {
        assert(m->published && v == 5 && m->regs[8 / 4] == (1U << 5));
        assert(m->regs[0x1140 / 4] == 0x003f0020 && m->regs[0xa0 / 4] == 2);
        m->ingress = 1;
    }
    if (offset == 0x3108 && !(v & 1) && (prior & 1)) {
        assert(m->regs[4 / 4] == 4 && !m->regs[0x1148 / 4]);
        if (!m->skip_rps)
            m->regs[0x3160 / 4] |= RPS;
        m->regs[0x3020 / 4] = m->sticky_debug;
    }
}
static uint64_t *word(struct model *m, uint32_t a)
{
    assert(!m->revoked && (a % 8) == 0);
    if (a >= FES_RX_DESC && a < FES_RX_DESC + sizeof(m->desc))
        return &m->desc[(a - FES_RX_DESC) / 8];
    assert(a >= FES_RX_DATA && a < FES_RX_DATA + sizeof(m->data));
    return &m->data[(a - FES_RX_DATA) / 8];
}
static uint64_t load(void *ctx, uint32_t a)
{
    return *word(ctx, a);
}
static void store(void *ctx, uint32_t a, uint64_t v)
{
    struct model *m = ctx;
    assert(!m->ingress && !m->pending_sync);
    *word(m, a) = v;
    m->stores++;
    m->pending_sync = 1;
}
static uint64_t clock_us(void *ctx)
{
    return ((struct model *)ctx)->cycles * 5000ULL;
}
static void barrier(void *ctx)
{
    ((struct model *)ctx)->pending_sync = 0;
}
static void init(struct model *m)
{
    memset(m, 0, sizeof(*m));
    m->cutoff = 2000;
    m->regs[0x300 / 4] = 0x800001cb;
    m->regs[0x304 / 4] = 0x0825fe02;
    memset(m->desc, 0xb2, sizeof(m->desc));
}
static const uint8_t reply[60] = {
    2,   0xfe, 0x25, 8,   0xcb, 1, 2, 3, 4, 5, 6, 7, 0x88, 0xb5, 'F', 'E', 'S', '-', 'C', 'B', 'L', '-',
    'R', 'X',  '-',  'v', '1',  0, 1, 2, 3, 4, 5, 6, 7,    8,    9,   10,  11,  12,  13,  14,  15,  16,
};
static void receive(struct model *m, uint32_t desc)
{
    assert(m->ingress && m->desc[1] == (uint64_t)OWN << 32);
    for (unsigned i = 0; i < sizeof(reply); i++) {
        uint64_t *q = &m->data[(FES_RX_OFFSET + i) / 8], shift = (i % 8) * 8;
        *q = (*q & ~(255ULL << shift)) | (uint64_t)reply[i] << shift;
    }
    m->desc[1] = (uint64_t)desc << 32;
    m->regs[0x3020 / 4] = 4 << 8; /* one buffer spent: suspended */
}
int main(void)
{
    struct model m;
    const struct fes_tx_io io = {.ctx = &m,
                                 .read = read_reg,
                                 .write = write_reg,
                                 .load = load,
                                 .store = store,
                                 .sync_memory = barrier,
                                 .time_us = clock_us};
    const struct fes_rx_path p = {.io = &io, .trial = &m, .guard = guard};
    struct fes_rx_result r;
    uint8_t request[60];
    memcpy(request, reply, sizeof(request));
    memcpy(request, reply + 6, 6);
    memcpy(request + 6, reply, 6);
    request[22] = 'T';
#define RESET()                                                                                            \
    do {                                                                                                   \
        init(&m);                                                                                          \
        memset(&r, 0, sizeof(r));                                                                          \
    } while (0)
#define ARM()                                                                                              \
    do {                                                                                                   \
        RESET();                                                                                           \
        assert(!fes_rx_arm(&p, &r, request));                                                              \
        assert(r.armed && r.dma_possible); m.regs[0x3160 / 4] |= 2U;                                                                  \
    } while (0)
#define MATCH()                                                                                            \
    do {                                                                                                   \
        ARM();                                                                                             \
        receive(&m, DONE);                                                                                 \
        assert(!fes_rx_wait(&p, &r));                                                                      \
    } while (0)
    MATCH();
    assert(!memcmp(r.bytes, reply, 60) && !fes_rx_stop(&p, &r) && r.stopped);
    assert(fes_rx_arm(&p, &r, request) && fes_rx_wait(&p, &r) && fes_rx_stop(&p, &r));
    assert(!fes_rx_retained(&p, &r));
    m.data[FES_RX_OFFSET / 8]++;
    assert(fes_rx_retained(&p, &r));
    MATCH();
    m.desc[1] ^= 1ULL << 32;
    assert(fes_rx_stop(&p, &r) && !r.stopped);
    MATCH();
    m.data[FES_RX_OFFSET / 8]++;
    assert(fes_rx_stop(&p, &r) && !r.stopped);
    ARM();
    assert(fes_rx_wait(&p, &r) && r.failure == FES_RX_TIMED_OUT && !r.matched);
    assert(!fes_rx_stop(&p, &r) && r.stopped && !r.matched && !fes_rx_retained(&p, &r));
    m.data[FES_RX_OFFSET / 8 + 191] ^= 1;
    assert(fes_rx_retained(&p, &r));
    ARM();
    receive(&m, DONE + 4);
    assert(fes_rx_wait(&p, &r) && r.failure == FES_RX_BAD_DESCRIPTOR);
    assert(!fes_rx_stop(&p, &r) && !r.matched && !fes_rx_retained(&p, &r));
    m.desc[0] ^= 1;
    assert(fes_rx_retained(&p, &r));
    ARM();
    receive(&m, DONE);
    m.data[FES_RX_OFFSET / 8] ^= 4;
    assert(fes_rx_wait(&p, &r) && r.failure == FES_RX_WRONG_FRAME);
    assert(!fes_rx_stop(&p, &r) && !r.matched && !fes_rx_retained(&p, &r));
    ARM();
    receive(&m, DONE + 4);
    assert(fes_rx_wait(&p, &r));
    m.data[2]++;
    assert(fes_rx_stop(&p, &r) && !r.stopped);
    ARM();
    m.cutoff = m.cycles;
    assert(fes_rx_wait(&p, &r) && !r.failure);
    assert(fes_rx_stop(&p, &r) && !r.stopped);
    const uint32_t bad[] = {DONE ^ (1U << 29), DONE ^ (1U << 28), DONE | (1U << 30), DONE | (1U << 15),
                            DONE + 4,          DONE - 1,          DONE | OWN};
    for (unsigned i = 0; i < sizeof(bad) / sizeof(bad[0]); i++) {
        ARM();
        receive(&m, bad[i]);
        assert(fes_rx_wait(&p, &r) && !r.matched);
        m.desc[1] = (uint64_t)DONE << 32;
        assert(fes_rx_wait(&p, &r));
    }
    for (unsigned i = 0; i < 60; i++) {
        ARM();
        receive(&m, DONE);
        m.data[(FES_RX_OFFSET + i) / 8] ^= 1ULL << (8 * (i % 8));
        assert(fes_rx_wait(&p, &r) && !r.matched);
    }
    ARM();
    receive(&m, DONE);
    m.data[FES_RX_OFFSET / 8 - 1]++;
    assert(fes_rx_wait(&p, &r));
    ARM();
    receive(&m, DONE);
    m.data[(FES_RX_OFFSET + FES_RX_CAPACITY) / 8]++;
    assert(fes_rx_wait(&p, &r));
    ARM();
    receive(&m, DONE);
    m.desc[2]++;
    assert(fes_rx_wait(&p, &r));
    ARM();
    m.regs[0x3160 / 4] = 1U << 12;
    assert(fes_rx_wait(&p, &r));
    ARM();
    m.cutoff = m.cycles;
    assert(fes_rx_wait(&p, &r) && m.revoked);
    RESET();
    m.cutoff = 0;
    assert(fes_rx_arm(&p, &r, request) && !m.stores && !r.dma_possible);
    RESET();
    m.cutoff = 32;
    assert(fes_rx_arm(&p, &r, request) && m.stores && !r.dma_possible && !m.ingress);
    RESET();
    m.regs[0x3160 / 4] = RPS;
    assert(fes_rx_arm(&p, &r, request) && !m.stores);
    RESET();
    m.regs[0x3160 / 4] = 0x80; /* Without observed-clear RBU is stale. */
    assert(fes_rx_arm(&p, &r, request) && !m.stores);
    MATCH();
    m.regs[0x3020 / 4] = 0;
    m.regs[0x3160 / 4] = 0x86; /* Actual CBL TX/TBU/RBU stop sample. */
    assert(!fes_rx_stop(&p, &r) && r.stop.phase == 6 && r.stop.status == 0x86);
    assert(r.stop.cfg == 4 && r.stop.ctrl == 0x40c81 && r.stop.post_ctrl == 0x40c80);
    assert((r.channel_status & 0x102) == 0x102 && !r.stop.shared);
    MATCH();
    m.regs[0x3020 / 4] = 0;
    assert(fes_rx_stop(&p, &r) && r.stop.phase == 3 && !r.stop.post_ctrl);
    for (unsigned i = 0; i < 4; i++) {
        const uint32_t busy[] = {4U << 16, 5U << 19, 1U << 28, 2U << 16};
        MATCH();
        m.regs[0x3160 / 4] = 0x86;
        m.regs[0x3020 / 4] = 0;
        m.regs[0x3164 / 4] = busy[i];
        assert(fes_rx_stop(&p, &r) && r.stop.phase == 3 && r.stop.ch == busy[i]);
        assert(!m.revoked && (m.regs[0x3108 / 4] & 1)); /* bounded wait preserves the outer grant */
    }
    ARM();
    assert(fes_rx_wait(&p, &r) && r.failure == FES_RX_TIMED_OUT);
    m.regs[0x3020 / 4] = 0;
    m.regs[0x3160 / 4] = 0x86;
    assert(fes_rx_stop(&p, &r) && r.stop.phase == 3 && !r.stop.post_ctrl);
    MATCH();
    m.regs[0x3160 / 4] = 0x84; /* Missing independent TX acknowledgement. */
    assert(fes_rx_stop(&p, &r) && r.stop.phase == 1);
    MATCH();
    m.skip_rps = 1;
    assert(fes_rx_stop(&p, &r) && !r.stopped);
    MATCH();
    m.sticky_debug = 1;
    assert(fes_rx_stop(&p, &r) && !r.stopped);
    MATCH();
    m.regs[0x3164 / 4] = 1U << 28;
    assert(fes_rx_stop(&p, &r) && !r.stopped);
    MATCH();
    m.regs[0x1148 / 4] = 1U << 16;
    assert(fes_rx_stop(&p, &r) && !r.stopped);
    MATCH();
    m.regs[0x114 / 4] = 1;
    assert(fes_rx_stop(&p, &r) && !r.stopped);
    MATCH();
    m.regs[0x3160 / 4] = RPS;
    assert(fes_rx_stop(&p, &r) && !r.stopped);
    MATCH();
    m.data[0]++;
    assert(fes_rx_stop(&p, &r) && !r.stopped);
    MATCH();
    m.regs[0x3020 / 4] = 5 << 8;
    assert(fes_rx_stop(&p, &r) && !r.stopped);
    MATCH();
    m.cutoff = m.cycles;
    assert(fes_rx_stop(&p, &r) && !r.stopped && m.revoked);
    RESET();
    request[22] = 'R';
    assert(fes_rx_arm(&p, &r, request) && !m.stores && !r.dma_possible);
    RESET();
    request[22] = 'T';
    memset(request + 28, 0, 16);
    assert(fes_rx_arm(&p, &r, request) && !m.stores);
    return 0;
}
