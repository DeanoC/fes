#ifndef FES_JENT_EVAL_ARENA_H
#define FES_JENT_EVAL_ARENA_H

#include <stdbool.h>
#include <stddef.h>

#define FES_JENT_ARENA_BYTES (192U * 1024U)
#define FES_JENT_ARENA_ALIGNMENT 16U

/* Single-CPU cold-boot workspace. A successful allocation is zeroed and
 * aligned to 16 bytes. Zero-sized and unsatisfiable requests return NULL. */
void *fes_jent_alloc(size_t bytes);
void fes_jent_free(void *ptr);

/* Wipe every allocation and all bookkeeping. All previous pointers expire. */
void fes_jent_arena_reset(void);

/* Peak end offset from the start of the whole arena since reset. Includes
 * the control prefix, per-block header, alignment padding and any holes. */
size_t fes_jent_heap_high_water(void);

/* Verify zero outstanding allocations and the erasure of the storage. */
bool fes_jent_heap_empty(void);

#endif
