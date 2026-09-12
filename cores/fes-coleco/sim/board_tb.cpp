// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vtop.h"
#include "Vtop___024root.h"
#include "verilated.h"

#include <cstdint>
#include <array>
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
    bool interactive = false;
    bool controllers = false;
    bool vdp_io = false;
    bool sprites = false;
    bool joystick = false;
    bool exchanging = false;
    uint64_t matrix = 0xffffffffffULL;
    unsigned bank_polls[4] = {};
    std::array<uint8_t, 16384> observed_vram{};
    unsigned bank_writes[4] = {};
    bool consumed_write = false;
    bool consumed_read = false;
    unsigned quiet_cycles = 0;
    unsigned polls[2] = {0, 0};
    unsigned writes = 0;
    std::vector<bool> initialized = std::vector<bool>(16384, false);

    Board() : root(*dut.rootp) {
        dut.FPGA_CLK1_50 = 0;
        root.top__DOT__system_clock__DOT__outclk_0 = 0;
        root.top__DOT__video_clock__DOT__outclk_0 = 0;
        root.top__DOT__hps_gp__DOT__gp_out = 0;
        dut.eval();
    }

    void sys_tick() {
        if (interactive && root.top__DOT__machine__DOT__cpu__DOT__RESET_n) {
            if (!root.top__DOT__machine__DOT__nIORQ && !root.top__DOT__machine__DOT__nWR) {
                const unsigned group = root.top__DOT__machine__DOT__cpu_addr & 0xe0;
                if (group == 0x80) joystick = false;
                if (group == 0xc0) joystick = true;
            }
            require(root.top__DOT__machine__DOT__nHALT,
                    "interactive diagnostic halted instead of polling controllers");
            ++quiet_cycles;
            if (root.top__DOT__machine__DOT__nIORQ || root.top__DOT__machine__DOT__nWR)
                consumed_write = false;
            // bus_write is the qualified CPU OUT strobe, even for controller
            // mode ports. Only BE/BF are VDP writes and break the quiet window.
            if (root.top__DOT__machine__DOT__vdp__DOT__bus_write &&
                (root.top__DOT__machine__DOT__vdp__DOT__data_port ||
                 root.top__DOT__machine__DOT__vdp__DOT__control_port)) {
                require(!consumed_write, "VDP consumed the same CPU OUT transaction twice");
                consumed_write = true;
                quiet_cycles = 0;
                polls[0] = polls[1] = 0;
                for (auto &count : bank_polls) count = 0;
                ++writes;
                if (root.top__DOT__machine__DOT__vdp__DOT__data_port) {
                    unsigned addr = root.top__DOT__machine__DOT__vdp__DOT__vram_addr;
                    initialized[addr] = true;
                    observed_vram[addr] = root.top__DOT__machine__DOT__cpu_dout;
                    for (unsigned bank = 0; bank < 4; ++bank)
                        if (addr / 32 >= 3 + 5 * bank && addr / 32 <= 4 + 5 * bank)
                            ++bank_writes[bank];
                }
            }
            if (root.top__DOT__machine__DOT__nIORQ || root.top__DOT__machine__DOT__nRD)
                consumed_read = false;
            else if (!consumed_read && root.top__DOT__machine__DOT__ce_cpu_n) {
                const unsigned port = root.top__DOT__machine__DOT__cpu_addr & 255;
                if (port >= 0xe0) {
                    unsigned player = (port >> 1) & 1;
                    ++polls[player];
                    unsigned bank = player * 2 + !joystick;
                    ++bank_polls[bank];
                    if (controllers && !exchanging)
                        require(root.top__DOT__machine__DOT__cpu_din == expected_banks()[bank],
                                "CPU controller mode/read mismatch bank " + std::to_string(bank));
                }
                consumed_read = true;
            }
        }
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
        exchanging = true;
        set_gpo(command(toggle, opcode, index, argument));
        sys_tick();
        toggle = !toggle;
        set_gpo(command(toggle, opcode, index, argument));
        for (unsigned i = 0; i != 8; ++i) {
            if (((gpi() >> 23) & 1u) == unsigned(toggle)) {
                require(gpi() == expected, name + ": unexpected mailbox response");
                exchanging = false;
                return;
            }
            sys_tick();
        }
        fail(name + ": mailbox ACK timeout");
    }

    void hold(bool &toggle) {
        exchange(toggle, 2, 0, 0, response(!toggle, false, 0), "hold reset");
        if (interactive) {
            matrix = 0xffffffffffULL;
            joystick = false;
            require(root.top__DOT__machine__DOT__controller1_value == 0x7f &&
                        root.top__DOT__machine__DOT__controller2_value == 0x7f,
                    "HOLD did not reset both controller rows to neutral");
            initialized.assign(16384, false);
            consumed_read = consumed_write = false;
        }
    }

    void upload(bool &toggle, const std::vector<uint8_t> &cartridge) {
        hold(toggle);
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
        release_and_check_copy(toggle, cartridge);
    }

    void reset_only(bool &toggle, const std::vector<uint8_t> &cartridge) {
        require(matrix != 0xffffffffffULL, "reset-only test must start with keys held");
        require(root.top__DOT__media_ready, "reset-only test needs committed media");
        hold(toggle);
        require(root.top__DOT__media_ready, "HOLD discarded committed media");
        // No BEGIN/DATA/COMMIT: reset's rising edge must re-arm the copy.
        release_and_check_copy(toggle, cartridge);
        require(root.top__DOT__media_ready, "reset-only release lost committed media");
    }

    void release_and_check_copy(bool &toggle, const std::vector<uint8_t> &cartridge) {
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
        if (vdp_io) {
            require(root.top__DOT__machine__DOT__cpu_ram_block__DOT__ram[0] == 0xa5 &&
                    root.top__DOT__machine__DOT__cpu_ram_block__DOT__ram[1] == 2 &&
                    root.top__DOT__machine__DOT__cpu_ram_block__DOT__ram[4] == 0,
                    "VDP I/O diagnostic halted without CPU pass/NMI results");
            for (unsigned addr = 0; addr < 768; ++addr)
                require(written[addr], "CPU did not initialize pass name table");
            for (unsigned addr = 0x800; addr < 0x810; ++addr)
                require(written[addr], "CPU did not initialize pass patterns");
            require(written[0x2000] && written[0x2001], "CPU did not initialize pass colors");
        } else if (sprites) {
            const unsigned status = root.top__DOT__machine__DOT__cpu_ram_block__DOT__ram[2];
            require(root.top__DOT__machine__DOT__cpu_ram_block__DOT__ram[0] == 0xa5,
                    "sprite diagnostic halted without CPU pass result");
            require((status & 0x60) == 0x60 && (status & 0x1f) == 4,
                    "sprite diagnostic did not observe collision/fifth index 4");
            for (unsigned addr = 0x3c00; addr < 0x3c00 + 768; ++addr)
                require(written[addr], "sprite diagnostic did not initialize name table");
            require(written[0x0800] && written[0x0801],
                    "sprite diagnostic did not initialize sprite patterns");
            for (unsigned addr = 0x1000; addr < 0x1010; ++addr)
                require(written[addr], "sprite diagnostic did not initialize background patterns");
            require(written[0x2000] && written[0x2001],
                    "sprite diagnostic did not initialize background colors");
            for (unsigned addr = 0x1b00; addr < 0x1b18; ++addr)
                require(written[addr], "sprite diagnostic did not initialize SAT entries");
        } else {
            for (bool value : written)
                require(value, "diagnostic did not initialize all 16 KiB VRAM through CPU I/O");
        }
        // Let two complete logical rasters replace every framebuffer location.
        // Pixel and system clocks are independent simulation boundaries.
        for (unsigned i = 0; i < 2 * 256 * 262 * 16; ++i) sys_tick();
    }

    void row(bool &toggle, unsigned index, uint8_t value) {
        matrix = (matrix & ~(uint64_t(31) << (5 * index))) | (uint64_t(value) << (5 * index));
        exchange(toggle, 3, index, value, response(!toggle, false, 0), "keyboard row");
    }

    std::array<uint8_t, 4> expected_banks() const {
        const uint8_t keys[] = {0xa, 0xd, 7, 0xc, 2, 3, 0xe, 5, 1, 0xb, 9, 6};
        std::array<uint8_t, 4> result{};
        for (unsigned p = 0; p < 2; ++p) {
            unsigned nibble = 15;
            for (unsigned k = 0; k < 12; ++k)
                if (!(matrix & (1ULL << (12 + 12 * p + k)))) { nibble = keys[k]; break; }
            result[2*p] = 0x30 | ((matrix >> (5*p)) & 15) | (((matrix >> (4+5*p)) & 1) << 6);
            result[2*p+1] = 0x30 | nibble | (((matrix >> (10+p)) & 1) << 6);
        }
        return result;
    }

    void check_names(const std::string &name) {
        const auto banks = expected_banks();
        for (unsigned row = 0; row < 24; ++row)
            for (unsigned col = 0; col < 32; ++col) {
                unsigned expected = row == 0 || row == 23 || col == 0 || col == 31 ? 0 : 3;
                for (unsigned bank = 0; bank < 4; ++bank)
                    for (unsigned bit = 0; bit < 8; ++bit)
                        if (row >= 3+5*bank && row <= 4+5*bank && col >= 4+3*bit && col <= 5+3*bit)
                            expected = banks[bank] & (1 << bit) ? 2 : 1;
                require(observed_vram[row*32+col] == expected, name + ": CPU VRAM panel mismatch");
            }
    }

    void settle(const std::string &name, bool startup = false, bool raster = true) {
        quiet_cycles = 0;
        polls[0] = polls[1] = 0;
        for (auto &count : bank_polls) count = 0;
        unsigned cycles = 0;
        // Require repeated reads of BOTH controllers after the last VDP write,
        // plus a quiet window. This also rejects unconditional panel repainting.
        for (; cycles < 20000000; ++cycles) {
            sys_tick();
            bool all_banks = true;
            if (controllers) for (auto count : bank_polls) all_banks &= count >= 8;
            if (quiet_cycles >= 100000 && polls[0] >= 8 && polls[1] >= 8 && all_banks) break;
        }
        require(cycles < 20000000, name + ": controller polling/VDP completion timeout; quiet=" +
                std::to_string(quiet_cycles) + " writes=" + std::to_string(writes) +
                " bank polls=" + std::to_string(bank_polls[0]) + "," + std::to_string(bank_polls[1]) +
                "," + std::to_string(bank_polls[2]) + "," + std::to_string(bank_polls[3]) +
                " cache=" + std::to_string(root.top__DOT__machine__DOT__cpu_ram_block__DOT__ram[0]) +
                "," + std::to_string(root.top__DOT__machine__DOT__cpu_ram_block__DOT__ram[1]) +
                "," + std::to_string(root.top__DOT__machine__DOT__cpu_ram_block__DOT__ram[2]) +
                "," + std::to_string(root.top__DOT__machine__DOT__cpu_ram_block__DOT__ram[3]));
        if (startup)
            for (bool value : initialized)
                require(value, name + ": CPU did not initialize all 16 KiB VRAM");
        if (controllers) check_names(name);
        if (!raster) return;
        const unsigned before = writes;
        for (unsigned i = 0; i < 2 * 256 * 262 * 16; ++i) sys_tick();
        require(writes == before, name + ": unchanged input repainted VDP");
    }
};

