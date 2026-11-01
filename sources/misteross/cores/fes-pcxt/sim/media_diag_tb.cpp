// SPDX-License-Identifier: GPL-3.0-or-later
// The actual guest diagnostic checks a mailbox-loaded disk through FDC/DMA.
#include "Vmedia_diag_top.h"
#include "verilated.h"
#include "memory_model.hpp"
#include <algorithm>
#include <fstream>
#include <iostream>
#include <iterator>
#include <vector>

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    require(argc == 2, "diagnostic firmware path", 0);
    std::ifstream input(argv[1], std::ios::binary);
    std::vector<uint8_t> firmware{std::istreambuf_iterator<char>(input), {}};
    require(firmware.size() == 65536, "64 KiB diagnostic firmware", 0);
    Vmedia_diag_top d;
    Sdram<Vmedia_diag_top> ram;
    uint64_t cycles = 0, reads = 0, transfers = 0;
    unsigned pixel_phase = 0;
    auto step = [&] {
        d.clk = 0;
        pixel_phase += 1485;
        if (pixel_phase >= 2000) { pixel_phase -= 2000; d.clk_pixel = !d.clk_pixel; }
        d.eval(); d.dq_sample = ram.tick(d); d.eval();
        if (d.media_read_valid && d.media_read_ready) ++reads;
        if (d.dma2_done) ++transfers;
        d.clk = 1; d.eval(); ++cycles;
        if (cycles % 10000000 == 0)
            std::cerr << "cycle=" << cycles << " pc=" << std::hex << d.instruction_address
                      << " marker=" << d.text_word << " status=" << ram.words[0x300]
                      << " dor=" << unsigned(d.debug_dor) << " fdc=" << unsigned(d.debug_fdc_state)
                      << " ready=" << unsigned(d.debug_motor_ready) << " present=" << unsigned(d.debug_media_present)
                      << " unit=" << unsigned(d.media_state)
                      << std::dec << " reads=" << reads << " transfers=" << transfers << '\n';
        if (cycles % 100000 == 0) ram.ram_writes.clear();
        require(!d.fault, "CPU/ISA/DMA fault", cycles);
        require(cycles < 180000000, "diagnostic timeout", cycles);
    };
    d.clk=0; d.clk_pixel=0; d.cold_reset=d.reset_pixel=1; d.warm_reset=d.hold=0;
    d.firmware_valid=0; d.firmware_address=0; d.firmware_data=0;
    d.media_present=0; d.disk_restart=0; d.disk_load_valid=0; d.disk_load_address=0; d.disk_load_data=0;
    d.media_gpo=0; d.code_valid=0; d.key_code=0; d.text_address=4;
    for (unsigned i=0; i<32; ++i) step();
    ram.cold_reset(); d.cold_reset=d.reset_pixel=0;
    while(!d.initialized) step();
    for(unsigned a=0; a<65536; ++a) {
        d.firmware_address=a; d.firmware_data=firmware[a]; d.firmware_valid=1; d.eval();
        while(!d.firmware_ready) step();
        step(); d.firmware_valid=0;
        while(!d.firmware_done) step();
    }
    unsigned toggle=0;
    auto exchange=[&](unsigned op,unsigned index,unsigned arg) {
        unsigned command=(op<<24)|(index<<16)|arg;
        d.media_gpo=(toggle<<31)|command;
        for(unsigned i=0;i<3;++i)step();
        toggle^=1; d.media_gpo=(toggle<<31)|command;
        for(unsigned i=0;i<4000;++i) {
            step();
            if (((d.media_gpi>>23)&1)==toggle) {
                require((d.media_gpi&0xff000000u)==0xf5000000u && !(d.media_gpi&(1u<<22)),
                        "mailbox command rejected",cycles);
                return unsigned(d.media_gpi&0xffff);
            }
        }
        require(false,"mailbox timeout",cycles); return 0u;
    };
    auto wait_marker=[&](uint16_t value, uint64_t budget) {
        for(uint64_t i=0;i<budget;++i) { step(); if(d.text_word == value) return true; }
        return false;
    };
    require(exchange(1,6,0)==1 && exchange(1,16,0)==16,"PCXT media identity",cycles);
    exchange(2,0,1);
    // A completed board diagnostic is visible while the drive is empty.
    require(wait_marker(0x55aa, 60000000), "empty-drive diagnostic display", cycles);
    require(d.media_state==1 && !d.disk_loaded && reads==0, "empty drive read media",cycles);
    auto upload=[&](bool good) {
        std::vector<uint8_t> disk(368640,0);
        for(unsigned i=0;i<512;++i) disk[i]=uint8_t(11+37*i);
        if(!good) disk[173]^=1;
        uint32_t crc=0xffffffffu;
        for(uint8_t b:disk) { crc^=b; for(unsigned n=0;n<8;++n)crc=(crc&1)?(crc>>1)^0xedb88320u:crc>>1; }
        crc^=0xffffffffu;
        exchange(6,0,disk.size()&0xffff); exchange(6,1,disk.size()>>16);
        exchange(6,2,crc&0xffff); exchange(6,3,crc>>16);
        for(unsigned offset=0;offset<disk.size();) {
            unsigned count=std::min<unsigned>(512,disk.size()-offset);
            exchange(7,0,offset&0xffff);exchange(7,1,offset>>16);exchange(7,2,count);
            for(unsigned i=0;i<count;i+=2) exchange(8,i/2,disk[offset+i]|(unsigned(disk[offset+i+1])<<8));
            offset+=count;
        }
        exchange(9,0,0);
        require(d.media_state==3 && d.disk_loaded, "disk commit",cycles);
    };
    // Incorrect media must actually be read, yet never paint success.
    upload(false);
    auto before=reads;
    for(uint64_t i=0; (reads-before<512 || transfers<512) && i<60000000; ++i) step();
    std::cerr << "bad disk reads=" << reads-before << " DMA=" << transfers << "\n";
    require(reads-before>=512 && transfers>=512,"bad image not read over real FDC/DMA",cycles);
    for(unsigned i=0;i<1000000;++i) { step(); require(d.text_word!=0xffff,"bad image painted success",cycles); }
    exchange(10,0,0);
    require(d.media_state==1,"eject",cycles);
    upload(true);
    before=reads;
    require(wait_marker(0xffff,60000000),"matching disk did not paint success",cycles);
    require(reads-before>=512 && transfers>=1024,"matching image bypassed FDC/DMA",cycles);
    exchange(10,0,0);
    require(wait_marker(0x55aa,10000000),"eject retained success marker",cycles);
    std::cout << "PASS PC/XT diagnostic: empty, bad, matching and ejected images; mailbox to SDRAM to FDC/DMA and video marker; cycles=" << cycles << " reads=" << reads << "\n";
}
