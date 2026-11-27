/* SPDX-License-Identifier: BSD-3-Clause */
#include <common/debug.h>
#include <drivers/console.h>
#include "nano_jitter.h"

int fes_nano_boot_jitter(void);

#ifdef FES_NANO_JITTER_CAPTURE
#include "jitter_capture.h"

int fes_jitter_putc(int c)
{
    return console_putc(c);
}

int fes_jitter_uart_idle(void)
{
    console_flush();
    return 0;
}

int fes_nano_boot_jitter(void)
{
    int ret = fes_jitter_capture();
    NOTICE("FES_NANO_JITTER capture-terminal status=%d\n", ret);
    console_flush();
    return -1; /* Dedicated measurement stops at the cold entry's existing park. */
}
#else
#include <common/runtime_svc.h>
#include <lib/utils.h>
#include <plat/common/plat_trng.h>

#define BOOT_DATA __attribute__((section(".data.fes_boot_entropy")))
DEFINE_SVC_UUID2(fes_jitter_rng_uuid, 0x0428441e, 0x94f6, 0x47ea, 0xa8, 0x65,
                 0x5e, 0x61, 0x95, 0xba, 0xaf, 0xc4);
uuid_t plat_trng_uuid BOOT_DATA;
struct {
    uint64_t words[FES_JITTER_SEED_BYTES / sizeof(uint64_t)];
    unsigned int next;
    bool attempted, available;
} fes_nano_boot_entropy BOOT_DATA;

void plat_entropy_setup(void)
{
    /* During runtime service setup, EL3's private cache mapping is not live.
     * The zero-initialized provider UUID keeps TRNG unavailable until collection. */
}

int fes_nano_boot_jitter(void)
{
    if (fes_nano_boot_entropy.attempted)
        return -1;
    fes_nano_boot_entropy.attempted = true;
    console_flush();
    struct fes_nano_jitter_metrics metrics = {0};
    int ret = fes_nano_jitter_seed((uint8_t *)fes_nano_boot_entropy.words,
                                   sizeof(fes_nano_boot_entropy.words), &metrics);
    if (ret || metrics.status || metrics.health || metrics.reinitializations) {
        zeromem(fes_nano_boot_entropy.words, sizeof(fes_nano_boot_entropy.words));
        NOTICE("FES_NANO_JITTER unavailable call=%d status=%d health=%u retries=%u\n",
               ret, metrics.status, metrics.health, metrics.reinitializations);
        return 0; /* Boot the local launcher with its network endpoint unavailable. */
    }
    fes_nano_boot_entropy.available = true;
    plat_trng_uuid = fes_jitter_rng_uuid;
    NOTICE("FES_NANO_JITTER ready bytes=%u osr=%u\n", FES_JITTER_SEED_BYTES, metrics.osr);
    return 0;
}

/* The standard TF-A entropy pool serializes consumers with its spinlock. */
bool plat_get_entropy(uint64_t *out)
{
    unsigned int n = fes_nano_boot_entropy.next;
    if (!out || !fes_nano_boot_entropy.available || n >= ARRAY_SIZE(fes_nano_boot_entropy.words))
        return false;
    *out = fes_nano_boot_entropy.words[n];
    fes_nano_boot_entropy.words[n] = 0;
    fes_nano_boot_entropy.next++;
    return true;
}
#endif
