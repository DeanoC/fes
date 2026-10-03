// SPDX-License-Identifier: GPL-3.0-or-later
// Runs an unmodified 192 KiB EmuTOS ROM on the actual CPU/chipset assembly.
#include "Vst_boot_sim_top.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iomanip>
#include <iostream>
#include <iterator>
#include <vector>

static void check(bool ok, const char* message) {
    if (!ok) { std::cerr << "FAIL: " << message << '\n'; std::exit(1); }
}
struct Pending { bool seen=false, done=false; unsigned wait=0; uint32_t addr=0; uint16_t data=0; uint8_t lanes=0; bool write=false; };
struct Boot {
    Vst_boot_sim_top dut;
    std::vector<uint8_t> rom;
    std::array<uint8_t,524288> ram{};
    Pending rp, mp;
    uint64_t cycles=0, writes=0, faults=0, vbl=0, mfp=0;
    bool last_fault=false, last_ack=false;
    uint16_t word(unsigned a) const { return uint16_t(ram[a])<<8 | ram[a+1]; }
    uint32_t longword(unsigned a) const { return uint32_t(word(a))<<16 | word(a+2); }
    explicit Boot(const char* path) {
        std::ifstream f(path,std::ios::binary); check(f.good(),"ROM unavailable");
        rom.assign(std::istreambuf_iterator<char>(f),{}); check(rom.size()==196608,"192 KiB ROM required");
        dut.clk_sys=0; dut.reset=1; dut.monochrome=0;
        dut.exp_ack=0; dut.exp_berr=0; dut.exp_rdata=0xffff; dut.exp_irq=0; dut.exp_present=0;
        dut.rom_ready=0; dut.ram_ready=0; dut.media_ready=0; dut.media_valid=0;
        dut.dma_ready=0; dut.mouse_valid=0; dut.controller_buttons=0;
        for(unsigned i=0;i<5;++i) dut.keyboard[i]=0;
        dut.eval();
    }
    void memory() {
        if (dut.reset) { rp={}; mp={}; dut.rom_ready=0; dut.ram_ready=0; return; }
        if (!dut.rom_req) { rp={}; dut.rom_ready=0; }
        else {
            unsigned a=dut.rom_addr*2; check(a+1<rom.size(),"ROM bounds");
            if(!rp.seen) { rp.seen=true; rp.addr=a; rp.wait=2+(a%3); }
            check(rp.addr==a,"ROM address changed while waiting");
            dut.rom_rdata=uint16_t(rom[a])<<8|rom[a+1];
            dut.rom_ready=rp.wait==0; if(rp.wait) --rp.wait;
        }
        if (!dut.ram_req) { mp={}; dut.ram_ready=0; }
        else {
            unsigned a=dut.ram_addr*2; check(a+1<ram.size(),"RAM bounds");
            if(!mp.seen) { mp.seen=true; mp.addr=a; mp.data=dut.ram_wdata; mp.lanes=dut.ram_byte_enable; mp.write=dut.ram_write; mp.wait=8+(a%7); }
            check(mp.addr==a && mp.data==dut.ram_wdata && mp.lanes==dut.ram_byte_enable && mp.write==bool(dut.ram_write),"RAM transaction changed while waiting");
            dut.ram_ready=mp.wait==0;
            if(mp.wait) --mp.wait;
            else if(!mp.done) { mp.done=true; if(mp.write) { if(mp.lanes&2) ram[a]=mp.data>>8; if(mp.lanes&1) ram[a+1]=mp.data; ++writes; } }
            dut.ram_rdata=word(a);
        }
    }
    void tick() {
        memory(); dut.eval(); dut.clk_sys=1; dut.eval();
        if(dut.debug_bus_error&&!last_fault) ++faults;
        last_fault=dut.debug_bus_error;
        if(dut.irq_ack&&!last_ack) { if(dut.irq_level==6)++mfp; if(dut.irq_level==4)++vbl; }
        last_ack=dut.irq_ack;
        dut.clk_sys=0; dut.eval(); ++cycles;
    }
    void status() {
        std::cout<<"cycles="<<std::dec<<cycles<<" pc="<<std::hex<<dut.debug_pc<<" bus="<<dut.debug_addr
            <<" phystop="<<longword(0x42e)<<" screen="<<longword(0x44e)<<" memvalid="<<longword(0x420)
            <<" hz200="<<std::dec<<longword(0x4ba)<<" frclock="<<longword(0x466)
            <<" writes="<<writes<<" faults="<<faults<<" MFP="<<mfp<<" VBL="<<vbl<<'\n'<<std::flush;
    }
};
int main(int argc,char**argv) {
    Verilated::commandArgs(argc,argv); check(argc==2,"usage: boot_tb ROM"); Boot b(argv[1]);
    for(unsigned i=0;i<64;++i)b.tick(); b.dut.reset=0;
    const uint64_t limit=uint64_t(52224000)*15;
    for(;b.cycles<limit;) {
        b.tick();
        if(b.cycles%52224000==0)b.status();
        if(b.cycles>1000 && b.dut.debug_halted) { b.status(); check(false,"CPU double-fault HALT"); }
    }
    b.status();
    check(b.longword(0x420)==0x752019f3,"EmuTOS did not validate RAM");
    check(b.longword(0x42e)==0x80000,"EmuTOS did not detect exactly 512 KiB");
    check(b.longword(0x44e)==0x78000,"EmuTOS screen address is incorrect");
    check(b.longword(0x4ba)>1000 && b.mfp>1000,"200 Hz system timer stopped");
    check(b.longword(0x466)>100 && b.vbl>100,"VBL processing stopped");
    std::ofstream raw("emutos-ram.bin",std::ios::binary); raw.write(reinterpret_cast<char*>(b.ram.data()),b.ram.size());
    std::cout<<"EmuTOS system boot invariants PASS\n";
}
