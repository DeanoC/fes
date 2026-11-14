// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vrom_machine.h"
#include <verilated.h>
#include <cstdint>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

static void require(bool ok, const char* why) { if(!ok)throw std::runtime_error(why); }
int main(int argc,char** argv) {
    try {
        require(argc==3,"image length and independent CRC required");
        const unsigned size=std::stoul(argv[1]);const uint32_t crc=std::stoul(argv[2],nullptr,16);
        const uint8_t code[]={0x3e,0x5a,0x32,0x00,0x40,0x76}; // LD A,5a; LD (4000),A; HALT.
        std::vector<uint8_t> image(size,0);
        for(unsigned i=0;i<sizeof(code);++i)image[i]=code[i];
        Vrom_machine d;
        auto tick=[&](){d.clk=0;d.eval();d.clk=1;d.eval();};
        auto reset=[&](){
            d.reset_n=0;d.cpu_held=1;d.start=0;d.abort_load=0;d.stream_valid=0;d.stream_last=0;
            for(unsigned i=0;i<128;++i)tick();d.reset_n=1;tick();
        };
        auto load=[&](bool valid){
            d.expected_crc32=valid?crc:crc^1;d.start=1;tick();d.start=0;d.eval();
            for(unsigned offset=0;offset<size;offset+=16){
                require(d.stream_ready,"machine ROM sink not ready");
                for(unsigned word=0;word<4;++word){
                    uint32_t data=0;for(unsigned byte=0;byte<4;++byte)data|=uint32_t(image[offset+word*4+byte])<<(byte*8);
                    d.stream_data[word]=data;
                }
                d.stream_last=offset+16==size;d.stream_valid=1;tick();d.stream_valid=0;
                for(unsigned i=0;i<16;++i){tick();require(d.cpu_writes==0&&d.cpu_fetches==0,"CPU ran before ROM admission");}
            }
            require(d.written==size&&bool(d.loaded)==valid&&bool(d.failed)!=valid,"machine ROM CRC gate differs");
        };
        reset();load(false);d.cpu_held=0;
        for(unsigned i=0;i<10000;++i)tick();
        require(d.failed&&!d.loaded&&d.cpu_writes==0&&d.cpu_fetches==0,"rejected image released the CPU");
        reset();load(true);
        for(unsigned i=0;i<1000;++i)tick();
        require(d.cpu_writes==0&&d.cpu_fetches==0,"completed image bypassed explicit Hold");
        d.cpu_held=0;
        unsigned clocks=0;
        while(!d.halted&&clocks<200000){tick();++clocks;}
        require(d.halted&&d.cpu_writes>0,"real Z80 failed to execute streamed ROM");
        for(unsigned i=0;i<4;++i)tick();
        require(d.result_byte==0x5a,"streamed code did not write its expected RAM result");
        std::cout<<"PASS real machine executed "<<size<<"-byte streamed ROM, wrote RAM=5a and halted; bad CRC/Hold retained reset\n";
        return 0;
    }catch(const std::exception& e){std::cerr<<"FAIL "<<e.what()<<'\n';return 1;}
}
