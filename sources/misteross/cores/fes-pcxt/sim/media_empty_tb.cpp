// SPDX-License-Identifier: GPL-3.0-or-later
// A valid firmware image may run when the PC/XT floppy unit is empty.
#include "Vbios_top.h"
#include "verilated.h"
#include "memory_model.hpp"
#include <iostream>

int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    Vbios_top d;
    Sdram<Vbios_top> ram;
    uint64_t cycles=0, instructions=0;
    auto step=[&] {
        d.clk=0;d.eval();d.dq_sample=ram.tick(d);d.eval();
        const bool consumed=d.instruction_consumed;
        d.clk=1;d.eval();++cycles;
        if(consumed)++instructions;
        require(cycles<10000000,"empty-media execution timeout",cycles);
    };
    d.clk=0;d.clk_pixel=0;d.cold_reset=1;d.reset_pixel=1;d.warm_reset=0;d.hold=0;
    d.firmware_valid=0;d.firmware_address=0;d.firmware_data=0;d.code_valid=0;d.key_code=0;
    d.media_present=0;d.disk_restart=0;d.disk_load_valid=0;d.disk_load_address=0;d.disk_load_data=0;
    d.media_gpo=0;d.text_address=0;
    for(unsigned i=0;i<32;++i)step();
    ram.cold_reset();d.cold_reset=0;d.reset_pixel=0;
    while(!d.initialized)step();
    // HLT at the reset vector and throughout the entire firmware window.
    for(unsigned a=0;a<65536;++a) {
        d.firmware_address=a;d.firmware_data=0xf4;d.firmware_valid=1;d.eval();
        while(!d.firmware_ready)step();
        step();d.firmware_valid=0;
        while(!d.firmware_done)step();
    }
    while(!d.firmware_loaded)step();
    require(d.firmware_loaded && d.machine_reset && d.media_state==1 && !d.disk_loaded,
            "firmware did not finish while held",cycles);
    // Execution Release (opcode 2) is valid with an empty medium.
    d.media_gpo=0x02000001u;for(unsigned i=0;i<3;++i)step();
    d.media_gpo=0x82000001u;
    for(unsigned i=0;!(d.media_gpi&(1u<<23)) && i<1000;++i)step();
    require((d.media_gpi&(1u<<23)) && !(d.media_gpi&(1u<<22)),"execution release failed",cycles);
    for(unsigned i=0;d.machine_reset && i<1000;++i)step();
    require(!d.machine_reset && d.media_state==1 && !d.disk_loaded,"empty unit kept guest reset",cycles);
    for(unsigned i=0;!d.halted && i<1000000;++i)step();
    require(d.halted && instructions>0,"guest did not execute firmware with empty disk",cycles);
    require(!d.machine_reset && d.media_state==1,"empty disk reset running guest",cycles);
    std::cout<<"PASS PC/XT empty drive: execution released and ran firmware with no disk\n";
}
