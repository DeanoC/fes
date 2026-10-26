// SPDX-License-Identifier: MIT
// pc386 presenter bench: rtl/pc386_presenter.sv against pc386_presenter_model.h.
//
// Each case writes a capture of one size into a capture buffer of the DDR
// model, publishes it as the VGA does (buffer, width, height, then the frame
// sequence), and compares the second complete 720p frame after that with the
// model's scaling, pixel for pixel, including the black bars. The pixels are
// 6-bit DAC values widened by replication, as captures are. The last case
// publishes a new buffer while the old one shows, to check the switch. The
// pixel clock runs at 74.25 MHz against a 100 MHz system clock; DDR port 2 is
// pc386_ddr_model.h.
//
// Prints PASS/FAIL per case; exit 0 when every case passes.
#include "Vpc386_presenter.h"
#include "pc386_ddr_model.h"
#include "pc386_presenter_model.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <string>
#include <vector>

namespace {

constexpr uint64_t SYS_PERIOD_PS = 10000;
constexpr uint64_t PIXEL_HALF_PS = 6734;

class Bench {
public:
    Bench() {
        top.reset = 1;
        top.pixel_reset = 1;
        for (int n = 0; n < 8; ++n) step();
        top.reset = 0;
        top.pixel_reset = 0;
    }
    Vpc386_presenter top;
    pc386::Window window;
    pc386::Port ddr{window, 0x5eed0002, "port 2"};
    uint8_t write = 0;
    uint64_t writedata = 0;
    uint8_t byteenable = 0;
    uint64_t now = 0, pixel_edge = 0, cycles = 0;
    pc386::Frame raster{1280, 720, std::vector<uint32_t>(1280 * 720)};
    pc386::Frame last;
    unsigned x = 0, y = 0, complete = 0;
    bool started = false, last_de = false, last_vs = false;

    void step() {
        pc386::PortSignals port{&top.ddr_address, &top.ddr_burstcount, &top.ddr_read, &write, &writedata,
                                &byteenable, &top.ddr_waitrequest, &top.ddr_readdata, &top.ddr_readdatavalid};
        ddr.drive(port, cycles);
        now += SYS_PERIOD_PS;
        while (pixel_edge <= now) {
            top.pixel_clk = !top.pixel_clk;
            top.eval();
            if (top.pixel_clk) observe();
            pixel_edge += PIXEL_HALF_PS;
        }
        top.clk = 1;
        top.eval();
        top.clk = 0;
        top.eval();
        ++cycles;
    }
    void observe() {
        uint32_t word = top.video;
        bool de = word & (1u << 24), vs = word & (1u << 26);
        if (vs && !last_vs) {
            if (started && y == 720) { last = raster; ++complete; }
            started = true;
            y = 0;
        }
        if (de && !last_de) x = 0;
        if (de && started && y < 720 && x < 1280) raster.pixels[y * 1280 + x] = word & 0xffffff;
        if (de) ++x;
        if (!de && last_de) ++y;
        last_de = de;
        last_vs = vs;
    }
    void publish(unsigned buffer, const pc386::Frame &frame) {
        uint32_t base = pc386::CAPTURE_OFFSET + buffer * pc386::CAPTURE_BYTES;
        for (unsigned row = 0; row < frame.height; ++row)
            for (unsigned column = 0; column < frame.width; ++column) {
                uint32_t value = frame.at(column, row);
                std::memcpy(window.at(base + row * pc386::CAPTURE_STRIDE + 4 * column), &value, 4);
            }
        top.frame_buffer = buffer;
        top.frame_width = frame.width;
        top.frame_height = frame.height;
        step();
        top.frame_sequence = uint16_t(top.frame_sequence + 1);
    }
    bool wait_frames(unsigned count) {
        unsigned target = complete + count;
        for (uint64_t limit = cycles + uint64_t(count + 2) * 1700000; cycles < limit;) {
            step();
            if (complete >= target) return true;
        }
        return false;
    }
};

// A test card: bands, a checkerboard, single-pixel lines and a gradient, so
// every blend weight meets an edge.
pc386::Frame card(unsigned width, unsigned height, unsigned seed) {
    pc386::Frame frame{width, height, std::vector<uint32_t>(size_t(width) * height)};
    pc386::Random random(seed);
    auto widen = [](unsigned v) { v &= 63; return (v << 2) | (v >> 4); };
    for (unsigned y = 0; y < height; ++y)
        for (unsigned x = 0; x < width; ++x) {
            unsigned r, g, b;
            if (y < height / 4) { r = (x * 64) / width; g = (y * 64) / (height / 4 + 1); b = 63 - r; }
            else if (y < height / 2) { bool on = ((x / 3) ^ (y / 3)) & 1; r = g = b = on ? 63 : 0; }
            else if (y < 3 * height / 4) { r = x % 7 == 0 ? 63 : 0; g = y % 5 == 0 ? 63 : 0; b = 20; }
            else { unsigned v = random.next(); r = v; g = v >> 6; b = v >> 12; }
            frame.pixels[size_t(y) * width + x] = widen(r) << 16 | widen(g) << 8 | widen(b);
        }
    return frame;
}

bool check(Bench &bench, const pc386::Frame &source, const std::string &name) {
    if (!bench.wait_frames(2)) {
        std::printf("FAIL %s: no 720p frame\n", name.c_str());
        return false;
    }
    pc386::Frame expected = pc386::present_720p(source);
    unsigned differing = 0, first = 0;
    for (size_t n = 0; n < expected.pixels.size(); ++n)
        if (expected.pixels[n] != bench.last.pixels[n] && differing++ == 0) first = unsigned(n);
    if (differing) {
        std::printf("FAIL %s: %u pixels differ, first (%u,%u) rtl %06x model %06x\n", name.c_str(), differing,
                    first % 1280, first / 1280, bench.last.pixels[first], expected.pixels[first]);
        pc386::write_ppm("presenter-" + name + "-rtl.ppm", bench.last);
        pc386::write_ppm("presenter-" + name + "-model.ppm", expected);
        return false;
    }
    std::printf("PASS %s %ux%u\n", name.c_str(), source.width, source.height);
    return true;
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    std::setvbuf(stdout, nullptr, _IOLBF, 0);
    Bench bench;
    bool ok = true;
    const unsigned sizes[][2] = {{720, 400}, {640, 400}, {320, 400}, {640, 480}, {320, 200}, {360, 480},
                                 {800, 600}, {640, 350}};
    unsigned buffer = 0, seed = 1;
    for (const auto &size : sizes) {
        pc386::Frame source = card(size[0], size[1], seed++);
        bench.publish(buffer, source);
        ok &= check(bench, source, std::to_string(size[0]) + "x" + std::to_string(size[1]));
        buffer = (buffer + 1) % 3;
    }
    // A new buffer while one shows: the next frames show it.
    pc386::Frame next = card(320, 400, 99);
    bench.publish(buffer, next);
    ok &= check(bench, next, "switch");
    std::printf("PRESENTER underflows=%u frames=%u\n", bench.top.underflows, bench.top.frames_shown);
    if (bench.top.underflows) { std::printf("FAIL underflows\n"); ok = false; }
    return ok ? 0 : 1;
}
