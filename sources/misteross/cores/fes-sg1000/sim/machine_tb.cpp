// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vsg1000_machine.h"
#include "Vsg1000_machine___024root.h"
#include "verilated.h"

#include <algorithm>
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

void check_cpu_pins(const Vsg1000_machine &dut) {
    const auto &pins = *dut.rootp;
    require(!(pins.sg1000_machine__DOT__ce_cpu_p &&
              pins.sg1000_machine__DOT__ce_cpu_n), "overlapping CPU enables");
    require(!pins.sg1000_machine__DOT__cpu_illegal, "CPU illegal-state signal");
    if (!pins.sg1000_machine__DOT__nRFSH) {
        require(pins.sg1000_machine__DOT__nRD &&
                pins.sg1000_machine__DOT__nWR &&
                pins.sg1000_machine__DOT__nIORQ,
                "refresh must not access memory data or I/O devices");
    }
    require(pins.sg1000_machine__DOT__nMREQ ||
            pins.sg1000_machine__DOT__nIORQ,
            "memory and I/O requests overlap");
}

void check_captured_enables(const Vsg1000_machine &dut, bool raw_p,
                            bool raw_n, bool machine_reset) {
    const auto &pins = *dut.rootp;
    require(pins.sg1000_machine__DOT__ce_cpu_p == (!machine_reset && raw_p) &&
            pins.sg1000_machine__DOT__ce_cpu_n == (!machine_reset && raw_n),
            "CPU enables must capture both polarities on the system edge");
}

}  // namespace

