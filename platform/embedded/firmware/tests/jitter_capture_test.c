/* SPDX-License-Identifier: BSD-3-Clause */
/* Deterministic lab fixture for framing, failure handling and sample accounting. */
#include "jitter_capture.h"
#include "nano_jitter.h"
#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static const char *scenario;
static unsigned rows, open_row, reads, total, idle_calls, characters;
static uint32_t state = 1;

int fes_jitter_uart_idle(void)
{
    assert(!open_row);
    idle_calls++;
    return !strcmp(scenario, "uart-busy") ? -1 : 0;
}

int fes_jitter_putc(int c)
{
    assert(!open_row);
    if (!strcmp(scenario, "good") || !strcmp(scenario, "frozen")) {
        assert(total == 1000000 || total == 2000000);
        assert(rows == 1 || rows == 1001);
    }
    if (!strcmp(scenario, "wire") && characters == 512)
        return -1;
    ++characters;
    return putchar(c);
}

int fes_nano_jitter_record_open(void)
{
    assert(!open_row);
    if (!strcmp(scenario, "open"))
        return -42;
    open_row = 1;
    reads = 0;
    ++rows;
    assert(idle_calls == (rows == 1 ? 1U : 2U));
    return 0;
}

int fes_nano_jitter_record_sample(uint64_t *delta)
{
    assert(open_row);
    if (!strcmp(scenario, "sample") && reads == 3)
        return -17;
    ++reads;
    ++total;
    state ^= state << 13;
    state ^= state >> 17;
    state ^= state << 5;
    *delta = !strcmp(scenario, "frozen") ? 0 : state;
    return 0;
}

int fes_nano_jitter_record_close(struct fes_nano_jitter_metrics *m)
{
    assert(open_row);
    open_row = 0;
    assert(reads == (!strcmp(scenario, "sample") ? 3U : (rows == 1 ? 1000000U : 1000U)));
    memset(m, 0, sizeof(*m));
    m->osr = FES_JITTER_OSR;
    m->flags = 0x40400130U;
    m->memory_bytes = FES_JITTER_MEMORY_BYTES;
    m->hash_loops = FES_JITTER_HASH_LOOPS;
    if (!strcmp(scenario, "flags") && rows == 2)
        m->flags ^= 0x100U;
    if (!strcmp(scenario, "profile"))
        m->internal_timer = 1;
    if (!strcmp(scenario, "health"))
        m->health = 2;
    if (!strcmp(scenario, "reinit"))
        m->reinitializations = 1;
    return !strcmp(scenario, "close") ? -13 : 0;
}

int main(int argc, char **argv)
{
    assert(argc == 2);
    scenario = argv[1];
    int rc = fes_jitter_capture();
    assert(!open_row);
    for (unsigned i = 0; i < FES_JITTER_CAPTURE_BYTES; ++i)
        assert(!fes_jitter_capture_buffer[i]);
    int success = !strcmp(scenario, "good") || !strcmp(scenario, "frozen");
    assert((rc == 0) == success);
    if (success)
        assert(rows == 1001 && total == 2000000 && idle_calls == 2);
    unsigned before = characters;
    assert(fes_jitter_capture() < 0 && before == characters);
    return 0;
}
