// SPDX-License-Identifier: GPL-3.0-or-later
// Run a real BIOS and DOS on the assembled 8088. The only simulated devices
// are physical SDRAM pins, a read-only disk byte service and an XT keyboard.
#include "Vbios_top.h"
#include "verilated.h"
#include "memory_model.hpp"
#include <array>
#include <deque>
#include <fstream>
#include <string>
#include <vector>

static std::vector<unsigned char> read_file(const char *path) {
    std::ifstream input(path, std::ios::binary);
    require(bool(input), "fixture file cannot be opened", 0);
    return {std::istreambuf_iterator<char>(input), {}};
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    require(argc == 3, "firmware and floppy image arguments", 0);
    const auto firmware = read_file(argv[1]);
    const auto disk = read_file(argv[2]);
    require(firmware.size() == 65536, "64 KiB firmware window", 0);
    require(disk.size() == 40*2*9*512, "360 KiB floppy geometry", 0);
    Vbios_top d;
    Sdram<Vbios_top> ram;
    std::deque<unsigned> keys;
    std::ofstream trace("bus.log");
    uint64_t cycles=0, post_cycle=0, boot_cycle=0, sector_cycle=0, prompt_cycle=0, dir_cycle=0;
    unsigned pixel_phase=0, held=0, bat_sent=0, bat_reads=0, key_reads=0, media_reads=0;
    unsigned dma2_completions=0, irq6_edges=0, delay=0, pending_address=0;
    bool pending=false, last_dma=false, last_irq=false, post=false, boot=false, sector=false;
    bool prompt=false, directory=false, frame_complete=false, collecting=false;
    std::vector<unsigned char> frame(1280*720*3);
    unsigned frame_pixels=0;
    auto byte=[&](unsigned a) { return unsigned((ram.words.at(a/2) >> (8*(a&1)))&255); };
    auto word=[&](unsigned a) { return byte(a) | (byte(a+1)<<8); };
    auto screen=[&] {
        std::string text;
        std::ofstream cells("screen.bin", std::ios::binary);
        for(unsigned y=0;y<25;++y) {
            for(unsigned x=0;x<80;++x) {
                d.text_address=y*80+x;d.eval();unsigned c=d.text_word&255;
                cells.put(char(c));cells.put(char(d.text_word>>8));
                text+=char(c>=32 && c<127?c:' ');
            }
            text+='\n';
        }
        std::ofstream("screen.txt")<<text;
        return text;
    };
    auto pixel_edge=[&] {
        pixel_phase+=1485;
        if(pixel_phase>=2000) {
            pixel_phase-=2000;bool rising=!d.clk_pixel;
            d.clk_pixel=!d.clk_pixel;d.eval();
            // Let the CPU and video capture settle for two full guest frames
            // after DIR before capturing the next complete 720p surface.
            if(rising && directory && cycles-dir_cycle>=4000000 && !frame_complete) {
                if(d.video_request&(1u<<28)) {frame_pixels=0;collecting=true;}
                if(collecting && (d.video_response&(1u<<24))) {
                    require(frame_pixels<1280*720,"720p frame overflow",cycles);
                    const auto rgb=d.video_response;
                    for(unsigned c=0;c<3;++c)frame[frame_pixels*3+c]=(rgb>>(16-8*c))&255;
                    if(++frame_pixels==1280*720)frame_complete=true;
                }
            }
        }
    };
    auto step=[&] {
        d.clk=0; d.media_read_ready=0;d.eval();
        if(d.media_read_valid) {
            if(!pending) {pending=true;pending_address=d.media_read_addr;delay=3;}
            require(d.media_read_addr==pending_address,"floppy byte address changed while held",cycles);
            require(pending_address<disk.size(),"floppy read beyond image",cycles);
            if(!delay) {d.media_read_data=disk[pending_address];d.media_read_ready=1;}
        } else require(!pending,"floppy byte request withdrawn",cycles);
        d.eval();d.dq_sample=ram.tick(d);d.eval();pixel_edge();
        const bool media_taken=d.media_read_valid && d.media_read_ready;
        const bool code_taken=d.code_valid && d.code_ready;
        const bool done=d.bus_done, wr=d.bus_write;
        const unsigned addr=d.bus_address, data=d.bus_data, kind=d.bus_kind;
        const bool consumed=d.instruction_consumed;
        const unsigned ip=d.instruction_address, flags=d.post_flags;
        const bool dma=d.dma2_done, irq=d.fdc_irq;
        d.clk=1;d.eval();pixel_edge();++cycles;
        if(media_taken) {pending=false;++media_reads;} else if(pending && delay) --delay;
        if(code_taken) {require(!keys.empty(),"keyboard accepted without offer",cycles);if(keys.front()==0xaa)++bat_sent;keys.pop_front();}
        if(dma && !last_dma)++dma2_completions;
        if(irq && !last_irq)++irq6_edges;
        last_dma=dma;last_irq=irq;
        if(consumed) {
            if(ip==0xfe47a)require(false,"BIOS reported POST error",cycles);
            if(ip==0xfe49a && !post) {require(flags==0,"BIOS POST flags",cycles);post=true;post_cycle=cycles;}
            if(ip==0xfe6f2 && !boot) {boot=true;boot_cycle=cycles;}
            if(ip==0x7c00 && !sector) {
                require(boot && media_reads>=512 && dma2_completions>=512,"boot sector skipped the floppy/DMA path",cycles);
                for(unsigned i=0;i<512;++i)require(byte(0x7c00+i)==disk[i],"boot sector differs from image",cycles);
                sector=true;sector_cycle=cycles;
            }
        }
        if(done && kind==1) {
            trace<<cycles<<" "<<(wr?'W':'R')<<" "<<std::hex<<addr<<" "<<data<<std::dec<<"\n";
            if(!wr && (addr&0x3ff)==0x60) {
                if(data==0xaa)++bat_reads;
                else ++key_reads;
            }
        }
        if(!d.machine_reset) {
            if(d.keyboard_clock_low) {if(held<2000000)++held;}
            else {if(held>=2000000)keys.push_back(0xaa);held=0;}
        }
        d.code_valid=!keys.empty();d.code=keys.empty()?0:keys.front();
        if(cycles%5000000==0) {
            const auto text=screen();
            if(!prompt && sector && text.find("A:\\>")!=std::string::npos) {
                prompt=true;prompt_cycle=cycles;
                for(auto c:std::array<unsigned,8>{0x20,0xa0,0x17,0x97,0x13,0x93,0x1c,0x9c})keys.push_back(c);
            }
            // Require the listed file on one line and a *subsequent* prompt;
            // the volume label also contains FESXT, and the original prompt
            // remains on screen while DIR is still producing output.
            if(prompt && !directory && key_reads>=8) {
                for(auto entry=text.find("FESXT"); entry!=std::string::npos;
                    entry=text.find("FESXT",entry+5)) {
                    const auto end=text.find('\n',entry);
                    const auto extension=text.find("TXT",entry);
                    if(end!=std::string::npos && extension!=std::string::npos && extension<end &&
                       text.find("A:\\>",end)!=std::string::npos) {
                        directory=true;dir_cycle=cycles;break;
                    }
                }
            }
            if(cycles%50000000==0) {
                std::cerr<<"cycles="<<cycles<<" IP="<<std::hex<<ip<<" BP="<<flags<<std::dec
                         <<" disk="<<media_reads<<" DMA2="<<dma2_completions<<" IRQ6="<<irq6_edges
                         <<" POST="<<post<<" BOOT="<<boot<<" SECTOR="<<sector
                         <<" PROMPT="<<prompt<<" DIR="<<directory<<"\n";
                trace.flush();
            }
            ram.ram_writes.clear();
        }
        require(d.fault==0,"CPU/ISA/DMA fault",cycles);
        require(cycles<6000000000ULL,"BIOS/DOS boot timeout",cycles);
    };
    d.cold_reset=d.reset_pixel=1;d.warm_reset=0;d.firmware_valid=0;d.code_valid=0;d.code=0;
    d.media_present=1;d.media_read_ready=0;d.media_read_data=0;
    for(unsigned i=0;i<32;++i)step();
    ram.cold_reset();d.cold_reset=d.reset_pixel=0;
    while(!d.initialized)step();
    for(unsigned a=0;a<65536;++a) {
        require(d.machine_reset && !d.firmware_loaded,"early CPU release",cycles);
        d.firmware_address=a;d.firmware_data=firmware[a];d.firmware_valid=1;d.eval();
        while(!d.firmware_ready)step();
        step();d.firmware_valid=0;
        while(!d.firmware_done)step();
    }
    while(!(post && boot && sector && prompt && directory && frame_complete))step();
    require(bat_sent==1 && bat_reads==1,"keyboard self-test",cycles);
    require(key_reads>=8,"DIR make/break bytes not delivered",cycles);
    require(word(0x413)==640,"BIOS did not detect 640 KiB",cycles);
    require((word(0x410)&0x31)==0x21,"CGA and one floppy equipment profile",cycles);
    require(irq6_edges>=2,"floppy interrupts not observed",cycles);
    screen();
    std::ofstream picture("dos.ppm",std::ios::binary);
    picture<<"P6\n1280 720\n255\n";
    picture.write(reinterpret_cast<const char*>(frame.data()),frame.size());
    std::ofstream result("result-pending.json");
    result<<"{\"classification\":\"host simulation only\",\"ram_kib\":640,"
          <<"\"bat_reads\":"<<bat_reads<<",\"key_reads\":"<<key_reads
          <<",\"media_reads\":"<<media_reads<<",\"dma2_completions\":"<<dma2_completions
          <<",\"irq6_edges\":"<<irq6_edges<<",\"post_cycle\":"<<post_cycle
          <<",\"boot_cycle\":"<<boot_cycle<<",\"boot_sector_cycle\":"<<sector_cycle
          <<",\"prompt_cycle\":"<<prompt_cycle<<",\"dir_cycle\":"<<dir_cycle
          <<",\"cycles\":"<<cycles<<",\"sdram_refreshes\":"<<ram.refreshes<<"}\n";
    std::cout<<"PASS XT: real BIOS -> DMA2 floppy boot -> DOS prompt -> typed DIR listing; "
             <<cycles<<" fabric clocks, "<<media_reads<<" disk bytes\n";
}
