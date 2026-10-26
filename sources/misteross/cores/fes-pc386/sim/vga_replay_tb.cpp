// SPDX-License-Identifier: MIT
// VGA trace replay (docs/pc386-machine-design.md, "Verification", item 3).
//
// Replays a machine_tb --trace file into the RTL VGA (vga_replay_top: the
// machine's I/O decoder and VGA, the same DDR port models) at the recorded
// clocks, and into the reference VGA (Bochs's, vga_reference.h) in the same
// order. It checks:
//   - every read the RTL answers equals the recorded answer (the replay is
//     the machine's run), and equals the reference's, except input status 1,
//     whose retrace timing is the RTL's own;
//   - VRAM equals the reference's after every stable frame and at the end;
//   - every stable captured frame equals the reference's display of it. A
//     frame is stable when no VGA write landed between the start-address latch
//     before it and the end of its capture. The reference renders it at the
//     RTL's frame start, with its start address latched when the RTL latched
//     it. 256-colour and dot-clock/2 frames are half the reference's width
//     (Bochs doubles those pixels). Matches are counted by size and mode,
//     and by the picture marker (machine_tb's "M" lines) they follow until
//     the next write.
// Frames are not compared where the reference cannot show the RTL's picture,
// each counted by name (sim/vga_divergences.toml lists every divergence with
// its deciding case): text frames whose cursor or character blink phase
// differs between the two (frames-blink-phase), and text frames before the
// CRTC is programmed, which Bochs draws as a default 80 x 25 screen
// (frames-divergence-reference-unprogrammed). Any other difference fails.
//
// Prints REPLAY, READ-MISMATCH, FRAME, VRAM-MISMATCH and SUMMARY lines; exit 0
// when everything matched.
// usage: vga_replay_tb --trace F [--frames DIR] [--max-mismatch-dumps N]
#include "Vvga_replay_top.h"
#include "pc386_ddr_model.h"
#include "vga_reference.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <fstream>
#include <map>
#include <sstream>
#include <stdexcept>
#include <string>
#include <vector>

namespace {

struct Event {
    char kind;          // I/O: I O; MMIO: R W; M a picture marker
    uint64_t cycle;
    uint32_t address;   // port, or offset of the dword in A0000h-BFFFFh
    unsigned size;      // I/O bytes; MMIO byte enables
    uint32_t value;
    std::string name;   // the marker's picture
};

std::vector<Event> load(const std::string &path) {
    std::ifstream input(path);
    if (!input) throw std::runtime_error("cannot read " + path);
    std::vector<Event> events;
    std::string line;
    while (std::getline(input, line)) {
        if (line.empty() || line[0] == '#') continue;
        std::istringstream fields(line);
        Event event{};
        fields >> event.kind >> event.cycle;
        if (event.kind == 'M') fields >> event.name;
        else fields >> std::hex >> event.address >> event.size >> event.value;
        if (!fields) throw std::runtime_error("bad trace line: " + line);
        events.push_back(event);
    }
    return events;
}

class Bench {
public:
    Bench() {
        top.reset = 1;
        for (int n = 0; n < 4; ++n) clock();
        top.reset = 0;
    }
    Vvga_replay_top top;
    pc386::Window window;
    pc386::Port ddr1{window, 0x5eed0001, "port 1"};
    pc386::Port ddr2{window, 0x5eed0002, "port 2"};
    uint64_t cycles = 0;
    uint8_t p2_read = 0, p2_readdatavalid = 0;
    uint64_t p2_readdata = 0;
    uint8_t p2_byteenable = 0xff;
    // Frame events from the last clock.
    bool frame_start = false, frame_done = false, start_latched = false;

