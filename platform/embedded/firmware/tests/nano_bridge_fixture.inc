/* SPDX-License-Identifier: BSD-3-Clause */
/* Compiled with actual pinned vendor functions by verify_nano_bridges.py. */
#include "nano_bridge_fixture.h"
#include "nano_boot.h"
#include <assert.h>

static struct {
    uint32_t reset, request, enable, closing;
    unsigned masters, fabric_ready, fabric_mode, fault, disabling;
    unsigned disables, enables, polls, fabric_polls, hardware_polls, writes;
    uint64_t epoch;
} registers;
struct fes_nano_bridges fes_nano_fixture_service;
static int fes_nano_raw_enable(uint32_t mask);
static int fes_nano_raw_disable(uint32_t mask);
int fes_nano_bridge_fabric_handshake(void) { return registers.fabric_mode == FES_NANO_FABRIC_PRESENT; }

static uint32_t mmio_read_32(uint32_t address)
{
    if (address == FES_REG_BRGMODRST) return registers.reset;
    if (address == FES_REG_HDSKREQ) return registers.request;
    if (address == FES_REG_HDSKEN) return registers.enable;
    if (address == FES_REG_HDSKACK)
        return (registers.request & 0x1e00U) |
               (registers.fabric_ready ? registers.request & 4U : 0U);
    return 0;
}
static void mmio_setbits_32(uint32_t address, uint32_t mask)
{
    registers.writes++;
    if (address == FES_REG_BRGMODRST) registers.reset |= mask;
    else if (address == FES_REG_HDSKREQ) registers.request |= mask;
    else if (address == FES_REG_HDSKEN) registers.enable |= mask;
    /* ACK is W1C; the modeled response is injected independently. */
}
static void mmio_clrbits_32(uint32_t address, uint32_t mask)
{
    registers.writes++;
    if (address == FES_REG_BRGMODRST) registers.reset &= ~mask;
    else if (address == FES_REG_HDSKREQ) registers.request &= ~mask;
    else if (address == FES_REG_HDSKEN) registers.enable &= ~mask;
}
static void udelay(unsigned usec) { (void)usec; }
static int poll_idle_status(uint32_t address, uint32_t mask, uint32_t expected, unsigned budget)
{
    assert(address == FES_REG_HDSKACK && (budget == 300 || budget == 1000));
    registers.polls++;
    /* Check both asserted and cleared polls against the actual pinned header
     * names. Unopened paths must not be polled during a partial close. */
    if (registers.disabling) {
        if (mask == RSTMGR_HDSKACK_FPGA2SOCACK)
            assert(registers.closing & RSTMGR_BRGMODRSTMASK_FPGA2SOC);
        if (mask == RSTMGR_HDSKACK_F2SDRAM0ACK)
            assert(registers.closing & RSTMGR_BRGMODRSTMASK_F2SDRAM0);
    }
    if (mask == 4) registers.fabric_polls++;
    else registers.hardware_polls++;
    if ((registers.fault == 1 && registers.disabling &&
         ((mask == RSTMGR_HDSKACK_FPGA2SOCACK && expected == RSTMGR_HDSKACK_F2S_FLUSH) ||
          (mask == RSTMGR_HDSKACK_F2SDRAM0ACK && expected == RSTMGR_HDSKACK_F2SDRAM0ACK_DASRT))) ||
        (registers.fault == 2 && registers.disabling && mask == RSTMGR_HDSKACK_FPGAHSACK) ||
        registers.fault == 6)
        return -60;
    return (mmio_read_32(address) & mask) == expected ? 0 : -60;
}
static void socfpga_s2f_bridge_mask(uint32_t mask, uint32_t *bridge, uint32_t *noc)
{ *bridge = mask & 3U; *noc = 0; }
static void socfpga_f2s_bridge_mask(uint32_t mask, uint32_t *bridge, uint32_t *idle,
                                  uint32_t *drain, uint32_t *enable, uint32_t *ack,
                                  uint32_t *empty, uint32_t *command)
{ *bridge = mask & 12U; *idle = *drain = *enable = *ack = *empty = *command = 0; }

