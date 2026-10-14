// SPDX-License-Identifier: MIT
// A standard IEC host: released wires are logical 1, a CLOCK-high ready
// handshake precedes every byte, and EOI is a prolonged ready interval.
#include "Vc64_iec.h"
#include "verilated.h"
#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <iterator>
#include <vector>

struct Host {
    Vc64_iec t;
    std::vector<unsigned char> disk;
    Host(bool chained=false) {
        std::ifstream f("build/diagnostics/fes-c64/disk.d64", std::ios::binary);
        disk.assign(std::istreambuf_iterator<char>(f), {});
        if (disk.size()!=174848) fail("missing synthetic disk");
        if(chained) {
            disk[0]=1; disk[1]=1; // Track 1/sector 1 follows the full sector.
            disk[256]=0; disk[257]=7; // Six final payload bytes.
            for(unsigned i=0; i<260; ++i) {
                unsigned offset=i<254 ? 2+i : 258+i-254;
                disk[offset]=i==0 ? 1 : i==1 ? 8 : (i ^ 0xa5) & 255;
            }
        }
        t.clk=0; t.phi=1; t.reset=1; t.disk_ready=1;
        t.atn_low=0; t.cpu_clk_low=0; t.cpu_data_low=0;
        tick(4); t.reset=0; tick(4);
    }
    [[noreturn]] void fail(const char *msg) {
        std::printf("IEC: %s (state=%u clock-low=%u data-low=%u)\n",
                    msg,t.debug,t.dev_clk_low,t.dev_data_low); std::exit(1);
    }
    bool clock_low() { return t.cpu_clk_low || t.dev_clk_low; }
    bool data_low() { return t.cpu_data_low || t.dev_data_low; }
    void tick(unsigned n=1) {
        while(n--) {
            t.disk_byte = t.disk_addr<disk.size() ? disk[t.disk_addr] : 0;
            t.clk=0; t.eval(); t.clk=1; t.eval();
        }
    }
    void wait_data(bool low) {
        for (unsigned i=0; i<10000; ++i) { tick(); if(data_low()==low) return; }
        fail("DATA handshake timeout");
    }
    void wait_clock(bool low) {
        for (unsigned i=0; i<100000; ++i) { tick(); if(clock_low()==low) return; }
        fail("CLOCK handshake timeout");
    }
    void attention(bool low) { t.atn_low=low; tick(10); }
    void send(unsigned byte, bool eoi=false) {
        t.cpu_clk_low=1; t.cpu_data_low=0; tick(20);
        wait_data(true); // Listener is present, holding DATA low.
        t.cpu_clk_low=0; tick(); wait_data(false);
        if(eoi) { wait_data(true); wait_data(false); }
        t.cpu_clk_low=1; tick(40);
        for(unsigned bit=0; bit<8; ++bit) {
            t.cpu_data_low = !(byte & (1u<<bit)); tick(40);
            t.cpu_clk_low=0; tick(40);
            // Listener must not corrupt the last data bit with early ACK.
            if(t.dev_data_low) fail("ACK before final CLOCK-low edge");
            t.cpu_clk_low=1; t.cpu_data_low=0; tick(40);
        }
        wait_data(true);
    }
    unsigned receive(bool &eoi) {
        t.cpu_data_low=1; t.cpu_clk_low=0;
        wait_clock(false); t.cpu_data_low=0; tick();
        eoi=false;
        unsigned waited=0;
        while(!clock_low() && waited<300) { tick(); ++waited; }
        if(!clock_low()) {
            eoi=true; t.cpu_data_low=1; tick(60); t.cpu_data_low=0;
            wait_clock(true);
        }
        unsigned byte=0;
        for(unsigned bit=0; bit<8; ++bit) {
            wait_clock(false);
            if(!data_low()) byte |= 1u<<bit;
            wait_clock(true);
        }
        t.cpu_data_low=1; tick(20);
        return byte;
    }
};

static void run(bool chained) {
    Host h(chained);
    h.t.cpu_clk_low=1; h.attention(true);
    h.send(0x28); h.send(0xf0); h.attention(false);
    h.send('B'); h.send('O'); h.send('O'); h.send('T',true);
    h.attention(true); h.send(0x3f); h.attention(false);
    h.t.cpu_clk_low=0; h.tick(400);
    if(h.t.dev_data_low || h.t.dev_clk_low) h.fail("UNLISTEN must release idle bus");
    h.t.cpu_clk_low=1; h.attention(true);
    h.send(0x48); h.send(0x60);
    h.t.cpu_data_low=1; h.attention(false); h.t.cpu_clk_low=0;
    const unsigned expected[]={1,8,0x11,0x22,0x33,0x44};
    for(unsigned i=0;i<(chained ? 260u : 6u);++i) {
        bool eoi;
        unsigned byte=h.receive(eoi);
        unsigned want = chained ? (i==0 ? 1 : i==1 ? 8 : (i ^ 0xa5) & 255) : expected[i];
        bool final = i==(chained ? 259u : 5u);
        if(byte!=want || eoi!=final) {
            std::printf("byte %u: expected %02x EOI %u, got %02x EOI %u\n", i, want, final, byte, eoi);
            h.fail("PRG byte or final EOI mismatch");
        }
    }
    h.t.cpu_data_low=0; h.tick(100);
    std::printf("c64 IEC: standard handshakes, exact %u-byte PRG and final EOI passed\n", chained ? 260 : 6);
}
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    run(false);
    run(true);
}
