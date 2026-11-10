// S1c unit bench driver: see lcell_comb_eq.v.
#include "Vlcell_comb_eq.h"
#include <cstdio>
#include <cstring>
int main() {
    Vlcell_comb_eq t;
    long bad = 0, n = 0;
    // every value, then every ordered pair of values, so a stale latch in either model shows
    for (int a = 0; a < 512; ++a) {
        for (int b = 0; b < 512; ++b) {
            t.in = a; t.eval();
            t.in = b; t.eval();
            ++n;
            if (std::memcmp(t.intel.data(), t.fes.data(), sizeof(t.intel)) != 0) {
                if (bad++ < 10) std::printf("mismatch after %03x -> %03x\n", a, b);
            }
        }
    }
    std::printf("lcell_comb_eq: %ld transitions, %ld mismatches\n", n, bad);
    return bad != 0;
}
