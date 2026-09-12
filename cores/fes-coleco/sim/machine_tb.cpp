// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vcoleco_machine.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <vector>

namespace {

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

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    std::vector<uint8_t> cartridge(16384, 0x00);
    // LD A,5A; LD (6000),A; JR -2.
    cartridge[0] = 0x3e;
    cartridge[1] = 0x5a;
    cartridge[2] = 0x32;
    cartridge[3] = 0x00;
    cartridge[4] = 0x60;
    cartridge[5] = 0x18;
    cartridge[6] = 0xfe;

    Vcoleco_machine dut;
    dut.clk_sys = 0;
    dut.reset = 1;
    dut.keyboard = 0xffffffffffull & ~0x01ull;
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
