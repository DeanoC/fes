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
    SLOW_PHY,
    VENDOR_PHY_FLAG,
    FIFTEEN_SECOND_NEGOTIATION,
    FLP_ALREADY_CENTERED,
    FLP_UNDOCUMENTED,
    FLP_LO_WRITE_LOST,
    FLP_RESTORE_LOST,
    FLP_RESTORE_FOREIGN_PHY,
    TRAINING_IDLE_ERRORS,
    NO_CLEANUP_BUDGET,
    PCS_DOWN,
    BAD_LOCAL_RECEIVER,
    IDLE_ERRORS_SATURATED,
    TX_TIMEOUT,
    AXI_STILL_ACTIVE,
    LATE_BUS_FAULT,
    BAD_DESCRIPTOR,
    CONTEXT_DESCRIPTOR,
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
    OPTIONAL_FEATURES,
    WRONG_VERSION,
    WRONG_MODE,
    NO_RGMII,
    NO_GMII,
    NO_MDIO,
    NO_AXI_DMA,
    SMALL_FIFO,
    WRONG_ADDRESS_WIDTH,
    TOO_MANY_CHANNELS,
    WRONG_QUEUES,
    UNKNOWN_CR2
};
struct fixture {
    uint32_t smmu[0x10100 / 4], mac[0x3800 / 4], reset, sys, ctrl, sid;
    uint64_t table[0xa000 / 8], packet[0x6000 / 8], ticks;
    uint16_t selector, offsets[32], timing[5];
    unsigned timing_writes, external_writes, restarts, training_samples;
    unsigned writes, transmits, queued, acknowledged, revocations, observations;
    enum fault fault;
    uint8_t wire[60];
    unsigned caps_seen, phy_reports, drain_reads, partner_reads;
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
    struct fixture *f=ctx;
    if (f->fault == LATE_BUS_FAULT && a==M+0x3020 && ++f->drain_reads==2)
        *reg(f,M+0x3160) |= 1U<<12;
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
static void initial_phy(struct fixture *f)
{
    f->selector=0x8002; /* pre-existing post-increment mode, foreign device */
    f->offsets[2]=17;
    f->offsets[0]=23;
    f->timing[3]=0x4000;
    f->timing[4]=3;
    if (f->fault==FLP_ALREADY_CENTERED) { f->timing[3]=0x1a80; f->timing[4]=6; }
    if (f->fault==FLP_UNDOCUMENTED) f->timing[3]=0xbeef;
}
static void phy_write(struct fixture *f,unsigned number,uint16_t value)
{
    f->external_writes++;
    assert(!(*reg(f,M+4)&1) && !(*reg(f,M+0x3104)&1));
    if (number==0) {
        assert(value==0x1200);
        f->restarts++;
    } else if (number==13) {
        f->selector=value;
    } else {
        assert(number==14);
        unsigned dev=f->selector&31;
        if (!(f->selector>>14)) {
            f->offsets[dev]=value;
        } else {
            unsigned offset=f->offsets[dev];
            assert(f->selector==0x4000 && (offset==3 || offset==4));
            f->timing_writes++;
            if ((f->fault==FLP_LO_WRITE_LOST && value==0x1a80) ||
                (f->fault==FLP_RESTORE_LOST && value==0x4000)) return;
            f->timing[offset]=value;
        }
    }
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
        const uint16_t phy[32] = {[0]=0x1000, [1]=0x24, [2]=0x22, [3]=0x1622,
                                  [4]=0x1e1, [5]=0xcde1, [9]=1U<<9, [10]=0x3c00, [19]=0x0806, [31]=0x0348};
        assert((addr & 31) < sizeof(phy) / sizeof(*phy));
        unsigned number=addr&31;
        if (((v>>16)&3)==1) {
            phy_write(f,number,(uint16_t)v);
            *p=0;
            return;
        }
        assert(((v>>16)&3)==3);
        *p = phy[number];
        if (number==13) *p=f->selector;
        if (number==14) {
            unsigned dev=f->selector&31, off=f->offsets[dev];
            if (f->selector>>14) { assert(f->selector==0x4000 && (off==3 || off==4)); *p=f->timing[off]; }
            else *p=(uint16_t)off;
        }
        if (number==3 && f->fault==FLP_RESTORE_FOREIGN_PHY && f->transmits) ++*p;
        if ((addr & 31) == 1 && f->fault == FIFTEEN_SECOND_NEGOTIATION && f->ticks<15000000) *p=0;
        if ((addr & 31) == 31 && f->fault == SLOW_PHY) *p=0x28;
        if ((addr & 31) == 31 && f->fault == VENDOR_PHY_FLAG) *p=0x49;
        if ((addr & 31) == 19 && f->fault == PCS_DOWN) *p=0x0802;
        if ((addr & 31) == 10 && f->fault == BAD_LOCAL_RECEIVER) *p=0x1c00;
        if ((addr & 31) == 10 && f->fault == TRAINING_IDLE_ERRORS) {
            ++f->partner_reads;
            if (f->partner_reads == 1) *p = 0x0c00;
            if (f->partner_reads == 2) *p = 0x2cff;
        }
        if ((addr & 31) == 10 && f->fault == IDLE_ERRORS_SATURATED) *p|=0xff;
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
        if (f->fault==TRAINING_IDLE_ERRORS) assert(f->partner_reads>=4);
        if (f->fault==FIFTEEN_SECOND_NEGOTIATION) assert(f->ticks>=15100000);
        if (f->fault == MTL_PENDING)
            *reg(f, M + 0x1108) = 1U << 5;
        assert(*reg(f, M + 0x3104) & 1);
        assert((*reg(f, P) & 3) == 1 && (*reg(f, M) >> 29) == 3);
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
        if (f->fault != TX_TIMEOUT) *ram(f, desc + 8) &= ~(1ULL << 63);
        if (f->fault == AXI_STILL_ACTIVE) *reg(f, M+0x3020)=1;
        if (f->fault == BAD_DESCRIPTOR)
            *ram(f, desc + 8) |= 1ULL << (32 + 27);
        if (f->fault == CONTEXT_DESCRIPTOR)
            *ram(f, desc + 8) |= 1ULL << (32 + 30);
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
    struct fixture *f=ctx;
    return f->ticks += (!f->transmits || f->fault==TX_TIMEOUT) ? 16 : 1;
}
static void barrier(void *ctx)
{
    (void)ctx;
}
static void report_caps(void *ctx, const struct fes_tx_caps *caps)
{
    struct fixture *f = ctx;
    assert(++f->caps_seen == 1 && !(f->reset & 0x101) && !f->transmits);
    assert(caps->version == *reg(f, M+0x110) && caps->feature0 == *reg(f, M+0x11c));
    assert(caps->feature1 == *reg(f, M+0x120) && caps->feature2 == *reg(f,M+0x124));
}
static void report_phy(void *ctx,unsigned sample,const struct fes_tx_phy_sample *p)
{
    struct fixture *f=ctx;
    if (sample>=2) { assert(sample == 2+f->training_samples++ && !f->transmits); return; }
    assert(sample==f->phy_reports++ && sample<2 && !f->transmits && !(*reg(f,M+0x3104)&1));
    assert(p->time_us);
    if (sample==1 && f->fault==NO_CLEANUP_BUDGET) f->ticks+=31000000;
}
static int observe(void *ctx)
{
    struct fixture *f = ctx;
    f->observations++;
    return 1;
}
static int change_on_second_observation(void *ctx)
{
    struct fixture *f = ctx;
    if (++f->observations == 2)
        f->sid = 1;
    return 1;
}
static void denied_startup(void)
{
    struct fixture f = {.fault = WRONG_OWNER, .reset = 0xff7fbfff, .ctrl = 0x30};
    *reg(&f, S) = 0x080f7e3f;
    *reg(&f, S + 4) = 0x0e739d18;
    *reg(&f, S + 0x14) = 0x400075;
    struct fes_tx_io io = {&f, read32, write32, get, put, barrier, now, owns, observe, report_caps, .report_phy=report_phy};
    struct fes_tx_result out = {0};
    assert(!owns(&f) && fes_tx_terminal(&io, &out, -1) == FES_TX_UNCHANGED);
    assert(f.observations == 2 && !f.writes && !f.transmits);
    f.observations = 0;
    io.observe_unchanged = change_on_second_observation;
    assert(fes_tx_terminal(&io, &out, -1) == FES_TX_COLD_RECOVERY);
    assert(f.observations == 2 && !f.writes);
    out.write_attempted = 1;
    io.observe_unchanged = observe;
    f.observations = 0;
    assert(fes_tx_terminal(&io, &out, -1) == FES_TX_COLD_RECOVERY && !f.observations);
}
static void test(enum fault fault)
{
    struct fixture f = {0};
    f.fault = fault;
    initial_phy(&f);
    f.reset = 0xff7fbfff;
    f.ctrl = 0x30;
    *reg(&f, S) = 0x080f7e3f;
    *reg(&f, S + 4) = 0x0e739d18;
    *reg(&f, S + 0x14) = 0x400075;
    *reg(&f, S + 0x2c) = 4;
    *reg(&f, M + 0x110) = 0x107631;
    *reg(&f, M + 0x11c) = 0xcafd5b3f;
    *reg(&f, M + 0x120) = 0x49ef7a27;
    *reg(&f, M + 0x124) = 0x221c71c7;
    if (fault == OPTIONAL_FEATURES) {
        *reg(&f, M+0x11c) ^= (1U<<30) | (1U<<13);
        *reg(&f, M+0x120) ^= (1U<<18) | (1U<<20);
        *reg(&f, M+0x124) ^= 1U<<24;
    }
    if (fault == WRONG_VERSION) *reg(&f, M+0x110) ^= 1;
    if (fault == WRONG_MODE) *reg(&f, M+0x11c) ^= 1U<<23;
    if (fault == NO_RGMII) *reg(&f, M+0x11c) &= ~1U;
    if (fault == NO_GMII) *reg(&f, M+0x11c) &= ~2U;
    if (fault == NO_MDIO) *reg(&f, M+0x11c) &= ~(1U<<5);
    if (fault == NO_AXI_DMA) *reg(&f, M+0x11c) &= ~(1U<<31);
    if (fault == SMALL_FIFO) *reg(&f, M+0x120) ^= 15U<<6;
    if (fault == WRONG_ADDRESS_WIDTH) *reg(&f, M+0x120) ^= 1U<<14;
    if (fault == TOO_MANY_CHANNELS) *reg(&f, M+0x124) |= 8U<<18;
    if (fault == WRONG_QUEUES) *reg(&f, M+0x124) ^= 1U<<6;
    if (fault == DIRTY_QUEUE)
        *reg(&f, S + 0x100a8) = 1;
    if (fault == UNKNOWN_CR2)
        *reg(&f, S + 0x2c) = 0;
    if (fault == BAD_ROUTE)
        f.sid = 9;
    struct fes_tx_io io = {&f, read32, write32, get, put, barrier, now, owns, owns, report_caps, .report_phy=report_phy};
    struct fes_tx_result out = {0};
    uint8_t frame[60] = {0x02, 0x00, 0x00, 0x00, 0x00, 0x45, 2, 0xfe, 0x25, 8, 0xcb, 1, 0x88, 0xb5};
    memcpy(frame + 14, "FES-CBL-TX-v1", 14);
    memset(frame + 28, 0x7e, 16);
    int rc = fes_tx_once(&io, frame, &out);
    int success = fault == OK || fault == FLP_ALREADY_CENTERED || fault == OPTIONAL_FEATURES || fault == VENDOR_PHY_FLAG ||
                  fault == FIFTEEN_SECOND_NEGOTIATION || fault == TRAINING_IDLE_ERRORS;
    assert((rc == 0) == success);
    assert(f.transmits <= 1);
    if (success) {
        assert(out.phy.snapshot && out.phy.applied && out.phy.restart && out.phy.restored);
        assert(f.selector==0x8002 && f.offsets[0]==23 && f.offsets[2]==17);
        assert(f.timing_writes == (fault==FLP_ALREADY_CENTERED ? 0 : 4));
        assert(f.restarts == (fault==FLP_ALREADY_CENTERED ? 1 : 2));
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
    if (fault == WRONG_PHY || fault == SLOW_PHY || fault == PCS_DOWN || fault == BAD_LOCAL_RECEIVER || fault == IDLE_ERRORS_SATURATED)
        assert(!f.transmits && !out.dma_possible && out.translation_revoked && out.mac_in_reset);
    if (fault == LOST_STOP || fault == MTL_PENDING || fault == AXI_STILL_ACTIVE || fault == LATE_BUS_FAULT)
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
    if (success)
        expected = FES_TX_CLOSED;
    if (fault >= WRONG_VERSION && fault <= WRONG_QUEUES) {
        assert(f.caps_seen == 1 && !out.dispatched && !out.dma_possible);
        assert(out.fault_address == M + (fault == WRONG_VERSION ? 0x110 :
               fault <= NO_AXI_DMA ? 0x11c : fault <= WRONG_ADDRESS_WIDTH ? 0x120 : 0x124));
        expected = FES_TX_REVOKED_UNDISPATCHED;
    }
    if (fault == BAD_DESCRIPTOR || fault == CONTEXT_DESCRIPTOR || fault == TX_TIMEOUT) {
        assert(out.dispatched && out.process_stopped && out.translation_revoked && out.mac_in_reset);
        expected = FES_TX_FAILED_DRAINED;
    }
    if (fault == NO_CLEANUP_BUDGET) {
        assert(!f.transmits && !out.dma_possible && out.process_stopped==0);
        expected = FES_TX_REVOKED_UNDISPATCHED;
    }
    if (fault == UNKNOWN_CR2)
        expected = FES_TX_UNCHANGED;
    if (fault == WRONG_PHY || fault == SLOW_PHY || fault == PCS_DOWN || fault == BAD_LOCAL_RECEIVER || fault == IDLE_ERRORS_SATURATED)
        expected = FES_TX_REVOKED_UNDISPATCHED;
    if (fault == FLP_UNDOCUMENTED || fault == FLP_LO_WRITE_LOST) {
        assert(!f.transmits && out.phy.restored && !out.phy.applied);
        assert(f.selector==0x8002 && f.offsets[0]==23 && f.offsets[2]==17);
        assert(f.timing[4]==3 && f.timing[3]==(fault==FLP_UNDOCUMENTED ? 0xbeef : 0x4000));
        if (fault==FLP_UNDOCUMENTED) assert(!f.timing_writes && !f.restarts);
        expected=FES_TX_REVOKED_UNDISPATCHED;
    }
    if (fault == FLP_RESTORE_FOREIGN_PHY || fault == FLP_RESTORE_LOST)
        assert(f.transmits && out.phy.applied && !out.phy.restored && out.translation_revoked && out.mac_in_reset);
    if (fault == WRONG_PHY) assert(!out.phy.portal && !f.external_writes);
    unsigned writes = f.writes;
    assert(fes_tx_terminal(&io, &out, rc) == expected);
    assert(f.writes == writes);
    assert(fes_tx_once(&io, frame, &out));
    assert(f.writes == writes);
}
int main(void)
{
    denied_startup();
    for (enum fault i = OK; i <= UNKNOWN_CR2; i++)
        test(i);
    puts("CBL TX domain/translation, frame and retirement fault checks passed");
    return 0;
}
