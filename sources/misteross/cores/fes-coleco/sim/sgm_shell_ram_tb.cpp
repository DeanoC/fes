// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vsgm_shell_machine.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <initializer_list>
#include <iostream>
#include <iterator>
#include <vector>

static void require(bool ok, const char *why) {
    if (!ok) { std::cerr << why << '\n'; std::exit(EXIT_FAILURE); }
}
static void emit(std::vector<uint8_t> &rom, std::initializer_list<uint8_t> bytes) {
    rom.insert(rom.end(), bytes);
}
static void expect_a(std::vector<uint8_t> &rom, uint8_t value, std::vector<size_t> &patches) {
    emit(rom, {0xfe, value, 0xc2, 0, 0}); // CP immediate; JP NZ failure
    patches.push_back(rom.size() - 2);
}
struct Driver {
    Vsgm_shell_machine dut;
    std::vector<uint8_t> rom;
    uint8_t data = 0;
    bool upper = false;
    bool lower = false;
    unsigned upper_claims = 0;
    unsigned wait_ticks = 0;
    void tick() {
#ifndef SGM_INTEGRATED
        const uint32_t request = dut.plug_request;
        const uint16_t addr = request & 0xffff;
        const bool reset = request & (1u << 30);
        const bool io_write = !(request & (1u << 25)) && !(request & (1u << 27));
        const bool mem_cycle = !(request & (1u << 24)) &&
                               (!(request & (1u << 26)) || !(request & (1u << 27)));
        if (reset) { upper = false; lower = false; }
        else if (io_write && (addr & 0xff) == 0x53) upper = (request >> 16) & 1;
        else if (io_write && (addr & 0xff) == 0x7f) lower = !((request >> 17) & 1);
        // Deliberately malformed responder claims cartridge cycles too: the
        // shell must reject bit 11 outside its lower-32-KiB aperture.
        const bool forbidden_claim = mem_cycle && addr >= 0x8000;
        if (forbidden_claim) ++upper_claims;
        const bool claim = forbidden_claim ||
                           (mem_cycle && (addr < 0x2000 ? lower : upper));
        dut.plug_response = claim ? (1u << 11) : 0;
        if (!reset && mem_cycle && addr == 0x2000 &&
            !(request & (1u << 26)) && wait_ticks < 32) {
            dut.plug_response |= 1u << 9;
            ++wait_ticks;
        }
#endif
        const uint16_t media_addr = dut.media_addr;
        dut.media_data = data;
        dut.clk_sys = 1; dut.eval();
        dut.clk_sys = 0; dut.eval();
        data = media_addr < rom.size() ? rom[media_addr] : 0xff;
    }
};

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Driver d;
#ifdef SGM_INTEGRATED
    if (argc == 2) {
        std::ifstream file(argv[1], std::ios::binary);
        require(bool(file), "cannot open SGM cartridge probe");
        d.rom.assign(std::istreambuf_iterator<char>(file), {});
        require(!d.rom.empty() && d.rom.size() <= 16384, "invalid SGM cartridge probe size");
        d.dut.clk_sys = 0; d.dut.reset = 1; d.dut.media_ready = 1;
        d.dut.media_size = d.rom.size(); d.dut.media_data = 0;
        d.dut.peek_addr = 0x6000; d.dut.eval();
        for (size_t i = 0; i < d.rom.size() + 32; ++i) d.tick();
        d.dut.reset = 0;
        unsigned cycles = 0;
        for (; cycles < 50000000 && d.dut.cpu_halt_n; ++cycles) d.tick();
        d.tick();
        require(cycles < 50000000, "SGM cartridge probe did not reach pass frame");
        require(d.dut.peek_data == 0x66, "SGM cartridge probe changed hidden console RAM");
        std::cout << "Coleco SGM cartridge probe passed after " << cycles << " cycles\n";
        return 0;
    }
#endif
    require(argc == 1, "unexpected SGM shell RAM test argument");
    std::vector<size_t> patches;
    // The console's mirrored RAM must remain intact beneath the SGM window.
    emit(d.rom, {0x3e, 0xd4, 0x32, 0x00, 0x60});
    emit(d.rom, {0x3e, 0x01, 0xd3, 0x53});
    for (uint16_t addr : {0x2000, 0x5fff, 0x6000, 0x7fff}) {
        const uint8_t value = uint8_t((addr >> 8) ^ 0xa5);
        emit(d.rom, {0x3e, value, 0x32, uint8_t(addr), uint8_t(addr >> 8),
                     0x3a, uint8_t(addr), uint8_t(addr >> 8)});
        expect_a(d.rom, value, patches);
    }
    emit(d.rom, {0xaf, 0xd3, 0x53, 0x3a, 0x00, 0x60});
    expect_a(d.rom, 0xd4, patches);
    emit(d.rom, {0xaf, 0xd3, 0x7f});
    for (uint16_t addr : {0x0000, 0x1fff}) {
        const uint8_t value = uint8_t((addr >> 8) ^ 0xc7);
        emit(d.rom, {0x3e, value, 0x32, uint8_t(addr), uint8_t(addr >> 8),
                     0x3a, uint8_t(addr), uint8_t(addr >> 8)});
        expect_a(d.rom, value, patches);
    }
    emit(d.rom, {0x3e, 0x02, 0xd3, 0x7f, 0x3a, 0x00, 0x00});
    expect_a(d.rom, 0xc3, patches);
    emit(d.rom, {0x3e, 0xa5, 0x32, 0x01, 0x60, 0x76});
    const uint16_t fail = 0x8000 + d.rom.size();
    emit(d.rom, {0x3e, 0xe1, 0x32, 0x01, 0x60, 0x76});
    for (size_t p : patches) { d.rom[p] = fail & 0xff; d.rom[p + 1] = fail >> 8; }

    d.dut.clk_sys = 0; d.dut.reset = 1; d.dut.media_ready = 1;
    d.dut.media_size = d.rom.size(); d.dut.media_data = 0;
#ifndef SGM_INTEGRATED
    d.dut.peek_addr = 0x6001; d.dut.plug_response = 0; d.dut.eval();
#else
    d.dut.peek_addr = 0x6001; d.dut.eval();
#endif
    for (size_t i = 0; i < d.rom.size() + 32; ++i) d.tick();
    d.dut.reset = 0;
    unsigned cycles = 0;
    for (; cycles < 300000 && d.dut.cpu_halt_n; ++cycles) d.tick();
    require(cycles < 300000, "SGM shell CPU program did not halt");
    d.tick();
    require(d.dut.peek_data == 0xa5, "SGM shell RAM overlay or console RAM isolation failed");
#ifndef SGM_INTEGRATED
    require(d.upper_claims > 0, "forbidden upper-memory claim was not exercised");
    require(d.wait_ticks == 32, "WAIT-stretched RAM read was not exercised");
#endif
    std::cout << "Coleco SGM shell RAM CPU/socket probe passed\n";
}
