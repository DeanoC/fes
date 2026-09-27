// SPDX-License-Identifier: GPL-2.0-or-later
// Board-level Apple II simulation through the real fes.computer mailbox.
// The testbench plays the host: it discovers identity, releases execution,
// types through USB HID key rows, inserts the synthetic disk into media unit 0
// while the machine runs, boots it, checks keyboard translation, audio,
// Control-F12 reset and a live eject, and writes the final HDMI frame.
#include "Vtop.h"
#include "Vtop___024root.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <memory>
#include <string>
#include <vector>

namespace {

constexpr uint64_t kSysHalfPs = 9574;     // 52.224 MHz
constexpr uint64_t kPixHalfPs = 6734;     // 74.25 MHz
constexpr uint64_t kAudioHalfPs = 40690;  // 12.288 MHz (model: refclk is MCLK)
constexpr int kWidth = 1280;
constexpr int kHeight = 720;

[[noreturn]] void fail(const std::string& message) {
    std::fprintf(stderr, "FES Apple II board: %s\n", message.c_str());
    std::exit(EXIT_FAILURE);
}

struct Board {
    Vtop dut;
    Vtop___024root& root;
    uint64_t now = 0, next_sys = 0, next_pix = 0, next_audio = 0;
    bool video = true;
    bool toggle = false;
    int result = -1, stage = -1;
    uint64_t cpu_cycles = 0;
    int speaker_edges = 0, last_speaker = 0;
    int32_t peak_sample = 0;
    std::vector<uint8_t> frame;
    bool capturing = false, captured = false, seen_vsync = false;
    int cap_x = 0, cap_y = 0, last_vsync = 0, last_de = 0;

    Board() : root(*dut.rootp) {
        dut.FPGA_CLK1_50 = 0;
        root.top__DOT__system_clock__DOT__outclk_0 = 0;
        root.top__DOT__video_clock__DOT__outclk_0 = 0;
        root.top__DOT__hps_gp__DOT__gp_out = 0;
        dut.eval();
    }

    void step() {
        if (!video) next_pix = UINT64_MAX;
        uint64_t t = std::min(next_sys, std::min(next_pix, next_audio));
        now = t;
        if (t == next_sys) {
            next_sys += kSysHalfPs;
            uint8_t level = !root.top__DOT__system_clock__DOT__outclk_0;
            root.top__DOT__system_clock__DOT__outclk_0 = level;
            dut.eval();
            if (level) on_sys();
        } else if (t == next_pix) {
            next_pix += kPixHalfPs;
            uint8_t level = !root.top__DOT__video_clock__DOT__outclk_0;
            root.top__DOT__video_clock__DOT__outclk_0 = level;
            dut.eval();
            if (level) on_pix();
        } else {
            next_audio += kAudioHalfPs;
            dut.FPGA_CLK1_50 = !dut.FPGA_CLK1_50;
            dut.eval();
        }
    }

    void set_video(bool on) {
        video = on;
        if (on) next_pix = now + 1;
    }

    void on_sys() {
        if (root.top__DOT__machine__DOT__cycle_clock == 0) {
            cpu_cycles++;
            if (root.top__DOT__machine__DOT__bus_we) {
                uint16_t addr = root.top__DOT__machine__DOT__bus_addr;
                if (addr == 0x03F0) result = root.top__DOT__machine__DOT__bus_wdata;
                if (addr == 0x03F1) stage = root.top__DOT__machine__DOT__bus_wdata;
            }
        }
        int speaker = root.top__DOT__machine__DOT__speaker;
        if (speaker != last_speaker) {
            speaker_edges++;
            last_speaker = speaker;
        }
        int32_t sample = static_cast<int16_t>(root.top__DOT__audio_sample);
        peak_sample = std::max(peak_sample, sample < 0 ? -sample : sample);
    }

    void on_pix() {
        if (!capturing) return;
        if (dut.HDMI_TX_VS && !last_vsync) {
            if (!seen_vsync) {
                seen_vsync = true;
                cap_y = -1;
            } else if (cap_y >= kHeight - 1) {
                capturing = false;
                captured = true;
            }
        }
        last_vsync = dut.HDMI_TX_VS;
        if (seen_vsync && dut.HDMI_TX_DE) {
            if (!last_de) {
                cap_y++;
                cap_x = 0;
            }
            if (cap_y >= 0 && cap_y < kHeight && cap_x < kWidth) {
                size_t o = (static_cast<size_t>(cap_y) * kWidth + cap_x) * 3;
                uint32_t d = dut.HDMI_TX_D;
                frame[o] = d >> 16;
                frame[o + 1] = d >> 8;
                frame[o + 2] = d;
            }
            cap_x++;
        }
        last_de = dut.HDMI_TX_DE;
    }

