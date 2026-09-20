// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vcoleco_machine.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>
#include <vector>

namespace {

// Historical independent CPU oracle vectors translated only at test stimulus.
void set_controller_state(Vcoleco_machine &dut, uint64_t matrix) {
    uint16_t buttons = 0;
    for (unsigned p = 0; p < 2; ++p) {
        const unsigned old = unsigned(~(matrix >> (5*p))) & 31;
        const unsigned pad = (old & 1) | ((old & 2) << 2) | ((old & 4) >> 1) |
                             ((old & 8) >> 1) | (old & 16) |
                             (((~matrix >> (10+p)) & 1) << 5);
        buttons |= pad << (8*p);
    }
    dut.controller_buttons = buttons;
    dut.controller_keypad = (~matrix >> 12) & 0xffffff;
}


[[noreturn]] void fail(const char *message) {
    std::cerr << "FES Coleco machine: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const char *message) {
    if (!condition) fail(message);
}

void tick(Vcoleco_machine &dut, const std::vector<uint8_t> &cartridge,
          uint8_t &registered_media_data) {
    const uint16_t requested_address = uint16_t(dut.media_addr);
#ifdef FES_COLECO_OSS
    // The OSS mailbox RAM returns the byte requested on the preceding edge.
    // Keep the standalone machine test at the same interface timing as top.v.
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
#ifdef FES_COLECO_OSS
    registered_media_data = requested_address < cartridge.size()
                                ? cartridge[requested_address]
                                : 0xff;
#endif
}

uint8_t peek(Vcoleco_machine &dut, uint16_t address,
             uint8_t &registered_media_data) {
    dut.peek_addr = address;
    dut.eval();
#ifdef FES_COLECO_OSS
    // The OSS RAM's B port is registered too.
    dut.clk_sys = 1;
    dut.eval();
    dut.clk_sys = 0;
    dut.eval();
#endif
    return uint8_t(dut.peek_data);
}

void controller_reads(uint64_t matrix) {
    // Independent bus-byte oracle transcribed from MiSTer's pin-to-D mapping.
    const uint8_t keypad_codes[] = {0x0a, 0x0d, 0x07, 0x0c, 0x02, 0x03,
                                    0x0e, 0x05, 0x01, 0x0b, 0x09, 0x06};
    const auto expected = [&](bool joystick, unsigned player) {
        uint8_t nibble = 15;
        if (joystick) nibble = (matrix >> (5 * player)) & 15;
        else for (unsigned key = 0; key < 12; ++key)
            if (!(matrix & (1ull << (12 + 12 * player + key)))) {
                nibble = keypad_codes[key];
                break;
            }
        const unsigned fire = joystick ? 5 * player + 4 : 10 + player;
        return uint8_t(0x30 | (((matrix >> fire) & 1) << 6) | nibble);
    };
    std::vector<uint8_t> program{0xf3}; // DI; no BIOS, stack or initialized RAM
    std::vector<uint8_t> results;
    const auto out = [&](uint8_t port, uint8_t value) {
        program.insert(program.end(), {0x3e, value, 0xd3, port});
    };
    const auto in = [&](uint8_t port, bool joystick) {
        const unsigned offset = results.size();
        program.insert(program.end(), {0xdb, port, 0x32, uint8_t(offset),
                                       uint8_t(0x60 + (offset >> 8))});
        results.push_back(expected(joystick, (port >> 1) & 1));
    };
    in(0xfc, false); in(0xff, false); // RESET selects keypad, even with held directions
    for (unsigned mode = 0; mode < 2; ++mode) {
        const bool joystick = mode == 0;
        const unsigned base = joystick ? 0xc0 : 0x80;
        for (unsigned alias = 0; alias < 32; ++alias) {
            for (uint8_t value : {0x00, 0xff}) {
                out(joystick ? 0x80 : 0xc0, 0); // each alias must change mode
                out(base + alias, value); // value never selects mode
                in(0xe0 + alias, joystick); // all aliases, A1 selects player
                in(0xfc, joystick); in(0xff, joystick); // shared mode affects both players
            }
        }
        for (uint8_t port : {0x00, 0x7f, 0xa0, 0xbf, 0xe0, 0xff}) {
            out(port, 0xff); // unrelated writes cannot switch the controller mode
            in(0xfc, joystick); in(0xff, joystick);
        }
    }
    out(0xc0, 0); // leave joystick selected so the second reset must clear it
    program.push_back(0x76);
    Vcoleco_machine dut;
    dut.clk_sys = 0; dut.reset = 1; set_controller_state(dut, matrix);
    dut.media_ready = 1; dut.media_size = program.size();
    dut.media_data = 0; dut.peek_addr = 0;
    dut.eval();
    uint8_t registered_media_data = 0;
    for (unsigned reset = 0; reset < 2; ++reset) {
        // Second pass changes no media: reset must re-copy and select keypad.
        dut.reset = 1;
        for (unsigned i = 0; i < program.size() + 32; ++i)
            tick(dut, program, registered_media_data);
        dut.reset = 0;
        unsigned cycles = 0;
        for (; cycles < 1000000 && dut.cpu_halt_n; ++cycles)
            tick(dut, program, registered_media_data);
        require(cycles < 1000000, "controller probe did not HALT");
        for (unsigned i = 0; i < results.size(); ++i) {
            const auto actual = peek(dut, 0x6000 + i, registered_media_data);
            if (actual != results[i]) {
                std::cerr << "controller matrix=" << std::hex << matrix << std::dec
                          << " reset=" << reset << " sample=" << i
                          << " actual=" << unsigned(actual)
                          << " expected=" << unsigned(results[i]) << '\n';
                fail("CPU controller-mode/read alias result");
            }
        }
    }
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    controller_reads(0xffffffffffull);
    for (unsigned bit = 0; bit < 40; ++bit)
        controller_reads(0xffffffffffull ^ (1ull << bit));
    // Independent simultaneous players, and keypad priority (0 before 1/*/#).
    controller_reads(0xffffffffffull ^ 0x005a000aaaull);
    controller_reads(0);
    std::vector<uint8_t> cartridge(16384, 0x00);
    // LD A,5A; LD (6000),A; OUT(C0),A selects joystick; JR -2.
    cartridge[0] = 0x3e;
    cartridge[1] = 0x5a;
    cartridge[2] = 0x32;
    cartridge[3] = 0x00;
    cartridge[4] = 0x60;
    cartridge[5] = 0xd3;
    cartridge[6] = 0xc0;
    cartridge[7] = 0x18;
    cartridge[8] = 0xfe;

    Vcoleco_machine dut;
    dut.clk_sys = 0;
    dut.reset = 1;
    set_controller_state(dut, 0xffffffffffull & ~0x01ull);
    dut.media_ready = 1;
    dut.media_size = uint16_t(cartridge.size());
    dut.media_data = 0;
    dut.peek_addr = 0x8000;
    dut.eval();
    uint8_t registered_media_data = 0;
    for (unsigned i = 0; i != cartridge.size() + 32; ++i)
        tick(dut, cartridge, registered_media_data);

    require(peek(dut, 0x8000, registered_media_data) == cartridge[0],
            "cartridge base byte");
    require(peek(dut, 0xc000, registered_media_data) == cartridge[0],
            "cartridge mirror byte");
    require(peek(dut, 0xffff, registered_media_data) == cartridge.back(),
            "cartridge final byte");
    require(peek(dut, 0x1fff, registered_media_data) == 0,
            "reset shim image was not initialized");
    dut.reset = 0;
    for (unsigned i = 0; i != 250000; ++i)
        tick(dut, cartridge, registered_media_data);

    require(peek(dut, 0x6000, registered_media_data) == 0x5a,
            "CPU did not write RAM");
    require(peek(dut, 0x6400, registered_media_data) == 0x5a,
            "RAM mirror did not retain write");
    require((uint8_t(dut.controller1_value) & 0x01) == 0,
            "controller 1 active-low input was not visible");

    // MEDIA_BEGIN drops media_ready. The machine must discard the prior
    // loaded flag and accept a subsequent committed blob as a fresh image.
    std::vector<uint8_t> replacement = {0x99, 0x88, 0x77};
    dut.reset = 1;
    dut.media_ready = 0;
    dut.media_size = uint16_t(replacement.size());
    for (unsigned i = 0; i != 4; ++i)
        tick(dut, replacement, registered_media_data);
    dut.media_ready = 1;
    for (unsigned i = 0; i != replacement.size() + 8; ++i)
        tick(dut, replacement, registered_media_data);
    require(peek(dut, 0x8000, registered_media_data) == replacement[0],
            "second cartridge load did not replace the base byte");
    require(peek(dut, 0x8002, registered_media_data) == replacement[2],
            "second cartridge load did not copy the final byte");

    std::cout << "FES Coleco machine map/CPU/controller path passed\n";
    return EXIT_SUCCESS;
}
