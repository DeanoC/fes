/* SPDX-License-Identifier: BSD-3-Clause */
#include "mac_rx_diagnostic.h"

#define MAC 0x10810000U
#define RX_CFG 0x004U
#define FILTER 0x008U
#define RX_Q 0x0a0U
#define MAC_DEBUG 0x114U
#define Q_DMA_MAP 0x1030U
#define MTL_RX_MODE 0x1140U
#define MTL_RX_DEBUG 0x1148U
#define DMA_DEBUG 0x3020U
#define CH_CTRL 0x3100U
#define RX_CTRL 0x3108U
#define RX_BASE_HI 0x3118U
#define RX_BASE_LO 0x311cU
#define RX_TAIL 0x312cU
#define RX_LENGTH 0x3134U
#define CH_STATUS 0x3160U
#define CH_DEBUG 0x3164U
#define RX_STOPPED (1U << 8)
#define RX_BUFFER_UNAVAILABLE (1U << 7)
#define TX_STOPPED (1U << 1)
#define STOP_WAIT_US 2000000U
#define FAULTS ((1U << 9) | (1U << 12) | (1U << 13))
#define RX_BUFFER_CTRL ((4U << 16) | (FES_RX_CAPACITY << 1))
#define OWN (1U << 31)
#define CTXT (1U << 30)
#define FD_LD (3U << 28)
#define ES (1U << 15)
#define PL 0x3fffU
#define SYNC_LIMIT 1000000U
#define CANARY 0xa55a6996f00f9669ULL

static uint32_t rd(const struct fes_rx_path *p, uint32_t reg)
{
    return p->io->read(p->io->ctx, MAC + reg);
}
static int wr(const struct fes_rx_path *p, uint32_t reg, uint32_t value)
{
    if (p->guard(p->trial))
        return -1;
    p->io->sync_memory(p->io->ctx);
    p->io->write(p->io->ctx, MAC + reg, value);
    p->io->sync_memory(p->io->ctx);
    return rd(p, reg) == value ? 0 : -1;
}
static uint64_t mem(const struct fes_rx_path *p, uint32_t address)
{
    return p->io->load(p->io->ctx, address);
}
static int put(const struct fes_rx_path *p, uint32_t address, uint64_t value)
{
    if (p->guard(p->trial))
        return -1;
    p->io->store(p->io->ctx, address, value);
    p->io->sync_memory(p->io->ctx);
    return mem(p, address) == value ? 0 : -1;
}
static int memory_unchanged(const struct fes_rx_path *p)
{
    for (uint32_t offset = 0; offset < 4096; offset += 8) {
        if ((offset & 255U) == 0 && p->guard(p->trial))
            return -1;
        if ((offset < FES_RX_OFFSET || offset >= FES_RX_OFFSET + FES_RX_CAPACITY) &&
            mem(p, FES_RX_DATA + offset) != CANARY)
            return -1;
    }
    for (uint32_t offset = 16; offset < FES_RX_ENTRIES * 16; offset += 8)
        if (mem(p, FES_RX_DESC + offset) != 0)
            return -1;
    return 0;
}
/* Derive the only permitted reply from the same sealed 60 bytes the outer
 * producer transmits. No receive input or second nonce can select it. */
