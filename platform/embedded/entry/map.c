/* SPDX-License-Identifier: GPL-3.0-or-later */
#include <stddef.h>
#include <stdint.h>
#include "map.h"
#include "memory.h"

#define BLOCK (2ULL << 20)
#define ROOT (1ULL << 30)
#define AF (1ULL << 10)
#define XN (1ULL << 54)
#define NORMAL (AF | (3ULL << 8) | (1ULL << 2))
#define DEVICE (AF | (2ULL << 8) | XN)

int fes_entry_map_plan(struct fes_entry_tables *t, uint64_t base,
                      uint64_t ram_start, uint64_t ram_end,
                      uint64_t image_start, uint64_t image_end, uint64_t uart)
{
    if (!t || ram_start < ROOT || ram_end <= ram_start || ram_end > (1ULL << 32) ||
        (ram_start | ram_end) & (BLOCK-1) ||
        !image_start || image_end <= image_start || image_end > (1ULL << 32) ||
        (image_start | image_end | base) & (FES_ENTRY_MAP_PAGE-1) ||
        image_end-image_start > FES_ENTRY_MAP_MAX_IMAGE ||
        (ram_start < image_end && image_start < ram_end) ||
        ram_start/ROOT != (ram_end-1)/ROOT || ram_start/ROOT != image_start/ROOT ||
        ram_start/ROOT != (image_end-1)/ROOT ||
        base < image_start || base > image_end || sizeof(*t) > image_end-base ||
        uart < FES_ENTRY_MAP_PAGE || uart >= ROOT || (uart & 3) ||
        (uart & (FES_ENTRY_MAP_PAGE-1)) > FES_ENTRY_MAP_PAGE-0x100) return 0;
    uint64_t first = image_start & ~(BLOCK-1);
    uint64_t blocks = ((image_end-1)/BLOCK)-(image_start/BLOCK)+1;
    if (blocks > FES_ENTRY_MAP_LOADER_BLOCKS) return 0;
    fes_entry_zero((uint8_t *)t, sizeof(*t));
    t->root[ram_start/ROOT] = (base+offsetof(struct fes_entry_tables,ram)) | 3;
    t->root[0] = (base+offsetof(struct fes_entry_tables,uart_middle)) | 3;
    for (uint64_t p = ram_start; p < ram_end; p += BLOCK)
        t->ram[(p/BLOCK)&511] = p | NORMAL | XN | 1;
    for (uint64_t i = 0; i < blocks; i++) {
        uint64_t block = first+i*BLOCK;
        t->ram[(block/BLOCK)&511] = (base+offsetof(struct fes_entry_tables,loader)+i*FES_ENTRY_MAP_PAGE) | 3;
        for (uint64_t j = 0; j < 512; j++) {
            uint64_t page = block+j*FES_ENTRY_MAP_PAGE;
            if (page >= image_start && page < image_end)
                t->loader[i][j] = page | NORMAL | 3;
        }
    }
    t->uart_middle[(uart/BLOCK)&511] = (base+offsetof(struct fes_entry_tables,uart_page)) | 3;
    t->uart_page[(uart/FES_ENTRY_MAP_PAGE)&511] = (uart & ~(FES_ENTRY_MAP_PAGE-1)) | DEVICE | 3;
    return 1;
}
