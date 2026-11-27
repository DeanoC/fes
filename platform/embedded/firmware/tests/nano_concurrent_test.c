/* SPDX-License-Identifier: BSD-3-Clause */
/* Verify opt-in concurrent SD + active FPGA + confirmed Ethernet MAC policy
 * through the actual owned SiP dispatcher and C engines. Hardware MMIO is
 * modeled in-process; this file confers no physical hardware qualification. */
#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "nano_bridge_platform.h"
#include "nano_mac.h"

struct fes_nano_bridges fes_nano_owned_bridges;
struct fes_nano_sd fes_nano_owned_sd;
struct fes_nano_usb_service fes_nano_owned_usb;
struct fes_nano_mac fes_nano_owned_mac;
void fes_nano_bridge_platform_lock(void) {}
void fes_nano_bridge_platform_unlock(void) {}

#define INTEL_SIP_SMC_STATUS_OK 0
#define INTEL_SIP_SMC_STATUS_BUSY 1
#define INTEL_SIP_SMC_STATUS_REJECTED 2
#define INTEL_SIP_SMC_STATUS_ERROR 4
#define INTEL_SIP_SMC_FPGA_CONFIG_START 0xc2000001U
#define INTEL_SIP_SMC_FPGA_CONFIG_WRITE 0x42000002U
#define INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE 0xc2000003U
#define INTEL_SIP_SMC_FPGA_CONFIG_ISDONE 0xc2000004U
#define NO_REQUEST 0
#define RECONFIGURATION 1
#define FPGA_CONFIG_BUFFER_SIZE 1
static unsigned request_type, configurations;
static struct { unsigned write_requested; } fpga_config_buffers[1];
static int intel_fpga_config_start(unsigned a) { (void)a; configurations++; return 0; }
static int intel_fpga_config_write(uint64_t a, uint64_t b) { (void)a; (void)b; configurations++; return 0; }
static int intel_fpga_config_completed_write(uint32_t a[3], uint32_t *b, uint32_t *c)
{ (void)a; (void)b; (void)c; configurations++; return 0; }
static int intel_mailbox_fpga_config_isdone(uint32_t *a) { *a = 0; configurations++; return 0; }
static uint32_t fes_contained_isdone_detail(int a, uint32_t b) { (void)a; return b; }
void fes_nano_platform_sd_report(void) {}
void fes_nano_platform_hps_io_sip(uint64_t operation, uint64_t owner, uint64_t generation,
                                  unsigned busy, uint64_t result[4])
{
    (void)owner; (void)generation; (void)busy;
    result[0] = FES_EMBEDDED_BRIDGE_NOT_READY;
    result[1] = ((uint64_t)FES_EMBEDDED_BRIDGE_HPS_IO_TAG << 32) |
                (operation == FES_EMBEDDED_BRIDGE_HPS_IO_PART1 ? 1U : 0U);
    result[2] = result[3] = 0;
}
#include "nano_owned_sip.inc"

struct model {
    uint64_t epoch;
    uint32_t reset;
    unsigned never_enabled;
    unsigned producers_quiescent;
    unsigned sd_active, sd_prepared, mac_active, mac_confirmed;
    unsigned sd_fault, sd_valid_fault, sd_contained_fault;
    unsigned mac_fault, mac_valid_fault;
    unsigned owns_calls, sd_scopes, sd_resumes, sd_prepares, sd_stops, sd_contained_calls;
    unsigned mac_scopes, mac_acquires, mac_confirms, mac_stops;
};

static struct model m;

