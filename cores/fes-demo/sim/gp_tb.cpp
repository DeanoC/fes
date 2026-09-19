// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_application_gp.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <fstream>
#include <vector>

static void require(bool ok, const char* message) {
    if (!ok) { std::cerr << message << '\n'; std::exit(1); }
}
struct Mailbox {
    Vfes_application_gp dut;
    bool toggle = false;
    std::vector<uint8_t> bytes;
    Mailbox() {
        dut.clk = 0; dut.gpo = 0;
        dut.build_id[3] = 0x00112233; dut.build_id[2] = 0x44556677;
        dut.build_id[1] = 0x8899aabb; dut.build_id[0] = 0xccddeeff;
        dut.eval();
    }
    void tick() {
        if (dut.media_write_enable & 1) {
            require(dut.media_write_addr == bytes.size(), "write address discontinuity");
            bytes.push_back(dut.media_write_data & 255);
            if (dut.media_write_enable & 2) bytes.push_back(dut.media_write_data >> 8);
        }
        dut.clk = 1; dut.eval(); dut.clk = 0; dut.eval();
    }
    uint32_t command(unsigned op, unsigned index, unsigned arg) {
        auto fields = (op << 24) | (index << 16) | arg;
        // Hold payload before toggle; no request may execute during this hold.
        const auto previous = dut.gpi;
        dut.gpo = fields | (uint32_t(toggle) << 31); dut.eval();
        tick(); require(dut.gpi == previous, "payload without toggle executed");
        toggle = !toggle;
        dut.gpo = fields | (uint32_t(toggle) << 31); dut.eval();
        for (int i=0; i<8 && bool(dut.gpi & 0x800000) != toggle; ++i) tick();
        require(bool(dut.gpi & 0x800000) == toggle, "ACK timeout");
        const auto result = dut.gpi;
        for (int i=0;i<5;++i) tick();
        require(result == dut.gpi, "repeated request changed response");
        return result & 0x40ffff;
    }
    void ok(unsigned op, unsigned index, unsigned arg) {
        require(command(op,index,arg)==0, "expected successful command");
    }
    void error(unsigned op,unsigned index,unsigned arg,unsigned code) {
        require(command(op,index,arg)==(0x400000|code), "wrong rejection");
    }
};
int main(int argc, char** argv) {
    Verilated::commandArgs(argc,argv);
    if (argc == 2) {
        Mailbox fixture;
        std::ifstream stream(argv[1]);
        require(bool(stream), "fixture missing");
        uint32_t fields, request, expected;
        unsigned count = 0;
        while (stream >> fields >> request >> expected) {
            require((fields ^ request) == 0x80000000, "fixture framing");
            fixture.command((request >> 24) & 127, (request >> 16) & 255, request & 65535);
            require(fixture.dut.gpi == expected, "shared fixture response mismatch");
            ++count;
        }
        require(count > 0, "empty shared fixture");
    }
    Mailbox m;
    require(m.dut.exec_reset && !m.dut.buttons && !m.dut.media_ready, "startup state");
    const unsigned identity[] = {0x4546,0x3153,1,0,3,1,0,2|GAMEPAD|(MEDIA<<2)|(AUDIO<<4),
        0x1100,0x3322,0x5544,0x7766,0x9988,0xbbaa,0xddcc,0xffee};
    for(unsigned i=0;i<16;++i) require(m.command(1,i,0)==identity[i], "identity mismatch");
    m.error(1,16,0,2); m.error(1,0,1,3); m.error(127,0,0,1);
    m.error(7,0,0,1); // Stream is independently unimplemented, never advertised.
    if (GAMEPAD) {
        m.ok(3,0,0xa5); require(m.dut.buttons==0xa5,"buttons");
        m.error(3,1,0,2); m.error(3,0,256,3);
        require(m.dut.buttons==0xa5,"invalid command changed buttons");
    } else m.error(3,0,1,1);
    if (MEDIA) {
        m.error(2,0,1,4); // Cannot execute before first complete asset.
        m.error(4,0,0,3); m.error(4,0,16385,3);
        m.ok(4,0,3);
        m.error(5,1,0x77,3); require(m.bytes.empty(),"early tail wrote media");
        m.ok(5,0,0x3412);
        m.error(4,0,1,4); // Failed nested BEGIN preserves partial data.
        if (GAMEPAD) m.ok(3,0,0x18);
        require(m.command(1,4,0)==3,"interleaved identity");
        m.ok(2,0,0); // HOLD preserves partial transfer and neutralizes input.
        require(!m.dut.buttons,"HOLD did not neutralize input");
        m.error(2,0,1,4); m.error(6,0,0,4);
        m.error(5,1,0x100,3); m.ok(5,1,0x56); m.ok(6,0,0);
        require(m.bytes==std::vector<uint8_t>({0x12,0x34,0x56}),"interleaving corrupted media");
        require(m.dut.media_size==3 && m.dut.media_ready,"media publication");
        require(m.dut.media_byte0==0x12 && m.dut.media_byte1==0x34 && m.dut.media_byte2==0x56,"palette cache");
        m.ok(2,0,1); require(!m.dut.exec_reset,"release failed");
        m.error(4,0,3,4); require(m.dut.media_ready,"live BEGIN damaged committed data");
        m.ok(2,0,0); m.ok(2,0,1); // Committed asset survives HOLD.
        m.ok(2,0,0); m.bytes.clear(); m.ok(4,0,16384);
        for(unsigned i=0;i<16384;i+=2) m.ok(5,0,((i+1)&255)<<8|(i&255));
        m.error(5,1,0,3); m.ok(6,0,0);
        require(m.bytes.size()==16384 && m.dut.media_size==16384,"maximum blob");
        for(unsigned i=0;i<16384;++i) require(m.bytes[i]==uint8_t(i),"maximum blob contents");
        m.ok(2,0,0); m.bytes.clear(); m.ok(4,0,1);
        require(!m.dut.media_ready,"replacement did not invalidate readiness");
        m.error(2,0,1,4); m.ok(5,1,0xaa); m.ok(6,0,0); m.ok(2,0,1);
        require(m.dut.media_byte1==0 && m.dut.media_byte2==0,"short replacement retained palette bytes");
    } else {
        m.error(4,0,3,1); m.error(5,0,0,1); m.error(6,0,0,1);
        require(m.bytes.empty(),"absent media endpoint wrote data"); m.ok(2,0,1);
    }
    m.ok(2,0,0); require(m.dut.exec_reset && !m.dut.buttons,"final HOLD");
    std::cout << "application GP passed gamepad=" << GAMEPAD << " media=" << MEDIA << '\n';
}
