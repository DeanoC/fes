/* SPDX-License-Identifier: BSD-3-Clause */
#include <assert.h>
#include <setjmp.h>
#include <string.h>

#include "../nano_trng.c"

enum fault { GOOD, BUSY, ZERO, REPEAT, SHORT, UNSUPPORTED, NOT_SUBMITTED, LOST, DELAYED, OVERLONG };
static enum fault fault;
static unsigned calls, reads, idle_checks;
static unsigned cin, rout;
static jmp_buf trapped;

void test_panic(void) { longjmp(trapped, 1); }
int fes_nano_sdm_control_mailbox_idle(void)
{
    ++idle_checks;
    return fault != BUSY && !(calls && fault == LOST);
}
uint32_t mmio_read_32(uintptr_t reg)
{
    ++reads;
    assert(reg == MBOX_CIN || reg == MBOX_ROUT);
    return reg == MBOX_CIN ? cin : rout;
}
int mailbox_send_cmd(uint32_t job, uint32_t cmd, uint32_t *args, unsigned nargs,
                     uint32_t urgent, uint32_t *response, unsigned *length)
{
    assert(job == 14 && cmd == 0x80 && !args && !nargs && !urgent && *length == 8);
    assert(response == fes_nano_boot_entropy.words[calls]);
    ++calls;
    if (fault == NOT_SUBMITTED)
        return -4;
    cin = (cin + 1) % MBOX_CMD_BUFFER_SIZE;
    if (fault == DELAYED)
        return -2047;
    if (fault == SHORT || fault == UNSUPPORTED || fault == OVERLONG)
        *length = fault == SHORT ? 7 : fault == OVERLONG ? 9 : 0;
    for (unsigned i = 0; i < *length && i < 8; ++i)
        response[i] = fault == ZERO ? 0 : (fault == REPEAT ? 1 : calls) * 100 + i;
    rout = (rout + 1 + *length) % MBOX_RESP_BUFFER_SIZE;
    return fault == UNSUPPORTED ? -1 : 0;
}

int main(void)
{
    for (fault = GOOD; fault <= OVERLONG; ++fault) {
        memset(&fes_nano_boot_entropy, 0, sizeof(fes_nano_boot_entropy));
        memset(&plat_trng_uuid, 0, sizeof(plat_trng_uuid));
        calls = reads = idle_checks = 0;
        cin = MBOX_CMD_BUFFER_SIZE - 1;
        rout = MBOX_RESP_BUFFER_SIZE - 2;
        if (setjmp(trapped)) {
            assert(fault == LOST || fault == DELAYED || fault == OVERLONG);
            continue;
        }
        plat_entropy_setup();
        assert(fault != LOST && fault != DELAYED && fault != OVERLONG);
        unsigned before = calls + reads + idle_checks;
        plat_entropy_setup(); /* No re-use after success, failure or exhaustion. */
        uint64_t returned = 1;
        if (fault == GOOD) {
            assert(calls == BOOT_BLOCKS);
            assert(!plat_get_entropy(NULL));
            for (unsigned i = 0; i < 16; ++i) {
                uint32_t first = (1 + i / 4) * 100 + (i % 4) * 2;
                assert(plat_get_entropy(&returned));
                assert(returned == (((uint64_t)(first + 1) << 32) | first));
            }
        } else {
            assert(!plat_trng_uuid.time_low[0]);
            assert(calls == (fault == BUSY ? 0U : fault == REPEAT ? 2U : 1U));
        }
        assert(!plat_get_entropy(&returned));
        for (unsigned i = 0; i < BOOT_BLOCKS; ++i)
            for (unsigned j = 0; j < FCS_RANDOM_WORD_SIZE; ++j)
                assert(!fes_nano_boot_entropy.words[i][j]);
        assert(calls + reads + idle_checks == before);
    }
    return 0;
}
