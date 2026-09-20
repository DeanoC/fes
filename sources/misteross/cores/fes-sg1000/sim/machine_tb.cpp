// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vsg1000_machine.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <iostream>
#include <string>
#include <vector>

namespace {

[[noreturn]] void fail(const char *message) {
    std::cerr << "FES SG-1000 machine: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const char *message) {
    if (!condition) fail(message);
}

void tick(Vsg1000_machine &dut, const std::vector<uint8_t> &cartridge,
          uint8_t &registered_media_data) {
    const uint16_t requested_address = uint16_t(dut.media_addr);
#ifdef FES_SG1000_OSS
    dut.media_data = registered_media_data;
#else
    if (requested_address < cartridge.size())
        dut.media_data = cartridge[requested_address];
    else
        dut.media_data = 0xff;
#endif
    dut.clk_sys = 1;
    dut.eval();
    dut.clk_sys = 0;
    dut.eval();
#ifdef FES_SG1000_OSS
    registered_media_data = requested_address < cartridge.size()
                                ? cartridge[requested_address]
                                : 0xff;
#endif
}

uint8_t peek(Vsg1000_machine &dut, uint16_t address,
             uint8_t &registered_media_data) {
    dut.peek_addr = address;
    dut.eval();
#ifdef FES_SG1000_OSS
    dut.clk_sys = 1;
    dut.eval();
    dut.clk_sys = 0;
    dut.eval();
#endif
    return uint8_t(dut.peek_data);
}

void load_blob(Vsg1000_machine &dut, const std::vector<uint8_t> &blob,
               uint8_t &registered_media_data) {
    dut.reset = 1;
    dut.media_ready = 0;
    dut.media_size = uint16_t(blob.size());
    dut.media_data = 0;
    dut.peek_addr = 0;
    dut.eval();
    for (unsigned i = 0; i < 4; ++i)
        tick(dut, blob, registered_media_data);
    dut.media_ready = 1;
    dut.eval();
    for (unsigned i = 0; i < blob.size() + 32; ++i)
        tick(dut, blob, registered_media_data);
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);

    std::vector<uint8_t> pattern(16384);
    for (unsigned i = 0; i < pattern.size(); ++i)
        pattern[i] = uint8_t(0x40 ^ i ^ (i >> 8));

    Vsg1000_machine dut;
    dut.clk_sys = 0;
    dut.reset = 1;
    dut.keyboard = 0xffffffffffull;
    dut.media_ready = 0;
    dut.media_size = 0;
    dut.media_data = 0;
    dut.peek_addr = 0;
    dut.eval();
    uint8_t registered_media_data = 0;
    for (unsigned i = 0; i < 8; ++i)
        tick(dut, pattern, registered_media_data);

    load_blob(dut, pattern, registered_media_data);
    require(peek(dut, 0x0000, registered_media_data) == pattern[0],
            "cartridge base byte");
    require(peek(dut, 0x3fff, registered_media_data) == pattern.back(),
            "cartridge final byte");
    require(peek(dut, 0x4000, registered_media_data) == 0xff,
            "unmapped 4000 must read FF");
    require(peek(dut, 0x8000, registered_media_data) == 0xff,
            "unmapped 8000 must read FF");
    require(uint8_t(dut.port_dc) == 0xff, "neutral DC");
    require(uint8_t(dut.port_dd) == 0xff, "neutral DD");

    std::vector<uint8_t> ram_prog{0xf3, 0x3e, 0x5a, 0x32, 0x00, 0xc0, 0x76};
    load_blob(dut, ram_prog, registered_media_data);
    require(peek(dut, 0x0000, registered_media_data) == 0xf3,
            "tiny program base");
    dut.reset = 0;
    unsigned cycles = 0;
    for (; cycles < 100000 && dut.cpu_halt_n; ++cycles)
        tick(dut, ram_prog, registered_media_data);
    require(cycles < 100000, "RAM probe did not HALT");
    require(peek(dut, 0xc000, registered_media_data) == 0x5a,
            "CPU did not write RAM at C000");
    require(peek(dut, 0xc400, registered_media_data) == 0x5a,
            "1 KiB RAM mirror at C400");
    require(peek(dut, 0xffff, registered_media_data) != 0x5a,
            "FFFF aliases C3FF, not C000");

