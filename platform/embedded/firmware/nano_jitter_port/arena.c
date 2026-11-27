#include "arena.h"

#define UNIT FES_JENT_ARENA_ALIGNMENT
#define STORAGE_BYTES (FES_JENT_ARENA_BYTES - UNIT)

/* Free units are zero. Each live allocation starts with a private header
 * unit encoding its total span in units, followed by aligned caller storage.
 * Walking from a block boundary skips its entire payload. Adjacent released
 * units therefore form a single gap without a free list or a block limit. */
struct workspace {
    size_t high_water;
    size_t live;
    unsigned char storage[STORAGE_BYTES] __attribute__((aligned(UNIT)));
};

static struct workspace arena
    __attribute__((section(".data.fes_jent_eval"), aligned(UNIT))) = {0};

_Static_assert((UNIT & (UNIT-1U)) == 0 && UNIT >= 2U, "invalid quantum");
_Static_assert(FES_JENT_ARENA_BYTES % UNIT == 0, "unaligned arena");
_Static_assert(STORAGE_BYTES / UNIT <= 65535U, "span encoding too narrow");
_Static_assert(offsetof(struct workspace, storage) == UNIT, "control prefix changed");
_Static_assert(sizeof(arena) == FES_JENT_ARENA_BYTES, "workspace budget changed");

static void wipe(void *ptr, size_t bytes)
{
    volatile unsigned char *out = ptr;
    while (bytes--)
        *out++ = 0;
}

static size_t span_at(size_t offset)
{
    const unsigned char *p = arena.storage + offset;
    return ((size_t)p[0] + ((size_t)p[1] << 8)) * UNIT;
}

static size_t scan_end(void)
{
    return arena.high_water ? arena.high_water - UNIT : 0;
}

void *fes_jent_alloc(size_t bytes)
{
    size_t first = 0;
    size_t end = scan_end();
    size_t offset = 0;
    size_t payload, span, units;

    /* Bound before either round-up or header addition. */
    if (bytes == 0 || bytes > STORAGE_BYTES - UNIT)
        return NULL;
    payload = (bytes + UNIT - 1U) & ~(size_t)(UNIT - 1U);
    span = UNIT + payload;

    while (offset < end) {
        size_t used = span_at(offset);
        if (used == 0) {
            offset += UNIT;
            continue;
        }
        if (offset - first >= span)
            break;
        offset += used;
        first = offset;
    }

    if (span > STORAGE_BYTES - first)
        return NULL;

    units = span / UNIT;
    arena.storage[first] = (unsigned char)(units & 0xffU);
    arena.storage[first+1] = (unsigned char)(units >> 8);
    arena.live++;
    end = UNIT + first + span;
    if (arena.high_water < end)
        arena.high_water = end;
    return arena.storage + first + UNIT;
}

void fes_jent_free(void *ptr)
{
    size_t offset = 0;
    size_t end = scan_end();

    if (!ptr)
        return;
    while (offset < end) {
        size_t used = span_at(offset);
        if (used == 0) {
            offset += UNIT;
            continue;
        }
        if (ptr == arena.storage + offset + UNIT) {
            wipe(arena.storage + offset, used);
            arena.live--;
            return;
        }
        offset += used;
    }
}

void fes_jent_arena_reset(void)
{
    wipe(&arena, sizeof(arena));
}

size_t fes_jent_heap_high_water(void)
{
    return arena.high_water;
}

bool fes_jent_heap_empty(void)
{
    size_t i;
    if (arena.live != 0)
        return false;
    for (i = 0; i < STORAGE_BYTES; i++) {
        if (arena.storage[i] != 0)
            return false;
    }
    return true;
}
