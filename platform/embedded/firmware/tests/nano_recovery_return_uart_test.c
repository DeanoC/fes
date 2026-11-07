/* SPDX-License-Identifier: BSD-3-Clause */
/* Compile the actual helper with modeled UART registers and a trapped park. */
#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <setjmp.h>
static int mode, polls, writes;
static char output[1024];
static jmp_buf parked;
static void test_park(void) __attribute__((noreturn));
static void test_park(void) { longjmp(parked, 1); }
#define FES_RETURN_PARK() test_park()
uint32_t readl(const void *address)
{
    assert((uintptr_t)address == 0x10c02014); polls++;
    return mode == 1 ? 0 : mode == 2 ? 0x20 : 0x60;
}
void writel(uint32_t value, void *address)
{
    assert((uintptr_t)address == 0x10c02000 && value < 128);
    assert(writes < (int)sizeof(output)-1); output[writes++] = value;
}
#include "helper.inc"
int main(int argc, char **argv)
{
    assert(argc == 2); mode = atoi(argv[1]);
    bool failed = setjmp(parked);
    if (!failed) {
        if (mode == 5) { fes_recovery_return_mark("inactive"); fes_recovery_return_end(); }
        else {
            fes_recovery_return_begin();
            if (mode == 3) { char text[97]; memset(text, 'a', 96); text[96] = 0; fes_recovery_return_mark(text); }
            if (mode == 4) fes_recovery_return_begin();
            fes_recovery_return_end();
            fes_recovery_return_mark("inactive");
        }
    }
    if (mode == 0) {
        assert(!failed && !strcmp(output,"FES_RECOVERY_RETURN phase=begin\r\nFES_RECOVERY_RETURN phase=command-return\r\n"));
        assert(polls == writes+2);
    } else if (mode == 1) assert(failed && polls == 100000 && writes == 0);
    else if (mode == 2) assert(failed && polls == writes+100000 && strstr(output,"phase=begin\r\n"));
    else if (mode == 3) assert(failed && writes == (int)strlen("FES_RECOVERY_RETURN phase=begin\r\nFES_RECOVERY_RETURN phase=")+96);
    else if (mode == 4) assert(failed && writes == (int)strlen("FES_RECOVERY_RETURN phase=begin\r\n") && polls == writes+1);
    else { assert(mode == 5); assert(!failed && !writes && !polls); }
    printf("PASS mode=%d failed=%d polls=%d writes=%d\n",mode,failed,polls,writes);
    return 0;
}
