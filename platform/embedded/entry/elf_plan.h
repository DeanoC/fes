/* SPDX-License-Identifier: GPL-3.0-or-later */
#ifndef FES_ENTRY_ELF_PLAN_H
#define FES_ENTRY_ELF_PLAN_H

#include <stdint.h>

/* Fixed bounds keep validation and copying allocation-free before Go starts. */
#define FES_ENTRY_MAX_SEGMENTS 16U

struct fes_entry_segment {
    uint64_t address, memory_size, file_offset, file_size;
};

struct fes_entry_plan {
    uint64_t entry;
    uint32_t count;
    struct fes_entry_segment segments[FES_ENTRY_MAX_SEGMENTS];
};

/* Validate the complete static ELF before publishing a load plan. `ram_start`
 * and `ram_end` delimit the owned RAM, `staging_*` the live loader/image.
 * The first 64 KiB belongs to early tables; the final 128 KiB protects the
 * runtime's initial stack. The caller must reserve the staging range itself.
 * This checks structure and bounds; it does not authenticate the image. */
int fes_entry_plan_elf(const uint8_t *data, uint64_t size,
                       uint64_t ram_start, uint64_t ram_end,
                       uint64_t staging_start, uint64_t staging_end,
                       struct fes_entry_plan *result);

#endif