uint32_t expected_rgb(unsigned x, unsigned y, bool vdp_io = false) {
    if (x < 384 || x >= 896 || y < 168 || y >= 552) return 0;
    // The current shell has a registered framebuffer read: one HDMI pixel
    // of data latency, clipped by the undelayed image-active window.
    const unsigned logical_x = x == 384 ? 0 : (x - 385) / 2;
    const unsigned logical_y = (y - 168) / 2;
    const unsigned col = logical_x / 8, row = logical_y / 8;
    if (col == 0 || col == 31 || row == 0 || row == 23) return 0x00ff40;
    if (vdp_io) return 0;
    if (logical_x % 8 == 0 || logical_x % 8 == 7 ||
        logical_y % 8 == 0 || logical_y % 8 == 7) return 0;
    return (col + row) % 2 == 0 ? 0x00ff40 : 0xff4000;
}

uint32_t sprite_rgb(unsigned x, unsigned y) {
    if (x < 384 || x >= 896 || y < 168 || y >= 552) return 0;
    const unsigned logical_x = x == 384 ? 0 : (x - 385) / 2;
    const unsigned logical_y = (y - 168) / 2;
    if ((logical_x == 188 || logical_x == 189) &&
        (logical_y == 81 || logical_y == 82))
        return 0xff4000;
    if (logical_x == 255 && (logical_y == 101 || logical_y == 102))
        return 0xff4000;
    if ((logical_x == 10 || logical_x == 11) &&
        (logical_y == 131 || logical_y == 132))
        return 0x00ff40;
    const unsigned col = logical_x / 8, row = logical_y / 8;
    if (col == 0 || col == 31 || row == 0 || row == 23) return 0x00ff40;
    return 0;
}