    std::vector<uint8_t> joy_prog{0xf3, 0xdb, 0xdc, 0x32, 0x01, 0xc0,
                                  0xdb, 0xdd, 0x32, 0x02, 0xc0, 0x76};
    dut.keyboard = 0xffffffffffull ^ 0x01ull;  // P1 Up
    load_blob(dut, joy_prog, registered_media_data);
    dut.reset = 0;
    cycles = 0;
    for (; cycles < 100000 && dut.cpu_halt_n; ++cycles)
        tick(dut, joy_prog, registered_media_data);
    require(cycles < 100000, "joystick probe did not HALT");
    require(uint8_t(dut.port_dc) == 0xfe, "P1 Up should clear DC bit0");
    require(peek(dut, 0xc001, registered_media_data) == 0xfe,
            "CPU IN (DC) mismatch");
    require(peek(dut, 0xc002, registered_media_data) == 0xff,
            "CPU IN (DD) mismatch");

    if (argc > 1) {
        FILE *rom = std::fopen(argv[1], "rb");
        require(rom != nullptr, "cannot open diagnostic ROM");
        std::vector<uint8_t> diagnostic;
        uint8_t byte = 0;
        while (std::fread(&byte, 1, 1, rom) == 1)
            diagnostic.push_back(byte);
        std::fclose(rom);
        require(!diagnostic.empty(), "empty diagnostic ROM");
        dut.keyboard = 0xffffffffffull;
        load_blob(dut, diagnostic, registered_media_data);
        dut.reset = 0;
        cycles = 0;
        for (; cycles < 40000000 && dut.cpu_halt_n; ++cycles)
            tick(dut, diagnostic, registered_media_data);
        require(cycles < 40000000, "diagnostic did not HALT");
        require(peek(dut, 0xc000, registered_media_data) == 0xa5,
                "diagnostic RAM signature");
        require(peek(dut, 0xc001, registered_media_data) == 0xff,
                "diagnostic captured DC");
    }

    if (argc > 2) {
        FILE *rom = std::fopen(argv[2], "rb");
        require(rom != nullptr, "cannot open controller diagnostic ROM");
        std::vector<uint8_t> controller_diagnostic;
        uint8_t byte = 0;
        while (std::fread(&byte, 1, 1, rom) == 1)
            controller_diagnostic.push_back(byte);
        std::fclose(rom);
        require(!controller_diagnostic.empty(), "empty controller diagnostic ROM");
        dut.keyboard = 0xffffffffffull;
        load_blob(dut, controller_diagnostic, registered_media_data);
        dut.reset = 0;
        auto wait_for_cache = [&](uint8_t expected_dc, uint8_t expected_dd,
                                  const char *message) {
            for (unsigned i = 0; i < 40000000; ++i) {
                tick(dut, controller_diagnostic, registered_media_data);
                if ((i & 1023) == 0 &&
                    peek(dut, 0xc001, registered_media_data) == expected_dc &&
                    peek(dut, 0xc002, registered_media_data) == expected_dd)
                    return;
            }
            fail(message);
        };

        wait_for_cache(0xff, 0xff, "controller diagnostic neutral cache timeout");
        require(uint8_t(dut.port_dc) == 0xff, "controller diagnostic neutral DC");
        require(uint8_t(dut.port_dd) == 0xff, "controller diagnostic neutral DD");

        dut.keyboard = 0xffffffffffull ^ 0x01ull ^ (0x01ull << 9);
        wait_for_cache(0xfe, 0xfd, "controller diagnostic pressed cache timeout");
        require(uint8_t(dut.port_dc) == 0xfe, "controller diagnostic pressed DC");
        require(uint8_t(dut.port_dd) == 0xfd, "controller diagnostic pressed DD");
        require(peek(dut, 0xc001, registered_media_data) == 0xfe,
                "controller diagnostic cached pressed DC");
        require(peek(dut, 0xc002, registered_media_data) == 0xfd,
                "controller diagnostic cached pressed DD");
    }

    std::cout << "FES SG-1000 machine checks passed\n";
    return 0;
}
