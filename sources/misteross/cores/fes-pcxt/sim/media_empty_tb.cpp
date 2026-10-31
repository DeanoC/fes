// SPDX-License-Identifier: GPL-3.0-or-later
// A valid firmware image may run when the PC/XT floppy unit is empty.
#include "Vbios_top.h"
#include "verilated.h"
#include "memory_model.hpp"
#include <fstream>
#include <iostream>
#include <vector>

static std::vector<unsigned char> read_file(const char *path) {
    std::ifstream input(path, std::ios::binary);
    require(bool(input), "firmware fixture cannot be opened", 0);
    return {std::istreambuf_iterator<char>(input), {}};
}

int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    require(argc == 2, "firmware argument", 0);
    const auto firmware = read_file(argv[1]);
    require(firmware.size() == 65536, "64 KiB firmware window", 0);
    Vbios_top d;
    Sdram<Vbios_top> ram;
    uint64_t cycles=0, instructions=0;
    auto step=[&] {
        d.clk=0;d.eval();d.dq_sample=ram.tick(d);d.eval();
        const bool consumed=d.instruction_consumed;
        d.clk=1;d.eval();++cycles;
        if(consumed)++instructions;
        require(cycles<20000000,"empty-media BIOS timeout",cycles);
    };
    d.clk=0;d.clk_pixel=0;d.cold_reset=1;d.reset_pixel=1;d.warm_reset=0;d.hold=0;
    d.firmware_valid=0;d.firmware_address=0;d.firmware_data=0;d.code_valid=0;d.key_code=0;
    d.media_present=0;d.disk_restart=0;d.disk_load_valid=0;d.disk_load_address=0;d.disk_load_data=0;
    d.media_gpo=0;d.text_address=0;
    for(unsigned i=0;i<32;++i)step();
    ram.cold_reset();d.cold_reset=0;d.reset_pixel=0;
    while(!d.initialized)step();
    for(unsigned a=0;a<65536;++a) {
        d.firmware_address=a;d.firmware_data=firmware[a];d.firmware_valid=1;d.eval();
        while(!d.firmware_ready)step();
        step();d.firmware_valid=0;
        while(!d.firmware_done)step();
    }
    while(!d.firmware_loaded)step();
    require(d.firmware_loaded && d.machine_reset && d.media_state==1 && !d.disk_loaded,
            "firmware did not finish while held",cycles);
    // Keep the actual boot gate enabled. An empty drive stays held through the
    // firmware upload, then starts when the host releases execution.
    d.media_gpo=0x02000001u;for(unsigned i=0;i<3;++i)step();
    d.media_gpo=0x82000001u;
    for(unsigned i=0;!(d.media_gpi&(1u<<23)) && i<1000;++i)step();
    require((d.media_gpi&(1u<<23)) && !(d.media_gpi&(1u<<22)),"execution release failed",cycles);
    for(unsigned i=0;d.machine_reset && i<1000;++i)step();
    require(!d.machine_reset && d.media_state==1 && !d.disk_loaded,"empty unit kept guest reset",cycles);
    for(unsigned i=0;i<20000000 && instructions<10000;++i)step();
    require(instructions>=10000,"diskless BIOS did not execute",cycles);
    require(!d.machine_reset && d.media_state==1 && !d.disk_loaded,
            "empty disk reset running guest or became loaded",cycles);
    std::cout<<"PASS PC/XT empty drive: execution release sustains 10000 BIOS instructions with no disk\n";
}
