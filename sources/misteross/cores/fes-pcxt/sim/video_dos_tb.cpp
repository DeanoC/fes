// SPDX-License-Identifier: GPL-2.0-or-later
// Portable POSIX C++17 simulator for CGA, MDA, and Hercules DOS tests and games.
#include "Vvideo_dos_top.h"
#include "verilated.h"
#include "memory_model.hpp"
#include <algorithm>
#include <array>
#include <cctype>
#include <cstdint>
#include <fstream>
#include <iostream>
#include <sstream>
#include <string>
#include <vector>

static std::vector<unsigned char> read_file(const char* path) {
    std::ifstream input(path, std::ios::binary);
    require(bool(input), "fixture file cannot be opened", 0);
    return {std::istreambuf_iterator<char>(input), {}};
}

static unsigned hid_usage_for_name(const std::string& raw) {
    std::string s;
    for (char ch : raw) s.push_back(static_cast<char>(std::tolower(static_cast<unsigned char>(ch))));
    if (s.size() == 1) {
        char c = s[0];
        if (c >= 'a' && c <= 'z') return 4u + unsigned(c - 'a');
        if (c >= '1' && c <= '9') return 30u + unsigned(c - '1');
        if (c == '0') return 39u;
    }
    if (s == "enter" || s == "return") return 40u;
    if (s == "esc" || s == "escape") return 41u;
    if (s == "backspace" || s == "bksp") return 42u;
    if (s == "tab") return 43u;
    if (s == "space") return 44u;
    if (s == "right") return 79u;
    if (s == "left") return 80u;
    if (s == "down") return 81u;
    if (s == "up") return 82u;
    if (s == "f1") return 58u;
    if (s == "f2") return 59u;
    if (s == "f3") return 60u;
    if (s == "f4") return 61u;
    if (s == "f5") return 62u;
    if (s == "f6") return 63u;
    if (s == "f7") return 64u;
    if (s == "f8") return 65u;
    if (s == "f9") return 66u;
    if (s == "f10") return 67u;
    if (s == "y") return 28u;
    if (s == "n") return 17u;
    require(false, ("unknown key token: " + raw).c_str(), 0);
    return 0u;
}

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    require(argc >= 7 && argc <= 9,
            "usage: Vvideo_dos_top <firmware> <floppy> <video_card> <mono_tint> <mode> <out_dir> [max_seconds] [keys_spec]",
            0);
    const auto firmware = read_file(argv[1]);
    const auto disk = read_file(argv[2]);
    const unsigned video_card = std::stoul(argv[3]);
    const unsigned mono_tint = std::stoul(argv[4]);
    const std::string mode = argv[5];
    const std::string out_dir = argv[6];
    const double max_seconds = argc >= 8 ? std::stod(argv[7]) : 60.0;
    const std::string keys_spec = argc >= 9 ? argv[8] : "";

    require(firmware.size() == 65536, "64 KiB firmware window", 0);
    require(disk.size() == 40 * 2 * 9 * 512, "360 KiB floppy geometry", 0);
    require(video_card <= 2, "video_card must be 0 (CGA), 1 (MDA), or 2 (HGC)", 0);
    require(mono_tint <= 2, "mono_tint must be 0 (White), 1 (Green), or 2 (Amber)", 0);
    require(mode == "test-mda" || mode == "test-hgc" || mode == "run", "unknown mode", 0);

    Vvideo_dos_top d;
    Sdram<Vvideo_dos_top> ram;

    uint64_t cycles = 0, post_cycle = 0, boot_cycle = 0, sector_cycle = 0;
    uint64_t vram_writes = 0, speaker_edges = 0;
    unsigned pixel_phase = 0, bat_reads = 0, key_reads = 0, media_reads = 0;
    unsigned dma2_completions = 0, irq6_edges = 0, pending_address = 0;
    bool pending = false, last_dma = false, last_irq = false;
    bool post = false, boot = false, sector = false;
    bool saw_hgc_gfx = false, cga_gfx = false;
    int16_t last_sample = 0;
    std::vector<unsigned> key_codes;

    bool capturing_frame = false, collecting = false, frame_complete = false;
    std::vector<unsigned char> frame(1280 * 720 * 3);
    unsigned frame_pixels = 0;

    auto byte = [&](unsigned a) { return unsigned((ram.words.at(a / 2) >> (8 * (a & 1))) & 255); };
    auto word = [&](unsigned a) { return byte(a) | (byte(a + 1) << 8); };

    auto read_screen = [&](bool dump_files) {
        std::string text;
        std::ofstream cells;
        if (dump_files) cells.open(out_dir + "/screen.bin", std::ios::binary);
        for (unsigned y = 0; y < 25; ++y) {
            for (unsigned x = 0; x < 80; ++x) {
                unsigned w = 0;
                if (video_card == 0) {
                    d.cga_text_address = y * 80 + x;
                    d.eval();
                    w = d.cga_text_word;
                } else {
                    d.mda_text_address = y * 80 + x;
                    d.eval();
                    w = d.mda_text_word;
                }
                unsigned c = w & 255;
                if (dump_files) {
                    cells.put(char(c));
                    cells.put(char(w >> 8));
                }
                text += char(c >= 32 && c < 127 ? c : ' ');
            }
            text += '\n';
        }
        if (dump_files) std::ofstream(out_dir + "/screen.txt") << text;
        return text;
    };

    auto pixel_edge = [&] {
        pixel_phase += 1485;
        if (pixel_phase >= 2000) {
            pixel_phase -= 2000;
            bool rising = !d.clk_pixel;
            d.clk_pixel = !d.clk_pixel;
            d.eval();
            if (rising && capturing_frame && !frame_complete) {
                if (d.video_request & (1u << 28)) {
                    frame_pixels = 0;
                    collecting = true;
                }
                if (collecting && (d.video_response & (1u << 24))) {
                    require(frame_pixels < 1280 * 720, "720p frame overflow", cycles);
                    const auto rgb = d.video_response;
                    for (unsigned c = 0; c < 3; ++c) {
                        frame[frame_pixels * 3 + c] = (rgb >> (16 - 8 * c)) & 255;
                    }
                    if (++frame_pixels == 1280 * 720) frame_complete = true;
                }
            }
        }
    };

    const uint64_t max_cycles = uint64_t(max_seconds * 100000000.0);
    auto step = [&] {
        d.clk = 0;
        d.eval();
        if (d.cold_reset || d.warm_reset || d.machine_reset) pending = false;
        else if (d.media_read_valid) {
            if (!pending) { pending = true; pending_address = d.media_read_addr; }
            require(d.media_read_addr == pending_address, "floppy byte address changed while held", cycles);
            require(pending_address < disk.size(), "floppy read beyond image", cycles);
        } else require(!pending, "floppy byte request withdrawn", cycles);
        d.eval();
        d.dq_sample = ram.tick(d);
        d.eval();
        pixel_edge();

        const bool media_taken = !d.cold_reset && !d.warm_reset && !d.machine_reset &&
                                 d.media_read_valid && d.media_read_ready;
        const unsigned media_data = d.media_read_data;
        const bool done = d.bus_done, wr = d.bus_write;
        const unsigned addr = d.bus_address, data = d.bus_data, kind = d.bus_kind;
        const bool consumed = d.instruction_consumed;
        const unsigned ip = d.instruction_address, flags = d.post_flags;
        const bool dma = d.dma2_done, irq = d.fdc_irq;

        d.clk = 1;
        d.eval();
        pixel_edge();
        ++cycles;

        if (d.audio_valid && d.audio_sample != last_sample) {
            ++speaker_edges;
            last_sample = d.audio_sample;
        }
        if (d.hercules_graphics_mode) saw_hgc_gfx = true;
        if (done && wr && kind == 0 && addr >= 0xb0000 && addr < 0xc0000) ++vram_writes;
        if (done && wr && kind == 1 && (addr & 0xffff) == 0x3d8 && (data & 2)) cga_gfx = true;

        if (media_taken) {
            require(media_data == disk[pending_address], "SDRAM floppy byte differs from image", cycles);
            pending = false;
            ++media_reads;
        }
        if (dma && !last_dma) ++dma2_completions;
        if (irq && !last_irq) ++irq6_edges;
        last_dma = dma;
        last_irq = irq;

        if (consumed) {
            if (ip == 0xfe47a) require(false, "BIOS reported POST error", cycles);
            if (ip == 0xfe49a && !post) {
                require(flags == 0, "BIOS POST flags", cycles);
                post = true;
                post_cycle = cycles;
            }
            if (ip == 0xfe6f2 && !boot) {
                boot = true;
                boot_cycle = cycles;
                require(word(0x41a) == word(0x41c), "BIOS keyboard buffer not empty after self-test", cycles);
                const unsigned expected_crtc = video_card == 0 ? 0x03d4u : 0x03b4u;
                require(word(0x463) == expected_crtc, "BDA 0040:0063 CRTC base mismatch for selected card", cycles);
                const unsigned expected_eq = video_card == 0 ? 0x20u : 0x30u;
                require((word(0x410) & 0x30u) == expected_eq, "BDA equipment video bits mismatch", cycles);
                const auto post_text = read_screen(false);
                require(post_text.find("GLaBIOS") != std::string::npos &&
                        post_text.find("640") != std::string::npos, "BIOS POST text missing", cycles);
                require(post_text.find("POST Error") == std::string::npos, "BIOS POST error text", cycles);
            }
            if (ip == 0x7c00 && !sector) {
                require(boot && media_reads >= 512 && dma2_completions >= 512, "boot sector skipped floppy/DMA", cycles);
                sector = true;
                sector_cycle = cycles;
            }
        }
        if (done && kind == 1 && !wr && (addr & 0x3ff) == 0x60) {
            if (data == 0xaa && !sector) ++bat_reads;
            else { ++key_reads; key_codes.push_back(data); }
        }
        if (cycles % 100000000ULL == 0) {
            std::cerr << "t=" << (cycles / 100000000ULL) << "s IP=" << std::hex << ip << std::dec
                      << " disk=" << media_reads << " vram_wr=" << vram_writes
                      << " hgc_gfx=" << d.hercules_graphics_mode << " audio_edges=" << speaker_edges << "\n";
        }
        require(d.fault == 0, "CPU/ISA/DMA fault", cycles);
        require(cycles <= max_cycles, "simulation time limit reached", cycles);
    };

    d.video_card = video_card;
    d.mono_tint = mono_tint;
    d.cold_reset = d.reset_pixel = 1;
    d.warm_reset = 0;
    d.hold = 0;
    d.firmware_valid = 0;
    d.media_gpo = 0;
    for (unsigned i = 0; i < 32; ++i) step();
    ram.cold_reset();
    d.cold_reset = d.reset_pixel = 0;
    while (!d.initialized) step();
    for (unsigned a = 0; a < 65536; ++a) {
        d.firmware_address = a;
        d.firmware_data = firmware[a];
        d.firmware_valid = 1;
        d.eval();
        while (!d.firmware_ready) step();
        step();
        d.firmware_valid = 0;
        while (!d.firmware_done) step();
    }

    unsigned toggle = 0;
    auto exchange = [&](unsigned op, unsigned index, unsigned arg) {
        const unsigned command = (op << 24) | (index << 16) | arg;
        d.media_gpo = (toggle << 31) | command;
        for (unsigned i = 0; i < 3; ++i) step();
        toggle ^= 1;
        d.media_gpo = (toggle << 31) | command;
        for (unsigned i = 0; i < 4000; ++i) {
            step();
            if (((d.media_gpi >> 23) & 1) == toggle) {
                require((d.media_gpi & 0xff000000u) == 0xf5000000u && !(d.media_gpi & (1u << 22)),
                        "runtime mailbox rejected command", cycles);
                return unsigned(d.media_gpi & 0xffff);
            }
        }
        require(false, "runtime mailbox timeout", cycles);
        return 0u;
    };

    uint32_t crc = 0xffffffffu;
    for (auto b : disk) {
        crc ^= b;
        for (unsigned i = 0; i < 8; ++i) crc = (crc & 1) ? (crc >> 1) ^ 0xedb88320u : crc >> 1;
    }
    crc ^= 0xffffffffu;
    exchange(6, 0, disk.size() & 0xffff);
    exchange(6, 1, disk.size() >> 16);
    exchange(6, 2, crc & 0xffff);
    exchange(6, 3, crc >> 16);
    for (unsigned offset = 0; offset < disk.size();) {
        const unsigned length = std::min<unsigned>(512, disk.size() - offset);
        exchange(7, 0, offset & 0xffff);
        exchange(7, 1, offset >> 16);
        exchange(7, 2, length);
        for (unsigned i = 0; i < length; i += 2) {
            const unsigned value = disk[offset + i] | (i + 1 < length ? unsigned(disk[offset + i + 1]) << 8 : 0);
            exchange(8, i / 2, value);
        }
        offset += length;
    }
    exchange(9, 0, 0);
    exchange(2, 0, 1);

    auto send_key = [&](unsigned usage, uint64_t hold_cycles) {
        const unsigned prior_make = key_codes.size();
        exchange(3, usage / 16, 1u << (usage % 16));
        const uint64_t dl_make = cycles + 20000000;
        while (key_codes.size() == prior_make && cycles < dl_make) step();
        require(key_codes.size() > prior_make, "key make did not reach XT PPI", cycles);
        for (uint64_t end = cycles + hold_cycles; cycles < end;) step();
        const unsigned prior_break = key_codes.size();
        exchange(3, usage / 16, 0);
        const uint64_t dl_break = cycles + 20000000;
        while (key_codes.size() == prior_break && cycles < dl_break) step();
        require(key_codes.size() > prior_break, "key break did not reach XT PPI", cycles);
    };

    auto capture_to = [&](const std::string& filename) {
        // Let the dynamic video pipeline settle across at least three 50 Hz guest frames before capture.
        for (uint64_t end = cycles + 6000000; cycles < end;) step();
        capturing_frame = true;
        collecting = false;
        frame_complete = false;
        frame_pixels = 0;
        while (!frame_complete) step();
        capturing_frame = false;
        std::ofstream pic(out_dir + "/" + filename, std::ios::binary);
        pic << "P6\n1280 720\n255\n";
        pic.write(reinterpret_cast<const char*>(frame.data()), frame.size());
        require(bool(pic), "failed to write PPM capture", cycles);
    };

    while (!sector) step();

    if (mode == "test-mda") {
        bool passed = false;
        while (!passed) {
            for (unsigned i = 0; i < 2000000; ++i) step();
            const auto text = read_screen(false);
            require(text.find("FES MDA FAIL") == std::string::npos, "MDATEST.COM reported FAIL", cycles);
            const auto pos = text.find("FES MDA PASS");
            if (pos != std::string::npos && text.find("A:\\>", pos) != std::string::npos) passed = true;
        }
        read_screen(true);
        capture_to("mda-pass.ppm");
        std::ofstream res(out_dir + "/result.json");
        res << "{\"mode\":\"test-mda\",\"video_card\":" << video_card << ",\"mono_tint\":" << mono_tint
            << ",\"post_cycle\":" << post_cycle << ",\"boot_cycle\":" << boot_cycle
            << ",\"sector_cycle\":" << sector_cycle << ",\"cycles\":" << cycles
            << ",\"vram_writes\":" << vram_writes << "}\n";
        std::cout << "PASS MDA DOS: GLaBIOS POST + FreeDOS + MDATEST.COM at " << cycles << " clocks\n";
        return 0;
    }

    if (mode == "test-hgc") {
        while (!d.hercules_graphics_mode) step();
        capture_to("hgc-page0.ppm");
        send_key(30u, 2000000); // '1' -> flip to Page 1
        capture_to("hgc-page1.ppm");
        send_key(31u, 2000000); // '2' -> restore text mode & exit
        while (d.hercules_graphics_mode) step();
        bool passed = false;
        while (!passed) {
            for (unsigned i = 0; i < 2000000; ++i) step();
            const auto text = read_screen(false);
            require(text.find("FES HGC FAIL") == std::string::npos, "HGCTEST.COM reported FAIL", cycles);
            const auto pos = text.find("FES HGC PASS");
            if (pos != std::string::npos && text.find("A:\\>", pos) != std::string::npos) passed = true;
        }
        read_screen(true);
        capture_to("hgc-text.ppm");
        std::ofstream res(out_dir + "/result.json");
        res << "{\"mode\":\"test-hgc\",\"video_card\":" << video_card << ",\"mono_tint\":" << mono_tint
            << ",\"post_cycle\":" << post_cycle << ",\"boot_cycle\":" << boot_cycle
            << ",\"sector_cycle\":" << sector_cycle << ",\"cycles\":" << cycles
            << ",\"vram_writes\":" << vram_writes << "}\n";
        std::cout << "PASS HGC DOS: GLaBIOS POST + FreeDOS + HGCTEST.COM (3BAh vsync, Page 0/1, text restore) at "
                  << cycles << " clocks\n";
        return 0;
    }

    // Generic game / application runner ("run" mode).
    std::vector<std::string> captured_files;
    if (!keys_spec.empty()) {
        std::stringstream ss(keys_spec);
        std::string item;
        unsigned snap_idx = 0;
        while (std::getline(ss, item, ',')) {
            if (item.empty()) continue;
            if (item.rfind("wait:", 0) == 0) {
                std::string arg = item.substr(5);
                double ms = 0.0;
                if (arg.size() > 2 && arg.substr(arg.size() - 2) == "ms") ms = std::stod(arg.substr(0, arg.size() - 2));
                else if (arg.back() == 's') ms = std::stod(arg.substr(0, arg.size() - 1)) * 1000.0;
                else ms = std::stod(arg);
                uint64_t wait_cycles = uint64_t(ms * 100000.0);
                for (uint64_t end = cycles + wait_cycles; cycles < end;) step();
            } else if (item.rfind("snap:", 0) == 0) {
                std::string name = item.substr(5) + ".ppm";
                capture_to(name);
                captured_files.push_back(name);
            } else if (item == "snap") {
                std::string name = "snap-" + std::to_string(snap_idx++) + ".ppm";
                capture_to(name);
                captured_files.push_back(name);
            } else {
                auto colon = item.find(':');
                std::string keyname = colon == std::string::npos ? item : item.substr(0, colon);
                double hold_ms = 120.0;
                if (colon != std::string::npos) {
                    std::string dur = item.substr(colon + 1);
                    if (dur.size() > 2 && dur.substr(dur.size() - 2) == "ms") hold_ms = std::stod(dur.substr(0, dur.size() - 2));
                    else hold_ms = std::stod(dur);
                }
                send_key(hid_usage_for_name(keyname), uint64_t(hold_ms * 100000.0));
            }
        }
    } else {
        // Default run: run up to max_seconds - 1s, capture periodic snapshots every 5s.
        uint64_t interval = 500000000ULL;
        uint64_t target_end = max_cycles > 500000000ULL ? max_cycles - 500000000ULL : max_cycles;
        unsigned snap_idx = 0;
        while (cycles + interval < target_end) {
            for (uint64_t end = cycles + interval; cycles < end;) step();
            std::string name = "snap-" + std::to_string(snap_idx++) + ".ppm";
            capture_to(name);
            captured_files.push_back(name);
        }
    }
    read_screen(true);
    capture_to("final.ppm");
    captured_files.push_back("final.ppm");

    std::ofstream summary(out_dir + "/run-summary.json");
    summary << "{\"mode\":\"run\",\"video_card\":" << video_card << ",\"mono_tint\":" << mono_tint
            << ",\"cycles\":" << cycles << ",\"simulated_seconds\":" << (double(cycles) / 100000000.0)
            << ",\"vram_writes\":" << vram_writes << ",\"saw_hercules_graphics\":" << (saw_hgc_gfx ? "true" : "false")
            << ",\"saw_cga_graphics\":" << (cga_gfx ? "true" : "false")
            << ",\"speaker_edges\":" << speaker_edges << ",\"captures\":[";
    for (size_t i = 0; i < captured_files.size(); ++i) {
        if (i) summary << ",";
        summary << "\"" << captured_files[i] << "\"";
    }
    summary << "]}\n";
    return 0;
}
