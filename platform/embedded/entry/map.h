/* SPDX-License-Identifier: GPL-3.0-or-later */
#ifndef FES_ENTRY_MAP_H
#define FES_ENTRY_MAP_H
#include <stdint.h>

#define FES_ENTRY_MAP_PAGE 4096ULL
#define FES_ENTRY_MAP_LOADER_BLOCKS 18U
#define FES_ENTRY_MAP_MAX_IMAGE (33ULL << 20)
#define FES_ENTRY_MAP_TCR 0x80823519ULL /* EL2 RES1, 39-bit VA, 40-bit PA, 4K, inner WBWA */
#define FES_ENTRY_MAP_MAIR 0xff00ULL   /* attr0 Device-nGnRnE, attr1 normal WBWA */
struct fes_entry_tables {
    uint64_t root[512], ram[512], uart_middle[512], uart_page[512];
    uint64_t loader[FES_ENTRY_MAP_LOADER_BLOCKS][512];
};
struct fes_entry_cache_state {
    uint64_t sctlr, tcr, ttbr, mair, ram_start, ram_end, image_start, image_end;
};

/* All geometry is validated before writing tables. Only owned Go RAM and the
 * page-complete loader become normal memory; only the selected UART page is
 * device memory. Every other address is invalid. Physical tables lie in image. */
int fes_entry_map_plan(struct fes_entry_tables *tables, uint64_t tables_address,
                      uint64_t ram_start, uint64_t ram_end,
                      uint64_t image_start, uint64_t image_end, uint64_t uart);
int fes_entry_cache_supported(void);
int fes_entry_cache_enable(uint64_t tables_address, struct fes_entry_cache_state *state,
                          uint64_t ram_start, uint64_t ram_end,
                          uint64_t image_start, uint64_t image_end);
int fes_entry_cache_disable(const struct fes_entry_cache_state *state);
int fes_entry_cache_observe(void);
extern struct fes_entry_tables fes_entry_tables;
#endif