uint32_t interactive_rgb(unsigned x, unsigned y, uint8_t p0, uint8_t p1) {
    if (x < 384 || x >= 896 || y < 168 || y >= 552) return 0;
    const unsigned col = (x == 384 ? 0 : (x - 385) / 2) / 8;
    const unsigned row = ((y - 168) / 2) / 8;
    if (col == 0 || col == 31 || row == 0 || row == 23) return 0x00ff40;
    // Independent geometry oracle: no cartridge bytes, VRAM or preview used.
    for (unsigned bit = 0; bit < 5; ++bit) {
        if (col < 2 + 6 * bit || col > 5 + 6 * bit) continue;
        if (row >= 5 && row <= 8) return p0 & (1u << bit) ? 0xff4000 : 0x00ff40;
        if (row >= 15 && row <= 18) return p1 & (1u << bit) ? 0xff4000 : 0x00ff40;
    }
    return 0;
}

uint32_t controllers_rgb(unsigned x, unsigned y, const std::array<uint8_t, 4> &banks) {
    if (x < 384 || x >= 896 || y < 168 || y >= 552) return 0;
    const unsigned col = (x == 384 ? 0 : (x - 385) / 2) / 8;
    const unsigned row = (y - 168) / 16;
    if (col == 0 || col == 31 || row == 0 || row == 23) return 0x00ff40;
    for (unsigned bank = 0; bank < 4; ++bank)
        for (unsigned bit = 0; bit < 8; ++bit)
            if (row >= 3+5*bank && row <= 4+5*bank && col >= 4+3*bit && col <= 5+3*bit)
                return banks[bank] & (1 << bit) ? 0xff4000 : 0x00ff40;
    return 0;
}