#ifdef FES_SG1000_ROM_LINK
int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    if (argc != 2) {
        std::cerr << "FES SG-1000 linked ROM: ROM argument missing\n";
        return EXIT_FAILURE;
    }
    FILE *rom = std::fopen(argv[1], "rb");
    if (rom == nullptr) return EXIT_FAILURE;
    std::vector<uint8_t> expected;
    uint8_t byte = 0;
    while (std::fread(&byte, 1, 1, rom) == 1) expected.push_back(byte);
    std::fclose(rom);
    if (expected.size() != 16384) return EXIT_FAILURE;

    Vsg1000_machine dut;
    dut.clk_sys = 0;
    dut.reset = 1;
    dut.keyboard = 0xffffffffffull;
    dut.media_ready = 0;
    dut.media_size = 0;
    dut.media_data = 0;
    dut.peek_addr = 0;
    dut.eval();
    unsigned rom_address[2] = {0, 0};
    unsigned rom_primed = 0;
    auto tick = [&]() {
        dut.eval();
        const unsigned cartridge_address = dut.cpu_addr_debug & 0x3fff;
        const bool raw_p = dut.rootp->sg1000_machine__DOT__ce_cpu_raw_p;
        const bool raw_n = dut.rootp->sg1000_machine__DOT__ce_cpu_raw_n;
        const bool machine_reset = dut.rootp->sg1000_machine__DOT__machine_reset;
        dut.clk_sys = 1; dut.eval();
        check_captured_enables(dut, raw_p, raw_n, machine_reset);
        // The linked ROM is a two-clock pipeline, so the byte matches the
        // address sampled two ticks earlier. The first two ticks fill it.
        if (rom_primed >= 2) {
            require(dut.rootp->sg1000_machine__DOT__cartridge_read ==
                        expected[rom_address[0]],
                    "linked cartridge byte must follow the sampled ROM address");
        }
        rom_address[0] = rom_address[1];
        rom_address[1] = cartridge_address;
        if (rom_primed < 2) ++rom_primed;
        check_cpu_pins(dut);
        dut.clk_sys = 0; dut.eval();
    };
    for (unsigned i = 0; i < 8; ++i) tick();
    for (unsigned address : {0u, 0x03ffu, 0x1000u, 0x3fffu}) {
        dut.peek_addr = address;
        dut.eval();
        if (uint8_t(dut.peek_data) != expected[address]) {
            std::cerr << "FES SG-1000 linked ROM: peek mismatch at " << address << '\n';
            return EXIT_FAILURE;
        }
    }
    dut.peek_addr = 0x4000;
    dut.eval();
    if (uint8_t(dut.peek_data) != 0xff) return EXIT_FAILURE;
    dut.reset = 0;
    unsigned cycles = 0;
    for (; cycles < 40000000 && dut.cpu_halt_n; ++cycles) tick();
    if (cycles == 40000000) {
        std::cerr << "FES SG-1000 linked ROM: diagnostic did not HALT\n";
        return EXIT_FAILURE;
    }
    dut.peek_addr = 0xc000;
    tick();
    if (uint8_t(dut.peek_data) != 0xa5) {
        std::cerr << "FES SG-1000 linked ROM: RAM signature missing\n";
        return EXIT_FAILURE;
    }
    dut.peek_addr = 0xc001;
    tick();
    require(uint8_t(dut.peek_data) == 0xff,
            "linked cartridge did not capture neutral controller port");
    unsigned colors_seen = 0;
    for (unsigned i = 0; i < 1000000; ++i) {
        tick();
        if (!dut.logical_blank)
            colors_seen |= 1u << dut.logical_pixel;
    }
    require((colors_seen & (colors_seen - 1)) != 0,
            "linked cartridge VDP writes did not produce multiple colors");
    require(dut.media_addr == 0, "linked cartridge issued a media request");
    std::cout << "FES SG-1000 linked-ROM machine checks passed\n";
    return EXIT_SUCCESS;
}
#else
namespace {

void tick(Vsg1000_machine &dut, const std::vector<uint8_t> &cartridge,
          uint8_t &registered_media_data, unsigned *psg_writes = nullptr,
          unsigned *psg_ticks = nullptr) {
    const uint16_t requested_address = uint16_t(dut.media_addr);
#ifdef FES_SG1000_OSS
    dut.media_data = registered_media_data;
#else
    if (requested_address < cartridge.size())
        dut.media_data = cartridge[requested_address];
    else
        dut.media_data = 0xff;
#endif
    dut.eval();
    if (psg_writes != nullptr && dut.psg_write_debug) ++*psg_writes;
    if (psg_ticks != nullptr && dut.psg_ce_debug) ++*psg_ticks;
    const bool raw_p = dut.rootp->sg1000_machine__DOT__ce_cpu_raw_p;
    const bool raw_n = dut.rootp->sg1000_machine__DOT__ce_cpu_raw_n;
    const bool machine_reset = dut.rootp->sg1000_machine__DOT__machine_reset;
    dut.clk_sys = 1;
    dut.eval();
    check_captured_enables(dut, raw_p, raw_n, machine_reset);
    check_cpu_pins(dut);
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

    // The physical CPU RAM has registered reads. Exercise read-modify-write,
    // mirrored operands, indexed operands and both stack-byte reads.
    const std::vector<uint8_t> ram_readback_prog{
        0xf3, 0x31, 0x00, 0xc4,    // DI; LD SP,C400
        0x01, 0x34, 0x12, 0xc5,    // LD BC,1234; PUSH BC
        0x01, 0x00, 0x00, 0xc1,    // LD BC,0000; POP BC
        0x78, 0x32, 0x10, 0xc0,    // save recovered B
        0x79, 0x32, 0x11, 0xc0,    // save recovered C
        0x21, 0x00, 0xc0,          // LD HL,C000
        0x36, 0x5a, 0x34,          // LD (HL),5A; INC (HL)
        0x3a, 0x00, 0xc4,          // LD A,(C400), mirrored C000
        0x32, 0x12, 0xc0,
        0xdd, 0x21, 0x02, 0xc0,    // LD IX,C002
        0xdd, 0xcb, 0xfe, 0x06,    // RLC (IX-2)
        0xdd, 0x7e, 0xfe,          // LD A,(IX-2)
        0x32, 0x13, 0xc0, 0x76
    };
    load_blob(dut, ram_readback_prog, registered_media_data);
    dut.reset = 0;
    cycles = 0;
    for (; cycles < 100000 && dut.cpu_halt_n; ++cycles)
        tick(dut, ram_readback_prog, registered_media_data);
    require(cycles < 100000, "RAM readback probe did not HALT");
    require(peek(dut, 0xc010, registered_media_data) == 0x12 &&
            peek(dut, 0xc011, registered_media_data) == 0x34,
            "stack reads returned stale registered RAM data");
    require(peek(dut, 0xc012, registered_media_data) == 0x5b,
            "mirrored RAM read-modify-write mismatch");
    require(peek(dut, 0xc013, registered_media_data) == 0xb6,
            "indexed RAM readback mismatch");

    // A held OUT must write one VRAM byte; each held IN must return its
    // pre-side-effect byte and advance the registered read-ahead exactly once.
    const std::vector<uint8_t> vdp_readback_prog{
        0xf3,
        0x3e, 0x00, 0xd3, 0xbf,  // write address 0100
        0x3e, 0x41, 0xd3, 0xbf,
        0x3e, 0xa6, 0xd3, 0xbe,
        0x3e, 0xb7, 0xd3, 0xbe,
        0x3e, 0xc8, 0xd3, 0xbe,
        0x3e, 0x00, 0xd3, 0xbf,  // read address 0100, prime read-ahead
        0x3e, 0x01, 0xd3, 0xbf,
        0xdb, 0xbe, 0x32, 0x20, 0xc0,
        0xdb, 0xbe, 0x32, 0x21, 0xc0,
        0xdb, 0xbe, 0x32, 0x22, 0xc0,
        0x76
    };
    load_blob(dut, vdp_readback_prog, registered_media_data);
    dut.reset = 0;
    cycles = 0;
    for (; cycles < 100000 && dut.cpu_halt_n; ++cycles)
        tick(dut, vdp_readback_prog, registered_media_data);
    require(cycles < 100000, "VDP readback probe did not HALT");
    require(peek(dut, 0xc020, registered_media_data) == 0xa6 &&
            peek(dut, 0xc021, registered_media_data) == 0xb7 &&
            peek(dut, 0xc022, registered_media_data) == 0xc8,
            "VDP held-read byte or single-advance mismatch");

    // Check that the integrated clock generator retains native cadence and
    // that three successive M1 cycles expose the pre-increment refresh R.
    const std::vector<uint8_t> cadence_prog{0x00, 0x00, 0x76};
    load_blob(dut, cadence_prog, registered_media_data);
    dut.reset = 0;
    int last_positive = -1;
    unsigned positive_edges = 0, refreshes = 0;
    bool refresh_active = false;
    cycles = 0;
    for (; cycles < 100000 && dut.cpu_halt_n; ++cycles) {
        const auto &pins = *dut.rootp;
        if (pins.sg1000_machine__DOT__ce_cpu_p) {
            if (last_positive >= 0)
                require(cycles - unsigned(last_positive) == 14 ||
                        cycles - unsigned(last_positive) == 15,
                        "CPU positive-enable cadence changed");
            last_positive = int(cycles);
            ++positive_edges;
        }
        if (!pins.sg1000_machine__DOT__nRFSH && !refresh_active) {
            require(uint16_t(dut.cpu_addr_debug) == refreshes,
                    "M1 refresh did not expose pre-increment R");
            ++refreshes;
        }
        refresh_active = !pins.sg1000_machine__DOT__nRFSH;
        tick(dut, cadence_prog, registered_media_data);
    }
    require(cycles < 100000 && refreshes == 3 && positive_edges >= 12,
            "native NOP/NOP/HALT cadence or refresh count");

    // Release reset at different running oscillator phases. Both first-enable
    // polarities must work, and every following half-cycle must alternate
    // with the same seven/eight-clock spacing.
    bool first_positive_seen = false, first_negative_seen = false;
    for (unsigned offset = 0; offset < 30; ++offset) {
        load_blob(dut, cadence_prog, registered_media_data);
        for (unsigned i = 0; i < offset; ++i)
            tick(dut, cadence_prog, registered_media_data);
        require(!dut.rootp->sg1000_machine__DOT__ce_cpu_p &&
                !dut.rootp->sg1000_machine__DOT__ce_cpu_n,
                "reset must clear both captured enables");
        dut.reset = 0;
        dut.eval();
        int last_half = -1;
        bool last_polarity = false;
        refresh_active = false;
        refreshes = 0;
        cycles = 0;
        for (; cycles < 10000 && dut.cpu_halt_n; ++cycles) {
            const auto &pins = *dut.rootp;
            if (pins.sg1000_machine__DOT__ce_cpu_p ||
                pins.sg1000_machine__DOT__ce_cpu_n) {
                const bool positive = pins.sg1000_machine__DOT__ce_cpu_p;
                if (last_half >= 0) {
                    require(cycles - unsigned(last_half) == 7 ||
                            cycles - unsigned(last_half) == 8,
                            "reset release changed half-cycle spacing");
                    require(positive != last_polarity,
                            "reset release repeated an enable polarity");
                } else {
                    first_positive_seen |= positive;
                    first_negative_seen |= !positive;
                }
                last_half = int(cycles);
                last_polarity = positive;
            }
            if (!pins.sg1000_machine__DOT__nRFSH && !refresh_active) {
                require(uint16_t(dut.cpu_addr_debug) == refreshes,
                        "warm release refresh sequence");
                ++refreshes;
            }
            refresh_active = !pins.sg1000_machine__DOT__nRFSH;
            tick(dut, cadence_prog, registered_media_data);
        }
        require(cycles < 10000 && refreshes == 3,
                "reset phase probe did not execute NOP/NOP/HALT");
    }
    require(first_positive_seen && first_negative_seen,
            "reset phase probe did not cover both first-enable polarities");

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

    // Five real Z80 OUTs exercise the low, middle and high PSG decode mirrors.
    const std::vector<uint8_t> sound_prog{
        0x3e, 0x84, 0xd3, 0x40,  // tone 0 low nibble
        0x3e, 0x02, 0xd3, 0x5f,  // tone 0 high bits
        0x3e, 0x90, 0xd3, 0x7f,  // unmute tone 0
        0x3e, 0xe0, 0xd3, 0x7f,  // noise control
        0x3e, 0xf0, 0xd3, 0x7f,  // unmute noise
        0xd3, 0x3f,              // adjacent port is not the PSG
        0xd3, 0x80,              // next decode range is not the PSG
        0xdb, 0x40,              // PSG port read is not a write
        0x32, 0x00, 0xc0,        // memory write is not a PSG write
        0x76
    };
    load_blob(dut, sound_prog, registered_media_data);
    require(int16_t(dut.psg_sample) == 0, "PSG must be silent on reset");
    dut.reset = 0;
    unsigned psg_writes = 0;
    bool heard_sample = false;
    cycles = 0;
    for (; cycles < 100000 && dut.cpu_halt_n; ++cycles) {
        tick(dut, sound_prog, registered_media_data, &psg_writes);
        heard_sample |= int16_t(dut.psg_sample) != 0;
    }
    require(cycles < 100000, "sound program did not HALT");
    require(psg_writes == 5, "PSG must accept each OUT exactly once");
    require(heard_sample, "PSG tone/noise program produced no PCM");

    // Isolate noise after reset: tone 0 cannot satisfy this sign-change test.
    const std::vector<uint8_t> noise_prog{
        0x3e, 0xe4, 0xd3, 0x40,  // white noise, fixed shift rate
        0x3e, 0xf4, 0xd3, 0x40,  // unmute noise only
        0x76
    };
    load_blob(dut, noise_prog, registered_media_data);
    dut.reset = 0;
    cycles = 0;
    for (; cycles < 100000 && dut.cpu_halt_n; ++cycles)
        tick(dut, noise_prog, registered_media_data);
    require(cycles < 100000, "noise-only program did not HALT");
    unsigned psg_ticks = 0;
    bool noise_positive = false, noise_negative = false;
    for (unsigned i = 0; i < 520000; ++i) {
        tick(dut, noise_prog, registered_media_data, nullptr, &psg_ticks);
        noise_positive |= int16_t(dut.psg_sample) > 0;
        noise_negative |= int16_t(dut.psg_sample) < 0;
    }
    require(psg_ticks == 35641 || psg_ticks == 35642,
            "PSG fractional clock rate is not 3.579545 MHz");
    require(noise_positive && noise_negative, "noise-only PSG phase did not vary");
    dut.reset = 1;
    for (unsigned i = 0; i < 4; ++i)
        tick(dut, noise_prog, registered_media_data);
    require(int16_t(dut.psg_sample) == 0, "PSG reset did not mute PCM");

    // Execute original test code through the NMOS CPU: DI must mask VDP VBlank,
    // while EI/IM1 dispatches to 0038. 0066 is a distinct NMI failure trap.
    for (bool interrupts_enabled : {false, true}) {
        std::vector<uint8_t> interrupt_rom(256, 0x00);
        const std::vector<uint8_t> setup{
            0xf3,                   // DI
            0x31, 0x00, 0xc4,       // LD SP,C400
            0xaf,                   // XOR A
            0x32, 0x00, 0xc0,       // clear IM1 signature
            0x32, 0x01, 0xc0,       // clear NMI signature
            0x3e, 0x20, 0xd3, 0xbf, // VDP R1 IE, display disabled
            0x3e, 0x81, 0xd3, 0xbf,
            0xed, 0x56,             // IM 1
            uint8_t(interrupts_enabled ? 0xfb : 0x00), // EI or NOP
            0x76, 0x18, 0xfd        // HALT; JR HALT
        };
        std::copy(setup.begin(), setup.end(), interrupt_rom.begin());
        const std::vector<uint8_t> int_handler{
            0xdb, 0xbf,             // read status: acknowledges VBlank
            0x3e, 0x5a, 0x32, 0x00, 0xc0,
            0xf3, 0x76, 0x18, 0xfd  // DI; HALT; JR HALT
        };
        std::copy(int_handler.begin(), int_handler.end(), interrupt_rom.begin() + 0x38);
        const std::vector<uint8_t> nmi_handler{
            0x3e, 0xa5, 0x32, 0x01, 0xc0,
            0x76, 0x18, 0xfd
        };
        std::copy(nmi_handler.begin(), nmi_handler.end(), interrupt_rom.begin() + 0x66);
        load_blob(dut, interrupt_rom, registered_media_data);
        dut.reset = 0;
        // More than two full raster frames, independent of CPU execution speed.
        for (unsigned i = 0; i < 2000000; ++i)
            tick(dut, interrupt_rom, registered_media_data);
        require(peek(dut, 0xc001, registered_media_data) == 0,
                "SG-1000 VDP must not invoke NMI at 0066");
        require(peek(dut, 0xc000, registered_media_data) ==
                    (interrupts_enabled ? 0x5a : 0),
                "SG-1000 VDP must respect DI and enter IM1 at 0038 after EI");
        require(!dut.cpu_halt_n, "interrupt diagnostic must return to HALT");
    }

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
#endif
