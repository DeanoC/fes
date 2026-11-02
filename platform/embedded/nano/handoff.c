/* SPDX-License-Identifier: GPL-3.0-or-later */
#include "handoff.h"

static int valid_range(struct fes_nano_range range)
{
    return range.size && range.start <= ~(fes_u64)0 - range.size;
}

static int overlap(struct fes_nano_range a, struct fes_nano_range b)
{
    return a.start < b.start + b.size && b.start < a.start + a.size;
}

int fes_nano_memory_ok(const struct fes_nano_range *banks, unsigned bank_count,
                       const struct fes_nano_range *used, unsigned used_count,
                       fes_u64 image_size,
                       const struct fes_nano_range *owned, unsigned owned_count)
{
    const struct fes_nano_range go = {FES_NANO_GO_START, FES_NANO_GO_SIZE};
    const struct fes_nano_range stage = {FES_NANO_STAGE, image_size};
    int covered = 0;
    if (!banks || !bank_count || bank_count > 3 || used_count > 64 ||
        (used_count && !used) || owned_count > 4 || (owned_count && !owned) ||
        !image_size || image_size > FES_NANO_STAGE_LIMIT - FES_NANO_STAGE)
        return 0;
    for (unsigned i = 0; i < bank_count; i++) {
        if (!valid_range(banks[i])) return 0;
        if (banks[i].start == FES_NANO_DDR_START &&
            banks[i].size == FES_NANO_DDR_END - FES_NANO_DDR_START)
            covered = 1;
    }
    if (!covered) return 0;
    for (unsigned i = 0; i < owned_count; i++) {
        if (!valid_range(owned[i]) || owned[i].start < FES_NANO_DDR_START ||
            owned[i].size > FES_NANO_DDR_END - FES_NANO_DDR_START ||
            owned[i].start > FES_NANO_DDR_END - owned[i].size ||
            overlap(owned[i], go) || overlap(owned[i], stage)) return 0;
        for (unsigned j = 0; j < i; j++)
            if (overlap(owned[i], owned[j])) return 0;
    }
    for (unsigned i = 0; i < used_count; i++) {
        if (!valid_range(used[i]) || overlap(used[i], go) || overlap(used[i], stage))
            return 0;
        for (unsigned j = 0; j < owned_count; j++)
            if (overlap(used[i], owned[j])) return 0;
    }
    return 1;
}

int fes_nano_cpus_ok(const struct fes_nano_state *s)
{
    if (!s || (s->affinity != 0 && s->affinity != 0x200) ||
        s->counter_hz != 400000000ULL)
        return 0;
    for (unsigned i = 0; i < 4; i++)
        if (s->cpu_state[i] != (i == s->affinity / 0x100 ? 0 : 1)) return 0;
    return 1;
}

int fes_nano_state_ok(const struct fes_nano_state *s)
{
    if (!fes_nano_cpus_ok(s) ||
        (s->per0_reset & (FES_NANO_PER0_FUNCTION | FES_NANO_PER0_OCP)) !=
            (FES_NANO_PER0_FUNCTION | FES_NANO_PER0_OCP) ||
        (s->bridge_reset & FES_NANO_BRIDGES) != FES_NANO_BRIDGES ||
        (s->per1_reset & (1U << 17)) || (s->uart_lcr & 0x83) != 3 ||
        (s->uart_lsr & 0x60) != 0x60)
        return 0;
    for (unsigned i = 0; i < 5; i++)
        if (s->watchdog_control[i] & 1) return 0;
    return 1;
}