    void clock() {
        pc386::PortSignals one{&top.p1_address, &top.p1_burstcount, &top.p1_read, &top.p1_write, &top.p1_writedata,
                               &top.p1_byteenable, &top.p1_waitrequest, &top.p1_readdata, &top.p1_readdatavalid};
        pc386::PortSignals two{&top.p2_address, &top.p2_burstcount, &p2_read, &top.p2_write, &top.p2_writedata,
                               &p2_byteenable, &top.p2_waitrequest, &p2_readdata, &p2_readdatavalid};
        ddr1.drive(one, cycles);
        ddr2.drive(two, cycles);
        top.clk = 1;
        top.eval();
        top.clk = 0;
        top.eval();
        ++cycles;
        frame_start = top.frame_start;
        frame_done = top.frame_done;
        start_latched = top.start_latched;
    }
    pc386::Frame captured() {
        pc386::Frame frame;
        frame.width = top.frame_width;
        frame.height = top.frame_height;
        frame.pixels.resize(size_t(frame.width) * frame.height);
        uint32_t base = pc386::CAPTURE_OFFSET + top.frame_buffer * pc386::CAPTURE_BYTES;
        for (unsigned y = 0; y < frame.height; ++y)
            for (unsigned x = 0; x < frame.width; ++x) {
                uint32_t value;
                std::memcpy(&value, window.at(base + y * pc386::CAPTURE_STRIDE + 4 * x), 4);
                frame.pixels[size_t(y) * frame.width + x] = value & 0xffffff;
            }
        return frame;
    }
};

struct Options {
    std::string trace, frames;
    unsigned dumps = 4;
};

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    std::setvbuf(stdout, nullptr, _IOLBF, 0);
    try {
        Options options;
        for (int n = 1; n + 1 < argc; n += 2) {
            std::string flag = argv[n], value = argv[n + 1];
            if (flag == "--trace") options.trace = value;
            else if (flag == "--frames") options.frames = value;
            else if (flag == "--max-mismatch-dumps") options.dumps = unsigned(std::stoul(value));
            else throw std::runtime_error("unknown option " + flag);
        }
        if (options.trace.empty()) throw std::runtime_error("--trace is required");
        std::vector<Event> events = load(options.trace);
        Bench bench;
        auto reference = pc386::make_vga_reference();
        pc386::Frame reference_frame;
        bool reference_ok = false, reference_text = false, reference_blink = false, reference_unprogrammed = false;
        bool reference_visible = false;
        reference->render(reference_frame);   // frame 0: the cursor blink phase starts with the RTL's
        uint64_t last_write = 0, last_latch = 0, window_start = 0;
        bool any_write = false;
        unsigned frame_index = 0;
        std::string marker;   // the last picture marker, while its picture holds
        std::map<std::string, unsigned> counts;
        unsigned dumps = 0;
        unsigned long long read_mismatches = 0, replay_mismatches = 0, vram_mismatches = 0, frame_mismatches = 0;

        auto observe = [&]() {
            if (bench.start_latched) { reference->latch_start(); last_latch = bench.cycles; }
            if (bench.frame_start) {
                frame_index = bench.top.frame_count;
                window_start = last_latch;
                reference_ok = reference->render(reference_frame);
                reference_text = reference->text_mode();
                reference_blink = reference->blink_enabled();
                reference_unprogrammed = reference->text_mode() && reference->unprogrammed();
                reference_visible = reference->blink_visible();
            }
            if (!bench.frame_done) return;
            ++counts["frames"];
            bool stable = !any_write || last_write < window_start;
            if (!stable) { ++counts["frames-unstable"]; return; }
            if (!reference_ok) { ++counts["frames-reference-skipped"]; return; }
            if (reference_unprogrammed) { ++counts["frames-divergence-reference-unprogrammed"]; return; }
            // The RTL blinks the cursor every 8 frames and characters every 16;
            // Bochs both every 8 updates, from its own phase.
            bool cursor_on = !(frame_index & 8), characters_on = !(frame_index & 16);
            if (reference_text && (cursor_on != reference_visible || (reference_blink && characters_on != reference_visible))) {
                ++counts["frames-blink-phase"];
                return;
            }
            pc386::Frame rtl = bench.captured();
            pc386::Frame ref = reference_frame;
            if (ref.width == 2 * rtl.width) {
                pc386::Frame half;
                half.width = rtl.width; half.height = ref.height; half.pixels.resize(size_t(half.width) * half.height);
                for (unsigned y = 0; y < ref.height; ++y)
                    for (unsigned x = 0; x < half.width; ++x) half.pixels[size_t(y) * half.width + x] = ref.at(2 * x, y);
                ref = half;
            }
            ++counts["frames-compared"];
            unsigned differing = 0, first_x = 0, first_y = 0;
            if (ref.width != rtl.width || ref.height != rtl.height) differing = ~0u;
            else
                for (unsigned y = 0; y < rtl.height; ++y)
                    for (unsigned x = 0; x < rtl.width; ++x)
                        if (rtl.at(x, y) != ref.at(x, y) && differing++ == 0) { first_x = x; first_y = y; }
            if (differing) {
                ++frame_mismatches;
                if (differing == ~0u)
                    std::printf("FRAME %u size rtl=%ux%u reference=%ux%u cycle=%llu\n", frame_index, rtl.width,
                                rtl.height, ref.width, ref.height, (unsigned long long)bench.cycles);
                else
                    std::printf("FRAME %u mismatch pixels=%u first=(%u,%u) rtl=%06x reference=%06x cycle=%llu\n",
                                frame_index, differing, first_x, first_y, rtl.at(first_x, first_y),
                                ref.at(first_x, first_y), (unsigned long long)bench.cycles);
                if (!options.frames.empty() && dumps < options.dumps) {
                    ++dumps;
                    pc386::write_ppm(options.frames + "/mismatch-" + std::to_string(frame_index) + "-rtl.ppm", rtl);
                    pc386::write_ppm(options.frames + "/mismatch-" + std::to_string(frame_index) + "-reference.ppm",
                                     reference_frame);
                }
            } else {
                ++counts["frames-matched"];
                ++counts["frames-matched-" + std::to_string(rtl.width) + "x" + std::to_string(rtl.height) +
                         (reference_text ? "-text" : "-graphics")];
                if (!marker.empty()) ++counts["frames-matched-at-" + marker];
            }
            // VRAM: no write since before this frame, so the coalescer has drained.
            const uint8_t *expected = reference->vram();
            unsigned bad = 0;
            for (uint32_t n = 0; n < 0x40000; ++n)
                if (bench.window.byte(pc386::VRAM_OFFSET + n) != expected[n] && bad++ < 4)
                    std::printf("VRAM-MISMATCH frame=%u offset=%04x plane=%u rtl=%02x reference=%02x\n", frame_index,
                                n >> 2, n & 3, bench.window.byte(pc386::VRAM_OFFSET + n), expected[n]);
            if (bad) ++vram_mismatches;
        };
        auto advance_to = [&](uint64_t cycle) {
            if (bench.cycles > cycle) throw std::runtime_error("replay fell behind the trace at cycle " + std::to_string(cycle));
            while (bench.cycles < cycle) { bench.clock(); observe(); }
        };
        auto read_check = [&](const char *what, uint32_t address, uint8_t rtl, uint8_t ref) {
            if (rtl == ref) return;
            ++read_mismatches;
            if (read_mismatches <= 20)
                std::printf("READ-MISMATCH %s %05x rtl=%02x reference=%02x cycle=%llu\n", what, address, rtl, ref,
                            (unsigned long long)bench.cycles);
        };

        for (const Event &event : events) {
            advance_to(event.cycle);
            if (event.kind == 'M') { marker = event.name; continue; }
            bool write = event.kind == 'O' || event.kind == 'W';
            if (event.kind == 'I' || event.kind == 'O') {
                while (bench.top.io_busy) { bench.clock(); observe(); }
                bench.top.io_valid = 1;
                bench.top.io_write = write;
                bench.top.io_port = uint16_t(event.address);
                bench.top.io_size = event.size == 1 ? 0 : event.size == 2 ? 1 : 2;
                bench.top.io_wdata = event.value;
                bench.clock(); observe();
                bench.top.io_valid = 0;
                while (!bench.top.io_done) { bench.clock(); observe(); }
                uint32_t mask = event.size == 4 ? ~0u : (1u << (8 * event.size)) - 1;
                if (!write && (bench.top.io_rdata & mask) != (event.value & mask)) ++replay_mismatches;
                for (unsigned k = 0; k < event.size; ++k) {
                    uint16_t port = uint16_t(event.address + k);
                    uint8_t byte = uint8_t(event.value >> (8 * k));
                    if (write) {
                        if (pc386::VgaReference::claims(port)) reference->out(port, byte);
                    } else {
                        uint8_t rtl = uint8_t(bench.top.io_rdata >> (8 * k));
                        uint8_t ref = pc386::VgaReference::claims(port) ? reference->in(port) : 0xff;
                        if (port != 0x3ba && port != 0x3da) read_check("port", port, rtl, ref);
                    }
                }
            } else {
                bench.top.mem_valid = 1;
                bench.top.mem_write = write;
                bench.top.mem_address = event.address >> 2;
                bench.top.mem_byteenable = event.size;
                bench.top.mem_wdata = event.value;
                for (;;) {
                    bench.top.eval();
                    bool taken = bench.top.mem_ready;
                    bench.clock(); observe();
                    if (taken) break;
                }
                bench.top.mem_valid = 0;
                if (!write) {
                    while (!bench.top.mem_rvalid) { bench.clock(); observe(); }
                    if (bench.top.mem_rdata != event.value) ++replay_mismatches;
                }
                for (unsigned k = 0; k < 4; ++k) {
                    if (!(event.size & (1u << k))) continue;
                    uint32_t physical = 0xa0000 + event.address + k;
                    if (write) reference->mem_write(physical, uint8_t(event.value >> (8 * k)));
                    else read_check("memory", physical, uint8_t(bench.top.mem_rdata >> (8 * k)), reference->mem_read(physical));
                }
            }
            if (write) { last_write = bench.cycles; any_write = true; marker.clear(); }
            ++counts[std::string("events-") + event.kind];
        }
        // Let the coalescer drain and the last frames finish.
        uint64_t end = bench.cycles + 4000000;
        while (bench.cycles < end) { bench.clock(); observe(); }
        const uint8_t *expected = reference->vram();
        unsigned bad = 0;
        for (uint32_t n = 0; n < 0x40000; ++n)
            if (bench.window.byte(pc386::VRAM_OFFSET + n) != expected[n] && bad++ < 8)
                std::printf("VRAM-MISMATCH end offset=%04x plane=%u rtl=%02x reference=%02x\n", n >> 2, n & 3,
                            bench.window.byte(pc386::VRAM_OFFSET + n), expected[n]);
        if (bad) ++vram_mismatches;
        for (const auto &[name, count] : counts) std::printf("REPLAY %s %u\n", name.c_str(), count);
        std::printf("REPLAY scanout_underflows %u\nREPLAY capture_drops %u\n", bench.top.scanout_underflows,
                    bench.top.capture_drops);
        bool ok = !read_mismatches && !replay_mismatches && !vram_mismatches && !frame_mismatches;
        std::printf("SUMMARY %s events=%zu replay_mismatches=%llu read_mismatches=%llu vram_mismatches=%llu "
                    "frame_mismatches=%llu end_vram_bytes_differing=%u\n",
                    ok ? "match" : "mismatch", events.size(), replay_mismatches, read_mismatches, vram_mismatches,
                    frame_mismatches, bad);
        return ok ? 0 : 1;
    } catch (const std::exception &error) {
        std::fprintf(stderr, "vga_replay_tb: %s\n", error.what());
        return 2;
    }
}