static int bridge_owns(void *ctx, uint64_t owner, uint64_t epoch)
{
    struct model *p = ctx;
    p->owns_calls++;
    return owner == 17 && epoch == p->epoch;
}
static int bridge_read_reset(void *ctx, uint32_t *reset)
{
    struct model *p = ctx;
    *reset = p->reset;
    return 0;
}
static int bridge_masters_contained(void *ctx)
{
    struct model *p = ctx;
    return (p->reset & FES_NANO_BRIDGES_DATA) == FES_NANO_BRIDGES_DATA || p->producers_quiescent;
}
static int bridge_stop_producers(void *ctx)
{
    struct model *p = ctx;
    p->producers_quiescent = 1;
    return 0;
}
static int bridge_disable(void *ctx, uint32_t mask, unsigned fabric)
{
    struct model *p = ctx;
    if (fabric != FES_NANO_FABRIC_ABSENT) return -1;
    p->reset |= mask;
    return 0;
}
static int bridge_enable(void *ctx, uint32_t mask, unsigned fabric)
{
    struct model *p = ctx;
    if (fabric != FES_NANO_FABRIC_ABSENT) return -1;
    p->reset &= ~mask;
    p->never_enabled = 0;
    if (mask & FES_NANO_BRIDGES_DATA) p->producers_quiescent = 0;
    return 0;
}

static int sd_scope(void *ctx, uint64_t owner, uint64_t epoch, unsigned active)
{
    struct model *p = ctx;
    p->sd_scopes++;
    if (owner != 17 || epoch != p->epoch || p->sd_active != active || p->sd_fault == 1) return -5;
    return 0;
}
static int sd_resume(void *ctx)
{
    struct model *p = ctx;
    p->sd_resumes++;
    p->sd_active = 1;
    return p->sd_fault == 2 ? -6 : 0;
}
static int sd_prepare(void *ctx)
{
    struct model *p = ctx;
    p->sd_prepares++;
    p->sd_prepared = 1;
    return p->sd_fault == 3 ? -7 : 0;
}
static int sd_stop(void *ctx)
{
    struct model *p = ctx;
    p->sd_stops++;
    if (p->sd_fault == 4) return -8;
    p->sd_active = p->sd_prepared = 0;
    return 0;
}
static int sd_shared_valid(void *ctx, const struct fes_nano_bridges *b, const struct fes_nano_sd *s)
{
    struct model *p = ctx;
    return p == &m && b == &fes_nano_owned_bridges && s == &fes_nano_owned_sd && !p->sd_valid_fault;
}
static int sd_shared_contained(void *ctx, uint64_t owner, uint64_t epoch, unsigned active, unsigned prepared)
{
    struct model *p = ctx;
    p->sd_contained_calls++;
    if (p->sd_contained_fault || owner != 17 || epoch != p->epoch || p->sd_active != active ||
        p->sd_prepared != prepared)
        return -9;
    return 0;
}

static struct fes_nano_mac_binding mac_binding_val(uint64_t epoch)
{
    return (struct fes_nano_mac_binding){17, epoch, 0x92000000U, 0x200000U, 0, 4, 0x001cc916U};
}
static int mac_scope(void *ctx, const struct fes_nano_mac_binding *b, unsigned active, unsigned confirmed)
{
    struct model *p = ctx;
    p->mac_scopes++;
    if (p->mac_fault == 1 || b->owner != 17 || b->epoch != p->epoch ||
        p->mac_active != active || p->mac_confirmed != confirmed)
        return -5;
    return 0;
}
static int mac_acquire(void *ctx, const struct fes_nano_mac_binding *b)
{
    struct model *p = ctx;
    (void)b;
    p->mac_acquires++;
    p->mac_active = 1;
    return p->mac_fault == 2 ? -6 : 0;
}
static int mac_confirm(void *ctx, const struct fes_nano_mac_binding *b)
{
    struct model *p = ctx;
    (void)b;
    p->mac_confirms++;
    p->mac_confirmed = 1;
    return p->mac_fault == 3 ? -7 : 0;
}
static int mac_stop(void *ctx, const struct fes_nano_mac_binding *b)
{
    struct model *p = ctx;
    (void)b;
    p->mac_stops++;
    if (p->mac_fault == 4) return -8;
    p->mac_active = p->mac_confirmed = 0;
    return 0;
}
static int mac_shared_valid(void *ctx, const struct fes_nano_mac *s)
{
    struct model *p = ctx;
    return p == &m && s == &fes_nano_owned_mac && !p->mac_valid_fault;
}

