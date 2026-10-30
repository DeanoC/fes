// SPDX-License-Identifier: GPL-3.0-or-later
// Verify actual embedded ROM upload, physical SDRAM bytes and boot reset gate.
#include "Vembedded_boot_top.h"
#include "verilated.h"
#include "memory_model.hpp"
#include <algorithm>
#include <fstream>
#include <iostream>
#include <iterator>
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
    bool check_upload = true;
    auto step = [&] {
        d.clk = 0;
        pixel_phase += 1485;
        if (pixel_phase >= 2000) { pixel_phase -= 2000; d.clk_pixel = !d.clk_pixel; }
        d.eval();
        d.dq_sample = ram.tick(d);
        d.eval();
        const bool accept = d.disk_valid && d.disk_ready;
        const bool done = d.disk_done;
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
        require(!d.fault, "CPU/ISA/DMA fault", cycles);
        require(cycles < 100000000, "embedded boot timed out", cycles);
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
    std::cout << "PASS PC/XT embedded DOS: two ROM-to-SDRAM floppy loads, "
              << image.size() << " checked bytes each, reset gating and warm/cold behavior; "
              << cycles << " clocks\n";
}
