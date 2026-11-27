/* SPDX-License-Identifier: BSD-3-Clause */
#include <assert.h>
#include <setjmp.h>
#include <string.h>

#include "../nano_trng.c"

enum fault {
    NONE, INITIAL_BUSY, NOT_SUBMITTED, REJECTED, LOST, WRONG_CIN, WRONG_ROUT,
    OVER_CAPACITY, SHORT, WRONG_SESSION, WRONG_CONTEXT, ALL_ZERO, REPEATED
};

struct scenario {
    enum fault fault;
    unsigned command, occurrence, expected_opens, expected_draws, expected_closes;
    bool panic;
};

enum { OPEN = 0xa0, DRAW = 0x80, CLOSE = 0xa1, BLOCK_WORDS = 8, HEADER_WORDS = 3 };
static const uint32_t session = 0x18209256U;
static const struct scenario cases[] = {
    {NONE, 0, 0, 1, 4, 1, false},
    {INITIAL_BUSY, 0, 0, 0, 0, 0, false},
    {NOT_SUBMITTED, OPEN, 1, 1, 0, 0, false},
    {REJECTED, OPEN, 1, 1, 0, 0, false},
    {SHORT, OPEN, 1, 1, 0, 0, true},
    {LOST, OPEN, 1, 1, 0, 0, true},
    {WRONG_CIN, OPEN, 1, 1, 0, 0, true},
    {OVER_CAPACITY, OPEN, 1, 1, 0, 0, true},
    {NOT_SUBMITTED, DRAW, 2, 1, 2, 1, false},
    {REJECTED, DRAW, 2, 1, 2, 1, false},
    {SHORT, DRAW, 2, 1, 2, 1, false},
    {WRONG_SESSION, DRAW, 3, 1, 3, 1, false},
    {WRONG_CONTEXT, DRAW, 4, 1, 4, 1, false},
    {ALL_ZERO, DRAW, 3, 1, 3, 1, false},
    {REPEATED, DRAW, 3, 1, 3, 1, false},
    {OVER_CAPACITY, DRAW, 2, 1, 2, 0, true},
    {LOST, DRAW, 2, 1, 2, 0, true},
    {WRONG_ROUT, DRAW, 2, 1, 2, 0, true},
    {NOT_SUBMITTED, CLOSE, 1, 1, 4, 1, true},
    {REJECTED, CLOSE, 1, 1, 4, 1, true},
    {LOST, CLOSE, 1, 1, 4, 1, true},
    {WRONG_CIN, CLOSE, 1, 1, 4, 1, true},
};

static const struct scenario *current;
static unsigned opens, draws, closes, observations, cin, rout;
static bool session_open, mailbox_idle;
static uint32_t contexts[4];
static jmp_buf trapped;

void test_panic(void) { longjmp(trapped, 1); }

int fes_nano_sdm_control_mailbox_idle(void)
{
    ++observations;
    return mailbox_idle;
}

uint32_t mmio_read_32(uintptr_t reg)
{
    ++observations;
    assert(reg == MBOX_CIN || reg == MBOX_ROUT);
    return reg == MBOX_CIN ? cin : rout;
}

