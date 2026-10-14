// SPDX-License-Identifier: MIT
// Observe user drive firmware, without asserting disk/loader acceptance.
#include "Vc64_drive1541.h"
#include "Vc64_drive1541___024root.h"
#include "verilated.h"
#include <cstdio>
#include <cstdint>
#include <cstdlib>
#include <fstream>
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    Vc64_drive1541 t;
    t.reset=1;t.clk=0;t.atn_low=0;t.host_clock_low=0;t.host_data_low=0;
    t.disk_data=0xff;t.disk_pb=0xef;t.byte_ready_n=1;
    uint64_t cpu_cycles=0, cycles=52224000;
    if (const char *arg=Verilated::commandArgsPlusMatch("C64_DRIVE_CYCLES="))
        if (*arg) cycles=std::strtoull(arg+sizeof("+C64_DRIVE_CYCLES=")-1,nullptr,10);
    bool diagnostic=false;
    if (const char *arg=Verilated::commandArgsPlusMatch("C64_DRIVE_DIAGNOSTIC="))
        diagnostic=*arg;
    bool trace=*Verilated::commandArgsPlusMatch("C64_DRIVE_TRACE=");
    for(uint64_t i=0;i<cycles;++i){
        t.clk=0;t.eval();bool enabled=t.phi;cpu_cycles+=enabled;
        if(i==400)t.reset=0;
        t.clk=1;t.eval();
        if (trace && enabled && cpu_cycles < 1000)
            std::printf("bus=%04x PC=%04x data=%02x state=%u Z=%u\n",
                t.rootp->c64_drive1541__DOT__bus_addr,
                t.rootp->c64_drive1541__DOT__cpu__DOT__PC,
                t.rootp->c64_drive1541__DOT__cpu_data,
                t.rootp->c64_drive1541__DOT__cpu__DOT__state,
                t.rootp->c64_drive1541__DOT__cpu__DOT__Z);

        if(i%5222400==0)std::printf("cycle=%llu bus=%04x IEC clock=%u data=%u motor=%u LED=%u disk-PCR=%02x serial-PCR=%02x\n",
            static_cast<unsigned long long>(i),t.debug_pc,t.drive_clock_low,t.drive_data_low,
            (t.disk_pb_out>>2)&1,(t.disk_pb_out>>3)&1,
            t.rootp->c64_drive1541__DOT__disk_via__DOT__pcr,
            t.rootp->c64_drive1541__DOT__serial_via__DOT__pcr);
    }
    const auto &ram=t.rootp->c64_drive1541__DOT__ram;
    std::ofstream dump("drive-ram.bin",std::ios::binary);
    dump.write(reinterpret_cast<const char *>(&ram[0]),2048);
    std::printf("Final bus=%04x CPU enables=%llu IEC clock=%u data=%u\n",t.debug_pc,
        static_cast<unsigned long long>(cpu_cycles),t.drive_clock_low,t.drive_data_low);
    if (cpu_cycles != cycles * 1000000 / 52224000) return 1;
    if (diagnostic && (ram[0x7ff]!=0xa5 || t.drive_clock_low || t.drive_data_low)) {
        std::printf("1541 diagnostic failed marker=%02x bus=%04x\n",ram[0x7ff],t.debug_pc);
        return 1;
    }
    if (diagnostic) {
        t.atn_low=1;t.eval();
        if (!t.drive_data_low || t.drive_clock_low) return 1;
        t.atn_low=0;t.eval();
        if (t.drive_data_low || t.drive_clock_low) return 1;
    }
    if (diagnostic) std::puts("c64 1541: mirrored RAM, VIA register map, both CPU IRQs and IEC ATN acknowledgement passed");
    return 0;
}
