// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vcoleco_machine.h"
#include "verilated.h"
#include <array>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <iterator>
#include <vector>

static void require(bool condition, const char *message) {
    if (!condition) { std::cerr << "Coleco CPU VDP: " << message << '\n'; std::exit(1); }
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    require(argc >= 2, "usage: Vcoleco_machine vdp-io.rom [full-size.rom]");
    Verilated::randReset(2);
    Verilated::randSeed(0x9918);
    Vcoleco_machine dut;
    dut.clk_sys = 0; dut.reset = 1; dut.media_ready = 0;
    dut.keyboard = 0xffffffffffULL; dut.peek_addr = 0; dut.media_data = 0;
    dut.eval();
    for (int file = 1; file < argc; ++file) {
        std::ifstream input(argv[file], std::ios::binary);
        require(input.good(), "cannot open ROM");
        const std::vector<uint8_t> rom{std::istreambuf_iterator<char>(input), {}};
        require(!rom.empty() && rom.size() <= 16384, "invalid raw ROM size");
        uint8_t registered_data = 0xff;
        const auto tick = [&]() {
            const unsigned address = dut.media_addr;
#ifdef FES_COLECO_OSS
            dut.media_data = registered_data;
#else
            dut.media_data = address < rom.size() ? rom[address] : 0xff;
#endif
            dut.clk_sys = 1; dut.eval(); dut.clk_sys = 0; dut.eval();
            registered_data = address < rom.size() ? rom[address] : 0xff;
        };
        const auto peek = [&](unsigned address) {
            dut.peek_addr = address; dut.eval(); tick(); tick();
            return unsigned(dut.peek_data);
        };
        dut.reset = 1; dut.media_ready = 0;
        for (unsigned i = 0; i < 4; ++i) tick();
        dut.media_size = rom.size(); dut.media_ready = 1;
        for (unsigned pass = 0; pass < 2; ++pass) {
            dut.reset = 1;
            for (unsigned i = 0; i < rom.size() + 32; ++i) tick();
            for (unsigned i = 0; i < rom.size(); ++i)
                require(peek(0x8000 + i) == rom[i], "uploaded cartridge byte mismatch");
            dut.reset = 0;
            unsigned cycles = 0;
            while (dut.cpu_halt_n && cycles++ < 30000000) tick();
            const unsigned result = peek(0x6000), count = peek(0x6001);
            std::cout << "CPU VDP " << rom.size() << " bytes reset=" << pass
                      << " result=" << std::hex << result << " nmi=" << count
                      << " status=" << peek(0x6002) << "," << peek(0x6003)
                      << " handler_error=" << peek(0x6004) << std::dec << std::endl;
            require(cycles < 30000000, "CPU timeout (never a pass)");
            require(result == 0xa5, "CPU diagnostic failure result");
            require(count == 2 && peek(0x6004) == 0, "NMI count/handler failure");
            require((peek(0x6002) & 0x80) && !(peek(0x6003) & 0x80), "held status IN lost/duplicated clear");
            const std::array<unsigned, 5> reads = {0x19, 0xa6, 0x73, 0x42, 0xbd};
            for (unsigned i = 0; i < reads.size(); ++i)
                require(peek(0x6010+i) == reads[i], "prefetch/sequential/wrap CPU sample");
            std::array<bool, 256*192> seen{};
            // Inspect actual output coordinates/pixels; never force raster,
            // VRAM, RAM, CPU registers, NMI or instruction execution.
            for (unsigned i = 0; i < 2*256*262*16; ++i) {
                tick();
                if (!dut.logical_blank && dut.logical_y < 192) {
                    const unsigned x = dut.logical_x, y = dut.logical_y;
                    const unsigned expected = x < 8 || x >= 248 || y < 8 || y >= 184 ? 2 : 0;
                    require(dut.logical_pixel == expected, "CPU pass picture pixel mismatch");
                    seen[y*256+x] = true;
                }
            }
            for (bool pixel : seen) require(pixel, "incomplete logical pass frame");
            require(!dut.cpu_halt_n && peek(0x6001) == 2, "pass did not remain halted with NMI disabled");
        }
    }
    std::cout << "Coleco CPU VDP buffered reads/status/NMI/pass picture passed\n";
}