static void reset_all(unsigned allow_during_config)
{
    memset(&fes_nano_owned_bridges, 0, sizeof(fes_nano_owned_bridges));
    memset(&fes_nano_owned_sd, 0, sizeof(fes_nano_owned_sd));
    memset(&fes_nano_owned_usb, 0, sizeof(fes_nano_owned_usb));
    memset(&fes_nano_owned_mac, 0, sizeof(fes_nano_owned_mac));
    memset(&m, 0, sizeof(m));
    request_type = NO_REQUEST;
    configurations = 0;
    m.epoch = 7;
    m.reset = FES_NANO_BRIDGES_ALL;
    m.never_enabled = 1;
    m.producers_quiescent = 1;
    struct fes_nano_bridge_ops bops = {
        &m, bridge_owns, bridge_read_reset, bridge_masters_contained,
        bridge_stop_producers, bridge_disable, bridge_enable,
    };
    assert(!fes_nano_bridges_init(&fes_nano_owned_bridges, bops, 17, m.epoch, FES_NANO_FABRIC_ABSENT, 1));
    struct fes_nano_sd_ops sdops = {&m, sd_scope, sd_resume, sd_prepare, sd_stop, 0};
    struct fes_nano_sd_shared_ops sdshared = {&m, sd_shared_valid, sd_shared_contained, allow_during_config};
    assert(!fes_nano_sd_init_shared(&fes_nano_owned_sd, &fes_nano_owned_bridges, sdops, FES_NANO_SD_QUALIFIED, sdshared));
    struct fes_nano_mac_ops mops = {&m, mac_scope, mac_acquire, mac_confirm, mac_stop};
    struct fes_nano_mac_shared_ops mshared = {&m, mac_shared_valid, allow_during_config};
    assert(!fes_nano_mac_init_shared(&fes_nano_owned_mac, mac_binding_val(m.epoch), mops, FES_NANO_MAC_QUALIFIED, mshared));
}