static int derive_reply(const uint8_t *request, uint8_t *reply)
{
    const uint8_t station[] = {0x02, 0xfe, 0x25, 0x08, 0xcb, 0x01};
    const uint8_t tag[] = "FES-CBL-TX-v1";
    unsigned source = 0, different = 0, nonce = 0;
    for (unsigned i = 0; i < 6; i++) {
        if (request[6 + i] != station[i])
            return 0;
        source |= request[i];
        different |= request[i] ^ station[i];
    }
    if (!source || !different || (request[0] & 1) || request[12] != 0x88 || request[13] != 0xb5)
        return 0;
    for (unsigned i = 0; i < sizeof(tag); i++)
        if (request[14 + i] != tag[i])
            return 0;
    for (unsigned i = 28; i < 44; i++)
        nonce |= request[i];
    for (unsigned i = 44; i < FES_RX_BYTES; i++)
        if (request[i])
            return 0;
    if (!nonce)
        return 0;
    for (unsigned i = 0; i < 6; i++) {
        reply[i] = request[6 + i];
        reply[6 + i] = request[i];
    }
    for (unsigned i = 12; i < FES_RX_BYTES; i++)
        reply[i] = request[i];
    reply[22] = 'R';
    return 1;
}
int fes_rx_arm(const struct fes_rx_path *p, struct fes_rx_result *r, const uint8_t request[FES_RX_BYTES])
{
    if (!p || !p->io || !p->guard || !r || !request || r->arm_attempted || !p->io->read || !p->io->write ||
        !p->io->store || !p->io->load || !p->io->sync_memory || !p->io->time_us)
        return -1;
    r->arm_attempted = 1;
    if (!derive_reply(request, r->expected) || p->guard(p->trial) || rd(p, RX_CFG) & 1 ||
        rd(p, RX_CTRL) & 1 || rd(p, DMA_DEBUG) & 0xf00 || rd(p, CH_STATUS) & (FAULTS | RX_STOPPED | RX_BUFFER_UNAVAILABLE) ||
        rd(p, CH_CTRL) || rd(p, 0x300) != 0x800001cb || rd(p, 0x304) != 0x0825fe02)
        return -1;
    /* The one initial descriptor can receive immediately when SR is enabled.
     * Every other ring entry is initialized, CPU-owned and never reposted. */
    for (uint32_t offset = 0; offset < FES_RX_ENTRIES * 16; offset += 8)
        if (put(p, FES_RX_DESC + offset, 0))
            return -1;
    for (uint32_t offset = 0; offset < 4096; offset += 8)
        if (put(p, FES_RX_DATA + offset, CANARY))
            return -1;
    /* Accept only station unicast, discard broadcasts; no promiscuous mode,
     * hash matches, IP checksum or jumbo frames. Strip EtherType FCS (CST). */
    if (wr(p, FILTER, 1U << 5) || wr(p, MTL_RX_MODE, (63U << 16) | (1U << 5)) || wr(p, Q_DMA_MAP, 0) ||
        wr(p, RX_Q, 2) || wr(p, RX_CFG, 1U << 2) || wr(p, RX_CTRL, RX_BUFFER_CTRL) ||
        wr(p, RX_BASE_HI, 0) || wr(p, RX_BASE_LO, FES_RX_RING_IOVA) ||
        wr(p, RX_LENGTH, FES_RX_ENTRIES - 1) || put(p, FES_RX_DESC, FES_RX_DATA_IOVA + FES_RX_OFFSET) ||
        put(p, FES_RX_DESC + 8, (uint64_t)OWN << 32))
        return -1;
    r->dma_possible = 1;
    if (wr(p, RX_CTRL, RX_BUFFER_CTRL | 1) || wr(p, RX_TAIL, FES_RX_RING_IOVA + 16) ||
        wr(p, RX_CFG, (1U << 2) | 1))
        return -1;
    r->armed = 1;
    return 0;
}
/* After OWN clears, only the device's bytes can satisfy the sealed request.
 * Check them a second time after stop and again under the outer revoked guard. */
