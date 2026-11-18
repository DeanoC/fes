// SPDX-License-Identifier: MIT
// The lockstep bench's copy of the x87 built set against the RTL's, over all
// 2^11 x87 opcodes at both backend values (#1958).
//
// x87_built_form.h says which ESC forms the lockstep steps on the RTL and
// which it expects to stop as not built; fes_i386_pkg's x87_decode and
// x87_format say which the RTL builds. They are two tables written apart,
// and a form on one and not the other is either a run the lockstep ends
// early with every check passing or an instruction it steps that the RTL
// stops at. This compares them mechanically instead of by reading both.
//
// Output: one "MISMATCH <backend> <esc> <modrm> rtl <0|1> bench <0|1>" line a
// disagreement, then "BUILT de10 <count> de25 <count> mismatches <count>".
// Exit status 1 on any mismatch. scripts/test_fes_i386_lockstep.py builds
// and runs it.
#include "Vx87_built_tb_top.h"
#include "verilated.h"
#include "x87_built_form.h"

#include <cstdio>

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vx87_built_tb_top top;
    unsigned built[2] = {0, 0}, mismatches = 0;
    for (unsigned op = 0; op < 2048; ++op) {
        const uint8_t esc = uint8_t(0xd8 | (op >> 8)), modrm = uint8_t(op);
        top.op = op;
        top.eval();
        for (unsigned backend = 0; backend < 2; ++backend) {
            const bool rtl = backend ? top.built_de25 : top.built_de10;
            const bool bench = x87_built_form(esc, modrm, backend == 1);
            if (rtl) ++built[backend];
            if (rtl != bench) {
                ++mismatches;
                std::printf("MISMATCH %s %02x %02x rtl %d bench %d\n", backend ? "de25" : "de10", unsigned(esc),
                            unsigned(modrm), int(rtl), int(bench));
            }
        }
    }
    std::printf("BUILT de10 %u de25 %u mismatches %u\n", built[0], built[1], mismatches);
    return mismatches ? 1 : 0;
}
