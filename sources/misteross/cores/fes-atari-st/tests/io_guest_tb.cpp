// SPDX-License-Identifier: GPL-3.0-or-later
// Original AUTO PRG on actual FX68K/stock EmuTOS/IKBD/YM RTL. RAM and disk
// are bounded simulation storage. This is guest execution, not physical SDRAM,
// mailbox delivery, HDMI transport or hardware acceptance.
#include "Vst_io_guest_sim_top.h"
#include "verilated.h"
#include "io_events.h"
#include <array>
#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <iterator>
#include <vector>
static void check(bool ok,const char *message) { if(!ok) { std::cerr<<"FAIL "<<message<<'\n'; std::exit(1); } }
struct Pending { bool seen=false,done=false; unsigned wait=0; uint32_t addr=0; uint16_t data=0; uint8_t lanes=0; bool write=false; };
struct Boot {
    static constexpr uint64_t Hz=52224000;
    Vst_io_guest_sim_top dut;
    std::vector<uint8_t> rom,disk,program;
    std::array<uint8_t,524288> ram{};
    Pending rp,mp,dp;
    uint64_t cycles=0,writes=0,faults=0,vbl=0,mfp=0,fetches=0,traps=0,dma=0,media=0,samples=0;
    unsigned base=0,event_index=0,phase_mask=0;
    uint64_t next_input=0,silence_start=0;
    uint32_t last_marker=0;
    bool last_fault=false,last_ack=false;
    std::ofstream pcm,markers,trace;
    uint16_t word(unsigned a) const { check(a+1<ram.size(),"RAM read bounds"); return uint16_t(ram[a])<<8|ram[a+1]; }
    uint32_t longword(unsigned a) const { return uint32_t(word(a))<<16|word(a+2); }
    uint32_t marker() const { return base ? longword(base+MarkerOffset) : 0; }
    static std::vector<uint8_t> read(const char *path) {
        std::ifstream input(path,std::ios::binary); check(input.good(),"input unavailable");
        return {std::istreambuf_iterator<char>(input),{}};
    }
    Boot(const char *rom_path,const char *disk_path,const char *program_path,const char *prefix) {
        rom=read(rom_path); disk=read(disk_path); program=read(program_path);
        check(rom.size()==196608 && disk.size()==737280 && program.size()>28,"input geometry");
        dut.clk_sys=0; dut.reset=1; dut.monochrome=0;
        dut.exp_ack=0; dut.exp_berr=0; dut.exp_rdata=0xffff; dut.exp_irq=0; dut.exp_present=0;
        dut.rom_ready=0; dut.ram_ready=0; dut.media_ready=1; dut.media_valid=0;
        dut.dma_ready=0; dut.mouse_valid=0; dut.controller_buttons=0;
        for(unsigned i=0;i<5;++i)dut.keyboard[i]=0;
        pcm.open(std::string(prefix)+"-pcm-s16le.raw",std::ios::binary);
        markers.open(std::string(prefix)+"-markers.jsonl");
        trace.open(std::string(prefix)+"-fetch.jsonl");
        dut.eval();
    }
    void storage() {
        if(dut.reset) { rp={};mp={};dp={};dut.rom_ready=0;dut.ram_ready=0;dut.media_valid=0;dut.dma_ready=0;return; }
        if(!dut.rom_req) { rp={}; dut.rom_ready=0; }
        else {
            unsigned a=dut.rom_addr*2; check(a+1<rom.size(),"ROM bounds");
            if(!rp.seen) { rp.seen=true;rp.addr=a;rp.wait=2+(a%3); }
            check(rp.addr==a,"ROM changed before ACK");
            dut.rom_rdata=uint16_t(rom[a])<<8|rom[a+1]; dut.rom_ready=rp.wait==0; if(rp.wait)--rp.wait;
        }
        if(!dut.ram_req) { mp={};dut.ram_ready=0; }
        else {
            unsigned a=dut.ram_addr*2;check(a+1<ram.size(),"RAM bounds");
            if(!mp.seen) { mp.seen=true;mp.addr=a;mp.data=dut.ram_wdata;mp.lanes=dut.ram_byte_enable;mp.write=dut.ram_write;mp.wait=8+(a%7); }
            check(mp.addr==a && mp.data==dut.ram_wdata && mp.lanes==dut.ram_byte_enable && mp.write==bool(dut.ram_write),"RAM changed before ACK");
            dut.ram_ready=mp.wait==0;
            if(mp.wait)--mp.wait;
            else if(!mp.done) {
                mp.done=true;
                if(mp.write) { if(mp.lanes&2)ram[a]=mp.data>>8; if(mp.lanes&1)ram[a+1]=mp.data; ++writes; }
                else if(dut.cpu_fc==2 || dut.cpu_fc==6) {
                    if(!base && a+program.size()-28<=ram.size() && word(a)==0x7cff && word(a+2)==0x286f && word(a+4)==4) {
                        for(unsigned i=0;i<program.size()-28;++i)check(ram[a+i]==program[28+i],"loaded PRG differs from exact generated text");
                        base=a;std::cout<<"original guest entry="<<std::hex<<base<<std::dec<<'\n'<<std::flush;
                    }
                    if(base && a>=base && a<base+program.size()-28) {
                        ++fetches;traps+=word(a)==0x4e41;
                        if(fetches<20000)trace<<"{\"cycle\":"<<cycles<<",\"address\":"<<a<<",\"word\":"<<word(a)<<"}\n";
                    }
                }
            }
            dut.ram_rdata=word(a);
        }
        if(!dut.media_req)dut.media_valid=0;
        else { check(dut.media_addr<disk.size(),"media bounds");dut.media_data=disk[dut.media_addr];dut.media_valid=1;++media; }
        if(!dut.dma_req) { dp={};dut.dma_ready=0; }
        else {
            unsigned a=dut.dma_addr;check(!(a&1) && a+1<ram.size(),"DMA bounds");
            if(!dp.seen){dp.seen=true;dp.addr=a;dp.wait=9;dp.data=dut.dma_wdata;dp.lanes=dut.dma_byte_enable;}
            check(dp.addr==a && dp.data==dut.dma_wdata && dp.lanes==dut.dma_byte_enable,"DMA changed before ACK");
            dut.dma_ready=dp.wait==0;
            if(dp.wait)--dp.wait;
            else if(!dp.done){dp.done=true;if(dp.lanes&2)ram[a]=dp.data>>8;if(dp.lanes&1)ram[a+1]=dp.data;++dma;}
        }
    }
    void inputs() {
        const auto current=marker();
        if(current!=last_marker) {
            last_marker=current;
            markers<<"{\"marker\":"<<current<<",\"cycle\":"<<cycles<<",\"pcm_sample\":"<<samples<<"}\n";markers.flush();
            std::cout<<"marker="<<std::hex<<current<<std::dec<<" cycle="<<cycles<<'\n'<<std::flush;
            check(current!=0x494f4641,"guest FAIL marker");
            if(current>=0x41550000 && current<=0x41550005)phase_mask|=1u<<(current-0x41550000);
        }
        if(event_index<InputCount && cycles>=next_input && current==0x494f0100+Inputs[event_index].group) {
            const auto &input=Inputs[event_index++];
            if(input.player<0) { if(input.down)dut.keyboard[input.code/32]|=1u<<(input.code%32); else dut.keyboard[input.code/32]&=~(1u<<(input.code%32)); }
            else { const uint16_t mask=1u<<(input.code+8*input.player);if(input.down)dut.controller_buttons|=mask;else dut.controller_buttons&=~mask; }
            next_input=cycles+Hz/100;
        }
    }
    void tick() {
        storage();inputs();dut.eval();dut.clk_sys=1;dut.eval();
        if(dut.audio_valid) {const int16_t value=dut.audio_pcm;pcm.write(reinterpret_cast<const char*>(&value),2);++samples;}
        if(dut.debug_bus_error&&!last_fault)++faults;last_fault=dut.debug_bus_error;
        if(dut.irq_ack&&!last_ack){if(dut.irq_level==6)++mfp;if(dut.irq_level==4)++vbl;}last_ack=dut.irq_ack;
        dut.clk_sys=0;dut.eval();++cycles;
    }
    void run(unsigned seconds,const char *prefix) {
        for(unsigned i=0;i<64;++i)tick();dut.reset=0;
        while(cycles<Hz*seconds) {
            tick();check(cycles<1000 || !dut.debug_halted,"CPU halted");
            if(cycles%Hz==0)std::cout<<"seconds="<<cycles/Hz<<" PC="<<std::hex<<dut.debug_pc<<" marker="<<marker()<<std::dec<<" events="<<event_index<<'\n'<<std::flush;
            if(marker()==0x41550005){if(!silence_start)silence_start=cycles;if(cycles-silence_start>Hz/20)break;}
        }
        check(base && fetches>1000 && traps>=15,"original guest did not execute real traps/instructions");
        check(longword(0x420)==0x752019f3 && longword(0x42e)==0x80000,"stock EmuTOS RAM discovery");
        check(longword(0x44e)==0x78000 && dut.screen_base==0x78000,"guest changed established framebuffer");
        check(mfp>100 && vbl>25,"timer/VBL stopped");
        check(event_index==InputCount && phase_mask==63 && silence_start,"guest did not complete all exact input/audio phases");
        check(dma>1024 && media>1024,"AUTO guest did not load via actual RTL floppy/DMA");
        std::ofstream raw(std::string(prefix)+"-ram.bin",std::ios::binary);raw.write(reinterpret_cast<char*>(ram.data()),ram.size());
        std::ofstream captured(std::string(prefix)+"-disk.st",std::ios::binary);captured.write(reinterpret_cast<char*>(disk.data()),disk.size());
        std::ofstream metrics(std::string(prefix)+"-guest.json");
        metrics<<"{\"schema\":1,\"system_cycles\":"<<cycles<<",\"program_base\":"<<base<<",\"program_fetches\":"<<fetches<<",\"trap_fetches\":"<<traps<<",\"input_events\":"<<event_index<<",\"audio_phase_mask\":"<<phase_mask<<",\"pcm_samples\":"<<samples<<",\"mfp\":"<<mfp<<",\"vbl\":"<<vbl<<",\"dma_words\":"<<dma<<",\"media_exchanges\":"<<media<<",\"physical_sdram\":false}\n";
        std::cout<<"Actual FX68K/EmuTOS original IKBD/YM guest PASS\n";dut.final();
    }
};
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);check(argc==6,"ROM DISK PRG SECONDS PREFIX required");
    Boot boot(argv[1],argv[2],argv[3],argv[5]);boot.run(std::strtoul(argv[4],nullptr,10),argv[5]);
}