void check_frame(Board &board, uint8_t p0 = 31, uint8_t p1 = 31) {
    const auto banks = board.expected_banks();
    // Synchronize by observing counters; never write the raster or framebuffer.
    unsigned sync = 0;
    while (board.root.top__DOT__video__DOT__horizontal != 0 ||
           board.root.top__DOT__video__DOT__vertical != 0) {
        require(++sync <= 1650 * 750, "HDMI frame synchronization timeout");
        board.pixel_tick();
    }
    unsigned lit = 0;
    unsigned active = 0;
    for (unsigned y = 0; y < 750; ++y) {
        for (unsigned x = 0; x < 1650; ++x) {
            require(bool(board.dut.HDMI_TX_DE) == (x < 1280 && y < 720), "HDMI DE");
            require(bool(board.dut.HDMI_TX_HS) == (x >= 1390 && x < 1430), "HDMI HS");
            require(bool(board.dut.HDMI_TX_VS) == (y >= 725 && y < 730), "HDMI VS");
            const uint32_t expected = board.controllers ? controllers_rgb(x, y, banks) :
                board.interactive ? interactive_rgb(x, y, p0, p1) :
                board.sprites ? sprite_rgb(x, y) : expected_rgb(x, y, board.vdp_io);
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
    require(lit == (board.vdp_io ? 27648u : board.sprites ? 27680u :
                   board.controllers ? 60416u : board.interactive ? 68608u : 122688u),
            "nonblack pixel count");
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    const bool controllers = argc > 1 && std::string(argv[1]) == "--controllers";
    const bool vdp_io = argc > 1 && std::string(argv[1]) == "--vdp-io";
    const bool sprites = argc > 1 && std::string(argv[1]) == "--sprites";
    const bool interactive = controllers || (argc > 1 && std::string(argv[1]) == "--interactive");
    require(argc >= (interactive || vdp_io || sprites ? 3 : 2),
            "usage: Vtop [--interactive|--controllers|--vdp-io|--sprites] cartridge.rom [more cartridges...]");
    std::vector<std::vector<uint8_t>> cartridges;
    const int first_cartridge = interactive || vdp_io || sprites ? 2 : 1;
    for (int i = first_cartridge; i < argc; ++i) {
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
    board.interactive = interactive;
    board.controllers = controllers;
    board.vdp_io = vdp_io;
    board.sprites = sprites;

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
    unsigned load = 0;
    for (const auto &cartridge : cartridges) {
        board.upload(toggle, cartridge);
        if (controllers) {
            const uint64_t neutral = 0xffffffffffULL;
            const auto check = [&](const std::string &name, bool startup, bool frame) {
                board.settle(name, startup, frame);
                if (frame) check_frame(board);
                std::cout << "controllers " << cartridge.size() << " bytes: " << name << std::endl;
            };
            const auto change = [&](uint64_t matrix, const std::string &name, bool frame = false) {
                const auto previous = board.expected_banks();
                const std::array<unsigned, 4> before = {board.bank_writes[0], board.bank_writes[1],
                                                       board.bank_writes[2], board.bank_writes[3]};
                for (unsigned row = 0; row < 8; ++row)
                    board.row(toggle, row, (matrix >> (5*row)) & 31);
                check(name, false, frame);
                const auto after = board.expected_banks();
                for (unsigned bank = 0; bank < 4; ++bank)
                    if (previous[bank] == after[bank])
                        require(board.bank_writes[bank] == before[bank], name + ": unchanged bank repainted");
            };
            check("neutral after HOLD", true, true);
            if (load++ == 0) {
                for (unsigned bit = 0; bit < 40; ++bit) {
                    change(neutral ^ (1ULL << bit), "matrix bit " + std::to_string(bit));
                    change(neutral, "release bit " + std::to_string(bit));
                }
                for (unsigned p = 0; p < 2; ++p)
                    for (unsigned key = 0; key < 11; ++key) {
                        change(neutral ^ (3ULL << (12 + 12*p + key)), "key priority");
                        change(neutral, "priority release");
                    }
                change(0, "all keys and both fires", true);
                change(neutral, "all released", true);
            }
            const uint64_t mixed = neutral ^ (1ULL << 0) ^ (1ULL << 4) ^ (1ULL << 11) ^
                                   (1ULL << 13) ^ (1ULL << 21) ^ (1ULL << 35);
            change(mixed, "mixed players/fire/key priority", true);
            board.reset_only(toggle, cartridge);
            check("reset-only neutral", true, false);
            change(mixed, "reset-only held restore");
            board.upload(toggle, cartridge);
            check("held reload neutral", true, false);
            change(mixed, "held reload restore", true);
            continue;
        }
        if (interactive) {
            const auto check = [&](const std::string &name, uint8_t p0, uint8_t p1, bool startup = false) {
                std::cout << "interactive " << cartridge.size() << " bytes: " << name << std::endl;
                board.settle(name, startup);
                const unsigned before = board.writes;
                check_frame(board, p0, p1);
                require(board.writes == before, name + ": held frame caused VDP writes");
            };
            check("idle after HOLD row reset", 31, 31, true);
            for (unsigned player = 0; player < 2; ++player) {
                for (unsigned bit = 0; bit < 5; ++bit) {
                    const uint8_t pressed = 31 ^ (1u << bit);
                    const std::string name = "player " + std::to_string(player) + " bit " + std::to_string(bit);
                    board.row(toggle, player, pressed);
                    check(name + " press", player == 0 ? pressed : 31, player == 1 ? pressed : 31);
                    board.row(toggle, player, 31);
                    check(name + " release", 31, 31);
                }
            }
            board.row(toggle, 0, 0x0a);
            board.row(toggle, 1, 0x15);
            check("simultaneous mixed bits", 0x0a, 0x15);
            const unsigned before = board.writes;
            board.row(toggle, 7, 0);
            check("unrelated row ignored", 0x0a, 0x15);
            require(board.writes == before, "unrelated keyboard row repainted panels");
            // Reload while held; HOLD clears rows. Runtime restores explicitly.
            board.upload(toggle, cartridge);
            check("held reload neutral", 31, 31, true);
            board.row(toggle, 0, 0x0a);
            board.row(toggle, 1, 0x15);
            check("explicit held input restore", 0x0a, 0x15);
            board.reset_only(toggle, cartridge);
            check("reset-only held keys become neutral", 31, 31, true);
            board.row(toggle, 0, 0x0a);
            board.row(toggle, 1, 0x15);
            check("reset-only explicit held input restore", 0x0a, 0x15);
            continue;
        }
        board.run_cartridge();
        check_frame(board);
    }
    require(board.dut.HDMI_TX_CLK == 0, "pixel clock boundary did not settle");
    if (vdp_io) {
        std::cout << "FES Coleco VDP I/O board CPU/NMI/720p pass picture passed: "
                  << cartridges.size() << " GP loads, 27648 green active pixels per frame\n";
        return EXIT_SUCCESS;
    }
    if (sprites) {
        std::cout << "FES Coleco sprite board passed: " << cartridges.size()
                  << " CPU status samples and exact 720p frames\n";
        return EXIT_SUCCESS;
    }
    if (controllers) {
        std::cout << "FES Coleco controllers board passed: " << cartridges.size()
                  << " cartridges; all 40 matrix bits, keypad priority, changed-bank writes, reset/reload, exact frames\n";
        return EXIT_SUCCESS;
    }
    if (interactive) {
        std::cout << "FES Coleco interactive board passed: " << cartridges.size()
                  << " cartridges, each with held reload, reset-only and 27 exact full frames\n";
        return EXIT_SUCCESS;
    }
    std::cout << "FES Coleco board I2C/GP cartridge/CPU Graphics I/720p passed: "
              << cartridges.size() << " loads, 921600 active pixels and 122688 nonblack per frame\n";
    return EXIT_SUCCESS;
}
