/* SPDX-License-Identifier: BSD-3-Clause */
/* Host-only concurrent SD + FPGA bridge + Ethernet MAC wire/PIO integration. */
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

struct concurrent_hw {
    uint64_t epoch;
    uint32_t reset, clock, mode, busy, signal, status_enable, fault;
    unsigned producers_quiescent;
    unsigned sd_active, sd_prepared, mac_active, mac_confirmed;
    unsigned owns, sd_resumes, sd_prepares, sd_stops, sd_scopes;
    unsigned mac_acquires, mac_confirms, mac_stops, mac_scopes;
};
static struct concurrent_hw hw;

static int bridge_owns(void *ctx, uint64_t owner, uint64_t epoch)
{
    struct concurrent_hw *h = ctx;
    h->owns++;
    return owner == 17 && epoch == h->epoch;
}
static int bridge_read_reset(void *ctx, uint32_t *reset)
{ *reset = ((struct concurrent_hw *)ctx)->reset; return 0; }
static int bridge_masters_contained(void *ctx)
{
    struct concurrent_hw *h = ctx;
    return (h->reset & FES_NANO_BRIDGES_DATA) == FES_NANO_BRIDGES_DATA || h->producers_quiescent;
}
static int bridge_stop(void *ctx)
{ ((struct concurrent_hw *)ctx)->producers_quiescent = 1; return 0; }
static int bridge_disable(void *ctx, uint32_t mask, unsigned fabric)
{
    struct concurrent_hw *h = ctx;
    if (fabric != FES_NANO_FABRIC_ABSENT) return -1;
    h->reset |= mask; return 0;
}
static int bridge_enable(void *ctx, uint32_t mask, unsigned fabric)
{
    struct concurrent_hw *h = ctx;
    if (fabric != FES_NANO_FABRIC_ABSENT) return -1;
    h->reset &= ~mask;
    if (mask & FES_NANO_BRIDGES_DATA) h->producers_quiescent = 0;
    return 0;
}

static int sd_scope(void *ctx, uint64_t owner, uint64_t epoch, unsigned active)
{
    struct concurrent_hw *h = ctx;
    h->sd_scopes++;
    if (owner != 17 || epoch != h->epoch || h->sd_active != active || h->fault == 1) return -5;
    return 0;
}
static int sd_resume(void *ctx)
{ struct concurrent_hw *h = ctx; h->sd_resumes++; h->sd_active = 1; return h->fault == 2 ? -6 : 0; }
static int sd_prepare(void *ctx)
{
    struct concurrent_hw *h = ctx;
    h->sd_prepares++;
    if ((h->clock & (7U | (7U << 24))) != 3U || h->mode || h->signal) return -79;
    if (h->fault == 4) return -7;
    h->sd_prepared = 1;
    return 0;
}
static int sd_stop(void *ctx)
{
    struct concurrent_hw *h = ctx;
    h->sd_stops++;
    if ((h->clock & (7U | (7U << 24))) != 3U || h->mode || h->busy || h->signal || h->status_enable) return -80;
    if (h->fault == 6) return -8;
    h->sd_active = h->sd_prepared = 0;
    return 0;
}
static int sd_valid(void *ctx, const struct fes_nano_bridges *b, const struct fes_nano_sd *s)
{
    struct concurrent_hw *h = ctx;
    return h == &hw && b == &fes_nano_owned_bridges && s == &fes_nano_owned_sd && h->fault != 9;
}
static int sd_contained(void *ctx, uint64_t owner, uint64_t epoch, unsigned active, unsigned prepared)
{
    struct concurrent_hw *h = ctx;
    if (owner != 17 || epoch != h->epoch || h->sd_active != active ||
        h->sd_prepared != prepared || h->fault == 10)
        return -11;
    return 0;
}

static int mac_scope(void *ctx, const struct fes_nano_mac_binding *b, unsigned active, unsigned confirmed)
{
    struct concurrent_hw *h = ctx;
    h->mac_scopes++;
    if (b->owner != 17 || b->epoch != h->epoch || h->mac_active != active ||
        h->mac_confirmed != confirmed || h->fault == 11)
        return -5;
    return 0;
}
static int mac_acquire(void *ctx, const struct fes_nano_mac_binding *b)
{ (void)b; struct concurrent_hw *h = ctx; h->mac_acquires++; h->mac_active = 1; return 0; }
static int mac_confirm(void *ctx, const struct fes_nano_mac_binding *b)
{ (void)b; struct concurrent_hw *h = ctx; h->mac_confirms++; h->mac_confirmed = 1; return 0; }
static int mac_stop(void *ctx, const struct fes_nano_mac_binding *b)
{ (void)b; struct concurrent_hw *h = ctx; h->mac_stops++; h->mac_active = h->mac_confirmed = 0; return 0; }
static int mac_valid(void *ctx, const struct fes_nano_mac *s)
{ return ctx == &hw && s == &fes_nano_owned_mac && hw.fault != 12; }

int main(void)
{
    memset(&hw, 0, sizeof(hw));
    hw.epoch = 7;
    hw.reset = FES_NANO_BRIDGES_ALL;
    hw.clock = 3;
    hw.producers_quiescent = 1;
    struct fes_nano_bridge_ops bops = {
        &hw, bridge_owns, bridge_read_reset, bridge_masters_contained,
        bridge_stop, bridge_disable, bridge_enable,
    };
    assert(!fes_nano_bridges_init(&fes_nano_owned_bridges, bops, 17, hw.epoch, FES_NANO_FABRIC_ABSENT, 1));
    struct fes_nano_sd_ops sdops = {&hw, sd_scope, sd_resume, sd_prepare, sd_stop, 0};
    struct fes_nano_sd_shared_ops sdshared = {&hw, sd_valid, sd_contained, 0};
    assert(!fes_nano_sd_init_shared(&fes_nano_owned_sd, &fes_nano_owned_bridges, sdops, FES_NANO_SD_QUALIFIED, sdshared));
    struct fes_nano_mac_binding mb = {17, hw.epoch, 0x92000000U, 0x200000U, 0, 4, 0x001cc916U};
    struct fes_nano_mac_ops mops = {&hw, mac_scope, mac_acquire, mac_confirm, mac_stop};
    struct fes_nano_mac_shared_ops mshared = {&hw, mac_valid, 0};
    assert(!fes_nano_mac_init_shared(&fes_nano_owned_mac, mb, mops, FES_NANO_MAC_QUALIFIED, mshared));

    unsigned long long q[7];
    while (scanf("%llx %llx %llx %llx %llx %llx %llx %x %x %x %x %x %x",
                 &q[0], &q[1], &q[2], &q[3], &q[4], &q[5], &q[6],
                 &hw.clock, &hw.mode, &hw.busy, &hw.signal, &hw.status_enable, &hw.fault) == 13) {
        uint64_t r[4] = {0};
        fes_nano_owned_call((uint32_t)q[0], q[1], q[2], q[3], q[4], r);
        printf("%llx %llx %llx %llx %x %x %x %x %x %x %x\n",
               (unsigned long long)r[0], (unsigned long long)r[1],
               (unsigned long long)r[2], (unsigned long long)r[3],
               hw.sd_active, hw.sd_resumes, hw.sd_prepares, hw.sd_stops,
               hw.mac_active, hw.mac_confirmed, hw.reset);
        fflush(stdout);
    }
    return ferror(stdin) ? 1 : 0;
}
