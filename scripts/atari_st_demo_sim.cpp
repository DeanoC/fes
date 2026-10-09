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
    Pending rp,mp,dp,cp;
    std::vector<unsigned char> native_picture = std::vector<unsigned char>(320*200*3);
    std::vector<unsigned char> completed_native_picture;
    uint64_t cycles=0,writes=0,faults=0,vbl=0,hbl=0,mfp=0,dma=0,media=0;
    uint32_t last_media=UINT32_MAX,last_fdc=UINT32_MAX;
    bool last_fault=false,last_ack=false,last_io=false;
    unsigned consecutive_captures=0, last_sync=0;
    std::array<unsigned,16> last_palette{};
    uint64_t native_frames=0,palette_changes=0,palette_frame_changes=0,max_palette_frame_changes=0;
    unsigned palette_frame_samples=0;
    std::string prefix;
    std::ofstream sectors,trace,fdc,fault_trace,palette_trace,raster_trace;
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
        dut.capture_ready=0;dut.capture_data=0;
        for(unsigned i=0;i<5;++i)dut.keyboard[i]=0;
        sectors.open(prefix+"-disk-access.jsonl");trace.open(prefix+"-trace.jsonl");fdc.open(prefix+"-fdc.jsonl");
        fault_trace.open(prefix+"-faults.jsonl");
        palette_trace.open(prefix+"-palette.jsonl");
        raster_trace.open(prefix+"-raster.jsonl");
        check(sectors.good()&&trace.good()&&fdc.good()&&fault_trace.good()&&palette_trace.good()&&raster_trace.good(),"trace output unavailable");dut.eval();
    }
    void storage() {
        if(dut.reset){rp={};mp={};dp={};cp={};dut.rom_ready=0;dut.ram_ready=0;dut.media_valid=0;dut.dma_ready=0;dut.capture_ready=0;return;}
        if(!dut.capture_req){cp={};dut.capture_ready=0;}
        else {
            unsigned a=dut.capture_addr*2;check(a+1<ram.size(),"native capture RAM bounds");
            if(!cp.seen){cp.seen=true;cp.addr=a;cp.wait=12+(a%7);}
            check(cp.addr==a,"native read changed before ACK");
            dut.capture_data=word(a);dut.capture_ready=cp.wait==0;if(cp.wait)--cp.wait;
        }
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
        storage();dut.eval();
        // Observe the event consumed on this edge, before the accumulators advance.
        const bool native_vblank=!dut.reset&&dut.vblank;
        const unsigned native_line=dut.debug_native_line;
        const uint32_t horizontal_phase=dut.debug_horizontal_phase;
        const bool native_pixel=dut.capture_pixel;
        const unsigned native_x=dut.capture_x,native_y=dut.capture_y,native_rgb=dut.capture_rgb;
        const unsigned completed_before=dut.capture_frames;
        dut.clk_sys=1;dut.eval();
        if(native_pixel){
            check(native_x<320&&native_y<200,"native capture coordinates");
            auto expand=[](unsigned c){return (c<<5)|(c<<2)|(c>>1);};
            const unsigned offset=(native_y*320+native_x)*3;
            native_picture[offset]=expand((native_rgb>>6)&7);
            native_picture[offset+1]=expand((native_rgb>>3)&7);
            native_picture[offset+2]=expand(native_rgb&7);
        }
        if(dut.capture_frames!=completed_before){
            completed_native_picture=native_picture;
            if(cycles>=13*Hz/2 && consecutive_captures<16){
                std::ofstream image(prefix+"-consecutive-"+std::to_string(consecutive_captures++)+"-native.ppm",std::ios::binary);
                image<<"P6\n320 200\n255\n";
                image.write(reinterpret_cast<const char*>(native_picture.data()),native_picture.size());
            }
        }
        // A bounded bus trace locates timer programming and mode writes in
        // native coordinates. It records each request start, never repeated
        // held requests; the PC remains diagnostic prefetch state.
        if(cycles>=6*Hz && cycles<7*Hz){
            const bool ack=dut.irq_ack&&!last_ack;
            const bool io=dut.debug_io_req&&!last_io&&dut.exp_write;
            if(ack||io||dut.sync_mode!=last_sync) raster_trace<<"{\"cycle\":"<<cycles<<",\"frame\":"<<native_frames
                <<",\"line\":"<<native_line<<",\"horizontal_phase\":"<<horizontal_phase
                <<",\"pc\":"<<dut.debug_pc<<",\"iack\":"<<(ack?unsigned(dut.irq_level):0)
                <<",\"sync_mode\":"<<unsigned(dut.sync_mode)<<",\"write_address\":"<<(io?dut.exp_addr*2:0)<<",\"write_data\":"<<(io?dut.exp_wdata:0)<<"}\n";
        }
        last_io=dut.debug_io_req; last_sync=dut.sync_mode;
        if(native_vblank){
            if(cycles>=6*Hz){
                palette_trace<<"{\"kind\":\"frame\",\"cycle\":"<<cycles<<",\"frame\":"<<native_frames
                             <<",\"changed_entries\":"<<palette_frame_changes<<"}\n";
                if(palette_frame_changes>max_palette_frame_changes)max_palette_frame_changes=palette_frame_changes;
            }
            ++native_frames;palette_frame_changes=0;palette_frame_samples=0;
        }
        unsigned changes=0;
        for(unsigned i=0;i<16;++i)changes+=color(i)!=last_palette[i];
        const bool sample=cycles>=6*Hz&&changes&&palette_frame_samples<8;
        if(sample)palette_trace<<"{\"kind\":\"palette\",\"cycle\":"<<cycles<<",\"frame\":"<<native_frames
                               <<",\"line\":"<<native_line<<",\"horizontal_phase\":"<<horizontal_phase<<",\"entries\":[";
        unsigned emitted=0;
        for(unsigned i=0;i<16;++i){
            const unsigned value=color(i);
            if(value!=last_palette[i]){
                if(sample){if(emitted++)palette_trace<<',';palette_trace<<'['<<i<<','<<value<<']';}
                last_palette[i]=value;
            }
        }
        if(sample){palette_trace<<"]}\n";++palette_frame_samples;}
        if(cycles>=6*Hz){palette_changes+=changes;palette_frame_changes+=changes;}
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
        if(!completed_native_picture.empty()){
            std::ofstream native(prefix+suffix+"-native.ppm",std::ios::binary);
            native<<"P6\n320 200\n255\n";
            native.write(reinterpret_cast<const char*>(completed_native_picture.data()),completed_native_picture.size());
        }
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
           <<",\"dma_words\":"<<dma<<",\"media_bytes\":"<<media
           <<",\"palette_changed_entries_after_six_seconds\":"<<palette_changes
           <<",\"max_completed_native_frame_palette_changes\":"<<max_palette_frame_changes
           <<",\"native_rgb_frames\":"<<dut.capture_frames<<",\"native_rgb_underruns\":"<<dut.capture_underruns<<"}";
    }
    void run(unsigned seconds,unsigned key_b_at=0) {
        for(unsigned i=0;i<64;++i)tick();dut.reset=0;
        while(cycles<Hz*seconds){
            // HID usage 5 is B. Exercise the existing keyboard/IKBD path;
            // the original disk and firmware are never modified.
            dut.keyboard[0]=key_b_at&&cycles>=Hz*key_b_at&&cycles<Hz*key_b_at+Hz*150/1000 ? 1u<<5 : 0;
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
    try {check(argc==5||argc==6,"ROM DISK SECONDS PREFIX [KEY_B_AT] required");Demo demo(argv[1],argv[2],argv[4]);demo.run(std::strtoul(argv[3],nullptr,10),argc==6?std::strtoul(argv[5],nullptr,10):0);}
    catch(const std::exception &error){std::cerr<<"FAIL "<<error.what()<<'\n';return 1;}
}
