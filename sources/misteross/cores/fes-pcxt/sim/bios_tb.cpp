// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vbios_top.h"
#include "verilated.h"
#include "memory_model.hpp"
#include <fstream>
#include <iomanip>
#include <deque>
#include <string>

// This fixture models only storage pins and an external XT keyboard. It never
// changes CPU registers, RAM, interrupts or BIOS code to advance POST.
int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    require(argc == 2, "firmware image argument", 0);
    std::ifstream image(argv[1], std::ios::binary);
    std::vector<unsigned char> firmware((std::istreambuf_iterator<char>(image)), {});
    require(firmware.size() == 65536, "64 KiB firmware window", 0);
    Vbios_top d;
    Sdram<Vbios_top> ram;
    uint64_t cycles=0, io_count=0, post_cycle=0, boot_cycle=0;
    unsigned pixel_phase=0, bat_sent=0, held=0, bat_reads=0, key_reads=0;
    bool boot=false, post=false, error=false;
    std::deque<unsigned> keys;
    std::ofstream trace("bus.log");
    std::vector<unsigned char> frame(1280*720*3);
    unsigned frame_pixels=0;
    bool collecting=false, frame_complete=false;
    auto byte=[&](unsigned a) {return unsigned((ram.words.at(a/2) >> (8 * (a & 1))) & 255);};
    auto word=[&](unsigned a) {return byte(a) | (byte(a+1) << 8);};
    auto screen=[&] {
        std::string text;
        std::ofstream cells("screen.bin", std::ios::binary);
        for(unsigned y=0;y<25;++y) {
            for(unsigned x=0;x<80;++x) {
                d.text_address=y*80+x;d.eval();unsigned c=d.text_word&255;
                cells.put(char(c)); cells.put(char(d.text_word >> 8));
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
            pixel_phase-=2000; bool rising=!d.clk_pixel;
            d.clk_pixel=!d.clk_pixel;d.eval();
            if(rising && post && !frame_complete) {
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
        d.clk=0; d.eval(); d.dq_sample=ram.tick(d); d.eval();
        pixel_edge();
        const bool done=d.bus_done, wr=d.bus_write;
        const unsigned addr=d.bus_address, data=d.bus_data, kind=d.bus_kind;
        const bool consumed=d.instruction_consumed;
        const unsigned ip=d.instruction_address, flags=d.post_flags;
        const bool code_taken=d.code_valid && d.code_ready;
        d.clk=1;d.eval();pixel_edge();++cycles;
        if(code_taken) {
            require(!keys.empty(),"keyboard accepted without offer",cycles);
            if(keys.front()==0xaa)++bat_sent;
            keys.pop_front();
        }
        if(consumed) {
            if(ip==0xfe47a)error=true; // stock BIOS POST_ERROR
            if(ip==0xfe49a && !post) { // stock BIOS POST_OK, after flags cleared
                require(flags==0 && !error,"BIOS POST error",cycles);
                post=true;post_cycle=cycles;
                keys.push_back(0x1e);keys.push_back(0x9e); // A make/break
            }
            if(ip==0xfe6f2 && !boot) {boot=true;boot_cycle=cycles;} // INT 19
        }
        if(done && kind==1) {
            ++io_count;
            trace<<cycles<<" "<<(wr?'W':'R')<<" "<<std::hex<<addr<<" "<<data<<std::dec<<"\n";
            if(!wr && (addr&0x3ff)==0x60) {
                if(data==0xaa)++bat_reads;
                if(data==0x1e || data==0x9e)++key_reads;
            }
        }
        // A 20 ms host clock inhibit resets an XT keyboard. Queue BAT only
        // after release; serializer READY also respects PB7 clear and CTS.
        if(!d.machine_reset) {
            if(d.keyboard_clock_low) {if(held<2000000)++held;}
            else {if(held>=2000000)keys.push_back(0xaa);held=0;}
        }
        d.code_valid=!keys.empty();d.key_code=keys.empty()?0:keys.front();
        if(cycles%50000000==0) {
            std::cerr<<"cycles="<<cycles<<" IP="<<std::hex<<ip<<" BP="<<flags<<std::dec
                     <<" IO="<<io_count<<" BAT="<<bat_reads<<"\n";
            screen();trace.flush();
        }
        require(d.fault==0,"CPU/ISA/DMA fault",cycles);
        require(cycles<2000000000ULL,"BIOS POST timeout",cycles);
    };
    d.cold_reset=d.reset_pixel=1;d.warm_reset=0;d.hold=0;d.firmware_valid=0;d.code_valid=0;d.key_code=0;
    d.media_present=0;d.disk_restart=0;d.disk_load_valid=0;d.disk_load_address=0;d.disk_load_data=0;d.media_gpo=0;
    for(unsigned i=0;i<32;++i)step();
    ram.cold_reset();d.cold_reset=d.reset_pixel=0;
    while(!d.initialized)step();
    // Every firmware byte enters via the production reset-only loader and
    // must finish its real SDRAM write before admitting the next byte.
    for(unsigned a=0;a<65536;++a) {
        require(d.machine_reset && !d.firmware_loaded,"early CPU release",cycles);
        d.firmware_address=a;d.firmware_data=firmware[a];d.firmware_valid=1;d.eval();
        while(!d.firmware_ready)step();
        step();d.firmware_valid=0;
        while(!d.firmware_done)step();
    }
    while(!(boot && post && frame_complete && key_reads==2))step();
    require(!error && bat_sent==1 && bat_reads==1,"keyboard self-test/POST",cycles);
    require(word(0x413)==640,"BIOS did not detect 640 KiB",cycles);
    require((word(0x410)&0x31)==0x20,"diskless CGA equipment profile",cycles);
    require(word(0x41a)==0x1e && word(0x41c)==0x20 && word(0x41e)==0x1e61,
            "BIOS keyboard buffer lacks typed A",cycles);
    const auto text=screen();
    require(text.find("GLaBIOS")!=std::string::npos && text.find("640")!=std::string::npos,
            "BIOS POST text missing",cycles);
    require(text.find("POST Error")==std::string::npos,"POST error text",cycles);
    unsigned lit = 0;
    for (size_t i=0; i<frame.size(); i+=3)
        if (frame[i] > 32 || frame[i+1] > 32 || frame[i+2] > 32) ++lit;
    require(lit > 10000, "720p output does not contain the rendered POST", cycles);
    std::ofstream picture("post.ppm",std::ios::binary);
    picture<<"P6\n1280 720\n255\n";
    picture.write(reinterpret_cast<const char*>(frame.data()),frame.size());
    std::ofstream result("result.json");
    result<<"{\"classification\":\"host simulation only\",\"post_errors\":0,\"ram_kib\":640,"
          <<"\"bat_reads\":"<<bat_reads<<",\"key_reads\":"<<key_reads
          <<",\"bios_key_word\":"<<word(0x41e)<<",\"post_cycle\":"<<post_cycle
          <<",\"boot_cycle\":"<<boot_cycle<<",\"cycles\":"<<cycles
          <<",\"sdram_refreshes\":"<<ram.refreshes<<"}\n";
    std::cout<<"PASS XT BIOS: error-free POST, 640 KiB, CGA, BAT/IRQ1, typed A, INT 19 and 720p frame; "
             <<cycles<<" fabric clocks\n";
}
