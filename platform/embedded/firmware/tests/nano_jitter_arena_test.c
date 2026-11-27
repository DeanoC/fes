#include "arena.h"

#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

static void verify(const unsigned char *buffer, size_t count, unsigned char value)
{
    size_t i;
    for (i = 0; i < count; i++)
        assert(buffer[i] == value);
}

static unsigned char *allocate(size_t count, unsigned char value)
{
    unsigned char *p = fes_jent_alloc(count);
    assert(p && (uintptr_t)p % FES_JENT_ARENA_ALIGNMENT == 0);
    verify(p, count, 0);
    memset(p, value, count);
    return p;
}

static void check_exhaustion(void)
{
    const size_t maximum = FES_JENT_ARENA_BYTES - 2U * FES_JENT_ARENA_ALIGNMENT;
    unsigned char *large;
    fes_jent_arena_reset();
    assert(fes_jent_heap_empty() && fes_jent_heap_high_water() == 0);
    assert(!fes_jent_alloc(0) && !fes_jent_alloc((size_t)-1));
    assert(!fes_jent_alloc(maximum+1));
    assert(fes_jent_heap_empty() && fes_jent_heap_high_water() == 0);
    large = allocate(maximum, 0xdb);
    assert(!fes_jent_alloc(1) && fes_jent_heap_high_water() == FES_JENT_ARENA_BYTES);
    verify(large, maximum, 0xdb);
    assert(!fes_jent_heap_empty());
    fes_jent_free(large);
    assert(fes_jent_heap_empty() && fes_jent_heap_high_water() == FES_JENT_ARENA_BYTES);
    verify(large, maximum, 0);
}

static void check_out_of_order(void)
{
    unsigned char *a, *b, *c, *d, *replacement;
    size_t peak;
    int unrelated = 0;
    fes_jent_arena_reset();
    a = allocate(1,0x11);
    b = allocate(517,0x22);
    c = allocate(3901,0x33);
    d = allocate(177,0x44);
    peak = fes_jent_heap_high_water();
    fes_jent_free(NULL);
    fes_jent_free(&unrelated);
    fes_jent_free(c+16);
    verify(c,3901,0x33);
    fes_jent_free(c);
    fes_jent_free(b);
    fes_jent_free(b);
    verify(b,517,0);
    verify(c,3901,0);
    replacement = allocate(4400,0x66);
    assert(replacement == b && fes_jent_heap_high_water() == peak);
    verify(a,1,0x11);
    verify(d,177,0x44);
    fes_jent_arena_reset();
    assert(fes_jent_heap_empty() && fes_jent_heap_high_water() == 0);
    verify(a,1,0);
    verify(replacement,4400,0);
    verify(d,177,0);
}

static void check_fragmentation(void)
{
    unsigned char *blocks[3];
    unsigned char *p;
    size_t i;
    fes_jent_arena_reset();
    for (i = 0; i < 3; i++)
        blocks[i] = allocate(60000,(unsigned char)(i+1));
    fes_jent_free(blocks[0]);
    fes_jent_free(blocks[2]);
    assert(!fes_jent_alloc(100000));
    verify(blocks[1],60000,2);
    fes_jent_free(blocks[1]);
    p = allocate(165000,0x78);
    assert(p == blocks[0]);
    fes_jent_free(p);
    assert(fes_jent_heap_empty());
}

int main(void)
{
    check_exhaustion();
    check_out_of_order();
    check_fragmentation();
    puts("arena allocation, alignment, fragmentation, erasure and reset passed");
    return 0;
}
