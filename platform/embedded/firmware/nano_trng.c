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

/* A completed command must retire exactly its request and reply. A late
 * reply must never alias a later FPGA programming transaction. */
static int entropy_command(unsigned cmd, uint32_t *args, unsigned nargs,
                           uint32_t *reply, unsigned *words)
{
    unsigned capacity = *words;
    uint32_t cin = mmio_read_32(MBOX_OFFSET + MBOX_CIN);
    uint32_t rout = mmio_read_32(MBOX_OFFSET + MBOX_ROUT);
    int status = mailbox_send_cmd(ENTROPY_JOB, cmd, args, nargs, CMD_CASUAL, reply, words);
    if (!fes_nano_sdm_control_mailbox_idle())
        panic();
    uint32_t next_cin = mmio_read_32(MBOX_OFFSET + MBOX_CIN);
    uint32_t next_rout = mmio_read_32(MBOX_OFFSET + MBOX_ROUT);
    if (status && next_cin == cin && next_rout == rout) {
        *words = 0; /* The command was never submitted. */
    } else if (*words > capacity ||
               next_cin != (cin + 1U + nargs) % MBOX_CMD_BUFFER_SIZE ||
               next_rout != (rout + 1U + *words) % MBOX_RESP_BUFFER_SIZE) {
        panic();
    }
    if (status != MBOX_RET_OK)
        NOTICE("FES_NANO_TRNG command=%x status=%d words=%u\n", cmd, status, *words);
    return status;
}

/* Agilex 5 FCS uses a crypto session. The three-word reply prefix carries
 * the session ID, context ID and crypto parameters. It is never entropy. */
static bool entropy_block(uint32_t session, unsigned block)
{
    enum { HEADER_WORDS = 3, REPLY_WORDS = HEADER_WORDS + FCS_RANDOM_WORD_SIZE };
    uint32_t args[] = {session, block + 1U,
        (FCS_CS_FIELD_FLAG_INIT | FCS_CS_FIELD_FLAG_FINALIZE) << FCS_CS_FIELD_FLAG_OFFSET,
        FCS_RANDOM_WORD_SIZE * sizeof(uint32_t)};
    uint32_t reply[REPLY_WORDS] = {0};
    unsigned words = REPLY_WORDS;
    bool valid = false;
    if (entropy_command(MBOX_FCS_RANDOM_GEN, args, 4U, reply, &words) != MBOX_RET_OK)
        goto done;
    if (words != REPLY_WORDS || reply[0] != session || reply[1] != args[1]) {
        NOTICE("FES_NANO_TRNG rejected-reply block=%u words=%u\n", block, words);
        goto done;
    }

    uint32_t *data = reply + HEADER_WORDS;
    uint32_t nonzero = 0U;
    for (unsigned i = 0; i < FCS_RANDOM_WORD_SIZE; ++i)
        nonzero |= data[i];
    if (!nonzero)
        goto done;
    for (unsigned prior = 0; prior < block; ++prior) {
        uint32_t different = 0U;
        for (unsigned i = 0; i < FCS_RANDOM_WORD_SIZE; ++i)
            different |= data[i] ^ fes_nano_boot_entropy.words[prior][i];
        if (!different)
            goto done;
    }
    for (unsigned i = 0; i < FCS_RANDOM_WORD_SIZE; ++i)
        fes_nano_boot_entropy.words[block][i] = data[i];
    valid = true;
done:
    zeromem(reply, sizeof(reply));
    return valid;
}

/* bl31_main runs platform and SDM setup before standard services. Complete
 * and close the session before making any random bytes available to BL33. */
void plat_entropy_setup(void)
{
    if (fes_nano_boot_entropy.attempted)
        return;
    fes_nano_boot_entropy.attempted = true;
    if (!fes_nano_sdm_control_mailbox_idle()) {
        NOTICE("FES_NANO_TRNG unavailable mailbox-busy\n");
        return;
    }

    uint32_t session = 0;
    unsigned words = 1;
    if (entropy_command(MBOX_FCS_OPEN_CS_SESSION, NULL, 0U, &session, &words) != MBOX_RET_OK)
        return;
    if (words != 1U)
        panic(); /* An opened session cannot be closed without its ID. */

    unsigned block = 0;
    while (block < BOOT_BLOCKS && entropy_block(session, block))
        ++block;
    words = 0;
    if (entropy_command(MBOX_FCS_CLOSE_CS_SESSION, &session, 1U, NULL, &words) != MBOX_RET_OK)
        panic();
    if (block != BOOT_BLOCKS) {
        zeromem(fes_nano_boot_entropy.words, sizeof(fes_nano_boot_entropy.words));
        NOTICE("FES_NANO_TRNG unavailable block=%u\n", block);
        return;
    }
    fes_nano_boot_entropy.available = true;
    plat_trng_uuid = fes_sdm_rng_uuid;
    NOTICE("FES_NANO_TRNG ready bytes=%u\n", (unsigned)sizeof(fes_nano_boot_entropy.words));
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
