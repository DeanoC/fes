// SPDX-License-Identifier: GPL-3.0-or-later
// Original firmware checks native 68000 instruction timing against the actual
// motherboard/MFP path. No Atari ROM or external assembler is used.
#include "Vst_boot_sim_top.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>
#include <vector>

static void require(bool good, const std::string &message) {
    if (!good) { std::cerr << "MFP CPU bus: " << message << '\n'; std::exit(1); }
}
struct Test {
    Vst_boot_sim_top dut;
    std::array<uint8_t,196608> rom{};
    std::array<uint8_t,524288> ram{};
    uint64_t clocks=0, cpu_ticks=0;
    bool rom_seen=false, ram_seen=false;
    unsigned rom_wait=0, ram_wait=0;
    std::vector<std::pair<bool,uint64_t>> starts;
    void word(unsigned &at, unsigned value) { rom.at(at++)=value>>8; rom.at(at++)=value; }
    void longword(unsigned &at, unsigned value) { word(at,value>>16); word(at,value); }
    Test() {
        unsigned at=0; longword(at,0x7fff0); longword(at,0xfc0100);
        at=0x100;
        word(at,0x13fc); word(at,4); longword(at,0xff8001); // 512 KiB bank
        word(at,0x705a); // MOVEQ #$5A,D0
        for (unsigned i=0;i<125;++i) {
            word(at,0x1239); longword(at,0xfffa01); word(at,0x4e71); // byte read; NOP
        }
        for (unsigned i=0;i<125;++i) {
            word(at,0x13c0); longword(at,0xfffa03); word(at,0x4e71); // byte write; NOP
        }
        word(at,0x1239); longword(at,0xfffa03); // read back AER
        word(at,0x13c1); longword(at,0x405); // save byte result
        word(at,0x33fc); word(at,0xc0de); longword(at,0x400);
        word(at,0x4e72); word(at,0x2700);
        dut.reset=1; dut.clk_sys=0; dut.monochrome=0;
        dut.rom_ready=0; dut.ram_ready=0; dut.rom_rdata=0xffff; dut.ram_rdata=0xffff;
        dut.exp_ack=0; dut.exp_berr=0; dut.exp_present=0; dut.exp_rdata=0xffff; dut.exp_irq=0;
        dut.mouse_valid=0; dut.mouse_dx=0; dut.mouse_dy=0; dut.mouse_buttons=0;
        dut.controller_buttons=0; dut.media_ready=0; dut.media_size=0; dut.media_valid=0;
        dut.media_data=0; dut.dma_ready=0; dut.capture_ready=0; dut.capture_data=0;
        for(unsigned i=0;i<5;++i)dut.keyboard[i]=0;
        dut.eval();
    }
    void storage() {
        if (!dut.rom_req) { rom_seen=false; dut.rom_ready=0; }
        else {
            unsigned a=dut.rom_addr*2;
            require(a+1<rom.size(),"ROM address bounds");
            if(!rom_seen) {rom_seen=true;rom_wait=2+a%3;}
            dut.rom_rdata=(unsigned(rom[a])<<8)|rom[a+1];
            dut.rom_ready=rom_wait==0; if(rom_wait)--rom_wait;
        }
        if (!dut.ram_req) {ram_seen=false;dut.ram_ready=0;}
        else {
            unsigned a=dut.ram_addr*2;
            require(a+1<ram.size(),"RAM address bounds");
            if(!ram_seen) {
                ram_seen=true;ram_wait=8+a%7;
                if(dut.ram_write) {
                    if(dut.ram_byte_enable&2)ram[a]=dut.ram_wdata>>8;
                    if(dut.ram_byte_enable&1)ram[a+1]=dut.ram_wdata;
                }
            }
            dut.ram_rdata=(unsigned(ram[a])<<8)|ram[a+1];
            dut.ram_ready=ram_wait==0; if(ram_wait)--ram_wait;
        }
    }
    void tick() {
        storage(); dut.eval();
        if(!dut.reset&&dut.exp_phi2)++cpu_ticks;
        dut.clk_sys=0; dut.eval(); dut.clk_sys=1; dut.eval(); ++clocks;
    }
    void run() {
        for(unsigned i=0;i<64;++i)tick();dut.reset=0;
        bool previous=false;
        while(clocks<200000) {
            if(dut.debug_io_req&&!previous)starts.emplace_back(dut.exp_write,cpu_ticks);
            previous=dut.debug_io_req; tick();
            require(!dut.debug_bus_error&&!dut.debug_halted,"original CPU program has no fault/halt");
            if(ram[0x400]==0xc0&&ram[0x401]==0xde)break;
        }
        require(ram[0x400]==0xc0&&ram[0x401]==0xde,"original timing program completed");
        require(ram[0x405]==0x5a,"delayed write and read retain MFP data");
        require(starts.size()==251,"125 reads, 125 writes and one readback");
        unsigned checked=0;
        for(unsigned first : {0u,125u}) {
            for(unsigned i=first+1;i<first+125;++i) {
                require(starts[i].first==(first!=0),"read/write direction");
                // MOVE.B abs.L <-> Dn is 16 cycles plus four native MFP
                // wait states. NOP adds four. 24 and 125 are coprime, so
                // each block covers every fractional-clock phase.
                require(starts[i].second-starts[i-1].second==24,"24 native CPU cycles per MOVE.B plus NOP");
                ++checked;
            }
        }
        std::cout<<"MFP CPU bus PASS: "<<checked<<" instruction intervals; late read/write data retained\n";
    }
};
int main(int argc,char**argv) {Verilated::commandArgs(argc,argv);Test t;t.run();}
