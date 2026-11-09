// SPDX-License-Identifier: MIT
// CIA1 joystick port isolation through the real motherboard (fes#1531).
//
// The shell delivers fes.gamepad.ports port 0 (FogCast gamepad player 0) on
// controller_buttons[7:0] and port 1 (player 1) on [15:8]; bits 0-4 are
// Up/Down/Left/Right/A. The C64 reads player 0 as joystick port 2 ($DC00)
// and player 1 as joystick port 1 ($DC01). A RAM program leaves CIA1 as the
// KERNAL does after a key scan (DDRA $FF, PA $FF, DDRB $00) and copies both
// data registers to $02/$03 forever. Each case must read back exactly: a
// switch on one port never appears on the other, and B/Select/Start do not
// reach either port. One case encodes the real wired-AND ghost: port 2 fire
// pulls column 4 low, so a key held in that column reads in $DC01.
#include "Vjoyports_sim_top.h"
#include "Vjoyports_sim_top___024root.h"
#include "verilated.h"
#include <array>
#include <cstdio>
#include <fstream>
#include <string>

static const unsigned char program[] = {
    0x78,             // SEI
    0xa9,0xff,        // LDA #$FF
    0x8d,0x02,0xdc,   // STA $DC02  DDRA: all columns are outputs
    0x8d,0x00,0xdc,   // STA $DC00  PA: all columns high
    0xa9,0x00,        // LDA #$00
    0x8d,0x03,0xdc,   // STA $DC03  DDRB: all rows are inputs
    0xad,0x00,0xdc,   // LDA $DC00
    0x85,0x02,        // STA $02
    0xad,0x01,0xdc,   // LDA $DC01
    0x85,0x03,        // STA $03
    0x4c,0x0e,0x10    // JMP $100E
};

struct Case { const char *name; unsigned buttons, key, dc00, dc01; };

int main(int argc,char **argv) {
    if(argc!=2)return 2;
    // Own firmware supplies only a reset vector into the open RAM program.
    std::array<unsigned char,36864> firmware{};
    firmware[16381]=0x10;
    std::ofstream file(argv[1]);
    for(unsigned byte:firmware) {
        char text[4];std::snprintf(text,sizeof(text),"%02x\n",byte);file<<text;
    }
    file.close();if(!file)return 2;
    std::string option=std::string("+C64_FIRMWARE_HEX=")+argv[1];
    char *args[]={argv[0],option.data()};Verilated::commandArgs(2,args);

    static const char *direction[5]={"up","down","left","right","fire"};
    std::array<Case,32> cases{};unsigned count=0;
    cases[count++]={"idle",0,0,0xff,0xff};
    static char names[10][24];
    for(unsigned bit=0;bit<5;++bit) {
        std::snprintf(names[bit],sizeof(names[bit]),"player0 %s",direction[bit]);
        cases[count++]={names[bit],1u<<bit,0,0xffu^(1u<<bit),0xff};
        std::snprintf(names[5+bit],sizeof(names[5+bit]),"player1 %s",direction[bit]);
        cases[count++]={names[5+bit],1u<<(8+bit),0,0xff,0xffu^(1u<<bit)};
    }
    cases[count++]={"player0 B/Select/Start only",0x00e0,0,0xff,0xff};
    cases[count++]={"player1 B/Select/Start only",0xe000,0,0xff,0xff};
    cases[count++]={"player0 up+fire, player1 right",0x0811,0,0xee,0xf7};
    cases[count++]={"player0 left, player1 down+fire",0x1204,0,0xfb,0xed};
    cases[count++]={"all switches on both",0x1f1f,0,0xe0,0xe0};
    cases[count++]={"key N alone, columns high",0,0x11,0xff,0xff};
    cases[count++]={"player0 fire ghosts key N",0x0010,0x11,0xef,0x7f};

    Vjoyports_sim_top top;top.clk=0;top.reset=1;
    for(unsigned i=0;i<5;++i)top.keyboard_rows[i]=0;
    top.controller_buttons=0;top.eval();
    auto *r=top.rootp;
    auto &ram=r->joyports_sim_top__DOT__machine__DOT__main_ram__DOT__ram;
    for(unsigned i=0;i<sizeof(program);++i)ram[0x1000+i]=program[i];
    auto run=[&](unsigned cycles) {
        for(unsigned i=0;i<cycles;++i){top.clk=0;top.eval();top.clk=1;top.eval();}
    };
    run(400);top.reset=0;
    // Reach the polling loop before the first case.
    bool looping=false;
    for(unsigned i=0;i<200000&&!looping;++i){run(1);looping=top.debug_pc==0x100e;}
    if(!looping){std::printf("C64 joystick ports: program never reached its loop, PC %04x\n",top.debug_pc);return 1;}

    unsigned failures=0;
    for(unsigned n=0;n<count;++n) {
        const Case &c=cases[n];
        for(unsigned i=0;i<5;++i)top.keyboard_rows[i]=0;
        if(c.key)top.keyboard_rows[c.key/32]=1u<<(c.key%32);
        top.controller_buttons=c.buttons;
        // One loop is 17 Phi2 cycles, about 900 system clocks. Settle for
        // more than two loops, then require fresh copies of both registers.
        run(3000);
        ram[2]=0xa5;ram[3]=0xa5;
        run(3000);
        unsigned dc00=ram[2],dc01=ram[3];
        if(dc00!=c.dc00||dc01!=c.dc01) {
            std::printf("C64 joystick ports %s: $DC00 %02x $DC01 %02x, expected %02x %02x\n",
                        c.name,dc00,dc01,c.dc00,c.dc01);
            ++failures;
        }
    }
    std::printf("C64 joystick ports: %u failures across %u isolation cases\n",failures,count);
    return failures?1:0;
}
