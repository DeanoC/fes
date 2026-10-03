// SPDX-License-Identifier: GPL-3.0-or-later
// Execute the original diagnostic on fx68k, the motherboard and the RTL probe.
#include "Vst_sim_top.h"
#include "verilated.h"

#include <array>
#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <iterator>
#include <map>
#include <string>
#include <vector>

namespace {

[[noreturn]] void fail(const std::string& message) {
    std::cerr << "fes.atari-st machine: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string& message) {
    if (!condition) fail(message);
}

std::vector<uint8_t> load_rom(const std::string& path) {
    std::ifstream input(path, std::ios::binary);
    require(input.good(), "cannot read diagnostic ROM " + path);
    std::vector<uint8_t> bytes((std::istreambuf_iterator<char>(input)), {});
    require(bytes.size() == 192 * 1024, "firmware must be exactly 192 KiB");
    return bytes;
}

struct Transfer {
    bool active = false;
    bool complete = false;
    unsigned remaining = 0;
    uint32_t address = 0;
    uint16_t data = 0;
    uint8_t lanes = 0;
    bool write = false;
};

struct Machine {
    Vst_sim_top dut;
    std::vector<uint8_t> rom;
    std::array<uint8_t, 512 * 1024> ram{};
    Transfer rom_transfer, ram_transfer, expansion;
    std::map<uint32_t, unsigned> ram_write_counts;
    std::map<uint32_t, unsigned> expansion_write_counts;
    uint64_t cycles = 0;
    uint64_t rom_wait_ticks = 0;
    uint64_t ram_wait_ticks = 0;
    uint64_t expansion_wait_ticks = 0;
    unsigned faults = 0;
    bool last_fault = false;
    bool highest_rom_word = false;
    bool highest_ram_word = false;
    bool irq_sent = false;
    bool saw_iack = false;
    unsigned delay = 0;

    explicit Machine(std::vector<uint8_t> firmware) : rom(std::move(firmware)) {
        dut.clk_sys = 0;
        dut.reset = 1;
        dut.rom_rdata = 0xFFFF;
        dut.rom_ready = 0;
        dut.ram_rdata = 0xFFFF;
        dut.ram_ready = 0;
        dut.probe_enable = 1;
        dut.probe_wait_cycles = 0;
        dut.exp_irq = 0;
        dut.video_hold = 0;
        dut.video_scanlines = 0;
        dut.video_data = 0;
        dut.eval();
    }

    uint16_t word(uint32_t address) const {
        require(address + 1 < ram.size(), "test RAM peek outside capacity");
        return (uint16_t(ram[address]) << 8) | ram[address + 1];
    }

    uint32_t longword(uint32_t address) const {
        return (uint32_t(word(address)) << 16) | word(address + 2);
    }

    static void stable(const Transfer& transfer, uint32_t address, uint16_t data,
                       uint8_t lanes, bool write, const char* name) {
        require(transfer.address == address && transfer.data == data &&
                    transfer.lanes == lanes && transfer.write == write,
                std::string(name) + " request changed before completion");
    }

