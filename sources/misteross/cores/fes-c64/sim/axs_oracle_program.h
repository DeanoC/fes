// SPDX-License-Identifier: MIT
#ifndef FES_AXS_ORACLE_PROGRAM_H
#define FES_AXS_ORACLE_PROGRAM_H
#include <stdint.h>
#include <string.h>
#define AXS_ORACLE_CASES 132
#define AXS_INTERRUPT_PHASES 12
struct AXSTransistorCase { unsigned a,x,immediate,flags,pc,fragment,pushed; };
static struct AXSTransistorCase axs_transistor_case(unsigned index) {
    static const unsigned values[8][3]={{0x17,0x10,0xff},{0xff,0xff,0},{0,0xff,1},{0xa5,0x5a,0},{0x80,0xff,1},{0x7f,0xff,0xff},{0xaa,0xff,0xaa},{0xff,1,0x81}};
    unsigned v=index<128?index/16:0;
    struct AXSTransistorCase c={values[v][0],values[v][1],values[v][2],index&15,0x1100,0,0};
    if(index==128)c.pc=0x11ff;
    if(index==129)c.pc=0xffff;
    if(index>=130){c.pc=0xf88;c.fragment=1;c.pushed=index==130?0:0x80;}
    return c;
}
static void axs_emit(uint8_t *ram,unsigned *at,unsigned value) {ram[(*at)++ & 65535]=value;}
static void axs_transistor_program(uint8_t *ram,unsigned index) {
    struct AXSTransistorCase c=axs_transistor_case(index);memset(ram,0xea,65536);unsigned at=0x1000;
    const uint8_t prefix[]={0xa2,0xfd,0x9a,0xa0,0x7f,0xa9,(uint8_t)(0x30|((c.flags&1)?1:0)|((c.flags&2)?8:0)|((c.flags&4)?4:0)|((c.flags&8)?0x40:0)),0x48,0x28};
    memcpy(ram+at,prefix,sizeof(prefix));at+=sizeof(prefix);
    if(c.fragment){axs_emit(ram,&at,0xa9);axs_emit(ram,&at,c.pushed);axs_emit(ram,&at,0x48);}
    axs_emit(ram,&at,0xa2);axs_emit(ram,&at,c.x);axs_emit(ram,&at,0xa9);axs_emit(ram,&at,c.a);
    unsigned entry=c.pc-(c.fragment?2:0);
    axs_emit(ram,&at,0x4c);axs_emit(ram,&at,entry&255);axs_emit(ram,&at,entry>>8);
    at=entry;
    if(c.fragment){axs_emit(ram,&at,0xa9);axs_emit(ram,&at,0x17);}
    axs_emit(ram,&at,0xcb);axs_emit(ram,&at,c.immediate);
    if(c.fragment){const uint8_t follow[]={0x68,0xf0,2,0xa9,0xe7};for(unsigned i=0;i<sizeof(follow);++i)axs_emit(ram,&at,follow[i]);}
    const uint8_t tail[]={0x8e,0,2,0x8d,1,2,0x8c,2,2,0x08,0x68,0x8d,3,2,0xba,0x8e,4,2,0xa9,0xa5,0x8d,5,2};
    for(unsigned i=0;i<sizeof(tail);++i)axs_emit(ram,&at,tail[i]);
    unsigned end=at;
    axs_emit(ram,&at,0x4c);axs_emit(ram,&at,end&255);axs_emit(ram,&at,end>>8);
    ram[0xfffc]=0;ram[0xfffd]=0x10;
}
static void axs_interrupt_program(uint8_t *ram) {
    memset(ram,0xea,65536);
    const uint8_t prefix[]={0xa2,0xfd,0x9a,0xa9,0x17,0xa2,0x10,0xf8,0x18,0x58,0x4c,0,0x11};
    memcpy(ram+0x1000,prefix,sizeof(prefix));ram[0x1101]=0xcb;ram[0x1102]=0xff;
    ram[0xfffc]=0;ram[0xfffd]=0x10;ram[0xfffe]=0;ram[0xffff]=0x20;ram[0xfffa]=0;ram[0xfffb]=0x21;
}
#endif