static int payload(const struct fes_rx_path *p, const struct fes_rx_result *r, uint8_t *capture)
{
    unsigned diff = 0;
    for (unsigned i = 0; i < FES_RX_BYTES; i += 8) {
        uint64_t word = mem(p, FES_RX_DATA + FES_RX_OFFSET + i);
        for (unsigned j = 0; j < 8 && i + j < FES_RX_BYTES; j++) {
            uint8_t actual = (uint8_t)(word >> (j * 8));
            if (capture)
                capture[i + j] = actual;
            diff |= actual ^ r->expected[i + j];
        }
    }
    return diff ? -1 : 0;
}
static int retained(const struct fes_rx_path *p, const struct fes_rx_result *r)
{
    if (p->guard(p->trial))
        return -1;
    p->io->sync_memory(p->io->ctx);
    for (unsigned i = 0; i < 2; i++)
        if (mem(p, FES_RX_DESC + 8 * i) != r->retired_desc[i])
            return -1;
    for (unsigned i = 0; i < FES_RX_CAPACITY / 8; i++)
        if ((i % 32 == 0 && p->guard(p->trial)) ||
            mem(p, FES_RX_DATA + FES_RX_OFFSET + 8 * i) != r->retired_buffer[i])
            return -1;
    return memory_unchanged(p) || p->guard(p->trial) ? -1 : 0;
}
static int save_retirement(const struct fes_rx_path *p, struct fes_rx_result *r)
{
    if (p->guard(p->trial))
        return -1;
    p->io->sync_memory(p->io->ctx);
    for (unsigned i = 0; i < 2; i++)
        r->retired_desc[i] = mem(p, FES_RX_DESC + 8 * i);
    for (unsigned i = 0; i < FES_RX_CAPACITY / 8; i++) {
        if (i % 32 == 0 && p->guard(p->trial))
            return -1;
        r->retired_buffer[i] = mem(p, FES_RX_DATA + FES_RX_OFFSET + 8 * i);
    }
    if (r->matched && ((uint32_t)(r->retired_desc[1] >> 32) != r->desc_status || payload(p, r, 0)))
        return -1;
    return retained(p, r);
}
int fes_rx_retained(const struct fes_rx_path *p, const struct fes_rx_result *r)
{
    return p && r && r->stopped ? retained(p, r) : -1;
}
int fes_rx_wait(const struct fes_rx_path *p, struct fes_rx_result *r)
{
    if (!p || !r || !r->armed || r->wait_attempted)
        return -1;
    r->wait_attempted = 1;
    uint64_t first = p->io->time_us(p->io->ctx);
    for (unsigned n = 0; n < SYNC_LIMIT; n++) {
        if (p->guard(p->trial))
            return -1;
        r->channel_status = rd(p, CH_STATUS);
        if (r->channel_status & (FAULTS | RX_STOPPED))
            return -1;
        r->desc_status = (uint32_t)(mem(p, FES_RX_DESC + 8) >> 32);
        uint64_t now = p->io->time_us(p->io->ctx);
        if (now < first)
            return -1;
        if (now - first >= 2000000U) {
            if (!memory_unchanged(p) && !p->guard(p->trial))
                r->failure = FES_RX_TIMED_OUT;
            return -1;
        }
        if (r->desc_status & OWN)
            continue;
        p->io->sync_memory(p->io->ctx); /* acquire device writeback before payload */
        if (memory_unchanged(p) || p->guard(p->trial))
            return -1;
        if ((r->desc_status & (CTXT | FD_LD | ES | PL)) != (FD_LD | FES_RX_BYTES)) {
            r->failure = FES_RX_BAD_DESCRIPTOR;
            return -1;
        }
        if (payload(p, r, r->bytes)) {
            r->failure = FES_RX_WRONG_FRAME;
            return -1;
        }
        if (p->guard(p->trial))
            return -1;
        r->matched = 1;
        return 0;
    }
    return -1;
}
/* Altera HPS TRM §5.1.7.7 and RC28 DMA_CH0_Rx_Control.SR. Stop can be
 * requested after closing ingress and draining the MTL FIFO, while the DMA
 * waits for a packet or is suspended. On this one-buffer ring, newly latched
 * RBU plus a returned descriptor and no subsequent tail write also establish
 * suspension. Inspect the channel's descriptor/data state before clearing
 * SR. A generic debug register reading zero alone establishes neither state. */