static int owns(void *context, uint64_t owner, uint64_t epoch)
{ (void)context; return owner == 17 && epoch == registers.epoch; }
static int read_reset(void *context, uint32_t *reset)
{ (void)context; *reset = registers.reset; return 0; }
static int masters(void *context)
{ (void)context; return (int)registers.masters; }
static int stop(void *context)
{
    (void)context;
    if (registers.fault == 3) return -5;
    if (registers.fault == 7) registers.epoch++;
    registers.masters = 1;
    return 0;
}
static int disable(void *context, uint32_t mask, unsigned fabric)
{
    (void)context; registers.disables++; registers.fabric_mode = fabric;
    registers.closing = mask;
    fes_nano_bridge_diag_reset();
    registers.disabling = 1;
    int result = fes_nano_raw_disable(mask);
    registers.disabling = 0;
    if (registers.fault == 5) registers.reset &= ~4U;
    return result;
}
static int enable(void *context, uint32_t mask, unsigned fabric)
{
    (void)context; registers.enables++; registers.fabric_mode = fabric;
    int result = fes_nano_raw_enable(mask);
    if (!result) registers.masters = mask == FES_NANO_BRIDGES_CONTROL;
    /* Opening a reset bridge may skip every poll in the pinned driver. Inject
     * an uncertain callback return at the requested transition, rather than a
     * timeout that might never run (or run only at a later data opening). */
    if ((registers.fault == 8 && mask == FES_NANO_BRIDGES_CONTROL) ||
        (registers.fault == 9 && (mask & FES_NANO_BRIDGES_DATA))) return -60;
    return result;
}
int fes_nano_fixture_boot(unsigned reset, unsigned cold, unsigned fabric)
{
    registers.reset = reset; registers.request = registers.enable = 0;
    registers.masters = cold; registers.epoch = 3; registers.fault = 0;
    registers.fabric_ready = fabric == FES_NANO_FABRIC_PRESENT;
    registers.disables = registers.enables = registers.polls = registers.writes = 0;
    registers.fabric_polls = registers.hardware_polls = 0;
    fes_nano_fixture_service = (struct fes_nano_bridges){0}; /* simulated reboot */
    struct fes_nano_bridge_ops ops = {0, owns, read_reset, masters, stop, disable, enable};
    return fes_nano_bridges_init(&fes_nano_fixture_service, ops, 17, 3, fabric, cold);
}
static struct fes_nano_boot boot_handoff;
static int quiesce_all(void *context, uint32_t mask)
{ assert(mask == FES_NANO_PRODUCERS_ALL); return stop(context); }
static int quiescent_all(void *context, uint32_t *mask)
{
    (void)context;
    *mask = registers.masters ? FES_NANO_PRODUCERS_ALL : 0;
    if (registers.fault == 10) *mask &= ~FES_NANO_PRODUCERS_FPGA;
    return 0;
}
static int per0(void *context, uint32_t *value)
{ (void)context; *value = registers.masters ? FES_NANO_MASTER_RESET : 0; return 0; }
static int per0_assert(void *context, uint32_t mask)
{ (void)context; (void)mask; return registers.masters ? 0 : -1; }
static void barrier(void *context) { (void)context; }
static void wait_master(void *context) { (void)context; }
int fes_nano_fixture_boot_handoff(unsigned reset, unsigned cold, unsigned fabric)
{
    /* This is the host's simulated secure boot entry, never a SiP operation.
     * Register/reset details are modeled; standalone boot tests exercise the
     * independent quiescence and reset-write ordering in detail. */
    int status = fes_nano_fixture_boot(reset, cold, fabric);
    if (status) return status;
    fes_nano_fixture_service = (struct fes_nano_bridges){0};
    boot_handoff = (struct fes_nano_boot){0};
    struct fes_nano_boot_record record = {0};
    struct fes_nano_boot_policy policy = {0};
    record.magic = FES_NANO_BOOT_MAGIC; record.version = FES_NANO_BOOT_VERSION;
    record.size = sizeof(record); record.owner = 17; record.epoch = 3;
    record.chain[0] = policy.chain[0] = 0x42;
    record.history = policy.history = cold ? FES_NANO_BOOT_NEVER_ENABLED : FES_NANO_BOOT_ACTIVE;
    record.fabric = policy.fabric = fabric;
    record.producers = policy.producers = FES_NANO_PRODUCERS_ALL;
    policy.qualifications = FES_NANO_BOOT_QUALIFIED;
    struct fes_nano_master_ops m = {0, owns, quiesce_all, quiescent_all, per0, per0_assert, barrier, wait_master};
    struct fes_nano_bridge_ops raw = {0, 0, read_reset, 0, 0, disable, enable};
    return fes_nano_boot_accept(&boot_handoff, &fes_nano_fixture_service, &record, &policy, m, raw);
}
void fes_nano_fixture_fault(unsigned fault)
{
    registers.fault = fault;
    if (fault == 4) registers.epoch++; /* same reset bits, new external epoch */
}
void fes_nano_fixture_stats(uint64_t result[6])
{
    result[0] = registers.disables; result[1] = registers.enables;
    result[2] = registers.polls; result[3] = registers.fabric_polls;
    result[4] = registers.hardware_polls; result[5] = registers.writes;
}