    void run_ps(uint64_t ps) {
        uint64_t end = now + ps;
        while (now < end) step();
    }

    template <typename Pred>
    void run_until(const char* what, uint64_t timeout_ps, Pred pred) {
        uint64_t end = now + timeout_ps;
        while (now < end) {
            step();
            if (pred()) return;
        }
        fail(std::string("timeout waiting for ") + what + " (result " + std::to_string(result) +
             ", stage " + std::to_string(stage) + ")");
    }

    uint32_t gpi() const { return root.top__DOT__hps_gp__DOT__observed_gpi; }

    // One GP transaction; returns the GPI word.
    uint32_t request(uint32_t opcode, uint32_t index, uint32_t argument) {
        uint32_t fields = opcode << 24 | index << 16 | argument;
        root.top__DOT__hps_gp__DOT__gp_out = fields | (toggle ? 0x80000000u : 0);
        run_ps(4 * kSysHalfPs * 2);
        toggle = !toggle;
        root.top__DOT__hps_gp__DOT__gp_out = fields | (toggle ? 0x80000000u : 0);
        for (int i = 0; i < 64; ++i) {
            run_ps(kSysHalfPs * 2);
            uint32_t word = gpi();
            if ((word >> 24) == 0xf5 && ((word >> 23) & 1) == static_cast<uint32_t>(toggle)) return word;
        }
        fail("mailbox did not acknowledge");
    }

    uint16_t ok(uint32_t opcode, uint32_t index, uint32_t argument, const char* what) {
        uint32_t word = request(opcode, index, argument);
        if (word & 0x400000)
            fail(std::string(what) + " rejected with error " + std::to_string(word & 0xffff));
        return static_cast<uint16_t>(word);
    }

    void keys(const uint16_t rows[9]) {
        for (int r = 0; r < 9; ++r) ok(3, r, rows[r], "keyboard row");
    }