int main(void)
{
    uint64_t r[4];

    /* 1. Invalid shared initialization fails closed without leaving authority. */
    reset_all(0);
    struct fes_nano_sd bad_sd;
    struct fes_nano_sd_ops sdops = {&m, sd_scope, sd_resume, sd_prepare, sd_stop, 0};
    struct fes_nano_sd_shared_ops bad_sd_shared = {&m, sd_shared_valid, sd_shared_contained, 2};
    memset(&bad_sd, 0, sizeof(bad_sd));
    assert(fes_nano_sd_init_shared(&bad_sd, &fes_nano_owned_bridges, sdops, FES_NANO_SD_QUALIFIED, bad_sd_shared) < 0);
    assert(bad_sd.attempted && !bad_sd.initialized && bad_sd.failed);

    struct fes_nano_mac bad_mac;
    struct fes_nano_mac_ops mops = {&m, mac_scope, mac_acquire, mac_confirm, mac_stop};
    struct fes_nano_mac_shared_ops bad_mac_shared = {&m, mac_shared_valid, 2};
    memset(&bad_mac, 0, sizeof(bad_mac));
    assert(fes_nano_mac_init_shared(&bad_mac, mac_binding_val(m.epoch), mops, FES_NANO_MAC_QUALIFIED, bad_mac_shared) < 0);
    assert(bad_mac.attempted && !bad_mac.initialized && bad_mac.failed);

    /* 2. Full Concurrent SD + Active FPGA (control+data) + Confirmed MAC. */
    reset_all(0);
    assert(fes_nano_sd_returned(&fes_nano_owned_sd, 17, 1, 2) < 0);

    fes_nano_owned_call(FES_EMBEDDED_SD_CALL, FES_EMBEDDED_SD_QUERY, 0, FES_EMBEDDED_SD_COOKIE, 0, r);
    assert(r[0] == 0 && (uint32_t)r[1] == 0xd9U && r[2] == 17 && r[3] == 0);

    /* Resume + Prepare SD (gen 0 -> 1 -> 2), bridge remains gen 1, device_lease stays 0. */
    fes_nano_owned_call(FES_EMBEDDED_SD_CALL, FES_EMBEDDED_SD_RESUME, 17, FES_EMBEDDED_SD_COOKIE, 0, r);
    assert(r[0] == 0 && (uint32_t)r[1] == 0xd3U && r[3] == 1 && !fes_nano_owned_bridges.device_lease);

    /* Half-initialized SD (active && !prepared) returns BUSY to MAC, bridges, config. */
    fes_nano_owned_call(FES_EMBEDDED_MAC_CALL, FES_EMBEDDED_MAC_ACQUIRE, 17, FES_EMBEDDED_MAC_COOKIE, 0, r);
    assert(r[0] == FES_EMBEDDED_MAC_BUSY);
    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL, FES_EMBEDDED_BRIDGE_CONTROL, 17, FES_EMBEDDED_BRIDGE_COOKIE, 1, r);
    assert(r[0] == INTEL_SIP_SMC_STATUS_BUSY);
    fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_START, 0, 0, 0, 0, r);
    assert(r[0] == INTEL_SIP_SMC_STATUS_BUSY && configurations == 0);

    /* Finish SD Prepare (gen 2, flags 0xd7). */
    fes_nano_owned_call(FES_EMBEDDED_SD_CALL, FES_EMBEDDED_SD_PREPARE, 17, FES_EMBEDDED_SD_COOKIE, 1, r);
    assert(r[0] == 0 && (uint32_t)r[1] == 0xd7U && r[3] == 2);

    /* Acquire MAC (unconfirmed): SD mutations & bridges stay BUSY until Confirm. */
    fes_nano_owned_call(FES_EMBEDDED_MAC_CALL, FES_EMBEDDED_MAC_ACQUIRE, 17, FES_EMBEDDED_MAC_COOKIE, 0, r);
    assert(r[0] == 0 && r[3] == 1);
    fes_nano_owned_call(FES_EMBEDDED_SD_CALL, FES_EMBEDDED_SD_STOP, 17, FES_EMBEDDED_SD_COOKIE, 2, r);
    assert(r[0] == FES_EMBEDDED_SD_BUSY);
    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL, FES_EMBEDDED_BRIDGE_CONTROL, 17, FES_EMBEDDED_BRIDGE_COOKIE, 1, r);
    assert(r[0] == INTEL_SIP_SMC_STATUS_BUSY);

    /* Confirm MAC (gen 2). */
    fes_nano_owned_call(FES_EMBEDDED_MAC_CALL, FES_EMBEDDED_MAC_CONFIRM, 17, FES_EMBEDDED_MAC_COOKIE, 1, r);
    assert(r[0] == 0 && r[3] == 2);

    /* Open FPGA control (bridge gen 2) and data (F2SDRAM0=8, bridge gen 3) WITH SD & MAC ACTIVE! */
    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL, FES_EMBEDDED_BRIDGE_CONTROL, 17, FES_EMBEDDED_BRIDGE_COOKIE, 1, r);
    assert(r[0] == 0 && r[3] == 2);
    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL, FES_EMBEDDED_BRIDGE_RELEASE | (8U << FES_EMBEDDED_BRIDGE_RELEASE_SHIFT),
                        17, FES_EMBEDDED_BRIDGE_COOKIE, 2, r);
    assert(r[0] == 0 && r[3] == 3);

    /* SD QUERY now shows DATA_HELD cleared (0xc7), gen still 2. */
    fes_nano_owned_call(FES_EMBEDDED_SD_CALL, FES_EMBEDDED_SD_QUERY, 0, FES_EMBEDDED_SD_COOKIE, 0, r);
    assert(r[0] == 0 && (uint32_t)r[1] == 0xc7U && r[3] == 2);

    /* Cycle SD (Stop -> Resume -> Prepare) WHILE BOTH FPGA BRIDGES ARE OPEN AND MAC IS CONFIRMED. */
    fes_nano_owned_call(FES_EMBEDDED_SD_CALL, FES_EMBEDDED_SD_STOP, 17, FES_EMBEDDED_SD_COOKIE, 2, r);
    assert(r[0] == 0 && (uint32_t)r[1] == 0xc9U && r[3] == 3);
    fes_nano_owned_call(FES_EMBEDDED_SD_CALL, FES_EMBEDDED_SD_RESUME, 17, FES_EMBEDDED_SD_COOKIE, 3, r);
    assert(r[0] == 0 && (uint32_t)r[1] == 0xc3U && r[3] == 4);
    fes_nano_owned_call(FES_EMBEDDED_SD_CALL, FES_EMBEDDED_SD_PREPARE, 17, FES_EMBEDDED_SD_COOKIE, 4, r);
    assert(r[0] == 0 && (uint32_t)r[1] == 0xc7U && r[3] == 5);
    assert(fes_nano_owned_bridges.state.generation == 3 && fes_nano_owned_mac.generation == 2);

    /* Cycle MAC (Stop -> Acquire -> Confirm) WHILE BOTH FPGA BRIDGES ARE OPEN AND SD IS ACTIVE. */
    fes_nano_owned_call(FES_EMBEDDED_MAC_CALL, FES_EMBEDDED_MAC_STOP, 17, FES_EMBEDDED_MAC_COOKIE, 2, r);
    assert(r[0] == 0 && r[3] == 3);
    fes_nano_owned_call(FES_EMBEDDED_MAC_CALL, FES_EMBEDDED_MAC_ACQUIRE, 17, FES_EMBEDDED_MAC_COOKIE, 3, r);
    assert(r[0] == 0 && r[3] == 4);
    fes_nano_owned_call(FES_EMBEDDED_MAC_CALL, FES_EMBEDDED_MAC_CONFIRM, 17, FES_EMBEDDED_MAC_COOKIE, 4, r);
    assert(r[0] == 0 && r[3] == 5);
    assert(fes_nano_owned_sd.generation == 5 && fes_nano_owned_bridges.state.generation == 3);

    /* Close FPGA bridges WITH SD & MAC ACTIVE; DATA_HELD returns to SD QUERY (0xd7). */
    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL, FES_EMBEDDED_BRIDGE_CLOSE, 17, FES_EMBEDDED_BRIDGE_COOKIE, 3, r);
    assert(r[0] == 0 && r[3] == 4);
    fes_nano_owned_call(FES_EMBEDDED_SD_CALL, FES_EMBEDDED_SD_QUERY, 0, FES_EMBEDDED_SD_COOKIE, 0, r);
    assert(r[0] == 0 && (uint32_t)r[1] == 0xd7U && r[3] == 5);

    /* allow_during_config == 0 blocks CONFIG_START while SD or MAC is active, allows when paused. */
    fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_START, 0, 0, 0, 0, r);
    assert(r[0] == INTEL_SIP_SMC_STATUS_BUSY && configurations == 0);
    fes_nano_owned_call(FES_EMBEDDED_SD_CALL, FES_EMBEDDED_SD_STOP, 17, FES_EMBEDDED_SD_COOKIE, 5, r);
    assert(r[0] == 0 && r[3] == 6);
    fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_START, 0, 0, 0, 0, r);
    assert(r[0] == INTEL_SIP_SMC_STATUS_BUSY && configurations == 0);
    fes_nano_owned_call(FES_EMBEDDED_MAC_CALL, FES_EMBEDDED_MAC_STOP, 17, FES_EMBEDDED_MAC_COOKIE, 5, r);
    assert(r[0] == 0 && r[3] == 6);
    fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_START, 0, 0, 0, 0, r);
    assert(r[0] == INTEL_SIP_SMC_STATUS_OK && configurations == 1);

    /* 3. allow_during_config == 1 permits active SD & MAC across FPGA config while
     * configuring == 1 still rejects SD/MAC mutations with BUSY. */
    reset_all(1);
    fes_nano_owned_call(FES_EMBEDDED_SD_CALL, FES_EMBEDDED_SD_RESUME, 17, FES_EMBEDDED_SD_COOKIE, 0, r);
    assert(r[0] == 0);
    fes_nano_owned_call(FES_EMBEDDED_SD_CALL, FES_EMBEDDED_SD_PREPARE, 17, FES_EMBEDDED_SD_COOKIE, 1, r);
    assert(r[0] == 0);
    fes_nano_owned_call(FES_EMBEDDED_MAC_CALL, FES_EMBEDDED_MAC_ACQUIRE, 17, FES_EMBEDDED_MAC_COOKIE, 0, r);
    assert(r[0] == 0);
    fes_nano_owned_call(FES_EMBEDDED_MAC_CALL, FES_EMBEDDED_MAC_CONFIRM, 17, FES_EMBEDDED_MAC_COOKIE, 1, r);
    assert(r[0] == 0);
    fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_START, 0, 0, 0, 0, r);
    assert(r[0] == INTEL_SIP_SMC_STATUS_OK && configurations == 1);
    request_type = RECONFIGURATION;
    fes_nano_owned_call(FES_EMBEDDED_SD_CALL, FES_EMBEDDED_SD_QUERY, 0, FES_EMBEDDED_SD_COOKIE, 0, r);
    assert(r[0] == 0);
    fes_nano_owned_call(FES_EMBEDDED_SD_CALL, FES_EMBEDDED_SD_STOP, 17, FES_EMBEDDED_SD_COOKIE, 2, r);
    assert(r[0] == FES_EMBEDDED_SD_BUSY);
    fes_nano_owned_call(FES_EMBEDDED_MAC_CALL, FES_EMBEDDED_MAC_STOP, 17, FES_EMBEDDED_MAC_COOKIE, 2, r);
    assert(r[0] == FES_EMBEDDED_MAC_BUSY);

    /* 4. Fault in SD contained/valid or MAC valid retires shared authority fail-closed. */
    reset_all(0);
    fes_nano_owned_call(FES_EMBEDDED_SD_CALL, FES_EMBEDDED_SD_RESUME, 17, FES_EMBEDDED_SD_COOKIE, 0, r);
    assert(r[0] == 0);
    fes_nano_owned_call(FES_EMBEDDED_SD_CALL, FES_EMBEDDED_SD_PREPARE, 17, FES_EMBEDDED_SD_COOKIE, 1, r);
    assert(r[0] == 0);
    m.sd_contained_fault = 1;
    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL, FES_EMBEDDED_BRIDGE_SNAPSHOT, 0, FES_EMBEDDED_BRIDGE_COOKIE, 0, r);
    assert(r[0] == INTEL_SIP_SMC_STATUS_ERROR && fes_nano_owned_sd.failed && fes_nano_owned_bridges.failed && fes_nano_owned_sd.reservation);

    reset_all(0);
    fes_nano_owned_call(FES_EMBEDDED_MAC_CALL, FES_EMBEDDED_MAC_ACQUIRE, 17, FES_EMBEDDED_MAC_COOKIE, 0, r);
    assert(r[0] == 0);
    fes_nano_owned_call(FES_EMBEDDED_MAC_CALL, FES_EMBEDDED_MAC_CONFIRM, 17, FES_EMBEDDED_MAC_COOKIE, 1, r);
    assert(r[0] == 0);
    m.mac_valid_fault = 1;
    fes_nano_owned_call(FES_EMBEDDED_SD_CALL, FES_EMBEDDED_SD_RESUME, 17, FES_EMBEDDED_SD_COOKIE, 0, r);
    assert(r[0] == INTEL_SIP_SMC_STATUS_ERROR && fes_nano_owned_mac.failed && fes_nano_owned_bridges.failed && fes_nano_owned_mac.reservation);

    puts("PASS concurrent SD + active FPGA + confirmed Ethernet C dispatcher cases");
    return 0;
}