int mailbox_send_cmd(uint32_t job, uint32_t cmd, uint32_t *args, unsigned nargs,
                     uint32_t urgent, uint32_t *response, unsigned *length)
{
    assert(job == 14 && urgent == 0 && mailbox_idle);
    assert(!fes_nano_boot_entropy.available && !plat_trng_uuid.time_low[0]);
    unsigned occurrence, words;
    if (cmd == OPEN) {
        assert(!session_open && !opens && !args && !nargs);
        assert(response && length && *length == 1);
        occurrence = ++opens;
        words = 1;
    } else if (cmd == DRAW) {
        assert(session_open && !closes && draws < 4 && args && nargs == 4);
        assert(args[0] == session && args[1] && args[2] == 0x05000000U && args[3] == 32);
        for (unsigned i = 0; i < draws; ++i)
            assert(args[1] != contexts[i]);
        contexts[draws] = args[1];
        assert(response && length && *length == HEADER_WORDS + BLOCK_WORDS);
        occurrence = ++draws;
        words = HEADER_WORDS + BLOCK_WORDS;
    } else {
        assert(cmd == CLOSE && session_open && !closes && args && nargs == 1 && args[0] == session);
        assert(!length || !*length);
        occurrence = ++closes;
        words = 0;
    }

    enum fault inject = cmd == current->command && occurrence == current->occurrence ? current->fault : NONE;
    if (inject == NOT_SUBMITTED)
        return -4;
    cin = (cin + 1 + nargs) % MBOX_CMD_BUFFER_SIZE;
    if (inject == LOST || inject == OVER_CAPACITY) {
        /* The production V3 parser retains an oversized response before copying. */
        mailbox_idle = false;
        return -2047;
    }
    if (inject == REJECTED) {
        words = 0;
    } else if (cmd == OPEN) {
        session_open = true;
        response[0] = session;
    } else if (cmd == DRAW) {
        response[0] = inject == WRONG_SESSION ? session ^ 1U : session;
        response[1] = inject == WRONG_CONTEXT ? contexts[0] : args[1];
        response[2] = 0x05000000U;
        unsigned block = inject == REPEATED ? 1 : draws;
        for (unsigned i = 0; i < BLOCK_WORDS; ++i)
            response[HEADER_WORDS + i] = inject == ALL_ZERO ? 0 : block * 100 + i;
    } else {
        session_open = false;
    }
    if (inject == SHORT)
        --words;
    if (length)
        *length = words;
    rout = (rout + 1 + words) % MBOX_RESP_BUFFER_SIZE;
    if (inject == WRONG_CIN)
        cin = (cin + 1) % MBOX_CMD_BUFFER_SIZE;
    if (inject == WRONG_ROUT)
        rout = (rout + 1) % MBOX_RESP_BUFFER_SIZE;
    return inject == REJECTED ? -1 : 0;
}

static void run(const struct scenario *test)
{
    current = test;
    memset(&fes_nano_boot_entropy, 0, sizeof(fes_nano_boot_entropy));
    memset(&plat_trng_uuid, 0, sizeof(plat_trng_uuid));
    memset(contexts, 0, sizeof(contexts));
    opens = draws = closes = observations = 0;
    session_open = false;
    mailbox_idle = test->fault != INITIAL_BUSY;
    cin = MBOX_CMD_BUFFER_SIZE - 1;
    rout = MBOX_RESP_BUFFER_SIZE - 2;

    bool panicked;
    if (setjmp(trapped) == 0) {
        plat_entropy_setup();
        panicked = false;
    } else {
        panicked = true;
    }
    assert(panicked == test->panic);
    assert(opens == test->expected_opens && draws == test->expected_draws && closes == test->expected_closes);
    if (panicked) {
        assert(!plat_trng_uuid.time_low[0]);
        return;
    }
    assert(!session_open);
    unsigned before = opens + draws + closes + observations;
    plat_entropy_setup();
    uint64_t returned = 0;
    if (test->fault == NONE) {
        assert(plat_trng_uuid.time_low[0] && !plat_get_entropy(NULL));
        for (unsigned i = 0; i < 16; ++i) {
            uint32_t first = (1 + i / 4) * 100 + (i % 4) * 2;
            assert(plat_get_entropy(&returned));
            assert(returned == (((uint64_t)(first + 1) << 32) | first));
        }
    } else {
        assert(!plat_trng_uuid.time_low[0]);
    }
    assert(!plat_get_entropy(&returned));
    for (unsigned i = 0; i < BOOT_BLOCKS; ++i)
        for (unsigned j = 0; j < BLOCK_WORDS; ++j)
            assert(!fes_nano_boot_entropy.words[i][j]);
    assert(opens + draws + closes + observations == before);
}

int main(void)
{
    for (unsigned i = 0; i < sizeof(cases) / sizeof(cases[0]); ++i)
        run(&cases[i]);
    return 0;
}
