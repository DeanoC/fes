// SPDX-License-Identifier: GPL-3.0-or-later
// PC/XT and ATA both contribute to the fes.computer 1.1 high capability word.
#include "Vcomputer_mailbox_pcxt_abi_top.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

static void require(bool ok, const char *what) {
    if (!ok) { std::cerr << "FAIL: " << what << '\n'; std::exit(1); }
}

struct Test {
    Vcomputer_mailbox_pcxt_abi_top d;
    uint32_t toggle = 0;
    Test() {
        d.clk=0; d.gpo=0; d.eval();
        for (unsigned i=0; i<6; ++i) tick();
    }
    void tick() {
        d.clk=1; d.eval(); d.clk=0; d.eval();
    }
    uint32_t exchange(uint8_t opcode, uint8_t index) {
        const uint32_t command=(uint32_t(opcode)<<24)|(uint32_t(index)<<16);
        d.gpo=(toggle<<31)|command;
        for (unsigned i=0; i<3; ++i) tick();
        toggle^=1; d.gpo=(toggle<<31)|command;
        for (unsigned i=0; i<40; ++i) {
            tick();
            if (((d.gpi>>23)&1)==toggle) return d.gpi;
        }
        require(false,"mailbox timeout"); return 0;
    }
};

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Test t;
    const uint32_t minor=t.exchange(1,6);
    require((minor&0xff000000u)==0xf5000000u && !(minor&(1u<<22)) && uint16_t(minor)==1,
            "combined 1.1 minor identity");
    const uint32_t capabilities=t.exchange(1,16);
    require((capabilities&0xff000000u)==0xf5000000u && !(capabilities&(1u<<22)) &&
            uint16_t(capabilities)==0x0011,
            "PC/XT and ATA capability bits must both survive in word 16");
    std::cout << "PASS combined PC/XT+ATA identity word 16 = 0x0011\n";
}