    void memory() {
        if (dut.reset) {
            rom_transfer = {};
            ram_transfer = {};
            dut.rom_ready = 0;
            dut.ram_ready = 0;
            return;
        }

        if (!dut.rom_req) {
            require(!rom_transfer.active || rom_transfer.complete,
                    "ROM request abandoned before ready");
            rom_transfer = {};
            dut.rom_ready = 0;
        } else {
            const uint32_t address = uint32_t(dut.rom_addr) * 2;
            require(address + 1 < rom.size(), "ROM request escaped exact 192 KiB socket");
            if (!rom_transfer.active) {
                rom_transfer.active = true;
                rom_transfer.address = address;
                rom_transfer.remaining = delay ? delay + ((address >> 1) % 5) : 0;
            } else {
                require(rom_transfer.address == address, "ROM address changed during wait");
            }
            dut.rom_rdata = (uint16_t(rom[address]) << 8) | rom[address + 1];
            if (rom_transfer.remaining) {
                --rom_transfer.remaining;
                ++rom_wait_ticks;
                dut.rom_ready = 0;
            } else {
                rom_transfer.complete = true;
                dut.rom_ready = 1;
                highest_rom_word |= address == rom.size() - 2;
            }
        }

        if (!dut.ram_req) {
            require(!ram_transfer.active || ram_transfer.complete,
                    "RAM request abandoned before ready");
            ram_transfer = {};
            dut.ram_ready = 0;
        } else {
            const uint32_t address = uint32_t(dut.ram_addr) * 2;
            require(address >= 8 && address + 1 < ram.size(),
                    "protected vector alias or invalid address reached RAM socket");
            const uint16_t data = dut.ram_wdata;
            const uint8_t lanes = dut.ram_byte_enable;
            const bool writing = dut.ram_write;
            require(lanes != 0, "RAM transaction has neither byte lane");
            if (!ram_transfer.active) {
                ram_transfer.active = true;
                ram_transfer.address = address;
                ram_transfer.data = data;
                ram_transfer.lanes = lanes;
                ram_transfer.write = writing;
                ram_transfer.remaining = delay ? delay + ((address >> 1) % 7) : 0;
            } else {
                stable(ram_transfer, address, data, lanes, writing, "RAM");
            }
            if (ram_transfer.remaining) {
                --ram_transfer.remaining;
                ++ram_wait_ticks;
                dut.ram_ready = 0;
            } else {
                if (!ram_transfer.complete && writing) {
                    if (lanes & 2) ram[address] = data >> 8;
                    if (lanes & 1) ram[address + 1] = data;
                    ++ram_write_counts[address];
                }
                ram_transfer.complete = true;
                dut.ram_ready = 1;
                highest_ram_word |= address == ram.size() - 2;
            }
            dut.ram_rdata = word(address);
        }
    }

    void observe_expansion() {
        if (dut.reset || !dut.exp_req) {
            expansion = {};
            return;
        }
        const uint32_t address = uint32_t(dut.exp_addr) * 2;
        const uint16_t data = dut.exp_wdata;
        const uint8_t lanes = dut.exp_byte_enable;
        const bool writing = dut.exp_write;
        require(dut.exp_fc == 5, "non-supervisor-data request reached expansion probe");
        require(!(writing && address >= 0xFA0000 && address < 0xFC0000),
                "read-only cartridge write reached expansion socket");
        if (!expansion.active) {
            expansion.active = true;
            expansion.address = address;
            expansion.data = data;
            expansion.lanes = lanes;
            expansion.write = writing;
        } else {
            stable(expansion, address, data, lanes, writing, "expansion");
        }
        if (!dut.exp_ack && !dut.exp_berr) ++expansion_wait_ticks;
        if (!expansion.complete && (dut.exp_ack || dut.exp_berr)) {
            expansion.complete = true;
            if (writing && dut.exp_ack) ++expansion_write_counts[address];
        }
    }

    void tick() {
        memory();
        dut.video_data = word(uint32_t(dut.video_addr) * 2);
        dut.eval();
        observe_expansion();
        if (!dut.reset && word(0x402) == 13 && !irq_sent) {
            dut.exp_irq = 3;
            irq_sent = true;
        }
        if (word(0x408) != 0) dut.exp_irq = 0;
        dut.clk_sys = 1;
        dut.eval();
        dut.clk_sys = 0;
        dut.eval();
        ++cycles;
        if (dut.debug_bus_error && !last_fault) ++faults;
        last_fault = dut.debug_bus_error;
        // Level-3 autovector acknowledge presents the encoded level on A3..A1.
        saw_iack |= irq_sent && dut.debug_addr == 0xFFFFF6;
    }

