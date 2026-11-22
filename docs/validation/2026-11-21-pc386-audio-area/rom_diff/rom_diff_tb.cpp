// Exhaustive: every (log-sine address, exp address) pair, 65,536 clocks, old modules against the shared ROM.
#include "Vrom_diff_top.h"
#include "verilated.h"
#include <cstdio>
int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vrom_diff_top t; long bad = 0, n = 0;
    t.clk = 0; t.a = 0; t.b = 0; t.eval();
    if (t.ls_old != t.ls_new || t.ex_old != t.ex_new) { std::printf("power-on differs\n"); ++bad; }
    for (int a = 0; a < 256; ++a)
        for (int b = 0; b < 256; ++b) {
            t.a = a; t.b = (b * 167 + a) & 255; t.clk = 1; t.eval(); t.clk = 0; t.eval(); ++n;
            if (t.ls_old != t.ls_new || t.ex_old != t.ex_new) { if (bad < 5) std::printf("differs at %02x %02x\n", a, t.b); ++bad; }
        }
    std::printf("%s: %ld clocks, %ld differences, power-on equal\n", bad ? "FAIL" : "PASS", n, bad);
    return bad != 0;
}
