// SPDX-License-Identifier: MIT
#include "Vc64_boot_sim_top.h"
#include "Vc64_boot_sim_top___024root.h"
#include "verilated.h"
#include <cstdio>
#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <vector>

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    // Two machine seconds, using the production clock ratio. Plusargs select
    // external firmware/disk hex files; all copyrighted bytes stay outside Git.
    Vc64_boot_sim_top top;
    top.reset = 1;
    uint64_t sys_edge = 0, pixel_edge = 0;
    std::vector<uint8_t> frame, last_frame;
    bool previous_vsync = false;
    bool recording = false;
    struct Cycle { uint16_t bus, pc; uint8_t data, state; };
    Cycle history[64] = {};
    unsigned history_count = 0;
    while (sys_edge < 2ull * 52224000 * 2) {
        const bool system = sys_edge * 74250000 <= pixel_edge * 52224000;
        if (system) {
            top.clk_sys = !(sys_edge & 1);
            ++sys_edge;
            if (sys_edge == 800) top.reset = 0;
        } else {
            top.pixel_clk = !(pixel_edge & 1);
            ++pixel_edge;
        }
        if (system && top.clk_sys && top.rootp->c64_boot_sim_top__DOT__machine__DOT__cpu_ce) {
            auto *r = top.rootp;
            history[history_count++ % 64] = {top.debug_pc,
                r->c64_boot_sim_top__DOT__machine__DOT__cpu__DOT__PC,
                r->c64_boot_sim_top__DOT__machine__DOT__cpu_di,
                r->c64_boot_sim_top__DOT__machine__DOT__cpu__DOT__state};
        }
        top.eval();
        if (!system && top.pixel_clk) {
            if (top.vsync && !previous_vsync) {
                if (frame.size() == 1280 * 720 * 3) last_frame.swap(frame);
                frame.clear();
                recording = true;
            }
            if (recording && top.de) {
                frame.push_back(top.red); frame.push_back(top.green); frame.push_back(top.blue);
            }
            previous_vsync = top.vsync;
        }
    }
    const auto &ram = top.rootp->c64_boot_sim_top__DOT__machine__DOT__main_ram__DOT__ram;
    std::ofstream memory("boot-ram.bin", std::ios::binary);
    memory.write(reinterpret_cast<const char *>(&ram[0]), 65536);
    if (!last_frame.empty()) {
        std::ofstream image("boot.ppm", std::ios::binary);
        image << "P6\n1280 720\n255\n";
        image.write(reinterpret_cast<const char *>(last_frame.data()), last_frame.size());
    }
    std::printf("C64 two-second boot observation: bus PC=%04x, screen codes:\n", top.debug_pc);
    for (int row = 0; row < 25; ++row) {
        for (int col = 0; col < 40; ++col) {
            uint8_t c = ram[0x400 + row * 40 + col] & 0x7f;
            std::putchar(c >= 1 && c <= 26 ? c + 'A' - 1 : c >= 32 && c <= 63 ? c : '.');
        }
        std::putchar('\n');
    }
    std::puts("Final CPU cycles (bus address, internal PC, data, microcode state):");
    for (unsigned i = history_count > 64 ? history_count - 64 : 0; i < history_count; ++i) {
        const auto &c = history[i % 64];
        std::printf("%04x %04x %02x %u\n", c.bus, c.pc, c.data, c.state);
    }
    // This is an observation harness; successful execution is not boot or
    // demo acceptance. RAM and the complete HDMI frame are the evidence.
    return 0;
}
