/* SPDX-License-Identifier: GPL-3.0-or-later */
#include "elf_plan.h"

static uint16_t get16(const uint8_t *p)
{
    return (uint16_t)p[0] | (uint16_t)p[1] << 8;
}

static uint32_t get32(const uint8_t *p)
{
    return (uint32_t)get16(p) | (uint32_t)get16(p + 2) << 16;
}

static uint64_t get64(const uint8_t *p)
{
    return (uint64_t)get32(p) | (uint64_t)get32(p + 4) << 32;
}

static int overlaps(uint64_t a, uint64_t b, uint64_t x, uint64_t y)
{
    return a < y && x < b;
}

int fes_entry_plan_elf(const uint8_t *data, uint64_t size,
                       uint64_t ram_start, uint64_t ram_end,
                       uint64_t staging_start, uint64_t staging_end,
                       struct fes_entry_plan *result)
{
    struct fes_entry_plan plan = {0};
    uint64_t phoff, entry;
    uint16_t count;
    int executable = 0;

    if (!data || !result || size < 64 || ram_end <= ram_start ||
        ram_end - ram_start <= 0x30000 || staging_end <= staging_start)
        return 0;
    if (data[0] != 0x7f || data[1] != 'E' || data[2] != 'L' || data[3] != 'F' ||
        data[4] != 2 || data[5] != 1 || data[6] != 1 || get16(data + 16) != 2 ||
        get16(data + 18) != 183 || get32(data + 20) != 1 ||
        get16(data + 52) != 64 || get16(data + 54) != 56)
        return 0;
    entry = get64(data + 24);
    phoff = get64(data + 32);
    count = get16(data + 56);
    if (!count || count > 64 || phoff > size || count > (size - phoff) / 56 ||
        (entry & 3) || entry < ram_start + 0x10000 || entry >= ram_end - 0x20000)
        return 0;

    for (uint16_t i = 0; i < count; i++) {
        const uint8_t *p = data + phoff + (uint64_t)i * 56;
        uint32_t type = get32(p), flags = get32(p + 4);
        uint64_t offset = get64(p + 8), address = get64(p + 16);
        uint64_t physical = get64(p + 24), file_size = get64(p + 32);
        uint64_t memory_size = get64(p + 40), align = get64(p + 48);
        if (type == 2 || type == 3) /* no dynamic linker or interpreter */
            return 0;
        if (type != 1)
            continue;
        if (plan.count == FES_ENTRY_MAX_SEGMENTS || flags & ~7U ||
            address != physical || address < ram_start + 0x10000 || address >= ram_end - 0x20000 ||
            !memory_size || memory_size > ram_end - 0x20000 - address || file_size > memory_size ||
            offset > size || file_size > size - offset ||
            (align > 1 && ((align & (align - 1)) || ((address - offset) & (align - 1)))) ||
            overlaps(address, address + memory_size, staging_start, staging_end))
            return 0;
        for (uint32_t j = 0; j < plan.count; j++) {
            const struct fes_entry_segment *prior = &plan.segments[j];
            if (overlaps(address, address + memory_size, prior->address, prior->address + prior->memory_size))
                return 0;
        }
        struct fes_entry_segment *seg = &plan.segments[plan.count++];
        seg->address = address;
        seg->memory_size = memory_size;
        seg->file_offset = offset;
        seg->file_size = file_size;
        if ((flags & 1U) && entry >= address && entry - address < file_size)
            executable = 1;
    }
    if (!plan.count || !executable)
        return 0;
    plan.entry = entry;
    *result = plan;
    return 1;
}
