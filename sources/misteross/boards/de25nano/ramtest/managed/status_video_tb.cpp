#include "Vfes_de25_ramtest_video.h"
#include "verilated.h"
#include <cstdint>
#include <iostream>
#include <stdexcept>
static void check(bool ok, const char *why) { if (!ok) throw std::runtime_error(why); }
int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    try {
        Vfes_de25_ramtest_video d;
        const uint32_t colors[] = {0x2020e0, 0xe0a000, 0x00e000, 0xe00000, 0xe0e0e0};
        // Both counters start at zero, so observe a complete frame in each state.
        for (unsigned state = 0; state < 5; ++state) {
            d.result_flags = 1u << state;
            for (unsigned v = 0; v < 750; ++v) for (unsigned h = 0; h < 1650; ++h) {
                d.pixel_clk = 0; d.eval();
                const uint32_t rgb = d.video & 0xffffff;
                if (h == 160 + 3*270 && v == 3*76) check(rgb == colors[state], "wrong status marker");
                if (h == 160 + 3*270 && (v == 3*116 || v == 3*156))
                    check(rgb == 0x606060, "LPDDR bank implied tested");
                if (h == 160 + 3*270 && v == 3*28) check(rgb == 0xd020d0, "identity marker missing");
                d.pixel_clk = 1; d.eval();
            }
        }
        std::cout << "PASS status raster: waiting/running/pass/fail/stopped and both LPDDR banks untested\n";
    } catch (const std::exception &e) { std::cerr << "FAIL " << e.what() << '\n'; return 1; }
}
