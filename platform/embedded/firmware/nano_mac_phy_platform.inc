/* SPDX-License-Identifier: BSD-3-Clause */
/* The matched MMIO accessor selects the secure system-manager alias for
 * PHYSEL and BOOT_SCRATCH_COLD1 once EL3 translation is active. The clock,
 * reset and MAC blocks use their existing device mappings. */
#include <arch_helpers.h>
#include <lib/mmio.h>
#ifndef FES_NANO_SECURE_MMIO_H
#error Matched_CBL_secure_system_manager_alias_required
#endif
static uint32_t phy_read(uint32_t address) { return mmio_read_32(address); }
static void phy_write(uint32_t address, uint32_t value)
{
    mmio_write_32(address, value);
    dsbsy();
}
static uint64_t phy_us(void) { return read_cntpct_el0() / 400U; }
static int phy_clock_valid(void) { return read_cntfrq_el0() == 400000000U; }
