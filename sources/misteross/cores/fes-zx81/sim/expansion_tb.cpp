// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vexpansion_machine.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

static void tick(Vexpansion_machine &dut) {
    dut.clk_sys = 1;
    dut.eval();
    dut.clk_sys = 0;
    dut.eval();
}

static uint8_t peek(Vexpansion_machine &dut, uint16_t address) {
    dut.peek_addr = address;
    // The optional cart's diagnostic read crosses both bus registers.
    for (unsigned i = 0; i < 3; ++i) tick(dut);
    return dut.peek_data;
}

static uint16_t word(Vexpansion_machine &dut, uint16_t address) {
    return peek(dut, address) | (uint16_t(peek(dut, address + 1)) << 8);
}

static uint64_t key(unsigned row, unsigned bit) { return 0xffffffffffull & ~(1ull << (row*5+bit)); }
static bool idle(Vexpansion_machine &dut,uint64_t timeout=80000000) {
    dut.keyboard=0xffffffffffull;
    for (uint64_t cycle=0;cycle<timeout;cycle++) {
        tick(dut);
        if (dut.cpu_addr==0x04cf && word(dut,0x4025)==0xffff && peek(dut,0x4027)==0) return true;
    }
    return false;
}
static bool type_token(Vexpansion_machine &dut,uint64_t keys,uint8_t token) {
    dut.keyboard=keys;
    for(uint64_t cycle=0;cycle<10000000;cycle++) {
        tick(dut);
        if ((cycle&0xffff)!=0) continue;
        auto line=word(dut,0x4014);
        for(unsigned i=0;i<20;i++) if(peek(dut,line+i)==token) return idle(dut);
    }
    return false;
}
static bool print_ramtop(Vexpansion_machine &dut,unsigned top) {
    if(!idle(dut,400000000)){std::cerr<<"initial idle failed\n";return false;}
    if(!type_token(dut,key(5,0),0xf5)){std::cerr<<"PRINT token failed\n";return false;} // PRINT
    const auto mode=peek(dut,0x4006);
    dut.keyboard=key(0,0)&key(6,0); // SHIFT+ENTER: function mode
    bool changed=false;
    for(uint64_t cycle=0;cycle<10000000;cycle++) {tick(dut);if((cycle&0xffff)==0&&peek(dut,0x4006)!=mode){changed=true;break;}}
    if(!changed||!idle(dut)){std::cerr<<"function mode failed initial="<<unsigned(mode)<<" current="<<unsigned(peek(dut,0x4006))<<"\n";return false;}
    if(!type_token(dut,key(5,1),0xd3)){std::cerr<<"PEEK token failed\n";return false;} // PEEK
    for(auto digit : {1,6,3,8,9}) {
        unsigned row=digit<=5?3:4,bit=digit<=5?digit-1:10-digit;
        if(!type_token(dut,key(row,bit),0x1c+digit)){std::cerr<<"digit failed "<<digit<<"\n";return false;}
    }
    dut.keyboard=key(6,0);
    for(uint64_t cycle=0;cycle<10000000;cycle++)tick(dut);
    if(!idle(dut))return false;
    const auto display=word(dut,0x400c);
    const char* expected=top==0x4400?"68":"128";
    for(unsigned i=0;expected[i];i++)if(peek(dut,display+1+i)!=0x1c+expected[i]-'0'){std::cerr<<"display mismatch index="<<i<<" value="<<unsigned(peek(dut,display+1+i))<<"\n";return false;}
    return true;
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    if (argc != 2) return EXIT_FAILURE;
    const unsigned expected_top = std::strtoul(argv[1], nullptr, 0);
    Vexpansion_machine dut;
    dut.keyboard = 0xffffffffffull;
    dut.tape_ready = 0;
    dut.tape_size = 0;
    dut.tape_data = 0;
    dut.peek_addr = 0;
    dut.reset = 1;
    for (unsigned i = 0; i < 64; ++i) tick(dut);
    dut.reset = 0;
    bool basic_ready = false, pixels = false, halted = false;
    for (uint64_t cycle = 0; cycle < 120000000; ++cycle) {
        tick(dut);
        halted |= !dut.halt_n;
        pixels |= dut.ce_6m5 && !dut.hblank && !dut.vblank && dut.video_pixel;
        if ((cycle & 0x1ffff) == 0) {
            const auto display = word(dut, 0x400c);
            basic_ready = display >= 0x4070 && display < expected_top && peek(dut, display) == 0x76;
            if (basic_ready && halted && pixels) break;
        }
    }
    const unsigned ram_top = word(dut, 0x4004);
    if (!basic_ready || !halted || !pixels || ram_top != expected_top) {
        std::cerr << "ZX81 expansion boot failed: BASIC=" << basic_ready
                  << " HALT=" << halted << " pixels=" << pixels
                  << " RAMTOP=" << std::hex << ram_top << " expected=" << expected_top << '\n';
        return EXIT_FAILURE;
    }
    if(!print_ramtop(dut,expected_top)){std::cerr<<"keyboard PRINT PEEK RAMTOP failed\n";return EXIT_FAILURE;}
    std::cout << "ZX81 socket BASIC/keyboard/display passed, RAMTOP=" << std::hex << ram_top << '\n';
}