    void reset(bool enabled, unsigned memory_delay, unsigned expansion_delay) {
        dut.reset = 1;
        dut.probe_enable = enabled;
        dut.probe_wait_cycles = expansion_delay;
        dut.exp_irq = 0;
        delay = memory_delay;
        ram_write_counts.clear();
        expansion_write_counts.clear();
        rom_wait_ticks = ram_wait_ticks = expansion_wait_ticks = 0;
        faults = 0;
        last_fault = false;
        highest_rom_word = highest_ram_word = irq_sent = saw_iack = false;
        for (unsigned i = 0; i < 128; ++i) tick();
        dut.reset = 0;
        // A previous image's pass marker must not terminate the new boot early.
        for (unsigned i = 0; i < 1000000 && word(0x400) != 0; ++i) tick();
        require(word(0x400) == 0, "reset did not start the selected firmware");
    }

    void boot(unsigned variant) {
        unsigned i = 0;
        for (; i < 2000000 && word(0x400) != 0xC0DE && word(0x400) != 0xE000; ++i)
            tick();
        require(word(0x400) == 0xC0DE,
                "diagnostic failed or timed out at stage " + std::to_string(word(0x402)) +
                    ", bus errors " + std::to_string(word(0x406)) +
                    ", bus address " + std::to_string(dut.debug_addr));
        // Drain the final store and let a retriggered interrupt become visible.
        for (unsigned n = 0; n < 2000; ++n) tick();
        require(word(0x402) == 0 && word(0x404) == variant, "wrong firmware pass signature");
        require(word(0x406) == 8 && faults == 8, "bus-error vector did not handle eight faults");
        require(word(0x408) == 1 && irq_sent && saw_iack, "expansion IRQ did not autovector once");
        require(highest_rom_word && highest_ram_word, "highest ROM/RAM words were not exercised");
        require(word(0x800) == 0xA55A && longword(0x804) == 0x12345678,
                "byte lanes or big-endian longword storage failed");
        require(word(0x820) == 0x92B4 && ram_write_counts[0x820] == 3,
                "TAS did not perform exactly one write on each RAM byte lane");
        require(longword(0x07FFFC) == 0xFEA51234, "512 KiB RAM boundary failed");
        const std::array<uint16_t, 4> banks{0x1101, 0x2202, 0x3303, 0x4404};
        for (unsigned bank = 0; bank < banks.size(); ++bank)
            require(word(bank * 0x20000 + 0x880) == banks[bank], "RAM bank address aliased");
        require(ram_write_counts[0x800] == 3 && ram_write_counts[0x804] == 1 &&
                    ram_write_counts[0x806] == 1,
                "RAM writes did not complete exactly once");
        const std::array<uint16_t, 4> planes{0xAAAA, 0xCCCC, 0xF0F0, 0xFF00};
        for (unsigned plane = 0; plane < planes.size(); ++plane)
            require(word(0x10000 + 2 * plane) == planes[plane], "CPU framebuffer write failed");
        require(dut.screen_base == 0x10000 && dut.resolution == 0, "video register programming failed");
        for (unsigned index = 0; index < 16; ++index) {
            const unsigned offset = index * 9;
            const uint64_t packed = uint64_t(dut.palette[offset / 32]) |
                (offset / 32 + 1 < 5 ? uint64_t(dut.palette[offset / 32 + 1]) << 32 : 0);
            const unsigned expected = ((index & 7) << 6) | (((index + 2) & 7) << 3) |
                                      ((index + 4) & 7);
            require(((packed >> (offset % 32)) & 0x1FF) == expected,
                    "palette register packing or bit masking failed");
        }
        require(expansion_write_counts[0xFF9000] == 5 &&
                    expansion_write_counts[0xFF9008] == 1 &&
                    expansion_write_counts[0xFF900A] == 1,
                "expansion writes did not complete exactly once");
        require(dut.probe_scratch == 0xD6F8 && dut.probe_long == 0xABCDEF01 &&
                    dut.probe_write_count == 7,
                "RTL expansion probe state or write count failed");
        if (delay)
            require(rom_wait_ticks && ram_wait_ticks, "delayed memory was never stalled");
        require(expansion_wait_ticks, "expansion timeout/wait behavior was not exercised");
        std::cout << "fes.atari-st machine: firmware " << variant << " passed after " << i
                  << " cycles (memory waits " << rom_wait_ticks << '/' << ram_wait_ticks
                  << ", expansion waits " << expansion_wait_ticks << ")\n";
    }

