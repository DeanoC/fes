/* SPDX-License-Identifier: GPL-3.0-or-later */
#ifndef FES_NANO_HANDOFF_H
#define FES_NANO_HANDOFF_H
typedef unsigned long long fes_u64;
typedef unsigned int fes_u32;
typedef int fes_s32;
_Static_assert(sizeof(fes_u64) == 8 && sizeof(fes_u32) == 4 && sizeof(fes_s32) == 4, "Nano handoff integer widths");

/* Pinned Terasic Nano DDR layout; live U-Boot banks/reservations are checked. */
#define FES_NANO_DDR_START 0x80000000ULL
#define FES_NANO_DDR_END 0xc0000000ULL
#define FES_NANO_FW_DDR_START 0x80000000ULL
#define FES_NANO_FW_DDR_BYTES 0x00200000ULL
#define FES_NANO_GO_START 0x84000000ULL
#define FES_NANO_GO_SIZE 0x10000000ULL
#define FES_NANO_STAGE 0x96080000ULL
#define FES_NANO_STAGE_LIMIT 0x98080000ULL
#define FES_NANO_PER0_FUNCTION 0xff0100bfU
#define FES_NANO_PER0_OCP 0x0020bf00U
#define FES_NANO_BRIDGES 0xfU

struct fes_nano_range { fes_u64 start, size; };
struct fes_nano_state {
    fes_u64 affinity, counter_hz;
    fes_s32 cpu_state[4]; /* PSCI AFFINITY_INFO level 0: ON=0, OFF=1 */
    fes_u32 per0_reset, per1_reset, bridge_reset;
    fes_u32 watchdog_control[5], uart_lcr, uart_lsr;
};

/* Pure policy shared by the real U-Boot adapter and host rejection tests. */
int fes_nano_memory_ok(const struct fes_nano_range *banks, unsigned bank_count,
                       const struct fes_nano_range *used, unsigned used_count,
                       fes_u64 image_size,
                       const struct fes_nano_range *owned, unsigned owned_count);
int fes_nano_state_ok(const struct fes_nano_state *state);
int fes_nano_cpus_ok(const struct fes_nano_state *state);
int fes_nano_firmware_reservation_ok(const struct fes_nano_range *regions,
                                    const fes_u32 *flags, unsigned count);
#endif
