/* SPDX-License-Identifier: BSD-3-Clause */
/* Compiled with actual pinned vendor functions by verify_nano_bridges.py. */
#include "nano_bridge_fixture.h"
#include <assert.h>

static struct {
    uint32_t reset, request, enable;
    unsigned masters, fabric_ready, fabric_mode, fault;
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
    if (mask == 4) registers.fabric_polls++;
    else registers.hardware_polls++;
    if ((registers.fault == 1 && mask == 0x1000 && expected == 0x1000) ||
        (registers.fault == 2 && mask == 4 && expected == 4) ||
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
    fes_nano_bridge_diag_reset();
    int result = fes_nano_raw_disable(mask);
    if (registers.fault == 5) registers.reset &= ~4U;
    return result;
}
static int enable(void *context, uint32_t mask, unsigned fabric)
{
    (void)context; registers.enables++; registers.fabric_mode = fabric;
    unsigned fault = registers.fault;
    if (fault == 8 || (fault == 9 && mask == FES_NANO_BRIDGES_DATA)) registers.fault = 6;
    int result = fes_nano_raw_enable(mask);
    registers.fault = fault;
    if (!result) registers.masters = mask == FES_NANO_BRIDGES_CONTROL;
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