static int can_clear_sr(const struct fes_rx_stop_observation *s, uint32_t descriptor)
{
    unsigned rx_state = (s->dma >> 8) & 15;
    unsigned fetch = (s->ch >> 16) & 7;
    unsigned transfer = (s->ch >> 19) & 511;
    unsigned write = (s->ch >> 28) & 7;
    if ((s->mac & 7) || (s->mtl & 0x3fff0037U) || (s->dma & 3) ||
        (fetch != 0 && fetch != 3) || transfer > 2 || write)
        return 0;
    return rx_state == 3 || rx_state == 4 ||
        (rx_state == 0 && (s->status & RX_BUFFER_UNAVAILABLE) && !(descriptor & OWN));
}
static int stop_window(const struct fes_rx_path *p, uint64_t start)
{
    if (p->guard(p->trial))
        return 0;
    uint64_t now = p->io->time_us(p->io->ctx);
    return now >= start && now - start < STOP_WAIT_US;
}
int fes_rx_stop(const struct fes_rx_path *p, struct fes_rx_result *r)
{
    if (!p || !r || !r->armed || (!r->matched && r->failure == FES_RX_NO_FAILURE) || r->stop_attempted)
        return -1;
    r->stop_attempted = 1;
    struct fes_rx_stop_observation *s = &r->stop;
    s->phase = 1;
    if (p->guard(p->trial))
        return -1;
    s->ctrl = rd(p, RX_CTRL);
    s->cfg = rd(p, RX_CFG);
    s->status = rd(p, CH_STATUS);
    if (s->ctrl != (RX_BUFFER_CTRL | 1) || s->cfg != 5 ||
        (s->status & (FAULTS | RX_STOPPED | TX_STOPPED)) != TX_STOPPED)
        return -1;
    s->phase = 2;
    if (wr(p, RX_CFG, 4)) {
        if (!p->guard(p->trial)) s->cfg = rd(p, RX_CFG);
        return -1;
    }
    s->phase = 3;
    uint64_t start = p->io->time_us(p->io->ctx);
    unsigned n;
    for (n = 0; n < SYNC_LIMIT; n++) {
        if (!stop_window(p, start))
            return -1;
        s->cfg = rd(p, RX_CFG);
        s->ctrl = rd(p, RX_CTRL);
        s->status = rd(p, CH_STATUS);
        s->mac = rd(p, MAC_DEBUG);
        s->mtl = rd(p, MTL_RX_DEBUG);
        s->dma = rd(p, DMA_DEBUG);
        s->ch = rd(p, CH_DEBUG);
        if (s->cfg != 4 || s->ctrl != (RX_BUFFER_CTRL | 1) ||
            (s->status & (FAULTS | RX_STOPPED | TX_STOPPED)) != TX_STOPPED)
            return -1;
        p->io->sync_memory(p->io->ctx);
        uint32_t descriptor = (uint32_t)(mem(p, FES_RX_DESC + 8) >> 32);
        if (descriptor != r->desc_status)
            return -1;
        if (can_clear_sr(s, descriptor))
            break;
    }
    if (n == SYNC_LIMIT)
        return -1;
    s->phase = 4;
    if (wr(p, RX_CTRL, RX_BUFFER_CTRL)) {
        if (!p->guard(p->trial)) s->post_ctrl = rd(p, RX_CTRL);
        return -1;
    }
    if (p->guard(p->trial))
        return -1;
    s->post_ctrl = rd(p, RX_CTRL);
    if (s->post_ctrl != RX_BUFFER_CTRL)
        return -1;
    s->phase = 5;
    start = p->io->time_us(p->io->ctx);
    unsigned idle_samples = 0;
    for (n = 0; n < SYNC_LIMIT; n++) {
        if (!stop_window(p, start))
            return -1;
        r->channel_status = rd(p, CH_STATUS);
        if ((r->channel_status & (FAULTS | TX_STOPPED)) != TX_STOPPED)
            return -1;
        r->ch_debug = rd(p, CH_DEBUG);
        r->dma_debug = rd(p, DMA_DEBUG);
        r->mac_debug = rd(p, MAC_DEBUG);
        r->mtl_debug = rd(p, MTL_RX_DEBUG);
        if ((r->channel_status & RX_STOPPED) && !(r->ch_debug & 0x7fff0000U) && !(r->dma_debug & 0xf03U) &&
            !(r->mac_debug & 7) && !(r->mtl_debug & 0x3fff0037U)) {
            if (idle_samples == 0) {
                if (save_retirement(p, r))
                    return -1;
                idle_samples++;
                continue;
            }
            if (retained(p, r))
                return -1;
            s->phase = 6;
            r->stopped = 1;
            return 0;
        }
        /* Once we observed a stopped engine, renewed activity is ambiguous. */
        if (idle_samples)
            return -1;
    }
    return -1;
}
