/* SPDX-License-Identifier: BSD-3-Clause */
#include <assert.h>
#include <string.h>
#include "../nano_boot_jitter.c"

static unsigned int fault;
static int draws, flushed;

void console_flush(void)
{
    flushed++;
}

int fes_nano_jitter_seed(uint8_t *out, size_t n, struct fes_nano_jitter_metrics *m)
{
    assert(flushed == 1 && !draws && n == 128 && !plat_trng_uuid.time_low[0]);
    draws++;
    memset(out, 0x5a, n);
    m->osr = 3;
    m->status = fault == 2 ? -1 : 0;
    m->health = fault == 3;
    m->reinitializations = fault == 4;
    return fault == 1 ? -1 : 0;
}

int main(void)
{
    for (fault = 0; fault < 5; fault++) {
        uint64_t word = 0;
        draws = flushed = 0;
        memset(&fes_nano_boot_entropy, 0, sizeof(fes_nano_boot_entropy));
        memset(&plat_trng_uuid, 0, sizeof(plat_trng_uuid));
        plat_entropy_setup();
        assert(!draws && !plat_get_entropy(&word));
        assert(fes_nano_boot_jitter() == 0 && draws == 1);
        assert(fes_nano_boot_jitter() != 0 && draws == 1);
        assert(!plat_get_entropy(NULL));
        if (fault == 0) {
            assert(plat_trng_uuid.time_low[0]);
            for (unsigned i = 0; i < 16; ++i) {
                assert(plat_get_entropy(&word) && word == UINT64_C(0x5a5a5a5a5a5a5a5a));
                assert(!fes_nano_boot_entropy.words[i]);
            }
        } else {
            assert(!plat_trng_uuid.time_low[0]);
        }
        assert(!plat_get_entropy(&word));
        for (unsigned i = 0; i < 16; ++i)
            assert(!fes_nano_boot_entropy.words[i]);
    }
}
