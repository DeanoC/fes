// SPDX-License-Identifier: MIT
#ifndef FES_C64_JSR_PROGRAM_H
#define FES_C64_JSR_PROGRAM_H
#include <stdint.h>
#include <string.h>
static const uint8_t jsr_setup[]={0xa9,13,0xa2,0xfd,0x9a,0x4c,0,0x10};
static const uint8_t jsr_code[]={0x20,0x10,0x10};
static const uint8_t jsr_target[]={0x8d,0x20,0xd0,0x4c,0x13,0x10};
static const struct { uint16_t pc; uint8_t sp; } jsr_cases[]={
    {0x1000,0xfd}, {0x10fe,0x00}, {0x01fb,0xfd}
};
static void jsr_bus_program(uint8_t *memory,unsigned scenario) {
    memset(memory,0xea,65536);
    memcpy(memory+0x0f00,jsr_setup,sizeof(jsr_setup));
    memory[0x0f03]=jsr_cases[scenario].sp;
    memory[0x0f06]=jsr_cases[scenario].pc&255;
    memory[0x0f07]=jsr_cases[scenario].pc>>8;
    memory[jsr_cases[scenario].pc]=0x20;
    memory[jsr_cases[scenario].pc+1]=0x45;
    memory[jsr_cases[scenario].pc+2]=0x23;
    memory[0xfffc]=0;memory[0xfffd]=0x0f;
}
#endif
