// SPDX-License-Identifier: GPL-3.0-or-later
// Warm and cold restarts of the assembled DOS machine (#1534). The floppy
// enters through the runtime mailbox and keys through the host HID mailbox and
// XT keyboard cable, as on a kit.
// reboot: boot FreeDOS, press Ctrl+Alt+Del and require GLaBIOS to finish its
//   warm POST (8259 interrupt test included) and boot the floppy again.
// relaunch: SDRAM keeps the 1234h warm-boot flag a previous run's
//   Ctrl+Alt+Del left; a cold reset must still give a cold POST.
#include "Vbios_top.h"
#include "verilated.h"
#include "memory_model.hpp"
#include <algorithm>
#include <fstream>
#include <iostream>
#include <string>
#include <vector>

static std::vector<unsigned char> read_file(const char *path) {
    std::ifstream input(path, std::ios::binary);
    require(bool(input), "fixture file cannot be opened", 0);
    return {std::istreambuf_iterator<char>(input), {}};
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    require(argc == 3 || argc == 4, "firmware, floppy and optional mode arguments", 0);
    const std::string mode=argc==4 ? argv[3] : "reboot";
    require(mode=="reboot" || mode=="relaunch", "unknown mode", 0);
    const auto firmware = read_file(argv[1]);
    const auto disk = read_file(argv[2]);
    require(firmware.size() == 65536, "64 KiB firmware window", 0);
    require(disk.size() == 40*2*9*512, "360 KiB floppy geometry", 0);
    Vbios_top d;
    Sdram<Vbios_top> ram;
    std::ofstream trace("bus.log");
    uint64_t cycles=0;
    unsigned pixel_phase=0, pending_address=0;
    bool pending=false, tracing=false, pressed=false;
    // GLaBIOS 0.4.2 8X addresses from its listing.
    constexpr unsigned BOOT_WARM=0xfe05b, HALT_BEEP=0xfe876, POST_DONE=0xfe49a, INT_19=0xfe6f2;
    std::vector<unsigned> recent(4096);
    unsigned recent_next=0;
    unsigned boot_warm=0, post_done=0, int19=0, boot_sector=0;
    auto byte=[&](unsigned a) { return unsigned((ram.words.at(a/2) >> (8*(a&1)))&255); };
    auto word=[&](unsigned a) { return byte(a) | (byte(a+1)<<8); };
    auto screen=[&] {
        std::string text;
        for(unsigned y=0;y<25;++y) {
            for(unsigned x=0;x<80;++x) {
                d.text_address=y*80+x;d.eval();unsigned c=d.text_word&255;
                text+=char(c>=32 && c<127?c:' ');
            }
            text+='\n';
        }
        std::ofstream("screen.txt")<<text;
        return text;
    };
    auto pixel_edge=[&] {
        pixel_phase+=1485;
        if(pixel_phase>=2000) {pixel_phase-=2000;d.clk_pixel=!d.clk_pixel;d.eval();}
    };
    auto step=[&] {
        d.clk=0;d.eval();
        if(d.cold_reset || d.warm_reset || d.machine_reset) pending=false;
        else if(d.media_read_valid) {
            if(!pending) {pending=true;pending_address=d.media_read_addr;}
            require(d.media_read_addr==pending_address,"floppy byte address changed while held",cycles);
            require(pending_address<disk.size(),"floppy read beyond image",cycles);
        } else require(!pending,"floppy byte request withdrawn",cycles);
        d.eval();d.dq_sample=ram.tick(d);d.eval();pixel_edge();
        const bool media_taken=!d.cold_reset && !d.warm_reset && !d.machine_reset &&
                               d.media_read_valid && d.media_read_ready;
        const unsigned media_data=d.media_read_data;
        const bool done=d.bus_done, wr=d.bus_write;
        const unsigned addr=d.bus_address, data=d.bus_data, kind=d.bus_kind;
        const bool consumed=d.instruction_consumed;
        const unsigned ip=d.instruction_address;
        d.clk=1;d.eval();pixel_edge();++cycles;
        if(media_taken) {
            require(media_data==disk[pending_address],"SDRAM floppy byte differs from image",cycles);
            pending=false;
        }
        if(consumed) {
            recent[recent_next++%recent.size()]=ip;
            if(ip==BOOT_WARM && pressed) {++boot_warm;tracing=true;}
            if(ip==HALT_BEEP) {
                trace.flush();screen();
                std::ofstream history("ip-history.txt");
                for(unsigned i=0;i<recent.size();++i)
                    history<<std::hex<<recent[(recent_next+i)%recent.size()]<<"\n";
                require(false,"BIOS halted with a POST beep code",cycles);
            }
            if(ip==POST_DONE)++post_done;
            if(ip==INT_19)++int19;
            if(ip==0x7c00)++boot_sector;
        }
        if(done && kind==1 && !wr && (addr&0x3ff)==0x60 && pressed && !boot_warm)
            std::cerr<<"key "<<std::hex<<data<<std::dec<<" at "<<cycles<<"\n";
        if(tracing && done && kind==1)
            trace<<cycles<<" "<<(wr?'W':'R')<<" "<<std::hex<<addr<<" "<<data<<std::dec<<"\n";
        require(d.fault==0,"CPU/ISA/DMA fault",cycles);
        require(cycles<16000000000ULL,"Ctrl+Alt+Del reboot timeout",cycles);
    };
    d.cold_reset=d.reset_pixel=1;d.warm_reset=0;d.hold=0;d.firmware_valid=0;d.code_valid=0;d.key_code=0;
    d.media_present=0;d.disk_restart=0;d.disk_load_valid=0;d.disk_load_address=0;d.disk_load_data=0;d.media_gpo=0;
    // A previous run's Ctrl+Alt+Del left the BIOS warm-boot flag in SDRAM,
    // which keeps its contents across an FPGA reconfiguration.
    if(mode=="relaunch") ram.words.at(0x472/2)=0x1234;
    for(unsigned i=0;i<32;++i)step();
    ram.cold_reset();d.cold_reset=d.reset_pixel=0;
    while(!d.initialized)step();
    for(unsigned a=0;a<65536;++a) {
        d.firmware_address=a;d.firmware_data=firmware[a];d.firmware_valid=1;d.eval();
        while(!d.firmware_ready)step();
        step();d.firmware_valid=0;
        while(!d.firmware_done)step();
    }
    require(word(0x472)==0,"cold reset kept the BIOS warm-boot flag",cycles);
    unsigned toggle=0;
    auto exchange=[&](unsigned op,unsigned index,unsigned arg) {
        const unsigned command=(op<<24)|(index<<16)|arg;
        d.media_gpo=(toggle<<31)|command;
        for(unsigned i=0;i<3;++i)step();
        toggle^=1;d.media_gpo=(toggle<<31)|command;
        for(unsigned i=0;i<4000;++i) {
            step();
            if(((d.media_gpi>>23)&1)==toggle) {
                require((d.media_gpi&0xff000000u)==0xf5000000u && !(d.media_gpi&(1u<<22)),
                        "runtime mailbox rejected command",cycles);
                return unsigned(d.media_gpi&0xffff);
            }
        }
        require(false,"runtime mailbox timeout",cycles);return 0u;
    };
    uint32_t crc=0xffffffffu;
    for(auto b:disk) {crc^=b;for(unsigned i=0;i<8;++i)crc=(crc&1)?(crc>>1)^0xedb88320u:crc>>1;}
    crc^=0xffffffffu;
    exchange(6,0,disk.size()&0xffff);exchange(6,1,disk.size()>>16);
    exchange(6,2,crc&0xffff);exchange(6,3,crc>>16);
    for(unsigned offset=0;offset<disk.size();offset+=512) {
        exchange(7,0,offset&0xffff);exchange(7,1,offset>>16);exchange(7,2,512);
        for(unsigned i=0;i<512;i+=2)exchange(8,i/2,disk[offset+i] | (unsigned(disk[offset+i+1])<<8));
    }
    exchange(9,0,0);
    require(d.media_state==3,"committed disk not ready",cycles);
    exchange(2,0,1);
    auto wait_prompt=[&](const char *what) {
        const uint64_t deadline=cycles+6000000000ULL;
        while(cycles<deadline) {
            for(unsigned i=0;i<5000000;++i)step();
            const auto text=screen();
            if(text.find("A:\\>")!=std::string::npos) return;
        }
        require(false,what,cycles);
    };
    wait_prompt("first boot did not reach the DOS prompt");
    if(mode=="relaunch") {
        require(post_done==1 && int19==1 && boot_sector>=1,"relaunch boot path",cycles);
        require(screen().find("Boot   [ COLD ]")!=std::string::npos,"relaunch did not run a cold POST",cycles);
        std::cout<<"PASS PC/XT relaunch: booted the floppy to A:\\> after "<<cycles<<" fabric clocks\n";
        return 0;
    }
    require(post_done==1 && int19==1 && boot_sector>=1,"cold boot path",cycles);
    // Clear the screen first so the old prompt cannot satisfy the reboot check.
    for(unsigned usage:{6u,15u,22u,40u}) { // C, L, S, Enter
        exchange(3,usage/16,1u<<(usage%16));for(unsigned i=0;i<2000000;++i)step();
        exchange(3,usage/16,0);for(unsigned i=0;i<2000000;++i)step();
    }
    for(unsigned i=0;i<40000000;++i)step();
    require(screen().find("A:\\>CLS")==std::string::npos,"CLS did not clear the screen",cycles);
    // Host HID: left Ctrl is row 8 bit 0, left Alt row 8 bit 2, Delete usage 0x4c.
    pressed=true;
    exchange(3,8,1);for(unsigned i=0;i<2000000;++i)step();
    exchange(3,8,5);for(unsigned i=0;i<2000000;++i)step();
    exchange(3,0x4c/16,1u<<(0x4c%16));
    const uint64_t press=cycles;
    while(!boot_warm && cycles<press+50000000)step();
    require(boot_warm==1,"Ctrl+Alt+Del did not enter the BIOS warm boot",cycles);
    exchange(3,0x4c/16,0);exchange(3,8,0);
    while(post_done<2)step();
    require(word(0x472)==0,"warm POST did not clear the warm-boot flag",cycles);
    wait_prompt("warm reboot did not reach the DOS prompt");
    require(int19==2 && boot_sector>=2,"warm reboot did not boot the floppy",cycles);
    std::cout<<"PASS PC/XT Ctrl+Alt+Del: warm POST and floppy reboot to A:\\> after "
             <<cycles<<" fabric clocks\n";
}
