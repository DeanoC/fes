// SPDX-License-Identifier: GPL-2.0-or-later
// Apple II machine simulation: boots the open diagnostic firmware, drives the
// keyboard through its command screens, boots the synthetic Disk II image and
// captures complete HDMI frames. Frames are written as PPM files and compared
// byte for byte with the independently rendered references when present.
#include "Vapple2_sim_top.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <iterator>
#include <memory>
#include <string>
#include <vector>

namespace {

constexpr uint64_t kSysHalfPs = 9574;     // 52.224 MHz
constexpr uint64_t kPixHalfPs = 6734;     // 74.25 MHz
constexpr int kWidth = 1280;
constexpr int kHeight = 720;

struct Bench {
    Vapple2_sim_top top;
    uint64_t now = 0;
    uint64_t next_sys = 0;
    uint64_t next_pix = 0;
    bool video_on = true;
    uint64_t cpu_cycles = 0;
    int result = -1;
    int stage = -1;
    int speaker_toggles = 0;
    int last_speaker = 0;
    std::vector<uint8_t> frame;
    bool capturing = false;
    bool captured = false;
    int cap_x = 0;
    int cap_y = 0;
    bool seen_vsync = false;
    int last_vsync = 0;
    int last_de = 0;

    void step() {
        if (!video_on) next_pix = next_sys + 1;
        if (next_sys <= next_pix) {
            now = next_sys;
            top.clk_sys = !top.clk_sys;
            next_sys += kSysHalfPs;
            top.eval();
            if (top.clk_sys) on_sys_edge();
        } else {
            now = next_pix;
            top.pixel_clk = !top.pixel_clk;
            next_pix += kPixHalfPs;
            top.eval();
            if (top.pixel_clk) on_pix_edge();
        }
    }

    void on_sys_edge() {
        if (top.bus_cycle) {
            cpu_cycles++;
            if (top.bus_write && top.bus_addr == 0x03F0) result = top.bus_data;
            if (top.bus_write && top.bus_addr == 0x03F1) stage = top.bus_data;
        }
        if (top.speaker != last_speaker) {
            speaker_toggles++;
            last_speaker = top.speaker;
        }
    }

    void on_pix_edge() {
        if (!capturing) return;
        if (top.vsync && !last_vsync) {
            if (!seen_vsync) {
                seen_vsync = true;
                cap_y = -1;
            } else if (cap_y >= kHeight - 1) {
                capturing = false;
                captured = true;
            }
        }
        last_vsync = top.vsync;
        if (seen_vsync && top.de) {
            if (!last_de) {
                cap_y++;
                cap_x = 0;
            }
            if (cap_y >= 0 && cap_y < kHeight && cap_x < kWidth) {
                size_t o = (static_cast<size_t>(cap_y) * kWidth + cap_x) * 3;
                frame[o] = top.red;
                frame[o + 1] = top.green;
                frame[o + 2] = top.blue;
            }
            cap_x++;
        }
        last_de = top.de;
    }

    void run_ps(uint64_t ps) {
        uint64_t end = now + ps;
        while (now < end) step();
    }

    template <typename Pred>
    bool run_until(const char* what, uint64_t timeout_ps, Pred pred) {
        uint64_t end = now + timeout_ps;
        while (now < end) {
            step();
            if (pred()) return true;
        }
        std::fprintf(stderr, "timeout waiting for %s (result=%02x stage=%02x cycles=%llu qt=%d)\n",
                     what, result & 0xff, stage & 0xff,
                     static_cast<unsigned long long>(cpu_cycles), top.quarter_track);
        return false;
    }

    void key(uint8_t code) {
        // Hold the event for one system clock.
        while (!top.clk_sys) step();
        top.key_code = code & 0x7f;
        top.key_event = 1;
        while (top.clk_sys) step();
        while (!top.clk_sys) step();
        top.key_event = 0;
    }

