// SPDX-License-Identifier: MIT
#ifndef FES_C64_READY_PROGRAM_H
#define FES_C64_READY_PROGRAM_H
#include <stdint.h>
// An open raster-read stabilizer followed by a visible register write.
static const uint8_t ready_code[] = {
    0xa2,13,           // LDX #13
    0xad,0x12,0xd0,    // LDA $D012
    0xcd,0x12,0xd0,    // CMP $D012
    0xf0,1,0xea,       // BEQ past NOP
    0x8e,0x11,0xd0,    // STX $D011
    0x4c,14,0x10       // JMP $100E
};
#define READY_FIRST_PHASE 12
#define READY_LAST_PHASE 62
#endif
