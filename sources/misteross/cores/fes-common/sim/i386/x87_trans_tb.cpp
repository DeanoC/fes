// SPDX-License-Identifier: MIT
// fes_x87_trans against its model, bit for bit (#1958).
//
// Each line of the vector file, written by scripts/x87_trans_ucode.py
// --vectors, is an instruction, its operands as the unit normalises them, and
// what the model says the engine hands back: the unrounded last result and
// the other result of a two-result form. There is no tolerance here. The
// significand's round and sticky bits are compared as one, because the
// backend's lanes and the model need not split what lies below the guard the
// same way and no rounding can tell them apart.
//
//   Vx87_trans_tb_top <vectors>
// Output: "PASS <n>" or the failures, then "SUMMARY <passed> <failed>
// <clocks, most> <clocks, total>". Exit status 1 on any failure.
#include "Vx87_trans_tb_top.h"
#include "verilated.h"

#include <cinttypes>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    if (argc != 2) {
        std::fprintf(stderr, "usage: Vx87_trans_tb_top <vectors>\n");
        return 2;
    }
    std::FILE *file = std::fopen(argv[1], "r");
    if (!file) {
        std::fprintf(stderr, "cannot read %s\n", argv[1]);
        return 2;
    }
    Vx87_trans_tb_top top;
    auto tick = [&] {
        top.clk = 0;
        top.eval();
        top.clk = 1;
        top.eval();
    };
    top.reset = 1;
    top.start = 0;
    for (int n = 0; n < 4; ++n) tick();
    top.reset = 0;
    unsigned passed = 0, failed = 0, line = 0;
    uint64_t most = 0, total = 0;
    unsigned op, xs, xe, ys, ye, zero, sign, exponent, mode, rc, quotient, partial, inexact, away;
    uint64_t xm, ym;
    char sig_text[32], second_text[32];
    // The five trailing fields (#2027 slice 6): the rounding mode the engine
    // is given, and FPREM's quotient bits and partial flag and FRNDINT's PE
    // and C1, which the transcendentals' lines hold as zeros.
    while (std::fscanf(file, "%u %u %x %" SCNx64 " %u %x %" SCNx64 " %u %u %x %31s %u %31s %u %u %u %u %u", &op, &xs,
                       &xe, &xm, &ys, &ye, &ym, &zero, &sign, &exponent, sig_text, &mode, second_text, &rc, &quotient,
                       &partial, &inexact, &away) == 18) {
        ++line;
        // 67 bits: the top three, then sixteen hex digits.
        const unsigned sig_hi = unsigned(std::strtoul(std::string(sig_text, 1).c_str(), nullptr, 16));
        const uint64_t sig_lo = std::strtoull(sig_text + 1, nullptr, 16);
        const unsigned second_se = unsigned(std::strtoul(std::string(second_text, 4).c_str(), nullptr, 16));
        const uint64_t second_sig = std::strtoull(second_text + 4, nullptr, 16);
        top.instruction = op;
        top.rc = rc;
        top.xs = xs;
        top.xe = xe;
        top.xm = xm;
        top.ys = ys;
        top.ye = ye;
        top.ym = ym;
        top.start = 1;
        tick();
        top.start = 0;
        uint64_t clocks = 1;
        while (!top.done && clocks < 20000) {
            tick();
            ++clocks;
        }
        most = clocks > most ? clocks : most;
        total += clocks;
        bool ok = top.done;
        // fin_sig is 67 bits: Verilator packs it into three 32-bit words.
        const uint64_t got_lo = uint64_t(top.fin_sig[0]) | uint64_t(top.fin_sig[1]) << 32;
        const unsigned got_hi = top.fin_sig[2] & 7u;
        auto fold = [](uint64_t lo) { return (lo & ~3ull) | ((lo & 3) ? 1 : 0); };
        if (ok) {
            // An exact zero's sign is checked for the exact forms (instruction
            // 8 on), where FPREM's result depends on it; a transcendental's
            // zeros are the unit's, not the engine's.
            if (zero) ok = top.fin_zero && (op < 8 || top.fin_sign == sign);
            else
                ok = !top.fin_zero && top.fin_sign == sign && (top.fin_exp & 0x3ffff) == exponent && got_hi == sig_hi
                     && fold(got_lo) == fold(sig_lo);
            if (ok && top.second_mode != mode) ok = false;
            if (ok && (top.prem_q != quotient || top.prem_part != partial || top.rnd_pe != inexact
                       || top.rnd_c1 != away)) ok = false;
            if (ok && mode == 1) {
                const uint64_t s_lo = uint64_t(top.second[0]) | uint64_t(top.second[1]) << 32;
                const unsigned s_hi = top.second[2] & 0xffffu;
                ok = s_lo == second_sig && s_hi == second_se;
            }
        }
        if (ok) ++passed;
        else {
            ++failed;
            if (failed <= 20)
                std::printf("FAIL line %u op %u: done %d zero %d sign %d exp %05x sig %x%016" PRIx64 " mode %d, "
                            "model zero %u sign %u exp %05x sig %s mode %u\n",
                            line, op, int(top.done), int(top.fin_zero), int(top.fin_sign), unsigned(top.fin_exp) & 0x3ffff,
                            got_hi, got_lo, int(top.second_mode), zero, sign, exponent, sig_text, mode);
        }
        for (int n = 0; n < 3; ++n) tick();
    }
    std::fclose(file);
    if (!failed) std::printf("PASS %u\n", passed);
    std::printf("SUMMARY %u %u %" PRIu64 " %" PRIu64 "\n", passed, failed, most, total);
    return failed ? 1 : 0;
}
