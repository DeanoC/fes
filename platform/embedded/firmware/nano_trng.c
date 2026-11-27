/* SPDX-License-Identifier: BSD-3-Clause */
/* The Agilex SDM supplies boot entropy before BL31 admits nonsecure callers.
 * Standard TRNG calls consume this finite, single-use pool and never access
 * the SDM while the FPGA programming service owns the mailbox. */
#include <common/debug.h>
#include <common/runtime_svc.h>
#include <lib/mmio.h>
#include <lib/utils.h>
#include <plat/common/plat_trng.h>
#include "socfpga_fcs.h"
#include "socfpga_mailbox.h"

/* The vendor's verbose mailbox logger dumps response payloads. */
#if LOG_LEVEL >= LOG_LEVEL_VERBOSE
#error "SDM boot entropy requires payload logging disabled"
#endif

int fes_nano_sdm_control_mailbox_idle(void);

DEFINE_SVC_UUID2(fes_sdm_rng_uuid, 0x335209fc, 0x3df8, 0x4090, 0x89, 0x3a,
                 0x72, 0xd6, 0xb4, 0x4d, 0x08, 0x19);
/* This region is file-backed inside BL31's already reserved private DDR. */
#define BOOT_DATA __attribute__((section(".data.fes_boot_entropy")))
uuid_t plat_trng_uuid BOOT_DATA;

/* Separate from synchronous control (job 15) and async DATA (jobs 0..13). */
#define ENTROPY_JOB 14U
#define BOOT_BLOCKS 4U
_Static_assert(MBOX_MAX_IND_JOB_ID == ENTROPY_JOB && MBOX_JOB_ID == 15U,
               "reserved mailbox job changed");
_Static_assert(FCS_RANDOM_WORD_SIZE == 8U, "SDM RNG must return 256 bits");

struct {
    uint32_t words[BOOT_BLOCKS][FCS_RANDOM_WORD_SIZE];
    unsigned consumed;
    bool attempted;
    bool available;
} fes_nano_boot_entropy BOOT_DATA;

/* bl31_main runs platform and SDM setup before standard services, and only
 * then enters BL33. No outstanding command or stale response may be reused. */
void plat_entropy_setup(void)
{
    if (fes_nano_boot_entropy.attempted)
        return;
    fes_nano_boot_entropy.attempted = true;
    if (!fes_nano_sdm_control_mailbox_idle())
        return;

    for (unsigned block = 0; block < BOOT_BLOCKS; ++block) {
        unsigned length = FCS_RANDOM_WORD_SIZE;
        uint32_t cin = mmio_read_32(MBOX_OFFSET + MBOX_CIN);
        uint32_t rout = mmio_read_32(MBOX_OFFSET + MBOX_ROUT);
        int status = mailbox_send_cmd(ENTROPY_JOB, MBOX_FCS_RANDOM_GEN, NULL, 0,
                                      CMD_CASUAL, fes_nano_boot_entropy.words[block], &length);

        /* A late reply must never alias a programming transaction. Only a
         * completely consumed response or a proven non-submission can return
         * control to BL33. Do not clear or reinitialize an uncertain FIFO. */
        if (!fes_nano_sdm_control_mailbox_idle())
            panic();
        uint32_t next_cin = mmio_read_32(MBOX_OFFSET + MBOX_CIN);
        uint32_t next_rout = mmio_read_32(MBOX_OFFSET + MBOX_ROUT);
        if (status && next_cin == cin && next_rout == rout)
            goto unavailable;
        if (length > FCS_RANDOM_WORD_SIZE ||
            next_cin != (cin + 1U) % MBOX_CMD_BUFFER_SIZE ||
            next_rout != (rout + 1U + length) % MBOX_RESP_BUFFER_SIZE)
            panic();
        if (status != MBOX_RET_OK || length != FCS_RANDOM_WORD_SIZE)
            goto unavailable;
        uint32_t nonzero = 0U;
        for (unsigned i = 0; i < length; ++i)
            nonzero |= fes_nano_boot_entropy.words[block][i];
        if (!nonzero)
            goto unavailable;
        for (unsigned prior = 0; prior < block; ++prior) {
            uint32_t different = 0U;
            for (unsigned i = 0; i < length; ++i)
                different |= fes_nano_boot_entropy.words[block][i] ^ fes_nano_boot_entropy.words[prior][i];
            if (!different)
                goto unavailable;
        }
    }
    fes_nano_boot_entropy.available = true;
    plat_trng_uuid = fes_sdm_rng_uuid;
    return;

unavailable:
    zeromem(fes_nano_boot_entropy.words, sizeof(fes_nano_boot_entropy.words));
}

/* TRNG's generic entropy pool holds its spinlock across every invocation. */
bool plat_get_entropy(uint64_t *out)
{
    if (!out || !fes_nano_boot_entropy.available || fes_nano_boot_entropy.consumed == BOOT_BLOCKS * FCS_RANDOM_WORD_SIZE)
        return false;
    unsigned n = fes_nano_boot_entropy.consumed;
    uint32_t *pair = &fes_nano_boot_entropy.words[n / FCS_RANDOM_WORD_SIZE][n % FCS_RANDOM_WORD_SIZE];
    *out = ((uint64_t)pair[1] << 32) | pair[0];
    pair[0] = pair[1] = 0;
    fes_nano_boot_entropy.consumed += 2U;
    return true;
}
