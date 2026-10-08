// SPDX-License-Identifier: GPL-3.0-or-later
// Diagnostic execution of an unchanged raw disk and stock ROM. Storage callbacks
// are bounded models. PPM is a static framebuffer reconstruction, not HDMI/raster.
#include "Vst_boot_sim_top.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <iterator>
#include <stdexcept>
#include <string>
#include <vector>

static void check(bool value, const char *message) { if (!value) throw std::runtime_error(message); }
struct Pending { bool seen=false, done=false; unsigned wait=0; uint32_t addr=0; uint16_t data=0; uint8_t lanes=0; bool write=false; };
struct Demo {
    static constexpr uint64_t Hz=52224000;
    Vst_boot_sim_top dut;
    std::vector<uint8_t> rom,disk;
    std::array<uint8_t,524288> ram{};
    Pending rp,mp,dp;
    uint64_t cycles=0,writes=0,faults=0,vbl=0,hbl=0,mfp=0,dma=0,media=0;
    uint32_t last_media=UINT32_MAX,last_fdc=UINT32_MAX;
    bool last_fault=false,last_ack=false;
    std::string prefix;
    std::ofstream sectors,trace,fdc,fault_trace;
    static std::vector<uint8_t> read(const char *path) {
        std::ifstream input(path,std::ios::binary);check(input.good(),"input unavailable");
        return {std::istreambuf_iterator<char>(input),{}};
    }
    uint16_t word(unsigned address) const { check(address+1<ram.size(),"RAM read bounds");return uint16_t(ram[address])<<8|ram[address+1]; }
    uint32_t longword(unsigned address) const { return uint32_t(word(address))<<16|word(address+2); }
    Demo(const char *rom_path,const char *disk_path,const char *out):prefix(out) {
        rom=read(rom_path);disk=read(disk_path);
        check(rom.size()==196608 && disk.size()>=368640 && disk.size()<=839680,"input size");
        dut.clk_sys=0;dut.reset=1;dut.monochrome=0;
        dut.exp_ack=0;dut.exp_berr=0;dut.exp_rdata=0xffff;dut.exp_irq=0;dut.exp_present=0;
        dut.rom_ready=0;dut.ram_ready=0;dut.media_ready=1;dut.media_size=disk.size();dut.media_valid=0;
        dut.dma_ready=0;dut.mouse_valid=0;dut.mouse_dx=0;dut.mouse_dy=0;dut.mouse_buttons=0;dut.controller_buttons=0;
        for(unsigned i=0;i<5;++i)dut.keyboard[i]=0;
        sectors.open(prefix+"-disk-access.jsonl");trace.open(prefix+"-trace.jsonl");fdc.open(prefix+"-fdc.jsonl");
        fault_trace.open(prefix+"-faults.jsonl");
        check(sectors.good()&&trace.good()&&fdc.good()&&fault_trace.good(),"trace output unavailable");dut.eval();
    }
    void storage() {
        if(dut.reset){rp={};mp={};dp={};dut.rom_ready=0;dut.ram_ready=0;dut.media_valid=0;dut.dma_ready=0;return;}
        if(!dut.rom_req){rp={};dut.rom_ready=0;}
        else {
            unsigned a=dut.rom_addr*2;check(a+1<rom.size(),"ROM bounds");
            if(!rp.seen){rp.seen=true;rp.addr=a;rp.wait=2+(a%3);}
            check(rp.addr==a,"ROM changed before ACK");
            dut.rom_rdata=uint16_t(rom[a])<<8|rom[a+1];dut.rom_ready=rp.wait==0;if(rp.wait)--rp.wait;
        }
        if(!dut.ram_req){mp={};dut.ram_ready=0;}
        else {
            unsigned a=dut.ram_addr*2;check(a+1<ram.size(),"RAM bounds");
            if(!mp.seen){mp.seen=true;mp.addr=a;mp.data=dut.ram_wdata;mp.lanes=dut.ram_byte_enable;mp.write=dut.ram_write;mp.wait=8+(a%7);}
            check(mp.addr==a&&mp.data==dut.ram_wdata&&mp.lanes==dut.ram_byte_enable&&mp.write==bool(dut.ram_write),"RAM changed before ACK");
            dut.ram_ready=mp.wait==0;
            if(mp.wait)--mp.wait;
            else if(!mp.done){mp.done=true;if(mp.write){if(mp.lanes&2)ram[a]=mp.data>>8;if(mp.lanes&1)ram[a+1]=mp.data;++writes;}}
            dut.ram_rdata=word(a);
        }
        if(!dut.media_req){dut.media_valid=0;last_media=UINT32_MAX;}
        else {
            check(dut.media_addr<disk.size(),"media bounds");dut.media_data=disk[dut.media_addr];dut.media_valid=1;
            if(last_media!=dut.media_addr){++media;if((dut.media_addr&511)==0)sectors<<"{\"cycle\":"<<cycles<<",\"byte_address\":"<<dut.media_addr<<",\"logical_sector\":"<<(dut.media_addr/512)<<",\"pc\":"<<dut.debug_pc<<"}\n";last_media=dut.media_addr;}
        }
        if(!dut.dma_req){dp={};dut.dma_ready=0;}
        else {
            unsigned a=dut.dma_addr;check(!(a&1)&&a+1<ram.size(),"DMA bounds");
            if(!dp.seen){dp.seen=true;dp.addr=a;dp.wait=9;dp.data=dut.dma_wdata;dp.lanes=dut.dma_byte_enable;}
            check(dp.addr==a&&dp.data==dut.dma_wdata&&dp.lanes==dut.dma_byte_enable,"DMA changed before ACK");
            dut.dma_ready=dp.wait==0;
            if(dp.wait)--dp.wait;
            else if(!dp.done){dp.done=true;if(dp.lanes&2)ram[a]=dp.data>>8;if(dp.lanes&1)ram[a+1]=dp.data;++dma;}
        }
    }
    void tick() {
        storage();dut.eval();dut.clk_sys=1;dut.eval();
        if(dut.debug_bus_error&&!last_fault){
            ++faults;
            if(faults<=64){
                fault_trace<<"{\"fault\":"<<faults<<",\"cycle\":"<<cycles
                           <<",\"pc\":"<<dut.debug_pc<<",\"address\":"<<dut.debug_fault_address
                           <<",\"function_code\":"<<unsigned(dut.debug_fault_fc)
                           <<",\"write\":"<<(dut.debug_fault_write?"true":"false")<<"}\n";
                fault_trace.flush();
            }
            if(faults<=8){
                std::ofstream memory(prefix+"-fault-"+std::to_string(faults)+"-ram.bin",std::ios::binary);
                memory.write(reinterpret_cast<const char*>(ram.data()),ram.size());
            }
        }
        last_fault=dut.debug_bus_error;
        if(dut.irq_ack&&!last_ack){if(dut.irq_level==6)++mfp;if(dut.irq_level==4)++vbl;if(dut.irq_level==2)++hbl;}last_ack=dut.irq_ack;
        const uint32_t state=uint32_t(dut.debug_fdc_status)|(uint32_t(dut.debug_fdc_track)<<8)|(uint32_t(dut.debug_fdc_sector)<<16)|(uint32_t(dut.debug_fdc_head)<<24);
        if(state!=last_fdc){fdc<<"{\"cycle\":"<<cycles<<",\"status\":"<<unsigned(dut.debug_fdc_status)<<",\"track\":"<<unsigned(dut.debug_fdc_track)<<",\"sector\":"<<unsigned(dut.debug_fdc_sector)<<",\"head_track\":"<<unsigned(dut.debug_fdc_head)<<",\"pc\":"<<dut.debug_pc<<"}\n";last_fdc=state;}
        dut.clk_sys=0;dut.eval();++cycles;
    }
    unsigned color(unsigned index) const {
        const unsigned shift=index*9,slot=shift/32,bit=shift%32;
        uint64_t packed=dut.palette[slot];if(slot+1<5)packed|=uint64_t(dut.palette[slot+1])<<32;
        return (packed>>bit)&511;
    }
    void snapshot(const std::string &suffix) {
        std::ofstream memory(prefix+suffix+"-ram.bin",std::ios::binary);
        memory.write(reinterpret_cast<const char*>(ram.data()),ram.size());
        const unsigned mode=dut.resolution,planes=mode==0?4:mode==1?2:1;
        const unsigned width=mode==0?320:640,height=mode==2?400:200;
        const unsigned base=dut.screen_base&0xffff00;
        if(mode>2||base+width*height*planes/8>ram.size())return;
        std::ofstream ppm(prefix+suffix+"-screen.ppm",std::ios::binary);ppm<<"P6\n"<<width<<' '<<height<<"\n255\n";
        for(unsigned y=0;y<height;++y)for(unsigned x=0;x<width;++x){
            unsigned index=0,a=base+(y*(width/16)+x/16)*planes*2;
            for(unsigned plane=0;plane<planes;++plane)index|=((word(a+plane*2)>>(15-x%16))&1)<<plane;
            unsigned c=color(index);unsigned char rgb[3];
            if(mode==2)rgb[0]=rgb[1]=rgb[2]=index?0:255;
            else {rgb[0]=((c>>6)&7)*255/7;rgb[1]=((c>>3)&7)*255/7;rgb[2]=(c&7)*255/7;}
            ppm.write(reinterpret_cast<char*>(rgb),3);
        }
    }
    void status(std::ostream &out) {
        out<<"{\"cycle\":"<<cycles<<",\"pc\":"<<dut.debug_pc<<",\"bus_address\":"<<dut.debug_addr
           <<",\"halted\":"<<(dut.debug_halted?"true":"false")<<",\"screen_base\":"<<dut.screen_base
           <<",\"fdc_status\":"<<unsigned(dut.debug_fdc_status)<<",\"fdc_track\":"<<unsigned(dut.debug_fdc_track)<<",\"fdc_sector\":"<<unsigned(dut.debug_fdc_sector)<<",\"fdc_head_track\":"<<unsigned(dut.debug_fdc_head)
           <<",\"resolution\":"<<unsigned(dut.resolution)<<",\"sync_mode\":"<<unsigned(dut.sync_mode)
           <<",\"memvalid\":"<<longword(0x420)<<",\"phystop\":"<<longword(0x42e)
           <<",\"hz200\":"<<longword(0x4ba)<<",\"ram_writes\":"<<writes<<",\"bus_faults\":"<<faults
           <<",\"mfp_acks\":"<<mfp<<",\"vbl_acks\":"<<vbl<<",\"hbl_acks\":"<<hbl
           <<",\"dma_words\":"<<dma<<",\"media_bytes\":"<<media<<"}";
    }
    void run(unsigned seconds) {
        for(unsigned i=0;i<64;++i)tick();dut.reset=0;
        while(cycles<Hz*seconds){
            tick();
            if(cycles%(Hz/10)==0){status(trace);trace<<'\n';}
            if(cycles%Hz==0){status(std::cout);std::cout<<'\n'<<std::flush;snapshot("-second-"+std::to_string(cycles/Hz));}
            if(cycles>1000&&dut.debug_halted)break;
        }
        snapshot("-final");std::ofstream metrics(prefix+"-metrics.json");status(metrics);metrics<<'\n';
        dut.final();std::cout<<"Bounded diagnostic ended; demo compatibility is not asserted.\n";
    }
};
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    try {check(argc==5,"ROM DISK SECONDS PREFIX required");Demo demo(argv[1],argv[2],argv[4]);demo.run(std::strtoul(argv[3],nullptr,10));}
    catch(const std::exception &error){std::cerr<<"FAIL "<<error.what()<<'\n';return 1;}
}