    void picture(bool scanlines) {
        constexpr unsigned width = 1650, frame = width * 750;
        constexpr uint32_t sof = 1u << 28;
        dut.video_scanlines = scanlines;
        unsigned waiting = 0;
        for (; waiting <= frame && !(dut.video_request & sof); ++waiting) tick();
        require(waiting <= frame, "integrated raster did not reach start of frame");

        unsigned checked = 0;
        for (unsigned position = 0; position < width * 62 + 66; ++position) {
            tick();
            if (position == 0) continue;
            // The response after this edge is the preceding registered request.
            const unsigned source = position - 1;
            const unsigned x = source % width, y = source / width;
            if (y < 60 || y > 62 || x >= 64) continue;
            const unsigned native_x = x / 4;
            unsigned index = 0;
            const std::array<uint16_t, 4> planes{0xAAAA, 0xCCCC, 0xF0F0, 0xFF00};
            for (unsigned plane = 0; plane < planes.size(); ++plane)
                index |= ((planes[plane] >> (15 - native_x)) & 1) << plane;
            const auto expand = [](unsigned channel) {
                return (channel << 5) | (channel << 2) | (channel >> 1);
            };
            uint32_t rgb = (expand(index & 7) << 16) |
                           (expand((index + 2) & 7) << 8) | expand((index + 4) & 7);
            if (scanlines && (y & 1)) rgb = (rgb & 0xFEFEFE) >> 1;
            require(dut.video_response == ((1u << 27) | (1u << 24) | rgb),
                    std::string(scanlines ? "scanline" : "direct") +
                        " video part failed CPU-written pixel " + std::to_string(x) +
                        "," + std::to_string(y));
            ++checked;
        }
        require(checked == 64 * 3, "integrated CPU picture sampling was incomplete");
        std::cout << "fes.atari-st machine: CPU-written four-plane pixels passed through "
                  << (scanlines ? "scanline" : "direct") << " video part\n";
    }
};

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    const std::string directory = argc > 1 ? argv[1] : "build/diagnostics/fes-atari-st";
    const auto primary = load_rom(directory + "/firmware.rom");
    const auto replacement = load_rom(directory + "/firmware-variant.rom");
    require(primary != replacement, "replacement firmware bytes did not change");
    Machine machine(primary);
    machine.reset(true, 0, 0);
    machine.boot(1);
    machine.picture(false);
    machine.picture(true);

    // Replace the ROM in the same machine while held in reset, preserving RAM.
    machine.dut.reset = 1;
    machine.rom = replacement;
    machine.reset(true, 9, 17);
    machine.boot(2);

    machine.dut.reset = 1;
    machine.rom = primary;
    machine.reset(false, 3, 0);
    for (unsigned i = 0; i < 1000000 && machine.word(0x400) != 0xE000; ++i)
        machine.tick();
    require(machine.word(0x400) == 0xE000 && machine.word(0x402) == 5,
            "vacant expansion did not fail the CPU probe at stage 5");
    require(machine.faults == 1 && machine.word(0x406) == 1,
            "vacant expansion did not deliver its timeout through the bus-error vector");
    require(machine.dut.probe_write_count == 0, "vacant expansion accepted a write");
    std::cout << "fes.atari-st machine: vacant expansion timed out through CPU exception\n";
    machine.dut.final();
    return EXIT_SUCCESS;
}
