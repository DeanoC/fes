// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vcoleco_megacart_rom.h"
#include "verilated.h"
#include <cstdio>
#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <stdexcept>

static void tick(Vcoleco_megacart_rom& core) {
    for (int cycle = 0; cycle < 2; ++cycle) {
        core.clk = 0;
        core.eval();
        core.clk = 1;
        core.eval();
    }
}
static void check(bool ok, const char* what) {
    if (!ok) throw std::runtime_error(what);
}
int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    std::filesystem::create_directories("build/diagnostics/fes-coleco");
    {
        std::ofstream image("build/diagnostics/fes-coleco/megacart.hex");
        for (unsigned i = 0; i < 139264; ++i) {
            unsigned value = i < 8192 ? (i == 0 ? 0xc3 : i == 1 ? 0x00 : i == 2 ? 0x80 : 0xff)
                                      : ((i - 8192) / 16384) * 16 + (i & 15);
            char byte[4];
            std::snprintf(byte, sizeof(byte), "%02x", value);
            image << byte << '\n';
        }
    }
    Vcoleco_megacart_rom core;
    core.clk = 0;
    core.reset = 1;
    core.ce_cpu_n = 1;
    core.mem_read = 1;
    core.cpu_addr = 0;
    tick(core);
    check(core.data == 0xc3 && core.selected_bank_debug == 0, "linked BIOS reset vector");
    core.reset = 0;
    core.cpu_addr = 0x8000;
    tick(core);
    check(core.data == 0x70, "fixed final bank");
    core.cpu_addr = 0xc000;
    tick(core);
    check(core.data == 0x00, "bank zero on reset");
    for (unsigned bank = 0; bank < 8; ++bank) {
        core.cpu_addr = 0xffc0 | bank;
        tick(core);
        check(core.selected_bank_debug == bank && core.data == bank * 16 + bank,
              "selector read must return newly selected bank");
        tick(core);
        check(core.data == bank * 16 + bank, "held selector read");
        core.cpu_addr = 0xc000;
        tick(core);
        check(core.data == bank * 16, "following banked read");
        core.cpu_addr = 0x8000;
        tick(core);
        check(core.data == 0x70, "fixed bank after switch");
    }
    core.mem_read = 0;
    core.cpu_addr = 0xffc2;
    tick(core);
    check(core.selected_bank_debug == 7, "write must not switch bank");
    core.reset = 1;
    tick(core);
    check(core.selected_bank_debug == 0, "reset after Stop");
    return 0;
}
