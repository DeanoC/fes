// SPDX-License-Identifier: MIT
#ifndef FES_C64_IRQ_PROGRAM_H
#define FES_C64_IRQ_PROGRAM_H
#include <stdint.h>
#include <string.h>
#define IRQ_SCENARIOS 10
static const char *const irq_names[IRQ_SCENARIOS] = {
    "nop", "cli", "sei", "bcc_loop", "beq_untaken", "plp", "rti",
    "bcc_page", "bcc_forward", "absolute_rmw"
};
static void irq_program(uint8_t *ram, unsigned scenario) {
    memset(ram, 0xea, 65536);
    const uint8_t prefix[] = {0xa2,0xfd,0x9a,0xa9,1,0x18,0xd8,0x58};
    memcpy(ram+0x1000,prefix,sizeof(prefix));
    unsigned at=0x1000+sizeof(prefix);
    if(scenario==1)ram[at++]=0x78;
    if(scenario==5) {
        const uint8_t setup[]={0xa9,0x20,0x48,0x78};
        memcpy(ram+at,setup,sizeof(setup));at+=sizeof(setup);
    }
    if(scenario==6) {
        const uint8_t setup[]={0xa9,0x11,0x48,0xa9,0x10,0x48,0xa9,0x20,0x48,0x78};
        memcpy(ram+at,setup,sizeof(setup));at+=sizeof(setup);
    }
    ram[at++]=0x4c;ram[at++]=0;ram[at]=0x11;
    switch(scenario) {
        case 1:ram[0x1100]=0x58;break;
        case 2:ram[0x1100]=0x78;break;
        case 3:ram[0x1100]=0x90;ram[0x1101]=0xfe;break;
        case 4:ram[0x1100]=0xf0;ram[0x1101]=0;break;
        case 5:ram[0x1100]=0x28;break;
        case 6:ram[0x1100]=0x40;break;
        case 7:ram[0x1100]=0x90;ram[0x1101]=0xfd;break;
        case 8:ram[0x1100]=0x90;ram[0x1101]=2;break;
        case 9:{const uint8_t code[]={0xad,0,0x80,0x8d,1,0x90,0xee,2,0x90};
            memcpy(ram+0x1100,code,sizeof(code));break;}
    }
    ram[0xfffc]=0;ram[0xfffd]=0x10;ram[0xfffe]=0;ram[0xffff]=0x20;
}
#endif
