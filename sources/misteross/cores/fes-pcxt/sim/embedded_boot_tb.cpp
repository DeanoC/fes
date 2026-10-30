// SPDX-License-Identifier: GPL-3.0-or-later
// Verify actual embedded ROM upload, physical SDRAM bytes and boot reset gate.
#include "Vembedded_boot_top.h"
#include "verilated.h"
#include "memory_model.hpp"
#include <algorithm>
#include <fstream>
#include <iostream>
#include <iterator>
#include <string>
#include <vector>

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    require(argc == 2, "expected floppy image argument", 0);
    std::ifstream input(argv[1], std::ios::binary);
    const std::vector<uint8_t> image{std::istreambuf_iterator<char>(input), {}};
    require(image.size() == 368640, "floppy image geometry", 0);
    Vembedded_boot_top d;
    Sdram<Vembedded_boot_top> ram;
    uint64_t cycles = 0;
    unsigned pixel_phase = 0;
    uint32_t offered = 0, completed = 0;
    bool pending = false;
    uint64_t writes_before = 0;
    bool check_upload = true, observe_boot = false;
    bool post = false, boot = false, sector = false, prompt = false, last_irq = false;
    unsigned bat_reads = 0, irq1_edges = 0;
    uint64_t post_cycle = 0, boot_cycle = 0, sector_cycle = 0, prompt_cycle = 0;
    auto byte = [&](unsigned address) { return (ram.words.at(address / 2) >> (8 * (address & 1))) & 0xff; };
    auto word = [&](unsigned address) { return byte(address) | (byte(address + 1) << 8); };
    auto screen = [&] {
        std::string text;
        for (unsigned y = 0; y < 25; ++y) {
            for (unsigned x = 0; x < 80; ++x) {
                d.text_address = y * 80 + x;
                d.eval();
                unsigned ch = d.text_word & 0xff;
                text += char(ch >= 32 && ch < 127 ? ch : ' ');
            }
            text += '\n';
        }
        return text;
    };
    auto step = [&] {
        d.clk = 0;
        pixel_phase += 1485;
        if (pixel_phase >= 2000) { pixel_phase -= 2000; d.clk_pixel = !d.clk_pixel; }
        d.eval();
        d.dq_sample = ram.tick(d);
        d.eval();
        const bool accept = d.disk_valid && d.disk_ready;
        const bool done = d.disk_done;
        const bool consumed = d.instruction_consumed;
        const unsigned ip = d.instruction_address, flags = d.post_flags;
        const bool io_read = d.bus_done && d.bus_kind == 1 && !d.bus_write;
        const unsigned address = d.bus_address, data = d.bus_data;
        const bool irq = d.keyboard_irq;
        if (check_upload) {
            if (!d.disk_loaded) require(d.machine_reset, "CPU released before full disk", cycles);
            if (accept) {
                require(!pending && offered < image.size(), "overlapping or excess disk offer", cycles);
                require(d.disk_address == offered, "disk ROM offered wrong address", cycles);
                require(d.disk_data == image[offered], "disk ROM offered wrong byte", cycles);
                writes_before = ram.writes;
                pending = true;
                ++offered;
            }
            if (done) {
                require(pending && completed < offered, "unmatched disk completion", cycles);
                require(ram.writes == writes_before + 1, "disk completion lacked one physical write", cycles);
                const auto word = ram.words[0x80000u + completed / 2];
                const auto actual = static_cast<uint8_t>((word >> ((completed & 1) * 8)) & 0xff);
                require(actual == image[completed], "physical SDRAM byte differs from ROM", cycles);
                pending = false;
                ++completed;
            }
            if (d.disk_loaded)
                require(completed == image.size() && !pending, "disk visible before last physical completion", cycles);
        }
        d.clk = 1;
        d.eval();
        ++cycles;
        if (observe_boot) {
            if (irq && !last_irq) ++irq1_edges;
            last_irq = irq;
            if (io_read && (address & 0x3ff) == 0x60 && data == 0xaa) ++bat_reads;
            if (consumed) {
                require(ip != 0xfe47a, "BIOS reported POST error", cycles);
                if (ip == 0xfe49a && !post) {
                    require(flags == 0 && bat_reads == 1 && irq1_edges >= 1,
                            "POST did not receive keyboard BAT over IRQ1", cycles);
                    post = true; post_cycle = cycles;
                }
                if (ip == 0xfe6f2 && !boot) { boot = true; boot_cycle = cycles; }
                if (ip == 0x7c00 && !sector) {
                    require(boot, "boot sector executed before BIOS bootstrap", cycles);
                    for (unsigned i = 0; i < 512; ++i)
                        require(byte(0x7c00 + i) == image[i], "BIOS loaded wrong boot sector", cycles);
                    sector = true; sector_cycle = cycles;
                }
            }
            if (cycles % 5000000 == 0) {
                if (sector && screen().find("A:\\>") != std::string::npos) {
                    prompt = true; prompt_cycle = cycles;
                }
                ram.ram_writes.clear();
                if (cycles % 50000000 == 0)
                    std::cerr << "cycles=" << cycles << " ip=" << std::hex << ip << std::dec
                              << " bat=" << bat_reads << " irq1=" << irq1_edges
                              << " post=" << post << " boot=" << boot << " sector=" << sector
                              << " prompt=" << prompt << '\n';
            }
        }
        require(!d.fault, "CPU/ISA/DMA fault", cycles);
        require(cycles < 6000000000ULL, "embedded DOS boot timed out", cycles);
    };
    auto run_until = [&](auto predicate, const char *what) {
        for (uint64_t i = 0; i < 45000000 && !predicate(); ++i) step();
        require(predicate(), what, cycles);
    };
    auto upload = [&] {
        run_until([&] { return d.disk_loaded; }, "full disk upload did not finish");
        require(completed == image.size(), "disk missing completed bytes", cycles);
        run_until([&] { return !d.machine_reset; }, "CPU did not release after disk upload");
    };
    auto poison = [&] {
        for (size_t i = 0; i < image.size(); i += 2)
            ram.words[0x80000u + i / 2] = static_cast<uint16_t>(
                (image[i] ^ 0xffu) | ((image[i + 1] ^ 0xffu) << 8));
    };
    poison();
    d.clk = d.clk_pixel = 0;
    d.text_address = 0;
    d.cold_reset = d.reset_pixel = d.execution_hold = 1;
    for (unsigned i = 0; i < 32; ++i) step();
    ram.cold_reset();
    d.cold_reset = d.reset_pixel = 0;
    for (unsigned i = 0; i < 20000; ++i) step();
    require(!d.firmware_loaded && !d.disk_loaded && d.machine_reset && !offered,
            "execution hold failed to hold the ROM loader", cycles);
    d.execution_hold = 0;
    upload();
    check_upload = false;
    d.execution_hold = 1;
    for (unsigned i = 0; i < 128; ++i) step();
    require(d.machine_reset && d.firmware_loaded && d.disk_loaded && !d.disk_valid,
            "warm reset discarded disk or firmware", cycles);
    d.execution_hold = 0;
    run_until([&] { return !d.machine_reset; }, "CPU did not release after warm reset");
    d.cold_reset = d.reset_pixel = d.execution_hold = 1;
    for (unsigned i = 0; i < 32; ++i) step();
    require(!d.disk_loaded && !d.firmware_loaded && d.machine_reset,
            "cold reset retained disk or firmware admission", cycles);
    ram.cold_reset();
    poison();
    offered = completed = 0;
    pending = false;
    check_upload = true;
    d.cold_reset = d.reset_pixel = 0;
    for (unsigned i = 0; i < 20000; ++i) step();
    require(!d.firmware_loaded && !d.disk_loaded && d.machine_reset && !offered,
            "cold restart ignored execution hold", cycles);
    d.execution_hold = 0;
    upload();
    check_upload = false;
    observe_boot = true;
    while (!prompt) step();
    require(post && boot && sector && bat_reads == 1 && irq1_edges >= 1,
            "incomplete embedded BIOS/DOS boot", cycles);
    require(word(0x413) == 640 && (word(0x410) & 0x31) == 0x21,
            "BIOS equipment profile differs", cycles);
    std::cout << "PASS PC/XT embedded DOS: keyboard BAT/IRQ1, POST, INT19, floppy boot sector "
              << "and A: prompt; post=" << post_cycle << " boot=" << boot_cycle
              << " sector=" << sector_cycle << " prompt=" << prompt_cycle << "\n";
    std::cout << "PASS PC/XT embedded DOS: two ROM-to-SDRAM floppy loads, "
              << image.size() << " checked bytes each, reset gating and warm/cold behavior; "
              << cycles << " clocks\n";
}
