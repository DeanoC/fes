// SPDX-License-Identifier: GPL-2.0-or-later
// Exercise linked BIOS, MegaCart and an installed SGM through real T80 cycles.
#include "Vsgm_shell_machine.h"
#include "verilated.h"

#include <array>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <stdexcept>

static void require(bool ok, const char* why) {
    if (!ok) throw std::runtime_error(why);
}

static void write_rom() {
    std::array<uint8_t, 139264> rom{};
    rom.fill(0xff);
    rom[0] = 0xc3; rom[1] = 0x00; rom[2] = 0x80; // JP 8000h
    // Distinct values force the T80 to sample each new address at its read edge.
    rom[8192] = 0x10;                         // bank 0, C000h
    rom[8192 + 3 * 16384] = 0x30;             // bank 3, C000h
    rom[8192 + 3 * 16384 + 0x3fc3] = 0x33;    // select bank 3, FFC3h
    rom[8192 + 5 * 16384] = 0x50;             // bank 5, C000h
    rom[8192 + 5 * 16384 + 0x3fc5] = 0x55;    // select bank 5, FFC5h
    rom[8192 + 7 * 16384 + 0x3fc3] = 0x73;    // stale selector value
    constexpr uint8_t program[] = {
        0x3a, 0x00, 0xc0, 0x32, 0x00, 0x60, // LD A,(C000); LD (6000),A
        0x3a, 0xc3, 0xff, 0x32, 0x01, 0x60, // selecting read; save value
        0x3a, 0x00, 0xc0, 0x32, 0x02, 0x60, // new bank; save value
        0x3a, 0xc5, 0xff, 0x32, 0x03, 0x60, // next selector; save value
        0x3a, 0x00, 0xc0, 0x32, 0x04, 0x60, // bank 5 immediately follows
        0x3a, 0x00, 0x80, 0x32, 0x05, 0x60, // fixed bank still bank 7
        0x3e, 0x01, 0xd3, 0x53,             // enable SGM upper RAM window
        0x3a, 0x00, 0x80, 0x47,             // cartridge read with SGM active
        0xaf, 0xd3, 0x53, 0x78,             // disable window; restore A
        0x32, 0x06, 0x60,                   // save sampled cartridge value
        0x76,
    };
    for (size_t i = 0; i < sizeof(program); ++i) rom[8192 + 7 * 16384 + i] = program[i];
    std::filesystem::create_directories("build/diagnostics/fes-coleco");
    std::ofstream image("build/diagnostics/fes-coleco/megacart.hex");
    require(bool(image), "cannot create synthetic MegaCart image");
    for (uint8_t value : rom) {
        char byte[4];
        std::snprintf(byte, sizeof(byte), "%02x", value);
        image << byte << '\n';
    }
    require(bool(image), "cannot write synthetic MegaCart image");
}

static void tick(Vsgm_shell_machine& core) {
    core.clk_sys = 1; core.eval();
    core.clk_sys = 0; core.eval();
}

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    write_rom();
    Vsgm_shell_machine core;
    core.clk_sys = 0;
    core.reset = 1;
    core.media_ready = 0;
    core.media_size = 0;
    core.media_data = 0;
    core.peek_addr = 0x6000;
    core.eval();
    for (unsigned i = 0; i < 64; ++i) tick(core);
    core.reset = 0;
    unsigned cycles = 0;
    unsigned cart_cycles = 0;
    unsigned active_sgm_cart_cycles = 0;
    bool sgm_upper_enabled = false;
    for (; cycles < 300000 && core.cpu_halt_n; ++cycles) {
        const uint32_t request = core.plug_request;
        const uint16_t addr = request & 0xffff;
        if (!(request & (1u << 25)) && !(request & (1u << 27)) &&
            (addr & 0xff) == 0x53)
            sgm_upper_enabled = (request >> 16) & 1;
        const bool mem_read = !(request & (1u << 24)) && !(request & (1u << 26));
        if (mem_read && addr >= 0x8000) {
            ++cart_cycles;
            if (sgm_upper_enabled) ++active_sgm_cart_cycles;
            require((core.plug_response & ((1u << 11) | (1u << 8))) == 0,
                    "SGM claimed a cartridge read");
        }
        tick(core);
    }
    require(cycles < 300000, "linked MegaCart CPU program did not halt");
    require(cart_cycles > 0, "cartridge claim check did not run");
    require(active_sgm_cart_cycles > 0, "SGM-active cartridge claim check did not run");
    for (unsigned i = 0; i < 7; ++i) {
        core.peek_addr = 0x6000 + i;
        tick(core);
        constexpr uint8_t expected[] = {0x10, 0x33, 0x30, 0x55, 0x50, 0x3a, 0x3a};
        require(core.peek_data == expected[i], "T80 sampled wrong MegaCart byte");
    }
    std::printf("Coleco linked MegaCart CPU/SGM probe passed after %u cycles\n", cycles);
}
