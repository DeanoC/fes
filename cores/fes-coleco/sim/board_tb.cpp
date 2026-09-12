// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vtop.h"
#include "Vtop___024root.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <iterator>
#include <string>
#include <vector>

namespace {

[[noreturn]] void fail(const std::string &message) {
    std::cerr << "FES Coleco board: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string &message) {
    if (!condition) fail(message);
}

uint32_t command(bool toggle, uint8_t opcode, uint8_t index, uint16_t argument) {
    return (toggle ? 0x80000000u : 0u) | (uint32_t(opcode) << 24) |
           (uint32_t(index) << 16) | argument;
}

uint32_t response(bool toggle, bool error, uint16_t data) {
    return 0xf5000000u | (toggle ? 0x00800000u : 0u) |
           (error ? 0x00400000u : 0u) | data;
}

struct Board {
    Vtop dut;
    Vtop___024root &root;

    Board() : root(*dut.rootp) {
        dut.FPGA_CLK1_50 = 0;
        root.top__DOT__system_clock__DOT__outclk_0 = 0;
        root.top__DOT__video_clock__DOT__outclk_0 = 0;
        root.top__DOT__hps_gp__DOT__gp_out = 0;
        dut.eval();
    }

    void sys_tick() {
        root.top__DOT__system_clock__DOT__outclk_0 = 1;
        dut.eval();
        root.top__DOT__system_clock__DOT__outclk_0 = 0;
        dut.eval();
    }

    void pixel_tick() {
        root.top__DOT__video_clock__DOT__outclk_0 = 1;
        dut.eval();
        root.top__DOT__video_clock__DOT__outclk_0 = 0;
        dut.eval();
    }

    void set_gpo(uint32_t value) {
        root.top__DOT__hps_gp__DOT__gp_out = value;
        dut.eval();
    }

    uint32_t gpi() const { return root.top__DOT__hps_gp__DOT__observed_gpi; }

    void exchange(bool &toggle, uint8_t opcode, uint8_t index, uint16_t argument,
                  uint32_t expected, const std::string &name) {
        set_gpo(command(toggle, opcode, index, argument));
        sys_tick();
        toggle = !toggle;
        set_gpo(command(toggle, opcode, index, argument));
        for (unsigned i = 0; i != 8; ++i) {
            if (((gpi() >> 23) & 1u) == unsigned(toggle)) {
                require(gpi() == expected, name + ": unexpected mailbox response");
                return;
            }
            sys_tick();
        }
        fail(name + ": mailbox ACK timeout");
    }

    void upload(bool &toggle, const std::vector<uint8_t> &cartridge) {
        exchange(toggle, 2, 0, 0, response(!toggle, false, 0), "hold reset");
        exchange(toggle, 4, 0, uint16_t(cartridge.size()),
                 response(!toggle, false, 0), "media begin");
        size_t offset = 0;
        for (; offset + 1 < cartridge.size(); offset += 2) {
            exchange(toggle, 5, 0, uint16_t(cartridge[offset]) |
                     (uint16_t(cartridge[offset + 1]) << 8),
                     response(!toggle, false, 0), "media pair");
        }
        if (offset < cartridge.size())
            exchange(toggle, 5, 1, cartridge[offset],
                     response(!toggle, false, 0), "media tail");
        exchange(toggle, 6, 0, 0, response(!toggle, false, 0), "media commit");
        require(root.top__DOT__machine__DOT__nHALT, "CPU not reset before reload");
        // Commit only publishes media_ready. Release immediately, exactly as
        // the runtime does: the FPGA must hold its CPU/VDP through the copy.
        exchange(toggle, 2, 0, 1, response(!toggle, false, 0), "execution release");
        require(!root.top__DOT__machine__DOT__media_loaded,
                "test did not exercise execution release during cartridge copy");
        for (size_t i = 0; i < cartridge.size() + 32 &&
                           !root.top__DOT__machine__DOT__media_loaded; ++i) {
            require(!root.top__DOT__machine__DOT__cpu__DOT__RESET_n &&
                        root.top__DOT__machine__DOT__vdp__DOT__reset,
                    "CPU/VDP escaped reset before cartridge copy completed");
            sys_tick();
        }
        require(root.top__DOT__machine__DOT__media_loaded, "cartridge copy incomplete");
        for (size_t i = 0; i < cartridge.size(); ++i)
            require(root.top__DOT__machine__DOT__cartridge_ram__DOT__ram[i] == cartridge[i],
                    "cartridge RAM differs from exact uploaded bytes at " + std::to_string(i));
    }

