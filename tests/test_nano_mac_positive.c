/* SPDX-License-Identifier: BSD-3-Clause */
/* Build with: cc -std=c11 -Wall -Wextra -Werror -Iplatform/embedded/nano
 * tests/test_nano_mac_positive.c platform/embedded/nano/mac_tx_diagnostic.c */
#include "mac_tx_diagnostic.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>

#define M 0x10810000u
#define S 0x16000000u
#define R 0x10d11024u
#define P 0x10d12044u
#define SID 0x10d121c8u
#define CTRL 0x10d1219cu

enum fault {
    OK,
    WRONG_OWNER,
    DIRTY_QUEUE,
    BAD_ROUTE,
    WRONG_PHY,
    BAD_DESCRIPTOR,
    LOST_STOP,
    FAILED_SYNC,
    LOST_OWNER,
    LOST_ARM_ACK,
    MTL_PENDING,
    NEGATIVE_OWNER,
    RESET_RELEASED_EARLY,
    LOST_ROUTE_BEFORE_TAIL,
    LOST_SMMU_BEFORE_TAIL,
    LOST_PTE_BEFORE_OWN,
    UNKNOWN_CR2
};
struct fixture {
    uint32_t smmu[0x10100 / 4], mac[0x3800 / 4], reset, sys, ctrl, sid;
    uint64_t table[0xa000 / 8], packet[0x4000 / 8], ticks;
    unsigned writes, transmits, queued, acknowledged, revocations;
    enum fault fault;
    uint8_t wire[60];
};
static uint32_t *reg(struct fixture *f, uint32_t a)
{
    if (a >= S && a < S + sizeof(f->smmu))
        return &f->smmu[(a - S) / 4];
    if (a >= M && a < M + sizeof(f->mac))
        return &f->mac[(a - M) / 4];
    switch (a) {
    case R:
        return &f->reset;
    case P:
        return &f->sys;
    case SID:
        return &f->sid;
    case CTRL:
        return &f->ctrl;
    }
    assert(0);
    return 0;
}
static uint64_t *ram(struct fixture *f, uint32_t a)
{
    assert(!(a & 7));
    if (a >= FES_TX_TABLES && a < FES_TX_TABLES + sizeof(f->table))
        return &f->table[(a - FES_TX_TABLES) / 8];
    if (a >= FES_TX_DESC && a < FES_TX_DESC + sizeof(f->packet))
        return &f->packet[(a - FES_TX_DESC) / 8];
    assert(0);
    return 0;
}
static uint64_t get(void *ctx, uint32_t a)
{
    struct fixture *f = ctx;
    if (a >= S && a < S + 0xb0)
        return ((uint64_t)*reg(f, a + 4) << 32) | *reg(f, a);
    return *ram(f, a);
}
static void put(void *ctx, uint32_t a, uint64_t v)
{
    struct fixture *f = ctx;
    if (a >= S && a < S + 0xb0) {
        *reg(f, a) = (uint32_t)v;
        *reg(f, a + 4) = v >> 32;
    } else
        *ram(f, a) = v;
}
static uint32_t read32(void *ctx, uint32_t a)
{
    return *reg(ctx, a);
}
static uint32_t translate(struct fixture *f, uint32_t va, unsigned write)
{
    assert(*reg(f, S + 0x24) == 13 && f->ctrl == 3 && f->sid == 0x10001);
    uint32_t ste = *reg(f, S + 0x80) + 64;
    assert(get(f, ste) == 13);
    uint32_t root = (uint32_t)get(f, ste + 24);
    uint64_t l1 = get(f, root + 8 * ((va >> 30) & 511));
    assert((l1 & 3) == 3);
    uint64_t l2 = get(f, (l1 & ~0xfffu) + 8 * ((va >> 21) & 511));
    assert((l2 & 3) == 3);
    uint64_t page = get(f, (l2 & ~0xfffu) + 8 * ((va >> 12) & 511));
    assert((page & 3) == 3);
    if (write)
        assert((page >> 6 & 3) == 3);
    else
        assert((page >> 6) & 1);
    return ((uint32_t)page & ~0xfffu) | (va & 0xfff);
}
static void write32(void *ctx, uint32_t a, uint32_t v)
{
    struct fixture *f = ctx;
    f->writes++;
    uint32_t *p = reg(f, a), old = *p;
    *p = v;
    if (a == S + 0x44) {
        *p = v & ~(1U << 31);
        if (f->fault == RESET_RELEASED_EARLY)
            f->reset &= ~1U;
    }
    if (a == S + 0x20) {
        *reg(f, S + 0x24) = f->fault == LOST_ARM_ACK && v == 13 ? 12 : v;
    }
    if (a == S + 0x98) {
        unsigned cons = *reg(f, S + 0x9c);
        uint64_t cmd = get(f, FES_TX_CMDQ + 16 * (cons & 7));
        assert(get(f, FES_TX_CMDQ + 16 * ((cons + 1) & 7)) == 0xfc00046);
        if (cmd == (3ULL | (1ULL << 32))) {
            f->revocations++;
            assert(get(f, FES_TX_TABLES + 64) < 2);
        }
        if (f->fault != FAILED_SYNC || !f->transmits) {
            *reg(f, S + 0x9c) = v;
            f->acknowledged++;
        }
        f->queued++;
    }
    if (a == M + 0x3000)
        *p = 0;
    if (a == M + 0x204) {
        unsigned addr = *reg(f, M + 0x200);
        assert((addr >> 16) == 1);
        const uint16_t phy[] = {0x1000, 0x24, 0x22, 0x1622, 0, 0, 0, 0, 0, 1U << 9, 1U << 11};
        assert((addr & 31) < sizeof(phy) / sizeof(*phy));
        *p = phy[addr & 31];
        if ((addr & 31) == 3 && f->fault == WRONG_PHY)
            ++*p;
    }
    if (a == M && (v & 1)) {
        if (f->fault == LOST_ROUTE_BEFORE_TAIL)
            f->sid = 2;
        if (f->fault == LOST_SMMU_BEFORE_TAIL)
            *reg(f, S + 0x24) = 0;
    }
    if (a == M + 0x3160 && f->fault == LOST_PTE_BEFORE_OWN)
        *ram(f, FES_TX_TABLES + 0x6000 + 8 * ((FES_TX_BUFFER >> 12) & 511)) = 0;
    if (a == M + 0x3160)
        *p = old & ~v;
    if (a == M + 0x3104 && (old & 1) && !(v & 1) && f->fault != LOST_STOP)
        *reg(f, M + 0x3160) |= 2;
    if (a == M + 0x3124) {
        f->transmits++;
        if (f->fault == MTL_PENDING)
            *reg(f, M + 0x1108) = 1U << 5;
        assert(*reg(f, M + 0x3104) & 1);
        assert(*reg(f, M) & 1);
        assert(!(*reg(f, M + 4) & 1));
        assert(v == FES_TX_IOVA + 16 && *reg(f, M + 0x3130) == 31);
        assert(!get(f, FES_TX_IOVA) && !get(f, FES_TX_IOVA + 8));
        uint32_t desc = translate(f, *reg(f, M + 0x3114), 1);
        assert(desc == FES_TX_DESC);
        uint32_t data = translate(f, (uint32_t)get(f, desc), 0);
        assert(data == FES_TX_DATA && (uint32_t)get(f, desc + 8) == 60);
        assert((get(f, desc + 8) >> 60 & 0xb) == 0xb);
        memcpy(f->wire, ram(f, data), 60);
        *ram(f, desc + 8) &= ~(1ULL << 63);
        if (f->fault == BAD_DESCRIPTOR)
            *ram(f, desc + 8) |= 1ULL << 47;
    }
}
static int owns(void *ctx)
{
    struct fixture *f = ctx;
    if (f->fault == NEGATIVE_OWNER)
        return -1;
    return f->fault != WRONG_OWNER && !(f->fault == LOST_OWNER && f->transmits);
}
static uint64_t now(void *ctx)
{
    return ++((struct fixture *)ctx)->ticks;
}
static void barrier(void *ctx)
{
    (void)ctx;
}
static void test(enum fault fault)
{
    struct fixture f = {0};
    f.fault = fault;
    f.reset = 0xff7fbfff;
    f.ctrl = 0x30;
    *reg(&f, S) = 0x080f7e3f;
    *reg(&f, S + 4) = 0x0e739d18;
    *reg(&f, S + 0x14) = 0x400075;
    *reg(&f, S + 0x2c) = 4;
    *reg(&f, M + 0x110) = 0x107631;
    *reg(&f, M + 0x11c) = 0xca7d5b3f;
    *reg(&f, M + 0x120) = 0x49ef7a27;
    *reg(&f, M + 0x124) = 0x221c71c7;
    if (fault == DIRTY_QUEUE)
        *reg(&f, S + 0x100a8) = 1;
    if (fault == UNKNOWN_CR2)
        *reg(&f, S + 0x2c) = 0;
    if (fault == BAD_ROUTE)
        f.sid = 9;
    struct fes_tx_io io = {&f, read32, write32, get, put, barrier, now, owns};
    struct fes_tx_result out = {0};
    uint8_t frame[60] = {0x02, 0x00, 0x00, 0x00, 0x00, 0x45, 2, 0xfe, 0x25, 8, 0xcb, 1, 0x88, 0xb5};
    memcpy(frame + 14, "FES-CBL-TX-v1", 14);
    memset(frame + 28, 0x7e, 16);
    int rc = fes_tx_once(&io, frame, &out);
    assert((rc == 0) == (fault == OK));
    assert(f.transmits <= 1);
    if (fault == OK) {
        assert(!memcmp(frame, f.wire, 60) && out.mapped && out.dma_possible && out.dispatched &&
               out.process_stopped);
        assert(out.stage == FES_TX_PARKED && out.translation_revoked && out.mac_in_reset &&
               f.revocations == 2);
        assert(f.queued == 6 && f.acknowledged == 6 && get(&f, FES_TX_TABLES + 64) == 1);
        for (unsigned i = 0; i < 256; i++)
            assert(get(&f, FES_TX_TABLES + 64 * i) == 1);
        for (unsigned i = 0; i < 1024; i++)
            assert(!get(&f, FES_TX_IOVA + i * 8));
    }
    if (fault == DIRTY_QUEUE || fault == WRONG_OWNER || fault == NEGATIVE_OWNER ||
        fault == BAD_ROUTE)
        assert(!f.writes && !f.transmits);
    if (fault == WRONG_PHY)
        assert(!f.transmits && !out.dma_possible && out.translation_revoked && out.mac_in_reset);
    if (fault == BAD_DESCRIPTOR || fault == LOST_STOP || fault == MTL_PENDING)
        assert(f.transmits && !out.process_stopped && out.translation_revoked && out.mac_in_reset);
    if (fault == FAILED_SYNC || fault == LOST_OWNER)
        assert(f.transmits && !out.translation_revoked && !out.mac_in_reset);
    if (fault == RESET_RELEASED_EARLY)
        assert(!out.arm_attempted && !out.mac_release_attempted && !out.dispatched);
    if (fault == LOST_ARM_ACK)
        assert(out.arm_attempted && !out.mapped && !out.mac_release_attempted && !out.dispatched);
    if (fault == LOST_ROUTE_BEFORE_TAIL || fault == LOST_SMMU_BEFORE_TAIL ||
        fault == LOST_PTE_BEFORE_OWN)
        assert(!f.transmits && !out.dispatched && !out.translation_revoked);
    enum fes_tx_disposition expected = FES_TX_COLD_RECOVERY;
    if (fault == OK)
        expected = FES_TX_CLOSED;
    if (fault == UNKNOWN_CR2)
        expected = FES_TX_UNCHANGED;
    if (fault == WRONG_PHY)
        expected = FES_TX_REVOKED_UNDISPATCHED;
    unsigned writes = f.writes;
    assert(fes_tx_terminal(&io, &out, rc) == expected);
    assert(f.writes == writes);
    assert(fes_tx_once(&io, frame, &out));
    assert(f.writes == writes);
}
int main(void)
{
    for (enum fault i = OK; i <= UNKNOWN_CR2; i++)
        test(i);
    puts("CBL TX domain/translation, frame and retirement fault checks passed");
}
