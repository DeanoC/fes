// SPDX-License-Identifier: MIT
#ifndef FES_C64_PUSH_PROGRAM_H
#define FES_C64_PUSH_PROGRAM_H
#include <stdint.h>
#include <string.h>
static const struct { unsigned opcode,pc,sp,a,carry,decimal; } push_cases[]={
    {0x08,0x1000,0xfd,0x0d,0,0}, {0x48,0x1000,0xfd,0x0d,0,0},
    {0x08,0x10ff,0x00,0x00,1,1}, {0x48,0x10ff,0x00,0x80,1,1},
    {0x08,0x01fc,0xfd,0x0d,0,0}, {0x48,0x01fc,0xfd,0x0d,0,0},
    {0x08,0x1000,0x80,0x80,1,0}, {0x48,0x1000,0x80,0x00,0,1}
};
#define PUSH_CASES (sizeof(push_cases)/sizeof(*push_cases))
static void push_setup_program(uint8_t *memory,unsigned pc,unsigned sp,unsigned a,unsigned carry,unsigned decimal) {
    memset(memory,0xea,65536);
    uint8_t setup[]={0xa2,(uint8_t)sp,0x9a,0xa9,(uint8_t)a,
        (uint8_t)(carry?0x38:0x18),(uint8_t)(decimal?0xf8:0xd8),0x4c,(uint8_t)pc,(uint8_t)(pc>>8)};
    memcpy(memory+0x0f00,setup,sizeof(setup));memory[0xfffc]=0;memory[0xfffd]=0x0f;
}
static void push_bus_program(uint8_t *memory,unsigned scenario) {
    unsigned pc=push_cases[scenario].pc;
    push_setup_program(memory,pc,push_cases[scenario].sp,push_cases[scenario].a,
        push_cases[scenario].carry,push_cases[scenario].decimal);
    // Preserve A immediately; push/copy P before TSX changes its N/Z flags.
    // The last marker records S after the first push and the balanced PHP/PLA.
    uint8_t code[]={(uint8_t)push_cases[scenario].opcode,0x8d,1,2,0x08,0x68,0x8d,2,2,0xba,0x8e,0,2,0x4c,
        (uint8_t)(pc+13),(uint8_t)((pc+13)>>8)};
    memcpy(memory+pc,code,sizeof(code));
}
static void push_dma_program(uint8_t *memory,unsigned opcode) {
    push_setup_program(memory,0x1000,0xfd,13,0,0);
    uint8_t code[]={(uint8_t)opcode,0x8d,0x20,0xd0,0x4c,4,0x10};
    memcpy(memory+0x1000,code,sizeof(code));
}
#endif