    bool capture(const std::string& path) {
        frame.assign(static_cast<size_t>(kWidth) * kHeight * 3, 0);
        capturing = true;
        captured = false;
        seen_vsync = false;
        last_vsync = top.vsync;
        if (!run_until("frame", 60'000'000'000ull, [&] { return captured; })) return false;
        std::ofstream out(path, std::ios::binary);
        out << "P6\n" << kWidth << " " << kHeight << "\n255\n";
        out.write(reinterpret_cast<const char*>(frame.data()), static_cast<std::streamsize>(frame.size()));
        return true;
    }
};

std::vector<char> slurp(const std::string& path) {
    std::ifstream in(path, std::ios::binary);
    return std::vector<char>((std::istreambuf_iterator<char>(in)), {});
}

// A frame matches its reference, or the flashing-phase reference when one exists.
bool compare(const std::string& got, const std::string& want, bool required) {
    std::vector<char> frame = slurp(got);
    std::vector<char> normal = slurp(want + ".ppm");
    std::vector<char> flashing = slurp(want + "-flash.ppm");
    if (normal.empty()) {
        if (required) std::fprintf(stderr, "missing reference %s.ppm\n", want.c_str());
        return !required;
    }
    if (frame == normal || (!flashing.empty() && frame == flashing)) return true;
    size_t n = std::min(frame.size(), normal.size()), first = 0;
    while (first < n && frame[first] == normal[first]) first++;
    size_t pixel = first > 15 ? (first - 15) / 3 : 0;
    std::fprintf(stderr, "%s differs from %s.ppm at pixel (%zu,%zu)\n", got.c_str(), want.c_str(),
                 pixel % 1280, pixel / 1280);
    return false;
}

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    std::string out_dir = "build/sim/fes-apple2-machine";
    std::string ref_dir = "build/diagnostics/fes-apple2";
    bool require_refs = false;
    bool disk = true;
    for (int i = 1; i < argc; ++i) {
        if (!std::strcmp(argv[i], "--out") && i + 1 < argc) out_dir = argv[++i];
        else if (!std::strcmp(argv[i], "--refs") && i + 1 < argc) ref_dir = argv[++i];
        else if (!std::strcmp(argv[i], "--require-refs")) require_refs = true;
        else if (!std::strcmp(argv[i], "--no-disk")) disk = false;
    }

    auto bench = std::make_unique<Bench>();
    Bench& b = *bench;
    b.top.clk_sys = 0;
    b.top.pixel_clk = 0;
    b.top.reset = 1;
    b.top.reset_key = 0;
    b.top.key_event = 0;
    b.top.key_code = 0;
    b.top.disk_present = 1;
    b.top.eval();
    b.run_ps(2'000'000);
    b.top.reset = 0;

    bool ok = true;
    auto check = [&](const char* name) {
        std::string got = out_dir + "/" + name + ".ppm";
        if (!b.capture(got)) return false;
        return compare(got, ref_dir + "/" + name, require_refs);
    };

    // Self tests: RAM and language card, then the banner screen.
    b.video_on = false;
    if (!b.run_until("self tests", 2'000'000'000'000ull,
                     [&] { return b.result == 0x11 || b.result >= 0xE0; })) return 1;
    if (b.result != 0x11) {
        std::fprintf(stderr, "self test failed: result %02x\n", b.result);
        return 1;
    }
    std::printf("self tests passed after %llu CPU cycles\n",
                static_cast<unsigned long long>(b.cpu_cycles));
    b.video_on = true;
    ok = check("text") && ok;

    struct Screen { char key; const char* name; };
    const Screen screens[] = {{'L', "lores"}, {'H', "hires"}, {'M', "mixed"}, {'T', "text2"}};
    for (const Screen& s : screens) {
        uint8_t code = static_cast<uint8_t>(s.key) | 0x80;
        b.stage = -1;
        b.video_on = false;
        b.key(code);
        if (!b.run_until(s.name, 2'000'000'000'000ull, [&] { return b.stage == code; })) return 1;
        b.video_on = true;
        ok = check(s.name) && ok;
        std::printf("%s screen captured\n", s.name);
    }

    // Keyboard echo and speaker clicks.
    int clicks = b.speaker_toggles;
    b.stage = -1;
    b.video_on = false;
    b.key('A' | 0x80);
    if (!b.run_until("echo", 200'000'000'000ull, [&] { return b.stage == ('A' | 0x80); })) return 1;
    if (b.speaker_toggles - clicks != 2) {
        std::fprintf(stderr, "expected two speaker toggles, saw %d\n", b.speaker_toggles - clicks);
        ok = false;
    }

    if (disk) {
        b.result = -1;
        b.key('B' | 0x80);
        if (!b.run_until("disk boot", 12'000'000'000'000ull,
                         [&] { return b.result == 0xA5 || (b.result >= 0xE0 && b.result <= 0xEF); }))
            return 1;
        if (b.result != 0xA5) {
            std::fprintf(stderr, "disk boot failed: result %02x stage %02x\n", b.result, b.stage);
            return 1;
        }
        std::printf("disk boot verified after %llu CPU cycles (quarter track %d)\n",
                    static_cast<unsigned long long>(b.cpu_cycles), b.top.quarter_track);
        b.video_on = true;
        ok = check("disk") && ok;
    }

    std::printf("%s\n", ok ? "PASS" : "FAIL");
    return ok ? 0 : 1;
}