    void press(int usage, uint16_t modifiers = 0) {
        uint16_t rows[9] = {};
        rows[usage / 16] = static_cast<uint16_t>(1u << (usage % 16));
        rows[8] = modifiers;
        keys(rows);
        run_ps(3'000'000'000);  // hold 3 ms: past the 1 ms quiet period, below the repeat delay
        uint16_t none[9] = {};
        keys(none);
        run_ps(3'000'000'000);  // a release shorter than the quiet period is not seen
    }

    bool capture(const std::string& path) {
        frame.assign(static_cast<size_t>(kWidth) * kHeight * 3, 0);
        set_video(true);
        capturing = true;
        captured = false;
        seen_vsync = false;
        last_vsync = dut.HDMI_TX_VS;
        run_until("frame", 60'000'000'000ull, [&] { return captured; });
        std::ofstream out(path, std::ios::binary);
        out << "P6\n" << kWidth << " " << kHeight << "\n255\n";
        out.write(reinterpret_cast<const char*>(frame.data()), static_cast<std::streamsize>(frame.size()));
        return true;
    }
};

uint32_t crc32(const std::vector<uint8_t>& bytes) {
    uint32_t c = 0xffffffffu;
    for (uint8_t b : bytes) {
        c ^= b;
        for (int i = 0; i < 8; ++i) c = (c & 1) ? (c >> 1) ^ 0xedb88320u : c >> 1;
    }
    return c ^ 0xffffffffu;
}

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    std::string disk_path = "build/diagnostics/fes-apple2/disk.dsk";
    std::string out_dir = "build/sim/fes-apple2-board";
    if (argc > 1) disk_path = argv[1];
    if (argc > 2) out_dir = argv[2];
    std::ifstream in(disk_path, std::ios::binary);
    std::vector<uint8_t> disk((std::istreambuf_iterator<char>(in)), {});
    if (disk.size() != 143360) fail("synthetic disk must be 143,360 bytes");

    auto board = std::make_unique<Board>();
    Board& b = *board;
    b.run_ps(1'000'000);

    // Identity: fes.computer tag 4 with all five capabilities.
    const uint16_t expected[8] = {0x4546, 0x3153, 1, 0, 4, 1, 0, 0x1f};
    for (int word = 0; word < 16; ++word) {
        uint16_t value = b.ok(1, word, 0, "identity");
        if (word < 8 && value != expected[word]) fail("identity word " + std::to_string(word));
    }
    if (b.ok(5, 0, 0, "info") != 0x3000 || b.ok(5, 1, 0, "info") != 2 ||
        b.ok(5, 2, 0, "info") != 0x3000 || b.ok(5, 3, 0, "info") != 2 ||
        b.ok(5, 4, 0, "info") != 512 || b.ok(5, 5, 0, "info") != 1)
        fail("unit 0 must advertise exactly 143,360 bytes and start empty");

    // Release with an empty drive; the diagnostic runs its self tests.
    b.set_video(false);
    b.ok(2, 0, 1, "release");
    b.run_until("self tests", 3'000'000'000'000ull, [&] { return b.result == 0x11 || b.result >= 0xE0; });
    if (b.result != 0x11) fail("self tests failed");
    std::printf("released without media; self tests passed after %llu CPU cycles\n",
                static_cast<unsigned long long>(b.cpu_cycles));

    // HID key translation through the firmware's echo of STAGE.
    struct Key { int usage; uint16_t modifiers; int code; const char* name; };
    const Key checks[] = {
        {0x04, 0x00, 0xC1, "A"}, {0x04, 0x02, 0xC1, "Shift+A"}, {0x1F, 0x02, 0xC0, "Shift+2 (@)"},
        {0x06, 0x01, 0x83, "Ctrl+C"}, {0x28, 0x00, 0x8D, "Return"}, {0x29, 0x00, 0x9B, "Escape"},
        {0x50, 0x00, 0x88, "Left"}, {0x4F, 0x00, 0x95, "Right"}, {0x33, 0x02, 0xBA, "Shift+; (:)"},
    };
    for (const Key& k : checks) {
        b.stage = -1;
        b.press(k.usage, k.modifiers);
        b.run_until(k.name, 200'000'000'000ull, [&] { return b.stage >= 0; });
        if (b.stage != k.code) fail(std::string(k.name) + " produced " + std::to_string(b.stage));
    }
    std::printf("HID keyboard translation matches (%d keys)\n", int(sizeof checks / sizeof checks[0]));
    if (b.speaker_edges < 2 || b.peak_sample < 4000)
        fail("echoed keys must click the speaker into the audio mix");

    // Live insert of the disk into unit 0 while the machine runs.
    uint32_t crc = crc32(disk);
    b.ok(6, 0, disk.size() & 0xffff, "begin");
    b.ok(6, 1, disk.size() >> 16, "begin");
    b.ok(6, 2, crc & 0xffff, "begin");
    b.ok(6, 3, crc >> 16, "begin");
    for (size_t offset = 0; offset < disk.size(); offset += 512) {
        b.ok(7, 0, offset & 0xffff, "chunk");
        b.ok(7, 1, offset >> 16, "chunk");
        b.ok(7, 2, 512, "chunk");
        for (int ordinal = 0; ordinal < 256; ++ordinal)
            b.ok(8, ordinal, disk[offset + 2 * ordinal] | disk[offset + 2 * ordinal + 1] << 8, "data");
    }
    b.ok(9, 0, 0, "commit");
    if (b.ok(5, 5, 0, "info") != 3) fail("unit 0 must be ready after commit");
    std::printf("disk inserted live (%zu bytes, CRC %08x)\n", disk.size(), crc);

    // Boot it from the keyboard.
    b.result = -1;
    b.press(0x05);  // B
    b.run_until("disk boot", 12'000'000'000'000ull, [&] { return b.result == 0xA5 || (b.result >= 0xE0 && b.result <= 0xEF); });
    if (b.result != 0xA5) fail("disk boot failed with " + std::to_string(b.result));
    std::printf("disk boot verified after %llu CPU cycles\n", static_cast<unsigned long long>(b.cpu_cycles));
    b.capture(out_dir + "/disk.ppm");

    // Live eject keeps the machine running; Control+F12 is a warm reset.
    b.ok(10, 0, 0, "eject");
    if (b.ok(5, 5, 0, "info") != 1) fail("unit 0 must be empty after eject");
    uint64_t cycles = b.cpu_cycles;
    b.run_ps(1'000'000'000);
    if (b.cpu_cycles <= cycles) fail("eject must not stop the machine");
    b.result = -1;
    b.press(0x45, 0x01);  // Control+F12
    b.run_until("warm reset", 3'000'000'000'000ull, [&] { return b.result == 0x11; });
    std::printf("eject kept the machine running; Control+F12 reset re-ran the self tests\n");
    b.ok(2, 0, 0, "hold");
    std::printf("PASS\n");
    return 0;
}