    void run_cartridge() {
        // HALT is the cartridge's completion marker, not a forced CPU/VDP
        // state. A missing clear, bad loop or bad upload must fail this test.
        std::vector<bool> written(16384, false);
        bool consumed_write = false;
        unsigned cycles = 0;
        for (; cycles < 20000000 && root.top__DOT__machine__DOT__nHALT; ++cycles) {
            if (root.top__DOT__machine__DOT__nIORQ || root.top__DOT__machine__DOT__nWR)
                consumed_write = false;
            if (root.top__DOT__machine__DOT__vdp__DOT__bus_write) {
                require(!consumed_write, "VDP consumed the same CPU OUT transaction twice");
                consumed_write = true;
            }
            if (root.top__DOT__machine__DOT__vdp__DOT__bus_write &&
                root.top__DOT__machine__DOT__vdp__DOT__data_port)
                written[root.top__DOT__machine__DOT__vdp__DOT__vram_addr] = true;
            sys_tick();
        }
        require(cycles < 20000000, "diagnostic CPU did not reach HALT");
        for (bool value : written)
            require(value, "diagnostic did not initialize all 16 KiB VRAM through CPU I/O");
        // Let two complete logical rasters replace every framebuffer location.
        // Pixel and system clocks are independent simulation boundaries.
        for (unsigned i = 0; i < 2 * 256 * 262 * 16; ++i) sys_tick();
    }
};

uint32_t expected_rgb(unsigned x, unsigned y) {
    if (x < 384 || x >= 896 || y < 168 || y >= 552) return 0;
    // The current shell has a registered framebuffer read: one HDMI pixel
    // of data latency, clipped by the undelayed image-active window.
    const unsigned logical_x = x == 384 ? 0 : (x - 385) / 2;
    const unsigned logical_y = (y - 168) / 2;
    const unsigned col = logical_x / 8, row = logical_y / 8;
    if (col == 0 || col == 31 || row == 0 || row == 23) return 0x00ff40;
    if (logical_x % 8 == 0 || logical_x % 8 == 7 ||
        logical_y % 8 == 0 || logical_y % 8 == 7) return 0;
    return (col + row) % 2 == 0 ? 0x00ff40 : 0xff4000;
}

void check_frame(Board &board) {
    // Synchronize by observing counters; never write the raster or framebuffer.
    while (board.root.top__DOT__video__DOT__horizontal != 0 ||
           board.root.top__DOT__video__DOT__vertical != 0)
        board.pixel_tick();
    unsigned lit = 0;
    unsigned active = 0;
    for (unsigned y = 0; y < 750; ++y) {
        for (unsigned x = 0; x < 1650; ++x) {
            require(bool(board.dut.HDMI_TX_DE) == (x < 1280 && y < 720), "HDMI DE");
            require(bool(board.dut.HDMI_TX_HS) == (x >= 1390 && x < 1430), "HDMI HS");
            require(bool(board.dut.HDMI_TX_VS) == (y >= 725 && y < 730), "HDMI VS");
            const uint32_t expected = expected_rgb(x, y);
            require(board.dut.HDMI_TX_D == expected,
                    "diagnostic RGB mismatch at " + std::to_string(x) + "," +
                    std::to_string(y) + ": got " + std::to_string(board.dut.HDMI_TX_D) +
                    " expected " + std::to_string(expected));
            if (board.dut.HDMI_TX_DE) ++active;
            if (expected) ++lit;
            board.pixel_tick();
            if (x % 3 == 0) board.sys_tick();
        }
    }
    require(active == 1280 * 720, "active pixel count");
    require(lit == 122688, "nonblack pixel count");
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    require(argc >= 2, "usage: Vtop path/to/graphics-i.rom [more generated cartridges...]");
    std::vector<std::vector<uint8_t>> cartridges;
    for (int i = 1; i < argc; ++i) {
        std::ifstream input(argv[i], std::ios::binary);
        require(input.good(), "cannot open generated cartridge");
        cartridges.emplace_back(std::istreambuf_iterator<char>(input),
                                std::istreambuf_iterator<char>());
        require(!input.bad() && !cartridges.back().empty() && cartridges.back().size() <= 16384,
                "cartridge must contain 1..16384 raw bytes");
    }
    // Exercise the intentionally uninitialized RAMs with nonzero power-up data.
    Verilated::randReset(2);
    Verilated::randSeed(0xc01ec0);
    Board board;

    for (unsigned hps_low = 0; hps_low != 4; ++hps_low) {
        for (unsigned external_low = 0; external_low != 4; ++external_low) {
            board.root.top__DOT__hdmi_i2c__DOT__out_clk = hps_low & 1;
            board.root.top__DOT__hdmi_i2c__DOT__out_data = (hps_low >> 1) & 1;
            board.root.top__DOT__hdmi_scl_pad__DOT__external_low = external_low & 1;
            board.root.top__DOT__hdmi_sda_pad__DOT__external_low = (external_low >> 1) & 1;
            board.dut.eval();
            require(!board.root.top__DOT__hdmi_scl_pad__DOT__drive_high &&
                        !board.root.top__DOT__hdmi_sda_pad__DOT__drive_high,
                    "I2C pad actively drives high");
            require(board.root.top__DOT__hdmi_scl_pad__DOT__drive_low == bool(hps_low & 1) &&
                        board.root.top__DOT__hdmi_sda_pad__DOT__drive_low == bool(hps_low & 2),
                    "I2C low enable is inverted or crossed");
            require(board.root.top__DOT__hdmi_i2c__DOT__observed_scl ==
                        !(hps_low & 1 || external_low & 1) &&
                        board.root.top__DOT__hdmi_i2c__DOT__observed_sda ==
                        !(hps_low & 2 || external_low & 2),
                    "I2C pad feedback does not reflect wired-AND bus");
        }
    }

    bool toggle = false;
    require(board.gpi() == 0xf5000000u, "initial mailbox signature");
    board.exchange(toggle, 1, 4, 0, response(!toggle, false, 2), "identity tag");
    board.exchange(toggle, 3, 0, 0x0011, response(!toggle, false, 0), "keyboard row");
    board.exchange(toggle, 2, 0, 1, response(!toggle, false, 0), "release without media");
    for (unsigned i = 0; i < 32; ++i) {
        require(!board.root.top__DOT__machine__DOT__cpu__DOT__RESET_n &&
                    board.root.top__DOT__machine__DOT__vdp__DOT__reset,
                "CPU/VDP escaped reset without committed media");
        board.sys_tick();
    }
    for (const auto &cartridge : cartridges) {
        board.upload(toggle, cartridge);
        board.run_cartridge();
        check_frame(board);
    }
    require(board.dut.HDMI_TX_CLK == 0, "pixel clock boundary did not settle");
    std::cout << "FES Coleco board I2C/GP cartridge/CPU Graphics I/720p passed: "
              << cartridges.size() << " loads, 921600 active pixels and 122688 nonblack per frame\n";
    return EXIT_SUCCESS;
}
